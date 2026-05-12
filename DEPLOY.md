# Deploying HVO to the cloud

Walks through deploying the four Go services to **Fly.io**, with **Neon** as
the Postgres host and **AWS S3** as the photo/avatar bucket. Total cost on
sane usage: $0–5/month. Total wall-clock time: ~1–2 hours, mostly waiting
on signups.

The iOS client is **untouched** in this doc — once the cloud URLs are live,
all you need is to flip `src/config.ts`.

---

## Step 0 — One-time signups

Open three tabs and create accounts. GitHub OAuth is fine for all three.

| Service | URL | Used for |
|---|---|---|
| **Fly.io** | https://fly.io/app/sign-in | Runs the four Go services |
| **Neon** | https://neon.tech | Hosted Postgres (free 0.5GB) |
| **AWS** | https://aws.amazon.com | S3 bucket for photos + avatars |

Install the Fly CLI:

```bash
brew install flyctl
fly auth login
```

Install the `migrate` CLI (we'll need it once to apply migrations to Neon):

```bash
brew install golang-migrate
```

---

## Step 1 — Create the Neon database

1. Inside Neon dashboard, hit **Create Project**.
   - Name: `hvo`
   - Postgres version: 16 (matches local Docker)
   - Region: close to Fly's `sjc` (Neon's `us-west-2` or `aws-us-east-1`)
2. Copy the connection string Neon shows you. Looks like:
   ```
   postgres://<user>:<password>@ep-foo-bar.us-west-2.aws.neon.tech/hvo?sslmode=require
   ```
3. Apply all migrations against it:
   ```bash
   DATABASE_URL='postgres://...neon.tech/hvo?sslmode=require' make migrate-remote
   ```
   You should see all four services migrated (`user-svc`, `property-svc`,
   `media-svc`, `ranking-svc`) reporting `N/u <name>`.

Connection string goes into `fly secrets` later — **never commit it**.

---

## Step 2 — Create the S3 bucket + IAM user (AWS)

### 2a. Bucket

1. AWS Console → **S3** → Create bucket.
   - Name: `hvo-prod` (must be globally unique — append your handle if taken)
   - Region: `us-west-2` (or wherever you want; same as Fly is fine)
   - **Block all public access**: leave on — we use presigned PUT for upload
     and a separate presigned GET / public-read prefix for display.
2. (Optional, for direct photo display) Add a bucket policy so the
   `media/` and `avatars/` prefixes are public-read:
   ```json
   {
     "Version": "2012-10-17",
     "Statement": [{
       "Sid": "PublicReadMediaAndAvatars",
       "Effect": "Allow",
       "Principal": "*",
       "Action": "s3:GetObject",
       "Resource": [
         "arn:aws:s3:::hvo-prod/media/*",
         "arn:aws:s3:::hvo-prod/avatars/*"
       ]
     }]
   }
   ```
   You also need to flip off "Block public bucket policies" for this
   particular bucket. Be deliberate about this.
3. CORS (so iOS can PUT directly from the device):
   ```json
   [
     {
       "AllowedHeaders": ["*"],
       "AllowedMethods": ["GET", "PUT", "HEAD"],
       "AllowedOrigins": ["*"],
       "ExposeHeaders": []
     }
   ]
   ```

### 2b. IAM user with programmatic access

1. AWS Console → **IAM** → Users → Create user.
   - Name: `hvo-app`
   - Access: **Programmatic access** (no console).
2. Attach an inline policy (least privilege):
   ```json
   {
     "Version": "2012-10-17",
     "Statement": [{
       "Effect": "Allow",
       "Action": ["s3:PutObject", "s3:GetObject", "s3:DeleteObject", "s3:HeadObject"],
       "Resource": "arn:aws:s3:::hvo-prod/*"
     }]
   }
   ```
3. Create an access key for this user. You'll get:
   ```
   AWS_ACCESS_KEY_ID=AKIA...
   AWS_SECRET_ACCESS_KEY=...
   ```
   Save them — AWS shows the secret **once**.

---

## Step 3 — Deploy each Fly app

Run **once per service**. Each becomes its own Fly app with its own URL.

```bash
# Pick a unique global name; fly will offer alternatives if taken.
cd services/user-svc
fly launch --copy-config --no-deploy
#   ? Choose an app name: hvo-user-svc-olivia
#   ? Choose region: sjc
#   ? Would you like to set up a Postgresql database now? No
#   ? Would you like to deploy now? No

fly secrets set \
  DATABASE_URL='postgres://...neon.tech/hvo?sslmode=require' \
  JWT_SECRET="$(openssl rand -hex 32)" \
  AWS_REGION='us-west-2' \
  AWS_ACCESS_KEY_ID='AKIA...' \
  AWS_SECRET_ACCESS_KEY='...' \
  S3_BUCKET='hvo-prod'

fly deploy
```

`fly deploy` builds the Docker image, ships it, and gives you a URL like
`https://hvo-user-svc-olivia.fly.dev`. `fly logs` tails. `fly status` shows
the running machine.

Repeat for `property-svc`, `media-svc`, `ranking-svc`. The secret set for
each varies:

| Service | Required secrets |
|---|---|
| user-svc | `DATABASE_URL`, `JWT_SECRET`, `AWS_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `S3_BUCKET` |
| property-svc | `DATABASE_URL`, `JWT_SECRET` |
| media-svc | `DATABASE_URL`, `JWT_SECRET`, `AWS_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `S3_BUCKET` |
| ranking-svc | `DATABASE_URL`, `JWT_SECRET`, *(optional)* `ANTHROPIC_API_KEY` |

Use the **same** `JWT_SECRET` across all four — they all verify tokens
issued by user-svc.

For media-svc, also set:
```bash
fly secrets set AWS_S3_PATH_STYLE=false S3_AUTO_CREATE_BUCKET=false
```
(LocalStack quirks; in real AWS we don't path-style and don't auto-create.)

---

## Step 4 — Point iOS at the cloud

```ts
// src/config.ts
const HOST_USER     = 'https://hvo-user-svc-olivia.fly.dev';
const HOST_PROPERTY = 'https://hvo-property-svc-olivia.fly.dev';
const HOST_MEDIA    = 'https://hvo-media-svc-olivia.fly.dev';
const HOST_RANKING  = 'https://hvo-ranking-svc-olivia.fly.dev';

export const API = {
  USER:     HOST_USER,
  PROPERTY: HOST_PROPERTY,
  MEDIA:    HOST_MEDIA,
  RANKING:  HOST_RANKING,
};
```

(Or read from `EXPO_PUBLIC_*_HOST` env vars for cleaner separation between
local + cloud builds.)

Run the app: `npx expo start`, scan with iPhone. Sign in. Add a property.
Upload a photo. Confirm it lands in your real S3 bucket.

---

## Health checks

After deploy, ping each one:

```bash
for s in user property media ranking; do
  echo -n "$s: "
  curl -s -o /dev/null -w "%{http_code}\n" "https://hvo-$s-svc-olivia.fly.dev/healthz"
done
```

All should return `200`. Then check `readyz` on each — it pings PG, so
that's a good end-to-end sanity check.

---

## Common snags

- **Migration fails with `pq: SSL is required`** — make sure Neon URL has
  `?sslmode=require`.
- **`fly deploy` builds but app crashes immediately** — run `fly logs` and
  look for missing env vars. Usually JWT_SECRET or DATABASE_URL.
- **iOS gets `network request failed`** — Fly URLs are HTTPS only. Make
  sure `src/config.ts` has `https://` not `http://`.
- **Photo upload returns 403 from S3** — bucket CORS not set, or the IAM
  user doesn't have PutObject on `hvo-prod/*`.
- **Free tier exhausted on Fly** — first VM per app is free up to a point;
  if you exceed, set `min_machines_running = 0` and `auto_stop_machines =
  "stop"` in fly.toml to scale-to-zero on idle services.

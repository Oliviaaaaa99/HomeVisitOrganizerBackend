#!/usr/bin/env bash
#
# demo-invite.sh — Add (or refresh) a user in the user-svc demo gate.
#
# Workflow:
#   1. Generates a 12-char random invite code for the given email.
#   2. Appends/updates a local file (~/.hvo-deploy/demo-users), which is the
#      source of truth on your laptop. Never committed.
#   3. Pushes the joined contents to user-svc as `ALLOWED_USERS` via
#      `fly secrets set`. ~30s rolling restart later, the user can sign in.
#   4. Prints a copy-pasteable invitation card you can DM the tester.
#
# Usage:
#   scripts/demo-invite.sh <email>
#
# Revoke:
#   scripts/demo-revoke.sh <email>

set -euo pipefail

# ---- args -------------------------------------------------------------------
email="${1:-}"
if [[ -z "$email" ]]; then
  echo "usage: $0 <email>" >&2
  exit 2
fi
email_lc=$(echo "$email" | tr '[:upper:]' '[:lower:]')

# ---- paths ------------------------------------------------------------------
repo_root=$(cd "$(dirname "$0")/.." && pwd)
fly_config="$repo_root/services/user-svc/fly.toml"
state_dir="$HOME/.hvo-deploy"
state_file="$state_dir/demo-users"
mkdir -p "$state_dir"
touch "$state_file"
chmod 600 "$state_file"

# ---- generate code ----------------------------------------------------------
# 6 alphanumeric chars from /dev/urandom. ~35 bits of entropy — at ~100
# req/sec to /auth/exchange that's ~18 years to brute force, which is fine
# for a small portfolio demo behind Fly's network rate-limiting.
code=$(LC_ALL=C tr -dc 'a-zA-Z0-9' < /dev/urandom | head -c 6)

# ---- update local file ------------------------------------------------------
# Remove any existing line for this email, then append fresh.
tmp=$(mktemp)
grep -v "^${email_lc}:" "$state_file" > "$tmp" || true
echo "${email_lc}:${code}" >> "$tmp"
mv "$tmp" "$state_file"
chmod 600 "$state_file"

# ---- push to fly ------------------------------------------------------------
joined=$(tr '\n' ',' < "$state_file" | sed 's/,$//')

echo ">>> Updating ALLOWED_USERS on hvo-user-svc-olivia ($(grep -c . "$state_file") user(s))..."
fly secrets set --config "$fly_config" ALLOWED_USERS="$joined"

# ---- invitation card --------------------------------------------------------
cat <<EOF

────────────────────────────────────────────────────────
  Invitation for ${email}
────────────────────────────────────────────────────────
  Demo URL:  https://home-visit-organizer-ios.vercel.app
  Email:     ${email}
  Passcode:  ${code}

  Sign in by entering the email + passcode above.
  Passcode is one-of-a-kind — only this email can use it.
────────────────────────────────────────────────────────

EOF

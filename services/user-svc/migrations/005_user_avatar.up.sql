-- Add the S3 key for the user's avatar. The actual image lives in the
-- shared media bucket under the prefix `avatars/<user_id>/...`. The column
-- is nullable: most users won't set an avatar.
ALTER TABLE users ADD COLUMN avatar_s3_key TEXT;

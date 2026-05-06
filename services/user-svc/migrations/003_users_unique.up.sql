-- 001 used UNIQUE(external_id) which is wrong: two providers (apple, google)
-- can independently issue the same external_id. The right key is the composite
-- (provider, external_id), needed by the auth-exchange ON CONFLICT upsert.

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_external_id_key;

DROP INDEX IF EXISTS idx_users_external;

ALTER TABLE users
  ADD CONSTRAINT users_provider_external_id_key
  UNIQUE (provider, external_id);

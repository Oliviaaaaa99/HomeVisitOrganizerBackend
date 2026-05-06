ALTER TABLE users DROP CONSTRAINT IF EXISTS users_provider_external_id_key;
ALTER TABLE users ADD CONSTRAINT users_external_id_key UNIQUE (external_id);
CREATE INDEX idx_users_external ON users (provider, external_id);

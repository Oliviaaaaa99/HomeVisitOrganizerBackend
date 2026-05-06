-- The (apple, google) CHECK in 001 is too narrow:
--   - 'dev' is needed for local-dev / CI flows.
--   - Future providers (e.g. 'magic_link') would each need a migration.
-- The IDP registry in code already gates which providers are accepted,
-- so the schema-level CHECK is redundant. Drop it and rely on app-level checks.

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_provider_check;

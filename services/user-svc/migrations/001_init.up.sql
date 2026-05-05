CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  external_id TEXT UNIQUE NOT NULL,
  provider TEXT NOT NULL CHECK (provider IN ('apple', 'google')),
  email_hash TEXT,
  preferences_json JSONB NOT NULL DEFAULT '{}',
  commute_origin GEOGRAPHY(POINT, 4326),
  retention_overrides JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_users_external ON users (provider, external_id);

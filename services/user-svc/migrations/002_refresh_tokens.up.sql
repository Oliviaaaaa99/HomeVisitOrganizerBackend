CREATE TABLE refresh_tokens (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  user_agent TEXT,
  ip_addr INET
);

-- Index for revoking all tokens for a user, and looking up active tokens.
CREATE INDEX idx_refresh_tokens_user_active
  ON refresh_tokens (user_id)
  WHERE revoked_at IS NULL;

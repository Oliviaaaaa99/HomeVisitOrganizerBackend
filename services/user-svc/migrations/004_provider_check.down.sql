ALTER TABLE users
  ADD CONSTRAINT users_provider_check
  CHECK (provider IN ('apple', 'google'));

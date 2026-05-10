-- A user-facing display name. Optional — nothing breaks if it's NULL,
-- the iOS client falls back to the email-derived initial.
ALTER TABLE users ADD COLUMN display_name TEXT;

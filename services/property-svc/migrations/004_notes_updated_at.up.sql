-- Notes are now editable, so we need to know when one was last touched.
-- Existing rows backfill from created_at so the column is never NULL.

ALTER TABLE notes
  ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

UPDATE notes SET updated_at = created_at;

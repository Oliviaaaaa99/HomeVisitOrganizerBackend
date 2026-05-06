-- Free-text notes attached to a property. Multi-row to allow per-tour notes
-- ("first visit", "second visit") rather than one mutating blob.

CREATE TABLE notes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  property_id UUID NOT NULL REFERENCES properties(id) ON DELETE CASCADE,
  body TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_notes_property ON notes (property_id, created_at DESC);

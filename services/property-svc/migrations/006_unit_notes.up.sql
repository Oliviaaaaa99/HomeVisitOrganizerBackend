-- Notes can now optionally attach to a specific unit. Existing notes (which
-- have no unit_id) stay property-level; new notes created via the unit
-- endpoint set unit_id. property_id remains required so we can always
-- look up "all notes touching this property" without a join.

ALTER TABLE notes
  ADD COLUMN unit_id UUID REFERENCES units(id) ON DELETE CASCADE;

-- Hot path: list a unit's notes.
CREATE INDEX idx_notes_unit ON notes (unit_id, created_at DESC) WHERE unit_id IS NOT NULL;

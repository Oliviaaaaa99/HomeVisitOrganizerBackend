-- Move tour-state (toured / shortlisted / rejected / archived) from
-- properties to units. The unit is the actual decision the user makes
-- (you sign a lease on a specific apartment, not on a building), so all
-- the decision-state lives there. The property keeps only its
-- organizational metadata (address, kind, source_url, location).

ALTER TABLE units
  ADD COLUMN status TEXT NOT NULL DEFAULT 'toured'
    CHECK (status IN ('toured', 'shortlisted', 'rejected', 'archived'));

-- Carry over the existing status: every unit inherits its parent
-- property's status. If a property had multiple units, they all get
-- the same status — semantically a slight overstatement (the user only
-- ever set one status for the whole place), but it's the safe default.
-- Users can re-tag specific units after the migration.
UPDATE units
SET status = properties.status
FROM properties
WHERE units.property_id = properties.id;

-- Hot path: list a unit's siblings filtered by status.
CREATE INDEX idx_units_property_status ON units (property_id, status);

ALTER TABLE properties DROP COLUMN status;

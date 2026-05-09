-- Rollback: put status back on properties. Picks the most-decisive
-- status across each property's units (shortlisted > toured > rejected
-- > archived) so we don't downgrade a property that has any
-- shortlisted unit just because its sibling was rejected.

ALTER TABLE properties
  ADD COLUMN status TEXT NOT NULL DEFAULT 'toured'
    CHECK (status IN ('toured', 'shortlisted', 'rejected', 'archived'));

UPDATE properties p
SET status = COALESCE(
  (SELECT u.status FROM units u
    WHERE u.property_id = p.id
    ORDER BY CASE u.status
      WHEN 'shortlisted' THEN 1
      WHEN 'toured'      THEN 2
      WHEN 'rejected'    THEN 3
      WHEN 'archived'    THEN 4
    END
    LIMIT 1),
  'toured'
);

CREATE INDEX idx_properties_user_status ON properties (user_id, status);

DROP INDEX IF EXISTS idx_units_property_status;
ALTER TABLE units DROP COLUMN status;

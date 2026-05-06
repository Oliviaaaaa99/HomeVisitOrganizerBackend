-- Properties table — owned by property-svc.
-- See Tech Design Doc §7 for the full schema rationale.

CREATE TABLE properties (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL,
  address TEXT NOT NULL,
  -- PostGIS point for cheap geo filters (neighborhood bounding box etc.)
  location GEOGRAPHY(POINT, 4326),
  kind TEXT NOT NULL CHECK (kind IN ('rental', 'for_sale')),
  source_url TEXT,
  status TEXT NOT NULL DEFAULT 'toured'
    CHECK (status IN ('toured', 'shortlisted', 'rejected', 'archived')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Filter by user + status is the most common Home-screen query (list, list filtered).
CREATE INDEX idx_properties_user_status ON properties (user_id, status);
-- Geo lookups (future "properties near here" UX).
CREATE INDEX idx_properties_location ON properties USING GIST (location);

-- Units — the smallest tracked entity. A property has 1+ units (e.g., a
-- community has Studio / 1B / 2B variants the user is comparing).

CREATE TABLE units (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  property_id UUID NOT NULL REFERENCES properties(id) ON DELETE CASCADE,
  unit_label TEXT,
  unit_type TEXT NOT NULL,
  -- price in cents to avoid float rounding; ranking/sort still works fine.
  price_cents BIGINT,
  sqft INTEGER,
  beds INTEGER,
  baths NUMERIC(3, 1),
  available_from DATE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_units_property ON units (property_id);

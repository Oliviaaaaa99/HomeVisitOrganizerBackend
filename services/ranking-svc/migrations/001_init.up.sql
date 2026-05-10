-- ranking-svc owns user_preferences (the inputs to scoring) and will eventually
-- own ranking snapshots. v1 stores prefs only; scores are computed on demand.

CREATE TABLE user_preferences (
  user_id UUID PRIMARY KEY,
  -- Free-form work address. Lat/lng are optional and only useful once we
  -- wire commute time (Google Distance Matrix); v1 leaves them null.
  work_address TEXT,
  work_lat FLOAT8,
  work_lng FLOAT8,
  -- Hard constraints. NULL = no opinion.
  budget_min_cents BIGINT,
  budget_max_cents BIGINT,
  min_beds INTEGER,
  min_baths NUMERIC(3, 1),
  min_sqft INTEGER,
  -- Soft preferences (0-100 weights — UI can present as sliders).
  -- NULL means "use default weight" so we don't force a complete profile
  -- before ranking is useful.
  weight_price INTEGER,
  weight_size INTEGER,
  weight_commute INTEGER,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

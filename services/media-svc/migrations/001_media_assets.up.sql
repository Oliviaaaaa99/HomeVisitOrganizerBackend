-- media_assets — owned by media-svc.
-- One row per uploaded photo / short video / long video. The raw object lives
-- in S3 under s3_key; thumb_key is set after async processing in M4.

CREATE TABLE media_assets (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  unit_id UUID NOT NULL,
  user_id UUID NOT NULL,                     -- denormalized for cheap "delete-my-data" sweeps
  media_type TEXT NOT NULL CHECK (media_type IN ('photo', 'video_short', 'video_long')),
  s3_key TEXT NOT NULL UNIQUE,
  thumb_key TEXT,
  duration_s NUMERIC(4, 1),
  caption TEXT,
  captured_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL,
  deleted_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Hot path: list a unit's active media (omitting soft-deleted).
CREATE INDEX idx_media_unit_active ON media_assets (unit_id) WHERE deleted_at IS NULL;
-- Retention sweep: find rows past expiry that still need a hard delete.
CREATE INDEX idx_media_expires ON media_assets (expires_at) WHERE deleted_at IS NULL;
-- "Delete my data" sweep.
CREATE INDEX idx_media_user ON media_assets (user_id);

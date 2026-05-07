package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MediaAsset mirrors a row in the media_assets table (active or deleted).
type MediaAsset struct {
	ID         uuid.UUID
	UnitID     uuid.UUID
	UserID     uuid.UUID
	MediaType  string
	S3Key      string
	ThumbKey   *string
	DurationS  *float64
	Caption    *string
	CapturedAt time.Time
	ExpiresAt  time.Time
	DeletedAt  *time.Time
	CreatedAt  time.Time
}

// Media is the data-access layer for media_assets.
type Media struct {
	pool *pgxpool.Pool
}

// NewMedia wraps a pgxpool.
func NewMedia(pool *pgxpool.Pool) *Media {
	return &Media{pool: pool}
}

// CommitInput captures one media item's metadata at commit time.
type CommitInput struct {
	UnitID    uuid.UUID
	UserID    uuid.UUID
	MediaType string
	S3Key     string
	DurationS *float64
	Caption   *string
	ExpiresAt time.Time
}

// Insert writes a single MediaAsset row. Returns the canonical row.
func (m *Media) Insert(ctx context.Context, in CommitInput) (*MediaAsset, error) {
	const q = `
		INSERT INTO media_assets
		  (unit_id, user_id, media_type, s3_key, duration_s, caption, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, unit_id, user_id, media_type, s3_key, thumb_key,
		          duration_s, caption, captured_at, expires_at, deleted_at, created_at`
	row := m.pool.QueryRow(ctx, q,
		in.UnitID, in.UserID, in.MediaType, in.S3Key, in.DurationS, in.Caption, in.ExpiresAt)
	return scanMedia(row)
}

// CountByType returns the count of active (not soft-deleted) media of a type
// for a unit. Used to enforce the per-unit quotas (10 photos / 3 short / 3 long).
func (m *Media) CountByType(ctx context.Context, unitID uuid.UUID, mediaType string) (int, error) {
	const q = `
		SELECT COUNT(*) FROM media_assets
		WHERE unit_id = $1 AND media_type = $2 AND deleted_at IS NULL`
	var n int
	if err := m.pool.QueryRow(ctx, q, unitID, mediaType).Scan(&n); err != nil {
		return 0, fmt.Errorf("count media: %w", err)
	}
	return n, nil
}

// ListActive returns a unit's active media, newest first.
func (m *Media) ListActive(ctx context.Context, unitID uuid.UUID) ([]*MediaAsset, error) {
	const q = `
		SELECT id, unit_id, user_id, media_type, s3_key, thumb_key,
		       duration_s, caption, captured_at, expires_at, deleted_at, created_at
		FROM media_assets
		WHERE unit_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC`
	rows, err := m.pool.Query(ctx, q, unitID)
	if err != nil {
		return nil, fmt.Errorf("list media: %w", err)
	}
	defer rows.Close()
	var out []*MediaAsset
	for rows.Next() {
		ma, err := scanMedia(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ma)
	}
	return out, rows.Err()
}

// FindOwned returns a media row IFF it belongs to the user, including deleted.
func (m *Media) FindOwned(ctx context.Context, id, userID uuid.UUID) (*MediaAsset, error) {
	const q = `
		SELECT id, unit_id, user_id, media_type, s3_key, thumb_key,
		       duration_s, caption, captured_at, expires_at, deleted_at, created_at
		FROM media_assets
		WHERE id = $1 AND user_id = $2`
	row := m.pool.QueryRow(ctx, q, id, userID)
	ma, err := scanMedia(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return ma, err
}

// UpdateCaption updates the caption on an active row owned by userID. An empty
// string clears the caption (stored as NULL). Returns the canonical updated row,
// or (nil, nil) if no matching active row exists.
func (m *Media) UpdateCaption(ctx context.Context, id, userID uuid.UUID, caption string) (*MediaAsset, error) {
	const q = `
		UPDATE media_assets
		SET caption = NULLIF($3, '')
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
		RETURNING id, unit_id, user_id, media_type, s3_key, thumb_key,
		          duration_s, caption, captured_at, expires_at, deleted_at, created_at`
	row := m.pool.QueryRow(ctx, q, id, userID, caption)
	ma, err := scanMedia(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return ma, err
}

// SoftDelete marks the row as deleted. Returns true if a row was affected.
// The S3 object stays in place — hard-delete happens in the retention sweeper.
func (m *Media) SoftDelete(ctx context.Context, id, userID uuid.UUID) (bool, error) {
	const q = `
		UPDATE media_assets SET deleted_at = now()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`
	tag, err := m.pool.Exec(ctx, q, id, userID)
	if err != nil {
		return false, fmt.Errorf("soft delete: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanMedia(s scannable) (*MediaAsset, error) {
	var ma MediaAsset
	if err := s.Scan(&ma.ID, &ma.UnitID, &ma.UserID, &ma.MediaType, &ma.S3Key, &ma.ThumbKey,
		&ma.DurationS, &ma.Caption, &ma.CapturedAt, &ma.ExpiresAt, &ma.DeletedAt, &ma.CreatedAt); err != nil {
		return nil, err
	}
	return &ma, nil
}

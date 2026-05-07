// Package store wraps Postgres queries for property-svc.
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

// Property mirrors a row in the properties table (without nested units/notes).
type Property struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Address   string
	Latitude  *float64
	Longitude *float64
	Kind      string
	SourceURL *string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Properties is the data-access layer for the properties table.
type Properties struct {
	pool *pgxpool.Pool
}

// NewProperties wraps a pgxpool.
func NewProperties(pool *pgxpool.Pool) *Properties {
	return &Properties{pool: pool}
}

// CreateInput captures the fields the caller supplies on POST /v1/properties.
type CreateInput struct {
	UserID    uuid.UUID
	Address   string
	Kind      string
	SourceURL string
	Latitude  *float64
	Longitude *float64
}

// Create inserts a new property and returns the canonical row.
func (p *Properties) Create(ctx context.Context, in CreateInput) (*Property, error) {
	const q = `
		INSERT INTO properties (user_id, address, location, kind, source_url)
		VALUES (
			$1, $2,
			CASE WHEN $3::FLOAT8 IS NOT NULL AND $4::FLOAT8 IS NOT NULL
			     THEN ST_SetSRID(ST_MakePoint($4::FLOAT8, $3::FLOAT8), 4326)::GEOGRAPHY
			     ELSE NULL END,
			$5, NULLIF($6, '')
		)
		RETURNING id, user_id, address,
		          ST_Y(location::geometry), ST_X(location::geometry),
		          kind, source_url, status, created_at, updated_at`
	row := p.pool.QueryRow(ctx, q, in.UserID, in.Address, in.Latitude, in.Longitude, in.Kind, in.SourceURL)
	return scanProperty(row)
}

// FindOwned returns a property by id, but only if it belongs to userID.
func (p *Properties) FindOwned(ctx context.Context, id, userID uuid.UUID) (*Property, error) {
	const q = `
		SELECT id, user_id, address,
		       ST_Y(location::geometry), ST_X(location::geometry),
		       kind, source_url, status, created_at, updated_at
		FROM properties WHERE id = $1 AND user_id = $2`
	row := p.pool.QueryRow(ctx, q, id, userID)
	prop, err := scanProperty(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return prop, err
}

// ListInput captures filter / pagination for GET /v1/properties.
type ListInput struct {
	UserID uuid.UUID
	Status string // empty = any
	Kind   string // empty = any
	Limit  int
	Offset int
}

// List returns the user's properties, filtered. Most-recent first.
func (p *Properties) List(ctx context.Context, in ListInput) ([]*Property, error) {
	const q = `
		SELECT id, user_id, address,
		       ST_Y(location::geometry), ST_X(location::geometry),
		       kind, source_url, status, created_at, updated_at
		FROM properties
		WHERE user_id = $1
		  AND ($2 = '' OR status = $2)
		  AND ($3 = '' OR kind = $3)
		ORDER BY created_at DESC
		LIMIT $4 OFFSET $5`
	rows, err := p.pool.Query(ctx, q, in.UserID, in.Status, in.Kind, in.Limit, in.Offset)
	if err != nil {
		return nil, fmt.Errorf("list properties: %w", err)
	}
	defer rows.Close()
	var out []*Property
	for rows.Next() {
		prop, err := scanProperty(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, prop)
	}
	return out, rows.Err()
}

// UpdateStatus transitions a property's status; returns the updated row.
// Kept as a thin wrapper for handlers that only need to flip status.
func (p *Properties) UpdateStatus(ctx context.Context, id, userID uuid.UUID, status string) (*Property, error) {
	return p.Update(ctx, UpdateInput{ID: id, UserID: userID, Status: &status})
}

// UpdateInput is a partial-update payload. nil pointer = leave field as-is.
type UpdateInput struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Address   *string
	Kind      *string
	SourceURL *string
	Latitude  *float64
	Longitude *float64
	Status    *string
}

// Update applies a partial update and returns the canonical row. Any nil
// pointer field is left untouched. Latitude+Longitude must be sent together
// (either both or neither) — sending only one is treated as no-op for location.
func (p *Properties) Update(ctx context.Context, in UpdateInput) (*Property, error) {
	const q = `
		UPDATE properties SET
		  address    = COALESCE($3, address),
		  kind       = COALESCE($4, kind),
		  source_url = CASE WHEN $5::TEXT IS NULL THEN source_url ELSE NULLIF($5, '') END,
		  location   = CASE
		    WHEN $6::FLOAT8 IS NOT NULL AND $7::FLOAT8 IS NOT NULL
		      THEN ST_SetSRID(ST_MakePoint($7::FLOAT8, $6::FLOAT8), 4326)::GEOGRAPHY
		    ELSE location
		  END,
		  status     = COALESCE($8, status),
		  updated_at = now()
		WHERE id = $1 AND user_id = $2
		RETURNING id, user_id, address,
		          ST_Y(location::geometry), ST_X(location::geometry),
		          kind, source_url, status, created_at, updated_at`
	row := p.pool.QueryRow(ctx, q,
		in.ID, in.UserID,
		in.Address, in.Kind, in.SourceURL,
		in.Latitude, in.Longitude,
		in.Status,
	)
	prop, err := scanProperty(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return prop, err
}

// HardDelete removes a property and all of its dependents in a single
// transaction:
//
//   - media_assets rows for any unit under this property (no FK exists across
//     services, so we delete them explicitly while we're in the same DB)
//   - units (FK CASCADE on properties.id)
//   - notes (FK CASCADE on properties.id)
//   - the property row itself
//
// Returns true if the property was found and deleted. S3 objects for the
// deleted media are intentionally not removed here — the retention sweeper
// (M4) will clean them up. Archive-as-status remains available via
// PATCH /v1/properties/{id} {status: "archived"}.
func (p *Properties) HardDelete(ctx context.Context, id, userID uuid.UUID) (bool, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // best-effort on success path

	// Verify ownership up front so we can return a clean false-not-found.
	const ownQ = `SELECT 1 FROM properties WHERE id = $1 AND user_id = $2`
	var dummy int
	if err := tx.QueryRow(ctx, ownQ, id, userID).Scan(&dummy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("ownership check: %w", err)
	}

	// 1. media_assets — no FK, delete by unit_id IN (...).
	const delMediaQ = `
		DELETE FROM media_assets
		WHERE unit_id IN (SELECT id FROM units WHERE property_id = $1)`
	if _, err := tx.Exec(ctx, delMediaQ, id); err != nil {
		return false, fmt.Errorf("delete media: %w", err)
	}

	// 2. property — units & notes cascade automatically.
	const delPropQ = `DELETE FROM properties WHERE id = $1 AND user_id = $2`
	tag, err := tx.Exec(ctx, delPropQ, id, userID)
	if err != nil {
		return false, fmt.Errorf("delete property: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}
	return true, nil
}

// scannable is the subset of pgx.Row / pgx.Rows we need.
type scannable interface {
	Scan(dest ...any) error
}

func scanProperty(s scannable) (*Property, error) {
	var p Property
	if err := s.Scan(&p.ID, &p.UserID, &p.Address, &p.Latitude, &p.Longitude, &p.Kind, &p.SourceURL, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return &p, nil
}

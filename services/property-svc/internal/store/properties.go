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
// Returns nil if not owned by user.
func (p *Properties) UpdateStatus(ctx context.Context, id, userID uuid.UUID, status string) (*Property, error) {
	const q = `
		UPDATE properties SET status = $3, updated_at = now()
		WHERE id = $1 AND user_id = $2
		RETURNING id, user_id, address,
		          ST_Y(location::geometry), ST_X(location::geometry),
		          kind, source_url, status, created_at, updated_at`
	row := p.pool.QueryRow(ctx, q, id, userID, status)
	prop, err := scanProperty(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return prop, err
}

// Archive soft-deletes by setting status='archived'. Returns true if a row matched.
func (p *Properties) Archive(ctx context.Context, id, userID uuid.UUID) (bool, error) {
	const q = `UPDATE properties SET status = 'archived', updated_at = now() WHERE id = $1 AND user_id = $2`
	tag, err := p.pool.Exec(ctx, q, id, userID)
	if err != nil {
		return false, fmt.Errorf("archive: %w", err)
	}
	return tag.RowsAffected() > 0, nil
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

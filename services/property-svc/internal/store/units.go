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

// Unit mirrors a row in the units table.
type Unit struct {
	ID            uuid.UUID
	PropertyID    uuid.UUID
	UnitLabel     *string
	UnitType      string
	PriceCents    *int64
	Sqft          *int
	Beds          *int
	Baths         *float64
	AvailableFrom *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Units is the data-access layer for the units table.
type Units struct {
	pool *pgxpool.Pool
}

// NewUnits wraps a pgxpool.
func NewUnits(pool *pgxpool.Pool) *Units {
	return &Units{pool: pool}
}

// CreateInput captures the fields the caller supplies on POST /v1/properties/{id}/units.
type UnitInput struct {
	PropertyID    uuid.UUID
	UnitLabel     *string
	UnitType      string
	PriceCents    *int64
	Sqft          *int
	Beds          *int
	Baths         *float64
	AvailableFrom *time.Time
}

// Create inserts a unit into a property.
func (u *Units) Create(ctx context.Context, in UnitInput) (*Unit, error) {
	const q = `
		INSERT INTO units (property_id, unit_label, unit_type, price_cents, sqft, beds, baths, available_from)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, property_id, unit_label, unit_type, price_cents, sqft, beds, baths, available_from, created_at, updated_at`
	row := u.pool.QueryRow(ctx, q,
		in.PropertyID, in.UnitLabel, in.UnitType, in.PriceCents, in.Sqft, in.Beds, in.Baths, in.AvailableFrom)
	var unit Unit
	if err := row.Scan(&unit.ID, &unit.PropertyID, &unit.UnitLabel, &unit.UnitType,
		&unit.PriceCents, &unit.Sqft, &unit.Beds, &unit.Baths, &unit.AvailableFrom,
		&unit.CreatedAt, &unit.UpdatedAt); err != nil {
		return nil, fmt.Errorf("create unit: %w", err)
	}
	return &unit, nil
}

// UpdateUnitInput is the partial-update payload. nil pointer = leave field as-is.
// UserID is checked via subquery so cross-user mutations 404.
type UpdateUnitInput struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	UnitLabel     *string
	UnitType      *string
	PriceCents    *int64
	Sqft          *int
	Beds          *int
	Baths         *float64
	AvailableFrom *time.Time
}

// Update applies a partial update; returns nil if not owned by user.
func (u *Units) Update(ctx context.Context, in UpdateUnitInput) (*Unit, error) {
	const q = `
		UPDATE units SET
		  unit_label     = CASE WHEN $3::TEXT IS NULL THEN unit_label ELSE NULLIF($3, '') END,
		  unit_type      = COALESCE($4, unit_type),
		  price_cents    = COALESCE($5, price_cents),
		  sqft           = COALESCE($6, sqft),
		  beds           = COALESCE($7, beds),
		  baths          = COALESCE($8, baths),
		  available_from = COALESCE($9, available_from),
		  updated_at     = now()
		WHERE id = $1
		  AND property_id IN (SELECT id FROM properties WHERE user_id = $2)
		RETURNING id, property_id, unit_label, unit_type, price_cents, sqft, beds, baths, available_from, created_at, updated_at`
	row := u.pool.QueryRow(ctx, q,
		in.ID, in.UserID,
		in.UnitLabel, in.UnitType, in.PriceCents,
		in.Sqft, in.Beds, in.Baths, in.AvailableFrom)
	var unit Unit
	if err := row.Scan(&unit.ID, &unit.PropertyID, &unit.UnitLabel, &unit.UnitType,
		&unit.PriceCents, &unit.Sqft, &unit.Beds, &unit.Baths, &unit.AvailableFrom,
		&unit.CreatedAt, &unit.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("update unit: %w", err)
	}
	return &unit, nil
}

// Delete removes a unit. Returns true if a row matched (and thus belonged to the user).
func (u *Units) Delete(ctx context.Context, id, userID uuid.UUID) (bool, error) {
	const q = `
		DELETE FROM units
		WHERE id = $1
		  AND property_id IN (SELECT id FROM properties WHERE user_id = $2)`
	tag, err := u.pool.Exec(ctx, q, id, userID)
	if err != nil {
		return false, fmt.Errorf("delete unit: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ListByProperty returns all units of a property, oldest first (created_at asc).
func (u *Units) ListByProperty(ctx context.Context, propertyID uuid.UUID) ([]*Unit, error) {
	const q = `
		SELECT id, property_id, unit_label, unit_type, price_cents, sqft, beds, baths, available_from, created_at, updated_at
		FROM units WHERE property_id = $1 ORDER BY created_at ASC`
	rows, err := u.pool.Query(ctx, q, propertyID)
	if err != nil {
		return nil, fmt.Errorf("list units: %w", err)
	}
	defer rows.Close()
	var out []*Unit
	for rows.Next() {
		var unit Unit
		if err := rows.Scan(&unit.ID, &unit.PropertyID, &unit.UnitLabel, &unit.UnitType,
			&unit.PriceCents, &unit.Sqft, &unit.Beds, &unit.Baths, &unit.AvailableFrom,
			&unit.CreatedAt, &unit.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &unit)
	}
	return out, rows.Err()
}

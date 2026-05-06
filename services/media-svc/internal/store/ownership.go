// Package store wraps Postgres queries for media-svc.
//
// media-svc reads `units` + `properties` (owned by property-svc) directly to
// verify ownership. This couples the two services at the schema level — by
// design per Tech Design Doc §3.4 (shared data plane). If we later split DBs,
// this file is the only thing that needs to swap to an HTTP client.
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UnitMeta is the minimum we need about a unit to make media decisions.
type UnitMeta struct {
	UnitID       uuid.UUID
	UserID       uuid.UUID
	PropertyID   uuid.UUID
	PropertyKind string // "rental" | "for_sale" — drives retention TTL
}

// Ownership reads units + properties.
type Ownership struct {
	pool *pgxpool.Pool
}

// NewOwnership wraps a pgxpool.
func NewOwnership(pool *pgxpool.Pool) *Ownership {
	return &Ownership{pool: pool}
}

// FindUnitForUser returns the unit + parent property metadata if and only if
// the unit belongs to a property owned by the user. Returns nil if not found.
func (o *Ownership) FindUnitForUser(ctx context.Context, unitID, userID uuid.UUID) (*UnitMeta, error) {
	const q = `
		SELECT u.id, p.user_id, p.id, p.kind
		FROM units u
		JOIN properties p ON p.id = u.property_id
		WHERE u.id = $1 AND p.user_id = $2`
	row := o.pool.QueryRow(ctx, q, unitID, userID)
	var m UnitMeta
	if err := row.Scan(&m.UnitID, &m.UserID, &m.PropertyID, &m.PropertyKind); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("ownership lookup: %w", err)
	}
	return &m, nil
}

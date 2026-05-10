package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// UnitForRanking is the flat shape ranking-svc needs: each row is one
// candidate unit with everything required to score it. We read across the
// property-svc tables directly (same DB) — same shared-data-plane pattern
// media-svc uses to verify ownership.
type UnitForRanking struct {
	UnitID         uuid.UUID
	PropertyID     uuid.UUID
	Address        string
	Kind           string
	UnitLabel      *string
	UnitType       string
	PriceCents     *int64
	Sqft           *int
	Beds           *int
	Baths          *float64
	AvailableFrom  *time.Time
	Status         string
	PropertyLat    *float64
	PropertyLng    *float64
	PropertyURL    *string
}

// ListUnitsForUser returns every unit owned by the user, joined with its
// parent property. archived units are excluded — they're explicit user
// signal that the unit is out of consideration.
func (s *Store) ListUnitsForUser(ctx context.Context, userID uuid.UUID) ([]*UnitForRanking, error) {
	const q = `
		SELECT u.id, u.property_id, p.address, p.kind,
		       u.unit_label, u.unit_type, u.price_cents, u.sqft, u.beds, u.baths,
		       u.available_from, u.status,
		       ST_Y(p.location::geometry), ST_X(p.location::geometry),
		       p.source_url
		FROM units u
		JOIN properties p ON p.id = u.property_id
		WHERE p.user_id = $1
		  AND u.status <> 'archived'
		ORDER BY u.created_at DESC`
	rows, err := s.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("list units: %w", err)
	}
	defer rows.Close()
	var out []*UnitForRanking
	for rows.Next() {
		var u UnitForRanking
		if err := rows.Scan(
			&u.UnitID, &u.PropertyID, &u.Address, &u.Kind,
			&u.UnitLabel, &u.UnitType, &u.PriceCents, &u.Sqft, &u.Beds, &u.Baths,
			&u.AvailableFrom, &u.Status,
			&u.PropertyLat, &u.PropertyLng,
			&u.PropertyURL,
		); err != nil {
			return nil, err
		}
		out = append(out, &u)
	}
	return out, rows.Err()
}

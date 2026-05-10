// Package store wraps Postgres queries for ranking-svc.
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

// Preferences mirrors the user_preferences row. Most fields are nullable —
// a brand-new user has no preferences set, and ranking should still work
// with sensible defaults rather than gating until everything is filled in.
type Preferences struct {
	UserID         uuid.UUID
	WorkAddress    *string
	WorkLat        *float64
	WorkLng        *float64
	BudgetMinCents *int64
	BudgetMaxCents *int64
	MinBeds        *int
	MinBaths       *float64
	MinSqft        *int
	WeightPrice    *int
	WeightSize     *int
	WeightCommute  *int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Preferences is the data-access layer for user_preferences.
type Store struct {
	pool *pgxpool.Pool
}

// New wraps a pgxpool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Get returns the user's preferences row, or nil if they haven't saved any
// yet. The ranking service treats nil as "all defaults".
func (s *Store) Get(ctx context.Context, userID uuid.UUID) (*Preferences, error) {
	const q = `
		SELECT user_id, work_address, work_lat, work_lng,
		       budget_min_cents, budget_max_cents,
		       min_beds, min_baths, min_sqft,
		       weight_price, weight_size, weight_commute,
		       created_at, updated_at
		FROM user_preferences WHERE user_id = $1`
	row := s.pool.QueryRow(ctx, q, userID)
	var p Preferences
	err := row.Scan(
		&p.UserID, &p.WorkAddress, &p.WorkLat, &p.WorkLng,
		&p.BudgetMinCents, &p.BudgetMaxCents,
		&p.MinBeds, &p.MinBaths, &p.MinSqft,
		&p.WeightPrice, &p.WeightSize, &p.WeightCommute,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get prefs: %w", err)
	}
	return &p, nil
}

// UpsertInput is the partial-upsert payload. nil pointer = leave field as-is
// (or use NULL for the first insert).
type UpsertInput struct {
	UserID         uuid.UUID
	WorkAddress    *string
	WorkLat        *float64
	WorkLng        *float64
	BudgetMinCents *int64
	BudgetMaxCents *int64
	MinBeds        *int
	MinBaths       *float64
	MinSqft        *int
	WeightPrice    *int
	WeightSize     *int
	WeightCommute  *int
}

// Upsert inserts or partially updates the user's preferences. nil fields are
// preserved from the existing row on update; on first insert nil fields land
// as NULL.
func (s *Store) Upsert(ctx context.Context, in UpsertInput) (*Preferences, error) {
	const q = `
		INSERT INTO user_preferences (
		  user_id, work_address, work_lat, work_lng,
		  budget_min_cents, budget_max_cents,
		  min_beds, min_baths, min_sqft,
		  weight_price, weight_size, weight_commute
		) VALUES (
		  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
		)
		ON CONFLICT (user_id) DO UPDATE SET
		  work_address     = COALESCE(EXCLUDED.work_address,     user_preferences.work_address),
		  work_lat         = COALESCE(EXCLUDED.work_lat,         user_preferences.work_lat),
		  work_lng         = COALESCE(EXCLUDED.work_lng,         user_preferences.work_lng),
		  budget_min_cents = COALESCE(EXCLUDED.budget_min_cents, user_preferences.budget_min_cents),
		  budget_max_cents = COALESCE(EXCLUDED.budget_max_cents, user_preferences.budget_max_cents),
		  min_beds         = COALESCE(EXCLUDED.min_beds,         user_preferences.min_beds),
		  min_baths        = COALESCE(EXCLUDED.min_baths,        user_preferences.min_baths),
		  min_sqft         = COALESCE(EXCLUDED.min_sqft,         user_preferences.min_sqft),
		  weight_price     = COALESCE(EXCLUDED.weight_price,     user_preferences.weight_price),
		  weight_size      = COALESCE(EXCLUDED.weight_size,      user_preferences.weight_size),
		  weight_commute   = COALESCE(EXCLUDED.weight_commute,   user_preferences.weight_commute),
		  updated_at       = now()
		RETURNING user_id, work_address, work_lat, work_lng,
		          budget_min_cents, budget_max_cents,
		          min_beds, min_baths, min_sqft,
		          weight_price, weight_size, weight_commute,
		          created_at, updated_at`
	row := s.pool.QueryRow(ctx, q,
		in.UserID, in.WorkAddress, in.WorkLat, in.WorkLng,
		in.BudgetMinCents, in.BudgetMaxCents,
		in.MinBeds, in.MinBaths, in.MinSqft,
		in.WeightPrice, in.WeightSize, in.WeightCommute,
	)
	var p Preferences
	if err := row.Scan(
		&p.UserID, &p.WorkAddress, &p.WorkLat, &p.WorkLng,
		&p.BudgetMinCents, &p.BudgetMaxCents,
		&p.MinBeds, &p.MinBaths, &p.MinSqft,
		&p.WeightPrice, &p.WeightSize, &p.WeightCommute,
		&p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("upsert prefs: %w", err)
	}
	return &p, nil
}

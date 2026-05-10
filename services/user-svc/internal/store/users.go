// Package store wraps Postgres queries for user-svc.
//
// Direct pgx for now; sqlc is in the Tech Design Doc and will land when the
// query count grows past ~10. Today there are 5.
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

// User mirrors the row returned by SELECT * FROM users.
type User struct {
	ID          uuid.UUID
	ExternalID  string
	Provider    string
	EmailHash   *string
	AvatarS3Key *string
	DisplayName *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Users is the data-access layer for the users table.
type Users struct {
	pool *pgxpool.Pool
}

// NewUsers wraps a pgxpool.
func NewUsers(pool *pgxpool.Pool) *Users {
	return &Users{pool: pool}
}

// FindByExternalID returns a user by (provider, external_id), or nil if not found.
func (u *Users) FindByExternalID(ctx context.Context, provider, externalID string) (*User, error) {
	const q = `
		SELECT id, external_id, provider, email_hash, avatar_s3_key, display_name, created_at, updated_at
		FROM users WHERE provider = $1 AND external_id = $2`
	row := u.pool.QueryRow(ctx, q, provider, externalID)
	var user User
	err := row.Scan(&user.ID, &user.ExternalID, &user.Provider, &user.EmailHash, &user.AvatarS3Key, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}
	return &user, nil
}

// FindByID returns a user by primary key, or nil if not found.
func (u *Users) FindByID(ctx context.Context, id uuid.UUID) (*User, error) {
	const q = `
		SELECT id, external_id, provider, email_hash, avatar_s3_key, display_name, created_at, updated_at
		FROM users WHERE id = $1`
	row := u.pool.QueryRow(ctx, q, id)
	var user User
	err := row.Scan(&user.ID, &user.ExternalID, &user.Provider, &user.EmailHash, &user.AvatarS3Key, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find user by id: %w", err)
	}
	return &user, nil
}

// Upsert finds-or-inserts a user keyed by (provider, external_id) and refreshes
// its email_hash if a new one is provided. Returns the canonical row.
func (u *Users) Upsert(ctx context.Context, provider, externalID string, emailHash string) (*User, error) {
	const q = `
		INSERT INTO users (provider, external_id, email_hash)
		VALUES ($1, $2, NULLIF($3, ''))
		ON CONFLICT (provider, external_id) DO UPDATE
		  SET email_hash = COALESCE(NULLIF(EXCLUDED.email_hash, ''), users.email_hash),
		      updated_at = now()
		RETURNING id, external_id, provider, email_hash, avatar_s3_key, created_at, updated_at`
	row := u.pool.QueryRow(ctx, q, provider, externalID, emailHash)
	var user User
	if err := row.Scan(&user.ID, &user.ExternalID, &user.Provider, &user.EmailHash, &user.AvatarS3Key, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt); err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}
	return &user, nil
}

// SetDisplayName updates the user's display name. Empty string clears it
// (column → NULL). Returns the canonical row.
func (u *Users) SetDisplayName(ctx context.Context, id uuid.UUID, name string) (*User, error) {
	const q = `
		UPDATE users
		SET display_name = NULLIF($2, ''), updated_at = now()
		WHERE id = $1
		RETURNING id, external_id, provider, email_hash, avatar_s3_key, display_name, created_at, updated_at`
	row := u.pool.QueryRow(ctx, q, id, name)
	var user User
	err := row.Scan(&user.ID, &user.ExternalID, &user.Provider, &user.EmailHash, &user.AvatarS3Key, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("set display name: %w", err)
	}
	return &user, nil
}

// SetAvatarKey replaces (or clears, if key is empty) the user's avatar_s3_key
// and returns the previously-stored key. Callers use the returned old key to
// delete the now-orphan S3 object.
//
// The CTE reads the existing row first so the returned key is the *prior*
// value, not the just-written one (otherwise it'd just be `key` again).
func (u *Users) SetAvatarKey(ctx context.Context, id uuid.UUID, key string) (*string, error) {
	const q = `
		WITH prior AS (
		  SELECT avatar_s3_key FROM users WHERE id = $1
		), upd AS (
		  UPDATE users
		  SET avatar_s3_key = NULLIF($2, ''), updated_at = now()
		  WHERE id = $1
		  RETURNING 1
		)
		SELECT prior.avatar_s3_key FROM prior, upd`
	var oldKey *string
	if err := u.pool.QueryRow(ctx, q, id, key).Scan(&oldKey); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("set avatar: %w", err)
	}
	return oldKey, nil
}

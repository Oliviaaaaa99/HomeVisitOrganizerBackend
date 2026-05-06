package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RefreshTokens is the data-access layer for refresh_tokens.
type RefreshTokens struct {
	pool *pgxpool.Pool
}

// NewRefreshTokens wraps a pgxpool.
func NewRefreshTokens(pool *pgxpool.Pool) *RefreshTokens {
	return &RefreshTokens{pool: pool}
}

// HashToken returns the canonical token-hash representation we store. The raw
// token is given to the client; only the hash lives in our DB so a stolen DB
// dump can't be used to mint requests.
func HashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// Insert records a new refresh token for a user.
func (r *RefreshTokens) Insert(ctx context.Context, userID uuid.UUID, rawToken string, expiresAt time.Time, userAgent, ipAddr string) (uuid.UUID, error) {
	const q = `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at, user_agent, ip_addr)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, '')::INET)
		RETURNING id`
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, q, userID, HashToken(rawToken), expiresAt, userAgent, ipAddr).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert refresh token: %w", err)
	}
	return id, nil
}

// Active represents the result of looking up an unrevoked, unexpired token.
type Active struct {
	ID     uuid.UUID
	UserID uuid.UUID
}

// FindActive returns the active refresh-token record for the given raw token,
// or nil if not found / revoked / expired.
func (r *RefreshTokens) FindActive(ctx context.Context, rawToken string) (*Active, error) {
	const q = `
		SELECT id, user_id
		FROM refresh_tokens
		WHERE token_hash = $1
		  AND revoked_at IS NULL
		  AND expires_at > now()`
	row := r.pool.QueryRow(ctx, q, HashToken(rawToken))
	var a Active
	err := row.Scan(&a.ID, &a.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find active refresh: %w", err)
	}
	return &a, nil
}

// Revoke marks a single refresh token as revoked. Idempotent.
func (r *RefreshTokens) Revoke(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE refresh_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`
	_, err := r.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("revoke: %w", err)
	}
	return nil
}

// RevokeAllForUser invalidates every active refresh token for a user. Used by
// "sign out everywhere" and "delete my data" flows.
func (r *RefreshTokens) RevokeAllForUser(ctx context.Context, userID uuid.UUID) error {
	const q = `UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`
	_, err := r.pool.Exec(ctx, q, userID)
	if err != nil {
		return fmt.Errorf("revoke all: %w", err)
	}
	return nil
}

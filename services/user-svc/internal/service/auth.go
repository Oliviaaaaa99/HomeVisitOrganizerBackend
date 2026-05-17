// Package service holds the business-logic layer for user-svc.
package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/user-svc/internal/clients"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/services/user-svc/internal/store"
	"github.com/Oliviaaaaa99/HomeVisitOrganizerBackend/shared/go-common/authx"
)

// TokenPair is what the API returns to a successful auth flow.
type TokenPair struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	ExpiresAt        time.Time `json:"expires_at"`         // access-token expiry
	RefreshExpiresAt time.Time `json:"refresh_expires_at"` // refresh-token expiry
	UserID           string    `json:"user_id"`
}

// ErrInvalidCredentials signals either an unknown external_id or a wrong
// passcode for an otherwise-valid external_id. Same error for both cases so
// strangers can't tell whether they guessed the email right.
var ErrInvalidCredentials = errors.New("invalid email or passcode")

// Auth is the orchestrator for the auth flow: id_token verification, user
// upsert, JWT signing, refresh-token rotation.
type Auth struct {
	idps       *clients.Registry
	users      *store.Users
	refresh    *store.RefreshTokens
	jwtIssuer  *authx.Issuer
	refreshTTL time.Duration
	// Per-user passcode map for the demo gate. Nil/empty = no gate (original
	// behavior, keeps local docker-compose runs frictionless). Keys are
	// lower-cased external_ids; values are the matching passcodes (plain text
	// — Fly encrypts secrets at rest, threat model docs see PR description).
	userPasscodes map[string]string
}

// NewAuth wires the auth service. Pass a nil/empty userPasscodes map to skip
// the demo gate (original behavior for local dev).
func NewAuth(idps *clients.Registry, users *store.Users, refresh *store.RefreshTokens, jwtIssuer *authx.Issuer, refreshTTL time.Duration, userPasscodes map[string]string) *Auth {
	return &Auth{
		idps:          idps,
		users:         users,
		refresh:       refresh,
		jwtIssuer:     jwtIssuer,
		refreshTTL:    refreshTTL,
		userPasscodes: userPasscodes,
	}
}

// Exchange takes a provider id_token, verifies it, finds-or-creates the local
// user, and returns a fresh access + refresh token pair.
//
// When userPasscodes is configured, the verified external_id must be in the
// map AND the request passcode must match that user's entry. Same error in
// both failure modes so attackers can't distinguish "wrong email" from
// "wrong code".
func (a *Auth) Exchange(ctx context.Context, provider, idToken, passcode, userAgent, ipAddr string) (*TokenPair, error) {
	identity, err := a.idps.Verify(ctx, provider, idToken)
	if err != nil {
		return nil, fmt.Errorf("verify id token: %w", err)
	}
	if len(a.userPasscodes) > 0 {
		expected, ok := a.userPasscodes[strings.ToLower(identity.ExternalID)]
		if !ok || subtle.ConstantTimeCompare([]byte(passcode), []byte(expected)) != 1 {
			return nil, ErrInvalidCredentials
		}
	}
	user, err := a.users.Upsert(ctx, identity.Provider, identity.ExternalID, identity.EmailHash)
	if err != nil {
		return nil, err
	}
	return a.issuePair(ctx, user.ID.String(), userAgent, ipAddr)
}

// ErrInvalidRefreshToken signals an invalid / expired / revoked refresh token.
var ErrInvalidRefreshToken = errors.New("invalid refresh token")

// Refresh rotates a refresh token: validates the old one, revokes it, issues a
// new pair (access + refresh).
func (a *Auth) Refresh(ctx context.Context, rawRefresh, userAgent, ipAddr string) (*TokenPair, error) {
	active, err := a.refresh.FindActive(ctx, rawRefresh)
	if err != nil {
		return nil, err
	}
	if active == nil {
		return nil, ErrInvalidRefreshToken
	}
	// Revoke the old token first to make rotation atomic from the client's POV.
	if err := a.refresh.Revoke(ctx, active.ID); err != nil {
		return nil, err
	}
	return a.issuePair(ctx, active.UserID.String(), userAgent, ipAddr)
}

func (a *Auth) issuePair(ctx context.Context, userID, userAgent, ipAddr string) (*TokenPair, error) {
	access, accessExp, err := a.jwtIssuer.Issue(userID)
	if err != nil {
		return nil, err
	}
	rawRefresh, refreshExp, err := newRefreshToken(a.refreshTTL)
	if err != nil {
		return nil, err
	}
	uid, err := parseUUID(userID)
	if err != nil {
		return nil, err
	}
	if _, err := a.refresh.Insert(ctx, uid, rawRefresh, refreshExp, userAgent, ipAddr); err != nil {
		return nil, err
	}
	return &TokenPair{
		AccessToken:      access,
		RefreshToken:     rawRefresh,
		ExpiresAt:        accessExp,
		RefreshExpiresAt: refreshExp,
		UserID:           userID,
	}, nil
}

// 32 bytes of CSPRNG, base64-url-no-pad encoded — opaque to the client.
func newRefreshToken(ttl time.Duration) (string, time.Time, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	return base64.RawURLEncoding.EncodeToString(b), time.Now().UTC().Add(ttl), nil
}

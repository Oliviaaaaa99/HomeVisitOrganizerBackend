// Package service holds the business-logic layer for user-svc.
package service

import (
	"context"
	"crypto/rand"
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

// ErrNotAllowed signals the verified identity isn't in the configured
// allowlist. Used to gate the demo deployment to a known set of testers.
var ErrNotAllowed = errors.New("identity not in allowlist")

// Auth is the orchestrator for the auth flow: id_token verification, user
// upsert, JWT signing, refresh-token rotation.
type Auth struct {
	idps       *clients.Registry
	users      *store.Users
	refresh    *store.RefreshTokens
	jwtIssuer  *authx.Issuer
	refreshTTL time.Duration
	// Nil = allow everyone (the original behavior). Non-nil and non-empty =
	// only the listed external_ids may exchange. Lookup keys are lower-cased
	// to keep "Email@Example.com" and "email@example.com" matchable.
	allowlist map[string]struct{}
}

// NewAuth wires the auth service. Pass a nil or empty allowlist to allow any
// verified identity (original demo behavior).
func NewAuth(idps *clients.Registry, users *store.Users, refresh *store.RefreshTokens, jwtIssuer *authx.Issuer, refreshTTL time.Duration, allowlist map[string]struct{}) *Auth {
	return &Auth{
		idps:       idps,
		users:      users,
		refresh:    refresh,
		jwtIssuer:  jwtIssuer,
		refreshTTL: refreshTTL,
		allowlist:  allowlist,
	}
}

// Exchange takes a provider id_token, verifies it, finds-or-creates the local
// user, and returns a fresh access + refresh token pair. Returns ErrNotAllowed
// if an allowlist is configured and the verified external_id isn't in it.
func (a *Auth) Exchange(ctx context.Context, provider, idToken, userAgent, ipAddr string) (*TokenPair, error) {
	identity, err := a.idps.Verify(ctx, provider, idToken)
	if err != nil {
		return nil, fmt.Errorf("verify id token: %w", err)
	}
	if len(a.allowlist) > 0 {
		if _, ok := a.allowlist[strings.ToLower(identity.ExternalID)]; !ok {
			return nil, ErrNotAllowed
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

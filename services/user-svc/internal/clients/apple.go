package clients

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

const (
	appleJWKSURL = "https://appleid.apple.com/auth/keys"
	appleIssuer  = "https://appleid.apple.com"
)

// AppleVerifier validates Sign in with Apple identity tokens.
type AppleVerifier struct {
	keyset   keyfunc.Keyfunc
	audience string // our App's client ID (Apple "Service ID" or bundle ID)
}

// NewAppleVerifier loads Apple's JWKS and returns a verifier scoped to one audience.
func NewAppleVerifier(ctx context.Context, audience string) (*AppleVerifier, error) {
	if audience == "" {
		return nil, fmt.Errorf("apple verifier: audience required")
	}
	ks, err := keyfunc.NewDefaultCtx(ctx, []string{appleJWKSURL})
	if err != nil {
		return nil, fmt.Errorf("fetch apple jwks: %w", err)
	}
	return &AppleVerifier{keyset: ks, audience: audience}, nil
}

// Verify parses an Apple identity_token, checks signature + standard claims,
// and returns the canonical Identity.
func (a *AppleVerifier) Verify(_ context.Context, idToken string) (*Identity, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(appleIssuer),
		jwt.WithAudience(a.audience),
		jwt.WithExpirationRequired(),
	)
	claims := jwt.MapClaims{}
	tok, err := parser.ParseWithClaims(idToken, claims, a.keyset.Keyfunc)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if !tok.Valid {
		return nil, ErrInvalidToken
	}

	sub, _ := claims["sub"].(string)
	if sub == "" {
		return nil, fmt.Errorf("%w: missing sub", ErrInvalidToken)
	}

	// Apple sometimes omits email; if present, hash it.
	emailHash := ""
	if email, ok := claims["email"].(string); ok && email != "" {
		h := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
		emailHash = hex.EncodeToString(h[:])
	}

	// Sanity check on iat — guard against catastrophically wrong clock.
	if iat, ok := claims["iat"].(float64); ok {
		if t := time.Unix(int64(iat), 0); time.Since(t) > 24*time.Hour {
			return nil, fmt.Errorf("%w: token too old", ErrInvalidToken)
		}
	}

	return &Identity{
		Provider:   "apple",
		ExternalID: sub,
		EmailHash:  emailHash,
	}, nil
}

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

const googleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"

// Google's iss claim varies; both forms below are accepted.
var googleIssuers = map[string]struct{}{
	"https://accounts.google.com": {},
	"accounts.google.com":         {},
}

// GoogleVerifier validates Google Sign-In identity tokens.
type GoogleVerifier struct {
	keyset   keyfunc.Keyfunc
	audience string // our OAuth Client ID
}

// NewGoogleVerifier loads Google's JWKS and scopes the verifier to an audience.
func NewGoogleVerifier(ctx context.Context, audience string) (*GoogleVerifier, error) {
	if audience == "" {
		return nil, fmt.Errorf("google verifier: audience required")
	}
	ks, err := keyfunc.NewDefaultCtx(ctx, []string{googleJWKSURL})
	if err != nil {
		return nil, fmt.Errorf("fetch google jwks: %w", err)
	}
	return &GoogleVerifier{keyset: ks, audience: audience}, nil
}

// Verify parses a Google id_token. Audience and issuer are checked manually
// because Google accepts two forms of `iss`.
func (g *GoogleVerifier) Verify(_ context.Context, idToken string) (*Identity, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithAudience(g.audience),
		jwt.WithExpirationRequired(),
	)
	claims := jwt.MapClaims{}
	tok, err := parser.ParseWithClaims(idToken, claims, g.keyset.Keyfunc)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if !tok.Valid {
		return nil, ErrInvalidToken
	}

	iss, _ := claims["iss"].(string)
	if _, ok := googleIssuers[iss]; !ok {
		return nil, fmt.Errorf("%w: unexpected issuer %q", ErrInvalidToken, iss)
	}

	sub, _ := claims["sub"].(string)
	if sub == "" {
		return nil, fmt.Errorf("%w: missing sub", ErrInvalidToken)
	}

	emailHash := ""
	if email, ok := claims["email"].(string); ok && email != "" {
		h := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
		emailHash = hex.EncodeToString(h[:])
	}

	if iat, ok := claims["iat"].(float64); ok {
		if t := time.Unix(int64(iat), 0); time.Since(t) > 24*time.Hour {
			return nil, fmt.Errorf("%w: token too old", ErrInvalidToken)
		}
	}

	return &Identity{
		Provider:   "google",
		ExternalID: sub,
		EmailHash:  emailHash,
	}, nil
}

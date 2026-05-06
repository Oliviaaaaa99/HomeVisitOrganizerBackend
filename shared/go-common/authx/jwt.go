// Package authx issues and verifies our service's own JWT access tokens.
//
// v0 uses HS256 with a shared secret loaded from env. M3+ will switch to RS256
// with the signing key in AWS KMS — verification stays the same shape so
// callers don't have to change.
package authx

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	defaultIssuer   = "user-svc"
	defaultAudience = "hvo-mobile"
)

// Claims are the JWT claims our access tokens carry.
type Claims struct {
	jwt.RegisteredClaims
}

// Issuer signs new access tokens.
type Issuer struct {
	secret   []byte
	issuer   string
	audience string
	tokenTTL time.Duration
}

// NewIssuer creates an Issuer with the given HMAC secret and access-token TTL.
func NewIssuer(secret []byte, tokenTTL time.Duration) *Issuer {
	return &Issuer{
		secret:   secret,
		issuer:   defaultIssuer,
		audience: defaultAudience,
		tokenTTL: tokenTTL,
	}
}

// Issue returns a signed JWT for the given user ID.
func (i *Issuer) Issue(userID string) (string, time.Time, error) {
	now := time.Now().UTC()
	exp := now.Add(i.tokenTTL)
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    i.issuer,
			Audience:  jwt.ClaimStrings{i.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(i.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign jwt: %w", err)
	}
	return signed, exp, nil
}

// Verifier checks JWTs issued by Issuer with the same secret.
type Verifier struct {
	secret   []byte
	issuer   string
	audience string
}

// NewVerifier creates a Verifier with the given HMAC secret.
func NewVerifier(secret []byte) *Verifier {
	return &Verifier{secret: secret, issuer: defaultIssuer, audience: defaultAudience}
}

// Verify parses and validates a JWT, returning the claims on success.
func (v *Verifier) Verify(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
	)
	tok, err := parser.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		return v.secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !tok.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

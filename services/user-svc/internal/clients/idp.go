// Package clients implements identity-provider verification: turn an Apple
// or Google id_token into a {provider, external_id, email_hash} triple that
// the auth service can use to upsert a local user.
package clients

import (
	"context"
	"errors"
	"fmt"
)

// Identity is the verified result of an id_token from an IdP.
type Identity struct {
	Provider   string // "apple" | "google" | "dev"
	ExternalID string // provider's stable user identifier (sub claim)
	EmailHash  string // sha256(lowercased email), or "" if not available
}

// Verifier verifies an id_token from a single provider.
type Verifier interface {
	Verify(ctx context.Context, idToken string) (*Identity, error)
}

// Registry is a fan-out verifier indexed by provider name.
type Registry struct {
	verifiers map[string]Verifier
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{verifiers: make(map[string]Verifier)}
}

// Register adds a verifier under a provider name.
func (r *Registry) Register(provider string, v Verifier) {
	r.verifiers[provider] = v
}

// Verify routes to the right provider verifier.
func (r *Registry) Verify(ctx context.Context, provider, idToken string) (*Identity, error) {
	v, ok := r.verifiers[provider]
	if !ok {
		return nil, fmt.Errorf("unsupported provider %q", provider)
	}
	return v.Verify(ctx, idToken)
}

// ErrInvalidToken is returned when an id_token fails verification.
var ErrInvalidToken = errors.New("invalid id_token")

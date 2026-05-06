package clients

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// DevVerifier accepts ANY id_token and treats it as the external user id.
// Only meant for local dev / CI — gated by a startup check on env var.
type DevVerifier struct{}

// NewDevVerifier returns a verifier that does no real verification.
func NewDevVerifier() *DevVerifier { return &DevVerifier{} }

// Verify treats the input as "<external_id>" or "<external_id>:<email>".
// Useful when shadowing real Apple/Google IdPs in local dev.
func (d *DevVerifier) Verify(_ context.Context, idToken string) (*Identity, error) {
	if idToken == "" {
		return nil, ErrInvalidToken
	}
	parts := strings.SplitN(idToken, ":", 2)
	id := parts[0]
	emailHash := ""
	if len(parts) == 2 && parts[1] != "" {
		h := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(parts[1]))))
		emailHash = hex.EncodeToString(h[:])
	}
	return &Identity{
		Provider:   "dev",
		ExternalID: id,
		EmailHash:  emailHash,
	}, nil
}

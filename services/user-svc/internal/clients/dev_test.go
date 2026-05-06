package clients

import (
	"context"
	"errors"
	"testing"
)

func TestDevVerifier_IDOnly(t *testing.T) {
	d := NewDevVerifier()
	id, err := d.Verify(context.Background(), "user-abc")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if id.Provider != "dev" {
		t.Errorf("provider: got %q", id.Provider)
	}
	if id.ExternalID != "user-abc" {
		t.Errorf("external_id: got %q", id.ExternalID)
	}
	if id.EmailHash != "" {
		t.Errorf("email_hash: expected empty, got %q", id.EmailHash)
	}
}

func TestDevVerifier_WithEmail(t *testing.T) {
	d := NewDevVerifier()
	id, err := d.Verify(context.Background(), "user-abc:Olivia@Example.COM")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if id.ExternalID != "user-abc" {
		t.Errorf("external_id: got %q", id.ExternalID)
	}
	if id.EmailHash == "" {
		t.Fatal("expected non-empty email_hash")
	}
	// Lowercasing is part of the contract — same email different case → same hash.
	id2, _ := d.Verify(context.Background(), "user-abc:olivia@example.com")
	if id.EmailHash != id2.EmailHash {
		t.Errorf("email hashing should be case-insensitive: %q vs %q", id.EmailHash, id2.EmailHash)
	}
}

func TestDevVerifier_RejectsEmpty(t *testing.T) {
	d := NewDevVerifier()
	_, err := d.Verify(context.Background(), "")
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

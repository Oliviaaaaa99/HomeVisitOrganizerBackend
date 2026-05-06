package authx

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestIssueVerifyRoundTrip(t *testing.T) {
	secret := []byte("test-secret-32-bytes-min-12345")
	iss := NewIssuer(secret, time.Hour)
	v := NewVerifier(secret)

	tok, exp, err := iss.Issue("user-123")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if tok == "" {
		t.Fatal("empty token")
	}
	if exp.Before(time.Now()) {
		t.Fatal("exp in the past")
	}

	claims, err := v.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "user-123" {
		t.Errorf("subject: got %q want %q", claims.Subject, "user-123")
	}
	if !contains(claims.Audience, "hvo-mobile") {
		t.Errorf("audience: got %v", claims.Audience)
	}
	if claims.Issuer != "user-svc" {
		t.Errorf("issuer: got %q", claims.Issuer)
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	secret := []byte("test-secret-32-bytes-min-12345")
	iss := NewIssuer(secret, -time.Minute) // already expired
	v := NewVerifier(secret)

	tok, _, err := iss.Issue("user-123")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := v.Verify(tok); err == nil {
		t.Fatal("expected verify to fail on expired token")
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	secret := []byte("test-secret-32-bytes-min-12345")
	other := []byte("DIFFERENT-secret-32-bytes-12345")
	iss := NewIssuer(secret, time.Hour)
	v := NewVerifier(other)

	tok, _, err := iss.Issue("user-123")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	_, err = v.Verify(tok)
	if err == nil {
		t.Fatal("expected verify to fail with wrong secret")
	}
}

func TestVerifyRejectsAlgNone(t *testing.T) {
	// Construct an unsigned ("none") token and verify our verifier refuses it.
	secret := []byte("test-secret-32-bytes-min-12345")
	v := NewVerifier(secret)

	claims := jwt.RegisteredClaims{Subject: "x", Issuer: "user-svc"}
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	signed, signErr := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if signErr != nil {
		t.Fatalf("sign none: %v", signErr)
	}
	if _, err := v.Verify(signed); err == nil {
		t.Fatal("expected verify to refuse alg=none")
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

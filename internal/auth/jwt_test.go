package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestIssue_RoundTrip(t *testing.T) {
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	secret := []byte("test-secret")
	ttl := time.Hour

	a := &Authenticator{
		secret: secret,
		ttl:    ttl,
		now:    func() time.Time { return fixed },
	}

	userID := uuid.New()
	tok, err := a.Issue(userID)
	if err != nil {
		t.Fatalf("unexpectedly failed to issue JWT: %v", err)
	}

	if tok.ExpiresIn != int(ttl.Seconds()) {
		t.Fatalf("JWT ExpiresIn (%v) != defined ttl (%v)", tok.ExpiresIn, int(ttl))
	}

	var claims jwt.RegisteredClaims
	_, err = jwt.ParseWithClaims(
		tok.Value,
		&claims,
		func(t *jwt.Token) (any, error) { return secret, nil },
		jwt.WithTimeFunc(func() time.Time { return fixed }))
	if err != nil {
		t.Fatalf("parsing JWT claims: %v", err)
	}

	if claims.Subject != userID.String() {
		t.Fatalf("claims.Subject (%v) != userID.String (%v)", claims.Subject, userID.String())
	}

	if !claims.ExpiresAt.Equal(fixed.Add(ttl)) {
		t.Fatalf("claims.ExpiresAt (%v) != fixed + ttl (%v)", claims.ExpiresAt, fixed.Add(ttl))
	}
}

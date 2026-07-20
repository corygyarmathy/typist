package auth

import (
	"errors"
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

func TestValidate_RoundTrip(t *testing.T) {
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

	id, err := a.Validate(tok.Value)
	if err != nil {
		t.Errorf("failed to validate token: %v", err)
	}
	if id == uuid.Nil {
		t.Errorf("token validation returned empty UUID unexpectedly")
	}
	if id != userID {
		t.Errorf("validated subject %v != issued userID %v", id, userID)
	}
}

func TestValidate_ExpiredToken(t *testing.T) {
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	secret := []byte("test-secret")
	ttl := time.Hour

	a1 := &Authenticator{
		secret: secret,
		ttl:    ttl,
		now:    func() time.Time { return fixed },
	}

	userID := uuid.New()
	tok, err := a1.Issue(userID)
	if err != nil {
		t.Fatalf("unexpectedly failed to issue JWT: %v", err)
	}

	a2 := &Authenticator{
		secret: secret,
		ttl:    ttl,
		now:    func() time.Time { return fixed.Add(ttl + time.Second) },
	}

	id, err := a2.Validate(tok.Value)
	if !errors.Is(err, jwt.ErrTokenExpired) {
		t.Errorf("did not receive expected error. Expected '%v', got: %v", jwt.ErrTokenExpired, err)
	}
	if id != uuid.Nil {
		t.Errorf("token validation unexpectedly returned non-nil UUID")
	}
}

func TestValidate_WrongSecret(t *testing.T) {
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	secret := []byte("test-secret")
	ttl := time.Hour

	a1 := &Authenticator{
		secret: secret,
		ttl:    ttl,
		now:    func() time.Time { return fixed },
	}

	userID := uuid.New()
	tok, err := a1.Issue(userID)
	if err != nil {
		t.Fatalf("unexpectedly failed to issue JWT: %v", err)
	}

	a2 := &Authenticator{
		secret: []byte("different-secret"),
		ttl:    ttl,
		now:    func() time.Time { return fixed },
	}

	id, err := a2.Validate(tok.Value)
	if err == nil {
		t.Errorf("expected to receive error, wrong secret should not be validated")
	}
	if id != uuid.Nil {
		t.Errorf("token validation unexpectedly returned non-nil UUID")
	}

}

func TestValidate_NonUUID(t *testing.T) {
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	secret := []byte("test-secret")
	ttl := time.Hour

	a := &Authenticator{
		secret: secret,
		ttl:    ttl,
		now:    func() time.Time { return fixed },
	}

	claims := jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   "not-a-uuid",
		IssuedAt:  jwt.NewNumericDate(fixed),
		ExpiresAt: jwt.NewNumericDate(fixed.Add(a.ttl)),
		NotBefore: jwt.NewNumericDate(fixed),
	}

	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	ss, err := jwtToken.SignedString(a.secret)
	if err != nil {
		t.Fatalf("signing jwt string: %v", err)
	}

	id, err := a.Validate(ss)
	if err == nil {
		t.Errorf("expected to receive error, invalid UUID in subject should not be validated")
	}
	if id != uuid.Nil {
		t.Errorf("token validation unexpectedly returned non-nil UUID")
	}
}

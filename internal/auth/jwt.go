package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	issuer = "typist"
)

type Authenticator struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time // injected in constructor, enables testing
}

func NewAuthenticator(secret []byte, ttl time.Duration) *Authenticator {
	return &Authenticator{secret: secret, ttl: ttl, now: time.Now}
}

// Issue issues a signed Token (JWT) for the given userID.
func (a *Authenticator) Issue(userID uuid.UUID) (Token, error) {
	now := a.now()

	claims := jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(a.ttl)),
		NotBefore: jwt.NewNumericDate(now),
	}

	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	ss, err := jwtToken.SignedString(a.secret)
	if err != nil {
		return Token{}, fmt.Errorf("signing jwt string: %w", err)
	}

	token := Token{
		Value:     ss,
		ExpiresIn: int(a.ttl.Seconds()),
	}

	return token, nil
}

// Validate parses and verifies tokenString. It pins the signing method to
// HS256, requires an expiry, and returns the user ID carried in the `sub`
// claim. Any failure (bad signature, expired, wrong alg, non-UUID subject)
// returns a zero UUID and an error.
func (a *Authenticator) Validate(tokenString string) (uuid.UUID, error) {
	var claims jwt.RegisteredClaims

	_, err := jwt.ParseWithClaims(
		tokenString,
		&claims,
		func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("invalid token signing method. Expected 'HS256', got: %v", t.Method)
			}
			return a.secret, nil
		},
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(a.now), // reuse the injected clock so tests are deterministic
		jwt.WithIssuer(issuer),
	)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("parsing token: %w", err)
	}

	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("parsing claims subject '%v' to UUID: %w", claims.Subject, err)
	}

	return id, nil
}

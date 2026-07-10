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

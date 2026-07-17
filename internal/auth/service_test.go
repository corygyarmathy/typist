package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type fakeRepo struct {
	registered bool
	err        error
}

func (f *fakeRepo) EmailRegistered(ctx context.Context, email string) (bool, error) {
	return f.registered, f.err
}
func (f *fakeRepo) CreateUser(context.Context) (uuid.UUID, error) { return uuid.UUID{}, nil }
func (f *fakeRepo) CreatePasswordCredential(context.Context, uuid.UUID, string, string) error {
	return nil
}

func TestRegister_GuardClauses(t *testing.T) {
	errBoom := errors.New("db is down")

	tests := map[string]struct {
		email    string
		password string
		fake     *fakeRepo
		wantErr  error
	}{
		"invalid email": {
			email:    "invalid email address",
			password: "correct horse battery staple",
			fake:     &fakeRepo{},
			wantErr:  ErrInvalidEmail,
		},
		"short password, multibyte runes": {
			email:    "valid@email.com",
			password: "🔑🔑🔑🔑🔑🔑🔑", // 7 runes, 28 bytes
			fake:     &fakeRepo{},
			wantErr:  ErrPasswordTooShort,
		},
		"duplicate email": {
			email:    "valid@email.com",
			password: "correct horse battery staple",
			fake:     &fakeRepo{registered: true},
			wantErr:  ErrEmailTaken,
		},
		"repo failure propagates": {
			email:    "valid@example.com",
			password: "long-enough-password",
			fake:     &fakeRepo{err: errBoom},
			wantErr:  errBoom,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			s := &Service{repo: tc.fake}

			_, err := s.Register(context.Background(), tc.email, tc.password)

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("received undesired error. Wanted: '%v', got: '%v'", tc.wantErr, err)
			}
		})
	}
}

package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeRepo struct {
	registered bool               // whether the email is registered in the db
	err        error              // specify error to be 'returned'
	cred       PasswordCredential // returned by FindPasswordCredential
	findErr    error              // e.g. errCredentialNotFound, or a DB error
}

func (f *fakeRepo) EmailRegistered(ctx context.Context, email string) (bool, error) {
	return f.registered, f.err
}
func (f *fakeRepo) CreateUser(context.Context) (uuid.UUID, error) { return uuid.UUID{}, nil }
func (f *fakeRepo) CreatePasswordCredential(context.Context, uuid.UUID, string, string) error {
	return nil
}

func (f *fakeRepo) FindPasswordCredential(ctx context.Context, email string) (PasswordCredential, error) {
	return f.cred, f.findErr
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

func TestLogin(t *testing.T) {
	errBoom := errors.New("db is down")

	const realPassword = "correct horse battery staple"
	knownHash, err := hashPassword(realPassword)
	if err != nil {
		t.Fatalf("setting up known hash: %v", err)
	}
	// The credential as it would come back from the DB for a registered user.
	knownCred := PasswordCredential{UserID: uuid.New(), PasswordHash: knownHash}

	tests := map[string]struct {
		email    string
		password string // the PLAINTEXT the user "types"
		fake     *fakeRepo
		wantErr  error
	}{
		"invalid email": {
			email:    "invalid email address",
			password: realPassword,
			fake:     &fakeRepo{}, // repo never reached; dummy verify runs
			wantErr:  ErrInvalidCredentials,
		},
		"unknown email": {
			email:    "valid@email.com",
			password: realPassword,
			fake:     &fakeRepo{findErr: errCredentialNotFound},
			wantErr:  ErrInvalidCredentials,
		},
		"wrong password": {
			email:    "valid@email.com",
			password: "this is NOT the stored password", // differs from realPassword
			fake:     &fakeRepo{cred: knownCred},        // real hash on file
			wantErr:  ErrInvalidCredentials,
		},
		"repo failure propagates": {
			email:    "valid@example.com",
			password: realPassword,
			fake:     &fakeRepo{findErr: errBoom},
			wantErr:  errBoom,
		},
		"success": {
			email:    "valid@example.com",
			password: realPassword, // matches what's hashed in knownCred
			fake:     &fakeRepo{cred: knownCred},
			wantErr:  nil,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			s := &Service{
				repo:          tc.fake,
				authenticator: NewAuthenticator([]byte("test-secret"), time.Hour),
			}

			token, err := s.Login(context.Background(), tc.email, tc.password)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("wanted err '%v', got '%v'", tc.wantErr, err)
			}
			// Only the success path issues a token; check it there and nowhere else.
			if tc.wantErr == nil && token == (Token{}) {
				t.Fatal("expected a token on success, got empty")
			}
		})
	}
}

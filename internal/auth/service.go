package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Business logic. The service depends on the Repository
// interface, never on the concrete implementation. Transactions are
// orchestrated here, not in handlers or repositories.

type ProgressInitialiser interface {
	CreateInitial(ctx context.Context, userID uuid.UUID) error
}

type Service struct {
	repo          Repository
	pool          *pgxpool.Pool
	newProgress   func(tx pgx.Tx) ProgressInitialiser
	authenticator *Authenticator
}

func NewService(
	pool *pgxpool.Pool,
	newProgress func(tx pgx.Tx) ProgressInitialiser,
	authenticator *Authenticator,
) *Service {
	return &Service{
		repo:          newPgxRepository(pool),
		pool:          pool,
		newProgress:   newProgress,
		authenticator: authenticator,
	}
}

const MinPasswordLen = 8

// Register takes a RFC 5322 compliant email address, and plaintext password,
// confirms email and password validity, hashes password, and registers the
// user to the database in a transaction. A JWT is generated and returned,
// enabling the user to immediately authenticate.
func (s *Service) Register(ctx context.Context, email string, password string) (Token, error) {
	email, err := parseEmail(email)
	if err != nil {
		return Token{}, err
	}
	// counts characters, rather than bytes (as len() does)
	if utf8.RuneCountInString(password) < MinPasswordLen {
		return Token{}, ErrPasswordTooShort
	}

	exists, err := s.repo.EmailRegistered(ctx, email)
	if err != nil {
		return Token{}, err
	}
	if exists {
		return Token{}, ErrEmailTaken
	}

	hash, err := hashPassword(password)
	if err != nil {
		return Token{}, fmt.Errorf("hashing password: %w", err)
	}

	// Begin db tx -> create user -> create credential -> commit to db
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Token{}, fmt.Errorf("beginning tx: %w", err)
	}
	defer func() {
		if cerr := tx.Rollback(ctx); cerr != nil && !errors.Is(cerr, pgx.ErrTxClosed) {
			slog.Error("rolling back database transaction", "cerr", cerr)
		}
	}()

	txRepo := newPgxRepository(tx)

	userID, err := txRepo.CreateUser(ctx)
	if err != nil {
		return Token{}, fmt.Errorf("creating user: %w", err)
	}

	if err := txRepo.CreatePasswordCredential(ctx, userID, email, hash); err != nil {
		return Token{}, fmt.Errorf("creating password credential: %w", err)
	}

	if err := s.newProgress(tx).CreateInitial(ctx, userID); err != nil {
		return Token{}, fmt.Errorf("seeding user progress: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Token{}, fmt.Errorf("committing tx: %w", err)
	}

	token, err := s.authenticator.Issue(userID)
	if err != nil {
		return Token{}, fmt.Errorf("issuing JWT: %w", err)
	}

	return token, nil
}

// Login verifies an email/password pair and, on success, issues a JWT.
// It returns ErrInvalidCredentials for a bad email, an unknown email, and a
// wrong password alike - and runs a constant-time verify on every path so an
// unregistered email is indistinguishable from a registered one by timing.
func (s *Service) Login(ctx context.Context, email string, password string) (Token, error) {
	// Verify against a real hash if found, else the dummy.
	hashToCheck := dummyHash
	var userID uuid.UUID
	found := false

	// parseEmail may fail (malformed); fall through to the dummy verify rather
	// than returning early, to keep timing uniform.
	if parsed, err := parseEmail(email); err == nil {
		cred, err := s.repo.FindPasswordCredential(ctx, parsed)
		switch {
		case err == nil:
			hashToCheck = cred.PasswordHash
			userID = cred.UserID
			found = true
		case errors.Is(err, errCredentialNotFound):
			// nothing to do - leave the dummy in place and verify to maintain timings.
		default:
			return Token{}, fmt.Errorf("getting password credential from db: %w", err)
		}
	}

	verified, err := verifyPassword(password, hashToCheck)
	if err != nil {
		// A malformed PHC can only come from a real stored credential (the dummy
		// is always valid) - so this means corrupt data. Return it (-> 500).
		return Token{}, fmt.Errorf("verifying password: %w", err)
	}

	if !found || !verified {
		return Token{}, ErrInvalidCredentials
	}

	token, err := s.authenticator.Issue(userID)
	if err != nil {
		return Token{}, fmt.Errorf("failed to issue JWT for successful login: %w", err)
	}

	return token, nil
}

// parseEmail parses a single RFC 5322 address, e.g. "Barry Gibbs <bg@example.com>".
// Returns trimmed, lowercase email string, sans display name.
// Returns "", ErrInvalidEmail if it fails to parse.
func parseEmail(email string) (string, error) {
	// Trims email, display name
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return "", ErrInvalidEmail
	}
	return strings.ToLower(addr.Address), nil
}

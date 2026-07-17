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

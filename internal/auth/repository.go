package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/corygyarmathy/typist/internal/auth/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type Repository interface {
	EmailRegistered(ctx context.Context, email string) (exists bool, err error)
	CreateUser(ctx context.Context) (userID uuid.UUID, err error)
	CreatePasswordCredential(ctx context.Context, userID uuid.UUID, email string, hash string) (err error)
}

type pgxRepository struct {
	q *db.Queries
}

func newPgxRepository(dbtx db.DBTX) *pgxRepository {
	return &pgxRepository{q: db.New(dbtx)}
}

func (r *pgxRepository) EmailRegistered(ctx context.Context, email string) (bool, error) {
	// call the generated PasswordCredentialExists, passing email as the identifier
	exists, err := r.q.PasswordCredentialExists(ctx, email)
	if err != nil {
		return false, fmt.Errorf("checking password credential: %w", err)
	}

	return exists, nil
}

func (r *pgxRepository) CreateUser(ctx context.Context) (uuid.UUID, error) {
	id, err := r.q.CreateUser(ctx)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("creating user in db: %w", err)
	}

	return uuid.UUID(id.Bytes), nil
}

func (r *pgxRepository) CreatePasswordCredential(
	ctx context.Context,
	userID uuid.UUID,
	email string,
	hash string,
) error {
	err := r.q.CreateCredential(ctx, db.CreateCredentialParams{
		UserID:     pgtype.UUID{Bytes: userID, Valid: true},
		CredKind:   "password",
		Identifier: email,
		Secret:     &hash,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrEmailTaken
		}
		return fmt.Errorf("creating password credential in db: %w", err)
	}

	return nil
}

// Prove pgxRepository satisfies the interface at compile time. Surfaces errors
// here if it does not.
var _ Repository = (*pgxRepository)(nil)

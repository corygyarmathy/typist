package progress

import (
	"context"
	"errors"
	"fmt"

	"github.com/corygyarmathy/typist/internal/progress/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type Repository interface {
	CreateUserProgress(ctx context.Context, userID uuid.UUID, competency []byte) (err error)
	GetUserProgress(ctx context.Context, userID uuid.UUID) ([]byte, error)
}

type pgxRepository struct {
	q *db.Queries
}

func newPgxRepository(dbtx db.DBTX) *pgxRepository {
	return &pgxRepository{q: db.New(dbtx)}
}

func (r *pgxRepository) CreateUserProgress(
	ctx context.Context,
	userID uuid.UUID,
	competency []byte,
) error {
	err := r.q.CreateUserProgress(ctx, db.CreateUserProgressParams{
		UserID:     pgtype.UUID{Bytes: userID, Valid: true},
		Competency: competency,
	})
	if err != nil {
		return fmt.Errorf("creating user progress in db: %w", err)
	}

	return nil
}

func (r *pgxRepository) GetUserProgress(
	ctx context.Context,
	userID uuid.UUID,
) ([]byte, error) {
	userProgress, err := r.q.GetUserProgress(
		ctx,
		pgtype.UUID{Bytes: userID, Valid: true},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProgressNotFound
		}
		return nil, fmt.Errorf("getting user progress from db: %w", err)
	}

	return userProgress, nil
}

// Prove pgxRepository satisfies the interface at compile time. Surfaces errors
// here if it does not.
var _ Repository = (*pgxRepository)(nil)

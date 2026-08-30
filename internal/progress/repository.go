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

// Prove pgxRepository satisfies the interface at compile time. Surfaces errors
// here if it does not.
var _ Repository = (*pgxRepository)(nil)

type Repository interface {
	CreateUserProgress(ctx context.Context, userID uuid.UUID, competency []byte) (err error)
	GetUserProgress(ctx context.Context, userID uuid.UUID) ([]byte, error)
	GetUserProgressForUpdate(ctx context.Context, userID uuid.UUID) ([]byte, error)
	UpdateUserProgress(ctx context.Context, userID uuid.UUID, competency []byte) error
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

// GetUserProgressForUpdate is only meaningful inside a transaction: the row lock
// its FOR UPDATE takes is released as soon as the connection is returned to the
// pool. NewStore is the constructor that enforces this, by accepting only a
// pgx.Tx. Nothing prevents a pool-bound caller reaching this method through the
// Repository interface, so the guard lives one layer up from the hazard - the
// seam that crosses a context boundary is the one that got it.
func (r *pgxRepository) GetUserProgressForUpdate(
	ctx context.Context,
	userID uuid.UUID,
) ([]byte, error) {
	userProgress, err := r.q.GetUserProgressForUpdate(
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

func (r *pgxRepository) UpdateUserProgress(
	ctx context.Context,
	userID uuid.UUID,
	competency []byte,
) error {
	err := r.q.UpdateUserProgress(ctx, db.UpdateUserProgressParams{
		UserID:     pgtype.UUID{Bytes: userID, Valid: true},
		Competency: competency,
	})
	if err != nil {
		return fmt.Errorf("updating user progress in db: %w", err)
	}

	return nil
}

package progress

import (
	"context"
	"fmt"

	"github.com/corygyarmathy/typist/internal/progress/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

type Repository interface {
	CreateUserProgress(ctx context.Context, userID uuid.UUID, competency []byte) (err error)
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

// Prove pgxRepository satisfies the interface at compile time. Surfaces errors
// here if it does not.
var _ Repository = (*pgxRepository)(nil)

package session

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/corygyarmathy/typist/internal/session/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// Prove pgxRepository satisfies the interface at compile time. Surfaces errors
// here if it does not.
var _ Repository = (*pgxRepository)(nil)

type Repository interface {
	CreateSession(
		ctx context.Context,
		userID uuid.UUID,
		wpm int32,
		accuracy float64,
		completedAt time.Time,
	) (Session, error)
}

type pgxRepository struct {
	q *db.Queries
}

func newPgxRepository(dbtx db.DBTX) *pgxRepository {
	return &pgxRepository{q: db.New(dbtx)}
}

func numericValue(value float64) (pgtype.Numeric, error) {
	// check should always be true, defense-in-depth
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
		return pgtype.Numeric{}, fmt.Errorf("accuracy must be between 0 and 1")
	}

	var n pgtype.Numeric
	err := n.Scan(strconv.FormatFloat(value, 'f', -1, 64))
	return n, err
}

// CreateSession saves the completed client session to the database.
func (r *pgxRepository) CreateSession(
	ctx context.Context,
	userID uuid.UUID,
	wpm int32,
	accuracy float64,
	completedAt time.Time,
) (Session, error) {
	nAcc, err := numericValue(accuracy)
	if err != nil {
		return Session{}, fmt.Errorf("converting accuracy from float64 to NUMERIC: %w", err)
	}
	sessionRow, err := r.q.CreateSession(ctx, db.CreateSessionParams{
		UserID:      pgtype.UUID{Bytes: userID, Valid: true},
		Wpm:         wpm,
		Accuracy:    nAcc,
		CompletedAt: pgtype.Timestamptz{Time: completedAt, Valid: true},
	})
	if err != nil {
		return Session{}, fmt.Errorf("creating session in db: %w", err)
	}

	accFloat8, err := sessionRow.Accuracy.Float64Value()

	if err != nil {
		return Session{}, err
	}

	if !accFloat8.Valid {
		// Accuracy was NULL in PostgreSQL.
		return Session{}, fmt.Errorf("accuracy is NULL in Session table row")
	}

	acc := accFloat8.Float64

	session := Session{
		ID:          uuid.UUID(sessionRow.ID.Bytes),
		WPM:         int(sessionRow.Wpm),
		Accuracy:    acc,
		CompletedAt: sessionRow.CompletedAt.Time,
	}

	return session, nil
}

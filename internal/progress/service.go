package progress

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Business logic. The service depends on the Repository
// interface, never on the concrete implementation. Transactions are
// orchestrated here, not in handlers or repositories.

type Initialiser struct {
	repo Repository
}

func NewInitialiser(tx pgx.Tx) *Initialiser {
	return &Initialiser{repo: newPgxRepository(tx)} // tx satisfies DBTX -> repo runs on this tx
}

func (s *Initialiser) CreateInitial(ctx context.Context, userID uuid.UUID) error {
	// TODO(phase-3): replace with engine.InitialCompetency()
	initialCompetency := []byte(`{"version":0}`)

	return s.repo.CreateUserProgress(ctx, userID, initialCompetency)
}

type Service struct {
	repo Repository
	pool *pgxpool.Pool
}

func NewService(
	pool *pgxpool.Pool,
) *Service {
	return &Service{
		repo: newPgxRepository(pool),
		pool: pool,
	}
}

func (s *Service) GetProgress(ctx context.Context, userID uuid.UUID) ([]byte, error) {
	userProgress, err := s.repo.GetUserProgress(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("getting user progress from db: %w", err)
	}
	return userProgress, nil
}

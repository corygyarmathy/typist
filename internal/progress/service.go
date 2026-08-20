package progress

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/corygyarmathy/typist/internal/engine"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Business logic. The service depends on the Repository
// interface, never on the concrete implementation. Transactions are
// orchestrated here, not in handlers or repositories.

type Initialiser struct {
	repo   Repository
	corpus engine.Corpus
}

func NewInitialiser(tx pgx.Tx, c engine.Corpus) *Initialiser {
	return &Initialiser{repo: newPgxRepository(tx), corpus: c}
}

func (s *Initialiser) CreateInitial(ctx context.Context, userID uuid.UUID) error {
	initialCompetency, err := json.Marshal(engine.InitialCompetency(s.corpus))
	if err != nil {
		return fmt.Errorf("marshalling initial competency: %w", err)
	}
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

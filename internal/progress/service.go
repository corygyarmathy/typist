package progress

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	// TODO(phase-3): replace with adaptive.InitialCompetency()
	initialCompetency := []byte(`{"version":0}`)

	return s.repo.CreateUserProgress(ctx, userID, initialCompetency)
}

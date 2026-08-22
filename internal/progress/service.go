package progress

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"time"

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
	repo    Repository
	pool    *pgxpool.Pool
	corpus  engine.Corpus
	newRand func() *rand.Rand
	now     func() time.Time
}

func NewService(
	pool *pgxpool.Pool,
	corpus engine.Corpus,
) *Service {
	return &Service{
		repo:    newPgxRepository(pool),
		pool:    pool,
		corpus:  corpus,
		newRand: func() *rand.Rand { return rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())) },
		now:     time.Now,
	}
}

func (s *Service) GetProgress(ctx context.Context, userID uuid.UUID) ([]byte, error) {
	userProgress, err := s.repo.GetUserProgress(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("getting user progress: %w", err)
	}
	return userProgress, nil
}

func (s *Service) NextLesson(ctx context.Context, userID uuid.UUID) (engine.Lesson, error) {
	userProgress, err := s.repo.GetUserProgress(ctx, userID)
	if err != nil {
		return engine.Lesson{}, fmt.Errorf("loading competency: %w", err)
	}
	var competency engine.CompetencyState
	if err := json.Unmarshal(userProgress, &competency); err != nil {
		return engine.Lesson{}, fmt.Errorf("unmarshalling stored competency: %w", err)
	}

	lesson := engine.NextLesson(competency, s.corpus, s.now(), s.newRand())
	if len(lesson.Words) == 0 {
		return engine.Lesson{}, ErrEmptyLesson
	}
	return lesson, nil
}

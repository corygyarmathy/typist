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

// Store lends the competency document to another bounded context for the
// duration of one transaction. internal/session declares the interface it
// satisfies; cmd/server/wiring.go supplies it, so session never imports this
// package.
//
// It speaks engine.CompetencyState rather than []byte deliberately: the JSON
// encoding is a persistence detail this package already owns (CreateInitial
// marshals the same document), and handing raw bytes across the seam would mean
// session decoding a storage format belonging to progress. Service.GetProgress
// keeps its []byte passthrough - there the bytes go straight to the wire and are
// never interpreted.
type Store struct {
	repo Repository
}

// NewStore binds a Store to a transaction. There is deliberately no pool-bound
// variant: LoadForUpdate issues SELECT ... FOR UPDATE, and a row lock taken on a
// pool connection is released the moment that connection returns to the pool.
// That would be a lock which appears to serialise concurrent submissions and
// silently does not. Taking pgx.Tx here makes the mistake fail to compile.
func NewStore(tx pgx.Tx) *Store {
	return &Store{repo: newPgxRepository(tx)}
}

// LoadForUpdate reads the caller's competency document and holds a row lock on
// it until the surrounding transaction ends, so a concurrent submission for the
// same user blocks here rather than computing its update from a document that is
// about to be overwritten.
func (s *Store) LoadForUpdate(
	ctx context.Context,
	userID uuid.UUID,
) (engine.CompetencyState, error) {
	raw, err := s.repo.GetUserProgressForUpdate(ctx, userID)
	if err != nil {
		return engine.CompetencyState{}, fmt.Errorf("getting user progress for update: %w", err)
	}

	var cs engine.CompetencyState
	if err := json.Unmarshal(raw, &cs); err != nil {
		return engine.CompetencyState{}, fmt.Errorf("unmarshalling stored competency: %w", err)
	}

	return cs, nil
}

// Save writes the whole document back. Competency is load-whole / write-whole
// per user (docs/schema.md), so this replaces rather than merges - which is only
// safe because LoadForUpdate held the lock across the read-modify-write.
func (s *Store) Save(ctx context.Context, userID uuid.UUID, cs engine.CompetencyState) error {
	data, err := json.Marshal(cs)
	if err != nil {
		return fmt.Errorf("marshalling competency state data: %w", err)
	}

	if err := s.repo.UpdateUserProgress(ctx, userID, data); err != nil {
		return fmt.Errorf("saving competency state: %w", err)
	}

	return nil
}

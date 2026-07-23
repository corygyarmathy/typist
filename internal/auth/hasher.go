package auth

import (
	"context"
	"fmt"
)

// dummyPlaintext is hashed once at Hasher construction so Login has a real
// argon2 verify to run when an email is unknown - see Hasher.Verify and the
// timing-oracle defence in Service.Login.
const dummyPlaintext = "dummy-password-defeats-timing-oracle"

// Hasher bounds how many argon2id operations run concurrently. Each derivation
// costs ~64 MiB (the memory parameter), so an unbounded burst of register/login
// requests could exhaust a small host's RAM. A buffered channel used as a
// counting semaphore caps concurrency at maxParallel; further calls block
// (backpressure) until a slot frees rather than all allocating at once.
//
// It pairs with the rate-limiting on /auth/* that is deferred to ADR 0008: the
// semaphore bounds resource use, rate-limiting will bound request rate.
type Hasher struct {
	sem       chan struct{}
	dummyHash string // precomputed PHC over dummyPlaintext; used by the login timing equaliser
}

// NewHasher builds a Hasher permitting maxParallel concurrent argon2 operations
// (clamped to at least 1) and precomputes the dummy hash. It returns an error
// only if that precomputation fails, which in practice means crypto/rand is
// unavailable - fatal at startup anyway.
func NewHasher(maxParallel int) (*Hasher, error) {
	if maxParallel < 1 {
		maxParallel = 1
	}
	h := &Hasher{sem: make(chan struct{}, maxParallel)}

	// No contention at startup, so this returns immediately; it also proves the
	// hashing path works before accepting traffic.
	dummy, err := h.Hash(context.Background(), dummyPlaintext)
	if err != nil {
		return nil, fmt.Errorf("precomputing dummy hash: %w", err)
	}
	h.dummyHash = dummy
	return h, nil
}

// Hash derives a PHC-format argon2id hash of plain, blocking until a
// concurrency slot is free or ctx is cancelled.
func (h *Hasher) Hash(ctx context.Context, plain string) (string, error) {
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer h.release()
	return hashPassword(plain)
}

// Verify re-derives plain under the parameters encoded in phc and reports
// whether it matches, blocking until a concurrency slot is free or ctx is
// cancelled. A non-matching password is (false, nil); only a malformed phc is an
// error.
func (h *Hasher) Verify(ctx context.Context, plain, phc string) (bool, error) {
	if err := h.acquire(ctx); err != nil {
		return false, err
	}
	defer h.release()
	return verifyPassword(plain, phc)
}

// acquire takes a semaphore slot, or returns ctx.Err() if ctx is cancelled while
// waiting - so a disconnected client doesn't tie up a slot indefinitely.
func (h *Hasher) acquire(ctx context.Context) error {
	select {
	case h.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Hasher) release() { <-h.sem }

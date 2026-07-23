package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHasher_HashVerifyRoundTrip(t *testing.T) {
	h, err := NewHasher(2)
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	ctx := context.Background()

	const plain = "correct horse battery staple"
	phc, err := h.Hash(ctx, plain)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	ok, err := h.Verify(ctx, plain, phc)
	if err != nil {
		t.Fatalf("Verify (right password): %v", err)
	}
	if !ok {
		t.Error("Verify rejected the correct password")
	}

	ok, err = h.Verify(ctx, "the wrong password", phc)
	if err != nil {
		t.Fatalf("Verify (wrong password): unexpected error: %v", err)
	}
	if ok {
		t.Error("Verify accepted the wrong password")
	}
}

func TestHasher_PrecomputesDummyHash(t *testing.T) {
	h, err := NewHasher(1)
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	if h.dummyHash == "" {
		t.Fatal("NewHasher left dummyHash empty")
	}
	// The dummy must be a real, verifiable hash so the login timing equaliser
	// runs a genuine argon2 verify rather than erroring on a malformed string.
	ok, err := verifyPassword(dummyPlaintext, h.dummyHash)
	if err != nil || !ok {
		t.Fatalf("dummy hash does not verify: ok=%v err=%v", ok, err)
	}
}

func TestHasher_CancelledContextWhileWaiting(t *testing.T) {
	h, err := NewHasher(1)
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	h.sem <- struct{}{} // occupy the only slot so acquire must wait

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := h.Hash(ctx, "anything"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Hash: want context.Canceled, got %v", err)
	}
}

func TestHasher_BlocksWhenSaturated(t *testing.T) {
	h, err := NewHasher(1)
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	h.sem <- struct{}{} // saturate the single slot

	done := make(chan struct{})
	go func() {
		_, _ = h.Hash(context.Background(), "queued call")
		close(done)
	}()

	// While saturated, the queued Hash must not proceed.
	select {
	case <-done:
		t.Fatal("Hash proceeded while the semaphore was full")
	case <-time.After(50 * time.Millisecond):
	}

	<-h.sem // free the slot

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Hash did not proceed after a slot freed")
	}
}

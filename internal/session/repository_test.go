package session

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/corygyarmathy/typist/internal/platform/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestPool opens a pool against DATABASE_URL, and migrates. Note that
// truncation isn't global to allow parallel testing. createTestUser cleans up
// after itself by primary key.
//
// Skips entirely when DATABASE_URL is unset (so local `make test` stays green).
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("integration test: set DATABASE_URL (make docker-up)")
	}
	ctx := context.Background()

	pool, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to open database pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	err = database.Migrate(ctx, pool)
	if err != nil {
		t.Fatalf("failed to apply database migrations: %v", err)
	}

	return pool
}

// createTestUser inserts a bare users row and returns its ID, deleting it when
// the test finishes.
//
// The statement is the one internal/auth/queries.sql already runs in production
// (CreateUser). Every users column has a default and the table carries no unique
// constraint, so repeated -count runs and parallel package binaries cannot
// collide with each other.
//
// Cleanup is by primary key rather than by identifier: a user created this way
// has no auth_credentials row to look it up through. ON DELETE CASCADE takes the
// sessions rows written during the test with it.
func createTestUser(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	// pgx has no codec registered for google/uuid, so scan the column into
	// pgtype.UUID and convert.
	var id pgtype.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO users DEFAULT VALUES RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("creating test user: %v", err)
	}
	userID := uuid.UUID(id.Bytes)

	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			`DELETE FROM users WHERE id = $1`, id); err != nil {
			t.Errorf("cleanup: deleting user %s: %v", userID, err)
		}
	})

	return userID
}

// TestCreateSession_NumericRoundTrip guards the accuracy conversio. accuracy is
// a NUMERIC column and the repository converts it to and from float64 by hand.
//
// That conversion is the only thing at this layer that can be quietly wrong. A
// truncated or rounded accuracy is still a *valid* accuracy - it satisfies the
// CHECK constraint, deserialises fine, and renders fine - so no other test in
// the stack would ever notice. Hence a test that asserts the value, not just
// that the insert succeeded.
func TestCreateSession_NumericRoundTrip(t *testing.T) {
	pool := newTestPool(t)
	repo := newPgxRepository(pool)
	ctx := context.Background()

	cases := []struct {
		name     string
		accuracy float64
	}{
		// Not exactly representable in binary floating point, and long enough
		// to catch a formatter pinned to a fixed number of decimal places
		// (FormatFloat(v, 'f', 2, 64) would land this on 0.92).
		{"three decimal places", 0.917},
		// 11/12. Full float64 precision, so a conversion that goes via float32
		// or via a fixed-scale NUMERIC loses digits here.
		{"repeating fraction", 0.9166666666666666},
		// The boundaries the column's CHECK allows. Perfect accuracy is also
		// the single most common real value, so it must not be special-cased
		// into failure.
		{"perfect", 1},
		{"total failure", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			userID := createTestUser(t, pool)

			// Postgres timestamptz has microsecond resolution; time.Now has
			// nanosecond. Truncating on the way in means the value read back is
			// the value written, so the comparison below tests the repository
			// rather than the column's precision.
			completedAt := time.Now().UTC().Truncate(time.Microsecond)

			const wpm = 42

			got, err := repo.CreateSession(ctx, userID, wpm, tc.accuracy, completedAt)
			if err != nil {
				t.Fatalf("CreateSession: %v", err)
			}

			if got.Accuracy != tc.accuracy {
				t.Errorf("accuracy round trip: got %v, want %v (delta %g)",
					got.Accuracy, tc.accuracy, got.Accuracy-tc.accuracy)
			}
			if got.WPM != wpm {
				t.Errorf("wpm: got %d, want %d", got.WPM, wpm)
			}
			// Equal, not ==: marshalling and a database round trip both strip
			// the monotonic clock reading a time.Time carries, so two values
			// naming the same instant compare unequal under ==.
			if !got.CompletedAt.Equal(completedAt) {
				t.Errorf("completed_at: got %v, want %v", got.CompletedAt, completedAt)
			}
			if got.ID == uuid.Nil {
				t.Error("returned session ID is the zero UUID")
			}
		})
	}
}

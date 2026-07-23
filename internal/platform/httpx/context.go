package httpx

import (
	"context"

	"github.com/google/uuid"
)

// ctxKey is unexported so no other package can collide with our context keys.
type ctxKey int

const (
	requestIDKey ctxKey = iota
	userIDKey
)

// WithRequestID returns a copy of ctx carrying the given request ID. The
// middleware sets it; handlers and the problem-detail encoder read it back.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFromContext returns the request ID stored by the middleware, or
// the empty string if none is present.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// WithUserID returns a copy of ctx carrying the authenticated user's ID.
// The auth middleware sets it after validating the bearer token.
func WithUserID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, userIDKey, id)
}

// UserIDFromContext returns the user ID injected by the auth middleware.
// The bool reports whether one was present - a zero uuid.UUID is
// indistinguishable from "unset" otherwise.
func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(userIDKey).(uuid.UUID)
	return id, ok
}

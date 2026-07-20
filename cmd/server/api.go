package main

import (
	"context"
	"errors"
	"time"

	"github.com/corygyarmathy/typist/internal/auth"
	"github.com/corygyarmathy/typist/internal/openapi"
)

// Sentinels the composition-root error handler maps to HTTP statuses.
// Returning these from a strict method is how we produce a problem+json
// without hand-building the generated Problem type (which uses pointer fields).
var (
	errNotImplemented = errors.New("not implemented") // -> 501
	errNotReady       = errors.New("not ready")       // -> 503
)

// API is the composition-root aggregate. oapi-codegen emits one interface
// covering every operation, but the handlers live per bounded context. This
// type is where the monolithic generated interface meets our split: it
// satisfies StrictServerInterface, and will delegate each op to the
// right context's handler. For now the domain ops are stubbed.
type API struct {
	ready func(ctx context.Context) error
	auth  *auth.Handler
}

// Compile-time proof that *API satisfies the generated interface. If a method
// is missing or has the wrong signature, this line fails to compile and names
// what's wrong.
var _ openapi.StrictServerInterface = (*API)(nil)

// GetHealthz success path returns a typed response object and a nil error;
// the generated strictHandler writes it (200, application/json,
// {"status":"ok"}). Note the return type is the generated one, not httpx.
func (a *API) GetHealthz(
	ctx context.Context,
	req openapi.GetHealthzRequestObject,
) (openapi.GetHealthzResponseObject, error) {
	return openapi.GetHealthz200JSONResponse{Status: openapi.Ok}, nil
}

// GetReadyz confirms the database is reachable, returning status ok if the db
// ping succeeds within 2 seconds.
func (a *API) GetReadyz(
	ctx context.Context,
	req openapi.GetReadyzRequestObject,
) (openapi.GetReadyzResponseObject, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// attempt to ping db; return not ready if it times out
	if err := a.ready(ctx); err != nil {
		return nil, errNotReady
	}

	return openapi.GetReadyz200JSONResponse{Status: openapi.Ok}, nil
}

// RegisterUser registers a user: initialising all required objects.
// Returns a JWT for the user to authenticate with.
func (a *API) RegisterUser(
	ctx context.Context,
	req openapi.RegisterUserRequestObject,
) (openapi.RegisterUserResponseObject, error) {
	return a.auth.RegisterUser(ctx, req)
}

// LoginUser logs in a user, assuming they are pre-registered and the correct
// credentials (email and password) are provided.
// Returns a JWT for the user to authenticate with.
func (a *API) LoginUser(
	ctx context.Context,
	req openapi.LoginUserRequestObject,
) (openapi.LoginUserResponseObject, error) {
	return a.auth.LoginUser(ctx, req)
}

// GetProgress Placeholder logic, not implemented
func (a *API) GetProgress(
	ctx context.Context,
	_ openapi.GetProgressRequestObject,
) (openapi.GetProgressResponseObject, error) {
	return nil, errNotImplemented
}

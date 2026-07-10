package auth

import (
	"context"
	"fmt"

	"github.com/corygyarmathy/typist/internal/openapi"
)

// HTTP handlers go here. Handlers should be thin:
//   - parse request (path params, query, body)
//   - call into the Service
//   - format response via internal/platform/httpx response helpers

type Handler struct {
	service *Service // you'll define Service next — this is the "pull" to step 5
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterUser(
	ctx context.Context,
	req openapi.RegisterUserRequestObject,
) (openapi.RegisterUserResponseObject, error) {
	if req.Body == nil {
		return nil, ErrReqBodyEmpty
	}
	// ensure border between handler and openapi pkg
	email := string(req.Body.Email)
	password := req.Body.Password

	token, err := h.service.Register(ctx, email, password)
	if err != nil {
		return nil, fmt.Errorf("registering user: %w", err)
	}
	return openapi.RegisterUser200JSONResponse{
			ExpiresIn: token.ExpiresIn,
			Token:     token.Value,
			TokenType: "Bearer",
		},
		nil
}

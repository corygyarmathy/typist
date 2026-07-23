package progress

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/corygyarmathy/typist/internal/openapi"
	"github.com/corygyarmathy/typist/internal/platform/httpx"
)

// HTTP handlers go here. Handlers should be thin:
//   - parse request (path params, query, body)
//   - call into the Service
//   - format response w/ openapi TokenResponse types

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetProgress(
	ctx context.Context,
	req openapi.GetProgressRequestObject,
) (openapi.GetProgressResponseObject, error) {
	userID, ok := httpx.UserIDFromContext(ctx)
	if !ok {
		// the middleware guarantees a user ID here, so this is an internal server error
		return nil, fmt.Errorf("failed to get userID from context")
	}

	userProgress, err := h.service.GetProgress(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("getting user progress: %w", err)
	}
	var competency openapi.Competency // map[string]interface{}
	if err := json.Unmarshal(userProgress, &competency); err != nil {
		return nil, fmt.Errorf("unmarshalling stored competency: %w", err)
	}
	return openapi.GetProgress200JSONResponse(competency), nil
}

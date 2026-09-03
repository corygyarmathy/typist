package session

import (
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/corygyarmathy/typist/internal/engine"
	"github.com/corygyarmathy/typist/internal/openapi"
	"github.com/corygyarmathy/typist/internal/platform/httpx"
)

// HTTP handlers go here. Handlers should be thin:
// - parse request (path params, query, body)
// - call into the Service
// - format response via internal/platform/httpx response helpers

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) SubmitSession(
	ctx context.Context,
	req openapi.SubmitSessionRequestObject,
) (openapi.SubmitSessionResponseObject, error) {
	userID, ok := httpx.UserIDFromContext(ctx)
	if !ok {
		// RequireAuth guarantees a user ID on this route, so its absence is a
		// server fault, not a client one.
		return nil, fmt.Errorf("failed to get userID from context")
	}

	res, err := toEngineResult(*req.Body)
	if err != nil {
		return nil, err // already wraps ErrInvalidObservation -> 400
	}

	sess, err := h.service.Submit(ctx, userID, res)
	if err != nil {
		return nil, fmt.Errorf("submitting session: %w", err)
	}

	return openapi.SubmitSession201JSONResponse{
		Id:          sess.ID,
		Wpm:         sess.WPM,
		Accuracy:    sess.Accuracy,
		CompletedAt: sess.CompletedAt,
	}, nil
}

func toEngineResult(sub openapi.SessionSubmission) (engine.Result, error) {
	res := engine.Result{
		Keys:   make(map[rune]engine.Observation, len(sub.Keys)),
		Ngrams: make(map[string]engine.Observation, len(sub.Ngrams)),
	}

	for k, o := range sub.Keys {
		// The wire keys the map by the character itself ("e"), not by code
		// point, so anything longer than one character is malformed input the
		// engine's map cannot represent.
		if utf8.RuneCountInString(k) != 1 {
			return engine.Result{}, fmt.Errorf(
				"%w: key %q is not a single character", ErrInvalidObservation, k)
		}
		res.Keys[[]rune(k)[0]] = toObservation(o)
	}

	for n, o := range sub.Ngrams {
		res.Ngrams[n] = toObservation(o)
	}

	return res, nil
}

// toObservation widens the wire's integral milliseconds to the float64 the
// engine averages in. openapi.Observation is integral on the wire by design;
// see the SessionSubmission schema comment.
func toObservation(o openapi.Observation) engine.Observation {
	return engine.Observation{
		Attempts:    o.Attempts,
		Errors:      o.Errors,
		TotalMillis: float64(o.TotalMillis),
	}
}

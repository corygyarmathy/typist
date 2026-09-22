package main

// the API client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/corygyarmathy/typist/internal/openapi"
)

// with pointer receivers, the target must be var se *statusError;
// errors.As(err, &se). Passing &statusError{} compiles and always
// fails to match.
type statusError struct {
	Status int
	err    error
}

func (e *statusError) Error() string { return e.err.Error() }
func (e *statusError) Unwrap() error { return e.err }

type Client struct {
	client  *http.Client
	baseURL string
	token   string
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		client:  &http.Client{Timeout: time.Second * 10},
		baseURL: baseURL,
		token:   token,
	}
}

func (c *Client) NextLesson(ctx context.Context) (openapi.Lesson, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.baseURL+"/api/v1/lessons/next",
		nil,
	)
	if err != nil {
		return openapi.Lesson{}, fmt.Errorf("constructing get request: %w", err)
	}

	// set request headers
	req.Header.Set("Authorization", "Bearer "+c.token)

	res, err := c.client.Do(req)
	if err != nil {
		return openapi.Lesson{}, fmt.Errorf("making get request: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return openapi.Lesson{}, errorFromResponse(res)
	}

	var lesson openapi.Lesson
	decoder := json.NewDecoder(res.Body)
	err = decoder.Decode(&lesson)
	if err != nil {
		return openapi.Lesson{}, fmt.Errorf("decoding lesson JSON: %w", err)
	}

	return lesson, nil
}

func (c *Client) SubmitSession(ctx context.Context, sub openapi.SessionSubmission) (openapi.SessionSummary, error) {
	jsonData, err := json.Marshal(sub)
	if err != nil {
		return openapi.SessionSummary{}, fmt.Errorf("JSON marshalling session submission: %w", err)
	}
	bodyReader := bytes.NewReader(jsonData)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/api/v1/sessions",
		bodyReader,
	)
	if err != nil {
		return openapi.SessionSummary{}, fmt.Errorf("constructing get request: %w", err)
	}

	// set request headers
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	res, err := c.client.Do(req)
	if err != nil {
		return openapi.SessionSummary{}, fmt.Errorf("making get request: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusCreated {
		return openapi.SessionSummary{}, errorFromResponse(res)
	}

	var summary openapi.SessionSummary
	decoder := json.NewDecoder(res.Body)
	err = decoder.Decode(&summary)
	if err != nil {
		return openapi.SessionSummary{}, fmt.Errorf("decoding session summary JSON: %w", err)
	}

	return summary, nil
}

func errorFromResponse(res *http.Response) error {
	return &statusError{Status: res.StatusCode, err: errorBody(res)}
}

func errorBody(res *http.Response) error {
	var maxBytes int64 = 4 * 1024
	if strings.HasPrefix(res.Header.Get("Content-Type"), "application/problem+json") {
		var problem openapi.Problem
		decoder := json.NewDecoder(io.LimitReader(res.Body, maxBytes))
		err := decoder.Decode(&problem)
		if err != nil {
			return fmt.Errorf("HTTP error: %d, decoding problem JSON: %w", res.StatusCode, err)
		}
		return fmt.Errorf(
			"title: %v, status: %v, detail %v, instance %v",
			problem.Title, problem.Status, deref(problem.Detail), deref(problem.Instance),
		)
	}

	body, err := readResponseBody(res, maxBytes)
	if err != nil {
		return fmt.Errorf("HTTP error: %d, reading plain problem response body: %w", res.StatusCode, err)
	}

	// plaintext error
	return fmt.Errorf("HTTP error: %d, body: %v", res.StatusCode, string(body))
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func readResponseBody(res *http.Response, maxBytes int64) (body []byte, err error) {
	// Read one extra byte so we can detect truncation
	data, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}

	if int64(len(data)) > maxBytes {
		return data[:maxBytes], nil
	}

	return data, nil
}

package main

// the API client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/corygyarmathy/typist/internal/openapi"
)

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
		return openapi.Lesson{}, fmt.Errorf("HTTP error: %v", res.StatusCode)
	}

	var lesson openapi.Lesson
	decoder := json.NewDecoder(res.Body)
	err = decoder.Decode(&lesson)
	if err != nil {
		return openapi.Lesson{}, fmt.Errorf("decoding lesson JSON: %w", err)
	}

	return lesson, nil
}

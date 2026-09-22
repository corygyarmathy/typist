package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/corygyarmathy/typist/internal/openapi"
)

type TokenStore struct{ dir string }

// NewTokenStoreIn takes the dir as given, returning a TokenStore
func NewTokenStoreIn(dir string) *TokenStore {
	return &TokenStore{dir: dir}
}

// NewTokenStore resolves XDG, may fail. Returns a TokenStore
func NewTokenStore() (*TokenStore, error) {
	dir, err := stateDir()
	if err != nil {
		return &TokenStore{}, fmt.Errorf("getting state dir: %w", err)
	}

	return NewTokenStoreIn(dir), nil
}

type storedToken struct {
	Token     string    `json:"token"` // the signed JWT
	ExpiresAt time.Time `json:"expires_at"`
}

// The filename lives in one place so Save and Load cannot disagree.
func (s *TokenStore) path() string {
	return filepath.Join(s.dir, "token.json")
}

// Save JWT to file, creates dir if missing.
func (s *TokenStore) Save(tr openapi.TokenResponse, now time.Time) error {
	err := os.MkdirAll(s.dir, 0o700)
	if err != nil {
		return fmt.Errorf("making token store directory: %w", err)
	}
	token := storedToken{
		Token:     tr.Token,
		ExpiresAt: now.Add(time.Duration(tr.ExpiresIn) * time.Second),
	}

	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("marshalling token to JSON: %w", err)
	}

	err = os.WriteFile(s.path(), data, 0o600)
	if err != nil {
		return fmt.Errorf("writing JWT file: %w", err)
	}

	return nil
}

func (s *TokenStore) Load() (storedToken, error) {
	file, err := os.ReadFile(s.path())
	if err != nil {
		return storedToken{}, fmt.Errorf("loading token file %w", err)
	}
	var token storedToken
	err = json.Unmarshal(file, &token)
	if err != nil {
		return storedToken{}, fmt.Errorf("unmarshalling stored token: %w", err)
	}

	return token, nil
}

// check if JWT is live (not expired)
func (t storedToken) Live(now time.Time) bool {
	return now.Before(t.ExpiresAt)
}

func stateDir() (string, error) { // which directory does this machine use for application state
	stateHome := "XDG_STATE_HOME"

	statePath := os.Getenv(stateHome)

	// XDG spec states a relative value must be treated as unset
	if statePath == "" || !filepath.IsAbs(statePath) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("getting user home dir: %w", err)
		}
		statePath = filepath.Join(homeDir, ".local", "state")
	}

	typistPath := filepath.Join(statePath, "typist")

	return typistPath, nil
}

// Command server is the entry point for the typist backend.
//
// Responsibilities:
//   - Load configuration from environment
//   - Initialise logging
//   - Open the database pool and run migrations
//   - Wire dependencies (repositories -> services -> handlers)
//   - Start the HTTP server with graceful shutdown
//
// All wiring lives here so the rest of the codebase has no global state.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/corygyarmathy/typist/internal/auth"
	"github.com/corygyarmathy/typist/internal/corpus"
	"github.com/corygyarmathy/typist/internal/platform/config"
	"github.com/corygyarmathy/typist/internal/platform/database"
	"github.com/corygyarmathy/typist/internal/platform/logging"
	"github.com/corygyarmathy/typist/internal/progress"
	"github.com/corygyarmathy/typist/internal/session"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited with error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	// TODO(phase-6): expose /metrics

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logging.Setup(cfg.LogLevel, cfg.Env)

	// intit before db: it's cheap and fails fast
	corpusProvider, err := corpus.New()
	if err != nil {
		return fmt.Errorf("loading corpus: %w", err)
	}

	dbPool, err := database.Open(
		context.Background(),
		cfg.DatabaseURL,
	)
	if err != nil {
		return err
	}
	defer dbPool.Close()
	slog.Info("database pool opened")

	err = database.Migrate(ctx, dbPool)
	if err != nil {
		return err
	}
	slog.Info("database migrations applied")

	authn := auth.NewAuthenticator([]byte(cfg.JWTSecret.Reveal()), cfg.JWTAccessTTL)
	hasher, err := auth.NewHasher(cfg.Argon2MaxParallel)
	if err != nil {
		return err
	}
	authSvc := auth.NewService(dbPool, newProgressInitialiser(corpusProvider), authn, hasher)
	authHandler := auth.NewHandler(authSvc)

	progressSvc := progress.NewService(dbPool, corpusProvider)
	progressHandler := progress.NewHandler(progressSvc)

	sessionSvc := session.NewService(dbPool, newCompetencyStore, corpusProvider)
	sessionHandler := session.NewHandler(sessionSvc)

	api := &API{
		ready:    dbPool.Ping,
		auth:     authHandler,
		progress: progressHandler,
		session:  sessionHandler,
	}
	srv := &http.Server{
		Addr:              ":8080",
		Handler:           Router(api, authn),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

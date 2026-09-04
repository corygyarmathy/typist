package main

// the TUI client: (env vars, call, print)

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if err := run(); err != nil {
		slog.Error("client exited with error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	baseURL := getDefault("TYPIST_API_URL", "http://localhost:8080")
	token := getDefault("TYPIST_TOKEN", "")

	client := NewClient(baseURL, token)

	lesson, err := client.NextLesson(ctx)
	if err != nil {
		return fmt.Errorf("getting next lesson: %w", err)
	}

	fmt.Printf("Lesson generated: %d words\n", len(lesson.Words))
	for _, w := range lesson.Words {
		fmt.Printf("%s ", w)
	}

	return nil
}

func getDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

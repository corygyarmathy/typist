package main

import (
	"github.com/corygyarmathy/typist/internal/auth"
	"github.com/corygyarmathy/typist/internal/engine"
	"github.com/corygyarmathy/typist/internal/progress"
	"github.com/corygyarmathy/typist/internal/session"
	"github.com/jackc/pgx/v5"
)

// newProgressInitialiser adapts progress's concrete initialiser to auth's
// ProgressInitialiser interface, so registration can seed user_progress inside
// its transaction without auth importing the progress package.
func newProgressInitialiser(c engine.Corpus) func(pgx.Tx) auth.ProgressInitialiser {
	return func(tx pgx.Tx) auth.ProgressInitialiser {
		return progress.NewInitialiser(tx, c)
	}
}

// newCompetencyStore adapts progress's transaction-bound Store to session's
// CompetencyStore interface, so a submission can lock and rewrite
// user_progress.competency inside its own transaction without session
// importing progress.
func newCompetencyStore(tx pgx.Tx) session.CompetencyStore {
	return progress.NewStore(tx)
}

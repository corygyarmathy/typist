// Package corpus provides the read-only language data the engine needs:
// a frequency order over letters, a frequency-ranked list of ngrams,
// and a context -> next-character transition graph for pseudo-word generation.
//
// Per ADR 0013 this is static reference data, derived from a source corpus by
// a committed generator (cmd/corpusgen) and embedded in the binary using
// a go:embed directive. It is identical for every user and every deployment,
// so it is NOT a database-backed bounded context: there is no handler.go, no
// repository.go, and no SQL. The package serves its data in-process, which is
// what lets the engine run both behind the API and in the offline / anonymous
// client (ADR 0014).
package corpus

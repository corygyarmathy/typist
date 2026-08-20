package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/corygyarmathy/typist/internal/corpus/gen"
)

// TODO: flags, read files, call gen, write JSON, exit non-zero on error
//
// TODO: decide whether the generator takes -in/-out flags or reads a committed
// manifest listing the titles (the manifest doubles as ADR 0013's provenance
// record); and whether make corpus or make corpusgen is the target name — the
// plan's tooling section asks for one either way.

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err) // not using sLog b/c this is run by hand
		os.Exit(1)
	}
}

func run() error { // flags -> read -> count -> encode -> write
	// flags
	in := flag.String("in", "internal/corpus/data/sources", "...")       // location to take sources from
	out := flag.String("out", "internal/corpus/data/corpus.json", "...") // location to output JSON corpus
	flag.Parse()

	// read
	inDir, err := os.ReadDir(*in)
	if err != nil {
		return fmt.Errorf("reading 'in' directory: %w", err)
	}

	var sources []string
	for _, entry := range inDir {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".txt") {
			path := filepath.Join(*in, entry.Name())
			sources = append(sources, path)
		}
	}
	if len(sources) == 0 {
		return fmt.Errorf("no *.txt sources found in the %v directory", *in)
	}

	var sourceData [][]byte
	for _, source := range sources {
		data, err := os.ReadFile(source)
		if err != nil {
			return fmt.Errorf("reading %s: %w", source, err)
		}
		sourceData = append(sourceData, data)
	}

	// count
	counts, err := gen.Count(sourceData)

	if err != nil {
		return fmt.Errorf("counting items in text(s): %w", err)
	}

	data, err := json.MarshalIndent(counts, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling item counts to JSON: %w", err)
	}

	// write
	err = os.WriteFile(*out, append(data, '\n'), 0o644)
	if err != nil {
		return fmt.Errorf("writing to file %v: %w", out, err)
	}

	return nil
}

// Package gen is the pure counting logic; build-time only (don't import into app logic)
package gen

import (
	"fmt"
	"iter"
	"strings"
)

type Counts struct {
	Letters map[string]int `json:"letters"`
	Bigrams map[string]int `json:"bigrams"`
}

// strip returns the Project Gutenberg book text with the prefix and suffix
// boilerplate removed. Returns error if prefix or suffix not found.
func strip(s string) (string, error) {
	const startPrefix = "*** START OF THE PROJECT GUTENBERG EBOOK"
	const endPrefix = "*** END OF THE PROJECT GUTENBERG EBOOK"

	start := strings.Index(s, startPrefix)
	if start == -1 {
		return "", fmt.Errorf("start marker not found")
	}
	startLineEnd := strings.IndexByte(s[start:], '\n')
	if startLineEnd == -1 {
		return "", fmt.Errorf("start marker line not terminated")
	}
	start += startLineEnd + 1

	end := strings.LastIndex(s, endPrefix)
	if end == -1 {
		return "", fmt.Errorf("end marker not found")
	}

	return strings.TrimSpace(s[start:end]), nil
}

// words returns a string iterator for yielding each word in s.
//
// Discards any token containing anything other than the letters a-z.
// This is because I am comparing it to the Norvig frequency count data, which
// does the same (see: https://www.norvig.com/mayzner.html)
func words(s string) iter.Seq[string] {
	return func(yield func(string) bool) {
		start := -1

		flush := func(end int) bool {
			if start != -1 {
				if !yield(strings.ToLower(s[start:end])) {
					return false
				}
			}
			start = -1
			return true
		}

		for i := 0; i < len(s); i++ {
			b := s[i]
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}

			if b >= 'a' && b <= 'z' {
				if start == -1 {
					start = i
				}
				continue
			}

			if !flush(i) {
				return
			}
		}
		_ = flush(len(s))
	}
}

// add adds the counts of letters and bigrams to the Counts pointer.
func (c *Counts) add(words iter.Seq[string]) {
	for w := range words {
		for i := 0; i < len(w); i++ {
			c.Letters[string(w[i])]++
		}
		for i := 0; i+1 < len(w); i++ {
			c.Bigrams[w[i:i+2]]++
		}
	}
}

// Count strips the Gutenberg boilerplate from each text, tokenises it, and
// returns the combined letter and bigram counts.
func Count(texts [][]byte) (Counts, error) {
	c := Counts{
		Letters: make(map[string]int),
		Bigrams: make(map[string]int),
	}

	for _, text := range texts {
		body, err := strip(string(text))

		if err != nil {
			return Counts{}, fmt.Errorf("stripping prefix and suffix from text: %w", err)
		}
		c.add(words(body))
	}

	return c, nil
}

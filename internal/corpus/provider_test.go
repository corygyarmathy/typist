package corpus

import (
	"math"
	"testing"
)

func TestNew(t *testing.T) {

	p, err := New()

	if err != nil {
		t.Fatal(err)
	}

	if len(p.KeyOrder()) != 26 {
		t.Errorf("expected 26 keys, got: %d", len(p.KeyOrder()))
	}

	cands := p.Transitions("t")

	if len(cands) == 0 {
		t.Errorf("expected transitions graph for 't', got none.")
	}

	var sum float64
	for _, c := range cands {
		sum += c.Freq
	}

	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("expected frequencies of 't' to sum to ~1.0, got: %v", sum)
	}
}

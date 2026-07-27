package engine

import "testing"

// The engine is the most heavily-tested package in the codebase
// because it contains the genuinely interesting domain logic and it's
// trivially testable (pure functions).
//
// Test categories to build out:
//
//   1. Selection invariants
//      - Generated lessons only contain unlocked keys
//      - Weak keys appear more often than strong keys
//      - New keys are introduced when all current keys exceed threshold
//
//   2. Scoring invariants
//      - Accuracy is weighted more heavily than speed
//      - A perfect-accuracy slow run scores higher than fast-with-errors
//      - Scores decay over time (revisit weak items)
//
//   3. End-to-end progression
//      - Simulated user with consistent accuracy unlocks all 26 keys in
//        a bounded number of lessons
//      - Engine transitions from key-focus to ngram-focus around the
//        expected competency threshold
//
// Use testing/quick or property-based testing for the invariants.

func TestEngine_Placeholder(t *testing.T) {
	t.Skip("phase 3")
}

package adaptive

import (
	"math"
	"testing"
	"time"
)

func TestScoring_NewItemScoreEqualsInstant(t *testing.T) {
	now := time.Now()
	zeroValue := ItemScore{Score: 0, Samples: 0, LastPracticed: now}
	o := Observation{Attempts: 10, Errors: 1, TotalMillis: 4000} // 30 WPM
	targetWPM := 40

	newItemScore, ok := updateScore(zeroValue, o, targetWPM, now)
	if !ok {
		t.Fatalf("updateScore() reported not ok, provided impossible inputs: %v & %v", o, targetWPM)
	}

	instantScore, ok := instant(o, targetWPM)
	if !ok {
		t.Fatalf("instant() reported not ok, provided impossible inputs: %v & %v", o, targetWPM)
	}
	if newItemScore.Score != instantScore {
		t.Errorf(
			`New item's score unexpectedly does not match the instant score:
			%v (new item) != %v (instant)`,
			newItemScore, instantScore,
		)
	}
}

func TestScoring_SlowAccuracyBeatsFastInaccuracy(t *testing.T) {
	targetWPM := 40
	slowAccurate := Observation{Attempts: 10, Errors: 1, TotalMillis: 4000}   // 30 WPM
	fastInaccurate := Observation{Attempts: 10, Errors: 3, TotalMillis: 2000} // 60 WPM

	slowScore, ok := instant(slowAccurate, targetWPM)
	if !ok {
		t.Fatalf("instant() reported not ok, provided impossible inputs: %v & %v",
			slowAccurate, targetWPM)
	}

	fastScore, ok := instant(fastInaccurate, targetWPM)
	if !ok {
		t.Fatalf("instant() reported not ok, provided impossible inputs: %v & %v",
			fastInaccurate, targetWPM)
	}

	if slowScore <= fastScore {
		t.Errorf(
			`Slow Accurate obs. was unexpectedly scored lower than Fast Inaccurate obs.:
			%v (slow accurate) != %v (fast inaccurate)`,
			slowScore, fastScore,
		)
	}
}

func TestScoring_StalerItemDecaysLower(t *testing.T) {
	now := time.Now()
	old := now.AddDate(0, 0, -30) //Y,M,D

	fresh := ItemScore{Score: 1, Samples: 10, LastPracticed: now}
	stale := ItemScore{Score: 1, Samples: 10, LastPracticed: old}
	freshScore := decayedScore(fresh, now)
	staleScore := decayedScore(stale, now)

	if freshScore < 0 || staleScore < 0 {
		t.Fatalf(
			"decayedScore() unexpectedly returned negative value. freshScore: %v, staleScore: %v",
			freshScore, staleScore,
		)
	}
	if freshScore == 0 || staleScore == 0 {
		t.Fatalf(
			"decayedScore() unexpectedly returned 0 value. freshScore: %v, staleScore: %v",
			freshScore, staleScore,
		)
	}
	if freshScore <= staleScore {
		t.Errorf(
			`decayedScore() unexpectedly returned fresh less than or equal to stale. 
		freshScore: %v, staleScore: %v`,
			freshScore, staleScore,
		)
	}

	tolerance := 0.1
	if math.Abs(fresh.Score-freshScore) > tolerance {
		t.Errorf(
			"decayedScore() unexpectedly decayed freshScore. fresh.Score: %v, freshScore: %v, tolerance: %v",
			fresh.Score, freshScore, tolerance,
		)
	}
}

func TestScoring_ImpossibleInputsNotOk(t *testing.T) {
	now := time.Now()
	prevScore := ItemScore{Score: 0.5, Samples: 10, LastPracticed: now}

	tests := []struct {
		name      string
		o         Observation
		targetWPM int
		wantOk    bool
	}{
		{
			"valid",
			Observation{
				Attempts:    10,
				Errors:      1,
				TotalMillis: 1000,
			},
			10,
			true,
		},
		{
			"impossible attempts",
			Observation{
				Attempts:    0,
				Errors:      1,
				TotalMillis: 1000,
			},
			10,
			false,
		},
		{
			"impossible TotalMillis",
			Observation{
				Attempts:    10,
				Errors:      1,
				TotalMillis: 0,
			},
			10,
			false,
		},
		{
			"impossible targetWPM",
			Observation{
				Attempts:    10,
				Errors:      1,
				TotalMillis: 1000,
			},
			0,
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := instant(tt.o, tt.targetWPM)
			if ok != tt.wantOk {
				t.Errorf("instant: ok = %v, want %v", ok, tt.wantOk)
			}
			updatedScore, ok := updateScore(prevScore, tt.o, tt.targetWPM, now)
			if ok != tt.wantOk {
				t.Errorf("updateScore: ok = %v, want %v", ok, tt.wantOk)
			}
			if !tt.wantOk && updatedScore != prevScore {
				t.Errorf(
					"expected updateScore to return prev. itemScore. Wanted '%v', got '%v'",
					prevScore, updatedScore,
				)
			}

		})
	}
}

// Package gating turns per-run scores into case statuses and a suite decision.
// All functions are pure so the same scores and settings always give the same result.
package gating

import "math"

// Status is the outcome for one case.
type Status string

// Case statuses.
const (
	StatusPass       Status = "PASS"
	StatusRegressed  Status = "REGRESSED"
	StatusUnreliable Status = "UNRELIABLE"
	StatusError      Status = "ERROR"
	// StatusNew marks a case of a suite with no baseline: candidate-only, never gated.
	StatusNew Status = "NEW"
)

// Decision is the overall suite outcome.
type Decision string

// Suite decisions.
const (
	DecisionPass  Decision = "PASS"
	DecisionWarn  Decision = "WARN"
	DecisionFail  Decision = "FAIL"
	DecisionError Decision = "ERROR"
)

// Side summarizes the runs of one variant (baseline or candidate) of a case.
type Side struct {
	// Runs is the number of planned runs.
	Runs int
	// FailedRuns is the number of runs that errored and carry no score.
	FailedRuns int
	// Scores holds the 0-100 score of every successful run.
	Scores []float64
	// Average is the mean of Scores (0 when there are none).
	Average float64
	// Spread is max(Scores) - min(Scores) (0 when there are none or one).
	Spread float64
}

// NewSide builds a Side from planned runs, the failed-run count, and successful scores.
func NewSide(planned, failed int, scores []float64) Side {
	s := Side{Runs: planned, FailedRuns: failed, Scores: append([]float64(nil), scores...)}
	if len(scores) == 0 {
		return s
	}
	sum, lo, hi := 0.0, math.Inf(1), math.Inf(-1)
	for _, v := range scores {
		sum += v
		lo = math.Min(lo, v)
		hi = math.Max(hi, v)
	}
	s.Average = sum / float64(len(scores))
	s.Spread = hi - lo
	return s
}

// Successful returns the number of runs that produced a score.
func (s Side) Successful() int { return len(s.Scores) }

// CaseResult is the gated outcome of one case.
type CaseResult struct {
	Suite     string
	Case      string
	Critical  bool
	Baseline  Side
	Candidate Side
	// Drop is baseline average minus candidate average (0 when either side has no data).
	Drop   float64
	Status Status
}

// Settings are the thresholds used for gating.
type Settings struct {
	// MinDropThreshold is the largest tolerated average drop, in score points.
	MinDropThreshold float64
	// MaxSpread is the largest tolerated max-min spread on either side, in score points.
	MaxSpread float64
}

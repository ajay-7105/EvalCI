// Package engine runs eval suites against baseline and candidate variants and gates the results.
package engine

import (
	"context"
	"time"
)

// Variant names the side of a comparison.
type Variant string

// Variants.
const (
	Baseline  Variant = "baseline"
	Candidate Variant = "candidate"
)

// CallInfo describes one finished provider call.
type CallInfo struct {
	Suite    string
	Case     string
	Variant  Variant
	Run      int
	Kind     string // "complete" or "embed"
	Provider string
	Model    string
	Duration time.Duration
	Err      error
}

// RunFailure describes one run that produced no score.
type RunFailure struct {
	Suite   string
	Case    string
	Variant Variant
	Run     int
	Err     error
}

// Observer receives diagnostics from the engine. Implementations must be safe for
// concurrent use.
type Observer interface {
	// CallFinished is invoked after every provider call.
	CallFinished(CallInfo)
	// RunFailed is invoked for every run that ended without a score.
	RunFailed(RunFailure)
}

// NopObserver discards all diagnostics.
type NopObserver struct{}

// CallFinished implements Observer.
func (NopObserver) CallFinished(CallInfo) {}

// RunFailed implements Observer.
func (NopObserver) RunFailed(RunFailure) {}

type runInfoKey struct{}

// runInfo is the per-run context attached to provider calls for observer reporting.
type runInfo struct {
	suite, caseID string
	variant       Variant
	run           int
}

func withRunInfo(ctx context.Context, ri runInfo) context.Context {
	return context.WithValue(ctx, runInfoKey{}, ri)
}

func runInfoFrom(ctx context.Context) runInfo {
	ri, _ := ctx.Value(runInfoKey{}).(runInfo)
	return ri
}

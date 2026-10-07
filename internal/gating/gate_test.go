package gating

import (
	"reflect"
	"testing"
)

var cfg = Settings{MinDropThreshold: 5, MaxSpread: 20}

func side(planned int, scores ...float64) Side {
	return NewSide(planned, planned-len(scores), scores)
}

func TestNewSide(t *testing.T) {
	s := NewSide(5, 1, []float64{80, 90, 100, 70})
	if s.Average != 85 || s.Spread != 30 || s.Successful() != 4 || s.FailedRuns != 1 || s.Runs != 5 {
		t.Fatalf("unexpected side: %+v", s)
	}
	empty := NewSide(3, 3, nil)
	if empty.Average != 0 || empty.Spread != 0 || empty.Successful() != 0 {
		t.Fatalf("unexpected empty side: %+v", empty)
	}
	one := NewSide(1, 0, []float64{42})
	if one.Spread != 0 || one.Average != 42 {
		t.Fatalf("single-run side must have spread 0: %+v", one)
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		base Side
		cand Side
		want Status
	}{
		{"equal passes", side(5, 90, 90, 90, 90, 90), side(5, 90, 90, 90, 90, 90), StatusPass},
		{"drop over threshold regresses", side(1, 90), side(1, 80), StatusRegressed},
		{"drop exactly at threshold passes (strict)", side(1, 90), side(1, 85), StatusPass},
		{"drop just over threshold regresses", side(1, 90), side(1, 84.9), StatusRegressed},
		{"candidate higher never regresses", side(1, 50), side(1, 100), StatusPass},
		{"averages used", side(2, 100, 80), side(2, 90, 70), StatusRegressed},
		{"baseline spread over limit is unreliable", side(2, 100, 70), side(2, 90, 90), StatusUnreliable},
		{"candidate spread over limit is unreliable", side(2, 90, 90), side(2, 100, 70), StatusUnreliable},
		{"spread exactly at limit is reliable", side(2, 100, 80), side(2, 100, 80), StatusPass},
		{"unreliable wins over regressed", side(2, 100, 75), side(2, 20, 20), StatusUnreliable},
		{"single run never unreliable by spread", side(1, 100), side(1, 0), StatusRegressed},
		{"baseline all failed is error", side(3), side(3, 90, 90, 90), StatusError},
		{"candidate all failed is error", side(3, 90, 90, 90), side(3), StatusError},
		{"both all failed is error", side(3), side(3), StatusError},
		{"under half succeeded is unreliable", side(5, 90, 90), side(5, 90, 90, 90, 90, 90), StatusUnreliable},
		{"exactly half succeeded is reliable", side(4, 90, 90), side(4, 90, 90), StatusPass},
		{"under half on candidate with big drop is unreliable", side(5, 90, 90, 90, 90, 90), side(5, 10, 10), StatusUnreliable},
		{"single planned run failed is error", side(1), side(1, 90), StatusError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.base, tt.cand, cfg); got != tt.want {
				t.Fatalf("Classify = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestEvaluateSetsFields(t *testing.T) {
	r := Evaluate("s", "c", false, side(1, 90), side(1, 70), cfg)
	if r.Suite != "s" || r.Case != "c" || r.Critical || r.Status != StatusRegressed || r.Drop != 20 {
		t.Fatalf("unexpected result: %+v", r)
	}
	e := Evaluate("s", "c", true, side(2), side(2, 90, 90), cfg)
	if e.Status != StatusError || e.Drop != 0 {
		t.Fatalf("error case must have zero drop: %+v", e)
	}
}

func rs(status Status, critical bool) CaseResult {
	return CaseResult{Status: status, Critical: critical}
}

func TestDecide(t *testing.T) {
	tests := []struct {
		name    string
		results []CaseResult
		want    Decision
	}{
		{"empty passes", nil, DecisionPass},
		{"all pass", []CaseResult{rs(StatusPass, true), rs(StatusPass, true)}, DecisionPass},
		{"critical regression fails", []CaseResult{rs(StatusPass, true), rs(StatusRegressed, true)}, DecisionFail},
		{"non-critical regression alone passes", []CaseResult{rs(StatusRegressed, false)}, DecisionPass},
		{"non-critical regression plus unreliable warns", []CaseResult{rs(StatusRegressed, false), rs(StatusUnreliable, true)}, DecisionWarn},
		{"unreliable warns", []CaseResult{rs(StatusPass, true), rs(StatusUnreliable, true)}, DecisionWarn},
		{"non-critical unreliable still warns", []CaseResult{rs(StatusUnreliable, false)}, DecisionWarn},
		{"error alone is error", []CaseResult{rs(StatusPass, true), rs(StatusError, true)}, DecisionError},
		{"critical regression plus error fails", []CaseResult{rs(StatusRegressed, true), rs(StatusError, true)}, DecisionFail},
		{"non-critical regression plus error is error", []CaseResult{rs(StatusRegressed, false), rs(StatusError, true)}, DecisionError},
		{"error beats unreliable", []CaseResult{rs(StatusUnreliable, true), rs(StatusError, true)}, DecisionError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Decide(tt.results); got != tt.want {
				t.Fatalf("Decide = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestDeterministic(t *testing.T) {
	base, cand := side(3, 90, 85, 95), side(3, 70, 72, 68)
	first := Evaluate("s", "c", true, base, cand, cfg)
	for i := 0; i < 100; i++ {
		if got := Evaluate("s", "c", true, base, cand, cfg); !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differs: %+v vs %+v", i, got, first)
		}
	}
}

func TestEvaluateNew(t *testing.T) {
	ok := EvaluateNew("s", "c", true, side(3, 90, 80, 70))
	if ok.Status != StatusNew || ok.Drop != 0 || ok.Baseline.Runs != 0 || ok.Baseline.Successful() != 0 || ok.Candidate.Average != 80 {
		t.Fatalf("unexpected: %+v", ok)
	}
	partial := EvaluateNew("s", "c", true, side(5, 90))
	if partial.Status != StatusNew {
		t.Fatalf("partial candidate failures must stay NEW: %+v", partial)
	}
	bad := EvaluateNew("s", "c", false, side(3))
	if bad.Status != StatusError || bad.Critical {
		t.Fatalf("all runs failed must be ERROR: %+v", bad)
	}
}

func TestDecideIgnoresNew(t *testing.T) {
	tests := []struct {
		name    string
		results []CaseResult
		want    Decision
	}{
		{"only new passes", []CaseResult{rs(StatusNew, true), rs(StatusNew, false)}, DecisionPass},
		{"new never warns", []CaseResult{rs(StatusNew, true), rs(StatusPass, true)}, DecisionPass},
		{"new plus critical regression fails", []CaseResult{rs(StatusNew, true), rs(StatusRegressed, true)}, DecisionFail},
		{"new plus error is error", []CaseResult{rs(StatusNew, true), rs(StatusError, true)}, DecisionError},
		{"new plus unreliable warns", []CaseResult{rs(StatusNew, true), rs(StatusUnreliable, true)}, DecisionWarn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Decide(tt.results); got != tt.want {
				t.Fatalf("Decide = %s, want %s", got, tt.want)
			}
		})
	}
}

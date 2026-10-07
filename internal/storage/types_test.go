package storage

import (
	"strings"
	"testing"
	"time"
)

func valid() RunRecord {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := 1.5
	c := int64(7)
	return RunRecord{
		ID: "r", SchemaVersion: RecordVersion, StartedAt: t0, FinishedAt: t0.Add(time.Second), HeadSHA: "abc",
		Mode: ModeScoped, Decision: DecisionPass, ExitCode: 0, TotalCostMicros: &c,
		Suites: []SuiteDecision{{Suite: "s", File: "evals/s.eval.yaml", Action: ActionRun, Reason: "x"}, {Suite: "k", Action: ActionSkip}},
		Cases:  []CaseRecord{{Suite: "s", Case: "c", Status: StatusPass, BaselineAverage: &f}},
		Usage:  []UsageRecord{{Provider: "p", Model: "m", CostMicros: &c}},
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*RunRecord)
		wantErr string
	}{
		{"valid", func(*RunRecord) {}, ""},
		{"empty id", func(r *RunRecord) { r.ID = "" }, "empty ID"},
		{"empty head", func(r *RunRecord) { r.HeadSHA = "" }, "head SHA"},
		{"bad mode", func(r *RunRecord) { r.Mode = "weird" }, "mode"},
		{"bad decision", func(r *RunRecord) { r.Decision = "MAYBE" }, "decision"},
		{"pass with exit 1", func(r *RunRecord) { r.ExitCode = 1 }, "exit code"},
		{"warn exit 0", func(r *RunRecord) { r.Decision = DecisionWarn }, ""},
		{"fail exit 1", func(r *RunRecord) { r.Decision, r.ExitCode = DecisionFail, 1 }, ""},
		{"error exit 2", func(r *RunRecord) { r.Decision, r.ExitCode = DecisionError, 2 }, ""},
		{"error exit 1", func(r *RunRecord) { r.Decision, r.ExitCode = DecisionError, 1 }, "exit code"},
		{"finished before start", func(r *RunRecord) { r.FinishedAt = r.StartedAt.Add(-time.Second) }, "finished before"},
		{"bad suite action", func(r *RunRecord) { r.Suites[0].Action = "maybe" }, "action"},
		{"duplicate suite", func(r *RunRecord) { r.Suites = append(r.Suites, r.Suites[0]) }, "duplicate suite"},
		{"bad case status", func(r *RunRecord) { r.Cases[0].Status = "OK" }, "status"},
		{"case of skipped suite", func(r *RunRecord) { r.Cases[0].Suite = "k" }, "did not run"},
		{"case of unknown suite", func(r *RunRecord) { r.Cases[0].Suite = "zzz" }, "did not run"},
		{"duplicate case", func(r *RunRecord) { r.Cases = append(r.Cases, r.Cases[0]) }, "duplicate case"},
		{"duplicate usage", func(r *RunRecord) { r.Usage = append(r.Usage, r.Usage[0]) }, "duplicate usage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := valid()
			tt.mutate(&r)
			err := r.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestCloneIsDeepAndNormalizesTime(t *testing.T) {
	r := valid()
	r.StartedAt = time.Date(2026, 1, 1, 10, 0, 0, 0, time.FixedZone("x", 3600))
	c := r.Clone()
	if c.StartedAt.Location() != time.UTC {
		t.Fatalf("clone must be UTC, got %v", c.StartedAt.Location())
	}
	*c.TotalCostMicros = 99
	*c.Cases[0].BaselineAverage = 99
	c.Suites[0].Suite = "tampered"
	if *r.TotalCostMicros != 7 || *r.Cases[0].BaselineAverage != 1.5 || r.Suites[0].Suite != "s" {
		t.Fatal("Clone must not share memory with the original")
	}
	var nilRec RunRecord
	if got := nilRec.Clone(); got.TotalCostMicros != nil || got.Cases == nil && false {
		t.Fatal("nil pointers must stay nil")
	}
}

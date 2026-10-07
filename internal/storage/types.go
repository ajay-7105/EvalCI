package storage

import (
	"fmt"
	"time"
)

// RecordVersion is the version of the run record format.
const RecordVersion = 1

// UnknownCommit is the head identifier recorded when the run is not inside a git repository.
const UnknownCommit = "unknown"

// Mode is how a run chose its suites.
type Mode string

// Run modes.
const (
	ModeScoped      Mode = "scoped"
	ModeFull        Mode = "full"
	ModeBaselineDir Mode = "baseline-dir"
)

// Decision is the gate decision of a run.
type Decision string

// Gate decisions.
const (
	DecisionPass  Decision = "PASS"
	DecisionWarn  Decision = "WARN"
	DecisionFail  Decision = "FAIL"
	DecisionError Decision = "ERROR"
)

// Suite actions in a SuiteDecision.
const (
	ActionRun     = "run"
	ActionSkip    = "skip"
	ActionRemoved = "removed"
)

// Case statuses in a CaseRecord.
const (
	StatusPass       = "PASS"
	StatusRegressed  = "REGRESSED"
	StatusUnreliable = "UNRELIABLE"
	StatusError      = "ERROR"
	StatusNew        = "NEW"
)

// Thresholds are the gating thresholds in effect for a run.
type Thresholds struct {
	RunsPerCase               int
	MinDropThreshold          float64
	MaxSpreadBeforeUnreliable float64
}

// SuiteDecision records whether a suite ran, was skipped, or was removed, and why.
type SuiteDecision struct {
	Suite  string
	File   string
	Action string
	Reason string
}

// CaseRecord is the result of one case in a run. Averages, spreads, and Delta are nil when
// there is no data to compute them; there is deliberately no field for responses, prompts,
// or case inputs.
type CaseRecord struct {
	Suite            string
	Case             string
	Critical         bool
	Status           string
	BaselineRuns     int
	BaselineFailed   int
	BaselineAverage  *float64
	BaselineSpread   *float64
	CandidateRuns    int
	CandidateFailed  int
	CandidateAverage *float64
	CandidateSpread  *float64
	// Delta is baseline average minus candidate average.
	Delta *float64
}

// UsageRecord is token usage and estimated cost for one provider and model in a run.
// CostMicros is nil when the cost is unknown (never zero by default).
type UsageRecord struct {
	Provider     string
	Model        string
	Calls        int64
	InputTokens  int64
	OutputTokens int64
	UnknownCalls int64
	CostMicros   *int64
}

// RunRecord is one finished gate run.
type RunRecord struct {
	ID            string
	SchemaVersion int
	StartedAt     time.Time
	FinishedAt    time.Time
	HeadSHA       string
	HeadBranch    string
	BaseRef       string
	BaseSHA       string
	Mode          Mode
	Decision      Decision
	ExitCode      int
	Thresholds    Thresholds
	Currency      string

	TotalInputTokens  int64
	TotalOutputTokens int64
	// TotalCostMicros is nil when no cost is known.
	TotalCostMicros *int64
	// CostPartial is true when some model's cost is unknown.
	CostPartial bool

	Suites []SuiteDecision
	Cases  []CaseRecord
	Usage  []UsageRecord
}

// Validate checks the invariants every store enforces on save.
func (r RunRecord) Validate() error {
	if r.ID == "" {
		return fmt.Errorf("invalid run record: empty ID")
	}
	if r.HeadSHA == "" {
		return fmt.Errorf("invalid run record %s: empty head SHA", r.ID)
	}
	switch r.Mode {
	case ModeScoped, ModeFull, ModeBaselineDir:
	default:
		return fmt.Errorf("invalid run record %s: unknown mode %q", r.ID, r.Mode)
	}
	want, ok := map[Decision]int{DecisionPass: 0, DecisionWarn: 0, DecisionFail: 1, DecisionError: 2}[r.Decision]
	if !ok {
		return fmt.Errorf("invalid run record %s: unknown decision %q", r.ID, r.Decision)
	}
	if r.ExitCode != want {
		return fmt.Errorf("invalid run record %s: exit code %d does not match decision %s", r.ID, r.ExitCode, r.Decision)
	}
	if r.FinishedAt.Before(r.StartedAt) {
		return fmt.Errorf("invalid run record %s: finished before it started", r.ID)
	}
	ran := map[string]bool{}
	seenSuite := map[string]bool{}
	for _, s := range r.Suites {
		switch s.Action {
		case ActionRun, ActionSkip, ActionRemoved:
		default:
			return fmt.Errorf("invalid run record %s: suite %q has unknown action %q", r.ID, s.Suite, s.Action)
		}
		if seenSuite[s.Suite] {
			return fmt.Errorf("invalid run record %s: duplicate suite %q", r.ID, s.Suite)
		}
		seenSuite[s.Suite] = true
		if s.Action == ActionRun {
			ran[s.Suite] = true
		}
	}
	seenCase := map[[2]string]bool{}
	for _, c := range r.Cases {
		switch c.Status {
		case StatusPass, StatusRegressed, StatusUnreliable, StatusError, StatusNew:
		default:
			return fmt.Errorf("invalid run record %s: case %s/%s has unknown status %q", r.ID, c.Suite, c.Case, c.Status)
		}
		if !ran[c.Suite] {
			return fmt.Errorf("invalid run record %s: case %s/%s belongs to a suite that did not run", r.ID, c.Suite, c.Case)
		}
		k := [2]string{c.Suite, c.Case}
		if seenCase[k] {
			return fmt.Errorf("invalid run record %s: duplicate case %s/%s", r.ID, c.Suite, c.Case)
		}
		seenCase[k] = true
	}
	seenUsage := map[[2]string]bool{}
	for _, u := range r.Usage {
		k := [2]string{u.Provider, u.Model}
		if seenUsage[k] {
			return fmt.Errorf("invalid run record %s: duplicate usage for %s/%s", r.ID, u.Provider, u.Model)
		}
		seenUsage[k] = true
	}
	return nil
}

// Clone returns a deep copy with UTC, monotonic-free times, so stores can hand out records
// callers may mutate freely.
func (r RunRecord) Clone() RunRecord {
	out := r
	out.StartedAt = r.StartedAt.UTC().Round(0)
	out.FinishedAt = r.FinishedAt.UTC().Round(0)
	out.TotalCostMicros = cloneInt(r.TotalCostMicros)
	out.Suites = append([]SuiteDecision(nil), r.Suites...)
	out.Cases = make([]CaseRecord, len(r.Cases))
	for i, c := range r.Cases {
		c.BaselineAverage = cloneFloat(c.BaselineAverage)
		c.BaselineSpread = cloneFloat(c.BaselineSpread)
		c.CandidateAverage = cloneFloat(c.CandidateAverage)
		c.CandidateSpread = cloneFloat(c.CandidateSpread)
		c.Delta = cloneFloat(c.Delta)
		out.Cases[i] = c
	}
	out.Usage = make([]UsageRecord, len(r.Usage))
	for i, u := range r.Usage {
		u.CostMicros = cloneInt(u.CostMicros)
		out.Usage[i] = u
	}
	return out
}

func cloneInt(p *int64) *int64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneFloat(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

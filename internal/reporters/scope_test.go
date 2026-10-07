package reporters

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ajay-7105/EvalCI/internal/cost"
	"github.com/ajay-7105/EvalCI/internal/diffscope"
	"github.com/ajay-7105/EvalCI/internal/gating"
)

func TestWriteScopeGolden(t *testing.T) {
	var buf bytes.Buffer
	err := WriteScope(&buf, ScopeSummary{
		BaseRef: "main", BaseSHA: "a1b2c3d4e5f6", HeadRef: "HEAD", HeadSHA: "d4e5f6a7b8c9", HeadName: "feature/x",
		Decisions: []diffscope.ScopeDecision{
			{Suite: "support", File: "evals/support.eval.yaml", Action: "run", Reason: diffscope.ReasonPromptChange, Files: []string{"prompts/support.txt"}},
			{Suite: "billing", File: "evals/billing.eval.yaml", Action: "skip", Reason: diffscope.ReasonNoChanges},
			{Suite: "fresh", File: "evals/fresh.eval.yaml", Action: "run", Reason: diffscope.ReasonNew, Files: []string{"evals/fresh.eval.yaml"}},
			{Suite: "legacy", File: "evals/legacy.eval.yaml", Action: "removed", Reason: diffscope.ReasonRemoved},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `Scope: base main (a1b2c3d) -> head feature/x (d4e5f6a); 2 of 3 suites selected
  support  RUN      prompt file changed: prompts/support.txt
  billing  SKIP     no related changes
  fresh    RUN      new suite
  legacy   REMOVED  suite removed

`
	if buf.String() != want {
		t.Fatalf("scope mismatch.\n got:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestWriteScopeFullAndHeadFallback(t *testing.T) {
	var buf bytes.Buffer
	_ = WriteScope(&buf, ScopeSummary{
		Full: true, BaseRef: "main", BaseSHA: "aaaaaaaa", HeadRef: "HEAD", HeadSHA: "bbbbbbbb",
		Decisions: []diffscope.ScopeDecision{
			{Suite: "a", Action: "run", Reason: diffscope.ReasonFull},
			{Suite: "b", Action: "run", Reason: diffscope.ReasonFull},
			{Suite: "c", Action: "run", Reason: diffscope.ReasonFull},
		},
	})
	first := strings.SplitN(buf.String(), "\n", 2)[0]
	if first != "Scope: base main (aaaaaaa) -> head HEAD (bbbbbbb); full run requested; 3 of 3 suites selected" {
		t.Fatalf("header = %q", first)
	}
}

func TestWriteNothingAffectedAndWriteErrors(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteNothingAffected(&buf); err != nil || buf.String() != "Decision: PASS (no suites affected)\n" {
		t.Fatalf("%q %v", buf.String(), err)
	}
	if err := WriteNothingAffected(failWriter{}); err == nil {
		t.Fatal("write error must propagate")
	}
	if err := WriteScope(failWriter{}, ScopeSummary{}); err == nil {
		t.Fatal("write error must propagate")
	}
}

func TestFormatMicrosAndCostLine(t *testing.T) {
	tests := map[int64]string{0: "0.00", 12300: "0.0123", 750000: "0.75", 255050: "0.25505", 1: "0.000001", 1_000_000: "1.00", 12_345_678: "12.345678", -5000: "-0.005"}
	for in, want := range tests {
		if got := FormatMicros(in); got != want {
			t.Errorf("FormatMicros(%d) = %q, want %q", in, got, want)
		}
	}
	if commas(0) != "0" || commas(999) != "999" || commas(1000) != "1,000" || commas(12400) != "12,400" || commas(1234567) != "1,234,567" {
		t.Fatal("commas")
	}
}

func TestWriteCostLine(t *testing.T) {
	i := func(v int64) *int64 { return &v }
	priced := cost.ModelCost{Usage: cost.Usage{Provider: "o", Model: "m", Calls: 1, InputTokens: 12400, OutputTokens: 3100}, CostMicros: i(12300)}
	unpriced := cost.ModelCost{Usage: cost.Usage{Provider: "p", Model: "n", Calls: 1, InputTokens: 5}}
	tests := []struct {
		name string
		s    cost.Summary
		want string
	}{
		{"known", cost.Summary{Models: []cost.ModelCost{priced}, TotalInput: 12400, TotalOutput: 3100, TotalCostMicros: i(12300)},
			"Cost: 12,400 input / 3,100 output tokens, est. 0.0123 EUR\n"},
		{"partial", cost.Summary{Models: []cost.ModelCost{priced, unpriced}, TotalInput: 12405, TotalOutput: 3100, TotalCostMicros: i(12300), Partial: true},
			"Cost: 12,405 input / 3,100 output tokens, est. 0.0123 EUR (cost unknown for 1 model)\n"},
		{"all unknown", cost.Summary{Models: []cost.ModelCost{unpriced, unpriced}, TotalInput: 10, Partial: true},
			"Cost: 10 input / 0 output tokens, est. unknown (cost unknown for 2 models)\n"},
		{"nothing ran prints nothing", cost.Summary{TotalCostMicros: i(0)}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteCost(&buf, tt.s, "EUR"); err != nil {
				t.Fatal(err)
			}
			if buf.String() != tt.want {
				t.Fatalf("got %q, want %q", buf.String(), tt.want)
			}
		})
	}
	if err := WriteCost(failWriter{}, cost.Summary{Models: []cost.ModelCost{priced}}, "USD"); err == nil {
		t.Fatal("write error must propagate")
	}
}

func TestTableShowsNewCases(t *testing.T) {
	results := []gating.CaseResult{
		{Suite: "fresh", Case: "c1", Critical: true, Baseline: sd(0), Candidate: sd(3, 90, 90, 90), Status: gating.StatusNew},
	}
	var buf bytes.Buffer
	if err := WriteTable(&buf, results, gating.DecisionPass); err != nil {
		t.Fatal(err)
	}
	want := `SUITE  CASE  BASELINE  CANDIDATE  DROP  STATUS
fresh  c1    -         90.0       -     NEW

Decision: PASS (1 cases)
`
	if buf.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
}

package reporters

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ajay-7105/EvalCI/internal/engine"
	"github.com/ajay-7105/EvalCI/internal/gating"
)

func sd(planned int, scores ...float64) gating.Side {
	return gating.NewSide(planned, planned-len(scores), scores)
}

func TestExitCode(t *testing.T) {
	tests := map[gating.Decision]int{
		gating.DecisionPass: 0, gating.DecisionWarn: 0, gating.DecisionFail: 1, gating.DecisionError: 2,
	}
	for d, want := range tests {
		if got := ExitCode(d); got != want {
			t.Errorf("ExitCode(%s) = %d, want %d", d, got, want)
		}
	}
}

func TestWriteTableGolden(t *testing.T) {
	results := []gating.CaseResult{
		{Suite: "support", Case: "refund-1", Critical: true, Baseline: sd(1, 92), Candidate: sd(1, 90), Drop: 2, Status: gating.StatusPass},
		{Suite: "support", Case: "tone-2", Critical: true, Baseline: sd(1, 88), Candidate: sd(1, 71), Drop: 17, Status: gating.StatusRegressed},
		{Suite: "support", Case: "edge-3", Critical: true, Baseline: sd(2, 80, 80), Candidate: sd(2, 80, 40), Drop: 20, Status: gating.StatusUnreliable},
	}
	var buf bytes.Buffer
	if err := WriteTable(&buf, results, gating.DecisionFail); err != nil {
		t.Fatal(err)
	}
	want := `SUITE    CASE      BASELINE  CANDIDATE  DROP  STATUS
support  refund-1  92.0      90.0       2.0   PASS
support  tone-2    88.0      71.0       17.0  REGRESSED
support  edge-3    80.0      60.0       20.0  UNRELIABLE

Decision: FAIL (1 regressed, 1 unreliable)
`
	if buf.String() != want {
		t.Fatalf("table mismatch.\n got:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestWriteTableNonCriticalAndFailedRunsAndError(t *testing.T) {
	results := []gating.CaseResult{
		{Suite: "s", Case: "flaky", Critical: false, Baseline: sd(1, 90), Candidate: sd(1, 50), Drop: 40, Status: gating.StatusRegressed},
		{Suite: "s", Case: "partial", Critical: true, Baseline: sd(5, 90, 90, 90, 90), Candidate: sd(5, 90, 90, 90, 90, 90), Drop: 0, Status: gating.StatusPass},
		{Suite: "s", Case: "dead", Critical: true, Baseline: sd(3), Candidate: sd(3, 90, 90, 90), Status: gating.StatusError},
	}
	var buf bytes.Buffer
	if err := WriteTable(&buf, results, gating.DecisionError); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"REGRESSED (non-critical)",
		"90.0 !1",
		"-  ", // missing baseline average
		"Decision: ERROR (1 regressed, 1 error)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "PASS (non-critical)") {
		t.Errorf("critical cases must not carry the suffix:\n%s", out)
	}
	deadLine := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "dead") {
			deadLine = l
		}
	}
	if !strings.Contains(deadLine, "!3") || strings.Count(deadLine, " -") < 2 {
		t.Errorf("ERROR row should show dashes and failed count: %q", deadLine)
	}
}

func TestWriteTableAllPassSummary(t *testing.T) {
	var buf bytes.Buffer
	results := []gating.CaseResult{{Suite: "s", Case: "a", Critical: true, Baseline: sd(1, 90), Candidate: sd(1, 90), Status: gating.StatusPass}}
	if err := WriteTable(&buf, results, gating.DecisionPass); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Decision: PASS (1 cases)") {
		t.Fatalf("unexpected: %s", buf.String())
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestWriteTablePropagatesWriteErrors(t *testing.T) {
	results := []gating.CaseResult{{Suite: "s", Case: "a", Critical: true, Baseline: sd(1, 90), Candidate: sd(1, 90), Status: gating.StatusPass}}
	if err := WriteTable(failWriter{}, results, gating.DecisionPass); err == nil {
		t.Fatal("write error must propagate")
	}
}

func TestStderrObserver(t *testing.T) {
	var quiet, loud bytes.Buffer
	q, l := NewStderr(&quiet, false), NewStderr(&loud, true)
	call := engine.CallInfo{Suite: "s", Case: "c", Variant: engine.Baseline, Run: 2, Kind: "complete", Provider: "ollama", Model: "m", Duration: 1500 * time.Millisecond}
	fail := engine.RunFailure{Suite: "s", Case: "c", Variant: engine.Candidate, Run: 1, Err: errors.New("timeout")}
	for _, o := range []*Stderr{q, l} {
		o.CallFinished(call)
		o.RunFailed(fail)
	}
	if strings.Contains(quiet.String(), "call suite") {
		t.Fatalf("non-verbose must not print call lines: %s", quiet.String())
	}
	if !strings.Contains(quiet.String(), "run failed: suite=s case=c variant=candidate run=1: timeout") {
		t.Fatalf("failure reason must always print: %s", quiet.String())
	}
	if !strings.Contains(loud.String(), "call suite=s case=c variant=baseline run=2 kind=complete provider=ollama model=m duration=1500ms ok") {
		t.Fatalf("verbose call line missing: %s", loud.String())
	}
	call.Err = errors.New("boom")
	l.CallFinished(call)
	if !strings.Contains(loud.String(), "error: boom") {
		t.Fatalf("call error missing: %s", loud.String())
	}
}

func TestStderrObserverConcurrent(t *testing.T) {
	var buf bytes.Buffer
	o := NewStderr(&buf, true)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.CallFinished(engine.CallInfo{Suite: "s"})
			o.RunFailed(engine.RunFailure{Suite: "s", Err: errors.New("x")})
		}()
	}
	wg.Wait()
	if got := strings.Count(buf.String(), "\n"); got != 100 {
		t.Fatalf("lines = %d, want 100", got)
	}
}

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ajay-7105/EvalCI/internal/providers"
	"github.com/ajay-7105/EvalCI/internal/providers/fake"
	"github.com/ajay-7105/EvalCI/internal/storage"
	"github.com/ajay-7105/EvalCI/internal/storage/factory"
	"github.com/ajay-7105/EvalCI/internal/storage/memory"
)

func openHistory(t *testing.T, f *fixture) storage.Store {
	t.Helper()
	s, err := factory.Open(context.Background(), storage.Config{Driver: "sqlite", Path: filepath.Join(f.histDir, "history.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestStoredRecordMatchesWhatWasPrinted(t *testing.T) {
	f := newFixture(t)
	f.repo.Write("prompts/support.txt", "OK\n")
	head := f.repo.Commit("tweak support prompt")

	o := f.run(okProvider())
	if o.exit != 0 {
		t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
	runs, err := openHistory(t, f).ListRunsByCommit(context.Background(), head)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs=%d err=%v, want exactly one record for the head commit", len(runs), err)
	}
	r := runs[0]
	if r.HeadSHA != head || r.HeadBranch != "feature" || r.BaseRef != "main" || r.BaseSHA == "" || r.Mode != storage.ModeScoped {
		t.Fatalf("identity fields wrong: %+v", r)
	}
	if r.Decision != storage.DecisionPass || r.ExitCode != 0 || !strings.Contains(o.stdout, "Decision: "+string(r.Decision)) {
		t.Fatalf("decision wrong: %+v", r)
	}
	if r.Thresholds.RunsPerCase != 2 || r.Thresholds.MinDropThreshold != 5 || r.Thresholds.MaxSpreadBeforeUnreliable != 20 || r.Currency != "USD" {
		t.Fatalf("settings wrong: %+v", r)
	}
	if r.FinishedAt.Before(r.StartedAt) || r.StartedAt.Location() != time.UTC {
		t.Fatalf("times wrong: %v %v", r.StartedAt, r.FinishedAt)
	}
	acts := map[string]string{}
	for _, s := range r.Suites {
		acts[s.Suite] = s.Action + ":" + s.Reason
	}
	if len(r.Suites) != 3 || acts["support"] != "run:prompt file changed" || acts["billing"] != "skip:no related changes" || acts["legacy"] != "skip:no related changes" {
		t.Fatalf("suites wrong: %+v", r.Suites)
	}
	if len(r.Cases) != 1 {
		t.Fatalf("cases = %+v, want only the suite that ran", r.Cases)
	}
	c := r.Cases[0]
	if c.Suite != "support" || c.Case != "c1" || !c.Critical || c.Status != "PASS" ||
		c.BaselineAverage == nil || *c.BaselineAverage != 100 || c.CandidateAverage == nil || *c.CandidateAverage != 100 ||
		c.Delta == nil || *c.Delta != 0 || c.BaselineRuns != 1 || c.CandidateRuns != 1 {
		t.Fatalf("case wrong: %+v", c)
	}
	// 2 generation calls (baseline + candidate) x 10 in / 5 out tokens at 1 and 2 per million
	if len(r.Usage) != 1 || r.Usage[0].Provider != "ollama" || r.Usage[0].Model != "m-support" || r.Usage[0].Calls != 2 ||
		r.Usage[0].InputTokens != 20 || r.Usage[0].OutputTokens != 10 || r.Usage[0].CostMicros == nil || *r.Usage[0].CostMicros != 40 {
		t.Fatalf("usage wrong: %+v", r.Usage)
	}
	if r.TotalInputTokens != 20 || r.TotalOutputTokens != 10 || r.TotalCostMicros == nil || *r.TotalCostMicros != 40 || r.CostPartial {
		t.Fatalf("totals wrong: %+v", r)
	}
	if !strings.Contains(o.stdout, "Cost: 20 input / 10 output tokens, est. 0.00 USD") && !strings.Contains(o.stdout, "est. 0.00004 USD") {
		t.Fatalf("cost line missing or wrong:\n%s", o.stdout)
	}
	if id := f.result()["run_id"]; id != r.ID {
		t.Fatalf("JSON run_id = %v, stored id = %s", id, r.ID)
	}
}

func TestEveryDecisionIsRecordedExactlyOnce(t *testing.T) {
	tests := []struct {
		name       string
		prompt     string // support prompt on the feature branch ("" = leave unchanged, docs only)
		provider   func() *fake.Provider
		wantExit   int
		wantStatus storage.Decision
		wantCases  int
	}{
		{"PASS", "OK\n", okProvider, 0, storage.DecisionPass, 1},
		{"FAIL", "WRONG", okProvider, 1, storage.DecisionFail, 1},
		{"ERROR", "OK\n", func() *fake.Provider {
			p := okProvider()
			p.CompleteFn = func(int, providers.Request) (providers.Response, error) {
				return providers.Response{}, errors.New("down")
			}
			return p
		}, 2, storage.DecisionError, 1},
		{"nothing selected", "", okProvider, 0, storage.DecisionPass, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.prompt != "" {
				f.repo.Write("prompts/support.txt", tt.prompt)
			} else {
				f.repo.Write("README.md", "docs")
			}
			head := f.repo.Commit("change")
			o := f.run(tt.provider())
			if o.exit != tt.wantExit {
				t.Fatalf("exit=%d want %d\n%s\n%s", o.exit, tt.wantExit, o.stdout, o.stderr)
			}
			runs, err := openHistory(t, f).ListRunsByCommit(context.Background(), head)
			if err != nil || len(runs) != 1 {
				t.Fatalf("runs=%d err=%v, want exactly 1 (SC-003)", len(runs), err)
			}
			if runs[0].Decision != tt.wantStatus || runs[0].ExitCode != tt.wantExit || len(runs[0].Cases) != tt.wantCases {
				t.Fatalf("record = %+v", runs[0])
			}
			if tt.name == "nothing selected" {
				if runs[0].TotalCostMicros == nil || *runs[0].TotalCostMicros != 0 || len(runs[0].Usage) != 0 || runs[0].CostPartial {
					t.Fatalf("a run with no calls must record a known zero cost: %+v", runs[0])
				}
			}
		})
	}
}

func TestSameCommitTwiceGivesTwoRecords(t *testing.T) {
	f := newFixture(t)
	f.repo.Write("prompts/support.txt", "OK\n")
	head := f.repo.Commit("change")
	for i := 0; i < 2; i++ {
		if o := f.run(okProvider()); o.exit != 0 {
			t.Fatalf("run %d: exit=%d\n%s", i, o.exit, o.stderr)
		}
	}
	runs, err := openHistory(t, f).ListRunsByCommit(context.Background(), head)
	if err != nil || len(runs) != 2 || runs[0].ID == runs[1].ID {
		t.Fatalf("runs=%+v err=%v, want two distinct records", runs, err)
	}
}

func TestStorageNotConfiguredSavesNothing(t *testing.T) {
	f := newFixture(t)
	f.repo.Write(".evalci.yaml", "runs_per_case: 2\nproviders:\n  ollama:\n    base_url: http://unused\n")
	f.repo.Write("prompts/support.txt", "OK\n")
	f.repo.Commit("no storage, change prompt")
	o := f.run(okProvider())
	if o.exit != 0 {
		t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
	if _, err := os.Stat(filepath.Join(f.histDir, "history.db")); err == nil {
		t.Fatal("no history file may be created when storage is not configured (FR-019)")
	}
	if _, err := os.Stat(filepath.Join(f.repo.Dir, ".evalci")); err == nil {
		t.Fatal("no .evalci directory may be created when storage is not configured")
	}
	if id, present := f.result()["run_id"]; !present || id != nil {
		t.Fatalf("run_id must be null when nothing was saved: %v", id)
	}
}

type failingStore struct{ storage.Store }

func (failingStore) SaveRun(context.Context, storage.RunRecord) error { return errors.New("disk full") }

func TestSaveFailureOnlyWarns(t *testing.T) {
	baseline := func() outcome {
		f := newFixture(t)
		f.repo.Write("prompts/support.txt", "WRONG")
		f.repo.Commit("regress")
		return f.run(okProvider())
	}()
	f := newFixture(t)
	f.repo.Write("prompts/support.txt", "WRONG")
	f.repo.Commit("regress")
	deps := depsFor(okProvider())
	deps.openStore = func(context.Context, storage.Config) (storage.Store, error) { return failingStore{memory.New()}, nil }
	o := executeDeps(t, backgroundCtx(), deps, "--base", "main", "--output", f.out())
	if !strings.Contains(o.stderr, "warning: could not save run history: disk full") {
		t.Fatalf("stderr:\n%s", o.stderr)
	}
	if o.exit != baseline.exit || o.exit != 1 || !strings.Contains(o.stdout, "Decision: FAIL") { // SC-008
		t.Fatalf("a save failure changed the outcome: exit=%d (want %d)\n%s", o.exit, baseline.exit, o.stdout)
	}
	if id := f.result()["run_id"]; id != nil {
		t.Fatalf("run_id must be null when saving failed: %v", id)
	}
}

func TestOpenFailureOnlyWarnsAndTheRunContinues(t *testing.T) {
	f := newFixture(t)
	f.repo.Write("prompts/support.txt", "OK\n")
	f.repo.Commit("change")
	deps := depsFor(okProvider())
	deps.openStore = func(context.Context, storage.Config) (storage.Store, error) {
		return nil, errors.New("history file is locked")
	}
	o := executeDeps(t, backgroundCtx(), deps, "--base", "main", "--output", f.out())
	if o.exit != 0 || !strings.Contains(o.stderr, "could not open run history") || !strings.Contains(o.stdout, "Decision: PASS") {
		t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
}

func TestCancelledRunSavesNothing(t *testing.T) {
	f := newFixture(t)
	f.repo.Write("prompts/support.txt", "OK\n")
	head := f.repo.Commit("change")
	p := okProvider()
	p.Delay = 5 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	o := executeDeps(t, ctx, depsFor(p), "--base", "main", "--output", f.out())
	if o.exit != 2 || o.err == nil {
		t.Fatalf("a cancelled run must fail: exit=%d err=%v", o.exit, o.err)
	}
	runs, err := openHistory(t, f).ListRunsByCommit(context.Background(), head)
	if err != nil || len(runs) != 0 {
		t.Fatalf("a cancelled run saved %d records, want 0 (err %v)", len(runs), err)
	}
}

func TestUnknownCostWhenNoPrice(t *testing.T) {
	f := newFixture(t)
	f.repo.Write("evals/billing.eval.yaml", suiteFile("billing", "prompts/billing.txt")+"# touched\n") // m-billing has no price
	head := f.repo.Commit("change billing")
	o := f.run(okProvider())
	if o.exit != 0 || !strings.Contains(o.stdout, "est. unknown (cost unknown for 1 model)") {
		t.Fatalf("exit=%d\n%s", o.exit, o.stdout)
	}
	runs, _ := openHistory(t, f).ListRunsByCommit(context.Background(), head)
	if len(runs) != 1 {
		t.Fatalf("runs=%d", len(runs))
	}
	r := runs[0]
	if r.TotalCostMicros != nil || !r.CostPartial || r.Usage[0].CostMicros != nil || r.Usage[0].InputTokens != 20 {
		t.Fatalf("unknown cost must be nil (not zero) with tokens still recorded: %+v", r)
	}
	if cost := f.result()["cost"].(map[string]any); cost["estimated_cost"] != nil || cost["partial"] != true {
		t.Fatalf("JSON cost wrong: %v", cost)
	}
}

func TestRecordsHoldNoResponsesPromptsOrInputs(t *testing.T) {
	f := newFixture(t)
	secret := "TOP-SECRET-PROMPT-TEXT"
	f.repo.Write("prompts/support.txt", secret)
	f.repo.Write("evals/support.eval.yaml", strings.Replace(suiteFile("support", "prompts/support.txt"), `input: "q"`, `input: "CUSTOMER-PII-INPUT"`, 1))
	head := f.repo.Commit("secret prompt and input")
	if o := f.run(okProvider()); o.exit == 2 {
		t.Fatalf("exit=%d\n%s", o.exit, o.stderr)
	}
	data, err := os.ReadFile(filepath.Join(f.histDir, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	wal, _ := os.ReadFile(filepath.Join(f.histDir, "history.db-wal"))
	for _, blob := range [][]byte{data, wal} {
		if strings.Contains(string(blob), secret) || strings.Contains(string(blob), "CUSTOMER-PII-INPUT") {
			t.Fatal("the history file contains prompt text or a case input (FR-013a)")
		}
	}
	if runs, _ := openHistory(t, f).ListRunsByCommit(context.Background(), head); len(runs) != 1 {
		t.Fatalf("runs = %d", len(runs))
	}
}

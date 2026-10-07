package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ajay-7105/EvalCI/internal/storage"
)

func TestBaselineUsesTheBaseBranchPromptAndDetectsRegression(t *testing.T) {
	f := newFixture(t)
	f.repo.Write("prompts/support.txt", "BAD")
	f.repo.Commit("regress support")

	p := okProvider()
	o := f.run(p)
	if o.exit != 1 || !strings.Contains(o.stdout, "Decision: FAIL") || !strings.Contains(o.stdout, "REGRESSED") {
		t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
	systems := systemsByModel(p)["m-support"]
	seen := map[string]bool{}
	for _, s := range systems {
		seen[s] = true
	}
	if !seen["OK"] || !seen["BAD"] || len(seen) != 2 {
		t.Fatalf("baseline must use the base branch's prompt (OK) and the candidate the PR's (BAD), got %v", systems)
	}
}

func TestBaselineModelComesFromTheBaseBranch(t *testing.T) {
	f := newFixture(t)
	f.repo.Write("evals/support.eval.yaml", strings.Replace(suiteFile("support", "prompts/support.txt"), "m-support", "m-support-v2", 1))
	f.repo.Commit("candidate switches model")
	p := okProvider()
	if o := f.run(p); o.exit != 0 {
		t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
	calls := callsByModel(p)
	if calls["m-support"] != 1 || calls["m-support-v2"] != 1 {
		t.Fatalf("calls = %v, want baseline on the old model and candidate on the new one", calls)
	}
}

func TestAddedSuiteIsNewAndDoesNotFail(t *testing.T) {
	f := newFixture(t)
	f.repo.Write("evals/fresh.eval.yaml", suiteFile("fresh", "prompts/fresh.txt"))
	f.repo.Write("prompts/fresh.txt", "OK")
	head := f.repo.Commit("add a suite")

	p := okProvider()
	o := f.run(p)
	if o.exit != 0 {
		t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
	for _, want := range []string{"fresh    RUN   new suite", "fresh  c1    -         100.0      -     NEW", "Decision: PASS (1 cases)"} {
		if !strings.Contains(o.stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, o.stdout)
		}
	}
	if calls := callsByModel(p); calls["m-fresh"] != 1 { // candidate only, temperature 0
		t.Fatalf("calls = %v, want a single candidate call for the new suite", calls)
	}
	runs, _ := openHistory(t, f).ListRunsByCommit(context.Background(), head)
	if len(runs) != 1 || len(runs[0].Cases) != 1 || runs[0].Cases[0].Status != "NEW" || runs[0].Cases[0].BaselineAverage != nil || runs[0].Cases[0].Delta != nil {
		t.Fatalf("record = %+v", runs)
	}
}

func TestFirstSuitesEverAddedAreAllNew(t *testing.T) {
	f := newFixture(t)
	f.repo.Checkout("main")
	for _, n := range []string{"support", "billing", "legacy"} {
		f.repo.Remove("evals/" + n + ".eval.yaml")
	}
	f.repo.Commit("main has no suites yet")
	f.repo.Branch("feature4")
	f.repo.Write("evals/first.eval.yaml", suiteFile("first", "prompts/first.txt"))
	f.repo.Write("prompts/first.txt", "OK")
	f.repo.Commit("first suite")
	p := okProvider()
	o := f.run(p)
	if o.exit != 0 || !strings.Contains(o.stdout, "first  c1") || !strings.Contains(o.stdout, "NEW") {
		t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
}

func TestNewSuiteWhoseCallsAllFailIsAnError(t *testing.T) {
	f := newFixture(t)
	f.repo.Write("evals/fresh.eval.yaml", suiteFile("fresh", "prompts/fresh.txt"))
	f.repo.Write("prompts/fresh.txt", "OK")
	f.repo.Commit("add a suite")
	o := f.run(failingProvider())
	if o.exit != 2 || !strings.Contains(o.stdout, "Decision: ERROR") {
		t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
}

func TestDeletedSuiteIsRemovedAndNotRun(t *testing.T) {
	f := newFixture(t)
	f.repo.Remove("evals/legacy.eval.yaml")
	f.repo.Remove("prompts/legacy.txt")
	head := f.repo.Commit("delete legacy")
	p := okProvider()
	o := f.run(p)
	if o.exit != 0 || !strings.Contains(o.stdout, "legacy   REMOVED  suite removed") || !strings.Contains(o.stdout, "no suites affected") {
		t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
	if p.Calls() != 0 {
		t.Fatalf("calls = %d, want 0", p.Calls())
	}
	runs, _ := openHistory(t, f).ListRunsByCommit(context.Background(), head)
	var removed bool
	for _, s := range runs[0].Suites {
		removed = removed || (s.Suite == "legacy" && s.Action == storage.ActionRemoved)
	}
	if !removed {
		t.Fatalf("the removed suite must be recorded: %+v", runs[0].Suites)
	}
}

func TestExplicitBaselineDirKeepsPhaseOneBehaviour(t *testing.T) {
	workspace(t, settingsYAML, "GOOD", "GOOD", "0")
	o := execute(t, echoSystem(), "--baseline-dir", "baseline")
	if o.exit != 0 || strings.Contains(o.stdout, "Scope:") || !strings.Contains(o.stdout, "Decision: PASS") {
		t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
	doc := readResult(t)
	scope := doc["scope"].(map[string]any)
	if scope["mode"] != "baseline-dir" || scope["base"] != nil || scope["head"] != nil {
		t.Fatalf("scope = %v", scope)
	}
}

func TestBaselineDirModeIsRecordedWhenStorageIsConfigured(t *testing.T) {
	workspace(t, settingsYAML+"storage:\n  driver: sqlite\n  path: hist/h.db\n", "GOOD", "GOOD", "0")
	if o := execute(t, echoSystem(), "--baseline-dir", "baseline"); o.exit != 0 {
		t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
	s, err := openAt("hist/h.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	runs, err := s.ListRecentRuns(context.Background(), 5)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs=%d err=%v", len(runs), err)
	}
	r := runs[0]
	if r.Mode != storage.ModeBaselineDir || r.HeadSHA != storage.UnknownCommit || r.BaseRef != "" || r.BaseSHA != "" ||
		len(r.Suites) != 1 || r.Suites[0].Reason != "baseline directory given" {
		t.Fatalf("record = %+v", r)
	}
}

func openAt(path string) (storage.Store, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	return factoryOpen(path)
}

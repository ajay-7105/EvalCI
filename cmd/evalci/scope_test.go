package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnePromptChangeRunsOnlyThatSuite(t *testing.T) {
	f := newFixture(t)
	f.repo.Write("prompts/support.txt", "OK\n")
	f.repo.Commit("tweak support prompt")

	p := okProvider()
	o := f.run(p)
	if o.exit != 0 {
		t.Fatalf("exit=%d err=%v\n%s\n%s", o.exit, o.err, o.stdout, o.stderr)
	}
	calls := callsByModel(p)
	if calls["m-support"] != 2 || calls["m-billing"] != 0 || calls["m-legacy"] != 0 { // SC-001
		t.Fatalf("calls = %v, want only m-support (one per variant)", calls)
	}
	for _, want := range []string{
		"1 of 3 suites selected",
		"support  RUN   prompt file changed: prompts/support.txt",
		"billing  SKIP  no related changes",
		"legacy   SKIP  no related changes",
		"Decision: PASS (1 cases)",
	} {
		if !strings.Contains(o.stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, o.stdout)
		}
	}
	table := o.stdout[strings.Index(o.stdout, "SUITE"):]
	if strings.Contains(table, "billing") || strings.Contains(table, "legacy") {
		t.Errorf("skipped suites must not appear in the results table:\n%s", table)
	}
	scope := f.result()["scope"].(map[string]any)
	if scope["mode"] != "scoped" || len(scope["suites"].([]any)) != 3 {
		t.Fatalf("JSON scope wrong: %v", scope)
	}
}

func TestChangedSuiteFileSelectsOnlyThatSuite(t *testing.T) {
	f := newFixture(t)
	f.repo.Write("evals/billing.eval.yaml", suiteFile("billing", "prompts/billing.txt")+"# touched\n")
	f.repo.Commit("edit billing suite")
	p := okProvider()
	o := f.run(p)
	if o.exit != 0 || !strings.Contains(o.stdout, "billing  RUN   suite file changed: evals/billing.eval.yaml") {
		t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
	if calls := callsByModel(p); calls["m-billing"] != 2 || calls["m-support"] != 0 || calls["m-legacy"] != 0 {
		t.Fatalf("calls = %v", calls)
	}
}

func TestSharedPromptSelectsEverySuiteUsingIt(t *testing.T) {
	f := newFixture(t)
	f.repo.Checkout("main")
	f.repo.Write("evals/billing.eval.yaml", suiteFile("billing", "prompts/shared.txt"))
	f.repo.Write("evals/legacy.eval.yaml", suiteFile("legacy", "prompts/shared.txt"))
	f.repo.Write("prompts/shared.txt", "OK")
	f.repo.Commit("share a prompt")
	f.repo.Branch("feature2")
	f.repo.Write("prompts/shared.txt", "OK\n")
	f.repo.Commit("edit shared prompt")

	p := okProvider()
	o := f.run(p)
	if o.exit != 0 {
		t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
	calls := callsByModel(p)
	if calls["m-billing"] != 2 || calls["m-legacy"] != 2 || calls["m-support"] != 0 {
		t.Fatalf("calls = %v, want billing and legacy only", calls)
	}
	if !strings.Contains(o.stdout, "2 of 3 suites selected") {
		t.Fatalf("stdout:\n%s", o.stdout)
	}
}

func TestRenamedPromptSelectsTheSuite(t *testing.T) {
	f := newFixture(t)
	f.repo.Rename("prompts/legacy.txt", "prompts/legacy-v2.txt")
	f.repo.Write("evals/legacy.eval.yaml", suiteFile("legacy", "prompts/legacy-v2.txt"))
	f.repo.Commit("rename legacy prompt")
	p := okProvider()
	o := f.run(p)
	if o.exit != 0 || !strings.Contains(o.stdout, "legacy   RUN") {
		t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
	if calls := callsByModel(p); calls["m-legacy"] != 2 || calls["m-support"] != 0 {
		t.Fatalf("calls = %v", calls)
	}
}

func TestNothingAffectedIsAPassWithZeroCalls(t *testing.T) {
	f := newFixture(t)
	f.repo.Write("README.md", "docs changed")
	f.repo.Commit("docs only")
	p := okProvider()
	o := f.run(p)
	if o.exit != 0 || !strings.Contains(o.stdout, "Decision: PASS (no suites affected)") {
		t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
	if p.Calls() != 0 {
		t.Fatalf("made %d model calls, want 0", p.Calls())
	}
	if strings.Contains(o.stdout, "SUITE") {
		t.Fatalf("no table expected:\n%s", o.stdout)
	}
	doc := f.result()
	if doc["decision"] != "PASS" || doc["exit_code"] != float64(0) || len(doc["cases"].([]any)) != 0 {
		t.Fatalf("JSON wrong: %v", doc)
	}
}

func TestDirtyWorkingTreeWarns(t *testing.T) {
	f := newFixture(t)
	f.repo.Write("README.md", "docs changed")
	f.repo.Commit("docs only")
	f.repo.Write("README.md", "uncommitted edit")
	o := f.run(okProvider())
	if !strings.Contains(o.stderr, "warning: working tree has uncommitted changes") {
		t.Fatalf("stderr:\n%s", o.stderr)
	}
}

func TestRefusalsHappenBeforeAnyModelCall(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, f *fixture)
		args    []string
		wantErr string
	}{
		{"unresolvable base", nil, []string{"--base", "nope"}, "nope"},
		{"head is not the checked-out commit", func(t *testing.T, f *fixture) {
			f.repo.Write("README.md", "feature moved ahead of main")
			f.repo.Commit("feature ahead")
		}, []string{"--base", "main", "--head", "main"}, "feature is checked out"},
		{"head names another branch", func(t *testing.T, f *fixture) {
			f.repo.Write("README.md", "x")
			f.repo.Commit("on feature")
			f.repo.Checkout("main")
		}, []string{"--base", "main", "--head", "feature"}, "check out"},
		{"unrelated histories", func(t *testing.T, f *fixture) {
			f.repo.Orphan("other")
			f.repo.Write("README.md", "orphan")
			f.repo.Commit("orphan root")
		}, []string{"--base", "main"}, "fetch more history"},
		{"run from a subdirectory", func(t *testing.T, f *fixture) {
			sub := filepath.Join(f.repo.Dir, "sub")
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			t.Chdir(sub)
		}, []string{"--base", "main"}, "repository root"},
		{"evals directory outside the repository", nil, []string{"--base", "main", "--evals", "../elsewhere"}, "inside the repository"},
		{"absolute evals directory", nil, []string{"--base", "main", "--evals", "/etc"}, "inside the repository"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.setup != nil {
				tt.setup(t, f)
			}
			p := okProvider()
			args := append([]string{"--output", f.out()}, tt.args...)
			o := executeDeps(t, backgroundCtx(), depsFor(p), args...)
			if o.exit != 2 || o.err == nil || !strings.Contains(o.err.Error(), tt.wantErr) {
				t.Fatalf("exit=%d err=%v, want exit 2 mentioning %q", o.exit, o.err, tt.wantErr)
			}
			if p.Calls() != 0 {
				t.Fatalf("made %d model calls before refusing", p.Calls())
			}
			if _, err := os.Stat(f.out()); err == nil {
				t.Fatal("no result file expected when refusing")
			}
		})
	}
}

func TestNotAGitRepository(t *testing.T) {
	t.Chdir(t.TempDir())
	write(t, ".evalci.yaml", settingsYAML)
	write(t, "evals/s.eval.yaml", suiteYAML("prompts/p.txt", "0"))
	write(t, "prompts/p.txt", "GOOD")
	p := echoSystem()
	o := execute(t, p, "--base", "main")
	if o.exit != 2 || o.err == nil || !strings.Contains(o.err.Error(), "not a git repository") || p.Calls() != 0 {
		t.Fatalf("exit=%d err=%v calls=%d", o.exit, o.err, p.Calls())
	}
}

func TestUnparseableBaseSettingsSelectEverythingWithAWarning(t *testing.T) {
	f := newFixture(t)
	f.repo.Checkout("main")
	f.repo.Write(".evalci.yaml", settingsFor(f.histDir, "bogus_key: 1\n")) // invalid under the current strict schema
	f.repo.Commit("legacy settings")
	f.repo.Branch("feature3")
	f.repo.Write(".evalci.yaml", settingsFor(f.histDir, "")) // the pull request fixes it
	f.repo.Commit("fix settings")

	p := okProvider()
	o := f.run(p)
	if o.exit != 0 {
		t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
	if !strings.Contains(o.stderr, "treating settings as changed") || !strings.Contains(o.stdout, "3 of 3 suites selected") || !strings.Contains(o.stdout, "settings changed") {
		t.Fatalf("stdout:\n%s\nstderr:\n%s", o.stdout, o.stderr)
	}
}

func TestBaselineIsTheMergeBaseNotTheBaseTip(t *testing.T) {
	f := newFixture(t)
	f.repo.Write("prompts/support.txt", "OK\n")
	f.repo.Commit("feature edits support")
	// main moves on after the branch point: a changed prompt and a brand-new suite
	f.repo.Checkout("main")
	f.repo.Write("prompts/support.txt", "MAIN-MOVED-ON")
	f.repo.Write("evals/extra.eval.yaml", suiteFile("extra", "prompts/extra.txt"))
	f.repo.Write("prompts/extra.txt", "OK")
	f.repo.Commit("main moves on")
	f.repo.Checkout("feature")

	p := okProvider()
	o := f.run(p)
	if o.exit != 0 {
		t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
	for _, sys := range systemsByModel(p)["m-support"] {
		if strings.Contains(sys, "MAIN-MOVED-ON") {
			t.Fatalf("baseline used the base tip instead of the merge base: %v", systemsByModel(p))
		}
	}
	if strings.Contains(o.stdout, "extra") {
		t.Fatalf("a suite added to main after the branch point must not appear as removed:\n%s", o.stdout)
	}
}

func TestSettingsOutsideTheRepositoryNeverSelectSuites(t *testing.T) {
	f := newFixture(t)
	f.repo.Write("README.md", "docs changed")
	f.repo.Commit("docs only")
	cfg := filepath.Join(f.histDir, "elsewhere.yaml")
	if err := os.WriteFile(cfg, []byte(settingsFor(f.histDir, "")), 0o600); err != nil {
		t.Fatal(err)
	}
	p := okProvider()
	o := f.run(p, "--config", cfg)
	if o.exit != 0 || !strings.Contains(o.stdout, "no suites affected") || p.Calls() != 0 {
		t.Fatalf("exit=%d calls=%d\n%s\n%s", o.exit, p.Calls(), o.stdout, o.stderr)
	}
}

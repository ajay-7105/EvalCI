package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ajay-7105/EvalCI/internal/storage"
)

func TestFullRunsEveryDespiteScoping(t *testing.T) {
	f := newFixture(t)
	f.repo.Write("README.md", "docs only") // nothing would be selected without --full
	head := f.repo.Commit("docs")

	p := okProvider()
	o := f.run(p, "--full")
	if o.exit != 0 {
		t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
	if calls := callsByModel(p); calls["m-support"] != 2 || calls["m-billing"] != 2 || calls["m-legacy"] != 2 {
		t.Fatalf("calls = %v, want every suite", calls)
	}
	for _, want := range []string{"full run requested; 3 of 3 suites selected", "support  RUN  full run requested", "billing  RUN  full run requested"} {
		if !strings.Contains(o.stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, o.stdout)
		}
	}
	runs, _ := openHistory(t, f).ListRunsByCommit(context.Background(), head)
	if len(runs) != 1 || runs[0].Mode != storage.ModeFull || len(runs[0].Cases) != 3 {
		t.Fatalf("record = %+v", runs)
	}
	for _, s := range runs[0].Suites {
		if s.Action != "run" || s.Reason != "full run requested" {
			t.Fatalf("suite decision = %+v", s)
		}
	}
	if sc := f.result()["scope"].(map[string]any); sc["mode"] != "full" {
		t.Fatalf("JSON scope mode = %v", sc["mode"])
	}
}

func TestReasonsAppearInOutputJSONAndHistory(t *testing.T) {
	f := newFixture(t)
	f.repo.Write("prompts/support.txt", "OK\n")
	head := f.repo.Commit("edit")
	o := f.run(okProvider())
	if o.exit != 0 {
		t.Fatalf("exit=%d", o.exit)
	}
	runs, _ := openHistory(t, f).ListRunsByCommit(context.Background(), head)
	json := f.result()["scope"].(map[string]any)["suites"].([]any)
	if len(runs) != 1 || len(runs[0].Suites) != 3 || len(json) != 3 {
		t.Fatalf("suites: stored=%d json=%d", len(runs[0].Suites), len(json))
	}
	for i, s := range runs[0].Suites {
		j := json[i].(map[string]any)
		if s.Suite != j["suite"] || s.Action != j["action"] || s.Reason != j["reason"] || s.Reason == "" {
			t.Errorf("stored %+v and JSON %v disagree", s, j)
		}
		if !strings.Contains(o.stdout, s.Suite) {
			t.Errorf("suite %s missing from the printed scope", s.Suite)
		}
	}
	found := false
	for _, e := range json {
		j := e.(map[string]any)
		if j["suite"] == "support" {
			found = true
			if j["reason"] != "prompt file changed" || j["action"] != "run" || j["files"].([]any)[0] != "prompts/support.txt" {
				t.Fatalf("selected suite JSON = %v", j)
			}
		}
	}
	if !found {
		t.Fatal("support missing from the JSON scope")
	}
}

func TestRunAffectingSettingsSelectEverything(t *testing.T) {
	f := newFixture(t)
	f.repo.Write(".evalci.yaml", strings.Replace(settingsFor(f.histDir, ""), "runs_per_case: 2", "runs_per_case: 3", 1))
	f.repo.Commit("change runs_per_case")
	p := okProvider()
	o := f.run(p)
	if o.exit != 0 || !strings.Contains(o.stdout, "3 of 3 suites selected") || !strings.Contains(o.stdout, "settings changed") {
		t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
	if calls := callsByModel(p); calls["m-support"] != 2 || calls["m-billing"] != 2 || calls["m-legacy"] != 2 {
		t.Fatalf("calls = %v", calls)
	}
}

func TestPriceAndStorageOnlySettingsChangesSelectNothing(t *testing.T) {
	f := newFixture(t)
	changed := strings.Replace(settingsFor(f.histDir, ""), "input_per_million: 1,", "input_per_million: 9,", 1)
	changed = strings.Replace(changed, "history.db", "other-history.db", 1)
	changed += "currency: EUR\n"
	f.repo.Write(".evalci.yaml", changed)
	f.repo.Commit("only prices, storage path and currency")
	p := okProvider()
	o := f.run(p)
	if o.exit != 0 || !strings.Contains(o.stdout, "no suites affected") || p.Calls() != 0 {
		t.Fatalf("exit=%d calls=%d\n%s\n%s", o.exit, p.Calls(), o.stdout, o.stderr)
	}
}

func TestDeletedSettingsFileSelectsEverything(t *testing.T) {
	f := newFixture(t)
	f.repo.Checkout("main")
	f.repo.Remove(".evalci.yaml")
	f.repo.Commit("main has no settings")
	f.repo.Checkout("feature")
	// feature still has it: base (merge base) has the file, head has the file -> unchanged
	p := okProvider()
	o := f.run(p)
	if o.exit != 0 {
		t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
}

func TestSymlinkedWorkingDirectoryStillDetectsSettingsChanges(t *testing.T) {
	f := newFixture(t)
	f.repo.Write(".evalci.yaml", strings.Replace(settingsFor(f.histDir, ""), "runs_per_case: 2", "runs_per_case: 3", 1))
	f.repo.Commit("change runs_per_case")
	link := filepath.Join(t.TempDir(), "via-symlink")
	if err := os.Symlink(f.repo.Dir, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	t.Chdir(link) // the process now reaches the repository through a symlink

	p := okProvider()
	o := f.run(p)
	if o.exit != 0 || !strings.Contains(o.stdout, "3 of 3 suites selected") || !strings.Contains(o.stdout, "settings changed") {
		t.Fatalf("a settings change was missed when run through a symlink:\nexit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
	}
}

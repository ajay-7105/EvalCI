package main

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ajay-7105/EvalCI/internal/storage"
	"github.com/ajay-7105/EvalCI/internal/storage/memory"
)

func TestUnavailableBackendStopsBeforeAnyModelCall(t *testing.T) {
	for _, driver := range []string{"postgres", "mysql"} {
		t.Run(driver, func(t *testing.T) {
			f := newFixture(t)
			f.repo.Write(".evalci.yaml", strings.Replace(settingsFor(f.histDir, ""), "driver: sqlite", "driver: "+driver, 1))
			f.repo.Write("prompts/support.txt", "OK\n")
			f.repo.Commit("change")
			p := okProvider()
			o := f.run(p)
			if o.exit != 2 || o.err == nil || !strings.Contains(o.err.Error(), `storage backend "`+driver+`" is not available`) {
				t.Fatalf("exit=%d err=%v", o.exit, o.err)
			}
			if p.Calls() != 0 {
				t.Fatalf("made %d model calls before rejecting the backend", p.Calls())
			}
		})
	}
}

// normalized strips what legitimately differs between two runs of the same scenario.
func normalized(r storage.RunRecord) storage.RunRecord {
	r = r.Clone()
	r.ID = ""
	r.StartedAt, r.FinishedAt = r.StartedAt.Truncate(0).UTC().Truncate(24*3600*1e9), r.FinishedAt.Truncate(0).UTC().Truncate(24*3600*1e9)
	return r
}

func TestSwappingTheBackendChangesNoCallerBehaviour(t *testing.T) {
	scenario := func(t *testing.T, mem *memory.Store) storage.RunRecord {
		t.Helper()
		f := newFixture(t)
		f.repo.Write("prompts/support.txt", "WRONG")
		head := f.repo.Commit("regress")
		deps := depsFor(okProvider())
		if mem != nil {
			deps.openStore = func(context.Context, storage.Config) (storage.Store, error) { return mem, nil }
		}
		o := executeDeps(t, backgroundCtx(), deps, "--base", "main", "--output", f.out())
		if o.exit != 1 || !strings.Contains(o.stdout, "Decision: FAIL") {
			t.Fatalf("exit=%d\n%s\n%s", o.exit, o.stdout, o.stderr)
		}
		var runs []storage.RunRecord
		var err error
		if mem != nil {
			runs, err = mem.ListRunsByCommit(context.Background(), head)
		} else {
			runs, err = openHistory(t, f).ListRunsByCommit(context.Background(), head)
		}
		if err != nil || len(runs) != 1 {
			t.Fatalf("runs=%d err=%v", len(runs), err)
		}
		r := normalized(runs[0])
		r.HeadSHA, r.BaseSHA = "", "" // differ between the two repositories
		return r
	}
	viaMemory, viaSQLite := scenario(t, memory.New()), scenario(t, nil)
	if !reflect.DeepEqual(viaMemory, viaSQLite) {
		t.Fatalf("records differ by backend:\nmemory: %+v\nsqlite: %+v", viaMemory, viaSQLite)
	}
}

func TestOnlyTheRunCommandExists(t *testing.T) {
	root := newRootCmd()
	var names []string
	for _, c := range root.Commands() {
		if c.Name() != "help" && c.Name() != "completion" {
			names = append(names, c.Name())
		}
	}
	if len(names) != 1 || names[0] != "run" { // FR-026: no history browser, dashboard, or PR comment command
		t.Fatalf("commands = %v, want only run", names)
	}
}

func TestFlagValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"both baseline sources", []string{"--base", "main", "--baseline-dir", "b"}, "mutually exclusive"},
		{"neither baseline source", nil, "required"},
		{"full without base", []string{"--baseline-dir", "b", "--full"}, "--full only applies with --base"},
		{"head without base", []string{"--baseline-dir", "b", "--head", "x"}, "--head only applies with --base"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace(t, settingsYAML, "GOOD", "GOOD", "0")
			p := echoSystem()
			o := execute(t, p, tt.args...)
			if o.exit != 2 || o.err == nil || !strings.Contains(o.err.Error(), tt.wantErr) || p.Calls() != 0 {
				t.Fatalf("exit=%d err=%v calls=%d", o.exit, o.err, p.Calls())
			}
		})
	}
}

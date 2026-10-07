package diffscope

import (
	"reflect"
	"testing"

	"github.com/ajay-7105/EvalCI/configs"
)

func suite(name, file, prompt string) configs.Suite {
	return configs.Suite{Name: name, File: "evals/" + file, PromptRel: prompt}
}

func change(status, p string, old ...string) FileChange {
	fc := FileChange{Status: status, Path: p}
	if len(old) > 0 {
		fc.OldPath = old[0]
	}
	return fc
}

type want struct {
	suite, action, reason string
	files                 []string
}

func check(t *testing.T, got []ScopeDecision, wants []want) {
	t.Helper()
	if len(got) != len(wants) {
		t.Fatalf("got %d decisions, want %d: %+v", len(got), len(wants), got)
	}
	for i, w := range wants {
		g := got[i]
		if g.Suite != w.suite || g.Action != w.action || g.Reason != w.reason {
			t.Errorf("decision %d = {%s %s %q}, want {%s %s %q}", i, g.Suite, g.Action, g.Reason, w.suite, w.action, w.reason)
		}
		if w.files != nil && !reflect.DeepEqual(g.Files, w.files) {
			t.Errorf("decision %d files = %v, want %v", i, g.Files, w.files)
		}
	}
}

func baseline() []configs.Suite {
	return []configs.Suite{
		suite("support", "support.eval.yaml", "prompts/support.txt"),
		suite("billing", "billing.eval.yaml", "prompts/billing.txt"),
		suite("legacy", "legacy.eval.yaml", "prompts/shared.txt"),
		suite("other", "other.eval.yaml", "prompts/shared.txt"),
	}
}

func TestSelect(t *testing.T) {
	tests := []struct {
		name       string
		candidates []configs.Suite
		base       []configs.Suite
		changes    []FileChange
		settings   bool
		full       bool
		wants      []want
	}{
		{
			name:       "one prompt edit selects only the suite using it",
			candidates: baseline(), base: baseline(),
			changes: []FileChange{change(StatusModified, "prompts/support.txt")},
			wants: []want{
				{"support", ActionRun, ReasonPromptChange, []string{"prompts/support.txt"}},
				{"billing", ActionSkip, ReasonNoChanges, nil},
				{"legacy", ActionSkip, ReasonNoChanges, nil},
				{"other", ActionSkip, ReasonNoChanges, nil},
			},
		},
		{
			name:       "a shared prompt selects every suite referencing it",
			candidates: baseline(), base: baseline(),
			changes: []FileChange{change(StatusModified, "prompts/shared.txt")},
			wants: []want{
				{"support", ActionSkip, ReasonNoChanges, nil},
				{"billing", ActionSkip, ReasonNoChanges, nil},
				{"legacy", ActionRun, ReasonPromptChange, []string{"prompts/shared.txt"}},
				{"other", ActionRun, ReasonPromptChange, []string{"prompts/shared.txt"}},
			},
		},
		{
			name:       "an edited suite file selects that suite",
			candidates: baseline(), base: baseline(),
			changes: []FileChange{change(StatusModified, "evals/billing.eval.yaml")},
			wants: []want{
				{"support", ActionSkip, ReasonNoChanges, nil},
				{"billing", ActionRun, ReasonSuiteChanged, []string{"evals/billing.eval.yaml"}},
				{"legacy", ActionSkip, ReasonNoChanges, nil},
				{"other", ActionSkip, ReasonNoChanges, nil},
			},
		},
		{
			name: "a prompt renamed away: baseline path still matches the old path",
			candidates: []configs.Suite{
				suite("support", "support.eval.yaml", "prompts/support-v2.txt"),
			},
			base:    []configs.Suite{suite("support", "support.eval.yaml", "prompts/support.txt")},
			changes: []FileChange{change(StatusRenamed, "prompts/support-v2.txt", "prompts/support.txt")},
			wants:   []want{{"support", ActionRun, ReasonPromptChange, []string{"prompts/support-v2.txt", "prompts/support.txt"}}},
		},
		{
			name:       "a rename's old path alone selects a suite whose prompt did not change name",
			candidates: baseline()[:1], base: baseline()[:1],
			changes: []FileChange{change(StatusRenamed, "archive/support.txt", "prompts/support.txt")},
			wants:   []want{{"support", ActionRun, ReasonPromptChange, []string{"prompts/support.txt"}}},
		},
		{
			name:       "a candidate prompt that changed on the pull request but not at base",
			candidates: []configs.Suite{suite("support", "support.eval.yaml", "prompts/new.txt")},
			base:       []configs.Suite{suite("support", "support.eval.yaml", "prompts/support.txt")},
			changes:    []FileChange{change(StatusAdded, "prompts/new.txt")},
			wants:      []want{{"support", ActionRun, ReasonPromptChange, []string{"prompts/new.txt"}}},
		},
		{
			name:       "a suite added on the pull request is new",
			candidates: append(baseline(), suite("fresh", "fresh.eval.yaml", "prompts/fresh.txt")),
			base:       baseline(),
			changes:    []FileChange{change(StatusAdded, "evals/fresh.eval.yaml"), change(StatusAdded, "prompts/fresh.txt")},
			wants: []want{
				{"support", ActionSkip, ReasonNoChanges, nil},
				{"billing", ActionSkip, ReasonNoChanges, nil},
				{"legacy", ActionSkip, ReasonNoChanges, nil},
				{"other", ActionSkip, ReasonNoChanges, nil},
				{"fresh", ActionRun, ReasonNew, []string{"evals/fresh.eval.yaml"}},
			},
		},
		{
			name:       "a suite deleted on the pull request is removed and never run",
			candidates: baseline()[:2], base: baseline(),
			changes: []FileChange{change(StatusDeleted, "evals/other.eval.yaml"), change(StatusDeleted, "evals/legacy.eval.yaml")},
			wants: []want{
				{"support", ActionSkip, ReasonNoChanges, nil},
				{"billing", ActionSkip, ReasonNoChanges, nil},
				{"legacy", ActionRemoved, ReasonRemoved, nil},
				{"other", ActionRemoved, ReasonRemoved, nil},
			},
		},
		{
			name:       "only unrelated files changed selects nothing",
			candidates: baseline(), base: baseline(),
			changes: []FileChange{change(StatusModified, "README.md"), change(StatusAdded, "docs/x.md")},
			wants: []want{
				{"support", ActionSkip, ReasonNoChanges, nil},
				{"billing", ActionSkip, ReasonNoChanges, nil},
				{"legacy", ActionSkip, ReasonNoChanges, nil},
				{"other", ActionSkip, ReasonNoChanges, nil},
			},
		},
		{
			name:       "no changes at all selects nothing",
			candidates: baseline(), base: baseline(),
			wants: []want{
				{"support", ActionSkip, ReasonNoChanges, nil},
				{"billing", ActionSkip, ReasonNoChanges, nil},
				{"legacy", ActionSkip, ReasonNoChanges, nil},
				{"other", ActionSkip, ReasonNoChanges, nil},
			},
		},
		{
			name:       "run-affecting settings select every suite",
			candidates: baseline(), base: baseline(), settings: true,
			changes: []FileChange{change(StatusModified, ".evalci.yaml")},
			wants: []want{
				{"support", ActionRun, ReasonSettings, nil},
				{"billing", ActionRun, ReasonSettings, nil},
				{"legacy", ActionRun, ReasonSettings, nil},
				{"other", ActionRun, ReasonSettings, nil},
			},
		},
		{
			name:       "a changed settings file with no run-affecting difference selects nothing",
			candidates: baseline()[:1], base: baseline()[:1], settings: false,
			changes: []FileChange{change(StatusModified, ".evalci.yaml")},
			wants:   []want{{"support", ActionSkip, ReasonNoChanges, nil}},
		},
		{
			name:       "prompt change outranks settings change in the reported reason",
			candidates: baseline()[:2], base: baseline()[:2], settings: true,
			changes: []FileChange{change(StatusModified, "prompts/support.txt")},
			wants: []want{
				{"support", ActionRun, ReasonPromptChange, []string{"prompts/support.txt"}},
				{"billing", ActionRun, ReasonSettings, nil},
			},
		},
		{
			name:       "full run selects everything, new suites included",
			candidates: append(baseline()[:1], suite("fresh", "fresh.eval.yaml", "prompts/fresh.txt")), base: baseline()[:1], full: true,
			wants: []want{
				{"support", ActionRun, ReasonFull, nil},
				{"fresh", ActionRun, ReasonFull, nil},
			},
		},
		{
			name:       "full run still reports removed suites",
			candidates: baseline()[:1], base: baseline()[:2], full: true,
			wants: []want{
				{"support", ActionRun, ReasonFull, nil},
				{"billing", ActionRemoved, ReasonRemoved, nil},
			},
		},
		{
			name:       "suite file and prompt both changed reports the suite file first",
			candidates: baseline()[:1], base: baseline()[:1],
			changes: []FileChange{change(StatusModified, "evals/support.eval.yaml"), change(StatusModified, "prompts/support.txt")},
			wants:   []want{{"support", ActionRun, ReasonSuiteChanged, []string{"evals/support.eval.yaml"}}},
		},
		{
			name:       "suites are matched by file name, not suite name",
			candidates: []configs.Suite{suite("renamed-inside", "support.eval.yaml", "prompts/support.txt")},
			base:       []configs.Suite{suite("support", "support.eval.yaml", "prompts/support.txt")},
			wants:      []want{{"renamed-inside", ActionSkip, ReasonNoChanges, nil}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Select(SelectInput{
				Candidates: tt.candidates, Base: tt.base, EvalsDir: "evals",
				Changes: tt.changes, SettingsChanged: tt.settings, Full: tt.full,
			})
			check(t, got, tt.wants)
			for _, d := range got {
				if d.File == "" || d.Reason == "" || d.Suite == "" {
					t.Errorf("incomplete decision: %+v", d)
				}
			}
		})
	}
}

func TestSelectIsDeterministic(t *testing.T) {
	in := SelectInput{
		Candidates: baseline(), Base: baseline()[:3], EvalsDir: "evals",
		Changes: []FileChange{change(StatusModified, "prompts/shared.txt")},
	}
	first := Select(in)
	for i := 0; i < 50; i++ {
		if got := Select(in); !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differs", i)
		}
	}
}

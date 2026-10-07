package diffscope

import (
	"path"
	"path/filepath"
	"sort"

	"github.com/ajay-7105/EvalCI/configs"
)

// Scope actions.
const (
	ActionRun     = "run"
	ActionSkip    = "skip"
	ActionRemoved = "removed"
)

// Scope reasons, exactly as shown to users and stored in history.
const (
	ReasonFull         = "full run requested"
	ReasonNew          = "new suite"
	ReasonSuiteChanged = "suite file changed"
	ReasonPromptChange = "prompt file changed"
	ReasonSettings     = "settings changed"
	ReasonNoChanges    = "no related changes"
	ReasonRemoved      = "suite removed"
	// ReasonBaselineDir is used when an explicit baseline directory is given: every suite runs.
	ReasonBaselineDir = "baseline directory given"
)

// ScopeDecision is the outcome of scoping for one suite.
type ScopeDecision struct {
	// Suite is the suite name.
	Suite string
	// File is the suite file path relative to the repository root (slash-separated).
	File string
	// Action is run, skip, or removed.
	Action string
	// Reason is one of the Reason constants.
	Reason string
	// Files are the changed paths that caused selection (may be empty).
	Files []string
}

// SelectInput is everything Select needs; it performs no I/O.
type SelectInput struct {
	// Candidates are the suites at the head commit.
	Candidates []configs.Suite
	// Base are the suites at the baseline commit (the merge base).
	Base []configs.Suite
	// EvalsDir is the suites directory relative to the repository root, slash-separated.
	EvalsDir string
	// Changes is the change set from the baseline commit to the head commit.
	Changes []FileChange
	// SettingsChanged is true when run-affecting settings differ (see RunAffectingChanged).
	SettingsChanged bool
	// Full selects every suite.
	Full bool
}

// Select decides, per suite, whether to run it. For each candidate suite the first matching
// rule wins: full run requested; the suite has no baseline counterpart (new); its own file
// changed; its prompt (as written in the candidate or the baseline version) changed; run-affecting
// settings changed; otherwise skipped. Renamed files count under both paths. Suites that
// exist in the baseline but not at the head are reported as removed and never run.
func Select(in SelectInput) []ScopeDecision {
	changed := map[string]bool{}
	for _, c := range in.Changes {
		for _, p := range c.Paths() {
			changed[p] = true
		}
	}
	baseByFile := map[string]configs.Suite{}
	for _, b := range in.Base {
		baseByFile[filepath.Base(b.File)] = b
	}

	var out []ScopeDecision
	seen := map[string]bool{}
	for _, c := range in.Candidates {
		name := filepath.Base(c.File)
		seen[name] = true
		file := path.Join(in.EvalsDir, name)
		d := ScopeDecision{Suite: c.Name, File: file, Action: ActionRun}
		b, hasBase := baseByFile[name]

		switch {
		case in.Full:
			d.Reason = ReasonFull
		case !hasBase:
			d.Reason = ReasonNew
			d.Files = []string{file}
		case changed[file]:
			d.Reason = ReasonSuiteChanged
			d.Files = []string{file}
		case promptChanged(changed, c, b, &d):
			d.Reason = ReasonPromptChange
		case in.SettingsChanged:
			d.Reason = ReasonSettings
		default:
			d.Action = ActionSkip
			d.Reason = ReasonNoChanges
		}
		out = append(out, d)
	}

	var removed []ScopeDecision
	for _, b := range in.Base {
		name := filepath.Base(b.File)
		if seen[name] {
			continue
		}
		removed = append(removed, ScopeDecision{
			Suite: b.Name, File: path.Join(in.EvalsDir, name), Action: ActionRemoved, Reason: ReasonRemoved,
		})
	}
	sort.Slice(removed, func(i, j int) bool { return removed[i].File < removed[j].File })
	return append(out, removed...)
}

// promptChanged reports whether the candidate's or the baseline's prompt path changed, and
// records the changed path(s) on d.
func promptChanged(changed map[string]bool, cand, base configs.Suite, d *ScopeDecision) bool {
	var files []string
	for _, p := range []string{cand.PromptRel, base.PromptRel} {
		if p != "" && changed[p] && !contains(files, p) {
			files = append(files, p)
		}
	}
	d.Files = files
	return len(files) > 0
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

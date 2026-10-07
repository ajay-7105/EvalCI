package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ajay-7105/EvalCI/configs"
	"github.com/ajay-7105/EvalCI/internal/assertions"
	"github.com/ajay-7105/EvalCI/internal/cost"
	"github.com/ajay-7105/EvalCI/internal/diffscope"
	"github.com/ajay-7105/EvalCI/internal/engine"
	"github.com/ajay-7105/EvalCI/internal/gating"
	"github.com/ajay-7105/EvalCI/internal/reporters"
	"github.com/ajay-7105/EvalCI/internal/storage"
	"github.com/ajay-7105/EvalCI/internal/storage/factory"
)

type runFlags struct {
	base        string
	head        string
	headSet     bool
	full        bool
	baselineDir string
	configPath  string
	evalsDir    string
	output      string
	verbose     bool
}

// runDeps are the seams tests replace; production uses the real registry, git, and storage.
type runDeps struct {
	newLookup func(configs.Settings) assertions.ProviderLookup
	openRepo  func(dir string) (diffscope.Repository, error)
	openStore func(ctx context.Context, cfg storage.Config) (storage.Store, error)
	now       func() time.Time
}

func defaultDeps() runDeps {
	return runDeps{
		newLookup: func(s configs.Settings) assertions.ProviderLookup { return newRegistry(s, os.Getenv) },
		openRepo:  func(dir string) (diffscope.Repository, error) { return diffscope.OpenGit(dir) },
		openStore: factory.Open,
		now:       time.Now,
	}
}

func newRunCmd() *cobra.Command {
	return newRunCmdWith(defaultDeps())
}

func newRunCmdWith(deps runDeps) *cobra.Command {
	var f runFlags
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run eval suites against a baseline and a candidate and print a gate decision",
		Long: `Run eval suites against a baseline and a candidate and print a gate decision.

With --base, the baseline is the base branch (as of its common ancestor with the pull request
branch) and only suites affected by the changes are run, unless --full is given. With
--baseline-dir, a repo-shaped directory supplies the baseline and every suite runs.
Run from the repository root.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f.headSet = cmd.Flags().Changed("head")
			return runGate(cmd, f, deps)
		},
	}
	cmd.Flags().StringVar(&f.base, "base", "", "base branch or ref: baseline comes from it and enables suite scoping")
	cmd.Flags().StringVar(&f.head, "head", "HEAD", "pull request branch or ref; must be the checked-out commit")
	cmd.Flags().BoolVar(&f.full, "full", false, "run every suite regardless of changes (with --base)")
	cmd.Flags().StringVar(&f.baselineDir, "baseline-dir", "", "repo-shaped directory holding the baseline suites and prompts (runs every suite)")
	cmd.Flags().StringVar(&f.configPath, "config", configs.DefaultSettingsPath, "settings file")
	cmd.Flags().StringVar(&f.evalsDir, "evals", configs.DefaultEvalsDirectory, "directory scanned for *.eval.yaml suites")
	cmd.Flags().StringVar(&f.output, "output", "evalci-result.json", "JSON result path")
	cmd.Flags().BoolVar(&f.verbose, "verbose", false, "log one line per model call to stderr")
	return cmd
}

// scopeInfo is what a --base run learned about the change set.
type scopeInfo struct {
	cs        diffscope.ChangeSet
	decisions []diffscope.ScopeDecision
	pairs     []engine.SuitePair
	cleanup   func()
}

func runGate(cmd *cobra.Command, f runFlags, deps runDeps) error {
	stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()
	if err := validateFlags(f); err != nil {
		return err
	}
	evalsDir, err := cleanEvalsDir(f.evalsDir)
	if err != nil {
		return err
	}
	var repo diffscope.Repository
	if f.base != "" {
		// Checked first so running from a subdirectory gets a clear message rather than a
		// confusing "settings file not found".
		if repo, err = deps.openRepo("."); err != nil {
			return err
		}
		if err := requireRepoRoot(repo); err != nil {
			return err
		}
	}
	settings, err := configs.LoadSettings(f.configPath)
	if err != nil {
		return fmt.Errorf("loading settings: %w", err)
	}
	started := deps.now()

	store, err := openStore(cmd.Context(), settings, deps, stderr)
	if err != nil {
		return err
	}
	if store != nil {
		defer store.Close()
	}

	candidates, err := configs.LoadSuites(".", evalsDir)
	if err != nil {
		return fmt.Errorf("loading candidate suites: %w", err)
	}

	var (
		pairs     []engine.SuitePair
		decisions []diffscope.ScopeDecision
		cs        *diffscope.ChangeSet
		mode      = storage.ModeBaselineDir
	)
	if f.base != "" {
		info, err := scopeFromGit(repo, f, settings, candidates, evalsDir, stderr)
		if err != nil {
			return err
		}
		defer info.cleanup()
		pairs, decisions, cs = info.pairs, info.decisions, &info.cs
		mode = storage.ModeScoped
		if f.full {
			mode = storage.ModeFull
		}
	} else {
		baselines, err := configs.LoadSuites(f.baselineDir, evalsDir)
		if err != nil {
			return fmt.Errorf("loading baseline suites: %w", err)
		}
		if pairs, err = engine.ResolveBaseline(candidates, baselines, false); err != nil {
			return err
		}
		for _, c := range candidates {
			decisions = append(decisions, diffscope.ScopeDecision{
				Suite: c.Name, File: path.Join(evalsDir, filepath.Base(c.File)),
				Action: diffscope.ActionRun, Reason: diffscope.ReasonBaselineDir,
			})
		}
	}

	if cs != nil {
		if err := reporters.WriteScope(stdout, scopeSummary(f, cs, decisions)); err != nil {
			return err
		}
	}

	var results []gating.CaseResult
	var summary cost.Summary
	decision := gating.DecisionPass
	if len(pairs) == 0 {
		zero := int64(0)
		summary = cost.Summary{TotalCostMicros: &zero}
		if err := reporters.WriteNothingAffected(stdout); err != nil {
			return err
		}
	} else {
		lookup := deps.newLookup(settings)
		if err := engine.Preflight(engine.PreflightInput{Settings: settings, Pairs: pairs, Lookup: lookup}); err != nil {
			return fmt.Errorf("pre-flight checks failed:\n%w", err)
		}
		eng := engine.New(engine.Config{
			Settings: settings,
			Lookup:   lookup,
			Observer: reporters.NewStderr(stderr, f.verbose),
		})
		if results, err = eng.Run(cmd.Context(), pairs); err != nil {
			return err
		}
		decision = gating.Decide(results)
		us, ps := costInputs(eng.Usage(), settings.Prices)
		summary = cost.Compute(us, ps)

		if err := reporters.WriteTable(stdout, results, decision); err != nil {
			return err
		}
		if err := reporters.WriteCost(stdout, summary, settings.Currency); err != nil {
			return err
		}
	}
	finished := deps.now()
	exit := reporters.ExitCode(decision)

	runID := ""
	if store != nil {
		runID = saveRun(cmd.Context(), store, stderr, deps, runFacts{
			ID: newRunID(started), StartedAt: started, FinishedAt: finished,
			HeadSHA: "", Mode: mode, Decision: decision, ExitCode: exit,
			Settings: settings, Scope: decisions, Results: results, Cost: summary,
		}, cs)
	}

	if err := writeJSONFile(f.output, reporters.Report{
		RunID: runID, Decision: decision,
		Settings: reporters.ResultSettings{
			RunsPerCase:               settings.RunsPerCase,
			MinDropThreshold:          settings.MinDropThreshold,
			MaxSpreadBeforeUnreliable: settings.MaxSpreadBeforeUnreliable,
		},
		Scope: scopeReport(mode, cs, decisions), Cost: summary, Currency: settings.Currency, Results: results,
	}); err != nil {
		return err
	}
	if exit != reporters.ExitOK {
		return &exitError{code: exit}
	}
	return nil
}

// validateFlags enforces the flag combinations before anything else runs.
func validateFlags(f runFlags) error {
	switch {
	case f.base != "" && f.baselineDir != "":
		return errors.New("--base and --baseline-dir are mutually exclusive: use --base to take the baseline from git, or --baseline-dir to supply it yourself")
	case f.base == "" && f.baselineDir == "":
		return errors.New("one of --base <ref> or --baseline-dir <dir> is required")
	case f.base == "" && f.full:
		return errors.New("--full only applies with --base (a --baseline-dir run already runs every suite)")
	case f.base == "" && f.headSet:
		return errors.New("--head only applies with --base")
	}
	return nil
}

// cleanEvalsDir returns the suites directory as a clean, slash-separated path inside the repo.
func cleanEvalsDir(dir string) (string, error) {
	clean := path.Clean(filepath.ToSlash(dir))
	if filepath.IsAbs(dir) || clean == ".." || strings.HasPrefix(clean, "../") || clean == "." {
		return "", fmt.Errorf("--evals %q must be a directory inside the repository (relative to the repository root)", dir)
	}
	return clean, nil
}

// openStore opens the configured history store before any model call. An unavailable backend
// is a configuration error; any other failure to open only produces a warning, because history
// must never change the gate.
func openStore(ctx context.Context, s configs.Settings, deps runDeps, stderr io.Writer) (storage.Store, error) {
	if s.Storage.Driver == "" {
		return nil, nil
	}
	st, err := deps.openStore(ctx, storage.Config{Driver: s.Storage.Driver, Path: s.Storage.Path})
	switch {
	case errors.Is(err, storage.ErrBackendUnavailable):
		return nil, fmt.Errorf("loading settings: %w", err)
	case err != nil:
		fmt.Fprintf(stderr, "warning: could not open run history, this run will not be saved: %v\n", err)
		return nil, nil
	}
	return st, nil
}

// saveRun saves the record and returns its ID, or "" (with a warning) when saving failed.
func saveRun(ctx context.Context, store storage.Store, stderr io.Writer, deps runDeps, f runFacts, cs *diffscope.ChangeSet) string {
	f.HeadSHA = storage.UnknownCommit
	if cs != nil {
		f.HeadSHA, f.HeadBranch, f.BaseRef, f.BaseSHA = string(cs.Head), cs.HeadBranch, cs.BaseRef, string(cs.Base)
	} else if repo, err := deps.openRepo("."); err == nil {
		if sha, branch, err := repo.CheckedOut(); err == nil {
			f.HeadSHA, f.HeadBranch = string(sha), branch
		}
	}
	if ctx.Err() != nil {
		return "" // a cancelled run saves nothing
	}
	rec := buildRecord(f)
	if err := store.SaveRun(ctx, rec); err != nil {
		fmt.Fprintf(stderr, "warning: could not save run history: %v\n", err)
		return ""
	}
	return rec.ID
}

// scopeFromGit works out the change set, exports the baseline at the merge base, and selects
// the suites to run.
func scopeFromGit(repo diffscope.Repository, f runFlags, settings configs.Settings, candidates []configs.Suite,
	evalsDir string, stderr io.Writer) (*scopeInfo, error) {
	cs, err := diffscope.BuildChangeSet(repo, f.base, f.head)
	if err != nil {
		return nil, err
	}
	if dirty, err := repo.Dirty(); err == nil && dirty {
		fmt.Fprintln(stderr, "warning: working tree has uncommitted changes; they are not part of the comparison")
	}

	// The baseline is the base branch as of the merge base, so changes that landed on the base
	// branch after the pull request branched are not mistaken for regressions or removals.
	_, baseSuites, cleanup, err := diffscope.ExportBase(repo, cs.MergeBase, evalsDir)
	if err != nil {
		return nil, err
	}

	changed, err := settingsChanged(repo, cs, f.configPath, settings, stderr)
	if err != nil {
		cleanup()
		return nil, err
	}
	decisions := diffscope.Select(diffscope.SelectInput{
		Candidates: candidates, Base: baseSuites, EvalsDir: evalsDir,
		Changes: cs.Files, SettingsChanged: changed, Full: f.full,
	})

	selected := make([]configs.Suite, 0, len(candidates))
	run := map[string]bool{}
	for _, d := range decisions {
		if d.Action == diffscope.ActionRun {
			run[path.Base(d.File)] = true
		}
	}
	for _, c := range candidates {
		if run[filepath.Base(c.File)] {
			selected = append(selected, c)
		}
	}
	pairs, err := engine.ResolveBaseline(selected, baseSuites, true)
	if err != nil {
		cleanup()
		return nil, err
	}
	return &scopeInfo{cs: cs, decisions: decisions, pairs: pairs, cleanup: cleanup}, nil
}

// requireRepoRoot refuses to run from a subdirectory: suite and prompt paths are matched
// against repository-relative changed paths, so a different working directory would silently
// skip suites.
func requireRepoRoot(repo diffscope.Repository) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("reading the working directory: %w", err)
	}
	a, errA := filepath.EvalSymlinks(cwd)
	b, errB := filepath.EvalSymlinks(repo.Root())
	if errA != nil || errB != nil {
		a, b = cwd, repo.Root()
	}
	if a != b {
		return fmt.Errorf("run evalci from the repository root (%s); the current directory is %s", repo.Root(), cwd)
	}
	return nil
}

// settingsChanged compares run-affecting settings at the merge base and at the head. A
// settings file outside the repository cannot be part of a pull request, so it counts as
// unchanged; a base copy that cannot be parsed counts as changed (with a warning) so no
// suite is skipped on a guess.
func settingsChanged(repo diffscope.Repository, cs diffscope.ChangeSet, configPath string, head configs.Settings, stderr io.Writer) (bool, error) {
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return false, fmt.Errorf("resolving %s: %w", configPath, err)
	}
	// Resolve symlinks on both sides: a working directory reached through a symlink (such as
	// /var -> /private/var on macOS) must not make the file look like it is outside the repo.
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	root := repo.Root()
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, nil
	}
	rel = filepath.ToSlash(rel)
	data, err := repo.FileAt(cs.MergeBase, rel)
	switch {
	case errors.Is(err, diffscope.ErrFileNotFound):
		return diffscope.RunAffectingChanged(nil, &head), nil
	case err != nil:
		return false, fmt.Errorf("reading %s at %s: %w", rel, cs.MergeBase.Short(), err)
	}
	base, err := configs.ParseSettings(cs.BaseRef+":"+rel, data)
	if err != nil {
		fmt.Fprintf(stderr, "warning: could not parse %s at the base commit (%v); treating settings as changed\n", rel, err)
		return true, nil
	}
	return diffscope.RunAffectingChanged(&base, &head), nil
}

func scopeSummary(f runFlags, cs *diffscope.ChangeSet, decisions []diffscope.ScopeDecision) reporters.ScopeSummary {
	return reporters.ScopeSummary{
		Full: f.full, BaseRef: cs.BaseRef, BaseSHA: string(cs.Base), HeadRef: cs.HeadRef,
		HeadSHA: string(cs.Head), HeadName: cs.HeadBranch, Decisions: decisions,
	}
}

func scopeReport(mode storage.Mode, cs *diffscope.ChangeSet, decisions []diffscope.ScopeDecision) reporters.ScopeReport {
	r := reporters.ScopeReport{Mode: string(mode), Suites: decisions}
	if cs != nil {
		r.Base = &reporters.RefInfo{Ref: cs.BaseRef, SHA: string(cs.Base)}
		r.Head = &reporters.RefInfo{Ref: cs.HeadRef, SHA: string(cs.Head), Branch: cs.HeadBranch}
	}
	return r
}

func writeJSONFile(path string, rep reporters.Report) (err error) {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating result file: %w", err)
	}
	defer func() {
		if cerr := file.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("closing result file: %w", cerr)
		}
	}()
	return reporters.WriteJSON(file, rep)
}

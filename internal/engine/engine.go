package engine

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/ajay-7105/EvalCI/configs"
	"github.com/ajay-7105/EvalCI/internal/assertions"
	"github.com/ajay-7105/EvalCI/internal/gating"
	"github.com/ajay-7105/EvalCI/internal/providers"
)

// Config holds the engine's dependencies; all are passed explicitly.
type Config struct {
	// Settings are the repo-level settings.
	Settings configs.Settings
	// Lookup resolves a configured provider by name.
	Lookup assertions.ProviderLookup
	// Observer receives diagnostics; nil discards them.
	Observer Observer
}

// Engine runs suite pairs and gates the results.
type Engine struct {
	cfg Config
	obs Observer

	mu    sync.Mutex
	prov  map[string]providers.Provider
	meter *usageMeter
}

// New returns an Engine.
func New(cfg Config) *Engine {
	obs := cfg.Observer
	if obs == nil {
		obs = NopObserver{}
	}
	return &Engine{cfg: cfg, obs: obs, prov: map[string]providers.Provider{}, meter: newUsageMeter()}
}

// provider returns the named provider wrapped with its limiter and observer. Wrapped
// instances are cached so a provider's limit is shared by every call to it.
func (e *Engine) provider(name string) (providers.Provider, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if p, ok := e.prov[name]; ok {
		return p, nil
	}
	inner, err := e.cfg.Lookup(name)
	if err != nil {
		return nil, err
	}
	p := &limited{inner: inner, name: name, lim: newLimiter(e.cfg.Settings.Providers[name]), obs: e.obs, meter: e.meter}
	e.prov[name] = p
	return p, nil
}

type runResult struct {
	score float64
	err   error
	done  bool
}

// side collects the run slots of one variant of one case.
type side struct {
	variant Variant
	runs    []runResult
}

type caseJob struct {
	suite    string
	caseID   string
	critical bool
	base     *side // nil for a candidate-only (new) suite
	cand     *side
}

// Run executes every case of every pair against both variants and returns gated results
// sorted by suite name then case id. Failed runs are excluded from averages and reported to
// the observer; only context cancellation aborts the whole run.
func (e *Engine) Run(ctx context.Context, pairs []SuitePair) ([]gating.CaseResult, error) {
	g, gctx := errgroup.WithContext(ctx)
	var jobs []*caseJob

	for _, pair := range pairs {
		var baseTpl *Template
		var baseProv providers.Provider
		if !pair.NoBaseline {
			var err error
			if baseTpl, err = LoadTemplate(pair.Baseline.Prompt); err != nil {
				return nil, fmt.Errorf("suite %q baseline: %w", pair.Candidate.Name, err)
			}
			if baseProv, err = e.provider(pair.Baseline.Model.Provider); err != nil {
				return nil, fmt.Errorf("suite %q baseline provider: %w", pair.Candidate.Name, err)
			}
		}
		candTpl, err := LoadTemplate(pair.Candidate.Prompt)
		if err != nil {
			return nil, fmt.Errorf("suite %q candidate: %w", pair.Candidate.Name, err)
		}
		candProv, err := e.provider(pair.Candidate.Model.Provider)
		if err != nil {
			return nil, fmt.Errorf("suite %q candidate provider: %w", pair.Candidate.Name, err)
		}
		for _, c := range pair.Candidate.Cases {
			as, err := e.buildAssertions(c)
			if err != nil {
				return nil, fmt.Errorf("suite %q case %q: %w", pair.Candidate.Name, c.ID, err)
			}
			job := &caseJob{
				suite: pair.Candidate.Name, caseID: c.ID, critical: c.IsCritical(),
				cand: &side{variant: Candidate, runs: make([]runResult, e.plannedRuns(pair.Candidate.Model))},
			}
			jobs = append(jobs, job)
			if !pair.NoBaseline {
				job.base = &side{variant: Baseline, runs: make([]runResult, e.plannedRuns(pair.Baseline.Model))}
				e.launch(gctx, g, job, job.base, pair.Baseline.Model, baseTpl, baseProv, c, as)
			}
			e.launch(gctx, g, job, job.cand, pair.Candidate.Model, candTpl, candProv, c, as)
		}
	}
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("running evals: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("running evals: %w", err)
	}

	gs := gating.Settings{
		MinDropThreshold: e.cfg.Settings.MinDropThreshold,
		MaxSpread:        e.cfg.Settings.MaxSpreadBeforeUnreliable,
	}
	results := make([]gating.CaseResult, 0, len(jobs))
	for _, j := range jobs {
		if j.base == nil {
			results = append(results, gating.EvaluateNew(j.suite, j.caseID, j.critical, toSide(j.cand)))
			continue
		}
		results = append(results, gating.Evaluate(j.suite, j.caseID, j.critical, toSide(j.base), toSide(j.cand), gs))
	}
	sort.SliceStable(results, func(a, b int) bool {
		if results[a].Suite != results[b].Suite {
			return results[a].Suite < results[b].Suite
		}
		return results[a].Case < results[b].Case
	})
	return results, nil
}

// Usage returns the token usage of every successful provider call made so far (generation,
// judge, and embedding calls), aggregated per provider and model and sorted. It makes no
// calls of its own.
func (e *Engine) Usage() []ModelUsage { return e.meter.snapshot() }

// plannedRuns is 1 for temperature-0 models and runs_per_case otherwise.
func (e *Engine) plannedRuns(m configs.Model) int {
	if m.SingleRun() {
		return 1
	}
	return e.cfg.Settings.RunsPerCase
}

func (e *Engine) buildAssertions(c configs.Case) ([]assertions.Assertion, error) {
	lookup := func(name string) (providers.Provider, error) { return e.provider(name) }
	as := make([]assertions.Assertion, 0, len(c.Assertions))
	for _, a := range c.Assertions {
		built, err := assertions.New(a, lookup)
		if err != nil {
			return nil, err
		}
		as = append(as, built)
	}
	return as, nil
}

// launch starts one goroutine per planned run of a side. Each goroutine writes only its
// own slot, so no locking is needed.
func (e *Engine) launch(ctx context.Context, g *errgroup.Group, j *caseJob, s *side, m configs.Model,
	tpl *Template, prov providers.Provider, c configs.Case, as []assertions.Assertion) {
	system, user := tpl.Render(c.Input)
	req := providers.Request{Model: m.Name, System: system, User: user, MaxTokens: m.MaxTokens}
	if m.Temperature != nil {
		req.Temperature = *m.Temperature
	}
	for i := range s.runs {
		i := i
		g.Go(func() error {
			rctx := withRunInfo(ctx, runInfo{suite: j.suite, caseID: j.caseID, variant: s.variant, run: i})
			score, err := e.oneRun(rctx, prov, req, as)
			if err != nil && ctx.Err() != nil {
				return ctx.Err()
			}
			s.runs[i] = runResult{score: score, err: err, done: true}
			if err != nil {
				e.obs.RunFailed(RunFailure{Suite: j.suite, Case: j.caseID, Variant: s.variant, Run: i, Err: err})
			}
			return nil
		})
	}
}

func (e *Engine) oneRun(ctx context.Context, prov providers.Provider, req providers.Request, as []assertions.Assertion) (float64, error) {
	resp, err := prov.Complete(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("generation failed: %w", err)
	}
	return assertions.Score(ctx, as, resp.Text)
}

func toSide(s *side) gating.Side {
	var scores []float64
	failed := 0
	for _, r := range s.runs {
		if r.err != nil || !r.done {
			failed++
			continue
		}
		scores = append(scores, r.score)
	}
	return gating.NewSide(len(s.runs), failed, scores)
}

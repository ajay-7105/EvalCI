package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ajay-7105/EvalCI/configs"
	"github.com/ajay-7105/EvalCI/internal/gating"
	"github.com/ajay-7105/EvalCI/internal/providers"
	"github.com/ajay-7105/EvalCI/internal/providers/fake"
)

func fp(f float64) *float64 { return &f }
func bp(b bool) *bool       { return &b }

func writePrompt(t *testing.T, dir, name, text string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func exact(expected string) []configs.Assertion {
	return []configs.Assertion{{Type: configs.AssertExactMatch, Expected: expected}}
}

func suiteOf(name, prompt string, temp float64, cases ...configs.Case) configs.Suite {
	return configs.Suite{
		File: name + ".eval.yaml", Name: name, Prompt: prompt,
		Model: configs.Model{Provider: "fake", Name: "m", Temperature: fp(temp)},
		Cases: cases,
	}
}

func pairOf(cand configs.Suite, basePrompt string) SuitePair {
	b := cand
	b.Prompt = basePrompt
	return SuitePair{Candidate: cand, Baseline: b}
}

func newEngine(p providers.Provider, runs int, obs Observer, ps configs.ProviderSettings) *Engine {
	return New(Config{
		Settings: configs.Settings{
			RunsPerCase: runs, MinDropThreshold: 5, MaxSpreadBeforeUnreliable: 20,
			Providers: map[string]configs.ProviderSettings{"fake": ps},
		},
		Lookup:   func(string) (providers.Provider, error) { return p, nil },
		Observer: obs,
	})
}

// systemEcho replies with the system message so a prompt file controls the output.
func systemEcho(p *fake.Provider) {
	p.CompleteFn = func(_ int, req providers.Request) (providers.Response, error) {
		return providers.Response{Text: req.System}, nil
	}
}

type recorder struct {
	mu       sync.Mutex
	calls    []CallInfo
	failures []RunFailure
}

func (r *recorder) CallFinished(c CallInfo) { r.mu.Lock(); r.calls = append(r.calls, c); r.mu.Unlock() }
func (r *recorder) RunFailed(f RunFailure) {
	r.mu.Lock()
	r.failures = append(r.failures, f)
	r.mu.Unlock()
}

func TestRunCallCountsAndOrdering(t *testing.T) {
	dir := t.TempDir()
	prompt := writePrompt(t, dir, "p.txt", "OK")
	p := fake.New()
	systemEcho(p)
	s1 := suiteOf("zeta", prompt, 0.7,
		configs.Case{ID: "b", Input: "x", Assertions: exact("OK")},
		configs.Case{ID: "a", Input: "x", Assertions: exact("OK")})
	s2 := suiteOf("alpha", prompt, 0.7, configs.Case{ID: "c", Input: "x", Assertions: exact("OK")})

	e := newEngine(p, 3, nil, configs.ProviderSettings{MaxConcurrent: 4})
	res, err := e.Run(context.Background(), []SuitePair{pairOf(s1, prompt), pairOf(s2, prompt)})
	if err != nil {
		t.Fatal(err)
	}
	if want := 3 * 2 * 3; p.Calls() != want { // 3 cases x 2 variants x 3 runs
		t.Fatalf("calls = %d, want %d", p.Calls(), want)
	}
	var order []string
	for _, r := range res {
		order = append(order, r.Suite+"/"+r.Case)
		if r.Status != gating.StatusPass || r.Baseline.Runs != 3 || r.Candidate.Runs != 3 {
			t.Fatalf("unexpected result: %+v", r)
		}
	}
	want := []string{"alpha/c", "zeta/a", "zeta/b"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestRunDetectsRegressionBetweenVariants(t *testing.T) {
	dir := t.TempDir()
	good := writePrompt(t, dir, "good.txt", "GOOD")
	bad := writePrompt(t, dir, "bad.txt", "BAD")
	p := fake.New()
	systemEcho(p)
	cand := suiteOf("s", bad, 0, configs.Case{ID: "c", Input: "x", Assertions: exact("GOOD")})
	res, err := newEngine(p, 5, nil, configs.ProviderSettings{}).Run(context.Background(), []SuitePair{pairOf(cand, good)})
	if err != nil {
		t.Fatal(err)
	}
	r := res[0]
	if r.Status != gating.StatusRegressed || r.Baseline.Average != 100 || r.Candidate.Average != 0 || r.Drop != 100 {
		t.Fatalf("unexpected: %+v", r)
	}
	if gating.Decide(res) != gating.DecisionFail {
		t.Fatal("decision must be FAIL")
	}
}

func TestTemperatureZeroRunsOncePerVariant(t *testing.T) {
	dir := t.TempDir()
	prompt := writePrompt(t, dir, "p.txt", "OK")
	p := fake.New()
	systemEcho(p)
	s := suiteOf("s", prompt, 0,
		configs.Case{ID: "a", Input: "x", Assertions: exact("OK")},
		configs.Case{ID: "b", Input: "x", Assertions: exact("OK")})
	res, err := newEngine(p, 5, nil, configs.ProviderSettings{}).Run(context.Background(), []SuitePair{pairOf(s, prompt)})
	if err != nil {
		t.Fatal(err)
	}
	if p.Calls() != 4 { // 2 cases x 2 variants x 1 run (SC-004)
		t.Fatalf("calls = %d, want 4", p.Calls())
	}
	for _, r := range res {
		if r.Baseline.Runs != 1 || r.Candidate.Runs != 1 || r.Baseline.Spread != 0 {
			t.Fatalf("unexpected: %+v", r)
		}
	}
}

func TestMixedTemperaturesPlanIndependently(t *testing.T) {
	dir := t.TempDir()
	prompt := writePrompt(t, dir, "p.txt", "OK")
	p := fake.New()
	systemEcho(p)
	cand := suiteOf("s", prompt, 0.5, configs.Case{ID: "a", Input: "x", Assertions: exact("OK")})
	pair := pairOf(cand, prompt)
	pair.Baseline.Model.Temperature = fp(0)
	res, err := newEngine(p, 4, nil, configs.ProviderSettings{}).Run(context.Background(), []SuitePair{pair})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Baseline.Runs != 1 || res[0].Candidate.Runs != 4 || p.Calls() != 5 {
		t.Fatalf("unexpected: %+v calls=%d", res[0], p.Calls())
	}
}

func TestFailedRunsAreExcludedAndReported(t *testing.T) {
	dir := t.TempDir()
	prompt := writePrompt(t, dir, "p.txt", "OK")
	p := fake.New()
	var mu sync.Mutex
	n := 0
	p.CompleteFn = func(_ int, req providers.Request) (providers.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		if n == 1 { // exactly one failure overall
			return providers.Response{}, errors.New("timeout")
		}
		return providers.Response{Text: req.System}, nil
	}
	rec := &recorder{}
	s := suiteOf("s", prompt, 0.7, configs.Case{ID: "a", Input: "x", Assertions: exact("OK")})
	res, err := newEngine(p, 5, rec, configs.ProviderSettings{}).Run(context.Background(), []SuitePair{pairOf(s, prompt)})
	if err != nil {
		t.Fatal(err)
	}
	failed := res[0].Baseline.FailedRuns + res[0].Candidate.FailedRuns
	if failed != 1 || len(rec.failures) != 1 {
		t.Fatalf("failed=%d reported=%d", failed, len(rec.failures))
	}
	if res[0].Status != gating.StatusPass {
		t.Fatalf("one failure out of five must not flip status: %+v", res[0])
	}
	if rec.failures[0].Err == nil || rec.failures[0].Suite != "s" || rec.failures[0].Case != "a" {
		t.Fatalf("bad failure report: %+v", rec.failures[0])
	}
}

func TestAllRunsFailedIsError(t *testing.T) {
	dir := t.TempDir()
	prompt := writePrompt(t, dir, "p.txt", "OK")
	p := fake.New()
	p.CompleteFn = func(int, providers.Request) (providers.Response, error) {
		return providers.Response{}, errors.New("down")
	}
	s := suiteOf("s", prompt, 0.7, configs.Case{ID: "a", Input: "x", Assertions: exact("OK")})
	res, err := newEngine(p, 3, nil, configs.ProviderSettings{}).Run(context.Background(), []SuitePair{pairOf(s, prompt)})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Status != gating.StatusError || res[0].Baseline.FailedRuns != 3 {
		t.Fatalf("unexpected: %+v", res[0])
	}
	if gating.Decide(res) != gating.DecisionError {
		t.Fatal("decision must be ERROR")
	}
}

func TestMaxConcurrentIsRespected(t *testing.T) {
	dir := t.TempDir()
	prompt := writePrompt(t, dir, "p.txt", "OK")
	p := fake.New()
	p.Delay = 5 * time.Millisecond
	systemEcho(p)
	var cases []configs.Case
	for i := 0; i < 6; i++ {
		cases = append(cases, configs.Case{ID: fmt.Sprintf("c%d", i), Input: "x", Assertions: exact("OK")})
	}
	s := suiteOf("s", prompt, 0.7, cases...)
	if _, err := newEngine(p, 4, nil, configs.ProviderSettings{MaxConcurrent: 2}).Run(context.Background(), []SuitePair{pairOf(s, prompt)}); err != nil {
		t.Fatal(err)
	}
	if got := p.MaxInflight(); got > 2 || got < 2 {
		t.Fatalf("max in flight = %d, want exactly 2 (limit reached but not exceeded)", got)
	}
}

func TestContextCancellationAbortsRun(t *testing.T) {
	dir := t.TempDir()
	prompt := writePrompt(t, dir, "p.txt", "OK")
	p := fake.New()
	p.Delay = time.Second
	s := suiteOf("s", prompt, 0.7, configs.Case{ID: "a", Input: "x", Assertions: exact("OK")})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := newEngine(p, 3, nil, configs.ProviderSettings{}).Run(ctx, []SuitePair{pairOf(s, prompt)})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
}

func TestCriticalFlagReachesResult(t *testing.T) {
	dir := t.TempDir()
	prompt := writePrompt(t, dir, "p.txt", "OK")
	p := fake.New()
	systemEcho(p)
	s := suiteOf("s", prompt, 0,
		configs.Case{ID: "default", Input: "x", Assertions: exact("OK")},
		configs.Case{ID: "no", Input: "x", Critical: bp(false), Assertions: exact("OK")},
		configs.Case{ID: "yes", Input: "x", Critical: bp(true), Assertions: exact("OK")})
	res, err := newEngine(p, 1, nil, configs.ProviderSettings{}).Run(context.Background(), []SuitePair{pairOf(s, prompt)})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range res {
		got[r.Case] = r.Critical
	}
	if !got["default"] || got["no"] || !got["yes"] {
		t.Fatalf("critical flags wrong: %v", got)
	}
}

func TestPlaceholderPromptSentAsSingleUserMessage(t *testing.T) {
	dir := t.TempDir()
	prompt := writePrompt(t, dir, "p.txt", "Q: {{input}}")
	p := fake.New()
	s := suiteOf("s", prompt, 0, configs.Case{ID: "a", Input: "hello", Assertions: exact("Q: hello")})
	res, err := newEngine(p, 1, nil, configs.ProviderSettings{}).Run(context.Background(), []SuitePair{pairOf(s, prompt)})
	if err != nil {
		t.Fatal(err)
	}
	req := p.Requests()[0]
	if req.System != "" || req.User != "Q: hello" || res[0].Status != gating.StatusPass {
		t.Fatalf("req = %+v res = %+v", req, res[0])
	}
}

func TestObserverSeesEveryCall(t *testing.T) {
	dir := t.TempDir()
	prompt := writePrompt(t, dir, "p.txt", "OK")
	p := fake.New()
	systemEcho(p)
	rec := &recorder{}
	s := suiteOf("s", prompt, 0, configs.Case{ID: "a", Input: "x", Assertions: exact("OK")})
	if _, err := newEngine(p, 1, rec, configs.ProviderSettings{}).Run(context.Background(), []SuitePair{pairOf(s, prompt)}); err != nil {
		t.Fatal(err)
	}
	if len(rec.calls) != 2 {
		t.Fatalf("calls observed = %d, want 2", len(rec.calls))
	}
	seen := map[Variant]bool{}
	for _, c := range rec.calls {
		seen[c.Variant] = true
		if c.Kind != "complete" || c.Provider != "fake" || c.Model != "m" || c.Suite != "s" || c.Case != "a" {
			t.Fatalf("bad call info: %+v", c)
		}
	}
	if !seen[Baseline] || !seen[Candidate] {
		t.Fatalf("variants seen: %v", seen)
	}
}

func TestRunErrorsOnBadInputs(t *testing.T) {
	dir := t.TempDir()
	prompt := writePrompt(t, dir, "p.txt", "OK")
	p := fake.New()
	s := suiteOf("s", prompt, 0, configs.Case{ID: "a", Input: "x", Assertions: exact("OK")})
	e := newEngine(p, 1, nil, configs.ProviderSettings{})

	if _, err := e.Run(context.Background(), []SuitePair{pairOf(s, filepath.Join(dir, "missing.txt"))}); err == nil {
		t.Fatal("missing baseline prompt must error")
	}
	bad := s
	bad.Prompt = filepath.Join(dir, "missing.txt")
	if _, err := e.Run(context.Background(), []SuitePair{{Candidate: bad, Baseline: s}}); err == nil {
		t.Fatal("missing candidate prompt must error")
	}
	noProv := New(Config{
		Settings: configs.Settings{RunsPerCase: 1},
		Lookup:   func(string) (providers.Provider, error) { return nil, errors.New("no provider") },
	})
	if _, err := noProv.Run(context.Background(), []SuitePair{pairOf(s, prompt)}); err == nil {
		t.Fatal("unresolvable provider must error")
	}
	noEmbed := suiteOf("s", prompt, 0, configs.Case{ID: "a", Input: "x", Assertions: []configs.Assertion{{
		Type: configs.AssertSemantic, Expected: "e", Embedding: &configs.ModelRef{Provider: "missing", Model: "m"},
	}}})
	lookup := func(name string) (providers.Provider, error) {
		if name == "fake" {
			return p, nil
		}
		return nil, errors.New("unknown " + name)
	}
	e2 := New(Config{Settings: configs.Settings{RunsPerCase: 1}, Lookup: lookup})
	if _, err := e2.Run(context.Background(), []SuitePair{pairOf(noEmbed, prompt)}); err == nil {
		t.Fatal("assertion provider failure must error before running")
	}
}

func TestCandidateOnlySuiteIsNew(t *testing.T) {
	dir := t.TempDir()
	prompt := writePrompt(t, dir, "p.txt", "OK")
	p := fake.New()
	systemEcho(p)
	s := suiteOf("s", prompt, 0.7,
		configs.Case{ID: "a", Input: "x", Assertions: exact("OK")},
		configs.Case{ID: "b", Input: "x", Critical: bp(false), Assertions: exact("OK")})
	rec := &recorder{}
	res, err := newEngine(p, 3, rec, configs.ProviderSettings{}).Run(context.Background(), []SuitePair{{Candidate: s, NoBaseline: true}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Calls() != 6 { // 2 cases x 3 candidate runs, no baseline calls
		t.Fatalf("calls = %d, want 6", p.Calls())
	}
	for _, c := range rec.calls {
		if c.Variant != Candidate {
			t.Fatalf("baseline call made for a new suite: %+v", c)
		}
	}
	for _, r := range res {
		if r.Status != gating.StatusNew || r.Candidate.Runs != 3 || r.Baseline.Runs != 0 || r.Candidate.Average != 100 {
			t.Fatalf("unexpected: %+v", r)
		}
	}
	if res[1].Critical || !res[0].Critical {
		t.Fatalf("critical flags lost: %+v", res)
	}
	if gating.Decide(res) != gating.DecisionPass {
		t.Fatal("NEW cases must never fail the gate")
	}
}

func TestCandidateOnlyAllRunsFailedIsError(t *testing.T) {
	dir := t.TempDir()
	prompt := writePrompt(t, dir, "p.txt", "OK")
	p := fake.New()
	p.CompleteFn = func(int, providers.Request) (providers.Response, error) {
		return providers.Response{}, errors.New("down")
	}
	s := suiteOf("s", prompt, 0, configs.Case{ID: "a", Input: "x", Assertions: exact("OK")})
	res, err := newEngine(p, 2, nil, configs.ProviderSettings{}).Run(context.Background(), []SuitePair{{Candidate: s, NoBaseline: true}})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Status != gating.StatusError || gating.Decide(res) != gating.DecisionError {
		t.Fatalf("unexpected: %+v", res[0])
	}
}

func TestPreflightSkipsBaselineForNewSuites(t *testing.T) {
	dir := t.TempDir()
	good := writePrompt(t, dir, "good.txt", "x")
	s := suiteOf("s", good, 0, configs.Case{ID: "c", Input: "x", Assertions: exact("x")})
	s.Model.Provider = "gen"
	lookup, called := lookupFrom(map[string]providers.Provider{"gen": fake.New()})
	pair := SuitePair{Candidate: s, NoBaseline: true} // zero Baseline would otherwise fail on its prompt and provider
	if err := Preflight(PreflightInput{Settings: settingsWith("gen"), Pairs: []SuitePair{pair}, Lookup: lookup}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(*called) != 1 {
		t.Fatalf("lookups = %v, want only the candidate provider", *called)
	}
}

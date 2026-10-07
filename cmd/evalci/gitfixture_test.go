package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ajay-7105/EvalCI/internal/diffscope/gittest"
	"github.com/ajay-7105/EvalCI/internal/providers"
	"github.com/ajay-7105/EvalCI/internal/providers/fake"
	"github.com/ajay-7105/EvalCI/internal/storage"
	"github.com/ajay-7105/EvalCI/internal/storage/factory"
)

// fixture is a git repository with three suites (support, billing, legacy), each with its own
// prompt, model name (m-<suite>, so calls can be attributed), and exact-match case. The fake
// provider echoes the system prompt, which is also the expected answer, so everything passes
// until a prompt is changed to something else.
type fixture struct {
	t       *testing.T
	repo    *gittest.Repo
	histDir string
}

func settingsFor(histDir, extra string) string {
	return fmt.Sprintf(`runs_per_case: 2
providers:
  ollama:
    base_url: http://unused
prices:
  ollama/m-support: {input_per_million: 1, output_per_million: 2}
storage:
  driver: sqlite
  path: %s
%s`, filepath.Join(histDir, "history.db"), extra)
}

func suiteFile(name, prompt string) string {
	return fmt.Sprintf(`name: %s
prompt: %s
model: {provider: ollama, name: m-%s, temperature: 0}
cases:
  - {id: c1, input: "q", assertions: [{type: exact-match, expected: OK}]}
`, name, prompt, name)
}

// newFixture creates the repository on main, then switches to branch "feature". Files that
// evalci writes (the history database and the result JSON) live outside the repository.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, repo: gittest.New(t), histDir: t.TempDir()}
	t.Chdir(f.repo.Dir)
	f.repo.Write(".evalci.yaml", settingsFor(f.histDir, ""))
	for _, n := range []string{"support", "billing", "legacy"} {
		f.repo.Write("evals/"+n+".eval.yaml", suiteFile(n, "prompts/"+n+".txt"))
		f.repo.Write("prompts/"+n+".txt", "OK")
	}
	f.repo.Write("README.md", "docs")
	f.repo.Commit("base")
	f.repo.Branch("feature")
	return f
}

func (f *fixture) out() string { return filepath.Join(f.histDir, "result.json") }

// run executes `evalci run --base main` (plus extra args) with provider p.
func (f *fixture) run(p providers.Provider, extra ...string) outcome {
	f.t.Helper()
	args := append([]string{"--base", "main", "--output", f.out()}, extra...)
	return executeDeps(f.t, backgroundCtx(), depsFor(p), args...)
}

func (f *fixture) result() map[string]any {
	f.t.Helper()
	data, err := os.ReadFile(f.out())
	if err != nil {
		f.t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		f.t.Fatal(err)
	}
	return doc
}

// callsByModel counts generation calls per model name (m-<suite>).
func callsByModel(p *fake.Provider) map[string]int {
	m := map[string]int{}
	for _, r := range p.Requests() {
		m[r.Model]++
	}
	return m
}

// systemsByModel lists the distinct system prompts each model was called with.
func systemsByModel(p *fake.Provider) map[string][]string {
	seen := map[string]map[string]bool{}
	for _, r := range p.Requests() {
		if seen[r.Model] == nil {
			seen[r.Model] = map[string]bool{}
		}
		seen[r.Model][r.System] = true
	}
	out := map[string][]string{}
	for m, set := range seen {
		for sys := range set {
			out[m] = append(out[m], strings.TrimSpace(sys))
		}
	}
	return out
}

func okProvider() *fake.Provider {
	p := echoSystem()
	p.UsagePerCall = providers.Usage{InputTokens: 10, OutputTokens: 5, Known: true}
	return p
}

func failingProvider() *fake.Provider {
	p := fake.New()
	p.CompleteFn = func(int, providers.Request) (providers.Response, error) {
		return providers.Response{}, fmt.Errorf("provider down")
	}
	return p
}

func factoryOpen(path string) (storage.Store, error) {
	return factory.Open(context.Background(), storage.Config{Driver: "sqlite", Path: path})
}

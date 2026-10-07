package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ajay-7105/EvalCI/configs"
	"github.com/ajay-7105/EvalCI/internal/assertions"
	"github.com/ajay-7105/EvalCI/internal/providers"
	"github.com/ajay-7105/EvalCI/internal/providers/fake"
)

const settingsYAML = "runs_per_case: 2\nproviders:\n  ollama:\n    base_url: http://unused\n"

func suiteYAML(prompt string, temp string) string {
	return `name: s
prompt: ` + prompt + `
model:
  provider: ollama
  name: m
  temperature: ` + temp + `
cases:
  - id: c
    input: hi
    assertions:
      - type: exact-match
        expected: GOOD
`
}

func write(t *testing.T, rel, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rel, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// workspace lays out a repo (candidate) and baseline/ in a temp dir and chdirs into it.
func workspace(t *testing.T, settings, candPrompt, basePrompt, temp string) {
	t.Helper()
	t.Chdir(t.TempDir())
	write(t, ".evalci.yaml", settings)
	write(t, "prompts/p.txt", candPrompt)
	write(t, "evals/s.eval.yaml", suiteYAML("prompts/p.txt", temp))
	write(t, "baseline/prompts/p.txt", basePrompt)
	write(t, "baseline/evals/s.eval.yaml", suiteYAML("prompts/p.txt", temp))
}

type outcome struct {
	stdout, stderr string
	exit           int
	err            error
}

func depsFor(p providers.Provider) runDeps {
	deps := defaultDeps()
	deps.newLookup = func(configs.Settings) assertions.ProviderLookup {
		return func(string) (providers.Provider, error) { return p, nil }
	}
	return deps
}

func execute(t *testing.T, p *fake.Provider, args ...string) outcome {
	t.Helper()
	return executeDeps(t, context.Background(), depsFor(p), args...)
}

func executeDeps(t *testing.T, ctx context.Context, deps runDeps, args ...string) outcome {
	t.Helper()
	root := &cobra.Command{Use: "evalci", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newRunCmdWith(deps))
	var out, errb bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(append([]string{"run"}, args...))
	err := root.ExecuteContext(ctx)
	o := outcome{stdout: out.String(), stderr: errb.String(), err: err}
	var ee *exitError
	switch {
	case err == nil:
		o.exit = 0
	case errors.As(err, &ee):
		o.exit = ee.code
	default:
		o.exit = 2
	}
	return o
}

// echoSystem replies with the system message, so each prompt file decides the output.
func echoSystem() *fake.Provider {
	p := fake.New()
	p.CompleteFn = func(_ int, req providers.Request) (providers.Response, error) {
		return providers.Response{Text: req.System}, nil
	}
	return p
}

// alternating replies GOOD then BAD per distinct system prompt, giving each variant a
// 0/100 spread.
func alternating() *fake.Provider {
	p := fake.New()
	var mu sync.Mutex
	counts := map[string]int{}
	p.CompleteFn = func(_ int, req providers.Request) (providers.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		counts[req.System]++
		if counts[req.System]%2 == 1 {
			return providers.Response{Text: "GOOD"}, nil
		}
		return providers.Response{Text: "BAD"}, nil
	}
	return p
}

func readResult(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile("evalci-result.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestDecisionsAgreeAcrossStdoutJSONAndExitStatus(t *testing.T) {
	tests := []struct {
		name         string
		candPrompt   string
		basePrompt   string
		temp         string
		provider     func() *fake.Provider
		wantDecision string
		wantExit     int
	}{
		{"PASS", "GOOD", "GOOD", "0", echoSystem, "PASS", 0},
		{"FAIL", "BAD", "GOOD", "0", echoSystem, "FAIL", 1},
		{"WARN", "cand-prompt", "base-prompt", "0.7", alternating, "WARN", 0},
		{"ERROR", "GOOD", "GOOD", "0", func() *fake.Provider {
			p := fake.New()
			p.CompleteFn = func(int, providers.Request) (providers.Response, error) {
				return providers.Response{}, errors.New("provider down")
			}
			return p
		}, "ERROR", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace(t, settingsYAML, tt.candPrompt, tt.basePrompt, tt.temp)
			o := execute(t, tt.provider(), "--baseline-dir", "baseline")
			if o.exit != tt.wantExit {
				t.Fatalf("exit = %d, want %d (err %v)\nstdout:\n%s\nstderr:\n%s", o.exit, tt.wantExit, o.err, o.stdout, o.stderr)
			}
			if !strings.Contains(o.stdout, "Decision: "+tt.wantDecision) {
				t.Fatalf("stdout missing decision %s:\n%s", tt.wantDecision, o.stdout)
			}
			doc := readResult(t)
			if doc["decision"] != tt.wantDecision || doc["exit_code"] != float64(tt.wantExit) {
				t.Fatalf("json disagrees: %v", doc)
			}
			if tt.name == "ERROR" && !strings.Contains(o.stderr, "provider down") {
				t.Fatalf("failure reason must reach stderr:\n%s", o.stderr)
			}
		})
	}
}

func TestRunLeavesOnlyTheResultFile(t *testing.T) {
	workspace(t, settingsYAML, "GOOD", "GOOD", "0")
	before := listFiles(t)
	if o := execute(t, echoSystem(), "--baseline-dir", "baseline"); o.exit != 0 {
		t.Fatalf("exit = %d: %v", o.exit, o.err)
	}
	after := listFiles(t)
	var added []string
	for f := range after {
		if !before[f] {
			added = append(added, f)
		}
	}
	sort.Strings(added)
	if len(added) != 1 || added[0] != "evalci-result.json" {
		t.Fatalf("new files = %v, want only evalci-result.json (FR-018)", added)
	}
}

func listFiles(t *testing.T) map[string]bool {
	t.Helper()
	files := map[string]bool{}
	err := filepath.WalkDir(".", func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files[p] = true
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestCustomOutputPathAndVerbose(t *testing.T) {
	workspace(t, settingsYAML, "GOOD", "GOOD", "0")
	o := execute(t, echoSystem(), "--baseline-dir", "baseline", "--output", "out.json", "--verbose")
	if o.exit != 0 {
		t.Fatalf("exit = %d: %v", o.exit, o.err)
	}
	if _, err := os.Stat("out.json"); err != nil {
		t.Fatal("custom output path not honored")
	}
	if _, err := os.Stat("evalci-result.json"); err == nil {
		t.Fatal("default path must not be written when --output is set")
	}
	// temperature 0: exactly one generation call line per variant (SC-004)
	if n := strings.Count(o.stderr, "kind=complete"); n != 2 {
		t.Fatalf("verbose call lines = %d, want 2:\n%s", n, o.stderr)
	}
	if o2 := execute(t, echoSystem(), "--baseline-dir", "baseline", "--output", "out.json"); strings.Contains(o2.stderr, "kind=complete") {
		t.Fatalf("call lines must be off without --verbose:\n%s", o2.stderr)
	}
}

func TestPreflightFailuresMakeNoModelCalls(t *testing.T) {
	tests := []struct {
		name     string
		settings string
		prompt   string
		wantMsg  string
	}{
		{"unknown settings key", settingsYAML + "bogus_key: 1\n", "GOOD", "bogus_key"},
		{"unknown placeholder", settingsYAML, "hello {{nme}}", "{{nme}}"},
		{"unconfigured provider", "runs_per_case: 2\n", "GOOD", "not configured"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace(t, tt.settings, tt.prompt, "GOOD", "0")
			p := echoSystem()
			o := execute(t, p, "--baseline-dir", "baseline")
			if o.exit != 2 || o.err == nil || !strings.Contains(o.err.Error(), tt.wantMsg) {
				t.Fatalf("exit=%d err=%v, want exit 2 mentioning %q", o.exit, o.err, tt.wantMsg)
			}
			if p.Calls() != 0 {
				t.Fatalf("pre-flight failure made %d model calls", p.Calls())
			}
			if _, err := os.Stat("evalci-result.json"); err == nil {
				t.Fatal("no result file expected on pre-flight failure")
			}
		})
	}
}

func TestMissingBaselineDirAndNoSuites(t *testing.T) {
	workspace(t, settingsYAML, "GOOD", "GOOD", "0")
	if o := execute(t, echoSystem(), "--baseline-dir", "nope"); o.exit != 2 || o.err == nil {
		t.Fatalf("missing baseline dir: exit=%d err=%v", o.exit, o.err)
	}
	if o := execute(t, echoSystem(), "--baseline-dir", "baseline", "--evals", "empty"); o.exit != 2 {
		t.Fatalf("no suites: exit=%d err=%v", o.exit, o.err)
	}
	if o := execute(t, echoSystem()); o.exit != 2 {
		t.Fatalf("missing required flag must exit 2, got %d", o.exit)
	}
}

func TestRegistry(t *testing.T) {
	env := map[string]string{"OA": "key"}
	getenv := func(k string) string { return env[k] }
	settings := configs.Settings{Providers: map[string]configs.ProviderSettings{
		"openai":    {APIKeyEnv: "OA"},
		"anthropic": {APIKeyEnv: "MISSING"},
		"ollama":    {BaseURL: "http://localhost:11434"},
	}}
	lookup := newRegistry(settings, getenv)
	for _, name := range []string{"openai", "ollama"} {
		p, err := lookup(name)
		if err != nil || p == nil {
			t.Fatalf("%s: %v", name, err)
		}
		if p2, _ := lookup(name); p2 != p {
			t.Fatalf("%s must be cached", name)
		}
	}
	if _, err := lookup("anthropic"); err == nil || !strings.Contains(err.Error(), "MISSING") {
		t.Fatalf("anthropic err = %v", err)
	}
	if _, err := lookup("cohere"); err == nil {
		t.Fatal("unconfigured provider must error")
	}
	bad := newRegistry(configs.Settings{Providers: map[string]configs.ProviderSettings{
		"openai": {}, "ollama": {}, "weird": {},
	}}, getenv)
	for _, name := range []string{"openai", "ollama", "weird"} {
		if _, err := bad(name); err == nil {
			t.Fatalf("%s with incomplete settings must error", name)
		}
	}
	p, _ := lookup("ollama")
	if !p.SupportsEmbeddings() {
		t.Fatal("ollama supports embeddings")
	}
	a, err := newRegistry(configs.Settings{Providers: map[string]configs.ProviderSettings{"anthropic": {APIKeyEnv: "OA"}}}, getenv)("anthropic")
	if err != nil || a.SupportsEmbeddings() {
		t.Fatalf("anthropic must be built without embeddings: %v", err)
	}
}

func backgroundCtx() context.Context { return context.Background() }

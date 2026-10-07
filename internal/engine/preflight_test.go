package engine

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ajay-7105/EvalCI/configs"
	"github.com/ajay-7105/EvalCI/internal/assertions"
	"github.com/ajay-7105/EvalCI/internal/providers"
	"github.com/ajay-7105/EvalCI/internal/providers/fake"
)

func settingsWith(names ...string) configs.Settings {
	s := configs.Settings{RunsPerCase: 1, Providers: map[string]configs.ProviderSettings{}}
	for _, n := range names {
		s.Providers[n] = configs.ProviderSettings{MaxConcurrent: 4}
	}
	return s
}

func lookupFrom(m map[string]providers.Provider, failing ...string) (assertions.ProviderLookup, *[]string) {
	var called []string
	bad := map[string]bool{}
	for _, f := range failing {
		bad[f] = true
	}
	return func(name string) (providers.Provider, error) {
		called = append(called, name)
		if bad[name] {
			return nil, errors.New("environment variable KEY is not set")
		}
		p, ok := m[name]
		if !ok {
			return nil, errors.New("unknown provider")
		}
		return p, nil
	}, &called
}

func TestPreflight(t *testing.T) {
	dir := t.TempDir()
	good := writePrompt(t, dir, "good.txt", "hello {{input}}")
	badPH := writePrompt(t, dir, "bad.txt", "hello {{imput}}")
	missing := filepath.Join(dir, "missing.txt")

	semantic := configs.Assertion{Type: configs.AssertSemantic, Expected: "x", Embedding: &configs.ModelRef{Provider: "embedder", Model: "e"}}
	rubric := configs.Assertion{Type: configs.AssertRubric, Rubric: "r", Judge: &configs.ModelRef{Provider: "judge", Model: "j"}}

	build := func(prompt string, as ...configs.Assertion) SuitePair {
		s := suiteOf("s", prompt, 0, configs.Case{ID: "c", Input: "x", Assertions: as})
		s.Model.Provider = "gen"
		return pairOf(s, good)
	}

	embedOK, embedNo := fake.New(), fake.New()
	embedNo.Embeddings = false

	tests := []struct {
		name     string
		settings configs.Settings
		pair     SuitePair
		provs    map[string]providers.Provider
		failing  []string
		wantErrs []string // substrings that must each appear
		unused   []string // provider names that must NOT be looked up
	}{
		{name: "valid", settings: settingsWith("gen"), pair: build(good, exact("x")[0]),
			provs: map[string]providers.Provider{"gen": fake.New()}},
		{name: "missing candidate prompt", settings: settingsWith("gen"), pair: build(missing, exact("x")[0]),
			provs: map[string]providers.Provider{"gen": fake.New()}, wantErrs: []string{"missing.txt"}},
		{name: "unknown placeholder names file and line", settings: settingsWith("gen"), pair: build(badPH, exact("x")[0]),
			provs: map[string]providers.Provider{"gen": fake.New()}, wantErrs: []string{"bad.txt:1", "{{imput}}"}},
		{name: "unconfigured provider", settings: settingsWith(), pair: build(good, exact("x")[0]),
			provs: map[string]providers.Provider{"gen": fake.New()}, wantErrs: []string{`provider "gen" is not configured`}},
		{name: "missing credentials for a used provider", settings: settingsWith("gen"), pair: build(good, exact("x")[0]),
			provs: map[string]providers.Provider{}, failing: []string{"gen"}, wantErrs: []string{`provider "gen"`, "KEY"}},
		{name: "credentials of unused provider are not checked", settings: settingsWith("gen", "unused"), pair: build(good, exact("x")[0]),
			provs: map[string]providers.Provider{"gen": fake.New()}, failing: []string{"unused"}, unused: []string{"unused"}},
		{name: "embedding provider without embeddings rejected", settings: settingsWith("gen", "embedder"), pair: build(good, semantic),
			provs: map[string]providers.Provider{"gen": fake.New(), "embedder": embedNo}, wantErrs: []string{"does not support embeddings"}},
		{name: "embedding provider with embeddings accepted", settings: settingsWith("gen", "embedder"), pair: build(good, semantic),
			provs: map[string]providers.Provider{"gen": fake.New(), "embedder": embedOK}},
		{name: "judge provider unconfigured", settings: settingsWith("gen"), pair: build(good, rubric),
			provs: map[string]providers.Provider{"gen": fake.New()}, wantErrs: []string{`provider "judge" is not configured`}},
		{name: "judge needs no embeddings", settings: settingsWith("gen", "judge"), pair: build(good, rubric),
			provs: map[string]providers.Provider{"gen": fake.New(), "judge": embedNo}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup, called := lookupFrom(tt.provs, tt.failing...)
			err := Preflight(PreflightInput{Settings: tt.settings, Pairs: []SuitePair{tt.pair}, Lookup: lookup})
			if len(tt.wantErrs) == 0 {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected an error")
				}
				for _, w := range tt.wantErrs {
					if !strings.Contains(err.Error(), w) {
						t.Errorf("error %q missing %q", err, w)
					}
				}
			}
			for _, u := range tt.unused {
				for _, c := range *called {
					if c == u {
						t.Errorf("unused provider %q was looked up", u)
					}
				}
			}
		})
	}
}

func TestPreflightReportsAllProblems(t *testing.T) {
	dir := t.TempDir()
	good := writePrompt(t, dir, "good.txt", "x")
	s := suiteOf("s", filepath.Join(dir, "gone.txt"), 0, configs.Case{ID: "c", Input: "x", Assertions: exact("x")})
	s.Model.Provider = "gen"
	lookup, _ := lookupFrom(nil)
	err := Preflight(PreflightInput{Settings: settingsWith(), Pairs: []SuitePair{pairOf(s, good)}, Lookup: lookup})
	if err == nil || !strings.Contains(err.Error(), "gone.txt") || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected both problems, got %v", err)
	}
}

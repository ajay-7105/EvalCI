package configs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, rel, content string) string {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadSettings(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string // substring; empty means success
		check   func(t *testing.T, s Settings)
	}{
		{
			name: "defaults applied",
			yaml: "providers:\n  ollama:\n    base_url: http://localhost:11434\n",
			check: func(t *testing.T, s Settings) {
				if s.RunsPerCase != 5 || s.MinDropThreshold != 5.0 || s.MaxSpreadBeforeUnreliable != 20.0 {
					t.Fatalf("defaults wrong: %+v", s)
				}
				if s.Providers["ollama"].MaxConcurrent != 4 {
					t.Fatalf("max_concurrent default = %d", s.Providers["ollama"].MaxConcurrent)
				}
			},
		},
		{
			name: "explicit zero threshold accepted",
			yaml: "min_drop_threshold: 0\nmax_spread_before_unreliable: 0\n",
			check: func(t *testing.T, s Settings) {
				if s.MinDropThreshold != 0 || s.MaxSpreadBeforeUnreliable != 0 {
					t.Fatalf("zero thresholds overridden: %+v", s)
				}
			},
		},
		{name: "unknown key rejected", yaml: "runs_per_cse: 3\n", wantErr: "runs_per_cse"},
		{name: "runs_per_case zero rejected", yaml: "runs_per_case: 0\n", wantErr: "runs_per_case"},
		{name: "negative drop rejected", yaml: "min_drop_threshold: -1\n", wantErr: "min_drop_threshold"},
		{name: "storage driver is not validated here (the storage factory owns the set)", yaml: "storage:\n  driver: mysql\n"},
		{
			name: "storage path defaults when a driver is set",
			yaml: "storage:\n  driver: sqlite\n",
			check: func(t *testing.T, s Settings) {
				if s.Storage.Path != ".evalci/evalci.db" {
					t.Fatalf("path = %q", s.Storage.Path)
				}
			},
		},
		{
			name: "storage path honoured and no driver means no default path",
			yaml: "storage:\n  driver: sqlite\n  path: /tmp/h.db\n",
			check: func(t *testing.T, s Settings) {
				if s.Storage.Path != "/tmp/h.db" {
					t.Fatalf("path = %q", s.Storage.Path)
				}
			},
		},
		{
			name: "no storage configured leaves driver and path empty",
			yaml: "runs_per_case: 2\n",
			check: func(t *testing.T, s Settings) {
				if s.Storage.Driver != "" || s.Storage.Path != "" {
					t.Fatalf("storage = %+v", s.Storage)
				}
			},
		},
		{
			name: "currency defaults to USD",
			yaml: "runs_per_case: 2\n",
			check: func(t *testing.T, s Settings) {
				if s.Currency != "USD" || len(s.Prices) != 0 {
					t.Fatalf("currency/prices = %q %v", s.Currency, s.Prices)
				}
			},
		},
		{
			name: "valid prices and currency",
			yaml: "currency: EUR\nprices:\n  openai/gpt-4o-mini:\n    input_per_million: 0.15\n    output_per_million: 0.6\n  ollama/llama3.2:1b:\n    input_per_million: 0\n    output_per_million: 0\n",
			check: func(t *testing.T, s Settings) {
				p, ok := s.Prices["openai/gpt-4o-mini"]
				if s.Currency != "EUR" || !ok || p.InputPerMillion != 0.15 || p.OutputPerMillion != 0.6 || len(s.Prices) != 2 {
					t.Fatalf("settings = %+v", s)
				}
			},
		},
		{name: "price key without provider", yaml: "prices:\n  gpt-4o:\n    input_per_million: 1\n    output_per_million: 1\n", wantErr: "prices.gpt-4o"},
		{name: "price key with unknown provider", yaml: "prices:\n  cohere/x:\n    input_per_million: 1\n    output_per_million: 1\n", wantErr: "prices.cohere/x"},
		{name: "price key with empty model", yaml: "prices:\n  openai/:\n    input_per_million: 1\n    output_per_million: 1\n", wantErr: "prices.openai/"},
		{name: "negative input price", yaml: "prices:\n  openai/m:\n    input_per_million: -1\n    output_per_million: 1\n", wantErr: "input_per_million"},
		{name: "negative output price", yaml: "prices:\n  openai/m:\n    input_per_million: 1\n    output_per_million: -1\n", wantErr: "output_per_million"},
		{name: "missing output price", yaml: "prices:\n  openai/m:\n    input_per_million: 1\n", wantErr: "output_per_million"},
		{name: "missing input price", yaml: "prices:\n  openai/m:\n    output_per_million: 1\n", wantErr: "input_per_million"},
		{name: "unknown price key", yaml: "prices:\n  openai/m:\n    input_per_million: 1\n    output_per_million: 1\n    per_call: 3\n", wantErr: "per_call"},
		{name: "unknown provider", yaml: "providers:\n  cohere: {}\n", wantErr: "providers.cohere"},
		{name: "unknown provider key", yaml: "providers:\n  openai:\n    key: x\n", wantErr: "key"},
		{name: "empty file", yaml: "", wantErr: "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := write(t, t.TempDir(), ".evalci.yaml", tt.yaml)
			s, err := LoadSettings(p)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				if !strings.Contains(err.Error(), ".evalci.yaml") {
					t.Fatalf("error does not name the file: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.check != nil {
				tt.check(t, s)
			}
		})
	}
}

const goodSuite = `name: support
prompt: prompts/support.txt
model:
  provider: ollama
  name: llama3
  temperature: 0
cases:
  - id: c1
    input: hello
    assertions:
      - type: exact-match
        expected: hi
`

func TestLoadSuites(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		wantErr string
		check   func(t *testing.T, s []Suite)
	}{
		{
			name:  "valid with defaults",
			files: map[string]string{"evals/a.eval.yaml": goodSuite},
			check: func(t *testing.T, s []Suite) {
				c := s[0].Cases[0]
				if !c.IsCritical() || !c.Assertions[0].IsCaseSensitive() {
					t.Fatal("critical and case_sensitive must default to true")
				}
				if !s[0].Model.SingleRun() {
					t.Fatal("temperature 0 must be single-run")
				}
				if !strings.HasSuffix(s[0].Prompt, filepath.Join("prompts", "support.txt")) {
					t.Fatalf("prompt not resolved: %s", s[0].Prompt)
				}
			},
		},
		{
			name: "critical false and case_sensitive false honoured",
			files: map[string]string{"evals/a.eval.yaml": strings.Replace(
				strings.Replace(goodSuite, "    input: hello\n", "    input: hello\n    critical: false\n", 1),
				"        expected: hi\n", "        expected: hi\n        case_sensitive: false\n", 1)},
			check: func(t *testing.T, s []Suite) {
				if s[0].Cases[0].IsCritical() || s[0].Cases[0].Assertions[0].IsCaseSensitive() {
					t.Fatal("explicit false was lost")
				}
			},
		},
		{
			name:    "missing temperature rejected",
			files:   map[string]string{"evals/a.eval.yaml": strings.Replace(goodSuite, "  temperature: 0\n", "", 1)},
			wantErr: "model.temperature",
		},
		{
			name:  "temperature nonzero is not single run",
			files: map[string]string{"evals/a.eval.yaml": strings.Replace(goodSuite, "temperature: 0", "temperature: 0.7", 1)},
			check: func(t *testing.T, s []Suite) {
				if s[0].Model.SingleRun() {
					t.Fatal("0.7 must not be single run")
				}
			},
		},
		{
			name:    "unknown key",
			files:   map[string]string{"evals/a.eval.yaml": goodSuite + "extra: 1\n"},
			wantErr: "extra",
		},
		{
			name:    "bad provider",
			files:   map[string]string{"evals/a.eval.yaml": strings.Replace(goodSuite, "ollama", "cohere", 1)},
			wantErr: "model.provider",
		},
		{
			name:    "no cases",
			files:   map[string]string{"evals/a.eval.yaml": "name: s\nprompt: p\nmodel:\n  provider: ollama\n  name: m\n  temperature: 0\ncases: []\n"},
			wantErr: "cases",
		},
		{
			name:    "no assertions",
			files:   map[string]string{"evals/a.eval.yaml": strings.Replace(goodSuite, "    assertions:\n      - type: exact-match\n        expected: hi\n", "    assertions: []\n", 1)},
			wantErr: "cases[0].assertions",
		},
		{
			name: "duplicate case id",
			files: map[string]string{"evals/a.eval.yaml": goodSuite + `  - id: c1
    input: again
    assertions:
      - type: exact-match
        expected: x
`},
			wantErr: "duplicate case id",
		},
		{
			name: "duplicate suite name",
			files: map[string]string{
				"evals/a.eval.yaml": goodSuite,
				"evals/b.eval.yaml": goodSuite,
			},
			wantErr: "duplicate suite name",
		},
		{
			name:    "semantic needs embedding",
			files:   map[string]string{"evals/a.eval.yaml": strings.Replace(goodSuite, "type: exact-match", "type: semantic-similarity", 1)},
			wantErr: "embedding",
		},
		{
			name: "rubric needs judge",
			files: map[string]string{"evals/a.eval.yaml": strings.Replace(
				strings.Replace(goodSuite, "type: exact-match", "type: llm-rubric", 1), "expected: hi", "rubric: be nice", 1)},
			wantErr: "judge",
		},
		{
			name:    "unknown assertion type",
			files:   map[string]string{"evals/a.eval.yaml": strings.Replace(goodSuite, "exact-match", "regex", 1)},
			wantErr: "assertions[0].type",
		},
		{name: "no suites", files: map[string]string{}, wantErr: "no eval suites"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, c := range tt.files {
				write(t, dir, rel, c)
			}
			if len(tt.files) == 0 {
				if err := os.MkdirAll(filepath.Join(dir, "evals"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			s, err := LoadSuites(dir, "evals")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				if tt.name == "no suites" && !errors.Is(err, ErrNoSuites) {
					t.Fatalf("want ErrNoSuites, got %v", err)
				}
				if tt.name != "no suites" && !strings.Contains(err.Error(), ".eval.yaml") {
					t.Fatalf("error does not name the file: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.check != nil {
				tt.check(t, s)
			}
		})
	}
}

func TestParseSettingsFromBytesNamesTheSuppliedName(t *testing.T) {
	_, err := ParseSettings("main:.evalci.yaml", []byte("bogus: 1\n"))
	if err == nil || !strings.Contains(err.Error(), "main:.evalci.yaml") || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("err = %v", err)
	}
	s, err := ParseSettings("x", []byte("runs_per_case: 7\n"))
	if err != nil || s.RunsPerCase != 7 {
		t.Fatalf("s=%+v err=%v", s, err)
	}
}

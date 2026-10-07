package diffscope

import (
	"testing"

	"github.com/ajay-7105/EvalCI/configs"
)

func parse(t *testing.T, yaml string) *configs.Settings {
	t.Helper()
	s, err := configs.ParseSettings("test.yaml", []byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return &s
}

const baseSettings = `runs_per_case: 5
min_drop_threshold: 5
max_spread_before_unreliable: 20
providers:
  ollama:
    base_url: http://localhost:11434
    max_concurrent: 2
  openai:
    api_key_env: OPENAI_API_KEY
prices:
  openai/gpt-4o-mini: {input_per_million: 0.15, output_per_million: 0.6}
currency: USD
storage: {driver: sqlite}
`

func TestRunAffectingChanged(t *testing.T) {
	tests := []struct {
		name string
		head string
		want bool
	}{
		{"identical", baseSettings, false},
		{"runs_per_case", replace(baseSettings, "runs_per_case: 5", "runs_per_case: 3"), true},
		{"min_drop_threshold", replace(baseSettings, "min_drop_threshold: 5", "min_drop_threshold: 2"), true},
		{"max_spread", replace(baseSettings, "max_spread_before_unreliable: 20", "max_spread_before_unreliable: 30"), true},
		{"provider base_url", replace(baseSettings, "http://localhost:11434", "http://other:11434"), true},
		{"provider api_key_env", replace(baseSettings, "OPENAI_API_KEY", "OTHER_KEY"), true},
		{"provider max_concurrent", replace(baseSettings, "max_concurrent: 2", "max_concurrent: 8"), true},
		{"provider requests_per_minute added", replace(baseSettings, "max_concurrent: 2", "max_concurrent: 2\n    requests_per_minute: 30"), true},
		{"provider removed", replace(baseSettings, "  openai:\n    api_key_env: OPENAI_API_KEY\n", ""), true},
		{"price only", replace(baseSettings, "input_per_million: 0.15", "input_per_million: 0.30"), false},
		{"price added", replace(baseSettings, "prices:\n", "prices:\n  ollama/x: {input_per_million: 0, output_per_million: 0}\n"), false},
		{"currency only", replace(baseSettings, "currency: USD", "currency: EUR"), false},
		{"storage only", replace(baseSettings, "storage: {driver: sqlite}", "storage: {driver: sqlite, path: /elsewhere.db}"), false},
		{"storage removed", replace(baseSettings, "storage: {driver: sqlite}\n", ""), false},
		{"comments and key order", "# a comment\nproviders:\n  openai:\n    api_key_env: OPENAI_API_KEY\n  ollama:\n    max_concurrent: 2\n    base_url: http://localhost:11434\nmax_spread_before_unreliable: 20\nmin_drop_threshold: 5\nruns_per_case: 5\n", false},
		{"explicit default equals omitted key", replace(baseSettings, "runs_per_case: 5\n", ""), false},
	}
	base := parse(t, baseSettings)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RunAffectingChanged(base, parse(t, tt.head)); got != tt.want {
				t.Fatalf("RunAffectingChanged = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunAffectingChangedWhenFilesAppearOrDisappear(t *testing.T) {
	s := parse(t, baseSettings)
	if !RunAffectingChanged(nil, s) {
		t.Fatal("settings file added must count as changed")
	}
	if !RunAffectingChanged(s, nil) {
		t.Fatal("settings file deleted must count as changed")
	}
	if RunAffectingChanged(nil, nil) {
		t.Fatal("no settings file on either side is no change")
	}
}

func replace(s, old, new string) string {
	out := ""
	for i := 0; i < len(s); {
		if len(s)-i >= len(old) && s[i:i+len(old)] == old {
			out += new
			i += len(old)
			continue
		}
		out += string(s[i])
		i++
	}
	return out
}

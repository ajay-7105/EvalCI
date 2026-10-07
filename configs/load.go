package configs

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// FieldError is a validation failure that names the file and field.
type FieldError struct {
	File  string
	Field string
	Msg   string
}

// Error implements error.
func (e *FieldError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("%s: %s", e.File, e.Msg)
	}
	return fmt.Sprintf("%s: %s: %s", e.File, e.Field, e.Msg)
}

type rawPrice struct {
	InputPerMillion  *float64 `yaml:"input_per_million"`
	OutputPerMillion *float64 `yaml:"output_per_million"`
}

type rawSettings struct {
	RunsPerCase               *int                        `yaml:"runs_per_case"`
	MinDropThreshold          *float64                    `yaml:"min_drop_threshold"`
	MaxSpreadBeforeUnreliable *float64                    `yaml:"max_spread_before_unreliable"`
	Providers                 map[string]ProviderSettings `yaml:"providers"`
	Storage                   StorageSettings             `yaml:"storage"`
	Prices                    map[string]rawPrice         `yaml:"prices"`
	Currency                  string                      `yaml:"currency"`
}

// LoadSettings reads and validates the settings file at path, applying defaults.
func LoadSettings(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, fmt.Errorf("reading %s: %w", path, err)
	}
	return ParseSettings(path, data)
}

// ParseSettings validates settings from raw YAML bytes, applying defaults. name is used in
// error messages (a path, or for a base-ref blob something like "main:.evalci.yaml").
func ParseSettings(path string, data []byte) (Settings, error) {
	var raw rawSettings
	if err := decodeBytes(path, data, &raw); err != nil {
		return Settings{}, err
	}
	s := Settings{
		RunsPerCase:               DefaultRunsPerCase,
		MinDropThreshold:          DefaultMinDrop,
		MaxSpreadBeforeUnreliable: DefaultMaxSpread,
		Providers:                 map[string]ProviderSettings{},
		Storage:                   raw.Storage,
		Prices:                    map[string]Price{},
		Currency:                  DefaultCurrency,
	}
	if raw.Currency != "" {
		s.Currency = raw.Currency
	}
	if s.Storage.Driver != "" && s.Storage.Path == "" {
		s.Storage.Path = DefaultStoragePath
	}
	for key, p := range raw.Prices {
		field := "prices." + key
		provider, model, ok := strings.Cut(key, "/")
		if !ok || model == "" || !validProvider(provider) {
			return Settings{}, &FieldError{path, field, `key must be "<provider>/<model>" with provider openai, anthropic, or ollama`}
		}
		if p.InputPerMillion == nil {
			return Settings{}, &FieldError{path, field + ".input_per_million", "is required"}
		}
		if p.OutputPerMillion == nil {
			return Settings{}, &FieldError{path, field + ".output_per_million", "is required"}
		}
		if *p.InputPerMillion < 0 {
			return Settings{}, &FieldError{path, field + ".input_per_million", "must be >= 0"}
		}
		if *p.OutputPerMillion < 0 {
			return Settings{}, &FieldError{path, field + ".output_per_million", "must be >= 0"}
		}
		s.Prices[key] = Price{InputPerMillion: *p.InputPerMillion, OutputPerMillion: *p.OutputPerMillion}
	}
	if raw.RunsPerCase != nil {
		if *raw.RunsPerCase < 1 {
			return Settings{}, &FieldError{path, "runs_per_case", "must be >= 1"}
		}
		s.RunsPerCase = *raw.RunsPerCase
	}
	if raw.MinDropThreshold != nil {
		if *raw.MinDropThreshold < 0 {
			return Settings{}, &FieldError{path, "min_drop_threshold", "must be >= 0"}
		}
		s.MinDropThreshold = *raw.MinDropThreshold
	}
	if raw.MaxSpreadBeforeUnreliable != nil {
		if *raw.MaxSpreadBeforeUnreliable < 0 {
			return Settings{}, &FieldError{path, "max_spread_before_unreliable", "must be >= 0"}
		}
		s.MaxSpreadBeforeUnreliable = *raw.MaxSpreadBeforeUnreliable
	}
	for name, p := range raw.Providers {
		field := "providers." + name
		if !validProvider(name) {
			return Settings{}, &FieldError{path, field, "unknown provider (want openai, anthropic, or ollama)"}
		}
		if p.MaxConcurrent < 0 {
			return Settings{}, &FieldError{path, field + ".max_concurrent", "must be >= 0"}
		}
		if p.RequestsPerMinute < 0 {
			return Settings{}, &FieldError{path, field + ".requests_per_minute", "must be >= 0"}
		}
		if p.MaxConcurrent == 0 {
			p.MaxConcurrent = DefaultMaxConcurrent
		}
		s.Providers[name] = p
	}
	return s, nil
}

// LoadSuites loads every <root>/<evalsDir>/*.eval.yaml suite, sorted by file name.
// Prompt paths in the returned suites are resolved relative to root.
func LoadSuites(root, evalsDir string) ([]Suite, error) {
	files, err := filepath.Glob(filepath.Join(root, evalsDir, "*.eval.yaml"))
	if err != nil {
		return nil, fmt.Errorf("globbing suites: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: %w", filepath.Join(root, evalsDir), ErrNoSuites)
	}
	sort.Strings(files)
	suites := make([]Suite, 0, len(files))
	seen := map[string]string{}
	for _, f := range files {
		var s Suite
		if err := decodeStrict(f, &s); err != nil {
			return nil, err
		}
		s.File = f
		if err := validateSuite(&s, root); err != nil {
			return nil, err
		}
		if prev, dup := seen[s.Name]; dup {
			return nil, &FieldError{f, "name", fmt.Sprintf("duplicate suite name %q (also in %s)", s.Name, prev)}
		}
		seen[s.Name] = f
		suites = append(suites, s)
	}
	return suites, nil
}

func validateSuite(s *Suite, root string) error {
	fail := func(field, msg string) error { return &FieldError{s.File, field, msg} }
	if s.Name == "" {
		return fail("name", "is required")
	}
	if s.Prompt == "" {
		return fail("prompt", "is required")
	}
	s.PromptRel = filepath.ToSlash(filepath.Clean(s.Prompt))
	s.Prompt = filepath.Join(root, s.Prompt)
	if !validProvider(s.Model.Provider) {
		return fail("model.provider", fmt.Sprintf("must be openai, anthropic, or ollama, got %q", s.Model.Provider))
	}
	if s.Model.Name == "" {
		return fail("model.name", "is required")
	}
	if s.Model.Temperature == nil {
		return fail("model.temperature", "is required (use 0 for deterministic single runs)")
	}
	if len(s.Cases) == 0 {
		return fail("cases", "must contain at least one case")
	}
	ids := map[string]bool{}
	for i, c := range s.Cases {
		cf := fmt.Sprintf("cases[%d]", i)
		if c.ID == "" {
			return fail(cf+".id", "is required")
		}
		if ids[c.ID] {
			return fail(cf+".id", fmt.Sprintf("duplicate case id %q", c.ID))
		}
		ids[c.ID] = true
		if len(c.Assertions) == 0 {
			return fail(cf+".assertions", "must contain at least one assertion")
		}
		for j, a := range c.Assertions {
			if err := validateAssertion(a); err != nil {
				return fail(fmt.Sprintf("%s.assertions[%d]", cf, j)+err.field, err.msg)
			}
		}
	}
	return nil
}

type assertionError struct{ field, msg string }

func (e *assertionError) Error() string { return e.field + ": " + e.msg }

func validateAssertion(a Assertion) *assertionError {
	switch a.Type {
	case AssertExactMatch:
		if a.Expected == "" {
			return &assertionError{".expected", "is required"}
		}
		if a.Embedding != nil || a.Judge != nil || a.Rubric != "" {
			return &assertionError{"", "exact-match only accepts expected and case_sensitive"}
		}
	case AssertSemantic:
		if a.Expected == "" {
			return &assertionError{".expected", "is required"}
		}
		if a.Embedding == nil {
			return &assertionError{".embedding", "is required (no default embedding model)"}
		}
		if err := checkRef(a.Embedding, ".embedding"); err != nil {
			return err
		}
		if a.Judge != nil || a.Rubric != "" || a.CaseSensitive != nil {
			return &assertionError{"", "semantic-similarity only accepts expected and embedding"}
		}
	case AssertRubric:
		if a.Rubric == "" {
			return &assertionError{".rubric", "is required"}
		}
		if a.Judge == nil {
			return &assertionError{".judge", "is required (no default judge model)"}
		}
		if err := checkRef(a.Judge, ".judge"); err != nil {
			return err
		}
		if a.Expected != "" || a.Embedding != nil || a.CaseSensitive != nil {
			return &assertionError{"", "llm-rubric only accepts rubric and judge"}
		}
	default:
		return &assertionError{".type", fmt.Sprintf("must be exact-match, semantic-similarity, or llm-rubric, got %q", a.Type)}
	}
	return nil
}

func checkRef(r *ModelRef, field string) *assertionError {
	if !validProvider(r.Provider) {
		return &assertionError{field + ".provider", fmt.Sprintf("must be openai, anthropic, or ollama, got %q", r.Provider)}
	}
	if r.Model == "" {
		return &assertionError{field + ".model", "is required"}
	}
	return nil
}

func validProvider(name string) bool {
	switch name {
	case ProviderOpenAI, ProviderAnthropic, ProviderOllama:
		return true
	}
	return false
}

func decodeStrict(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	return decodeBytes(path, data, out)
}

func decodeBytes(path string, data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return &FieldError{File: path, Msg: "file is empty"}
		}
		return &FieldError{File: path, Msg: cleanYAMLError(err)}
	}
	return nil
}

// cleanYAMLError strips yaml.v3's internal Go type names so messages read as config errors.
func cleanYAMLError(err error) string {
	msg := strings.TrimPrefix(err.Error(), "yaml: unmarshal errors:\n  ")
	msg = strings.TrimPrefix(msg, "yaml: ")
	lines := strings.Split(msg, "\n")
	for i, l := range lines {
		l = strings.TrimSpace(l)
		if idx := strings.Index(l, " in type "); idx >= 0 {
			l = l[:idx]
		}
		lines[i] = l
	}
	return strings.Join(lines, "; ")
}

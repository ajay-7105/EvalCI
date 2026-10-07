// Package configs parses and strictly validates .evalci.yaml and *.eval.yaml files.
package configs

import "errors"

// Provider names accepted in configuration files.
const (
	ProviderOpenAI    = "openai"
	ProviderAnthropic = "anthropic"
	ProviderOllama    = "ollama"
)

// Assertion type names accepted in suite files.
const (
	AssertExactMatch = "exact-match"
	AssertSemantic   = "semantic-similarity"
	AssertRubric     = "llm-rubric"
)

// Defaults applied when .evalci.yaml omits a setting.
const (
	DefaultRunsPerCase    = 5
	DefaultMinDrop        = 5.0
	DefaultMaxSpread      = 20.0
	DefaultMaxConcurrent  = 4
	DefaultSettingsPath   = ".evalci.yaml"
	DefaultEvalsDirectory = "evals"
	DefaultCurrency       = "USD"
	DefaultStoragePath    = ".evalci/evalci.db"
)

// ErrNoSuites is returned when no *.eval.yaml files are found.
var ErrNoSuites = errors.New("no eval suites found")

// Settings holds repo-level settings with all defaults applied.
type Settings struct {
	RunsPerCase               int
	MinDropThreshold          float64
	MaxSpreadBeforeUnreliable float64
	Providers                 map[string]ProviderSettings
	Storage                   StorageSettings
	// Prices are per-model prices keyed "<provider>/<model>"; models without one have unknown cost.
	Prices map[string]Price
	// Currency is an informational label for Prices (default USD).
	Currency string
}

// Price is a model's price per million tokens, in the settings' currency.
type Price struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

// ProviderSettings configures one provider.
type ProviderSettings struct {
	// APIKeyEnv names the environment variable holding the API key.
	APIKeyEnv string `yaml:"api_key_env"`
	// BaseURL overrides the provider endpoint (required for ollama).
	BaseURL string `yaml:"base_url"`
	// MaxConcurrent caps in-flight requests; defaults to DefaultMaxConcurrent.
	MaxConcurrent int `yaml:"max_concurrent"`
	// RequestsPerMinute paces request starts; 0 means unlimited.
	RequestsPerMinute int `yaml:"requests_per_minute"`
}

// StorageSettings selects where run history is saved. An empty Driver means nothing is saved.
type StorageSettings struct {
	// Driver names the backend; the storage factory decides which are available.
	Driver string `yaml:"driver"`
	// Path is the history file for file-based drivers (default DefaultStoragePath).
	Path string `yaml:"path"`
}

// Suite is one eval suite file.
type Suite struct {
	// File is the path the suite was loaded from; it is not part of the YAML.
	File string `yaml:"-"`
	Name string `yaml:"name"`
	// Prompt is the prompt path after loading, resolved under the load root.
	Prompt string `yaml:"prompt"`
	// PromptRel is the prompt path as written in the suite (slash-separated, cleaned),
	// relative to the load root; it is not part of the YAML.
	PromptRel string `yaml:"-"`
	Model     Model  `yaml:"model"`
	Cases     []Case `yaml:"cases"`
}

// Model selects the provider and model used to generate outputs.
type Model struct {
	Provider string `yaml:"provider"`
	Name     string `yaml:"name"`
	// Temperature is a pointer so a missing key can be told apart from an explicit 0.
	Temperature *float64 `yaml:"temperature"`
	MaxTokens   int      `yaml:"max_tokens"`
}

// SingleRun reports whether the model is deterministic (temperature 0) and so runs once.
func (m Model) SingleRun() bool {
	return m.Temperature != nil && *m.Temperature == 0
}

// Case is one test input with its assertions.
type Case struct {
	ID    string `yaml:"id"`
	Input string `yaml:"input"`
	// Critical is a pointer so an absent key defaults to true.
	Critical   *bool       `yaml:"critical"`
	Assertions []Assertion `yaml:"assertions"`
}

// IsCritical reports whether a regression in the case may fail the suite (default true).
func (c Case) IsCritical() bool {
	return c.Critical == nil || *c.Critical
}

// Assertion is one check on a model output.
type Assertion struct {
	Type     string `yaml:"type"`
	Expected string `yaml:"expected"`
	Rubric   string `yaml:"rubric"`
	// CaseSensitive is a pointer so an absent key defaults to true.
	CaseSensitive *bool     `yaml:"case_sensitive"`
	Embedding     *ModelRef `yaml:"embedding"`
	Judge         *ModelRef `yaml:"judge"`
}

// IsCaseSensitive reports whether exact-match compares case-sensitively (default true).
func (a Assertion) IsCaseSensitive() bool {
	return a.CaseSensitive == nil || *a.CaseSensitive
}

// ModelRef names a provider and model used by a semantic or rubric assertion.
type ModelRef struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

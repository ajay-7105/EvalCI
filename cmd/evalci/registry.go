package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ajay-7105/EvalCI/configs"
	"github.com/ajay-7105/EvalCI/internal/assertions"
	"github.com/ajay-7105/EvalCI/internal/providers"
	"github.com/ajay-7105/EvalCI/internal/providers/anthropic"
	"github.com/ajay-7105/EvalCI/internal/providers/ollama"
	"github.com/ajay-7105/EvalCI/internal/providers/openai"
)

const httpTimeout = 2 * time.Minute

// newRegistry returns a lookup that builds each provider on first use from the settings and
// the environment. It is the only place that knows the concrete adapters, so core packages
// never name a vendor.
func newRegistry(settings configs.Settings, getenv func(string) string) assertions.ProviderLookup {
	var mu sync.Mutex
	cache := map[string]providers.Provider{}
	client := &http.Client{Timeout: httpTimeout}
	return func(name string) (providers.Provider, error) {
		mu.Lock()
		defer mu.Unlock()
		if p, ok := cache[name]; ok {
			return p, nil
		}
		ps, ok := settings.Providers[name]
		if !ok {
			return nil, fmt.Errorf("provider %q is not configured in .evalci.yaml", name)
		}
		p, err := buildProvider(name, ps, client, getenv)
		if err != nil {
			return nil, err
		}
		cache[name] = p
		return p, nil
	}
}

func buildProvider(name string, ps configs.ProviderSettings, c *http.Client, getenv func(string) string) (providers.Provider, error) {
	retry := providers.NewRetrier()
	switch name {
	case configs.ProviderOllama:
		if ps.BaseURL == "" {
			return nil, fmt.Errorf("providers.ollama.base_url is required")
		}
		return ollama.New(ps.BaseURL, c, retry), nil
	case configs.ProviderOpenAI, configs.ProviderAnthropic:
		if ps.APIKeyEnv == "" {
			return nil, fmt.Errorf("providers.%s.api_key_env is required", name)
		}
		key := getenv(ps.APIKeyEnv)
		if key == "" {
			return nil, fmt.Errorf("environment variable %s is not set", ps.APIKeyEnv)
		}
		if name == configs.ProviderOpenAI {
			return openai.New(key, ps.BaseURL, c, retry), nil
		}
		return anthropic.New(key, ps.BaseURL, c, retry), nil
	}
	return nil, fmt.Errorf("unknown provider %q", name)
}

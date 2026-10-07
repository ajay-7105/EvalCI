package engine

import (
	"sort"
	"sync"

	"github.com/ajay-7105/EvalCI/internal/providers"
)

// ModelUsage is the token usage of one provider and model over a run.
type ModelUsage struct {
	Provider string
	Model    string
	// Calls is the number of successful calls.
	Calls        int64
	InputTokens  int64
	OutputTokens int64
	// UnknownCalls counts successful calls for which the provider reported no usage.
	UnknownCalls int64
}

type usageKey struct{ provider, model string }

// usageMeter aggregates usage per (provider, model); it is safe for concurrent use.
type usageMeter struct {
	mu sync.Mutex
	m  map[usageKey]*ModelUsage
}

func newUsageMeter() *usageMeter { return &usageMeter{m: map[usageKey]*ModelUsage{}} }

// record adds one successful call.
func (u *usageMeter) record(provider, model string, use providers.Usage) {
	u.mu.Lock()
	defer u.mu.Unlock()
	k := usageKey{provider, model}
	mu, ok := u.m[k]
	if !ok {
		mu = &ModelUsage{Provider: provider, Model: model}
		u.m[k] = mu
	}
	mu.Calls++
	if use.Known {
		mu.InputTokens += int64(use.InputTokens)
		mu.OutputTokens += int64(use.OutputTokens)
	} else {
		mu.UnknownCalls++
	}
}

// snapshot returns the totals sorted by provider then model.
func (u *usageMeter) snapshot() []ModelUsage {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]ModelUsage, 0, len(u.m))
	for _, v := range u.m {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].Model < out[j].Model
	})
	return out
}

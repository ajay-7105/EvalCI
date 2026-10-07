// Package fake is a scriptable in-memory provider for tests and examples.
package fake

import (
	"context"
	"sync"
	"time"

	"github.com/ajay-7105/EvalCI/internal/providers"
)

// Provider is a scriptable fake implementing providers.Provider.
type Provider struct {
	// CompleteFn produces replies; when nil the reply is the user message.
	CompleteFn func(call int, req providers.Request) (providers.Response, error)
	// EmbedFn produces embeddings; when nil a fixed vector is returned.
	EmbedFn func(model, text string) ([]float32, error)
	// UsagePerCall is attached to every reply and embedding that carries no usage of its own.
	UsagePerCall providers.Usage
	// Embeddings is the value reported by SupportsEmbeddings.
	Embeddings bool
	// Delay simulates latency for each Complete call.
	Delay time.Duration

	mu          sync.Mutex
	calls       int
	embedCalls  int
	inflight    int
	maxInflight int
	requests    []providers.Request
}

// New returns a fake that echoes the user message and supports embeddings.
func New() *Provider { return &Provider{Embeddings: true} }

// Complete implements providers.Provider.
func (p *Provider) Complete(ctx context.Context, req providers.Request) (providers.Response, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.requests = append(p.requests, req)
	p.inflight++
	if p.inflight > p.maxInflight {
		p.maxInflight = p.inflight
	}
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.inflight--
		p.mu.Unlock()
	}()

	if p.Delay > 0 {
		select {
		case <-time.After(p.Delay):
		case <-ctx.Done():
			return providers.Response{}, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return providers.Response{}, err
	}
	resp := providers.Response{Text: req.User}
	if p.CompleteFn != nil {
		var err error
		if resp, err = p.CompleteFn(call, req); err != nil {
			return resp, err
		}
	}
	if !resp.Usage.Known {
		resp.Usage = p.UsagePerCall
	}
	return resp, nil
}

// Embed implements providers.Provider.
func (p *Provider) Embed(_ context.Context, model, text string) (providers.Embedding, error) {
	p.mu.Lock()
	p.embedCalls++
	p.mu.Unlock()
	if !p.Embeddings {
		return providers.Embedding{}, providers.ErrEmbeddingsUnsupported
	}
	vec := []float32{1, 0}
	if p.EmbedFn != nil {
		var err error
		if vec, err = p.EmbedFn(model, text); err != nil {
			return providers.Embedding{}, err
		}
	}
	return providers.Embedding{Vector: vec, Usage: p.UsagePerCall}, nil
}

// SupportsEmbeddings implements providers.Provider.
func (p *Provider) SupportsEmbeddings() bool { return p.Embeddings }

// Calls returns the number of Complete calls made.
func (p *Provider) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// EmbedCalls returns the number of Embed calls made.
func (p *Provider) EmbedCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.embedCalls
}

// MaxInflight returns the highest number of concurrent Complete calls observed.
func (p *Provider) MaxInflight() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.maxInflight
}

// Requests returns a copy of every request received, in arrival order.
func (p *Provider) Requests() []providers.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]providers.Request(nil), p.requests...)
}

package engine

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/time/rate"

	"github.com/ajay-7105/EvalCI/configs"
	"github.com/ajay-7105/EvalCI/internal/providers"
)

// limiter caps in-flight requests with a semaphore and optionally paces request starts.
type limiter struct {
	sem chan struct{}
	rl  *rate.Limiter // nil means unlimited
}

func newLimiter(ps configs.ProviderSettings) *limiter {
	n := ps.MaxConcurrent
	if n <= 0 {
		n = configs.DefaultMaxConcurrent
	}
	l := &limiter{sem: make(chan struct{}, n)}
	if ps.RequestsPerMinute > 0 {
		l.rl = rate.NewLimiter(rate.Every(time.Minute/time.Duration(ps.RequestsPerMinute)), 1)
	}
	return l
}

// acquire blocks until a request may start and returns the matching release func.
func (l *limiter) acquire(ctx context.Context) (func(), error) {
	select {
	case l.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if l.rl != nil {
		if err := l.rl.Wait(ctx); err != nil {
			<-l.sem
			return nil, fmt.Errorf("waiting for rate limit: %w", err)
		}
	}
	return func() { <-l.sem }, nil
}

// limited wraps a provider so every call honors the limiter and is reported to the observer.
type limited struct {
	inner providers.Provider
	name  string
	lim   *limiter
	obs   Observer
	meter *usageMeter
}

// Complete implements providers.Provider.
func (p *limited) Complete(ctx context.Context, req providers.Request) (providers.Response, error) {
	release, err := p.lim.acquire(ctx)
	if err != nil {
		return providers.Response{}, err
	}
	defer release()
	start := time.Now()
	resp, err := p.inner.Complete(ctx, req)
	p.report(ctx, "complete", req.Model, start, err)
	if err == nil {
		p.meter.record(p.name, req.Model, resp.Usage)
	}
	return resp, err
}

// Embed implements providers.Provider.
func (p *limited) Embed(ctx context.Context, model, text string) (providers.Embedding, error) {
	release, err := p.lim.acquire(ctx)
	if err != nil {
		return providers.Embedding{}, err
	}
	defer release()
	start := time.Now()
	v, err := p.inner.Embed(ctx, model, text)
	p.report(ctx, "embed", model, start, err)
	if err == nil {
		p.meter.record(p.name, model, v.Usage)
	}
	return v, err
}

// SupportsEmbeddings implements providers.Provider.
func (p *limited) SupportsEmbeddings() bool { return p.inner.SupportsEmbeddings() }

func (p *limited) report(ctx context.Context, kind, model string, start time.Time, err error) {
	ri := runInfoFrom(ctx)
	p.obs.CallFinished(CallInfo{
		Suite: ri.suite, Case: ri.caseID, Variant: ri.variant, Run: ri.run,
		Kind: kind, Provider: p.name, Model: model, Duration: time.Since(start), Err: err,
	})
}

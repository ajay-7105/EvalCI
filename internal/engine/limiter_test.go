package engine

import (
	"context"
	"testing"
	"time"

	"github.com/ajay-7105/EvalCI/configs"
)

func TestLimiterSemaphore(t *testing.T) {
	l := newLimiter(configs.ProviderSettings{MaxConcurrent: 1})
	release, err := l.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := l.acquire(ctx); err == nil {
		t.Fatal("second acquire must block until the first is released")
	}
	release()
	if rel2, err := l.acquire(context.Background()); err != nil {
		t.Fatal(err)
	} else {
		rel2()
	}
}

func TestLimiterDefaultsConcurrency(t *testing.T) {
	l := newLimiter(configs.ProviderSettings{})
	if cap(l.sem) != configs.DefaultMaxConcurrent {
		t.Fatalf("cap = %d, want %d", cap(l.sem), configs.DefaultMaxConcurrent)
	}
}

func TestLimiterPacesRequests(t *testing.T) {
	l := newLimiter(configs.ProviderSettings{MaxConcurrent: 10, RequestsPerMinute: 600}) // one per 100ms
	start := time.Now()
	for i := 0; i < 3; i++ {
		rel, err := l.acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		rel()
	}
	if elapsed := time.Since(start); elapsed < 180*time.Millisecond {
		t.Fatalf("3 acquires took %v, want >= ~200ms of pacing", elapsed)
	}
}

func TestLimiterRateWaitCancelReleasesSlot(t *testing.T) {
	l := newLimiter(configs.ProviderSettings{MaxConcurrent: 1, RequestsPerMinute: 1})
	rel, err := l.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rel()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := l.acquire(ctx); err == nil {
		t.Fatal("rate wait must honor cancellation")
	}
	if len(l.sem) != 0 {
		t.Fatal("semaphore slot leaked after rate-wait cancellation")
	}
}

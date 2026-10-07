package engine

import (
	"context"
	"sync"
	"testing"

	"github.com/ajay-7105/EvalCI/configs"
	"github.com/ajay-7105/EvalCI/internal/providers"
	"github.com/ajay-7105/EvalCI/internal/providers/fake"
)

func TestUsageMeterAggregates(t *testing.T) {
	m := newUsageMeter()
	m.record("openai", "a", providers.Usage{InputTokens: 10, OutputTokens: 2, Known: true})
	m.record("openai", "a", providers.Usage{InputTokens: 5, OutputTokens: 1, Known: true})
	m.record("openai", "a", providers.Usage{}) // unknown
	m.record("anthropic", "b", providers.Usage{InputTokens: 7, Known: true})
	got := m.snapshot()
	if len(got) != 2 || got[0].Provider != "anthropic" {
		t.Fatalf("snapshot = %+v", got)
	}
	oa := got[1]
	if oa.Calls != 3 || oa.InputTokens != 15 || oa.OutputTokens != 3 || oa.UnknownCalls != 1 {
		t.Fatalf("openai/a = %+v", oa)
	}
}

func TestUsageMeterIsConcurrencySafe(t *testing.T) {
	m := newUsageMeter()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.record("p", "m", providers.Usage{InputTokens: 2, OutputTokens: 1, Known: true})
		}()
	}
	wg.Wait()
	if s := m.snapshot()[0]; s.Calls != 100 || s.InputTokens != 200 || s.OutputTokens != 100 {
		t.Fatalf("%+v", s)
	}
}

func TestEngineMetersGenerationJudgeAndEmbeddingCalls(t *testing.T) {
	dir := t.TempDir()
	prompt := writePrompt(t, dir, "p.txt", "OK")
	gen := fake.New()
	gen.UsagePerCall = providers.Usage{InputTokens: 10, OutputTokens: 4, Known: true}
	systemEcho(gen)
	judge := fake.New()
	judge.UsagePerCall = providers.Usage{InputTokens: 100, OutputTokens: 6, Known: true}
	judge.CompleteFn = func(int, providers.Request) (providers.Response, error) {
		return providers.Response{Text: `{"score": 80}`}, nil
	}
	emb := fake.New()
	emb.UsagePerCall = providers.Usage{InputTokens: 3, Known: true}

	s := suiteOf("s", prompt, 0, configs.Case{ID: "c", Input: "x", Assertions: []configs.Assertion{
		{Type: configs.AssertRubric, Rubric: "r", Judge: &configs.ModelRef{Provider: "judge", Model: "jm"}},
		{Type: configs.AssertSemantic, Expected: "OK", Embedding: &configs.ModelRef{Provider: "emb", Model: "em"}},
	}})
	provs := map[string]providers.Provider{"fake": gen, "judge": judge, "emb": emb}
	e := New(Config{
		Settings: configs.Settings{RunsPerCase: 1},
		Lookup: func(name string) (providers.Provider, error) {
			return provs[name], nil
		},
	})
	if _, err := e.Run(context.Background(), []SuitePair{pairOf(s, prompt)}); err != nil {
		t.Fatal(err)
	}
	byKey := map[string]ModelUsage{}
	for _, u := range e.Usage() {
		byKey[u.Provider+"/"+u.Model] = u
	}
	// 2 variants x 1 run each: generation 2 calls, judge 2 calls, embeddings: expected once
	// (cached per assertion) + one per output.
	if g := byKey["fake/m"]; g.Calls != 2 || g.InputTokens != 20 || g.OutputTokens != 8 || g.UnknownCalls != 0 {
		t.Fatalf("generation = %+v", g)
	}
	if j := byKey["judge/jm"]; j.Calls != 2 || j.InputTokens != 200 || j.OutputTokens != 12 {
		t.Fatalf("judge = %+v", j)
	}
	if m := byKey["emb/em"]; m.Calls != 3 || m.InputTokens != 9 {
		t.Fatalf("embedding = %+v", m)
	}
}

func TestEngineCountsUnknownUsageAndIgnoresFailedCalls(t *testing.T) {
	dir := t.TempDir()
	prompt := writePrompt(t, dir, "p.txt", "OK")
	p := fake.New() // UsagePerCall zero => Known false
	n := 0
	var mu sync.Mutex
	p.CompleteFn = func(_ int, req providers.Request) (providers.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		if n == 1 {
			return providers.Response{}, context.DeadlineExceeded
		}
		return providers.Response{Text: req.System}, nil
	}
	s := suiteOf("s", prompt, 0.7, configs.Case{ID: "c", Input: "x", Assertions: exact("OK")})
	e := newEngine(p, 3, nil, configs.ProviderSettings{})
	if _, err := e.Run(context.Background(), []SuitePair{pairOf(s, prompt)}); err != nil {
		t.Fatal(err)
	}
	u := e.Usage()
	if len(u) != 1 || u[0].Calls != 5 || u[0].UnknownCalls != 5 || u[0].InputTokens != 0 {
		t.Fatalf("usage = %+v (6 calls, 1 failed and so not counted; the other 5 had unknown usage)", u)
	}
}

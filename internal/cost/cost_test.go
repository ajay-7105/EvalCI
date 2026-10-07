package cost

import "testing"

func ptr(v int64) *int64 { return &v }

func TestCompute(t *testing.T) {
	prices := map[string]Price{
		"openai/gpt-4o-mini": {InputPerMillion: 0.15, OutputPerMillion: 0.60},
		"ollama/llama":       {InputPerMillion: 0, OutputPerMillion: 0},
		"anthropic/haiku":    {InputPerMillion: 1.00, OutputPerMillion: 5.00},
	}
	tests := []struct {
		name        string
		usage       []Usage
		wantTotal   *int64
		wantPartial bool
		wantModels  []*int64
		wantIn      int64
		wantOut     int64
	}{
		{
			name:      "one model priced",
			usage:     []Usage{{Provider: "openai", Model: "gpt-4o-mini", Calls: 2, InputTokens: 1_000_000, OutputTokens: 1_000_000}},
			wantTotal: ptr(750_000), wantModels: []*int64{ptr(750_000)}, wantIn: 1_000_000, wantOut: 1_000_000,
		},
		{
			name:      "rounds once per model, half up",
			usage:     []Usage{{Provider: "openai", Model: "gpt-4o-mini", Calls: 1, InputTokens: 3, OutputTokens: 0}},
			wantTotal: ptr(0), wantModels: []*int64{ptr(0)}, wantIn: 3, // 3 * 0.15 = 0.45 micro -> 0
		},
		{
			name:      "round half up",
			usage:     []Usage{{Provider: "anthropic", Model: "haiku", Calls: 1, InputTokens: 1, OutputTokens: 1}},
			wantTotal: ptr(6), wantModels: []*int64{ptr(6)}, wantIn: 1, wantOut: 1, // 1*1 + 1*5 = 6 micro
		},
		{
			name: "cents exact for realistic usage (SC-009)",
			usage: []Usage{
				{Provider: "openai", Model: "gpt-4o-mini", Calls: 40, InputTokens: 523_000, OutputTokens: 61_000},
				{Provider: "anthropic", Model: "haiku", Calls: 10, InputTokens: 80_000, OutputTokens: 12_000},
			},
			// openai: 523000*0.15 + 61000*0.6 = 78450 + 36600 = 115050 micros; anthropic: 80000 + 60000 = 140000
			wantTotal: ptr(255_050), wantModels: []*int64{ptr(140_000), ptr(115_050)}, wantIn: 603_000, wantOut: 73_000,
		},
		{
			name:      "no price is unknown, never zero, tokens still counted",
			usage:     []Usage{{Provider: "ollama", Model: "unpriced", Calls: 3, InputTokens: 100, OutputTokens: 50}},
			wantTotal: nil, wantPartial: true, wantModels: []*int64{nil}, wantIn: 100, wantOut: 50,
		},
		{
			name:      "explicit zero price is a known zero",
			usage:     []Usage{{Provider: "ollama", Model: "llama", Calls: 3, InputTokens: 100, OutputTokens: 50}},
			wantTotal: ptr(0), wantModels: []*int64{ptr(0)}, wantIn: 100, wantOut: 50,
		},
		{
			name:      "unknown calls make a priced model unknown",
			usage:     []Usage{{Provider: "openai", Model: "gpt-4o-mini", Calls: 4, InputTokens: 1000, OutputTokens: 100, UnknownCalls: 1}},
			wantTotal: nil, wantPartial: true, wantModels: []*int64{nil}, wantIn: 1000, wantOut: 100,
		},
		{
			name: "partial: known costs summed, flag set",
			usage: []Usage{
				{Provider: "openai", Model: "gpt-4o-mini", Calls: 1, InputTokens: 1_000_000},
				{Provider: "ollama", Model: "unpriced", Calls: 1, InputTokens: 5},
			},
			wantTotal: ptr(150_000), wantPartial: true, wantModels: []*int64{nil, ptr(150_000)}, wantIn: 1_000_005,
		},
		{name: "no calls at all is a known zero", usage: nil, wantTotal: ptr(0), wantModels: []*int64{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Compute(tt.usage, prices)
			if (s.TotalCostMicros == nil) != (tt.wantTotal == nil) || (tt.wantTotal != nil && *s.TotalCostMicros != *tt.wantTotal) {
				t.Fatalf("total = %v, want %v", deref(s.TotalCostMicros), deref(tt.wantTotal))
			}
			if s.Partial != tt.wantPartial {
				t.Fatalf("partial = %v, want %v", s.Partial, tt.wantPartial)
			}
			if s.TotalInput != tt.wantIn || s.TotalOutput != tt.wantOut {
				t.Fatalf("tokens = %d/%d, want %d/%d", s.TotalInput, s.TotalOutput, tt.wantIn, tt.wantOut)
			}
			if len(s.Models) != len(tt.wantModels) {
				t.Fatalf("models = %d, want %d", len(s.Models), len(tt.wantModels))
			}
			for i, w := range tt.wantModels {
				if (s.Models[i].CostMicros == nil) != (w == nil) || (w != nil && *s.Models[i].CostMicros != *w) {
					t.Errorf("model %d (%s/%s) cost = %v, want %v", i, s.Models[i].Provider, s.Models[i].Model, deref(s.Models[i].CostMicros), deref(w))
				}
			}
		})
	}
}

func TestTotalIsTheSumOfModelCosts(t *testing.T) {
	prices := map[string]Price{"a/x": {0.33, 0.77}, "b/y": {1.01, 2.02}}
	s := Compute([]Usage{
		{Provider: "a", Model: "x", InputTokens: 12_345, OutputTokens: 6_789, Calls: 3},
		{Provider: "b", Model: "y", InputTokens: 98_765, OutputTokens: 4_321, Calls: 5},
	}, prices)
	var sum int64
	for _, m := range s.Models {
		sum += *m.CostMicros
	}
	if *s.TotalCostMicros != sum {
		t.Fatalf("total %d != sum of model costs %d", *s.TotalCostMicros, sum)
	}
}

func TestModelsAreSortedAndInputNotMutated(t *testing.T) {
	in := []Usage{{Provider: "z", Model: "b"}, {Provider: "a", Model: "z"}, {Provider: "a", Model: "b"}}
	s := Compute(in, nil)
	got := ""
	for _, m := range s.Models {
		got += m.Provider + "/" + m.Model + " "
	}
	if got != "a/b a/z z/b " {
		t.Fatalf("order = %q", got)
	}
	if in[0].Provider != "z" {
		t.Fatal("input slice was reordered")
	}
	if Key("p", "m") != "p/m" {
		t.Fatal("Key")
	}
}

func deref(p *int64) any {
	if p == nil {
		return "nil"
	}
	return *p
}

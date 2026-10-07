package assertions

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/ajay-7105/EvalCI/configs"
	"github.com/ajay-7105/EvalCI/internal/providers/fake"
)

func semanticWith(t *testing.T, embed func(model, text string) ([]float32, error)) (Assertion, *fake.Provider) {
	t.Helper()
	p := fake.New()
	p.EmbedFn = embed
	a, err := New(configs.Assertion{
		Type: configs.AssertSemantic, Expected: "expected",
		Embedding: &configs.ModelRef{Provider: "x", Model: "emb"},
	}, lookupOf(p))
	if err != nil {
		t.Fatal(err)
	}
	return a, p
}

func TestSemanticSimilarity(t *testing.T) {
	vectors := map[string][]float32{"expected": {1, 0}}
	tests := []struct {
		name string
		out  []float32
		want float64
	}{
		{"identical", []float32{1, 0}, 100},
		{"orthogonal", []float32{0, 1}, 0},
		{"negative clamps to zero", []float32{-1, 0}, 0},
		{"45 degrees", []float32{1, 1}, 100 / math.Sqrt2},
		{"zero vector scores zero", []float32{0, 0}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _ := semanticWith(t, func(_, text string) ([]float32, error) {
				if v, ok := vectors[text]; ok {
					return v, nil
				}
				return tt.out, nil
			})
			got, err := a.Score(context.Background(), "output")
			if err != nil || math.Abs(got-tt.want) > 1e-6 {
				t.Fatalf("Score = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestSemanticCachesExpectedEmbedding(t *testing.T) {
	a, p := semanticWith(t, nil)
	for i := 0; i < 3; i++ {
		if _, err := a.Score(context.Background(), "out"); err != nil {
			t.Fatal(err)
		}
	}
	if p.EmbedCalls() != 4 { // 1 expected (cached) + 3 outputs
		t.Fatalf("embed calls = %d, want 4", p.EmbedCalls())
	}
}

func TestSemanticErrors(t *testing.T) {
	a, _ := semanticWith(t, func(string, string) ([]float32, error) { return nil, errors.New("down") })
	if _, err := a.Score(context.Background(), "o"); err == nil {
		t.Fatal("embedding failure must be an error, not a zero")
	}
	b, _ := semanticWith(t, func(_, text string) ([]float32, error) {
		if text == "expected" {
			return []float32{1, 0}, nil
		}
		return []float32{1, 0, 0}, nil
	})
	if _, err := b.Score(context.Background(), "o"); err == nil {
		t.Fatal("length mismatch must be an error")
	}
	c, p := semanticWith(t, nil)
	p.Embeddings = false
	if _, err := c.Score(context.Background(), "o"); err == nil {
		t.Fatal("unsupported embeddings must be an error")
	}
}

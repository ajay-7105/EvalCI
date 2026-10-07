package assertions

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/ajay-7105/EvalCI/internal/providers"
)

type semantic struct {
	expected string
	provider providers.Provider
	model    string

	once sync.Once
	vec  []float32
	err  error
}

// Score implements Assertion: cosine similarity between the output and the expected text,
// clamped to [0, 1] and scaled to 0-100. The expected text is embedded once and cached.
func (s *semantic) Score(ctx context.Context, output string) (float64, error) {
	s.once.Do(func() {
		var e providers.Embedding
		e, s.err = s.provider.Embed(ctx, s.model, s.expected)
		s.vec = e.Vector
	})
	if s.err != nil {
		return 0, fmt.Errorf("embedding expected text: %w", s.err)
	}
	out, err := s.provider.Embed(ctx, s.model, output)
	if err != nil {
		return 0, fmt.Errorf("embedding output: %w", err)
	}
	sim, err := cosine(s.vec, out.Vector)
	if err != nil {
		return 0, err
	}
	return math.Min(1, math.Max(0, sim)) * 100, nil
}

func cosine(a, b []float32) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("embedding length mismatch: %d vs %d", len(a), len(b))
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0, nil
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb)), nil
}

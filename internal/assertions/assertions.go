// Package assertions scores a model output on a 0-100 scale.
package assertions

import (
	"context"
	"fmt"
	"strings"

	"github.com/ajay-7105/EvalCI/configs"
	"github.com/ajay-7105/EvalCI/internal/providers"
)

// Assertion scores one model output from 0 to 100.
type Assertion interface {
	// Score returns a score in [0, 100]. An error means the assertion could not be
	// evaluated (for example a provider failure); it is never silently scored as 0.
	Score(ctx context.Context, output string) (float64, error)
}

// ProviderLookup resolves a configured provider by name.
type ProviderLookup func(name string) (providers.Provider, error)

// New builds the assertion described by a. Assertions are built once per case and reused
// across runs, so they may cache work such as the expected text's embedding.
func New(a configs.Assertion, lookup ProviderLookup) (Assertion, error) {
	switch a.Type {
	case configs.AssertExactMatch:
		return exactMatch{expected: a.Expected, caseSensitive: a.IsCaseSensitive()}, nil
	case configs.AssertSemantic:
		p, err := lookup(a.Embedding.Provider)
		if err != nil {
			return nil, fmt.Errorf("semantic-similarity embedding provider: %w", err)
		}
		return &semantic{expected: a.Expected, provider: p, model: a.Embedding.Model}, nil
	case configs.AssertRubric:
		p, err := lookup(a.Judge.Provider)
		if err != nil {
			return nil, fmt.Errorf("llm-rubric judge provider: %w", err)
		}
		return &rubric{rubric: a.Rubric, provider: p, model: a.Judge.Model}, nil
	}
	return nil, fmt.Errorf("unknown assertion type %q", a.Type)
}

// Score returns the plain average of the assertions' scores for output. If any assertion
// fails to evaluate, the whole run is unscorable and an error is returned.
func Score(ctx context.Context, as []Assertion, output string) (float64, error) {
	if len(as) == 0 {
		return 0, fmt.Errorf("no assertions to score")
	}
	sum := 0.0
	for i, a := range as {
		s, err := a.Score(ctx, output)
		if err != nil {
			return 0, fmt.Errorf("assertion %d: %w", i, err)
		}
		sum += s
	}
	return sum / float64(len(as)), nil
}

type exactMatch struct {
	expected      string
	caseSensitive bool
}

// Score implements Assertion: 100 on a trimmed match, else 0.
func (e exactMatch) Score(_ context.Context, output string) (float64, error) {
	got, want := strings.TrimSpace(output), strings.TrimSpace(e.expected)
	if e.caseSensitive {
		if got == want {
			return 100, nil
		}
		return 0, nil
	}
	if strings.EqualFold(got, want) {
		return 100, nil
	}
	return 0, nil
}

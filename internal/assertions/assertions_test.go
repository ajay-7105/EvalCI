package assertions

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/ajay-7105/EvalCI/configs"
	"github.com/ajay-7105/EvalCI/internal/providers"
	"github.com/ajay-7105/EvalCI/internal/providers/fake"
)

func boolp(b bool) *bool { return &b }

func lookupOf(p providers.Provider) ProviderLookup {
	return func(string) (providers.Provider, error) { return p, nil }
}

func TestExactMatch(t *testing.T) {
	tests := []struct {
		name   string
		a      configs.Assertion
		output string
		want   float64
	}{
		{"match", configs.Assertion{Type: configs.AssertExactMatch, Expected: "REFUND"}, "REFUND", 100},
		{"trimmed", configs.Assertion{Type: configs.AssertExactMatch, Expected: "REFUND"}, "  REFUND\n", 100},
		{"case sensitive by default", configs.Assertion{Type: configs.AssertExactMatch, Expected: "REFUND"}, "refund", 0},
		{"case insensitive when false", configs.Assertion{Type: configs.AssertExactMatch, Expected: "REFUND", CaseSensitive: boolp(false)}, "refund", 100},
		{"mismatch", configs.Assertion{Type: configs.AssertExactMatch, Expected: "A"}, "B", 0},
		{"explicit case sensitive", configs.Assertion{Type: configs.AssertExactMatch, Expected: "A", CaseSensitive: boolp(true)}, "a", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := New(tt.a, nil)
			if err != nil {
				t.Fatal(err)
			}
			got, err := a.Score(context.Background(), tt.output)
			if err != nil || got != tt.want {
				t.Fatalf("Score = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

type constAssertion float64

func (c constAssertion) Score(context.Context, string) (float64, error) { return float64(c), nil }

type failAssertion struct{}

func (failAssertion) Score(context.Context, string) (float64, error) {
	return 0, errors.New("boom")
}

func TestScoreAverages(t *testing.T) {
	tests := []struct {
		name    string
		as      []Assertion
		want    float64
		wantErr bool
	}{
		{"single", []Assertion{constAssertion(80)}, 80, false},
		{"plain average", []Assertion{constAssertion(100), constAssertion(0), constAssertion(50)}, 50, false},
		{"one failing assertion fails the run", []Assertion{constAssertion(100), failAssertion{}}, 0, true},
		{"none", nil, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Score(context.Background(), tt.as, "x")
			if (err != nil) != tt.wantErr || (!tt.wantErr && math.Abs(got-tt.want) > 1e-9) {
				t.Fatalf("Score = %v, %v; want %v (err %v)", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestNewErrors(t *testing.T) {
	if _, err := New(configs.Assertion{Type: "bogus"}, nil); err == nil {
		t.Fatal("unknown type must fail")
	}
	bad := func(string) (providers.Provider, error) { return nil, fmt.Errorf("no such provider") }
	sem := configs.Assertion{Type: configs.AssertSemantic, Expected: "x", Embedding: &configs.ModelRef{Provider: "p", Model: "m"}}
	if _, err := New(sem, bad); err == nil {
		t.Fatal("lookup failure must propagate for semantic")
	}
	rub := configs.Assertion{Type: configs.AssertRubric, Rubric: "r", Judge: &configs.ModelRef{Provider: "p", Model: "m"}}
	if _, err := New(rub, bad); err == nil {
		t.Fatal("lookup failure must propagate for rubric")
	}
	if _, err := New(sem, lookupOf(fake.New())); err != nil {
		t.Fatal(err)
	}
	if _, err := New(rub, lookupOf(fake.New())); err != nil {
		t.Fatal(err)
	}
}

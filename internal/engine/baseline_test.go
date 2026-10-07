package engine

import (
	"strings"
	"testing"

	"github.com/ajay-7105/EvalCI/configs"
)

func TestResolveBaseline(t *testing.T) {
	c := []configs.Suite{{Name: "a", File: "cand/evals/a.eval.yaml"}, {Name: "b", File: "cand/evals/b.eval.yaml"}}
	b := []configs.Suite{{Name: "a-old", File: "base/evals/a.eval.yaml"}, {Name: "b", File: "base/evals/b.eval.yaml"}}
	pairs, err := ResolveBaseline(c, b, false)
	if err != nil {
		t.Fatal(err)
	}
	if pairs[0].Baseline.Name != "a-old" || pairs[1].Baseline.Name != "b" {
		t.Fatalf("matched by file name expected: %+v", pairs)
	}
	orphan := configs.Suite{Name: "orphan", File: "cand/evals/orphan.eval.yaml"}
	_, err = ResolveBaseline(append(c, orphan), b, false)
	if err == nil || !strings.Contains(err.Error(), "orphan") {
		t.Fatalf("err = %v, want naming the suite", err)
	}
	pairs, err = ResolveBaseline(append(c, orphan), b, true)
	if err != nil || len(pairs) != 3 || !pairs[2].NoBaseline || pairs[0].NoBaseline || pairs[2].Candidate.Name != "orphan" {
		t.Fatalf("allowMissing must give a candidate-only pair: %+v err=%v", pairs, err)
	}
}

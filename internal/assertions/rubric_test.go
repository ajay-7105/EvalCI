package assertions

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ajay-7105/EvalCI/configs"
	"github.com/ajay-7105/EvalCI/internal/providers"
	"github.com/ajay-7105/EvalCI/internal/providers/fake"
)

func rubricWith(t *testing.T, reply string, callErr error) (Assertion, *fake.Provider) {
	t.Helper()
	p := fake.New()
	p.CompleteFn = func(int, providers.Request) (providers.Response, error) {
		return providers.Response{Text: reply}, callErr
	}
	a, err := New(configs.Assertion{
		Type: configs.AssertRubric, Rubric: "be polite",
		Judge: &configs.ModelRef{Provider: "x", Model: "judge"},
	}, lookupOf(p))
	if err != nil {
		t.Fatal(err)
	}
	return a, p
}

func TestRubricReplies(t *testing.T) {
	tests := []struct {
		name    string
		reply   string
		want    float64
		wantErr bool
	}{
		{"plain json", `{"score": 87}`, 87, false},
		{"fenced json", "```json\n{\"score\": 42.5}\n```", 42.5, false},
		{"text around json", `Sure! {"score": 10} done`, 10, false},
		{"bounds ok", `{"score": 0}`, 0, false},
		{"upper bound ok", `{"score": 100}`, 100, false},
		{"not json", `great answer`, 0, true},
		{"missing score", `{"grade": 5}`, 0, true},
		{"over range", `{"score": 101}`, 0, true},
		{"negative", `{"score": -1}`, 0, true},
		{"string score", `{"score": "high"}`, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _ := rubricWith(t, tt.reply, nil)
			got, err := a.Score(context.Background(), "hello")
			if (err != nil) != tt.wantErr || (!tt.wantErr && got != tt.want) {
				t.Fatalf("Score = %v, %v; want %v (err %v)", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestRubricCallFailureAndRequest(t *testing.T) {
	a, _ := rubricWith(t, "", errors.New("down"))
	if _, err := a.Score(context.Background(), "x"); err == nil {
		t.Fatal("judge call failure must be an error")
	}
	b, p := rubricWith(t, `{"score": 1}`, nil)
	if _, err := b.Score(context.Background(), "the output"); err != nil {
		t.Fatal(err)
	}
	req := p.Requests()[0]
	if req.Model != "judge" || req.Temperature != 0 ||
		!strings.Contains(req.User, "be polite") || !strings.Contains(req.User, "the output") {
		t.Fatalf("unexpected judge request: %+v", req)
	}
}

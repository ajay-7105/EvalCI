package assertions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ajay-7105/EvalCI/internal/providers"
)

const judgeSystem = `You are a strict evaluator. Score the RESPONSE against the RUBRIC from 0 (fails the rubric completely) to 100 (fully satisfies it).
Reply with only a JSON object of the form {"score": <number between 0 and 100>} and nothing else.`

type rubric struct {
	rubric   string
	provider providers.Provider
	model    string
}

// Score implements Assertion by asking the judge model for a JSON score. A reply that is
// not valid JSON, or whose score is outside 0-100, is an error (a failed run), not a zero.
func (r *rubric) Score(ctx context.Context, output string) (float64, error) {
	resp, err := r.provider.Complete(ctx, providers.Request{
		Model:       r.model,
		System:      judgeSystem,
		User:        fmt.Sprintf("RUBRIC:\n%s\n\nRESPONSE:\n%s", r.rubric, output),
		Temperature: 0,
	})
	if err != nil {
		return 0, fmt.Errorf("judge call: %w", err)
	}
	return parseJudgeScore(resp.Text)
}

func parseJudgeScore(text string) (float64, error) {
	text = strings.TrimSpace(text)
	if start, end := strings.Index(text, "{"), strings.LastIndex(text, "}"); start >= 0 && end > start {
		text = text[start : end+1]
	}
	var v struct {
		Score *float64 `json:"score"`
	}
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		return 0, fmt.Errorf("judge reply is not valid JSON: %w", err)
	}
	if v.Score == nil {
		return 0, fmt.Errorf("judge reply has no score field")
	}
	if *v.Score < 0 || *v.Score > 100 {
		return 0, fmt.Errorf("judge score %v is outside 0-100", *v.Score)
	}
	return *v.Score, nil
}

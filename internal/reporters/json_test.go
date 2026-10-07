package reporters

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ajay-7105/EvalCI/internal/cost"
	"github.com/ajay-7105/EvalCI/internal/diffscope"
	"github.com/ajay-7105/EvalCI/internal/gating"
)

// validate checks doc against a JSON Schema subset (type, enum, const, required, properties,
// additionalProperties:false, items, minimum, maximum, local $ref) — enough for result.schema.json.
func validate(root, schema map[string]any, doc any, path string) []string {
	var errs []string
	if ref, ok := schema["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/$defs/")
		defs := root["$defs"].(map[string]any)
		return validate(root, defs[name].(map[string]any), doc, path)
	}
	if c, ok := schema["const"]; ok && fmt.Sprint(c) != fmt.Sprint(doc) {
		errs = append(errs, fmt.Sprintf("%s: want const %v, got %v", path, c, doc))
	}
	if e, ok := schema["enum"].([]any); ok {
		found := false
		for _, v := range e {
			if fmt.Sprint(v) == fmt.Sprint(doc) {
				found = true
			}
		}
		if !found {
			errs = append(errs, fmt.Sprintf("%s: %v not in enum %v", path, doc, e))
		}
	}
	if ty, ok := schema["type"]; ok {
		types := []any{ty}
		if arr, isArr := ty.([]any); isArr {
			types = arr
		}
		match := false
		for _, tt := range types {
			switch tt {
			case "object":
				_, m := doc.(map[string]any)
				match = match || m
			case "array":
				_, m := doc.([]any)
				match = match || m
			case "string":
				_, m := doc.(string)
				match = match || m
			case "boolean":
				_, m := doc.(bool)
				match = match || m
			case "null":
				match = match || doc == nil
			case "number":
				_, m := doc.(float64)
				match = match || m
			case "integer":
				f, m := doc.(float64)
				match = match || (m && f == float64(int64(f)))
			}
		}
		if !match {
			return append(errs, fmt.Sprintf("%s: type %v does not match %T", path, ty, doc))
		}
	}
	if f, ok := doc.(float64); ok {
		if min, has := schema["minimum"].(float64); has && f < min {
			errs = append(errs, fmt.Sprintf("%s: %v < minimum %v", path, f, min))
		}
		if max, has := schema["maximum"].(float64); has && f > max {
			errs = append(errs, fmt.Sprintf("%s: %v > maximum %v", path, f, max))
		}
	}
	if obj, ok := doc.(map[string]any); ok {
		props, _ := schema["properties"].(map[string]any)
		if req, ok := schema["required"].([]any); ok {
			for _, r := range req {
				if _, present := obj[r.(string)]; !present {
					errs = append(errs, fmt.Sprintf("%s: missing required %q", path, r))
				}
			}
		}
		for k, v := range obj {
			sub, known := props[k].(map[string]any)
			if !known {
				if ap, has := schema["additionalProperties"]; has && ap == false {
					errs = append(errs, fmt.Sprintf("%s: unexpected property %q", path, k))
				}
				continue
			}
			errs = append(errs, validate(root, sub, v, path+"."+k)...)
		}
	}
	if arr, ok := doc.([]any); ok {
		if items, ok := schema["items"].(map[string]any); ok {
			for i, v := range arr {
				errs = append(errs, validate(root, items, v, fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
	}
	return errs
}

func loadSchema(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "specs", "002-diff-scoping-storage", "contracts", "result.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	return schema
}

var rset = ResultSettings{RunsPerCase: 5, MinDropThreshold: 5, MaxSpreadBeforeUnreliable: 20}

func report(results []gating.CaseResult, d gating.Decision) Report {
	return Report{
		Decision: d, Settings: rset, Results: results, Currency: "USD",
		Scope: ScopeReport{Mode: "baseline-dir", Suites: []diffscope.ScopeDecision{}},
	}
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestWriteJSONValidatesAgainstSchema(t *testing.T) {
	fixtures := map[string]struct {
		results  []gating.CaseResult
		decision gating.Decision
		exit     float64
	}{
		"pass": {[]gating.CaseResult{
			{Suite: "s", Case: "a", Critical: true, Baseline: sd(2, 90, 92), Candidate: sd(2, 90, 91), Drop: 0.5, Status: gating.StatusPass}}, gating.DecisionPass, 0},
		"warn": {[]gating.CaseResult{
			{Suite: "s", Case: "a", Critical: true, Baseline: sd(2, 100, 50), Candidate: sd(2, 10, 10), Drop: 40, Status: gating.StatusUnreliable}}, gating.DecisionWarn, 0},
		"fail": {[]gating.CaseResult{
			{Suite: "s", Case: "a", Critical: true, Baseline: sd(1, 90), Candidate: sd(1, 10), Drop: 80, Status: gating.StatusRegressed}}, gating.DecisionFail, 1},
		"error": {[]gating.CaseResult{
			{Suite: "s", Case: "a", Critical: false, Baseline: sd(3), Candidate: sd(3, 90, 90, 90), Status: gating.StatusError}}, gating.DecisionError, 2},
	}
	schema := loadSchema(t)
	for name, f := range fixtures {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteJSON(&buf, report(f.results, f.decision)); err != nil {
				t.Fatal(err)
			}
			doc := decode(t, buf.Bytes())
			if errs := validate(schema, schema, doc, "$"); len(errs) > 0 {
				t.Fatalf("schema violations:\n%s\n%s", strings.Join(errs, "\n"), buf.String())
			}
			if doc["decision"] != string(f.decision) || doc["exit_code"] != f.exit {
				t.Fatalf("decision/exit mismatch: %v", doc)
			}
		})
	}
}

func TestWriteJSONNullsForErrorCase(t *testing.T) {
	var buf bytes.Buffer
	res := []gating.CaseResult{{Suite: "s", Case: "a", Critical: true, Baseline: sd(2), Candidate: sd(2, 80, 90), Status: gating.StatusError}}
	if err := WriteJSON(&buf, report(res, gating.DecisionError)); err != nil {
		t.Fatal(err)
	}
	c := decode(t, buf.Bytes())["cases"].([]any)[0].(map[string]any)
	if c["drop"] != nil {
		t.Fatalf("drop must be null: %v", c["drop"])
	}
	base := c["baseline"].(map[string]any)
	if base["average"] != nil || base["spread"] != nil || len(base["scores"].([]any)) != 0 || base["failed_runs"] != float64(2) {
		t.Fatalf("baseline side wrong: %v", base)
	}
	if cand := c["candidate"].(map[string]any); cand["average"] != float64(85) || cand["spread"] != float64(10) {
		t.Fatalf("candidate side wrong: %v", cand)
	}
}

func TestWriteJSONDeterministicAndEmpty(t *testing.T) {
	res := []gating.CaseResult{{Suite: "s", Case: "a", Critical: true, Baseline: sd(1, 90), Candidate: sd(1, 90), Status: gating.StatusPass}}
	var a, b bytes.Buffer
	_ = WriteJSON(&a, report(res, gating.DecisionPass))
	_ = WriteJSON(&b, report(res, gating.DecisionPass))
	if a.String() != b.String() {
		t.Fatal("output must be deterministic")
	}
	var empty bytes.Buffer
	if err := WriteJSON(&empty, report(nil, gating.DecisionPass)); err != nil {
		t.Fatal(err)
	}
	if cases := decode(t, empty.Bytes())["cases"].([]any); len(cases) != 0 {
		t.Fatalf("cases = %v", cases)
	}
	if err := WriteJSON(failWriter{}, report(res, gating.DecisionPass)); err == nil {
		t.Fatal("write error must propagate")
	}
}

func i64(v int64) *int64 { return &v }

func TestWriteJSONV2ScopeCostAndRunID(t *testing.T) {
	schema := loadSchema(t)
	rep := Report{
		RunID: "20260301T120000Z-abcd1234", Decision: gating.DecisionWarn, Settings: rset, Currency: "EUR",
		Scope: ScopeReport{
			Mode: "scoped",
			Base: &RefInfo{Ref: "main", SHA: "aaaaaaa"}, Head: &RefInfo{Ref: "HEAD", SHA: "bbbbbbb", Branch: "feature/x"},
			Suites: []diffscope.ScopeDecision{
				{Suite: "support", File: "evals/support.eval.yaml", Action: "run", Reason: "prompt file changed", Files: []string{"prompts/support.txt"}},
				{Suite: "billing", File: "evals/billing.eval.yaml", Action: "skip", Reason: "no related changes"},
				{Suite: "legacy", File: "evals/legacy.eval.yaml", Action: "removed", Reason: "suite removed"},
			},
		},
		Cost: cost.Summary{
			Models: []cost.ModelCost{
				{Usage: cost.Usage{Provider: "openai", Model: "gpt", Calls: 4, InputTokens: 1000, OutputTokens: 200}, CostMicros: i64(250_000)},
				{Usage: cost.Usage{Provider: "ollama", Model: "llama", Calls: 2, InputTokens: 50, UnknownCalls: 1}},
			},
			TotalInput: 1050, TotalOutput: 200, TotalCostMicros: i64(250_000), Partial: true,
		},
		Results: []gating.CaseResult{
			{Suite: "support", Case: "a", Critical: true, Baseline: sd(0), Candidate: sd(2, 90, 80), Status: gating.StatusNew},
		},
	}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, rep); err != nil {
		t.Fatal(err)
	}
	doc := decode(t, buf.Bytes())
	if errs := validate(schema, schema, doc, "$"); len(errs) > 0 {
		t.Fatalf("schema violations:\n%s\n%s", strings.Join(errs, "\n"), buf.String())
	}
	if doc["schema_version"] != float64(2) || doc["run_id"] != "20260301T120000Z-abcd1234" {
		t.Fatalf("header wrong: %v", doc)
	}
	cs := doc["cost"].(map[string]any)
	if cs["estimated_cost"] != 0.25 || cs["partial"] != true || cs["currency"] != "EUR" {
		t.Fatalf("cost wrong: %v", cs)
	}
	models := cs["models"].([]any)
	if models[1].(map[string]any)["cost"] != nil || models[0].(map[string]any)["cost"] != 0.25 {
		t.Fatalf("model costs wrong: %v", models)
	}
	newCase := doc["cases"].([]any)[0].(map[string]any)
	if newCase["status"] != "NEW" || newCase["drop"] != nil || newCase["baseline"].(map[string]any)["runs"] != float64(0) {
		t.Fatalf("NEW case wrong: %v", newCase)
	}
	sc := doc["scope"].(map[string]any)
	if sc["mode"] != "scoped" || len(sc["suites"].([]any)) != 3 || sc["head"].(map[string]any)["branch"] != "feature/x" {
		t.Fatalf("scope wrong: %v", sc)
	}
}

func TestWriteJSONRunIDNullWhenNothingSaved(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, report(nil, gating.DecisionPass)); err != nil {
		t.Fatal(err)
	}
	doc := decode(t, buf.Bytes())
	if v, present := doc["run_id"]; !present || v != nil {
		t.Fatalf("run_id must be present and null: %v", doc)
	}
	if sc := doc["scope"].(map[string]any); sc["base"] != nil || sc["head"] != nil || sc["mode"] != "baseline-dir" {
		t.Fatalf("baseline-dir scope wrong: %v", sc)
	}
	if c := doc["cost"].(map[string]any); c["estimated_cost"] != nil {
		t.Fatalf("empty cost with a zero-value summary must be null, got %v", c)
	}
}

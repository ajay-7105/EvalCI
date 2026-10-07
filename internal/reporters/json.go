package reporters

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/ajay-7105/EvalCI/internal/cost"
	"github.com/ajay-7105/EvalCI/internal/diffscope"
	"github.com/ajay-7105/EvalCI/internal/gating"
)

// SchemaVersion is the version of the JSON result document.
const SchemaVersion = 2

// ResultSettings are the thresholds echoed into the result document.
type ResultSettings struct {
	RunsPerCase               int     `json:"runs_per_case"`
	MinDropThreshold          float64 `json:"min_drop_threshold"`
	MaxSpreadBeforeUnreliable float64 `json:"max_spread_before_unreliable"`
}

// RefInfo identifies a base or head in the scope block.
type RefInfo struct {
	Ref    string `json:"ref"`
	SHA    string `json:"sha"`
	Branch string `json:"branch,omitempty"`
}

// ScopeReport is the scoping part of the result document.
type ScopeReport struct {
	// Mode is scoped, full, or baseline-dir.
	Mode string
	// Base and Head are nil in baseline-dir mode.
	Base   *RefInfo
	Head   *RefInfo
	Suites []diffscope.ScopeDecision
}

// Report is everything the JSON result contains.
type Report struct {
	// RunID is the stored run's ID; empty when nothing was saved (the field is then null).
	RunID    string
	Decision gating.Decision
	Settings ResultSettings
	Scope    ScopeReport
	Cost     cost.Summary
	Currency string
	Results  []gating.CaseResult
}

type jsonSide struct {
	Runs       int       `json:"runs"`
	FailedRuns int       `json:"failed_runs"`
	Scores     []float64 `json:"scores"`
	Average    *float64  `json:"average"`
	Spread     *float64  `json:"spread"`
}

type jsonCase struct {
	Suite     string   `json:"suite"`
	Case      string   `json:"case"`
	Critical  bool     `json:"critical"`
	Status    string   `json:"status"`
	Drop      *float64 `json:"drop"`
	Baseline  jsonSide `json:"baseline"`
	Candidate jsonSide `json:"candidate"`
}

type jsonScopeSuite struct {
	Suite  string   `json:"suite"`
	File   string   `json:"file"`
	Action string   `json:"action"`
	Reason string   `json:"reason"`
	Files  []string `json:"files,omitempty"`
}

type jsonScope struct {
	Mode   string           `json:"mode"`
	Base   *RefInfo         `json:"base"`
	Head   *RefInfo         `json:"head"`
	Suites []jsonScopeSuite `json:"suites"`
}

type jsonModelCost struct {
	Provider     string   `json:"provider"`
	Model        string   `json:"model"`
	Calls        int64    `json:"calls"`
	InputTokens  int64    `json:"input_tokens"`
	OutputTokens int64    `json:"output_tokens"`
	UnknownCalls int64    `json:"unknown_calls"`
	Cost         *float64 `json:"cost"`
}

type jsonCost struct {
	Currency      string          `json:"currency"`
	InputTokens   int64           `json:"input_tokens"`
	OutputTokens  int64           `json:"output_tokens"`
	EstimatedCost *float64        `json:"estimated_cost"`
	Partial       bool            `json:"partial"`
	Models        []jsonModelCost `json:"models"`
}

type jsonResult struct {
	SchemaVersion int            `json:"schema_version"`
	RunID         *string        `json:"run_id"`
	Decision      string         `json:"decision"`
	ExitCode      int            `json:"exit_code"`
	Settings      ResultSettings `json:"settings"`
	Scope         jsonScope      `json:"scope"`
	Cost          jsonCost       `json:"cost"`
	Cases         []jsonCase     `json:"cases"`
}

// WriteJSON writes the machine-readable result document (schema version 2). Cases appear in
// the order given (the engine sorts them by suite then case id), so output is deterministic.
func WriteJSON(w io.Writer, r Report) error {
	doc := jsonResult{
		SchemaVersion: SchemaVersion,
		Decision:      string(r.Decision),
		ExitCode:      ExitCode(r.Decision),
		Settings:      r.Settings,
		Scope:         toJSONScope(r.Scope),
		Cost:          toJSONCost(r.Cost, r.Currency),
		Cases:         make([]jsonCase, 0, len(r.Results)),
	}
	if r.RunID != "" {
		id := r.RunID
		doc.RunID = &id
	}
	for _, res := range r.Results {
		c := jsonCase{
			Suite: res.Suite, Case: res.Case, Critical: res.Critical, Status: string(res.Status),
			Baseline: toJSONSide(res.Baseline), Candidate: toJSONSide(res.Candidate),
		}
		if res.Baseline.Successful() > 0 && res.Candidate.Successful() > 0 {
			d := res.Drop
			c.Drop = &d
		}
		doc.Cases = append(doc.Cases, c)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("encoding result: %w", err)
	}
	return nil
}

func toJSONScope(s ScopeReport) jsonScope {
	out := jsonScope{Mode: s.Mode, Base: s.Base, Head: s.Head, Suites: make([]jsonScopeSuite, 0, len(s.Suites))}
	for _, d := range s.Suites {
		out.Suites = append(out.Suites, jsonScopeSuite{Suite: d.Suite, File: d.File, Action: d.Action, Reason: d.Reason, Files: d.Files})
	}
	return out
}

func toJSONCost(s cost.Summary, currency string) jsonCost {
	out := jsonCost{
		Currency: currency, InputTokens: s.TotalInput, OutputTokens: s.TotalOutput,
		EstimatedCost: units(s.TotalCostMicros), Partial: s.Partial, Models: make([]jsonModelCost, 0, len(s.Models)),
	}
	for _, m := range s.Models {
		out.Models = append(out.Models, jsonModelCost{
			Provider: m.Provider, Model: m.Model, Calls: m.Calls, InputTokens: m.InputTokens,
			OutputTokens: m.OutputTokens, UnknownCalls: m.UnknownCalls, Cost: units(m.CostMicros),
		})
	}
	return out
}

func units(micros *int64) *float64 {
	if micros == nil {
		return nil
	}
	v := float64(*micros) / 1_000_000
	return &v
}

func toJSONSide(s gating.Side) jsonSide {
	js := jsonSide{Runs: s.Runs, FailedRuns: s.FailedRuns, Scores: s.Scores}
	if js.Scores == nil {
		js.Scores = []float64{}
	}
	if s.Successful() > 0 {
		avg, spread := s.Average, s.Spread
		js.Average, js.Spread = &avg, &spread
	}
	return js
}

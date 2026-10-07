package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/ajay-7105/EvalCI/configs"
	"github.com/ajay-7105/EvalCI/internal/cost"
	"github.com/ajay-7105/EvalCI/internal/diffscope"
	"github.com/ajay-7105/EvalCI/internal/engine"
	"github.com/ajay-7105/EvalCI/internal/gating"
	"github.com/ajay-7105/EvalCI/internal/storage"
)

// newRunID returns a sortable, collision-safe run identifier: YYYYMMDDTHHMMSSZ-<8 hex chars>.
func newRunID(t time.Time) string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on supported platforms; fall back to the nanoseconds.
		return fmt.Sprintf("%s-%08x", t.UTC().Format("20060102T150405Z"), uint32(t.UnixNano()))
	}
	return t.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b[:])
}

// runFacts is everything buildRecord needs about a finished run.
type runFacts struct {
	ID         string
	StartedAt  time.Time
	FinishedAt time.Time
	HeadSHA    string
	HeadBranch string
	BaseRef    string
	BaseSHA    string
	Mode       storage.Mode
	Decision   gating.Decision
	ExitCode   int
	Settings   configs.Settings
	Scope      []diffscope.ScopeDecision
	Results    []gating.CaseResult
	Cost       cost.Summary
}

// buildRecord assembles the history record. It copies only numbers, names, and decisions:
// never responses, prompts, or case inputs (FR-013a).
func buildRecord(f runFacts) storage.RunRecord {
	r := storage.RunRecord{
		ID: f.ID, SchemaVersion: storage.RecordVersion,
		StartedAt: f.StartedAt, FinishedAt: f.FinishedAt,
		HeadSHA: f.HeadSHA, HeadBranch: f.HeadBranch, BaseRef: f.BaseRef, BaseSHA: f.BaseSHA,
		Mode: f.Mode, Decision: storage.Decision(f.Decision), ExitCode: f.ExitCode,
		Thresholds: storage.Thresholds{
			RunsPerCase:               f.Settings.RunsPerCase,
			MinDropThreshold:          f.Settings.MinDropThreshold,
			MaxSpreadBeforeUnreliable: f.Settings.MaxSpreadBeforeUnreliable,
		},
		Currency:          f.Settings.Currency,
		TotalInputTokens:  f.Cost.TotalInput,
		TotalOutputTokens: f.Cost.TotalOutput,
		TotalCostMicros:   f.Cost.TotalCostMicros,
		CostPartial:       f.Cost.Partial,
	}
	for _, d := range f.Scope {
		r.Suites = append(r.Suites, storage.SuiteDecision{Suite: d.Suite, File: d.File, Action: d.Action, Reason: d.Reason})
	}
	for _, c := range f.Results {
		r.Cases = append(r.Cases, caseRecord(c))
	}
	for _, m := range f.Cost.Models {
		r.Usage = append(r.Usage, storage.UsageRecord{
			Provider: m.Provider, Model: m.Model, Calls: m.Calls, InputTokens: m.InputTokens,
			OutputTokens: m.OutputTokens, UnknownCalls: m.UnknownCalls, CostMicros: m.CostMicros,
		})
	}
	return r
}

func caseRecord(c gating.CaseResult) storage.CaseRecord {
	rec := storage.CaseRecord{
		Suite: c.Suite, Case: c.Case, Critical: c.Critical, Status: string(c.Status),
		BaselineRuns: c.Baseline.Runs, BaselineFailed: c.Baseline.FailedRuns,
		CandidateRuns: c.Candidate.Runs, CandidateFailed: c.Candidate.FailedRuns,
	}
	if c.Baseline.Successful() > 0 {
		avg, spread := c.Baseline.Average, c.Baseline.Spread
		rec.BaselineAverage, rec.BaselineSpread = &avg, &spread
	}
	if c.Candidate.Successful() > 0 {
		avg, spread := c.Candidate.Average, c.Candidate.Spread
		rec.CandidateAverage, rec.CandidateSpread = &avg, &spread
	}
	if c.Baseline.Successful() > 0 && c.Candidate.Successful() > 0 {
		d := c.Drop
		rec.Delta = &d
	}
	return rec
}

// costInputs converts engine usage and configured prices into the cost package's plain types.
func costInputs(usage []engine.ModelUsage, prices map[string]configs.Price) ([]cost.Usage, map[string]cost.Price) {
	us := make([]cost.Usage, 0, len(usage))
	for _, u := range usage {
		us = append(us, cost.Usage{
			Provider: u.Provider, Model: u.Model, Calls: u.Calls,
			InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, UnknownCalls: u.UnknownCalls,
		})
	}
	ps := make(map[string]cost.Price, len(prices))
	for k, p := range prices {
		ps[k] = cost.Price{InputPerMillion: p.InputPerMillion, OutputPerMillion: p.OutputPerMillion}
	}
	return us, ps
}

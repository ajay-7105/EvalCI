package storagetest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ajay-7105/EvalCI/internal/storage"
)

// Epoch is the base time used by Sample.
var Epoch = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func fp(f float64) *float64 { return &f }
func ip(i int64) *int64     { return &i }

// Sample returns a valid, fully populated run record: it includes unknown (nil) values so
// round trips cover NULL handling. The run started offset after Epoch.
func Sample(id, sha string, offset time.Duration) storage.RunRecord {
	start := Epoch.Add(offset)
	return storage.RunRecord{
		ID: id, SchemaVersion: storage.RecordVersion,
		StartedAt: start, FinishedAt: start.Add(3 * time.Second),
		HeadSHA: sha, HeadBranch: "feature/x", BaseRef: "main", BaseSHA: "b4b4b4b4b4b4b4b4b4b4b4b4b4b4b4b4b4b4b4b4",
		Mode: storage.ModeScoped, Decision: storage.DecisionFail, ExitCode: 1,
		Thresholds: storage.Thresholds{RunsPerCase: 5, MinDropThreshold: 5, MaxSpreadBeforeUnreliable: 20},
		Currency:   "USD", TotalInputTokens: 1200, TotalOutputTokens: 300,
		TotalCostMicros: ip(12345), CostPartial: true,
		Suites: []storage.SuiteDecision{
			{Suite: "support", File: "evals/support.eval.yaml", Action: storage.ActionRun, Reason: "prompt file changed"},
			{Suite: "billing", File: "evals/billing.eval.yaml", Action: storage.ActionSkip, Reason: "no related changes"},
			{Suite: "legacy", File: "evals/legacy.eval.yaml", Action: storage.ActionRemoved, Reason: "suite removed"},
		},
		Cases: []storage.CaseRecord{
			{
				Suite: "support", Case: "refund", Critical: true, Status: storage.StatusRegressed,
				BaselineRuns: 5, BaselineFailed: 0, BaselineAverage: fp(92.5), BaselineSpread: fp(10),
				CandidateRuns: 5, CandidateFailed: 1, CandidateAverage: fp(71), CandidateSpread: fp(15), Delta: fp(21.5),
			},
			{
				Suite: "support", Case: "dead", Critical: false, Status: storage.StatusError,
				BaselineRuns: 3, BaselineFailed: 3, CandidateRuns: 3, CandidateFailed: 0, CandidateAverage: fp(80), CandidateSpread: fp(0),
			},
		},
		Usage: []storage.UsageRecord{
			{Provider: "openai", Model: "gpt-4o-mini", Calls: 10, InputTokens: 1000, OutputTokens: 300, CostMicros: ip(12345)},
			{Provider: "ollama", Model: "llama3.2:1b", Calls: 4, InputTokens: 200, UnknownCalls: 1},
		},
	}
}

// RunContract runs the behavior every storage.Store implementation must satisfy. newStore must
// return a fresh, empty store.
func RunContract(t *testing.T, newStore func(t *testing.T) storage.Store) {
	t.Helper()
	ctx := context.Background()

	t.Run("round trip preserves everything including unknown values", func(t *testing.T) {
		s := newStore(t)
		in := Sample("r1", "aaa", 0)
		if err := s.SaveRun(ctx, in); err != nil {
			t.Fatal(err)
		}
		out, err := s.GetRun(ctx, "r1")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(in.Clone(), out) {
			t.Fatalf("round trip differs:\n in: %+v\nout: %+v", in.Clone(), out)
		}
		if out.Cases[1].BaselineAverage != nil || out.Cases[1].Delta != nil || out.Usage[1].CostMicros != nil {
			t.Fatal("unknown values must stay nil, never become zero")
		}
	})

	t.Run("the zero-cost run with no usage round trips as known zero", func(t *testing.T) {
		s := newStore(t)
		in := Sample("zero", "aaa", 0)
		in.Decision, in.ExitCode, in.TotalCostMicros, in.CostPartial = storage.DecisionPass, 0, ip(0), false
		in.TotalInputTokens, in.TotalOutputTokens = 0, 0
		in.Suites, in.Cases, in.Usage = nil, nil, nil
		if err := s.SaveRun(ctx, in); err != nil {
			t.Fatal(err)
		}
		out, err := s.GetRun(ctx, "zero")
		if err != nil || out.TotalCostMicros == nil || *out.TotalCostMicros != 0 || len(out.Cases) != 0 {
			t.Fatalf("out=%+v err=%v", out, err)
		}
	})

	t.Run("an invalid record is rejected and leaves nothing behind", func(t *testing.T) {
		s := newStore(t)
		bad := []storage.RunRecord{}
		a := Sample("bad-exit", "aaa", 0)
		a.ExitCode = 0 // FAIL must be 1
		bad = append(bad, a)
		b := Sample("bad-case", "aaa", 0)
		b.Cases[0].Suite = "billing" // billing was skipped
		bad = append(bad, b)
		c := Sample("bad-dup-case", "aaa", 0)
		c.Cases = append(c.Cases, c.Cases[0])
		bad = append(bad, c)
		d := Sample("", "aaa", 0)
		bad = append(bad, d)
		for _, r := range bad {
			if err := s.SaveRun(ctx, r); err == nil {
				t.Fatalf("record %q must be rejected", r.ID)
			}
			if _, err := s.GetRun(ctx, r.ID); !errors.Is(err, storage.ErrNotFound) {
				t.Fatalf("rejected record %q is visible: %v", r.ID, err)
			}
		}
		if got, _ := s.ListRecentRuns(ctx, 10); len(got) != 0 {
			t.Fatalf("rejected records leaked into listings: %d", len(got))
		}
	})

	t.Run("duplicate id is rejected and the stored run is unchanged", func(t *testing.T) {
		s := newStore(t)
		first := Sample("dup", "aaa", 0)
		if err := s.SaveRun(ctx, first); err != nil {
			t.Fatal(err)
		}
		second := Sample("dup", "bbb", time.Hour)
		second.Decision, second.ExitCode = storage.DecisionPass, 0
		if err := s.SaveRun(ctx, second); !errors.Is(err, storage.ErrDuplicateID) {
			t.Fatalf("err = %v, want ErrDuplicateID", err)
		}
		got, err := s.GetRun(ctx, "dup")
		if err != nil || !reflect.DeepEqual(first.Clone(), got) {
			t.Fatalf("stored run changed: %+v err=%v", got, err)
		}
	})

	t.Run("two runs of the same commit both exist", func(t *testing.T) {
		s := newStore(t)
		for i, id := range []string{"x1", "x2"} {
			if err := s.SaveRun(ctx, Sample(id, "same", time.Duration(i)*time.Minute)); err != nil {
				t.Fatal(err)
			}
		}
		got, err := s.ListRunsByCommit(ctx, "same")
		if err != nil || len(got) != 2 {
			t.Fatalf("got %d err=%v, want 2", len(got), err)
		}
	})

	t.Run("queries filter, order newest first, and limit", func(t *testing.T) {
		s := newStore(t)
		for i, sha := range []string{"a", "b", "a", "c", "a"} {
			if err := s.SaveRun(ctx, Sample(fmt.Sprintf("q%d", i), sha, time.Duration(i)*time.Minute)); err != nil {
				t.Fatal(err)
			}
		}
		byA, err := s.ListRunsByCommit(ctx, "a")
		if err != nil || ids(byA) != "q4,q2,q0" {
			t.Fatalf("ListRunsByCommit(a) = %s err=%v, want q4,q2,q0", ids(byA), err)
		}
		recent, err := s.ListRecentRuns(ctx, 3)
		if err != nil || ids(recent) != "q4,q3,q2" {
			t.Fatalf("ListRecentRuns(3) = %s err=%v, want q4,q3,q2", ids(recent), err)
		}
		all, _ := s.ListRecentRuns(ctx, 100)
		if len(all) != 5 {
			t.Fatalf("limit above count: got %d", len(all))
		}
		none, err := s.ListRunsByCommit(ctx, "unknown")
		if err != nil || none == nil || len(none) != 0 {
			t.Fatalf("unknown commit must give an empty slice, got %#v err=%v", none, err)
		}
		if zero, err := s.ListRecentRuns(ctx, 0); err != nil || len(zero) != 0 {
			t.Fatalf("limit 0: %v %v", zero, err)
		}
		if _, err := s.GetRun(ctx, "missing"); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("equal timestamps are ordered deterministically by id", func(t *testing.T) {
		s := newStore(t)
		for _, id := range []string{"m", "z", "a"} {
			if err := s.SaveRun(ctx, Sample(id, "same", 0)); err != nil {
				t.Fatal(err)
			}
		}
		got, _ := s.ListRecentRuns(ctx, 10)
		if ids(got) != "z,m,a" {
			t.Fatalf("order = %s, want z,m,a", ids(got))
		}
	})

	t.Run("callers cannot mutate stored records", func(t *testing.T) {
		s := newStore(t)
		in := Sample("mut", "aaa", 0)
		if err := s.SaveRun(ctx, in); err != nil {
			t.Fatal(err)
		}
		in.Cases[0].Status = storage.StatusPass
		*in.TotalCostMicros = 1
		out, _ := s.GetRun(ctx, "mut")
		out.Cases[0].Case = "tampered"
		again, _ := s.GetRun(ctx, "mut")
		if again.Cases[0].Status != storage.StatusRegressed || again.Cases[0].Case != "refund" || *again.TotalCostMicros != 12345 {
			t.Fatalf("stored record was mutated through a caller's copy: %+v", again)
		}
	})

	t.Run("parallel saves all complete", func(t *testing.T) {
		s := newStore(t)
		const n = 25
		var wg sync.WaitGroup
		errs := make(chan error, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				errs <- s.SaveRun(ctx, Sample(fmt.Sprintf("p%02d", i), "par", time.Duration(i)*time.Second))
			}(i)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		got, err := s.ListRunsByCommit(ctx, "par")
		if err != nil || len(got) != n {
			t.Fatalf("got %d runs err=%v, want %d", len(got), err, n)
		}
		for _, r := range got {
			if len(r.Cases) != 2 || len(r.Suites) != 3 || len(r.Usage) != 2 {
				t.Fatalf("run %s is incomplete: %d cases %d suites %d usage", r.ID, len(r.Cases), len(r.Suites), len(r.Usage))
			}
		}
	})

	t.Run("a cancelled context saves nothing", func(t *testing.T) {
		s := newStore(t)
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		if err := s.SaveRun(cctx, Sample("c1", "aaa", 0)); err == nil {
			t.Fatal("a cancelled context must fail the save")
		}
		if _, err := s.GetRun(ctx, "c1"); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("cancelled save left a record: %v", err)
		}
	})

	t.Run("record types have no field for responses, prompts, or inputs", func(t *testing.T) {
		forbidden := map[string]bool{
			"response": true, "responses": true, "prompt": true, "prompttext": true, "input": true,
			"inputs": true, "caseinput": true, "output": true, "outputs": true, "text": true,
			"content": true, "body": true, "reply": true, "completion": true,
		}
		for _, typ := range []reflect.Type{
			reflect.TypeOf(storage.RunRecord{}), reflect.TypeOf(storage.CaseRecord{}),
			reflect.TypeOf(storage.SuiteDecision{}), reflect.TypeOf(storage.UsageRecord{}),
			reflect.TypeOf(storage.Thresholds{}),
		} {
			for i := 0; i < typ.NumField(); i++ {
				if name := typ.Field(i).Name; forbidden[strings.ToLower(name)] {
					t.Errorf("%s.%s looks like a content field (FR-013a)", typ.Name(), name)
				}
			}
		}
	})
}

func ids(runs []storage.RunRecord) string {
	var out []string
	for _, r := range runs {
		out = append(out, r.ID)
	}
	return strings.Join(out, ",")
}

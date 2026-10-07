package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ajay-7105/EvalCI/internal/storage"
	"github.com/ajay-7105/EvalCI/internal/storage/storagetest"
)

// SC-006: saving a run adds under 1 second for runs with up to 500 cases.
func TestSaving500CasesTakesUnderASecond(t *testing.T) {
	if testing.Short() {
		t.Skip("performance check skipped with -short")
	}
	s := open(t, filepath.Join(t.TempDir(), "h.db"))
	r := storagetest.Sample("big", "sha", 0)
	avg, spread := 90.0, 5.0
	for i := 0; i < 500; i++ {
		r.Cases = append(r.Cases, storage.CaseRecord{
			Suite: "support", Case: fmt.Sprintf("case-%03d", i), Critical: true, Status: storage.StatusPass,
			BaselineRuns: 5, BaselineAverage: &avg, BaselineSpread: &spread,
			CandidateRuns: 5, CandidateAverage: &avg, CandidateSpread: &spread,
		})
	}
	start := time.Now()
	if err := s.SaveRun(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed > time.Second {
		t.Fatalf("saving a 500-case run took %v, want under 1s (SC-006)", elapsed)
	}
	got, err := s.GetRun(context.Background(), "big")
	if err != nil || len(got.Cases) != 502 {
		t.Fatalf("cases=%d err=%v", len(got.Cases), err)
	}
	t.Logf("saved a 502-case run in %v", elapsed)
}

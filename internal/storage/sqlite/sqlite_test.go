package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ajay-7105/EvalCI/internal/storage"
	"github.com/ajay-7105/EvalCI/internal/storage/storagetest"
)

func open(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestContract(t *testing.T) {
	storagetest.RunContract(t, func(t *testing.T) storage.Store {
		return open(t, filepath.Join(t.TempDir(), "h.db"))
	})
}

func fileDigest(t *testing.T, path string) [32]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(data)
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file://"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestNewerSchemaIsRejectedAndLeftUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "h.db")
	db := rawDB(t, path)
	if _, err := db.Exec("CREATE TABLE future (x INTEGER); PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	before, files := fileDigest(t, path), listDir(t, dir)

	_, err := Open(context.Background(), path)
	if !errors.Is(err, storage.ErrNewerSchema) {
		t.Fatalf("err = %v, want ErrNewerSchema", err)
	}
	if fileDigest(t, path) != before {
		t.Fatal("a newer-version file must be byte-for-byte unchanged")
	}
	if got := listDir(t, dir); fmt.Sprint(got) != fmt.Sprint(files) {
		t.Fatalf("directory changed: %v -> %v", files, got)
	}
}

func TestOlderDatabaseIsMigratedWithoutLosingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.db")
	db := rawDB(t, path)
	if _, err := db.Exec("CREATE TABLE keepme (x TEXT); INSERT INTO keepme VALUES ('precious')"); err != nil { // version 0
		t.Fatal(err)
	}
	_ = db.Close()

	s := open(t, path)
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil || v != currentVersion() {
		t.Fatalf("version = %d err=%v, want %d", v, err, currentVersion())
	}
	var x string
	if err := s.db.QueryRow("SELECT x FROM keepme").Scan(&x); err != nil || x != "precious" {
		t.Fatalf("pre-existing data lost: %q %v", x, err)
	}
	if err := s.SaveRun(context.Background(), storagetest.Sample("r", "a", 0)); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationRunnerAppliesLaterStepsAndKeepsRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.db")
	s := open(t, path)
	if err := s.SaveRun(context.Background(), storagetest.Sample("r1", "a", 0)); err != nil {
		t.Fatal(err)
	}
	// simulate a future binary: version 1 -> 2 adds a column
	steps := append(append([]string{}, migrations...), "ALTER TABLE runs ADD COLUMN note TEXT")
	if err := migrate(context.Background(), s.db, steps); err != nil {
		t.Fatal(err)
	}
	var v int
	_ = s.db.QueryRow("PRAGMA user_version").Scan(&v)
	if v != 2 {
		t.Fatalf("version = %d, want 2", v)
	}
	if _, err := s.db.Exec("UPDATE runs SET note = 'x'"); err != nil {
		t.Fatalf("new column missing: %v", err)
	}
	if _, err := s.GetRun(context.Background(), "r1"); err != nil {
		t.Fatalf("existing record lost across upgrade: %v", err)
	}
	// running the same steps again is a no-op
	if err := migrate(context.Background(), s.db, steps); err != nil {
		t.Fatal(err)
	}
}

func TestReopenKeepsRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRun(context.Background(), storagetest.Sample("r1", "a", 0)); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	again := open(t, path)
	if _, err := again.GetRun(context.Background(), "r1"); err != nil {
		t.Fatalf("record lost after reopen: %v", err)
	}
}

func TestOpenCreatesParentDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "h.db")
	open(t, path)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestOpenErrors(t *testing.T) {
	if _, err := Open(context.Background(), ""); err == nil {
		t.Fatal("empty path must error")
	}
	notDB := filepath.Join(t.TempDir(), "junk.db")
	if err := os.WriteFile(notDB, []byte("this is not a database, just text padded out to be longer than a header "+
		"so that sqlite reports a clear error when asked to read its schema version"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), notDB); err == nil {
		t.Fatal("a non-database file must error")
	}
}

func TestReadOnlyLocationReturnsAnErrorNotAPanic(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	ro := filepath.Join(dir, "ro")
	if err := os.Mkdir(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	if _, err := Open(context.Background(), filepath.Join(ro, "h.db")); err == nil {
		t.Fatal("a read-only location must return an error")
	}
}

func TestTwoHandlesOnOneFileSaveConcurrently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.db")
	a, b := open(t, path), open(t, path)
	const each = 15
	var wg sync.WaitGroup
	errs := make(chan error, 2*each)
	for i := 0; i < each; i++ {
		for h, s := range []*Store{a, b} {
			wg.Add(1)
			go func(i, h int, s *Store) {
				defer wg.Done()
				errs <- s.SaveRun(context.Background(), storagetest.Sample(fmt.Sprintf("h%d-%02d", h, i), "shared", time.Duration(i)*time.Second))
			}(i, h, s)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := a.ListRunsByCommit(context.Background(), "shared")
	if err != nil || len(got) != 2*each {
		t.Fatalf("got %d runs err=%v, want %d (SC-010)", len(got), err, 2*each)
	}
	for _, r := range got {
		if len(r.Cases) != 2 || len(r.Usage) != 2 {
			t.Fatalf("run %s incomplete after concurrent writes", r.ID)
		}
	}
	var integrity string
	if err := a.db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity_check = %q err=%v", integrity, err)
	}
}

func TestMidSaveFailureLeavesNothing(t *testing.T) {
	for _, failAt := range []string{"runs", "suites", "cases", "usage"} {
		t.Run(failAt, func(t *testing.T) {
			s := open(t, filepath.Join(t.TempDir(), "h.db"))
			s.hook = func(stage string) error {
				if stage == failAt {
					return errors.New("disk exploded")
				}
				return nil
			}
			if err := s.SaveRun(context.Background(), storagetest.Sample("r1", "a", 0)); err == nil {
				t.Fatal("expected the injected failure")
			}
			s.hook = nil
			if _, err := s.GetRun(context.Background(), "r1"); !errors.Is(err, storage.ErrNotFound) {
				t.Fatalf("partial record visible: %v", err)
			}
			for _, table := range []string{"runs", "run_suites", "run_cases", "run_usage"} {
				var n int
				if err := s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
					t.Fatalf("%s has %d rows after a failed save (err %v)", table, n, err)
				}
			}
			// and the id is still usable afterwards
			if err := s.SaveRun(context.Background(), storagetest.Sample("r1", "a", 0)); err != nil {
				t.Fatalf("retry after failure: %v", err)
			}
		})
	}
}

func TestSchemaMatchesTheContract(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "h.db"))
	want := map[string][]string{
		"runs": {"id", "schema_version", "started_at", "finished_at", "head_sha", "head_branch", "base_ref", "base_sha",
			"mode", "decision", "exit_code", "runs_per_case", "min_drop_threshold", "max_spread", "currency",
			"total_input_tokens", "total_output_tokens", "total_cost_micros", "cost_partial"},
		"run_suites": {"run_id", "suite", "file", "action", "reason"},
		"run_cases": {"run_id", "suite", "case_id", "critical", "status", "baseline_runs", "baseline_failed", "baseline_average",
			"baseline_spread", "candidate_runs", "candidate_failed", "candidate_average", "candidate_spread", "delta"},
		"run_usage": {"run_id", "provider", "model", "calls", "input_tokens", "output_tokens", "unknown_calls", "cost_micros"},
	}
	for table, cols := range want {
		rows, err := s.db.Query("SELECT name FROM pragma_table_info(?)", table)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for rows.Next() {
			var n string
			_ = rows.Scan(&n)
			got = append(got, n)
		}
		_ = rows.Close()
		if fmt.Sprint(got) != fmt.Sprint(cols) {
			t.Errorf("%s columns = %v, want %v", table, got, cols)
		}
	}
	var v int
	_ = s.db.QueryRow("PRAGMA user_version").Scan(&v)
	if v != 1 {
		t.Fatalf("user_version = %d, want 1", v)
	}
	var mode string
	_ = s.db.QueryRow("PRAGMA journal_mode").Scan(&mode)
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
}

func TestUnknownCostIsStoredAsNull(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "h.db"))
	r := storagetest.Sample("r1", "a", 0)
	r.TotalCostMicros = nil
	r.Usage[0].CostMicros = nil
	if err := s.SaveRun(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	var nulls int
	if err := s.db.QueryRow("SELECT (SELECT COUNT(*) FROM runs WHERE total_cost_micros IS NULL) + (SELECT COUNT(*) FROM run_usage WHERE cost_micros IS NULL)").Scan(&nulls); err != nil || nulls != 3 {
		t.Fatalf("NULL cells = %d err=%v, want 3 (unknown must be NULL, never 0)", nulls, err)
	}
}

func TestTimesOrderChronologically(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "h.db"))
	// fractional seconds that would sort wrongly as variable-width RFC 3339 text
	offsets := []time.Duration{0, 500 * time.Millisecond, time.Second, time.Second + time.Nanosecond}
	for i, off := range offsets {
		if err := s.SaveRun(context.Background(), storagetest.Sample(fmt.Sprintf("t%d", i), "a", off)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListRecentRuns(context.Background(), 10)
	if err != nil || len(got) != 4 || got[0].ID != "t3" || got[1].ID != "t2" || got[2].ID != "t1" || got[3].ID != "t0" {
		t.Fatalf("order wrong: %v err=%v", got, err)
	}
}

package factory

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ajay-7105/EvalCI/internal/storage"
	"github.com/ajay-7105/EvalCI/internal/storage/storagetest"
)

func TestOpenSQLiteSavesAndReads(t *testing.T) {
	s, err := Open(context.Background(), storage.Config{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "h.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SaveRun(context.Background(), storagetest.Sample("r1", "a", 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetRun(context.Background(), "r1"); err != nil {
		t.Fatal(err)
	}
}

func TestUnavailableBackendsAreRejectedByName(t *testing.T) {
	for _, driver := range []string{"postgres", "mysql", "SQLITE"} {
		_, err := Open(context.Background(), storage.Config{Driver: driver, Path: "x"})
		if !errors.Is(err, storage.ErrBackendUnavailable) || !strings.Contains(err.Error(), `"`+driver+`"`) {
			t.Errorf("driver %q: err = %v, want ErrBackendUnavailable naming it", driver, err)
		}
	}
}

func TestEmptyDriverIsInvalid(t *testing.T) {
	_, err := Open(context.Background(), storage.Config{})
	if err == nil || errors.Is(err, storage.ErrBackendUnavailable) {
		t.Fatalf("err = %v, want a plain invalid-config error", err)
	}
}

func TestSQLiteNeedsAPath(t *testing.T) {
	if _, err := Open(context.Background(), storage.Config{Driver: "sqlite"}); err == nil {
		t.Fatal("a missing path must error")
	}
}

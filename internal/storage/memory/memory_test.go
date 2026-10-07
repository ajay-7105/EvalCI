package memory

import (
	"testing"

	"github.com/ajay-7105/EvalCI/internal/storage"
	"github.com/ajay-7105/EvalCI/internal/storage/storagetest"
)

func TestContract(t *testing.T) {
	storagetest.RunContract(t, func(*testing.T) storage.Store { return New() })
}

func TestClose(t *testing.T) {
	if err := New().Close(); err != nil {
		t.Fatal(err)
	}
}

package storage

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const modulePath = "github.com/ajay-7105/EvalCI/"

// importsOf returns the import paths of every non-test Go file under dir, keyed by file path
// relative to the repository root.
func importsOf(t *testing.T, root, dir string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		f, perr := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		rel, _ := filepath.Rel(root, p)
		for _, imp := range f.Imports {
			out[filepath.ToSlash(rel)] = append(out[filepath.ToSlash(rel)], strings.Trim(imp.Path.Value, `"`))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal("cannot find the repository root")
	}
	return root
}

func allNonTestImports(t *testing.T, root string) map[string][]string {
	all := map[string][]string{}
	for _, d := range []string{"cmd", "configs", "internal"} {
		for f, imps := range importsOf(t, root, d) {
			all[f] = imps
		}
	}
	return all
}

// Only the SQLite package may import the SQLite driver, and only gitrepo.go (plus the test
// fixture builder) may import go-git, so a backend or git library can be swapped in one place.
func TestThirdPartyBackendsAreConfinedToTheirPackages(t *testing.T) {
	root := repoRoot(t)
	for file, imps := range allNonTestImports(t, root) {
		for _, imp := range imps {
			if strings.HasPrefix(imp, "modernc.org/sqlite") && !strings.HasPrefix(file, "internal/storage/sqlite/") {
				t.Errorf("%s imports the SQLite driver; only internal/storage/sqlite may", file)
			}
			if strings.HasPrefix(imp, "github.com/go-git/") &&
				file != "internal/diffscope/gitrepo.go" && !strings.HasPrefix(file, "internal/diffscope/gittest/") {
				t.Errorf("%s imports go-git; only internal/diffscope/gitrepo.go (and gittest) may", file)
			}
		}
	}
}

// Callers depend on the storage interface; only the CLI may import the driver factory, and
// nothing outside the storage tree imports a concrete backend.
func TestCallersNeverImportAConcreteStorageBackend(t *testing.T) {
	root := repoRoot(t)
	for file, imps := range allNonTestImports(t, root) {
		if strings.HasPrefix(file, "internal/storage/") {
			continue
		}
		for _, imp := range imps {
			switch imp {
			case modulePath + "internal/storage/sqlite", modulePath + "internal/storage/memory":
				t.Errorf("%s imports a concrete storage backend (%s); depend on storage.Store", file, imp)
			case modulePath + "internal/storage/factory":
				if !strings.HasPrefix(file, "cmd/evalci/") {
					t.Errorf("%s imports the storage factory; only the CLI may", file)
				}
			}
		}
	}
	// the storage interface package itself must not import any backend (it would be a cycle)
	for file, imps := range importsOf(t, root, "internal/storage") {
		if file != "internal/storage/types.go" && file != "internal/storage/store.go" && file != "internal/storage/doc.go" {
			continue
		}
		for _, imp := range imps {
			if strings.HasPrefix(imp, modulePath+"internal/storage/") {
				t.Errorf("%s imports %s", file, imp)
			}
		}
	}
}

// Core packages stay vendor neutral: no concrete providers, no vendor-name literals.
func TestCostAndCorePackagesStayVendorNeutral(t *testing.T) {
	root := repoRoot(t)
	literals := []string{`"openai"`, `"anthropic"`, `"ollama"`}
	for _, dir := range []string{"internal/cost", "internal/engine", "internal/assertions", "internal/gating", "internal/diffscope", "internal/storage"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			src, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			rel, _ := filepath.Rel(root, p)
			if strings.HasPrefix(filepath.ToSlash(rel), "internal/storage/storagetest/") {
				return nil // sample data for the shared contract suite, not code that branches
			}
			for _, lit := range literals {
				if strings.Contains(string(src), lit) {
					t.Errorf("%s names a vendor (%s)", rel, lit)
				}
			}
			for _, imp := range []string{"providers/openai", "providers/anthropic", "providers/ollama"} {
				if strings.Contains(string(src), modulePath+"internal/"+imp) {
					t.Errorf("%s imports a concrete provider %s", rel, imp)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

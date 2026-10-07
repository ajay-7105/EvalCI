package engine

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Constitution IV: core packages talk only to the providers.Provider interface and never
// import or branch on a concrete vendor.
func TestCorePackagesAreVendorNeutral(t *testing.T) {
	core := []string{".", "../assertions", "../gating"}
	forbiddenImports := []string{"providers/openai", "providers/anthropic", "providers/ollama"}
	vendorLiterals := []string{`"openai"`, `"anthropic"`, `"ollama"`}

	for _, dir := range core {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), f, src, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range parsed.Imports {
				for _, bad := range forbiddenImports {
					if strings.Contains(imp.Path.Value, bad) {
						t.Errorf("%s imports concrete provider %s", f, imp.Path.Value)
					}
				}
			}
			for _, lit := range vendorLiterals {
				if strings.Contains(string(src), lit) {
					t.Errorf("%s names a vendor (%s); core code must not branch on a provider", f, lit)
				}
			}
		}
	}
}

// The adapters themselves must not hard-code vendor names either; only configuration and the
// registry in cmd/evalci may.
func TestAdaptersDoNotBranchOnVendorNames(t *testing.T) {
	for _, dir := range []string{"../providers/openai", "../providers/anthropic", "../providers/ollama"} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, lit := range []string{`"openai"`, `"anthropic"`, `"ollama"`} {
				if strings.Contains(string(src), lit) {
					t.Errorf("%s contains vendor literal %s", f, lit)
				}
			}
		}
	}
}

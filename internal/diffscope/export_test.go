package diffscope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ajay-7105/EvalCI/internal/diffscope/gittest"
)

func suiteYAML(name, prompt string) string {
	return "name: " + name + "\nprompt: " + prompt +
		"\nmodel: {provider: ollama, name: m, temperature: 0}\ncases:\n  - {id: c, input: \"q\", assertions: [{type: exact-match, expected: OK}]}\n"
}

func TestExportBaseUsesTheBaseCommitNotTheWorkingTree(t *testing.T) {
	r := gittest.New(t)
	r.Write("evals/a.eval.yaml", suiteYAML("a", "prompts/a.txt"))
	r.Write("evals/b.eval.yaml", suiteYAML("b", "prompts/shared.txt"))
	r.Write("prompts/a.txt", "BASE-A")
	r.Write("prompts/shared.txt", "BASE-SHARED")
	r.Write("prompts/unreferenced.txt", "nobody uses me")
	r.Write("README.md", "docs")
	r.Write("evals/notes.txt", "not a suite")
	base := r.Commit("base")

	r.Branch("feature")
	r.Write("prompts/a.txt", "HEAD-A") // working tree and head differ from base
	r.Write("evals/a.eval.yaml", suiteYAML("a-renamed", "prompts/a.txt"))
	r.Commit("feature")

	dir, suites, cleanup, err := ExportBase(open(t, r.Dir), Commit(base), "evals")
	if err != nil {
		t.Fatal(err)
	}
	if len(suites) != 2 || suites[0].Name != "a" || suites[1].Name != "b" {
		t.Fatalf("suites = %+v", suites)
	}
	if suites[0].PromptRel != "prompts/a.txt" || !strings.HasPrefix(suites[0].Prompt, dir) {
		t.Fatalf("prompt paths: rel=%q abs=%q dir=%q", suites[0].PromptRel, suites[0].Prompt, dir)
	}
	got, err := os.ReadFile(suites[0].Prompt)
	if err != nil || string(got) != "BASE-A" {
		t.Fatalf("prompt = %q err=%v, want the base version", got, err)
	}
	for _, mustNot := range []string{"README.md", "prompts/unreferenced.txt", "evals/notes.txt"} {
		if _, err := os.Stat(filepath.Join(dir, mustNot)); err == nil {
			t.Errorf("%s must not be exported", mustNot)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "prompts", "shared.txt")); err != nil {
		t.Errorf("referenced prompt missing: %v", err)
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("cleanup must remove %s", dir)
	}
}

func TestExportBaseWithoutEvalsIsEmptyNotAnError(t *testing.T) {
	r := gittest.New(t)
	r.Write("README.md", "no suites yet")
	base := r.Commit("base")
	dir, suites, cleanup, err := ExportBase(open(t, r.Dir), Commit(base), "evals")
	if err != nil || len(suites) != 0 {
		t.Fatalf("suites=%v err=%v", suites, err)
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("cleanup must remove the directory")
	}
}

func TestExportBaseEvalsDirWithNoSuiteFiles(t *testing.T) {
	r := gittest.New(t)
	r.Write("evals/readme.txt", "empty")
	base := r.Commit("base")
	_, suites, cleanup, err := ExportBase(open(t, r.Dir), Commit(base), "evals")
	if err != nil || len(suites) != 0 {
		t.Fatalf("suites=%v err=%v", suites, err)
	}
	cleanup()
}

func TestExportBaseMissingPromptNamesTheSuite(t *testing.T) {
	r := gittest.New(t)
	r.Write("evals/a.eval.yaml", suiteYAML("a", "prompts/gone.txt"))
	base := r.Commit("base")
	_, _, _, err := ExportBase(open(t, r.Dir), Commit(base), "evals")
	if err == nil || !strings.Contains(err.Error(), `"a"`) || !strings.Contains(err.Error(), "prompts/gone.txt") {
		t.Fatalf("err = %v", err)
	}
}

func TestExportBaseInvalidSuiteIsAnError(t *testing.T) {
	r := gittest.New(t)
	r.Write("evals/a.eval.yaml", "name: a\nbogus: 1\n")
	base := r.Commit("base")
	_, _, _, err := ExportBase(open(t, r.Dir), Commit(base), "evals")
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("err = %v", err)
	}
}

func TestExportBaseRefusesPathsEscapingTheDirectory(t *testing.T) {
	r := gittest.New(t)
	r.Write("evals/a.eval.yaml", suiteYAML("a", "../outside.txt"))
	base := r.Commit("base")
	_, _, _, err := ExportBase(open(t, r.Dir), Commit(base), "evals")
	if err == nil {
		t.Fatal("a prompt path escaping the repository must be rejected")
	}
}

func TestListDir(t *testing.T) {
	r := gittest.New(t)
	r.Write("evals/a.eval.yaml", "x")
	r.Write("evals/sub/inner.eval.yaml", "y")
	sha := r.Commit("c")
	g := open(t, r.Dir)
	names, err := g.ListDir(Commit(sha), "evals")
	if err != nil || len(names) != 1 || names[0] != "a.eval.yaml" {
		t.Fatalf("names=%v err=%v (subdirectories must not be listed)", names, err)
	}
	if _, err := g.ListDir(Commit(sha), "nope"); err == nil {
		t.Fatal("missing directory must error")
	}
}

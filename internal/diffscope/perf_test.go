package diffscope

import (
	"fmt"
	"testing"
	"time"

	"github.com/ajay-7105/EvalCI/configs"
	"github.com/ajay-7105/EvalCI/internal/diffscope/gittest"
)

// SC-005: resolving which suites are affected takes under 2 seconds for up to 1,000 changed files.
func TestResolvingScopeFor1000ChangedFilesIsFast(t *testing.T) {
	if testing.Short() {
		t.Skip("performance check skipped with -short")
	}
	r := gittest.New(t)
	var suites []configs.Suite
	for i := 0; i < 100; i++ {
		r.Write(fmt.Sprintf("evals/s%03d.eval.yaml", i), fmt.Sprintf("name: s%03d\n", i))
		suites = append(suites, configs.Suite{
			Name: fmt.Sprintf("s%03d", i), File: fmt.Sprintf("evals/s%03d.eval.yaml", i),
			PromptRel: fmt.Sprintf("prompts/p%03d.txt", i%50),
		})
	}
	for i := 0; i < 1000; i++ {
		r.Write(fmt.Sprintf("prompts/p%03d.txt", i%50), "base")
		r.Write(fmt.Sprintf("src/file%04d.txt", i), "base")
	}
	r.Commit("base")
	r.Branch("feature")
	for i := 0; i < 1000; i++ {
		r.Write(fmt.Sprintf("src/file%04d.txt", i), "changed "+fmt.Sprint(i))
	}
	r.Write("prompts/p007.txt", "changed prompt")
	r.Commit("change 1,001 files")

	g, err := OpenGit(r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	cs, err := BuildChangeSet(g, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	decisions := Select(SelectInput{Candidates: suites, Base: suites, EvalsDir: "evals", Changes: cs.Files})
	elapsed := time.Since(start)

	if len(cs.Files) != 1001 {
		t.Fatalf("change set has %d files, want 1001", len(cs.Files))
	}
	selected := 0
	for _, d := range decisions {
		if d.Action == ActionRun {
			selected++
		}
	}
	if selected != 2 { // p007 is used by suites 7 and 57
		t.Fatalf("selected %d suites, want 2", selected)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("scope resolution took %v, want under 2s (SC-005)", elapsed)
	}
	t.Logf("resolved 1,001 changed files across 100 suites in %v", elapsed)
}

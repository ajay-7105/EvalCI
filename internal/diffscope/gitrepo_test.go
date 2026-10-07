package diffscope

import (
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/ajay-7105/EvalCI/internal/diffscope/gittest"
)

func open(t *testing.T, dir string) *GitRepo {
	t.Helper()
	g, err := OpenGit(dir)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func byPath(files []FileChange) map[string]FileChange {
	m := map[string]FileChange{}
	for _, f := range files {
		m[f.Path] = f
	}
	return m
}

func TestOpenGitNotARepository(t *testing.T) {
	if _, err := OpenGit(t.TempDir()); !errors.Is(err, ErrNotARepository) {
		t.Fatalf("err = %v, want ErrNotARepository", err)
	}
}

func TestChangedFilesAddModifyDeleteRename(t *testing.T) {
	r := gittest.New(t)
	r.Write("keep.txt", "keep\n")
	r.Write("edit.txt", "one\n")
	r.Write("gone.txt", "bye\n")
	r.Write("prompts/old.txt", "a long enough prompt body so rename similarity is exact\n")
	base := r.Commit("base")

	r.Branch("feature")
	r.Write("edit.txt", "two\n")
	r.Remove("gone.txt")
	r.Write("new.txt", "fresh\n")
	r.Rename("prompts/old.txt", "prompts/new.txt")
	head := r.Commit("feature work")

	g := open(t, r.Dir)
	files, err := g.ChangedFiles(Commit(base), Commit(head))
	if err != nil {
		t.Fatal(err)
	}
	m := byPath(files)
	if m["edit.txt"].Status != StatusModified || m["gone.txt"].Status != StatusDeleted || m["new.txt"].Status != StatusAdded {
		t.Fatalf("unexpected statuses: %+v", files)
	}
	if _, touched := m["keep.txt"]; touched {
		t.Fatalf("unchanged file reported: %+v", files)
	}
	ren := m["prompts/new.txt"]
	if ren.Status != StatusRenamed || ren.OldPath != "prompts/old.txt" {
		t.Fatalf("rename not detected: %+v", files)
	}
	paths := ren.Paths()
	sort.Strings(paths)
	if len(paths) != 2 || paths[0] != "prompts/new.txt" || paths[1] != "prompts/old.txt" {
		t.Fatalf("rename must expose both paths: %v", paths)
	}
}

func TestMergeBaseAcrossBranchPoint(t *testing.T) {
	r := gittest.New(t)
	r.Write("a.txt", "1\n")
	root := r.Commit("root")
	r.Branch("feature")
	r.Write("f.txt", "f\n")
	r.Commit("feature commit")
	r.Checkout("main")
	r.Write("m.txt", "m\n")
	r.Commit("main moved on")
	r.Checkout("feature")

	g := open(t, r.Dir)
	base, err := g.Resolve("main")
	if err != nil {
		t.Fatal(err)
	}
	head, err := g.Resolve("feature")
	if err != nil {
		t.Fatal(err)
	}
	mb, err := g.MergeBase(base, head)
	if err != nil || string(mb) != root {
		t.Fatalf("merge base = %s err=%v, want %s", mb, err, root)
	}
}

func TestBuildChangeSetIsThePullRequestDiff(t *testing.T) {
	r := gittest.New(t)
	r.Write("a.txt", "1\n")
	r.Commit("root")
	r.Branch("feature")
	r.Write("f.txt", "f\n")
	r.Commit("feature commit")
	r.Checkout("main")
	r.Write("m.txt", "m\n") // changed on main after the branch point: must NOT appear
	r.Commit("main moved on")
	r.Checkout("feature")

	cs, err := BuildChangeSet(open(t, r.Dir), "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Files) != 1 || cs.Files[0].Path != "f.txt" || cs.Files[0].Status != StatusAdded {
		t.Fatalf("files = %+v, want only f.txt added", cs.Files)
	}
	if cs.HeadBranch != "feature" || cs.BaseRef != "main" || cs.HeadRef != "HEAD" || cs.MergeBase == cs.Base {
		t.Fatalf("unexpected change set: %+v", cs)
	}
}

func TestBuildChangeSetHeadMustBeCheckedOut(t *testing.T) {
	r := gittest.New(t)
	r.Write("a.txt", "1\n")
	r.Commit("root")
	r.Branch("feature")
	r.Write("f.txt", "f\n")
	r.Commit("feature commit")
	r.Checkout("main")

	_, err := BuildChangeSet(open(t, r.Dir), "main", "feature")
	if !errors.Is(err, ErrHeadNotCheckedOut) || !strings.Contains(err.Error(), "main is checked out") || !strings.Contains(err.Error(), "check out") {
		t.Fatalf("err = %v", err)
	}
	// naming the checked-out branch explicitly is fine
	if _, err := BuildChangeSet(open(t, r.Dir), "main", "main"); err != nil {
		t.Fatalf("explicit head equal to the checked-out branch must work: %v", err)
	}
}

func TestBuildChangeSetSameCommitHasNoChanges(t *testing.T) {
	r := gittest.New(t)
	r.Write("a.txt", "1\n")
	r.Commit("root")
	r.Branch("feature")
	cs, err := BuildChangeSet(open(t, r.Dir), "main", "")
	if err != nil || len(cs.Files) != 0 || cs.Base != cs.Head || cs.MergeBase != cs.Head {
		t.Fatalf("cs=%+v err=%v", cs, err)
	}
}

func TestDetachedHeadBranchName(t *testing.T) {
	r := gittest.New(t)
	r.Write("a.txt", "1\n")
	r.Commit("root")
	r.Branch("feature")
	r.Write("f.txt", "f\n")
	sha := r.Commit("feature commit")
	r.CheckoutDetached(sha)
	g := open(t, r.Dir)

	cs, err := BuildChangeSet(g, "main", "")
	if err != nil || cs.HeadBranch != "" {
		t.Fatalf("detached HEAD must have no branch name: %+v err=%v", cs, err)
	}
	cs, err = BuildChangeSet(g, "main", "feature")
	if err != nil || cs.HeadBranch != "feature" {
		t.Fatalf("named head branch expected: %+v err=%v", cs, err)
	}
	cs, err = BuildChangeSet(g, "main", sha)
	if err != nil || cs.HeadBranch != "" {
		t.Fatalf("raw SHA head must have no branch name: %+v err=%v", cs, err)
	}
}

func TestUnresolvableRefs(t *testing.T) {
	r := gittest.New(t)
	r.Write("a.txt", "1\n")
	r.Commit("root")
	g := open(t, r.Dir)
	if _, err := BuildChangeSet(g, "nope", ""); !errors.Is(err, ErrRefNotFound) || !strings.Contains(err.Error(), "base") {
		t.Fatalf("err = %v", err)
	}
	if _, err := BuildChangeSet(g, "main", "nope"); !errors.Is(err, ErrRefNotFound) || !strings.Contains(err.Error(), "head") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnrelatedHistoriesHaveNoCommonAncestor(t *testing.T) {
	r := gittest.New(t)
	r.Write("a.txt", "1\n")
	r.Commit("root")
	r.Orphan("other")
	r.Write("b.txt", "2\n")
	r.Commit("orphan root")

	_, err := BuildChangeSet(open(t, r.Dir), "main", "")
	if !errors.Is(err, ErrNoCommonAncestor) || !strings.Contains(err.Error(), "fetch more history") {
		t.Fatalf("err = %v", err)
	}
}

func TestShallowHistoryGivesAnActionableError(t *testing.T) {
	r := gittest.New(t)
	r.Write("a.txt", "1\n")
	root := r.Commit("root")
	r.Write("a.txt", "2\n")
	mid := r.Commit("mid")
	r.Branch("feature")
	r.Write("f.txt", "f\n")
	r.Commit("feature commit")
	r.Checkout("main")
	r.Write("m.txt", "m\n")
	r.Commit("main tip")
	r.Checkout("feature")
	_ = mid
	r.ForgetObject(root) // simulate a shallow clone: the oldest commit is missing

	_, err := BuildChangeSet(open(t, r.Dir), "main", "")
	if !errors.Is(err, ErrNoCommonAncestor) || !strings.Contains(err.Error(), "fetch more history") {
		t.Fatalf("err = %v", err)
	}
}

func TestFileAt(t *testing.T) {
	r := gittest.New(t)
	r.Write("evals/a.eval.yaml", "v1\n")
	first := r.Commit("one")
	r.Write("evals/a.eval.yaml", "v2\n")
	second := r.Commit("two")
	g := open(t, r.Dir)

	for sha, want := range map[string]string{first: "v1\n", second: "v2\n"} {
		got, err := g.FileAt(Commit(sha), "evals/a.eval.yaml")
		if err != nil || string(got) != want {
			t.Fatalf("FileAt(%s) = %q, %v; want %q", sha[:7], got, err, want)
		}
	}
	if _, err := g.FileAt(Commit(first), "missing.txt"); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("err = %v, want ErrFileNotFound", err)
	}
	if _, err := g.FileAt(Commit(first), "nodir/x.txt"); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("missing directory: err = %v, want ErrFileNotFound", err)
	}
}

func TestDirty(t *testing.T) {
	r := gittest.New(t)
	r.Write("a.txt", "1\n")
	r.Commit("root")
	g := open(t, r.Dir)

	if d, err := g.Dirty(); err != nil || d {
		t.Fatalf("clean tree: dirty=%v err=%v", d, err)
	}
	r.Write("untracked.txt", "x\n") // untracked files (results, history db) are not "dirty"
	if d, err := g.Dirty(); err != nil || d {
		t.Fatalf("untracked file must not count: dirty=%v err=%v", d, err)
	}
	r.Write("a.txt", "changed\n")
	if d, err := g.Dirty(); err != nil || !d {
		t.Fatalf("modified tracked file must count: dirty=%v err=%v", d, err)
	}
}

func TestRootAndCheckedOut(t *testing.T) {
	r := gittest.New(t)
	r.Write("a.txt", "1\n")
	sha := r.Commit("root")
	g := open(t, r.Dir)
	if g.Root() != r.Dir {
		t.Fatalf("root = %s, want %s", g.Root(), r.Dir)
	}
	c, branch, err := g.CheckedOut()
	if err != nil || string(c) != sha || branch != "main" {
		t.Fatalf("checked out = %s %s %v", c, branch, err)
	}
	if Commit(sha).Short() != sha[:7] || Commit("abc").Short() != "abc" {
		t.Fatal("Short misbehaves")
	}
}

func TestOpenFromSubdirectory(t *testing.T) {
	r := gittest.New(t)
	r.Write("sub/a.txt", "1\n")
	r.Commit("root")
	g, err := OpenGit(r.Dir + "/sub")
	if err != nil || g.Root() != r.Dir {
		t.Fatalf("root = %v err=%v, want %s", g, err, r.Dir)
	}
}

// Package gittest builds real git repositories in temporary directories for tests, using
// go-git (no git binary needed).
package gittest

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Repo is a temporary repository on branch main.
type Repo struct {
	// Dir is the working-tree root.
	Dir string

	t     *testing.T
	r     *git.Repository
	wt    *git.Worktree
	clock time.Time
}

// New creates an empty repository whose default branch is main.
func New(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	r, err := git.PlainInitWithOptions(dir, &git.PlainInitOptions{
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	})
	if err != nil {
		t.Fatal(err)
	}
	wt, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	return &Repo{Dir: dir, t: t, r: r, wt: wt, clock: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

// Write creates or overwrites a file (slash path relative to the root).
func (g *Repo) Write(path, content string) {
	g.t.Helper()
	full := filepath.Join(g.Dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		g.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		g.t.Fatal(err)
	}
}

// Remove deletes a file from the working tree.
func (g *Repo) Remove(path string) {
	g.t.Helper()
	if err := os.Remove(filepath.Join(g.Dir, filepath.FromSlash(path))); err != nil {
		g.t.Fatal(err)
	}
}

// Rename moves a file in the working tree.
func (g *Repo) Rename(oldPath, newPath string) {
	g.t.Helper()
	to := filepath.Join(g.Dir, filepath.FromSlash(newPath))
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		g.t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(g.Dir, filepath.FromSlash(oldPath)), to); err != nil {
		g.t.Fatal(err)
	}
}

// Commit stages every change (including deletions) and commits; it returns the commit SHA.
func (g *Repo) Commit(msg string) string {
	g.t.Helper()
	if err := g.wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		g.t.Fatal(err)
	}
	g.clock = g.clock.Add(time.Minute)
	h, err := g.wt.Commit(msg, &git.CommitOptions{
		Author:            &object.Signature{Name: "t", Email: "t@example.com", When: g.clock},
		AllowEmptyCommits: true,
	})
	if err != nil {
		g.t.Fatal(err)
	}
	return h.String()
}

// Branch creates a branch at HEAD and checks it out.
func (g *Repo) Branch(name string) {
	g.t.Helper()
	if err := g.wt.Checkout(&git.CheckoutOptions{Branch: plumbing.NewBranchReferenceName(name), Create: true}); err != nil {
		g.t.Fatal(err)
	}
}

// Checkout switches to an existing branch.
func (g *Repo) Checkout(name string) {
	g.t.Helper()
	if err := g.wt.Checkout(&git.CheckoutOptions{Branch: plumbing.NewBranchReferenceName(name)}); err != nil {
		g.t.Fatal(err)
	}
}

// CheckoutDetached checks out a commit, leaving HEAD detached.
func (g *Repo) CheckoutDetached(sha string) {
	g.t.Helper()
	if err := g.wt.Checkout(&git.CheckoutOptions{Hash: plumbing.NewHash(sha)}); err != nil {
		g.t.Fatal(err)
	}
}

// Orphan points HEAD at a new unborn branch so the next commit has no parent.
func (g *Repo) Orphan(name string) {
	g.t.Helper()
	ref := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(name))
	if err := g.r.Storer.SetReference(ref); err != nil {
		g.t.Fatal(err)
	}
}

// Head returns the SHA of the checked-out commit.
func (g *Repo) Head() string {
	g.t.Helper()
	h, err := g.r.Head()
	if err != nil {
		g.t.Fatal(err)
	}
	return h.Hash().String()
}

// ForgetObject deletes a loose object (a commit, for instance) to simulate a shallow clone
// whose older history is missing.
func (g *Repo) ForgetObject(sha string) {
	g.t.Helper()
	p := filepath.Join(g.Dir, ".git", "objects", sha[:2], sha[2:])
	if err := os.Remove(p); err != nil {
		g.t.Fatal(err)
	}
}

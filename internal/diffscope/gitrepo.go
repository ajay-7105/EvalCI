package diffscope

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/utils/merkletrie"
)

// dirtyTimeout bounds the best-effort dirty check: go-git's status can be slow on huge trees.
const dirtyTimeout = 3 * time.Second

// ErrDirtyCheckTimeout means the dirty-tree check took too long and was skipped.
var ErrDirtyCheckTimeout = errors.New("dirty check timed out")

// GitRepo implements Repository with go-git (pure Go; no git binary is required).
type GitRepo struct {
	repo *git.Repository
	root string
}

// OpenGit opens the repository containing dir.
func OpenGit(dir string) (*GitRepo, error) {
	r, err := git.PlainOpenWithOptions(dir, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		if errors.Is(err, git.ErrRepositoryNotExists) {
			return nil, fmt.Errorf("%w: %s", ErrNotARepository, dir)
		}
		return nil, fmt.Errorf("opening repository at %s: %w", dir, err)
	}
	wt, err := r.Worktree()
	if err != nil {
		return nil, fmt.Errorf("reading working tree of %s: %w", dir, err)
	}
	return &GitRepo{repo: r, root: wt.Filesystem.Root()}, nil
}

// Root implements Repository.
func (g *GitRepo) Root() string { return g.root }

// Resolve implements Repository.
func (g *GitRepo) Resolve(ref string) (Commit, error) {
	h, err := g.repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return "", fmt.Errorf("%w: %q: %w", ErrRefNotFound, ref, err)
	}
	c, err := g.repo.CommitObject(*h)
	if err != nil {
		return "", fmt.Errorf("%w: %q does not point at a commit: %w", ErrRefNotFound, ref, err)
	}
	return Commit(c.Hash.String()), nil
}

// CheckedOut implements Repository.
func (g *GitRepo) CheckedOut() (Commit, string, error) {
	head, err := g.repo.Head()
	if err != nil {
		return "", "", fmt.Errorf("reading HEAD: %w", err)
	}
	branch := ""
	if head.Name().IsBranch() {
		branch = head.Name().Short()
	}
	return Commit(head.Hash().String()), branch, nil
}

// MergeBase implements Repository.
func (g *GitRepo) MergeBase(a, b Commit) (Commit, error) {
	ca, err := g.commit(a)
	if err != nil {
		return "", err
	}
	cb, err := g.commit(b)
	if err != nil {
		return "", err
	}
	bases, err := ca.MergeBase(cb)
	if err != nil {
		return "", noAncestor(fmt.Sprintf("history is incomplete (%v)", err))
	}
	if len(bases) == 0 {
		return "", noAncestor("the branches share no history")
	}
	return Commit(bases[0].Hash.String()), nil
}

func noAncestor(why string) error {
	return fmt.Errorf("%w: %s; if this is a shallow clone, fetch more history (for example `git fetch --unshallow`)", ErrNoCommonAncestor, why)
}

func (g *GitRepo) commit(c Commit) (*object.Commit, error) {
	obj, err := g.repo.CommitObject(plumbing.NewHash(string(c)))
	if err != nil {
		return nil, noAncestor(fmt.Sprintf("commit %s is not available (%v)", c.Short(), err))
	}
	return obj, nil
}

// ChangedFiles implements Repository.
func (g *GitRepo) ChangedFiles(from, to Commit) ([]FileChange, error) {
	cf, err := g.commit(from)
	if err != nil {
		return nil, err
	}
	ct, err := g.commit(to)
	if err != nil {
		return nil, err
	}
	tf, err := cf.Tree()
	if err != nil {
		return nil, fmt.Errorf("reading tree of %s: %w", from.Short(), err)
	}
	tt, err := ct.Tree()
	if err != nil {
		return nil, fmt.Errorf("reading tree of %s: %w", to.Short(), err)
	}
	changes, err := object.DiffTreeWithOptions(context.Background(), tf, tt, &object.DiffTreeOptions{
		DetectRenames:    true,
		RenameScore:      60,
		RenameLimit:      1000,
		OnlyExactRenames: false,
	})
	if err != nil {
		return nil, fmt.Errorf("diffing trees: %w", err)
	}
	out := make([]FileChange, 0, len(changes))
	for _, ch := range changes {
		action, err := ch.Action()
		if err != nil {
			return nil, fmt.Errorf("classifying change: %w", err)
		}
		from, to := ch.From.Name, ch.To.Name
		switch {
		case action == merkletrie.Insert:
			out = append(out, FileChange{Status: StatusAdded, Path: to})
		case action == merkletrie.Delete:
			out = append(out, FileChange{Status: StatusDeleted, Path: from})
		case from != to:
			out = append(out, FileChange{Status: StatusRenamed, Path: to, OldPath: from})
		default:
			out = append(out, FileChange{Status: StatusModified, Path: to})
		}
	}
	return out, nil
}

// FileAt implements Repository.
func (g *GitRepo) FileAt(c Commit, path string) ([]byte, error) {
	obj, err := g.commit(c)
	if err != nil {
		return nil, err
	}
	f, err := obj.File(filepath.ToSlash(path))
	if err != nil {
		if errors.Is(err, object.ErrFileNotFound) || errors.Is(err, object.ErrDirectoryNotFound) {
			return nil, fmt.Errorf("%w: %s at %s", ErrFileNotFound, path, c.Short())
		}
		return nil, fmt.Errorf("reading %s at %s: %w", path, c.Short(), err)
	}
	r, err := f.Reader()
	if err != nil {
		return nil, fmt.Errorf("opening %s at %s: %w", path, c.Short(), err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading %s at %s: %w", path, c.Short(), err)
	}
	return data, nil
}

// ListDir implements Repository.
func (g *GitRepo) ListDir(c Commit, dir string) ([]string, error) {
	obj, err := g.commit(c)
	if err != nil {
		return nil, err
	}
	tree, err := obj.Tree()
	if err != nil {
		return nil, fmt.Errorf("reading tree of %s: %w", c.Short(), err)
	}
	sub, err := tree.Tree(filepath.ToSlash(dir))
	if err != nil {
		if errors.Is(err, object.ErrDirectoryNotFound) || errors.Is(err, object.ErrEntryNotFound) {
			return nil, fmt.Errorf("%w: directory %s at %s", ErrFileNotFound, dir, c.Short())
		}
		return nil, fmt.Errorf("reading directory %s at %s: %w", dir, c.Short(), err)
	}
	var names []string
	for _, e := range sub.Entries {
		if e.Mode.IsFile() {
			names = append(names, e.Name)
		}
	}
	return names, nil
}

// Dirty implements Repository: true when tracked files have staged or unstaged changes.
// Untracked files (such as the result file or history database) do not count. The check is
// best effort and returns ErrDirtyCheckTimeout when it takes longer than dirtyTimeout.
func (g *GitRepo) Dirty() (bool, error) {
	type result struct {
		dirty bool
		err   error
	}
	done := make(chan result, 1)
	go func() {
		wt, err := g.repo.Worktree()
		if err != nil {
			done <- result{err: err}
			return
		}
		st, err := wt.Status()
		if err != nil {
			done <- result{err: err}
			return
		}
		for _, s := range st {
			if changed(s.Staging) || changed(s.Worktree) {
				done <- result{dirty: true}
				return
			}
		}
		done <- result{}
	}()
	select {
	case r := <-done:
		return r.dirty, r.err
	case <-time.After(dirtyTimeout):
		return false, ErrDirtyCheckTimeout
	}
}

func changed(c git.StatusCode) bool {
	return c != git.Unmodified && c != git.Untracked
}

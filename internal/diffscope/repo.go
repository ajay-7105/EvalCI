package diffscope

import (
	"errors"
	"fmt"
	"regexp"
)

// Commit is a full hex commit identifier.
type Commit string

// Short returns the first seven characters, for display.
func (c Commit) Short() string {
	if len(c) > 7 {
		return string(c[:7])
	}
	return string(c)
}

// File change statuses.
const (
	StatusAdded    = "added"
	StatusModified = "modified"
	StatusDeleted  = "deleted"
	StatusRenamed  = "renamed"
)

// FileChange is one file difference between two commits. Paths are slash-separated and
// relative to the repository root. For a rename, OldPath is the previous path.
type FileChange struct {
	Status  string
	Path    string
	OldPath string
}

// Paths returns every path the change touches: both paths for a rename.
func (f FileChange) Paths() []string {
	if f.OldPath != "" && f.OldPath != f.Path {
		return []string{f.OldPath, f.Path}
	}
	return []string{f.Path}
}

// ChangeSet is the pull-request style difference: from the merge base of the base and head
// commits to the head commit.
type ChangeSet struct {
	Base      Commit
	Head      Commit
	MergeBase Commit
	// BaseRef and HeadRef are the refs as the user gave them.
	BaseRef string
	HeadRef string
	// HeadBranch is the branch name of the head when known (empty when detached).
	HeadBranch string
	Files      []FileChange
}

// Sentinel errors returned by Repository implementations.
var (
	// ErrNotARepository means the directory is not inside a git repository.
	ErrNotARepository = errors.New("not a git repository")
	// ErrRefNotFound means a branch, tag, or commit could not be resolved.
	ErrRefNotFound = errors.New("ref not found")
	// ErrNoCommonAncestor means base and head share no history (or the history is missing).
	ErrNoCommonAncestor = errors.New("no common ancestor")
	// ErrHeadNotCheckedOut means the named head is not the checked-out commit.
	ErrHeadNotCheckedOut = errors.New("head is not the checked-out commit")
	// ErrFileNotFound means a path does not exist at a commit.
	ErrFileNotFound = errors.New("file not found at commit")
)

// Repository is the small slice of git that scoping needs. Only the go-git implementation
// touches a git library, which keeps selection logic pure and testable.
type Repository interface {
	// Root returns the repository's working-tree root directory.
	Root() string
	// Resolve turns a branch, tag, or commit id into a commit (ErrRefNotFound if unknown).
	Resolve(ref string) (Commit, error)
	// CheckedOut returns the checked-out commit and its branch name ("" when detached).
	CheckedOut() (Commit, string, error)
	// MergeBase returns the best common ancestor (ErrNoCommonAncestor if none).
	MergeBase(a, b Commit) (Commit, error)
	// ChangedFiles lists files that differ between two commits, with renames detected.
	ChangedFiles(from, to Commit) ([]FileChange, error)
	// FileAt reads a file at a commit (ErrFileNotFound if absent).
	FileAt(c Commit, path string) ([]byte, error)
	// ListDir lists the names of the files directly inside dir at a commit (ErrFileNotFound
	// if the directory does not exist).
	ListDir(c Commit, dir string) ([]string, error)
	// Dirty reports whether tracked files have uncommitted changes.
	Dirty() (bool, error)
}

var shaLike = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

// BuildChangeSet resolves the refs, checks that headRef is the checked-out commit, and
// computes the change set from the merge base to the head. An empty headRef means HEAD.
func BuildChangeSet(repo Repository, baseRef, headRef string) (ChangeSet, error) {
	if headRef == "" {
		headRef = "HEAD"
	}
	base, err := repo.Resolve(baseRef)
	if err != nil {
		return ChangeSet{}, fmt.Errorf("base %q: %w", baseRef, err)
	}
	head, err := repo.Resolve(headRef)
	if err != nil {
		return ChangeSet{}, fmt.Errorf("head %q: %w", headRef, err)
	}
	checked, branch, err := repo.CheckedOut()
	if err != nil {
		return ChangeSet{}, fmt.Errorf("reading the checked-out commit: %w", err)
	}
	if head != checked {
		current := branch
		if current == "" {
			current = "detached HEAD at " + checked.Short()
		}
		return ChangeSet{}, fmt.Errorf("%w: %q resolves to %s but %s is checked out; check out %q first",
			ErrHeadNotCheckedOut, headRef, head.Short(), current, headRef)
	}
	mb, err := repo.MergeBase(base, head)
	if err != nil {
		return ChangeSet{}, err
	}
	files, err := repo.ChangedFiles(mb, head)
	if err != nil {
		return ChangeSet{}, fmt.Errorf("listing changed files: %w", err)
	}
	cs := ChangeSet{Base: base, Head: head, MergeBase: mb, BaseRef: baseRef, HeadRef: headRef, Files: files}
	switch {
	case branch != "":
		cs.HeadBranch = branch
	case headRef != "HEAD" && !shaLike.MatchString(headRef):
		cs.HeadBranch = headRef
	}
	return cs, nil
}

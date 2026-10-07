package diffscope

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ajay-7105/EvalCI/configs"
)

// ExportBase materializes the base commit's suites and the prompts they reference into a
// temporary directory laid out like the repository, so the existing baseline machinery can
// read it unchanged. It reads from git objects only; the working tree is never touched.
//
// It returns the directory, the parsed base suites (with prompt paths under that directory),
// and a cleanup func that removes it. A base commit with no eval suites yields an empty suite
// list (every candidate suite is then new). A prompt a base suite references but the base
// commit lacks is an error naming the suite.
func ExportBase(repo Repository, base Commit, evalsDir string) (dir string, suites []configs.Suite, cleanup func(), err error) {
	dir, err = os.MkdirTemp("", "evalci-base-*")
	if err != nil {
		return "", nil, nil, fmt.Errorf("creating temporary baseline directory: %w", err)
	}
	tmp := dir
	remove := func() { _ = os.RemoveAll(tmp) }
	cleanup = remove
	defer func() {
		if err != nil {
			remove() // the named results were already reset by the failing return
		}
	}()

	names, lerr := repo.ListDir(base, evalsDir)
	switch {
	case errors.Is(lerr, ErrFileNotFound):
		return dir, nil, cleanup, nil // no evals/ at base: nothing to compare against
	case lerr != nil:
		return "", nil, nil, fmt.Errorf("listing %s at %s: %w", evalsDir, base.Short(), lerr)
	}
	exported := 0
	for _, n := range names {
		if !strings.HasSuffix(n, ".eval.yaml") {
			continue
		}
		rel := evalsDir + "/" + n
		data, ferr := repo.FileAt(base, rel)
		if ferr != nil {
			return "", nil, nil, fmt.Errorf("reading %s at %s: %w", rel, base.Short(), ferr)
		}
		if werr := writeUnder(dir, rel, data); werr != nil {
			return "", nil, nil, werr
		}
		exported++
	}
	if exported == 0 {
		return dir, nil, cleanup, nil
	}

	suites, err = configs.LoadSuites(dir, evalsDir)
	if err != nil {
		return "", nil, nil, fmt.Errorf("loading baseline suites from %s: %w", base.Short(), err)
	}
	for _, s := range suites {
		data, ferr := repo.FileAt(base, s.PromptRel)
		if ferr != nil {
			err = fmt.Errorf("baseline suite %q (%s) references prompt %s, which is missing at %s: %w",
				s.Name, filepath.Base(s.File), s.PromptRel, base.Short(), ferr)
			return "", nil, nil, err
		}
		if werr := writeUnder(dir, s.PromptRel, data); werr != nil {
			return "", nil, nil, werr
		}
	}
	return dir, suites, cleanup, nil
}

func writeUnder(root, rel string, data []byte) error {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to export %q outside the baseline directory", rel)
	}
	full := filepath.Join(root, clean)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("creating directory for %s: %w", rel, err)
	}
	if err := os.WriteFile(full, data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", rel, err)
	}
	return nil
}

package engine

import (
	"fmt"
	"path/filepath"

	"github.com/ajay-7105/EvalCI/configs"
)

// SuitePair couples a candidate suite with the baseline suite supplying the baseline
// variant's prompt and model. Cases and assertions always come from the candidate so both
// variants are scored on identical inputs. A pair with NoBaseline set is a new suite: only the
// candidate runs and its cases get status NEW.
type SuitePair struct {
	Candidate configs.Suite
	Baseline  configs.Suite
	// NoBaseline marks a suite that has no baseline counterpart.
	NoBaseline bool
}

// ResolveBaseline matches each candidate suite to the same-named suite file in the baseline
// set. A candidate with no counterpart is an error naming the suite, unless allowMissing is
// set, in which case it becomes a candidate-only pair.
func ResolveBaseline(candidates, baseline []configs.Suite, allowMissing bool) ([]SuitePair, error) {
	byFile := make(map[string]configs.Suite, len(baseline))
	for _, b := range baseline {
		byFile[filepath.Base(b.File)] = b
	}
	pairs := make([]SuitePair, 0, len(candidates))
	for _, c := range candidates {
		b, ok := byFile[filepath.Base(c.File)]
		if !ok && allowMissing {
			pairs = append(pairs, SuitePair{Candidate: c, NoBaseline: true})
			continue
		}
		if !ok {
			return nil, fmt.Errorf("suite %q (%s) has no baseline counterpart named %s", c.Name, c.File, filepath.Base(c.File))
		}
		pairs = append(pairs, SuitePair{Candidate: c, Baseline: b})
	}
	return pairs, nil
}

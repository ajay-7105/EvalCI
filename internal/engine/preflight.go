package engine

import (
	"errors"
	"fmt"
	"sort"

	"github.com/ajay-7105/EvalCI/configs"
	"github.com/ajay-7105/EvalCI/internal/assertions"
)

// PreflightInput is everything pre-flight validation needs; no model call is made.
type PreflightInput struct {
	// Settings are the repo-level settings.
	Settings configs.Settings
	// Pairs are the candidate suites with their baseline counterparts.
	Pairs []SuitePair
	// Lookup resolves a provider by name, failing when it is unconfigured or lacks credentials.
	Lookup assertions.ProviderLookup
}

// Preflight validates everything that can be checked before the first model call and
// returns all problems found, joined. Only providers actually used by a suite are resolved,
// so a missing credential for an unused provider is not an error.
func Preflight(in PreflightInput) error {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	checkedProviders := map[string]bool{}
	checkProvider := func(where, name string, needEmbeddings bool) {
		if _, ok := in.Settings.Providers[name]; !ok {
			add("%s: provider %q is not configured in .evalci.yaml", where, name)
			return
		}
		p, err := in.Lookup(name)
		if err != nil {
			if !checkedProviders[name] {
				add("%s: provider %q: %v", where, name, err)
			}
			checkedProviders[name] = true
			return
		}
		checkedProviders[name] = true
		if needEmbeddings && !p.SupportsEmbeddings() {
			add("%s: provider %q does not support embeddings", where, name)
		}
	}

	for _, pair := range in.Pairs {
		variants := []struct {
			label string
			suite configs.Suite
		}{{"candidate", pair.Candidate}}
		if !pair.NoBaseline {
			variants = append(variants, struct {
				label string
				suite configs.Suite
			}{"baseline", pair.Baseline})
		}
		for _, v := range variants {
			if _, err := LoadTemplate(v.suite.Prompt); err != nil {
				add("%s (%s): %v", v.suite.File, v.label, err)
			}
			checkProvider(v.suite.File+" "+v.label+" model.provider", v.suite.Model.Provider, false)
		}
		for i, c := range pair.Candidate.Cases {
			for j, a := range c.Assertions {
				where := fmt.Sprintf("%s: cases[%d].assertions[%d]", pair.Candidate.File, i, j)
				switch {
				case a.Embedding != nil:
					checkProvider(where+".embedding.provider", a.Embedding.Provider, true)
				case a.Judge != nil:
					checkProvider(where+".judge.provider", a.Judge.Provider, false)
				}
			}
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	errs := make([]error, len(problems))
	for i, p := range problems {
		errs[i] = errors.New(p)
	}
	return errors.Join(errs...)
}

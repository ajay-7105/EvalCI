package reporters

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/ajay-7105/EvalCI/internal/diffscope"
)

// ScopeSummary is what the scope block prints.
type ScopeSummary struct {
	// Full is true when --full was given.
	Full      bool
	BaseRef   string
	BaseSHA   string
	HeadRef   string
	HeadSHA   string
	HeadName  string // branch name when known, otherwise HeadRef is shown
	Decisions []diffscope.ScopeDecision
}

// WriteScope prints the scope block: a header line and one row per suite showing whether it
// RUN, was skipped (SKIP) or REMOVED, with the reason.
func WriteScope(w io.Writer, s ScopeSummary) error {
	selected, total := 0, 0
	for _, d := range s.Decisions {
		if d.Action != diffscope.ActionRemoved {
			total++
		}
		if d.Action == diffscope.ActionRun {
			selected++
		}
	}
	head := s.HeadName
	if head == "" {
		head = s.HeadRef
	}
	mode := ""
	if s.Full {
		mode = diffscope.ReasonFull + "; "
	}
	if _, err := fmt.Fprintf(w, "Scope: base %s (%s) -> head %s (%s); %s%d of %d suites selected\n",
		s.BaseRef, short(s.BaseSHA), head, short(s.HeadSHA), mode, selected, total); err != nil {
		return fmt.Errorf("writing scope: %w", err)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, d := range s.Decisions {
		reason := d.Reason
		if (d.Reason == diffscope.ReasonPromptChange || d.Reason == diffscope.ReasonSuiteChanged) && len(d.Files) > 0 {
			reason += ": " + strings.Join(d.Files, ", ")
		}
		if _, err := fmt.Fprintf(tw, "  %s\t%s\t%s\n", d.Suite, strings.ToUpper(d.Action), reason); err != nil {
			return fmt.Errorf("writing scope: %w", err)
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("writing scope: %w", err)
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return fmt.Errorf("writing scope: %w", err)
	}
	return nil
}

// WriteNothingAffected prints the decision line for a scoped run that selected no suites.
func WriteNothingAffected(w io.Writer) error {
	if _, err := fmt.Fprintln(w, "Decision: PASS (no suites affected)"); err != nil {
		return fmt.Errorf("writing decision: %w", err)
	}
	return nil
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

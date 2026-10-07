package reporters

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/ajay-7105/EvalCI/internal/gating"
)

// WriteTable prints the per-case table and the final decision line to w.
func WriteTable(w io.Writer, results []gating.CaseResult, decision gating.Decision) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "SUITE\tCASE\tBASELINE\tCANDIDATE\tDROP\tSTATUS"); err != nil {
		return fmt.Errorf("writing table: %w", err)
	}
	for _, r := range results {
		status := string(r.Status)
		if !r.Critical {
			status += " (non-critical)"
		}
		drop := "-"
		if r.Baseline.Successful() > 0 && r.Candidate.Successful() > 0 {
			drop = fmt.Sprintf("%.1f", r.Drop)
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Suite, r.Case, sideCell(r.Baseline), sideCell(r.Candidate), drop, status); err != nil {
			return fmt.Errorf("writing table: %w", err)
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("writing table: %w", err)
	}
	if _, err := fmt.Fprintf(w, "\nDecision: %s%s\n", decision, summary(results)); err != nil {
		return fmt.Errorf("writing decision: %w", err)
	}
	return nil
}

func sideCell(s gating.Side) string {
	cell := "-"
	if s.Successful() > 0 {
		cell = fmt.Sprintf("%.1f", s.Average)
	}
	if s.FailedRuns > 0 {
		cell += fmt.Sprintf(" !%d", s.FailedRuns)
	}
	return cell
}

func summary(results []gating.CaseResult) string {
	var regressed, unreliable, errored int
	for _, r := range results {
		switch r.Status {
		case gating.StatusRegressed:
			regressed++
		case gating.StatusUnreliable:
			unreliable++
		case gating.StatusError:
			errored++
		}
	}
	var parts []string
	if regressed > 0 {
		parts = append(parts, fmt.Sprintf("%d regressed", regressed))
	}
	if unreliable > 0 {
		parts = append(parts, fmt.Sprintf("%d unreliable", unreliable))
	}
	if errored > 0 {
		parts = append(parts, fmt.Sprintf("%d error", errored))
	}
	if len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d cases", len(results)))
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

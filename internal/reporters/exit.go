// Package reporters renders gate results: a stdout table, a JSON file, an exit code, and
// stderr diagnostics.
package reporters

import "github.com/ajay-7105/EvalCI/internal/gating"

// Process exit codes.
const (
	ExitOK    = 0 // PASS or WARN
	ExitFail  = 1 // a critical case REGRESSED
	ExitError = 2 // configuration, infrastructure, or an unevaluable case
)

// ExitCode maps a suite decision to the process exit code.
func ExitCode(d gating.Decision) int {
	switch d {
	case gating.DecisionFail:
		return ExitFail
	case gating.DecisionError:
		return ExitError
	}
	return ExitOK
}

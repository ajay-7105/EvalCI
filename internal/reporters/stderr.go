package reporters

import (
	"fmt"
	"io"
	"sync"

	"github.com/ajay-7105/EvalCI/internal/engine"
)

// Stderr is an engine.Observer that writes diagnostics. Failure reasons are always
// written; per-call lines only when verbose. It is safe for concurrent use.
type Stderr struct {
	mu      sync.Mutex
	w       io.Writer
	verbose bool
}

// NewStderr returns an observer writing to w.
func NewStderr(w io.Writer, verbose bool) *Stderr {
	return &Stderr{w: w, verbose: verbose}
}

// CallFinished implements engine.Observer.
func (s *Stderr) CallFinished(c engine.CallInfo) {
	if !s.verbose {
		return
	}
	outcome := "ok"
	if c.Err != nil {
		outcome = "error: " + c.Err.Error()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(s.w, "call suite=%s case=%s variant=%s run=%d kind=%s provider=%s model=%s duration=%dms %s\n",
		c.Suite, c.Case, c.Variant, c.Run, c.Kind, c.Provider, c.Model, c.Duration.Milliseconds(), outcome)
}

// RunFailed implements engine.Observer.
func (s *Stderr) RunFailed(f engine.RunFailure) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(s.w, "run failed: suite=%s case=%s variant=%s run=%d: %v\n", f.Suite, f.Case, f.Variant, f.Run, f.Err)
}

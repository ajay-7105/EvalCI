// Command evalci gates merges on LLM prompt/RAG eval regressions.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

// exitError carries a specific process exit code through cobra.
type exitError struct{ code int }

// Error implements error.
func (e *exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "evalci",
		Short:         "Gate merges on LLM eval regressions",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newRunCmd())
	return root
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := newRootCmd().ExecuteContext(ctx)
	stop()
	if err == nil {
		return
	}
	var ee *exitError
	if errors.As(err, &ee) {
		os.Exit(ee.code)
	}
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(2)
}

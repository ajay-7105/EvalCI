package reporters

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ajay-7105/EvalCI/internal/cost"
)

// WriteCost prints the cost line. It prints nothing when no model was called.
func WriteCost(w io.Writer, s cost.Summary, currency string) error {
	if len(s.Models) == 0 {
		return nil
	}
	est := "unknown"
	if s.TotalCostMicros != nil {
		est = FormatMicros(*s.TotalCostMicros) + " " + currency
	}
	unknown := 0
	for _, m := range s.Models {
		if m.CostMicros == nil {
			unknown++
		}
	}
	note := ""
	if unknown > 0 {
		noun := "models"
		if unknown == 1 {
			noun = "model"
		}
		note = fmt.Sprintf(" (cost unknown for %d %s)", unknown, noun)
	}
	if _, err := fmt.Fprintf(w, "Cost: %s input / %s output tokens, est. %s%s\n",
		commas(s.TotalInput), commas(s.TotalOutput), est, note); err != nil {
		return fmt.Errorf("writing cost: %w", err)
	}
	return nil
}

// FormatMicros renders millionths of a currency unit with at least two decimals and no more
// than six, for example 12300 -> "0.0123" and 750000 -> "0.75".
func FormatMicros(m int64) string {
	sign := ""
	if m < 0 {
		sign, m = "-", -m
	}
	frac := strings.TrimRight(fmt.Sprintf("%06d", m%1_000_000), "0")
	for len(frac) < 2 {
		frac += "0"
	}
	return fmt.Sprintf("%s%d.%s", sign, m/1_000_000, frac)
}

func commas(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

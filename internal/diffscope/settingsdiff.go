package diffscope

import (
	"reflect"

	"github.com/ajay-7105/EvalCI/configs"
)

// RunAffectingChanged reports whether the settings differ in a way that can change what a
// suite run produces or how it is gated: runs per case, the two thresholds, and any provider's
// connection details (base URL, key variable, concurrency, request rate). Differences only in
// prices, currency, or storage do not count. Both inputs have defaults applied, so an explicit
// default and an omitted key compare equal. A nil side means the settings file does not exist
// there: one nil side counts as changed, two nil sides as unchanged.
func RunAffectingChanged(base, head *configs.Settings) bool {
	if base == nil || head == nil {
		return base != head
	}
	if base.RunsPerCase != head.RunsPerCase ||
		base.MinDropThreshold != head.MinDropThreshold ||
		base.MaxSpreadBeforeUnreliable != head.MaxSpreadBeforeUnreliable {
		return true
	}
	// ProviderSettings holds exactly the connection fields listed above.
	return !reflect.DeepEqual(base.Providers, head.Providers)
}

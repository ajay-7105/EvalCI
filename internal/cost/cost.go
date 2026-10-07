package cost

import (
	"math"
	"sort"
)

// Usage is the token usage of one provider and model across a run. Callers convert from their
// own types; this package imports neither providers nor the engine.
type Usage struct {
	Provider     string
	Model        string
	Calls        int64
	InputTokens  int64
	OutputTokens int64
	// UnknownCalls counts calls for which the provider reported no usage.
	UnknownCalls int64
}

// Price is a model's price per million tokens in the user's currency units.
type Price struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

// ModelCost is one model's usage and its estimated cost.
type ModelCost struct {
	Usage
	// CostMicros is the cost in millionths of a currency unit; nil means unknown.
	CostMicros *int64
}

// Summary is the cost of a whole run.
type Summary struct {
	Models      []ModelCost
	TotalInput  int64
	TotalOutput int64
	// TotalCostMicros is the sum of known model costs: nil when none is known, and a known
	// zero when no model was called at all.
	TotalCostMicros *int64
	// Partial is true when some model's cost is unknown.
	Partial bool
}

const microsPerUnit = 1_000_000

// Key is how prices are keyed in the settings: "<provider>/<model>".
func Key(provider, model string) string { return provider + "/" + model }

// Compute estimates cost with integer arithmetic. A model's cost is known only when it has a
// price and every one of its calls reported usage; otherwise it is nil (never zero), though
// its tokens are still totalled. Cost per model is
// round(input*inputPrice + output*outputPrice) with prices in micro-units per million tokens.
func Compute(usage []Usage, prices map[string]Price) Summary {
	models := append([]Usage(nil), usage...)
	sort.Slice(models, func(i, j int) bool {
		if models[i].Provider != models[j].Provider {
			return models[i].Provider < models[j].Provider
		}
		return models[i].Model < models[j].Model
	})

	s := Summary{Models: make([]ModelCost, 0, len(models))}
	var total int64
	anyKnown := false
	for _, u := range models {
		mc := ModelCost{Usage: u}
		s.TotalInput += u.InputTokens
		s.TotalOutput += u.OutputTokens
		if p, ok := prices[Key(u.Provider, u.Model)]; ok && u.UnknownCalls == 0 {
			c := micros(u.InputTokens, p.InputPerMillion) + micros(u.OutputTokens, p.OutputPerMillion)
			c = (c + microsPerUnit/2) / microsPerUnit
			mc.CostMicros = &c
			total += c
			anyKnown = true
		} else {
			s.Partial = true
		}
		s.Models = append(s.Models, mc)
	}
	switch {
	case len(models) == 0:
		zero := int64(0)
		s.TotalCostMicros = &zero
	case anyKnown:
		s.TotalCostMicros = &total
	}
	return s
}

// micros returns tokens * (price in micro-units per million tokens), before the final division
// by one million, so rounding happens once per model.
func micros(tokens int64, pricePerMillion float64) int64 {
	return tokens * int64(math.Round(pricePerMillion*microsPerUnit))
}

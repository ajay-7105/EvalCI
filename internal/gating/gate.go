package gating

// Classify gates one case. Rules are evaluated in order:
//  1. either side has no successful run => ERROR (nothing to compare)
//  2. either side's spread exceeds MaxSpread, or fewer than half of its planned runs
//     succeeded => UNRELIABLE
//  3. baseline average minus candidate average strictly exceeds MinDropThreshold => REGRESSED
//  4. otherwise PASS
func Classify(baseline, candidate Side, s Settings) Status {
	if baseline.Successful() == 0 || candidate.Successful() == 0 {
		return StatusError
	}
	if unreliable(baseline, s) || unreliable(candidate, s) {
		return StatusUnreliable
	}
	if baseline.Average-candidate.Average > s.MinDropThreshold {
		return StatusRegressed
	}
	return StatusPass
}

func unreliable(side Side, s Settings) bool {
	return side.Spread > s.MaxSpread || side.Successful()*2 < side.Runs
}

// Evaluate builds the CaseResult for one case from its two sides.
func Evaluate(suite, caseID string, critical bool, baseline, candidate Side, s Settings) CaseResult {
	r := CaseResult{
		Suite:     suite,
		Case:      caseID,
		Critical:  critical,
		Baseline:  baseline,
		Candidate: candidate,
		Status:    Classify(baseline, candidate, s),
	}
	if baseline.Successful() > 0 && candidate.Successful() > 0 {
		r.Drop = baseline.Average - candidate.Average
	}
	return r
}

// EvaluateNew builds the CaseResult for a case whose suite has no baseline (it exists only on
// the candidate side). There is nothing to regress from, so the status is NEW unless every
// candidate run failed, which is ERROR as for any unevaluable case.
func EvaluateNew(suite, caseID string, critical bool, candidate Side) CaseResult {
	r := CaseResult{
		Suite: suite, Case: caseID, Critical: critical,
		Baseline: NewSide(0, 0, nil), Candidate: candidate,
		Status: StatusNew,
	}
	if candidate.Successful() == 0 {
		r.Status = StatusError
	}
	return r
}

// Decide computes the suite decision. A critical REGRESSED case gives FAIL even when
// another case is ERROR, so a confirmed regression is never masked by an outage; otherwise
// any ERROR gives ERROR, any UNRELIABLE gives WARN, and anything else PASS. A REGRESSED
// case that is not critical never causes FAIL, and NEW cases never affect the decision.
func Decide(results []CaseResult) Decision {
	var hasError, hasUnreliable bool
	for _, r := range results {
		switch r.Status {
		case StatusRegressed:
			if r.Critical {
				return DecisionFail
			}
		case StatusError:
			hasError = true
		case StatusUnreliable:
			hasUnreliable = true
		}
	}
	switch {
	case hasError:
		return DecisionError
	case hasUnreliable:
		return DecisionWarn
	}
	return DecisionPass
}

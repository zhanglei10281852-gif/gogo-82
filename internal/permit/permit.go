// Package permit decides whether a high-risk work permit may be honoured.
//
// A permit is refused for one of a small number of stated reasons: it is outside
// its validity window, it runs longer than the policy allows, one of the points
// it depends on is unusable, or one of those points is above its alarm level. The
// reason is always recorded, because a refused permit needs an explanation a
// supervisor can act on.
package permit

import (
	"fmt"
	"sort"

	"MineGuard/internal/config"
	"MineGuard/internal/gas"
	"MineGuard/internal/model"
	"MineGuard/internal/reading"
	"MineGuard/internal/timeutil"
	"MineGuard/internal/units"
)

// Verdict is the outcome for one permit.
type Verdict string

// The permit verdicts.
const (
	VerdictCleared Verdict = "cleared"
	VerdictRefused Verdict = "refused"
	VerdictExpired Verdict = "expired"
	VerdictPending Verdict = "pending"
)

// Reason names a refusal cause.
type Reason string

// The refusal reasons.
const (
	ReasonOutsideWindow Reason = "outside_validity_window"
	ReasonTooLong       Reason = "duration_exceeds_policy"
	ReasonPointUnusable Reason = "required_point_unusable"
	ReasonPointAlarm    Reason = "required_point_in_alarm"
	ReasonPointMissing  Reason = "required_point_missing"
	ReasonOccupancy     Reason = "area_occupied_beyond_limit"
)

// Check is one condition evaluated for a permit.
type Check struct {
	PointID string    `json:"point_id"`
	Usable  bool      `json:"usable"`
	Level   gas.Level `json:"level"`
	Value   float64   `json:"value"`
	Unit    string    `json:"unit"`
	Passed  bool      `json:"passed"`
	Detail  string    `json:"detail"`
}

// Assessment is the verdict for one permit.
type Assessment struct {
	PermitID      string          `json:"permit_id"`
	Kind          string          `json:"kind"`
	AreaKind      model.AreaKind  `json:"area_kind"`
	AreaID        string          `json:"area_id"`
	Window        timeutil.Window `json:"window"`
	Verdict       Verdict         `json:"verdict"`
	Reasons       []Reason        `json:"reasons,omitempty"`
	Checks        []Check         `json:"checks"`
	DurationHours float64         `json:"duration_hours"`
	Explanation   string          `json:"explanation"`
}

// Result is the outcome of the permit stage.
type Result struct {
	AsOf         timeutil.Stamp `json:"as_of"`
	Assessments  []Assessment   `json:"assessments"`
	ClearedCount int            `json:"cleared_count"`
	RefusedCount int            `json:"refused_count"`
}

// Assess evaluates every permit as of asOf.
func Assess(cfg config.Config, permits []model.Permit, states []reading.PointState,
	gasResult gas.Result, asOf timeutil.Stamp) Result {
	stateIndex := reading.StateByID(states)
	gasIndex := gas.Index(gasResult.Assessments)
	result := Result{AsOf: asOf}
	for _, item := range permits {
		assessment := assess(cfg, item, stateIndex, gasIndex, asOf)
		result.Assessments = append(result.Assessments, assessment)
		switch assessment.Verdict {
		case VerdictCleared:
			result.ClearedCount++
		case VerdictRefused, VerdictExpired:
			result.RefusedCount++
		}
	}
	sort.SliceStable(result.Assessments, func(a, b int) bool {
		return result.Assessments[a].PermitID < result.Assessments[b].PermitID
	})
	return result
}

// assess evaluates one permit.
func assess(cfg config.Config, item model.Permit, states map[string]reading.PointState,
	assessments map[string]gas.Assessment, asOf timeutil.Stamp) Assessment {
	out := Assessment{
		PermitID: item.PermitID,
		Kind:     item.Kind,
		AreaKind: item.AreaKind,
		AreaID:   item.AreaID,
		Window:   item.Window,
		Verdict:  VerdictCleared,
	}
	out.DurationHours = units.Round(item.Window.Minutes()/60, 2)
	if out.DurationHours > cfg.Permits.MaxDurationHours {
		out.Reasons = append(out.Reasons, ReasonTooLong)
	}
	switch {
	case !item.Window.From.IsSet() || !item.Window.To.IsSet():
		out.Reasons = append(out.Reasons, ReasonOutsideWindow)
	case asOf.IsSet() && asOf.Before(item.Window.From):
		out.Verdict = VerdictPending
	case asOf.IsSet() && !item.Window.Contains(asOf):
		out.Verdict = VerdictExpired
		out.Reasons = append(out.Reasons, ReasonOutsideWindow)
	}
	required := append([]string(nil), item.RequiredPoints...)
	sort.Strings(required)
	for _, pointID := range required {
		check := Check{PointID: pointID}
		state, hasState := states[pointID]
		if !hasState || !state.HasLatest {
			check.Detail = "required point produced no reading"
			out.Checks = append(out.Checks, check)
			out.Reasons = append(out.Reasons, ReasonPointMissing)
			continue
		}
		check.Usable = state.Usable
		check.Value = state.Latest.Value
		check.Unit = state.Latest.Unit
		if assessment, ok := assessments[pointID]; ok {
			check.Level = assessment.Level
		}
		if cfg.Permits.RequireFreshPoints && !state.Usable {
			check.Detail = state.Explanation
			out.Checks = append(out.Checks, check)
			out.Reasons = append(out.Reasons, ReasonPointUnusable)
			continue
		}
		if check.Level == gas.LevelTrip || check.Level == gas.LevelWarn {
			check.Detail = fmt.Sprintf("%s stands at %s %s which is %s",
				pointID, units.Format(check.Value, 3), check.Unit, check.Level)
			out.Checks = append(out.Checks, check)
			out.Reasons = append(out.Reasons, ReasonPointAlarm)
			continue
		}
		check.Passed = true
		check.Detail = fmt.Sprintf("%s stands at %s %s which is normal",
			pointID, units.Format(check.Value, 3), check.Unit)
		out.Checks = append(out.Checks, check)
	}
	out.finalise()
	return out
}

// finalise deduplicates the reasons and settles the verdict.
func (a *Assessment) finalise() {
	sort.SliceStable(a.Reasons, func(i, j int) bool { return a.Reasons[i] < a.Reasons[j] })
	deduped := make([]Reason, 0, len(a.Reasons))
	for index, reason := range a.Reasons {
		if index > 0 && a.Reasons[index-1] == reason {
			continue
		}
		deduped = append(deduped, reason)
	}
	a.Reasons = deduped
	if len(a.Reasons) > 0 && a.Verdict != VerdictExpired {
		a.Verdict = VerdictRefused
	}
	sort.SliceStable(a.Checks, func(i, j int) bool { return a.Checks[i].PointID < a.Checks[j].PointID })
	if len(a.Reasons) == 0 {
		a.Explanation = fmt.Sprintf("%s is %s with %d condition(s) met",
			a.PermitID, a.Verdict, len(a.Checks))
		return
	}
	names := make([]string, 0, len(a.Reasons))
	for _, reason := range a.Reasons {
		names = append(names, string(reason))
	}
	a.Explanation = fmt.Sprintf("%s is %s because %s", a.PermitID, a.Verdict, join(names))
}

// join renders reason names as a stable list.
func join(names []string) string {
	if len(names) == 0 {
		return "no reason"
	}
	out := names[0]
	for _, name := range names[1:] {
		out += ", " + name
	}
	return out
}

// Index maps permit identifiers to assessments.
func Index(assessments []Assessment) map[string]Assessment {
	out := make(map[string]Assessment, len(assessments))
	for _, assessment := range assessments {
		out[assessment.PermitID] = assessment
	}
	return out
}

// Refused returns the identifiers of every permit that is not cleared.
func Refused(assessments []Assessment) []string {
	out := make([]string, 0, len(assessments))
	for _, assessment := range assessments {
		if assessment.Verdict == VerdictRefused || assessment.Verdict == VerdictExpired {
			out = append(out, assessment.PermitID)
		}
	}
	sort.Strings(out)
	return out
}

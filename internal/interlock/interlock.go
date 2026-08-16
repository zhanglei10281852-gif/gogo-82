// Package interlock decides whether each controlled circuit may stay energised.
//
// The rule that matters most is negative: a circuit is never cleared while any
// mandatory input is missing, refused, stale or out of calibration. An interlock
// that treats "no data" as "no gas" is worse than no interlock at all, so the
// absence of evidence always produces a trip, and the decision records which
// rule fired and which points it read.
package interlock

import (
	"fmt"
	"sort"

	"MineGuard/internal/config"
	"MineGuard/internal/gas"
	"MineGuard/internal/model"
	"MineGuard/internal/reading"
	"MineGuard/internal/timeutil"
	"MineGuard/internal/units"
	"MineGuard/internal/ventilation"
)

// Action is the decision taken for a circuit.
type Action string

// The three actions.
const (
	ActionAllow Action = "allow"
	ActionWarn  Action = "warn"
	ActionTrip  Action = "trip"
)

// Rank orders actions so the most restrictive can be taken.
func Rank(action Action) int {
	switch action {
	case ActionTrip:
		return 2
	case ActionWarn:
		return 1
	default:
		return 0
	}
}

// Worst returns the more restrictive of two actions.
func Worst(a, b Action) Action {
	if Rank(b) > Rank(a) {
		return b
	}
	return a
}

// Rule names the rule that produced a decision.
type Rule string

// The interlock rules.
const (
	RuleGasTrip       Rule = "gas_trip_level"
	RuleGasWarn       Rule = "gas_warn_level"
	RuleInvalidInput  Rule = "mandatory_input_invalid"
	RuleMissingInput  Rule = "mandatory_input_missing"
	RuleVentilation   Rule = "ventilation_inadequate"
	RuleRecirculation Rule = "recirculation_detected"
	RuleRateOfRise    Rule = "gas_rate_of_rise"
	RuleClear         Rule = "all_conditions_met"
)

// Evidence is one input a decision relied on.
type Evidence struct {
	PointID string     `json:"point_id"`
	Usable  bool       `json:"usable"`
	Level   gas.Level  `json:"level"`
	Value   float64    `json:"value"`
	Unit    string     `json:"unit"`
	Flags   []gas.Flag `json:"flags,omitempty"`
	Detail  string     `json:"detail"`
}

// Decision is the verdict for one circuit.
type Decision struct {
	CircuitID   string         `json:"circuit_id"`
	Label       string         `json:"label"`
	AreaKind    model.AreaKind `json:"area_kind"`
	AreaID      string         `json:"area_id"`
	Action      Action         `json:"action"`
	Rules       []Rule         `json:"rules"`
	Evidence    []Evidence     `json:"evidence"`
	At          timeutil.Stamp `json:"at"`
	Fingerprint string         `json:"policy_fingerprint"`
	Explanation string         `json:"explanation"`
}

// Result is the outcome of the interlock stage.
type Result struct {
	AsOf       timeutil.Stamp `json:"as_of"`
	Decisions  []Decision     `json:"decisions"`
	TripCount  int            `json:"trip_count"`
	WarnCount  int            `json:"warn_count"`
	AllowCount int            `json:"allow_count"`
	Worst      Action         `json:"worst_action"`
}

// Decide evaluates every circuit in the layout.
func Decide(cfg config.Config, layout model.Layout, states []reading.PointState,
	gasResult gas.Result, vent ventilation.Result, asOf timeutil.Stamp) Result {
	stateIndex := reading.StateByID(states)
	gasIndex := gas.Index(gasResult.Assessments)
	faceIndex := ventilation.FaceIndex(vent.Faces)
	result := Result{AsOf: asOf, Worst: ActionAllow}
	for _, circuit := range layout.Circuits {
		decision := decide(cfg, layout, circuit, stateIndex, gasIndex, faceIndex, vent, asOf)
		result.Decisions = append(result.Decisions, decision)
		result.Worst = Worst(result.Worst, decision.Action)
		switch decision.Action {
		case ActionTrip:
			result.TripCount++
		case ActionWarn:
			result.WarnCount++
		default:
			result.AllowCount++
		}
	}
	sort.SliceStable(result.Decisions, func(a, b int) bool {
		return result.Decisions[a].CircuitID < result.Decisions[b].CircuitID
	})
	return result
}

// decide evaluates one circuit.
func decide(cfg config.Config, layout model.Layout, circuit model.Circuit,
	states map[string]reading.PointState, assessments map[string]gas.Assessment,
	faces map[string]ventilation.FaceAdequacy, vent ventilation.Result,
	asOf timeutil.Stamp) Decision {
	decision := Decision{
		CircuitID:   circuit.CircuitID,
		Label:       circuit.Label,
		AreaKind:    circuit.AreaKind,
		AreaID:      circuit.AreaID,
		Action:      ActionAllow,
		At:          asOf,
		Fingerprint: cfg.Fingerprint(),
	}
	points := layout.PointByID()
	guards := append([]string(nil), circuit.GuardPoints...)
	sort.Strings(guards)
	for _, guard := range guards {
		point, known := points[guard]
		state, hasState := states[guard]
		assessment, hasAssessment := assessments[guard]
		evidence := Evidence{PointID: guard}
		if !known {
			decision.Action = Worst(decision.Action, ActionTrip)
			decision.Rules = append(decision.Rules, RuleMissingInput)
			evidence.Detail = "guard point does not exist in the layout"
			decision.Evidence = append(decision.Evidence, evidence)
			continue
		}
		if !hasState || !state.HasLatest {
			decision.Action = Worst(decision.Action, ActionTrip)
			decision.Rules = append(decision.Rules, RuleMissingInput)
			evidence.Detail = "guard point produced no reading"
			decision.Evidence = append(decision.Evidence, evidence)
			continue
		}
		evidence.Usable = state.Usable
		evidence.Value = state.Latest.Value
		evidence.Unit = state.Latest.Unit
		if hasAssessment {
			evidence.Level = assessment.Level
			evidence.Flags = append([]gas.Flag(nil), assessment.Flags...)
		}
		switch {
		case !state.Usable:
			// An unusable mandatory input always trips. A non-mandatory input that is
			// unusable can only raise a warning, which is what lets a mine keep
			// running while a secondary sensor is being serviced.
			if point.Mandatory || cfg.Interlock.RequireFresh {
				decision.Action = Worst(decision.Action, ActionTrip)
				decision.Rules = append(decision.Rules, RuleInvalidInput)
			} else {
				decision.Action = Worst(decision.Action, ActionWarn)
				decision.Rules = append(decision.Rules, RuleInvalidInput)
			}
			evidence.Detail = state.Explanation
		case evidence.Level == gas.LevelTrip:
			decision.Action = Worst(decision.Action, ActionTrip)
			decision.Rules = append(decision.Rules, RuleGasTrip)
			evidence.Detail = assessment.Explanation
		case evidence.Level == gas.LevelWarn:
			if cfg.Interlock.AllowWarnOperation {
				decision.Action = Worst(decision.Action, ActionWarn)
			} else {
				decision.Action = Worst(decision.Action, ActionTrip)
			}
			decision.Rules = append(decision.Rules, RuleGasWarn)
			evidence.Detail = assessment.Explanation
		default:
			evidence.Detail = state.Explanation
		}
		if hasAssessment && containsFlag(assessment.Flags, gas.FlagRateOfRise) {
			decision.Action = Worst(decision.Action, ActionWarn)
			decision.Rules = append(decision.Rules, RuleRateOfRise)
		}
		decision.Evidence = append(decision.Evidence, evidence)
	}
	if circuit.RequiresVentilation {
		if circuit.AreaKind == model.AreaWorkingFace {
			face, ok := faces[circuit.AreaID]
			if !ok || !face.Adequate {
				decision.Action = Worst(decision.Action, ActionTrip)
				decision.Rules = append(decision.Rules, RuleVentilation)
			}
		}
		if len(vent.Recirculations) > 0 {
			decision.Action = Worst(decision.Action, ActionTrip)
			decision.Rules = append(decision.Rules, RuleRecirculation)
		}
	}
	decision.finalise()
	return decision
}

// containsFlag reports whether a flag is present.
func containsFlag(flags []gas.Flag, wanted gas.Flag) bool {
	for _, flag := range flags {
		if flag == wanted {
			return true
		}
	}
	return false
}

// finalise deduplicates the rules and writes the explanation.
func (d *Decision) finalise() {
	sort.SliceStable(d.Rules, func(a, b int) bool { return d.Rules[a] < d.Rules[b] })
	deduped := make([]Rule, 0, len(d.Rules))
	for index, rule := range d.Rules {
		if index > 0 && d.Rules[index-1] == rule {
			continue
		}
		deduped = append(deduped, rule)
	}
	d.Rules = deduped
	if len(d.Rules) == 0 {
		d.Rules = []Rule{RuleClear}
	}
	sort.SliceStable(d.Evidence, func(a, b int) bool { return d.Evidence[a].PointID < d.Evidence[b].PointID })
	names := make([]string, 0, len(d.Rules))
	for _, rule := range d.Rules {
		names = append(names, string(rule))
	}
	d.Explanation = fmt.Sprintf("%s decided %s from %d input(s) by %s",
		d.CircuitID, d.Action, len(d.Evidence), join(names))
}

// join renders rule names as a stable list.
func join(names []string) string {
	if len(names) == 0 {
		return "no rule"
	}
	out := names[0]
	for _, name := range names[1:] {
		out += ", " + name
	}
	return out
}

// Index maps circuit identifiers to decisions.
func Index(decisions []Decision) map[string]Decision {
	out := make(map[string]Decision, len(decisions))
	for _, decision := range decisions {
		out[decision.CircuitID] = decision
	}
	return out
}

// Tripped returns the circuits that must be de-energised.
func Tripped(decisions []Decision) []string {
	out := make([]string, 0, len(decisions))
	for _, decision := range decisions {
		if decision.Action == ActionTrip {
			out = append(out, decision.CircuitID)
		}
	}
	sort.Strings(out)
	return out
}

// Describe renders one decision as a single line.
func (d Decision) Describe() string {
	return fmt.Sprintf("%-14s %-6s %-14s %s", d.CircuitID, d.Action, d.AreaID,
		units.Format(float64(len(d.Evidence)), 0)+" input(s)")
}

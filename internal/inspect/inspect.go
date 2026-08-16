// Package inspect tracks scheduled inspections and builds the event timeline.
//
// An inspection is due at its last completion plus its interval. Reporting the
// overdue hours rather than a plain boolean matters because a support inspection
// two hours late and one two weeks late call for different responses.
package inspect

import (
	"fmt"
	"sort"

	"MineGuard/internal/config"
	"MineGuard/internal/gas"
	"MineGuard/internal/interlock"
	"MineGuard/internal/model"
	"MineGuard/internal/timeutil"
	"MineGuard/internal/units"
)

// Status is the state of one scheduled inspection.
type Status struct {
	InspectionID   string         `json:"inspection_id"`
	Subject        string         `json:"subject"`
	AreaKind       model.AreaKind `json:"area_kind"`
	AreaID         string         `json:"area_id"`
	LastDoneAt     timeutil.Stamp `json:"last_done_at"`
	DueAt          timeutil.Stamp `json:"due_at"`
	Overdue        bool           `json:"overdue"`
	OverdueHours   float64        `json:"overdue_hours"`
	RemainingHours float64        `json:"remaining_hours"`
	Explanation    string         `json:"explanation"`
}

// Event is one entry in the assessment timeline.
type Event struct {
	At      timeutil.Stamp `json:"at"`
	Kind    string         `json:"kind"`
	Subject string         `json:"subject"`
	Detail  string         `json:"detail"`
}

// Result is the outcome of the inspection stage.
type Result struct {
	AsOf         timeutil.Stamp `json:"as_of"`
	Statuses     []Status       `json:"statuses"`
	OverdueCount int            `json:"overdue_count"`
	Timeline     []Event        `json:"timeline"`
}

// Assess evaluates every scheduled inspection as of asOf.
func Assess(cfg config.Config, layout model.Layout, asOf timeutil.Stamp) Result {
	result := Result{AsOf: asOf}
	for _, inspection := range layout.Inspections {
		status := Status{
			InspectionID: inspection.InspectionID,
			Subject:      inspection.Subject,
			AreaKind:     inspection.AreaKind,
			AreaID:       inspection.AreaID,
			LastDoneAt:   inspection.LastDoneAt,
		}
		if inspection.LastDoneAt.IsSet() && inspection.IntervalHours > 0 {
			status.DueAt = inspection.LastDoneAt.AddHours(inspection.IntervalHours)
		}
		if asOf.IsSet() && status.DueAt.IsSet() {
			delta := asOf.HoursSince(status.DueAt)
			if delta > 0 {
				status.Overdue = true
				status.OverdueHours = units.Round(delta, 2)
			} else {
				status.RemainingHours = units.Round(-delta, 2)
			}
		}
		if status.Overdue {
			result.OverdueCount++
			status.Explanation = fmt.Sprintf("%s on %s was due at %s and is %s hour(s) overdue",
				inspection.Subject, inspection.AreaID, status.DueAt,
				units.Format(status.OverdueHours, 1))
		} else {
			status.Explanation = fmt.Sprintf("%s on %s is due at %s in %s hour(s)",
				inspection.Subject, inspection.AreaID, status.DueAt,
				units.Format(status.RemainingHours, 1))
		}
		result.Statuses = append(result.Statuses, status)
	}
	sort.SliceStable(result.Statuses, func(a, b int) bool {
		return result.Statuses[a].InspectionID < result.Statuses[b].InspectionID
	})
	return result
}

// Timeline builds a deterministic ordered timeline from the assessed stages.
func Timeline(gasResult gas.Result, interlockResult interlock.Result, statuses []Status) []Event {
	events := make([]Event, 0, 16)
	for _, assessment := range gasResult.Assessments {
		if assessment.Level == gas.LevelNormal || !assessment.At.IsSet() {
			continue
		}
		events = append(events, Event{
			At:      assessment.At,
			Kind:    "gas_" + string(assessment.Level),
			Subject: assessment.PointID,
			Detail:  assessment.Explanation,
		})
	}
	for _, decision := range interlockResult.Decisions {
		if decision.Action == interlock.ActionAllow || !decision.At.IsSet() {
			continue
		}
		events = append(events, Event{
			At:      decision.At,
			Kind:    "interlock_" + string(decision.Action),
			Subject: decision.CircuitID,
			Detail:  decision.Explanation,
		})
	}
	for _, status := range statuses {
		if !status.Overdue || !status.DueAt.IsSet() {
			continue
		}
		events = append(events, Event{
			At:      status.DueAt,
			Kind:    "inspection_overdue",
			Subject: status.InspectionID,
			Detail:  status.Explanation,
		})
	}
	sort.SliceStable(events, func(a, b int) bool {
		left, right := events[a], events[b]
		if !left.At.Equal(right.At) {
			return left.At.Before(right.At)
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		return left.Subject < right.Subject
	})
	return events
}

// Overdue returns the identifiers of the overdue inspections.
func Overdue(statuses []Status) []string {
	out := make([]string, 0, len(statuses))
	for _, status := range statuses {
		if status.Overdue {
			out = append(out, status.InspectionID)
		}
	}
	sort.Strings(out)
	return out
}

// Index maps inspection identifiers to statuses.
func Index(statuses []Status) map[string]Status {
	out := make(map[string]Status, len(statuses))
	for _, status := range statuses {
		out[status.InspectionID] = status
	}
	return out
}

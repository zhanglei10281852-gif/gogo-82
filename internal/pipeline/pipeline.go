// Package pipeline wires the assessment stages together.
//
// The order is fixed and each stage only reads what the previous ones produced:
// readings are normalised, gas is assessed from the valid samples, ventilation is
// assessed from the airflow points, the interlock decides from gas plus
// ventilation, permits and personnel are assessed alongside, and inspections
// close the timeline. Nothing reads the wall clock; the reporting instant is
// supplied or derived from the newest input.
package pipeline

import (
	"fmt"
	"sort"

	"MineGuard/internal/config"
	"MineGuard/internal/gas"
	"MineGuard/internal/inspect"
	"MineGuard/internal/interlock"
	"MineGuard/internal/model"
	"MineGuard/internal/permit"
	"MineGuard/internal/personnel"
	"MineGuard/internal/reading"
	"MineGuard/internal/timeutil"
	"MineGuard/internal/ventilation"
)

// SnapshotSchema tags a stored assessment.
const SnapshotSchema = "mineguard-assessment/v1"

// Assessment is the whole computed picture.
type Assessment struct {
	Schema      string             `json:"schema"`
	MineID      string             `json:"mine_id"`
	Fingerprint string             `json:"policy_fingerprint"`
	AsOf        timeutil.Stamp     `json:"as_of"`
	Readings    reading.Result     `json:"readings"`
	Gas         gas.Result         `json:"gas"`
	Ventilation ventilation.Result `json:"ventilation"`
	Interlock   interlock.Result   `json:"interlock"`
	Personnel   personnel.Result   `json:"personnel"`
	Permits     permit.Result      `json:"permits"`
	Inspections inspect.Result     `json:"inspections"`
	Timeline    []inspect.Event    `json:"timeline"`
	Issues      model.Issues       `json:"issues,omitempty"`
	Summary     Summary            `json:"summary"`
}

// Summary is the headline of an assessment.
type Summary struct {
	Points             int    `json:"points"`
	UsablePoints       int    `json:"usable_points"`
	GasWorstLevel      string `json:"gas_worst_level"`
	GasTripCount       int    `json:"gas_trip_count"`
	VentilationOK      bool   `json:"ventilation_ok"`
	InadequateFaces    int    `json:"inadequate_faces"`
	Recirculations     int    `json:"recirculations"`
	TrippedCircuits    int    `json:"tripped_circuits"`
	Underground        int    `json:"underground"`
	MusterComplete     bool   `json:"muster_complete"`
	Unaccounted        int    `json:"unaccounted"`
	RefusedPermits     int    `json:"refused_permits"`
	OverdueInspections int    `json:"overdue_inspections"`
	Safe               bool   `json:"safe"`
	Headline           string `json:"headline"`
}

// Options controls one assessment run.
type Options struct {
	AsOf      timeutil.Stamp
	OrderedAt timeutil.Stamp
}

// Run executes every stage over one bundle.
func Run(cfg config.Config, bundle model.Bundle, options Options) Assessment {
	asOf := options.AsOf
	if !asOf.IsSet() {
		asOf = DeriveAsOf(bundle)
	}
	assessment := Assessment{
		Schema:      SnapshotSchema,
		MineID:      bundle.Layout.MineID,
		Fingerprint: cfg.Fingerprint(),
		AsOf:        asOf,
	}
	assessment.Readings = reading.Normalise(cfg, bundle.Layout, bundle.Readings, asOf)
	assessment.Gas = gas.Assess(cfg, bundle.Layout, assessment.Readings.Samples, assessment.Readings.States, asOf)
	assessment.Ventilation = ventilation.Assess(cfg, bundle.Layout, assessment.Readings.States, asOf)
	assessment.Interlock = interlock.Decide(cfg, bundle.Layout, assessment.Readings.States,
		assessment.Gas, assessment.Ventilation, asOf)
	assessment.Personnel = personnel.Assess(cfg, bundle.Layout, bundle.Roster, bundle.Events,
		options.OrderedAt, asOf)
	assessment.Permits = permit.Assess(cfg, bundle.Permits, assessment.Readings.States, assessment.Gas, asOf)
	assessment.Inspections = inspect.Assess(cfg, bundle.Layout, asOf)
	assessment.Timeline = inspect.Timeline(assessment.Gas, assessment.Interlock, assessment.Inspections.Statuses)
	assessment.Inspections.Timeline = assessment.Timeline
	assessment.Issues = collectIssues(assessment)
	assessment.Summary = summarise(assessment)
	return assessment
}

// DeriveAsOf returns the newest instant present in the inputs, which keeps a run
// reproducible without reading the system clock.
func DeriveAsOf(bundle model.Bundle) timeutil.Stamp {
	stamps := make([]timeutil.Stamp, 0, len(bundle.Readings)+len(bundle.Events))
	for _, item := range bundle.Readings {
		stamps = append(stamps, item.At)
	}
	for _, item := range bundle.Events {
		stamps = append(stamps, item.At)
	}
	for _, item := range bundle.Permits {
		stamps = append(stamps, item.Window.To)
	}
	latest, ok := timeutil.Latest(stamps)
	if !ok {
		return timeutil.Stamp{}
	}
	return latest
}

// collectIssues gathers every stage's findings in canonical order.
func collectIssues(assessment Assessment) model.Issues {
	issues := model.Issues{}
	issues = append(issues, assessment.Readings.Issues...)
	issues = append(issues, assessment.Ventilation.Issues...)
	issues = append(issues, assessment.Personnel.Issues...)
	return issues.Sorted()
}

// summarise derives the headline numbers.
func summarise(assessment Assessment) Summary {
	summary := Summary{
		Points:             len(assessment.Readings.States),
		GasWorstLevel:      string(assessment.Gas.Worst),
		GasTripCount:       assessment.Gas.TripCount,
		VentilationOK:      assessment.Ventilation.OK,
		InadequateFaces:    assessment.Ventilation.InadequateFaces,
		Recirculations:     len(assessment.Ventilation.Recirculations),
		TrippedCircuits:    assessment.Interlock.TripCount,
		Underground:        assessment.Personnel.Underground,
		MusterComplete:     assessment.Personnel.Muster.Complete,
		Unaccounted:        len(assessment.Personnel.Muster.Unaccounted),
		RefusedPermits:     assessment.Permits.RefusedCount,
		OverdueInspections: assessment.Inspections.OverdueCount,
	}
	for _, state := range assessment.Readings.States {
		if state.Usable {
			summary.UsablePoints++
		}
	}
	// Safe is the all-clear the report headline and the process exit code are
	// derived from. A muster that cannot account for every rostered person is a
	// negative verdict even when gas, ventilation, interlocks, permits and
	// inspections are clean: the headline must name the unaccounted people and
	// the exit code must be non-zero, or a scheduler would treat a missing miner
	// as routine.
	summary.Safe = summary.GasTripCount == 0 && summary.TrippedCircuits == 0 &&
		summary.VentilationOK && summary.RefusedPermits == 0 &&
		summary.OverdueInspections == 0 && summary.Unaccounted == 0
	if summary.Safe {
		summary.Headline = fmt.Sprintf("%d point(s) usable of %d, no trip, ventilation adequate",
			summary.UsablePoints, summary.Points)
		return summary
	}
	parts := make([]string, 0, 6)
	if summary.GasTripCount > 0 {
		parts = append(parts, fmt.Sprintf("%d gas trip level(s)", summary.GasTripCount))
	}
	if summary.TrippedCircuits > 0 {
		parts = append(parts, fmt.Sprintf("%d circuit(s) tripped", summary.TrippedCircuits))
	}
	if !summary.VentilationOK {
		parts = append(parts, "ventilation inadequate")
	}
	if summary.Unaccounted > 0 {
		parts = append(parts, fmt.Sprintf("%d person(s) unaccounted", summary.Unaccounted))
	}
	if summary.RefusedPermits > 0 {
		parts = append(parts, fmt.Sprintf("%d permit(s) refused", summary.RefusedPermits))
	}
	if summary.OverdueInspections > 0 {
		parts = append(parts, fmt.Sprintf("%d inspection(s) overdue", summary.OverdueInspections))
	}
	sort.Strings(parts)
	summary.Headline = join(parts)
	return summary
}

// join renders summary parts as a stable list.
func join(parts []string) string {
	if len(parts) == 0 {
		return "nothing to report"
	}
	out := parts[0]
	for _, part := range parts[1:] {
		out += "; " + part
	}
	return out
}

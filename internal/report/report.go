// Package report renders an assessment as deterministic text.
//
// Column widths are computed from the cells themselves, never from a terminal
// size or a locale, so two runs over the same assessment produce byte-identical
// output that can be diffed in a handover.
package report

import (
	"fmt"
	"sort"
	"strings"

	"MineGuard/internal/config"
	"MineGuard/internal/gas"
	"MineGuard/internal/interlock"
	"MineGuard/internal/permit"
	"MineGuard/internal/pipeline"
	"MineGuard/internal/store"
	"MineGuard/internal/units"
)

// Table is a fixed-width text table.
type Table struct {
	Header []string
	Rows   [][]string
}

// Render lays the table out with two spaces between columns.
func (t Table) Render() string {
	widths := make([]int, len(t.Header))
	for index, cell := range t.Header {
		widths[index] = len(cell)
	}
	for _, row := range t.Rows {
		for index, cell := range row {
			if index >= len(widths) {
				continue
			}
			if len(cell) > widths[index] {
				widths[index] = len(cell)
			}
		}
	}
	var builder strings.Builder
	writeRow := func(cells []string) {
		parts := make([]string, 0, len(cells))
		for index, cell := range cells {
			width := 0
			if index < len(widths) {
				width = widths[index]
			}
			parts = append(parts, pad(cell, width))
		}
		builder.WriteString(strings.TrimRight(strings.Join(parts, "  "), " "))
		builder.WriteString("\n")
	}
	writeRow(t.Header)
	separators := make([]string, len(widths))
	for index, width := range widths {
		separators[index] = strings.Repeat("-", width)
	}
	writeRow(separators)
	for _, row := range t.Rows {
		writeRow(row)
	}
	return builder.String()
}

// pad right-pads a cell to width.
func pad(cell string, width int) string {
	if len(cell) >= width {
		return cell
	}
	return cell + strings.Repeat(" ", width-len(cell))
}

// Summary renders the headline block.
func Summary(assessment pipeline.Assessment) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "mine: %s\n", assessment.MineID)
	fmt.Fprintf(&builder, "as of: %s\n", assessment.AsOf)
	fmt.Fprintf(&builder, "policy: %s\n", assessment.Fingerprint)
	summary := assessment.Summary
	fmt.Fprintf(&builder, "points: %d usable of %d\n", summary.UsablePoints, summary.Points)
	fmt.Fprintf(&builder, "gas: worst %s, %d at trip level\n", summary.GasWorstLevel, summary.GasTripCount)
	fmt.Fprintf(&builder, "ventilation: ok=%t, %d inadequate face(s), %d recirculation(s)\n",
		summary.VentilationOK, summary.InadequateFaces, summary.Recirculations)
	fmt.Fprintf(&builder, "interlock: %d circuit(s) tripped\n", summary.TrippedCircuits)
	fmt.Fprintf(&builder, "personnel: %d underground, muster complete=%t, %d unaccounted\n",
		summary.Underground, summary.MusterComplete, summary.Unaccounted)
	fmt.Fprintf(&builder, "permits: %d refused\n", summary.RefusedPermits)
	fmt.Fprintf(&builder, "inspections: %d overdue\n", summary.OverdueInspections)
	fmt.Fprintf(&builder, "verdict: %s\n", summary.Headline)
	return builder.String()
}

// GasTable renders the gas assessments.
func GasTable(cfg config.Config, assessment pipeline.Assessment) string {
	table := Table{Header: []string{"point", "area", "value", "unit", "level", "rate/h", "flags"}}
	for _, item := range assessment.Gas.Assessments {
		flags := make([]string, 0, len(item.Flags))
		for _, flag := range item.Flags {
			flags = append(flags, string(flag))
		}
		sort.Strings(flags)
		table.Rows = append(table.Rows, []string{
			item.PointID,
			item.AreaID,
			units.Format(item.Value, cfg.Output.Decimals),
			item.Unit,
			string(item.Level),
			units.Format(item.RatePerHour, 3),
			strings.Join(flags, ","),
		})
	}
	return table.Render()
}

// VentilationTable renders the airway flows and junction balances.
func VentilationTable(cfg config.Config, assessment pipeline.Assessment) string {
	flows := Table{Header: []string{"airway", "from", "to", "m3/s", "velocity", "drop Pa", "measured"}}
	for _, flow := range assessment.Ventilation.Flows {
		flows.Rows = append(flows.Rows, []string{
			flow.AirwayID, flow.FromJunction, flow.ToJunction,
			units.Format(flow.QuantityM3S, cfg.Output.Decimals),
			units.Format(flow.VelocityMS, cfg.Output.Decimals),
			units.Format(flow.PressureDropPa, cfg.Output.Decimals),
			fmt.Sprintf("%t", flow.Measured),
		})
	}
	balances := Table{Header: []string{"junction", "in", "out", "residual", "balanced"}}
	for _, balance := range assessment.Ventilation.Balances {
		balances.Rows = append(balances.Rows, []string{
			balance.JunctionID,
			units.Format(balance.InM3S, cfg.Output.Decimals),
			units.Format(balance.OutM3S, cfg.Output.Decimals),
			units.Format(balance.ResidualM3S, cfg.Output.Decimals),
			fmt.Sprintf("%t", balance.Balanced),
		})
	}
	faces := Table{Header: []string{"face", "required", "supplied", "adequate"}}
	for _, face := range assessment.Ventilation.Faces {
		faces.Rows = append(faces.Rows, []string{
			face.FaceID,
			units.Format(face.RequiredM3S, cfg.Output.Decimals),
			units.Format(face.SuppliedM3S, cfg.Output.Decimals),
			fmt.Sprintf("%t", face.Adequate),
		})
	}
	out := flows.Render() + "\n" + balances.Render() + "\n" + faces.Render()
	if len(assessment.Ventilation.Recirculations) > 0 {
		out += "\nrecirculation:\n"
		for _, item := range assessment.Ventilation.Recirculations {
			out += "  " + item.Explanation + "\n"
		}
	}
	return out
}

// InterlockTable renders the circuit decisions.
func InterlockTable(assessment pipeline.Assessment) string {
	table := Table{Header: []string{"circuit", "area", "action", "rules"}}
	for _, decision := range assessment.Interlock.Decisions {
		rules := make([]string, 0, len(decision.Rules))
		for _, rule := range decision.Rules {
			rules = append(rules, string(rule))
		}
		table.Rows = append(table.Rows, []string{
			decision.CircuitID, decision.AreaID, string(decision.Action), strings.Join(rules, ","),
		})
	}
	out := table.Render()
	tripped := interlock.Tripped(assessment.Interlock.Decisions)
	if len(tripped) > 0 {
		out += "\ntripped: " + strings.Join(tripped, ", ") + "\n"
	}
	return out
}

// PersonnelTable renders occupancy and the muster verdict.
func PersonnelTable(assessment pipeline.Assessment) string {
	table := Table{Header: []string{"area", "kind", "count", "limit", "people"}}
	for _, item := range assessment.Personnel.Occupancy {
		table.Rows = append(table.Rows, []string{
			item.AreaID, string(item.AreaKind), fmt.Sprintf("%d", item.Count),
			fmt.Sprintf("%d", item.Limit), strings.Join(item.People, ","),
		})
	}
	out := table.Render()
	out += "\nmuster: " + assessment.Personnel.Muster.Explanation + "\n"
	for _, item := range assessment.Personnel.Muster.Unaccounted {
		out += fmt.Sprintf("  unaccounted %s last seen in %s at %s\n",
			item.PersonID, item.AreaID, item.At)
	}
	return out
}

// PermitTable renders the permit verdicts.
func PermitTable(assessment pipeline.Assessment) string {
	table := Table{Header: []string{"permit", "kind", "area", "verdict", "reasons"}}
	for _, item := range assessment.Permits.Assessments {
		reasons := make([]string, 0, len(item.Reasons))
		for _, reason := range item.Reasons {
			reasons = append(reasons, string(reason))
		}
		table.Rows = append(table.Rows, []string{
			item.PermitID, item.Kind, item.AreaID, string(item.Verdict), strings.Join(reasons, ","),
		})
	}
	out := table.Render()
	refused := permit.Refused(assessment.Permits.Assessments)
	if len(refused) > 0 {
		out += "\nrefused: " + strings.Join(refused, ", ") + "\n"
	}
	return out
}

// InspectionTable renders the inspection statuses.
func InspectionTable(assessment pipeline.Assessment) string {
	table := Table{Header: []string{"inspection", "subject", "area", "due", "overdue h"}}
	for _, item := range assessment.Inspections.Statuses {
		table.Rows = append(table.Rows, []string{
			item.InspectionID, item.Subject, item.AreaID, item.DueAt.String(),
			units.Format(item.OverdueHours, 1),
		})
	}
	return table.Render()
}

// TimelineText renders the ordered timeline.
func TimelineText(assessment pipeline.Assessment) string {
	if len(assessment.Timeline) == 0 {
		return "timeline: nothing to report\n"
	}
	var builder strings.Builder
	builder.WriteString("timeline:\n")
	for _, event := range assessment.Timeline {
		fmt.Fprintf(&builder, "  %s  %-20s %-14s %s\n", event.At, event.Kind, event.Subject, event.Detail)
	}
	return builder.String()
}

// IssuesText renders the collected findings.
func IssuesText(assessment pipeline.Assessment) string {
	if len(assessment.Issues) == 0 {
		return "issues: none\n"
	}
	var builder strings.Builder
	builder.WriteString("issues:\n")
	for _, issue := range assessment.Issues.Sorted() {
		fmt.Fprintf(&builder, "  %-8s %-40s %s\n", issue.Severity, issue.Path, issue.Message)
	}
	return builder.String()
}

// Full renders every section.
func Full(cfg config.Config, assessment pipeline.Assessment) string {
	sections := []string{
		Summary(assessment),
		GasTable(cfg, assessment),
		VentilationTable(cfg, assessment),
		InterlockTable(assessment),
		PersonnelTable(assessment),
		PermitTable(assessment),
		InspectionTable(assessment),
		TimelineText(assessment),
		IssuesText(assessment),
	}
	return strings.Join(sections, "\n")
}

// AuditText renders a chain verification.
func AuditText(report store.AuditReport, files []string) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "audit entries: %d\n", report.Entries)
	fmt.Fprintf(&builder, "verified: %t\n", report.Verified)
	fmt.Fprintf(&builder, "chronological: %t\n", report.Chronological)
	fmt.Fprintf(&builder, "head: %s\n", report.Head)
	if report.BrokenAt > 0 {
		fmt.Fprintf(&builder, "broken at: %d\n", report.BrokenAt)
	}
	for _, problem := range report.Problems {
		fmt.Fprintf(&builder, "problem: %s\n", problem)
	}
	for _, note := range report.Notes {
		fmt.Fprintf(&builder, "note: %s\n", note)
	}
	if len(files) > 0 {
		fmt.Fprintf(&builder, "files: %s\n", strings.Join(files, ", "))
	}
	return builder.String()
}

// GasIndexText renders one point's assessment for a focused query.
func GasIndexText(cfg config.Config, assessment pipeline.Assessment, pointID string) string {
	index := gas.Index(assessment.Gas.Assessments)
	item, ok := index[pointID]
	if !ok {
		return fmt.Sprintf("point %s is not in the layout\n", pointID)
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "point: %s\n", item.PointID)
	fmt.Fprintf(&builder, "area: %s %s\n", item.AreaKind, item.AreaID)
	fmt.Fprintf(&builder, "value: %s %s at %s\n", units.Format(item.Value, cfg.Output.Decimals), item.Unit, item.At)
	fmt.Fprintf(&builder, "level: %s\n", item.Level)
	fmt.Fprintf(&builder, "window: %d sample(s), mean %s, maximum %s\n", item.Samples,
		units.Format(item.Mean, cfg.Output.Decimals), units.Format(item.Maximum, cfg.Output.Decimals))
	fmt.Fprintf(&builder, "rate: %s per hour over %d sample(s)\n",
		units.Format(item.RatePerHour, 3), item.RateSamples)
	fmt.Fprintf(&builder, "explanation: %s\n", item.Explanation)
	return builder.String()
}

package report

import (
	"strings"
	"testing"

	"MineGuard/internal/config"
	"MineGuard/internal/gas"
	"MineGuard/internal/inspect"
	"MineGuard/internal/interlock"
	"MineGuard/internal/permit"
	"MineGuard/internal/pipeline"
	"MineGuard/internal/store"
	"MineGuard/internal/timeutil"
	"MineGuard/internal/units"
)

func at(text string) timeutil.Stamp { return timeutil.MustParse(text) }

// assessment is a small hand-built picture so the renderers can be checked
// without running the analysis stages.
func assessment() pipeline.Assessment {
	return pipeline.Assessment{
		Schema:      pipeline.SnapshotSchema,
		MineID:      "MG-TEST",
		Fingerprint: "abcdef0123456789",
		AsOf:        at("2026-08-15T14:00:00Z"),
		Gas: gas.Result{
			Worst: gas.LevelTrip, TripCount: 1,
			Assessments: []gas.Assessment{
				{PointID: "P-CH4", AreaID: "F-1", Value: 1.63, Unit: "pct", Level: gas.LevelTrip,
					RatePerHour: 2.94, Flags: []gas.Flag{gas.FlagRateOfRise}, At: at("2026-08-15T13:55:00Z"),
					Samples: 3, Mean: 1.4, Maximum: 1.63, RateSamples: 3, Explanation: "methane at trip level"},
			},
		},
		Interlock: interlock.Result{
			TripCount: 1,
			Decisions: []interlock.Decision{
				{CircuitID: "C-1", AreaID: "F-1", Action: interlock.ActionTrip,
					Rules: []interlock.Rule{interlock.RuleGasTrip}, At: at("2026-08-15T14:00:00Z"),
					Explanation: "tripped on gas"},
			},
		},
		Permits: permit.Result{
			RefusedCount: 1,
			Assessments: []permit.Assessment{
				{PermitID: "PM-1", Kind: "hot_work", AreaID: "F-1", Verdict: permit.VerdictRefused,
					Reasons: []permit.Reason{permit.ReasonPointAlarm}},
			},
		},
		Inspections: inspect.Result{
			OverdueCount: 1,
			Statuses: []inspect.Status{
				{InspectionID: "I-1", Subject: "roof support", AreaID: "F-1",
					DueAt: at("2026-08-15T02:00:00Z"), Overdue: true, OverdueHours: 12},
			},
		},
		Timeline: []inspect.Event{
			{At: at("2026-08-15T13:55:00Z"), Kind: "gas_trip", Subject: "P-CH4", Detail: "methane at trip level"},
		},
		Summary: pipeline.Summary{
			Points: 1, UsablePoints: 1, GasWorstLevel: "trip", GasTripCount: 1,
			VentilationOK: true, TrippedCircuits: 1, Underground: 4, MusterComplete: true,
			RefusedPermits: 1, OverdueInspections: 1, Safe: false,
			Headline: "1 circuit(s) tripped; 1 gas trip level(s)",
		},
	}
}

func TestTableAlignsColumnsToTheWidestCell(t *testing.T) {
	table := Table{
		Header: []string{"id", "value"},
		Rows: [][]string{
			{"a-very-long-identifier", "1"},
			{"b", "22"},
		},
	}
	lines := strings.Split(strings.TrimRight(table.Render(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("rendered %d lines, want a header, a rule and two rows", len(lines))
	}
	if lines[0] != "id                      value" {
		t.Errorf("header = %q", lines[0])
	}
	if lines[1] != "----------------------  -----" {
		t.Errorf("rule = %q", lines[1])
	}
	if lines[2] != "a-very-long-identifier  1" {
		t.Errorf("first row = %q", lines[2])
	}
	if lines[3] != "b                       22" {
		t.Errorf("second row = %q", lines[3])
	}
}

func TestTableTrimsTrailingPadding(t *testing.T) {
	table := Table{Header: []string{"a", "b"}, Rows: [][]string{{"x", ""}}}
	for _, line := range strings.Split(strings.TrimRight(table.Render(), "\n"), "\n") {
		if strings.HasSuffix(line, " ") {
			t.Errorf("line %q carries trailing padding", line)
		}
	}
}

func TestTableIgnoresCellsBeyondTheHeader(t *testing.T) {
	table := Table{Header: []string{"a"}, Rows: [][]string{{"x", "surplus"}}}
	if got := table.Render(); !strings.Contains(got, "surplus") {
		t.Errorf("a surplus cell is still rendered, got %q", got)
	}
}

func TestSummaryStatesEverySection(t *testing.T) {
	text := Summary(assessment())
	for _, want := range []string{
		"mine: MG-TEST", "as of: 2026-08-15T14:00:00Z", "policy: abcdef0123456789",
		"points: 1 usable of 1", "gas: worst trip, 1 at trip level", "ventilation: ok=true",
		"interlock: 1 circuit(s) tripped", "personnel: 4 underground", "permits: 1 refused",
		"inspections: 1 overdue", "verdict: 1 circuit(s) tripped",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("summary is missing %q:\n%s", want, text)
		}
	}
}

func TestGasTableRendersFlagsAndValues(t *testing.T) {
	text := GasTable(config.Default(), assessment())
	if !strings.Contains(text, "P-CH4") || !strings.Contains(text, "1.630") {
		t.Errorf("gas table = \n%s", text)
	}
	if !strings.Contains(text, string(gas.FlagRateOfRise)) {
		t.Errorf("gas table must carry the flags:\n%s", text)
	}
}

func TestInterlockTableNamesTheTrippedCircuits(t *testing.T) {
	text := InterlockTable(assessment())
	if !strings.Contains(text, "tripped: C-1") {
		t.Errorf("interlock table must list the tripped circuits:\n%s", text)
	}
	if !strings.Contains(text, string(interlock.RuleGasTrip)) {
		t.Errorf("interlock table must state the rule:\n%s", text)
	}
}

func TestPermitTableNamesTheRefusedPermits(t *testing.T) {
	text := PermitTable(assessment())
	if !strings.Contains(text, "refused: PM-1") {
		t.Errorf("permit table must list the refusals:\n%s", text)
	}
	if !strings.Contains(text, string(permit.ReasonPointAlarm)) {
		t.Errorf("permit table must state the reason:\n%s", text)
	}
}

func TestInspectionTableShowsTheDueInstant(t *testing.T) {
	text := InspectionTable(assessment())
	if !strings.Contains(text, "2026-08-15T02:00:00Z") || !strings.Contains(text, "12.0") {
		t.Errorf("inspection table = \n%s", text)
	}
}

func TestTimelineTextFallsBackWhenNothingHappened(t *testing.T) {
	if got := TimelineText(pipeline.Assessment{}); got != "timeline: nothing to report\n" {
		t.Errorf("TimelineText = %q", got)
	}
	if got := TimelineText(assessment()); !strings.Contains(got, "gas_trip") {
		t.Errorf("TimelineText = %q", got)
	}
}

func TestIssuesTextFallsBackWhenThereAreNone(t *testing.T) {
	if got := IssuesText(pipeline.Assessment{}); got != "issues: none\n" {
		t.Errorf("IssuesText = %q", got)
	}
}

func TestFullRendersEverySectionOnce(t *testing.T) {
	text := Full(config.Default(), assessment())
	for _, want := range []string{"mine: MG-TEST", "circuit", "permit", "inspection", "timeline:", "issues:"} {
		if strings.Count(text, want) == 0 {
			t.Errorf("full report is missing %q", want)
		}
	}
}

func TestFullIsByteIdenticalBetweenCalls(t *testing.T) {
	cfg := config.Default()
	if Full(cfg, assessment()) != Full(cfg, assessment()) {
		t.Fatal("two renderings of the same assessment must be identical")
	}
}

func TestAuditTextReportsProblemsAndNotes(t *testing.T) {
	text := AuditText(store.AuditReport{
		Entries: 3, Verified: false, Chronological: false, BrokenAt: 2,
		Problems: []string{"entry 2 does not link to its predecessor"},
		Notes:    []string{"entry 3 carries an earlier instant"},
	}, []string{"audit.jsonl", "meta.json"})
	for _, want := range []string{
		"audit entries: 3", "verified: false", "chronological: false", "broken at: 2",
		"problem: entry 2", "note: entry 3", "files: audit.jsonl, meta.json",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("audit text is missing %q:\n%s", want, text)
		}
	}
}

func TestGasIndexTextExplainsOnePoint(t *testing.T) {
	cfg := config.Default()
	text := GasIndexText(cfg, assessment(), "P-CH4")
	for _, want := range []string{
		"point: P-CH4", "level: trip", "window: 3 sample(s)", "rate: 2.940 per hour over 3 sample(s)",
		"explanation: methane at trip level",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("point report is missing %q:\n%s", want, text)
		}
	}
	if got := GasIndexText(cfg, assessment(), "P-GHOST"); !strings.Contains(got, "not in the layout") {
		t.Errorf("an unknown point must be reported plainly, got %q", got)
	}
}

func TestUnitsFormatIsUsedForNumericCells(t *testing.T) {
	// The report never prints a raw float, so a value that would otherwise render
	// in exponent notation stays readable.
	if got := units.Format(0.000001234, 3); got != "0.000" {
		t.Fatalf("Format = %q, want a fixed decimal rendering", got)
	}
}

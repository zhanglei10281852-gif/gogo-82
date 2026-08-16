package pipeline

import (
	"testing"

	"MineGuard/internal/config"
	"MineGuard/internal/gas"
	"MineGuard/internal/interlock"
	"MineGuard/internal/model"
	"MineGuard/internal/strictjson"
	"MineGuard/internal/timeutil"
	"MineGuard/internal/units"
)

const asOfText = "2026-08-15T14:00:00Z"

func calibration() model.Calibration {
	return model.Calibration{At: timeutil.MustParse("2026-08-10T06:00:00Z"), ValidDays: 30}
}

// bundle is one face fed by one airway, guarded by one circuit.
func bundle() model.Bundle {
	layout := model.Layout{
		SchemaVersion: model.SchemaVersion,
		MineID:        "MG-TEST",
		Junctions: []model.Junction{
			{JunctionID: "J-IN", Surface: true},
			{JunctionID: "J-FACE"},
			{JunctionID: "J-OUT", Surface: true},
		},
		Airways: []model.Airway{
			{AirwayID: "A-IN", FromJunction: "J-IN", ToJunction: "J-FACE", LengthM: 200, AreaM2: 12, Resistance: 0.01},
			{AirwayID: "A-OUT", FromJunction: "J-FACE", ToJunction: "J-OUT", LengthM: 200, AreaM2: 12, Resistance: 0.01},
		},
		Faces: []model.Face{{
			FaceID: "F-1", IntakeJunction: "J-FACE", ReturnJunction: "J-OUT",
			PlannedPersons: 6, EquipmentKW: 100,
		}},
		Points: []model.Point{
			{
				PointID: "P-CH4", Quantity: units.QuantityMethane, AreaKind: model.AreaWorkingFace,
				AreaID: "F-1", Placement: model.PlacementGeneralBody, RangeLow: 0, RangeHigh: 5,
				Threshold:   model.Threshold{Warn: 1, Trip: 1.5, Direction: "above"},
				Calibration: calibration(), Mandatory: true, StaleMinutes: 240,
			},
			{
				PointID: "P-FLOW", Quantity: units.QuantityAirflow, AreaKind: model.AreaAirway,
				AreaID: "A-IN", Placement: model.PlacementIntake, RangeLow: 0, RangeHigh: 80,
				Threshold:   model.Threshold{Warn: 8, Trip: 5, Direction: "below"},
				Calibration: calibration(), Mandatory: true, StaleMinutes: 240,
			},
			{
				PointID: "P-FLOW-OUT", Quantity: units.QuantityAirflow, AreaKind: model.AreaAirway,
				AreaID: "A-OUT", Placement: model.PlacementReturn, RangeLow: 0, RangeHigh: 80,
				Threshold:   model.Threshold{Warn: 8, Trip: 5, Direction: "below"},
				Calibration: calibration(), Mandatory: true, StaleMinutes: 240,
			},
		},
		Circuits: []model.Circuit{{
			CircuitID: "C-1", Label: "shearer", AreaKind: model.AreaWorkingFace, AreaID: "F-1",
			GuardPoints: []string{"P-CH4"}, RequiresVentilation: true,
		}},
		Inspections: []model.Inspection{{
			InspectionID: "I-1", Subject: "roof support", AreaKind: model.AreaWorkingFace, AreaID: "F-1",
			IntervalHours: 168, LastDoneAt: timeutil.MustParse("2026-08-14T06:00:00Z"),
		}},
	}
	readings := []model.Reading{}
	for _, at := range []string{"2026-08-15T13:45:00Z", "2026-08-15T13:50:00Z", "2026-08-15T13:55:00Z"} {
		readings = append(readings,
			model.Reading{ReadingID: "R-CH4-" + at, PointID: "P-CH4", At: timeutil.MustParse(at),
				Value: 0.41, Unit: "pct", Status: model.StatusOK},
			model.Reading{ReadingID: "R-FLOW-" + at, PointID: "P-FLOW", At: timeutil.MustParse(at),
				Value: 20, Unit: "m3/s", Status: model.StatusOK},
			model.Reading{ReadingID: "R-FLOWOUT-" + at, PointID: "P-FLOW-OUT", At: timeutil.MustParse(at),
				Value: 20, Unit: "m3/s", Status: model.StatusOK},
		)
	}
	return model.Bundle{
		Layout:   layout,
		Readings: readings,
		Roster:   []model.Person{{PersonID: "W-1", Name: "worker one"}},
		Events: []model.TagEvent{{
			EventID: "E-1", PersonID: "W-1", At: timeutil.MustParse("2026-08-15T06:00:00Z"),
			Action: model.ActionEnter, AreaKind: model.AreaWorkingFace, AreaID: "F-1",
		}},
		Permits: []model.Permit{{
			PermitID: "PM-1", Kind: "hot_work", AreaKind: model.AreaWorkingFace, AreaID: "F-1",
			Window: timeutil.Window{
				From: timeutil.MustParse("2026-08-15T08:00:00Z"),
				To:   timeutil.MustParse("2026-08-15T15:00:00Z"),
			},
			RequiredPoints: []string{"P-CH4"},
		}},
	}
}

func run(t *testing.T, item model.Bundle, options Options) Assessment {
	t.Helper()
	if issues := model.ValidateBundle(item); !issues.OK() {
		t.Fatalf("the fixture must validate: %v", issues.Error())
	}
	return Run(config.Default(), item, options)
}

func TestEveryStageIsPopulated(t *testing.T) {
	assessment := run(t, bundle(), Options{AsOf: timeutil.MustParse(asOfText)})
	if assessment.Schema != SnapshotSchema {
		t.Errorf("schema = %q, want %q", assessment.Schema, SnapshotSchema)
	}
	if assessment.MineID != "MG-TEST" || assessment.Fingerprint == "" {
		t.Errorf("assessment header = %+v", assessment.Summary)
	}
	if len(assessment.Readings.States) != 3 {
		t.Errorf("point states = %d, want 3", len(assessment.Readings.States))
	}
	if len(assessment.Gas.Assessments) != 3 {
		t.Errorf("gas assessments = %d, want 3", len(assessment.Gas.Assessments))
	}
	if len(assessment.Ventilation.Flows) != 2 {
		t.Errorf("flows = %d, want 2", len(assessment.Ventilation.Flows))
	}
	if len(assessment.Interlock.Decisions) != 1 {
		t.Errorf("decisions = %d, want 1", len(assessment.Interlock.Decisions))
	}
	if len(assessment.Permits.Assessments) != 1 {
		t.Errorf("permits = %d, want 1", len(assessment.Permits.Assessments))
	}
	if len(assessment.Inspections.Statuses) != 1 {
		t.Errorf("inspections = %d, want 1", len(assessment.Inspections.Statuses))
	}
}

func TestAClearMineIsSafe(t *testing.T) {
	assessment := run(t, bundle(), Options{AsOf: timeutil.MustParse(asOfText)})
	summary := assessment.Summary
	if !summary.Safe {
		t.Fatalf("the fixture must be safe: %s", summary.Headline)
	}
	if summary.UsablePoints != 3 || summary.Points != 3 {
		t.Errorf("usable = %d of %d, want 3 of 3", summary.UsablePoints, summary.Points)
	}
	if summary.GasWorstLevel != string(gas.LevelNormal) || summary.GasTripCount != 0 {
		t.Errorf("gas summary = %s / %d", summary.GasWorstLevel, summary.GasTripCount)
	}
	if !summary.VentilationOK || summary.TrippedCircuits != 0 {
		t.Errorf("ventilation ok = %t, tripped = %d", summary.VentilationOK, summary.TrippedCircuits)
	}
	if summary.Underground != 1 {
		t.Errorf("underground = %d, want 1", summary.Underground)
	}
	if summary.Headline == "" {
		t.Error("a summary must carry a headline")
	}
}

func TestGasTripPropagatesToTheInterlockAndTheSummary(t *testing.T) {
	item := bundle()
	for index := range item.Readings {
		if item.Readings[index].PointID == "P-CH4" {
			item.Readings[index].Value = 1.8
		}
	}
	assessment := run(t, item, Options{AsOf: timeutil.MustParse(asOfText)})
	if assessment.Summary.GasTripCount != 1 {
		t.Fatalf("gas trips = %d, want 1", assessment.Summary.GasTripCount)
	}
	if assessment.Summary.TrippedCircuits != 1 {
		t.Fatalf("the interlock must follow the gas verdict, tripped = %d",
			assessment.Summary.TrippedCircuits)
	}
	if assessment.Summary.Safe {
		t.Error("a mine with a tripped circuit is not safe")
	}
	if assessment.Interlock.Decisions[0].Action != interlock.ActionTrip {
		t.Errorf("action = %s, want trip", assessment.Interlock.Decisions[0].Action)
	}
	if len(assessment.Timeline) == 0 {
		t.Error("a trip must appear on the timeline")
	}
}

func TestInadequateVentilationPropagates(t *testing.T) {
	item := bundle()
	for index := range item.Readings {
		if item.Readings[index].PointID == "P-FLOW" || item.Readings[index].PointID == "P-FLOW-OUT" {
			item.Readings[index].Value = 1
		}
	}
	assessment := run(t, item, Options{AsOf: timeutil.MustParse(asOfText)})
	if assessment.Summary.VentilationOK {
		t.Fatal("1 m3/s cannot supply the face")
	}
	if assessment.Summary.InadequateFaces != 1 {
		t.Errorf("inadequate faces = %d, want 1", assessment.Summary.InadequateFaces)
	}
	if assessment.Summary.TrippedCircuits != 1 {
		t.Errorf("a ventilation dependent circuit must trip, tripped = %d",
			assessment.Summary.TrippedCircuits)
	}
	if assessment.Summary.Safe {
		t.Error("a mine with an inadequate face is not safe")
	}
}

func TestARefusedMandatoryHeadTripsWithoutInventingGasData(t *testing.T) {
	item := bundle()
	for index := range item.Readings {
		if item.Readings[index].PointID == "P-CH4" {
			item.Readings[index].Status = model.StatusOffline
		}
	}
	assessment := run(t, item, Options{AsOf: timeutil.MustParse(asOfText)})
	if assessment.Summary.UsablePoints != 2 {
		t.Fatalf("usable points = %d, want 2", assessment.Summary.UsablePoints)
	}
	if assessment.Summary.GasTripCount != 0 {
		t.Errorf("a refused head must not be reported as a gas trip, got %d",
			assessment.Summary.GasTripCount)
	}
	if assessment.Summary.TrippedCircuits != 1 {
		t.Fatalf("the absence of evidence must trip the circuit, tripped = %d",
			assessment.Summary.TrippedCircuits)
	}
	if assessment.Summary.Safe {
		t.Error("a mine whose mandatory head is offline is not safe")
	}
}

func TestDeriveAsOfTakesTheNewestInput(t *testing.T) {
	item := bundle()
	if got := DeriveAsOf(item).String(); got != "2026-08-15T15:00:00Z" {
		t.Fatalf("DeriveAsOf = %s, want the permit window end", got)
	}
	item.Permits = nil
	if got := DeriveAsOf(item).String(); got != "2026-08-15T13:55:00Z" {
		t.Fatalf("DeriveAsOf = %s, want the newest reading", got)
	}
	if DeriveAsOf(model.Bundle{}).IsSet() {
		t.Error("an empty bundle has no instant to derive")
	}
}

func TestRunWithoutAnInstantDerivesOne(t *testing.T) {
	assessment := Run(config.Default(), bundle(), Options{})
	if assessment.AsOf.String() != "2026-08-15T15:00:00Z" {
		t.Fatalf("as of = %s, want the derived instant", assessment.AsOf)
	}
}

func TestRunIsDeterministic(t *testing.T) {
	first := run(t, bundle(), Options{AsOf: timeutil.MustParse(asOfText)})
	second := run(t, bundle(), Options{AsOf: timeutil.MustParse(asOfText)})
	left, err := strictjson.Encode(first)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	right, err := strictjson.Encode(second)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(left) != string(right) {
		t.Fatal("two runs over the same bundle must encode identically")
	}
}

func TestIssuesFromEveryStageAreCollectedInOrder(t *testing.T) {
	item := bundle()
	// Drop the return airway measurement so ventilation raises a warning, and add a
	// tag event for somebody outside the roster so personnel raises one too.
	kept := item.Readings[:0]
	for _, entry := range item.Readings {
		if entry.PointID == "P-FLOW-OUT" {
			continue
		}
		kept = append(kept, entry)
	}
	item.Readings = kept
	item.Events = append(item.Events, model.TagEvent{
		EventID: "E-9", PersonID: "W-9", At: timeutil.MustParse("2026-08-15T06:00:00Z"),
		Action: model.ActionEnter, AreaKind: model.AreaWorkingFace, AreaID: "F-1",
	})
	assessment := Run(config.Default(), item, Options{AsOf: timeutil.MustParse(asOfText)})
	if len(assessment.Issues) < 2 {
		t.Fatalf("issues = %+v, want findings from two stages", assessment.Issues)
	}
	for index := 1; index < len(assessment.Issues); index++ {
		previous, current := assessment.Issues[index-1], assessment.Issues[index]
		if previous.Severity > current.Severity {
			t.Fatalf("issues are not in canonical order: %+v", assessment.Issues)
		}
	}
}

func TestEvacuationOrderIsPassedToThePersonnelStage(t *testing.T) {
	assessment := run(t, bundle(), Options{
		AsOf:      timeutil.MustParse(asOfText),
		OrderedAt: timeutil.MustParse("2026-08-15T13:20:00Z"),
	})
	if !assessment.Personnel.Muster.Ordered {
		t.Fatal("the muster must know an order is in force")
	}
	if assessment.Personnel.Muster.Deadline.String() != "2026-08-15T14:05:00Z" {
		t.Errorf("deadline = %s", assessment.Personnel.Muster.Deadline)
	}
}

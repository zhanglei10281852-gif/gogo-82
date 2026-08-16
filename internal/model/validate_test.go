package model

import (
	"strings"
	"testing"

	"MineGuard/internal/timeutil"
	"MineGuard/internal/units"
)

func calibration() Calibration {
	return Calibration{At: timeutil.MustParse("2026-08-10T06:00:00Z"), ValidDays: 30}
}

// sound is a minimal layout that passes validation.
func sound() Layout {
	return Layout{
		SchemaVersion: SchemaVersion,
		MineID:        "MG-TEST",
		Districts:     []District{{DistrictID: "D-1", Label: "north"}},
		Junctions:     []Junction{{JunctionID: "J-1", Surface: true}, {JunctionID: "J-2"}},
		Airways: []Airway{{
			AirwayID: "A-1", FromJunction: "J-1", ToJunction: "J-2",
			LengthM: 100, AreaM2: 12, Resistance: 0.01,
		}},
		Faces: []Face{{
			FaceID: "F-1", DistrictID: "D-1", IntakeJunction: "J-2", ReturnJunction: "J-1",
			PlannedPersons: 6, EquipmentKW: 100,
		}},
		Points: []Point{{
			PointID: "P-CH4", Quantity: units.QuantityMethane, AreaKind: AreaWorkingFace,
			AreaID: "F-1", Placement: PlacementGeneralBody, RangeLow: 0, RangeHigh: 5,
			Threshold: Threshold{Warn: 1, Trip: 1.5, Direction: "above"}, Calibration: calibration(),
		}},
		Circuits: []Circuit{{
			CircuitID: "C-1", AreaKind: AreaWorkingFace, AreaID: "F-1",
			GuardPoints: []string{"P-CH4"},
		}},
		Inspections: []Inspection{{
			InspectionID: "I-1", Subject: "roof support", AreaKind: AreaWorkingFace, AreaID: "F-1",
			IntervalHours: 24, LastDoneAt: timeutil.MustParse("2026-08-15T02:00:00Z"),
		}},
	}
}

func hasPath(issues Issues, want string) bool {
	for _, issue := range issues {
		if issue.Path == want {
			return true
		}
	}
	return false
}

func TestSoundLayoutValidates(t *testing.T) {
	issues := ValidateLayout(sound())
	if !issues.OK() {
		t.Fatalf("layout must validate: %v", issues.Error())
	}
}

func TestWrongSchemaVersionIsAnError(t *testing.T) {
	layout := sound()
	layout.SchemaVersion = "mineguard/v0"
	if hasPath(ValidateLayout(layout).Errors(), "schema_version") == false {
		t.Fatal("a foreign schema version must be an error")
	}
}

func TestGuardPointMustExist(t *testing.T) {
	layout := sound()
	layout.Circuits[0].GuardPoints = []string{"P-GHOST"}
	issues := ValidateLayout(layout).Errors()
	if !hasPath(issues, "circuits[0].guard_points[0]") {
		t.Fatalf("a guard point nobody can resolve must be an error: %v", issues)
	}
}

func TestGuardPointListedTwiceIsAnError(t *testing.T) {
	layout := sound()
	layout.Circuits[0].GuardPoints = []string{"P-CH4", "P-CH4"}
	if !hasPath(ValidateLayout(layout).Errors(), "circuits[0].guard_points[1]") {
		t.Fatal("a duplicated guard point must be an error")
	}
}

func TestCircuitWithoutGuardPointsIsAnError(t *testing.T) {
	layout := sound()
	layout.Circuits[0].GuardPoints = nil
	if !hasPath(ValidateLayout(layout).Errors(), "circuits[0].guard_points") {
		t.Fatal("a controlled circuit must name at least one guard point")
	}
}

func TestAirwayEndpointsMustResolveAndDiffer(t *testing.T) {
	layout := sound()
	layout.Airways[0].ToJunction = "J-1"
	if !hasPath(ValidateLayout(layout).Errors(), "airways[0].to_junction") {
		t.Fatal("an airway cannot start and end at the same junction")
	}
	layout = sound()
	layout.Airways[0].FromJunction = "J-GHOST"
	if !hasPath(ValidateLayout(layout).Errors(), "airways[0].from_junction") {
		t.Fatal("an airway must start at a known junction")
	}
}

func TestAirwayGeometryMustBePositive(t *testing.T) {
	for field, mutate := range map[string]func(Layout) Layout{
		"airways[0].length_m":         func(l Layout) Layout { l.Airways[0].LengthM = 0; return l },
		"airways[0].area_m2":          func(l Layout) Layout { l.Airways[0].AreaM2 = 0; return l },
		"airways[0].resistance_ns2m8": func(l Layout) Layout { l.Airways[0].Resistance = 0; return l },
		"airways[0].fan_pressure_pa":  func(l Layout) Layout { l.Airways[0].FanPressurePa = -1; return l },
	} {
		if !hasPath(ValidateLayout(mutate(sound())).Errors(), field) {
			t.Errorf("%s accepted a non-physical value", field)
		}
	}
}

func TestFaceIntakeAndReturnMustDiffer(t *testing.T) {
	layout := sound()
	layout.Faces[0].ReturnJunction = layout.Faces[0].IntakeJunction
	if !hasPath(ValidateLayout(layout).Errors(), "faces[0].return_junction") {
		t.Fatal("a face cannot take and return air through one junction")
	}
}

func TestDuplicateIdentifiersAreErrors(t *testing.T) {
	layout := sound()
	layout.Points = append(layout.Points, layout.Points[0])
	if !hasPath(ValidateLayout(layout).Errors(), "points[1].point_id") {
		t.Fatal("a duplicated point identifier must be an error")
	}
}

func TestThresholdDirectionMustBeStated(t *testing.T) {
	layout := sound()
	layout.Points[0].Threshold.Direction = "sideways"
	if !hasPath(ValidateLayout(layout).Errors(), "points[0].threshold.direction") {
		t.Fatal("an unknown alarm direction must be an error")
	}
}

func TestRisingThresholdMustNotTripBeforeItWarns(t *testing.T) {
	layout := sound()
	layout.Points[0].Threshold = Threshold{Warn: 2, Trip: 1, Direction: "above"}
	if !hasPath(ValidateLayout(layout).Errors(), "points[0].threshold.trip") {
		t.Fatal("a rising trip level below the warn level must be an error")
	}
}

func TestFallingThresholdMustNotTripBeforeItWarns(t *testing.T) {
	layout := sound()
	layout.Points[0].RangeHigh = 25
	layout.Points[0].Threshold = Threshold{Warn: 19, Trip: 19.5, Direction: "below"}
	if !hasPath(ValidateLayout(layout).Errors(), "points[0].threshold.trip") {
		t.Fatal("a falling trip level above the warn level must be an error")
	}
}

func TestAlarmLevelsMustSitInsideTheInstrumentRange(t *testing.T) {
	layout := sound()
	layout.Points[0].Threshold = Threshold{Warn: 1, Trip: 9, Direction: "above"}
	if !hasPath(ValidateLayout(layout).Errors(), "points[0].threshold.trip") {
		t.Fatal("an alarm level past the instrument span must be an error")
	}
}

func TestCalibrationMustBeStated(t *testing.T) {
	layout := sound()
	layout.Points[0].Calibration = Calibration{}
	issues := ValidateLayout(layout).Errors()
	if !hasPath(issues, "points[0].calibration.at") || !hasPath(issues, "points[0].calibration.valid_days") {
		t.Fatalf("a point without calibration must be an error: %v", issues)
	}
}

func TestLayoutWithoutPointsIsAnErrorAndWithoutFacesAWarning(t *testing.T) {
	layout := sound()
	layout.Points = nil
	layout.Circuits = nil
	layout.Faces = nil
	layout.Inspections = nil
	issues := ValidateLayout(layout)
	if !hasPath(issues.Errors(), "points") {
		t.Fatal("a layout with no monitoring point cannot be assessed")
	}
	if !hasPath(issues, "faces") {
		t.Fatal("a layout with no working face must be reported")
	}
	for _, issue := range issues {
		if issue.Path == "faces" && issue.Severity != SeverityWarning {
			t.Errorf("the missing face finding must be a warning, got %s", issue.Severity)
		}
	}
}

func TestValidateReadingsChecksUnitsAndPoints(t *testing.T) {
	layout := sound()
	readings := []Reading{
		{ReadingID: "R-1", PointID: "P-CH4", At: timeutil.MustParse("2026-08-15T13:55:00Z"),
			Value: 0.4, Unit: "pct", Status: StatusOK},
		{ReadingID: "R-2", PointID: "P-GHOST", At: timeutil.MustParse("2026-08-15T13:55:00Z"),
			Value: 0.4, Unit: "pct", Status: StatusOK},
		{ReadingID: "R-1", PointID: "P-CH4", At: timeutil.MustParse("2026-08-15T13:56:00Z"),
			Value: 0.4, Unit: "Pa", Status: "broken"},
	}
	issues := ValidateReadings(layout, readings).Errors()
	for _, want := range []string{
		"readings[1].point_id", "readings[2].reading_id", "readings[2].unit", "readings[2].status",
	} {
		if !hasPath(issues, want) {
			t.Errorf("missing finding at %s: %v", want, issues)
		}
	}
}

func TestValidatePersonnelChecksRosterAndAreas(t *testing.T) {
	layout := sound()
	roster := []Person{{PersonID: "W-1"}, {PersonID: "W-1"}}
	events := []TagEvent{
		{EventID: "E-1", PersonID: "W-1", At: timeutil.MustParse("2026-08-15T06:00:00Z"),
			Action: ActionEnter, AreaKind: AreaWorkingFace, AreaID: "F-GHOST"},
		{EventID: "E-2", PersonID: "W-9", At: timeutil.MustParse("2026-08-15T06:00:00Z"),
			Action: ActionEnter, AreaKind: AreaWorkingFace, AreaID: "F-1"},
		{EventID: "E-3", PersonID: "W-1", At: timeutil.MustParse("2026-08-15T06:00:00Z"),
			Action: "teleport", AreaKind: AreaWorkingFace, AreaID: "F-1"},
	}
	issues := ValidatePersonnel(layout, roster, events).Errors()
	for _, want := range []string{
		"roster[1].person_id", "events[0].area_id", "events[1].person_id", "events[2].action",
	} {
		if !hasPath(issues, want) {
			t.Errorf("missing finding at %s: %v", want, issues)
		}
	}
}

func TestExitEventNeedsNoResolvableArea(t *testing.T) {
	layout := sound()
	roster := []Person{{PersonID: "W-1"}}
	events := []TagEvent{{
		EventID: "E-1", PersonID: "W-1", At: timeutil.MustParse("2026-08-15T12:00:00Z"),
		Action: ActionExit, AreaKind: AreaSurface, AreaID: "LAMP-ROOM",
	}}
	if issues := ValidatePersonnel(layout, roster, events); !issues.OK() {
		t.Fatalf("an exit to a free-form surface area must validate: %v", issues.Error())
	}
}

func TestValidatePermitsChecksWindowsAndPoints(t *testing.T) {
	layout := sound()
	permits := []Permit{
		{PermitID: "PM-1", Kind: "", AreaKind: AreaWorkingFace, AreaID: "F-GHOST",
			Window: timeutil.Window{
				From: timeutil.MustParse("2026-08-15T15:00:00Z"),
				To:   timeutil.MustParse("2026-08-15T08:00:00Z"),
			},
			RequiredPoints: []string{"P-GHOST"}},
		{PermitID: "PM-2", Kind: "hot_work", AreaKind: AreaWorkingFace, AreaID: "F-1"},
	}
	issues := ValidatePermits(layout, permits).Errors()
	for _, want := range []string{
		"permits[0].kind", "permits[0].area_id", "permits[0].window",
		"permits[0].required_points[0]", "permits[1].window", "permits[1].required_points",
	} {
		if !hasPath(issues, want) {
			t.Errorf("missing finding at %s: %v", want, issues)
		}
	}
}

func TestIssuesSortAndRenderDeterministically(t *testing.T) {
	issues := Issues{}
	issues.Add(SeverityWarning, "b", "second")
	issues.Add(SeverityError, "a", "first")
	issues.Add(SeverityError, "a", "another")
	sorted := issues.Sorted()
	if sorted[0].Severity != SeverityError || sorted[0].Message != "another" {
		t.Fatalf("sorted issues = %+v", sorted)
	}
	if issues.OK() {
		t.Fatal("a collection with an error is not ok")
	}
	err := issues.Error()
	if err == nil || !strings.Contains(err.Error(), "a: another") {
		t.Fatalf("rendered error = %v", err)
	}
	if (Issues{}).Error() != nil {
		t.Error("an empty collection renders no error")
	}
}

func TestSortPutsEveryCollectionInOrder(t *testing.T) {
	layout := sound()
	layout.Points = append([]Point{{PointID: "P-ZZ"}}, layout.Points...)
	layout.Sort()
	if layout.Points[0].PointID != "P-CH4" {
		t.Fatalf("points must be ordered, got %s first", layout.Points[0].PointID)
	}
}

func TestPointsInAreaAndIndexes(t *testing.T) {
	layout := sound()
	if got := layout.PointsInArea(AreaWorkingFace, "F-1"); len(got) != 1 {
		t.Fatalf("PointsInArea = %d points, want 1", len(got))
	}
	if _, ok := layout.PointByID()["P-CH4"]; !ok {
		t.Error("PointByID must index the point")
	}
	if _, ok := layout.FaceByID()["F-1"]; !ok {
		t.Error("FaceByID must index the face")
	}
	if _, ok := layout.JunctionByID()["J-1"]; !ok {
		t.Error("JunctionByID must index the junction")
	}
	if _, ok := layout.AirwayByID()["A-1"]; !ok {
		t.Error("AirwayByID must index the airway")
	}
	if _, ok := layout.CircuitByID()["C-1"]; !ok {
		t.Error("CircuitByID must index the circuit")
	}
}

func TestSortReadingsAndEventsAreTotalOrders(t *testing.T) {
	readings := []Reading{
		{ReadingID: "R-2", PointID: "P-B", At: timeutil.MustParse("2026-08-15T13:00:00Z")},
		{ReadingID: "R-1", PointID: "P-B", At: timeutil.MustParse("2026-08-15T13:00:00Z")},
		{ReadingID: "R-3", PointID: "P-A", At: timeutil.MustParse("2026-08-15T13:00:00Z")},
		{ReadingID: "R-4", PointID: "P-A", At: timeutil.MustParse("2026-08-15T12:00:00Z")},
	}
	SortReadings(readings)
	if readings[0].ReadingID != "R-4" || readings[1].ReadingID != "R-3" ||
		readings[2].ReadingID != "R-1" || readings[3].ReadingID != "R-2" {
		t.Fatalf("SortReadings produced %s %s %s %s",
			readings[0].ReadingID, readings[1].ReadingID, readings[2].ReadingID, readings[3].ReadingID)
	}
	events := []TagEvent{
		{EventID: "E-2", PersonID: "W-2", At: timeutil.MustParse("2026-08-15T06:00:00Z")},
		{EventID: "E-1", PersonID: "W-1", At: timeutil.MustParse("2026-08-15T06:00:00Z")},
	}
	SortEvents(events)
	if events[0].EventID != "E-1" {
		t.Fatalf("SortEvents produced %s first", events[0].EventID)
	}
}

func TestStatusAndKindPredicates(t *testing.T) {
	if !StatusOK.Usable() || StatusFaulty.Usable() || StatusMaintenance.Usable() || StatusOffline.Usable() {
		t.Error("only an ok sensor is usable")
	}
	if !ValidStatus(StatusOffline) || ValidStatus(SensorStatus("melted")) {
		t.Error("ValidStatus must accept the known statuses only")
	}
	if !ValidAreaKind(AreaSurface) || ValidAreaKind(AreaKind("orbit")) {
		t.Error("ValidAreaKind must accept the known kinds only")
	}
	if !ValidPlacement(PlacementReturn) || ValidPlacement(Placement("floor")) {
		t.Error("ValidPlacement must accept the known placements only")
	}
	if !ValidAction(ActionMuster) || ValidAction("teleport") {
		t.Error("ValidAction must accept the known actions only")
	}
}

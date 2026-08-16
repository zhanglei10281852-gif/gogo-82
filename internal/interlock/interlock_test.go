package interlock

import (
	"testing"

	"MineGuard/internal/config"
	"MineGuard/internal/gas"
	"MineGuard/internal/model"
	"MineGuard/internal/reading"
	"MineGuard/internal/timeutil"
	"MineGuard/internal/units"
	"MineGuard/internal/ventilation"
)

const asOfText = "2026-08-15T14:00:00Z"

// fixture is one face with a methane head, an airflow head and a guarded circuit.
func fixture() model.Layout {
	calibration := model.Calibration{At: timeutil.MustParse("2026-08-10T06:00:00Z"), ValidDays: 30}
	return model.Layout{
		SchemaVersion: model.SchemaVersion,
		MineID:        "MG-TEST",
		Junctions: []model.Junction{
			{JunctionID: "J-IN", Surface: true},
			{JunctionID: "J-FACE"},
			{JunctionID: "J-OUT", Surface: true},
		},
		Airways: []model.Airway{
			{AirwayID: "A-IN", FromJunction: "J-IN", ToJunction: "J-FACE", LengthM: 100, AreaM2: 12, Resistance: 0.01},
			{AirwayID: "A-OUT", FromJunction: "J-FACE", ToJunction: "J-OUT", LengthM: 100, AreaM2: 12, Resistance: 0.01},
		},
		Faces: []model.Face{
			{FaceID: "F-1", IntakeJunction: "J-FACE", ReturnJunction: "J-OUT", PlannedPersons: 6, EquipmentKW: 100},
		},
		Points: []model.Point{
			{
				PointID: "P-CH4", Quantity: units.QuantityMethane, AreaKind: model.AreaWorkingFace,
				AreaID: "F-1", Placement: model.PlacementGeneralBody, RangeLow: 0, RangeHigh: 5,
				Threshold:   model.Threshold{Warn: 1, Trip: 1.5, Direction: "above"},
				Calibration: calibration, Mandatory: true, StaleMinutes: 240,
			},
			{
				PointID: "P-FLOW", Quantity: units.QuantityAirflow, AreaKind: model.AreaAirway,
				AreaID: "A-IN", Placement: model.PlacementIntake, RangeLow: 0, RangeHigh: 80,
				Threshold:   model.Threshold{Warn: 8, Trip: 5, Direction: "below"},
				Calibration: calibration, Mandatory: true, StaleMinutes: 240,
			},
		},
		Circuits: []model.Circuit{
			{
				CircuitID: "C-1", Label: "shearer", AreaKind: model.AreaWorkingFace, AreaID: "F-1",
				GuardPoints: []string{"P-CH4"}, RequiresVentilation: true,
			},
		},
	}
}

// decideWith runs the reading, gas and ventilation stages before the interlock so
// a test states its inputs as raw readings rather than intermediate structures.
func decideWith(t *testing.T, cfg config.Config, layout model.Layout, readings []model.Reading) Result {
	t.Helper()
	asOf := timeutil.MustParse(asOfText)
	normalised := reading.Normalise(cfg, layout, readings, asOf)
	gasResult := gas.Assess(cfg, layout, normalised.Samples, normalised.States, asOf)
	vent := ventilation.Assess(cfg, layout, normalised.States, asOf)
	return Decide(cfg, layout, normalised.States, gasResult, vent, asOf)
}

func sample(pointID string, at string, value float64, unit string, status model.SensorStatus) model.Reading {
	return model.Reading{
		ReadingID: pointID + "-" + at, PointID: pointID, At: timeutil.MustParse(at),
		Value: value, Unit: unit, Status: status,
	}
}

// healthy is a clear face: low methane and ample air.
func healthy() []model.Reading {
	return []model.Reading{
		sample("P-CH4", "2026-08-15T13:50:00Z", 0.40, "pct", model.StatusOK),
		sample("P-CH4", "2026-08-15T13:55:00Z", 0.41, "pct", model.StatusOK),
		sample("P-FLOW", "2026-08-15T13:50:00Z", 20, "m3/s", model.StatusOK),
		sample("P-FLOW", "2026-08-15T13:55:00Z", 20, "m3/s", model.StatusOK),
	}
}

func TestRankAndWorstOrderTheActions(t *testing.T) {
	if Rank(ActionTrip) <= Rank(ActionWarn) || Rank(ActionWarn) <= Rank(ActionAllow) {
		t.Fatal("trip is more restrictive than warn which is more restrictive than allow")
	}
	if Worst(ActionAllow, ActionTrip) != ActionTrip {
		t.Error("Worst must escalate to trip")
	}
	if Worst(ActionTrip, ActionAllow) != ActionTrip {
		t.Error("Worst must never de-escalate")
	}
}

func TestClearConditionsAllowTheCircuit(t *testing.T) {
	result := decideWith(t, config.Default(), fixture(), healthy())
	if result.TripCount != 0 || result.AllowCount != 1 {
		t.Fatalf("trips = %d, allows = %d: %s", result.TripCount, result.AllowCount,
			result.Decisions[0].Explanation)
	}
	decision := result.Decisions[0]
	if len(decision.Rules) != 1 || decision.Rules[0] != RuleClear {
		t.Errorf("rules = %v, want %s", decision.Rules, RuleClear)
	}
	if len(decision.Evidence) != 1 || decision.Evidence[0].PointID != "P-CH4" {
		t.Errorf("evidence = %+v, want the guard point", decision.Evidence)
	}
	if decision.Fingerprint == "" {
		t.Error("a decision must record the policy it was taken under")
	}
}

func TestMissingGuardReadingAlwaysTrips(t *testing.T) {
	readings := []model.Reading{
		sample("P-FLOW", "2026-08-15T13:55:00Z", 20, "m3/s", model.StatusOK),
	}
	result := decideWith(t, config.Default(), fixture(), readings)
	decision := result.Decisions[0]
	if decision.Action != ActionTrip {
		t.Fatalf("a circuit whose guard point never reported must trip, got %s", decision.Action)
	}
	if !hasRule(decision.Rules, RuleMissingInput) {
		t.Errorf("rules = %v, want %s", decision.Rules, RuleMissingInput)
	}
}

func TestUnknownGuardPointTrips(t *testing.T) {
	layout := fixture()
	layout.Circuits[0].GuardPoints = []string{"P-GHOST"}
	result := decideWith(t, config.Default(), layout, healthy())
	decision := result.Decisions[0]
	if decision.Action != ActionTrip {
		t.Fatalf("a guard point that does not exist must trip, got %s", decision.Action)
	}
	if !hasRule(decision.Rules, RuleMissingInput) {
		t.Errorf("rules = %v, want %s", decision.Rules, RuleMissingInput)
	}
}

func TestRefusedGuardReadingTrips(t *testing.T) {
	readings := healthy()
	readings[1].Status = model.StatusFaulty
	result := decideWith(t, config.Default(), fixture(), readings)
	decision := result.Decisions[0]
	if decision.Action != ActionTrip {
		t.Fatalf("a faulty mandatory head must trip, got %s: %s", decision.Action, decision.Explanation)
	}
	if !hasRule(decision.Rules, RuleInvalidInput) {
		t.Errorf("rules = %v, want %s", decision.Rules, RuleInvalidInput)
	}
}

func TestStaleGuardReadingTrips(t *testing.T) {
	layout := fixture()
	layout.Points[0].StaleMinutes = 15
	readings := []model.Reading{
		sample("P-CH4", "2026-08-15T12:00:00Z", 0.40, "pct", model.StatusOK),
		sample("P-FLOW", "2026-08-15T13:55:00Z", 20, "m3/s", model.StatusOK),
	}
	result := decideWith(t, config.Default(), layout, readings)
	if result.Decisions[0].Action != ActionTrip {
		t.Fatalf("a two hour old guard reading must trip, got %s", result.Decisions[0].Explanation)
	}
}

func TestNonMandatoryPointOnlyWarnsWhenFreshnessIsNotRequired(t *testing.T) {
	layout := fixture()
	layout.Points[0].Mandatory = false
	cfg := config.Default()
	cfg.Interlock.RequireFresh = false
	readings := healthy()
	readings[0].Status = model.StatusMaintenance
	readings[1].Status = model.StatusMaintenance
	result := decideWith(t, cfg, layout, readings)
	decision := result.Decisions[0]
	if decision.Action != ActionWarn {
		t.Fatalf("a secondary head under service must warn rather than trip, got %s", decision.Action)
	}
	if !hasRule(decision.Rules, RuleInvalidInput) {
		t.Errorf("rules = %v, want %s", decision.Rules, RuleInvalidInput)
	}
}

func TestRequireFreshOverridesTheNonMandatoryAllowance(t *testing.T) {
	layout := fixture()
	layout.Points[0].Mandatory = false
	cfg := config.Default() // RequireFresh defaults to true
	readings := healthy()
	readings[0].Status = model.StatusMaintenance
	readings[1].Status = model.StatusMaintenance
	if got := decideWith(t, cfg, layout, readings).Decisions[0].Action; got != ActionTrip {
		t.Fatalf("action = %s, want trip while the policy requires fresh inputs", got)
	}
}

func TestGasTripLevelTrips(t *testing.T) {
	readings := healthy()
	readings[0].Value = 1.4
	readings[1].Value = 1.7
	result := decideWith(t, config.Default(), fixture(), readings)
	decision := result.Decisions[0]
	if decision.Action != ActionTrip {
		t.Fatalf("1.7 pct methane must trip, got %s", decision.Action)
	}
	if !hasRule(decision.Rules, RuleGasTrip) {
		t.Errorf("rules = %v, want %s", decision.Rules, RuleGasTrip)
	}
	if got := Tripped(result.Decisions); len(got) != 1 || got[0] != "C-1" {
		t.Errorf("Tripped = %v, want [C-1]", got)
	}
}

func TestGasWarnLevelWarnsOrTripsWithThePolicy(t *testing.T) {
	readings := healthy()
	readings[0].Value = 1.1
	readings[1].Value = 1.2
	permissive := config.Default()
	if got := decideWith(t, permissive, fixture(), readings).Decisions[0].Action; got != ActionWarn {
		t.Fatalf("action = %s, want warn while warn operation is allowed", got)
	}
	strict := config.Default()
	strict.Interlock.AllowWarnOperation = false
	if got := decideWith(t, strict, fixture(), readings).Decisions[0].Action; got != ActionTrip {
		t.Fatalf("action = %s, want trip while warn operation is forbidden", got)
	}
}

func TestRateOfRiseRaisesAWarningOnItsOwn(t *testing.T) {
	readings := []model.Reading{
		sample("P-CH4", "2026-08-15T13:40:00Z", 0.10, "pct", model.StatusOK),
		sample("P-CH4", "2026-08-15T13:50:00Z", 0.35, "pct", model.StatusOK),
		sample("P-CH4", "2026-08-15T13:55:00Z", 0.60, "pct", model.StatusOK),
		sample("P-FLOW", "2026-08-15T13:50:00Z", 20, "m3/s", model.StatusOK),
		sample("P-FLOW", "2026-08-15T13:55:00Z", 20, "m3/s", model.StatusOK),
	}
	decision := decideWith(t, config.Default(), fixture(), readings).Decisions[0]
	if decision.Action != ActionWarn {
		t.Fatalf("a steep climb below the warn level must still warn, got %s", decision.Action)
	}
	if !hasRule(decision.Rules, RuleRateOfRise) {
		t.Errorf("rules = %v, want %s", decision.Rules, RuleRateOfRise)
	}
}

func TestInadequateVentilationTripsAVentilationDependentCircuit(t *testing.T) {
	readings := healthy()
	readings[2].Value = 2
	readings[3].Value = 2 // below the 3 m3/s policy floor for the face
	result := decideWith(t, config.Default(), fixture(), readings)
	decision := result.Decisions[0]
	if decision.Action != ActionTrip {
		t.Fatalf("a starved face must trip its circuit, got %s", decision.Action)
	}
	if !hasRule(decision.Rules, RuleVentilation) {
		t.Errorf("rules = %v, want %s", decision.Rules, RuleVentilation)
	}
}

func TestCircuitWithoutVentilationDependencyIgnoresAirflow(t *testing.T) {
	layout := fixture()
	layout.Circuits[0].RequiresVentilation = false
	readings := healthy()
	readings[2].Value = 2
	readings[3].Value = 2
	if got := decideWith(t, config.Default(), layout, readings).Decisions[0].Action; got != ActionAllow {
		t.Fatalf("action = %s, want allow for a circuit that does not depend on ventilation", got)
	}
}

func TestRecirculationTripsEveryVentilationDependentCircuit(t *testing.T) {
	layout := fixture()
	layout.Airways = append(layout.Airways, model.Airway{
		AirwayID: "A-LEAK", FromJunction: "J-FACE", ToJunction: "J-IN",
		LengthM: 20, AreaM2: 4, Resistance: 0.4,
	})
	layout.Points = append(layout.Points, model.Point{
		PointID: "P-LEAK", Quantity: units.QuantityAirflow, AreaKind: model.AreaAirway,
		AreaID: "A-LEAK", Placement: model.PlacementReturn, RangeLow: 0, RangeHigh: 80,
		Threshold:    model.Threshold{Warn: 0, Trip: 0, Direction: "below"},
		Calibration:  model.Calibration{At: timeutil.MustParse("2026-08-10T06:00:00Z"), ValidDays: 30},
		StaleMinutes: 240,
	})
	readings := append(healthy(),
		sample("P-LEAK", "2026-08-15T13:50:00Z", 5, "m3/s", model.StatusOK),
		sample("P-LEAK", "2026-08-15T13:55:00Z", 5, "m3/s", model.StatusOK))
	decision := decideWith(t, config.Default(), layout, readings).Decisions[0]
	if !hasRule(decision.Rules, RuleRecirculation) {
		t.Fatalf("rules = %v, want %s", decision.Rules, RuleRecirculation)
	}
	if decision.Action != ActionTrip {
		t.Errorf("action = %s, want trip", decision.Action)
	}
}

func TestDecisionsAreOrderedAndRulesDeduplicated(t *testing.T) {
	layout := fixture()
	layout.Circuits = append(layout.Circuits, model.Circuit{
		CircuitID: "C-0", Label: "conveyor", AreaKind: model.AreaAirway, AreaID: "A-IN",
		GuardPoints: []string{"P-CH4", "P-FLOW"},
	})
	readings := []model.Reading{
		sample("P-FLOW", "2026-08-15T13:55:00Z", 20, "m3/s", model.StatusOK),
	}
	result := decideWith(t, config.Default(), layout, readings)
	if result.Decisions[0].CircuitID != "C-0" {
		t.Fatalf("decisions must be ordered by circuit, got %s first", result.Decisions[0].CircuitID)
	}
	decision := result.Decisions[0]
	seen := map[Rule]int{}
	for _, rule := range decision.Rules {
		seen[rule]++
	}
	for rule, count := range seen {
		if count > 1 {
			t.Errorf("rule %s appears %d times", rule, count)
		}
	}
	if _, ok := Index(result.Decisions)["C-1"]; !ok {
		t.Error("Index must contain every circuit")
	}
	if decision.Describe() == "" {
		t.Error("Describe must render a line")
	}
}

func hasRule(rules []Rule, wanted Rule) bool {
	for _, rule := range rules {
		if rule == wanted {
			return true
		}
	}
	return false
}

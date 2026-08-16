package ventilation

import (
	"math"
	"testing"

	"MineGuard/internal/config"
	"MineGuard/internal/model"
	"MineGuard/internal/reading"
	"MineGuard/internal/timeutil"
	"MineGuard/internal/units"
)

const asOfText = "2026-08-15T14:00:00Z"

// airflowState builds the point state an airflow sensor would produce.
func airflowState(pointID, airwayID string, value float64, usable bool) reading.PointState {
	return reading.PointState{
		PointID:   pointID,
		Quantity:  units.QuantityAirflow,
		AreaKind:  model.AreaAirway,
		AreaID:    airwayID,
		HasLatest: true,
		Usable:    usable,
		Latest: reading.Sample{
			PointID: pointID,
			Value:   value,
			Unit:    "m3/s",
			Valid:   usable,
			At:      timeutil.MustParse(asOfText),
		},
	}
}

// chain is a downcast, two parallel districts and an upcast.
func chain() model.Layout {
	return model.Layout{
		SchemaVersion: model.SchemaVersion,
		MineID:        "MG-TEST",
		Junctions: []model.Junction{
			{JunctionID: "J-IN", Surface: true},
			{JunctionID: "J-OUT", Surface: true},
			{JunctionID: "J-SPLIT"},
			{JunctionID: "J-N"},
			{JunctionID: "J-S"},
			{JunctionID: "J-JOIN"},
		},
		Airways: []model.Airway{
			{AirwayID: "A-IN", FromJunction: "J-IN", ToJunction: "J-SPLIT", LengthM: 100, AreaM2: 20, Resistance: 0.002},
			{AirwayID: "A-N", FromJunction: "J-SPLIT", ToJunction: "J-N", LengthM: 500, AreaM2: 12, Resistance: 0.01},
			{AirwayID: "A-N-RET", FromJunction: "J-N", ToJunction: "J-JOIN", LengthM: 500, AreaM2: 12, Resistance: 0.01},
			{AirwayID: "A-S", FromJunction: "J-SPLIT", ToJunction: "J-S", LengthM: 500, AreaM2: 12, Resistance: 0.02},
			{AirwayID: "A-S-RET", FromJunction: "J-S", ToJunction: "J-JOIN", LengthM: 500, AreaM2: 12, Resistance: 0.02},
			{AirwayID: "A-OUT", FromJunction: "J-JOIN", ToJunction: "J-OUT", LengthM: 100, AreaM2: 20, Resistance: 0.002},
		},
		Faces: []model.Face{
			{FaceID: "F-N", IntakeJunction: "J-N", ReturnJunction: "J-JOIN", PlannedPersons: 8, EquipmentKW: 200},
			{FaceID: "F-S", IntakeJunction: "J-S", ReturnJunction: "J-JOIN", PlannedPersons: 4, EquipmentKW: 100},
		},
	}
}

func balancedStates() []reading.PointState {
	return []reading.PointState{
		airflowState("P-IN", "A-IN", 30, true),
		airflowState("P-N", "A-N", 18, true),
		airflowState("P-N-RET", "A-N-RET", 18, true),
		airflowState("P-S", "A-S", 12, true),
		airflowState("P-S-RET", "A-S-RET", 12, true),
		airflowState("P-OUT", "A-OUT", 30, true),
	}
}

func TestPressureDropFollowsTheSquareLaw(t *testing.T) {
	if got := PressureDrop(0.01, 10); math.Abs(got-1) > 1e-12 {
		t.Errorf("PressureDrop(0.01, 10) = %v, want 1", got)
	}
	if got := PressureDrop(0.01, 20); math.Abs(got-4) > 1e-12 {
		t.Errorf("doubling the quantity must quadruple the drop, got %v", got)
	}
	if got := PressureDrop(0.01, -10); math.Abs(got-1) > 1e-12 {
		t.Errorf("the drop is independent of the sign of the flow, got %v", got)
	}
}

func TestSeriesResistanceIgnoresNonPositiveBranches(t *testing.T) {
	if got := SeriesResistance([]float64{0.01, 0.02, 0.03}); math.Abs(got-0.06) > 1e-12 {
		t.Errorf("SeriesResistance = %v, want 0.06", got)
	}
	if got := SeriesResistance([]float64{0.01, 0, -1}); math.Abs(got-0.01) > 1e-12 {
		t.Errorf("a non-positive branch must be ignored, got %v", got)
	}
	if got := SeriesResistance(nil); got != 0 {
		t.Errorf("SeriesResistance(nil) = %v, want 0", got)
	}
}

func TestParallelResistanceHalvesTwoEqualBranches(t *testing.T) {
	// Two identical square-law branches in parallel present one quarter of the
	// resistance of one branch, because each carries half the quantity.
	got := ParallelResistance([]float64{0.04, 0.04})
	if math.Abs(got-0.01) > 1e-9 {
		t.Errorf("ParallelResistance = %v, want 0.01", got)
	}
	if got := ParallelResistance([]float64{0.04}); math.Abs(got-0.04) > 1e-9 {
		t.Errorf("a single branch keeps its resistance, got %v", got)
	}
	if got := ParallelResistance([]float64{0, -1}); got != 0 {
		t.Errorf("no usable branch means no resistance, got %v", got)
	}
}

func TestSqrtMatchesTheStandardLibrary(t *testing.T) {
	for _, value := range []float64{0.0001, 0.25, 1, 2, 9, 1234.5678} {
		if got := sqrt(value); math.Abs(got-math.Sqrt(value)) > 1e-9 {
			t.Errorf("sqrt(%v) = %v, want %v", value, got, math.Sqrt(value))
		}
	}
	if got := sqrt(-1); got != 0 {
		t.Errorf("sqrt of a negative value = %v, want 0", got)
	}
}

func TestAssessAcceptsABalancedNetwork(t *testing.T) {
	result := Assess(config.Default(), chain(), balancedStates(), timeutil.MustParse(asOfText))
	if !result.OK {
		t.Fatalf("a balanced network must pass: %+v", result.Issues)
	}
	if result.UnbalancedCount != 0 {
		t.Errorf("unbalanced junctions = %d, want 0", result.UnbalancedCount)
	}
	if result.InadequateFaces != 0 {
		t.Errorf("inadequate faces = %d, want 0", result.InadequateFaces)
	}
	if len(result.Recirculations) != 0 {
		t.Errorf("recirculations = %v, want none", result.Recirculations)
	}
	if math.Abs(result.TotalIntakeM3S-30) > 1e-9 {
		t.Errorf("total intake = %v, want 30", result.TotalIntakeM3S)
	}
}

func TestAssessReportsAnUnbalancedJunction(t *testing.T) {
	states := balancedStates()
	states[1] = airflowState("P-N", "A-N", 10, true) // 8 m3/s lost between J-SPLIT and J-N
	result := Assess(config.Default(), chain(), states, timeutil.MustParse(asOfText))
	if result.OK {
		t.Fatal("a network losing 8 m3/s at a junction must not pass")
	}
	byJunction := map[string]JunctionBalance{}
	for _, item := range result.Balances {
		byJunction[item.JunctionID] = item
	}
	split := byJunction["J-SPLIT"]
	if split.Balanced {
		t.Errorf("J-SPLIT must be unbalanced: %+v", split)
	}
	if math.Abs(split.ResidualM3S-8) > 1e-9 {
		t.Errorf("J-SPLIT residual = %v, want 8", split.ResidualM3S)
	}
	if byJunction["J-N"].Balanced != false {
		t.Errorf("J-N must be unbalanced too: %+v", byJunction["J-N"])
	}
}

func TestBoundaryJunctionsAreNeverUnbalanced(t *testing.T) {
	result := Assess(config.Default(), chain(), balancedStates(), timeutil.MustParse(asOfText))
	for _, balance := range result.Balances {
		if balance.JunctionID != "J-IN" && balance.JunctionID != "J-OUT" {
			continue
		}
		if !balance.Balanced {
			t.Errorf("%s is a surface junction and must be treated as a boundary", balance.JunctionID)
		}
	}
}

func TestUnmeasuredAirwayIsReportedRatherThanAssumed(t *testing.T) {
	states := balancedStates()
	states[3] = airflowState("P-S", "A-S", 12, false) // the sensor is refused
	result := Assess(config.Default(), chain(), states, timeutil.MustParse(asOfText))
	var south AirwayFlow
	for _, flow := range result.Flows {
		if flow.AirwayID == "A-S" {
			south = flow
		}
	}
	if south.Measured {
		t.Fatal("an airway with only a refused sensor is not measured")
	}
	if south.QuantityM3S != 0 {
		t.Errorf("an unmeasured airway must carry no assumed quantity, got %v", south.QuantityM3S)
	}
	if len(result.Issues) == 0 {
		t.Error("an unmeasured airway must raise an issue")
	}
	for _, face := range result.Faces {
		if face.FaceID == "F-S" && face.Adequate {
			t.Error("a face whose intake is unmeasured cannot be declared adequate")
		}
	}
}

func TestFaceRequirementTakesTheLargestClaim(t *testing.T) {
	layout := chain()
	layout.Faces[0].MinAirflowM3S = 25 // an explicit floor above the computed demand
	states := balancedStates()
	result := Assess(config.Default(), layout, states, timeutil.MustParse(asOfText))
	byFace := FaceIndex(result.Faces)
	north := byFace["F-N"]
	if math.Abs(north.RequiredM3S-25) > 1e-9 {
		t.Fatalf("F-N required = %v, want 25", north.RequiredM3S)
	}
	if north.Adequate {
		t.Errorf("18 m3/s cannot satisfy a 25 m3/s requirement: %s", north.Explanation)
	}
	south := byFace["F-S"]
	// 4 people at 0.1 plus 100 kW at 0.02 is 2.4, below the 3 m3/s policy floor.
	if math.Abs(south.RequiredM3S-3) > 1e-9 {
		t.Errorf("F-S required = %v, want the 3 m3/s floor", south.RequiredM3S)
	}
}

func TestRecirculationIsDetectedAndExplained(t *testing.T) {
	layout := chain()
	// A leaking door lets return air travel back from the join to the split.
	layout.Airways = append(layout.Airways, model.Airway{
		AirwayID: "A-LEAK", FromJunction: "J-JOIN", ToJunction: "J-SPLIT",
		LengthM: 30, AreaM2: 4, Resistance: 0.5,
	})
	states := append(balancedStates(), airflowState("P-LEAK", "A-LEAK", 4, true))
	result := Assess(config.Default(), layout, states, timeutil.MustParse(asOfText))
	if len(result.Recirculations) == 0 {
		t.Fatal("a loop carrying 4 m3/s must be reported as recirculation")
	}
	if result.OK {
		t.Error("a network with recirculation must not pass")
	}
	loop := result.Recirculations[0]
	if loop.MinimumM3S <= 0 {
		t.Errorf("the loop must carry a positive minimum quantity, got %v", loop.MinimumM3S)
	}
	if loop.Explanation == "" {
		t.Error("a recirculation finding must explain itself")
	}
}

func TestRecirculationIgnoresLoopsBelowTolerance(t *testing.T) {
	layout := chain()
	layout.Airways = append(layout.Airways, model.Airway{
		AirwayID: "A-LEAK", FromJunction: "J-JOIN", ToJunction: "J-SPLIT",
		LengthM: 30, AreaM2: 4, Resistance: 0.5,
	})
	cfg := config.Default()
	states := append(balancedStates(), airflowState("P-LEAK", "A-LEAK", cfg.Ventilation.RecirculationTolerance, true))
	result := Assess(cfg, layout, states, timeutil.MustParse(asOfText))
	if len(result.Recirculations) != 0 {
		t.Fatalf("a loop at the tolerance must not be reported: %+v", result.Recirculations)
	}
}

func TestFlowsAreOrderedByAirwayRegardlessOfInputOrder(t *testing.T) {
	states := balancedStates()
	reversed := make([]reading.PointState, 0, len(states))
	for index := len(states) - 1; index >= 0; index-- {
		reversed = append(reversed, states[index])
	}
	first := Assess(config.Default(), chain(), states, timeutil.MustParse(asOfText))
	second := Assess(config.Default(), chain(), reversed, timeutil.MustParse(asOfText))
	if len(first.Flows) != len(second.Flows) {
		t.Fatalf("flow counts differ: %d and %d", len(first.Flows), len(second.Flows))
	}
	for index := range first.Flows {
		if first.Flows[index] != second.Flows[index] {
			t.Fatalf("flow %d differs between input orders: %+v and %+v",
				index, first.Flows[index], second.Flows[index])
		}
	}
}

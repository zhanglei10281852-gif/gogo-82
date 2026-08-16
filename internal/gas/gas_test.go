package gas

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

func methanePoint(id string, placement model.Placement) model.Point {
	return model.Point{
		PointID:   id,
		Quantity:  units.QuantityMethane,
		AreaKind:  model.AreaWorkingFace,
		AreaID:    "F-1",
		Placement: placement,
		RangeLow:  0,
		RangeHigh: 5,
		Threshold: model.Threshold{Warn: 1, Trip: 1.5, Direction: "above"},
		Calibration: model.Calibration{
			At: timeutil.MustParse("2026-08-10T06:00:00Z"), ValidDays: 30,
		},
		Mandatory:    true,
		StaleMinutes: 240,
	}
}

func oxygenPoint(id string) model.Point {
	point := methanePoint(id, model.PlacementGeneralBody)
	point.Quantity = units.QuantityOxygen
	point.RangeLow = 0
	point.RangeHigh = 25
	point.Threshold = model.Threshold{Warn: 19.5, Trip: 19, Direction: "below"}
	return point
}

func carbonMonoxidePoint(id string) model.Point {
	point := methanePoint(id, model.PlacementGeneralBody)
	point.Quantity = units.QuantityCarbonMono
	point.RangeLow = 0
	point.RangeHigh = 200
	point.Threshold = model.Threshold{Warn: 15, Trip: 30, Direction: "above"}
	return point
}

// assessed runs the reading stage and then the gas stage over one series.
func assessed(t *testing.T, cfg config.Config, points []model.Point, readings []model.Reading) Result {
	t.Helper()
	layout := model.Layout{SchemaVersion: model.SchemaVersion, MineID: "MG-TEST", Points: points}
	asOf := timeutil.MustParse(asOfText)
	normalised := reading.Normalise(cfg, layout, readings, asOf)
	return Assess(cfg, layout, normalised.Samples, normalised.States, asOf)
}

func series(pointID string, unit string, stamps []string, values []float64) []model.Reading {
	out := make([]model.Reading, 0, len(stamps))
	for index, at := range stamps {
		out = append(out, model.Reading{
			ReadingID: pointID + "-" + at,
			PointID:   pointID,
			At:        timeutil.MustParse(at),
			Value:     values[index],
			Unit:      unit,
			Status:    model.StatusOK,
		})
	}
	return out
}

func TestRankAndWorstOrderTheLevels(t *testing.T) {
	if Rank(LevelTrip) <= Rank(LevelWarn) || Rank(LevelWarn) <= Rank(LevelNormal) {
		t.Fatal("trip is worse than warn which is worse than normal")
	}
	if Worst(LevelNormal, LevelWarn) != LevelWarn {
		t.Error("Worst must pick warn over normal")
	}
	if Worst(LevelTrip, LevelWarn) != LevelTrip {
		t.Error("Worst must keep trip")
	}
}

func TestLevelForRisingQuantity(t *testing.T) {
	threshold := model.Threshold{Warn: 1, Trip: 1.5, Direction: "above"}
	cases := map[float64]Level{0.9: LevelNormal, 1: LevelWarn, 1.49: LevelWarn, 1.5: LevelTrip, 3: LevelTrip}
	for value, want := range cases {
		if got := levelFor(threshold, value); got != want {
			t.Errorf("levelFor(%v) = %s, want %s", value, got, want)
		}
	}
}

func TestLevelForFallingQuantity(t *testing.T) {
	threshold := model.Threshold{Warn: 19.5, Trip: 19, Direction: "below"}
	cases := map[float64]Level{20.6: LevelNormal, 19.5: LevelWarn, 19.1: LevelWarn, 19: LevelTrip, 17: LevelTrip}
	for value, want := range cases {
		if got := levelFor(threshold, value); got != want {
			t.Errorf("levelFor(%v) = %s, want %s", value, got, want)
		}
	}
}

func TestSteadyMethaneIsNormal(t *testing.T) {
	stamps := []string{"2026-08-15T13:40:00Z", "2026-08-15T13:50:00Z", "2026-08-15T13:55:00Z"}
	result := assessed(t, config.Default(), []model.Point{methanePoint("P-1", model.PlacementGeneralBody)},
		series("P-1", "pct", stamps, []float64{0.41, 0.42, 0.42}))
	if result.Worst != LevelNormal || result.TripCount != 0 {
		t.Fatalf("worst = %s, trips = %d", result.Worst, result.TripCount)
	}
	assessment := result.Assessments[0]
	if assessment.Samples != 3 {
		t.Errorf("window samples = %d, want 3", assessment.Samples)
	}
	if math.Abs(assessment.Maximum-0.42) > 1e-9 {
		t.Errorf("window maximum = %v, want 0.42", assessment.Maximum)
	}
	if len(assessment.Flags) != 0 {
		t.Errorf("flags = %v, want none", assessment.Flags)
	}
}

func TestMethaneAboveTripLevelIsCounted(t *testing.T) {
	stamps := []string{"2026-08-15T13:40:00Z", "2026-08-15T13:50:00Z", "2026-08-15T13:55:00Z"}
	result := assessed(t, config.Default(), []model.Point{methanePoint("P-1", model.PlacementGeneralBody)},
		series("P-1", "pct", stamps, []float64{1.2, 1.4, 1.63}))
	if result.Worst != LevelTrip || result.TripCount != 1 {
		t.Fatalf("worst = %s, trips = %d", result.Worst, result.TripCount)
	}
	if !hasFlag(result.Assessments[0].Flags, FlagRateOfRise) {
		t.Errorf("a climb of 0.43 pct in 15 minutes must flag rate of rise: %v", result.Assessments[0].Flags)
	}
}

func TestRateOfRiseNeedsEnoughSamples(t *testing.T) {
	cfg := config.Default()
	stamps := []string{"2026-08-15T13:45:00Z", "2026-08-15T13:55:00Z"}
	result := assessed(t, cfg, []model.Point{methanePoint("P-1", model.PlacementGeneralBody)},
		series("P-1", "pct", stamps, []float64{0.4, 0.9}))
	assessment := result.Assessments[0]
	if assessment.RatePerHour <= cfg.Gas.RateOfRiseThreshold {
		t.Fatalf("the computed rate should be steep, got %v", assessment.RatePerHour)
	}
	if hasFlag(assessment.Flags, FlagRateOfRise) {
		t.Error("two samples are below the three sample trend threshold")
	}
}

func TestRateOfRiseIsMeasuredPerHour(t *testing.T) {
	stamps := []string{"2026-08-15T13:40:00Z", "2026-08-15T13:50:00Z", "2026-08-15T14:00:00Z"}
	result := assessed(t, config.Default(), []model.Point{methanePoint("P-1", model.PlacementGeneralBody)},
		series("P-1", "pct", stamps, []float64{0.4, 0.5, 0.6}))
	// 0.2 pct over twenty minutes is 0.6 pct per hour.
	if got := result.Assessments[0].RatePerHour; math.Abs(got-0.6) > 1e-6 {
		t.Fatalf("rate = %v, want 0.6 per hour", got)
	}
}

func TestOnlyTheRateWindowFeedsTheTrend(t *testing.T) {
	cfg := config.Default() // the rate window is the last thirty minutes
	stamps := []string{
		"2026-08-15T12:10:00Z", "2026-08-15T13:35:00Z",
		"2026-08-15T13:45:00Z", "2026-08-15T13:55:00Z",
	}
	result := assessed(t, cfg, []model.Point{methanePoint("P-1", model.PlacementGeneralBody)},
		series("P-1", "pct", stamps, []float64{0.05, 0.40, 0.41, 0.42}))
	assessment := result.Assessments[0]
	if assessment.Samples != 4 {
		t.Fatalf("window samples = %d, want 4", assessment.Samples)
	}
	if assessment.RateSamples != 3 {
		t.Fatalf("rate samples = %d, want the 3 inside the rate window", assessment.RateSamples)
	}
	if hasFlag(assessment.Flags, FlagRateOfRise) {
		t.Errorf("0.02 pct over twenty minutes is not a rate of rise: %v", assessment.RatePerHour)
	}
}

func TestSamplesOutsideTheAnalysisWindowAreIgnored(t *testing.T) {
	stamps := []string{"2026-08-15T10:00:00Z", "2026-08-15T13:55:00Z"}
	result := assessed(t, config.Default(), []model.Point{methanePoint("P-1", model.PlacementGeneralBody)},
		series("P-1", "pct", stamps, []float64{4.9, 0.42}))
	assessment := result.Assessments[0]
	if assessment.Samples != 1 {
		t.Fatalf("window samples = %d, want 1", assessment.Samples)
	}
	if math.Abs(assessment.Maximum-0.42) > 1e-9 {
		t.Errorf("a sample four hours old must not set the window maximum, got %v", assessment.Maximum)
	}
}

func TestExplosiveRangeIsFlagged(t *testing.T) {
	point := methanePoint("P-1", model.PlacementGeneralBody)
	point.RangeHigh = 20
	point.Threshold = model.Threshold{Warn: 1, Trip: 1.5, Direction: "above"}
	stamps := []string{"2026-08-15T13:50:00Z", "2026-08-15T13:55:00Z"}
	result := assessed(t, config.Default(), []model.Point{point},
		series("P-1", "pct", stamps, []float64{7.9, 8.0}))
	if !hasFlag(result.Assessments[0].Flags, FlagExplosiveRange) {
		t.Fatalf("8 pct methane sits inside the explosive range: %v", result.Assessments[0].Flags)
	}
}

func TestConcentrationAboveTheUpperLimitIsNotExplosive(t *testing.T) {
	point := methanePoint("P-1", model.PlacementGeneralBody)
	point.RangeHigh = 100
	stamps := []string{"2026-08-15T13:50:00Z", "2026-08-15T13:55:00Z"}
	result := assessed(t, config.Default(), []model.Point{point},
		series("P-1", "pct", stamps, []float64{40, 40}))
	if hasFlag(result.Assessments[0].Flags, FlagExplosiveRange) {
		t.Fatal("40 pct methane is above the upper explosive limit")
	}
}

func TestOxygenDeficiencyIsFlagged(t *testing.T) {
	stamps := []string{"2026-08-15T13:50:00Z", "2026-08-15T13:55:00Z"}
	result := assessed(t, config.Default(), []model.Point{oxygenPoint("P-O2")},
		series("P-O2", "pct", stamps, []float64{18.9, 18.5}))
	assessment := result.Assessments[0]
	if !hasFlag(assessment.Flags, FlagOxygenLow) {
		t.Fatalf("18.5 pct oxygen is deficient: %v", assessment.Flags)
	}
	if assessment.Level != LevelTrip {
		t.Errorf("level = %s, want trip for a falling threshold", assessment.Level)
	}
}

func TestCarbonMonoxideHeatingIsFlagged(t *testing.T) {
	stamps := []string{"2026-08-15T13:50:00Z", "2026-08-15T13:55:00Z"}
	result := assessed(t, config.Default(), []model.Point{carbonMonoxidePoint("P-CO")},
		series("P-CO", "ppm", stamps, []float64{22, 26}))
	if !hasFlag(result.Assessments[0].Flags, FlagCarbonMonoxide) {
		t.Fatalf("26 ppm is above the heating indicator: %v", result.Assessments[0].Flags)
	}
}

func TestLayeringIsFlaggedOnTheRoofPoint(t *testing.T) {
	stamps := []string{"2026-08-15T13:50:00Z", "2026-08-15T13:55:00Z"}
	readings := append(
		series("P-ROOF", "pct", stamps, []float64{1.10, 1.10}),
		series("P-BODY", "pct", stamps, []float64{0.40, 0.40})...)
	result := assessed(t, config.Default(), []model.Point{
		methanePoint("P-ROOF", model.PlacementRoof),
		methanePoint("P-BODY", model.PlacementGeneralBody),
	}, readings)
	byPoint := Index(result.Assessments)
	if !hasFlag(byPoint["P-ROOF"].Flags, FlagLayering) {
		t.Fatalf("0.7 pct above the general body is layering: %v", byPoint["P-ROOF"].Flags)
	}
	if hasFlag(byPoint["P-BODY"].Flags, FlagLayering) {
		t.Error("layering belongs to the roof point, not the general body")
	}
}

func TestLayeringNeedsBothPointsUsable(t *testing.T) {
	stamps := []string{"2026-08-15T13:50:00Z", "2026-08-15T13:55:00Z"}
	body := series("P-BODY", "pct", stamps, []float64{0.40, 0.40})
	for index := range body {
		body[index].Status = model.StatusFaulty
	}
	readings := append(series("P-ROOF", "pct", stamps, []float64{1.10, 1.10}), body...)
	result := assessed(t, config.Default(), []model.Point{
		methanePoint("P-ROOF", model.PlacementRoof),
		methanePoint("P-BODY", model.PlacementGeneralBody),
	}, readings)
	if hasFlag(Index(result.Assessments)["P-ROOF"].Flags, FlagLayering) {
		t.Fatal("layering cannot be inferred against a refused general body reading")
	}
}

func TestLayeringStaysBelowTheMargin(t *testing.T) {
	stamps := []string{"2026-08-15T13:50:00Z", "2026-08-15T13:55:00Z"}
	readings := append(
		series("P-ROOF", "pct", stamps, []float64{0.60, 0.60}),
		series("P-BODY", "pct", stamps, []float64{0.40, 0.40})...)
	result := assessed(t, config.Default(), []model.Point{
		methanePoint("P-ROOF", model.PlacementRoof),
		methanePoint("P-BODY", model.PlacementGeneralBody),
	}, readings)
	if hasFlag(Index(result.Assessments)["P-ROOF"].Flags, FlagLayering) {
		t.Fatal("0.2 pct is below the 0.5 pct layering margin")
	}
}

func TestPointWithoutUsableDataIsFlagged(t *testing.T) {
	result := assessed(t, config.Default(), []model.Point{methanePoint("P-1", model.PlacementGeneralBody)}, nil)
	assessment := result.Assessments[0]
	if !hasFlag(assessment.Flags, FlagNoData) {
		t.Fatalf("flags = %v, want %s", assessment.Flags, FlagNoData)
	}
	if assessment.Usable {
		t.Error("a point without data is not usable")
	}
	if assessment.Level != LevelNormal {
		t.Errorf("level = %s; the absence of data is reported as a flag, not an alarm", assessment.Level)
	}
}

func TestAssessmentsAreOrderedAndFlagsDeduplicated(t *testing.T) {
	stamps := []string{"2026-08-15T13:50:00Z", "2026-08-15T13:55:00Z"}
	readings := append(
		series("P-Z", "pct", stamps, []float64{0.40, 0.40}),
		series("P-A", "pct", stamps, []float64{0.40, 0.40})...)
	result := assessed(t, config.Default(), []model.Point{
		methanePoint("P-Z", model.PlacementGeneralBody),
		methanePoint("P-A", model.PlacementGeneralBody),
	}, readings)
	if result.Assessments[0].PointID != "P-A" {
		t.Fatalf("assessments must be ordered by point, got %s first", result.Assessments[0].PointID)
	}
	if got := InArea(result.Assessments, model.AreaWorkingFace, "F-1"); len(got) != 2 {
		t.Errorf("InArea returned %d assessments, want 2", len(got))
	}
}

func hasFlag(flags []Flag, wanted Flag) bool {
	for _, flag := range flags {
		if flag == wanted {
			return true
		}
	}
	return false
}

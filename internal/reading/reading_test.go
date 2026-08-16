package reading

import (
	"testing"

	"MineGuard/internal/config"
	"MineGuard/internal/model"
	"MineGuard/internal/timeutil"
	"MineGuard/internal/units"
)

const asOfText = "2026-08-15T14:00:00Z"

// methanePoint is a general body methane head with a valid calibration.
func methanePoint() model.Point {
	return model.Point{
		PointID:   "P-CH4",
		Label:     "face methane",
		Quantity:  units.QuantityMethane,
		AreaKind:  model.AreaWorkingFace,
		AreaID:    "F-1",
		Placement: model.PlacementGeneralBody,
		RangeLow:  0,
		RangeHigh: 5,
		Threshold: model.Threshold{Warn: 1, Trip: 1.5, Direction: "above"},
		Calibration: model.Calibration{
			At:        timeutil.MustParse("2026-08-10T06:00:00Z"),
			ValidDays: 30,
		},
		Mandatory: true,
	}
}

func layoutWith(points ...model.Point) model.Layout {
	return model.Layout{SchemaVersion: model.SchemaVersion, MineID: "MG-TEST", Points: points}
}

func reading(id string, at string, value float64, unit string, status model.SensorStatus) model.Reading {
	return model.Reading{
		ReadingID: id,
		PointID:   "P-CH4",
		At:        timeutil.MustParse(at),
		Value:     value,
		Unit:      unit,
		Status:    status,
	}
}

func TestFreshHealthyReadingIsUsable(t *testing.T) {
	result := Normalise(config.Default(), layoutWith(methanePoint()),
		[]model.Reading{reading("R-1", "2026-08-15T13:55:00Z", 0.42, "pct", model.StatusOK)},
		timeutil.MustParse(asOfText))
	if len(result.States) != 1 {
		t.Fatalf("states = %d, want 1", len(result.States))
	}
	state := result.States[0]
	if !state.Usable {
		t.Fatalf("state must be usable: %s", state.Explanation)
	}
	if state.Latest.Value != 0.42 || state.Latest.Unit != "pct" {
		t.Errorf("latest = %v %s, want 0.42 pct", state.Latest.Value, state.Latest.Unit)
	}
	if state.Latest.AgeMinutes != 5 {
		t.Errorf("age = %v minutes, want 5", state.Latest.AgeMinutes)
	}
	if len(state.Reasons) != 0 {
		t.Errorf("a usable state carries no reasons, got %v", state.Reasons)
	}
}

func TestPPMReadingIsConvertedToPercent(t *testing.T) {
	result := Normalise(config.Default(), layoutWith(methanePoint()),
		[]model.Reading{reading("R-1", "2026-08-15T13:55:00Z", 4200, "ppm", model.StatusOK)},
		timeutil.MustParse(asOfText))
	sample := result.Samples[0]
	if sample.Value != 0.42 {
		t.Fatalf("4200 ppm normalised to %v pct, want 0.42", sample.Value)
	}
	if sample.RawUnit != "ppm" || sample.Raw != 4200 {
		t.Errorf("the raw reading must be preserved, got %v %s", sample.Raw, sample.RawUnit)
	}
}

func TestFaultySensorIsRefused(t *testing.T) {
	result := Normalise(config.Default(), layoutWith(methanePoint()),
		[]model.Reading{reading("R-1", "2026-08-15T13:55:00Z", 0.42, "pct", model.StatusFaulty)},
		timeutil.MustParse(asOfText))
	state := result.States[0]
	if state.Usable {
		t.Fatal("a faulty sensor cannot inform a decision")
	}
	if !hasReason(state.Reasons, ReasonStatus) {
		t.Errorf("reasons = %v, want %s", state.Reasons, ReasonStatus)
	}
}

func TestStaleReadingIsRefused(t *testing.T) {
	result := Normalise(config.Default(), layoutWith(methanePoint()),
		[]model.Reading{reading("R-1", "2026-08-15T13:00:00Z", 0.42, "pct", model.StatusOK)},
		timeutil.MustParse(asOfText))
	state := result.States[0]
	if state.Usable {
		t.Fatal("a reading one hour old is stale under a 15 minute policy")
	}
	if !hasReason(state.Reasons, ReasonStale) {
		t.Errorf("reasons = %v, want %s", state.Reasons, ReasonStale)
	}
	if state.Latest.AgeMinutes != 60 {
		t.Errorf("age = %v, want 60", state.Latest.AgeMinutes)
	}
}

func TestPointStaleOverrideWidensTheWindow(t *testing.T) {
	point := methanePoint()
	point.StaleMinutes = 120
	result := Normalise(config.Default(), layoutWith(point),
		[]model.Reading{reading("R-1", "2026-08-15T13:00:00Z", 0.42, "pct", model.StatusOK)},
		timeutil.MustParse(asOfText))
	if !result.States[0].Usable {
		t.Fatalf("a two hour window accepts a one hour old reading: %s", result.States[0].Explanation)
	}
}

func TestOutOfRangeReadingIsRefusedBeyondTheMargin(t *testing.T) {
	cfg := config.Default() // range margin 2 percent of a 0..5 span is 0.1
	inside := Normalise(cfg, layoutWith(methanePoint()),
		[]model.Reading{reading("R-1", "2026-08-15T13:55:00Z", 5.05, "pct", model.StatusOK)},
		timeutil.MustParse(asOfText))
	if !inside.States[0].Usable {
		t.Errorf("5.05 pct sits inside the tolerated margin: %s", inside.States[0].Explanation)
	}
	outside := Normalise(cfg, layoutWith(methanePoint()),
		[]model.Reading{reading("R-2", "2026-08-15T13:55:00Z", 5.5, "pct", model.StatusOK)},
		timeutil.MustParse(asOfText))
	if outside.States[0].Usable {
		t.Fatal("5.5 pct is past the instrument span and its margin")
	}
	if !hasReason(outside.States[0].Reasons, ReasonOutOfRange) {
		t.Errorf("reasons = %v, want %s", outside.States[0].Reasons, ReasonOutOfRange)
	}
}

func TestExpiredCalibrationIsRefused(t *testing.T) {
	point := methanePoint()
	point.Calibration.At = timeutil.MustParse("2026-06-01T06:00:00Z")
	point.Calibration.ValidDays = 30
	result := Normalise(config.Default(), layoutWith(point),
		[]model.Reading{reading("R-1", "2026-08-15T13:55:00Z", 0.42, "pct", model.StatusOK)},
		timeutil.MustParse(asOfText))
	if result.States[0].Usable {
		t.Fatal("a head calibrated 75 days ago under a 30 day validity is overdue")
	}
	if !hasReason(result.States[0].Reasons, ReasonCalibration) {
		t.Errorf("reasons = %v, want %s", result.States[0].Reasons, ReasonCalibration)
	}
}

func TestExcessiveSpanDriftIsRefused(t *testing.T) {
	point := methanePoint()
	point.Calibration.SpanDriftPct = 25
	result := Normalise(config.Default(), layoutWith(point),
		[]model.Reading{reading("R-1", "2026-08-15T13:55:00Z", 0.42, "pct", model.StatusOK)},
		timeutil.MustParse(asOfText))
	if !hasReason(result.States[0].Reasons, ReasonDrift) {
		t.Errorf("reasons = %v, want %s", result.States[0].Reasons, ReasonDrift)
	}
}

func TestUnitMismatchIsRefusedWithoutInventingAValue(t *testing.T) {
	result := Normalise(config.Default(), layoutWith(methanePoint()),
		[]model.Reading{reading("R-1", "2026-08-15T13:55:00Z", 0.42, "Pa", model.StatusOK)},
		timeutil.MustParse(asOfText))
	sample := result.Samples[0]
	if sample.Valid {
		t.Fatal("a methane head cannot report pascals")
	}
	if sample.Value != 0 {
		t.Errorf("a refused conversion must not produce a value, got %v", sample.Value)
	}
	if !hasReason(sample.Reasons, ReasonUnitMismatch) {
		t.Errorf("reasons = %v, want %s", sample.Reasons, ReasonUnitMismatch)
	}
}

func TestFrozenRunIsRefused(t *testing.T) {
	cfg := config.Default() // six identical samples is a frozen head
	readings := []model.Reading{}
	stamps := []string{
		"2026-08-15T13:30:00Z", "2026-08-15T13:35:00Z", "2026-08-15T13:40:00Z",
		"2026-08-15T13:45:00Z", "2026-08-15T13:50:00Z", "2026-08-15T13:55:00Z",
	}
	for index, at := range stamps {
		readings = append(readings, reading(string(rune('A'+index)), at, 0.42, "pct", model.StatusOK))
	}
	point := methanePoint()
	point.StaleMinutes = 120
	result := Normalise(cfg, layoutWith(point), readings, timeutil.MustParse(asOfText))
	frozen := 0
	for _, sample := range result.Samples {
		if hasReason(sample.Reasons, ReasonFrozen) {
			frozen++
		}
	}
	if frozen != len(stamps) {
		t.Fatalf("frozen samples = %d, want %d", frozen, len(stamps))
	}
	if result.States[0].Usable {
		t.Error("a frozen head is not measuring and cannot be usable")
	}
}

func TestFiveIdenticalSamplesAreNotYetFrozen(t *testing.T) {
	readings := []model.Reading{}
	stamps := []string{
		"2026-08-15T13:35:00Z", "2026-08-15T13:40:00Z", "2026-08-15T13:45:00Z",
		"2026-08-15T13:50:00Z", "2026-08-15T13:55:00Z",
	}
	for index, at := range stamps {
		readings = append(readings, reading(string(rune('A'+index)), at, 0.42, "pct", model.StatusOK))
	}
	point := methanePoint()
	point.StaleMinutes = 120
	result := Normalise(config.Default(), layoutWith(point), readings, timeutil.MustParse(asOfText))
	for _, sample := range result.Samples {
		if hasReason(sample.Reasons, ReasonFrozen) {
			t.Fatalf("five samples are below the six sample threshold: %s", sample.Explanation)
		}
	}
}

func TestPointWithoutReadingsIsNotUsable(t *testing.T) {
	result := Normalise(config.Default(), layoutWith(methanePoint()), nil, timeutil.MustParse(asOfText))
	state := result.States[0]
	if state.HasLatest || state.Usable {
		t.Fatal("a point that produced no reading cannot be usable")
	}
	if state.Samples != 0 {
		t.Errorf("samples = %d, want 0", state.Samples)
	}
}

func TestReadingForUnknownPointRaisesAnIssue(t *testing.T) {
	orphan := reading("R-1", "2026-08-15T13:55:00Z", 0.42, "pct", model.StatusOK)
	orphan.PointID = "P-GHOST"
	result := Normalise(config.Default(), layoutWith(methanePoint()), []model.Reading{orphan},
		timeutil.MustParse(asOfText))
	if len(result.Issues) == 0 {
		t.Fatal("a reading for an unknown point must raise an issue")
	}
	if len(result.Samples) != 0 {
		t.Errorf("an orphan reading must not become a sample, got %d", len(result.Samples))
	}
}

func TestLatestIsChosenByInstantThenIdentifier(t *testing.T) {
	point := methanePoint()
	point.StaleMinutes = 120
	result := Normalise(config.Default(), layoutWith(point), []model.Reading{
		reading("R-2", "2026-08-15T13:55:00Z", 0.50, "pct", model.StatusOK),
		reading("R-1", "2026-08-15T13:55:00Z", 0.40, "pct", model.StatusOK),
		reading("R-0", "2026-08-15T13:30:00Z", 0.30, "pct", model.StatusOK),
	}, timeutil.MustParse(asOfText))
	if got := result.States[0].Latest.ReadingID; got != "R-2" {
		t.Fatalf("latest reading = %s, want R-2", got)
	}
}

func TestGroupingHelpersSeparateValidSamples(t *testing.T) {
	point := methanePoint()
	point.StaleMinutes = 120
	result := Normalise(config.Default(), layoutWith(point), []model.Reading{
		reading("R-1", "2026-08-15T13:50:00Z", 0.40, "pct", model.StatusOK),
		reading("R-2", "2026-08-15T13:55:00Z", 0.42, "pct", model.StatusFaulty),
	}, timeutil.MustParse(asOfText))
	all := SamplesByPoint(result.Samples)
	valid := ValidSamplesByPoint(result.Samples)
	if len(all["P-CH4"]) != 2 {
		t.Errorf("all samples = %d, want 2", len(all["P-CH4"]))
	}
	if len(valid["P-CH4"]) != 1 {
		t.Errorf("valid samples = %d, want 1", len(valid["P-CH4"]))
	}
	if _, ok := StateByID(result.States)["P-CH4"]; !ok {
		t.Error("StateByID must index the point")
	}
}

func hasReason(reasons []Reason, wanted Reason) bool {
	for _, reason := range reasons {
		if reason == wanted {
			return true
		}
	}
	return false
}

package permit

import (
	"testing"

	"MineGuard/internal/config"
	"MineGuard/internal/gas"
	"MineGuard/internal/model"
	"MineGuard/internal/reading"
	"MineGuard/internal/timeutil"
	"MineGuard/internal/units"
)

const asOfText = "2026-08-15T14:00:00Z"

func layout() model.Layout {
	return model.Layout{
		SchemaVersion: model.SchemaVersion,
		MineID:        "MG-TEST",
		Junctions:     []model.Junction{{JunctionID: "J-1"}, {JunctionID: "J-2"}},
		Faces:         []model.Face{{FaceID: "F-1", IntakeJunction: "J-1", ReturnJunction: "J-2"}},
		Points: []model.Point{{
			PointID: "P-CH4", Quantity: units.QuantityMethane, AreaKind: model.AreaWorkingFace,
			AreaID: "F-1", Placement: model.PlacementGeneralBody, RangeLow: 0, RangeHigh: 5,
			Threshold:    model.Threshold{Warn: 1, Trip: 1.5, Direction: "above"},
			Calibration:  model.Calibration{At: timeutil.MustParse("2026-08-10T06:00:00Z"), ValidDays: 30},
			Mandatory:    true,
			StaleMinutes: 240,
		}},
	}
}

func window(from, to string) timeutil.Window {
	return timeutil.Window{From: timeutil.MustParse(from), To: timeutil.MustParse(to)}
}

func hotWork(win timeutil.Window) model.Permit {
	return model.Permit{
		PermitID: "PM-1", Kind: "hot_work", AreaKind: model.AreaWorkingFace, AreaID: "F-1",
		Window: win, RequiredPoints: []string{"P-CH4"},
	}
}

// assessWith runs the reading and gas stages so a test states raw readings.
func assessWith(t *testing.T, cfg config.Config, permits []model.Permit, readings []model.Reading) Result {
	t.Helper()
	asOf := timeutil.MustParse(asOfText)
	normalised := reading.Normalise(cfg, layout(), readings, asOf)
	gasResult := gas.Assess(cfg, layout(), normalised.Samples, normalised.States, asOf)
	return Assess(cfg, permits, normalised.States, gasResult, asOf)
}

func sample(value float64, status model.SensorStatus) []model.Reading {
	return []model.Reading{{
		ReadingID: "R-1", PointID: "P-CH4", At: timeutil.MustParse("2026-08-15T13:55:00Z"),
		Value: value, Unit: "pct", Status: status,
	}}
}

func TestPermitInsideItsWindowWithCleanAirIsCleared(t *testing.T) {
	result := assessWith(t, config.Default(),
		[]model.Permit{hotWork(window("2026-08-15T08:00:00Z", "2026-08-15T15:00:00Z"))},
		sample(0.42, model.StatusOK))
	assessment := result.Assessments[0]
	if assessment.Verdict != VerdictCleared {
		t.Fatalf("verdict = %s: %s", assessment.Verdict, assessment.Explanation)
	}
	if result.ClearedCount != 1 || result.RefusedCount != 0 {
		t.Errorf("cleared = %d, refused = %d", result.ClearedCount, result.RefusedCount)
	}
	if len(assessment.Checks) != 1 || !assessment.Checks[0].Passed {
		t.Errorf("checks = %+v", assessment.Checks)
	}
	if assessment.DurationHours != 7 {
		t.Errorf("duration = %v hours, want 7", assessment.DurationHours)
	}
}

func TestPermitBeforeItsWindowIsPending(t *testing.T) {
	result := assessWith(t, config.Default(),
		[]model.Permit{hotWork(window("2026-08-15T18:00:00Z", "2026-08-15T22:00:00Z"))},
		sample(0.42, model.StatusOK))
	if got := result.Assessments[0].Verdict; got != VerdictPending {
		t.Fatalf("verdict = %s, want pending", got)
	}
	if result.RefusedCount != 0 {
		t.Errorf("a pending permit is not a refusal, refused = %d", result.RefusedCount)
	}
}

func TestPermitPastItsWindowIsExpired(t *testing.T) {
	result := assessWith(t, config.Default(),
		[]model.Permit{hotWork(window("2026-08-15T06:00:00Z", "2026-08-15T12:00:00Z"))},
		sample(0.42, model.StatusOK))
	assessment := result.Assessments[0]
	if assessment.Verdict != VerdictExpired {
		t.Fatalf("verdict = %s, want expired", assessment.Verdict)
	}
	if !hasReason(assessment.Reasons, ReasonOutsideWindow) {
		t.Errorf("reasons = %v, want %s", assessment.Reasons, ReasonOutsideWindow)
	}
	if result.RefusedCount != 1 {
		t.Errorf("an expired permit counts as refused, refused = %d", result.RefusedCount)
	}
	if got := Refused(result.Assessments); len(got) != 1 || got[0] != "PM-1" {
		t.Errorf("Refused = %v, want [PM-1]", got)
	}
}

func TestPermitLongerThanThePolicyIsRefused(t *testing.T) {
	result := assessWith(t, config.Default(),
		[]model.Permit{hotWork(window("2026-08-15T06:00:00Z", "2026-08-15T20:00:00Z"))},
		sample(0.42, model.StatusOK))
	assessment := result.Assessments[0]
	if assessment.Verdict != VerdictRefused {
		t.Fatalf("verdict = %s, want refused", assessment.Verdict)
	}
	if !hasReason(assessment.Reasons, ReasonTooLong) {
		t.Errorf("reasons = %v, want %s", assessment.Reasons, ReasonTooLong)
	}
}

func TestPermitIsRefusedWhenARequiredPointIsUnusable(t *testing.T) {
	result := assessWith(t, config.Default(),
		[]model.Permit{hotWork(window("2026-08-15T08:00:00Z", "2026-08-15T15:00:00Z"))},
		sample(0.42, model.StatusFaulty))
	assessment := result.Assessments[0]
	if assessment.Verdict != VerdictRefused {
		t.Fatalf("verdict = %s, want refused", assessment.Verdict)
	}
	if !hasReason(assessment.Reasons, ReasonPointUnusable) {
		t.Errorf("reasons = %v, want %s", assessment.Reasons, ReasonPointUnusable)
	}
	if assessment.Checks[0].Passed {
		t.Error("a refused reading cannot pass its check")
	}
}

func TestPolicyCanAcceptAnUnusablePoint(t *testing.T) {
	cfg := config.Default()
	cfg.Permits.RequireFreshPoints = false
	result := assessWith(t, cfg,
		[]model.Permit{hotWork(window("2026-08-15T08:00:00Z", "2026-08-15T15:00:00Z"))},
		sample(0.42, model.StatusMaintenance))
	if got := result.Assessments[0].Verdict; got != VerdictCleared {
		t.Fatalf("verdict = %s, want cleared while freshness is not required", got)
	}
}

func TestPermitIsRefusedWhenARequiredPointIsInAlarm(t *testing.T) {
	result := assessWith(t, config.Default(),
		[]model.Permit{hotWork(window("2026-08-15T08:00:00Z", "2026-08-15T15:00:00Z"))},
		sample(1.2, model.StatusOK))
	assessment := result.Assessments[0]
	if assessment.Verdict != VerdictRefused {
		t.Fatalf("verdict = %s, want refused", assessment.Verdict)
	}
	if !hasReason(assessment.Reasons, ReasonPointAlarm) {
		t.Errorf("reasons = %v, want %s", assessment.Reasons, ReasonPointAlarm)
	}
	if assessment.Checks[0].Level != gas.LevelWarn {
		t.Errorf("check level = %s, want warn", assessment.Checks[0].Level)
	}
}

func TestPermitIsRefusedWhenARequiredPointNeverReported(t *testing.T) {
	result := assessWith(t, config.Default(),
		[]model.Permit{hotWork(window("2026-08-15T08:00:00Z", "2026-08-15T15:00:00Z"))}, nil)
	assessment := result.Assessments[0]
	if assessment.Verdict != VerdictRefused {
		t.Fatalf("verdict = %s, want refused", assessment.Verdict)
	}
	if !hasReason(assessment.Reasons, ReasonPointMissing) {
		t.Errorf("reasons = %v, want %s", assessment.Reasons, ReasonPointMissing)
	}
}

func TestOpenWindowIsRefused(t *testing.T) {
	item := hotWork(timeutil.Window{From: timeutil.MustParse("2026-08-15T08:00:00Z")})
	result := assessWith(t, config.Default(), []model.Permit{item}, sample(0.42, model.StatusOK))
	if !hasReason(result.Assessments[0].Reasons, ReasonOutsideWindow) {
		t.Fatalf("reasons = %v, want %s", result.Assessments[0].Reasons, ReasonOutsideWindow)
	}
}

func TestAssessmentsAreOrderedAndIndexable(t *testing.T) {
	first := hotWork(window("2026-08-15T08:00:00Z", "2026-08-15T15:00:00Z"))
	second := first
	second.PermitID = "PM-0"
	result := assessWith(t, config.Default(), []model.Permit{first, second}, sample(0.42, model.StatusOK))
	if result.Assessments[0].PermitID != "PM-0" {
		t.Fatalf("assessments must be ordered by permit, got %s first", result.Assessments[0].PermitID)
	}
	if _, ok := Index(result.Assessments)["PM-1"]; !ok {
		t.Error("Index must contain every permit")
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

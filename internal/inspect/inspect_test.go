package inspect

import (
	"math"
	"testing"

	"MineGuard/internal/config"
	"MineGuard/internal/gas"
	"MineGuard/internal/interlock"
	"MineGuard/internal/model"
	"MineGuard/internal/timeutil"
)

const asOfText = "2026-08-20T12:00:00Z"

func layoutWith(inspections ...model.Inspection) model.Layout {
	return model.Layout{
		SchemaVersion: model.SchemaVersion,
		MineID:        "MG-TEST",
		Faces:         []model.Face{{FaceID: "F-1"}},
		Inspections:   inspections,
	}
}

func inspection(id, lastDone string, interval float64) model.Inspection {
	return model.Inspection{
		InspectionID:  id,
		Subject:       "roof support",
		AreaKind:      model.AreaWorkingFace,
		AreaID:        "F-1",
		IntervalHours: interval,
		LastDoneAt:    timeutil.MustParse(lastDone),
	}
}

func TestDueInstantIsTheLastCompletionPlusTheInterval(t *testing.T) {
	result := Assess(config.Default(),
		layoutWith(inspection("I-1", "2026-08-19T06:00:00Z", 168)),
		timeutil.MustParse(asOfText))
	status := result.Statuses[0]
	if status.DueAt.String() != "2026-08-26T06:00:00Z" {
		t.Fatalf("due at = %s, want the completion plus 168 hours", status.DueAt)
	}
	if status.Overdue {
		t.Errorf("an inspection due next week is not overdue: %s", status.Explanation)
	}
	if math.Abs(status.RemainingHours-138) > 1e-6 {
		t.Errorf("remaining = %v hours, want 138", status.RemainingHours)
	}
}

func TestInspectionSeveralDaysPastItsDueInstantIsOverdue(t *testing.T) {
	result := Assess(config.Default(),
		layoutWith(inspection("I-1", "2026-08-10T06:00:00Z", 24)),
		timeutil.MustParse(asOfText))
	status := result.Statuses[0]
	if !status.Overdue {
		t.Fatalf("an inspection due on 11 August is overdue on 20 August: %s", status.Explanation)
	}
	if result.OverdueCount != 1 {
		t.Errorf("overdue count = %d, want 1", result.OverdueCount)
	}
	// Due 2026-08-11T06:00:00Z, reported at 2026-08-20T12:00:00Z.
	if math.Abs(status.OverdueHours-222) > 1e-6 {
		t.Errorf("overdue = %v hours, want 222", status.OverdueHours)
	}
	if status.RemainingHours != 0 {
		t.Errorf("an overdue inspection has no remaining hours, got %v", status.RemainingHours)
	}
	if got := Overdue(result.Statuses); len(got) != 1 || got[0] != "I-1" {
		t.Errorf("Overdue = %v, want [I-1]", got)
	}
}

func TestInspectionWithoutACompletionInstantHasNoDueInstant(t *testing.T) {
	item := inspection("I-1", "2026-08-19T06:00:00Z", 24)
	item.LastDoneAt = timeutil.Stamp{}
	result := Assess(config.Default(), layoutWith(item), timeutil.MustParse(asOfText))
	status := result.Statuses[0]
	if status.DueAt.IsSet() {
		t.Fatalf("due at = %s, want unset", status.DueAt)
	}
	if status.Overdue {
		t.Error("an inspection with no history cannot be declared overdue")
	}
}

func TestStatusesAreOrderedAndIndexable(t *testing.T) {
	result := Assess(config.Default(), layoutWith(
		inspection("I-Z", "2026-08-19T06:00:00Z", 24),
		inspection("I-A", "2026-08-19T06:00:00Z", 24),
	), timeutil.MustParse(asOfText))
	if result.Statuses[0].InspectionID != "I-A" {
		t.Fatalf("statuses must be ordered, got %s first", result.Statuses[0].InspectionID)
	}
	if _, ok := Index(result.Statuses)["I-Z"]; !ok {
		t.Error("Index must contain every inspection")
	}
}

func TestTimelineKeepsOnlyTheNoteworthyEntries(t *testing.T) {
	gasResult := gas.Result{Assessments: []gas.Assessment{
		{PointID: "P-1", At: timeutil.MustParse("2026-08-20T11:00:00Z"), Level: gas.LevelTrip, Explanation: "trip"},
		{PointID: "P-2", At: timeutil.MustParse("2026-08-20T11:30:00Z"), Level: gas.LevelNormal, Explanation: "normal"},
		{PointID: "P-3", Level: gas.LevelWarn, Explanation: "no instant"},
	}}
	interlockResult := interlock.Result{Decisions: []interlock.Decision{
		{CircuitID: "C-1", At: timeutil.MustParse("2026-08-20T12:00:00Z"), Action: interlock.ActionTrip, Explanation: "trip"},
		{CircuitID: "C-2", At: timeutil.MustParse("2026-08-20T12:00:00Z"), Action: interlock.ActionAllow, Explanation: "allow"},
	}}
	statuses := []Status{
		{InspectionID: "I-1", Overdue: true, DueAt: timeutil.MustParse("2026-08-11T06:00:00Z"), Explanation: "overdue"},
		{InspectionID: "I-2", Overdue: false, DueAt: timeutil.MustParse("2026-08-26T06:00:00Z"), Explanation: "fine"},
	}
	events := Timeline(gasResult, interlockResult, statuses)
	if len(events) != 3 {
		t.Fatalf("timeline = %d events, want 3: %+v", len(events), events)
	}
	if events[0].Subject != "I-1" || events[0].Kind != "inspection_overdue" {
		t.Errorf("the oldest entry is the overdue inspection, got %+v", events[0])
	}
	if events[1].Subject != "P-1" || events[1].Kind != "gas_trip" {
		t.Errorf("second entry = %+v", events[1])
	}
	if events[2].Subject != "C-1" || events[2].Kind != "interlock_trip" {
		t.Errorf("third entry = %+v", events[2])
	}
}

func TestTimelineIsEmptyWhenNothingHappened(t *testing.T) {
	events := Timeline(gas.Result{}, interlock.Result{}, nil)
	if len(events) != 0 {
		t.Fatalf("timeline = %+v, want empty", events)
	}
}

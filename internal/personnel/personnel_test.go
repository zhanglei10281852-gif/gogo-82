package personnel

import (
	"testing"

	"MineGuard/internal/config"
	"MineGuard/internal/model"
	"MineGuard/internal/timeutil"
)

const asOfText = "2026-08-15T14:00:00Z"

func layout() model.Layout {
	return model.Layout{
		SchemaVersion: model.SchemaVersion,
		MineID:        "MG-TEST",
		Junctions:     []model.Junction{{JunctionID: "J-MAIN"}},
		Faces: []model.Face{
			{FaceID: "F-1", IntakeJunction: "J-MAIN", ReturnJunction: "J-MAIN"},
			{FaceID: "F-2", IntakeJunction: "J-MAIN", ReturnJunction: "J-MAIN"},
		},
	}
}

func roster(ids ...string) []model.Person {
	out := make([]model.Person, 0, len(ids))
	for _, id := range ids {
		out = append(out, model.Person{PersonID: id, Name: "worker " + id})
	}
	return out
}

func event(id, person, at, action string, kind model.AreaKind, areaID string) model.TagEvent {
	return model.TagEvent{
		EventID: id, PersonID: person, At: timeutil.MustParse(at),
		Action: action, AreaKind: kind, AreaID: areaID,
	}
}

func TestEntryPutsAPersonUnderground(t *testing.T) {
	result := Assess(config.Default(), layout(), roster("W-1"),
		[]model.TagEvent{event("E-1", "W-1", "2026-08-15T06:00:00Z", model.ActionEnter, model.AreaWorkingFace, "F-1")},
		timeutil.Stamp{}, timeutil.MustParse(asOfText))
	if result.Underground != 1 {
		t.Fatalf("underground = %d, want 1", result.Underground)
	}
	location := result.Locations[0]
	if location.AreaID != "F-1" || !location.Underground || location.Mustered {
		t.Fatalf("location = %+v", location)
	}
}

func TestExitBringsAPersonToTheSurface(t *testing.T) {
	result := Assess(config.Default(), layout(), roster("W-1"), []model.TagEvent{
		event("E-1", "W-1", "2026-08-15T06:00:00Z", model.ActionEnter, model.AreaWorkingFace, "F-1"),
		event("E-2", "W-1", "2026-08-15T12:00:00Z", model.ActionExit, model.AreaSurface, "LAMP-ROOM"),
	}, timeutil.Stamp{}, timeutil.MustParse(asOfText))
	if result.Underground != 0 {
		t.Fatalf("underground = %d, want 0", result.Underground)
	}
	if result.Locations[0].AreaKind != model.AreaSurface {
		t.Errorf("area kind = %s, want surface", result.Locations[0].AreaKind)
	}
}

func TestMoveRelocatesWithoutChangingTheHeadcount(t *testing.T) {
	result := Assess(config.Default(), layout(), roster("W-1"), []model.TagEvent{
		event("E-1", "W-1", "2026-08-15T06:00:00Z", model.ActionEnter, model.AreaWorkingFace, "F-1"),
		event("E-2", "W-1", "2026-08-15T09:00:00Z", model.ActionMove, model.AreaWorkingFace, "F-2"),
	}, timeutil.Stamp{}, timeutil.MustParse(asOfText))
	if result.Underground != 1 {
		t.Fatalf("underground = %d, want 1", result.Underground)
	}
	if result.Locations[0].AreaID != "F-2" {
		t.Errorf("area = %s, want F-2", result.Locations[0].AreaID)
	}
}

func TestEventsAfterTheReportingInstantAreIgnored(t *testing.T) {
	result := Assess(config.Default(), layout(), roster("W-1"), []model.TagEvent{
		event("E-1", "W-1", "2026-08-15T06:00:00Z", model.ActionEnter, model.AreaWorkingFace, "F-1"),
		event("E-2", "W-1", "2026-08-15T15:00:00Z", model.ActionExit, model.AreaSurface, "LAMP-ROOM"),
	}, timeutil.Stamp{}, timeutil.MustParse(asOfText))
	if result.Underground != 1 {
		t.Fatalf("an exit an hour after the reporting instant must not count, underground = %d",
			result.Underground)
	}
}

func TestEventForSomebodyOutsideTheRosterRaisesAnIssue(t *testing.T) {
	result := Assess(config.Default(), layout(), roster("W-1"),
		[]model.TagEvent{event("E-1", "W-9", "2026-08-15T06:00:00Z", model.ActionEnter, model.AreaWorkingFace, "F-1")},
		timeutil.Stamp{}, timeutil.MustParse(asOfText))
	if len(result.Issues) == 0 {
		t.Fatal("a tag event outside the roster must raise an issue")
	}
	if result.Underground != 0 {
		t.Errorf("underground = %d; an unrostered tag cannot be placed", result.Underground)
	}
}

func TestOccupancyFlagsAFaceOverItsLimit(t *testing.T) {
	cfg := config.Default()
	cfg.Personnel.MaxFaceOccupancy = 2
	events := []model.TagEvent{
		event("E-1", "W-1", "2026-08-15T06:00:00Z", model.ActionEnter, model.AreaWorkingFace, "F-1"),
		event("E-2", "W-2", "2026-08-15T06:00:00Z", model.ActionEnter, model.AreaWorkingFace, "F-1"),
		event("E-3", "W-3", "2026-08-15T06:00:00Z", model.ActionEnter, model.AreaWorkingFace, "F-1"),
	}
	result := Assess(cfg, layout(), roster("W-1", "W-2", "W-3"), events,
		timeutil.Stamp{}, timeutil.MustParse(asOfText))
	if len(result.Occupancy) != 1 {
		t.Fatalf("occupancy groups = %d, want 1", len(result.Occupancy))
	}
	entry := result.Occupancy[0]
	if entry.Count != 3 || !entry.OverLimit {
		t.Fatalf("occupancy = %+v", entry)
	}
	if got := InArea(result.Locations, model.AreaWorkingFace, "F-1"); len(got) != 3 {
		t.Errorf("InArea = %v, want three people", got)
	}
}

func TestWithoutAnOrderTheMusterIsComplete(t *testing.T) {
	result := Assess(config.Default(), layout(), roster("W-1"),
		[]model.TagEvent{event("E-1", "W-1", "2026-08-15T06:00:00Z", model.ActionEnter, model.AreaWorkingFace, "F-1")},
		timeutil.Stamp{}, timeutil.MustParse(asOfText))
	if !result.Muster.Complete || result.Muster.Ordered {
		t.Fatalf("muster = %+v", result.Muster)
	}
}

func TestUndergroundPersonWithoutMusterIsUnaccounted(t *testing.T) {
	result := Assess(config.Default(), layout(), roster("W-1", "W-2"), []model.TagEvent{
		event("E-1", "W-1", "2026-08-15T06:00:00Z", model.ActionEnter, model.AreaWorkingFace, "F-1"),
		event("E-2", "W-2", "2026-08-15T06:00:00Z", model.ActionEnter, model.AreaWorkingFace, "F-1"),
		event("E-3", "W-2", "2026-08-15T13:30:00Z", model.ActionMuster, model.AreaJunction, "J-MAIN"),
	}, timeutil.MustParse("2026-08-15T13:20:00Z"), timeutil.MustParse(asOfText))
	muster := result.Muster
	if muster.Complete {
		t.Fatal("a person still at the face after an order is not accounted for")
	}
	if len(muster.Unaccounted) != 1 || muster.Unaccounted[0].PersonID != "W-1" {
		t.Fatalf("unaccounted = %+v, want W-1", muster.Unaccounted)
	}
	if muster.Accounted != 1 || muster.Expected != 2 {
		t.Errorf("accounted = %d of %d, want 1 of 2", muster.Accounted, muster.Expected)
	}
	if muster.Deadline.String() != "2026-08-15T14:05:00Z" {
		t.Errorf("deadline = %s, want the order plus the 45 minute grace", muster.Deadline)
	}
}

func TestMusterBeforeTheOrderDoesNotAccountForAnybody(t *testing.T) {
	result := Assess(config.Default(), layout(), roster("W-1"), []model.TagEvent{
		event("E-1", "W-1", "2026-08-15T06:00:00Z", model.ActionEnter, model.AreaWorkingFace, "F-1"),
		event("E-2", "W-1", "2026-08-15T09:00:00Z", model.ActionMuster, model.AreaJunction, "J-MAIN"),
	}, timeutil.MustParse("2026-08-15T13:20:00Z"), timeutil.MustParse(asOfText))
	if result.Muster.Complete {
		t.Fatal("a muster recorded four hours before the order proves nothing")
	}
}

func TestReachingTheSurfaceAccountsForAPerson(t *testing.T) {
	result := Assess(config.Default(), layout(), roster("W-1"), []model.TagEvent{
		event("E-1", "W-1", "2026-08-15T06:00:00Z", model.ActionEnter, model.AreaWorkingFace, "F-1"),
		event("E-2", "W-1", "2026-08-15T13:25:00Z", model.ActionExit, model.AreaSurface, "LAMP-ROOM"),
	}, timeutil.MustParse("2026-08-15T13:20:00Z"), timeutil.MustParse(asOfText))
	if !result.Muster.Complete {
		t.Fatalf("muster = %s", result.Muster.Explanation)
	}
}

func TestNeverTaggedPersonIsUnaccounted(t *testing.T) {
	result := Assess(config.Default(), layout(), roster("W-1", "W-2"), []model.TagEvent{
		event("E-1", "W-1", "2026-08-15T13:25:00Z", model.ActionMuster, model.AreaJunction, "J-MAIN"),
	}, timeutil.MustParse("2026-08-15T13:20:00Z"), timeutil.MustParse(asOfText))
	if len(result.Muster.Unaccounted) != 1 {
		t.Fatalf("unaccounted = %+v, want the untagged person", result.Muster.Unaccounted)
	}
	entry := result.Muster.Unaccounted[0]
	if entry.PersonID != "W-2" || entry.Action != "never_tagged" {
		t.Errorf("unaccounted entry = %+v", entry)
	}
}

func TestLocationsAreOrderedAndIndexable(t *testing.T) {
	result := Assess(config.Default(), layout(), roster("W-1", "W-2"), []model.TagEvent{
		event("E-2", "W-2", "2026-08-15T06:00:00Z", model.ActionEnter, model.AreaWorkingFace, "F-1"),
		event("E-1", "W-1", "2026-08-15T06:00:00Z", model.ActionEnter, model.AreaWorkingFace, "F-2"),
	}, timeutil.Stamp{}, timeutil.MustParse(asOfText))
	if result.Locations[0].PersonID != "W-1" {
		t.Fatalf("locations must be ordered by person, got %s first", result.Locations[0].PersonID)
	}
	if _, ok := Index(result.Locations)["W-2"]; !ok {
		t.Error("Index must contain every person seen")
	}
}

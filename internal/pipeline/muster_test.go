package pipeline

import (
	"strings"
	"testing"

	"MineGuard/internal/config"
	"MineGuard/internal/model"
	"MineGuard/internal/timeutil"
)

// TestUnaccountedPersonMakesTheMineUnsafe covers the case where every technical
// condition is clear but the muster is not complete. The summary is what the
// report headline and the process exit code are derived from, so a mine with a
// rostered person nobody can locate must not come back as safe.
func TestUnaccountedPersonMakesTheMineUnsafe(t *testing.T) {
	item := bundle()
	item.Roster = append(item.Roster, model.Person{PersonID: "W-2", Name: "worker two"})
	item.Events = append(item.Events, model.TagEvent{
		EventID: "E-2", PersonID: "W-2", At: timeutil.MustParse("2026-08-15T06:05:00Z"),
		Action: model.ActionEnter, AreaKind: model.AreaWorkingFace, AreaID: "F-1",
	}, model.TagEvent{
		EventID: "E-3", PersonID: "W-1", At: timeutil.MustParse("2026-08-15T13:25:00Z"),
		Action: model.ActionMuster, AreaKind: model.AreaJunction, AreaID: "J-FACE",
	})
	if issues := model.ValidateBundle(item); !issues.OK() {
		t.Fatalf("the fixture must validate: %v", issues.Error())
	}
	assessment := Run(config.Default(), item, Options{
		AsOf:      timeutil.MustParse(asOfText),
		OrderedAt: timeutil.MustParse("2026-08-15T13:20:00Z"),
	})
	summary := assessment.Summary

	// Everything except the muster is clear, which is exactly what makes this the
	// interesting case.
	if summary.GasTripCount != 0 || summary.TrippedCircuits != 0 || !summary.VentilationOK ||
		summary.RefusedPermits != 0 || summary.OverdueInspections != 0 {
		t.Fatalf("the fixture must be technically clear: %+v", summary)
	}
	if summary.Unaccounted != 1 {
		t.Fatalf("unaccounted = %d, want 1", summary.Unaccounted)
	}
	if summary.MusterComplete {
		t.Fatal("the muster cannot be complete while a rostered person is unaccounted")
	}
	if summary.Safe {
		t.Fatalf("a mine with an unaccounted person is not safe: %q", summary.Headline)
	}
	if !strings.Contains(summary.Headline, "unaccounted") {
		t.Errorf("the headline must name the unaccounted people, got %q", summary.Headline)
	}
}

// TestCompleteMusterKeepsTheMineSafe is the matching positive case: once the
// last person reports, the same bundle is safe again.
func TestCompleteMusterKeepsTheMineSafe(t *testing.T) {
	item := bundle()
	item.Events = append(item.Events, model.TagEvent{
		EventID: "E-3", PersonID: "W-1", At: timeutil.MustParse("2026-08-15T13:25:00Z"),
		Action: model.ActionMuster, AreaKind: model.AreaJunction, AreaID: "J-FACE",
	})
	assessment := Run(config.Default(), item, Options{
		AsOf:      timeutil.MustParse(asOfText),
		OrderedAt: timeutil.MustParse("2026-08-15T13:20:00Z"),
	})
	if assessment.Summary.Unaccounted != 0 || !assessment.Summary.MusterComplete {
		t.Fatalf("muster = %+v", assessment.Personnel.Muster)
	}
	if !assessment.Summary.Safe {
		t.Fatalf("a clear mine with a complete muster is safe: %q", assessment.Summary.Headline)
	}
}

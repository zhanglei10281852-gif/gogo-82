package timeutil

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseAcceptsOnlyUTCSecondResolution(t *testing.T) {
	if _, err := Parse("2026-08-15T14:00:00Z"); err != nil {
		t.Fatalf("canonical instant must parse: %v", err)
	}
	for _, text := range []string{
		"", "2026-08-15T14:00:00+02:00", "2026-08-15T14:00:00.500Z",
		"2026-08-15 14:00:00Z", "2026-08-15", "not an instant",
	} {
		if _, err := Parse(text); err == nil {
			t.Errorf("%q must be refused", text)
		}
	}
}

func TestStampStringRoundTripsThroughJSON(t *testing.T) {
	stamp := MustParse("2026-08-15T13:55:00Z")
	data, err := json.Marshal(stamp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `"2026-08-15T13:55:00Z"` {
		t.Fatalf("encoded as %s", data)
	}
	var decoded Stamp
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !decoded.Equal(stamp) {
		t.Fatalf("round trip changed the instant: %s", decoded)
	}
}

func TestUnsetStampEncodesAsNull(t *testing.T) {
	var stamp Stamp
	data, err := json.Marshal(stamp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != "null" {
		t.Fatalf("unset stamp encoded as %s, want null", data)
	}
	var decoded Stamp
	if err := json.Unmarshal([]byte("null"), &decoded); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if decoded.IsSet() || decoded.String() != "" {
		t.Fatal("null must decode into an unset stamp")
	}
	if err := json.Unmarshal([]byte("1755000000"), &decoded); err == nil {
		t.Error("a numeric instant must be refused")
	}
}

func TestFromTimeTruncatesToSeconds(t *testing.T) {
	stamp := FromTime(time.Date(2026, 8, 15, 14, 0, 0, 999_000_000, time.UTC))
	if stamp.String() != "2026-08-15T14:00:00Z" {
		t.Fatalf("FromTime kept sub-second precision: %s", stamp)
	}
}

func TestArithmeticAndDifferences(t *testing.T) {
	base := MustParse("2026-08-15T12:00:00Z")
	if got := base.AddMinutes(90).String(); got != "2026-08-15T13:30:00Z" {
		t.Errorf("AddMinutes(90) = %s", got)
	}
	if got := base.AddHours(-3).String(); got != "2026-08-15T09:00:00Z" {
		t.Errorf("AddHours(-3) = %s", got)
	}
	later := MustParse("2026-08-15T14:30:00Z")
	if got := later.MinutesSince(base); got != 150 {
		t.Errorf("MinutesSince = %v, want 150", got)
	}
	if got := later.HoursSince(base); got != 2.5 {
		t.Errorf("HoursSince = %v, want 2.5", got)
	}
	if got := base.MinutesSince(later); got != -150 {
		t.Errorf("MinutesSince must be signed, got %v", got)
	}
	var unset Stamp
	if unset.AddMinutes(10).IsSet() {
		t.Error("shifting an unset stamp must leave it unset")
	}
}

func TestStartOfDayAndDay(t *testing.T) {
	stamp := MustParse("2026-08-15T23:59:59Z")
	if got := stamp.StartOfDay().String(); got != "2026-08-15T00:00:00Z" {
		t.Errorf("StartOfDay = %s", got)
	}
	if got := stamp.Day(); got != "2026-08-15" {
		t.Errorf("Day = %s", got)
	}
	var unset Stamp
	if unset.Day() != "" || unset.StartOfDay().IsSet() {
		t.Error("an unset stamp has no day")
	}
}

func TestWindowContainsIsHalfOpen(t *testing.T) {
	window := Window{From: MustParse("2026-08-15T08:00:00Z"), To: MustParse("2026-08-15T15:00:00Z")}
	if !window.Contains(MustParse("2026-08-15T08:00:00Z")) {
		t.Error("the lower bound is inside the window")
	}
	if window.Contains(MustParse("2026-08-15T15:00:00Z")) {
		t.Error("the upper bound is outside the window")
	}
	if window.Contains(Stamp{}) {
		t.Error("an unset instant is never inside a window")
	}
	if got := window.Minutes(); got != 420 {
		t.Errorf("Minutes = %v, want 420", got)
	}
	inverted := Window{From: window.To, To: window.From}
	if got := inverted.Minutes(); got != 0 {
		t.Errorf("an inverted window has zero length, got %v", got)
	}
	if got := (Window{}).Minutes(); got != 0 {
		t.Errorf("an open window has zero length, got %v", got)
	}
}

func TestWindowCoversRequiresBothEnds(t *testing.T) {
	outer := Window{From: MustParse("2026-08-15T08:00:00Z"), To: MustParse("2026-08-15T16:00:00Z")}
	inner := Window{From: MustParse("2026-08-15T09:00:00Z"), To: MustParse("2026-08-15T15:00:00Z")}
	if !outer.Covers(inner) {
		t.Error("the outer window covers the inner one")
	}
	if inner.Covers(outer) {
		t.Error("the inner window cannot cover the outer one")
	}
	if outer.Covers(Window{From: inner.From}) {
		t.Error("a half-open window cannot be covered")
	}
}

func TestSortAndLatest(t *testing.T) {
	stamps := []Stamp{
		MustParse("2026-08-15T13:55:00Z"),
		{},
		MustParse("2026-08-15T06:00:00Z"),
		MustParse("2026-08-15T10:15:00Z"),
	}
	Sort(stamps)
	if stamps[0].IsSet() {
		t.Error("an unset stamp must sort first")
	}
	if stamps[1].String() != "2026-08-15T06:00:00Z" || stamps[3].String() != "2026-08-15T13:55:00Z" {
		t.Fatalf("Sort produced %v", []string{stamps[1].String(), stamps[2].String(), stamps[3].String()})
	}
	latest, ok := Latest(stamps)
	if !ok || latest.String() != "2026-08-15T13:55:00Z" {
		t.Fatalf("Latest = %s (ok %t)", latest, ok)
	}
	if _, ok := Latest([]Stamp{{}, {}}); ok {
		t.Error("a slice of unset stamps has no latest instant")
	}
}

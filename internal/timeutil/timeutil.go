// Package timeutil holds the single instant type MineGuard uses.
//
// Every instant in the system is an RFC 3339 UTC timestamp with second
// resolution and no offset. Refusing offsets and fractional seconds at the
// boundary means two records that describe the same moment always carry the same
// bytes, which is what makes stored artefacts comparable between runs.
package timeutil

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Layout is the only accepted timestamp layout.
const Layout = "2006-01-02T15:04:05Z"

// Stamp is a validated UTC instant.
type Stamp struct {
	value time.Time
	set   bool
}

// Parse converts text into a Stamp.
func Parse(text string) (Stamp, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return Stamp{}, fmt.Errorf("instant is empty")
	}
	parsed, err := time.Parse(Layout, trimmed)
	if err != nil {
		return Stamp{}, fmt.Errorf("instant %q must look like %s", text, Layout)
	}
	// time.Parse accepts a fractional second even when the layout carries none,
	// so the canonical rendering is compared back against the input. Two records
	// describing the same moment must carry the same bytes.
	if parsed.UTC().Format(Layout) != trimmed {
		return Stamp{}, fmt.Errorf("instant %q must look like %s", text, Layout)
	}
	return Stamp{value: parsed.UTC(), set: true}, nil
}

// MustParse is Parse for fixtures and constants.
func MustParse(text string) Stamp {
	stamp, err := Parse(text)
	if err != nil {
		panic(err)
	}
	return stamp
}

// FromTime wraps a time.Time, truncating it to whole seconds in UTC.
func FromTime(value time.Time) Stamp {
	return Stamp{value: value.UTC().Truncate(time.Second), set: true}
}

// IsSet reports whether the stamp carries an instant.
func (s Stamp) IsSet() bool { return s.set }

// Time returns the underlying instant.
func (s Stamp) Time() time.Time { return s.value }

// String renders the canonical form, or an empty string when unset.
func (s Stamp) String() string {
	if !s.set {
		return ""
	}
	return s.value.Format(Layout)
}

// Day renders the calendar day the instant falls in.
func (s Stamp) Day() string {
	if !s.set {
		return ""
	}
	return s.value.Format("2006-01-02")
}

// StartOfDay truncates to midnight UTC.
func (s Stamp) StartOfDay() Stamp {
	if !s.set {
		return s
	}
	year, month, day := s.value.Date()
	return Stamp{value: time.Date(year, month, day, 0, 0, 0, 0, time.UTC), set: true}
}

// Before, After and Equal compare two set stamps. An unset stamp is treated as
// the zero instant, which sorts before every real reading.
func (s Stamp) Before(other Stamp) bool { return s.value.Before(other.value) }

// After reports whether s is later than other.
func (s Stamp) After(other Stamp) bool { return s.value.After(other.value) }

// Equal reports whether both stamps describe the same instant.
func (s Stamp) Equal(other Stamp) bool { return s.set == other.set && s.value.Equal(other.value) }

// Add returns the instant shifted by d, truncated to whole seconds.
func (s Stamp) Add(d time.Duration) Stamp {
	if !s.set {
		return s
	}
	return Stamp{value: s.value.Add(d).Truncate(time.Second), set: true}
}

// AddMinutes shifts the instant by whole minutes.
func (s Stamp) AddMinutes(minutes float64) Stamp {
	return s.Add(time.Duration(minutes * float64(time.Minute)))
}

// AddHours shifts the instant by whole hours.
func (s Stamp) AddHours(hours float64) Stamp {
	return s.Add(time.Duration(hours * float64(time.Hour)))
}

// MinutesSince returns the signed minutes from other to s.
func (s Stamp) MinutesSince(other Stamp) float64 {
	return s.value.Sub(other.value).Minutes()
}

// HoursSince returns the signed hours from other to s.
func (s Stamp) HoursSince(other Stamp) float64 {
	return s.value.Sub(other.value).Hours()
}

// MarshalJSON encodes the canonical form; an unset stamp encodes as null.
func (s Stamp) MarshalJSON() ([]byte, error) {
	if !s.set {
		return []byte("null"), nil
	}
	return json.Marshal(s.String())
}

// UnmarshalJSON decodes a canonical instant or null.
func (s *Stamp) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" {
		*s = Stamp{}
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("instant must be a string: %w", err)
	}
	parsed, err := Parse(text)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

// Window is a half-open instant range [From, To).
type Window struct {
	From Stamp `json:"from"`
	To   Stamp `json:"to"`
}

// Contains reports whether at falls inside the window.
func (w Window) Contains(at Stamp) bool {
	if !at.IsSet() {
		return false
	}
	if w.From.IsSet() && at.Before(w.From) {
		return false
	}
	if w.To.IsSet() && !at.Before(w.To) {
		return false
	}
	return true
}

// Covers reports whether the window fully contains the other window.
func (w Window) Covers(other Window) bool {
	if !w.From.IsSet() || !w.To.IsSet() || !other.From.IsSet() || !other.To.IsSet() {
		return false
	}
	if other.From.Before(w.From) {
		return false
	}
	return !other.To.After(w.To)
}

// Minutes returns the window length in minutes, or zero when either end is
// unset or the window is inverted.
func (w Window) Minutes() float64 {
	if !w.From.IsSet() || !w.To.IsSet() {
		return 0
	}
	minutes := w.To.MinutesSince(w.From)
	if minutes < 0 {
		return 0
	}
	return minutes
}

// String renders the window for reports.
func (w Window) String() string {
	return fmt.Sprintf("%s..%s", w.From, w.To)
}

// Sort orders stamps ascending in place.
func Sort(stamps []Stamp) {
	for i := 1; i < len(stamps); i++ {
		for j := i; j > 0 && stamps[j].Before(stamps[j-1]); j-- {
			stamps[j], stamps[j-1] = stamps[j-1], stamps[j]
		}
	}
}

// Latest returns the newest stamp in the slice.
func Latest(stamps []Stamp) (Stamp, bool) {
	best := Stamp{}
	found := false
	for _, item := range stamps {
		if !item.IsSet() {
			continue
		}
		if !found || item.After(best) {
			best = item
			found = true
		}
	}
	return best, found
}

// Package reading turns raw measurements into validated samples.
//
// A sample only informs a decision when four separate things hold: the sensor
// says it is healthy, the value sits inside the instrument range, the point's
// calibration has not expired, and the reading is recent enough. Each condition
// is recorded separately so a refusal can be explained by naming the condition
// that failed rather than a single opaque "invalid" flag.
package reading

import (
	"fmt"
	"sort"

	"MineGuard/internal/config"
	"MineGuard/internal/model"
	"MineGuard/internal/timeutil"
	"MineGuard/internal/units"
)

// Reason names a single validity condition.
type Reason string

// The refusal reasons.
const (
	ReasonStatus       Reason = "sensor_status"
	ReasonOutOfRange   Reason = "out_of_range"
	ReasonCalibration  Reason = "calibration_overdue"
	ReasonDrift        Reason = "span_drift"
	ReasonStale        Reason = "stale"
	ReasonFrozen       Reason = "frozen"
	ReasonUnitMismatch Reason = "unit_mismatch"
)

// Sample is one normalised measurement with its validity verdict.
type Sample struct {
	ReadingID   string             `json:"reading_id"`
	PointID     string             `json:"point_id"`
	Quantity    units.Quantity     `json:"quantity"`
	AreaKind    model.AreaKind     `json:"area_kind"`
	AreaID      string             `json:"area_id"`
	Placement   model.Placement    `json:"placement"`
	At          timeutil.Stamp     `json:"at"`
	Raw         float64            `json:"raw_value"`
	RawUnit     string             `json:"raw_unit"`
	Value       float64            `json:"value"`
	Unit        string             `json:"unit"`
	Status      model.SensorStatus `json:"status"`
	Valid       bool               `json:"valid"`
	Reasons     []Reason           `json:"reasons,omitempty"`
	AgeMinutes  float64            `json:"age_minutes"`
	Explanation string             `json:"explanation"`
}

// PointState is the newest picture of one monitoring point.
type PointState struct {
	PointID      string          `json:"point_id"`
	Label        string          `json:"label"`
	Quantity     units.Quantity  `json:"quantity"`
	AreaKind     model.AreaKind  `json:"area_kind"`
	AreaID       string          `json:"area_id"`
	Placement    model.Placement `json:"placement"`
	Mandatory    bool            `json:"mandatory"`
	Samples      int             `json:"samples"`
	ValidSamples int             `json:"valid_samples"`
	Latest       Sample          `json:"latest"`
	HasLatest    bool            `json:"has_latest"`
	Usable       bool            `json:"usable"`
	Reasons      []Reason        `json:"reasons,omitempty"`
	Explanation  string          `json:"explanation"`
}

// Result is the outcome of the reading stage.
type Result struct {
	AsOf    timeutil.Stamp `json:"as_of"`
	Samples []Sample       `json:"samples"`
	States  []PointState   `json:"point_states"`
	Issues  model.Issues   `json:"issues,omitempty"`
}

// Normalise validates every reading and derives the per-point state as of asOf.
func Normalise(cfg config.Config, layout model.Layout, readings []model.Reading,
	asOf timeutil.Stamp) Result {
	points := layout.PointByID()
	ordered := append([]model.Reading(nil), readings...)
	model.SortReadings(ordered)
	result := Result{AsOf: asOf}
	byPoint := make(map[string][]Sample)
	for _, raw := range ordered {
		point, ok := points[raw.PointID]
		if !ok {
			result.Issues.Add(model.SeverityError, "readings."+raw.ReadingID,
				"reading references an unknown point")
			continue
		}
		sample := evaluate(cfg, point, raw, asOf)
		byPoint[point.PointID] = append(byPoint[point.PointID], sample)
		result.Samples = append(result.Samples, sample)
	}
	for pointID := range byPoint {
		applyFrozen(cfg, byPoint[pointID])
	}
	result.Samples = result.Samples[:0]
	ids := make([]string, 0, len(byPoint))
	for pointID := range byPoint {
		ids = append(ids, pointID)
	}
	sort.Strings(ids)
	for _, pointID := range ids {
		result.Samples = append(result.Samples, byPoint[pointID]...)
	}
	SortSamples(result.Samples)
	for _, point := range layout.Points {
		result.States = append(result.States, stateFor(cfg, point, byPoint[point.PointID], asOf))
	}
	sort.SliceStable(result.States, func(a, b int) bool {
		return result.States[a].PointID < result.States[b].PointID
	})
	result.Issues = result.Issues.Sorted()
	return result
}

// evaluate turns one raw reading into a sample with its verdict.
func evaluate(cfg config.Config, point model.Point, raw model.Reading, asOf timeutil.Stamp) Sample {
	sample := Sample{
		ReadingID: raw.ReadingID,
		PointID:   point.PointID,
		Quantity:  point.Quantity,
		AreaKind:  point.AreaKind,
		AreaID:    point.AreaID,
		Placement: point.Placement,
		At:        raw.At,
		Raw:       raw.Value,
		RawUnit:   raw.Unit,
		Unit:      point.Quantity.Canonical(),
		Status:    raw.Status,
	}
	value, err := units.ToCanonical(point.Quantity, raw.Value, raw.Unit)
	if err != nil {
		sample.Reasons = append(sample.Reasons, ReasonUnitMismatch)
	} else {
		sample.Value = units.Round(value, cfg.Output.Decimals)
	}
	if !raw.Status.Usable() {
		sample.Reasons = append(sample.Reasons, ReasonStatus)
	}
	if err == nil && outOfRange(cfg, point, value) {
		sample.Reasons = append(sample.Reasons, ReasonOutOfRange)
	}
	if calibrationOverdue(point, raw.At) {
		sample.Reasons = append(sample.Reasons, ReasonCalibration)
	}
	if point.Calibration.SpanDriftPct > cfg.Readings.MaxSpanDriftPct {
		sample.Reasons = append(sample.Reasons, ReasonDrift)
	}
	if asOf.IsSet() && raw.At.IsSet() {
		sample.AgeMinutes = units.Round(asOf.MinutesSince(raw.At), 2)
		if sample.AgeMinutes > cfg.StaleMinutes(point.StaleMinutes) {
			sample.Reasons = append(sample.Reasons, ReasonStale)
		}
	}
	sample.finalise()
	return sample
}

// outOfRange applies the configured margin around the instrument range, because
// a sensor reading slightly past its span is a fault rather than a measurement.
func outOfRange(cfg config.Config, point model.Point, value float64) bool {
	span := point.RangeHigh - point.RangeLow
	margin := span * cfg.Readings.RangeMarginPct / 100
	return value < point.RangeLow-margin || value > point.RangeHigh+margin
}

// calibrationOverdue reports whether the point's calibration had expired at the
// instant the reading was taken.
func calibrationOverdue(point model.Point, at timeutil.Stamp) bool {
	if !point.Calibration.At.IsSet() || !at.IsSet() {
		return true
	}
	limit := point.Calibration.At.AddHours(float64(point.Calibration.ValidDays) * 24)
	return !at.Before(limit)
}

// applyFrozen marks a run of identical readings as frozen. A sensor that repeats
// one value for long enough is not measuring, even when it claims to be healthy.
func applyFrozen(cfg config.Config, samples []Sample) {
	need := cfg.Readings.FrozenSamples
	if need < 2 || len(samples) < need {
		return
	}
	run := 1
	for index := 1; index < len(samples); index++ {
		if difference(samples[index].Value, samples[index-1].Value) <= cfg.Readings.FrozenTolerance {
			run++
		} else {
			run = 1
		}
		if run >= need {
			for back := index - need + 1; back <= index; back++ {
				samples[back].markFrozen()
			}
		}
	}
}

// difference is the absolute gap between two values.
func difference(left, right float64) float64 {
	if left > right {
		return left - right
	}
	return right - left
}

// markFrozen adds the frozen reason once and refreshes the verdict.
func (s *Sample) markFrozen() {
	for _, reason := range s.Reasons {
		if reason == ReasonFrozen {
			return
		}
	}
	s.Reasons = append(s.Reasons, ReasonFrozen)
	s.finalise()
}

// finalise sorts the reasons and derives the validity flag and explanation.
func (s *Sample) finalise() {
	sort.SliceStable(s.Reasons, func(a, b int) bool { return s.Reasons[a] < s.Reasons[b] })
	s.Valid = len(s.Reasons) == 0
	if s.Valid {
		s.Explanation = fmt.Sprintf("%s %s %s at %s is usable",
			s.PointID, units.Format(s.Value, 3), s.Unit, s.At)
		return
	}
	names := make([]string, 0, len(s.Reasons))
	for _, reason := range s.Reasons {
		names = append(names, string(reason))
	}
	s.Explanation = fmt.Sprintf("%s at %s refused: %s", s.PointID, s.At, joinReasons(names))
}

// stateFor derives the point state from its samples.
func stateFor(cfg config.Config, point model.Point, samples []Sample, asOf timeutil.Stamp) PointState {
	state := PointState{
		PointID:   point.PointID,
		Label:     point.Label,
		Quantity:  point.Quantity,
		AreaKind:  point.AreaKind,
		AreaID:    point.AreaID,
		Placement: point.Placement,
		Mandatory: point.Mandatory,
		Samples:   len(samples),
	}
	for _, sample := range samples {
		if sample.Valid {
			state.ValidSamples++
		}
	}
	if len(samples) == 0 {
		state.Reasons = []Reason{ReasonStale}
		state.Explanation = fmt.Sprintf("%s produced no reading", point.PointID)
		return state
	}
	latest := samples[0]
	for _, sample := range samples[1:] {
		if sample.At.After(latest.At) || (sample.At.Equal(latest.At) && sample.ReadingID > latest.ReadingID) {
			latest = sample
		}
	}
	state.Latest = latest
	state.HasLatest = true
	state.Reasons = append([]Reason(nil), latest.Reasons...)
	state.Usable = latest.Valid
	if state.Usable {
		state.Explanation = fmt.Sprintf("%s latest %s %s at %s (age %s min)",
			point.PointID, units.Format(latest.Value, cfg.Output.Decimals), latest.Unit,
			latest.At, units.Format(latest.AgeMinutes, 1))
		return state
	}
	names := make([]string, 0, len(state.Reasons))
	for _, reason := range state.Reasons {
		names = append(names, string(reason))
	}
	state.Explanation = fmt.Sprintf("%s latest reading refused: %s", point.PointID, joinReasons(names))
	return state
}

// joinReasons renders reason names as a stable comma separated list.
func joinReasons(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	out := names[0]
	for _, name := range names[1:] {
		out += ", " + name
	}
	return out
}

// SortSamples orders samples by point, then instant, then identifier.
func SortSamples(samples []Sample) {
	sort.SliceStable(samples, func(a, b int) bool {
		left, right := samples[a], samples[b]
		if left.PointID != right.PointID {
			return left.PointID < right.PointID
		}
		if !left.At.Equal(right.At) {
			return left.At.Before(right.At)
		}
		return left.ReadingID < right.ReadingID
	})
}

// StateByID indexes point states.
func StateByID(states []PointState) map[string]PointState {
	out := make(map[string]PointState, len(states))
	for _, state := range states {
		out[state.PointID] = state
	}
	return out
}

// SamplesByPoint groups samples by point, preserving order.
func SamplesByPoint(samples []Sample) map[string][]Sample {
	out := make(map[string][]Sample)
	for _, sample := range samples {
		out[sample.PointID] = append(out[sample.PointID], sample)
	}
	return out
}

// ValidSamplesByPoint groups only the usable samples by point.
func ValidSamplesByPoint(samples []Sample) map[string][]Sample {
	out := make(map[string][]Sample)
	for _, sample := range samples {
		if !sample.Valid {
			continue
		}
		out[sample.PointID] = append(out[sample.PointID], sample)
	}
	return out
}

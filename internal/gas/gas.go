// Package gas assesses the atmosphere at each monitoring point.
//
// Four independent questions are answered per point: how the concentration
// compares with its alarm levels, how fast it is changing, whether the mixture
// sits in the explosive range, and whether a roof point is running far above the
// general body of air in the same area, which is the signature of layering. They
// are kept separate because they fail for different reasons and a decision needs
// to name which one fired.
package gas

import (
	"fmt"
	"sort"

	"MineGuard/internal/config"
	"MineGuard/internal/model"
	"MineGuard/internal/reading"
	"MineGuard/internal/timeutil"
	"MineGuard/internal/units"
)

// Level is the alarm level a reading reaches.
type Level string

// The alarm levels, weakest first.
const (
	LevelNormal Level = "normal"
	LevelWarn   Level = "warn"
	LevelTrip   Level = "trip"
)

// Rank orders levels so the worst of several can be taken.
func Rank(level Level) int {
	switch level {
	case LevelTrip:
		return 2
	case LevelWarn:
		return 1
	default:
		return 0
	}
}

// Worst returns the more serious of two levels.
func Worst(a, b Level) Level {
	if Rank(b) > Rank(a) {
		return b
	}
	return a
}

// Flag names a qualitative gas finding.
type Flag string

// The gas flags.
const (
	FlagRateOfRise     Flag = "rate_of_rise"
	FlagExplosiveRange Flag = "explosive_range"
	FlagLayering       Flag = "methane_layering"
	FlagOxygenLow      Flag = "oxygen_deficient"
	FlagCarbonMonoxide Flag = "carbon_monoxide_elevated"
	FlagNoData         Flag = "no_usable_data"
)

// Assessment is the gas verdict for one point.
type Assessment struct {
	PointID     string          `json:"point_id"`
	Quantity    units.Quantity  `json:"quantity"`
	AreaKind    model.AreaKind  `json:"area_kind"`
	AreaID      string          `json:"area_id"`
	Placement   model.Placement `json:"placement"`
	Usable      bool            `json:"usable"`
	Value       float64         `json:"value"`
	Unit        string          `json:"unit"`
	At          timeutil.Stamp  `json:"at"`
	Level       Level           `json:"level"`
	WarnLevel   float64         `json:"warn_level"`
	TripLevel   float64         `json:"trip_level"`
	Direction   string          `json:"direction"`
	Samples     int             `json:"window_samples"`
	Mean        float64         `json:"window_mean"`
	Maximum     float64         `json:"window_maximum"`
	RatePerHour float64         `json:"rate_pct_per_hour"`
	RateSamples int             `json:"rate_samples"`
	Flags       []Flag          `json:"flags,omitempty"`
	Explanation string          `json:"explanation"`
}

// Result is the outcome of the gas stage.
type Result struct {
	AsOf        timeutil.Stamp `json:"as_of"`
	Assessments []Assessment   `json:"assessments"`
	Worst       Level          `json:"worst_level"`
	TripCount   int            `json:"trip_count"`
	WarnCount   int            `json:"warn_count"`
	Flags       []Flag         `json:"flags,omitempty"`
}

// Assess evaluates every point in the layout.
func Assess(cfg config.Config, layout model.Layout, samples []reading.Sample,
	states []reading.PointState, asOf timeutil.Stamp) Result {
	byPoint := reading.ValidSamplesByPoint(samples)
	stateIndex := reading.StateByID(states)
	result := Result{AsOf: asOf, Worst: LevelNormal}
	flags := map[Flag]bool{}
	for _, point := range layout.Points {
		assessment := assessPoint(cfg, point, byPoint[point.PointID], stateIndex[point.PointID], asOf)
		result.Assessments = append(result.Assessments, assessment)
	}
	// Layering compares a roof point with the general body in the same area, so it
	// runs after every point has its own value.
	applyLayering(cfg, layout, result.Assessments)
	for index := range result.Assessments {
		assessment := &result.Assessments[index]
		assessment.finalise()
		result.Worst = Worst(result.Worst, assessment.Level)
		switch assessment.Level {
		case LevelTrip:
			result.TripCount++
		case LevelWarn:
			result.WarnCount++
		}
		for _, flag := range assessment.Flags {
			flags[flag] = true
		}
	}
	sort.SliceStable(result.Assessments, func(a, b int) bool {
		return result.Assessments[a].PointID < result.Assessments[b].PointID
	})
	for flag := range flags {
		result.Flags = append(result.Flags, flag)
	}
	sort.SliceStable(result.Flags, func(a, b int) bool { return result.Flags[a] < result.Flags[b] })
	return result
}

// assessPoint evaluates one point against its own thresholds and window.
func assessPoint(cfg config.Config, point model.Point, samples []reading.Sample,
	state reading.PointState, asOf timeutil.Stamp) Assessment {
	assessment := Assessment{
		PointID:   point.PointID,
		Quantity:  point.Quantity,
		AreaKind:  point.AreaKind,
		AreaID:    point.AreaID,
		Placement: point.Placement,
		Unit:      point.Quantity.Canonical(),
		WarnLevel: point.Threshold.Warn,
		TripLevel: point.Threshold.Trip,
		Direction: point.Threshold.Direction,
		Level:     LevelNormal,
	}
	window := windowSamples(cfg, samples, asOf)
	assessment.Samples = len(window)
	if len(window) == 0 {
		assessment.Flags = append(assessment.Flags, FlagNoData)
		assessment.Usable = false
		return assessment
	}
	assessment.Usable = state.Usable
	latest := window[len(window)-1]
	assessment.Value = latest.Value
	assessment.At = latest.At
	total := 0.0
	assessment.Maximum = window[0].Value
	for _, sample := range window {
		total += sample.Value
		if sample.Value > assessment.Maximum {
			assessment.Maximum = sample.Value
		}
	}
	assessment.Mean = units.Round(total/float64(len(window)), cfg.Output.Decimals)
	assessment.Level = levelFor(point.Threshold, latest.Value)
	rate, rateSamples := rateOfRise(cfg, window, asOf)
	assessment.RatePerHour = rate
	assessment.RateSamples = rateSamples
	if rateSamples >= cfg.Gas.MinSamplesForTrend && rate >= cfg.Gas.RateOfRiseThreshold {
		assessment.Flags = append(assessment.Flags, FlagRateOfRise)
	}
	switch point.Quantity {
	case units.QuantityMethane:
		if latest.Value >= cfg.Gas.LowerExplosiveLimit && latest.Value <= cfg.Gas.UpperExplosiveLimit {
			assessment.Flags = append(assessment.Flags, FlagExplosiveRange)
		}
	case units.QuantityOxygen:
		if latest.Value < cfg.Gas.OxygenDeficientPct {
			assessment.Flags = append(assessment.Flags, FlagOxygenLow)
		}
	case units.QuantityCarbonMono:
		if latest.Value >= cfg.Gas.COHeatingPpm {
			assessment.Flags = append(assessment.Flags, FlagCarbonMonoxide)
		}
	}
	return assessment
}

// windowSamples returns the samples inside the analysis window, oldest first.
func windowSamples(cfg config.Config, samples []reading.Sample, asOf timeutil.Stamp) []reading.Sample {
	if !asOf.IsSet() {
		return append([]reading.Sample(nil), samples...)
	}
	from := asOf.AddMinutes(-cfg.Gas.WindowMinutes)
	out := make([]reading.Sample, 0, len(samples))
	for _, sample := range samples {
		if sample.At.Before(from) || sample.At.After(asOf) {
			continue
		}
		out = append(out, sample)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].At.Before(out[b].At) })
	return out
}

// levelFor compares a value with its alarm pair in the configured direction.
func levelFor(threshold model.Threshold, value float64) Level {
	if threshold.Direction == "below" {
		switch {
		case value <= threshold.Trip:
			return LevelTrip
		case value <= threshold.Warn:
			return LevelWarn
		default:
			return LevelNormal
		}
	}
	switch {
	case value >= threshold.Trip:
		return LevelTrip
	case value >= threshold.Warn:
		return LevelWarn
	default:
		return LevelNormal
	}
}

// rateOfRise is the slope in units per hour across the rate window, measured
// from the oldest to the newest sample inside it.
func rateOfRise(cfg config.Config, window []reading.Sample, asOf timeutil.Stamp) (float64, int) {
	if len(window) < 2 {
		return 0, len(window)
	}
	from := asOf.AddMinutes(-cfg.Gas.RateOfRiseWindow)
	subset := make([]reading.Sample, 0, len(window))
	for _, sample := range window {
		if asOf.IsSet() && sample.At.Before(from) {
			continue
		}
		subset = append(subset, sample)
	}
	if len(subset) < 2 {
		return 0, len(subset)
	}
	first := subset[0]
	last := subset[len(subset)-1]
	hours := last.At.HoursSince(first.At)
	if hours <= 0 {
		return 0, len(subset)
	}
	return units.Round((last.Value-first.Value)/hours, 4), len(subset)
}

// applyLayering flags a roof point that runs far above the general body of air in
// the same area. Both points must be usable, because comparing against a refused
// reading would invent a finding.
func applyLayering(cfg config.Config, layout model.Layout, assessments []Assessment) {
	index := make(map[string]*Assessment, len(assessments))
	for position := range assessments {
		index[assessments[position].PointID] = &assessments[position]
	}
	for _, roof := range layout.Points {
		if roof.Quantity != units.QuantityMethane || roof.Placement != model.PlacementRoof {
			continue
		}
		roofAssessment := index[roof.PointID]
		if roofAssessment == nil || !roofAssessment.Usable || roofAssessment.Samples == 0 {
			continue
		}
		for _, body := range layout.PointsInArea(roof.AreaKind, roof.AreaID) {
			if body.PointID == roof.PointID || body.Quantity != units.QuantityMethane {
				continue
			}
			if body.Placement != model.PlacementGeneralBody {
				continue
			}
			bodyAssessment := index[body.PointID]
			if bodyAssessment == nil || !bodyAssessment.Usable || bodyAssessment.Samples == 0 {
				continue
			}
			if roofAssessment.Value-bodyAssessment.Value >= cfg.Gas.LayeringMarginPct {
				roofAssessment.Flags = append(roofAssessment.Flags, FlagLayering)
			}
		}
	}
}

// finalise sorts the flags and writes the explanation.
func (a *Assessment) finalise() {
	sort.SliceStable(a.Flags, func(i, j int) bool { return a.Flags[i] < a.Flags[j] })
	deduped := make([]Flag, 0, len(a.Flags))
	for index, flag := range a.Flags {
		if index > 0 && a.Flags[index-1] == flag {
			continue
		}
		deduped = append(deduped, flag)
	}
	a.Flags = deduped
	if a.Samples == 0 {
		a.Explanation = fmt.Sprintf("%s has no usable reading in the analysis window", a.PointID)
		return
	}
	a.Explanation = fmt.Sprintf("%s %s %s at %s is %s against warn %s and trip %s",
		a.PointID, units.Format(a.Value, 3), a.Unit, a.At, a.Level,
		units.Format(a.WarnLevel, 3), units.Format(a.TripLevel, 3))
}

// Index maps point identifiers to assessments.
func Index(assessments []Assessment) map[string]Assessment {
	out := make(map[string]Assessment, len(assessments))
	for _, assessment := range assessments {
		out[assessment.PointID] = assessment
	}
	return out
}

// InArea returns the assessments for one area, ordered by point.
func InArea(assessments []Assessment, kind model.AreaKind, areaID string) []Assessment {
	out := make([]Assessment, 0, 4)
	for _, assessment := range assessments {
		if assessment.AreaKind == kind && assessment.AreaID == areaID {
			out = append(out, assessment)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].PointID < out[b].PointID })
	return out
}

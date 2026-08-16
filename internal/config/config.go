// Package config holds the MineGuard policy document.
//
// Every knob has a documented default and a range check. A monitoring policy
// that silently accepts a nonsensical value, such as a lower explosive limit
// above the upper one, would produce decisions nobody can defend, so loading is
// strict and validation is exhaustive.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"MineGuard/internal/strictjson"
)

// Config is the whole policy document.
type Config struct {
	Label       string      `json:"label"`
	Readings    Readings    `json:"readings"`
	Gas         Gas         `json:"gas"`
	Ventilation Ventilation `json:"ventilation"`
	Interlock   Interlock   `json:"interlock"`
	Personnel   Personnel   `json:"personnel"`
	Permits     Permits     `json:"permits"`
	Output      Output      `json:"output"`
}

// Readings governs how raw measurements are accepted.
type Readings struct {
	DefaultStaleMinutes float64 `json:"default_stale_minutes"`
	FrozenSamples       int     `json:"frozen_samples"`
	FrozenTolerance     float64 `json:"frozen_tolerance"`
	MaxSpanDriftPct     float64 `json:"max_span_drift_pct"`
	RangeMarginPct      float64 `json:"range_margin_pct"`
}

// Gas governs the gas analysis stage.
type Gas struct {
	WindowMinutes       float64 `json:"window_minutes"`
	RateOfRiseWindow    float64 `json:"rate_of_rise_window_minutes"`
	RateOfRiseThreshold float64 `json:"rate_of_rise_pct_per_hour"`
	LowerExplosiveLimit float64 `json:"lower_explosive_limit_pct"`
	UpperExplosiveLimit float64 `json:"upper_explosive_limit_pct"`
	LayeringMarginPct   float64 `json:"layering_margin_pct"`
	OxygenDeficientPct  float64 `json:"oxygen_deficient_pct"`
	COHeatingPpm        float64 `json:"co_heating_ppm"`
	MinSamplesForTrend  int     `json:"min_samples_for_trend"`
}

// Ventilation governs the airflow network stage.
type Ventilation struct {
	BalanceToleranceM3S    float64 `json:"balance_tolerance_m3s"`
	AirPerPersonM3S        float64 `json:"air_per_person_m3s"`
	AirPerKilowattM3S      float64 `json:"air_per_kilowatt_m3s"`
	MinFaceAirflowM3S      float64 `json:"min_face_airflow_m3s"`
	RecirculationTolerance float64 `json:"recirculation_tolerance_m3s"`
}

// Interlock governs the trip decision stage.
type Interlock struct {
	RequireCalibration bool    `json:"require_calibration"`
	RequireFresh       bool    `json:"require_fresh"`
	TripHoldMinutes    float64 `json:"trip_hold_minutes"`
	AllowWarnOperation bool    `json:"allow_warn_operation"`
}

// Personnel governs the muster and occupancy stage.
type Personnel struct {
	MusterGraceMinutes float64 `json:"muster_grace_minutes"`
	MaxFaceOccupancy   int     `json:"max_face_occupancy"`
}

// Permits governs permit clearance.
type Permits struct {
	MaxDurationHours   float64 `json:"max_duration_hours"`
	RequireFreshPoints bool    `json:"require_fresh_points"`
}

// Output governs rendering.
type Output struct {
	Decimals int `json:"decimals"`
}

// Default returns the built-in policy.
func Default() Config {
	return Config{
		Label: "mineguard-default",
		Readings: Readings{
			DefaultStaleMinutes: 15,
			FrozenSamples:       6,
			FrozenTolerance:     0.0001,
			MaxSpanDriftPct:     10,
			RangeMarginPct:      2,
		},
		Gas: Gas{
			WindowMinutes:       120,
			RateOfRiseWindow:    30,
			RateOfRiseThreshold: 0.5,
			LowerExplosiveLimit: 5,
			UpperExplosiveLimit: 15,
			LayeringMarginPct:   0.5,
			OxygenDeficientPct:  19,
			COHeatingPpm:        24,
			MinSamplesForTrend:  3,
		},
		Ventilation: Ventilation{
			BalanceToleranceM3S:    0.5,
			AirPerPersonM3S:        0.1,
			AirPerKilowattM3S:      0.02,
			MinFaceAirflowM3S:      3,
			RecirculationTolerance: 0.2,
		},
		Interlock: Interlock{
			RequireCalibration: true,
			RequireFresh:       true,
			TripHoldMinutes:    30,
			AllowWarnOperation: true,
		},
		Personnel: Personnel{
			MusterGraceMinutes: 45,
			MaxFaceOccupancy:   12,
		},
		Permits: Permits{
			MaxDurationHours:   8,
			RequireFreshPoints: true,
		},
		Output: Output{Decimals: 3},
	}
}

// Load reads a policy document, or returns the default when path is empty.
func Load(path string) (Config, error) {
	cfg := Default()
	if strings.TrimSpace(path) == "" {
		return cfg, cfg.Validate()
	}
	if err := strictjson.DecodeFile(path, &cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate range checks every field.
func (c Config) Validate() error {
	problems := []string{}
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	if strings.TrimSpace(c.Label) == "" {
		add("label is required")
	}
	if c.Readings.DefaultStaleMinutes <= 0 || c.Readings.DefaultStaleMinutes > 1440 {
		add("readings.default_stale_minutes must sit in (0, 1440]")
	}
	if c.Readings.FrozenSamples < 2 || c.Readings.FrozenSamples > 1000 {
		add("readings.frozen_samples must sit in [2, 1000]")
	}
	if c.Readings.FrozenTolerance < 0 {
		add("readings.frozen_tolerance cannot be negative")
	}
	if c.Readings.MaxSpanDriftPct < 0 || c.Readings.MaxSpanDriftPct > 100 {
		add("readings.max_span_drift_pct must sit in [0, 100]")
	}
	if c.Readings.RangeMarginPct < 0 || c.Readings.RangeMarginPct > 50 {
		add("readings.range_margin_pct must sit in [0, 50]")
	}
	if c.Gas.WindowMinutes <= 0 || c.Gas.WindowMinutes > 10080 {
		add("gas.window_minutes must sit in (0, 10080]")
	}
	if c.Gas.RateOfRiseWindow <= 0 || c.Gas.RateOfRiseWindow > c.Gas.WindowMinutes {
		add("gas.rate_of_rise_window_minutes must sit in (0, gas.window_minutes]")
	}
	if c.Gas.RateOfRiseThreshold <= 0 {
		add("gas.rate_of_rise_pct_per_hour must be positive")
	}
	if c.Gas.LowerExplosiveLimit <= 0 {
		add("gas.lower_explosive_limit_pct must be positive")
	}
	if c.Gas.UpperExplosiveLimit <= c.Gas.LowerExplosiveLimit {
		add("gas.upper_explosive_limit_pct must exceed the lower limit")
	}
	if c.Gas.LayeringMarginPct <= 0 {
		add("gas.layering_margin_pct must be positive")
	}
	if c.Gas.OxygenDeficientPct <= 0 || c.Gas.OxygenDeficientPct >= 21 {
		add("gas.oxygen_deficient_pct must sit in (0, 21)")
	}
	if c.Gas.COHeatingPpm <= 0 {
		add("gas.co_heating_ppm must be positive")
	}
	if c.Gas.MinSamplesForTrend < 2 {
		add("gas.min_samples_for_trend must be at least 2")
	}
	if c.Ventilation.BalanceToleranceM3S <= 0 {
		add("ventilation.balance_tolerance_m3s must be positive")
	}
	if c.Ventilation.AirPerPersonM3S <= 0 {
		add("ventilation.air_per_person_m3s must be positive")
	}
	if c.Ventilation.AirPerKilowattM3S < 0 {
		add("ventilation.air_per_kilowatt_m3s cannot be negative")
	}
	if c.Ventilation.MinFaceAirflowM3S <= 0 {
		add("ventilation.min_face_airflow_m3s must be positive")
	}
	if c.Ventilation.RecirculationTolerance < 0 {
		add("ventilation.recirculation_tolerance_m3s cannot be negative")
	}
	if c.Interlock.TripHoldMinutes < 0 {
		add("interlock.trip_hold_minutes cannot be negative")
	}
	if c.Personnel.MusterGraceMinutes <= 0 {
		add("personnel.muster_grace_minutes must be positive")
	}
	if c.Personnel.MaxFaceOccupancy <= 0 {
		add("personnel.max_face_occupancy must be positive")
	}
	if c.Permits.MaxDurationHours <= 0 || c.Permits.MaxDurationHours > 72 {
		add("permits.max_duration_hours must sit in (0, 72]")
	}
	if c.Output.Decimals < 0 || c.Output.Decimals > 9 {
		add("output.decimals must sit in [0, 9]")
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("%s", strings.Join(problems, "; "))
}

// Fingerprint is the digest of the canonical encoding of the policy, used to tag
// stored artefacts so a decision can be tied to the policy that produced it.
func (c Config) Fingerprint() string {
	data, err := strictjson.Encode(c)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// StaleMinutes returns the staleness limit for a point-specific override.
func (c Config) StaleMinutes(override float64) float64 {
	if override > 0 {
		return override
	}
	return c.Readings.DefaultStaleMinutes
}

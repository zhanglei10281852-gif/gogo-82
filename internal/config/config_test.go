package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultPolicyValidates(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("the built-in policy must be valid: %v", err)
	}
}

func TestLoadWithoutAPathReturnsTheDefault(t *testing.T) {
	cfg, err := Load("  ")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Label != Default().Label {
		t.Fatalf("label = %q, want the default", cfg.Label)
	}
}

func TestLoadAppliesOnlyTheSuppliedFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	document := "{\"label\":\"strict\",\"interlock\":{\"allow_warn_operation\":false}}\n"
	if err := os.WriteFile(path, []byte(document), 0o640); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Label != "strict" || cfg.Interlock.AllowWarnOperation {
		t.Fatalf("loaded policy = %+v", cfg.Interlock)
	}
	if cfg.Gas.LowerExplosiveLimit != Default().Gas.LowerExplosiveLimit {
		t.Errorf("an untouched field must keep its default, got %v", cfg.Gas.LowerExplosiveLimit)
	}
}

func TestLoadRefusesAnUnknownMember(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, []byte("{\"label\":\"x\",\"gass\":{}}\n"), 0o640); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("a renamed section must be refused rather than ignored")
	}
}

func TestLoadRefusesAMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("a missing policy file must be refused")
	}
}

func TestValidateRejectsAnInvertedExplosiveRange(t *testing.T) {
	cfg := Default()
	cfg.Gas.LowerExplosiveLimit = 15
	cfg.Gas.UpperExplosiveLimit = 5
	err := cfg.Validate()
	if err == nil {
		t.Fatal("an upper limit below the lower one must be refused")
	}
	if !strings.Contains(err.Error(), "upper_explosive_limit_pct") {
		t.Errorf("error must name the field, got %v", err)
	}
}

func TestValidateRejectsAnEmptyLabel(t *testing.T) {
	cfg := Default()
	cfg.Label = "   "
	if err := cfg.Validate(); err == nil {
		t.Fatal("a policy without a label must be refused")
	}
}

func TestValidateChecksEveryRange(t *testing.T) {
	cases := map[string]func(Config) Config{
		"readings.default_stale_minutes":          func(c Config) Config { c.Readings.DefaultStaleMinutes = 0; return c },
		"readings.frozen_samples":                 func(c Config) Config { c.Readings.FrozenSamples = 1; return c },
		"readings.frozen_tolerance":               func(c Config) Config { c.Readings.FrozenTolerance = -1; return c },
		"readings.max_span_drift_pct":             func(c Config) Config { c.Readings.MaxSpanDriftPct = 500; return c },
		"readings.range_margin_pct":               func(c Config) Config { c.Readings.RangeMarginPct = 80; return c },
		"gas.window_minutes":                      func(c Config) Config { c.Gas.WindowMinutes = 0; return c },
		"gas.rate_of_rise_window_minutes":         func(c Config) Config { c.Gas.RateOfRiseWindow = 100000; return c },
		"gas.rate_of_rise_pct_per_hour":           func(c Config) Config { c.Gas.RateOfRiseThreshold = 0; return c },
		"gas.lower_explosive_limit_pct":           func(c Config) Config { c.Gas.LowerExplosiveLimit = 0; return c },
		"gas.layering_margin_pct":                 func(c Config) Config { c.Gas.LayeringMarginPct = 0; return c },
		"gas.oxygen_deficient_pct":                func(c Config) Config { c.Gas.OxygenDeficientPct = 21; return c },
		"gas.co_heating_ppm":                      func(c Config) Config { c.Gas.COHeatingPpm = 0; return c },
		"gas.min_samples_for_trend":               func(c Config) Config { c.Gas.MinSamplesForTrend = 1; return c },
		"ventilation.balance_tolerance_m3s":       func(c Config) Config { c.Ventilation.BalanceToleranceM3S = 0; return c },
		"ventilation.air_per_person_m3s":          func(c Config) Config { c.Ventilation.AirPerPersonM3S = 0; return c },
		"ventilation.air_per_kilowatt_m3s":        func(c Config) Config { c.Ventilation.AirPerKilowattM3S = -1; return c },
		"ventilation.min_face_airflow_m3s":        func(c Config) Config { c.Ventilation.MinFaceAirflowM3S = 0; return c },
		"ventilation.recirculation_tolerance_m3s": func(c Config) Config { c.Ventilation.RecirculationTolerance = -1; return c },
		"interlock.trip_hold_minutes":             func(c Config) Config { c.Interlock.TripHoldMinutes = -1; return c },
		"personnel.muster_grace_minutes":          func(c Config) Config { c.Personnel.MusterGraceMinutes = 0; return c },
		"personnel.max_face_occupancy":            func(c Config) Config { c.Personnel.MaxFaceOccupancy = 0; return c },
		"permits.max_duration_hours":              func(c Config) Config { c.Permits.MaxDurationHours = 100; return c },
		"output.decimals":                         func(c Config) Config { c.Output.Decimals = 12; return c },
	}
	for field, mutate := range cases {
		err := mutate(Default()).Validate()
		if err == nil {
			t.Errorf("%s accepted an out of range value", field)
			continue
		}
		if !strings.Contains(err.Error(), field) {
			t.Errorf("error for %s does not name the field: %v", field, err)
		}
	}
}

func TestFingerprintIsStableAndSensitive(t *testing.T) {
	first := Default().Fingerprint()
	if first == "" {
		t.Fatal("a valid policy must have a fingerprint")
	}
	if first != Default().Fingerprint() {
		t.Fatal("the fingerprint must be stable between calls")
	}
	changed := Default()
	changed.Gas.RateOfRiseThreshold = 0.6
	if changed.Fingerprint() == first {
		t.Error("changing a threshold must change the fingerprint")
	}
}

func TestStaleMinutesPrefersThePointOverride(t *testing.T) {
	cfg := Default()
	if got := cfg.StaleMinutes(0); got != cfg.Readings.DefaultStaleMinutes {
		t.Errorf("StaleMinutes(0) = %v, want the default", got)
	}
	if got := cfg.StaleMinutes(90); got != 90 {
		t.Errorf("StaleMinutes(90) = %v, want the override", got)
	}
	if got := cfg.StaleMinutes(-5); got != cfg.Readings.DefaultStaleMinutes {
		t.Errorf("a negative override must fall back, got %v", got)
	}
}

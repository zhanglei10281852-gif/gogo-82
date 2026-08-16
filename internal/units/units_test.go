package units

import (
	"math"
	"testing"
)

func TestToCanonicalMethaneAcceptsPercentAndPPM(t *testing.T) {
	value, err := ToCanonical(QuantityMethane, 1.25, "pct")
	if err != nil || math.Abs(value-1.25) > 1e-12 {
		t.Fatalf("1.25 pct methane = %v (err %v), want 1.25", value, err)
	}
	value, err = ToCanonical(QuantityMethane, 12500, "ppm")
	if err != nil || math.Abs(value-1.25) > 1e-12 {
		t.Fatalf("12500 ppm methane = %v (err %v), want 1.25 pct", value, err)
	}
}

func TestToCanonicalCarbonMonoxideKeepsPPM(t *testing.T) {
	value, err := ToCanonical(QuantityCarbonMono, 34, "ppm")
	if err != nil || value != 34 {
		t.Fatalf("34 ppm CO = %v (err %v), want 34", value, err)
	}
	value, err = ToCanonical(QuantityCarbonMono, 0.0034, "pct")
	if err != nil || math.Abs(value-34) > 1e-9 {
		t.Fatalf("0.0034 pct CO = %v (err %v), want 34 ppm", value, err)
	}
}

func TestToCanonicalTemperature(t *testing.T) {
	cases := []struct {
		value float64
		unit  string
		want  float64
	}{
		{32, "degF", 0},
		{212, "F", 100},
		{-40, "fahrenheit", -40},
		{273.15, "K", 0},
		{300.15, "kelvin", 27},
		{25, "C", 25},
	}
	for _, item := range cases {
		got, err := ToCanonical(QuantityTemperature, item.value, item.unit)
		if err != nil {
			t.Fatalf("ToCanonical(%v, %q): %v", item.value, item.unit, err)
		}
		if math.Abs(got-item.want) > 1e-9 {
			t.Errorf("ToCanonical(%v, %q) = %v, want %v", item.value, item.unit, got, item.want)
		}
	}
}

func TestToCanonicalPressureAndAirflow(t *testing.T) {
	got, err := ToCanonical(QuantityPressure, 10, "mmH2O")
	if err != nil || math.Abs(got-98.0665) > 1e-9 {
		t.Errorf("10 mmH2O = %v (err %v), want 98.0665 Pa", got, err)
	}
	got, err = ToCanonical(QuantityAirflow, 300, "m3/min")
	if err != nil || math.Abs(got-5) > 1e-12 {
		t.Errorf("300 m3/min = %v (err %v), want 5 m3/s", got, err)
	}
}

func TestToCanonicalRejectsCrossQuantityUnits(t *testing.T) {
	if _, err := ToCanonical(QuantityMethane, 1, "Pa"); err == nil {
		t.Error("methane cannot be reported in pascals")
	}
	if _, err := ToCanonical(QuantityAirflow, 1, "ppm"); err == nil {
		t.Error("airflow cannot be reported in ppm")
	}
	if _, err := ToCanonical(QuantityDust, 1, ""); err == nil {
		t.Error("a reading without a unit must be refused")
	}
	if _, err := ToCanonical(Quantity("unobtainium"), 1, "ppm"); err == nil {
		t.Error("an unknown quantity must be refused")
	}
}

func TestParseQuantityAcceptsSpellings(t *testing.T) {
	cases := map[string]Quantity{
		"ch4":                   QuantityMethane,
		"Methane":               QuantityMethane,
		" CO ":                  QuantityCarbonMono,
		"o2":                    QuantityOxygen,
		"dust":                  QuantityDust,
		"temp":                  QuantityTemperature,
		"velocity":              QuantityAirflow,
		"differential_pressure": QuantityPressure,
	}
	for raw, want := range cases {
		got, err := ParseQuantity(raw)
		if err != nil {
			t.Fatalf("ParseQuantity(%q): %v", raw, err)
		}
		if got != want {
			t.Errorf("ParseQuantity(%q) = %q, want %q", raw, got, want)
		}
	}
	if _, err := ParseQuantity(""); err == nil {
		t.Error("an empty quantity must be refused")
	}
	if _, err := ParseQuantity("radon"); err == nil {
		t.Error("an unsupported quantity must be refused")
	}
}

func TestEveryQuantityHasACanonicalUnit(t *testing.T) {
	for _, quantity := range Quantities() {
		if !quantity.Valid() || quantity.Canonical() == "" {
			t.Errorf("quantity %q has no canonical unit", quantity)
		}
	}
	if Quantity("nothing").Valid() {
		t.Error("an unknown quantity must not be valid")
	}
}

func TestRoundIsHalfAwayFromZeroAndHasNoNegativeZero(t *testing.T) {
	cases := []struct {
		value  float64
		digits int
		want   float64
	}{
		{0.125, 2, 0.13},
		{-0.125, 2, -0.13},
		{2.5, 0, 3},
		{-2.5, 0, -3},
		{-0.0001, 2, 0},
	}
	for _, item := range cases {
		got := Round(item.value, item.digits)
		if got != item.want {
			t.Errorf("Round(%v, %d) = %v, want %v", item.value, item.digits, got, item.want)
		}
		if got == 0 && math.Signbit(got) {
			t.Errorf("Round(%v, %d) produced negative zero", item.value, item.digits)
		}
	}
	if got := Round(math.NaN(), 2); !math.IsNaN(got) {
		t.Errorf("Round must pass NaN through, got %v", got)
	}
}

func TestFormatUsesFixedDecimals(t *testing.T) {
	if got := Format(1.5, 3); got != "1.500" {
		t.Errorf("Format(1.5, 3) = %q, want \"1.500\"", got)
	}
	if got := Format(-0.0004, 2); got != "0.00" {
		t.Errorf("Format(-0.0004, 2) = %q, want \"0.00\"", got)
	}
}

func TestSafeDivAndClamp(t *testing.T) {
	if got := SafeDiv(4, 2, -1); got != 2 {
		t.Errorf("SafeDiv(4, 2) = %v, want 2", got)
	}
	if got := SafeDiv(4, 0, -1); got != -1 {
		t.Errorf("SafeDiv by zero must fall back, got %v", got)
	}
	if got := SafeDiv(math.Inf(1), 1, -1); got != -1 {
		t.Errorf("SafeDiv must refuse a non-finite result, got %v", got)
	}
	if got := Clamp(5, 0, 3); got != 3 {
		t.Errorf("Clamp(5, 0, 3) = %v, want 3", got)
	}
	if got := Clamp(-5, 0, 3); got != 0 {
		t.Errorf("Clamp(-5, 0, 3) = %v, want 0", got)
	}
}

func TestFiniteRejectsNaNAndInfinity(t *testing.T) {
	if Finite(math.NaN()) || Finite(math.Inf(-1)) {
		t.Error("NaN and infinity are not usable readings")
	}
	if !Finite(0) {
		t.Error("zero is a usable reading")
	}
}

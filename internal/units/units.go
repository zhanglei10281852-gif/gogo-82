// Package units normalises the physical quantities used across MineGuard.
//
// Every internal computation works in a single canonical unit per quantity:
// methane and oxygen in percent by volume, carbon monoxide in parts per million,
// dust in milligrams per cubic metre, temperature in degrees Celsius, airflow in
// cubic metres per second and pressure in pascals. Conversion happens once, at
// the edge, so no downstream stage has to guess what a number means.
package units

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Quantity names a physical quantity that MineGuard understands.
type Quantity string

// The supported quantities.
const (
	QuantityMethane     Quantity = "methane"
	QuantityCarbonMono  Quantity = "carbon_monoxide"
	QuantityOxygen      Quantity = "oxygen"
	QuantityDust        Quantity = "dust"
	QuantityTemperature Quantity = "temperature"
	QuantityAirflow     Quantity = "airflow"
	QuantityPressure    Quantity = "pressure"
)

// Canonical returns the canonical unit token for a quantity.
func (q Quantity) Canonical() string {
	switch q {
	case QuantityMethane, QuantityOxygen:
		return "pct"
	case QuantityCarbonMono:
		return "ppm"
	case QuantityDust:
		return "mg/m3"
	case QuantityTemperature:
		return "C"
	case QuantityAirflow:
		return "m3/s"
	case QuantityPressure:
		return "Pa"
	default:
		return ""
	}
}

// Valid reports whether the quantity is one MineGuard supports.
func (q Quantity) Valid() bool { return q.Canonical() != "" }

// Quantities returns every supported quantity in report order.
func Quantities() []Quantity {
	return []Quantity{
		QuantityMethane, QuantityCarbonMono, QuantityOxygen, QuantityDust,
		QuantityTemperature, QuantityAirflow, QuantityPressure,
	}
}

// ParseQuantity resolves a spelling to a Quantity.
func ParseQuantity(raw string) (Quantity, error) {
	switch normalize(raw) {
	case "methane", "ch4":
		return QuantityMethane, nil
	case "carbon_monoxide", "co":
		return QuantityCarbonMono, nil
	case "oxygen", "o2":
		return QuantityOxygen, nil
	case "dust":
		return QuantityDust, nil
	case "temperature", "temp":
		return QuantityTemperature, nil
	case "airflow", "velocity", "flow":
		return QuantityAirflow, nil
	case "pressure", "differential_pressure":
		return QuantityPressure, nil
	case "":
		return "", fmt.Errorf("quantity is empty")
	default:
		return "", fmt.Errorf("unsupported quantity %q", raw)
	}
}

// normalize lowercases and trims a token.
func normalize(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// ToCanonical converts value expressed in unit into the canonical unit of q.
//
// Percent and parts per million are related by a factor of ten thousand, which
// is the only conversion where a mistaken direction produces a plausible looking
// number, so both directions are spelled out explicitly.
func ToCanonical(q Quantity, value float64, unit string) (float64, error) {
	if !q.Valid() {
		return 0, fmt.Errorf("unsupported quantity %q", string(q))
	}
	token := normalize(unit)
	if token == "" {
		return 0, fmt.Errorf("%s reading carries no unit", q)
	}
	switch q {
	case QuantityMethane, QuantityOxygen:
		switch token {
		case "pct", "%", "percent", "vol%", "%vol":
			return value, nil
		case "ppm":
			return value / 10000, nil
		}
	case QuantityCarbonMono:
		switch token {
		case "ppm":
			return value, nil
		case "pct", "%", "percent":
			return value * 10000, nil
		}
	case QuantityDust:
		switch token {
		case "mg/m3", "mgm3":
			return value, nil
		case "g/m3":
			return value * 1000, nil
		case "ug/m3":
			return value / 1000, nil
		}
	case QuantityTemperature:
		switch token {
		case "c", "degc", "celsius":
			return value, nil
		case "k", "kelvin":
			return value - 273.15, nil
		case "f", "degf", "fahrenheit":
			return (value - 32) * 5 / 9, nil
		}
	case QuantityAirflow:
		switch token {
		case "m3/s", "m3s":
			return value, nil
		case "m3/min":
			return value / 60, nil
		}
	case QuantityPressure:
		switch token {
		case "pa":
			return value, nil
		case "kpa":
			return value * 1000, nil
		case "mmh2o", "mmwg":
			return value * 9.80665, nil
		}
	}
	return 0, fmt.Errorf("cannot convert %s from %q to %s", q, unit, q.Canonical())
}

// Finite reports whether value is a usable real number.
func Finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// Round rounds value to digits decimals, half away from zero, and never returns
// a negative zero so that text output is stable.
func Round(value float64, digits int) float64 {
	if !Finite(value) {
		return value
	}
	if digits < 0 {
		digits = 0
	}
	if digits > 9 {
		digits = 9
	}
	scale := math.Pow(10, float64(digits))
	scaled := value * scale
	rounded := math.Floor(math.Abs(scaled) + 0.5)
	if scaled < 0 {
		rounded = -rounded
	}
	result := rounded / scale
	if result == 0 {
		return 0
	}
	return result
}

// Format renders value with a fixed number of decimals.
func Format(value float64, digits int) string {
	return strconv.FormatFloat(Round(value, digits), 'f', digits, 64)
}

// Clamp constrains value to the inclusive range low..high.
func Clamp(value, low, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

// SafeDiv divides numerator by denominator, returning fallback when the
// denominator is zero or the result would not be finite.
func SafeDiv(numerator, denominator, fallback float64) float64 {
	if denominator == 0 {
		return fallback
	}
	result := numerator / denominator
	if !Finite(result) {
		return fallback
	}
	return result
}

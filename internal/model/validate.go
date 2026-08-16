package model

import (
	"fmt"
	"strings"

	"MineGuard/internal/units"
)

// ValidateLayout checks a layout for internal consistency. It is deliberately
// pedantic: a monitoring point that references a face which does not exist is an
// error rather than a warning, because a guard point nobody can resolve is a
// guard point that will never trip.
func ValidateLayout(layout Layout) Issues {
	issues := Issues{}
	if layout.SchemaVersion != SchemaVersion {
		issues.Add(SeverityError, "schema_version",
			fmt.Sprintf("layout declares %q, want %q", layout.SchemaVersion, SchemaVersion))
	}
	if strings.TrimSpace(layout.MineID) == "" {
		issues.Add(SeverityError, "mine_id", "mine identifier is required")
	}
	districts := map[string]bool{}
	for index, district := range layout.Districts {
		path := fmt.Sprintf("districts[%d]", index)
		if strings.TrimSpace(district.DistrictID) == "" {
			issues.Add(SeverityError, path+".district_id", "district identifier is required")
			continue
		}
		if districts[district.DistrictID] {
			issues.Add(SeverityError, path+".district_id", "district identifier is not unique")
		}
		districts[district.DistrictID] = true
	}
	junctions := map[string]bool{}
	for index, junction := range layout.Junctions {
		path := fmt.Sprintf("junctions[%d]", index)
		if strings.TrimSpace(junction.JunctionID) == "" {
			issues.Add(SeverityError, path+".junction_id", "junction identifier is required")
			continue
		}
		if junctions[junction.JunctionID] {
			issues.Add(SeverityError, path+".junction_id", "junction identifier is not unique")
		}
		junctions[junction.JunctionID] = true
	}
	faces := map[string]bool{}
	for index, face := range layout.Faces {
		path := fmt.Sprintf("faces[%d]", index)
		if strings.TrimSpace(face.FaceID) == "" {
			issues.Add(SeverityError, path+".face_id", "face identifier is required")
			continue
		}
		if faces[face.FaceID] {
			issues.Add(SeverityError, path+".face_id", "face identifier is not unique")
		}
		faces[face.FaceID] = true
		if face.DistrictID != "" && !districts[face.DistrictID] {
			issues.Add(SeverityError, path+".district_id", "face references an unknown district")
		}
		if !junctions[face.IntakeJunction] {
			issues.Add(SeverityError, path+".intake_junction", "face references an unknown junction")
		}
		if !junctions[face.ReturnJunction] {
			issues.Add(SeverityError, path+".return_junction", "face references an unknown junction")
		}
		if face.IntakeJunction != "" && face.IntakeJunction == face.ReturnJunction {
			issues.Add(SeverityError, path+".return_junction", "intake and return junction are the same")
		}
		if face.PlannedPersons < 0 {
			issues.Add(SeverityError, path+".planned_persons", "planned persons cannot be negative")
		}
		if face.EquipmentKW < 0 {
			issues.Add(SeverityError, path+".equipment_kw", "equipment load cannot be negative")
		}
		if face.MinAirflowM3S < 0 {
			issues.Add(SeverityError, path+".min_airflow_m3s", "minimum airflow cannot be negative")
		}
	}
	airways := map[string]bool{}
	for index, airway := range layout.Airways {
		path := fmt.Sprintf("airways[%d]", index)
		if strings.TrimSpace(airway.AirwayID) == "" {
			issues.Add(SeverityError, path+".airway_id", "airway identifier is required")
			continue
		}
		if airways[airway.AirwayID] {
			issues.Add(SeverityError, path+".airway_id", "airway identifier is not unique")
		}
		airways[airway.AirwayID] = true
		if !junctions[airway.FromJunction] {
			issues.Add(SeverityError, path+".from_junction", "airway starts at an unknown junction")
		}
		if !junctions[airway.ToJunction] {
			issues.Add(SeverityError, path+".to_junction", "airway ends at an unknown junction")
		}
		if airway.FromJunction != "" && airway.FromJunction == airway.ToJunction {
			issues.Add(SeverityError, path+".to_junction", "airway starts and ends at the same junction")
		}
		if airway.LengthM <= 0 {
			issues.Add(SeverityError, path+".length_m", "airway length must be positive")
		}
		if airway.AreaM2 <= 0 {
			issues.Add(SeverityError, path+".area_m2", "airway cross-section must be positive")
		}
		if airway.Resistance <= 0 {
			issues.Add(SeverityError, path+".resistance_ns2m8", "airway resistance must be positive")
		}
		if airway.FanPressurePa < 0 {
			issues.Add(SeverityError, path+".fan_pressure_pa", "fan pressure cannot be negative")
		}
	}
	points := map[string]bool{}
	for index, point := range layout.Points {
		path := fmt.Sprintf("points[%d]", index)
		if strings.TrimSpace(point.PointID) == "" {
			issues.Add(SeverityError, path+".point_id", "point identifier is required")
			continue
		}
		if points[point.PointID] {
			issues.Add(SeverityError, path+".point_id", "point identifier is not unique")
		}
		points[point.PointID] = true
		if !point.Quantity.Valid() {
			issues.Add(SeverityError, path+".quantity",
				fmt.Sprintf("unsupported quantity %q", string(point.Quantity)))
		}
		if !ValidAreaKind(point.AreaKind) {
			issues.Add(SeverityError, path+".area_kind", "unsupported area kind")
		}
		if !ValidPlacement(point.Placement) {
			issues.Add(SeverityError, path+".placement", "unsupported placement")
		}
		issues = append(issues, validateAreaReference(path, point.AreaKind, point.AreaID, faces, airways, junctions)...)
		if point.RangeHigh <= point.RangeLow {
			issues.Add(SeverityError, path+".range_high", "measurement range must be increasing")
		}
		issues = append(issues, validateThreshold(path+".threshold", point)...)
		if point.Calibration.ValidDays <= 0 {
			issues.Add(SeverityError, path+".calibration.valid_days", "calibration validity must be positive")
		}
		if !point.Calibration.At.IsSet() {
			issues.Add(SeverityError, path+".calibration.at", "calibration instant is required")
		}
		if point.Calibration.SpanDriftPct < 0 {
			issues.Add(SeverityError, path+".calibration.span_drift_pct", "span drift cannot be negative")
		}
		if point.StaleMinutes < 0 {
			issues.Add(SeverityError, path+".stale_minutes", "stale interval cannot be negative")
		}
	}
	circuits := map[string]bool{}
	for index, circuit := range layout.Circuits {
		path := fmt.Sprintf("circuits[%d]", index)
		if strings.TrimSpace(circuit.CircuitID) == "" {
			issues.Add(SeverityError, path+".circuit_id", "circuit identifier is required")
			continue
		}
		if circuits[circuit.CircuitID] {
			issues.Add(SeverityError, path+".circuit_id", "circuit identifier is not unique")
		}
		circuits[circuit.CircuitID] = true
		if !ValidAreaKind(circuit.AreaKind) {
			issues.Add(SeverityError, path+".area_kind", "unsupported area kind")
		}
		issues = append(issues, validateAreaReference(path, circuit.AreaKind, circuit.AreaID, faces, airways, junctions)...)
		if len(circuit.GuardPoints) == 0 {
			issues.Add(SeverityError, path+".guard_points", "a controlled circuit needs at least one guard point")
		}
		seen := map[string]bool{}
		for guardIndex, guard := range circuit.GuardPoints {
			guardPath := fmt.Sprintf("%s.guard_points[%d]", path, guardIndex)
			if !points[guard] {
				issues.Add(SeverityError, guardPath, "guard point does not exist")
			}
			if seen[guard] {
				issues.Add(SeverityError, guardPath, "guard point is listed twice")
			}
			seen[guard] = true
		}
	}
	for index, inspection := range layout.Inspections {
		path := fmt.Sprintf("inspections[%d]", index)
		if strings.TrimSpace(inspection.InspectionID) == "" {
			issues.Add(SeverityError, path+".inspection_id", "inspection identifier is required")
			continue
		}
		if inspection.IntervalHours <= 0 {
			issues.Add(SeverityError, path+".interval_hours", "inspection interval must be positive")
		}
		if !inspection.LastDoneAt.IsSet() {
			issues.Add(SeverityError, path+".last_done_at", "last completion instant is required")
		}
		issues = append(issues, validateAreaReference(path, inspection.AreaKind, inspection.AreaID, faces, airways, junctions)...)
	}
	if len(layout.Faces) == 0 {
		issues.Add(SeverityWarning, "faces", "layout declares no working face")
	}
	if len(layout.Points) == 0 {
		issues.Add(SeverityError, "points", "layout declares no monitoring point")
	}
	return issues.Sorted()
}

// validateAreaReference checks that an area reference resolves.
func validateAreaReference(path string, kind AreaKind, areaID string,
	faces, airways, junctions map[string]bool) Issues {
	issues := Issues{}
	if strings.TrimSpace(areaID) == "" {
		issues.Add(SeverityError, path+".area_id", "area identifier is required")
		return issues
	}
	switch kind {
	case AreaWorkingFace:
		if !faces[areaID] {
			issues.Add(SeverityError, path+".area_id", "references an unknown working face")
		}
	case AreaAirway:
		if !airways[areaID] {
			issues.Add(SeverityError, path+".area_id", "references an unknown airway")
		}
	case AreaJunction:
		if !junctions[areaID] {
			issues.Add(SeverityError, path+".area_id", "references an unknown junction")
		}
	case AreaSurface:
		// A surface area is a free-form label; nothing to resolve.
	}
	return issues
}

// validateThreshold checks that an alarm pair is ordered the way its direction
// implies. An "above" threshold whose trip level sits below its warn level would
// silently never warn before tripping.
func validateThreshold(path string, point Point) Issues {
	issues := Issues{}
	threshold := point.Threshold
	switch threshold.Direction {
	case "above":
		if threshold.Trip < threshold.Warn {
			issues.Add(SeverityError, path+".trip", "trip level must not sit below the warn level")
		}
	case "below":
		if threshold.Trip > threshold.Warn {
			issues.Add(SeverityError, path+".trip", "trip level must not sit above the warn level")
		}
	default:
		issues.Add(SeverityError, path+".direction", "direction must be above or below")
		return issues
	}
	if point.RangeHigh > point.RangeLow {
		for name, value := range map[string]float64{"warn": threshold.Warn, "trip": threshold.Trip} {
			if value < point.RangeLow || value > point.RangeHigh {
				issues.Add(SeverityError, path+"."+name, "alarm level sits outside the measurement range")
			}
		}
	}
	return issues
}

// ValidateReadings checks a reading ledger against a layout.
func ValidateReadings(layout Layout, readings []Reading) Issues {
	issues := Issues{}
	points := layout.PointByID()
	seen := map[string]bool{}
	for index, reading := range readings {
		path := fmt.Sprintf("readings[%d]", index)
		if strings.TrimSpace(reading.ReadingID) == "" {
			issues.Add(SeverityError, path+".reading_id", "reading identifier is required")
		} else if seen[reading.ReadingID] {
			issues.Add(SeverityError, path+".reading_id", "reading identifier is not unique")
		}
		seen[reading.ReadingID] = true
		point, ok := points[reading.PointID]
		if !ok {
			issues.Add(SeverityError, path+".point_id", "reading references an unknown point")
			continue
		}
		if !reading.At.IsSet() {
			issues.Add(SeverityError, path+".at", "reading instant is required")
		}
		if !ValidStatus(reading.Status) {
			issues.Add(SeverityError, path+".status", "unsupported sensor status")
		}
		if !units.Finite(reading.Value) {
			issues.Add(SeverityError, path+".value", "reading value is not finite")
			continue
		}
		if _, err := units.ToCanonical(point.Quantity, reading.Value, reading.Unit); err != nil {
			issues.Add(SeverityError, path+".unit", err.Error())
		}
	}
	return issues.Sorted()
}

// ValidatePersonnel checks the roster and the tag event stream.
func ValidatePersonnel(layout Layout, roster []Person, events []TagEvent) Issues {
	issues := Issues{}
	people := map[string]bool{}
	for index, person := range roster {
		path := fmt.Sprintf("roster[%d]", index)
		if strings.TrimSpace(person.PersonID) == "" {
			issues.Add(SeverityError, path+".person_id", "person identifier is required")
			continue
		}
		if people[person.PersonID] {
			issues.Add(SeverityError, path+".person_id", "person identifier is not unique")
		}
		people[person.PersonID] = true
	}
	faces := layout.FaceByID()
	airways := layout.AirwayByID()
	junctions := layout.JunctionByID()
	seen := map[string]bool{}
	for index, event := range events {
		path := fmt.Sprintf("events[%d]", index)
		if strings.TrimSpace(event.EventID) == "" {
			issues.Add(SeverityError, path+".event_id", "event identifier is required")
		} else if seen[event.EventID] {
			issues.Add(SeverityError, path+".event_id", "event identifier is not unique")
		}
		seen[event.EventID] = true
		if !people[event.PersonID] {
			issues.Add(SeverityError, path+".person_id", "event references a person outside the roster")
		}
		if !event.At.IsSet() {
			issues.Add(SeverityError, path+".at", "event instant is required")
		}
		if !ValidAction(event.Action) {
			issues.Add(SeverityError, path+".action", "unsupported tag action")
			continue
		}
		if event.Action == ActionExit {
			continue
		}
		switch event.AreaKind {
		case AreaWorkingFace:
			if _, ok := faces[event.AreaID]; !ok {
				issues.Add(SeverityError, path+".area_id", "references an unknown working face")
			}
		case AreaAirway:
			if _, ok := airways[event.AreaID]; !ok {
				issues.Add(SeverityError, path+".area_id", "references an unknown airway")
			}
		case AreaJunction:
			if _, ok := junctions[event.AreaID]; !ok {
				issues.Add(SeverityError, path+".area_id", "references an unknown junction")
			}
		case AreaSurface:
		default:
			issues.Add(SeverityError, path+".area_kind", "unsupported area kind")
		}
	}
	return issues.Sorted()
}

// ValidatePermits checks permits against a layout.
func ValidatePermits(layout Layout, permits []Permit) Issues {
	issues := Issues{}
	points := layout.PointByID()
	faces := layout.FaceByID()
	seen := map[string]bool{}
	for index, permit := range permits {
		path := fmt.Sprintf("permits[%d]", index)
		if strings.TrimSpace(permit.PermitID) == "" {
			issues.Add(SeverityError, path+".permit_id", "permit identifier is required")
		} else if seen[permit.PermitID] {
			issues.Add(SeverityError, path+".permit_id", "permit identifier is not unique")
		}
		seen[permit.PermitID] = true
		if strings.TrimSpace(permit.Kind) == "" {
			issues.Add(SeverityError, path+".kind", "permit kind is required")
		}
		if !permit.Window.From.IsSet() || !permit.Window.To.IsSet() {
			issues.Add(SeverityError, path+".window", "permit validity window is required")
		} else if !permit.Window.To.After(permit.Window.From) {
			issues.Add(SeverityError, path+".window", "permit validity window must be increasing")
		}
		if permit.AreaKind == AreaWorkingFace {
			if _, ok := faces[permit.AreaID]; !ok {
				issues.Add(SeverityError, path+".area_id", "references an unknown working face")
			}
		}
		if len(permit.RequiredPoints) == 0 {
			issues.Add(SeverityError, path+".required_points", "a permit must name at least one required point")
		}
		for pointIndex, required := range permit.RequiredPoints {
			if _, ok := points[required]; !ok {
				issues.Add(SeverityError,
					fmt.Sprintf("%s.required_points[%d]", path, pointIndex),
					"required point does not exist")
			}
		}
	}
	return issues.Sorted()
}

// ValidateBundle validates every document together.
func ValidateBundle(bundle Bundle) Issues {
	issues := ValidateLayout(bundle.Layout)
	issues = append(issues, ValidateReadings(bundle.Layout, bundle.Readings)...)
	issues = append(issues, ValidatePersonnel(bundle.Layout, bundle.Roster, bundle.Events)...)
	issues = append(issues, ValidatePermits(bundle.Layout, bundle.Permits)...)
	return issues.Sorted()
}

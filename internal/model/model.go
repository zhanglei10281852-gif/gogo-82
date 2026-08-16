// Package model holds the MineGuard domain types and their validation.
//
// The layout is deliberately explicit: a mine is districts, districts hold
// working faces, faces are connected by airways through junctions, and every
// monitoring point belongs to exactly one area. Nothing is inferred from a
// naming convention, because a mis-parsed identifier is not a safe way to decide
// which sensor guards which circuit.
package model

import (
	"fmt"
	"sort"
	"strings"

	"MineGuard/internal/timeutil"
	"MineGuard/internal/units"
)

// SchemaVersion tags a layout document.
const SchemaVersion = "mineguard/v1"

// SensorStatus is the self-reported health of a monitoring point.
type SensorStatus string

// The sensor statuses.
const (
	StatusOK          SensorStatus = "ok"
	StatusFaulty      SensorStatus = "faulty"
	StatusMaintenance SensorStatus = "maintenance"
	StatusOffline     SensorStatus = "offline"
)

// Usable reports whether a status permits the reading to inform a decision.
func (s SensorStatus) Usable() bool { return s == StatusOK }

// ValidStatus reports whether the status is one of the known values.
func ValidStatus(s SensorStatus) bool {
	switch s {
	case StatusOK, StatusFaulty, StatusMaintenance, StatusOffline:
		return true
	default:
		return false
	}
}

// AreaKind distinguishes the places a point can sit in.
type AreaKind string

// The area kinds.
const (
	AreaWorkingFace AreaKind = "working_face"
	AreaAirway      AreaKind = "airway"
	AreaJunction    AreaKind = "junction"
	AreaSurface     AreaKind = "surface"
)

// ValidAreaKind reports whether the kind is known.
func ValidAreaKind(kind AreaKind) bool {
	switch kind {
	case AreaWorkingFace, AreaAirway, AreaJunction, AreaSurface:
		return true
	default:
		return false
	}
}

// Placement says where in the cross-section a point sits, which is what makes
// methane layering detectable: a roof point reading far above a general-body
// point in the same area is the signature.
type Placement string

// The placements.
const (
	PlacementGeneralBody Placement = "general_body"
	PlacementRoof        Placement = "roof"
	PlacementReturn      Placement = "return"
	PlacementIntake      Placement = "intake"
)

// ValidPlacement reports whether the placement is known.
func ValidPlacement(p Placement) bool {
	switch p {
	case PlacementGeneralBody, PlacementRoof, PlacementReturn, PlacementIntake:
		return true
	default:
		return false
	}
}

// Layout is the whole mine description.
type Layout struct {
	SchemaVersion string       `json:"schema_version"`
	MineID        string       `json:"mine_id"`
	Label         string       `json:"label"`
	Districts     []District   `json:"districts"`
	Faces         []Face       `json:"faces"`
	Junctions     []Junction   `json:"junctions"`
	Airways       []Airway     `json:"airways"`
	Points        []Point      `json:"points"`
	Circuits      []Circuit    `json:"circuits"`
	Inspections   []Inspection `json:"inspections,omitempty"`
}

// District groups working faces.
type District struct {
	DistrictID string `json:"district_id"`
	Label      string `json:"label"`
	Seam       string `json:"seam,omitempty"`
}

// Face is a working face with the people and equipment it carries.
type Face struct {
	FaceID         string  `json:"face_id"`
	DistrictID     string  `json:"district_id"`
	Label          string  `json:"label"`
	IntakeJunction string  `json:"intake_junction"`
	ReturnJunction string  `json:"return_junction"`
	PlannedPersons int     `json:"planned_persons"`
	EquipmentKW    float64 `json:"equipment_kw"`
	MinAirflowM3S  float64 `json:"min_airflow_m3s,omitempty"`
}

// Junction is a node in the ventilation network.
type Junction struct {
	JunctionID string  `json:"junction_id"`
	Label      string  `json:"label"`
	DepthM     float64 `json:"depth_m,omitempty"`
	Surface    bool    `json:"surface,omitempty"`
}

// Airway is a directed branch carrying air from one junction to another.
type Airway struct {
	AirwayID      string  `json:"airway_id"`
	FromJunction  string  `json:"from_junction"`
	ToJunction    string  `json:"to_junction"`
	Label         string  `json:"label,omitempty"`
	LengthM       float64 `json:"length_m"`
	AreaM2        float64 `json:"area_m2"`
	Resistance    float64 `json:"resistance_ns2m8"`
	FanPressurePa float64 `json:"fan_pressure_pa,omitempty"`
	Regulator     bool    `json:"regulator,omitempty"`
}

// Threshold is one alarm level on a monitoring point. Warn is advisory; Trip is
// the level at which the bound circuit must be de-energised.
type Threshold struct {
	Warn float64 `json:"warn"`
	Trip float64 `json:"trip"`
	// Direction is "above" when high readings are dangerous, "below" when low
	// readings are (oxygen deficiency is the reason this is configurable).
	Direction string `json:"direction"`
}

// Calibration records the last calibration of a point.
type Calibration struct {
	At           timeutil.Stamp `json:"at"`
	ValidDays    int            `json:"valid_days"`
	ZeroDrift    float64        `json:"zero_drift,omitempty"`
	SpanDriftPct float64        `json:"span_drift_pct,omitempty"`
	Technician   string         `json:"technician,omitempty"`
}

// Point is a monitoring point.
type Point struct {
	PointID      string         `json:"point_id"`
	Label        string         `json:"label"`
	Quantity     units.Quantity `json:"quantity"`
	AreaKind     AreaKind       `json:"area_kind"`
	AreaID       string         `json:"area_id"`
	Placement    Placement      `json:"placement"`
	RangeLow     float64        `json:"range_low"`
	RangeHigh    float64        `json:"range_high"`
	Threshold    Threshold      `json:"threshold"`
	Calibration  Calibration    `json:"calibration"`
	Mandatory    bool           `json:"mandatory,omitempty"`
	StaleMinutes float64        `json:"stale_minutes,omitempty"`
}

// Circuit is a controlled electrical circuit guarded by monitoring points.
type Circuit struct {
	CircuitID           string   `json:"circuit_id"`
	Label               string   `json:"label"`
	AreaKind            AreaKind `json:"area_kind"`
	AreaID              string   `json:"area_id"`
	GuardPoints         []string `json:"guard_points"`
	RequiresVentilation bool     `json:"requires_ventilation,omitempty"`
}

// Inspection is a scheduled inspection of a fixed asset.
type Inspection struct {
	InspectionID  string         `json:"inspection_id"`
	Subject       string         `json:"subject"`
	AreaKind      AreaKind       `json:"area_kind"`
	AreaID        string         `json:"area_id"`
	IntervalHours float64        `json:"interval_hours"`
	LastDoneAt    timeutil.Stamp `json:"last_done_at"`
}

// Reading is one measurement from one point.
type Reading struct {
	ReadingID string         `json:"reading_id"`
	PointID   string         `json:"point_id"`
	At        timeutil.Stamp `json:"at"`
	Value     float64        `json:"value"`
	Unit      string         `json:"unit"`
	Status    SensorStatus   `json:"status"`
	Note      string         `json:"note,omitempty"`
}

// TagEvent is a personnel entry or exit event.
type TagEvent struct {
	EventID  string         `json:"event_id"`
	PersonID string         `json:"person_id"`
	At       timeutil.Stamp `json:"at"`
	Action   string         `json:"action"`
	AreaKind AreaKind       `json:"area_kind"`
	AreaID   string         `json:"area_id"`
}

// Actions a tag event can carry.
const (
	ActionEnter  = "enter"
	ActionMove   = "move"
	ActionExit   = "exit"
	ActionMuster = "muster"
)

// ValidAction reports whether the tag action is known.
func ValidAction(action string) bool {
	switch action {
	case ActionEnter, ActionMove, ActionExit, ActionMuster:
		return true
	default:
		return false
	}
}

// Person is a rostered worker for the shift under assessment.
type Person struct {
	PersonID string `json:"person_id"`
	Name     string `json:"name"`
	Role     string `json:"role,omitempty"`
	Crew     string `json:"crew,omitempty"`
}

// Permit is a high-risk work permit.
type Permit struct {
	PermitID       string          `json:"permit_id"`
	Kind           string          `json:"kind"`
	AreaKind       AreaKind        `json:"area_kind"`
	AreaID         string          `json:"area_id"`
	Window         timeutil.Window `json:"window"`
	RequiredPoints []string        `json:"required_points"`
	Issuer         string          `json:"issuer,omitempty"`
}

// Bundle is every input document loaded together.
type Bundle struct {
	Layout   Layout
	Readings []Reading
	Roster   []Person
	Events   []TagEvent
	Permits  []Permit
}

// Issue is one validation finding.
type Issue struct {
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Message  string `json:"message"`
}

// Issues is a sortable collection of findings.
type Issues []Issue

// Severities in use.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Add appends an issue.
func (i *Issues) Add(severity, path, message string) {
	*i = append(*i, Issue{Severity: severity, Path: path, Message: message})
}

// Errors returns only the error-severity findings.
func (i Issues) Errors() Issues {
	out := Issues{}
	for _, item := range i {
		if item.Severity == SeverityError {
			out = append(out, item)
		}
	}
	return out
}

// OK reports whether the collection carries no errors.
func (i Issues) OK() bool { return len(i.Errors()) == 0 }

// Sorted returns the findings in a canonical order.
func (i Issues) Sorted() Issues {
	out := append(Issues(nil), i...)
	sort.SliceStable(out, func(a, b int) bool {
		left, right := out[a], out[b]
		if left.Severity != right.Severity {
			return left.Severity < right.Severity
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		return left.Message < right.Message
	})
	return out
}

// Error renders the findings as one error, or nil when there are none.
func (i Issues) Error() error {
	errs := i.Errors().Sorted()
	if len(errs) == 0 {
		return nil
	}
	parts := make([]string, 0, len(errs))
	for _, item := range errs {
		parts = append(parts, item.Path+": "+item.Message)
	}
	return fmt.Errorf("%s", strings.Join(parts, "; "))
}

// PointByID indexes the layout points.
func (l Layout) PointByID() map[string]Point {
	out := make(map[string]Point, len(l.Points))
	for _, point := range l.Points {
		out[point.PointID] = point
	}
	return out
}

// FaceByID indexes the working faces.
func (l Layout) FaceByID() map[string]Face {
	out := make(map[string]Face, len(l.Faces))
	for _, face := range l.Faces {
		out[face.FaceID] = face
	}
	return out
}

// JunctionByID indexes the junctions.
func (l Layout) JunctionByID() map[string]Junction {
	out := make(map[string]Junction, len(l.Junctions))
	for _, junction := range l.Junctions {
		out[junction.JunctionID] = junction
	}
	return out
}

// AirwayByID indexes the airways.
func (l Layout) AirwayByID() map[string]Airway {
	out := make(map[string]Airway, len(l.Airways))
	for _, airway := range l.Airways {
		out[airway.AirwayID] = airway
	}
	return out
}

// CircuitByID indexes the circuits.
func (l Layout) CircuitByID() map[string]Circuit {
	out := make(map[string]Circuit, len(l.Circuits))
	for _, circuit := range l.Circuits {
		out[circuit.CircuitID] = circuit
	}
	return out
}

// PointsInArea returns the points in one area, ordered by identifier.
func (l Layout) PointsInArea(kind AreaKind, areaID string) []Point {
	out := make([]Point, 0, 4)
	for _, point := range l.Points {
		if point.AreaKind == kind && point.AreaID == areaID {
			out = append(out, point)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].PointID < out[b].PointID })
	return out
}

// Sort puts every collection in the layout into canonical order.
func (l *Layout) Sort() {
	sort.SliceStable(l.Districts, func(a, b int) bool { return l.Districts[a].DistrictID < l.Districts[b].DistrictID })
	sort.SliceStable(l.Faces, func(a, b int) bool { return l.Faces[a].FaceID < l.Faces[b].FaceID })
	sort.SliceStable(l.Junctions, func(a, b int) bool { return l.Junctions[a].JunctionID < l.Junctions[b].JunctionID })
	sort.SliceStable(l.Airways, func(a, b int) bool { return l.Airways[a].AirwayID < l.Airways[b].AirwayID })
	sort.SliceStable(l.Points, func(a, b int) bool { return l.Points[a].PointID < l.Points[b].PointID })
	sort.SliceStable(l.Circuits, func(a, b int) bool { return l.Circuits[a].CircuitID < l.Circuits[b].CircuitID })
	sort.SliceStable(l.Inspections, func(a, b int) bool { return l.Inspections[a].InspectionID < l.Inspections[b].InspectionID })
}

// SortReadings orders readings by instant, then point, then identifier.
func SortReadings(readings []Reading) {
	sort.SliceStable(readings, func(a, b int) bool {
		left, right := readings[a], readings[b]
		if !left.At.Equal(right.At) {
			return left.At.Before(right.At)
		}
		if left.PointID != right.PointID {
			return left.PointID < right.PointID
		}
		return left.ReadingID < right.ReadingID
	})
}

// SortEvents orders tag events by instant, then person, then identifier.
func SortEvents(events []TagEvent) {
	sort.SliceStable(events, func(a, b int) bool {
		left, right := events[a], events[b]
		if !left.At.Equal(right.At) {
			return left.At.Before(right.At)
		}
		if left.PersonID != right.PersonID {
			return left.PersonID < right.PersonID
		}
		return left.EventID < right.EventID
	})
}

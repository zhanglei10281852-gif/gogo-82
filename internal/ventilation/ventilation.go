// Package ventilation assesses the airflow network.
//
// The model is a directed graph of airways between junctions with a square-law
// resistance: the pressure drop across an airway is its resistance times the
// square of the quantity flowing through it. That is enough to check the three
// things a decision needs: whether air is conserved at every junction, whether
// each working face receives the quantity its people and equipment require, and
// whether any loop lets return air find its way back into an intake.
package ventilation

import (
	"fmt"
	"sort"

	"MineGuard/internal/config"
	"MineGuard/internal/model"
	"MineGuard/internal/reading"
	"MineGuard/internal/timeutil"
	"MineGuard/internal/units"
)

// AirwayFlow is the measured state of one airway.
type AirwayFlow struct {
	AirwayID       string  `json:"airway_id"`
	FromJunction   string  `json:"from_junction"`
	ToJunction     string  `json:"to_junction"`
	QuantityM3S    float64 `json:"quantity_m3s"`
	VelocityMS     float64 `json:"velocity_ms"`
	Resistance     float64 `json:"resistance_ns2m8"`
	PressureDropPa float64 `json:"pressure_drop_pa"`
	Measured       bool    `json:"measured"`
	PointID        string  `json:"point_id,omitempty"`
	Explanation    string  `json:"explanation"`
}

// JunctionBalance is the mass balance residual at one junction.
type JunctionBalance struct {
	JunctionID  string   `json:"junction_id"`
	InM3S       float64  `json:"in_m3s"`
	OutM3S      float64  `json:"out_m3s"`
	ResidualM3S float64  `json:"residual_m3s"`
	Balanced    bool     `json:"balanced"`
	Surface     bool     `json:"surface"`
	Inbound     []string `json:"inbound"`
	Outbound    []string `json:"outbound"`
	Explanation string   `json:"explanation"`
}

// FaceAdequacy is the airflow verdict for one working face.
type FaceAdequacy struct {
	FaceID         string  `json:"face_id"`
	RequiredM3S    float64 `json:"required_m3s"`
	SuppliedM3S    float64 `json:"supplied_m3s"`
	Adequate       bool    `json:"adequate"`
	Measured       bool    `json:"measured"`
	PersonShare    float64 `json:"person_share_m3s"`
	EquipmentShare float64 `json:"equipment_share_m3s"`
	FloorShare     float64 `json:"floor_share_m3s"`
	Explanation    string  `json:"explanation"`
}

// Recirculation is one directed cycle carrying air.
type Recirculation struct {
	Airways     []string `json:"airways"`
	Junctions   []string `json:"junctions"`
	MinimumM3S  float64  `json:"minimum_m3s"`
	Explanation string   `json:"explanation"`
}

// Result is the outcome of the ventilation stage.
type Result struct {
	AsOf            timeutil.Stamp    `json:"as_of"`
	Flows           []AirwayFlow      `json:"flows"`
	Balances        []JunctionBalance `json:"balances"`
	Faces           []FaceAdequacy    `json:"faces"`
	Recirculations  []Recirculation   `json:"recirculations"`
	TotalIntakeM3S  float64           `json:"total_intake_m3s"`
	UnbalancedCount int               `json:"unbalanced_junctions"`
	InadequateFaces int               `json:"inadequate_faces"`
	OK              bool              `json:"ok"`
	Issues          model.Issues      `json:"issues,omitempty"`
}

// Assess evaluates the network from the airflow points that reported a usable
// reading. An airway without a usable airflow point keeps its declared quantity
// of zero and is reported as unmeasured rather than assumed.
func Assess(cfg config.Config, layout model.Layout, states []reading.PointState,
	asOf timeutil.Stamp) Result {
	result := Result{AsOf: asOf}
	flowByAirway := measuredFlows(layout, states)
	for _, airway := range layout.Airways {
		flow := AirwayFlow{
			AirwayID:     airway.AirwayID,
			FromJunction: airway.FromJunction,
			ToJunction:   airway.ToJunction,
			Resistance:   airway.Resistance,
		}
		if measured, ok := flowByAirway[airway.AirwayID]; ok {
			flow.QuantityM3S = units.Round(measured.value, cfg.Output.Decimals)
			flow.Measured = true
			flow.PointID = measured.pointID
		}
		flow.VelocityMS = units.Round(units.SafeDiv(flow.QuantityM3S, airway.AreaM2, 0), cfg.Output.Decimals)
		flow.PressureDropPa = units.Round(PressureDrop(airway.Resistance, flow.QuantityM3S), cfg.Output.Decimals)
		if flow.Measured {
			flow.Explanation = fmt.Sprintf("%s carries %s m3/s from %s measured at %s",
				airway.AirwayID, units.Format(flow.QuantityM3S, 2), airway.FromJunction, flow.PointID)
		} else {
			flow.Explanation = fmt.Sprintf("%s has no usable airflow measurement", airway.AirwayID)
			result.Issues.Add(model.SeverityWarning, "ventilation."+airway.AirwayID,
				"airway has no usable airflow measurement")
		}
		result.Flows = append(result.Flows, flow)
	}
	sort.SliceStable(result.Flows, func(a, b int) bool { return result.Flows[a].AirwayID < result.Flows[b].AirwayID })
	result.Balances = balances(cfg, layout, result.Flows)
	for _, balance := range result.Balances {
		if !balance.Balanced {
			result.UnbalancedCount++
		}
	}
	result.Faces = faceAdequacy(cfg, layout, result.Flows)
	for _, face := range result.Faces {
		if !face.Adequate {
			result.InadequateFaces++
		}
	}
	result.Recirculations = recirculations(cfg, layout, result.Flows)
	result.TotalIntakeM3S = units.Round(totalIntake(layout, result.Flows), cfg.Output.Decimals)
	result.OK = result.UnbalancedCount == 0 && result.InadequateFaces == 0 && len(result.Recirculations) == 0
	result.Issues = result.Issues.Sorted()
	return result
}

// PressureDrop applies the square-law resistance model.
func PressureDrop(resistance, quantity float64) float64 {
	return resistance * quantity * quantity
}

// SeriesResistance is the equivalent resistance of airways in series.
func SeriesResistance(resistances []float64) float64 {
	total := 0.0
	for _, item := range resistances {
		if item <= 0 {
			continue
		}
		total += item
	}
	return total
}

// ParallelResistance is the equivalent resistance of airways in parallel under
// the square law: one over the square of the sum of the reciprocal square roots.
func ParallelResistance(resistances []float64) float64 {
	sum := 0.0
	for _, item := range resistances {
		if item <= 0 {
			continue
		}
		sum += 1 / sqrt(item)
	}
	if sum <= 0 {
		return 0
	}
	return 1 / (sum * sum)
}

// sqrt is a small local helper so the package needs no math import for one call.
func sqrt(value float64) float64 {
	if value <= 0 {
		return 0
	}
	guess := value
	for i := 0; i < 40; i++ {
		next := 0.5 * (guess + value/guess)
		if next == guess {
			break
		}
		guess = next
	}
	return guess
}

type measurement struct {
	value   float64
	pointID string
}

// measuredFlows collects the usable airflow readings, one per airway. When two
// points measure the same airway the lexicographically first point wins so the
// answer does not depend on map order.
func measuredFlows(layout model.Layout, states []reading.PointState) map[string]measurement {
	out := map[string]measurement{}
	ordered := append([]reading.PointState(nil), states...)
	sort.SliceStable(ordered, func(a, b int) bool { return ordered[a].PointID < ordered[b].PointID })
	for _, state := range ordered {
		if state.Quantity != units.QuantityAirflow || state.AreaKind != model.AreaAirway {
			continue
		}
		if !state.Usable || !state.HasLatest {
			continue
		}
		if _, taken := out[state.AreaID]; taken {
			continue
		}
		out[state.AreaID] = measurement{value: state.Latest.Value, pointID: state.PointID}
	}
	return out
}

// balances computes the junction residuals.
func balances(cfg config.Config, layout model.Layout, flows []AirwayFlow) []JunctionBalance {
	inbound := map[string][]AirwayFlow{}
	outbound := map[string][]AirwayFlow{}
	for _, flow := range flows {
		inbound[flow.ToJunction] = append(inbound[flow.ToJunction], flow)
		outbound[flow.FromJunction] = append(outbound[flow.FromJunction], flow)
	}
	out := make([]JunctionBalance, 0, len(layout.Junctions))
	for _, junction := range layout.Junctions {
		balance := JunctionBalance{JunctionID: junction.JunctionID, Surface: junction.Surface}
		for _, flow := range inbound[junction.JunctionID] {
			balance.InM3S += flow.QuantityM3S
			balance.Inbound = append(balance.Inbound, flow.AirwayID)
		}
		for _, flow := range outbound[junction.JunctionID] {
			balance.OutM3S += flow.QuantityM3S
			balance.Outbound = append(balance.Outbound, flow.AirwayID)
		}
		sort.Strings(balance.Inbound)
		sort.Strings(balance.Outbound)
		balance.InM3S = units.Round(balance.InM3S, cfg.Output.Decimals)
		balance.OutM3S = units.Round(balance.OutM3S, cfg.Output.Decimals)
		balance.ResidualM3S = units.Round(balance.InM3S-balance.OutM3S, cfg.Output.Decimals)
		// A surface junction exchanges air with the atmosphere, so its residual is
		// expected and is not a balance failure.
		if junction.Surface || len(balance.Inbound) == 0 || len(balance.Outbound) == 0 {
			balance.Balanced = true
			balance.Explanation = fmt.Sprintf("%s is a boundary junction with residual %s m3/s",
				junction.JunctionID, units.Format(balance.ResidualM3S, 2))
		} else {
			balance.Balanced = absolute(balance.ResidualM3S) <= cfg.Ventilation.BalanceToleranceM3S
			balance.Explanation = fmt.Sprintf("%s residual %s m3/s against tolerance %s m3/s",
				junction.JunctionID, units.Format(balance.ResidualM3S, 2),
				units.Format(cfg.Ventilation.BalanceToleranceM3S, 2))
		}
		out = append(out, balance)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].JunctionID < out[b].JunctionID })
	return out
}

// faceAdequacy compares supplied with required airflow per face.
func faceAdequacy(cfg config.Config, layout model.Layout, flows []AirwayFlow) []FaceAdequacy {
	byJunction := map[string]float64{}
	for _, flow := range flows {
		byJunction[flow.ToJunction] += flow.QuantityM3S
	}
	measured := map[string]bool{}
	for _, flow := range flows {
		if flow.Measured {
			measured[flow.ToJunction] = true
		}
	}
	out := make([]FaceAdequacy, 0, len(layout.Faces))
	for _, face := range layout.Faces {
		adequacy := FaceAdequacy{FaceID: face.FaceID}
		adequacy.PersonShare = units.Round(float64(face.PlannedPersons)*cfg.Ventilation.AirPerPersonM3S, cfg.Output.Decimals)
		adequacy.EquipmentShare = units.Round(face.EquipmentKW*cfg.Ventilation.AirPerKilowattM3S, cfg.Output.Decimals)
		adequacy.FloorShare = units.Round(cfg.Ventilation.MinFaceAirflowM3S, cfg.Output.Decimals)
		required := adequacy.PersonShare + adequacy.EquipmentShare
		if required < adequacy.FloorShare {
			required = adequacy.FloorShare
		}
		if face.MinAirflowM3S > required {
			required = face.MinAirflowM3S
		}
		adequacy.RequiredM3S = units.Round(required, cfg.Output.Decimals)
		adequacy.SuppliedM3S = units.Round(byJunction[face.IntakeJunction], cfg.Output.Decimals)
		adequacy.Measured = measured[face.IntakeJunction]
		adequacy.Adequate = adequacy.Measured && adequacy.SuppliedM3S+1e-9 >= adequacy.RequiredM3S
		if !adequacy.Measured {
			adequacy.Explanation = fmt.Sprintf("%s intake %s has no usable airflow measurement",
				face.FaceID, face.IntakeJunction)
		} else {
			adequacy.Explanation = fmt.Sprintf("%s receives %s m3/s against %s m3/s required",
				face.FaceID, units.Format(adequacy.SuppliedM3S, 2), units.Format(adequacy.RequiredM3S, 2))
		}
		out = append(out, adequacy)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].FaceID < out[b].FaceID })
	return out
}

// edge is one directed airway in the ventilation graph.
type edge struct {
	airwayID string
	to       string
	quantity float64
}

// recirculations finds directed cycles whose every airway carries more than the
// configured tolerance, which is the condition under which return air can reach
// an intake again.
func recirculations(cfg config.Config, layout model.Layout, flows []AirwayFlow) []Recirculation {
	graph := map[string][]edge{}
	nodes := make([]string, 0, len(layout.Junctions))
	for _, junction := range layout.Junctions {
		nodes = append(nodes, junction.JunctionID)
	}
	sort.Strings(nodes)
	for _, flow := range flows {
		if flow.QuantityM3S <= cfg.Ventilation.RecirculationTolerance {
			continue
		}
		graph[flow.FromJunction] = append(graph[flow.FromJunction], edge{
			airwayID: flow.AirwayID, to: flow.ToJunction, quantity: flow.QuantityM3S,
		})
	}
	for node := range graph {
		sort.SliceStable(graph[node], func(a, b int) bool { return graph[node][a].airwayID < graph[node][b].airwayID })
	}
	state := map[string]int{}
	stack := []string{}
	edges := []edge{}
	found := []Recirculation{}
	seen := map[string]bool{}
	var visit func(node string)
	visit = func(node string) {
		state[node] = 1
		stack = append(stack, node)
		for _, next := range graph[node] {
			edges = append(edges, next)
			switch state[next.to] {
			case 0:
				visit(next.to)
			case 1:
				cycle := extractCycle(stack, edges, next.to)
				if cycle != nil {
					key := joinStrings(cycle.Airways)
					if !seen[key] {
						seen[key] = true
						cycle.Explanation = fmt.Sprintf("air returns through %s carrying at least %s m3/s",
							key, units.Format(cycle.MinimumM3S, 2))
						found = append(found, *cycle)
					}
				}
			}
			edges = edges[:len(edges)-1]
		}
		stack = stack[:len(stack)-1]
		state[node] = 2
	}
	for _, node := range nodes {
		if state[node] == 0 {
			visit(node)
		}
	}
	sort.SliceStable(found, func(a, b int) bool {
		return joinStrings(found[a].Airways) < joinStrings(found[b].Airways)
	})
	return found
}

// extractCycle turns the current traversal stack into a cycle description.
func extractCycle(stack []string, edges []edge, target string) *Recirculation {
	start := -1
	for index, node := range stack {
		if node == target {
			start = index
			break
		}
	}
	if start < 0 {
		return nil
	}
	cycle := &Recirculation{}
	minimum := 0.0
	for index := start; index < len(edges); index++ {
		cycle.Airways = append(cycle.Airways, edges[index].airwayID)
		if minimum == 0 || edges[index].quantity < minimum {
			minimum = edges[index].quantity
		}
	}
	cycle.Junctions = append(cycle.Junctions, stack[start:]...)
	cycle.MinimumM3S = minimum
	if len(cycle.Airways) == 0 {
		return nil
	}
	return cycle
}

// totalIntake sums the air entering the mine from surface junctions.
func totalIntake(layout model.Layout, flows []AirwayFlow) float64 {
	surface := map[string]bool{}
	for _, junction := range layout.Junctions {
		if junction.Surface {
			surface[junction.JunctionID] = true
		}
	}
	total := 0.0
	for _, flow := range flows {
		if surface[flow.FromJunction] {
			total += flow.QuantityM3S
		}
	}
	return total
}

// absolute is the magnitude of a value.
func absolute(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

// joinStrings renders identifiers as a stable arrow separated path.
func joinStrings(items []string) string {
	if len(items) == 0 {
		return ""
	}
	out := items[0]
	for _, item := range items[1:] {
		out += "->" + item
	}
	return out
}

// FaceIndex maps face identifiers to their adequacy verdict.
func FaceIndex(faces []FaceAdequacy) map[string]FaceAdequacy {
	out := make(map[string]FaceAdequacy, len(faces))
	for _, face := range faces {
		out[face.FaceID] = face
	}
	return out
}

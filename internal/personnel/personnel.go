// Package personnel reconstructs who is underground and where.
//
// The tag stream is replayed in instant order: an entry or a move puts a person
// in an area, an exit takes them to the surface, and a muster event records that
// they reported at a muster station. After an evacuation order the interesting
// number is not how many people mustered but which rostered person did not, with
// their last known area and the instant it was recorded.
package personnel

import (
	"fmt"
	"sort"

	"MineGuard/internal/config"
	"MineGuard/internal/model"
	"MineGuard/internal/timeutil"
)

// Location is where one person was last seen.
type Location struct {
	PersonID    string         `json:"person_id"`
	Name        string         `json:"name"`
	AreaKind    model.AreaKind `json:"area_kind"`
	AreaID      string         `json:"area_id"`
	At          timeutil.Stamp `json:"at"`
	EventID     string         `json:"event_id"`
	Action      string         `json:"action"`
	Underground bool           `json:"underground"`
	Mustered    bool           `json:"mustered"`
}

// AreaOccupancy is the headcount of one area.
type AreaOccupancy struct {
	AreaKind    model.AreaKind `json:"area_kind"`
	AreaID      string         `json:"area_id"`
	Count       int            `json:"count"`
	People      []string       `json:"people"`
	Limit       int            `json:"limit"`
	OverLimit   bool           `json:"over_limit"`
	Explanation string         `json:"explanation"`
}

// Muster is the completeness verdict after an evacuation order.
type Muster struct {
	Ordered     bool           `json:"ordered"`
	OrderedAt   timeutil.Stamp `json:"ordered_at"`
	Deadline    timeutil.Stamp `json:"deadline"`
	Expected    int            `json:"expected"`
	Accounted   int            `json:"accounted"`
	Unaccounted []Location     `json:"unaccounted"`
	Complete    bool           `json:"complete"`
	Explanation string         `json:"explanation"`
}

// Result is the outcome of the personnel stage.
type Result struct {
	AsOf        timeutil.Stamp  `json:"as_of"`
	Underground int             `json:"underground"`
	Locations   []Location      `json:"locations"`
	Occupancy   []AreaOccupancy `json:"occupancy"`
	Muster      Muster          `json:"muster"`
	Issues      model.Issues    `json:"issues,omitempty"`
}

// Assess replays the tag stream up to asOf. orderedAt is the instant an
// evacuation order was given; an unset instant means no order is in force.
func Assess(cfg config.Config, layout model.Layout, roster []model.Person,
	events []model.TagEvent, orderedAt timeutil.Stamp, asOf timeutil.Stamp) Result {
	result := Result{AsOf: asOf}
	people := map[string]model.Person{}
	for _, person := range roster {
		people[person.PersonID] = person
	}
	ordered := append([]model.TagEvent(nil), events...)
	model.SortEvents(ordered)
	current := map[string]Location{}
	for _, event := range ordered {
		if asOf.IsSet() && event.At.After(asOf) {
			continue
		}
		person, known := people[event.PersonID]
		if !known {
			result.Issues.Add(model.SeverityWarning, "events."+event.EventID,
				"tag event describes a person outside the roster")
			continue
		}
		location := current[event.PersonID]
		location.PersonID = event.PersonID
		location.Name = person.Name
		location.At = event.At
		location.EventID = event.EventID
		location.Action = event.Action
		switch event.Action {
		case model.ActionEnter, model.ActionMove:
			location.AreaKind = event.AreaKind
			location.AreaID = event.AreaID
			location.Underground = event.AreaKind != model.AreaSurface
			location.Mustered = false
		case model.ActionExit:
			location.AreaKind = model.AreaSurface
			location.AreaID = event.AreaID
			location.Underground = false
			location.Mustered = true
		case model.ActionMuster:
			location.AreaKind = event.AreaKind
			location.AreaID = event.AreaID
			location.Underground = event.AreaKind != model.AreaSurface
			location.Mustered = true
		}
		current[event.PersonID] = location
	}
	ids := make([]string, 0, len(current))
	for id := range current {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		location := current[id]
		result.Locations = append(result.Locations, location)
		if location.Underground {
			result.Underground++
		}
	}
	result.Occupancy = occupancy(cfg, layout, result.Locations)
	result.Muster = muster(cfg, roster, current, orderedAt, asOf)
	result.Issues = result.Issues.Sorted()
	return result
}

// occupancy groups the underground population by area.
func occupancy(cfg config.Config, layout model.Layout, locations []Location) []AreaOccupancy {
	type key struct {
		kind model.AreaKind
		id   string
	}
	grouped := map[key][]string{}
	for _, location := range locations {
		if !location.Underground {
			continue
		}
		grouped[key{location.AreaKind, location.AreaID}] = append(
			grouped[key{location.AreaKind, location.AreaID}], location.PersonID)
	}
	out := make([]AreaOccupancy, 0, len(grouped))
	for item, people := range grouped {
		sort.Strings(people)
		entry := AreaOccupancy{
			AreaKind: item.kind,
			AreaID:   item.id,
			Count:    len(people),
			People:   people,
		}
		if item.kind == model.AreaWorkingFace {
			entry.Limit = cfg.Personnel.MaxFaceOccupancy
			entry.OverLimit = entry.Count > entry.Limit
		}
		if entry.OverLimit {
			entry.Explanation = fmt.Sprintf("%s holds %d people against a limit of %d",
				item.id, entry.Count, entry.Limit)
		} else {
			entry.Explanation = fmt.Sprintf("%s holds %d people", item.id, entry.Count)
		}
		out = append(out, entry)
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].AreaKind != out[b].AreaKind {
			return out[a].AreaKind < out[b].AreaKind
		}
		return out[a].AreaID < out[b].AreaID
	})
	return out
}

// muster checks whether every rostered person is accounted for.
func muster(cfg config.Config, roster []model.Person, current map[string]Location,
	orderedAt timeutil.Stamp, asOf timeutil.Stamp) Muster {
	result := Muster{Ordered: orderedAt.IsSet(), OrderedAt: orderedAt}
	if !result.Ordered {
		result.Complete = true
		result.Explanation = "no evacuation order is in force"
		return result
	}
	result.Deadline = orderedAt.AddMinutes(cfg.Personnel.MusterGraceMinutes)
	ordered := append([]model.Person(nil), roster...)
	sort.SliceStable(ordered, func(a, b int) bool { return ordered[a].PersonID < ordered[b].PersonID })
	for _, person := range ordered {
		result.Expected++
		location, seen := current[person.PersonID]
		if !seen {
			// Nobody tagged this person at all. They may never have gone
			// underground, but the muster cannot prove that, so they are unaccounted.
			result.Unaccounted = append(result.Unaccounted, Location{
				PersonID: person.PersonID,
				Name:     person.Name,
				AreaKind: model.AreaSurface,
				Action:   "never_tagged",
			})
			continue
		}
		if accountedFor(location, orderedAt) {
			result.Accounted++
			continue
		}
		result.Unaccounted = append(result.Unaccounted, location)
	}
	result.Complete = len(result.Unaccounted) == 0
	if result.Complete {
		result.Explanation = fmt.Sprintf("all %d rostered people are accounted for by %s",
			result.Expected, result.Deadline)
		return result
	}
	names := make([]string, 0, len(result.Unaccounted))
	for _, item := range result.Unaccounted {
		names = append(names, item.PersonID)
	}
	sort.Strings(names)
	result.Explanation = fmt.Sprintf("%d of %d rostered people are unaccounted for: %s",
		len(result.Unaccounted), result.Expected, joinIDs(names))
	return result
}

// accountedFor reports whether a location proves the person is safe. Reaching the
// surface or reporting at a muster station after the order both count; a stale
// underground position recorded before the order does not.
func accountedFor(location Location, orderedAt timeutil.Stamp) bool {
	if !location.Underground {
		return true
	}
	if !location.Mustered {
		return false
	}
	return !location.At.Before(orderedAt)
}

// joinIDs renders identifiers as a stable comma separated list.
func joinIDs(ids []string) string {
	if len(ids) == 0 {
		return "none"
	}
	out := ids[0]
	for _, id := range ids[1:] {
		out += ", " + id
	}
	return out
}

// Index maps person identifiers to their last known location.
func Index(locations []Location) map[string]Location {
	out := make(map[string]Location, len(locations))
	for _, location := range locations {
		out[location.PersonID] = location
	}
	return out
}

// InArea returns the identifiers of the people currently in one area.
func InArea(locations []Location, kind model.AreaKind, areaID string) []string {
	out := make([]string, 0, 4)
	for _, location := range locations {
		if location.Underground && location.AreaKind == kind && location.AreaID == areaID {
			out = append(out, location.PersonID)
		}
	}
	sort.Strings(out)
	return out
}

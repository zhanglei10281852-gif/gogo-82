# MineGuard

MineGuard is a self-contained command line tool that reads an underground coal
mine monitoring dataset and turns it into a deterministic assessment: which
sensor readings may be trusted, what the atmosphere at each monitoring point
looks like, whether the ventilation network is conserved and adequate, which
controlled circuits may stay energised, who is underground, which high risk work
permits may be honoured, and which scheduled inspections are overdue.

It is written in Go against the standard library only, runs entirely offline, and
never reads the wall clock: the reporting instant is either supplied with
`-as-of` or derived from the newest record in the input, so two runs over the
same data produce byte-identical output.

## Scope and disclaimers

- This is an independent study and engineering-practice project. It is not
  affiliated with, endorsed by, or connected to any mine, mining company,
  regulator, standards body, equipment vendor or safety authority.
- **MineGuard is a study tool. It provides no safety, engineering or regulatory
  advice and must not be used to make real decisions about a real mine, or to
  substitute for statutory monitoring, inspection or rescue arrangements.**
- Every threshold, limit and interval in the default policy and in the example
  data is a plausible-looking placeholder chosen to exercise the code. None of
  them is taken from, or claims to represent, any real standard or regulation.
- All example data under `examples/` is entirely fictional. The mine, the
  districts, the faces, the sensors, the permits and the people are invented.
  Any resemblance to a real installation or a real person is coincidental.

## Build and test

```
go build ./...
go vet ./...
go test ./...
```

The module targets Go 1.22 and has no third-party dependencies, so
`GOFLAGS=-mod=mod GOPROXY=off go build ./...` works on a machine with no network
access.

## Container

The repository carries a multi-stage `Dockerfile`. The builder stage vets, tests
and compiles a static binary with `CGO_ENABLED=0`, and the final stage is
`scratch` carrying only the binary and the example data.

```
docker build -t mineguard:local .
docker run --rm --network none mineguard:local version
docker run --rm --network none mineguard:local report \
  -layout /examples/demo-mine/layout.json \
  -readings /examples/demo-mine/readings.jsonl \
  -events /examples/demo-mine/events.jsonl \
  -roster /examples/demo-mine/roster.json \
  -permits /examples/demo-mine/permits.json \
  -config /examples/demo-mine/config.json \
  -as-of 2026-08-15T14:00:00Z
```

## Commands

| command       | what it answers                                                     |
| ------------- | ------------------------------------------------------------------- |
| `validate`    | is this layout and its ledgers internally consistent?               |
| `ingest`      | append readings and events to a store and record the audit entry    |
| `gas`         | what does the atmosphere look like at each monitoring point?        |
| `ventilation` | are the airway flows conserved, adequate and free of recirculation? |
| `interlock`   | may each controlled circuit stay energised, and under which rule?   |
| `personnel`   | who is underground, and is the muster complete?                     |
| `permit`      | which high risk work permits may be honoured?                       |
| `inspect`     | which scheduled inspections are overdue?                            |
| `verify`      | does the store audit chain still recompute?                         |
| `report`      | every section at once                                               |

### Exit codes

The exit code is part of the contract, so the tool is usable in a shell script:

- `0` the command ran and its verdict is clear.
- `1` the command could not run: bad flags, unreadable input, an invalid policy,
  or a dataset that failed validation.
- `2` the command ran correctly and the answer is negative: a validation finding,
  a gas trip level, inadequate ventilation, a tripped circuit, an incomplete
  muster, a refused permit, an overdue inspection or a broken audit chain.

### Common flags

| flag             | meaning                                                   |
| ---------------- | --------------------------------------------------------- |
| `-layout`        | layout document (JSON)                                    |
| `-readings`      | reading ledger (JSON Lines)                               |
| `-events`        | tag event ledger (JSON Lines)                             |
| `-roster`        | shift roster (JSON)                                       |
| `-permits`       | permit set (JSON)                                         |
| `-config`        | policy document; the built-in policy is used when omitted |
| `-store`         | store directory, used instead of the individual documents |
| `-as-of`         | reporting instant, `2006-01-02T15:04:05Z`                 |
| `-evacuation-at` | instant an evacuation order was given                     |
| `-point`         | restrict the `gas` output to one monitoring point         |
| `-format`        | `text` (default) or `json`                                |
| `-out`           | write the output to a file instead of standard output     |
| `-write`         | persist the computed assessment into the store            |

## Quick start

```
go run ./cmd/mineguard validate \
  -layout examples/demo-mine/layout.json \
  -readings examples/demo-mine/readings.jsonl \
  -events examples/demo-mine/events.jsonl \
  -roster examples/demo-mine/roster.json \
  -permits examples/demo-mine/permits.json \
  -config examples/demo-mine/config.json

go run ./cmd/mineguard report \
  -layout examples/demo-mine/layout.json \
  -readings examples/demo-mine/readings.jsonl \
  -events examples/demo-mine/events.jsonl \
  -roster examples/demo-mine/roster.json \
  -permits examples/demo-mine/permits.json \
  -config examples/demo-mine/config.json \
  -as-of 2026-08-15T14:00:00Z
```

The demo dataset is deliberately clear: every point is usable, no circuit trips
and the verdict is positive. A second reading ledger and event ledger describe an
incident on the same layout, with methane climbing at the north face, the north
intake throttled and an evacuation ordered:

```
go run ./cmd/mineguard report \
  -layout examples/demo-mine/layout.json \
  -readings examples/demo-mine/readings-incident.jsonl \
  -events examples/demo-mine/events-evacuation.jsonl \
  -roster examples/demo-mine/roster.json \
  -permits examples/demo-mine/permits.json \
  -config examples/demo-mine/config.json \
  -as-of 2026-08-15T14:00:00Z \
  -evacuation-at 2026-08-15T13:20:00Z
```

## Data model

A layout is explicit rather than inferred from naming conventions:

- **districts** group **faces**.
- **junctions** are the nodes of the ventilation network; a junction may be a
  surface collar, in which case it exchanges air with the atmosphere.
- **airways** are directed branches between junctions with a length, a
  cross-section and a square-law resistance.
- **points** are monitoring points. Each names one quantity, sits in exactly one
  area (a face, an airway, a junction or the surface), carries a placement in the
  cross-section, an instrument range, an alarm pair with a direction, and a
  calibration record.
- **circuits** are controlled electrical circuits, each guarded by a list of
  monitoring points, optionally dependent on adequate ventilation.
- **inspections** are scheduled inspections of a fixed asset with an interval.

Readings and tag events are JSON Lines ledgers. Every instant in the system is an
RFC 3339 UTC timestamp with second resolution and no offset; offsets and
fractional seconds are refused at the boundary so two records describing the same
moment always carry the same bytes.

JSON decoding is strict: an unknown member is an error, a second top-level value
is an error, and an empty document is an error. A monitoring configuration that
silently ignored a renamed threshold would be worse than one that refuses to
load.

## How the assessment is computed

The stages run in a fixed order and each reads only what the previous ones
produced.

1. **Readings.** Every raw measurement is converted into a single canonical unit
   per quantity (methane and oxygen in percent by volume, carbon monoxide in
   parts per million, dust in mg/m³, temperature in °C, airflow in m³/s, pressure
   in Pa). A sample is usable only when the sensor reports healthy, the value
   sits inside the instrument range plus a configured margin, the calibration has
   not expired, the reading is recent enough, and the head is not repeating one
   value for a configured run length. Each condition is recorded separately so a
   refusal names the condition that failed.
2. **Gas.** Per point: the alarm level against the point's own pair in its own
   direction, the rate of change per hour across the rate window, whether the
   mixture sits between the lower and upper explosive limits, oxygen deficiency,
   elevated carbon monoxide, and methane layering when a roof point runs far
   above the general body of air in the same area.
3. **Ventilation.** Per airway the measured quantity, velocity and square-law
   pressure drop; per junction the mass-balance residual, with surface and
   boundary junctions exempt; per face the required quantity, taken as the
   largest of the per-person demand, the equipment demand, the policy floor and
   the face's own declared minimum; and any directed cycle whose every airway
   carries more than the recirculation tolerance.
4. **Interlock.** Per circuit an allow, warn or trip decision with the rules that
   fired and the evidence read. The rule that matters most is negative: a circuit
   is never cleared while a mandatory input is missing, refused, stale or out of
   calibration. An interlock that treated "no data" as "no gas" would be worse
   than no interlock at all.
5. **Personnel.** The tag stream is replayed in instant order up to the reporting
   instant. After an evacuation order the reported number is not how many people
   mustered but which rostered person did not, with their last known area and the
   instant it was recorded.
6. **Permits.** Each permit is cleared or refused for a stated reason: outside its
   validity window, longer than the policy allows, a required point unusable, or
   a required point above its alarm level.
7. **Inspections and timeline.** An inspection is due at its last completion plus
   its interval, and the overdue hours are reported rather than a plain boolean.
   The timeline merges the gas, interlock and inspection findings into one
   deterministic order.

An unmeasured airway keeps a quantity of zero and is reported as unmeasured
rather than assumed, and a face whose intake has no usable measurement is never
declared adequate.

## Store and audit chain

`ingest` appends to a store directory. Observations go into append-only ledgers
because a reading is a historical fact; computed documents are replaced
atomically through a temporary file and a rename, so a reader never sees half a
snapshot. Every mutation appends an audit entry whose SHA-256 covers the entry's
own fields and the previous entry's hash, which means editing or removing an
entry invalidates every entry after it. `verify` recomputes the chain and reports
integrity and chronology separately: replaying history at earlier instants leaves
the chain intact and is a note, not a failure.

## Determinism

- No stage reads the system clock.
- Every collection is put into a canonical order before it is rendered or hashed.
- Table column widths are computed from the cells, never from a terminal size or
  a locale.
- JSON is encoded with a fixed indent and without HTML escaping.
- Where two sensors could answer the same question, the lexicographically first
  point wins, so the answer never depends on map iteration order.

## Repository layout

```
cmd/mineguard          entry point
internal/cli           command surface, flags and exit codes
internal/config        policy document with defaults and range checks
internal/gas           per-point atmosphere assessment
internal/inspect       scheduled inspections and the event timeline
internal/interlock     circuit decisions
internal/model         domain types and validation
internal/permit        permit clearance
internal/personnel     occupancy and muster
internal/pipeline      stage wiring and the summary
internal/reading       measurement normalisation and validity
internal/report        deterministic text rendering
internal/store         append-only ledgers and the audit chain
internal/strictjson    the only JSON entry point
internal/timeutil      the single instant type
internal/units         unit conversion and numeric helpers
internal/ventilation   airflow network assessment
examples/demo-mine     fictional demonstration dataset
```

## Licence

No licence is granted. This repository exists for study purposes.

# MineGuard release report

## What this is

MineGuard is an independent, self-contained Go command line tool that turns an
underground coal mine monitoring dataset into a deterministic assessment. It is
a study project built from scratch for this repository; it is not derived from,
affiliated with or endorsed by any mine, company, regulator, standards body or
vendor.

**MineGuard is a study tool. It provides no safety, engineering or regulatory
advice.** Every threshold in the default policy and every value in the example
data is an invented placeholder chosen to exercise the code, and all example
data is fictional.

## Scope

| item                                | value                                                                  |
| ----------------------------------- | ---------------------------------------------------------------------- |
| language                            | Go, standard library only                                              |
| module                              | `MineGuard`, `go 1.22`                                                 |
| third-party dependencies            | none                                                                   |
| network access at build or run time | none                                                                   |
| packages                            | 16 internal packages plus one command                                  |
| production Go lines                 | 4574 (excluding `_test.go`, blank lines and comment-only lines)        |
| test Go lines                       | 3883                                                                   |
| Go files                            | 34                                                                     |
| container                           | multi-stage `Dockerfile`, `golang:1.22` builder, `scratch` final stage |

## Verification performed

All commands were run on `linux/amd64` (the host toolchain is `go1.26.1
windows/amd64`; the container builds with `golang:1.22` and `GOTOOLCHAIN=local`).

| check                  | command                                                                                                 | result                                                          |
| ---------------------- | ------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------- |
| build                  | `go build ./...`                                                                                        | pass                                                            |
| vet                    | `go vet ./...`                                                                                          | pass                                                            |
| tests                  | `go test ./...`                                                                                         | pass, every package                                             |
| formatting             | `gofmt -l internal cmd`                                                                                 | empty                                                           |
| container build        | `docker build -t mineguard:local .`                                                                     | pass, including `go vet` and `go test` inside the builder stage |
| offline container run  | `docker run --rm --network none mineguard:local version`                                                | pass                                                            |
| offline end-to-end run | `docker run --rm --network none mineguard:local report -layout /examples/… -as-of 2026-08-15T14:00:00Z` | pass, identical output to the host run                          |

## Offline smoke run

The demonstration dataset was taken through the whole command surface on the
host, with an explicit reporting instant so the run is reproducible:

| command                                         | outcome                                                                            | exit code |
| ----------------------------------------------- | ---------------------------------------------------------------------------------- | --------- |
| `validate`                                      | 19 points, 95 readings, 14 events, 2 permits, `ok: true`                           | 0         |
| `ingest -store …`                               | 95 readings and 14 events added, audit entry 1                                     | 0         |
| `verify -store …`                               | 1 entry, verified, chronological                                                   | 0         |
| `gas`                                           | every point normal, no trip level                                                  | 0         |
| `ventilation`                                   | 8 airways measured, every junction balanced, both faces adequate, no recirculation | 0         |
| `interlock`                                     | all three circuits allowed under `all_conditions_met`                              | 0         |
| `personnel`                                     | 9 underground across two faces, no order in force                                  | 0         |
| `permit`                                        | both permits cleared                                                               | 0         |
| `inspect`                                       | no inspection overdue                                                              | 0         |
| `report`                                        | positive verdict                                                                   | 0         |
| `personnel -evacuation-at 2026-08-15T13:00:00Z` | 9 of 10 unaccounted, each with a last known area                                   | 2         |

The incident dataset on the same layout (`readings-incident.jsonl` and
`events-evacuation.jsonl`) exercises the negative paths in one run: 3 points at
trip level, methane layering at the north face roof, the north intake throttled
to 8 m³/s so `F-N101` becomes inadequate, one carbon monoxide head reporting
faulty so its circuit trips on `mandatory_input_invalid`, both permits refused,
and 2 of 10 rostered people unaccounted after the evacuation order. `report`
exits 2.

## Test coverage by area

| package       | what the tests pin down                                                                                                                                                                                                                                                                                           |
| ------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `units`       | conversions per quantity, cross-quantity refusals, half-away-from-zero rounding with no negative zero, safe division                                                                                                                                                                                              |
| `timeutil`    | the instant layout is exact (offsets and fractional seconds refused), JSON round trip, half-open windows, sorting                                                                                                                                                                                                 |
| `strictjson`  | empty documents, unknown members, byte order marks, JSON Lines line numbering, stable encoding                                                                                                                                                                                                                    |
| `config`      | the built-in policy validates, partial documents keep defaults, renamed sections are refused, every range check names its field, fingerprint stability                                                                                                                                                            |
| `model`       | layout consistency: dangling guard points, duplicate identifiers, self-looping airways, alarm pairs ordered by direction and inside the instrument range, reading and permit validation, total orders                                                                                                             |
| `reading`     | each refusal condition separately (status, range plus margin, calibration expiry, span drift, staleness, frozen run, unit mismatch), latest-sample selection                                                                                                                                                      |
| `gas`         | alarm levels in both directions, rate of rise measured per hour and gated on sample count, analysis window bounds, explosive range, oxygen deficiency, carbon monoxide, layering including the case where the general body reading is refused                                                                     |
| `ventilation` | square-law pressure drop, series and parallel resistance, the local square root against `math.Sqrt`, junction balance, boundary junctions, unmeasured airways never assumed, face demand as the largest claim, recirculation detection and its tolerance, order independence                                      |
| `interlock`   | clear conditions allow; missing, unknown, refused and stale mandatory inputs all trip; the non-mandatory allowance and the freshness override; gas trip and warn under both warn policies; rate of rise alone warns; inadequate ventilation and recirculation trip ventilation-dependent circuits                 |
| `personnel`   | entry, move and exit; events after the reporting instant ignored; unrostered tags raise issues; occupancy limits; muster completeness, including a muster recorded before the order and a person never tagged                                                                                                     |
| `permit`      | cleared, pending, expired, too long, required point unusable or in alarm or missing, open windows                                                                                                                                                                                                                 |
| `store`       | ledger idempotence on identifiers, atomic replacement leaving no temporary files, audit chain linking, edited and removed entries both detected, out-of-order instants noted rather than failed, hash sensitivity to every field, snapshot digests, metadata schema guard                                         |
| `pipeline`    | every stage populated, a clear mine is safe, gas trips and inadequate ventilation propagate to the interlock and the summary, a refused mandatory head trips without inventing gas data, `as-of` derivation, byte-identical reruns, issue collection order                                                        |
| `report`      | column widths from the widest cell, no trailing padding, every summary line, tripped and refused lists, timeline and issue fallbacks, byte-identical renderings, audit text                                                                                                                                       |
| `cli`         | exit code contract across `0`, `1` and `2`; unknown commands and flags; surplus arguments; malformed instants and policies; `validate` versus analysis on an unsound bundle; JSON output; `-out`; ingest idempotence; store-backed reporting; snapshot persistence; audit verification before and after tampering |

## Determinism notes

No stage reads the system clock: the reporting instant comes from `-as-of` or
from the newest record in the input. Every collection is ordered canonically
before rendering or hashing, table widths derive from the cells rather than the
terminal, JSON is encoded with a fixed indent and without HTML escaping, and
where two sensors could answer the same question the lexicographically first
point wins. `pipeline` and `report` both have tests asserting byte-identical
reruns.

## Known limitations

- The ventilation model checks conservation, adequacy and recirculation from
  measured quantities. It does not solve the network from fan pressures and
  resistances, so `fan_pressure_pa` and the series and parallel resistance
  helpers are descriptive rather than part of the verdict.
- Dust is a supported quantity with unit conversion and threshold handling, but
  no dedicated qualitative flag of its own.
- The store is a directory of plain files with an append-only audit chain. It has
  no locking, so concurrent writers are out of scope.
- Layering is detected between a roof point and a general body point in the same
  area. It does not attempt to infer layering from a single point's history.

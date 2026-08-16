package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// layoutDocument is one face on one airway, guarded by one circuit.
const layoutDocument = `{
  "schema_version": "mineguard/v1",
  "mine_id": "MG-TEST",
  "label": "fictional test mine",
  "junctions": [
    {"junction_id": "J-IN", "surface": true},
    {"junction_id": "J-FACE"},
    {"junction_id": "J-OUT", "surface": true}
  ],
  "airways": [
    {"airway_id": "A-IN", "from_junction": "J-IN", "to_junction": "J-FACE",
     "length_m": 200, "area_m2": 12, "resistance_ns2m8": 0.01},
    {"airway_id": "A-OUT", "from_junction": "J-FACE", "to_junction": "J-OUT",
     "length_m": 200, "area_m2": 12, "resistance_ns2m8": 0.01}
  ],
  "faces": [
    {"face_id": "F-1", "label": "test face", "intake_junction": "J-FACE",
     "return_junction": "J-OUT", "planned_persons": 6, "equipment_kw": 100}
  ],
  "points": [
    {"point_id": "P-CH4", "label": "face methane", "quantity": "methane",
     "area_kind": "working_face", "area_id": "F-1", "placement": "general_body",
     "range_low": 0, "range_high": 5,
     "threshold": {"warn": 1, "trip": 1.5, "direction": "above"},
     "calibration": {"at": "2026-08-10T06:00:00Z", "valid_days": 30},
     "mandatory": true, "stale_minutes": 240},
    {"point_id": "P-FLOW", "label": "intake airflow", "quantity": "airflow",
     "area_kind": "airway", "area_id": "A-IN", "placement": "intake",
     "range_low": 0, "range_high": 80,
     "threshold": {"warn": 8, "trip": 5, "direction": "below"},
     "calibration": {"at": "2026-08-10T06:00:00Z", "valid_days": 30},
     "mandatory": true, "stale_minutes": 240},
    {"point_id": "P-FLOW-OUT", "label": "return airflow", "quantity": "airflow",
     "area_kind": "airway", "area_id": "A-OUT", "placement": "return",
     "range_low": 0, "range_high": 80,
     "threshold": {"warn": 8, "trip": 5, "direction": "below"},
     "calibration": {"at": "2026-08-10T06:00:00Z", "valid_days": 30},
     "mandatory": true, "stale_minutes": 240}
  ],
  "circuits": [
    {"circuit_id": "C-1", "label": "shearer", "area_kind": "working_face",
     "area_id": "F-1", "guard_points": ["P-CH4"], "requires_ventilation": true}
  ]
}
`

const rosterDocument = `[{"person_id": "W-1", "name": "worker one"}]` + "\n"

const eventsDocument = `{"event_id":"E-1","person_id":"W-1","at":"2026-08-15T06:00:00Z","action":"enter","area_kind":"working_face","area_id":"F-1"}
`

// readingsFor builds a three sample ledger at one methane concentration.
func readingsFor(methane string) string {
	var builder strings.Builder
	for _, at := range []string{"2026-08-15T13:45:00Z", "2026-08-15T13:50:00Z", "2026-08-15T13:55:00Z"} {
		builder.WriteString(`{"reading_id":"R-CH4-` + at + `","point_id":"P-CH4","at":"` + at +
			`","value":` + methane + `,"unit":"pct","status":"ok"}` + "\n")
		builder.WriteString(`{"reading_id":"R-IN-` + at + `","point_id":"P-FLOW","at":"` + at +
			`","value":20,"unit":"m3/s","status":"ok"}` + "\n")
		builder.WriteString(`{"reading_id":"R-OUT-` + at + `","point_id":"P-FLOW-OUT","at":"` + at +
			`","value":20,"unit":"m3/s","status":"ok"}` + "\n")
	}
	return builder.String()
}

type workspace struct {
	dir      string
	layout   string
	readings string
	events   string
	roster   string
}

func setup(t *testing.T, methane string) workspace {
	t.Helper()
	dir := t.TempDir()
	space := workspace{
		dir:      dir,
		layout:   filepath.Join(dir, "layout.json"),
		readings: filepath.Join(dir, "readings.jsonl"),
		events:   filepath.Join(dir, "events.jsonl"),
		roster:   filepath.Join(dir, "roster.json"),
	}
	for path, body := range map[string]string{
		space.layout:   layoutDocument,
		space.readings: readingsFor(methane),
		space.events:   eventsDocument,
		space.roster:   rosterDocument,
	} {
		if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return space
}

// inputs are the flags naming every document in the workspace.
func (w workspace) inputs() []string {
	return []string{
		"-layout", w.layout, "-readings", w.readings,
		"-events", w.events, "-roster", w.roster,
		"-as-of", "2026-08-15T14:00:00Z",
	}
}

func invoke(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestNoArgumentsFails(t *testing.T) {
	code, _, stderr := invoke()
	if code != ExitFailure {
		t.Fatalf("exit = %d, want %d", code, ExitFailure)
	}
	if !strings.Contains(stderr, "usage:") {
		t.Errorf("stderr must carry the usage:\n%s", stderr)
	}
}

func TestVersionAndHelpSucceed(t *testing.T) {
	for _, name := range []string{"version", "--version", "-v"} {
		code, stdout, _ := invoke(name)
		if code != ExitOK {
			t.Errorf("%s exited %d", name, code)
		}
		if !strings.Contains(stdout, Version) {
			t.Errorf("%s printed %q", name, stdout)
		}
		if !strings.Contains(stdout, "study tool only") {
			t.Errorf("%s must state the scope of the tool", name)
		}
	}
	for _, name := range []string{"help", "--help", "-h"} {
		code, stdout, _ := invoke(name)
		if code != ExitOK || !strings.Contains(stdout, "usage:") {
			t.Errorf("%s exited %d with %q", name, code, stdout)
		}
	}
}

func TestUnknownCommandFails(t *testing.T) {
	code, _, stderr := invoke("excavate")
	if code != ExitFailure {
		t.Fatalf("exit = %d, want %d", code, ExitFailure)
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestUnknownFlagFails(t *testing.T) {
	space := setup(t, "0.41")
	code, _, stderr := invoke(append([]string{"validate", "-nope"}, space.inputs()...)...)
	if code != ExitFailure {
		t.Fatalf("exit = %d, want %d", code, ExitFailure)
	}
	if stderr == "" {
		t.Error("a rejected flag must be explained")
	}
}

func TestSurplusArgumentFails(t *testing.T) {
	space := setup(t, "0.41")
	code, _, stderr := invoke(append(append([]string{"validate"}, space.inputs()...), "extra")...)
	if code != ExitFailure {
		t.Fatalf("exit = %d, want %d", code, ExitFailure)
	}
	if !strings.Contains(stderr, "unexpected argument") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestUnsupportedFormatFails(t *testing.T) {
	space := setup(t, "0.41")
	code, _, _ := invoke(append([]string{"validate", "-format", "yaml"}, space.inputs()...)...)
	if code != ExitFailure {
		t.Fatalf("exit = %d, want %d", code, ExitFailure)
	}
}

func TestWithoutLayoutOrStoreTheCommandFails(t *testing.T) {
	code, _, stderr := invoke("validate")
	if code != ExitFailure {
		t.Fatalf("exit = %d, want %d", code, ExitFailure)
	}
	if !strings.Contains(stderr, "-layout") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestValidateAcceptsASoundBundle(t *testing.T) {
	space := setup(t, "0.41")
	code, stdout, stderr := invoke(append([]string{"validate"}, space.inputs()...)...)
	if code != ExitOK {
		t.Fatalf("exit = %d (%s)", code, stderr)
	}
	if !strings.Contains(stdout, "ok: true") {
		t.Errorf("stdout = \n%s", stdout)
	}
}

func TestValidateReportsAnUnsoundBundle(t *testing.T) {
	space := setup(t, "0.41")
	broken := strings.Replace(layoutDocument, `"guard_points": ["P-CH4"]`, `"guard_points": ["P-GHOST"]`, 1)
	if err := os.WriteFile(space.layout, []byte(broken), 0o640); err != nil {
		t.Fatalf("write: %v", err)
	}
	code, stdout, _ := invoke(append([]string{"validate"}, space.inputs()...)...)
	if code != ExitVerdict {
		t.Fatalf("exit = %d, want %d", code, ExitVerdict)
	}
	if !strings.Contains(stdout, "ok: false") {
		t.Errorf("stdout = \n%s", stdout)
	}
}

func TestAnalysisRefusesToRunOnAnUnsoundBundle(t *testing.T) {
	space := setup(t, "0.41")
	broken := strings.Replace(layoutDocument, `"guard_points": ["P-CH4"]`, `"guard_points": ["P-GHOST"]`, 1)
	if err := os.WriteFile(space.layout, []byte(broken), 0o640); err != nil {
		t.Fatalf("write: %v", err)
	}
	code, _, stderr := invoke(append([]string{"report"}, space.inputs()...)...)
	if code != ExitFailure {
		t.Fatalf("exit = %d, want %d", code, ExitFailure)
	}
	if !strings.Contains(stderr, "input is invalid") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestReportOnAClearMineSucceeds(t *testing.T) {
	space := setup(t, "0.41")
	code, stdout, stderr := invoke(append([]string{"report"}, space.inputs()...)...)
	if code != ExitOK {
		t.Fatalf("exit = %d (%s)", code, stderr)
	}
	if !strings.Contains(stdout, "mine: MG-TEST") {
		t.Errorf("stdout = \n%s", stdout)
	}
}

func TestReportOnATrippedMineReturnsTheVerdictCode(t *testing.T) {
	space := setup(t, "1.8")
	code, stdout, _ := invoke(append([]string{"report"}, space.inputs()...)...)
	if code != ExitVerdict {
		t.Fatalf("exit = %d, want %d", code, ExitVerdict)
	}
	if !strings.Contains(stdout, "tripped: C-1") {
		t.Errorf("stdout = \n%s", stdout)
	}
}

func TestGasSubcommandReturnsTheVerdictCodeOnATrip(t *testing.T) {
	space := setup(t, "1.8")
	code, stdout, _ := invoke(append([]string{"gas"}, space.inputs()...)...)
	if code != ExitVerdict {
		t.Fatalf("exit = %d, want %d", code, ExitVerdict)
	}
	if !strings.Contains(stdout, "P-CH4") {
		t.Errorf("stdout = \n%s", stdout)
	}
}

func TestGasSubcommandCanFocusOnOnePoint(t *testing.T) {
	space := setup(t, "0.41")
	code, stdout, stderr := invoke(append([]string{"gas", "-point", "P-CH4"}, space.inputs()...)...)
	if code != ExitOK {
		t.Fatalf("exit = %d (%s)", code, stderr)
	}
	if !strings.Contains(stdout, "point: P-CH4") {
		t.Errorf("stdout = \n%s", stdout)
	}
}

func TestInterlockSubcommandReturnsTheVerdictCodeOnAGasTrip(t *testing.T) {
	space := setup(t, "1.8")
	code, stdout, _ := invoke(append([]string{"interlock"}, space.inputs()...)...)
	if code != ExitVerdict {
		t.Fatalf("exit = %d, want %d", code, ExitVerdict)
	}
	if !strings.Contains(stdout, "C-1") {
		t.Errorf("stdout = \n%s", stdout)
	}
}

func TestEverySectionSubcommandRuns(t *testing.T) {
	space := setup(t, "0.41")
	for _, command := range []string{"gas", "ventilation", "interlock", "personnel", "permit", "inspect"} {
		code, stdout, stderr := invoke(append([]string{command}, space.inputs()...)...)
		if code != ExitOK {
			t.Errorf("%s exited %d (%s)", command, code, stderr)
		}
		if stdout == "" {
			t.Errorf("%s printed nothing", command)
		}
	}
}

func TestJSONFormatIsMachineReadable(t *testing.T) {
	space := setup(t, "0.41")
	code, stdout, stderr := invoke(append([]string{"gas", "-format", "json"}, space.inputs()...)...)
	if code != ExitOK {
		t.Fatalf("exit = %d (%s)", code, stderr)
	}
	var payload struct {
		WorstLevel  string `json:"worst_level"`
		Assessments []struct {
			PointID string `json:"point_id"`
		} `json:"assessments"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if payload.WorstLevel != "normal" || len(payload.Assessments) != 3 {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestOutFlagWritesToAFile(t *testing.T) {
	space := setup(t, "0.41")
	target := filepath.Join(space.dir, "out.txt")
	code, stdout, stderr := invoke(append([]string{"report", "-out", target}, space.inputs()...)...)
	if code != ExitOK {
		t.Fatalf("exit = %d (%s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout must stay empty when -out is given, got %q", stdout)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "mine: MG-TEST") {
		t.Errorf("file = \n%s", data)
	}
}

func TestIngestThenAssessFromTheStore(t *testing.T) {
	space := setup(t, "0.41")
	storePath := filepath.Join(space.dir, "session")
	code, stdout, stderr := invoke(append([]string{"ingest", "-store", storePath}, space.inputs()...)...)
	if code != ExitOK {
		t.Fatalf("ingest exited %d (%s)", code, stderr)
	}
	if !strings.Contains(stdout, "readings added 9") {
		t.Errorf("ingest said:\n%s", stdout)
	}
	code, stdout, stderr = invoke("ingest", "-store", storePath,
		"-layout", space.layout, "-readings", space.readings,
		"-events", space.events, "-roster", space.roster)
	if code != ExitOK {
		t.Fatalf("second ingest exited %d (%s)", code, stderr)
	}
	if !strings.Contains(stdout, "readings added 0") || !strings.Contains(stdout, "skipped 9") {
		t.Errorf("a repeated ingest must skip every record:\n%s", stdout)
	}
	code, stdout, stderr = invoke("report", "-store", storePath, "-as-of", "2026-08-15T14:00:00Z")
	if code != ExitOK {
		t.Fatalf("report from the store exited %d (%s)", code, stderr)
	}
	if !strings.Contains(stdout, "mine: MG-TEST") {
		t.Errorf("stdout = \n%s", stdout)
	}
}

func TestVerifyAcceptsAnIntactStoreAndRejectsAnEditedOne(t *testing.T) {
	space := setup(t, "0.41")
	storePath := filepath.Join(space.dir, "session")
	if code, _, stderr := invoke(append([]string{"ingest", "-store", storePath}, space.inputs()...)...); code != ExitOK {
		t.Fatalf("ingest exited %d (%s)", code, stderr)
	}
	code, stdout, stderr := invoke("verify", "-store", storePath)
	if code != ExitOK {
		t.Fatalf("verify exited %d (%s)", code, stderr)
	}
	if !strings.Contains(stdout, "verified: true") {
		t.Errorf("stdout = \n%s", stdout)
	}
	auditPath := filepath.Join(storePath, "audit.jsonl")
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	tampered := strings.Replace(string(data), `"records":10`, `"records":99`, 1)
	if tampered == string(data) {
		t.Fatalf("the audit entry did not carry the expected record count:\n%s", data)
	}
	if err := os.WriteFile(auditPath, []byte(tampered), 0o640); err != nil {
		t.Fatalf("write audit: %v", err)
	}
	code, stdout, _ = invoke("verify", "-store", storePath)
	if code != ExitVerdict {
		t.Fatalf("verify exited %d, want %d", code, ExitVerdict)
	}
	if !strings.Contains(stdout, "verified: false") {
		t.Errorf("stdout = \n%s", stdout)
	}
}

func TestVerifyRequiresAStore(t *testing.T) {
	code, _, stderr := invoke("verify")
	if code != ExitFailure {
		t.Fatalf("exit = %d, want %d", code, ExitFailure)
	}
	if !strings.Contains(stderr, "-store") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestWriteFlagPersistsASnapshot(t *testing.T) {
	space := setup(t, "0.41")
	storePath := filepath.Join(space.dir, "session")
	if code, _, stderr := invoke(append([]string{"ingest", "-store", storePath}, space.inputs()...)...); code != ExitOK {
		t.Fatalf("ingest exited %d (%s)", code, stderr)
	}
	code, _, stderr := invoke("report", "-store", storePath, "-write", "-as-of", "2026-08-15T14:00:00Z")
	if code != ExitOK {
		t.Fatalf("report exited %d (%s)", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(storePath, "assessment.json")); err != nil {
		t.Fatalf("the snapshot was not written: %v", err)
	}
	code, stdout, _ := invoke("verify", "-store", storePath)
	if code != ExitOK || !strings.Contains(stdout, "audit entries: 2") {
		t.Fatalf("verify exited %d with:\n%s", code, stdout)
	}
}

func TestWriteWithoutAStoreFails(t *testing.T) {
	space := setup(t, "0.41")
	code, _, stderr := invoke(append([]string{"report", "-write"}, space.inputs()...)...)
	if code != ExitFailure {
		t.Fatalf("exit = %d, want %d", code, ExitFailure)
	}
	if !strings.Contains(stderr, "-store") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestMalformedInstantFlagsFail(t *testing.T) {
	space := setup(t, "0.41")
	for _, flag := range []string{"-as-of", "-evacuation-at"} {
		args := append([]string{"report"}, space.inputs()...)
		args = append(args, flag, "15 August 2026")
		code, _, stderr := invoke(args...)
		if code != ExitFailure {
			t.Errorf("%s accepted a malformed instant (exit %d)", flag, code)
		}
		if stderr == "" {
			t.Errorf("%s must explain the refusal", flag)
		}
	}
}

func TestMissingInputFileFails(t *testing.T) {
	space := setup(t, "0.41")
	code, _, stderr := invoke("validate", "-layout", filepath.Join(space.dir, "absent.json"))
	if code != ExitFailure {
		t.Fatalf("exit = %d, want %d", code, ExitFailure)
	}
	if stderr == "" {
		t.Error("a missing input file must be explained")
	}
}

func TestMalformedPolicyFails(t *testing.T) {
	space := setup(t, "0.41")
	policy := filepath.Join(space.dir, "policy.json")
	if err := os.WriteFile(policy, []byte(`{"label":"x","gas":{"lower_explosive_limit_pct":-1}}`), 0o640); err != nil {
		t.Fatalf("write: %v", err)
	}
	args := append([]string{"report", "-config", policy}, space.inputs()...)
	code, _, stderr := invoke(args...)
	if code != ExitFailure {
		t.Fatalf("exit = %d, want %d", code, ExitFailure)
	}
	if !strings.Contains(stderr, "configuration") {
		t.Errorf("stderr = %q", stderr)
	}
}

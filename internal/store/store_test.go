package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"MineGuard/internal/model"
	"MineGuard/internal/timeutil"
	"MineGuard/internal/units"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	backend, err := Open(filepath.Join(t.TempDir(), "session"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return backend
}

func at(text string) timeutil.Stamp { return timeutil.MustParse(text) }

func layout() model.Layout {
	return model.Layout{
		SchemaVersion: model.SchemaVersion,
		MineID:        "MG-TEST",
		Junctions:     []model.Junction{{JunctionID: "J-1"}, {JunctionID: "J-2"}},
		Faces:         []model.Face{{FaceID: "F-1", IntakeJunction: "J-1", ReturnJunction: "J-2"}},
		Points: []model.Point{{
			PointID: "P-CH4", Quantity: units.QuantityMethane, AreaKind: model.AreaWorkingFace,
			AreaID: "F-1", Placement: model.PlacementGeneralBody, RangeLow: 0, RangeHigh: 5,
			Threshold:   model.Threshold{Warn: 1, Trip: 1.5, Direction: "above"},
			Calibration: model.Calibration{At: at("2026-08-10T06:00:00Z"), ValidDays: 30},
		}},
	}
}

func readings(ids ...string) []model.Reading {
	out := make([]model.Reading, 0, len(ids))
	for index, id := range ids {
		out = append(out, model.Reading{
			ReadingID: id, PointID: "P-CH4",
			At:    at("2026-08-15T13:00:00Z").AddMinutes(float64(index * 5)),
			Value: 0.4, Unit: "pct", Status: model.StatusOK,
		})
	}
	return out
}

func TestOpenRefusesAnEmptyPath(t *testing.T) {
	if _, err := Open("   "); err == nil {
		t.Fatal("an empty store path must be refused")
	}
}

func TestLayoutRoundTrips(t *testing.T) {
	backend := openTemp(t)
	if err := backend.SaveLayout(layout()); err != nil {
		t.Fatalf("SaveLayout: %v", err)
	}
	loaded, err := backend.LoadLayout()
	if err != nil {
		t.Fatalf("LoadLayout: %v", err)
	}
	if loaded.MineID != "MG-TEST" || len(loaded.Points) != 1 {
		t.Fatalf("loaded layout = %+v", loaded)
	}
}

func TestMissingOptionalDocumentsLoadEmpty(t *testing.T) {
	backend := openTemp(t)
	roster, err := backend.LoadRoster()
	if err != nil || len(roster) != 0 {
		t.Fatalf("LoadRoster on a fresh store = %v, %v", roster, err)
	}
	permits, err := backend.LoadPermits()
	if err != nil || len(permits) != 0 {
		t.Fatalf("LoadPermits on a fresh store = %v, %v", permits, err)
	}
	items, err := backend.LoadReadings()
	if err != nil || len(items) != 0 {
		t.Fatalf("LoadReadings on a fresh store = %v, %v", items, err)
	}
}

func TestAppendReadingsIsIdempotentOnIdentifiers(t *testing.T) {
	backend := openTemp(t)
	added, skipped, err := backend.AppendReadings(readings("R-1", "R-2", "R-3"))
	if err != nil || added != 3 || skipped != 0 {
		t.Fatalf("first append = %d added, %d skipped, err %v", added, skipped, err)
	}
	added, skipped, err = backend.AppendReadings(readings("R-1", "R-2", "R-3", "R-4"))
	if err != nil {
		t.Fatalf("second append: %v", err)
	}
	if added != 1 || skipped != 3 {
		t.Fatalf("second append = %d added, %d skipped, want 1 and 3", added, skipped)
	}
	stored, err := backend.LoadReadings()
	if err != nil {
		t.Fatalf("LoadReadings: %v", err)
	}
	if len(stored) != 4 {
		t.Fatalf("stored readings = %d, want 4", len(stored))
	}
}

func TestAppendEventsSkipsDuplicates(t *testing.T) {
	backend := openTemp(t)
	events := []model.TagEvent{
		{EventID: "E-1", PersonID: "W-1", At: at("2026-08-15T06:00:00Z"), Action: model.ActionEnter,
			AreaKind: model.AreaWorkingFace, AreaID: "F-1"},
	}
	if _, _, err := backend.AppendEvents(events); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}
	added, skipped, err := backend.AppendEvents(events)
	if err != nil || added != 0 || skipped != 1 {
		t.Fatalf("re-append = %d added, %d skipped, err %v", added, skipped, err)
	}
}

func TestWriteAtomicReplacesTheWholeFile(t *testing.T) {
	backend := openTemp(t)
	if err := backend.WriteAtomic("probe.json", []byte("{\"a\":1}\n")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := backend.WriteAtomic("probe.json", []byte("{}\n")); err != nil {
		t.Fatalf("second write: %v", err)
	}
	data, err := os.ReadFile(backend.Path("probe.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "{}\n" {
		t.Fatalf("file holds %q, want the replacement", data)
	}
	entries, err := os.ReadDir(backend.Root)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Errorf("temporary file %s was left behind", entry.Name())
		}
	}
}

func TestAuditChainLinksEveryEntry(t *testing.T) {
	backend := openTemp(t)
	first, err := backend.Record(at("2026-08-15T13:00:00Z"), "ingest", "MG-TEST", 3, []byte("a"))
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if first.Sequence != 1 || first.Previous != GenesisHash {
		t.Fatalf("first entry = %+v", first)
	}
	second, err := backend.Record(at("2026-08-15T13:30:00Z"), "assess", "MG-TEST", 1, []byte("b"))
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if second.Previous != first.Hash {
		t.Fatalf("second entry does not link to the first: %+v", second)
	}
	report, err := backend.VerifyAudit()
	if err != nil {
		t.Fatalf("VerifyAudit: %v", err)
	}
	if !report.Verified || report.Entries != 2 {
		t.Fatalf("report = %+v", report)
	}
	if report.Head != second.Hash {
		t.Errorf("head = %s, want %s", report.Head, second.Hash)
	}
	if !report.Chronological {
		t.Error("entries recorded in order are chronological")
	}
}

func TestEditingAnEntryBreaksTheChain(t *testing.T) {
	backend := openTemp(t)
	for _, payload := range []string{"a", "b", "c"} {
		if _, err := backend.Record(at("2026-08-15T13:00:00Z"), "ingest", "MG-TEST", 1, []byte(payload)); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	path := backend.Path(AuditFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	tampered := strings.Replace(string(data), "\"records\":1", "\"records\":99", 1)
	if tampered == string(data) {
		t.Fatal("the audit file did not contain the expected field")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o640); err != nil {
		t.Fatalf("write audit: %v", err)
	}
	report, err := backend.VerifyAudit()
	if err != nil {
		t.Fatalf("VerifyAudit: %v", err)
	}
	if report.Verified {
		t.Fatal("an edited entry must invalidate the chain")
	}
	if report.BrokenAt != 1 {
		t.Errorf("broken at = %d, want the edited entry", report.BrokenAt)
	}
	if len(report.Problems) == 0 {
		t.Error("a broken chain must state the problem")
	}
	if report.Head != "" {
		t.Errorf("a broken chain has no head, got %q", report.Head)
	}
}

func TestRemovingAnEntryBreaksTheChain(t *testing.T) {
	backend := openTemp(t)
	for _, payload := range []string{"a", "b", "c"} {
		if _, err := backend.Record(at("2026-08-15T13:00:00Z"), "ingest", "MG-TEST", 1, []byte(payload)); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	path := backend.Path(AuditFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	kept := append([]string{lines[0]}, lines[2])
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0o640); err != nil {
		t.Fatalf("write audit: %v", err)
	}
	report, err := backend.VerifyAudit()
	if err != nil {
		t.Fatalf("VerifyAudit: %v", err)
	}
	if report.Verified {
		t.Fatal("dropping the middle entry must invalidate the chain")
	}
}

func TestOutOfOrderInstantsAreANoteNotAFailure(t *testing.T) {
	backend := openTemp(t)
	if _, err := backend.Record(at("2026-08-15T13:30:00Z"), "ingest", "MG-TEST", 1, []byte("a")); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := backend.Record(at("2026-08-15T12:00:00Z"), "ingest", "MG-TEST", 1, []byte("b")); err != nil {
		t.Fatalf("Record: %v", err)
	}
	report, err := backend.VerifyAudit()
	if err != nil {
		t.Fatalf("VerifyAudit: %v", err)
	}
	if !report.Verified {
		t.Fatal("replaying history at an earlier instant leaves the chain intact")
	}
	if report.Chronological {
		t.Fatal("the report must say the instants are out of order")
	}
	if len(report.Notes) == 0 {
		t.Error("an out of order instant must be noted")
	}
}

func TestFreshStoreVerifiesAsAnEmptyChain(t *testing.T) {
	report, err := openTemp(t).VerifyAudit()
	if err != nil {
		t.Fatalf("VerifyAudit: %v", err)
	}
	if !report.Verified || report.Entries != 0 || report.Head != GenesisHash {
		t.Fatalf("report = %+v", report)
	}
}

func TestEntryHashCoversEveryField(t *testing.T) {
	base := AuditEntry{
		Sequence: 1, At: at("2026-08-15T13:00:00Z"), Action: "ingest",
		Subject: "MG-TEST", Records: 3, Payload: HashPayload([]byte("a")), Previous: GenesisHash,
	}
	base.Hash = base.ComputeHash()
	for name, mutate := range map[string]func(AuditEntry) AuditEntry{
		"sequence": func(e AuditEntry) AuditEntry { e.Sequence = 2; return e },
		"instant":  func(e AuditEntry) AuditEntry { e.At = at("2026-08-15T13:00:01Z"); return e },
		"action":   func(e AuditEntry) AuditEntry { e.Action = "assess"; return e },
		"subject":  func(e AuditEntry) AuditEntry { e.Subject = "OTHER"; return e },
		"records":  func(e AuditEntry) AuditEntry { e.Records = 4; return e },
		"payload":  func(e AuditEntry) AuditEntry { e.Payload = HashPayload([]byte("b")); return e },
		"previous": func(e AuditEntry) AuditEntry { e.Previous = base.Hash; return e },
	} {
		if mutate(base).ComputeHash() == base.Hash {
			t.Errorf("changing the %s field did not change the hash", name)
		}
	}
}

func TestSnapshotDigestTracksTheStoredDocument(t *testing.T) {
	backend := openTemp(t)
	if _, present, err := backend.SnapshotDigest(); err != nil || present {
		t.Fatalf("a fresh store has no snapshot, got present=%t err=%v", present, err)
	}
	digest, err := backend.SaveSnapshot(map[string]any{"safe": true})
	if err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	stored, present, err := backend.SnapshotDigest()
	if err != nil || !present {
		t.Fatalf("SnapshotDigest = %q, %t, %v", stored, present, err)
	}
	if stored != digest {
		t.Fatalf("digest drifted: %s and %s", stored, digest)
	}
	changed, err := backend.SaveSnapshot(map[string]any{"safe": false})
	if err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	if changed == digest {
		t.Error("a different snapshot must produce a different digest")
	}
}

func TestMetaRoundTripsAndRejectsAForeignSchema(t *testing.T) {
	backend := openTemp(t)
	meta, err := backend.LoadMeta()
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if meta.AuditHead != GenesisHash {
		t.Fatalf("a fresh store starts at the genesis hash, got %s", meta.AuditHead)
	}
	meta.MineID = "MG-TEST"
	meta.Readings = 4
	if err := backend.SaveMeta(meta); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}
	loaded, err := backend.LoadMeta()
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if loaded.Schema != MetaSchema || loaded.Readings != 4 {
		t.Fatalf("loaded meta = %+v", loaded)
	}
	if err := backend.WriteAtomic(MetaFile, []byte("{\"schema\":\"other/v9\"}\n")); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	if _, err := backend.LoadMeta(); err == nil {
		t.Error("a foreign metadata schema must be refused")
	}
}

func TestListReportsOnlyThePresentFiles(t *testing.T) {
	backend := openTemp(t)
	if got := backend.List(); len(got) != 0 {
		t.Fatalf("a fresh store holds no files, got %v", got)
	}
	if err := backend.SaveLayout(layout()); err != nil {
		t.Fatalf("SaveLayout: %v", err)
	}
	if _, _, err := backend.AppendReadings(readings("R-1")); err != nil {
		t.Fatalf("AppendReadings: %v", err)
	}
	got := backend.List()
	if len(got) != 2 || got[0] != LayoutFile || got[1] != ReadingsFile {
		t.Fatalf("List = %v, want the layout and the readings", got)
	}
}

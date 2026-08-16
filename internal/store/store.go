// Package store persists a MineGuard session.
//
// Observations go into append-only ledgers because a reading is a historical
// fact. Computed documents are replaced atomically, so a reader never sees half a
// snapshot. Every mutation appends an audit entry whose hash covers the entry's
// own fields and the previous entry's hash, which is what makes an edited entry
// invalidate every entry after it.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"MineGuard/internal/model"
	"MineGuard/internal/strictjson"
	"MineGuard/internal/timeutil"
)

// File names inside a store.
const (
	LayoutFile   = "layout.json"
	ReadingsFile = "readings.jsonl"
	EventsFile   = "events.jsonl"
	RosterFile   = "roster.json"
	PermitsFile  = "permits.json"
	SnapshotFile = "assessment.json"
	MetaFile     = "meta.json"
	AuditFile    = "audit.jsonl"
)

// GenesisHash is the previous hash of the first audit entry.
const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// MetaSchema tags the metadata document.
const MetaSchema = "mineguard-store/v1"

// Meta is the store bookkeeping document.
type Meta struct {
	Schema       string         `json:"schema"`
	MineID       string         `json:"mine_id"`
	Readings     int            `json:"readings"`
	Events       int            `json:"events"`
	Snapshots    int            `json:"snapshots"`
	AuditEntries int            `json:"audit_entries"`
	AuditHead    string         `json:"audit_head"`
	SnapshotHash string         `json:"snapshot_sha256,omitempty"`
	LastEventAt  timeutil.Stamp `json:"last_event_at,omitempty"`
	Fingerprint  string         `json:"policy_fingerprint,omitempty"`
}

// AuditEntry is one link in the hash chain.
type AuditEntry struct {
	Sequence int            `json:"sequence"`
	At       timeutil.Stamp `json:"at"`
	Action   string         `json:"action"`
	Subject  string         `json:"subject"`
	Records  int            `json:"records"`
	Payload  string         `json:"payload_sha256"`
	Previous string         `json:"previous_sha256"`
	Hash     string         `json:"sha256"`
}

// Canonical renders the pre-image the entry hash covers.
func (e AuditEntry) Canonical() string {
	return strings.Join([]string{
		strconv.Itoa(e.Sequence),
		e.At.String(),
		e.Action,
		e.Subject,
		strconv.Itoa(e.Records),
		e.Payload,
		e.Previous,
	}, "\n")
}

// ComputeHash returns the digest of the entry's own fields.
func (e AuditEntry) ComputeHash() string {
	sum := sha256.Sum256([]byte(e.Canonical()))
	return hex.EncodeToString(sum[:])
}

// AuditReport is the verdict of a chain verification.
type AuditReport struct {
	Entries       int      `json:"entries"`
	Verified      bool     `json:"verified"`
	Chronological bool     `json:"chronological"`
	Head          string   `json:"head"`
	BrokenAt      int      `json:"broken_at,omitempty"`
	Problems      []string `json:"problems,omitempty"`
	Notes         []string `json:"notes,omitempty"`
}

// Store is a directory holding one session.
type Store struct{ Root string }

// Open prepares a store directory.
func Open(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("store path is empty")
	}
	clean := filepath.Clean(root)
	if err := os.MkdirAll(clean, 0o750); err != nil {
		return nil, fmt.Errorf("create store: %w", err)
	}
	return &Store{Root: clean}, nil
}

// Path joins a file name onto the store root.
func (s *Store) Path(name string) string { return filepath.Join(s.Root, name) }

// HashPayload digests arbitrary bytes.
func HashPayload(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// WriteAtomic replaces a whole file through a temporary file and a rename.
func (s *Store) WriteAtomic(name string, data []byte) error {
	target := s.Path(name)
	temporary := target + ".tmp-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync %s: %w", name, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	_ = os.Remove(target)
	if err := os.Rename(temporary, target); err != nil {
		return fmt.Errorf("install %s: %w", name, err)
	}
	ok = true
	return nil
}

// AppendLines appends whole JSON Lines records.
func (s *Store) AppendLines(name string, lines [][]byte) error {
	if len(lines) == 0 {
		return nil
	}
	file, err := os.OpenFile(s.Path(name), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o640)
	if err != nil {
		return fmt.Errorf("open %s: %w", name, err)
	}
	defer file.Close()
	for _, line := range lines {
		record := line
		if len(record) == 0 || record[len(record)-1] != '\n' {
			record = append(append([]byte(nil), record...), '\n')
		}
		if _, err := file.Write(record); err != nil {
			return fmt.Errorf("append %s: %w", name, err)
		}
	}
	return file.Sync()
}

// SaveLayout writes the layout document.
func (s *Store) SaveLayout(layout model.Layout) error {
	data, err := strictjson.Encode(layout)
	if err != nil {
		return err
	}
	return s.WriteAtomic(LayoutFile, data)
}

// LoadLayout reads the layout document.
func (s *Store) LoadLayout() (model.Layout, error) {
	var layout model.Layout
	if err := strictjson.DecodeFile(s.Path(LayoutFile), &layout); err != nil {
		return model.Layout{}, err
	}
	layout.Sort()
	return layout, nil
}

// SaveRoster writes the shift roster.
func (s *Store) SaveRoster(roster []model.Person) error {
	data, err := strictjson.Encode(roster)
	if err != nil {
		return err
	}
	return s.WriteAtomic(RosterFile, data)
}

// LoadRoster reads the shift roster, returning an empty roster when absent.
func (s *Store) LoadRoster() ([]model.Person, error) {
	roster := []model.Person{}
	if !s.exists(RosterFile) {
		return roster, nil
	}
	if err := strictjson.DecodeFile(s.Path(RosterFile), &roster); err != nil {
		return nil, err
	}
	sort.SliceStable(roster, func(a, b int) bool { return roster[a].PersonID < roster[b].PersonID })
	return roster, nil
}

// SavePermits writes the permit set.
func (s *Store) SavePermits(permits []model.Permit) error {
	data, err := strictjson.Encode(permits)
	if err != nil {
		return err
	}
	return s.WriteAtomic(PermitsFile, data)
}

// LoadPermits reads the permit set, returning an empty set when absent.
func (s *Store) LoadPermits() ([]model.Permit, error) {
	permits := []model.Permit{}
	if !s.exists(PermitsFile) {
		return permits, nil
	}
	if err := strictjson.DecodeFile(s.Path(PermitsFile), &permits); err != nil {
		return nil, err
	}
	sort.SliceStable(permits, func(a, b int) bool { return permits[a].PermitID < permits[b].PermitID })
	return permits, nil
}

// AppendReadings appends readings, skipping identifiers already present.
func (s *Store) AppendReadings(readings []model.Reading) (int, int, error) {
	existing, err := s.LoadReadings()
	if err != nil {
		return 0, 0, err
	}
	seen := make(map[string]bool, len(existing))
	for _, item := range existing {
		seen[item.ReadingID] = true
	}
	lines := make([][]byte, 0, len(readings))
	added, skipped := 0, 0
	ordered := append([]model.Reading(nil), readings...)
	model.SortReadings(ordered)
	for _, item := range ordered {
		if seen[item.ReadingID] {
			skipped++
			continue
		}
		seen[item.ReadingID] = true
		line, err := strictjson.EncodeCompact(item)
		if err != nil {
			return added, skipped, err
		}
		lines = append(lines, line)
		added++
	}
	if err := s.AppendLines(ReadingsFile, lines); err != nil {
		return added, skipped, err
	}
	return added, skipped, nil
}

// LoadReadings reads the reading ledger.
func (s *Store) LoadReadings() ([]model.Reading, error) {
	readings := []model.Reading{}
	if !s.exists(ReadingsFile) {
		return readings, nil
	}
	err := strictjson.DecodeLines(s.Path(ReadingsFile), func(line int, data []byte) error {
		var item model.Reading
		if err := strictjson.DecodeInto(data, &item); err != nil {
			return err
		}
		readings = append(readings, item)
		return nil
	})
	if err != nil {
		return nil, err
	}
	model.SortReadings(readings)
	return readings, nil
}

// AppendEvents appends tag events, skipping identifiers already present.
func (s *Store) AppendEvents(events []model.TagEvent) (int, int, error) {
	existing, err := s.LoadEvents()
	if err != nil {
		return 0, 0, err
	}
	seen := make(map[string]bool, len(existing))
	for _, item := range existing {
		seen[item.EventID] = true
	}
	lines := make([][]byte, 0, len(events))
	added, skipped := 0, 0
	ordered := append([]model.TagEvent(nil), events...)
	model.SortEvents(ordered)
	for _, item := range ordered {
		if seen[item.EventID] {
			skipped++
			continue
		}
		seen[item.EventID] = true
		line, err := strictjson.EncodeCompact(item)
		if err != nil {
			return added, skipped, err
		}
		lines = append(lines, line)
		added++
	}
	if err := s.AppendLines(EventsFile, lines); err != nil {
		return added, skipped, err
	}
	return added, skipped, nil
}

// LoadEvents reads the tag event ledger.
func (s *Store) LoadEvents() ([]model.TagEvent, error) {
	events := []model.TagEvent{}
	if !s.exists(EventsFile) {
		return events, nil
	}
	err := strictjson.DecodeLines(s.Path(EventsFile), func(line int, data []byte) error {
		var item model.TagEvent
		if err := strictjson.DecodeInto(data, &item); err != nil {
			return err
		}
		events = append(events, item)
		return nil
	})
	if err != nil {
		return nil, err
	}
	model.SortEvents(events)
	return events, nil
}

// SaveSnapshot writes the computed assessment and returns its digest.
func (s *Store) SaveSnapshot(value any) (string, error) {
	data, err := strictjson.Encode(value)
	if err != nil {
		return "", err
	}
	if err := s.WriteAtomic(SnapshotFile, data); err != nil {
		return "", err
	}
	return HashPayload(data), nil
}

// SaveMeta writes the metadata document.
func (s *Store) SaveMeta(meta Meta) error {
	meta.Schema = MetaSchema
	data, err := strictjson.Encode(meta)
	if err != nil {
		return err
	}
	return s.WriteAtomic(MetaFile, data)
}

// LoadMeta reads the metadata document, returning a fresh one when absent.
func (s *Store) LoadMeta() (Meta, error) {
	meta := Meta{Schema: MetaSchema, AuditHead: GenesisHash}
	if !s.exists(MetaFile) {
		return meta, nil
	}
	if err := strictjson.DecodeFile(s.Path(MetaFile), &meta); err != nil {
		return Meta{}, err
	}
	if meta.Schema != MetaSchema {
		return Meta{}, fmt.Errorf("store metadata declares schema %q, want %q", meta.Schema, MetaSchema)
	}
	return meta, nil
}

// Record appends one audit entry and returns it.
func (s *Store) Record(at timeutil.Stamp, action, subject string, records int, payload []byte) (AuditEntry, error) {
	entries, err := s.AuditEntries()
	if err != nil {
		return AuditEntry{}, err
	}
	previous := GenesisHash
	if len(entries) > 0 {
		previous = entries[len(entries)-1].Hash
	}
	entry := AuditEntry{
		Sequence: len(entries) + 1,
		At:       at,
		Action:   action,
		Subject:  subject,
		Records:  records,
		Payload:  HashPayload(payload),
		Previous: previous,
	}
	entry.Hash = entry.ComputeHash()
	line, err := strictjson.EncodeCompact(entry)
	if err != nil {
		return AuditEntry{}, err
	}
	if err := s.AppendLines(AuditFile, [][]byte{line}); err != nil {
		return AuditEntry{}, err
	}
	return entry, nil
}

// AuditEntries reads the audit chain.
func (s *Store) AuditEntries() ([]AuditEntry, error) {
	entries := []AuditEntry{}
	if !s.exists(AuditFile) {
		return entries, nil
	}
	err := strictjson.DecodeLines(s.Path(AuditFile), func(line int, data []byte) error {
		var entry AuditEntry
		if err := strictjson.DecodeInto(data, &entry); err != nil {
			return err
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// VerifyAudit recomputes the chain. Integrity and chronology are reported
// separately: replaying history at earlier instants leaves the chain intact and
// is a note, not a failure.
func (s *Store) VerifyAudit() (AuditReport, error) {
	entries, err := s.AuditEntries()
	if err != nil {
		return AuditReport{}, err
	}
	report := AuditReport{Entries: len(entries), Verified: true, Chronological: true, Head: GenesisHash}
	previous := GenesisHash
	last := timeutil.Stamp{}
	for index, entry := range entries {
		sequence := index + 1
		if entry.Sequence != sequence {
			report.fail(sequence, fmt.Sprintf("entry %d declares sequence %d", sequence, entry.Sequence))
			return report, nil
		}
		if entry.Previous != previous {
			report.fail(sequence, fmt.Sprintf("entry %d does not link to its predecessor", sequence))
			return report, nil
		}
		if recomputed := entry.ComputeHash(); recomputed != entry.Hash {
			report.fail(sequence, fmt.Sprintf("entry %d hash does not match its contents", sequence))
			return report, nil
		}
		if last.IsSet() && entry.At.Before(last) {
			report.Chronological = false
			report.Notes = append(report.Notes,
				fmt.Sprintf("entry %d carries an instant earlier than its predecessor", sequence))
		}
		last = entry.At
		previous = entry.Hash
	}
	report.Head = previous
	return report, nil
}

// fail records a chain problem.
func (r *AuditReport) fail(sequence int, message string) {
	r.Verified = false
	if r.BrokenAt == 0 {
		r.BrokenAt = sequence
	}
	r.Problems = append(r.Problems, message)
	r.Head = ""
}

// List reports which store files exist, in name order.
func (s *Store) List() []string {
	names := []string{LayoutFile, ReadingsFile, EventsFile, RosterFile, PermitsFile, SnapshotFile, MetaFile, AuditFile}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if s.exists(name) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// exists reports whether a store file is present.
func (s *Store) exists(name string) bool {
	info, err := os.Stat(s.Path(name))
	return err == nil && !info.IsDir()
}

// SnapshotDigest returns the digest of the stored snapshot, if any.
func (s *Store) SnapshotDigest() (string, bool, error) {
	if !s.exists(SnapshotFile) {
		return "", false, nil
	}
	data, err := os.ReadFile(s.Path(SnapshotFile))
	if err != nil {
		return "", false, err
	}
	return HashPayload(data), true, nil
}

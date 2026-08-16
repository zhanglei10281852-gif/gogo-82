// Package cli is the MineGuard command surface.
//
// Exit codes are part of the contract: 0 means the command ran and its verdict is
// clear, 1 means the command could not run, and 2 means it ran correctly and the
// answer is negative. A monitoring tool that returns success while a circuit
// should be tripped is useless in a shell script, so a trip, a refused permit, an
// incomplete muster or a broken audit chain all produce 2.
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"MineGuard/internal/config"
	"MineGuard/internal/model"
	"MineGuard/internal/pipeline"
	"MineGuard/internal/report"
	"MineGuard/internal/store"
	"MineGuard/internal/strictjson"
	"MineGuard/internal/timeutil"
)

// Version identifies the build.
const Version = "mineguard 1.0.0"

// Exit codes.
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitVerdict = 2
)

// options holds the parsed flags shared by the subcommands.
type options struct {
	config    string
	storePath string
	layout    string
	readings  string
	events    string
	roster    string
	permits   string
	format    string
	out       string
	asOf      string
	orderedAt string
	point     string
	write     bool
}

// Run dispatches one invocation.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return ExitFailure
	}
	command := args[0]
	switch command {
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, Version)
		fmt.Fprintln(stdout, "study tool only: provides no safety, engineering or regulatory advice")
		return ExitOK
	case "help", "--help", "-h":
		usage(stdout)
		return ExitOK
	}
	known := map[string]bool{
		"validate": true, "ingest": true, "gas": true, "ventilation": true,
		"interlock": true, "personnel": true, "permit": true, "inspect": true,
		"verify": true, "report": true,
	}
	if !known[command] {
		fmt.Fprintf(stderr, "unknown command %q\n", command)
		usage(stderr)
		return ExitFailure
	}
	opts, err := parse(command, args[1:])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitFailure
	}
	cfg, err := config.Load(opts.config)
	if err != nil {
		fmt.Fprintf(stderr, "configuration: %v\n", err)
		return ExitFailure
	}
	code, err := dispatch(command, cfg, opts, stdout)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitFailure
	}
	return code
}

// parse reads the flags for one subcommand.
func parse(command string, args []string) (options, error) {
	opts := options{format: "text"}
	set := flag.NewFlagSet("mineguard "+command, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.StringVar(&opts.config, "config", "", "policy document")
	set.StringVar(&opts.storePath, "store", "", "store directory")
	set.StringVar(&opts.layout, "layout", "", "layout document")
	set.StringVar(&opts.readings, "readings", "", "reading ledger")
	set.StringVar(&opts.events, "events", "", "tag event ledger")
	set.StringVar(&opts.roster, "roster", "", "shift roster")
	set.StringVar(&opts.permits, "permits", "", "permit set")
	set.StringVar(&opts.format, "format", "text", "text or json")
	set.StringVar(&opts.out, "out", "", "write output to a file")
	set.StringVar(&opts.asOf, "as-of", "", "reporting instant")
	set.StringVar(&opts.orderedAt, "evacuation-at", "", "instant an evacuation order was given")
	set.StringVar(&opts.point, "point", "", "restrict output to one monitoring point")
	set.BoolVar(&opts.write, "write", false, "persist the computed assessment")
	if err := set.Parse(args); err != nil {
		return options{}, fmt.Errorf("%s: %w", command, err)
	}
	if set.NArg() > 0 {
		return options{}, fmt.Errorf("%s: unexpected argument %q", command, set.Arg(0))
	}
	if opts.format != "text" && opts.format != "json" {
		return options{}, fmt.Errorf("%s: format must be text or json", command)
	}
	return opts, nil
}

// dispatch runs one subcommand.
func dispatch(command string, cfg config.Config, opts options, stdout io.Writer) (int, error) {
	switch command {
	case "verify":
		return runVerify(opts, stdout)
	case "ingest":
		return runIngest(cfg, opts, stdout)
	}
	bundle, err := loadBundle(opts)
	if err != nil {
		return ExitFailure, err
	}
	issues := model.ValidateBundle(bundle)
	if command == "validate" {
		return renderValidate(opts, issues, bundle, stdout)
	}
	if err := issues.Error(); err != nil {
		return ExitFailure, fmt.Errorf("input is invalid: %w", err)
	}
	runOptions, err := runtimeOptions(opts)
	if err != nil {
		return ExitFailure, err
	}
	assessment := pipeline.Run(cfg, bundle, runOptions)
	if opts.write {
		if err := persist(cfg, opts, assessment); err != nil {
			return ExitFailure, err
		}
	}
	return renderAssessment(command, cfg, opts, assessment, stdout)
}

// runtimeOptions converts the instant flags.
func runtimeOptions(opts options) (pipeline.Options, error) {
	out := pipeline.Options{}
	if strings.TrimSpace(opts.asOf) != "" {
		parsed, err := timeutil.Parse(opts.asOf)
		if err != nil {
			return out, fmt.Errorf("as-of: %w", err)
		}
		out.AsOf = parsed
	}
	if strings.TrimSpace(opts.orderedAt) != "" {
		parsed, err := timeutil.Parse(opts.orderedAt)
		if err != nil {
			return out, fmt.Errorf("evacuation-at: %w", err)
		}
		out.OrderedAt = parsed
	}
	return out, nil
}

// loadBundle reads every input document, preferring explicit files over a store.
func loadBundle(opts options) (model.Bundle, error) {
	bundle := model.Bundle{}
	if strings.TrimSpace(opts.layout) != "" {
		if err := strictjson.DecodeFile(opts.layout, &bundle.Layout); err != nil {
			return bundle, err
		}
	} else if strings.TrimSpace(opts.storePath) != "" {
		backend, err := store.Open(opts.storePath)
		if err != nil {
			return bundle, err
		}
		layout, err := backend.LoadLayout()
		if err != nil {
			return bundle, err
		}
		bundle.Layout = layout
	} else {
		return bundle, fmt.Errorf("either -layout or -store is required")
	}
	bundle.Layout.Sort()
	if strings.TrimSpace(opts.readings) != "" {
		readings, err := readReadings(opts.readings)
		if err != nil {
			return bundle, err
		}
		bundle.Readings = readings
	} else if strings.TrimSpace(opts.storePath) != "" {
		backend, err := store.Open(opts.storePath)
		if err != nil {
			return bundle, err
		}
		readings, err := backend.LoadReadings()
		if err != nil {
			return bundle, err
		}
		bundle.Readings = readings
	}
	if strings.TrimSpace(opts.events) != "" {
		events, err := readEvents(opts.events)
		if err != nil {
			return bundle, err
		}
		bundle.Events = events
	} else if strings.TrimSpace(opts.storePath) != "" {
		backend, err := store.Open(opts.storePath)
		if err != nil {
			return bundle, err
		}
		events, err := backend.LoadEvents()
		if err != nil {
			return bundle, err
		}
		bundle.Events = events
	}
	if strings.TrimSpace(opts.roster) != "" {
		roster := []model.Person{}
		if err := strictjson.DecodeFile(opts.roster, &roster); err != nil {
			return bundle, err
		}
		bundle.Roster = roster
	} else if strings.TrimSpace(opts.storePath) != "" {
		backend, err := store.Open(opts.storePath)
		if err != nil {
			return bundle, err
		}
		roster, err := backend.LoadRoster()
		if err != nil {
			return bundle, err
		}
		bundle.Roster = roster
	}
	if strings.TrimSpace(opts.permits) != "" {
		permits := []model.Permit{}
		if err := strictjson.DecodeFile(opts.permits, &permits); err != nil {
			return bundle, err
		}
		bundle.Permits = permits
	} else if strings.TrimSpace(opts.storePath) != "" {
		backend, err := store.Open(opts.storePath)
		if err != nil {
			return bundle, err
		}
		permits, err := backend.LoadPermits()
		if err != nil {
			return bundle, err
		}
		bundle.Permits = permits
	}
	sort.SliceStable(bundle.Roster, func(a, b int) bool {
		return bundle.Roster[a].PersonID < bundle.Roster[b].PersonID
	})
	sort.SliceStable(bundle.Permits, func(a, b int) bool {
		return bundle.Permits[a].PermitID < bundle.Permits[b].PermitID
	})
	return bundle, nil
}

// readReadings reads a reading ledger file.
func readReadings(path string) ([]model.Reading, error) {
	readings := []model.Reading{}
	err := strictjson.DecodeLines(path, func(line int, data []byte) error {
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

// readEvents reads a tag event ledger file.
func readEvents(path string) ([]model.TagEvent, error) {
	events := []model.TagEvent{}
	err := strictjson.DecodeLines(path, func(line int, data []byte) error {
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

// renderValidate reports the validation findings.
func renderValidate(opts options, issues model.Issues, bundle model.Bundle, stdout io.Writer) (int, error) {
	payload := map[string]any{
		"mine_id":  bundle.Layout.MineID,
		"points":   len(bundle.Layout.Points),
		"readings": len(bundle.Readings),
		"events":   len(bundle.Events),
		"permits":  len(bundle.Permits),
		"ok":       issues.OK(),
		"issues":   issues.Sorted(),
	}
	if opts.format == "json" {
		if err := writeJSON(opts, payload, stdout); err != nil {
			return ExitFailure, err
		}
	} else {
		var builder strings.Builder
		fmt.Fprintf(&builder, "mine: %s\n", bundle.Layout.MineID)
		fmt.Fprintf(&builder, "points: %d\nreadings: %d\nevents: %d\npermits: %d\n",
			len(bundle.Layout.Points), len(bundle.Readings), len(bundle.Events), len(bundle.Permits))
		fmt.Fprintf(&builder, "ok: %t\n", issues.OK())
		for _, issue := range issues.Sorted() {
			fmt.Fprintf(&builder, "%-8s %-44s %s\n", issue.Severity, issue.Path, issue.Message)
		}
		if err := emit(opts, builder.String(), stdout); err != nil {
			return ExitFailure, err
		}
	}
	if !issues.OK() {
		return ExitVerdict, nil
	}
	return ExitOK, nil
}

// renderAssessment renders the section a subcommand asked for.
func renderAssessment(command string, cfg config.Config, opts options,
	assessment pipeline.Assessment, stdout io.Writer) (int, error) {
	if opts.format == "json" {
		payload := sectionJSON(command, assessment)
		if err := writeJSON(opts, payload, stdout); err != nil {
			return ExitFailure, err
		}
		return verdict(command, assessment), nil
	}
	text := ""
	switch command {
	case "gas":
		if strings.TrimSpace(opts.point) != "" {
			text = report.GasIndexText(cfg, assessment, opts.point)
		} else {
			text = report.GasTable(cfg, assessment)
		}
	case "ventilation":
		text = report.VentilationTable(cfg, assessment)
	case "interlock":
		text = report.InterlockTable(assessment)
	case "personnel":
		text = report.PersonnelTable(assessment)
	case "permit":
		text = report.PermitTable(assessment)
	case "inspect":
		text = report.InspectionTable(assessment) + "\n" + report.TimelineText(assessment)
	default:
		text = report.Full(cfg, assessment)
	}
	if err := emit(opts, text, stdout); err != nil {
		return ExitFailure, err
	}
	return verdict(command, assessment), nil
}

// sectionJSON selects the payload for a subcommand.
func sectionJSON(command string, assessment pipeline.Assessment) any {
	switch command {
	case "gas":
		return assessment.Gas
	case "ventilation":
		return assessment.Ventilation
	case "interlock":
		return assessment.Interlock
	case "personnel":
		return assessment.Personnel
	case "permit":
		return assessment.Permits
	case "inspect":
		return assessment.Inspections
	default:
		return assessment
	}
}

// verdict maps an assessment onto an exit code for one subcommand.
func verdict(command string, assessment pipeline.Assessment) int {
	switch command {
	case "gas":
		if assessment.Summary.GasTripCount > 0 {
			return ExitVerdict
		}
	case "ventilation":
		if !assessment.Summary.VentilationOK {
			return ExitVerdict
		}
	case "interlock":
		if assessment.Summary.TrippedCircuits > 0 {
			return ExitVerdict
		}
	case "personnel":
		if assessment.Summary.Unaccounted > 0 {
			return ExitVerdict
		}
	case "permit":
		if assessment.Summary.RefusedPermits > 0 {
			return ExitVerdict
		}
	case "inspect":
		if assessment.Summary.OverdueInspections > 0 {
			return ExitVerdict
		}
	default:
		if !assessment.Summary.Safe {
			return ExitVerdict
		}
	}
	return ExitOK
}

// runIngest appends the supplied documents to a store.
func runIngest(cfg config.Config, opts options, stdout io.Writer) (int, error) {
	if strings.TrimSpace(opts.storePath) == "" {
		return ExitFailure, fmt.Errorf("ingest requires -store")
	}
	bundle, err := loadBundle(opts)
	if err != nil {
		return ExitFailure, err
	}
	issues := model.ValidateBundle(bundle)
	if err := issues.Error(); err != nil {
		return ExitFailure, fmt.Errorf("refusing to ingest invalid input: %w", err)
	}
	backend, err := store.Open(opts.storePath)
	if err != nil {
		return ExitFailure, err
	}
	if err := backend.SaveLayout(bundle.Layout); err != nil {
		return ExitFailure, err
	}
	if len(bundle.Roster) > 0 {
		if err := backend.SaveRoster(bundle.Roster); err != nil {
			return ExitFailure, err
		}
	}
	if len(bundle.Permits) > 0 {
		if err := backend.SavePermits(bundle.Permits); err != nil {
			return ExitFailure, err
		}
	}
	addedReadings, skippedReadings, err := backend.AppendReadings(bundle.Readings)
	if err != nil {
		return ExitFailure, err
	}
	addedEvents, skippedEvents, err := backend.AppendEvents(bundle.Events)
	if err != nil {
		return ExitFailure, err
	}
	at := pipeline.DeriveAsOf(bundle)
	payload := fmt.Sprintf("%d:%d:%s", addedReadings, addedEvents, bundle.Layout.MineID)
	entry, err := backend.Record(at, "ingest", bundle.Layout.MineID,
		addedReadings+addedEvents, []byte(payload))
	if err != nil {
		return ExitFailure, err
	}
	meta, err := backend.LoadMeta()
	if err != nil {
		return ExitFailure, err
	}
	readings, err := backend.LoadReadings()
	if err != nil {
		return ExitFailure, err
	}
	events, err := backend.LoadEvents()
	if err != nil {
		return ExitFailure, err
	}
	meta.MineID = bundle.Layout.MineID
	meta.Readings = len(readings)
	meta.Events = len(events)
	meta.AuditEntries = entry.Sequence
	meta.AuditHead = entry.Hash
	meta.LastEventAt = at
	meta.Fingerprint = cfg.Fingerprint()
	if err := backend.SaveMeta(meta); err != nil {
		return ExitFailure, err
	}
	text := fmt.Sprintf("readings added %d skipped %d\nevents added %d skipped %d\naudit entry %d\n",
		addedReadings, skippedReadings, addedEvents, skippedEvents, entry.Sequence)
	if opts.format == "json" {
		if err := writeJSON(opts, map[string]any{
			"readings_added": addedReadings, "readings_skipped": skippedReadings,
			"events_added": addedEvents, "events_skipped": skippedEvents,
			"audit_sequence": entry.Sequence, "audit_head": entry.Hash,
		}, stdout); err != nil {
			return ExitFailure, err
		}
		return ExitOK, nil
	}
	if err := emit(opts, text, stdout); err != nil {
		return ExitFailure, err
	}
	return ExitOK, nil
}

// runVerify recomputes the audit chain.
func runVerify(opts options, stdout io.Writer) (int, error) {
	if strings.TrimSpace(opts.storePath) == "" {
		return ExitFailure, fmt.Errorf("verify requires -store")
	}
	backend, err := store.Open(opts.storePath)
	if err != nil {
		return ExitFailure, err
	}
	chain, err := backend.VerifyAudit()
	if err != nil {
		return ExitFailure, err
	}
	files := backend.List()
	if opts.format == "json" {
		if err := writeJSON(opts, map[string]any{"audit": chain, "files": files}, stdout); err != nil {
			return ExitFailure, err
		}
	} else if err := emit(opts, report.AuditText(chain, files), stdout); err != nil {
		return ExitFailure, err
	}
	if !chain.Verified {
		return ExitVerdict, nil
	}
	return ExitOK, nil
}

// persist writes the assessment snapshot and refreshes the store metadata.
func persist(cfg config.Config, opts options, assessment pipeline.Assessment) error {
	if strings.TrimSpace(opts.storePath) == "" {
		return fmt.Errorf("-write requires -store")
	}
	backend, err := store.Open(opts.storePath)
	if err != nil {
		return err
	}
	digest, err := backend.SaveSnapshot(assessment)
	if err != nil {
		return err
	}
	entry, err := backend.Record(assessment.AsOf, "assess", assessment.MineID,
		len(assessment.Readings.States), []byte(digest))
	if err != nil {
		return err
	}
	meta, err := backend.LoadMeta()
	if err != nil {
		return err
	}
	meta.MineID = assessment.MineID
	meta.Snapshots++
	meta.SnapshotHash = digest
	meta.AuditEntries = entry.Sequence
	meta.AuditHead = entry.Hash
	meta.Fingerprint = cfg.Fingerprint()
	return backend.SaveMeta(meta)
}

// writeJSON renders a payload as JSON.
func writeJSON(opts options, payload any, stdout io.Writer) error {
	data, err := strictjson.Encode(payload)
	if err != nil {
		return err
	}
	return emit(opts, string(data), stdout)
}

// emit writes text to the chosen destination.
func emit(opts options, text string, stdout io.Writer) error {
	if strings.TrimSpace(opts.out) == "" {
		_, err := io.WriteString(stdout, text)
		return err
	}
	return os.WriteFile(opts.out, []byte(text), 0o640)
}

// usage prints the command surface.
func usage(writer io.Writer) {
	fmt.Fprintln(writer, Version)
	fmt.Fprintln(writer, "study tool only: provides no safety, engineering or regulatory advice")
	fmt.Fprintln(writer, "")
	fmt.Fprintln(writer, "usage: mineguard <command> [flags]")
	fmt.Fprintln(writer, "")
	commands := []string{
		"validate     check a layout and its ledgers without writing anything",
		"ingest       append readings and events to a store and record the audit entry",
		"gas          report the gas assessment per monitoring point",
		"ventilation  report airway flows, junction balances and face adequacy",
		"interlock    report the circuit decisions and the rules behind them",
		"personnel    report occupancy and the muster verdict",
		"permit       report permit clearance",
		"inspect      report inspection status and the timeline",
		"verify       recompute the store audit chain",
		"report       render every section",
	}
	sort.Strings(commands)
	for _, line := range commands {
		fmt.Fprintln(writer, "  "+line)
	}
	fmt.Fprintln(writer, "")
	fmt.Fprintln(writer, "exit codes: 0 clear, 1 could not run, 2 ran with a negative verdict")
}

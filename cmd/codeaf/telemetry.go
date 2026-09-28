package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/pool/outbox"
	"github.com/Agent-Field/codeaf/internal/pool/poolcfg"
	"github.com/Agent-Field/codeaf/internal/pool/record"
	"github.com/Agent-Field/codeaf/internal/telemetry"
)

// The `telemetry` command: the person's door onto the anonymous-usage pipe.
// Five verbs, one per question a person arrives with — what is it doing, what
// is collected, what is waiting to leave right now, and the two ways of
// turning it off or back on. It EMITS NOTHING ITSELF: it is a command about
// telemetry, not a session, and the wiring in main.go's execute() looks at
// os.Args to make sure of it.
func runTelemetry(args []string) error {
	if len(args) == 0 {
		return runTelemetryStatus(nil)
	}
	switch args[0] {
	case "-h", "-help", "--help":
		// ASKING IS NEVER A FAILURE. This door took the flag for a sixth
		// verb and left with 1, the one door in the binary that did.
		return commandHelp("telemetry")
	case "status":
		return runTelemetryStatus(args[1:])
	case "info":
		return runTelemetryInfo(args[1:])
	case "show":
		return runTelemetryShow(args[1:])
	case "on", "off":
		return runTelemetrySet(args[0], args[1:])
	default:
		return fmt.Errorf("telemetry takes one of: status, info, show, on, off")
	}
}

// telemetryFlags holds the one flag every verb accepts so a --help reader and
// the tests share one parser. It is the binary's own seam, named for the whole
// line a person typed, so `codeaf telemetry status --help` prints the usage and
// leaves with 0 like every other verb instead of `flag: help requested` and 1.
func telemetryFlags(name string) *flag.FlagSet {
	return commandFlags("telemetry " + name)
}

// runTelemetryStatus prints the pipe's whole answer: on or off, why it is off,
// where events would go, whether the notice has been shown, how many events
// are waiting, and the first 12 characters of the install id hash — enough to
// recognise, far too little to be a person.
func runTelemetryStatus(args []string) error {
	flags := telemetryFlags("status")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	// The config answer goes through the package's single door so the row
	// this reads and the events the binary spools cannot disagree about
	// whether telemetry is off.
	profileDir := config.ProfileDir()
	configuredOff := false
	if cwd, err := os.Getwd(); err == nil {
		configuredOff, _ = config.TelemetryOffReason(cwd, profileDir)
	}
	telemetry.Configure(configuredOff)

	reason := telemetry.OffReason()
	state := "on"
	if reason != "" {
		state = "off"
	}
	notice := "not shown"
	if telemetry.NoticeShown() {
		notice = "shown"
	}
	fmt.Fprintf(usageOut, "telemetry %s\n", state)
	if reason != "" {
		fmt.Fprintf(usageOut, "  reason: %s\n", reason)
	}
	fmt.Fprintf(usageOut, "  endpoint: %s\n", telemetry.Endpoint())
	fmt.Fprintf(usageOut, "  notice: %s\n", notice)
	fmt.Fprintf(usageOut, "  spooled events: %d\n", len(telemetry.SpoolContents()))
	fmt.Fprintf(usageOut, "  install: %s…\n", telemetryInstallPrefix())
	return nil
}

// telemetryInstallPrefix is the first 12 characters of the install id hash —
// the identity the wire sees, truncated to something a person can compare
// between machines and nothing more.
func telemetryInstallPrefix() string {
	hash := telemetry.InstallIDHash()
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

// runTelemetryInfo prints what is collected — the shape of every row that can
// leave, in a person's words, for both streams. The notice promises "what is
// collected: codeaf telemetry info", and two streams leave: the anonymous
// usage counts this package spools, and the Model Pool's judged seat scores,
// which wait in the pool's own outbox under the profile and go to a different
// relay under a different switch. Until 2026-09-18 the only listing covered
// the first, so a person who read it and set CODEAF_TELEMETRY=off believed
// nothing more would leave while the pool went on sending. Both are described
// here, each under a line naming where it goes or why it does not.
func runTelemetryInfo(args []string) error {
	flags := telemetryFlags("info")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	profileDir := config.ProfileDir()
	telemetry.Configure(telemetryConfiguredOff())
	fmt.Fprintln(usageOut, infoText(profileDir, os.LookupEnv))
	return nil
}

// runTelemetryShow prints exactly what is waiting to leave the machine — ALL
// of it, from both streams — as one JSON object a person can read and a
// script can parse: a key per destination, and under each where it goes, why
// it is not sent when it is not, and the rows waiting in the bytes the relay
// would receive. Indented by two, because the person who runs this is reading
// it, not piping it; a pipe reads indented JSON just as well.
func runTelemetryShow(args []string) error {
	flags := telemetryFlags("show")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	profileDir := config.ProfileDir()
	telemetry.Configure(telemetryConfiguredOff())
	report := waitingReport{
		Usage: waitingStream{
			Destination: telemetry.Endpoint(),
			Off:         telemetry.OffReason(),
			Waiting:     nonNil(telemetry.SpoolContents()),
		},
	}
	cfg := config.ModelPoolResolved(profileDir, os.LookupEnv)
	report.ModelPool = waitingStream{
		Destination: cfg.SubmitURL,
		Off:         modelPoolOffReason(cfg),
		Waiting:     nonNil(poolRowsWaiting(config.ProfilePath(profileDir, "pool"))),
	}
	// The encoder, not json.MarshalIndent: a waiting row is written with
	// HTML escaping off, as the outbox and the spool write it, so the bytes
	// printed are the bytes a relay is sent.
	enc := json.NewEncoder(usageOut)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

// waitingReport is `codeaf telemetry show`'s whole answer: one entry per
// destination, keyed by the stream's short name, in the order the notice
// names them.
type waitingReport struct {
	Usage     waitingStream `json:"usage"`
	ModelPool waitingStream `json:"model_pool"`
}

// waitingStream is one destination: where its rows go, the reason nothing is
// sent there when that is so, and the rows waiting to leave, oldest first.
type waitingStream struct {
	Destination string            `json:"destination"`
	Off         string            `json:"off,omitempty"`
	Waiting     []json.RawMessage `json:"waiting"`
}

// nonNil renders an empty queue as `[]`, never `null`: a person reading the
// object should see an empty list where rows would be, not an absence.
func nonNil(rows []json.RawMessage) []json.RawMessage {
	if rows == nil {
		return []json.RawMessage{}
	}
	return rows
}

// modelPoolOffReason names why the pool sends nothing, or "" when it sends:
// `read` uses the pool and sends nothing, `off` asks no judge at all, and a
// cap that came from the telemetry off switch says so, because the person who
// set that switch is the one reading this.
func modelPoolOffReason(cfg poolcfg.Config) string {
	if cfg.CanSend() {
		return ""
	}
	reason := fmt.Sprintf("model_pool %s", cfg.Mode)
	if cfg.Source.Mode == "telemetry" {
		reason += " (capped by the telemetry off switch)"
	}
	return reason
}

// infoPreface is the first thing `codeaf telemetry info` says, before either
// stream: the one fact a person came to check. It is true of everything the
// binary sends to AgentField — the usage counts carry only the allowlisted
// fields below, and a Model Pool row carries model slugs, a score and a day.
// The judge that produces a score reads a clipped brief and deliverable, but
// that is a model call to your own provider, like any turn, and nothing it
// read rides in the row.
const infoPreface = `codeaf does NOT collect or share your chat. No prompts, replies, code, file names,
paths, repo names, keys, email, IP or machine name leave for AgentField. Only the
fields below do, as this machine would fill them.`

// infoText composes the two streams, in the order the notice names them: the
// usage counts first, the Model Pool second. Each sits under a heading naming
// where it goes or why it does not, then WHAT A ROW LOOKS LIKE: the fields
// with this machine's own values where they are known before a run, and one
// example row per event where they are not, spelled from the contract's own
// constants. Only what is sent is listed; the never lists live in the notice
// and docs/TELEMETRY.md, because a person reading a shape wants the shape,
// not a second disclaimer. What is waiting right now is `show`'s answer.
func infoText(profileDir string, lookup func(string) (string, bool)) string {
	var out strings.Builder
	out.WriteString(infoPreface)
	out.WriteString("\n\n")
	out.WriteString(usageCountsHeading())
	out.WriteByte('\n')
	writeUsageCountFields(&out)
	out.WriteByte('\n')
	cfg := config.ModelPoolResolved(profileDir, lookup)
	out.WriteString(modelPoolHeading(cfg))
	out.WriteByte('\n')
	writeModelPoolFields(&out)
	return strings.TrimRight(out.String(), "\n")
}

// showIndent is the two spaces every line under a stream heading starts with.
const showIndent = "  "

// showKeyWidth is the column the values start in: the widest key any row
// carries is model_calls_failed, eighteen characters, and two for air.
const showKeyWidth = 20

// writeUsageCountFields prints the usage-count row as this machine would fill
// it — the six every-event props with their live values and the four
// envelope fields — then one example row per event, then the stop reasons.
// The bands themselves are not listed: the example rows show one of each,
// and docs/TELEMETRY.md spells the rest.
func writeUsageCountFields(out *strings.Builder) {
	fmt.Fprintf(out, "%severy event, as this machine would send it now\n", showIndent)
	for _, prop := range telemetry.CommonPropValues() {
		writeField(out, prop.Name, prop.Value)
	}
	install := "sha256 of a random id, minted on the first send"
	if hash, ok := telemetry.InstallIDHashIfMinted(); ok {
		install = hash[:12] + "…"
	}
	writeField(out, "install_id_hash", install)
	writeField(out, "session_id_hash", "sha256 of the run id, one per session; absent on first_run")
	writeField(out, "event_id", "16 random bytes as hex, one per event")
	writeField(out, "event_time", time.Now().UTC().Format(time.RFC3339))
	out.WriteByte('\n')
	fmt.Fprintf(out, "%swhat each event adds, for example\n", showIndent)
	for _, event := range telemetry.AllowlistedEvents() {
		names := telemetry.EventPropNames(event)
		if len(names) == 0 {
			writeField(out, event, "nothing; sent once per install")
			continue
		}
		writeField(out, event, exampleRow(event, names))
	}
	writeField(out, "stop_reason", "one of "+strings.Join(telemetry.StopReasons(), " · "))
}

// exampleRow spells one event's props as key=value pairs in the doc's order,
// wrapped so a session_ended row does not run past the terminal's edge. The
// values are [telemetry.ExampleProp]'s, from the contract's constants.
func exampleRow(event string, names []string) string {
	var pairs []string
	for _, name := range names {
		pairs = append(pairs, name+"="+telemetry.ExampleProp(event, name))
	}
	const perLine = 5
	var lines []string
	for len(pairs) > 0 {
		n := perLine
		if n > len(pairs) {
			n = len(pairs)
		}
		lines = append(lines, strings.Join(pairs[:n], "  "))
		pairs = pairs[n:]
	}
	continuation := "\n" + showIndent + showIndent + strings.Repeat(" ", showKeyWidth+1)
	return strings.Join(lines, continuation)
}

// writeModelPoolFields prints what one pool row looks like: an example row in
// the bytes a relay would receive, indented so it reads, then the two
// identities a batch travels under.
func writeModelPoolFields(out *strings.Builder) {
	fmt.Fprintf(out, "%sone row per judged seat, after a task lands, for example\n", showIndent)
	// The row is indented by two, the way `show` prints a waiting one, and
	// set in under the heading; json.Indent keeps the bytes the row's own.
	var row bytes.Buffer
	if err := json.Indent(&row, []byte(record.ExampleRowJSON(time.Now())), showIndent+showIndent, "  "); err == nil {
		fmt.Fprintf(out, "%s%s%s\n", showIndent, showIndent, row.String())
	}
	writeField(out, "nonce", "16 random bytes as hex, one per row, so a resend is not a double count")
	writeField(out, "X-Codeaf-Install", "a header: a random per-install id, minted on the first send; not the usage counts' id")
}

// writeField prints one field line: the key in its column and the value.
func writeField(out *strings.Builder, key, value string) {
	fmt.Fprintf(out, "%s%s%-*s %s\n", showIndent, showIndent, showKeyWidth, key, value)
}

// usageCountsHeading names where the usage counts go, or the rung of the
// opt-out ladder that keeps them here. The two streams are numbered in the
// order the notice names them, so a person can say "the second one". It reads the same ladder `telemetry
// status` reads, so the two verbs cannot disagree about whether anything is
// sent.
func usageCountsHeading() string {
	if reason := telemetry.OffReason(); reason != "" {
		return fmt.Sprintf("1. Usage Counts (off: %s)", reason)
	}
	return fmt.Sprintf("1. Usage Counts (%s)", telemetry.Endpoint())
}

// modelPoolHeading names where the pool rows go, or the mode that keeps them
// here: `read` uses the pool and sends nothing, `off` asks no judge at all.
func modelPoolHeading(cfg poolcfg.Config) string {
	if !cfg.CanSend() {
		return fmt.Sprintf("2. Model Pool (model_pool %s, nothing is sent)", cfg.Mode)
	}
	return fmt.Sprintf("2. Model Pool (%s)", cfg.SubmitURL)
}

// poolRowsWaiting reads the pool outbox's pending rows the way
// telemetry.SpoolContents reads the spool: raw JSON rows, oldest first, nil
// when nothing waits. It reads the file by path and stats it first, like
// [pendingRows], because [outbox.Open] creates an absent outbox and a reading
// form must not write.
func poolRowsWaiting(poolDir string) []json.RawMessage {
	path := filepath.Join(poolDir, "outbox.jsonl")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	box, err := outbox.Open(path)
	if err != nil {
		return nil
	}
	defer box.Close()
	rows := box.Pending()
	if len(rows) == 0 {
		return nil
	}
	out := make([]json.RawMessage, 0, len(rows))
	for _, row := range rows {
		// The outbox stores a row compacted; encoding it again here, with
		// HTML escaping off as the outbox writes it, answers the same bytes
		// the relay is sent.
		var line bytes.Buffer
		enc := json.NewEncoder(&line)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(row); err != nil {
			continue
		}
		out = append(out, json.RawMessage(bytes.TrimRight(line.Bytes(), "\n")))
	}
	return out
}

// runTelemetrySet writes the settings row from internal/config: `telemetry off`
// turns the pipe off for this machine, `telemetry on` turns it back on. It
// writes the PROFILE row — the person's own answer — and says one confirming
// line, because a command that changed a setting silently would be a change
// nobody could audit.
func runTelemetrySet(word string, args []string) error {
	flags := telemetryFlags(word)
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	profileDir := config.ProfileDir()
	var value bool
	switch word {
	case "on":
		value = true
	case "off":
		value = false
	}
	if err := config.WriteTelemetry(profileDir, value); err != nil {
		return err
	}
	if value {
		fmt.Fprintln(usageOut, "telemetry on — anonymous usage counts are sent (see `codeaf telemetry info`)")
		return nil
	}
	fmt.Fprintln(usageOut, "telemetry off — nothing is sent; the session counters still count")
	return nil
}

// telemetryMode decides, from the command line, whether this invocation is a
// session worth counting and which of the two modes it ran in. Every command
// that is not named here emits nothing and sends nothing.
func telemetryMode(args []string) (mode telemetry.Mode, resumed bool, session bool) {
	// `plan run` is two args: the verb is the second word. Matching `plan`
	// alone would also count `plan new`, which writes a file and runs nothing.
	if len(args) >= 2 && args[0] == "plan" && args[1] == "run" {
		return telemetry.ModeTask, false, true
	}
	if len(args) < 1 {
		return telemetry.ModeChat, false, true
	}
	switch args[0] {
	case "chat":
		return telemetry.ModeChat, false, true
	case "resume":
		return telemetry.ModeChat, true, true
	case "do", "exec", "run":
		return telemetry.ModeTask, false, true
	}
	return "", false, false
}

// telemetryHasJSON reports whether this invocation carries --json, which
// stdout must stay machine-clean for: the notice would be one more line in
// somebody's jq pipeline.
func telemetryHasJSON(args []string) bool {
	for _, arg := range args {
		if arg == "--json" || strings.HasPrefix(arg, "--json=") {
			return true
		}
	}
	return false
}

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
// Four verbs, one per question a person arrives with — what is it doing, what
// exactly would leave, and the two ways of turning it off or back on. It
// EMITS NOTHING ITSELF: it is a command about telemetry, not a session, and
// the wiring in main.go's execute() looks at os.Args to make sure of it.
func runTelemetry(args []string) error {
	if len(args) == 0 {
		return runTelemetryStatus(nil)
	}
	switch args[0] {
	case "status":
		return runTelemetryStatus(args[1:])
	case "show":
		return runTelemetryShow(args[1:])
	case "on", "off":
		return runTelemetrySet(args[0], args[1:])
	default:
		return fmt.Errorf("telemetry takes one of: status, show, on, off")
	}
}

// telemetryFlags holds the one flag every verb accepts so a --help reader and
// the tests share one parser.
func telemetryFlags(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	return flags
}

// runTelemetryStatus prints the pipe's whole answer: on or off, why it is off,
// where events would go, whether the notice has been shown, how many events
// are waiting, and the first 12 characters of the install id hash — enough to
// recognise, far too little to be a person.
func runTelemetryStatus(args []string) error {
	flags := telemetryFlags("status")
	if err := flags.Parse(args); err != nil {
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

// runTelemetryShow prints exactly what is waiting to leave the machine — ALL
// of it. The notice promises "see exactly what leaves: codeaf telemetry show",
// and two streams leave: the anonymous usage counts this package spools, and
// the Model Pool's judged seat scores, which wait in the pool's own outbox
// under the profile and go to a different relay under a different switch.
// Until 2026-09-18 this verb printed only the first, so a person who read it
// and set CODEAF_TELEMETRY=off believed nothing more would leave while the
// pool went on sending. Both streams are printed here, each under a line
// naming where it goes or why it does not, so the sentence in the notice is
// true of everything the binary sends.
func runTelemetryShow(args []string) error {
	flags := telemetryFlags("show")
	if err := flags.Parse(args); err != nil {
		return err
	}
	profileDir := config.ProfileDir()
	telemetry.Configure(telemetryConfiguredOff())
	fmt.Fprintln(usageOut, showEverythingWaiting(profileDir, os.LookupEnv))
	return nil
}

// showEverythingWaiting composes the two streams, in the order the notice
// names them: the usage counts first, the Model Pool second. Each sits under
// a heading naming where it goes or why it does not, then what is waiting,
// then WHAT A ROW LOOKS LIKE: the fields with this machine's own values where
// they are known before a run, and one example row per event where they are
// not, spelled from the contract's own constants. Only what is sent is
// listed; the never lists live in the notice and docs/TELEMETRY.md, because a
// person reading a shape wants the shape, not a second disclaimer.
//
// The fields are printed whether or not anything is waiting. A person reads
// this verb once, on the day they install, when the spool is empty; two empty
// arrays told them nothing about what would leave the first time they used the
// program, and the notice had promised them exactly that.
func showEverythingWaiting(profileDir string, lookup func(string) (string, bool)) string {
	var out strings.Builder
	out.WriteString(usageCountsHeading())
	out.WriteByte('\n')
	writeWaiting(&out, telemetry.Show())
	out.WriteByte('\n')
	writeUsageCountFields(&out)
	out.WriteByte('\n')
	cfg := config.ModelPoolResolved(profileDir, lookup)
	out.WriteString(modelPoolHeading(cfg))
	out.WriteByte('\n')
	writeWaiting(&out, poolRowsWaiting(config.ProfilePath(profileDir, "pool")))
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
// envelope fields — then one example row per event, then the bands.
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
	out.WriteByte('\n')
	writeField(out, "bands", "counts "+strings.Join(telemetry.CountBands(), " · "))
	writeField(out, "", "dollars "+strings.Join(telemetry.CostBands(), " · "))
	writeField(out, "", "duration "+strings.Join(telemetry.DurationBands(), " · "))
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
// the bytes a relay would receive, then the two identities a batch travels
// under.
func writeModelPoolFields(out *strings.Builder) {
	fmt.Fprintf(out, "%sone row per judged seat, after a task lands, for example\n", showIndent)
	for _, line := range wrapJSONRow(record.ExampleRowJSON(time.Now()), 72) {
		fmt.Fprintf(out, "%s%s%s\n", showIndent, showIndent, line)
	}
	writeField(out, "nonce", "16 random bytes as hex, one per row, so a resend is not a double count")
	writeField(out, "X-Codeaf-Install", "a header: a random per-install id, minted on the first send; not the usage counts' id")
}

// wrapJSONRow breaks one flat JSON object over lines of about the width
// given, only ever at a comma before a key, and indents the continuation by
// one space so the braces line up; the bytes, read back without the breaks
// and the indent, are the row's own.
func wrapJSONRow(row string, width int) []string {
	var lines []string
	line := ""
	for i, part := range strings.Split(row, ",\"") {
		if i > 0 {
			part = "\"" + part
			if len(line)+1+len(part) > width {
				lines = append(lines, line+",")
				line = " "
			} else {
				line += ","
			}
		}
		line += part
	}
	return append(lines, line)
}

// writeField prints one field line: the key in its column and the value, or
// a continuation line under the value column when the key is empty.
func writeField(out *strings.Builder, key, value string) {
	fmt.Fprintf(out, "%s%s%-*s %s\n", showIndent, showIndent, showKeyWidth, key, value)
}

// writeWaiting prints the rows waiting to leave, or one line saying none are.
func writeWaiting(out *strings.Builder, rows string) {
	if rows == "[]" {
		fmt.Fprintf(out, "%swaiting to leave: none\n", showIndent)
		return
	}
	fmt.Fprintf(out, "%swaiting to leave:\n%s\n", showIndent, rows)
}

// usageCountsHeading names where the usage counts go, or the rung of the
// opt-out ladder that keeps them here. It reads the same ladder `telemetry
// status` reads, so the two verbs cannot disagree about whether anything is
// sent.
func usageCountsHeading() string {
	if reason := telemetry.OffReason(); reason != "" {
		return fmt.Sprintf("usage counts (off: %s)", reason)
	}
	return fmt.Sprintf("usage counts (%s)", telemetry.Endpoint())
}

// modelPoolHeading names where the pool rows go, or the mode that keeps them
// here: `read` uses the pool and sends nothing, `off` asks no judge at all.
func modelPoolHeading(cfg poolcfg.Config) string {
	if !cfg.CanSend() {
		return fmt.Sprintf("Model Pool (model_pool %s, nothing is sent)", cfg.Mode)
	}
	return fmt.Sprintf("Model Pool (%s)", cfg.SubmitURL)
}

// poolRowsWaiting renders the pool outbox's pending rows the way telemetry.Show
// renders the spool: a JSON array, one row per line, `[]` when nothing waits.
// It reads the file by path and stats it first, like [pendingRows], because
// [outbox.Open] creates an absent outbox and a reading form must not write.
func poolRowsWaiting(poolDir string) string {
	path := filepath.Join(poolDir, "outbox.jsonl")
	if _, err := os.Stat(path); err != nil {
		return "[]"
	}
	box, err := outbox.Open(path)
	if err != nil {
		return "[]"
	}
	defer box.Close()
	rows := box.Pending()
	if len(rows) == 0 {
		return "[]"
	}
	var out bytes.Buffer
	out.WriteString("[\n")
	for i, row := range rows {
		// The outbox stores a row compacted; encoding it again here, with
		// HTML escaping off as the outbox writes it, answers the same bytes
		// the relay is sent.
		var line bytes.Buffer
		enc := json.NewEncoder(&line)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(row); err != nil {
			continue
		}
		out.WriteString("  ")
		out.Write(bytes.TrimSpace(line.Bytes()))
		if i < len(rows)-1 {
			out.WriteByte(',')
		}
		out.WriteByte('\n')
	}
	out.WriteString("]")
	return out.String()
}

// runTelemetrySet writes the settings row from internal/config: `telemetry off`
// turns the pipe off for this machine, `telemetry on` turns it back on. It
// writes the PROFILE row — the person's own answer — and says one confirming
// line, because a command that changed a setting silently would be a change
// nobody could audit.
func runTelemetrySet(word string, args []string) error {
	flags := telemetryFlags(word)
	if err := flags.Parse(args); err != nil {
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
		fmt.Fprintln(usageOut, "telemetry on — anonymous usage counts are sent (see `codeaf telemetry show`)")
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

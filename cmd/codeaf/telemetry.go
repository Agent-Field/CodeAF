package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/pool/outbox"
	"github.com/Agent-Field/codeaf/internal/pool/poolcfg"
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
// names them: the usage counts first, the Model Pool second. Each stream is a
// heading line and then its rows as JSON, the usage counts in the telemetry
// package's own rendering and the pool rows in the outbox's own line shape,
// so what is printed is byte for byte what a relay would receive.
func showEverythingWaiting(profileDir string, lookup func(string) (string, bool)) string {
	var out strings.Builder
	out.WriteString(usageCountsHeading())
	out.WriteByte('\n')
	out.WriteString(telemetry.Show())
	out.WriteString("\n\n")
	cfg := poolcfg.Resolve(config.ModelPoolSettingAt(profileDir), config.ModelPoolPublicKeySettingAt(profileDir), lookup)
	out.WriteString(modelPoolHeading(cfg))
	out.WriteByte('\n')
	out.WriteString(poolRowsWaiting(config.ProfilePath(profileDir, "pool")))
	return out.String()
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

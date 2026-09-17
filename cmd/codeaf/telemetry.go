package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
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

// runTelemetryShow prints exactly what is waiting to leave the machine, the
// package's own rendering so the command and `codeaf telemetry show` a person
// reads in the notice are one and the same thing.
func runTelemetryShow(args []string) error {
	flags := telemetryFlags("show")
	if err := flags.Parse(args); err != nil {
		return err
	}
	fmt.Fprintln(usageOut, telemetry.Show())
	return nil
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

package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A BRIEF THAT BEGINS WITH "-" IS TEXT, NOT A FLAG.
//
// Measured, DeepSWE sweep of 2026-08-28: the `ink-grid-box-layout` cell was
// given a brief written as a bullet list and died in one second with
// `flag provided but not defined: - Update the display style property…` and a
// usage dump. The whole run was lost at argument parsing, before a model was
// asked anything.
//
// The old rule decided by shape — a leading dash meant a flag — and two
// different things wear that shape. The rule now asks the flag set, which is the
// only authority on which flags a command has.

// doFlags is `do`'s own flag set, built the way runDo builds it, so these tests
// exercise the real declarations rather than a fixture that could drift from
// them.
func doFlags() *flag.FlagSet {
	flags := flag.NewFlagSet("do", flag.ContinueOnError)
	flags.SetOutput(os.NewFile(0, os.DevNull))
	flags.String("db", "", "")
	flags.Bool("keep", false, "")
	flags.String("w", "", "")
	flags.Int("timeout", 0, "")
	flags.Bool("json", false, "")
	flags.String("model", "", "")
	return flags
}

func TestABriefThatBeginsWithADashIsTheBrief(t *testing.T) {
	brief := "- Update the display style property\n- Keep the grid measurable"
	flags := doFlags()
	if err := flags.Parse(reorder(flags, []string{"--json", brief})); err != nil {
		t.Fatalf("a bullet brief must parse: %v", err)
	}
	text, err := readText(flags.Name(), flags.Args())
	if err != nil {
		t.Fatal(err)
	}
	if text != brief {
		t.Fatalf("brief = %q, want the text the caller gave", text)
	}
	if flags.Lookup("json").Value.String() != "true" {
		t.Fatal("the flag before the brief must still be a flag")
	}
}

func TestFlagsAreStillFlagsAndTypesAreStillRefused(t *testing.T) {
	// A value flag takes the token after it even when the brief is in between.
	flags := doFlags()
	if err := flags.Parse(reorder(flags, []string{"- a bullet brief", "--model", "some/model"})); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := flags.Lookup("model").Value.String(); got != "some/model" {
		t.Fatalf("model = %q, want the value that followed the flag", got)
	}
	if got := strings.Join(flags.Args(), " "); got != "- a bullet brief" {
		t.Fatalf("brief = %q", got)
	}

	// A boolean does NOT eat the token after it: that token is the brief.
	flags = doFlags()
	if err := flags.Parse(reorder(flags, []string{"--keep", "- a bullet brief"})); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if flags.Lookup("keep").Value.String() != "true" {
		t.Fatal("--keep did not land")
	}
	if got := strings.Join(flags.Args(), " "); got != "- a bullet brief" {
		t.Fatalf("a boolean ate the brief: %q", got)
	}

	// And a misspelt flag is still refused by name rather than folded into the
	// brief, which is the whole reason this is not "anything with a dash is
	// text".
	flags = doFlags()
	if err := flags.Parse(reorder(flags, []string{"--dbb", "/tmp/x", "the brief"})); err == nil {
		t.Fatal("a flag this command does not have must be refused, not silently read as a brief")
	}
}

func TestTheTerminatorEndsTheFlags(t *testing.T) {
	flags := doFlags()
	if err := flags.Parse(reorder(flags, []string{"--keep", "--", "--json is part of my brief"})); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if flags.Lookup("json").Value.String() == "true" {
		t.Fatal("a token after -- must not be read as a flag")
	}
	if got := strings.Join(flags.Args(), " "); got != "--json is part of my brief" {
		t.Fatalf("brief = %q", got)
	}
}

func TestABriefMayArriveOnStandardInput(t *testing.T) {
	brief := "- the first bullet\n- the second"
	path := filepath.Join(t.TempDir(), "brief.txt")
	if err := os.WriteFile(path, []byte(brief+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	saved := os.Stdin
	os.Stdin = file
	defer func() { os.Stdin = saved }()

	// A lone `-` says so explicitly, which is the unix convention…
	flags := doFlags()
	if err := flags.Parse(reorder(flags, []string{"--keep", "-"})); err != nil {
		t.Fatalf("parse: %v", err)
	}
	text, err := readText(flags.Name(), flags.Args())
	if err != nil {
		t.Fatal(err)
	}
	if text != brief {
		t.Fatalf("brief = %q, want the piped text", text)
	}

	// …and so does no positional at all, when stdin is not a terminal.
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	text, err = readText("do", nil)
	if err != nil {
		t.Fatal(err)
	}
	if text != brief {
		t.Fatalf("brief = %q, want the piped text", text)
	}
}

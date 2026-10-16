package builtin

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// Every program this build carries is one that can run: its definition
// validates, and no two share a name.
func TestEveryCarriedProgramIsWellDefined(t *testing.T) {
	seen := map[string]bool{}
	for _, program := range All() {
		if err := program.Validate(); err != nil {
			t.Errorf("%v", err)
		}
		if seen[program.Name] {
			t.Errorf("two programs are called %s", program.Name)
		}
		seen[program.Name] = true
	}
}

// EVERY HELP PAGE A CARRIED PROGRAM PRINTS FITS EIGHTY CELLS: its own page and
// each command's, codeaf's shared flags included, the width every page of
// `codeaf --help` is held to.
func TestEveryCarriedProgramsHelpFitsEightyColumns(t *testing.T) {
	for _, program := range All() {
		lines := [][]string{{"--help"}}
		for _, command := range program.Commands {
			lines = append(lines, []string{command.Name, "--help"})
		}
		for _, line := range lines {
			var out bytes.Buffer
			if _, err := delegate.Parse(program, line, &out); err != delegate.ErrHelp {
				t.Fatalf("%s %v: err = %v, want the help", program.Name, line, err)
			}
			for at, printed := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
				if width := utf8.RuneCountInString(printed); width > 80 {
					t.Errorf("codeaf %s %s line %d draws %d cells: %q", program.Name, strings.Join(line, " "), at+1, width, printed)
				}
			}
		}
	}
}

// AN ENDING'S FACTS CHANGE NONE OF A PROGRAM'S WORDS: for every program this
// build carries, the ending line reads to the same step, text and outcome with
// its data as without it, and the typed facts ride beside them.
func TestEndingFactsLeaveEveryProgramsWordsAlone(t *testing.T) {
	at := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	for _, program := range All() {
		terminal := delegate.Terminal{Status: delegate.StatusPass, Message: "an ending in words", Data: map[string]json.RawMessage{
			"claim": json.RawMessage(`"it works"`), "observed": json.RawMessage(`"3 of 3 passed"`), "findings": json.RawMessage(`4`)}}
		plain := delegate.Terminal{Status: terminal.Status, Message: terminal.Message}
		with, okWith := program.Reader()(delegate.EndAction(at, terminal))
		without, okWithout := program.Reader()(delegate.EndAction(at, plain))
		if okWith != okWithout {
			t.Errorf("%s: the ending is shown %v with facts and %v without", program.Name, okWith, okWithout)
			continue
		}
		if with.Ending == nil || with.Ending.Claim != "it works" || with.Ending.Findings == nil || without.Ending != nil {
			t.Errorf("%s: facts = %+v without = %+v", program.Name, with.Ending, without.Ending)
		}
		with.Ending = nil
		if with != without {
			t.Errorf("%s: the ending reads %+v with facts and %+v without", program.Name, with, without)
		}
	}
}

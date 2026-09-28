package builtin

import (
	"bytes"
	"strings"
	"testing"
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

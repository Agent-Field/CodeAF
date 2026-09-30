package tui3

import (
	"strings"
	"testing"
	"time"
)

func TestShellOpeningNamesStayLiteralOnHomeAndResume(t *testing.T) {
	for _, command := range []string{`!for i in 1 2 3; do echo COMBO-$i; done`, `!my_script-name`, `!printf 'two  spaces;'`} {
		t.Run(command, func(t *testing.T) {
			lab := newHomeLab(t)
			now := lab.pin(time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC))
			mine := lab.session("-alpha", "aaaa000000000001", command, lab.workspace("alpha"), now)
			a := lab.app(mine)
			runCmd(a.openHome())
			if got := homeText(a); !strings.Contains(got, command) {
				t.Fatalf("Home changed the shell preview %q:\n%s", command, got)
			}
			for _, row := range []Session{{Title: command}, {Opening: command}} {
				if got := humanName(row); got != command {
					t.Fatalf("resume = %q, want %q", got, command)
				}
			}
			if got := chatTabName(command); got != command {
				t.Fatalf("tab = %q", got)
			}
		})
	}
}

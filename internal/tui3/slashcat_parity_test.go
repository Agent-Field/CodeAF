package tui3

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/slashcat"
)

// THE TERMINAL'S LIST AND THE WIRE'S ARE ONE TABLE. The wire serves
// internal/slashcat's rows and this package draws the same rows, so a word on
// one and not the other cannot exist; this pins the adapter between them. Every
// row the wire lists is a row here with the same name, argument form and
// aliases, every alias on the wire runs as the command the wire says it does,
// and the only text this surface adds is a place's chord.
func TestTheTerminalAndTheWireShareOneCommandTable(t *testing.T) {
	rows := slashcat.Rows()
	if len(baseCommands) != len(rows) {
		t.Fatalf("the terminal has %d rows and the table %d", len(baseCommands), len(rows))
	}
	for i, r := range rows {
		c := baseCommands[i]
		if c.name != r.Name || c.args != r.Args || len(c.alias) != len(r.Alias) {
			t.Fatalf("row %d: terminal %q %q %v, table %q %q %v", i, c.name, c.args, c.alias, r.Name, r.Args, r.Alias)
		}
		if (c.door == sendDoorTask) != (r.Door == slashcat.DoorTask) {
			t.Errorf("/%s: the send door disagrees with the table", r.Name)
		}
		if r.Place == "" && c.desc != r.Desc {
			t.Errorf("/%s: the terminal rewrote a description it should only have drawn", r.Name)
		}
		if r.Place != "" && c.desc != r.Desc+" · "+placeChord(placePages[r.Place]) {
			t.Errorf("/%s: %q is not the description plus its chord", r.Name, c.desc)
		}
	}
	for _, c := range appwire.BuildSlashCatalog(nil).Commands {
		if got := canonicalCommand(c.Name); got != c.Name {
			t.Errorf("/%s runs as /%s here", c.Name, got)
		}
		for _, a := range c.Aliases {
			if got := canonicalCommand(a); got != c.Name {
				t.Errorf("/%s is an alias of /%s on the wire and runs as /%s here", a, c.Name, got)
			}
		}
	}
}

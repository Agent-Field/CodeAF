package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestReturningHomeDetachesUnsentConversationAttachments(t *testing.T) {
	for _, door := range []string{"double space", "home", "other place"} {
		t.Run(door, func(t *testing.T) {
			lab := newHomeLab(t)
			now := lab.pin(time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC))
			mine := lab.session("-alpha", "aaaa000000000001", "here", lab.workspace("alpha"), now)
			a := lab.app(mine)
			a.chips = []chip{{path: "one.png"}, {path: "notes.txt", file: true}, {path: "two.png"}}
			a.input.setText("compare [image #1] with [image #2]")
			a.parks = []parked{{text: "already queued", chips: []chip{{path: "queued.png"}}}}
			wantDraft := "compare with"
			switch door {
			case "double space":
				a.input.setText("")
				wantDraft = ""
				drive(t, a, key(" "), key(" "))
			case "home":
				a.openHome()
			case "other place":
				a.showPage(pageTasks)
				a.openHome()
			}
			if !a.at(pageHome) || len(a.chips) != 0 {
				t.Fatalf("Home inherited attachments: %+v", a.chips)
			}
			if got := a.input.String(); got != wantDraft {
				t.Fatalf("conversation draft = %q", got)
			}
			if len(a.parks) != 1 || len(a.parks[0].chips) != 1 {
				t.Fatal("navigation changed a queued message")
			}
			a.chips = []chip{{path: "attached-on-home.png"}}
			drive(t, a, key("esc"))
			if len(a.chips) != 1 {
				t.Fatal("Leaving Home detached its own attachment")
			}
		})
	}
}

func TestAttachmentRemoveMarkIsVisibleAndClickable(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"one.png": 12, "two.png": 12})
	a.attach(dir + "/one.png")
	a.attach(dir + "/two.png")
	a.input.setText("compare [image #1] with [image #2]")
	labels := removableChipLabels(a.chips, a.pal)
	if !strings.HasSuffix(labels[0], " ×") {
		t.Fatalf("missing remove mark: %q", labels[0])
	}
	x := len(inputPad) + ansi.StringWidth(labels[0]) - 1
	y := trayRow(a)
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	if len(a.chips) != 1 || a.chips[0].name() != "two.png" || a.input.String() != "compare with [image #1]" {
		t.Fatalf("remove did not update the tray and draft: %+v %q", a.chips, a.input.String())
	}
}

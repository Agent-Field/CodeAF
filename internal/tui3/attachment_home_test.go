package tui3

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestReturningHomeKeepsUnsentConversationAttachments(t *testing.T) {
	for _, door := range []string{"double space", "home", "other place"} {
		t.Run(door, func(t *testing.T) {
			lab := newHomeLab(t)
			now := lab.pin(time.Date(2026, time.September, 28, 12, 0, 0, 0, time.UTC))
			mine := lab.session("-alpha", "aaaa000000000001", "here", lab.workspace("alpha"), now)
			a := lab.app(mine)
			a.chips = []chip{{path: "one.png"}, {path: "notes.txt", file: true}, {path: "two.png"}}
			a.input.setText("compare [image #1] with [image #2]")
			a.input.cursor = 4
			a.parks = []parked{{text: "already queued", chips: []chip{{path: "queued.png"}}}}
			wantDraft, wantCursor := a.input.String(), a.input.cursor
			switch door {
			case "double space":
				a.input.setText("")
				wantDraft, wantCursor = "", 0
				drive(t, a, key(" "), key(" "))
			case "home":
				a.openHome()
			case "other place":
				a.showPage(pageTasks)
				a.openHome()
			}
			if !a.at(pageHome) || len(a.home.chips) != 0 || a.home.box.String() != "" || a.home.carrying {
				t.Fatal("Home inherited a conversation draft")
			}
			if strings.Contains(homeText(a), "one.png") {
				t.Fatal("Home drew the conversation's tray")
			}
			a.home.chips = []chip{{path: "attached-on-home.png"}}
			a.home.box.setText("a different request")
			drive(t, a, key("esc"))
			if a.input.String() != wantDraft || a.input.cursor != wantCursor || len(a.chips) != 3 || a.chips[0].path != "one.png" {
				t.Fatalf("conversation draft changed: %q, cursor %d, chips %+v", a.input.String(), a.input.cursor, a.chips)
			}
			if len(a.parks) != 1 || len(a.parks[0].chips) != 1 {
				t.Fatal("navigation changed a queued message")
			}
			a.openHome()
			if a.home.box.String() != "" || len(a.home.chips) != 0 {
				t.Fatal("returning Home did not start clean")
			}
		})
	}
}

func TestStartingFromHomeKeepsThePreviousConversationsDraft(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		for _, target := range []string{"same project", "other project", "project row"} {
			t.Run(fmt.Sprintf("fresh=%t/%s", fresh, target), func(t *testing.T) {
				lab := newStartLab(t)
				a := lab.app()
				a.open = func(string, string) (Conversation, error) {
					t.Fatal("a held conversation was reopened instead of restored")
					return Conversation{}, nil
				}
				if fresh {
					a.turn = 0
				}
				old := a.file
				a.input.setText("unfinished [image #1]")
				a.input.cursor = 3
				a.chips = []chip{{path: "old.png"}, {path: "old.txt", file: true}}
				a.openHome()
				a.home.chips = []chip{{path: "new.txt", file: true}}
				if target == "project row" {
					spend(t, a, a.homeStartInProject("/tmp/another-project"))
				} else {
					if target == "other project" {
						a.target.where = "/tmp/another-project"
					}
					opened, ok := a.homeOpenAtTarget(nil)
					if !ok {
						t.Fatal("Home did not open the new conversation")
					}
					unfold(t, a, opened)
				}
				if a.input.String() != "" || len(a.chips) != 1 || a.chips[0].path != "new.txt" {
					t.Fatalf("new conversation inherited the old draft: %q %+v", a.input.String(), a.chips)
				}
				a.input.setText("second conversation's draft")
				a.openHome()
				if _, refusal := a.openSession(Session{File: old}); refusal != "" {
					t.Fatal(refusal)
				}
				a.closeHome()
				if a.input.String() != "unfinished [image #1]" || a.input.cursor != 3 || len(a.chips) != 2 || a.chips[0].path != "old.png" {
					t.Fatalf("return lost the old draft: %q, cursor %d, chips %+v", a.input.String(), a.input.cursor, a.chips)
				}
			})
		}
	}
}

func TestHomeCreationRefusalKeepsBothDrafts(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.input.setText("unsent conversation prompt")
	a.chips = []chip{{path: "old.png"}}
	a.openHome()
	a.home.box.setText("new conversation prompt")
	a.home.chips = []chip{{path: "new.png"}}
	lab.refuse = errors.New("cannot start")
	renewed, started := a.homeOpenAtTarget(nil)
	if !started {
		t.Fatal("the door was not queued")
	}
	unfold(t, a, renewed)
	if !a.at(pageHome) || a.home.box.String() != "new conversation prompt" || len(a.home.chips) != 1 {
		t.Fatal("refusal lost Home's draft")
	}
	a.closeHome()
	if a.input.String() != "unsent conversation prompt" || len(a.chips) != 1 || a.chips[0].path != "old.png" {
		t.Fatal("refusal lost the conversation's draft")
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

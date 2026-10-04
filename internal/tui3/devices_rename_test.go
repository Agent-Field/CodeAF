package tui3

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// typeText types a string one key at a time, as a person does.
func (r *pairRig) typeText(s string) {
	for _, c := range s {
		r.press(string(c))
	}
}

func renameApp(t *testing.T, door *fakeApprovals) (*app, *pairRig) {
	t.Helper()
	a, r := approveApp(t, door)
	r.slash("/devices")
	r.until("the list", func() bool { c, ok := a.pair.card.(*deviceCard); return ok && c.loaded })
	return a, r
}

// The field opens on this device's own row with its current name in it, so
// enter keeps the name and typing edits it.
func TestNKeyOpensTheFieldPrefilledWithTheCurrentName(t *testing.T) {
	a, r := renameApp(t, devicesFake())
	r.press("n")
	got := plain(frame(a))
	if !strings.Contains(got, devicesNameAsk+"this mac▏") || !strings.Contains(got, devicesNameKeys) {
		t.Fatalf("no prefilled field:\n%s", got)
	}
}

func TestTypingANameSavesItThroughTheDoorAndTheRowShowsIt(t *testing.T) {
	door := devicesFake()
	a, r := renameApp(t, door)
	r.press("n", "ctrl+u")
	r.typeText("studio r")
	r.press("enter")
	r.until("saved", func() bool { return strings.Contains(plain(frame(a)), "● studio r") })
	if door.did() != "rename:studio r" {
		t.Fatalf("door: %s", door.did())
	}
	if c := a.pair.card.(*deviceCard); c.typing() {
		t.Fatal("the field stayed open after the name was saved")
	}
}

// A name made of the card's own key letters must be typed, not obeyed: `r` in a
// name never revokes and `esc` closes the field and not the card.
func TestWhileTypingEveryLetterIsTextAndEscClosesOnlyTheField(t *testing.T) {
	door := devicesFake()
	a, r := renameApp(t, door)
	r.press("n", "ctrl+u")
	r.typeText("rnd")
	if door.did() != "" {
		t.Fatalf("a letter typed into the name reached the door: %s", door.did())
	}
	r.press("esc")
	c, ok := a.pair.card.(*deviceCard)
	if !ok || c.typing() || !a.pair.open {
		t.Fatalf("esc did not close just the field: card %v open %t", ok, a.pair.open)
	}
	if door.did() != "" {
		t.Fatalf("esc saved a name: %s", door.did())
	}
}

func TestARefusedNameIsSaidAndTheFieldStaysOpen(t *testing.T) {
	door := devicesFake()
	door.renameErr = errors.New("a device name is at most 32 characters")
	a, r := renameApp(t, door)
	r.press("n", "enter")
	r.until("the reason", func() bool { return strings.Contains(plain(frame(a)), door.renameErr.Error()) })
	if c := a.pair.card.(*deviceCard); !c.typing() {
		t.Fatal("a refused name closed the field, so the person must start over")
	}
}

func TestOnlyThisDevicesRowCanBeNamed(t *testing.T) {
	a, r := renameApp(t, devicesFake())
	r.press("down", "n")
	if c := a.pair.card.(*deviceCard); c.typing() || c.line != devicesNameOwn {
		t.Fatalf("another device's row opened the field or said nothing: %q", c.line)
	}
}

// THE FIELD, AS DRAWN, at the two widths the card is pinned at.
func TestRenameFieldGolden(t *testing.T) {
	golden := map[int][]string{
		80: {
			"› ● this mac  Mac    this device",
			"  ○ dumb      Mac    seen 3h ago",
			"  ○ spark     Linux  offline",
			"name this device: this mac▏",
		},
		40: {
			"› ● this mac  Mac    this device",
			"  ○ dumb      Mac    seen 3h ago",
			"  ○ spark     Linux  offline",
			"name this device: this mac▏",
		},
	}
	now := time.Now()
	for width, want := range golden {
		door := devicesFake()
		door.devices[2].LastSeen = now.Add(-3*time.Hour - time.Minute)
		a, r := renameApp(t, door)
		a.width = width
		r.press("n")
		c := a.pair.card.(*deviceCard)
		var got []string
		for _, line := range c.rows(width, now, a.pal) {
			got = append(got, strings.TrimRight(plain(line), " "))
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("width %d:\n%s\nwant:\n%s", width, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
}

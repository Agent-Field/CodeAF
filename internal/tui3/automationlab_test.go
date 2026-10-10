package tui3

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/automation"
)

// automationsSeamOf is the seam a window on the machine wires (cmd/codeaf's
// v3AutomationsSeam), over a store a test owns: THE REAL STORE, so what these
// tests read is what a window reads and not a second answer to it.
func automationsSeamOf(store *automation.Store) AutomationsSeam {
	presence := automation.NewPresence(store.Root())
	return AutomationsSeam{
		List:      store.List,
		Runs:      store.Runs,
		Changes:   store.Changes,
		Cursor:    store.Cursor,
		Active:    store.Active,
		Windows:   presence.Count,
		Create:    store.Create,
		Update:    store.Update,
		SetStatus: store.SetStatus,
		Delete:    store.Delete,
		RunNow: func(id string) error {
			_, err := store.QueueNow(id)
			return err
		},
		StopRun: store.RequestStop,
		Claim: func(id int64) bool {
			first, err := store.Claim(id)
			return err == nil && first
		},
		Zone: "UTC",
	}
}

// automationLab is a surface with an automations store behind it, holding the
// automations given — each made in this conversation unless it names another —
// and the watcher's first reading already landed.
func automationLab(t *testing.T, items ...automation.Automation) (*app, *automation.Store) {
	t.Helper()
	store, err := automation.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 140, 30
	a.file = filepath.Join(t.TempDir(), "transcript.jsonl")
	for _, item := range items {
		if strings.TrimSpace(item.Workspace) == "" {
			item.Workspace = t.TempDir()
		}
		if item.Origin.Transcript == "" {
			item.Origin.Transcript = a.file
		}
		if _, err := store.Create(item); err != nil {
			t.Fatal(err)
		}
	}
	a.autos = automationsSeamOf(store)
	readAutomationsNow(t, a)
	return a, store
}

// readAutomationsNow takes one reading of the store and lands it, the way the
// watcher's beat does, without waiting out the beat.
func readAutomationsNow(t *testing.T, a *app) {
	t.Helper()
	cmd := a.readAutomations()
	if cmd == nil {
		t.Fatal("the surface has no automations seam to read")
	}
	msg, ok := cmd().(automationsReadMsg)
	if !ok {
		t.Fatal("the reading did not come back as a reading")
	}
	a.automationsRead(msg)
}

// labReminder, labWork and labWatch are one of each kind, due in an hour.
func labReminder(title string) automation.Automation {
	return automation.Automation{Title: title, Schedule: automation.Schedule{At: time.Now().Add(time.Hour)},
		Action: automation.Action{Say: "time to " + title}}
}

func labWork(title, every string) automation.Automation {
	return automation.Automation{Title: title, Schedule: automation.Schedule{Every: every},
		Action: automation.Action{Do: "do " + title}}
}

func labWatch(title string) automation.Automation {
	return automation.Automation{Title: title, Schedule: automation.Schedule{Every: "15m"},
		Look:   &automation.Look{Command: "true", Condition: "it says so"},
		Action: automation.Action{Say: title + " happened"}}
}

// automationsScreen is the automations place as a reader sees it, blank lines
// dropped.
func automationsScreen(a *app) string {
	width, height := a.size()
	lines, _, _, _ := a.placeDraw(placeAutomations{}, width, height)
	var out []string
	for _, line := range lines {
		if text := strings.TrimRight(plain(line), " "); strings.TrimSpace(text) != "" {
			out = append(out, text)
		}
	}
	return strings.Join(out, "\n")
}

// automationsPlaceFrame is the place's frame and which list row each screen
// line draws, the way the pointer resolves a press.
func automationsPlaceFrame(a *app) ([]string, []int) {
	lines, hits, _, _ := a.placeDraw(placeAutomations{}, a.width, a.height)
	return lines, placeLineHits(hits)
}

package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/aforge-v2/internal/config"
)

// Reading back to the request is not a request to expose the worker's tools.
// Repeated real wheel events also cover trackpad momentum after reaching the top.
func TestFinishedTaskScrollingKeepsWorkCollapsed(t *testing.T) {
	a := openRoomOn(t, landedJournal(t))
	landRoom(t, a)
	for range 24 {
		drive(t, a, tea.MouseWheelMsg{X: 2, Y: a.bodyTop() + 2, Button: tea.MouseWheelUp})
	}
	page := roomText(a)
	for _, gone := range []string{"Reading the site first", "index.html", "generate_image"} {
		if strings.Contains(page, gone) {
			t.Fatalf("scrolling exposed %q:\n%s", gone, page)
		}
	}
	for _, want := range []string{"Draw two posters", "▸ worked", "Both posters"} {
		if !strings.Contains(page, want) {
			t.Fatalf("scrolling lost %q:\n%s", want, page)
		}
	}
}

// An explicit disclosure must advertise the same state the renderer uses.
// Closing the outer fold hides even captions the reader previously expanded.
func TestFinishedTaskDisclosureArrowTracksVisibleWork(t *testing.T) {
	a := openRoomOn(t, landedJournal(t))
	landRoom(t, a)
	drive(t, a, key("ctrl+e"))
	page := roomText(a)
	if !strings.Contains(page, "▾ worked") || strings.Contains(page, "index.html") {
		t.Fatalf("open work must show an honest arrow and a caption outline:\n%s", page)
	}
	openFirstCaption(t, a)
	if page = roomText(a); !strings.Contains(page, "index.html") {
		t.Fatalf("explicit caption did not open:\n%s", page)
	}
	drive(t, a, key("ctrl+e"))
	page = roomText(a)
	if !strings.Contains(page, "▸ worked") || strings.Contains(page, "index.html") || strings.Contains(page, "Reading the site first") {
		t.Fatalf("closing the work left details visible:\n%s", page)
	}
}

// The global preference and stopped turns use the same effective state as the
// drawing pass, rather than treating the chevron as decoration.
func TestWorkDisclosureArrowHonorsPreferenceAndStoppedTurns(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		for _, mode := range []string{config.WorkFold, config.WorkOpen} {
			a := newTestApp(&fakeAgent{model: "m"})
			a.workMode = mode
			f := workfold{key: 1, stopped: stopped}
			got := a.workfoldLabel(a.conversation(), f)
			want := "▸ "
			if mode == config.WorkOpen {
				want = "▾ "
			}
			if !strings.HasPrefix(got, want) {
				t.Fatalf("mode=%s stopped=%v label=%q", mode, stopped, got)
			}
		}
	}
}

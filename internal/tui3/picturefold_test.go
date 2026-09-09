package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The pointer and keyboard must operate the actual visible control in both
// transcript owners, including after the task's assembled page was cached.
func TestPictureExpansionThroughTheVisibleDoor(t *testing.T) {
	for _, room := range []bool{false, true} {
		name := "chat"
		if room {
			name = "task"
		}
		t.Run(name, func(t *testing.T) {
			a, agent, _ := roomApp(t)
			a.pal = newPalette(tokens.TrueColor, false)
			path := writePicture(t, t.TempDir(), "chart.png", wideTestPicture())
			if room {
				agent.journal = roomJournal(t, `{"type":"message","role":"user","content":"inspect this","parts":[{"type":"image","path":`+strconvQuote(path)+`}]}`)
				clickRail(t, a, 0)
			} else {
				a.entries = []entry{{kind: entryUser, text: "inspect this [#1 chart.png]", pictures: []string{path}, picturesHere: true}}
			}
			check := func(open bool) {
				t.Helper()
				d := a.bodyDeck()
				e := &d.entries[0]
				if e.picturesOpen != open {
					t.Fatalf("open = %v, want %v", e.picturesOpen, open)
				}
				rows := a.entryRows(d, 0, a.bodyWidth())
				n := paintedRows(rows)
				if n < 1 || n > pictureRowBudget(open) {
					t.Fatalf("painted %d rows, budget %d", n, pictureRowBudget(open))
				}
				if open && n <= pictureCompactRows {
					t.Fatalf("expansion did not enlarge the image: %d rows", n)
				}
				if !strings.Contains(plain(strings.Join(rows, "\n")), "inspect this") {
					t.Fatal("the picture fold hid the message")
				}
			}
			check(false)
			found := false
			for y := a.bodyTop(); y < a.bodyTop()+a.viewHeight(); y++ {
				if r, ok := a.rowAt(y); ok && r.hit == hitPictures {
					drive(t, a, tea.MouseClickMsg{Y: y, Button: tea.MouseLeft})
					drive(t, a, tea.MouseReleaseMsg{Y: y, Button: tea.MouseLeft})
					found = true
					break
				}
			}
			if !found {
				t.Fatal("no visible image expansion control")
			}
			check(true)
			drive(t, a, key("alt+i"))
			check(false)
		})
	}
}

func TestImageToolsUseTheAttachmentCompactBudget(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.TrueColor, false)
	path := writePicture(t, t.TempDir(), "chart.png", wideTestPicture())
	e := &entry{kind: entryTool, tool: "view_image", status: toolOK}
	e.detail.Args = `{"path":` + strconvQuote(path) + `}`
	rows, ok := a.pictureThumb(e, 100, previewWindow)
	if !ok || paintedRows(rows) > pictureCompactRows {
		t.Fatalf("compact tool preview: %v, %d rows", ok, paintedRows(rows))
	}
}

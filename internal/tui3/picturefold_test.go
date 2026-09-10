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
				if (e.pictureExpanded != 0) != open {
					t.Fatalf("open = %v, want %v", e.pictureExpanded, open)
				}
				rows := a.entryRows(d, 0, a.bodyWidth())
				for _, r := range a.mediaRows(e, 0, a.bodyWidth(), userLead) {
					rows = append(rows, r.text)
				}
				n := paintedRows(rows)
				if !open && n != 0 {
					t.Fatalf("collapsed image painted %d rows", n)
				}
				if open && (n < 1 || n > pictureRowsMax) {
					t.Fatalf("expanded image painted %d rows", n)
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

// Geometry and original-file clicks use the same rows at phone and desktop
// widths, on colourless terminals too, without needing terminal hyperlink keys.
func TestMediaOriginalClickMatchesItsPaintedAction(t *testing.T) {
	for _, width := range []int{40, 100} {
		for _, tool := range []bool{false, true} {
			a := newTestApp(&fakeAgent{model: "m"})
			a.width, a.height = width, 30
			a.pal = newPalette(tokens.NoColor, true)
			path := writePicture(t, t.TempDir(), "original.png", wideTestPicture())
			e := entry{kind: entryUser, text: "look", pictures: []string{path}, picturesHere: true}
			if tool {
				e = entry{kind: entryTool, tool: "view_image", status: toolOK, detail: toolDetail{Args: `{"path":` + strconvQuote(path) + `}`, Output: "looked at it"}}
			}
			a.entries = []entry{e}
			opened := watchOpener(t)
			found := false
			for y := a.bodyTop(); y < a.bodyTop()+a.viewHeight(); y++ {
				if r, ok := a.rowAt(y); ok && r.hit == hitPictures {
					if !r.pictureOpen.pressable() {
						t.Fatalf("no original action: %q", plain(r.text))
					}
					x := r.pictureOpen.from
					drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
					drive(t, a, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
					found = true
					break
				}
			}
			if !found || len(*opened) != 1 || (*opened)[0] != path {
				t.Fatalf("width %d tool %v: open %v, found %v", width, tool, *opened, found)
			}
			if a.entries[0].pictureExpanded != 0 || a.entries[0].open {
				t.Fatal("opening the original also expanded the transcript")
			}
		}
	}
}

func TestManyCollapsedImagesNeverDecodeAndOnlyOneExpands(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.TrueColor, false)
	path := writePicture(t, t.TempDir(), "screen.png", wideTestPicture())
	e := entry{kind: entryUser, picturesHere: true}
	for i := 0; i < pictureCacheMax+1; i++ {
		e.pictures = append(e.pictures, path)
	}
	a.entries = []entry{e}
	rows := a.mediaRows(&a.entries[0], 0, 80, userLead)
	if len(rows) != len(e.pictures) || len(a.previews) != 0 {
		t.Fatalf("collapsed: %d rows, %d cache entries", len(rows), len(a.previews))
	}
	a.togglePictureAt(0, 0)
	a.togglePictureAt(0, 1)
	rows = a.mediaRows(&a.entries[0], 0, 80, userLead)
	pixels := 0
	for _, r := range rows {
		if strings.Contains(r.text, halfBlock) {
			pixels++
		}
	}
	if a.entries[0].pictureExpanded != 2 || pixels < 1 || pixels > pictureRowsMax {
		t.Fatalf("second picture: index %d, %d rows", a.entries[0].pictureExpanded, pixels)
	}
	a.togglePictureAt(0, 1)
	if a.entries[0].pictureExpanded != 0 {
		t.Fatal("second click did not collapse")
	}
}

func TestMediaOriginalInHostedSessionUsesTheCorrectMachine(t *testing.T) {
	a, wire, _ := hostedFixture(t)
	opened := watchOpener(t)
	local := writePicture(t, t.TempDir(), "here.png", wideTestPicture())
	run(a.openMediaOriginal(mediaItem{path: local, here: true}))
	if len(*opened) != 1 || (*opened)[0] != local || len(wire.fetched()) != 0 {
		t.Fatalf("local attachment went to the host: %v", *opened)
	}
	far := "/srv/app/out/there.png"
	wire.farFile(far, "image/png", "original bytes", 1700)
	cmd := a.openMediaOriginal(mediaItem{path: far})
	msg := run(cmd)
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("remote open did not use asynchronous fetch: %T", msg)
	}
	answer := batch[0]()
	result, ok := answer.(remoteOpenedMsg)
	if !ok || result.err != nil {
		t.Fatalf("remote open: %#v", answer)
	}
	if len(*opened) != 2 || (*opened)[1] == far {
		t.Fatalf("remote file was not mirrored: %v", *opened)
	}
}

func TestUnavailableMediaKeepsItsOriginalAction(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.TrueColor, false)
	e := entry{kind: entryUser, pictures: []string{"/missing/screenshot.png"}, picturesHere: true, pictureExpanded: 1}
	rows := a.mediaRows(&e, 0, 60, userLead)
	if len(rows) != 2 || !rows[0].pictureOpen.pressable() || !strings.Contains(plain(rows[1].text), "Preview unavailable") {
		t.Fatalf("missing picture: %#v", rows)
	}
}

func TestClickingPreviewPixelsOpensFullQuality(t *testing.T) {
	for _, tool := range []bool{false, true} {
		for _, width := range []int{40, 100} {
			a := newTestApp(&fakeAgent{model: "m"})
			a.width, a.height = width, 40
			a.pal = newPalette(tokens.TrueColor, false)
			path := writePicture(t, t.TempDir(), "original.png", wideTestPicture())
			a.entries = []entry{{kind: entryUser, text: "look", pictures: []string{path}, picturesHere: true, pictureExpanded: 1}}
			if tool {
				a.entries = []entry{{kind: entryTool, tool: "view_image", status: toolOK, detail: toolDetail{Args: `{"path":` + strconvQuote(path) + `}`, Output: "seen"}}}
				a.openTool(0)
			}
			opened := watchOpener(t)
			clicked := false
			if a.expandShowing() {
				lines, hits, _, _ := a.expandFrame(width, 40)
				for y, line := range lines {
					if hits[y] == expandHitOriginal && strings.Contains(line, halfBlock) {
						drive(t, a, tea.MouseClickMsg{X: 3, Y: y, Button: tea.MouseLeft})
						clicked = true
						break
					}
				}
			} else {
				for y := a.bodyTop(); y < a.bodyTop()+a.viewHeight(); y++ {
					if r, ok := a.rowAt(y); ok && r.hit == hitPictureOriginal && strings.Contains(r.text, halfBlock) {
						drive(t, a, tea.MouseClickMsg{X: 3, Y: y, Button: tea.MouseLeft})
						drive(t, a, tea.MouseReleaseMsg{X: 3, Y: y, Button: tea.MouseLeft})
						clicked = true
						break
					}
				}
			}
			if !clicked || len(*opened) != 1 || (*opened)[0] != path {
				t.Fatalf("width=%d tool=%v clicked=%v opened=%v", width, tool, clicked, *opened)
			}
			if a.expandShowing() {
				drive(t, a, key("alt+o"))
				if len(*opened) != 2 {
					t.Fatal("phone sheet swallowed the original shortcut")
				}
			}
		}
	}
}

func TestPlainSSHDoesNotLaunchAViewerOnTheServer(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.remote = true
	opened := watchOpener(t)
	if cmd := a.openMediaOriginal(mediaItem{path: "/server/shot.png", here: true}); cmd != nil {
		t.Fatal("plain SSH tried to launch a viewer")
	}
	if len(*opened) != 0 {
		t.Fatal("opened a viewer on the server")
	}
	found := false
	for _, e := range a.entries {
		if strings.Contains(e.text, "aforge --host") && strings.Contains(e.text, "/server/shot.png") {
			found = true
		}
	}
	if !found {
		t.Fatal("SSH original action supplied no recovery path")
	}
}

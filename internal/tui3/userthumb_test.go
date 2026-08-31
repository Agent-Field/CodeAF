package tui3

import (
	"image"
	"image/color"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

func userEntryRows(t *testing.T, a *app, width int) []string {
	t.Helper()
	for i := range a.entries {
		if a.entries[i].kind == entryUser {
			a.entries[i].stale = true
			return a.entryRows(a.conversation(), i, width)
		}
	}
	t.Fatal("no user entry")
	return nil
}

func solidTestPicture(shade color.NRGBA) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetNRGBA(x, y, shade)
		}
	}
	return img
}

// Q1: a live truecolor picture message keeps its marker and draws underneath it.
func TestAUsersPictureDrawsUnderTheSentLine(t *testing.T) {
	a, _, dir := attachLab(t, nil)
	a.pal = newPalette(tokens.TrueColor, false)
	path := writePicture(t, dir, "shot.png", wideTestPicture())
	a.attach(path)
	typeLine(t, a, "what is wrong here")

	rows := userEntryRows(t, a, 100)
	body := strings.Join(rows, "\n")
	if !strings.Contains(plain(body), "what is wrong here [image #1] [#1 shot.png]") {
		t.Fatalf("the marker sentence changed:\n%s", plain(body))
	}
	if paintedRows(rows) == 0 {
		t.Fatalf("the sent picture drew no thumbnail:\n%s", body)
	}
	for _, row := range rows {
		if strings.Contains(row, halfBlock) && !strings.HasPrefix(row, userLead) {
			t.Fatalf("the thumbnail is not indented under the sentence: %q", row)
		}
	}
	if marker, picture := strings.Index(plain(body), "[#1 shot.png]"), strings.Index(plain(body), halfBlock); marker < 0 || picture < marker {
		t.Fatalf("the picture did not land below its marker:\n%s", plain(body))
	}
}

// Q2: user thumbnails use the same colour, glyph, reader, and width rungs as tool thumbnails.
func TestAUsersPictureDegradesByTheThumbnailRungs(t *testing.T) {
	dir := t.TempDir()
	path := writePicture(t, dir, "shot.png", wideTestPicture())
	for _, tc := range []struct {
		name  string
		pal   palette
		width int
		draws bool
		cube  bool
	}{
		{"truecolor", newPalette(tokens.TrueColor, false), 60, true, false},
		{"the xterm cube", newPalette(tokens.ANSI256, false), 60, true, true},
		{"sixteen colours", newPalette(tokens.ANSI16, false), 60, false, false},
		{"no colour", newPalette(tokens.NoColor, false), 60, false, false},
		{"no box drawing", newPalette(tokens.TrueColor, true), 60, false, false},
		{"read aloud", linearPalette(), 60, false, false},
		{"too narrow", newPalette(tokens.TrueColor, false), pictureColsMin + userLeadCols - 1, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			a.pal = tc.pal
			e := entry{kind: entryUser, text: "look [#1 shot.png]", pictures: []string{path}, picturesHere: true}
			got := a.renderEntry(0, &e, tc.width)
			base := e
			base.pictures = nil
			want := a.renderEntry(0, &base, tc.width)
			if tc.draws && paintedRows(got) == 0 {
				t.Fatal("no picture on a terminal that can carry one")
			}
			if !tc.draws && !reflect.DeepEqual(got, want) {
				t.Fatalf("the user block changed where no picture can draw:\n got %#v\nwant %#v", got, want)
			}
			if tc.cube && !strings.Contains(strings.Join(got, ""), "\x1b[38;5;") {
				t.Fatal("the ANSI256 picture did not use the xterm cube")
			}
		})
	}
}

// Q3: picture-only and mixed attachment messages keep every marker and draw only pictures.
func TestPictureOnlyAndMixedMessagesDrawTheirPictures(t *testing.T) {
	t.Run("picture only", func(t *testing.T) {
		a, _, dir := attachLab(t, nil)
		a.pal = newPalette(tokens.TrueColor, false)
		path := writePicture(t, dir, "shot.png", wideTestPicture())
		a.attach(path)
		drive(t, a, key("enter"))
		rows := userEntryRows(t, a, 80)
		if !strings.Contains(plain(strings.Join(rows, "\n")), "[image #1] [#1 shot.png]") || paintedRows(rows) == 0 {
			t.Fatalf("the picture-only message is incomplete:\n%s", plain(strings.Join(rows, "\n")))
		}
	})

	t.Run("picture and file", func(t *testing.T) {
		a, _, dir := fileLab(t, "", map[string]int{"server.log": 8})
		a.pal = newPalette(tokens.TrueColor, false)
		path := writePicture(t, dir, "shot.png", wideTestPicture())
		a.attach(path)
		a.attachFile(filepath.Join(dir, "server.log"))
		typeLine(t, a, "look")
		rows := userEntryRows(t, a, 80)
		body := plain(strings.Join(rows, "\n"))
		if !strings.Contains(body, "[#1 shot.png] [server.log]") || paintedRows(rows) == 0 {
			t.Fatalf("the mixed message is incomplete:\n%s", body)
		}
	})
}

// Q4: two user pictures stack in tray order within the shared wide and phone caps.
func TestTwoUserPicturesStackInTrayOrderWithinTheTierCap(t *testing.T) {
	a, _, dir := attachLab(t, nil)
	a.pal = newPalette(tokens.TrueColor, false)
	red := writePicture(t, dir, "red.png", solidTestPicture(color.NRGBA{R: 255, A: 255}))
	blue := writePicture(t, dir, "blue.png", solidTestPicture(color.NRGBA{B: 255, A: 255}))
	a.attach(red)
	a.attach(blue)
	typeLine(t, a, "compare")

	for _, tc := range []struct {
		name string
		wide int
		cap  int
	}{
		{"wide", 100, previewWindow},
		{"phone", phoneWidth, previewPhoneWindow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := userEntryRows(t, a, tc.wide)
			painted := paintedRows(rows)
			if painted == 0 || painted > 2*tc.cap {
				t.Fatalf("two pictures drew %d rows, want 1..%d", painted, 2*tc.cap)
			}
			body := strings.Join(rows, "\n")
			redRows, blueRows := 0, 0
			for _, row := range rows {
				if strings.Contains(row, "38;2;255;0;0") {
					redRows++
				}
				if strings.Contains(row, "38;2;0;0;255") {
					blueRows++
				}
			}
			if redRows == 0 || redRows > tc.cap || blueRows == 0 || blueRows > tc.cap {
				t.Fatalf("red drew %d rows and blue %d, want each within 1..%d", redRows, blueRows, tc.cap)
			}
			if first, second := strings.Index(body, "38;2;255;0;0"), strings.Index(body, "38;2;0;0;255"); first < 0 || second < first {
				t.Fatalf("the pictures did not draw red then blue:\n%q", body)
			}
			if strings.Contains(plain(body), "more lines") {
				t.Fatal("a user thumbnail grew a cap foot")
			}
		})
	}
}

// Q5: replay draws a picture still on disk and keeps only the marker when it moved.
func TestReplayedUserPicturesDrawOnlyWhileTheirFilesAreHere(t *testing.T) {
	dir := t.TempDir()
	here := writePicture(t, dir, "here.png", wideTestPicture())
	for _, tc := range []struct {
		name  string
		path  string
		draws bool
	}{
		{"still here", here, true},
		{"moved", filepath.Join(dir, "gone.png"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := &fakeAgent{model: "m", past: []session.DisplayEntry{{Role: "user", ImageRefs: []string{tc.path}}}}
			a := newTestApp(agent)
			a.pal = newPalette(tokens.TrueColor, false)
			a.entries = nil
			a.replay()
			rows := userEntryRows(t, a, 80)
			if !strings.Contains(plain(strings.Join(rows, "\n")), "[#1 "+filepath.Base(tc.path)+"]") {
				t.Fatal("the replayed marker was lost")
			}
			if got := paintedRows(rows) > 0; got != tc.draws {
				t.Fatalf("drawn = %v, want %v", got, tc.draws)
			}
		})
	}
}

// Q6: a task room draws the same thumbnail for a journaled user picture.
func TestARoomDrawsItsUsersPicture(t *testing.T) {
	a, agent, _ := roomApp(t)
	a.pal = newPalette(tokens.TrueColor, false)
	path := writePicture(t, t.TempDir(), "chart.png", wideTestPicture())
	agent.journal = roomJournal(t,
		`{"type":"message","role":"user","content":"what is wrong",`+
			`"parts":[{"type":"image","path":`+strconvQuote(path)+`}]}`,
	)
	clickRail(t, a, 0)
	if page := roomText(a); !strings.Contains(page, "[#1 chart.png]") || !strings.Contains(page, halfBlock) {
		t.Fatalf("the room did not draw the journaled picture:\n%s", page)
	}
}

// Q8: a pictured room brief folds and counts only its wrapped words.
func TestAPicturedRoomBriefCountsTextAndStillDrawsTheThumbnail(t *testing.T) {
	said := briefWords(160)
	a, agent, _ := roomApp(t)
	a.pal = newPalette(tokens.TrueColor, false)
	path := writePicture(t, t.TempDir(), "brief.png", wideTestPicture())
	agent.journal = roomJournal(t,
		`{"type":"message","role":"user","content":`+strconvQuote(said)+`,`+
			`"parts":[{"type":"image","path":`+strconvQuote(path)+`}]}`,
	)
	clickRail(t, a, 0)
	hidden := len(wrap(a.room.entries[0].text, a.bodyWidth()-userLeadCols)) - briefFoldLines
	door, ok := briefDoor(a)
	if !ok || !strings.Contains(plain(door.text), "…"+itoa(hidden)+" more lines") {
		t.Fatalf("the fold did not count %d text lines: %q", hidden, plain(door.text))
	}
	if !strings.Contains(roomText(a), halfBlock) {
		t.Fatal("the folded instruction lost its thumbnail")
	}
}

// Q9: the working-context clause lands before, and never on, painted picture rows.
func TestTheTurnContextNeverLandsOnAUsersPictureRow(t *testing.T) {
	dir := t.TempDir()
	path := writePicture(t, dir, "shot.png", wideTestPicture())
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.TrueColor, false)
	e := entry{kind: entryUser, text: "use this [#1 shot.png]", context: "designing the parser",
		pictures: []string{path}, picturesHere: true}
	rows := a.renderEntry(0, &e, 80)
	contextAt, pictureAt := -1, -1
	for i, row := range rows {
		if strings.Contains(plain(row), "designing the parser") {
			contextAt = i
		}
		if strings.Contains(row, halfBlock) {
			if strings.Contains(plain(row), "designing the parser") {
				t.Fatalf("the context landed on a picture row: %q", row)
			}
			if pictureAt < 0 {
				pictureAt = i
			}
		}
	}
	if contextAt < 0 || pictureAt <= contextAt {
		t.Fatalf("context row %d, first picture row %d", contextAt, pictureAt)
	}
}

// Q12: restyling one user row reuses the same decoded preview cache entry.
func TestAUsersPicturePreviewDoesNotGrowAcrossRestyles(t *testing.T) {
	dir := t.TempDir()
	path := writePicture(t, dir, "shot.png", wideTestPicture())
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.TrueColor, false)
	a.entries = []entry{{kind: entryUser, text: "look [#1 shot.png]", pictures: []string{path}, picturesHere: true}}
	userEntryRows(t, a, 80)
	kept := len(a.previews)
	if kept == 0 {
		t.Fatal("nothing was cached")
	}
	for i := 0; i < 5; i++ {
		a.restyleEntries()
		userEntryRows(t, a, 80)
	}
	if len(a.previews) != kept {
		t.Fatalf("cache grew from %d to %d across restyles", kept, len(a.previews))
	}
}

func strconvQuote(text string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(text, `\`, `\\`), `"`, `\"`) + `"`
}

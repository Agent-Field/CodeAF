package tui3

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The picture tools' expansion draws the picture (imagepreview.go). These are
// its acceptance tests: the arithmetic of the grid, the two colours one cell
// carries, the terminals that get nothing, and the line under it all.

// tinyPicture is two by two, one flat colour per pixel:
//
//	red    green
//	blue   white
func tinyPicture() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{G: 255, A: 255})
	img.SetNRGBA(0, 1, color.NRGBA{B: 255, A: 255})
	img.SetNRGBA(1, 1, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	return img
}

// writePicture puts one picture on disk and hands back its absolute path.
func writePicture(t *testing.T, dir, name string, img image.Image) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// A cell is the top pixel in front of the bottom one, and nothing else — the
// whole technique, stated as one row of one tiny picture.
func TestAHalfCellCarriesTheTopPixelInFrontOfTheBottomOne(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	rows := pictureCells(pal, tinyPicture(), 2, 1)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	want := "\x1b[38;2;255;0;0;48;2;0;0;255m" + halfBlock +
		"\x1b[38;2;0;255;0;48;2;255;255;255m" + halfBlock +
		"\x1b[39;49m"
	if rows[0] != want {
		t.Fatalf("row = %q\nwant %q", rows[0], want)
	}
}

// The same picture through the xterm cube: still two colours a cell, resolved
// to the nearest index rather than refused.
func TestAHalfCellDegradesToTheXtermCube(t *testing.T) {
	pal := newPalette(tokens.ANSI256, false)
	rows := pictureCells(pal, tinyPicture(), 2, 1)
	want := "\x1b[38;5;" + itoa(int(nearest256(255, 0, 0))) +
		";48;5;" + itoa(int(nearest256(0, 0, 255))) + "m" + halfBlock +
		"\x1b[38;5;" + itoa(int(nearest256(0, 255, 0))) +
		";48;5;" + itoa(int(nearest256(255, 255, 255))) + "m" + halfBlock +
		"\x1b[39;49m"
	if rows[0] != want {
		t.Fatalf("row = %q\nwant %q", rows[0], want)
	}
}

// A run of one colour is one escape sequence, not one per cell.
func TestAFlatRunIsPaintedOnce(t *testing.T) {
	flat := image.NewNRGBA(image.Rect(0, 0, 8, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 8; x++ {
			flat.SetNRGBA(x, y, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	row := pictureCells(newPalette(tokens.TrueColor, false), flat, 8, 1)[0]
	if opens := strings.Count(row, "\x1b[38;2;"); opens != 1 {
		t.Fatalf("colour written %d times, want once: %q", opens, row)
	}
	if cells := strings.Count(row, halfBlock); cells != 8 {
		t.Fatalf("cells = %d, want 8", cells)
	}
}

// A downscale AVERAGES rather than picks: half a black-and-white picture folded
// into one cell is the grey between them, which is what keeps a screenshot's
// thin strokes from disappearing.
func TestADownscaleAveragesTheBoxItCovers(t *testing.T) {
	checker := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for x := 0; x < 4; x++ {
		shade := uint8(0)
		if x%2 == 1 {
			shade = 255
		}
		checker.SetNRGBA(x, 0, color.NRGBA{R: shade, G: shade, B: shade, A: 255})
		checker.SetNRGBA(x, 1, color.NRGBA{R: shade, G: shade, B: shade, A: 255})
	}
	row := pictureCells(newPalette(tokens.TrueColor, false), checker, 2, 1)[0]
	if !strings.HasPrefix(row, "\x1b[38;2;127;127;127;48;2;127;127;127m") {
		t.Fatalf("row did not open on the average of black and white: %q", row)
	}
}

// Transparency is composited over the ground the ramp implies, so an icon
// exported on nothing is drawn against the terminal it is on rather than
// against its own fringe.
func TestTransparencyIsCompositedOverTheTerminalsGround(t *testing.T) {
	clear := image.NewNRGBA(image.Rect(0, 0, 1, 2))
	dark := pictureCells(newPalette(tokens.TrueColor, false), clear, 1, 1)[0]
	if !strings.HasPrefix(dark, "\x1b[38;2;0;0;0;48;2;0;0;0m") {
		t.Fatalf("a transparent picture on a dark ramp = %q, want black", dark)
	}
	light := newPalette(tokens.TrueColor, false)
	light.ramp = lightRamp
	page := pictureCells(light, clear, 1, 1)[0]
	if !strings.HasPrefix(page, "\x1b[38;2;255;255;255;48;2;255;255;255m") {
		t.Fatalf("a transparent picture on a light ramp = %q, want white", page)
	}
}

// The grid keeps the picture's shape, never enlarges it, and lets the row
// budget bind when the picture is tall.
func TestTheGridKeepsTheShapeAndNeverEnlarges(t *testing.T) {
	for _, c := range []struct {
		name                   string
		w, h, maxCols, maxRows int
		wantCols, wantRows     int
	}{
		// A square at 40 columns: half as many rows as columns, because one cell
		// is two stacked pixels.
		{name: "square", w: 1024, h: 1024, maxCols: 40, maxRows: 20, wantCols: 40, wantRows: 20},
		// Sixteen by nine, wide: the width binds and the height follows.
		{name: "wide", w: 1600, h: 900, maxCols: 40, maxRows: 20, wantCols: 40, wantRows: 11},
		// Portrait: twenty rows is the ceiling, so the width comes down to keep
		// the shape rather than the picture being squashed.
		{name: "portrait", w: 600, h: 1800, maxCols: 60, maxRows: 20, wantCols: 13, wantRows: 20},
		// A small icon is drawn at its own size and not blown up.
		{name: "icon", w: 16, h: 16, maxCols: 60, maxRows: 20, wantCols: 16, wantRows: 8},
		// A picture much wider than it is tall still gets its one row.
		{name: "banner", w: 1000, h: 10, maxCols: 40, maxRows: 20, wantCols: 40, wantRows: 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			cols, rows := pictureGrid(c.w, c.h, c.maxCols, c.maxRows)
			if cols != c.wantCols || rows != c.wantRows {
				t.Fatalf("grid = %d×%d, want %d×%d", cols, rows, c.wantCols, c.wantRows)
			}
		})
	}
}

// The three terminals that get no picture at all, and the width that is too
// narrow to be one. Every one of them keeps the words the expansion always had.
func TestThePictureIsAbsentWhereItCannotBeDrawn(t *testing.T) {
	dir := t.TempDir()
	path := writePicture(t, dir, "harbour.png", tinyPicture())
	size := int(fileSizeOf(t, path))

	for _, c := range []struct {
		name string
		pal  palette
		cols int
	}{
		{name: "sixteen colours", pal: newPalette(tokens.ANSI16, false), cols: 40},
		{name: "no colour", pal: newPalette(tokens.NoColor, false), cols: 40},
		{name: "no box drawing", pal: newPalette(tokens.TrueColor, true), cols: 40},
		{name: "read aloud", pal: linearPalette(), cols: 40},
		{name: "too narrow", pal: newPalette(tokens.TrueColor, false), cols: 4},
	} {
		t.Run(c.name, func(t *testing.T) {
			if preview := renderPicture(c.pal, path, size, c.cols, pictureRowsMax); preview.ok {
				t.Fatalf("a picture was drawn: %q", preview.rows)
			}
		})
	}
}

// linearPalette is the screen-reader tier with colour otherwise available.
func linearPalette() palette {
	pal := newPalette(tokens.TrueColor, false)
	pal.linear = true
	return pal
}

func fileSizeOf(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

// A file that is not a picture, and a file that is not there, are both simply
// not drawn — never an error where a row used to be.
func TestAnUnreadableFileDrawsNothingRatherThanAnError(t *testing.T) {
	dir := t.TempDir()
	lying := filepath.Join(dir, "not-really.png")
	if err := os.WriteFile(lying, []byte("this is not a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	pal := newPalette(tokens.TrueColor, false)
	if preview := renderPicture(pal, lying, 17, 40, pictureRowsMax); preview.ok {
		t.Fatal("a text file was drawn as a picture")
	}
	if preview := renderPicture(pal, filepath.Join(dir, "gone.png"), 100, 40, pictureRowsMax); preview.ok {
		t.Fatal("a missing file was drawn as a picture")
	}
}

// The line under the picture: the path whole, then its shape and its size —
// and the path never truncated, however narrow the frame.
func TestThePathUnderThePictureIsNeverCut(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	preview := imagePreview{width: 1024, height: 768, bytes: 1536, ok: true}
	path := "/tmp/lab/.aforge-v3/images/harbour.png"

	one := picturePathLine(pal, path, preview, 70)
	if len(one) != 1 {
		t.Fatalf("wide frame drew %d lines, want one: %q", len(one), one)
	}
	if got := plain(one[0]); got != path+" · 1024×768 · 1.5 KB" {
		t.Fatalf("line = %q", got)
	}

	two := picturePathLine(pal, path, preview, len(path)+2)
	if len(two) != 2 || plain(two[0]) != path {
		t.Fatalf("a frame with room for the path alone = %q", two)
	}
	if plain(two[1]) != "1024×768 · 1.5 KB" {
		t.Fatalf("the facts moved to their own line as %q", plain(two[1]))
	}

	narrow := picturePathLine(pal, path, preview, 20)
	joined := ""
	for _, line := range narrow[:len(narrow)-1] {
		joined += plain(line)
	}
	if joined != path {
		t.Fatalf("a narrow frame lost part of the path: %q", joined)
	}
	if strings.Contains(joined, glyphMore) {
		t.Fatalf("the path was truncated: %q", joined)
	}
}

// And it is a hyperlink, the way every other location on this surface is.
func TestThePathUnderThePictureIsAHyperlink(t *testing.T) {
	pal := newPalette(tokens.TrueColor, false)
	path := "/tmp/lab/harbour.png"
	line := picturePathLine(pal, path, imagePreview{width: 8, height: 8, bytes: 90, ok: true}, 70)[0]
	if !strings.Contains(line, "\x1b]8;;file://"+path) {
		t.Fatalf("no file link in %q", line)
	}
}

// ── the two calls, end to end ───────────────────────────────────────────────

// pictureApp is a surface whose workspace is a real directory with a real
// picture in it.
func pictureApp(t *testing.T, batches ...[]session.Event) (*app, string) {
	t.Helper()
	dir := t.TempDir()
	path := writePicture(t, dir, ".aforge-v3/images/harbour.png", wideTestPicture())
	var events []session.Event
	for _, batch := range batches {
		events = append(events, batch...)
	}
	events = append(events, session.Event{Kind: session.EventTurnDone})
	agent := &fakeAgent{model: "m", turns: [][]session.Event{events}}
	a := newTestApp(agent)
	a.pal = newPalette(tokens.TrueColor, false)
	a.workspace = dir
	runTurn(t, a, agent, "draw me a harbour")
	return a, path
}

// paintedRows counts the rows of one block that carry a picture. The half block
// is the whole technique, so a row with one in it is a row of picture and a
// block with none in it drew nothing.
func paintedRows(rows []string) int {
	n := 0
	for _, r := range rows {
		if strings.Contains(r, halfBlock) {
			n++
		}
	}
	return n
}

// stemless joins the expansion's rows with the rail taken off, which is how a
// path that had to wrap is read back as one string.
func stemless(rows []string) string {
	var joined strings.Builder
	for _, r := range rows {
		r = strings.TrimLeft(r, " ")
		r = strings.TrimPrefix(strings.TrimPrefix(r, railCont), railContASCII)
		joined.WriteString(strings.TrimRight(r, " "))
	}
	return joined.String()
}

// wideTestPicture is big enough that the grid has to downscale it.
func wideTestPicture() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 4), G: uint8(y * 8), B: 90, A: 255})
		}
	}
	return img
}

// Opening a generated picture shows the picture and the file, and nothing else:
// the result's one line said where the file was and how big it was, which is
// exactly what the line under the picture says.
func TestOpeningAGeneratedPictureDrawsThePicture(t *testing.T) {
	a, path := pictureApp(t, call("generate_image",
		`{"prompt":"a harbour at dawn"}`,
		".aforge-v3/images/harbour.png — 64×32 png, 1.2KB, generated on paint/model"))

	rows := openFirst(t, a)
	if !strings.Contains(strings.Join(rows, "\n"), halfBlock) {
		t.Fatalf("no picture in the expansion:\n%s", strings.Join(rows, "\n"))
	}
	// The path is read back off the stem, because a temporary directory's name
	// is longer than the test frame and the line wraps rather than being cut.
	if !strings.Contains(stemless(rows), path) {
		t.Fatalf("the file was not named:\n%s", strings.Join(rows, "\n"))
	}
	if strings.Contains(strings.Join(rows, "\n"), "generated on paint/model") {
		t.Fatalf("the result was printed under the picture as well:\n%s",
			strings.Join(rows, "\n"))
	}
}

// A look shows the picture AND what the looking model said about it — the two
// halves of the call.
func TestOpeningALookDrawsThePictureAndTheAnswer(t *testing.T) {
	a, _ := pictureApp(t, call("view_image",
		`{"path":".aforge-v3/images/harbour.png","question":"is the mast straight?"}`,
		"seen by look/model: the mast leans a little to the left."))

	body := strings.Join(openFirst(t, a), "\n")
	if !strings.Contains(body, halfBlock) {
		t.Fatalf("no picture in the expansion:\n%s", body)
	}
	if !strings.Contains(body, "the mast leans a little to the left.") {
		t.Fatalf("the answer was dropped:\n%s", body)
	}
	if strings.Contains(body, `"question"`) {
		t.Fatalf("the raw arguments were drawn under the picture:\n%s", body)
	}
}

// A call whose file is gone keeps the words it always had rather than a hole.
func TestAPictureCallWithNoFileKeepsItsWords(t *testing.T) {
	a, _ := pictureApp(t, call("view_image",
		`{"path":"nowhere/missing.png"}`,
		"missing.png: no such file"))

	body := strings.Join(openFirst(t, a), "\n")
	if strings.Contains(body, halfBlock) {
		t.Fatalf("a picture was drawn for a file that is not there:\n%s", body)
	}
	if !strings.Contains(body, "no such file") {
		t.Fatalf("the result was lost:\n%s", body)
	}
}

// The file is read out of the arguments when they name it, and out of the
// result when `generate_image` chose the name itself.
func TestThePictureFileIsFoundInEitherPlace(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.workspace = "/tmp/lab"

	named := &entry{tool: "view_image", detail: toolDetail{Args: `{"path":"art/one.png"}`}}
	if got, ok := a.picturePath(named); !ok || got != "/tmp/lab/art/one.png" {
		t.Fatalf("from the arguments = %q, %v", got, ok)
	}

	chosen := &entry{tool: "generate_image", detail: toolDetail{
		Args:   `{"prompt":"a harbour"}`,
		Output: ".aforge-v3/images/harbour.png — 1024×1024 png, 1.4MB, generated on paint/model",
	}}
	want := "/tmp/lab/.aforge-v3/images/harbour.png"
	if got, ok := a.picturePath(chosen); !ok || got != want {
		t.Fatalf("from the result = %q, %v", got, ok)
	}

	absolute := &entry{tool: "view_image", detail: toolDetail{Args: `{"path":"/elsewhere/x.jpg"}`}}
	if got, ok := a.picturePath(absolute); !ok || got != "/elsewhere/x.jpg" {
		t.Fatalf("an absolute path = %q, %v", got, ok)
	}

	// A file this surface cannot decode is not a picture path at all, so nothing
	// is opened and the expansion keeps its words.
	pdf := &entry{tool: "view_image", detail: toolDetail{Args: `{"path":"paper.pdf"}`}}
	if got, ok := a.picturePath(pdf); ok {
		t.Fatalf("a pdf resolved to %q", got)
	}

	// And no other tool is asked about pictures.
	read := &entry{tool: "read", detail: toolDetail{Args: `{"path":"art/one.png"}`}}
	if _, ok := a.picturePath(read); ok {
		t.Fatal("read was treated as a picture call")
	}
}

// A picture is decoded once, however many frames it is drawn on.
func TestAPictureIsDecodedOncePerShape(t *testing.T) {
	a, _ := pictureApp(t, call("generate_image", `{"path":".aforge-v3/images/harbour.png"}`,
		".aforge-v3/images/harbour.png — 64×32 png, 1.2KB, generated on paint/model"))
	openFirst(t, a)
	kept := len(a.previews)
	if kept == 0 {
		t.Fatal("nothing was cached")
	}
	for i := 0; i < 5; i++ {
		a.touch()
		rows(a)
	}
	if len(a.previews) != kept {
		t.Fatalf("cache grew from %d to %d across repaints", kept, len(a.previews))
	}
}

package tui3

// WHAT A PREVIEW LOOKS LIKE, WITHOUT A DISK.
//
// Every test here builds a [filePreview] by hand and draws it. That is the whole
// value of the split: the shape of the pane, the gutter, the clipping, the
// scroll floor and ceiling and the picture's aspect are all decided by pure
// functions, so they can be pinned exactly rather than described.

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// drawPalette is a terminal that can say everything: the rung where colour,
// highlighting and pictures are all on, so a test that wants to see paint can.
func drawPalette() palette { return newPalette(tokens.TrueColor, false) }

// drawStyler is a painter built from a stated environment rather than the
// process's, for [newMarkdownStyler]'s reason: the profile is detected once per
// process and a test that wants a truecolor terminal has to say so.
func drawStyler() *tokens.Styler {
	return newMarkdownStyler(func(name string) string {
		if name == "TERM" {
			return "xterm-256color"
		}
		if name == "COLORTERM" {
			return "truecolor"
		}
		return ""
	})
}

// textPreview is a source file that was read, with no disk behind it.
func textPreview(lang string, lines ...string) filePreview {
	return filePreview{
		Key:   previewKey{Path: "/tmp/x", Bytes: 120},
		Kind:  previewSource,
		Lang:  lang,
		Lines: lines,
		Total: len(lines),
	}
}

// widest is the widest drawn row, measured the way the terminal will draw it.
func widest(rows []string) int {
	most := 0
	for _, row := range rows {
		if w := ansi.StringWidth(row); w > most {
			most = w
		}
	}
	return most
}

func TestAPreviewNeverLeavesTheRegionItWasGiven(t *testing.T) {
	pal, st := drawPalette(), drawStyler()
	previews := map[string]filePreview{
		"source": textPreview("Go", "package main", "", "func main() { println(\"hello, world\") }"),
		"prose":  {Key: previewKey{Path: "/tmp/a", Bytes: 40}, Kind: previewProse, Lines: []string{strings.Repeat("word ", 40)}, Total: 1},
		"wide":   {Key: previewKey{Path: "/tmp/b", Bytes: 40}, Kind: previewProse, Lines: []string{"日本語のテキストがここにあります", "❤️🇯🇵 emoji"}, Total: 2},
		"folder": {Key: previewKey{Path: "/tmp/c"}, Kind: previewFolder, Shown: 2, Entries: []previewEntry{
			{Name: "src", Dir: true}, {Name: strings.Repeat("long-name-", 8) + ".txt", Bytes: 4096},
		}},
		"picture": picturePreview(120, 60),
		"binary":  {Key: previewKey{Path: "/tmp/d", Bytes: 900}, Kind: previewOpaque, Note: previewOpaqueWord},
		"refused": {Key: previewKey{Path: "/tmp/e"}, Kind: previewRefused, Note: previewDeniedWord},
	}
	for _, width := range []int{8, 20, 40, 96} {
		for _, height := range []int{1, 2, 6, 30} {
			box := previewBox{Width: width, Height: height, Numbers: true}
			for name, pv := range previews {
				rows := previewRows(pal, st, pv, box)
				if len(rows) > height {
					t.Errorf("%s at %d×%d: %d rows", name, width, height, len(rows))
				}
				if got := widest(rows); got > width {
					t.Errorf("%s at %d×%d: a row is %d cells wide", name, width, height, got)
				}
			}
		}
	}
}

// THE ESCAPE LAW, ON THE DRAWING SIDE. A preview handed a line with a screen
// clear in it — which the reader cannot produce, and a hand-built caller can —
// still draws no screen clear.
func TestNoControlSequenceFromAFileReachesTheFrame(t *testing.T) {
	pal, st := drawPalette(), drawStyler()
	pv := textPreview("Go", "before\x1b[2Jafter", "second\x07line")
	pv.Entries = nil
	rows := previewRows(pal, st, pv, previewBox{Width: 40, Height: 6})
	joined := strings.Join(rows, "\n")
	if strings.Contains(joined, "\x1b[2J") || strings.Contains(joined, "\x07") {
		t.Fatalf("a file's own control sequence was drawn: %q", joined)
	}
	if !strings.Contains(ansi.Strip(joined), "beforeafter") {
		t.Fatalf("the text around it was lost: %q", ansi.Strip(joined))
	}

	named := filePreview{Key: previewKey{Path: "/tmp/f"}, Kind: previewFolder, Shown: 1,
		Entries: []previewEntry{{Name: "ok\x1b[2Jgone"}}}
	folder := strings.Join(previewRows(pal, st, named, previewBox{Width: 40, Height: 4}), "\n")
	if strings.Contains(folder, "\x1b[2J") {
		t.Fatalf("a filename's own control sequence was drawn: %q", folder)
	}
}

// ── syntax ──────────────────────────────────────────────────────────────────

func TestSourceIsPaintedAndProseIsNot(t *testing.T) {
	pal, st := drawPalette(), drawStyler()
	box := previewBox{Width: 60, Height: 4}

	lit := previewRows(pal, st, textPreview("Go", `func main() { const n = 42 }`), box)
	// A highlighted row wears MORE THAN ONE ink: the keyword, the number and the
	// quiet tier around them. A flat row wears exactly one.
	if inks := strings.Count(lit[0], "\x1b["); inks < 3 {
		t.Fatalf("source was drawn with %d inks, want a highlighted row: %q", inks, lit[0])
	}
	flat := textPreview("", `func main() { const n = 42 }`)
	flat.Kind = previewProse
	plain := previewRows(pal, st, flat, box)
	if inks := strings.Count(plain[0], "\x1b["); inks > 2 {
		t.Fatalf("plain text was highlighted: %q", plain[0])
	}
	if ansi.Strip(lit[0]) != ansi.Strip(plain[0]) {
		t.Fatalf("paint changed the characters: %q vs %q", ansi.Strip(lit[0]), ansi.Strip(plain[0]))
	}
}

// THE SCREEN-READER TIER GETS NO SYNTAX COLOUR, for [codeLang]'s reason: a hue
// is a claim a person listening never receives, and lexing to say nothing is a
// pass spent on an audience that cannot hear it.
func TestASurfaceBeingReadAloudIsNotHighlighted(t *testing.T) {
	pal := drawPalette()
	pal.linear = true
	rows := previewRows(pal, drawStyler(), textPreview("Go", `func main() { const n = 42 }`),
		previewBox{Width: 60, Height: 3})
	if inks := strings.Count(rows[0], "\x1b["); inks > 2 {
		t.Fatalf("the linear tier was handed syntax colour: %q", rows[0])
	}
}

// ── the gutter ──────────────────────────────────────────────────────────────

func TestLineNumbersCountTheFileAndNotThePane(t *testing.T) {
	pal, st := drawPalette(), drawStyler()
	lines := make([]string, 30)
	for at := range lines {
		lines[at] = "line " + itoa(at+1)
	}
	pv := textPreview("", lines...)
	pv.Kind = previewProse

	rows := previewRows(pal, st, pv, previewBox{Width: 40, Height: 5, Top: 9, Numbers: true})
	first := ansi.Strip(rows[0])
	if !strings.HasPrefix(first, "10 ") {
		t.Fatalf("first row = %q, want it numbered 10", first)
	}
	if !strings.Contains(first, "line 10") {
		t.Fatalf("the number does not match the line: %q", first)
	}
	// The gutter is the same width on every row of one draw, so the text does
	// not shift sideways as a number gains a digit mid-pane.
	for _, row := range rows[:4] {
		if at := strings.Index(ansi.Strip(row), "line "); at != 3 {
			t.Fatalf("text starts at %d on %q", at, ansi.Strip(row))
		}
	}
}

func TestWithoutTheGutterTheWholeWidthIsText(t *testing.T) {
	pal, st := drawPalette(), drawStyler()
	pv := textPreview("", strings.Repeat("x", 100))
	pv.Kind = previewProse
	rows := previewRows(pal, st, pv, previewBox{Width: 30, Height: 3})
	if got := ansi.StringWidth(rows[0]); got != 30 {
		t.Fatalf("row is %d cells, want the whole 30", got)
	}
}

// ── scrolling ───────────────────────────────────────────────────────────────

func TestScrollingStopsWithTheLastLineStillOnScreen(t *testing.T) {
	lines := make([]string, 20)
	for at := range lines {
		lines[at] = itoa(at)
	}
	pv := textPreview("", lines...)
	pv.Kind = previewProse

	if got := previewClampTop(pv, previewBox{Height: 5, Top: 900}); got != 15 {
		t.Fatalf("top = %d, want 15", got)
	}
	if got := previewClampTop(pv, previewBox{Height: 5, Top: -4}); got != 0 {
		t.Fatalf("top = %d, want 0", got)
	}
	if got := previewClampTop(pv, previewBox{Height: 40, Top: 3}); got != 0 {
		t.Fatalf("a pane taller than the file scrolled to %d", got)
	}
}

// THE PANE'S ARITHMETIC AND ITS DRAWING MUST AGREE, or the last line of a file
// is unreachable by exactly the height of the foot.
func TestTheBodyBoxIsWhatAScrollIsClampedAgainst(t *testing.T) {
	pal := drawPalette()
	pv := textPreview("Go", "a", "b", "c", "d", "e")
	pv.Note = previewCutWord
	box := previewBox{Width: 40, Height: 4}
	body := previewBodyBox(pal, pv, box)
	if body.Height != 3 {
		t.Fatalf("body height = %d, want 3 with a foot drawn", body.Height)
	}
	if got := previewClampTop(pv, body); got != 0 {
		t.Fatalf("top = %d", got)
	}
	body.Top = 99
	if got := previewClampTop(pv, body); got != 2 {
		t.Fatalf("top = %d, want 2 so the last line is still shown", got)
	}
}

// A LONG LINE IS ANSWERED BY SLIDING SIDEWAYS, NOT BY WRAPPING — and the slide
// lands between characters, never inside one.
func TestTheHorizontalSlideLandsBetweenCharacters(t *testing.T) {
	pal, st := drawPalette(), drawStyler()
	pv := textPreview("", "日本語のテキスト")
	pv.Kind = previewProse
	rows := previewRows(pal, st, pv, previewBox{Width: 20, Height: 2, Left: 4})
	plain := ansi.Strip(rows[0])
	// Four cells is exactly two full-width characters, so the row opens on the
	// third — never in the middle of the second.
	if !strings.HasPrefix(plain, "語のテキスト") {
		t.Fatalf("slid to %q, want it to start two characters in", plain)
	}
	if strings.ContainsRune(plain, '�') {
		t.Fatalf("the slide cut a character in half: %q", plain)
	}
}

// ── the foot ────────────────────────────────────────────────────────────────

func TestTheFootSaysWhatIsMissingOnTheLeftAndWhatItIsOnTheRight(t *testing.T) {
	pal := drawPalette()
	pv := textPreview("Go", "package main")
	pv.Total, pv.Cut, pv.Note = 4000, true, previewCutWord
	pv.Key.Bytes = 90000

	foot := ansi.Strip(previewFoot(pal, pv, 70))
	if !strings.HasPrefix(foot, previewCutWord) {
		t.Fatalf("foot = %q, want it to open with the limitation", foot)
	}
	for _, want := range []string{"Go", "4000 lines", "87.9 KB"} {
		if !strings.Contains(foot, want) {
			t.Fatalf("foot = %q, want %q in it", foot, want)
		}
	}
	if !strings.HasSuffix(foot, "87.9 KB") {
		t.Fatalf("foot = %q, want the facts against the right edge", foot)
	}
}

// THE LIMITATION IS THE PART A PERSON CANNOT WORK OUT FOR THEMSELVES, so it is
// the part a narrow pane keeps.
func TestANarrowFootGivesUpTheFactsAndKeepsTheLimitation(t *testing.T) {
	pal := drawPalette()
	pv := textPreview("Go", "package main")
	pv.Note = previewCutWord
	foot := ansi.Strip(previewFoot(pal, pv, 30))
	if !strings.HasPrefix(foot, "more of this file") {
		t.Fatalf("foot = %q", foot)
	}
}

// NOTHING UNKNOWN IS DRAWN [design-law §EMPTINESS]: a file whose lines nobody
// counted says nothing about lines rather than saying nought.
func TestAFileNobodyCountedStatesNoLineCount(t *testing.T) {
	pal := drawPalette()
	pv := textPreview("Go", "package main")
	pv.Total, pv.Key.Bytes = 0, 0
	if got := ansi.Strip(previewFoot(pal, pv, 60)); got != "Go" {
		t.Fatalf("foot = %q, want the language alone", got)
	}
}

// A SENTENCE IS SAID ONCE. When the body is nothing but the limitation, the
// foot does not repeat it.
func TestTheOneSentenceIsNotSaidOnTwoRows(t *testing.T) {
	pal, st := drawPalette(), drawStyler()
	pv := filePreview{Key: previewKey{Path: "/tmp/a.out", Bytes: 900}, Kind: previewOpaque, Note: previewOpaqueWord}
	rows := previewRows(pal, st, pv, previewBox{Width: 60, Height: 8})
	said := 0
	for _, row := range rows {
		if strings.Contains(ansi.Strip(row), previewOpaqueWord) {
			said++
		}
	}
	if said != 1 {
		t.Fatalf("the sentence was drawn %d times", said)
	}
	if last := ansi.Strip(rows[len(rows)-1]); !strings.Contains(last, "900 B") {
		t.Fatalf("the foot lost the facts: %q", last)
	}
}

// THE FOOT SITS AT THE FOOT, so a pane whose file is two lines long does not
// move its own status line up under them.
func TestTheFootDoesNotFloatUpToMeetAShortFile(t *testing.T) {
	pal, st := drawPalette(), drawStyler()
	pv := textPreview("Go", "package main")
	pv.Note = previewCutWord
	rows := previewRows(pal, st, pv, previewBox{Width: 50, Height: 9})
	if len(rows) != 9 {
		t.Fatalf("rows = %d, want the region held open", len(rows))
	}
	if !strings.Contains(ansi.Strip(rows[8]), previewCutWord) {
		t.Fatalf("last row = %q", ansi.Strip(rows[8]))
	}
	if strings.TrimSpace(ansi.Strip(rows[4])) != "" {
		t.Fatalf("row 4 = %q, want it empty", ansi.Strip(rows[4]))
	}
}

// ── folders ─────────────────────────────────────────────────────────────────

func TestAFolderRowNamesTheThingAndAlignsItsSize(t *testing.T) {
	pal, st := drawPalette(), drawStyler()
	pv := filePreview{Key: previewKey{Path: "/tmp/p"}, Kind: previewFolder, Shown: 3, Entries: []previewEntry{
		{Name: "src", Dir: true},
		{Name: "main.go", Bytes: 2048},
		{Name: "tiny", Bytes: 7},
	}}
	rows := previewRows(pal, st, pv, previewBox{Width: 30, Height: 6})
	dir, file := ansi.Strip(rows[0]), ansi.Strip(rows[1])
	if dir != "src/" {
		t.Fatalf("a folder row = %q, want a trailing slash and no size", dir)
	}
	if !strings.HasPrefix(file, "main.go") || !strings.HasSuffix(file, "2.0 KB") {
		t.Fatalf("a file row = %q", file)
	}
	if ansi.StringWidth(file) != 30 {
		t.Fatalf("the size is not against the right edge: %q", file)
	}
	if got := ansi.Strip(rows[len(rows)-1]); !strings.Contains(got, "1 folder") || !strings.Contains(got, "2 files") {
		t.Fatalf("foot = %q", got)
	}
}

func TestAFolderTooFullToDrawSaysHowManyAreNotShown(t *testing.T) {
	pal := drawPalette()
	pv := filePreview{Key: previewKey{Path: "/tmp/p"}, Kind: previewFolder, Shown: 900,
		Entries: []previewEntry{{Name: "a"}, {Name: "b"}}}
	if got := ansi.Strip(previewFoot(pal, pv, 60)); !strings.Contains(got, "898 more not shown") {
		t.Fatalf("foot = %q", got)
	}
}

// ── pictures ────────────────────────────────────────────────────────────────

// picturePreview is a decoded picture of a stated shape, with no file behind it.
func picturePreview(w, h int) filePreview {
	thumb := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			thumb.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x30, A: 0xff})
		}
	}
	return filePreview{
		Key: previewKey{Path: "/tmp/photo.png", Bytes: 267600}, Kind: previewPicture,
		Wide: w * 8, High: h * 8, Format: "png", Thumb: thumb,
	}
}

// drawnPicture is the painted rows with the foot and the blank filler taken off.
func drawnPicture(rows []string) []string {
	picture := rows
	for len(picture) > 0 && !strings.Contains(picture[len(picture)-1], halfBlock) {
		picture = picture[:len(picture)-1]
	}
	return picture
}

func TestAPictureKeepsItsShapeAndIsDrawnInCells(t *testing.T) {
	pal, st := drawPalette(), drawStyler()
	// Twice as wide as it is tall, into a region with room to spare.
	wide := drawnPicture(previewRows(pal, st, picturePreview(120, 60), previewBox{Width: 60, Height: 30}))
	// A half-cell is nearly square, so a 2:1 picture across sixty columns wants
	// about fifteen rows.
	if len(wide) < 12 || len(wide) > 18 {
		t.Fatalf("a 2:1 picture drew %d rows into 60 columns", len(wide))
	}
	if !strings.Contains(wide[0], halfBlock) {
		t.Fatalf("the picture was not drawn out of half blocks: %q", wide[0])
	}
	if got := widest(wide); got > 60 {
		t.Fatalf("a picture row is %d cells wide", got)
	}

	// A PORTRAIT IS BOUND BY THE HEIGHT AND CENTRED IN WHAT IS LEFT. Pinned to
	// the left edge of an empty region it reads as a layout mistake rather than
	// as a photograph.
	portrait, box := picturePreview(60, 120), previewBox{Width: 60, Height: 30}
	tall := drawnPicture(previewRows(pal, st, portrait, box))
	// It fills the body, which is the region left once the facts have their row.
	if body := previewBodyBox(pal, portrait, box).Height; len(tall) != body {
		t.Fatalf("a 1:2 picture drew %d rows into %d", len(tall), body)
	}
	if !strings.HasPrefix(tall[0], " ") {
		t.Fatalf("the picture was not centred: %q", tall[0])
	}
	if got := widest(tall); got > 60 {
		t.Fatalf("a centred picture row is %d cells wide", got)
	}
}

// A TERMINAL THAT CANNOT PAINT GETS THE FACTS AND AN EXPLICIT SENTENCE, never a
// pane of blocks in sixteen colours.
func TestATerminalWithNoPicturesGetsWordsInstead(t *testing.T) {
	pal, st := newPalette(tokens.ANSI16, false), drawStyler()
	pv := picturePreview(40, 40)
	pv.Thumb, pv.Note = nil, previewNoPaintWord
	rows := previewRows(pal, st, pv, previewBox{Width: 50, Height: 10})
	first := ansi.Strip(rows[0])
	if first != previewNoPaintWord {
		t.Fatalf("first row = %q, want %q", first, previewNoPaintWord)
	}
	joined := strings.Join(rows, "\n")
	if strings.Contains(joined, halfBlock) {
		t.Fatal("a picture was painted on a terminal that cannot draw one")
	}
	if last := ansi.Strip(rows[len(rows)-1]); !strings.Contains(last, "320×320") || !strings.Contains(last, "png") {
		t.Fatalf("the facts were lost: %q", last)
	}
}

// EVEN WITH PIXELS IN HAND, A PALETTE THAT REFUSES PICTURES REFUSES THEM.
func TestAPaletteThatDrawsNoPicturesIsObeyedEvenWithPixelsInHand(t *testing.T) {
	pal, st := drawPalette(), drawStyler()
	pal.linear = true
	pv := picturePreview(40, 40)
	pv.Note = previewNoPaintWord
	rows := previewRows(pal, st, pv, previewBox{Width: 40, Height: 8})
	if strings.Contains(strings.Join(rows, "\n"), halfBlock) {
		t.Fatal("the linear tier was handed a picture")
	}
}

// ── padding ─────────────────────────────────────────────────────────────────

func TestPaddingFillsTheRegionAndNeverOverflowsIt(t *testing.T) {
	box := previewBox{Width: 10, Height: 4}
	if got := previewPad([]string{"a"}, box); len(got) != 4 {
		t.Fatalf("padded to %d rows, want 4", len(got))
	}
	if got := previewPad([]string{"a", "b", "c", "d", "e", "f"}, box); len(got) != 4 {
		t.Fatalf("trimmed to %d rows, want 4", len(got))
	}
}

func TestNothingSelectedDrawsNothing(t *testing.T) {
	if rows := previewRows(drawPalette(), drawStyler(), filePreview{}, previewBox{Width: 40, Height: 10}); rows != nil {
		t.Fatalf("an empty preview drew %d rows", len(rows))
	}
}

package tui3

// WHAT THE READER IS ALLOWED TO DO TO SOMEBODY ELSE'S FILE.
//
// Every test here is about a file a person did not write and the browser did not
// choose: a photograph, a core dump, a minified bundle, a file that went away
// between the stat and the open, a file with a screen-clearing escape sequence
// in it. The reader's whole job is to survive those and say something true.

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFixture is one file on disk, with its directories made, and the absolute
// path back.
func writeFixture(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if parent := filepath.Dir(path); parent != dir {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", parent, err)
		}
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// readOne is [loadPreview] with a live context, which is what every test that is
// not about cancellation wants.
func readOne(t *testing.T, path string, pictures bool) filePreview {
	t.Helper()
	return loadPreview(context.Background(), previewRequest{Path: path, Pictures: pictures})
}

func TestSourceIsReadAsItsOwnLanguage(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "main.go", "package main\n\nfunc main() {}\n")

	pv := readOne(t, path, false)
	if pv.Kind != previewSource {
		t.Fatalf("kind = %v, want previewSource", pv.Kind)
	}
	if pv.Lang != "Go" {
		t.Fatalf("lang = %q, want Go", pv.Lang)
	}
	if len(pv.Lines) != 3 || pv.Lines[0] != "package main" {
		t.Fatalf("lines = %q", pv.Lines)
	}
	if pv.Total != 3 || pv.Cut {
		t.Fatalf("total = %d cut = %v, want 3 false", pv.Total, pv.Cut)
	}
	if pv.Key.Path != path || pv.Key.Bytes == 0 {
		t.Fatalf("key = %+v", pv.Key)
	}
}

func TestTextThatNothingClaimsIsStillRead(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "NOTES", "just some words\nand a second line\n")

	pv := readOne(t, path, false)
	if pv.Kind != previewProse {
		t.Fatalf("kind = %v, want previewProse", pv.Kind)
	}
	if pv.Lang != "" {
		t.Fatalf("lang = %q, want none", pv.Lang)
	}
	if len(pv.Lines) != 2 {
		t.Fatalf("lines = %q", pv.Lines)
	}
}

// A SCRIPT WITH NO EXTENSION IS THE FILE PEOPLE PREVIEW WONDERING WHAT IT IS.
func TestAScriptIsNamedByItsShebangWhenItsNameSaysNothing(t *testing.T) {
	dir := t.TempDir()
	for _, probe := range []struct {
		name string
		head string
		want string
	}{
		{"deploy", "#!/bin/bash\necho hi\n", "Bash"},
		{"tidy", "#!/usr/bin/env python3\nprint(1)\n", "Python"},
		{"plain", "# not a shebang\n", ""},
	} {
		path := writeFixture(t, dir, probe.name, probe.head)
		pv := readOne(t, path, false)
		if pv.Lang != probe.want {
			t.Errorf("%s: lang = %q, want %q", probe.name, pv.Lang, probe.want)
		}
	}
}

// THE ESCAPE SEQUENCE LAW. A file is somebody else's bytes; a preview pane is
// inside a frame this surface composes. `\x1b[2J` in a source file must reach
// the pane as characters or not at all — never as a command to the terminal.
func TestAFileCannotRepaintTheTerminalItIsPreviewedIn(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "nasty.txt",
		"before\x1b[2J\x1b[1;31mred\x1b[0m after\n\tindented\rcarriage\n")

	pv := readOne(t, path, false)
	joined := strings.Join(pv.Lines, "\n")
	if strings.ContainsRune(joined, 0x1b) {
		t.Fatalf("an escape survived the read: %q", joined)
	}
	if strings.ContainsRune(joined, '\r') {
		t.Fatalf("a carriage return survived the read: %q", joined)
	}
	if strings.Contains(joined, "\t") {
		t.Fatalf("a tab survived the read: %q", joined)
	}
	// The visible text is kept — this is a preview, not a redaction.
	if !strings.Contains(joined, "before") || !strings.Contains(joined, "red") {
		t.Fatalf("the words were lost with the escapes: %q", joined)
	}
	// A tab becomes four cells, which is what the fitter will measure.
	if !strings.Contains(joined, "    indented") {
		t.Fatalf("the tab did not become four cells: %q", joined)
	}
}

func TestAFileThatIsNotTextIsNamedRatherThanDumped(t *testing.T) {
	dir := t.TempDir()
	body := append([]byte("\x7fELF\x02\x01\x01\x00"), bytes.Repeat([]byte{0x00, 0xfe, 0x91}, 400)...)
	path := filepath.Join(dir, "a.out")
	if err := os.WriteFile(path, body, 0o755); err != nil {
		t.Fatal(err)
	}

	pv := readOne(t, path, false)
	if pv.Kind != previewOpaque {
		t.Fatalf("kind = %v, want previewOpaque", pv.Kind)
	}
	if pv.Note != previewOpaqueWord {
		t.Fatalf("note = %q, want %q", pv.Note, previewOpaqueWord)
	}
	if len(pv.Lines) != 0 {
		t.Fatalf("a binary handed back %d lines", len(pv.Lines))
	}
}

// A LATIN-1 README IS STILL A README. The sniff is a proportion, not a flag,
// and a handful of bad bytes in a page of ASCII must not cost a person the
// preview.
func TestAFewBadBytesDoNotMakeAFileBinary(t *testing.T) {
	dir := t.TempDir()
	body := strings.Repeat("the quick brown fox jumps over the lazy dog\n", 20) + "caf\xe9\n"
	path := writeFixture(t, dir, "README", body)

	pv := readOne(t, path, false)
	if pv.Kind != previewProse {
		t.Fatalf("kind = %v, want previewProse", pv.Kind)
	}
}

func TestAFileLongerThanTheLineBoundSaysSoAndStops(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "long.txt", strings.Repeat("line\n", previewLinesMax+50))

	pv := readOne(t, path, false)
	if len(pv.Lines) != previewLinesMax {
		t.Fatalf("kept %d lines, want %d", len(pv.Lines), previewLinesMax)
	}
	if !pv.Cut {
		t.Fatal("a file with more lines than were kept did not say so")
	}
	if pv.Total != previewLinesMax+50 {
		t.Fatalf("total = %d, want %d", pv.Total, previewLinesMax+50)
	}
	if pv.Note != previewCutWord {
		t.Fatalf("note = %q, want %q", pv.Note, previewCutWord)
	}
}

// A FILE PAST THE BYTE BOUND HAS NO LINE COUNT, AND SAYS NOTHING RATHER THAN
// GUESSING ONE [design-law §EMPTINESS].
func TestAFileTooLargeToReadWholeNamesNoLineCount(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "huge.txt", strings.Repeat("x", previewBytesMax+4096)+"\n")

	pv := readOne(t, path, false)
	if pv.Total != 0 {
		t.Fatalf("total = %d, want nothing known", pv.Total)
	}
	if !pv.Cut || pv.Note != previewBigWord {
		t.Fatalf("cut = %v note = %q", pv.Cut, pv.Note)
	}
	for _, line := range pv.Lines {
		if len(line) > previewLineBytes {
			t.Fatalf("a line of %d bytes survived the bound", len(line))
		}
	}
}

// A MINIFIED BUNDLE IS ONE LINE OF TWO HUNDRED KILOBYTES, AND THE BOUND ON A
// LINE IS WHAT KEEPS THE PANE CHEAP.
func TestOneEnormousLineIsCutAtTheLineBound(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "bundle.js", strings.Repeat("a", previewLineBytes*3)+"\n")

	pv := readOne(t, path, false)
	if len(pv.Lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(pv.Lines))
	}
	if len(pv.Lines[0]) > previewLineBytes {
		t.Fatalf("the line is %d bytes, want at most %d", len(pv.Lines[0]), previewLineBytes)
	}
}

func TestAnEmptyFileSaysItIsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "nothing.txt", "")

	pv := readOne(t, path, false)
	if pv.Note != previewEmptyWord {
		t.Fatalf("note = %q, want %q", pv.Note, previewEmptyWord)
	}
	if len(pv.Lines) != 0 {
		t.Fatalf("lines = %q", pv.Lines)
	}
}

func TestEachWayAFileCanRefuseHasItsOwnSentence(t *testing.T) {
	dir := t.TempDir()
	gone := filepath.Join(dir, "not-here.txt")
	if pv := readOne(t, gone, false); pv.Kind != previewRefused || pv.Note != previewMissingWord {
		t.Fatalf("missing file: kind = %v note = %q", pv.Kind, pv.Note)
	}
	if os.Geteuid() == 0 {
		t.Skip("root can read anything; the permission rung cannot be shown here")
	}
	shut := writeFixture(t, dir, "shut.txt", "secret\n")
	if err := os.Chmod(shut, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(shut, 0o644) })
	if pv := readOne(t, shut, false); pv.Note != previewDeniedWord {
		t.Fatalf("denied file: kind = %v note = %q", pv.Kind, pv.Note)
	}
}

// MINIFIED JSON IS THE ONE FILE THIS READER RESHAPES, AND AN ALREADY-INDENTED
// ONE IS LEFT EXACTLY AS ITS AUTHOR WROTE IT.
func TestMinifiedJSONIsGivenAShapeAndIndentedJSONIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	flat := writeFixture(t, dir, "flat.json", `{"a":1,"b":{"c":[1,2,3]}}`)
	if pv := readOne(t, flat, false); len(pv.Lines) < 5 {
		t.Fatalf("minified json stayed flat: %q", pv.Lines)
	}
	kept := "{\n    \"a\": 1\n}\n"
	shaped := writeFixture(t, dir, "shaped.json", kept)
	pv := readOne(t, shaped, false)
	if strings.Join(pv.Lines, "\n") != "{\n    \"a\": 1\n}" {
		t.Fatalf("an indented file was re-indented: %q", pv.Lines)
	}
	if pv.Lang != previewJSONLang {
		t.Fatalf("lang = %q, want %q", pv.Lang, previewJSONLang)
	}
}

// ── folders ─────────────────────────────────────────────────────────────────

func TestAFolderListsWhatIsInsideItWithFoldersFirst(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "zebra.txt", "z\n")
	writeFixture(t, dir, "apple.txt", "aa\n")
	writeFixture(t, dir, "src/x.go", "package x\n")
	writeFixture(t, dir, ".hidden/x", "x\n")

	pv := readOne(t, dir, false)
	if pv.Kind != previewFolder {
		t.Fatalf("kind = %v, want previewFolder", pv.Kind)
	}
	var names []string
	for _, entry := range pv.Entries {
		names = append(names, entry.Name)
	}
	want := []string{"src", "apple.txt", "zebra.txt"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("entries = %v, want %v", names, want)
	}
	if !pv.Entries[0].Dir || pv.Entries[0].Bytes != 0 {
		t.Fatalf("a folder row carried a size: %+v", pv.Entries[0])
	}
	if pv.Entries[1].Bytes != 3 {
		t.Fatalf("apple.txt = %d bytes, want 3", pv.Entries[1].Bytes)
	}
}

func TestHiddenEntriesAppearOnlyWhenTheyWereAskedFor(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, ".env", "SECRET=1\n")
	writeFixture(t, dir, "seen.txt", "x\n")

	shown := loadPreview(context.Background(), previewRequest{Path: dir, Hidden: true})
	if len(shown.Entries) != 2 {
		t.Fatalf("hidden asked for: %d entries", len(shown.Entries))
	}
	if len(readOne(t, dir, false).Entries) != 1 {
		t.Fatal("a dot entry was shown to somebody who did not ask")
	}
}

func TestAFolderTooFullToListSaysHowManyAreNotShown(t *testing.T) {
	dir := t.TempDir()
	for at := 0; at < previewEntriesMax+7; at++ {
		writeFixture(t, dir, "f"+itoa(1000+at), "x")
	}
	pv := readOne(t, dir, false)
	if len(pv.Entries) != previewEntriesMax {
		t.Fatalf("kept %d entries, want %d", len(pv.Entries), previewEntriesMax)
	}
	if pv.Shown != previewEntriesMax+7 {
		t.Fatalf("shown = %d, want %d", pv.Shown, previewEntriesMax+7)
	}
}

func TestAFolderThatCannotBeReadIsNotAFolderThatIsEmpty(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read anything")
	}
	dir := t.TempDir()
	shut := filepath.Join(dir, "shut")
	if err := os.Mkdir(shut, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(shut, 0o755) })

	pv := readOne(t, shut, false)
	if pv.Kind != previewFolder || pv.Note != previewFolderWord {
		t.Fatalf("kind = %v note = %q", pv.Kind, pv.Note)
	}
}

// ── pictures ────────────────────────────────────────────────────────────────

// writePNG is a picture on disk with a known shape and a known corner colour.
func writePNG(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: 0x40, A: 0xff})
		}
	}
	path := filepath.Join(dir, name)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAPictureIsDecodedOnceAndReducedToTheThumbnailBound(t *testing.T) {
	dir := t.TempDir()
	path := writePNG(t, dir, "wide.png", 800, 200)

	pv := readOne(t, path, true)
	if pv.Kind != previewPicture {
		t.Fatalf("kind = %v, want previewPicture", pv.Kind)
	}
	if pv.Wide != 800 || pv.High != 200 {
		t.Fatalf("shape = %d×%d, want 800×200", pv.Wide, pv.High)
	}
	if pv.Format != "png" {
		t.Fatalf("format = %q, want png", pv.Format)
	}
	if pv.Thumb == nil {
		t.Fatal("no thumbnail was kept")
	}
	bounds := pv.Thumb.Bounds()
	if bounds.Dx() > previewThumbSide || bounds.Dy() > previewThumbSide {
		t.Fatalf("thumbnail is %v, past the %d bound", bounds, previewThumbSide)
	}
	// The reduction keeps the picture's own shape: 4:1 in, 4:1 out.
	if got := float64(bounds.Dx()) / float64(bounds.Dy()); got < 3.5 || got > 4.5 {
		t.Fatalf("thumbnail aspect = %.2f, want about 4", got)
	}
}

// A SMALL PICTURE IS NEVER ENLARGED ON THE WAY INTO THE CACHE.
func TestASmallPictureIsKeptAtItsOwnSize(t *testing.T) {
	dir := t.TempDir()
	pv := readOne(t, writePNG(t, dir, "icon.png", 16, 16), true)
	if pv.Thumb == nil || pv.Thumb.Bounds().Dx() != 16 {
		t.Fatalf("thumbnail = %v, want 16×16", pv.Thumb.Bounds())
	}
}

// THE DECODE IS THE EXPENSIVE PART AND A TERMINAL THAT CANNOT PAINT NEVER PAYS
// FOR IT — while the facts it CAN state still arrive.
func TestATerminalThatCannotPaintStillLearnsTheShapeOfThePicture(t *testing.T) {
	dir := t.TempDir()
	pv := readOne(t, writePNG(t, dir, "photo.png", 64, 32), false)
	if pv.Thumb != nil {
		t.Fatal("a picture was decoded for a terminal that cannot draw it")
	}
	if pv.Wide != 64 || pv.High != 32 || pv.Format != "png" {
		t.Fatalf("facts lost: %d×%d %q", pv.Wide, pv.High, pv.Format)
	}
	if pv.Note != previewNoPaintWord {
		t.Fatalf("note = %q, want %q", pv.Note, previewNoPaintWord)
	}
}

func TestAPictureThatWillNotDecodeSaysSoRatherThanDrawingItsBytes(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "broken.png", "this is not a png at all\n")
	pv := readOne(t, path, true)
	if pv.Kind != previewPicture || pv.Note != previewNoDecodeWord {
		t.Fatalf("kind = %v note = %q", pv.Kind, pv.Note)
	}
	if len(pv.Lines) != 0 {
		t.Fatalf("a broken picture handed back text: %q", pv.Lines)
	}
}

// ── documents ───────────────────────────────────────────────────────────────

func TestADocumentTooLargeToReadHereSaysSoBeforeItIsParsed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.pdf")
	if err := os.WriteFile(path, bytes.Repeat([]byte("%PDF-1.7\n"), previewDocBytesMax/9+64), 0o644); err != nil {
		t.Fatal(err)
	}
	pv := readOne(t, path, false)
	if pv.Kind != previewOpaque || pv.Note != previewDocBigWord {
		t.Fatalf("kind = %v note = %q", pv.Kind, pv.Note)
	}
}

func TestADocumentThatWillNotParseSaysSoAndDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "notreally.pdf", "%PDF-1.7\nnot a real document\n")
	pv := readOne(t, path, false)
	if pv.Kind != previewOpaque {
		t.Fatalf("kind = %v, want previewOpaque", pv.Kind)
	}
	if pv.Note != previewDocFailWord && pv.Note != previewScanWord {
		t.Fatalf("note = %q", pv.Note)
	}
	if len(pv.Lines) != 0 {
		t.Fatalf("a broken document handed back text: %q", pv.Lines)
	}
}

// ── cancellation, staleness and the pump ────────────────────────────────────

func TestACancelledReadAnswersNothingAtAll(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "main.go", "package main\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if pv := loadPreview(ctx, previewRequest{Path: path}); !pv.empty() {
		t.Fatalf("a cancelled read answered: %+v", pv)
	}
}

func TestARelativePathIsNeverGuessedAt(t *testing.T) {
	pv := loadPreview(context.Background(), previewRequest{Path: "main.go"})
	if pv.Kind != previewRefused {
		t.Fatalf("kind = %v, want previewRefused", pv.Kind)
	}
}

func TestAnAnswerFromAMomentThatHasPassedIsDropped(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "one.go", "package one\n")
	var pump previewPump

	_, cmd := pump.show(previewRequest{Path: path})
	if cmd == nil {
		t.Fatal("a first look answered off the cache")
	}
	first := cmd().(previewLoadedMsg)

	// The person moves on before the answer lands.
	pump.show(previewRequest{Path: writeFixture(t, dir, "two.go", "package two\n")})
	if _, took := pump.took(first); took {
		t.Fatal("an answer from the previous generation was drawn")
	}
}

func TestAnAnswerAboutAFileThatHasChangedUnderneathIsDropped(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "moving.txt", "before\n")
	var pump previewPump
	_, cmd := pump.show(previewRequest{Path: path})
	msg := cmd().(previewLoadedMsg)

	// A build rewrites the file while the read is in flight. The pump is asked
	// again and now holds a different identity for the same path.
	writeFixture(t, dir, "moving.txt", "after, and rather longer than before\n")
	pump.gen = msg.gen // the generation matches; only the file has moved.
	pump.show(previewRequest{Path: path})
	pump.gen = msg.gen
	if _, took := pump.took(msg); took {
		t.Fatal("a preview of the old bytes was drawn over the new file")
	}
}

func TestTheSecondLookAtAFileCostsNoRead(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "held.go", "package held\n")
	var pump previewPump

	_, cmd := pump.show(previewRequest{Path: path})
	msg := cmd().(previewLoadedMsg)
	if _, took := pump.took(msg); !took {
		t.Fatal("the answer to the current question was refused")
	}
	held, again := pump.show(previewRequest{Path: path})
	if again != nil {
		t.Fatal("a held preview was read from the disk a second time")
	}
	if held.Key != msg.preview.Key {
		t.Fatalf("held = %+v, want %+v", held.Key, msg.preview.Key)
	}
	if _, ok := pump.held(); !ok {
		t.Fatal("a redraw could not reach the preview the pane is showing")
	}
}

// THE FLAGS ARE PART OF THE QUESTION. A terminal that has just been measured as
// able to paint must not be handed the answer given to one that could not.
func TestAnAnswerGivenUnderOtherFlagsIsNotDrawn(t *testing.T) {
	dir := t.TempDir()
	path := writePNG(t, dir, "p.png", 8, 8)
	var pump previewPump
	_, cmd := pump.show(previewRequest{Path: path, Pictures: false})
	msg := cmd().(previewLoadedMsg)

	pump.gen--
	pump.show(previewRequest{Path: path, Pictures: true})
	pump.gen = msg.gen
	if _, took := pump.took(msg); took {
		t.Fatal("a preview read without pictures was drawn on a terminal that has them")
	}
}

func TestClosingTheSheetStopsTheReadAndForgetsWhatWasBeingShown(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "x.go", "package x\n")
	var pump previewPump
	pump.show(previewRequest{Path: path})
	pump.close()
	if _, ok := pump.held(); ok {
		t.Fatal("a closed pane still says what it is showing")
	}
	if pump.cancel != nil {
		t.Fatal("the read in flight was not stopped")
	}
}

func TestNoMoreThanTheStatedNumberOfPreviewsAreHeld(t *testing.T) {
	var cache previewCache
	for at := 0; at < previewKeep+8; at++ {
		cache.put(filePreview{
			Key:  previewKey{Path: "/tmp/f" + itoa(at), Bytes: 1, Mod: 2},
			Kind: previewProse,
		}, "")
	}
	if len(cache.by) != previewKeep {
		t.Fatalf("held %d previews, want %d", len(cache.by), previewKeep)
	}
	if _, held := cache.get(previewKey{Path: "/tmp/f0", Bytes: 1, Mod: 2}, ""); held {
		t.Fatal("the oldest preview was not given up")
	}
}

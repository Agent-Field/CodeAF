package tui3

// WHAT A FILE IS, READ OFF THE LOOP AND BOUNDED IN EVERY DIRECTION.
//
// The context browser shows a person what they are about to attach BEFORE they
// attach it, which means it opens strangers' files on a keystroke: a directory
// walked with the arrow keys hands this code a minified bundle, a four gigabyte
// core dump, a photograph, a scanned invoice and a symlink into a mount that has
// gone away, in whatever order the person happens to move.
//
// So this file is the reading half and it holds four laws.
//
// NOTHING IS EXECUTED AND NOTHING IS FETCHED. A preview opens a file for
// reading and stats a directory. It does not run a script because it is
// executable, does not follow a URL because a file mentions one, and does not
// shell out to a converter. Looking at a thing must never be the same gesture as
// running it — that is the whole difference between a browser and a launcher.
//
// SOMEBODY ELSE'S BYTES REACH NO FRAME UNPARSED. Every line kept here has been
// through [drawableLine]: escape sequences stripped, control bytes dropped, tabs
// expanded. A preview pane is a rectangle inside a framebuffer this surface
// composes, and a file carrying `\x1b[2J` would otherwise clear the person's
// screen when the cursor moved onto it.
//
// EVERY BOUND IS STATED AS A CONSTANT AND NONE IS A HOPE. Bytes read, lines
// held, cells in a line, pixels decoded, entries listed, previews cached and
// readers running at once all have a number below, because the file on the other
// side of the cursor is chosen by somebody else.
//
// AND AN ANSWER ABOUT A MOMENT THAT HAS PASSED IS DROPPED. A read is stamped
// with the generation that asked for it and with the file's own size and
// modification time; [previewPump.took] refuses an answer that does not match
// both. Walking down a directory faster than a disk can answer is the ordinary
// case, not the exotic one.
//
// The drawing half is contextpreviewdraw.go, and the split is deliberate: what
// this file produces is DATA — no palette, no width, no escape sequences — so a
// preview can be read once and drawn at any size, and the renderer can be tested
// without a disk.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/pdfx"
	"github.com/Agent-Field/aforge-v2/internal/tui2/prose"
)

// ── the bounds ──────────────────────────────────────────────────────────────

const (
	// previewBytesMax is how much of a text file is ever read. A preview is a
	// look, not a load: at eighty columns and forty rows a person can see about
	// three kilobytes at once, and a quarter of a megabyte is two orders of
	// magnitude of headroom over that for scrolling. Past it the file is read no
	// further and the pane says so.
	previewBytesMax = 256 << 10
	// previewLinesMax is how many lines are KEPT. It bounds what a minified file
	// costs — a quarter megabyte of JSON with a newline every four bytes is sixty
	// thousand lines, and holding them all would put a megabyte of strings in the
	// cache for a pane that can show forty.
	previewLinesMax = 400
	// previewLineBytes is where one line stops. A bundle is one line of two
	// hundred kilobytes; the pane shows at most a couple of hundred cells of it,
	// and measuring the rest on every frame is the cost of nothing.
	previewLineBytes = 4 << 10
	// previewJSONBytes is the largest document reformatted before it is shown.
	// Indenting is a whole re-encode, so it is offered to the minified files it
	// rescues and not to a log somebody happens to have named .json.
	previewJSONBytes = 128 << 10
	// previewDocBytesMax is the largest PDF whose text layer is extracted. The
	// extractor parses the whole document — there is no head-of-file rung — so
	// this is the only bound available, and it is set where a specification or a
	// paper still fits and a scanned book does not.
	previewDocBytesMax = 8 << 20
	// previewEntriesMax is how many names a folder preview holds. It is a
	// glance at what is inside, and a directory with forty thousand files in it
	// is answered by the count rather than by the names.
	previewEntriesMax = 256
	// previewThumbSide bounds the DECODED picture that is kept. The half-cell
	// renderer averages every source pixel inside each half-cell, so drawing a
	// six megapixel photograph from its original pixels would walk six million
	// samples on every repaint; reduced once to this square it is sixty-five
	// thousand, which is a millisecond. Two hundred and fifty-six is also more
	// than twice the widest preview pane a terminal has, so nothing is enlarged
	// on the way to the screen.
	previewThumbSide = 256
	// previewWorkers is how many files are read at once. Two lets a picture
	// decode while a directory is listed without a fast walk down a tree putting
	// a reader on every level of it.
	previewWorkers = 2
	// previewKeep is how many previews are held. Walking down and back up is the
	// gesture it exists for. It is small because a held preview can carry a
	// quarter megabyte of thumbnail, and because a cache of what is on a disk
	// right now is wrong as soon as it is large.
	previewKeep = 16
)

// ── what a file turned out to be ────────────────────────────────────────────

// previewKind is what the reader decided it is looking at. It decides which
// renderer draws the pane and nothing else.
type previewKind uint8

const (
	// previewNothing is no selection at all — the zero value, and the state a
	// pane is in before anybody has moved.
	previewNothing previewKind = iota
	// previewFolder is a directory: its own contents, counted and named.
	previewFolder
	// previewSource is text chroma claims a lexer for, drawn highlighted.
	previewSource
	// previewProse is text nothing claims, drawn plainly and legibly.
	previewProse
	// previewPicture is an image this program decoded.
	previewPicture
	// previewDocument is a PDF whose text layer was read.
	previewDocument
	// previewOpaque is a file that is not text — an executable, an archive, a
	// video. It gets its facts and an honest sentence, never its bytes.
	previewOpaque
	// previewRefused is a file that could not be read at all, and why.
	previewRefused
)

// previewKey is a file's identity for the purposes of a preview: which file,
// how big it was, and when it last changed.
//
// IT IS WHAT MAKES A LATE ANSWER SAFE. A path alone cannot tell a preview of
// this file from a preview of the file that replaced it while a reader was
// running, and a person watching a build rewrite a file under the cursor is the
// ordinary case. The three together are the same identity the picture cache
// already keys on (imagepreview.go).
type previewKey struct {
	Path  string
	Bytes int64
	Mod   int64
}

// previewEntry is one row of a folder preview.
type previewEntry struct {
	// Name is DRAWABLE TEXT AND NOT A PATH. A filename is somebody else's bytes
	// exactly as a file's contents are — `touch $'ok\e[2Jgone'` is a legal name
	// on every filesystem this program runs on — so it goes through
	// [drawableLine] on the way in and cannot be joined back onto a directory to
	// reach the file. Navigation is the browser's own listing's business.
	Name string
	Dir  bool
	// Bytes is the file's size. It is left at zero for a directory, whose size
	// on disk is not the number anybody means by it, and the renderer draws
	// nothing there rather than a `0 B` [design-law §EMPTINESS].
	Bytes int64
}

// filePreview is one file, read and decided — and NOT drawn. There is no
// palette in it, no width, and no escape sequence: it is what
// contextpreviewdraw.go turns into rows, at whatever size the pane turns out to
// be, without touching a disk.
type filePreview struct {
	// Key is what was read, and it is the only identity anything downstream
	// compares. Key.Path is absolute and cleaned.
	Key  previewKey
	Kind previewKind
	// Lang is chroma's own name for the language, or "" when nothing claimed
	// the file. It is what [prose.HighlightLine] takes.
	Lang string
	// Lines are the text, drawable, one per source line, in file order. They are
	// bounded by [previewLinesMax] and each by [previewLineBytes].
	Lines []string
	// Total is how many lines the whole file has, or 0 when the file was cut at
	// [previewBytesMax] and nobody counted the rest. Zero means UNKNOWN, and the
	// footer says so rather than naming a number it does not have.
	Total int
	// Cut says there is more of this file than Lines holds.
	Cut bool
	// Entries is a folder's contents, directories first, then files, each half
	// sorted by name. Shown is how many of them there are in total.
	Entries []previewEntry
	Shown   int
	// Wide and High are the picture's own pixel dimensions — the file's, not the
	// thumbnail's, because they are what the facts line states.
	Wide, High int
	// Format is the decoder's own name for the picture: `png`, `jpeg`, `gif`,
	// `webp`.
	Format string
	// Thumb is the decoded picture reduced to [previewThumbSide], premultiplied,
	// with its alpha intact — the ground it is composited over is the palette's,
	// and the palette is not known here.
	Thumb *image.RGBA
	// Note is the one sentence a person is owed about a limit or a refusal, or
	// "" when there is nothing to say. It is never a diagnostic.
	Note string
}

// empty answers whether there is nothing here to draw at all.
func (p filePreview) empty() bool { return p.Kind == previewNothing }

// ── the sentences ───────────────────────────────────────────────────────────

// The words a preview says instead of contents. Every one of them states a fact
// somebody just established, which is why saying them is not a breach of the
// emptiness law — and every one of them is a whole sentence in a person's own
// vocabulary, with no machinery in it [design-law §NO MACHINERY VOCABULARY].
const (
	previewDeniedWord   = "this file cannot be read · permission denied"
	previewMissingWord  = "this file is no longer here"
	previewUnreadWord   = "this file cannot be read"
	previewEmptyWord    = "this file is empty"
	previewOpaqueWord   = "this is not text · nothing to show here"
	previewCutWord      = "more of this file is not shown"
	previewBigWord      = "only the first part of this file was read"
	previewNoPaintWord  = "this terminal cannot draw pictures"
	previewNoDecodeWord = "this picture could not be opened"
	previewScanWord     = "this document is pages of pictures · there is no text in it"
	previewDocFailWord  = "this document could not be read"
	previewDocBigWord   = "this document is too large to read here"
	previewFolderWord   = "this folder cannot be read · permission denied"
)

// previewWord is the sentence a failed read gets, chosen the way
// [folderReadWord] chooses its own — one law for what a refusing filesystem is
// called, on both halves of this sheet.
func previewWord(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, fs.ErrPermission):
		return previewDeniedWord
	case errors.Is(err, fs.ErrNotExist):
		return previewMissingWord
	case errors.Is(err, syscall.EISDIR):
		return previewUnreadWord
	}
	return previewUnreadWord
}

// ── the request ─────────────────────────────────────────────────────────────

// previewRequest is one file, asked for.
//
// Pictures is in it because DECODING IS THE EXPENSIVE PART AND THE PALETTE
// DECIDES WHETHER IT IS WANTED. A terminal below the xterm cube, an ascii
// terminal and the screen-reader tier all draw no picture (imagepreview.go's
// [palette.paintsPictures]), and spending a decode to throw the pixels away is
// the one waste this file can avoid before it starts. The caller passes
// `a.pal.paintsPictures()`; the facts line is drawn either way.
type previewRequest struct {
	Path string
	Gen  uint64
	// Pictures says this terminal can paint half-cell pictures.
	Pictures bool
	// Hidden says a folder listing should include its dot entries, following
	// whatever the browser's own hidden-folder key is set to.
	Hidden bool
}

// flags is the part of a request that changes the ANSWER rather than the
// moment, and so belongs in the cache key beside the file's identity.
func (r previewRequest) flags() string {
	out := ""
	if r.Pictures {
		out += "p"
	}
	if r.Hidden {
		out += "h"
	}
	return out
}

// ── reading, off the loop ───────────────────────────────────────────────────

// previewLoadedMsg is one preview coming back, stamped with the generation that
// asked for it.
type previewLoadedMsg struct {
	preview filePreview
	gen     uint64
	flags   string
}

// previewCmd reads one file OFF THE LOOP, under the caller's context.
//
// The context is the cancellation: a person moving off a row cancels the read
// they no longer want, and every step below checks it — before the stat, before
// the open, and between chunks. What it CANNOT interrupt is a single blocking
// syscall on a hung mount, because Go has no interruptible ReadDir or Read; the
// answer to that one arrives late and is then dropped by
// [previewPump.took] rather than never arriving. That is the honest shape of
// the bound and the report says so.
func previewCmd(ctx context.Context, req previewRequest) tea.Cmd {
	return func() tea.Msg {
		return previewLoadedMsg{
			preview: loadPreview(ctx, req),
			gen:     req.Gen,
			flags:   req.flags(),
		}
	}
}

// previewGate is how many reads run at once, across every pane in the process.
// A buffered channel is the whole pool: there is no queue to inspect and no
// worker to leak, and a reader that cannot get in gives up on cancellation
// rather than waiting for a slot it no longer needs.
var previewGate = make(chan struct{}, previewWorkers)

// loadPreview is the read itself: blocking, bounded, cancellable, and free of
// any surface at all. It never returns an error — a file that could not be read
// is a preview that says why.
func loadPreview(ctx context.Context, req previewRequest) filePreview {
	path := strings.TrimSpace(req.Path)
	if path == "" {
		return filePreview{}
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		// A relative path has no meaning here: this reader has no working
		// directory of its own and guessing one would preview the wrong file.
		return filePreview{Key: previewKey{Path: path}, Kind: previewRefused, Note: previewUnreadWord}
	}
	if err := ctx.Err(); err != nil {
		return filePreview{}
	}
	select {
	case previewGate <- struct{}{}:
		defer func() { <-previewGate }()
	case <-ctx.Done():
		return filePreview{}
	}
	if err := ctx.Err(); err != nil {
		return filePreview{}
	}

	info, err := os.Stat(path)
	if err != nil {
		return filePreview{
			Key:  previewKey{Path: path},
			Kind: previewRefused,
			Note: previewWord(err),
		}
	}
	key := previewKey{Path: path, Bytes: info.Size(), Mod: info.ModTime().UnixNano()}
	if info.IsDir() {
		return loadFolderPreview(ctx, key, req.Hidden)
	}
	if !info.Mode().IsRegular() {
		// A socket, a device, a fifo. Opening one can block forever and reading
		// one can consume it, so it is named and left alone.
		return filePreview{Key: key, Kind: previewOpaque, Note: previewOpaqueWord}
	}
	if err := ctx.Err(); err != nil {
		return filePreview{}
	}

	switch {
	case pictureSuffixes[strings.ToLower(filepath.Ext(path))]:
		return loadPicturePreview(key, req.Pictures)
	case strings.EqualFold(filepath.Ext(path), ".pdf"):
		return loadDocumentPreview(ctx, key)
	}
	return loadTextPreview(ctx, key)
}

// ── a folder ────────────────────────────────────────────────────────────────

// loadFolderPreview is what a directory shows: its own contents, directories
// first, bounded and counted.
//
// It is ONE readdir and nothing deep, which is the same law the browser's
// columns keep (folderindex.go). A preview must never be the thing that walks a
// tree.
func loadFolderPreview(ctx context.Context, key previewKey, hidden bool) filePreview {
	entries, err := os.ReadDir(key.Path)
	if err != nil {
		return filePreview{Key: key, Kind: previewFolder, Note: previewFolderWord}
	}
	if err := ctx.Err(); err != nil {
		return filePreview{}
	}
	rows := make([]previewEntry, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !hidden && strings.HasPrefix(name, ".") {
			continue
		}
		rows = append(rows, previewEntry{Name: name, Dir: entry.IsDir()})
	}
	// Directories first, then files, each half by name. It is the order Finder,
	// Yazi and `ls --group-directories-first` all use, and it is the one that
	// answers "what can I go into from here" before "what is in here".
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Dir != rows[j].Dir {
			return rows[i].Dir
		}
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
	shown := len(rows)
	if len(rows) > previewEntriesMax {
		rows = rows[:previewEntriesMax]
	}
	// The sizes are asked for only on the rows that are kept. os.ReadDir hands
	// back a lazy Info, so this is one lstat per drawn row rather than one per
	// name in a directory that may hold forty thousand.
	byName := make(map[string]fs.DirEntry, len(entries))
	for _, entry := range entries {
		byName[entry.Name()] = entry
	}
	for at := range rows {
		if !rows[at].Dir {
			if entry, held := byName[rows[at].Name]; held {
				if stat, err := entry.Info(); err == nil {
					rows[at].Bytes = stat.Size()
				}
			}
		}
		// LAST, so the sort and the stat above both used the real name: what is
		// kept is what may be drawn, and it is no longer a path.
		rows[at].Name = drawableLine(rows[at].Name)
	}
	return filePreview{Key: key, Kind: previewFolder, Entries: rows, Shown: shown}
}

// ── a picture ───────────────────────────────────────────────────────────────

// loadPicturePreview decodes one picture and reduces it once.
//
// The header is read before the pixels, exactly as [renderPicture] does it, so
// a picture too large to hold in memory is refused before four bytes a pixel are
// asked for. The dimensions survive a refusal to paint, because on a terminal
// that draws no pictures `1920×1080 · jpeg · 267.6 KB` is the whole of what can
// honestly be said and it is worth saying.
func loadPicturePreview(key previewKey, paints bool) filePreview {
	out := filePreview{Key: key, Kind: previewPicture}
	if key.Bytes <= 0 {
		out.Note = previewEmptyWord
		return out
	}
	file, err := os.Open(key.Path)
	if err != nil {
		out.Note = previewWord(err)
		return out
	}
	defer file.Close()

	config, format, err := image.DecodeConfig(file)
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		out.Note = previewNoDecodeWord
		return out
	}
	out.Wide, out.High, out.Format = config.Width, config.Height, format
	switch {
	case !paints:
		// The terminal cannot paint, so the pixels are never asked for. This is
		// the branch the `Pictures` flag exists to reach.
		out.Note = previewNoPaintWord
		return out
	case key.Bytes > pictureBytesMax || config.Width*config.Height > picturePixelsMax:
		out.Note = previewNoDecodeWord
		return out
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		out.Note = previewNoDecodeWord
		return out
	}
	source, _, err := image.Decode(file)
	if err != nil {
		out.Note = previewNoDecodeWord
		return out
	}
	out.Thumb = previewThumbnail(source, previewThumbSide)
	if out.Thumb == nil {
		out.Note = previewNoDecodeWord
	}
	return out
}

// previewThumbnail reduces a decoded picture to fit inside a side×side square,
// preserving its shape and NEVER enlarging it.
//
// The filter is a box average over PREMULTIPLIED components, which is the same
// arithmetic [sampleBox] does and for the same two reasons: an average of
// unpremultiplied colour weights a transparent pixel as heavily as an opaque
// one, and a box average is the cheapest filter that keeps a screenshot's thin
// strokes visible across a downscale.
//
// Alpha is KEPT rather than composited. What a transparent pixel sits on is the
// palette's business ([palette.backdrop]), the palette is a drawing-time fact,
// and compositing here would bake one theme's ground into a cached thumbnail.
func previewThumbnail(source image.Image, side int) *image.RGBA {
	if source == nil || side < 1 {
		return nil
	}
	bounds := source.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w < 1 || h < 1 {
		return nil
	}
	cols, rows := w, h
	if cols > side || rows > side {
		if w >= h {
			cols, rows = side, (h*side+w/2)/w
		} else {
			cols, rows = (w*side+h/2)/h, side
		}
	}
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	out := image.NewRGBA(image.Rect(0, 0, cols, rows))
	for cy := 0; cy < rows; cy++ {
		for cx := 0; cx < cols; cx++ {
			x0 := bounds.Min.X + cx*w/cols
			x1 := bounds.Min.X + (cx+1)*w/cols
			y0 := bounds.Min.Y + cy*h/rows
			y1 := bounds.Min.Y + (cy+1)*h/rows
			if x1 <= x0 {
				x1 = x0 + 1
			}
			if y1 <= y0 {
				y1 = y0 + 1
			}
			if x1 > bounds.Max.X {
				x0, x1 = bounds.Max.X-1, bounds.Max.X
			}
			if y1 > bounds.Max.Y {
				y0, y1 = bounds.Max.Y-1, bounds.Max.Y
			}
			var sumR, sumG, sumB, sumA, n uint64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					r, g, b, a := source.At(x, y).RGBA()
					sumR += uint64(r)
					sumG += uint64(g)
					sumB += uint64(b)
					sumA += uint64(a)
					n++
				}
			}
			if n == 0 {
				continue
			}
			out.SetRGBA(cx, cy, color.RGBA{
				R: uint8(sumR / n >> 8),
				G: uint8(sumG / n >> 8),
				B: uint8(sumB / n >> 8),
				A: uint8(sumA / n >> 8),
			})
		}
	}
	return out
}

// ── a document ──────────────────────────────────────────────────────────────

// loadDocumentPreview reads a PDF's TEXT LAYER, through the extractor this
// binary already carries (internal/pdfx) — no new dependency, nothing to
// install, and no page rendered as a picture.
//
// THE THREE OUTCOMES ARE KEPT APART because they need different sentences. A
// document with text shows it; a scanned one says it is pages of pictures, which
// is the true and useful answer rather than an empty pane; a file that would not
// parse says so. pdfx has no head-of-file rung — it parses the whole document —
// so the only bound available is the file's size, and it is applied before the
// parser is handed the path.
func loadDocumentPreview(ctx context.Context, key previewKey) filePreview {
	out := filePreview{Key: key, Kind: previewDocument}
	switch {
	case key.Bytes <= 0:
		out.Note = previewEmptyWord
		return out
	case key.Bytes > previewDocBytesMax:
		out.Kind = previewOpaque
		out.Note = previewDocBigWord
		return out
	}
	text, err := pdfx.Extract(key.Path)
	if err != nil {
		out.Kind = previewOpaque
		if errors.Is(err, pdfx.ErrNoTextLayer) {
			out.Note = previewScanWord
			return out
		}
		out.Note = previewDocFailWord
		return out
	}
	if err := ctx.Err(); err != nil {
		return filePreview{}
	}
	out.Lines, out.Total, out.Cut = previewLines([]byte(text), false)
	if len(out.Lines) == 0 {
		out.Kind = previewOpaque
		out.Note = previewScanWord
		return out
	}
	if out.Cut {
		out.Note = previewCutWord
	}
	return out
}

// ── text ────────────────────────────────────────────────────────────────────

// loadTextPreview reads the head of a file and decides whether it is text at
// all.
//
// THE SNIFF COMES FIRST AND IT IS NOT THE EXTENSION. A `.dat` full of Go and a
// `.go` full of compiled object code both exist, and the cost of getting this
// wrong is a pane of replacement characters or, worse, a pane of somebody's
// escape sequences. So the bytes decide whether there is text here, and the
// name only decides which lexer reads it.
func loadTextPreview(ctx context.Context, key previewKey) filePreview {
	out := filePreview{Key: key}
	if key.Bytes <= 0 {
		out.Kind = previewProse
		out.Note = previewEmptyWord
		return out
	}
	file, err := os.Open(key.Path)
	if err != nil {
		out.Kind = previewRefused
		out.Note = previewWord(err)
		return out
	}
	defer file.Close()

	// One byte past the bound, so a file that exactly fills it is still known to
	// have been read whole.
	head := make([]byte, previewBytesMax+1)
	read, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		out.Kind = previewRefused
		out.Note = previewWord(err)
		return out
	}
	head = head[:read]
	if err := ctx.Err(); err != nil {
		return filePreview{}
	}
	if read == 0 {
		out.Kind = previewProse
		out.Note = previewEmptyWord
		return out
	}
	if !previewIsText(head) {
		out.Kind = previewOpaque
		out.Note = previewOpaqueWord
		return out
	}
	byteCut := read > previewBytesMax
	if byteCut {
		head = head[:previewBytesMax]
	}

	lang := previewLang(key.Path, head)
	if lang == previewJSONLang && !byteCut && len(head) <= previewJSONBytes {
		if shaped, ok := previewShapeJSON(head); ok {
			head = shaped
		}
	}
	out.Lines, out.Total, out.Cut = previewLines(head, byteCut)
	out.Lang = lang
	out.Kind = previewProse
	if lang != "" {
		out.Kind = previewSource
	}
	switch {
	case len(out.Lines) == 0:
		out.Note = previewEmptyWord
	case byteCut:
		out.Note = previewBigWord
	case out.Cut:
		out.Note = previewCutWord
	}
	return out
}

// previewIsText answers whether a run of bytes is text a person can read.
//
// TWO SIGNALS AND NEITHER IS THE EXTENSION. A NUL byte is the classic one — no
// UTF-8 text contains one, and every executable, archive and video does within
// its first few hundred bytes. Invalid UTF-8 is the second, and it is a
// PROPORTION rather than a flag: a Latin-1 README is mostly ASCII with a few
// bad bytes and is perfectly readable, while a compiled binary is a third
// nonsense. The threshold sits far from both.
func previewIsText(head []byte) bool {
	sniff := head
	if len(sniff) > 8<<10 {
		sniff = sniff[:8<<10]
	}
	if bytes.IndexByte(sniff, 0) >= 0 {
		return false
	}
	bad, total := 0, 0
	for at := 0; at < len(sniff); {
		r, size := utf8.DecodeRune(sniff[at:])
		if r == utf8.RuneError && size <= 1 {
			bad++
		}
		at += size
		total++
	}
	if total == 0 {
		return true
	}
	return bad*100/total < 10
}

// previewLines splits a run of text into the lines a preview holds: drawable,
// bounded in count and in width, and honest about what was left out.
//
// Total is the file's whole line count when the file was read whole, and ZERO
// when it was cut at [previewBytesMax] — because nobody counted the rest, and
// [design-law §EMPTINESS] says a number nobody has renders as nothing rather
// than as a guess.
func previewLines(text []byte, byteCut bool) (lines []string, total int, cut bool) {
	body := strings.TrimRight(string(text), "\n")
	if body == "" {
		return nil, 0, false
	}
	raw := strings.Split(body, "\n")
	if byteCut && len(raw) > 1 {
		// The last line of a cut read stops mid-word — and, worse, mid-rune. It
		// is dropped rather than drawn as half of itself.
		raw = raw[:len(raw)-1]
	}
	if !byteCut {
		total = len(raw)
	}
	if len(raw) > previewLinesMax {
		raw, cut = raw[:previewLinesMax], true
	}
	if byteCut {
		cut = true
	}
	lines = make([]string, 0, len(raw))
	for _, line := range raw {
		if len(line) > previewLineBytes {
			line = line[:previewLineBytes]
			// The cut may have landed inside a rune; the trailing partial one is
			// dropped so nothing downstream sees a replacement character this
			// file invented.
			for len(line) > 0 && !utf8.ValidString(line) {
				line = line[:len(line)-1]
			}
		}
		// THE ONE PLACE SOMEBODY ELSE'S BYTES BECOME A ROW. Escapes stripped,
		// control bytes dropped, tabs expanded to four cells — the width the
		// fitter will measure is the width the terminal will draw.
		lines = append(lines, drawableLine(line))
	}
	return lines, total, cut
}

// previewJSONLang is chroma's own name for JSON, which is what
// [prose.LexerName] answers for a `.json` file and what the reshaper below
// keys on.
const previewJSONLang = "JSON"

// previewShapeJSON re-indents a document that is valid JSON.
//
// It exists for exactly one file: the minified one. An API response saved to
// disk is a single line of ninety kilobytes, and a preview of it is one row of
// braces and a horizontal scrollbar — the structure a person opened it to see
// is all there and none of it is visible. Indenting turns that into something
// with a shape, and chroma then colours it.
//
// A document that is ALREADY indented is left exactly as it is, because
// re-indenting somebody's file to this function's taste is an opinion nobody
// asked for. The test is whether it has fewer lines than it has kilobytes.
func previewShapeJSON(text []byte) ([]byte, bool) {
	if bytes.Count(text, []byte("\n")) > len(text)/1024 {
		return nil, false
	}
	var out bytes.Buffer
	if err := json.Indent(&out, text, "", "  "); err != nil {
		return nil, false
	}
	return out.Bytes(), true
}

// previewShebangs are the interpreters worth recognising when a file has no
// extension at all. The value is a filename chroma's registry does claim, so
// there is one language table in this tree and it is chroma's.
var previewShebangs = map[string]string{
	"bash": "x.sh", "sh": "x.sh", "zsh": "x.sh", "fish": "x.fish",
	"python": "x.py", "python3": "x.py", "node": "x.js", "deno": "x.js",
	"ruby": "x.rb", "perl": "x.pl", "php": "x.php", "lua": "x.lua",
	"awk": "x.awk", "env": "",
}

// previewLang is the lexer a file's contents should be read as, or "" for text
// nothing claims.
//
// The name is asked first, because it is free and it is right almost always.
// The shebang is asked second, and only for a file whose name said nothing —
// which is what a `configure`, a `pre-commit` hook or a script somebody dropped
// in ~/bin looks like, and those are exactly the files a person previews
// wondering what is in them.
//
// The empty answer is load-bearing, as it is in [codeLang]: it means "draw this
// plainly", and it is what keeps chroma's fallback lexer from painting a log
// file as though it were source.
func previewLang(path string, head []byte) string {
	if lang := prose.LexerName(filepath.Base(path)); lang != "" {
		return lang
	}
	line := firstLine(string(head))
	if !strings.HasPrefix(line, "#!") {
		return ""
	}
	fields := strings.Fields(strings.TrimPrefix(line, "#!"))
	for _, field := range fields {
		name, held := previewShebangs[filepath.Base(strings.TrimSpace(field))]
		if !held {
			continue
		}
		if name == "" {
			// `#!/usr/bin/env python3` — the interpreter is the next word.
			continue
		}
		return prose.LexerName(name)
	}
	return ""
}

// ── holding what was read ───────────────────────────────────────────────────

// previewCache holds the last few previews, keyed by the file's identity AND by
// the flags that changed the answer. The zero value is an empty cache and is
// ready to use.
//
// It is keyed by identity rather than by path, so a file that changed under the
// cursor simply misses — there is no invalidation to get wrong. Eviction is
// first-in-first-out for [codeBlockCache]'s reason: the thing being evicted has
// been walked past, and telling that apart from the thing being looked at would
// mean writing to the map on every read.
type previewCache struct {
	by    map[string]filePreview
	order []string
}

func previewCacheKey(key previewKey, flags string) string {
	return key.Path + "\x00" + itoa(int(key.Bytes)) + "\x00" + itoa(int(key.Mod)) + "\x00" + flags
}

func (c *previewCache) get(key previewKey, flags string) (filePreview, bool) {
	held, ok := c.by[previewCacheKey(key, flags)]
	return held, ok
}

func (c *previewCache) put(preview filePreview, flags string) {
	if preview.empty() {
		return
	}
	key := previewCacheKey(preview.Key, flags)
	if c.by == nil {
		c.by = map[string]filePreview{}
	}
	if _, held := c.by[key]; !held {
		c.order = append(c.order, key)
	}
	c.by[key] = preview
	for len(c.order) > previewKeep {
		delete(c.by, c.order[0])
		c.order = c.order[1:]
	}
}

// ── the pump ────────────────────────────────────────────────────────────────

// previewPump is the whole seam a pane needs: it owns the generation counter,
// the cancellation of the read nobody wants any more, and the cache.
//
// A pane holds one of these and calls three methods. [previewPump.show] on
// every cursor move; [previewPump.took] on every answer that arrives;
// [previewPump.close] when the sheet closes. The zero value is ready to use.
//
// IT IS NOT SAFE ACROSS GOROUTINES and does not need to be: every one of its
// methods is called from Update, and the only thing that runs elsewhere is the
// read itself, which touches nothing in here.
type previewPump struct {
	gen    uint64
	cancel context.CancelFunc
	cache  previewCache
	// key is the file the pane is currently asking about, so a late answer about
	// a different file can be told from the one being waited for even if the
	// generation happened to match.
	key   previewKey
	flags string
}

// show is what a cursor landing on a row asks for: the preview if it is already
// held, and otherwise the command that will read it.
//
// The generation goes up on EVERY call, and the previous read is cancelled
// before this one is started — including on a cache hit, because a hit means
// the pane no longer wants whatever was in flight.
//
// A cache hit answers with `held, nil`; a miss answers with the zero preview and
// a command. The pane draws whatever it has and gets the rest when it arrives.
func (p *previewPump) show(req previewRequest) (filePreview, tea.Cmd) {
	p.stop()
	p.gen++
	p.flags = req.flags()
	req.Gen = p.gen
	path := filepath.Clean(strings.TrimSpace(req.Path))
	if req.Path == "" {
		p.key = previewKey{}
		return filePreview{}, nil
	}
	// The identity is taken here, on the loop, because a stat is microseconds
	// and it is what lets a hit skip the goroutine entirely. A stat that fails
	// is not answered here: the reader says why, in its own words.
	p.key = previewKey{Path: path}
	if info, err := os.Stat(path); err == nil {
		p.key = previewKey{Path: path, Bytes: info.Size(), Mod: info.ModTime().UnixNano()}
		if held, ok := p.cache.get(p.key, p.flags); ok {
			return held, nil
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	return filePreview{}, previewCmd(ctx, req)
}

// took decides whether an answer may be drawn, and holds it if so.
//
// THREE WAYS AN ANSWER IS REFUSED, and all three are ordinary rather than
// exotic. Its generation is not the current one — the person moved on. Its
// flags are not the current ones — the terminal was re-measured, or the hidden
// key was pressed. Its file is not the file the pane is asking about, or is that
// file at a size or a time it no longer has — something rewrote it while the
// read was running. A refused answer is dropped whole; it is not wrong, it is
// about a moment that has passed.
func (p *previewPump) took(msg previewLoadedMsg) (filePreview, bool) {
	if msg.gen != p.gen || msg.flags != p.flags || msg.preview.empty() {
		return filePreview{}, false
	}
	if msg.preview.Key != p.key {
		return filePreview{}, false
	}
	p.cache.put(msg.preview, msg.flags)
	return msg.preview, true
}

// held answers a preview already in hand for the file the pane is asking about.
// It is what a redraw calls; it never reads a disk.
func (p *previewPump) held() (filePreview, bool) {
	if p.key.Path == "" {
		return filePreview{}, false
	}
	return p.cache.get(p.key, p.flags)
}

// stop cancels the read in flight, if there is one.
func (p *previewPump) stop() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
}

// close is stop plus forgetting what the pane was looking at, which is what a
// sheet closing means. The cache is KEPT: reopening the browser on the same
// folder is the common next gesture, and the identity in every key makes a held
// preview safe to reuse or a miss to re-read.
func (p *previewPump) close() {
	p.stop()
	p.key, p.flags = previewKey{}, ""
}

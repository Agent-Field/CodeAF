package tui3

// THE PICTURE ITSELF, IN THE EXPANSION.
//
// Every other tool's expansion shows the thing the call was about: an edit
// shows its diff, a bash shows its output, a read shows the chunk it read. The
// two picture tools showed a file path and a byte count, which is the one place
// on this surface where opening a row answered a question nobody asked — a
// person who clicks a `generate_image` row wants to know whether the picture
// came out right, and no arrangement of the words "1024×1024 png" answers that.
//
// So the expansion draws the picture.
//
// THE TECHNIQUE IS HALF-CELL TRUECOLOR, and it is chosen because it survives a
// repaint. This surface is a framebuffer: every frame is composed as rows of
// text and handed over whole, so an inline-image escape sequence (iTerm2's OSC
// 1337, kitty's graphics protocol) written into the middle of one frame is
// painted over by the next — and there is no place in this program where the
// terminal is handed away to another process, which is the only context where
// those protocols are safe to use. What a framebuffer can carry is CELLS, and a
// cell can hold two colours: U+2580 UPPER HALF BLOCK paints its foreground over
// the top half and its background over the bottom. One cell is therefore two
// pixels stacked, which is also very nearly square, because a terminal cell is
// about twice as tall as it is wide.
//
// That is the same trick chafa and timg draw with, it needs nothing but SGR,
// and it is a real colour picture inside the normal render pipeline rather than
// a sequence fighting it.
//
// WHAT IT COSTS, AND WHERE IT DEGRADES. The rungs are the palette's own
// (styles.go), because "what can this terminal say" has one answer in this tree:
//
//	TrueColor  the picture as it is, 24-bit per half-cell
//	ANSI256    the picture through the xterm cube — coarser, still a picture
//	below      NO PICTURE. Sixteen colours are the person's own theme and
//	           painting a photograph out of them would be a lie about both.
//
// A terminal that cannot be trusted with box drawing (palette.ascii) draws no
// picture either — U+2580 is the whole technique — and neither does the linear
// tier, because twenty rows of half blocks read aloud is twenty rows of nothing.
//
// In every one of those cases, and for a file that is missing, too large, or not
// a picture this program can decode, the expansion is EXACTLY what it was before
// this file existed. A preview is an addition to a row that already works.

import (
	"image"
	// The decoders are imported for their side effect: they register themselves
	// with image.Decode. png, jpeg and gif are the standard library's; webp is
	// golang.org/x/image, which this module already requires — and the four
	// together are exactly the formats view_image accepts, so a picture the
	// session agreed to look at is a picture this surface can draw.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"
	_ "golang.org/x/image/webp"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// halfBlock is the whole alphabet of a preview: foreground is the top pixel,
// background is the bottom one.
const halfBlock = "▀"

const (
	// pictureRowsMax is how many terminal rows one preview may take. It is the
	// tallest block this surface draws under a tool row, and it is deliberately
	// larger than every text window in D11's table: a diff capped at forty lines
	// can be read forty lines at a time, while half a picture is not half an
	// answer. Twenty rows is forty pixel rows, which is enough to see whether a
	// diagram's labels are in the right places.
	pictureRowsMax = 20
	// pictureColsMin is the narrowest preview worth drawing. Under this the
	// picture is a smudge that says less than the path under it, so nothing is
	// drawn and the expansion keeps the words it always had.
	pictureColsMin = 8
	// pictureBytesMax is the largest file this surface will decode. It is the
	// session's own 10MB picture ceiling with room over it for a generated png,
	// and it exists so a person who opens a row never waits on a gigabyte.
	pictureBytesMax = 24 << 20
	// picturePixelsMax bounds the DECODE rather than the file: a small file can
	// hold an enormous picture, and decoding it allocates four bytes a pixel
	// whatever the row is going to show.
	picturePixelsMax = 64 << 20
	// pictureCacheMax is how many previews are kept. A preview is a few kilobytes
	// of painted rows and rebuilding one is a decode, so the cache exists to keep
	// a frame cheap rather than to save memory; past this many it is dropped
	// whole, because an eviction order over eight entries is more machinery than
	// the problem has.
	pictureCacheMax = 8
)

// imagePreview is one picture as this surface already drew it: the painted
// half-block rows, and the facts the line under them states. ok is false for a
// file that was looked at and could not be drawn, which is cached too — a
// missing file must not be re-stat'd ten times a second either.
type imagePreview struct {
	rows   []string
	width  int // the picture's own pixel width
	height int // and its height
	bytes  int
	ok     bool
}

// pictureRows is the expansion's picture: the half-block rows, then one dim
// line naming the file. It answers false when there is nothing to draw, and
// every caller falls back to the words it would have shown.
func (a *app) pictureRows(e *entry, width int) ([]string, bool) {
	path, found := a.picturePath(e)
	if !found {
		return nil, false
	}
	preview, drawn := a.picture(path, width)
	if !drawn {
		return nil, false
	}
	return append(append([]string(nil), preview.rows...),
		picturePathLine(a.pal, path, preview, width)...), true
}

// picture is one preview, off the cache or freshly decoded.
//
// The key carries everything the answer depends on — the file's identity AND
// the shape of the terminal it was drawn for — so a picture that was overwritten
// on disk, a window that was dragged wider, and a theme that was switched all
// produce a new rendering rather than a stale one.
func (a *app) picture(path string, cols int) (imagePreview, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return imagePreview{}, false
	}
	key := path + "\x00" + itoa(int(info.ModTime().UnixNano())) +
		"\x00" + itoa(int(info.Size())) + "\x00" + itoa(cols) +
		"\x00" + itoa(int(a.pal.profile)) + "\x00" + itoa(int(a.pal.ramp.ink.r))
	if hit, known := a.previews[key]; known {
		return hit, hit.ok
	}
	preview := renderPicture(a.pal, path, int(info.Size()), cols)
	if len(a.previews) >= pictureCacheMax {
		a.previews = nil
	}
	if a.previews == nil {
		a.previews = make(map[string]imagePreview, pictureCacheMax)
	}
	a.previews[key] = preview
	return preview, preview.ok
}

// renderPicture reads one file and draws it, or answers that it could not.
func renderPicture(pal palette, path string, size, cols int) imagePreview {
	if !pal.paintsPictures() || cols < pictureColsMin || size <= 0 || size > pictureBytesMax {
		return imagePreview{}
	}
	file, err := os.Open(path)
	if err != nil {
		return imagePreview{}
	}
	defer file.Close()

	// The header is read first and on its own, because it is the cheap half of
	// the question: a picture too large to hold in memory is refused here, before
	// four bytes a pixel are asked for.
	config, _, err := image.DecodeConfig(file)
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return imagePreview{}
	}
	if config.Width*config.Height > picturePixelsMax {
		return imagePreview{}
	}
	if _, err := file.Seek(0, 0); err != nil {
		return imagePreview{}
	}
	source, _, err := image.Decode(file)
	if err != nil {
		return imagePreview{}
	}

	gridCols, gridRows := pictureGrid(config.Width, config.Height, cols, pictureRowsMax)
	if gridCols < 1 || gridRows < 1 {
		return imagePreview{}
	}
	return imagePreview{
		rows:   pictureCells(pal, source, gridCols, gridRows),
		width:  config.Width,
		height: config.Height,
		bytes:  size,
		ok:     true,
	}
}

// paintsPictures is the terminal's answer to "can a photograph be drawn here".
//
// It is three vetoes and no yes of its own, the shape every capability question
// on this surface has (styles.go's [detectASCII]): colour below the xterm cube
// is the person's own theme, U+2580 is the technique, and the linear tier is
// being read aloud rather than looked at.
func (p palette) paintsPictures() bool {
	if p.ascii || p.linear {
		return false
	}
	return p.profile == tokens.TrueColor || p.profile == tokens.ANSI256
}

// pictureGrid is the cell rectangle one picture is drawn into: as wide as it is
// allowed and as tall as its own shape then demands, bounded by both.
//
// THE ARITHMETIC IS ABOUT SQUARE SUB-PIXELS. One cell carries two stacked
// pixels and is itself about twice as tall as it is wide, so a half-cell is
// very nearly square and the grid is sized as though it were: cols/(rows*2) is
// made to equal the picture's own aspect. Anything else draws a portrait as an
// egg.
//
// IT NEVER ENLARGES. A sixteen-pixel icon blown across sixty columns is sixty
// columns of blur that claims to be detail; drawn at sixteen it is a thumbnail
// that is honest about how much picture there is.
func pictureGrid(imageWidth, imageHeight, maxCols, maxRows int) (cols, rows int) {
	if imageWidth <= 0 || imageHeight <= 0 || maxCols <= 0 || maxRows <= 0 {
		return 0, 0
	}
	cols = maxCols
	if imageWidth < cols {
		cols = imageWidth
	}
	// rows = cols · height / width / 2, rounded to nearest so a wide-and-short
	// picture keeps at least the one row it is owed.
	rows = (cols*imageHeight + imageWidth) / (imageWidth * 2)
	if rows < 1 {
		rows = 1
	}
	if rows > maxRows {
		// Too tall for the budget: the height is what binds, and the width
		// follows from it so the shape survives.
		rows = maxRows
		cols = (rows * 2 * imageWidth) / imageHeight
		if cols < 1 {
			cols = 1
		}
		if cols > maxCols {
			cols = maxCols
		}
	}
	return cols, rows
}

// pictureCells maps a decoded picture onto cols×rows terminal cells and paints
// them, one string per row.
//
// The filter is a BOX: each half-cell averages every source pixel that falls
// inside it. Nearest-neighbour would be one line shorter and would alias a
// screenshot's text into noise — a box average over a downscale is the cheapest
// filter that keeps thin strokes visible, and this is always a downscale
// ([pictureGrid] never enlarges).
//
// Alpha is composited over the terminal's presumed ground rather than ignored,
// because a transparent png drawn as though it were opaque is a picture of the
// wrong colours: an icon exported on transparency would come out as its own
// unpremultiplied fringe.
func pictureCells(pal palette, source image.Image, cols, rows int) []string {
	bounds := source.Bounds()
	if bounds.Empty() || cols < 1 || rows < 1 {
		return nil
	}
	backR, backG, backB := pal.backdrop()

	// One scan of the source per half-cell row, so a picture is walked once.
	subRows := rows * 2
	out := make([]string, 0, rows)
	top := make([]cellColour, cols)
	bottom := make([]cellColour, cols)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			top[c] = sampleBox(source, bounds, c, 2*r, cols, subRows, backR, backG, backB)
			bottom[c] = sampleBox(source, bounds, c, 2*r+1, cols, subRows, backR, backG, backB)
		}
		out = append(out, pictureRow(pal, top, bottom))
	}
	return out
}

// cellColour is one half-cell's colour, already flattened onto the ground.
type cellColour struct{ r, g, b uint8 }

// sampleBox averages the source rectangle that half-cell (cx, cy) covers.
//
// The rectangle is computed from the cell index rather than accumulated, so
// rounding never drifts across the picture, and it is widened to at least one
// pixel: a grid finer than the source in one axis must still sample something.
func sampleBox(source image.Image, bounds image.Rectangle, cx, cy, cols, rows int,
	backR, backG, backB uint8) cellColour {

	w, h := bounds.Dx(), bounds.Dy()
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

	// The sums are over 16-bit PREMULTIPLIED components, which is what the image
	// interface hands back and the only form in which averaging is correct: an
	// unpremultiplied average weights a transparent pixel's colour as heavily as
	// an opaque one's.
	var sumR, sumG, sumB, sumA uint64
	var n uint64
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			r, g, b, alpha := source.At(x, y).RGBA()
			sumR += uint64(r)
			sumG += uint64(g)
			sumB += uint64(b)
			sumA += uint64(alpha)
			n++
		}
	}
	if n == 0 {
		return cellColour{backR, backG, backB}
	}
	over := func(sum uint64, ground uint8) uint8 {
		// src + ground·(1−α), all in the 16-bit space, then down to 8.
		value := sum/n + uint64(ground)*257*(0xffff-sumA/n)/0xffff
		if value > 0xffff {
			value = 0xffff
		}
		return uint8(value >> 8)
	}
	return cellColour{over(sumR, backR), over(sumG, backG), over(sumB, backB)}
}

// backdrop is what a transparent pixel is composited over: the ground the
// terminal almost certainly has behind this frame.
//
// It is read off the RAMP rather than asked of the terminal, because there is
// no reliable way to ask and the ramp is the answer this surface already
// committed to — a light ramp is drawn on a page and a dark one on a slate.
func (p palette) backdrop() (r, g, b uint8) {
	if p.ramp.ink == lightRamp.ink {
		return 0xff, 0xff, 0xff
	}
	return 0x00, 0x00, 0x00
}

// pictureRow paints one row of half blocks.
//
// A colour is only written when it CHANGES. A flat sky is one SGR and forty
// glyphs rather than forty SGRs, which matters twice: it is what a repaint
// costs, and it is what a row costs to hold in the cache. The row closes on
// SGR 39;49 so the next thing drawn inherits nothing.
func pictureRow(pal palette, top, bottom []cellColour) string {
	var b strings.Builder
	b.Grow(len(top) * 12)
	var lastTop, lastBottom cellColour
	started := false
	for i := range top {
		if !started || top[i] != lastTop || bottom[i] != lastBottom {
			b.WriteString(pal.halfCellSGR(top[i], bottom[i]))
			lastTop, lastBottom, started = top[i], bottom[i], true
		}
		b.WriteString(halfBlock)
	}
	if started {
		b.WriteString("\x1b[39;49m")
	}
	return b.String()
}

// halfCellSGR is one cell's two colours in a single escape sequence, on
// whichever rung this terminal reached.
func (p palette) halfCellSGR(top, bottom cellColour) string {
	if p.profile == tokens.ANSI256 {
		return "\x1b[38;5;" + itoa(int(nearest256(top.r, top.g, top.b))) +
			";48;5;" + itoa(int(nearest256(bottom.r, bottom.g, bottom.b))) + "m"
	}
	return "\x1b[38;2;" + itoa(int(top.r)) + ";" + itoa(int(top.g)) + ";" + itoa(int(top.b)) +
		";48;2;" + itoa(int(bottom.r)) + ";" + itoa(int(bottom.g)) + ";" + itoa(int(bottom.b)) + "m"
}

// ── the line under the picture ──────────────────────────────────────────────

// picturePathLine is everything the preview says in words: the file, whole and
// absolute, then its shape and its size.
//
// THE PATH IS NEVER TRUNCATED, which is the one place this file breaks the
// expansion's own rule that rows truncate rather than wrap. A path with an
// ellipsis in it is a path nobody can click, copy or paste, and the whole point
// of the line is that a person can get from the picture to the file. So it is
// given the lines it needs, and the shape and size step down to their own line
// first — they are the part a reader can lose.
//
// It is wrapped in OSC 8 as well, the way every other location on this surface
// is (opener.go), so a terminal that understands hyperlinks makes it clickable
// and one that does not shows exactly the characters that can be selected.
func picturePathLine(pal palette, path string, preview imagePreview, width int) []string {
	if width < 4 {
		width = 4
	}
	shape := itoa(preview.width) + "×" + itoa(preview.height)
	if size := byteWord(preview.bytes); size != "" {
		shape += " · " + size
	}
	uri := "file://" + path

	if together := path + " · " + shape; ansi.StringWidth(together) <= width {
		return []string{pal.dim(linkify(path, uri) + " · " + shape)}
	}
	if ansi.StringWidth(path) <= width {
		return []string{pal.dim(linkify(path, uri)), pal.dim(shape)}
	}
	// Narrower than the path itself. The path still goes down whole, across as
	// many rows as it takes, each segment carrying the same link — a wrapped
	// path can still be read and copied, and a cut one cannot be either.
	out := make([]string, 0, 3)
	for _, segment := range wrap(path, width) {
		out = append(out, pal.dim(linkify(segment, uri)))
	}
	return append(out, pal.dim(fit(shape, width)))
}

// ── which file a call is about ──────────────────────────────────────────────

// pictureSuffixes are the formats [renderPicture] can decode, checked before a
// file is opened so a call naming a pdf costs nothing.
var pictureSuffixes = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
}

// picturePath is the file one picture call is about, absolute, or false.
//
// It asks the ARGUMENTS first and the result second, which is the order
// [toolTarget] already uses and for the same reason: the arguments are what
// actually arrived. The result is asked at all because `generate_image` chooses
// the name itself when the caller did not — the path it picked is the first
// thing its one-line result says, before the em dash — and that is the only
// record of where the picture went.
func (a *app) picturePath(e *entry) (string, bool) {
	if e == nil || (e.tool != "generate_image" && e.tool != "view_image") {
		return "", false
	}
	candidates := []string{argString(argsOf(e.detail.Args), "path")}
	if e.tool == "generate_image" {
		candidates = append(candidates, generatedPicturePath(e.detail.Output))
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || !pictureSuffixes[strings.ToLower(filepath.Ext(candidate))] {
			continue
		}
		if !filepath.IsAbs(candidate) {
			if a.workspace == "" {
				continue
			}
			candidate = filepath.Join(a.workspace, candidate)
		}
		return filepath.Clean(candidate), true
	}
	return "", false
}

// generatedPicturePath reads the file out of `generate_image`'s result, whose
// whole first line is the path, an em dash, and the facts:
//
//	.aforge-v3/images/20260817-142201-a-harbour.png — 1024×1024 png, 1.4MB, generated on <model>
//
// A result that does not have that shape yields nothing, which is the honest
// floor: a guessed path would draw somebody else's picture.
func generatedPicturePath(output string) string {
	line := firstLine(strings.TrimSpace(resultText(output)))
	if at := strings.Index(line, " — "); at > 0 {
		return strings.TrimSpace(line[:at])
	}
	return ""
}

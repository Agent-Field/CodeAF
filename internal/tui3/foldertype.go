package tui3

// WHAT KIND OF THING A ROW IS, IN ONE CELL.
//
// The owner's reference (../reference-yazi.png) carries a small mark beside
// every name, and the 2026-09-08 review asks for the same thing here: a compact
// visual navigation aid, so a directory of forty files can be scanned for "the
// picture" or "the source" without reading a single name.
//
// ── THE THREE RULES THIS SET IS BUILT UNDER ─────────────────────────────────
//
//   - ONE CELL, FROM THE BLOCK THIS SURFACE ALREADY DRAWS. Every glyph below is
//     in the U+25A0 geometric-shapes block, which is single-width in every
//     terminal font aforge has been run in, and three of them (`▸`, `◆`, `▪`)
//     are already on this surface elsewhere. There are NO Nerd Font private-use
//     characters and no emoji: a private-use codepoint is tofu on a font that
//     does not carry it, and an emoji is two cells wide on some terminals and
//     one on others — either way it is a row whose columns no longer line up.
//   - SHAPE FIRST, WITH COLOUR AS A SECOND CUE. A small type mark helps the eye
//     find a picture or source file in a dense directory. Its restrained colour
//     reinforces the shape, while the name and extension remain readable without
//     either. Names stay in reading ink and metadata stays quieter.
//   - AN ASCII FLOOR THAT IS ASCII. A terminal that cannot be trusted with a
//     box-drawing character is given a character it can draw rather than
//     something close (styles.go states this rule for the rail).
//
// The set is deliberately SMALL. Seven kinds is what a person can learn without
// being taught; a mark per language would be a second alphabet on the sheet.

import (
	"path/filepath"
	"strings"
)

// folderKind is what a row is, for the purposes of the one cell beside it.
type folderKind uint8

const (
	// folderKindPlain is everything nothing else claimed. It draws the quietest
	// mark there is, because "an ordinary file" is not news.
	folderKindPlain folderKind = iota
	folderKindDir
	folderKindSource
	folderKindText
	folderKindPicture
	folderKindMedia
	folderKindBundle
)

// The marks, and the floor under them. They are constants because the manual
// quotes the set exactly as it is spelled here.
const (
	folderGlyphDir     = "▸"
	folderGlyphSource  = "◆"
	folderGlyphText    = "▤"
	folderGlyphPicture = "▣"
	folderGlyphMedia   = "▶"
	folderGlyphBundle  = "▦"
	folderGlyphPlain   = "·"
)

// The same seven at the plain floor. Each is one ASCII character, so a row's
// columns land in the same cells whichever floor a terminal is on.
const (
	folderAsciiDir     = ">"
	folderAsciiSource  = "*"
	folderAsciiText    = "="
	folderAsciiPicture = "#"
	folderAsciiMedia   = "+"
	folderAsciiBundle  = "%"
	folderAsciiPlain   = "."
)

// folderTypeGlyph is the mark for one row. The name may carry a trailing slash —
// it is what the column drew — and is stripped rather than trusted.
func folderTypeGlyph(pal palette, name string, dir bool) string {
	kind := folderKindDir
	if !dir {
		kind = folderKindOf(strings.TrimSuffix(name, "/"))
	}
	if pal.ascii {
		switch kind {
		case folderKindDir:
			return folderAsciiDir
		case folderKindSource:
			return folderAsciiSource
		case folderKindText:
			return folderAsciiText
		case folderKindPicture:
			return folderAsciiPicture
		case folderKindMedia:
			return folderAsciiMedia
		case folderKindBundle:
			return folderAsciiBundle
		}
		return folderAsciiPlain
	}
	switch kind {
	case folderKindDir:
		return folderGlyphDir
	case folderKindSource:
		return folderGlyphSource
	case folderKindText:
		return folderGlyphText
	case folderKindPicture:
		return folderGlyphPicture
	case folderKindMedia:
		return folderGlyphMedia
	case folderKindBundle:
		return folderGlyphBundle
	}
	return folderGlyphPlain
}

// folderTypeMark paints the same compact type cue in both browsable columns.
// The palette owns colour capability, so monochrome terminals keep the shape
// without introducing escape sequences or changing the row's cell width.
func folderTypeMark(pal palette, name string, dir bool) string {
	ink := pal.dim
	if dir {
		ink = pal.muted
	} else {
		switch folderKindOf(name) {
		case folderKindSource:
			ink = pal.violet
		case folderKindText:
			ink = pal.data
		case folderKindPicture:
			ink = pal.accent
		case folderKindMedia, folderKindBundle:
			ink = pal.money
		}
	}
	return ink(folderTypeGlyph(pal, name, dir) + " ")
}

// folderKindOf decides what one FILE is from its name alone.
//
// FROM THE NAME AND NEVER FROM THE BYTES. This runs once per drawn row of every
// frame the sheet is painted on, and opening a file to find out what it is would
// be a read per row per frame — the exact cost the preview pump exists to keep
// off this path (PERF.md's rule about work on the paint path). A name that lies
// draws the wrong mark and nothing else: the mark is an aid, and the preview
// beside it is the truth.
func folderKindOf(name string) folderKind {
	// A DOTFILE WITH NO EXTENSION IS CONFIGURATION, which is what `.gitignore`,
	// `.env` and `.bashrc` all are — and [filepath.Ext] calls the whole of each
	// of them an extension, so they are answered before it is asked.
	base := strings.ToLower(filepath.Base(name))
	if strings.HasPrefix(base, ".") && strings.Count(base, ".") == 1 {
		return folderKindSource
	}
	switch ext := strings.ToLower(filepath.Ext(base)); ext {
	case ".go", ".rs", ".c", ".h", ".cc", ".cpp", ".hpp", ".py", ".rb", ".java",
		".kt", ".swift", ".js", ".jsx", ".ts", ".tsx", ".sh", ".bash", ".zsh",
		".fish", ".lua", ".php", ".pl", ".sql", ".vim", ".el", ".hs", ".ml",
		".scala", ".clj", ".ex", ".exs", ".erl", ".zig", ".nim", ".dart", ".r":
		return folderKindSource
	case ".json", ".yaml", ".yml", ".toml", ".ini", ".conf", ".cfg", ".xml",
		".env", ".properties", ".lock", ".mod", ".sum", ".gradle", ".tf":
		return folderKindSource
	case ".md", ".markdown", ".txt", ".rst", ".org", ".adoc", ".log", ".csv",
		".tsv", ".pdf", ".rtf", ".doc", ".docx", ".tex":
		return folderKindText
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tiff", ".tif",
		".svg", ".ico", ".avif", ".heic":
		return folderKindPicture
	case ".mp4", ".mov", ".mkv", ".webm", ".avi", ".mp3", ".wav", ".flac",
		".ogg", ".m4a", ".aac", ".opus":
		return folderKindMedia
	case ".zip", ".tar", ".gz", ".tgz", ".bz2", ".xz", ".zst", ".7z", ".rar",
		".jar", ".whl", ".deb", ".rpm", ".dmg", ".iso":
		return folderKindBundle
	}
	// A name with no extension at all is very often a program — `Makefile`,
	// `Dockerfile`, `aforge` — but "very often" is not a fact about this file, so
	// it draws the quiet mark rather than a guess dressed as one.
	return folderKindPlain
}

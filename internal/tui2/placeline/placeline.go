package placeline

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// glyphWorkspace marks a [SegmentWorkspace] leg under the styler's repertoire
// tier (12.7): [tokens.GlyphHome] plain, nf-fa-home under a patched font. Both
// sides are one cell, so [fit]'s arithmetic is the same either way.
//
// doc.go used to say tokens did not carry this glyph and that the local
// constant was the honest way to draw it. Tokens carries it now, as the GHome
// SLOT, and a slot is the thing worth reaching for: the local constant could
// only ever draw the floor, so the place line was the one surface that stayed
// plain when a user turned the tier on.
func (m *Model) glyphWorkspace() string {
	if m.styler == nil {
		return tokens.Plain.Glyph(tokens.GHome)
	}
	return m.styler.Glyph(tokens.GHome)
}

// SegmentKind says what kind of leg a [Segment] is, which decides both its
// glyph and whether it is fish-abbreviated. See doc.go.
type SegmentKind uint8

const (
	// SegmentRoot is the resident root: an absolute path, fish-abbreviated
	// (5.19: "first-letter segments, full last segment") whenever the line is
	// not focused, no glyph. A ground has at most one of these, and it is
	// always first when present — 5.19's own first example, `~/aforge-v2`, is
	// a root segment standing alone with nothing after it.
	SegmentRoot SegmentKind = iota
	// SegmentWorkspace is a task's own workspace: an absolute path that may
	// sit anywhere on disk, marked with [glyphWorkspace], never
	// fish-abbreviated.
	SegmentWorkspace
	// SegmentRegion is a path relative to the nearest preceding absolute leg
	// (the last [SegmentWorkspace], or the [SegmentRoot] if the ground has no
	// workspace leg) — a worker's own slice of its task's workspace. No
	// glyph, never fish-abbreviated, for the same reason as
	// [SegmentWorkspace]: it IS the material ground.
	SegmentRegion
)

// Segment is one leg of the ground the place line describes.
type Segment struct {
	// Path is the leg's own text: an absolute path for [SegmentRoot] and
	// [SegmentWorkspace], a path relative to the nearest preceding absolute
	// leg for [SegmentRegion].
	Path string
	Kind SegmentKind
}

// Options configures a [Model]. Every field's name and type is the contract
// the wiring builds against.
type Options struct {
	// Styler paints the line. A nil Styler degrades to unstyled plain text —
	// the composer package's own posture for the same situation — rather
	// than panicking.
	Styler *tokens.Styler
	// Home is the resident user's home directory, used only to fold a
	// [SegmentRoot] path into the "~" form before fish-abbreviating it. An
	// empty Home disables the fold: the root is still abbreviated, just from
	// its own leading slash instead of from "~".
	Home string
}

// Model is the place line. The zero value is not meaningful; construct one
// with [New].
type Model struct {
	styler   *tokens.Styler
	home     string
	segments []Segment
	focused  bool
}

// New builds a place line from opts. It never fails: a missing Styler
// degrades to plain text and a missing Home simply disables tilde-folding —
// both honest, visible degradations rather than a construction error.
func New(opts Options) *Model {
	return &Model{styler: opts.Styler, home: strings.TrimRight(opts.Home, "/")}
}

// SetGround replaces the whole ground the line describes — the selected
// surface's material root (5.19: "it always describes the selected surface's
// material ground"). Passing no segments clears the line: [Model.Render]
// then draws nothing rather than a stale ground left over from whatever was
// selected before, and [Model.CopyText] returns "".
//
// The caller's slice is copied; Model owns its own storage afterward.
func (m *Model) SetGround(segments ...Segment) {
	m.segments = append(m.segments[:0], segments...)
}

// Focus tells the line whether the pane it sits on is the one the user is
// looking at. Focused reveals the root leg's full, unabbreviated form (5.19:
// "full path on focus") — the fish abbreviation is the only thing focus
// restores, because the workspace and region legs are already shown in full
// at every focus state (see [SegmentWorkspace], [SegmentRegion]).
func (m *Model) Focus(focused bool) { m.focused = focused }

// CopyText returns the real, resolved path of the ground's most specific
// leg — what `y` copies (5.19: "click/y copies"). A root or workspace leg IS
// that path outright; a region leg is relative to the nearest preceding
// absolute leg, joined with the "/" this line always displays paths in.
// Empty ground returns "".
func (m *Model) CopyText() string {
	var base string
	for _, seg := range m.segments {
		if seg.Kind == SegmentRegion {
			base = joinPath(base, seg.Path)
			continue
		}
		base = seg.Path
	}
	return base
}

// Render draws the line at width cells. It is a pure function of the ground
// and the focus state: never more than one row, never wider than width, and
// never a panic — down to width=1 and an empty ground alike.
//
// It opens at the lens's left edge ([tokens.LensIndent]) rather than at column
// 0: the ground is one of the room's surfaces, and a room has one left edge
// (5.13's spacing rhythm). The indent comes out of the fitting width, never out
// of the terminal's — an indent added without paying for it is an overflow one
// breakpoint later.
func (m *Model) Render(width int) string {
	if width <= 0 || len(m.segments) == 0 {
		return ""
	}
	pad, inner := indentAt(width)
	return pad + m.paint(fit(m.labels(), inner))
}

// Text is the ground as PLAIN words, fitted to width, with no left edge and no
// paint on it: the abbreviated form this component has always drawn, handed to
// a surface that wants to place it somewhere else and ink it itself.
//
// It exists because the chat hug stopped stacking the place line as a row of
// its own (§7 folds the directory into the bottom bar's right zone, beside the
// model word and the day's spend), and a footer cell cannot be built out of
// [Model.Render]: that method pads to the lens edge and paints, and a caller
// measuring a painted string is measuring its escape sequences. The FITTING is
// the part worth sharing — fish abbreviation, tilde folding, legs dropped from
// the left under the overflow mark — because that is the grammar 5.19 spent and
// a second spelling of it in the footer would be the same rule drifting apart.
//
// An empty ground returns "", which is §16's EMPTINESS: a surface with nothing
// to say about where it stands says nothing, rather than saying so at length.
func (m *Model) Text(width int) string {
	if width <= 0 || len(m.segments) == 0 {
		return ""
	}
	return fit(m.labels(), width)
}

// lensPad is the left edge written as cells.
var lensPad = strings.Repeat(" ", tokens.LensIndent)

// indentAt is the edge this row can afford and the width left over for the
// path. A terminal too narrow to hold the gutter AND a leg gives the gutter up:
// two cells of rhythm are not worth the last two cells of a ground.
func indentAt(width int) (pad string, inner int) {
	if width <= tokens.LensIndent {
		return "", width
	}
	return lensPad, width - tokens.LensIndent
}

// labels renders each segment's own display text — root fish-abbreviated
// (unless focused) and tilde-folded, workspace glyph-prefixed and full,
// region full — with no separators and no width fitting yet; [fit] does
// that.
func (m *Model) labels() []string {
	out := make([]string, len(m.segments))
	for i, seg := range m.segments {
		switch seg.Kind {
		case SegmentRoot:
			text := withTilde(seg.Path, m.home)
			if !m.focused {
				text = fishAbbreviate(text)
			}
			out[i] = text
		case SegmentWorkspace:
			out[i] = m.glyphWorkspace() + " " + seg.Path
		default: // SegmentRegion
			out[i] = seg.Path
		}
	}
	return out
}

func (m *Model) paint(text string) string {
	if m.styler == nil || text == "" {
		return text
	}
	return m.styler.PaintToken(text, tokens.TextTertiary)
}

const (
	// lineSep joins legs. Cell width 3: space, the telemetry separator, space.
	lineSep = " " + tokens.GlyphSeparator + " "
	// overflowMark stands in for legs dropped off the left under width
	// pressure. It is [tokens.GlyphEllipsis] and never [tokens.GlyphTruncated]:
	// nothing here is clickable, and that glyph means "click for more"
	// everywhere else it appears. Both marks are slots in one table now, so the
	// distinction is the vocabulary's to keep rather than this file's.
	overflowMark = tokens.GlyphEllipsis
)

// fit finds the widest suffix of labels (dropping legs from the LEFT — the
// broadest, oldest context, and the reverse of blocks.Header's own
// right-to-left degrade, see doc.go) that joins into width cells, prefixing
// overflowMark when anything was dropped. If even the last label alone does
// not fit, it gives way at the one point 5.19/5.21 allow a path itself to
// shrink: [blocks.TruncatePath]'s middle-ellipsis, never a tail cut.
func fit(labels []string, width int) string {
	n := len(labels)
	for start := 0; start < n; start++ {
		rest := strings.Join(labels[start:], lineSep)
		candidate := rest
		if start > 0 {
			candidate = overflowMark + lineSep + rest
		}
		if blocks.Width(candidate) <= width {
			return candidate
		}
	}
	last := labels[n-1]
	if blocks.Width(last) <= width {
		return last
	}
	return blocks.TruncatePath(last, width)
}

// fishAbbreviate shortens every segment but the last to its first rune —
// fish/p10k-style — because "never tail-truncate a path" (5.21) means the
// abbreviation may thin the ancestry, never the destination. A leading empty
// segment (an absolute path's own leading slash, or the "~" [withTilde]
// folded in) passes through unabbreviated: there is nothing there to
// shorten, and abbreviating "~" would not shorten it anyway.
func fishAbbreviate(path string) string {
	if path == "" {
		return path
	}
	segs := strings.Split(path, "/")
	for i := 0; i < len(segs)-1; i++ {
		if segs[i] == "" || segs[i] == "~" {
			continue
		}
		segs[i] = firstRune(segs[i])
	}
	return strings.Join(segs, "/")
}

// firstRune returns s's first rune as a string, rune-safe so a non-ASCII
// directory name abbreviates to one whole character rather than a broken
// UTF-8 byte.
func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return s
}

// withTilde folds home as a "~" prefix, the way every shell prompt does,
// before [fishAbbreviate] runs. An empty home or a path that does not sit
// under it passes through unchanged.
func withTilde(path, home string) string {
	if home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+"/") {
		return "~" + path[len(home):]
	}
	return path
}

// joinPath joins a region leg onto its base without lexical cleaning — the
// path is displayed and copied exactly as the wiring gave it, never
// reinterpreted.
func joinPath(base, rel string) string {
	if base == "" {
		return rel
	}
	if rel == "" {
		return base
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(rel, "/")
}

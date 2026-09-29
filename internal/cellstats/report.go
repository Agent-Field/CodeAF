package cellstats

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ExportNotice is the wording shown when the person asks for an export.
const ExportNotice = "counts only: bytes, objects and requests per turn; no content, no paths"

// Render prints one row per flush and a total row. With no lines it prints
// nothing at all: an empty report is silence, not a table of zeros.
func Render(w io.Writer, lines []Line) { RenderScopes(w, lines, nil) }

// RenderScopes prints a cell's rows and, as a row of its own, what carrying the
// vault cost on this device. The vault serves every chat, so it is never folded
// into one cell's total. With nothing in either scope it prints nothing.
func RenderScopes(w io.Writer, lines, vault []Line) {
	if len(lines)+len(vault) == 0 {
		return
	}
	fmt.Fprintf(w, "%-12s %5s %6s %7s %10s %5s %5s %5s %10s\n",
		"turn", "turns", "frames", "objects", "bytes_up", "puts", "gets", "has", "bytes_down")
	for _, l := range lines {
		row(w, shortID(l.Turn), l)
	}
	rowIfAny(w, "total", lines)
	rowIfAny(w, VaultScope, vault)
}

// rowIfAny prints the sum of lines under name, or nothing for no lines.
func rowIfAny(w io.Writer, name string, lines []Line) {
	if len(lines) > 0 {
		row(w, name, Total(lines))
	}
}

func row(w io.Writer, name string, l Line) {
	fmt.Fprintf(w, "%-12s %5d %6d %7d %10d %5d %5d %5d %10d\n",
		name, l.Turns, l.Frames, l.Objects, l.BytesUp, l.Puts, l.Gets, l.Has, l.BytesDown)
}

// shortID is the first ten characters of a turn id; a line no flush carried has
// no turn and shows a dash.
func shortID(id string) string {
	if id == "" {
		return "-"
	}
	if len(id) > 10 {
		return id[:10]
	}
	return id
}

// Export writes the lines, in the stats file's own encoding, to path. It is
// called only on request, and it writes nothing when there is nothing to say.
func Export(path string, lines []Line) error {
	if len(lines) == 0 {
		return nil
	}
	var out []byte
	for _, l := range lines {
		out = append(out, Marshal(l)...)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}

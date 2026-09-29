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
func Render(w io.Writer, lines []Line) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(w, "%-12s %5s %6s %7s %10s %5s %5s %5s %10s\n",
		"turn", "turns", "frames", "objects", "bytes_up", "puts", "gets", "has", "bytes_down")
	for _, l := range lines {
		row(w, shortID(l.Turn), l)
	}
	row(w, "total", Total(lines))
}

func row(w io.Writer, name string, l Line) {
	fmt.Fprintf(w, "%-12s %5d %6d %7d %10d %5d %5d %5d %10d\n",
		name, l.Turns, l.Frames, l.Objects, l.BytesUp, l.Puts, l.Gets, l.Has, l.BytesDown)
}

func shortID(id string) string {
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

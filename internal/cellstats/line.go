package cellstats

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Line is one flush, exactly as contract §11 freezes it. Every field is a
// count or an opaque id; adding a string field that could hold a name would
// break TestStatsCarryNoPaths, which is the point.
type Line struct {
	V         int    `json:"V"`
	Turn      string `json:"turn"`
	Turns     int    `json:"turns"`
	Frames    int    `json:"frames"`
	Objects   int    `json:"objects"`
	BytesUp   int64  `json:"bytes_up"`
	Puts      int64  `json:"puts"`
	Gets      int64  `json:"gets"`
	Has       int64  `json:"has"`
	BytesDown int64  `json:"bytes_down"`
}

// Version is the only line version there is.
const Version = 1

// VaultScope names the machine-level scope that holds what carrying the vault
// cost. It is not a cell id and never can be (those are ULIDs), so its lines sit
// beside the cells' in the same folder and are read the same way.
const VaultScope = "vault"

// Path is where one cell's lines live under a home directory.
func Path(home, cellID string) string {
	return filepath.Join(home, "v3", "sync", "stats", cellID+".jsonl")
}

// Marshal is the one encoding of a line, newline included, shared by the
// stats file and the export so the two can never disagree.
func Marshal(l Line) []byte {
	raw, _ := json.Marshal(l) // a struct of scalars cannot fail to encode
	return append(raw, '\n')
}

// Read answers every line of a cell's stats file in append order. A cell that
// never flushed has no file and no lines, which is not an error.
func Read(home, cellID string) ([]Line, error) {
	f, err := os.Open(Path(home, cellID))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return decode(f)
}

func decode(r io.Reader) ([]Line, error) {
	var lines []Line
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		var l Line
		if err := json.Unmarshal(raw, &l); err != nil {
			return nil, fmt.Errorf("cellstats: unreadable line: %w", err)
		}
		lines = append(lines, l)
	}
	return lines, sc.Err()
}

// Total sums a cell's lines. Its Turn is empty: a total names no turn.
func Total(lines []Line) Line {
	t := Line{V: Version}
	for _, l := range lines {
		t.Turns += l.Turns
		t.Frames += l.Frames
		t.Objects += l.Objects
		t.BytesUp += l.BytesUp
		t.Puts += l.Puts
		t.Gets += l.Gets
		t.Has += l.Has
		t.BytesDown += l.BytesDown
	}
	return t
}

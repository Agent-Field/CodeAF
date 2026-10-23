package cellstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// tailBytes is how much of turns.jsonl is read to find the last turn; a turn
// line is a few hundred bytes.
const tailBytes = 8192

// maxBlob caps a stored external-call output (SCHEMAS.md Q8). A larger stream
// is kept as its hash only.
const maxBlob = 1 << 20

// rel joins a cell-relative path onto the cell root.
func rel(c cell.Cell, p string) string { return filepath.Join(c.Root, filepath.FromSlash(p)) }

// writeFile creates the file's directory and writes it. Content-addressed
// files are idempotent, so no rename dance is needed.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// compose writes every harness-owned file the seal will carry: meta.json
// as the harness holds it, the receipt, and the output blobs of external calls.
func (e Engine) compose(c cell.Cell, raw []byte, id string, info TurnInfo) error {
	if err := c.RestoreMeta(); err != nil {
		return err
	}
	if err := writeFile(rel(c, ReceiptsDir+"/"+id+".json"), raw); err != nil {
		return err
	}
	if err := writeBlobs(c, info); err != nil {
		return err
	}
	commits.mark(c.Root, rel(c, ReceiptsDir+"/"+id+".json"), true)
	commits.mark(c.Root, rel(c, cell.TranscriptPath), false)
	return e.composeExtra(c)
}

// composeExtra runs the engine's Composer. A cell that seals its own folder has
// nothing outside the tree to carry: what would be composed already sits in it.
func (e Engine) composeExtra(c cell.Cell) error {
	if e.Compose == nil || e.cellDir(c) == "" {
		return nil
	}
	return e.Compose.Compose(c)
}

func writeBlobs(c cell.Cell, info TurnInfo) error {
	for _, e := range info.Calls {
		for _, out := range [][]byte{e.Stdout, e.Stderr} {
			if len(out) == 0 || len(out) > maxBlob {
				continue
			}
			path := rel(c, BlobsDir+"/"+hashHex(out))
			if err := writeFile(path, out); err != nil {
				return err
			}
			commits.mark(c.Root, path, true)
		}
	}
	return nil
}

// transcriptRange is what the transcript gained since the last seal. A cell
// with no transcript yet has an empty range at zero.
func transcriptRange(c cell.Cell, last *Sealed) Range {
	var start, end int64
	if last != nil {
		start = last.Receipt.Transcript.End
	}
	if info, err := os.Stat(rel(c, cell.TranscriptPath)); err == nil {
		end = info.Size()
	}
	// A transcript shorter than the last seal saw was rewound: what it gained
	// is counted from where it now ends.
	return Range{Start: min(start, end), End: end}
}

// appendTurn adds the turn to the cell's chain, durably.
func appendTurn(c cell.Cell, t Turn) error {
	line, err := json.Marshal(t)
	if err != nil {
		return err
	}
	path := rel(c, TurnsPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := repairTurnTail(path); err != nil {
		return err
	}
	_, before := os.Stat(path)
	created := errors.Is(before, fs.ErrNotExist)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	err = f.Close()
	commits.mark(c.Root, path, created)
	return err
}

// Head is the newest sealed turn of the cell and its receipt, or nil for a
// cell that was never sealed.
func Head(c cell.Cell) (*Sealed, error) {
	t, err := lastTurn(rel(c, TurnsPath))
	if err != nil {
		return nil, err
	}
	if a, ok := currentAdoption(c); ok {
		sealed := a.sealed(t)
		return &sealed, nil
	}
	if t == nil {
		return nil, nil
	}
	r, err := readReceipt(c, t.Receipt)
	if err != nil {
		return nil, err
	}
	return &Sealed{Turn: *t, Receipt: r}, nil
}

func readReceipt(c cell.Cell, id string) (Receipt, error) {
	raw, err := os.ReadFile(rel(c, ReceiptsDir+"/"+id+".json"))
	if err != nil {
		return Receipt{}, err
	}
	var r Receipt
	return r, json.Unmarshal(raw, &r)
}

// lastTurn reads the final line of turns.jsonl without reading the rest.
func lastTurn(path string) (*Turn, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tail, err := readTail(f)
	if err != nil {
		return nil, err
	}
	// A power cut can tear the last append even when its newline survived.
	// Only complete readable turns were published; keep the preceding one (C28).
	end := bytes.LastIndexByte(tail, '\n')
	for end >= 0 {
		start := bytes.LastIndexByte(tail[:end], '\n') + 1
		var t Turn
		if err := json.Unmarshal(tail[start:end], &t); err == nil {
			return &t, nil
		}
		end = start - 1
	}
	return nil, nil
}

func readTail(f *os.File) ([]byte, error) {
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	from := max(0, size-tailBytes)
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(f)
}

// repairTurnTail keeps the ordinary append bounded to the last few turns.
// Only a torn tail needs the full recovery read; long chats must not reread
// their whole history on every command just to handle a possible power cut.
func repairTurnTail(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	tail, err := readTail(f)
	_ = f.Close()
	if err != nil {
		return err
	}
	if len(tail) == 0 {
		return nil
	}
	if tail[len(tail)-1] == '\n' {
		start := bytes.LastIndexByte(tail[:len(tail)-1], '\n') + 1
		var turn Turn
		if json.Unmarshal(tail[start:len(tail)-1], &turn) == nil {
			return nil
		}
	}
	return repairLogTail(path)
}

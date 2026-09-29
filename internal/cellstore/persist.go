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
func compose(c cell.Cell, raw []byte, id string, info TurnInfo) error {
	if err := c.RestoreMeta(); err != nil {
		return err
	}
	if err := writeFile(rel(c, ReceiptsDir+"/"+id+".json"), raw); err != nil {
		return err
	}
	return writeBlobs(c, info)
}

func writeBlobs(c cell.Cell, info TurnInfo) error {
	for _, e := range info.Calls {
		for _, out := range [][]byte{e.Stdout, e.Stderr} {
			if len(out) == 0 || len(out) > maxBlob {
				continue
			}
			if err := writeFile(rel(c, BlobsDir+"/"+hashHex(out)), out); err != nil {
				return err
			}
		}
	}
	return nil
}

// transcriptRange is what the transcript gained since the last seal.
func transcriptRange(c cell.Cell, last *Sealed) Range {
	var start int64
	if last != nil {
		start = last.Receipt.Transcript.End
	}
	end := start
	if info, err := os.Stat(rel(c, cell.TranscriptPath)); err == nil {
		end = info.Size()
	}
	return Range{Start: start, End: max(start, end)}
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
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Head is the newest sealed turn of the cell and its receipt, or nil for a
// cell that was never sealed.
func Head(c cell.Cell) (*Sealed, error) {
	t, err := lastTurn(rel(c, TurnsPath))
	if err != nil || t == nil {
		return nil, err
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
	line := bytes.TrimSpace(tail[bytes.LastIndexByte(bytes.TrimRight(tail, "\n"), '\n')+1:])
	if len(line) == 0 {
		return nil, nil
	}
	var t Turn
	return &t, json.Unmarshal(line, &t)
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

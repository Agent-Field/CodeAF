package cellstore

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// rewindTool is the tool name the rewind turn's receipt row carries.
const rewindTool = "cell.rewind"

// IncompleteError refuses a rewind while calls are unresolved (L9): an
// incomplete call may have half-run, and restoring underneath it would hide
// what it did.
type IncompleteError struct{ Intents []Intent }

func (e IncompleteError) Error() string {
	tools := make([]string, len(e.Intents))
	for i, in := range e.Intents {
		tools[i] = in.Tool
	}
	return fmt.Sprintf("rewind refused: %d call(s) began and never finished (%s); resolve them first",
		len(tools), strings.Join(tools, ", "))
}

// WALPath is the device-local call-intent log of the cell.
func (e Engine) WALPath(c cell.Cell) string { return filepath.Join(e.LocalDir(c), "wal.jsonl") }

// Rewind restores the cell's files and transcript to the turn ref names, then
// seals a NEW turn whose parent is the current head and whose content is the
// target's. Nothing in the chain is removed (L12); a rewind can itself be
// rewound.
func (e Engine) Rewind(ctx context.Context, c cell.Cell, ref string) (Sealed, error) {
	if err := e.refuseIncomplete(c); err != nil {
		return Sealed{}, err
	}
	turns, err := Turns(c)
	if err != nil {
		return Sealed{}, err
	}
	target, err := Resolve(turns, ref)
	if err != nil {
		return Sealed{}, err
	}
	kept, err := readHistory(c)
	if err != nil {
		return Sealed{}, fmt.Errorf("rewind: read history: %w", err)
	}
	if err := e.Restore(ctx, c, target.ID, nil); err != nil {
		return Sealed{}, fmt.Errorf("rewind: %w", err)
	}
	if err := kept.restore(c); err != nil {
		return Sealed{}, fmt.Errorf("rewind: keep history: %w", err)
	}
	return e.Seal(ctx, c, TurnInfo{Calls: []Executed{e.rewindCall(target)}})
}

func (e Engine) refuseIncomplete(c cell.Cell) error {
	_, rec, err := OpenWAL(e.WALPath(c))
	if err != nil {
		return err
	}
	if len(rec.Incomplete) > 0 {
		return IncompleteError{Intents: rec.Incomplete}
	}
	return nil
}

// rewindCall is the receipt row that says why this turn exists.
func (e Engine) rewindCall(target Turn) Executed {
	now := e.now().UnixMilli()
	return Executed{Exact: true, Call: Call{
		Tool: rewindTool, ArgsHash: hashHex([]byte(target.ID)), Started: now, Ended: now,
		StdoutHash: hashHex(nil), StderrHash: hashHex(nil), SideEffect: "local",
	}}
}

// history is the chain's own files, by cell-relative path. They describe the
// past rather than belong to any turn's content, so a restore must not roll
// them back with the tree.
type history map[string][]byte

func readHistory(c cell.Cell) (history, error) {
	h := history{}
	for _, root := range []string{ReceiptsDir, BlobsDir} {
		if err := h.readTree(c, root); err != nil {
			return nil, err
		}
	}
	return h, h.readFile(c, TurnsPath)
}

func (h history) readFile(c cell.Cell, p string) error {
	raw, err := os.ReadFile(rel(c, p))
	h[p] = raw
	return err
}

func (h history) readTree(c cell.Cell, root string) error {
	err := filepath.WalkDir(rel(c, root), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		r, _ := filepath.Rel(c.Root, path)
		return h.readFile(c, filepath.ToSlash(r))
	})
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (h history) restore(c cell.Cell) error {
	for p, raw := range h {
		if err := writeFile(rel(c, p), raw); err != nil {
			return err
		}
	}
	return nil
}

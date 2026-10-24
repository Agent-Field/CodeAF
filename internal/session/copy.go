package session

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/store"
)

// PrepareCopy carries the older chat's dials, task journals and memories into
// its new cell. All mutations are confined to the copy (C29.1).
func PrepareCopy(ctx context.Context, original string, c cell.Cell, brain *store.Store) error {
	for _, name := range append(append([]string{placeTranscript, cell.TranscriptPath}, truthNames...), placeMeta) {
		info, err := os.Lstat(filepath.Join(c.Root, name))
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("older chat's state is a link: %s", name)
		}
	}
	for _, carrier := range TruthCarriers() {
		if err := carrier.Stage(c.Root, c); err != nil {
			return err
		}
		if err := carrier.Clear(c.Root); err != nil {
			return err
		}
	}
	meta, err := LoadMeta(c.Root)
	if err != nil {
		return err
	}
	oldID := filepath.Base(original)
	if err := copyHeader(c); err != nil {
		return err
	}
	meta.ID = c.ID
	if meta.Owned {
		meta.Workspace = filepath.Join(c.Root, "work")
	}
	if err := SaveMeta(c.Root, meta); err != nil {
		return err
	}
	if brain != nil {
		events, err := brain.MemoryEventsOf(oldID)
		if err != nil {
			return err
		}
		// Existing carried memory is truth too, even when the local index lost it.
		held, err := ReadMemoryEvents(c.Root)
		if err != nil {
			return err
		}
		events = append(held, events...)
		// The old identity remains in provenance; the copy holds its whole history.
		if err := writeMemoryLedger(memoryLedgerPath(c.Root), events); err != nil {
			return err
		}
	}
	// Presence describes a process on the original, never the new copy.
	_ = os.Remove(filepath.Join(c.Root, "presence.json"))
	return ctx.Err()
}

func copyHeader(c cell.Cell) error {
	path := filepath.Join(c.Root, cell.TranscriptPath)
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	reader := bufio.NewReader(in)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		in.Close()
		return err
	}
	var header map[string]json.RawMessage
	if err := json.Unmarshal(line, &header); err != nil {
		in.Close()
		return err
	}
	header["id"], _ = json.Marshal(c.ID)
	line, err = json.Marshal(header)
	if err != nil {
		in.Close()
		return err
	}
	out, err := os.CreateTemp(filepath.Dir(path), ".copy-journal-*")
	if err != nil {
		in.Close()
		return err
	}
	defer os.Remove(out.Name())
	if _, err = out.Write(append(line, '\n')); err == nil {
		_, err = io.Copy(out, reader)
	}
	if err == nil {
		err = out.Sync()
	}
	_ = in.Close()
	closeErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(out.Name(), path)
}

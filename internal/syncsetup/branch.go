package syncsetup

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"io"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/session"
)

// branchIdentity gives a restored branch its own conversation identity. Only
// the journal's header changes; every turn and saved tool result stays intact.
// This runs before derived indexes are built and before any window opens it.
func branchIdentity(_ context.Context, c cell.Cell) (err error) {
	path := transcriptOfBranch(c)
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	reader := bufio.NewReader(in)
	line, err := reader.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	var header map[string]json.RawMessage
	if err := json.Unmarshal(line, &header); err != nil {
		return err
	}
	if string(header["type"]) != `"session"` {
		return errors.New("the kept chat has no conversation header")
	}
	id, _ := json.Marshal(c.ID)
	if string(header["id"]) == string(id) {
		return branchRow(c)
	}
	header["id"] = id
	line, err = json.Marshal(header)
	if err != nil {
		return err
	}
	out, err := os.CreateTemp(filepath.Dir(path), ".branch-journal-*")
	if err != nil {
		return err
	}
	defer func() { out.Close(); os.Remove(out.Name()) }()
	if _, err = out.Write(append(line, '\n')); err != nil {
		return err
	}
	if _, err = io.Copy(out, reader); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	// K3: NTFS cannot replace the journal while this reader still holds it.
	if err := in.Close(); err != nil {
		return err
	}
	if err := fsreplace.Rename(out.Name(), path); err != nil {
		return err
	}
	return branchRow(c)
}

func transcriptOfBranch(c cell.Cell) string { return session.Place{Dir: c.Root}.Transcript() }

// branchRow also corrects an existing derived row: a zero-cost transcript
// need not make an old row fall behind, so index refresh alone cannot rename it.
func branchRow(c cell.Cell) error {
	m, err := session.LoadMeta(c.Root)
	if errors.Is(err, os.ErrNotExist) || (err == nil && m.ID == "") {
		return nil
	}
	if err != nil {
		return err
	}
	m.ID, m.Owned = c.ID, true
	m.Workspace = (session.Place{Dir: c.Root, Owned: true}).Work()
	return session.SaveMeta(c.Root, m)
}

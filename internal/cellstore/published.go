package cellstore

import (
	"encoding/json"
	"errors"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// publishedPath keeps the upload boundary separate from the newest local seal:
// a clean working tree can still hold turns no other device has received.
const publishedPath = ".cell/published.json"

// KnowsHead says whether head is in this tree's sealed ancestry. An adopted
// head has no line in the carried turn log, so its note counts even after the
// next local seal has extended it.
func KnowsHead(c cell.Cell, head string) (bool, error) {
	if head == "" {
		return true, nil
	}
	if adopted, ok := readAdoption(c); ok && adopted.Head == head {
		return true, nil
	}
	turns, err := Turns(c)
	for _, turn := range turns {
		if turn.ID == head {
			return true, err
		}
	}
	return false, err
}

// NotePublished records a head that the directory accepted or this device
// received, so a later take can distinguish uploaded history from local work.
func NotePublished(c cell.Cell, head string) error {
	raw, err := json.Marshal(struct {
		Head string `json:"head"`
	}{head})
	if err != nil {
		return err
	}
	p := rel(c, publishedPath)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(p+".tmp", raw, 0o600); err != nil {
		return err
	}
	return fsreplace.Rename(p+".tmp", p)
}

// Unpublished names the sealed local work beyond the uploaded head. An older
// installation without an upload note uses its last adoption, then the remote
// head; an unfamiliar boundary conservatively keeps the local history.
func Unpublished(c cell.Cell, remoteHead string) (string, uint32, error) {
	head, err := Head(c)
	if err != nil || head == nil || head.Turn.ID == remoteHead {
		return "", 0, err
	}
	base := remoteHead
	var published struct {
		Head string `json:"head"`
	}
	raw, err := os.ReadFile(rel(c, publishedPath))
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &published); err != nil {
			return "", 0, err
		}
		base = published.Head
	case errors.Is(err, os.ErrNotExist):
		if adopted, ok := readAdoption(c); ok {
			base = adopted.Head
		}
	default:
		return "", 0, err
	}
	if head.Turn.ID == base {
		return "", 0, nil
	}
	turns, err := Turns(c)
	if err != nil {
		return "", 0, err
	}
	var n uint32
	for i := len(turns) - 1; i >= 0; i-- {
		turn := turns[i]
		if turn.ID == base {
			break
		}
		n++
		// An adopted head has no line in this log. Its first local child
		// still names that boundary, so earlier history is already uploaded.
		if turn.Parent == base {
			break
		}
	}
	return head.Turn.ID, max(1, n), nil
}

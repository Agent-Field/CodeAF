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

// Shared reports whether a chat has ever crossed a publication or adoption boundary.
// Missing records mean a local chat; unreadable records cannot grant permission.
func Shared(c cell.Cell) (bool, error) {
	for _, path := range []string{publishedPath, adoptedPath} {
		if _, err := os.Stat(rel(c, path)); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	return false, nil
}

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

// Extends says whether this computer's newest turn is head or was sealed on
// top of it. A computer whose own turns did not reach the service (it ran
// unsynced, or its last upload died with it) still owns that work: publishing
// it continues the record's history instead of replacing it. An adopted head
// has no line in the log, but its first local child names it as the parent.
func Extends(c cell.Cell, head string) (bool, error) {
	local, err := Head(c)
	if err != nil || local == nil {
		return head == "", err
	}
	turns, err := Turns(c)
	if err != nil {
		return false, err
	}
	parent := make(map[string]string, len(turns))
	for _, t := range turns {
		parent[t.ID] = t.Parent
	}
	for at, seen := local.Turn.ID, 0; seen <= len(turns); seen++ {
		if at == head {
			return true, nil
		}
		next, ok := parent[at]
		if !ok || next == "" {
			return false, nil
		}
		at = next
	}
	return false, nil
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
	span := head.Receipt.Transcript
	for i := len(turns) - 1; i >= 0; i-- {
		turn := turns[i]
		if turn.ID == base {
			break
		}
		receipt, err := readReceipt(c, turn.Receipt)
		if err != nil {
			return "", 0, err
		}
		span.Start = min(span.Start, receipt.Transcript.Start)
		// An adopted head has no line in this log. Its first local child
		// still names that boundary, so earlier history is already uploaded.
		if turn.Parent == base {
			break
		}
	}
	n, err := unpublishedConversationTurns(c, span)
	return head.Turn.ID, max(1, n), err
}

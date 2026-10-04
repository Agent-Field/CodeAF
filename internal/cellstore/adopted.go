package cellstore

import (
	"encoding/json"
	"os"
	"path"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// adoptedPath is where a device notes the head a materialize adopted.
var adoptedPath = path.Join(".cell", "adopted.json")

// ADOPTED HEAD. A chat that arrives from another machine is restored to a head
// whose own turn line is not in the log it carried: the seal appends the line
// after it snapshots, so the snapshot cannot hold it. The engine adopts that
// head as the workspace's head (its ref log is the truth), and this file is the
// derived note of it that the Go side reads without spawning anything. It is
// NOT a turn record and nothing is ever appended to turns.jsonl for it: it says
// which snapshot the tree stands at, and how big turns.jsonl and the
// transcript were when it did.
//
// It counts only while turns.jsonl is still the size it had then. The next seal
// appends a line, which makes the note stale, and the log speaks for itself
// again.
type adoption struct {
	V             uint16 `json:"V"`
	Head          string `json:"head"`
	TurnsSize     int64  `json:"turns_size"`
	TranscriptEnd int64  `json:"transcript_end"`
	From          string `json:"from,omitempty"` // the device the chat was sealed on, by name
}

// noteAdopted records that c's tree now stands at head, as materialize left it.
func noteAdopted(c cell.Cell, head string) error {
	return writeAdoption(c, adoption{V: schemaV, Head: head})
}

func writeAdoption(c cell.Cell, a adoption) error {
	a.TurnsSize, a.TranscriptEnd = sizeOf(rel(c, TurnsPath)), sizeOf(rel(c, cell.TranscriptPath))
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return writeFile(rel(c, adoptedPath), append(raw, '\n'))
}

// AdoptedFrom says which device the chat that c adopted was sealed on. It
// changes nothing when c has no adopted head.
func AdoptedFrom(c cell.Cell, device string) error {
	a, ok := readAdoption(c)
	if !ok {
		return nil
	}
	a.From = device
	return writeAdoption(c, a)
}

func readAdoption(c cell.Cell) (adoption, bool) {
	raw, err := os.ReadFile(rel(c, adoptedPath))
	var a adoption
	if err != nil || json.Unmarshal(raw, &a) != nil || a.Head == "" {
		return adoption{}, false
	}
	return a, true
}

// currentAdoption is the adopted head when nothing has been sealed since.
func currentAdoption(c cell.Cell) (adoption, bool) {
	a, ok := readAdoption(c)
	if !ok || sizeOf(rel(c, TurnsPath)) != a.TurnsSize {
		return adoption{}, false
	}
	return a, true
}

// sealed is the adopted head as a head: a turn with no receipt, whose parent is
// the newest line of the log the chat carried.
func (a adoption) sealed(last *Turn) Sealed {
	parent := ""
	if last != nil {
		parent = last.ID
	}
	t := newTurn(a.Head, parent, time.UnixMilli(0), TurnInfo{}, "", Identity{})
	return Sealed{Turn: t, Receipt: Receipt{V: schemaV, Transcript: Range{Start: a.TranscriptEnd, End: a.TranscriptEnd}}}
}

// Adopted says which device sealed the head c adopted, "" when it did not say,
// and false when the log's own last turn is the head.
func Adopted(c cell.Cell) (device string, ok bool) {
	a, ok := currentAdoption(c)
	return a.From, ok
}

func sizeOf(p string) int64 {
	info, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return info.Size()
}

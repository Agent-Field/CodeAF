package cellstore

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// Entry is one turn of the chain with its receipt.
type Entry struct {
	Turn    Turn
	Receipt Receipt
	// Adopted is set on the entry that stands for a head this device received
	// rather than sealed: the name of the device that sealed it, "" when the
	// chat did not say. Such an entry has no receipt.
	Adopted   string
	IsAdopted bool
}

// Turns reads the cell's whole chain, oldest first. A cell that was never
// sealed has an empty chain.
func Turns(c cell.Cell) ([]Turn, error) {
	raw, err := os.ReadFile(rel(c, TurnsPath))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var turns []Turn
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(nil, len(raw)+1)
	for sc.Scan() {
		var t Turn
		if err := json.Unmarshal(sc.Bytes(), &t); err != nil {
			return nil, fmt.Errorf("read turns: %w", err)
		}
		turns = append(turns, t)
	}
	return turns, sc.Err()
}

// Log is the chain with receipts, newest first.
func Log(c cell.Cell) ([]Entry, error) {
	turns, err := Turns(c)
	if err != nil {
		return nil, err
	}
	slices.Reverse(turns)
	entries := make([]Entry, 0, len(turns)+1)
	if from, ok := Adopted(c); ok {
		head, _ := Head(c)
		entries = append(entries, Entry{Turn: head.Turn, Adopted: from, IsAdopted: true})
	}
	for _, t := range turns {
		r, err := readReceipt(c, t.Receipt)
		if err != nil {
			return nil, fmt.Errorf("turn %s: %w", t.ID, err)
		}
		entries = append(entries, Entry{Turn: t, Receipt: r})
	}
	return entries, nil
}

// Resolve finds the one turn whose id starts with ref.
func Resolve(turns []Turn, ref string) (found Turn, err error) {
	if ref == "" {
		return found, errors.New("name a turn")
	}
	var hits []Turn
	for _, t := range turns {
		if strings.HasPrefix(t.ID, ref) {
			hits = append(hits, t)
		}
	}
	if len(hits) != 1 {
		return found, fmt.Errorf("turn %q: %d matches in this cell", ref, len(hits))
	}
	return hits[0], nil
}

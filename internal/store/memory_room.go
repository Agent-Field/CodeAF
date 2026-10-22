package store

import (
	"database/sql"
	"errors"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MemoryBlockRunes is a soft prompt-cache budget, never a storage limit.
// Explicit pins bypass it. AssembleMemoryBlock explains every admission.
const MemoryBlockRunes = 4800

// MemoryQuote is one line's words as the block carries them: quoted, with the
// block's own markers escaped, so a line's words can never close the block or
// open a second one. The journal keeps the raw bytes; only the projection
// escapes.
func MemoryQuote(text string) string {
	q := strconv.Quote(text)
	q = strings.ReplaceAll(q, "<", "\\u003c")
	q = strings.ReplaceAll(q, ">", "\\u003e")
	return q
}

// MemoryLineCost is what one line spends of [MemoryBlockRunes]: the line as the
// block renders it, `- ` and the quoted words and the newline.
func MemoryLineCost(text string) int {
	return utf8.RuneCountInString("- "+MemoryQuote(text)) + 1
}

// ErrMemoryFull is a change some conversation's block has no room for. It is
// wrapped by [MemoryFullError], which names the block.
var ErrMemoryFull = errors.New("memory is full")

// ErrMemoryKept is a line put back, or reworded, onto words its owner already
// keeps active: two lines saying one thing would both ride, so the change is
// refused and the line already kept stands.
var ErrMemoryKept = errors.New("those words are already kept there")

// MemoryFullError says which conversations' block a change would overflow.
type MemoryFullError struct {
	// Owner is the project owner whose conversations would be overflowed, or ""
	// when the person's and this machine's lines alone would overflow — which
	// is every conversation's block.
	Owner string
	// Project is that project's name, or "" when none of its lines carries one.
	Project string
}

// Error is the refusal as words. Over --host it crosses the wire as only this
// text, so it carries the project's name in the one spelling a surface reads
// back (session's MemoryFullWords): "memory is full for <name>".
func (e *MemoryFullError) Error() string {
	if e.Project != "" {
		return ErrMemoryFull.Error() + " for " + e.Project
	}
	return ErrMemoryFull.Error()
}

func (e *MemoryFullError) Unwrap() error { return ErrMemoryFull }

// memoryRoomOn answers whether owner can hold one more active line of text —
// or text in place of the line exclude — without any block that owner reaches
// going past [MemoryBlockRunes].
//
// WHICH BLOCKS AN OWNER REACHES: a line for the person or for this machine is
// in every conversation's block, so it is measured against the person's lines,
// the machine's lines and the fullest project's; a project line is in its own
// project's block only. The quarantine reaches no block — no conversation is
// told those lines — so it is never refused.
func memoryRoomOn(tx *sql.Tx, owner, text, exclude string) error {
	err := memoryRoomAnswer(tx, owner, text, exclude)
	if memoryRoomRead != nil {
		memoryRoomRead(text, err)
	}
	return err
}

// memoryRoomRead, when a test sets it, is told each room check's answer from
// inside the write transaction that asked — the seam that holds one writer
// there while a second one tries (memory_room_test.go).
var memoryRoomRead func(text string, err error)

func memoryRoomAnswer(tx *sql.Tx, owner, text, exclude string) error {
	// Storage is unbounded; the prompt budget is enforced at assembly only.
	return nil
}

// memoryDuplicateOn is the active line in owner with the same words, other
// than exclude, or "" — the one spelling of "already kept there", asked of the
// TARGET owner only: the same words held for another owner are another line.
func memoryDuplicateOn(tx *sql.Tx, owner, text, exclude string) (string, error) {
	rows, err := tx.Query(`SELECT id, text FROM memories WHERE owner = ? AND status = ?`, normalizeOwner(owner), MemoryActive)
	if err != nil {
		return "", err
	}
	want, found := NormalizeMemoryWords(text), ""
	for rows.Next() {
		var id, body string
		if err := rows.Scan(&id, &body); err != nil {
			rows.Close()
			return "", err
		}
		if id != exclude && found == "" && NormalizeMemoryWords(body) == want {
			found = id
		}
	}
	return found, closeRows(rows)
}

// FitMemories is which of one block's lines ride, and how many are left out.
// Lines that fit together all ride, in the order they came in. Lines that do
// not — only a store an earlier build filled with no bound can hold them — are
// chosen in this order, newest first within each: what an earlier build kept
// as a rule (its `always` flag, [Memory.Rule]), then the project's lines, then
// this machine's, then the person's everywhere. The project's come before the
// person's because the person's lines are the ones every other project carries
// too, and a project block filled with them would lose the one thing only it
// says. The chosen lines go back out IN THE ORDER THEY CAME IN, so the block
// still renders byte for byte until a line changes.
func FitMemories(memories []Memory) ([]Memory, int) {
	total := 0
	for _, memory := range memories {
		total += MemoryLineCost(memory.Text)
	}
	if total <= MemoryBlockRunes {
		return memories, 0
	}
	order := make([]int, len(memories))
	for i := range order {
		order[i] = i
	}
	rank := func(memory Memory) int {
		switch {
		case memory.Rule:
			return 0
		case OwnerKindOf(memory.Owner) == OwnerKindProject:
			return 1
		case memory.Owner == OwnerMachine:
			return 2
		}
		return 3
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := memories[order[i]], memories[order[j]]
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra < rb
		}
		return a.CreatedSeq > b.CreatedSeq
	})
	keep := make([]bool, len(memories))
	used := 0
	for _, at := range order {
		if cost := MemoryLineCost(memories[at].Text); used+cost <= MemoryBlockRunes {
			keep[at], used = true, used+cost
		}
	}
	fit := make([]Memory, 0, len(memories))
	for at, memory := range memories {
		if keep[at] {
			fit = append(fit, memory)
		}
	}
	return fit, len(memories) - len(fit)
}

package store

import (
	"database/sql"
	"errors"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// THE BOUND EVERY CONVERSATION'S MEMORY BLOCK IS HELD TO, AND THE ONE PLACE IT IS
// ENFORCED.
//
// A conversation is told every active line its owners hold — the person's,
// this machine's and its project's — in one block in message[0]
// (internal/session's memory.go). That block has a size, and a line that does
// not fit in it is a line the model is not told. So the size is checked where a
// line becomes active, INSIDE THE WRITE TRANSACTION THAT MAKES IT ACTIVE: a
// check made before the transaction is a check two writers both pass, and a
// check made only against the writing conversation's own owners is a check a
// line kept everywhere from project A passes while it pushes project B past
// the bound. Every door that makes a line active — a new line, a replacement,
// a reworded line, a line moved, a line put back — asks [memoryRoomOn].
//
// What can still exceed the bound is a store an earlier build filled with no
// bound at all. [FitMemories] decides which of those lines ride, and the
// conversation and the Memories tab say how many do not.

// MemoryBlockRunes bounds the memory lines one block carries: about twelve
// hundred tokens at four characters a token, which is fifty-odd ordinary lines.
// It is paid on every request, in the cached prefix, so it is a bound on the
// bill as much as on the page. PERF.md records it; changing it changes that
// section in the same commit.
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
	owner = normalizeOwner(owner)
	if owner == OwnerLegacyProject {
		return nil
	}
	rows, err := tx.Query(`SELECT id, owner, text, project_name FROM memories WHERE status = ?`, MemoryActive)
	if err != nil {
		return err
	}
	costs, names := map[string]int{}, map[string]string{}
	for rows.Next() {
		var id, rowOwner, body, name string
		if err := rows.Scan(&id, &rowOwner, &body, &name); err != nil {
			rows.Close()
			return err
		}
		if id == exclude {
			continue
		}
		costs[rowOwner] += MemoryLineCost(body)
		if name = strings.TrimSpace(name); name != "" {
			names[rowOwner] = name
		}
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	base := costs[OwnerUser] + costs[OwnerMachine]
	add := MemoryLineCost(text)
	switch OwnerKindOf(owner) {
	case OwnerKindProject:
		if base+costs[owner]+add > MemoryBlockRunes {
			return &MemoryFullError{Owner: owner, Project: names[owner]}
		}
	default:
		worst, worstOwner := 0, ""
		for other, cost := range costs {
			if OwnerKindOf(other) == OwnerKindProject && other != OwnerLegacyProject && cost > worst {
				worst, worstOwner = cost, other
			}
		}
		if base+worst+add > MemoryBlockRunes {
			return &MemoryFullError{Owner: worstOwner, Project: names[worstOwner]}
		}
	}
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

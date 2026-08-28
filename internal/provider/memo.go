package provider

import (
	"encoding/json"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// What a client already knows its transcript and its tool block serialize to.
//
// A tool loop re-sends its whole transcript every turn. caching.go treats that
// as a billing problem; this treats the other half of it, which is a CPU one:
// the bytes going out on turn N+1 are the bytes of turn N with a few messages
// appended, and re-deriving every one of them costs about a millisecond and
// half a megabyte of garbage per call at eighty turns. It is paid again by the
// repair retry (client.go) and once more per rung of the relax ladder
// (endpoints.go), both of which encode the same transcript a second and a third
// time to change one field of it.
//
// ── WHAT MAKES THIS SOUND ──
//
// A RECORDED MESSAGE NEVER CHANGES. internal/session/agent.go states that where
// it hands the transcript over: the slice is copied per request and its elements
// are immutable once recorded. So an entry may be trusted whenever the message
// it came from still compares equal — and that comparison is very nearly free,
// because the strings are the SAME strings header for header and Go's string
// equality answers on the pointer without reading a byte.
//
// The memo therefore VERIFIES RATHER THAN ASSUMES. A compaction that rewrites
// the head, a relax rung that drops attachments, a second conversation on one
// client: each of them simply misses and re-encodes, which is slower than a hit
// and never wrong.
//
// ── WHAT IS NOT MEMOIZED, AND WHY ──
//
// The BREAKPOINTS. A marked message serializes to something else entirely, and
// the tail marker rolls forward every single turn — a cache invalidated as the
// marker moved would be a cache of the one thing that changes. What is kept is
// the breakpoint-free encoding of every message, and the two marked positions
// are re-derived per call: two marshals rather than the whole transcript's.
//
// The cost of all this is one encoded copy of the transcript held per client,
// which is about the size of one request body. That is the trade, stated plainly
// so nobody has to measure to find it.
type encodeMemo struct {
	mu      sync.Mutex
	entries []memoEntry
	tools   memoizedTools
}

// memoEntry is one message and its breakpoint-free encoding. The message is
// kept beside the bytes because it is the only thing that can vouch for them.
type memoEntry struct {
	message ai.Message
	encoded json.RawMessage
}

// encodeMessages is [encodeMessages] with the unchanged prefix taken off the
// bill. It returns the same bytes in the same order, always.
func (m *encodeMemo) encodeMessages(messages []ai.Message, dialect cacheDialect) ([]json.RawMessage, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	// A CONTENDED MEMO IS WORTH LESS THAN THE PARALLELISM IT WOULD COST. Two
	// leaves of one fan-out share a client and do not share a transcript, so a
	// blocking memo would serialize their encodes in exchange for nothing. The
	// second caller encodes directly instead, which is exactly what it paid
	// before this file existed.
	if !m.mu.TryLock() {
		return encodeMessages(messages, dialect)
	}
	defer m.mu.Unlock()

	placed := breakpoints{system: -1, tail: -1}
	if dialect == cacheDialectBreakpoints {
		placed = breakpointsFor(messages)
	}
	m.resize(len(messages))
	encoded := make([]json.RawMessage, len(messages))
	for index, message := range messages {
		entry := &m.entries[index]
		if !sameMessage(entry.message, message) {
			entry.message, entry.encoded = message, nil
		}
		if index == placed.system || index == placed.tail {
			raw, err := marshalMarked(message)
			if err != nil {
				return nil, err
			}
			encoded[index] = raw
			continue
		}
		if entry.encoded == nil {
			raw, err := json.Marshal(message)
			if err != nil {
				return nil, err
			}
			entry.encoded = raw
		}
		encoded[index] = entry.encoded
	}
	return encoded, nil
}

// resize brings the entry table to the transcript's length, zeroing whatever it
// grows into so a shrunk-then-regrown table cannot hand back a message's old
// neighbour as its own.
func (m *encodeMemo) resize(length int) {
	switch {
	case len(m.entries) > length:
		m.entries = m.entries[:length]
	case len(m.entries) < length:
		m.entries = append(m.entries, make([]memoEntry, length-len(m.entries))...)
	}
}

// sameMessage reports that two messages are the same recorded message. It is a
// field-by-field comparison rather than a hash because it has to be exact: a
// collision here would put stale bytes on the wire, and the whole point of the
// memo is that the wire cannot tell it is there.
func sameMessage(a, b ai.Message) bool {
	if a.Role != b.Role || a.ToolCallID != b.ToolCallID ||
		len(a.Content) != len(b.Content) || len(a.ToolCalls) != len(b.ToolCalls) {
		return false
	}
	for index := range a.Content {
		// Two equal attachments behind two different pointers compare unequal
		// and cost a re-encode. That is the conservative direction, and an
		// attachment is re-sent from the same part it was recorded in anyway.
		if a.Content[index] != b.Content[index] {
			return false
		}
	}
	for index := range a.ToolCalls {
		if a.ToolCalls[index] != b.ToolCalls[index] {
			return false
		}
	}
	return true
}

// memoizedTools is the encoded tool block and the identity of the slice it came
// from.
//
// THE BELT IS APPEND-ONLY AND NOTHING ON IT MOVES — internal/session/connect.go
// states that law where it arms a family, and the prompt cache is what pays when
// it is broken. So a tool block is identified by where it lives and how long it
// is: the same backing array at the same length is the same definitions, and the
// memo's own pointer into that array is what keeps the address from being
// recycled underneath it. Arming a family appends, which either lengthens the
// slice or moves it, and both miss — as they should, since both are already a
// cache write on the wire.
type memoizedTools struct {
	first   *ai.ToolDefinition
	count   int
	dialect cacheDialect
	encoded []json.RawMessage
}

// encodeTools is [encodeTools] answered from memory when the belt has not
// changed. The returned slice is the memo's own and is READ-ONLY to its caller,
// which is the one thing wire.go does with it.
func (m *encodeMemo) encodeTools(tools []ai.ToolDefinition, dialect cacheDialect) ([]json.RawMessage, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	if !m.mu.TryLock() {
		return encodeTools(tools, dialect)
	}
	defer m.mu.Unlock()

	if m.tools.first == &tools[0] && m.tools.count == len(tools) && m.tools.dialect == dialect {
		return m.tools.encoded, nil
	}
	encoded, err := encodeTools(tools, dialect)
	if err != nil {
		return nil, err
	}
	m.tools = memoizedTools{first: &tools[0], count: len(tools), dialect: dialect, encoded: encoded}
	return encoded, nil
}

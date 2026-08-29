package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// The leaf transcript: what a worker actually did, turn by turn.
//
// THE DEFECT THIS ANSWERS. A headless `aforge do` leaf ran the bare executor
// for one node and billed 1,292,313 prompt tokens, 14,316 completion tokens and
// fifty cents against it. It left no file on disk and no record of a single
// turn: internal/exec/bare's loop kept its messages in memory and dropped them
// when the process exited, so the only rows the graph held under that node were
// the harness's own progress lines — "reading the request", "starting the
// work". A run that cannot be read back is a run nobody can learn from, and
// half a dollar is not a rounding error.
//
// WHY THIS IS A SECOND TABLE AND NOT MORE ROWS IN messages. It is the same
// judgement usage_turns.go made about the usage table, for the same reason, and
// the reason is worth spelling out because "just post them as messages" is the
// obvious answer and it is wrong.
//
// The messages table is the CONVERSATION, and a message anchored to a node is
// read by half a dozen callers who each mean "what was said about this work":
// a running leaf's steering mailbox (cmd/aforge/chat.go), the pickup bank that
// reconstructs a re-claimed leaf's progress from its own record
// (internal/resident/bank.go, which pages the first 400 rows), the narrator,
// the missed-direction sweep (internal/resident/redirect.go), the node lens
// (internal/head/lens.go) and both TUIs' node trails. A leaf that emitted an
// assistant turn, three tool calls and three tool results per turn would put
// several hundred rows under its own node, and every one of those readers would
// have silently begun reading the worker's internal monologue as conversation —
// the bank's window filled with tool arguments, the trail a person opens
// rendering a file dump. None of them would have been changed to say so.
//
// So the conversation stays exactly what it was, byte for byte, and the shape
// underneath it lands here. The journal is still the truth: one event per flush
// carrying that flush's entries, projected into this table on write and on
// rebuild.
//
// WHY THIS IS NOT internal/exec/trace.go. That file is the OTHER recorder — a
// prose flight recorder the generalist writes to `.aforge/trace/<leaf>.trace.log`
// inside the job's workspace, with per-turn cache ratios a benchmark asserts on.
// It is the right tool for reading over a running leaf's shoulder and the wrong
// one for the failure above, for two reasons: the bare executor never wired
// itself to it, and — decisively — it lives in a directory that goes away with
// the job. A record that survives only while the scratch space does cannot
// answer "what did that node do last week", which is the question a $0.50 leaf
// leaves behind. The two are complements: the file is verbose and local, this
// is bounded and durable, and neither is a copy of the other.
//
// HOW TO READ ONE BACK. `aforge why <node-id> --db <path>` prints it. In SQL:
//
//	SELECT turn, kind, tool, body FROM transcript
//	WHERE node_id = ? ORDER BY seq, idx;
//
// seq orders the flushes (and therefore the attempts — a retried leaf appends a
// fresh run behind the failed one), idx orders the entries within a flush.

// TranscriptKind names one entry in a leaf's record. The four that carry the
// loop are assistant / tool_call / tool_result / fault; the two that carry the
// harness's own voice are note and elided.
type TranscriptKind string

const (
	// TranscriptAssistant is one model turn's text, as the model wrote it.
	TranscriptAssistant TranscriptKind = "assistant"
	// TranscriptToolCall is one tool the model asked for, with its arguments.
	// Tool and CallID are set; CallID ties it to its result.
	TranscriptToolCall TranscriptKind = "tool_call"
	// TranscriptToolResult is what that tool returned. Millis is how long it
	// took and Failed says the tool reported an error, which is the pair of
	// facts an autopsy asks for first.
	TranscriptToolResult TranscriptKind = "tool_result"
	// TranscriptFault is the loop ending on something other than its own
	// completion: a provider error the retries could not absorb, a deadline, a
	// panic caught by the guard. The text is the error as it was seen.
	TranscriptFault TranscriptKind = "fault"
	// TranscriptNote is the harness speaking about its own machinery — a
	// compaction pass, a retry budget spent. It is not the model's words and is
	// marked so nobody reads it as such.
	TranscriptNote TranscriptKind = "note"
	// TranscriptElided is the bound admitting itself. A run past the cap stops
	// being recorded, and this says so where the record stops rather than
	// leaving a reader to believe the loop ended there.
	TranscriptElided TranscriptKind = "elided"
)

// TranscriptEntry is one line of one leaf's record.
//
// Text is the payload for every kind and its meaning follows the kind: the
// assistant's prose, the tool call's arguments, the tool result's output, the
// fault's error. It is bounded — see MaxTranscriptTextBytes — because this is a
// record and not a content store; a tool that printed a megabyte is journaled
// as its head and its tail with the gap named.
type TranscriptEntry struct {
	// Turn is the model request this entry belongs to, numbered from one. Every
	// entry of a turn shares it, which is what lets a reader collapse a turn.
	Turn int            `json:"turn"`
	Kind TranscriptKind `json:"kind"`
	// Tool is the tool's name on tool_call and tool_result, empty elsewhere.
	Tool string `json:"tool,omitempty"`
	// CallID is the provider's own id for one call, carried on both halves so a
	// result can be matched to its call when a turn issued several in parallel
	// and they landed out of order.
	CallID string `json:"call_id,omitempty"`
	Text   string `json:"text,omitempty"`
	// Millis is wall time for a tool result. Zero everywhere else, and zero is
	// also the honest value for a tool that returned instantly.
	Millis int64 `json:"ms,omitempty"`
	// Failed says the tool reported an error. It is a separate fact from the
	// text because a tool that failed usefully and a tool that succeeded both
	// return prose, and only this tells them apart.
	Failed bool `json:"failed,omitempty"`
}

const (
	// MaxTranscriptTextBytes bounds one entry's payload. It is MaxDigestBytes
	// deliberately and not a new number: a transcript entry is a digest of one
	// step in exactly the sense a fold digest is a digest of one node, and the
	// question "how much of a thing is worth keeping so a later reader can tell
	// what happened" has one answer in this package, not two.
	MaxTranscriptTextBytes = MaxDigestBytes

	// MaxTranscriptBatch bounds one journal event's entry list, and therefore
	// one event's payload: at most this many entries of at most
	// MaxTranscriptTextBytes each. A recorder that batches larger than this is
	// refused rather than truncated, because silently dropping the tail of a
	// batch is exactly the kind of quiet loss this table exists to end.
	MaxTranscriptBatch = 64

	// MaxTranscriptEntries bounds one execution's whole record. A leaf that
	// runs away — and the leaf that prompted this table burned 1.29M prompt
	// tokens — must not turn the database into its own log file, so recording
	// stops here and says so with a TranscriptElided entry. The head of a run
	// is kept rather than its tail because what an autopsy needs first is what
	// the worker set out to do and where it started going wrong.
	MaxTranscriptEntries = 4000

	// MaxTranscriptBytes bounds one execution's record in bytes, for the run
	// whose entries are each large rather than many. Reached first or last,
	// either bound seals the record the same way.
	MaxTranscriptBytes = 1 << 20
)

const transcriptSchema = `
CREATE TABLE IF NOT EXISTS transcript (
    seq     INTEGER NOT NULL REFERENCES events(seq),
    idx     INTEGER NOT NULL,
    ts      TEXT NOT NULL,
    node_id TEXT NOT NULL,
    turn    INTEGER NOT NULL DEFAULT 0,
    kind    TEXT NOT NULL,
    tool    TEXT NOT NULL DEFAULT '',
    call_id TEXT NOT NULL DEFAULT '',
    body    TEXT NOT NULL DEFAULT '',
    millis  INTEGER NOT NULL DEFAULT 0,
    failed  INTEGER NOT NULL DEFAULT 0 CHECK (failed IN (0, 1)),
    model   TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (seq, idx)
);
CREATE INDEX IF NOT EXISTS transcript_node ON transcript (node_id, seq, idx);
`

// transcriptPayload is one flush, as journaled.
type transcriptPayload struct {
	NodeID  string            `json:"node_id"`
	Model   string            `json:"model,omitempty"`
	Entries []TranscriptEntry `json:"entries"`
}

// RecordTranscript appends one flush of one leaf's record to the journal.
//
// It is called repeatedly during a run rather than once at the end, and that is
// the point: a leaf can die mid-loop — a panic in a tool, a deadline, the
// process going away — and what it had already done must survive the way it
// happened. An empty batch is not an event.
//
// Bounds are applied HERE rather than trusted from the caller, because this is
// the durable edge and a writer that forgot to truncate would otherwise put an
// unbounded blob in the journal forever.
func (s *Store) RecordTranscript(nodeID, model string, entries []TranscriptEntry) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return fmt.Errorf("record transcript: %w: empty node id", ErrInvalid)
	}
	if len(entries) == 0 {
		return nil
	}
	if len(entries) > MaxTranscriptBatch {
		return fmt.Errorf("record transcript: %w: batch carries %d entries (limit %d); flush more often",
			ErrInvalid, len(entries), MaxTranscriptBatch)
	}
	bounded := make([]TranscriptEntry, 0, len(entries))
	for index, entry := range entries {
		if !validTranscriptKind(entry.Kind) {
			return fmt.Errorf("record transcript: %w: entry %d has no kind", ErrInvalid, index)
		}
		if entry.Turn < 0 {
			return fmt.Errorf("record transcript: %w: entry %d is on turn %d", ErrInvalid, index, entry.Turn)
		}
		entry.Tool = strings.TrimSpace(entry.Tool)
		entry.CallID = strings.TrimSpace(entry.CallID)
		entry.Text = TruncateTranscriptText(entry.Text)
		if entry.Millis < 0 {
			entry.Millis = 0
		}
		bounded = append(bounded, entry)
	}

	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("record transcript: %w", err)
	}
	defer tx.Rollback()

	if err := requireNode(tx, nodeID); err != nil {
		return fmt.Errorf("record transcript: %w", err)
	}
	payload := transcriptPayload{NodeID: nodeID, Model: strings.TrimSpace(model), Entries: bounded}
	seq, at, err := appendEvent(tx, nodeID, EventTranscriptRecorded, payload)
	if err != nil {
		return fmt.Errorf("record transcript: %w", err)
	}
	if err := applyTranscriptView(tx, payload, seq, at); err != nil {
		return fmt.Errorf("record transcript: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record transcript: %w", err)
	}
	return nil
}

// TranscriptFor is one node's record, oldest entry first: every flush of every
// attempt, in the order the entries happened. A limit of zero or less reads the
// whole thing; a node whose worker never recorded a transcript answers with
// nothing, which reads as "this executor keeps no record" rather than as "this
// leaf did nothing".
func (s *Store) TranscriptFor(nodeID string, limit int) ([]TranscriptEntry, error) {
	query := `
		SELECT turn, kind, tool, call_id, body, millis, failed
		FROM transcript WHERE node_id = ? ORDER BY seq, idx`
	args := []any{strings.TrimSpace(nodeID)}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("transcript: %w", err)
	}
	defer rows.Close()
	var entries []TranscriptEntry
	for rows.Next() {
		var entry TranscriptEntry
		var failed int
		if err := rows.Scan(&entry.Turn, &entry.Kind, &entry.Tool, &entry.CallID,
			&entry.Text, &entry.Millis, &failed); err != nil {
			return nil, fmt.Errorf("transcript: %w", err)
		}
		entry.Failed = failed != 0
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("transcript: %w", err)
	}
	return entries, nil
}

// TruncateTranscriptText bounds one entry's payload, keeping the head AND the
// tail with the gap named between them.
//
// Head-only would be the easy bound and it is the wrong one here. A tool result
// says what it did at the top and whether it worked at the bottom — a build log
// ends in the error, a test run ends in the count, a bash tool ends in its exit
// footer — so a record that keeps only the first four kilobytes of a fifty
// kilobyte output preserves the part a reader already guessed and throws away
// the part they opened it for.
func TruncateTranscriptText(text string) string {
	if len(text) <= MaxTranscriptTextBytes {
		return text
	}
	dropped := len(text) - MaxTranscriptTextBytes
	marker := fmt.Sprintf("\n… %d bytes elided …\n", dropped)
	// The marker spends part of the budget, so what is dropped is a little more
	// than the arithmetic above: the number is recomputed once the split is
	// known, which is why it is not simply len(text)-limit in the final string.
	room := MaxTranscriptTextBytes - len(marker)
	if room <= 0 {
		return marker
	}
	head := room * 2 / 3
	tail := room - head
	front := trimToRune(text[:head])
	back := trimFromRune(text[len(text)-tail:])
	dropped = len(text) - len(front) - len(back)
	return front + fmt.Sprintf("\n… %d bytes elided …\n", dropped) + back
}

// trimToRune cuts a prefix back to the last whole rune, so a truncation never
// leaves half a character behind for a terminal to draw as a replacement box.
func trimToRune(value string) string {
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

// trimFromRune is the same for a suffix: a slice taken from the end can begin
// inside a multi-byte rune, and the fix is to walk forward rather than back.
func trimFromRune(value string) string {
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[1:]
	}
	return value
}

func validTranscriptKind(kind TranscriptKind) bool {
	switch kind {
	case TranscriptAssistant, TranscriptToolCall, TranscriptToolResult,
		TranscriptFault, TranscriptNote, TranscriptElided:
		return true
	}
	return false
}

// applyTranscriptView projects one journaled flush into the table. The index is
// the entry's position within the flush, which together with the event sequence
// is a total order over everything a node's workers ever recorded.
func applyTranscriptView(tx *sql.Tx, payload transcriptPayload, seq int64, at time.Time) error {
	stamp := formatTime(at)
	for index, entry := range payload.Entries {
		failed := 0
		if entry.Failed {
			failed = 1
		}
		if _, err := tx.Exec(`
			INSERT INTO transcript (
			    seq, idx, ts, node_id, turn, kind, tool, call_id, body, millis, failed, model
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			seq, index, stamp, payload.NodeID, entry.Turn, string(entry.Kind),
			entry.Tool, entry.CallID, entry.Text, entry.Millis, failed, payload.Model); err != nil {
			return err
		}
	}
	return nil
}

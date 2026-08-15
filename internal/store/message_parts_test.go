package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// partsColumn reads the raw column, which is the only way to prove the legacy
// shape is byte-identical rather than merely equivalent after decoding.
func partsColumn(t *testing.T, graph *Store, seq int64) string {
	t.Helper()
	var encoded string
	if err := graph.db.QueryRow(`SELECT parts FROM messages WHERE seq = ?`, seq).Scan(&encoded); err != nil {
		t.Fatalf("read parts column of %d: %v", seq, err)
	}
	return encoded
}

func eventPayload(t *testing.T, graph *Store, seq int64) string {
	t.Helper()
	var payload string
	if err := graph.db.QueryRow(`SELECT payload FROM events WHERE seq = ?`, seq).Scan(&payload); err != nil {
		t.Fatalf("read event %d: %v", seq, err)
	}
	return payload
}

// TestLegacyMessageCarriesNoParts is the zero-change contract. A message posted
// the way every message has always been posted must journal the same payload,
// store the same column value, and read back with nothing new on it.
func TestLegacyMessageCarriesNoParts(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "legacy-parts.db"))
	posted, err := graph.PostMessage(Message{SessionID: "prose", Role: RoleUser, Body: "just words"})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if posted.Parts != nil {
		t.Fatalf("a prose message came back with %d parts", len(posted.Parts))
	}
	if got := partsColumn(t, graph, posted.Seq); got != "null" {
		t.Fatalf("parts column = %q, want the legacy null", got)
	}
	if payload := eventPayload(t, graph, posted.Seq); strings.Contains(payload, "parts") {
		t.Fatalf("the journaled payload grew a parts key: %s", payload)
	}
	read, err := graph.Messages("prose", 0, 0)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(read) != 1 || read[0].Parts != nil || read[0].Body != "just words" {
		t.Fatalf("legacy read back as %+v", read)
	}
}

func allKindsMessage() Message {
	return Message{
		SessionID: "parts",
		Role:      RoleAgent,
		Body:      "here is the diagram",
		Parts: []MessagePart{
			TextPart("here is the diagram"),
			QuestionRef(17),
			CardRef("wisp-nav2"),
			ProgressRef(ProgressPart{NodeID: "wisp-nav2", Phase: "compiling", Done: 2, Total: 5, Latest: "route table"}),
			ArtifactRef(ArtifactPart{
				Path: "/w/architecture.svg", MIME: "image/svg+xml", Bytes: 1611, NodeID: "wisp-nav2",
			}),
			EndedMark(EndedPart{How: EndLength, FinishReason: "length"}),
		},
	}
}

// TestEveryPartKindRoundTripsThroughTheJournal covers the write, both read
// queries, and a full replay from events — which is the only durable copy.
func TestEveryPartKindRoundTripsThroughTheJournal(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "parts-roundtrip.db"))
	seedPartsNode(t, graph, "wisp-nav2")

	want := allKindsMessage()
	want.NodeID = "wisp-nav2"
	posted, err := graph.PostMessage(want)
	if err != nil {
		t.Fatalf("post parts: %v", err)
	}
	if !reflect.DeepEqual(posted.Parts, want.Parts) {
		t.Fatalf("PostMessage returned %+v", posted.Parts)
	}

	check := func(stage string, messages []Message) {
		t.Helper()
		if len(messages) != 1 {
			t.Fatalf("%s: %d messages, want 1", stage, len(messages))
		}
		if !reflect.DeepEqual(messages[0].Parts, want.Parts) {
			t.Fatalf("%s: parts came back as %+v", stage, messages[0].Parts)
		}
		if messages[0].Body != want.Body {
			t.Fatalf("%s: body changed to %q", stage, messages[0].Body)
		}
	}

	listed, err := graph.Messages("parts", 0, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	check("Messages", listed)

	anchored, err := graph.NodeMessages("wisp-nav2", 0, 0)
	if err != nil {
		t.Fatalf("node messages: %v", err)
	}
	check("NodeMessages", anchored)

	if err := graph.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	replayed, err := graph.Messages("parts", 0, 0)
	if err != nil {
		t.Fatalf("list after rebuild: %v", err)
	}
	check("after Rebuild", replayed)
}

// TestTheLegacyPartsPathTouchesNoHeap holds the cost promise. Every message in
// every existing database takes this branch, on every poll tick of every open
// surface, and it must be free — no decoder, no conversion, no allocation.
func TestTheLegacyPartsPathTouchesNoHeap(t *testing.T) {
	var parts []MessagePart
	for _, column := range [][]byte{[]byte("null"), []byte("[]"), nil} {
		if allocations := testing.AllocsPerRun(200, func() {
			decodeMessageParts(column, 1, &parts)
		}); allocations != 0 {
			t.Fatalf("reading %q allocated %.0f times per message", column, allocations)
		}
		if parts != nil {
			t.Fatalf("%q decoded to %+v", column, parts)
		}
	}
}

// TestPartsChangeNothingALegacyReaderReads is what lets this wave attach parts
// to any message without touching a renderer. The old transcript reads Body,
// Attachments, Model, Options, Brief and Progress and has never heard of Parts;
// a parts-bearing message and its parts-free twin must therefore be identical
// in every one of those fields, so the page draws the same pixels either way.
func TestPartsChangeNothingALegacyReaderReads(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "legacy-view.db"))

	twin := Message{
		SessionID: "twin", Role: RoleAgent, Body: "the architecture has three layers",
		Model: "sim/model", Answers: 0,
	}
	plain, err := graph.PostMessage(twin)
	if err != nil {
		t.Fatalf("post plain: %v", err)
	}
	twin.Parts = []MessagePart{
		TextPart(twin.Body),
		CardRef("nobody"),
		ArtifactRef(ArtifactPart{Path: "/w/architecture.svg", MIME: "image/svg+xml", Bytes: 1611}),
		EndedMark(EndedPart{How: EndLength, FinishReason: "length"}),
	}
	bearing, err := graph.PostMessage(twin)
	if err != nil {
		t.Fatalf("post bearing: %v", err)
	}

	read, err := graph.Messages("twin", 0, 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(read) != 2 {
		t.Fatalf("read %d messages", len(read))
	}
	// Strip the two fields that must differ — the sequence and its timestamp —
	// and the parts themselves. Everything left is what a legacy reader sees.
	legacyView := func(message Message) Message {
		message.Seq, message.Time, message.Parts = 0, plain.Time, nil
		return message
	}
	if !reflect.DeepEqual(legacyView(read[0]), legacyView(read[1])) {
		t.Fatalf("attaching parts moved a legacy field:\n plain %+v\nparts %+v", read[0], read[1])
	}
	if read[1].Seq != bearing.Seq || len(read[1].Parts) != 4 {
		t.Fatalf("the parts-bearing twin lost its parts: %+v", read[1])
	}
}

// TestUnknownPartKindSurvivesAReaderThatDoesNotKnowIt is the forward-compat
// contract. A build that has never heard of a kind must carry it through the
// decode, the projection and a full rebuild without editing or dropping it —
// otherwise an older binary replaying a newer journal quietly deletes records.
func TestUnknownPartKindSurvivesAReaderThatDoesNotKnowIt(t *testing.T) {
	const encoded = `[{"kind":"text","text":"one"},` +
		`{"kind":"citation","citation":{"url":"https://x.test/a","span":[3,9]},"confidence":0.5},` +
		`{"kind":"ended","ended":{"how":"length","finish_reason":"length"}}]`

	var parts []MessagePart
	if err := json.Unmarshal([]byte(encoded), &parts); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(parts) != 3 || parts[1].Kind != "citation" {
		t.Fatalf("decoded %+v", parts)
	}
	// The neighbours still decode into their typed shapes; only the stranger is
	// held as bytes.
	if parts[0].Text != "one" || parts[2].Ended == nil || parts[2].Ended.How != EndLength {
		t.Fatalf("known kinds beside an unknown one decoded as %+v", parts)
	}
	normalized, err := normalizeMessageParts(parts)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	reEncoded, err := json.Marshal(normalized)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	var before, after any
	if err := json.Unmarshal([]byte(encoded), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(reEncoded, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("round trip changed the record:\n got %s\nwant %s", reEncoded, encoded)
	}

	// And the same through the durable path. The parts decoded above are exactly
	// what a newer build's message would arrive as, stranger included, so they
	// are what gets journaled — write, project, replay, read.
	graph := openTestStore(t, filepath.Join(t.TempDir(), "unknown-parts.db"))
	posted, err := graph.PostMessage(Message{
		SessionID: "future", Role: RoleAgent, Body: "one", Parts: parts,
	})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	replayed, err := graph.Messages("future", 0, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(replayed) != 1 || len(replayed[0].Parts) != 3 || replayed[0].Parts[1].Kind != "citation" {
		t.Fatalf("replay lost the unknown kind: %+v", replayed)
	}
	var column any
	if err := json.Unmarshal([]byte(partsColumn(t, graph, posted.Seq)), &column); err != nil {
		t.Fatalf("projected column is not JSON: %v", err)
	}
	if !reflect.DeepEqual(column, before) {
		t.Fatalf("the projection edited the unknown part: %s", partsColumn(t, graph, posted.Seq))
	}
}

// TestUnreadablePartsReadAsPlainProse is the degradation contract: a projection
// defect costs the structure, never the conversation.
func TestUnreadablePartsReadAsPlainProse(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "broken-parts.db"))
	posted, err := graph.PostMessage(Message{
		SessionID: "broken", Role: RoleAgent, Body: "the words survive",
		Parts: []MessagePart{TextPart("the words survive")},
	})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	// Valid JSON — the column's CHECK insists on that much — but not a parts
	// list, and then a parts list that fails validation. Both must degrade.
	for _, corruption := range []string{`{"kind":"text"}`, `[{"kind":"text","text":"   "}]`, `[7]`} {
		if _, err := graph.db.Exec(`UPDATE messages SET parts = ? WHERE seq = ?`, corruption, posted.Seq); err != nil {
			t.Fatalf("corrupt with %s: %v", corruption, err)
		}
		read, err := graph.Messages("broken", 0, 0)
		if err != nil {
			t.Fatalf("%s: read failed instead of degrading: %v", corruption, err)
		}
		if len(read) != 1 || read[0].Body != "the words survive" {
			t.Fatalf("%s: the message itself was lost: %+v", corruption, read)
		}
		if read[0].Parts != nil {
			t.Fatalf("%s: unreadable parts surfaced as %+v", corruption, read[0].Parts)
		}
	}
}

// TestPartsRefuseWhatCannotBeRendered keeps the invalid shapes out of the
// journal rather than out of the renderer.
func TestPartsRefuseWhatCannotBeRendered(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "invalid-parts.db"))
	cases := map[string][]MessagePart{
		"empty text":          {TextPart("  ")},
		"no kind":             {{Text: "x"}},
		"question zero":       {QuestionRef(0)},
		"card with nobody":    {CardRef("   ")},
		"artifact no path":    {ArtifactRef(ArtifactPart{MIME: "image/svg+xml"})},
		"negative bytes":      {ArtifactRef(ArtifactPart{Path: "/w/a.svg", Bytes: -1})},
		"ended nonsense":      {EndedMark(EndedPart{How: "vanished"})},
		"progress past total": {ProgressRef(ProgressPart{Phase: "compiling", Done: 6, Total: 5})},
		"payload missing":     {{Kind: PartEnded}},
	}
	for name, parts := range cases {
		_, err := graph.PostMessage(Message{SessionID: "invalid", Role: RoleAgent, Body: "x", Parts: parts})
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: PostMessage returned %v, want ErrInvalid", name, err)
		}
	}

	tooMany := make([]MessagePart, maxMessageParts+1)
	for index := range tooMany {
		tooMany[index] = TextPart("x")
	}
	if _, err := graph.PostMessage(Message{
		SessionID: "invalid", Role: RoleAgent, Body: "x", Parts: tooMany,
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an over-long parts list was accepted: %v", err)
	}

	if _, err := graph.PostMessage(Message{
		SessionID: "invalid", Role: RoleAgent, Body: "x",
		Parts: []MessagePart{TextPart(strings.Repeat("x", MaxMessagePartsBytes+1))},
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an oversized parts column was accepted: %v", err)
	}
}

// TestClassifyEndReadsTheProvidersWord pins the semantics table. The raw string
// is kept whatever we decide it means, which is what makes a wrong reading
// recoverable from the journal rather than permanent.
func TestClassifyEndReadsTheProvidersWord(t *testing.T) {
	cases := []struct {
		reason   string
		streamed bool
		want     EndKind
	}{
		{"stop", true, EndCompleted},
		{"end_turn", false, EndCompleted},
		{"tool_calls", true, EndCompleted},
		{"length", true, EndLength},
		{"LENGTH", false, EndLength},
		{" max_tokens ", false, EndLength},
		{"error", true, EndStreamDrop},
		{"", true, EndStreamDrop},
		{"", false, EndCompleted},
		{"something_new", true, EndCompleted},
	}
	for _, testCase := range cases {
		if got := ClassifyEnd(testCase.reason, testCase.streamed); got != testCase.want {
			t.Fatalf("ClassifyEnd(%q, streamed=%t) = %q, want %q",
				testCase.reason, testCase.streamed, got, testCase.want)
		}
		ended := EndedFor(testCase.reason, testCase.streamed)
		if testCase.want == EndCompleted {
			if ended != nil {
				t.Fatalf("a completed turn earned a mark: %+v", ended)
			}
			continue
		}
		if ended == nil || ended.How != testCase.want || ended.FinishReason != strings.TrimSpace(testCase.reason) {
			t.Fatalf("EndedFor(%q, %t) = %+v", testCase.reason, testCase.streamed, ended)
		}
	}
	if mark := InterruptedEnd(); mark.How != EndInterrupted || mark.FinishReason != "" {
		t.Fatalf("interrupted mark = %+v", mark)
	}
}

// TestPartsAreNotIndexedButTheBodyStillIs keeps this wave's FTS promise: parts
// change nothing about search.
func TestPartsAreNotIndexedButTheBodyStillIs(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "parts-fts.db"))
	if _, err := graph.PostMessage(Message{
		SessionID: "search", Role: RoleAgent, Body: "the harbour survey is done",
		Parts: []MessagePart{
			TextPart("the harbour survey is done"),
			ArtifactRef(ArtifactPart{Path: "/w/zeppelin.svg", MIME: "image/svg+xml", Bytes: 12}),
		},
	}); err != nil {
		t.Fatalf("post: %v", err)
	}
	hits, err := graph.SearchMessages("harbour", "search", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("body search found %d hits, want 1", len(hits))
	}
	zeppelin, err := graph.SearchMessages("zeppelin", "search", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(zeppelin) != 0 {
		t.Fatalf("parts reached the index this wave: %+v", zeppelin)
	}
}

// TestPartsColumnArrivesOnAPreExistingDatabase opens a database whose messages
// table predates the column, twice, and proves the migration is both real and
// idempotent — and that the rows already in it read as the prose they are.
func TestPartsColumnArrivesOnAPreExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pre-parts.db")
	graph, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := graph.PostMessage(Message{SessionID: "old", Role: RoleUser, Body: "written before parts"}); err != nil {
		t.Fatalf("post: %v", err)
	}
	if err := graph.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	stripPartsColumn(t, path)

	for pass := 1; pass <= 2; pass++ {
		reopened, err := Open(path)
		if err != nil {
			t.Fatalf("pass %d: open a pre-parts database: %v", pass, err)
		}
		found, err := tableHasColumn(reopened.db, "messages", "parts")
		if err != nil || !found {
			t.Fatalf("pass %d: parts column missing after migration (%v)", pass, err)
		}
		messages, err := reopened.Messages("old", 0, 0)
		if err != nil {
			t.Fatalf("pass %d: read migrated rows: %v", pass, err)
		}
		if len(messages) < 1 || messages[0].Body != "written before parts" || messages[0].Parts != nil {
			t.Fatalf("pass %d: migrated row = %+v", pass, messages)
		}
		if _, err := reopened.PostMessage(Message{
			SessionID: "old", Role: RoleAgent, Body: "and one after",
			Parts: []MessagePart{EndedMark(*InterruptedEnd())},
		}); err != nil {
			t.Fatalf("pass %d: post parts into a migrated database: %v", pass, err)
		}
		if err := reopened.Close(); err != nil {
			t.Fatalf("pass %d: close: %v", pass, err)
		}
	}
}

// stripPartsColumn rebuilds the messages table exactly as it stood before this
// wave. SQLite will not drop a column a CHECK constraint names, and the honest
// fixture is the old definition anyway.
func stripPartsColumn(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("reopen raw: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		ALTER TABLE messages RENAME TO messages_pre_parts;
		CREATE TABLE messages (
		    seq         INTEGER PRIMARY KEY REFERENCES events(seq),
		    ts          TEXT NOT NULL,
		    session_id  TEXT NOT NULL DEFAULT '',
		    role        TEXT NOT NULL CHECK (role IN ('user', 'agent', 'system')),
		    body        TEXT NOT NULL,
		    attachments JSON NOT NULL DEFAULT '[]' CHECK (json_valid(attachments)),
		    model       TEXT NOT NULL DEFAULT '',
		    node_id     TEXT NOT NULL DEFAULT '',
		    command_seq INTEGER NOT NULL DEFAULT 0,
		    question_seq INTEGER NOT NULL DEFAULT 0,
		    answers_seq INTEGER NOT NULL DEFAULT 0,
		    options     JSON NOT NULL DEFAULT '[]' CHECK (json_valid(options)),
		    brief       JSON NOT NULL DEFAULT 'null' CHECK (json_valid(brief)),
		    progress    JSON NOT NULL DEFAULT 'null' CHECK (json_valid(progress))
		);
		INSERT INTO messages SELECT seq, ts, session_id, role, body, attachments, model,
		    node_id, command_seq, question_seq, answers_seq, options, brief, progress
		    FROM messages_pre_parts;
		DROP TABLE messages_pre_parts;
		CREATE INDEX IF NOT EXISTS messages_session_seq ON messages (session_id, seq);
		CREATE INDEX IF NOT EXISTS messages_question_seq ON messages (question_seq, role, seq);
	`); err != nil {
		t.Fatalf("strip the parts column: %v", err)
	}
}

func seedPartsNode(t *testing.T, graph *Store, id string) {
	t.Helper()
	if err := graph.Splice(RootID,
		Subtree{Nodes: []NodeSpec{{ID: id, Brief: "navigate", Stage: 1}}},
		Provenance{Origin: OriginUser, SessionID: "parts", Intent: "navigate"}); err != nil {
		t.Fatalf("seed node %s: %v", id, err)
	}
}

package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// A PAGE MAY LOWER SALIENCE; IT MAY NEVER DROP A FACT.
//
// The conversation and a task's page are one transcript grammar pointed at two
// speakers (docs/design/lens/DESIGN.md). They are allowed to draw the same fact
// at different sizes — folded, clause-sized, in the header instead of inline —
// and they are not allowed to make one disappear.
//
// This is the JOURNAL-ROLE half of that contract: every role the engine can put
// in a record reaches a block on BOTH pages. It exists because the half that was
// missing is exactly how the defect happened — the page had a second reading of
// the record that knew two roles, so a marked line, a session's own line and a
// divider all arrived as ordinary questions or as nothing at all.

func TestEveryJournalRoleReachesBothPages(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	cases := []struct {
		role  string
		entry session.DisplayEntry
		// want is the text that has to be on a block of each page, and kind is
		// what that block has to be. A page that drew the fact as something else
		// is a page that changed what happened.
		want string
		kind entryKind
	}{
		{
			role:  "user",
			entry: session.DisplayEntry{Role: "user", Text: "Fix the nil-map crash"},
			want:  "Fix the nil-map crash", kind: entryUser,
		},
		{
			role: "user, marked as a correction",
			entry: session.DisplayEntry{Role: "user", Text: "use the staging bucket",
				Steer: &session.SteerMark{At: time.Now(), Consumed: true,
					Landing: session.SteerDelivered(false)}},
			want: "use the staging bucket", kind: entrySteer,
		},
		{
			role:  "assistant",
			entry: session.DisplayEntry{Role: "assistant", Text: "the map is never made"},
			want:  "the map is never made", kind: entryAssistant,
		},
		{
			role: "tool",
			entry: session.DisplayEntry{Role: "tool", Tool: "read", Hint: "read load.go",
				CallID: "c1", Args: `{"path":"load.go"}`, Output: "189 lines", Answered: true},
			want: "read load.go", kind: entryTool,
		},
		{
			role:  "note",
			entry: session.DisplayEntry{Role: "note", Text: "above here the model keeps a shortened record"},
			want:  "above here the model keeps a shortened record", kind: entryDivider,
		},
		{
			role:  "aside",
			entry: session.DisplayEntry{Role: "aside", Text: "part 2 of 3 finished"},
			want:  "part 2 of 3 finished", kind: entryNote,
		},
	}

	for _, c := range cases {
		entries := []session.DisplayEntry{c.entry}
		for page, shape := range map[string]replayShape{
			"the conversation": chatReplay(0),
			"a task's page":    roomReplay(0),
		} {
			blocks, _ := a.replayBlocks(entries, shape)
			found := false
			for _, block := range blocks {
				if block.kind != c.kind {
					continue
				}
				if strings.Contains(block.text, c.want) ||
					(block.steer != nil && strings.Contains(block.steer.words, c.want)) {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s dropped the %q role: %#v", page, c.role, blocks)
			}
		}
	}
}

// AND THE TWO PAGES DIFFER ONLY WHERE THE DESIGN SAYS THEY DO. A task's page is
// ONE question — the instruction it was given — with corrections hanging off it,
// and the conversation counts a turn per question. Nothing else about the walk
// changes.
func TestATaskPageIsOneTurnWithElbowsAndTheConversationCountsQuestions(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	entries := []session.DisplayEntry{
		{Role: "user", Text: "Fix the nil-map crash"},
		{Role: "assistant", Text: "reading the loader"},
		{Role: "user", Text: "the config lives under etc/",
			Steer: &session.SteerMark{At: time.Now(), Consumed: true,
				Landing: session.SteerDelivered(false)}},
	}

	room, roomTurns := a.replayBlocks(entries, roomReplay(0))
	if roomTurns != 1 {
		t.Fatalf("a task's page counted %d turns, want the one its instruction opened", roomTurns)
	}
	if !room[0].brief {
		t.Fatalf("the first of the person's blocks is not the instruction: %#v", room[0])
	}
	elbow := room[len(room)-1]
	if elbow.kind != entrySteer || elbow.turn != room[0].turn {
		t.Fatalf("the correction is not an elbow on the instruction's turn: %#v", elbow)
	}
	// A REPLAYED CORRECTION IS SETTLED AND UNFADED, AND WEARS NO CLAUSE. What
	// happened to those words is news, and there is no right now about yesterday:
	// the block's position is what says where they went.
	if !elbow.steer.consumed || !elbow.steer.landed.IsZero() || elbow.steer.receipt != "" {
		t.Fatalf("a replayed correction came back as live news: %+v", *elbow.steer)
	}
	if row := a.elbowRows(*elbow.steer, 60); len(row) != 1 ||
		strings.Contains(plain(row[0]), session.SteerDelivered(false)) {
		t.Fatalf("a replayed correction wears a receipt: %q", row)
	}

	// The conversation counts the question and never marks a brief.
	chat, chatTurns := a.replayBlocks(entries, chatReplay(0))
	if chatTurns != 1 {
		t.Fatalf("the conversation counted %d turns, want 1", chatTurns)
	}
	for _, block := range chat {
		if block.brief {
			t.Fatalf("the conversation folded a message as terms of reference: %#v", block)
		}
	}
}

// AND A CALL'S IDENTITY AND ARGUMENTS SURVIVE THE SHAPING, on both pages. They
// are what a live end pairs on: a page drawn out of the record and then kept
// listening has to land the end that arrives a second later on the row already
// standing, or the same call is drawn twice — once running for ever, once
// finished.
func TestTheShapingKeepsWhatALiveEndPairsOn(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	entries := []session.DisplayEntry{{
		Role: "tool", Tool: "bash", Hint: "bash go test ./...", CallID: "c2",
		Args: `{"command":"go test ./..."}`,
	}}
	for page, shape := range map[string]replayShape{
		"the conversation": chatReplay(0),
		"a task's page":    roomReplay(0),
	} {
		blocks, _ := a.replayBlocks(entries, shape)
		if len(blocks) != 1 {
			t.Fatalf("%s: %d blocks, want the one call", page, len(blocks))
		}
		if blocks[0].callID != "c2" {
			t.Fatalf("%s: the call lost its identity: %#v", page, blocks[0])
		}
		if !strings.Contains(blocks[0].detail.Args, "go test") {
			t.Fatalf("%s: the call lost its arguments: %#v", page, blocks[0].detail)
		}
	}
	// AND ONLY THE PAGE WATCHING LIVE WORK DRAWS IT AS RUNNING. The conversation
	// resumes from a record that was mended on the way in, so a row left spinning
	// there would be waiting for an end that already happened.
	room, _ := a.replayBlocks(entries, roomReplay(0))
	if !room[0].status.live() {
		t.Fatalf("a task's page drew an unanswered call as finished: %#v", room[0])
	}
	chat, _ := a.replayBlocks(entries, chatReplay(0))
	if chat[0].status.live() {
		t.Fatalf("the conversation left a resumed call running: %#v", chat[0])
	}
}

// ── the region a compaction pass edited away ────────────────────────────────

// A NODE'S PAGE DRAWS WHAT THE PASS SHORTENED, above the seam that says where
// the shortening starts.
//
// A compaction rewrites the work it folds and journals the rewritten copy again,
// so the same conversation is in the record twice: once as it happened, and once
// with its results stubbed and its long runs folded. A page that drew only the
// copy would be showing a person a summary of their own work as though it were
// the work — every call above the marker opening onto a stub — and THE LENS MAY
// LOWER SALIENCE; IT MAY NOT DROP A FACT (#252).
func TestATaskPageDrawsTheWorkACompactionShortened(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.jsonl")
	record := strings.Join([]string{
		`{"type":"session","version":1,"id":"n1","cwd":"/tmp/lab"}`,
		`{"type":"message","role":"user","content":"Fix the nil-map crash"}`,
		`{"type":"message","role":"assistant","content":"reading the loader","toolCalls":[` +
			`{"id":"c1","function":{"name":"read","arguments":"{\"path\":\"load.go\"}"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"c1","content":"189 lines of the real file"}`,
		// The pass rewrote those three and journaled all three again.
		`{"type":"compaction","stubbed":3,"window":3,"tokensBefore":84000}`,
		`{"type":"message","role":"user","content":"Fix the nil-map crash"}`,
		`{"type":"message","role":"assistant","content":"[folded]"}`,
		`{"type":"message","role":"tool","toolCallId":"c1","content":"[stubbed]"}`,
		// And this happened after the pass.
		`{"type":"message","role":"assistant","content":"the map is never made"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(record), 0o600); err != nil {
		t.Fatalf("writing the record: %v", err)
	}

	a := newTestApp(&fakeAgent{})
	blocks, turns := a.roomRecord(session.ReadTranscript(path), 0)
	if turns != 1 {
		t.Fatalf("the page counted %d turns, want the one its instruction opened", turns)
	}

	seam := -1
	for i := range blocks {
		if blocks[i].kind == entrySeam {
			seam = i
		}
	}
	if seam < 0 {
		t.Fatalf("the page drew no seam over a record that was compacted: %#v", blocks)
	}
	// ABOVE THE SEAM the work is what happened: the call opens onto the file it
	// really read, and the prose is the prose.
	above := blocks[:seam]
	var call *entry
	for i := range above {
		if above[i].kind == entryTool && above[i].tool == "read" {
			call = &above[i]
		}
	}
	if call == nil {
		t.Fatalf("the call the pass shortened is not on the page above the seam: %#v", above)
	}
	if !strings.Contains(call.detail.Output, "189 lines of the real file") {
		t.Fatalf("the call above the seam opens onto the pass's stub: %q", call.detail.Output)
	}
	if !blockSaying(above, "reading the loader") {
		t.Fatalf("the prose the pass folded is not above the seam: %#v", above)
	}
	// AND THE COPY IT REPLACES IS NOT DRAWN, or the page would show the node's
	// work to itself twice.
	if blockSaying(blocks, "[folded]") {
		t.Fatalf("the page drew the pass's own rewritten copy as well: %#v", blocks)
	}
	// BELOW THE SEAM is the conversation the model still carries.
	if !blockSaying(blocks[seam:], "the map is never made") {
		t.Fatalf("the work after the pass is not below the seam: %#v", blocks[seam:])
	}
	// And the instruction is marked once, above the seam, where the person said it.
	briefs := 0
	for i := range blocks {
		if blocks[i].brief {
			briefs++
		}
	}
	if briefs != 1 || !blocks[0].brief {
		t.Fatalf("%d blocks are marked as the instruction, want the first one only", briefs)
	}
}

// AND AN ORDINARY RECORD DRAWS NO SEAM AT ALL. The row is a statement about a
// boundary, and a page with no boundary has nothing to state.
func TestATaskPageThatWasNeverCompactedDrawsNoSeam(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.jsonl")
	if err := os.WriteFile(path, []byte(midCallRoomRecord), 0o600); err != nil {
		t.Fatalf("writing the record: %v", err)
	}
	a := newTestApp(&fakeAgent{})
	blocks, _ := a.roomRecord(session.ReadTranscript(path), 0)
	for i := range blocks {
		if blocks[i].kind == entrySeam {
			t.Fatalf("a record nobody compacted drew a seam: %#v", blocks)
		}
	}
	if !blockSaying(blocks, "Looking at the loader") {
		t.Fatalf("the page is missing the work: %#v", blocks)
	}
}

// midCallRoomRecord is an ordinary node's record: one instruction, one call that
// came back, and one the record left open.
const midCallRoomRecord = `{"type":"session","version":1,"id":"n1","cwd":"/tmp/lab"}
{"type":"message","role":"user","content":"Fix the nil-map crash"}
{"type":"message","role":"assistant","content":"Looking at the loader.","toolCalls":[{"id":"c1","function":{"name":"read","arguments":"{\"path\":\"load.go\"}"}}]}
{"type":"message","role":"tool","toolCallId":"c1","content":"189 lines"}
`

// blockSaying reports whether any block carries these words.
func blockSaying(blocks []entry, words string) bool {
	for i := range blocks {
		if strings.Contains(blocks[i].text, words) {
			return true
		}
	}
	return false
}

// ── the two things the record cannot draw over ──────────────────────────────

// A PAGE SAYS SO WHEN THE RECORD IT IS DRAWN FROM STOPS SHORT. A journaled
// message can be a whole file's content, so one very large paste is one line
// this build cannot read — and everything below it is missing from the page.
// Drawing a shorter transcript and saying nothing is the page claiming that is
// all the work there was.
func TestATaskPageSaysWhereItsRecordStopped(t *testing.T) {
	huge := strings.Repeat("x", 5<<20)
	path := filepath.Join(t.TempDir(), "node.jsonl")
	record := strings.Join([]string{
		`{"type":"session","version":1,"id":"n1","cwd":"/tmp/lab"}`,
		`{"type":"message","role":"user","content":"Fix the nil-map crash"}`,
		`{"type":"message","role":"assistant","content":"the map is never made"}`,
		`{"type":"message","role":"user","content":"` + huge + huge + `"}`,
		`{"type":"message","role":"assistant","content":"never read"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(record), 0o600); err != nil {
		t.Fatalf("writing the record: %v", err)
	}

	a := newTestApp(&fakeAgent{})
	blocks, _ := a.roomRecord(session.ReadTranscript(path), 0)
	if len(blocks) == 0 {
		t.Fatal("the page drew nothing at all")
	}
	// IT IS THE TOP ROW, and it names the line — a place in a file somebody can
	// open — rather than saying something went wrong somewhere.
	head := blocks[0]
	if head.kind != entrySeam || !strings.Contains(head.text, unreadWord+"4") {
		t.Fatalf("the page does not say where its record stopped: %#v", head)
	}
	if !blockSaying(blocks, "the map is never made") {
		t.Fatalf("the work above the unreadable line is not on the page: %#v", blocks)
	}

	// AND THE WINDOW DOES NOT CUT IT AWAY. It is a fact about the reading, not
	// about the work, so a page long enough to be windowed still says it.
	windowed, _ := a.roomRecord(session.ReadTranscript(path), 1)
	if len(windowed) != 2 || windowed[0].kind != entrySeam {
		t.Fatalf("a windowed page dropped the line it could not read: %#v", windowed)
	}
}

// AND AN ORDINARY RECORD SAYS NOTHING, which is what makes the row above mean
// something on the pages that do carry it.
func TestATaskPageThatCouldBeReadWholeSaysNothingAboutIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.jsonl")
	if err := os.WriteFile(path, []byte(midCallRoomRecord), 0o600); err != nil {
		t.Fatalf("writing the record: %v", err)
	}
	a := newTestApp(&fakeAgent{})
	blocks, _ := a.roomRecord(session.ReadTranscript(path), 0)
	for i := range blocks {
		if strings.Contains(blocks[i].text, unreadWord) {
			t.Fatalf("a record read whole claimed it stopped short: %#v", blocks)
		}
	}
}

// NOTHING ABOVE THE MARKER IS STILL RUNNING. A call is drawn as in flight
// because the record names it with no result under it, and the end that settles
// such a row arrives on the LIVE lane — which reaches the tail of the record and
// nothing above a compaction that finished minutes or days ago. A row left
// spinning up there spins for ever.
func TestACallAboveTheSeamIsNeverDrawnAsStillRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.jsonl")
	record := strings.Join([]string{
		`{"type":"session","version":1,"id":"n1","cwd":"/tmp/lab"}`,
		`{"type":"message","role":"user","content":"Fix the nil-map crash"}`,
		// A batch the pass swept up before its results were journaled: above the
		// marker this call has no answer under it and never will.
		`{"type":"message","role":"assistant","content":"reading","toolCalls":[` +
			`{"id":"c1","function":{"name":"read","arguments":"{\"path\":\"load.go\"}"}}]}`,
		`{"type":"compaction","stubbed":2,"window":2,"tokensBefore":84000}`,
		`{"type":"message","role":"user","content":"Fix the nil-map crash"}`,
		`{"type":"message","role":"assistant","content":"[folded]"}`,
		// And the work happening NOW, with a call that really is in flight.
		`{"type":"message","role":"assistant","content":"running the tests","toolCalls":[` +
			`{"id":"c9","function":{"name":"bash","arguments":"{\"command\":\"go test ./...\"}"}}]}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(record), 0o600); err != nil {
		t.Fatalf("writing the record: %v", err)
	}

	a := newTestApp(&fakeAgent{})
	blocks, _ := a.roomRecord(session.ReadTranscript(path), 0)
	seam := -1
	for i := range blocks {
		if blocks[i].kind == entrySeam {
			seam = i
		}
	}
	if seam < 0 {
		t.Fatalf("the page drew no seam: %#v", blocks)
	}
	for i := range blocks[:seam] {
		if blocks[i].kind == entryTool && blocks[i].status.live() {
			t.Fatalf("a call above the seam is drawn as still running: %#v", blocks[i])
		}
	}
	// AND THE ONE THAT REALLY IS IN FLIGHT STILL SPINS. The live lane reaches the
	// tail of the record, so that row has an end coming.
	running := 0
	for _, block := range blocks[seam:] {
		if block.kind == entryTool && block.status.live() {
			running++
		}
	}
	if running != 1 {
		t.Fatalf("%d calls below the seam are drawn as running, want the one in flight:\n%#v",
			running, blocks[seam:])
	}
}

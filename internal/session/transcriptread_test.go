package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ONE RECORD, ONE READING.
//
// A page watching another agent's work used to read the session file itself,
// with its own scanner and its own short list of which roles exist — so a line
// this package had MARKED came back unmarked, and yesterday's correction reopened
// as a second brief (#252). The reading is this package's now
// ([ReadTranscript]), and these are the facts a page is built on.

// deliveredLine puts one of the person's lines into a node's own record, the way
// [Agent.SteerTask] does, and hands back the file it was written to.
func deliveredLine(t *testing.T, text string, waiting bool) (string, *Agent) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "node.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.SessionFile = path })
	if !agent.enqueueSteeredLine(text, waiting) {
		t.Fatal("the node's own steering lane refused a line")
	}
	agent.mu.Lock()
	landed, _ := agent.drainSteeringLocked(nil)
	agent.mu.Unlock()
	if landed != 1 {
		t.Fatalf("%d lines drained, want the one that was delivered", landed)
	}
	return path, agent
}

// A LINE THE PERSON SENT INTO RUNNING WORK IS MARKED IN THE RECORD AS ONE. It is
// an ordinary user message on the wire — that is what the node has to read it as
// — and the mark is the only thing that remembers it did not open the turn it
// sits in. Without it a page reopened tomorrow draws a correction as a second
// brief, which is exactly what rooms did.
func TestALineDeliveredToANodeIsMarkedInItsRecord(t *testing.T) {
	const said = "the config lives under etc/"
	path, agent := deliveredLine(t, said, false)

	entry := steeredEntry(t, agent.Transcript(), said)
	if !entry.Steer.Consumed {
		t.Fatal("a delivered line is recorded as never delivered")
	}
	if entry.Steer.At.IsZero() {
		t.Fatal("the record does not say when the person sent the line")
	}
	// THE LANDING IS THE ENGINE'S OWN SENTENCE AND CARRIES AT MOST ONE FACT.
	if entry.Steer.Landing != SteerDelivered(false) {
		t.Fatalf("landing = %q, want %q", entry.Steer.Landing, SteerDelivered(false))
	}
	// AND IT SURVIVES BEING READ BACK OFF THE FILE BY SOMEBODY ELSE, which is the
	// only reading a task's page ever does: the node's agent is not this process's
	// to ask.
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened := steeredEntry(t, ReadTranscript(path).Entries, said)
	if reopened.Steer.Landing != SteerDelivered(false) || !reopened.Steer.Consumed {
		t.Fatalf("the mark did not survive the file: %+v", *reopened.Steer)
	}
	if reopened.Role != "user" {
		t.Fatalf("a correction came back as %q, want the person's own role", reopened.Role)
	}
}

// AND A LINE THAT WOKE A PARKED NODE KEEPS THE TRUER SENTENCE. The node had
// handed its work out and said everything it had to say; the line does not land
// in a step it was about to take, it starts one.
func TestALineThatWokeAParkedNodeSaysSoInTheRecord(t *testing.T) {
	const said = "check the staging bucket first"
	path, agent := deliveredLine(t, said, true)
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	entry := steeredEntry(t, ReadTranscript(path).Entries, said)
	if entry.Steer.Landing != SteerDelivered(true) {
		t.Fatalf("landing = %q, want %q", entry.Steer.Landing, SteerDelivered(true))
	}
	if SteerDelivered(true) == SteerDelivered(false) {
		t.Fatal("the two facts a delivery can carry are spelled the same")
	}
}

// steeredEntry is the one entry carrying these words, and it fails rather than
// returns when the record does not mark it.
func steeredEntry(t *testing.T, entries []DisplayEntry, words string) DisplayEntry {
	t.Helper()
	for _, entry := range entries {
		if strings.TrimSpace(entry.Text) != words {
			continue
		}
		if entry.Steer == nil {
			t.Fatalf("the line %q came back unmarked, as a question nobody asked", words)
		}
		return entry
	}
	t.Fatalf("no entry says %q: %#v", words, entries)
	return DisplayEntry{}
}

// ── the door ────────────────────────────────────────────────────────────────

// midCallRecord is a node's file at the instant somebody walks in: a call that
// came back, and a call the record names with no result under it because the
// batch is still running. This package writes the assistant message BEFORE the
// batch runs, so this shape is the ordinary one, not the exception.
const midCallRecord = `{"type":"session","version":1,"id":"n1","cwd":"/tmp/lab"}
{"type":"message","role":"user","content":"Fix the nil-map crash"}
{"type":"message","role":"assistant","content":"Looking at the loader.","toolCalls":[{"id":"c1","function":{"name":"read","arguments":"{\"path\":\"load.go\"}"}}]}
{"type":"message","role":"tool","toolCallId":"c1","content":"189 lines"}
{"type":"message","role":"assistant","content":"Running the tests.","toolCalls":[{"id":"c2","function":{"name":"bash","arguments":"{\"command\":\"go test ./...\"}"}}]}
`

// A CALL WITH NO RESULT UNDER IT COMES BACK UNANSWERED, AND KEEPS ITS IDENTITY
// AND ITS ARGUMENTS. Those three facts are what a page opened on running work is
// built from: it draws the call as still running, and pairs the end that arrives
// a second later with the row already standing rather than drawing the call
// twice.
func TestAnUnansweredCallReadsBackAsUnansweredWithItsIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.jsonl")
	if err := os.WriteFile(path, []byte(midCallRecord), 0o600); err != nil {
		t.Fatalf("writing the record: %v", err)
	}
	for name, entries := range map[string][]DisplayEntry{
		"path":  ReadTranscript(path).Entries,
		"bytes": ReadTranscriptBytes([]byte(midCallRecord)).Entries,
	} {
		calls := map[string]DisplayEntry{}
		for _, entry := range entries {
			if entry.Role == "tool" && entry.Tool != "" {
				calls[entry.CallID] = entry
			}
		}
		if len(calls) != 2 {
			t.Fatalf("%s: %d calls, want the two the record names: %#v", name, len(calls), entries)
		}
		if got := calls["c1"]; !got.Answered || got.Output == "" {
			t.Fatalf("%s: the call that came back reads as unanswered: %+v", name, got)
		}
		got := calls["c2"]
		if got.Answered {
			t.Fatalf("%s: a call the record left open reads as finished: %+v", name, got)
		}
		if got.Tool != "bash" || !strings.Contains(got.Args, "go test") {
			t.Fatalf("%s: the open call lost its arguments: %+v", name, got)
		}
	}
}

// AND THE READING IS NOT MENDED ON ITS WAY OUT. A resumed conversation has its
// transcript repaired so a provider will take it — an unanswered trailing batch
// is a 400 for ever — and that repair would delete the very shape above. A door
// that only DRAWS wants the record as written.
func TestReadingARecordKeepsTheBatchAResumeWouldDrop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.jsonl")
	if err := os.WriteFile(path, []byte(midCallRecord), 0o600); err != nil {
		t.Fatalf("writing the record: %v", err)
	}
	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replaySessionFile: %v", err)
	}
	for _, message := range replayed.messages {
		for _, call := range message.ToolCalls {
			if call.ID == "c2" {
				t.Fatal("the repair kept the unanswered batch, so this test proves nothing")
			}
		}
	}
	for _, entry := range ReadTranscript(path).Entries {
		if entry.CallID == "c2" {
			return
		}
	}
	t.Fatalf("the reading dropped the call a page opened mid-flight exists to draw: %#v",
		ReadTranscript(path).Entries)
}

// A RECORD THAT IS NOT THERE IS NOT AN ERROR. The file belongs to whoever is
// filling it in, and a page that refused to open because it did not exist yet
// would refuse to show the live work beside it as well.
func TestAnAbsentRecordReadsAsNothing(t *testing.T) {
	for name, entries := range map[string][]DisplayEntry{
		"empty path":   ReadTranscript("").Entries,
		"missing file": ReadTranscript(filepath.Join(t.TempDir(), "gone.jsonl")).Entries,
		"no bytes":     ReadTranscriptBytes(nil).Entries,
	} {
		if len(entries) != 0 {
			t.Fatalf("%s read as %d entries, want none", name, len(entries))
		}
	}
}

// A SESSION-AUTHORED LINE KEEPS ITS OWN LANE THROUGH THIS DOOR TOO. It is
// user-role on the wire because that is the only role a model can be told
// something in, and a page that drew it as the person's words would be putting
// words in somebody's mouth.
func TestTheDoorKeepsASessionsOwnLineOutOfThePersonsColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.SessionFile = path })
	const note = "part 2 of 3 finished"
	agent.enqueueAmbientNote(note)
	if landed := agent.drainSteering(nil); landed != 1 {
		t.Fatalf("%d notes drained, want 1", landed)
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for _, entry := range ReadTranscript(path).Entries {
		if strings.TrimSpace(entry.Text) == note {
			if entry.Role != "aside" {
				t.Fatalf("the session's own line reads as %q, want aside", entry.Role)
			}
			return
		}
	}
	t.Fatalf("the session's own line is not in the reading: %#v", ReadTranscript(path).Entries)
}

// ── the reading opens no picture's file ─────────────────────────────────────

// A READING DOOR NEVER OPENS A PICTURE. The messages it builds are shaped for
// display and thrown away; a room re-reads on every open and a run page four
// times a second, and rebuilding the bytes would be three syscalls and a hash
// per picture per read.
//
// AND ON A HOSTED RECORD IT WOULD ALSO BE FALSE: the paths belong to the other
// machine, so every one of them fails to open here and the rebuild writes
// `[image /their/path — file changed or gone]` into the person's own line about
// a file sitting untouched where it was made. This asserts the honest half —
// a path that IS NOT THERE still leaves the person's words alone and still gives
// the page the picture's name.
func TestAReadingDoorNeverOpensAPicture(t *testing.T) {
	const said = "what is wrong with this"
	gone := filepath.Join(t.TempDir(), "not-on-this-machine", "chart.png")
	record := `{"type":"session","version":1,"id":"n1","cwd":"/tmp/lab"}
{"type":"message","role":"user","content":"` + said + `","parts":[{"type":"image","path":"` + gone +
		`","sha256":"deadbeef","mime":"image/png"}]}
`
	path := filepath.Join(t.TempDir(), "node.jsonl")
	if err := os.WriteFile(path, []byte(record), 0o600); err != nil {
		t.Fatalf("writing the record: %v", err)
	}

	for name, entries := range map[string][]DisplayEntry{
		"path":  ReadTranscript(path).Entries,
		"bytes": ReadTranscriptBytes([]byte(record)).Entries,
	} {
		if len(entries) != 1 {
			t.Fatalf("%s: %d entries, want the one message: %#v", name, len(entries), entries)
		}
		got := entries[0]
		if got.Text != said {
			t.Fatalf("%s: the person's line reads %q, want %q — the reading opened the file",
				name, got.Text, said)
		}
		if strings.Contains(got.Text, "file changed or gone") || strings.Contains(got.Text, gone) {
			t.Fatalf("%s: a placeholder about a file this machine cannot see got into the line: %q",
				name, got.Text)
		}
		// AND THE PICTURE IS STILL NAMED. The reference is what the page draws the
		// marker from, so refusing to open the file costs nothing on screen.
		if len(got.ImageRefs) != 1 || got.ImageRefs[0] != gone {
			t.Fatalf("%s: the picture lost its name: %#v", name, got.ImageRefs)
		}
	}

	// AND THE RESUME PATH STILL REBUILDS, which is the other half of the same
	// decision: that transcript is about to be SENT.
	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replaySessionFile: %v", err)
	}
	if got := messageContentText(replayed.messages[0]); !strings.Contains(got, "file changed or gone") {
		t.Fatalf("the resume path stopped rebuilding the picture: %q", got)
	}
}

// ── a line this build cannot read ──────────────────────────────────────────

// A READER KEEPS EVERYTHING IT COULD READ AND NAMES THE LINE IT COULD NOT.
//
// A journaled message can be a whole file's content and the scanner's buffer has
// a ceiling, so one very large paste makes one unreadable line. That used to end
// the read with no messages at all — and every caller took it for "this file is
// not readable", so the conversation resumed EMPTY. The person had not lost a
// line, they had lost the session.
func TestALineTooLongKeepsEveryLineAboveItOnBothPaths(t *testing.T) {
	huge := strings.Repeat("x", 5<<20) // past readJournal's own 8MiB-per-line ceiling once quoted
	record := `{"type":"session","version":1,"id":"n1","cwd":"/tmp/lab"}
{"type":"message","role":"user","content":"Fix the nil-map crash"}
{"type":"message","role":"assistant","content":"Found it — the map is never made."}
{"type":"message","role":"user","content":"` + huge + huge + `"}
{"type":"message","role":"assistant","content":"never read"}
`
	path := filepath.Join(t.TempDir(), "node.jsonl")
	if err := os.WriteFile(path, []byte(record), 0o600); err != nil {
		t.Fatalf("writing the record: %v", err)
	}

	read := ReadTranscript(path)
	if len(read.Entries) != 2 {
		t.Fatalf("the door kept %d entries, want the two above the line it could not read: %#v",
			len(read.Entries), read.Entries)
	}
	if read.Entries[0].Text != "Fix the nil-map crash" {
		t.Fatalf("the door lost the first line: %#v", read.Entries)
	}
	// AND IT SAYS WHICH LINE. "Something went wrong somewhere in this file" is
	// not an answer somebody can act on; a line number is.
	if read.UnreadFrom != 4 {
		t.Fatalf("UnreadFrom = %d, want line 4 — the over-long one", read.UnreadFrom)
	}

	// THE RESUME PATH ANSWERS THE SAME, because it is the same fact about the
	// same file: the conversation comes back with everything above the line.
	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replaySessionFile: %v", err)
	}
	if len(replayed.messages) != 2 || replayed.unread != 4 {
		t.Fatalf("the resume kept %d messages and named line %d, want 2 and 4",
			len(replayed.messages), replayed.unread)
	}
	agent, agentErr := newAgent(Config{
		Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM", SessionFile: path,
	}, &scriptedCompleter{})
	if agentErr != nil {
		t.Fatalf("a session with one unreadable line refused to open: %v", agentErr)
	}
	t.Cleanup(func() { _ = agent.Close() })
	if got := agent.Transcript(); len(got) != 2 {
		t.Fatalf("the resumed conversation holds %d entries, want the two that were readable: %#v",
			len(got), got)
	}
}

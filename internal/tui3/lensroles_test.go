package tui3

import (
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

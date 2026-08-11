package head

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// answerWith runs one message through the head against a scripted decision and
// returns the message it answered. It is the shape most of this file needs: the
// model's judgment is the input, and what the head did with it is the assertion.
func answerWith(t *testing.T, graph *store.Store, session, body, response string) store.Message {
	t.Helper()
	user := postUser(t, graph, session, body)
	client := &fakeClient{responses: []string{response}}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatalf("answer %q: %v", body, err)
	}
	return user
}

// agentRepliesAfter counts what the user actually saw. Every route ends in
// exactly one visible reply; four work orders is still one sentence answered.
func agentRepliesAfter(t *testing.T, graph *store.Store, session string, seq int64) []store.Message {
	t.Helper()
	messages, err := graph.Messages(session, seq, 100)
	if err != nil {
		t.Fatalf("read thread: %v", err)
	}
	replies := make([]store.Message, 0, 2)
	for _, message := range messages {
		if message.Role == store.RoleAgent {
			replies = append(replies, message)
		}
	}
	return replies
}

// Four jobs narrating into one thread arrived at the model in one voice, and the
// model is the party that has to decide what "it" means. Every job-anchored line
// now says which job said it, by the same short name the user reads on screen —
// never an id, because deixis only resolves when both parties use one vocabulary.
func TestThreadSliceNamesTheJobThatSpoke(t *testing.T) {
	graph := openHeadStore(t)
	deliverJob(t, graph, "names", "auth-fix", "Auth token refresh",
		"patch the auth token refresh", "The refresh path now retries once.")
	deliverJob(t, graph, "names", "parser-fix", "Parser rewrite",
		"rewrite the config parser", "The parser accepts trailing commas now.")
	postUser(t, graph, "names", "how are those going?")
	latest := postUser(t, graph, "names", "and now?")

	head := New(nil, graph)
	recent, err := head.recentThread("names", latest.Seq)
	if err != nil {
		t.Fatal(err)
	}
	rendered := head.renderThread(recent)
	for _, label := range []string{"Auth token refresh", "Parser rewrite"} {
		if !strings.Contains(rendered, "["+label+"]") {
			t.Fatalf("the slice never says %q spoke:\n%s", label, rendered)
		}
	}
	if strings.Contains(rendered, "[auth-fix]") || strings.Contains(rendered, "[parser-fix]") {
		t.Fatalf("the slice attributed a line by its id rather than its name:\n%s", rendered)
	}
	// The user's own turns stay unattributed: they belong to the conversation,
	// not to any job.
	for _, line := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(line, "user: ") {
			continue
		}
		if strings.HasPrefix(line, "user [") {
			t.Fatalf("a user turn was filed under a job: %q", line)
		}
	}
}

// The board led with a flat list of every addressable node, so sixteen parts and
// four jobs arrived as twenty peers. Jobs lead now, carrying their subtree's
// counts, and a part says whose part it is.
func TestBoardLeadsWithJobsAndNamesTheOwnerOfEveryPart(t *testing.T) {
	graph := openHeadStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "issue-41", Title: "Patch auth token refresh", Brief: "patch the auth token refresh", Stage: 1},
		{ID: "issue-41-test", Parent: "issue-41", Brief: "add a regression test", Stage: 2},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "board", Intent: "work issue 41"}); err != nil {
		t.Fatal(err)
	}
	board := New(nil, graph).boardFor("board", "", nil, time.Now())

	jobLine, partLine := "", ""
	for _, line := range strings.Split(board, "\n") {
		switch {
		case strings.HasPrefix(line, "- issue-41 |"):
			jobLine = line
		case strings.HasPrefix(line, "- issue-41-test |"):
			partLine = line
		}
	}
	if !strings.Contains(jobLine, "queued") {
		t.Fatalf("the job row carries no rolled-up counts: %q\n%s", jobLine, board)
	}
	if !strings.Contains(partLine, "part of Patch auth token refresh") {
		t.Fatalf("the part never says whose part it is: %q\n%s", partLine, board)
	}
	if strings.Index(board, "- issue-41 |") > strings.Index(board, "- issue-41-test |") {
		t.Fatalf("a part was rendered above its own job:\n%s", board)
	}
}

// A running job speaks every two minutes. Three of them filled a twenty-message
// window in about thirteen minutes, and the user's own words fell out of a
// conversation they had never left. The person's turns are windowed on their
// own now, and no amount of narration can evict one.
func TestNarratorFloodCannotEvictTheUsersOwnWords(t *testing.T) {
	graph := openHeadStore(t)
	const asked = "find me somewhere in Lisbon that isn't too fancy"
	postUser(t, graph, "flood", asked)
	for _, id := range []string{"trip", "letter", "spreadsheet"} {
		spliceSurgeryJob(t, graph, id, strings.ToUpper(id[:1])+id[1:], "work on the "+id)
	}
	// Fifteen minutes of three jobs on a two-minute heartbeat.
	for round := 0; round < 8; round++ {
		for _, id := range []string{"trip", "letter", "spreadsheet"} {
			if _, err := graph.PostMessage(store.Message{
				SessionID: "flood", Role: store.RoleAgent, NodeID: id,
				Body: fmt.Sprintf("still working on the %s (round %d)", id, round),
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	latest := postUser(t, graph, "flood", "any luck?")

	head := New(nil, graph)
	recent, err := head.recentThread("flood", latest.Seq)
	if err != nil {
		t.Fatal(err)
	}
	kept := false
	ambient := 0
	for _, message := range recent {
		if message.Body == asked {
			kept = true
		}
		if strings.TrimSpace(message.NodeID) != "" {
			ambient++
		}
	}
	if !kept {
		t.Fatalf("twenty-four heartbeats evicted the user's own ask:\n%+v", recent)
	}
	if ambient > ambientWindowMax {
		t.Fatalf("ambient chatter held %d rows, over its own window of %d", ambient, ambientWindowMax)
	}
	if !strings.Contains(head.renderThread(recent), asked) {
		t.Fatal("the rendered slice lost the sentence the follow-up is deictic to")
	}
}

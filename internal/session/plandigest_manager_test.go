package session

// The conversation as the manager of a live run: what the digest in front of
// the person's sentence leads with and what words it says, every door the
// person speaks through opening on it, and the verbs of `tasks` reaching the
// rows the digest shows. Every fixture seeds the store through its own API and
// calls no model it did not script.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// armEndedAndLiveRuns is a conversation with history behind its live run: an
// ended run of eight hand-offs, stopped, in the archive the next run left behind
// (`plan.db.1`, [planArchivePaths]), and a live run holding one open task. It is
// the shape every conversation has from its second run on.
func armEndedAndLiveRuns(t *testing.T) *Agent {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	var old []plandb.TaskSpec
	for i := 1; i <= 8; i++ {
		old = append(old, plandb.TaskSpec{ID: strconv.Itoa(i), Title: "Old work " + strconv.Itoa(i)})
	}
	seedPlanStore(t, path+".1", "chat-a", old...)
	ended, err := plandb.Open(path+".1", "", "", "", "")
	if err != nil {
		t.Fatalf("open the ended run: %v", err)
	}
	if err := ended.StopRoot("stopped"); err != nil {
		t.Fatalf("end the old run: %v", err)
	}
	if err := ended.Close(); err != nil {
		t.Fatalf("close the ended run: %v", err)
	}
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "9", Title: "Live work"})
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	registerBeltRunEngine(t, &beltRunDouble{})
	armPlanStore(t, agent, path, "chat-a")
	return agent
}

// THE LIVE RUN LEADS, AND EVERY ROW SAYS THE RAIL'S WORD.
//
// The rows come oldest run first, and the digest took the first eight: with
// eight rows of an ended run behind it, a conversation was handed eight
// `cancelled` rows of work long over and not one line of the run the person was
// talking about. And the words were the store's — `cancelled`, `ready`,
// `claimed` — about rows the person's side list calls `stopped`, `running`,
// `queued`, so the two described one plan in two vocabularies.
func TestTheDigestLeadsWithTheLiveRunInTheRailsOwnWords(t *testing.T) {
	agent := armEndedAndLiveRuns(t)

	digest := agent.planDigest()
	live := strings.Index(digest, "Live work")
	if live < 0 {
		t.Fatalf("the live run's open task is not in the digest at all:\n%s", digest)
	}
	if old := strings.Index(digest, "Old work"); old >= 0 && old < live {
		t.Fatalf("an ended run's row comes before the live run's:\n%s", digest)
	}
	rail := map[string]bool{"queued": true, "running": true, "done": true, "stopped": true, "incomplete": true, "your call": true}
	for _, line := range strings.Split(digest, "\n") {
		if !strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, " · ")
		if len(fields) < 3 {
			t.Fatalf("a row carries no state word: %q", line)
		}
		if !rail[fields[2]] {
			t.Fatalf("a row says %q, which is not a word the side list draws: %q\n%s", fields[2], line, digest)
		}
	}
}

// `say` ON A ROW OF THE RUN IS A LINE TO ITS WORKER, AND `forward` SAYS WHY IT
// CANNOT BE ONE.
//
// Both used to fall through to the session tree's reader, which cannot find a
// run's rows, and came back `No task "2" in this project` about a row the
// digest had just shown the conversation. `say` is delivered the way a line to
// a run's worker is delivered — as a note — and its answer says so; `forward`
// promises to move what the task is judged by, which nothing the conversation
// holds does for a run's row, so it refuses and points at `note`.
func TestSayAndForwardOnARowOfTheLiveRunAnswerForTheRow(t *testing.T) {
	agent, path := armLiveRun(t, plandb.TaskSpec{ID: "2", Title: "Move the schema"})
	tool := agent.tasksTool()

	answer, failed, err := tool.Execute(context.Background(), json.RawMessage(`{"id":"2","say":"use postgres"}`))
	if err != nil || failed {
		t.Fatalf("say on a row of the live run failed (%v): %q", err, answer)
	}
	if strings.Contains(answer, "No task") {
		t.Fatalf("say on a row of the live run denied the row exists: %q", answer)
	}
	if !strings.Contains(answer, "note") {
		t.Fatalf("say's answer does not say its line went as a note: %q", answer)
	}
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	notes := store.Notes("2", 0)
	_ = store.Close()
	if len(notes) != 1 || notes[0].Body != "use postgres" || notes[0].Agent != plandb.NoteAgentChat {
		t.Fatalf("the row carries %+v, want the one line the conversation said, in its own voice", notes)
	}

	answer, failed, err = tool.Execute(context.Background(), json.RawMessage(`{"id":"2","forward":true}`))
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	if !failed {
		t.Fatalf("forward on a row of the live run was accepted: %q", answer)
	}
	if strings.Contains(answer, "No task") || !strings.Contains(answer, "`note`") {
		t.Fatalf("forward's refusal does not say what it cannot do and point at note: %q", answer)
	}
}

// armDigestOn puts a live run behind an agent some other fixture built, so a
// door that needs its own shape of agent — a standing store, a sighted model —
// can be asked whether it opens on the rows.
func armDigestOn(t *testing.T, agent *Agent) {
	t.Helper()
	path := filepath.Join(t.TempDir(), planStoreFilename)
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "2", Title: "Move the schema"})
	registerBeltRunEngine(t, &beltRunDouble{})
	armPlanStore(t, agent, path, "chat-a")
}

// A DRAFT MARKED STANDING OPENS ON THE ROWS TOO. It is a sentence the person
// said while work ran, and it can make a running row wrong as surely as any
// other; the digest reached the plain sentence only.
func TestAMarkedDraftOpensOnTheRunsRows(t *testing.T) {
	store := newFakeStanding(t)
	completer := &scriptedCompleter{steps: []step{finalText("that cannot stand")}}
	agent := standingAgent(t, completer, store, nil)
	armDigestOn(t, agent)

	events, err := agent.SubmitStanding(context.Background(), "what time is it?")
	if err != nil {
		t.Fatalf("SubmitStanding: %v", err)
	}
	drainAnsweringStanding(t, events, nil)
	opening := firstUserText(t, completer)
	if !strings.Contains(opening, planDigestHeading) || !strings.Contains(opening, "Move the schema") {
		t.Fatalf("a marked draft did not open on the run's rows:\n%s", opening)
	}
	if !strings.Contains(opening, standingMarkInstruction) || !strings.HasSuffix(opening, "what time is it?") {
		t.Fatalf("the digest displaced the mark or the sentence:\n%s", opening)
	}
}

// A MESSAGE WITH PICTURES OPENS ON THE ROWS TOO, and its pictures still ride
// with it. A screenshot is often the very thing that changes the plan.
func TestAMessageWithPicturesOpensOnTheRunsRows(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, withVision)
	armDigestOn(t, agent)

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	events, err := agent.SubmitImage(ctx, "this page is wrong", []Image{{Path: writeImage(t, workspace, "shot.png", "PNG")}})
	if err != nil {
		t.Fatalf("SubmitImage: %v", err)
	}
	collect(t, events)
	request := completer.request(0)
	if len(request) == 0 {
		t.Fatal("the model was never called")
	}
	sent := request[len(request)-1]
	if words := messageContentText(sent); !strings.Contains(words, planDigestHeading) || !strings.Contains(words, "this page is wrong") {
		t.Fatalf("a message with pictures did not open on the run's rows: %q", words)
	}
	if len(imagePartURLs(sent)) != 1 {
		t.Fatalf("the picture did not ride with the digested message: %+v", sent.Content)
	}
}

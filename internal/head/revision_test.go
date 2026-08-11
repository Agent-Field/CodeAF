package head

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A cue alone is conversation and an anchor alone is a topic; only both
// together, over work that is actually live, is a redirection.
func TestRedirectRecognitionNeedsCueAnchorAndLiveWork(t *testing.T) {
	tests := []struct {
		name    string
		message string
		jobs    bool
		cue     string
		fires   bool
	}{
		{"correction", "no, use the v2 API not v1", true, "correction", true},
		{"correction without comma", "actually the API client should speak v2", true, "correction", true},
		{"scope add", "also cover the API client error paths", true, "scope-add", true},
		{"scope add while you're at it", "while you're at it, sign the API client releases", true, "scope-add", true},
		{"scope cut", "don't bother with the v1 API fallback", true, "scope-cut", true},
		{"redirect", "focus on the v2 API instead", true, "redirect", true},
		{"deictic anchor", "skip the second half of the job", true, "scope-cut", true},
		{"cue without anchor", "also water the plants", true, "", false},
		{"anchor without cue", "how is the API client coming along", true, "", false},
		{"no live work", "no, use the v2 API not v1", false, "", false},
		{"plain thanks", "thanks, that helps", true, "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			graph := openHeadStore(t)
			if test.jobs {
				spliceSurgeryJob(t, graph, "api-client", "v1 API client", "write a client for the v1 API")
			}
			// The reading survives; its authority does not. Both halves are
			// asserted: the cue class the vocabulary reads, and — for the
			// sentences that used to fire terminally — that the reading reaches
			// the loop naming the live job it is about, so nothing that used to
			// resolve is now invisible to it.
			head := New(&fakeClient{}, graph)
			user := store.Message{SessionID: "recognize", Role: store.RoleUser, Body: test.message}
			cue, cued := redirectCue(test.message)
			if !cued {
				cue = ""
			}
			if test.fires && cue != test.cue {
				t.Fatalf("cue = %q, want %q", cue, test.cue)
			}
			active, err := head.activeUserJobs()
			if err != nil {
				t.Fatal(err)
			}
			readings := head.renderHints(user, active)
			named := strings.Contains(readings, "api-client")
			if test.fires && !named {
				t.Fatalf("a sentence that used to resolve reaches the loop naming nothing:\n%s", readings)
			}
			if !test.jobs && named {
				t.Fatalf("a reading named live work on a graph with none:\n%s", readings)
			}
		})
	}
}

// Surgery's vocabulary stays surgery's. "cancel" is a withdrawal, not a
// redirection, even when the sentence would otherwise anchor beautifully.
func TestSurgeryVocabularyWinsOverRedirection(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "api-client", "v1 API client", "write a client for the v1 API")
	user, err := graph.PostMessage(store.Message{
		SessionID: "collide", Role: store.RoleUser, Body: "cancel the v1 API client",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandCancel {
		t.Fatalf("surgery lost the collision: %+v err=%v", commands, err)
	}
}

// One live job is not ambiguity: the words go to it and the receipt says so.
func TestOneLiveJobIsRedirectedWithoutAsking(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "api-client", "v1 API client", "write a client for the v1 API")
	user, err := graph.PostMessage(store.Message{
		SessionID: "single", Role: store.RoleUser, Body: "no, use the v2 API not v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := New(&fakeClient{}, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandRedirect ||
		commands[0].Target != "api-client" || commands[0].Instruction != "no, use the v2 API not v1" {
		t.Fatalf("redirect command = %+v err=%v", commands, err)
	}
	questions, err := graph.UnresolvedQuestions(10)
	if err != nil || len(questions) != 0 {
		t.Fatalf("single live job asked anyway: %+v err=%v", questions, err)
	}
}

// Two live jobs and only a pronoun to go on: one structured question, the
// best-ranked job as the default, and nothing edited until the user answers.
func TestTwoLiveJobsWithWeakAnchorAskOnceWithTheRankedDefault(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "audio-en", "English audio", "produce the English audio")
	spliceSurgeryJob(t, graph, "audio-fr", "French audio", "produce the French audio")
	user, err := graph.PostMessage(store.Message{
		SessionID: "ambiguous", Role: store.RoleUser, Body: "also include an intro chime in that job",
	})
	if err != nil {
		t.Fatal(err)
	}
	conversational := New(&fakeClient{}, graph)
	if err := conversational.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("ambiguous redirection edited a plan: %+v", commands)
	}
	questions, err := graph.UnresolvedQuestions(10)
	if err != nil || len(questions) != 1 {
		t.Fatalf("questions = %+v err=%v", questions, err)
	}
	question := questions[0]
	if question.Category != store.QuestionCategoryRedirectTarget || question.DefaultAnswer != "1" ||
		len(question.Options) != 3 || !strings.Contains(question.Text, `"kind":"choose"`) {
		t.Fatalf("redirect question = %+v", question)
	}
	if !strings.HasPrefix(question.Options[0].Label, "apply it to ") ||
		question.Options[2].Label != "start it as new work" {
		t.Fatalf("redirect options = %+v", question.Options)
	}

	answer, err := graph.PostMessage(store.Message{SessionID: "ambiguous", Role: store.RoleUser, Body: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := conversational.answer(context.Background(), answer); err != nil {
		t.Fatal(err)
	}
	_, defaulted, _, ok := store.DecodeRedirectOption(question.Options[0].Value)
	if !ok {
		t.Fatalf("default option = %+v", question.Options[0])
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandRedirect ||
		commands[0].Target != defaulted ||
		commands[0].Instruction != "also include an intro chime in that job" {
		t.Fatalf("answered redirect = %+v err=%v", commands, err)
	}
}

// "Start it as new work" is the other half of the same question, and it must
// reach the ordinary splice path with the user's words intact.
func TestRedirectQuestionCanStartTheWordsAsNewWork(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "audio-en", "English audio", "produce the English audio")
	spliceSurgeryJob(t, graph, "audio-fr", "French audio", "produce the French audio")
	conversational := New(&fakeClient{}, graph)
	user, err := graph.PostMessage(store.Message{
		SessionID: "new-work", Role: store.RoleUser, Body: "also include an intro chime in that job",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := conversational.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	answer, err := graph.PostMessage(store.Message{SessionID: "new-work", Role: store.RoleUser, Body: "3"})
	if err != nil {
		t.Fatal(err)
	}
	if err := conversational.answer(context.Background(), answer); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandSplice ||
		commands[0].Instruction != "also include an intro chime in that job" {
		t.Fatalf("new-work answer = %+v err=%v", commands, err)
	}
}

// The message that cost a running job a racing duplicate, verbatim. Every cue
// list declined it and every score was zero — "review the changes" borrows no
// word from a job about middleware — while the one signal that mattered was
// sitting in plain sight: that job had just spoken.
const adjacentReviewAsk = "make sure you review the changes and check for bugs or security vul introduced as well"

// seedSpeakingJob is the shape of the failure: one job of the user's, running,
// whose own progress line is the last thing said before the user types.
func seedSpeakingJob(t *testing.T, graph *store.Store, session string) {
	t.Helper()
	spliceSurgeryJob(t, graph, "middleware", "Request logging middleware",
		"add gin logger middleware to server.go and commit and push it")
	startNode(t, graph, "middleware")
	if _, err := graph.PostMessage(store.Message{
		SessionID: session, Role: store.RoleAgent, NodeID: "middleware",
		Body: "Wired the logger into server.go — writing the middleware tests now.",
	}); err != nil {
		t.Fatal(err)
	}
}

// End to end over the live failure: the belt opens on adjacency alone, reads
// the board, and revises the running job. Nothing new races it.
func TestWorkRaisedBesideARunningJobRevisesItRatherThanRacingIt(t *testing.T) {
	graph := openHeadStore(t)
	session := "adjacent"
	seedSpeakingJob(t, graph, session)
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolBoard, map[string]any{})}},
		{calls: []ai.ToolCall{beltCall("c2", beltToolRevise, map[string]any{
			"job": "middleware", "words": adjacentReviewAsk})}},
		{text: "Adding the review before it commits."},
	}}
	user := postUser(t, graph, session, adjacentReviewAsk)
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}

	if _, tooled := client.counts(); tooled == 0 {
		t.Fatal("the belt never opened: no tooled call was made")
	}
	if opening := client.openingPrompt(); !strings.Contains(opening, "Board (the user's live work):") ||
		!strings.Contains(opening, "middleware") {
		t.Fatalf("the loop opened without the board: %q", opening)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0].Kind != store.CommandRedirect ||
		commands[0].Target != "middleware" || commands[0].Instruction != adjacentReviewAsk {
		t.Fatalf("adjacent work did not steer the running job: %+v", commands)
	}
	for _, command := range commands {
		if command.Kind == store.CommandSplice {
			t.Fatalf("a second job was spliced beside the running one: %+v", command)
		}
	}
}

// When the belt honestly finds this is separate work, it still may not race:
// the splice carries the job it arrived beside, and continuity turns that into
// a wait rather than a parallel edit of the same thing.
func TestNewWorkBesideARunningJobIsSplicedBehindIt(t *testing.T) {
	graph := openHeadStore(t)
	session := "adjacent-new"
	seedSpeakingJob(t, graph, session)
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolSpawn, map[string]any{
			"instruction": adjacentReviewAsk, "after": "middleware"})}},
		{text: "On it — it follows the work already underway."},
	}}
	user := postUser(t, graph, session, adjacentReviewAsk)
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	// The adjacency reading is what tells the loop which job this follows, and it
	// is in the prompt rather than applied behind its back.
	if opening := client.openingPrompt(); !strings.Contains(opening,
		"the last thing said in this conversation was middleware") {
		t.Fatalf("the adjacency reading never reached the loop:\n%s", opening)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandSplice {
		t.Fatalf("commands = %+v err=%v", commands, err)
	}
	if commands[0].Target != "middleware" {
		t.Fatalf("spliced work did not name the job it arrived beside: %+v", commands[0])
	}
}

// Adjacency is a claim about the current breath of a conversation, so it is
// bounded twice — by how long ago the job spoke, and by how much has been said
// since. Past either bound, position proves nothing.
func TestAdjacencyIsBoundedByQuietAndByTheThreadWindow(t *testing.T) {
	graph := openHeadStore(t)
	session := "stale"
	seedSpeakingJob(t, graph, session)
	conversational := New(&beltClient{}, graph)
	active, err := conversational.activeUserJobs()
	if err != nil || len(active) != 1 {
		t.Fatalf("active = %+v err=%v", active, err)
	}

	fresh := postUser(t, graph, session, adjacentReviewAsk)
	if _, adjoins, err := conversational.adjacencyTarget(fresh, active); err != nil || !adjoins {
		t.Fatalf("a line said a moment ago is not adjacent: adjoins=%t err=%v", adjoins, err)
	}
	late := fresh
	late.Time = fresh.Time.Add(AdjacencyQuiet + time.Minute)
	if _, adjoins, err := conversational.adjacencyTarget(late, active); err != nil || adjoins {
		t.Fatalf("a line older than the quiet window still anchored: adjoins=%t err=%v", adjoins, err)
	}

	for index := 0; index < AdjacencyMessageWindow; index++ {
		if _, err := graph.PostMessage(store.Message{
			SessionID: session, Role: store.RoleAgent,
			Body: fmt.Sprintf("unrelated line %d", index),
		}); err != nil {
			t.Fatal(err)
		}
	}
	buried := postUser(t, graph, session, adjacentReviewAsk)
	if _, adjoins, err := conversational.adjacencyTarget(buried, active); err != nil || adjoins {
		t.Fatalf("a line pushed out of the window still anchored: adjoins=%t err=%v", adjoins, err)
	}
	// And the reading the loop is handed says nothing about a job that stopped
	// speaking, so position cannot resolve a referent it no longer supports.
	if readings := conversational.renderHints(buried, active); strings.Contains(readings,
		"the last thing said in this conversation") {
		t.Fatalf("a job pushed out of the window is still offered as the referent:\n%s", readings)
	}
}

// Adjacency is a candidate, never a veto. When the user's own words name one
// job and the conversation points at another, both readings are good and the
// one structured question this path is allowed settles it — with the words
// leading, because they are the more deliberate signal.
func TestDecisiveWordsBeatAdjacencyByAskingRatherThanBySilence(t *testing.T) {
	graph := openHeadStore(t)
	session := "disagree"
	spliceSurgeryJob(t, graph, "api-client", "v1 API client", "write a client for the v1 API")
	spliceSurgeryJob(t, graph, "audio", "English audio", "produce the English audio")
	startNode(t, graph, "audio")
	if _, err := graph.PostMessage(store.Message{
		SessionID: session, Role: store.RoleAgent, NodeID: "audio",
		Body: "Half the takes are rendered.",
	}); err != nil {
		t.Fatal(err)
	}
	conversational := New(&beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolAsk, map[string]any{
			"question": "Apply that to the v1 API client, or to the English audio?",
			"options":  []string{"v1 API client", "English audio"},
		})}},
		{text: ""},
	}}, graph)
	user := postUser(t, graph, session, "actually the API client should speak v2")
	active, err := conversational.activeUserJobs()
	if err != nil {
		t.Fatal(err)
	}
	// Both readings reach the loop, and the words lead: they are the more
	// deliberate signal. Neither is applied, because a disagreement settled
	// silently is the wrong plan edited without anybody being asked.
	readings := conversational.renderHints(user, active)
	words := strings.Index(readings, "the words rank against")
	adjacent := strings.Index(readings, "the last thing said in this conversation")
	if words < 0 || adjacent < 0 {
		t.Fatalf("a disagreement did not reach the loop as two readings:\n%s", readings)
	}
	if words > adjacent {
		t.Fatalf("the conversation's pointer outranked the user's own words:\n%s", readings)
	}
	if !strings.Contains(readings[words:adjacent], "api-client") {
		t.Fatalf("the named job is not the one the words reached:\n%s", readings)
	}

	if err := conversational.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("a disagreement edited a plan: %+v", commands)
	}
	messages, err := graph.Messages(session, user.Seq, 10)
	if err != nil || len(messages) != 1 || len(messages[0].Options) != 2 {
		t.Fatalf("the user was never asked with options: %+v err=%v", messages, err)
	}
}

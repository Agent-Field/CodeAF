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

// recognizeRedirect is gone. The reading it performed survives in two pieces the
// loop is handed rather than obeyed: redirectCue says which of the five ways a
// person changes work already underway this sentence is, and renderHints says
// which live job the words or the conversation point at. The act it used to
// perform survives as revise and expedite. So the law below is stated against
// the reading, and the acts are driven as tools.

// redirectReading is the recognizer's verdict recomposed from the two arms that
// replaced it: a redirection needs a cue, an anchor, and live work to be about.
// It returns the cue whether or not it fires, because the demotion is precisely
// that a cue on its own no longer decides anything.
func redirectReading(t *testing.T, head *Head, user store.Message) (cue string, fires bool) {
	t.Helper()
	active, err := head.activeUserJobs()
	if err != nil {
		t.Fatal(err)
	}
	cue, cued := redirectCue(user.Body)
	if !cued {
		cue = ""
	}
	reading := head.renderHints(user, active)
	anchored := strings.Contains(reading, "the words rank against:") ||
		strings.Contains(reading, "points at work already underway without naming it")
	return cue, cue != "" && anchored && len(active) > 0
}

// askedQuestion is the durable numbered question one turn posted: an agent
// message carrying option rows, which is what the surfaces render as a row a
// person clicks rather than a sentence they retype.
func askedQuestion(t *testing.T, graph *store.Store, session string, afterSeq int64) store.Message {
	t.Helper()
	messages, err := graph.Messages(session, afterSeq, 0)
	if err != nil {
		t.Fatalf("read thread: %v", err)
	}
	for _, message := range messages {
		if message.Role == store.RoleAgent && len(message.Options) > 0 {
			return message
		}
	}
	t.Fatalf("nothing in %s after %d asked a numbered question: %+v", session, afterSeq, messages)
	return store.Message{}
}

// A cue alone is conversation and an anchor alone is a topic; only both
// together, over work that is actually live, is a redirection.
func TestRedirectRecognitionNeedsCueAnchorAndLiveWork(t *testing.T) {
	tests := []struct {
		name    string
		message string
		jobs    bool
		cue     string
		fires   bool
		// named is whether the words themselves reach the job, as opposed to
		// pointing at it deictically. Only a named anchor can be acted on without
		// resolving a referent first.
		named bool
	}{
		{"correction", "no, use the v2 API not v1", true, "correction", true, true},
		{"correction without comma", "actually the API client should speak v2", true, "correction", true, true},
		{"scope add", "also cover the API client error paths", true, "scope-add", true, true},
		{"scope add while you're at it", "while you're at it, sign the API client releases", true, "scope-add", true, true},
		{"scope cut", "don't bother with the v1 API fallback", true, "scope-cut", true, true},
		{"redirect", "focus on the v2 API instead", true, "redirect", true, true},
		{"deictic anchor", "skip the second half of the job", true, "scope-cut", true, false},
		// A cue with nothing to be about is conversation: the reading is offered
		// and it anchors on nothing, which is exactly why it may not act.
		{"cue without anchor", "also water the plants", true, "scope-add", false, false},
		{"anchor without cue", "how is the API client coming along", true, "", false, true},
		{"no live work", "no, use the v2 API not v1", false, "correction", false, false},
		{"plain thanks", "thanks, that helps", true, "", false, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			graph := openHeadStore(t)
			if test.jobs {
				spliceSurgeryJob(t, graph, "api-client", "v1 API client", "write a client for the v1 API")
			}
			head := New(&fakeClient{}, graph)
			user := postUser(t, graph, "recognize", test.message)
			cue, anchored := redirectReading(t, head, user)
			fires := cue != "" && anchored
			if fires != test.fires || cue != test.cue {
				t.Fatalf("fires/cue = %t/%q, want %t/%q", fires, cue, test.fires, test.cue)
			}
			// The named arm is what a single live job resolves through, and it must
			// still name the job by id so the tool that acts needs no second read.
			active, err := head.activeUserJobs()
			if err != nil {
				t.Fatal(err)
			}
			reading := head.renderHints(user, active)
			if named := strings.Contains(reading, "the words rank against: api-client"); named != test.named {
				t.Fatalf("named = %t, want %t:\n%s", named, test.named, reading)
			}
		})
	}
}

// Surgery's vocabulary stays surgery's. "cancel" is a withdrawal, not a
// redirection, even when the sentence would otherwise anchor beautifully — and
// the reading says so before anything acts, so the collision never reaches a
// verb that could edit a plan.
func TestSurgeryVocabularyWinsOverRedirection(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "api-client", "v1 API client", "write a client for the v1 API")
	head := New(&fakeClient{}, graph)
	user := postUser(t, graph, "collide", "cancel the v1 API client")

	active, err := head.activeUserJobs()
	if err != nil {
		t.Fatal(err)
	}
	reading := head.renderHints(user, active)
	if !strings.Contains(reading, `carries the verb "cancel" aimed at existing work`) {
		t.Fatalf("the withdrawal verb was not read as one:\n%s", reading)
	}
	for _, redirection := range []string{"reads as a correction", "reads as an addition",
		"reads as a cut", "reads as a redirection"} {
		if strings.Contains(reading, redirection) {
			t.Fatalf("a withdrawal was also read as a redirection (%q):\n%s", redirection, reading)
		}
	}

	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolControl, map[string]any{
			"verb": "cancel", "ids": []string{"api-client"}})}},
		{text: ""},
	}}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandCancel {
		t.Fatalf("surgery lost the collision: %+v err=%v", commands, err)
	}
}

// One live job is not ambiguity: the words go to it and nothing is asked.
func TestOneLiveJobIsRedirectedWithoutAsking(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "api-client", "v1 API client", "write a client for the v1 API")
	const words = "no, use the v2 API not v1"
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolRevise, map[string]any{
			"job": "api-client", "words": words})}},
		{text: ""},
	}}
	user := postUser(t, graph, "single", words)
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandRedirect ||
		commands[0].Target != "api-client" || commands[0].Instruction != words {
		t.Fatalf("redirect command = %+v err=%v", commands, err)
	}
	questions, err := graph.UnresolvedQuestions(10)
	if err != nil || len(questions) != 0 {
		t.Fatalf("single live job asked anyway: %+v err=%v", questions, err)
	}
}

// Two live jobs and only a pronoun to go on: one numbered question, nothing
// edited until the user answers, and the answer carries the ORIGINAL words into
// the revision rather than the digit that settled the referent.
//
// The question is the ask tool now rather than a recognizer's askback. It is
// still durable and still a row a person clicks, and — the property this test
// exists for — the answer comes back to the loop instead of being applied by the
// question machinery, because only the party that asked knows what it was for.
func TestTwoLiveJobsWithWeakAnchorAskOnceWithTheRankedDefault(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "audio-en", "English audio", "produce the English audio")
	spliceSurgeryJob(t, graph, "audio-fr", "French audio", "produce the French audio")
	const words = "also include an intro chime in that job"

	asking := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolAsk, map[string]any{
			"question": "Which one do you mean?",
			"options":  []string{"English audio", "French audio"}})}},
	}}
	user := postUser(t, graph, "ambiguous", words)
	if err := New(asking, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	// The reading that made this ambiguous is in the prompt: a pronoun pointing
	// at live work it does not name.
	if opening := asking.openingPrompt(); !strings.Contains(opening,
		"points at work already underway without naming it") {
		t.Fatalf("the loop was not told the referent was unresolved:\n%s", opening)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("ambiguous redirection edited a plan: %+v", commands)
	}
	question := askedQuestion(t, graph, "ambiguous", user.Seq)
	if len(question.Options) != 2 || question.Options[0].Label != "English audio" ||
		question.Options[1].Label != "French audio" {
		t.Fatalf("the question does not offer the jobs by their own names: %+v", question.Options)
	}
	if !isAskQuestion(question.Options) {
		t.Fatalf("the question was not minted by the loop, so its answer would never come back: %+v", question.Options)
	}
	if !strings.Contains(question.Body, `"kind":"choose"`) {
		t.Fatalf("the question is not a row the person can click: %q", question.Body)
	}

	answering := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c2", beltToolRevise, map[string]any{
			"job": "audio-en", "words": words})}},
		{text: ""},
	}}
	answer := postUser(t, graph, "ambiguous", "1")
	if err := New(answering, graph).answer(context.Background(), answer); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandRedirect ||
		commands[0].Target != "audio-en" || commands[0].Instruction != words {
		t.Fatalf("answered redirect = %+v err=%v", commands, err)
	}
	// Both halves of the exchange are in front of the loop that settles it, which
	// is the whole reason the ask tool may carry no action of its own.
	if opening := answering.openingPrompt(); !strings.Contains(opening, "Which one do you mean?") ||
		!strings.Contains(opening, words) {
		t.Fatalf("the loop settling the choice could not see what it was for:\n%s", opening)
	}
}

// "Start it as new work" is the other half of the same question. The loop's own
// answer to it is spawn; the durable option that encodes it is still decoded and
// applied by applyRedirectOption, and both must carry the user's words intact.
func TestRedirectQuestionCanStartTheWordsAsNewWork(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "audio-en", "English audio", "produce the English audio")
	spliceSurgeryJob(t, graph, "audio-fr", "French audio", "produce the French audio")
	const words = "also include an intro chime in that job"

	asking := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolAsk, map[string]any{
			"question": "Which one do you mean?",
			"options":  []string{"English audio", "French audio", "start it as new work"}})}},
	}}
	user := postUser(t, graph, "new-work", words)
	if err := New(asking, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	answering := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c2", beltToolSpawn, map[string]any{"instruction": words})}},
		{text: ""},
	}}
	answer := postUser(t, graph, "new-work", "3")
	if err := New(answering, graph).answer(context.Background(), answer); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandSplice ||
		commands[0].Instruction != words {
		t.Fatalf("new-work answer = %+v err=%v", commands, err)
	}

	// The durable option keeps its own path: a surface that renders the encoded
	// choice still reaches the ordinary splice with the words unaltered.
	direct := openHeadStore(t)
	spliceSurgeryJob(t, direct, "audio-en", "English audio", "produce the English audio")
	settled := postUser(t, direct, "encoded", "3")
	handled, err := New(nil, direct).applyRedirectOption(context.Background(), settled,
		store.QuestionOption{Label: "start it as new work",
			Value: store.RedirectOptionValue("new", "", words)})
	if err != nil || !handled {
		t.Fatalf("the encoded new-work option was not applied: handled=%t err=%v", handled, err)
	}
	encoded, err := direct.PendingCommands(10)
	if err != nil || len(encoded) != 1 || encoded[0].Kind != store.CommandSplice ||
		encoded[0].Instruction != words {
		t.Fatalf("encoded new-work option = %+v err=%v", encoded, err)
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

// End to end over the live failure: the loop opens on adjacency alone, reads
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
	opening := client.openingPrompt()
	if !strings.Contains(opening, "Live board (the work you can read and act on):") ||
		!strings.Contains(opening, "middleware") {
		t.Fatalf("the loop opened without the board: %q", opening)
	}
	// Adjacency is the only signal here, so it has to be in the prompt: no cue
	// fires and no word of this sentence reaches the job's own brief.
	if !strings.Contains(opening, "the last thing said in this conversation was middleware") {
		t.Fatalf("the loop was never told which work just spoke:\n%s", opening)
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

// When the loop honestly finds this is separate work, it still may not race:
// the splice carries the job it arrived beside, so continuity turns that into a
// wait rather than a parallel edit of the same thing. That "after" used to be
// spliceContinuity guessing from adjacency; it is spawn's own argument now, set
// by the party that read the board.
func TestNewWorkBesideARunningJobIsSplicedBehindIt(t *testing.T) {
	graph := openHeadStore(t)
	session := "adjacent-new"
	seedSpeakingJob(t, graph, session)
	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolSpawn, map[string]any{
			"instruction": adjacentReviewAsk, "after": "middleware"})}},
		{text: "Queued behind the work already underway."},
	}}
	user := postUser(t, graph, session, adjacentReviewAsk)
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	commands, err := graph.PendingCommands(10)
	if err != nil || len(commands) != 1 || commands[0].Kind != store.CommandSplice {
		t.Fatalf("spawned work = %+v err=%v", commands, err)
	}
	if commands[0].Target != "middleware" {
		t.Fatalf("spliced work did not name the job it arrived beside: %+v", commands[0])
	}
	if commands[0].Instruction != adjacentReviewAsk {
		t.Fatalf("the words did not travel verbatim: %q", commands[0].Instruction)
	}
}

// Adjacency is a claim about the current breath of a conversation, so it is
// bounded twice — by how long ago the job spoke, and by how much has been said
// since. Past either bound, position proves nothing, and the reading says
// nothing about it rather than guessing.
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
	// And the reading handed to the loop is silent about it. A stale position
	// stated as evidence is the same wrong answer arriving one layer later.
	if reading := conversational.renderHints(buried, active); strings.Contains(reading, "the last thing said") {
		t.Fatalf("a job that stopped speaking is still offered as the referent:\n%s", reading)
	}
}

// Adjacency is a candidate, never a veto. When the user's own words name one
// job and the conversation points at another, both readings are good and both
// are handed over — with the words leading, because they are the more deliberate
// signal — and one plain question settles it. Nothing is journaled first.
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
	head := New(&fakeClient{}, graph)
	user := postUser(t, graph, session, "actually the API client should speak v2")

	active, err := head.activeUserJobs()
	if err != nil {
		t.Fatal(err)
	}
	reading := head.renderHints(user, active)
	words := strings.Index(reading, "the words rank against: api-client")
	spoke := strings.Index(reading, "the last thing said in this conversation was audio")
	switch {
	case words < 0:
		t.Fatalf("the job the words name was never offered:\n%s", reading)
	case spoke < 0:
		t.Fatalf("the job that spoke last was silently dropped:\n%s", reading)
	case words > spoke:
		t.Fatalf("position was offered ahead of the more deliberate signal:\n%s", reading)
	}

	client := &beltClient{turns: []beltTurn{
		{calls: []ai.ToolCall{beltCall("c1", beltToolAsk, map[string]any{
			"question": "Which one do you mean?",
			"options":  []string{"v1 API client", "English audio"}})}},
	}}
	if err := New(client, graph).answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("a disagreement edited a plan: %+v", commands)
	}
	question := askedQuestion(t, graph, session, user.Seq)
	if !isAskQuestion(question.Options) || len(question.Options) != 2 {
		t.Fatalf("the disagreement was not put to the person as a choice: %+v", question.Options)
	}
}

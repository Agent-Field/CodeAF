package head

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
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
			intent, fires, err := New(&fakeClient{}, graph).recognizeRedirect(test.message)
			if err != nil {
				t.Fatal(err)
			}
			if fires != test.fires || intent.Cue != test.cue {
				t.Fatalf("fires/cue = %t/%q, want %t/%q", fires, intent.Cue, test.fires, test.cue)
			}
			if fires && (!intent.Certain || intent.Candidates[0].Node.ID != "api-client") {
				t.Fatalf("single live job should resolve without asking: %+v", intent)
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

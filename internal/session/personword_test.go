package session

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE PERSON'S OWN WORDS REACH THE MODEL. Every door onto a turn is checked
// here, and the check is always the same one: the sentence somebody typed is in
// the request the provider was actually sent, carried by a user-role message.
//
// It is written as a family because of the shape of the defect it guards. A
// session that loses the sentence loses it BETWEEN two places that both look
// right on their own — the journal, which is written from the same value, and
// the transcript the model reads — so a test that asserted the journal, or
// asserted a.messages, would have passed while a person watched the model
// answer a question nobody asked. The only assertion worth making is the one
// made against the request that left.
//
// The last case runs over the real adapter, so the assertion is made on the
// bytes rather than on the []ai.Message the loop handed over: the seam between
// those two is a place a message has been reshaped before (internal/provider's
// sanitizeMessages, the cache breakpoints in caching.go), and a scripted
// completer cannot see it.

// personWords is the sentence every case in this file sends. It is deliberately
// a real question with real punctuation and a real misspelling, because the
// defect this file exists for was reported against exactly such a sentence.
const personWords = "what are citadel's and jane streeets recent non trivial bets?"

// tailUserText is the words of the LAST user-role message of a request, which is
// the one the model answers. Empty means the request ended without anybody
// having asked anything — the shape a turn takes when the sentence is lost.
func tailUserText(messages []ai.Message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if strings.EqualFold(messages[index].Role, "user") {
			return messageContentText(messages[index])
		}
	}
	return ""
}

// carriesPersonWords fails the test when no request in the script ends on the
// person's sentence. It looks across every request of the turn rather than at
// one of them because a turn is several steps and the sentence has to be in
// front of the model at at least the one that answers it.
func carriesPersonWords(t *testing.T, completer *scriptedCompleter, words, where string) {
	t.Helper()
	for index := 0; index < completer.requests(); index++ {
		if strings.Contains(tailUserText(completer.request(index)), words) {
			return
		}
	}
	for index := 0; index < completer.requests(); index++ {
		t.Logf("request %d: roles %v, last user message %q",
			index, rolesOf(completer.request(index)), tailUserText(completer.request(index)))
	}
	t.Fatalf("%s: no request ended on the person's own words (%q)", where, words)
}

// A plain Submit puts the sentence in front of the model. This is the floor
// every other case stands on.
func TestPersonWordsReachTheModelOnAPlainTurn(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})

	collect(t, mustSubmit(t, agent, personWords))

	carriesPersonWords(t, completer, personWords, "a plain turn")
}

// AND SO DOES A SECOND SENTENCE TYPED ON TOP OF THE FIRST ANSWER. The regression
// this file guards was reported on the second and third turns of a conversation
// as well as the first, and the transcript grows between them — the fold, the
// stub pass and the state card all run in that gap.
func TestPersonWordsReachTheModelOnEveryTurnOfAConversation(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})

	said := []string{personWords, "and what did jane street do about it?", "write that up for me"}
	for _, words := range said {
		collect(t, mustSubmit(t, agent, words))
	}

	// Each sentence has to have been the tail of some request. The last one is
	// checked by name rather than by position because the title call rides the
	// same completer and is not a step of the turn.
	for _, words := range said {
		carriesPersonWords(t, completer, words, "turn "+words)
	}
}

// STEERING IS STILL THE PERSON ASKING. A sentence typed while a turn is running
// is queued and drained at the next step boundary, and the request after that
// boundary is the one that has to carry it — otherwise it lands in the journal
// at the turn's end and no model ever reads it.
func TestPersonWordsTypedMidTurnReachTheModel(t *testing.T) {
	completer := &scriptedCompleter{}
	var agent *Agent
	steered := make(chan struct{})
	completer.steps = []step{
		// The first step calls a tool, which is what gives the turn a second
		// step for the steering to be drained into.
		func(context.Context, []ai.Message) (*ai.Response, error) {
			if _, err := agent.Submit(context.Background(), personWords); err != nil {
				t.Errorf("steering Submit: %v", err)
			}
			close(steered)
			return toolResponse("call_1", "ls", `{}`), nil
		},
	}
	var workspace string
	agent, workspace = newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})
	_ = workspace

	collect(t, mustSubmit(t, agent, "have a look around first"))
	<-steered

	carriesPersonWords(t, completer, personWords, "a sentence typed mid-turn")
}

// A FOLLOW-UP IS A TURN OF ITS OWN and reaches the model the same way. It is the
// other door a surface has onto a running session, and it queues rather than
// steers, so it is a separate path through [Agent.startTurnLocked].
func TestPersonWordsSentAsAFollowUpReachTheModel(t *testing.T) {
	completer := &scriptedCompleter{}
	var agent *Agent
	// The follow-up's own stream, taken the moment it is queued: the turn it
	// starts runs off the back of the first turn's cleanup, so this channel — and
	// not a sleep — is what says the second turn is over.
	follow := make(chan (<-chan Event), 1)
	completer.steps = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			events, err := agent.FollowUp(personWords)
			if err != nil {
				t.Errorf("FollowUp: %v", err)
			}
			follow <- events
			return textResponse("done"), nil
		},
	}
	agent, _ = newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})

	collect(t, mustSubmit(t, agent, "first question"))
	collect(t, <-follow)

	carriesPersonWords(t, completer, personWords, "a follow-up")
}

// AND A SESSION RESUMED FROM A JOURNAL THAT CARRIES USAGE LINES still reads the
// conversation out of it. The journal grew a "usage" record per turn and per
// auxiliary call, so a person's message is now separated from the reply to it by
// several lines that are not messages at all; a replay that mistook one of those
// for a turn would rebuild a transcript with holes in it, and the model would be
// answering a conversation nobody had.
func TestAResumedJournalWithUsageLinesKeepsThePersonsWords(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "session.jsonl")
	// The interleaving is the evidence file's own: the person speaks, several
	// auxiliary calls are billed, the assistant answers, and the turn's own usage
	// lands after it.
	lines := []string{
		`{"type":"session","version":1,"id":"abc123","cwd":"/tmp","model":"test/model","timestamp":"2026-08-19T16:47:50Z"}`,
		`{"type":"message","role":"user","content":` + mustJSON(t, personWords) + `,"timestamp":"2026-08-19T16:47:53Z"}`,
		`{"type":"usage","usage":{"model":"aux/one","input":636,"output":115,"costUsd":0.00003,"calls":1,"aux":true},"timestamp":"2026-08-19T16:47:55Z"}`,
		`{"type":"usage","usage":{"model":"aux/two","input":534,"output":6,"costUsd":0.00005,"calls":1,"aux":true},"timestamp":"2026-08-19T16:48:31Z"}`,
		`{"type":"message","role":"assistant","content":"here is what i found","timestamp":"2026-08-19T16:48:20Z"}`,
		`{"type":"usage","usage":{"model":"test/model","input":14182,"output":673,"cacheRead":14080,"costUsd":0.0005,"calls":1,"durationMs":37933},"timestamp":"2026-08-19T16:48:31Z"}`,
		`{"type":"usage","usage":{"model":"aux/three","input":136,"output":9,"costUsd":0.000002,"calls":1,"aux":true},"timestamp":"2026-08-19T16:48:32Z"}`,
		`{"type":"title","title":"citadel jane street bets","timestamp":"2026-08-19T16:48:32Z"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write journal: %v", err)
	}

	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = path
	})

	collect(t, mustSubmit(t, agent, "keep going"))

	// The replayed sentence is in the request, and so is the new one: a resume
	// that dropped either would be a conversation the model cannot follow.
	request := completer.request(0)
	whole := ""
	for _, message := range request {
		whole += messageContentText(message)
	}
	if !strings.Contains(whole, personWords) {
		t.Fatalf("the resumed request lost the replayed sentence; roles %v", rolesOf(request))
	}
	carriesPersonWords(t, completer, "keep going", "a resumed session")
}

// AND THE WIRE ITSELF CARRIES IT. Everything above asserts the []ai.Message the
// loop handed to the client; this asserts the JSON body that left the process,
// because between those two lie the outbound hygiene pass and the cache
// breakpoints, and either one is a place a message could be reshaped into
// something the model reads as nothing.
func TestThePersonsWordsSurviveOntoTheWire(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		payload, _ := io.ReadAll(request.Body)
		mu.Lock()
		bodies = append(bodies, string(payload))
		mu.Unlock()
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}` + "\n\n"))
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	})
	client, err := provider.NewClient(provider.Config{
		APIKey: "k", BaseURL: "http://provider.test", Model: "test/model",
		HTTPClient: fakeHTTP(handler),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	agent, _ := newTestAgent(t, client, func(config *Config) {
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})

	collect(t, mustSubmit(t, agent, personWords))
	collect(t, mustSubmit(t, agent, "and the second half of the question"))

	mu.Lock()
	defer mu.Unlock()
	// The turn's own request is the one whose body carries the belt; the title
	// call is the session's errand and is allowed to say something else.
	found := false
	for _, body := range bodies {
		var parsed struct {
			Messages []json.RawMessage `json:"messages"`
		}
		if err := json.Unmarshal([]byte(body), &parsed); err != nil {
			t.Fatalf("the request body is not JSON: %v", err)
		}
		if len(parsed.Messages) == 0 {
			t.Fatalf("a request left with no messages at all")
		}
		var tail struct {
			Role string `json:"role"`
		}
		_ = json.Unmarshal(parsed.Messages[len(parsed.Messages)-1], &tail)
		if !strings.Contains(body, "and the second half of the question") {
			continue
		}
		found = true
		if !strings.EqualFold(tail.Role, "user") {
			t.Fatalf("the request ends on a %q message rather than on the person's", tail.Role)
		}
		if !strings.Contains(string(parsed.Messages[len(parsed.Messages)-1]), "and the second half of the question") {
			t.Fatalf("the last message on the wire is not the person's newest sentence: %s",
				parsed.Messages[len(parsed.Messages)-1])
		}
		if !strings.Contains(body, personWords[:20]) {
			t.Fatalf("the wire lost the first turn's sentence")
		}
	}
	if !found {
		t.Fatal("no request body carried the person's second sentence")
	}
}

func mustJSON(t *testing.T, text string) string {
	t.Helper()
	encoded, err := json.Marshal(text)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(encoded)
}

// AND A SENTENCE TYPED IN THE LAST SECONDS OF A TURN IS STILL ANSWERED.
//
// This is the defect this file was written for. A person who types while the
// model is answering is steering, and steering lands at the next STEP boundary —
// but a turn whose last request has already gone out has no next step. The end
// drain records what is left under the same lock that clears `running`
// ([Agent.startTurnLocked]'s cleanup), so the sentence reaches a.messages AND
// the journal, correctly, in the person's own words — and nothing ever asks the
// model about it. The transcript shows the question; the session goes idle.
//
// What it looked like from the outside: the message drawn on screen, in the
// journal, and then either silence or — after the person typed it again, which
// is what anybody does — a reply that reads as though the first one was never
// there.
func TestASentenceTypedInTheLastSecondsOfATurnIsStillAnswered(t *testing.T) {
	completer := &scriptedCompleter{}
	var agent *Agent
	typed := make(chan struct{})
	completer.steps = []step{
		// The turn's LAST request. Anything queued from here has no step
		// boundary left to land at.
		func(context.Context, []ai.Message) (*ai.Response, error) {
			if _, err := agent.Submit(context.Background(), personWords); err != nil {
				t.Errorf("Submit while the turn ran: %v", err)
			}
			close(typed)
			return textResponse("here is the first answer"), nil
		},
	}
	journalPath := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ = newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = journalPath
	})

	// The steering Submit subscribes to the running turn's hub, so draining that
	// stream is what waits for the turn — and for the turn the drain owes.
	first := mustSubmit(t, agent, "start something")
	<-typed
	collect(t, first)
	waitForQuiet(t, agent)

	// The journal has it — that half was never broken.
	journal := readJournalText(t, journalPath)
	if !strings.Contains(journal, personWords) {
		t.Fatalf("the journal lost the person's words, which is a different defect: %s", journal)
	}
	// And so must some request. This is the assertion that failed.
	carriesPersonWords(t, completer, personWords, "a sentence typed in the last seconds of a turn")
}

// waitForQuiet waits for the session to stop running, so a test can assert about
// a turn the drain started on its own rather than about the one it submitted.
func waitForQuiet(t *testing.T, agent *Agent) {
	t.Helper()
	for attempt := 0; attempt < 400; attempt++ {
		agent.mu.Lock()
		running := agent.running
		agent.mu.Unlock()
		if !running && attempt > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the session never went quiet")
}

// readJournalText is the whole session file as one string: enough to ask whether
// a sentence reached it at all.
func readJournalText(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	return string(raw)
}

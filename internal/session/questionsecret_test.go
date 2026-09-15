package session

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/connect"
)

// theKey is the exact string a person pastes into a connect box. It is written
// once so every assertion below is about the SAME bytes, and it is shaped like
// a real provider key so a partial leak — the first characters rendered into a
// line, the tail kept in a label — fails this test as loudly as the whole of it.
const theKey = "sk-or-v1-0fd41c9ae3b74d2f8a6e5b1c7d9e0f2a3b4c5d6e7f8091a2b3c4d5e6f70819"

// A SECRET'S WORDS GO TO THE LANE THAT ASKED FOR THEM AND NOWHERE ELSE.
//
// A question whose [InputShape.Secret] is set is answered with a CREDENTIAL, and
// the four places an answer ordinarily goes are four places a key must never
// reach: the decision file on disk, the questions lane every window and every
// `--host` frame reads, the preference an explained override would write, and
// message[0] — which is the very next thing sent to the provider.
//
// Observed on 2026-09-11 before the fix: an OpenRouter key typed into the
// connect box was written to decisions.jsonl as the answer's `change`, rendered
// into the system prompt's record section as `with: sk-or-…`, sent to the
// provider on the next request, and carried whole on EventQuestionAnswered to
// every attached surface.
func TestASecretAnswerReachesTheLaneAndNothingElse(t *testing.T) {
	agent, dir := questionSession(t, "sscr1111sscr1111", func(config *Config) { config.Interactive = true })
	hub := watched(agent)
	go func() {
		for range hub {
		}
	}()
	asks, stopAsking := agent.WatchQuestions()
	defer stopAsking()

	done := make(chan connectAnswer, 1)
	go func() {
		answer, _ := agent.askConnect(context.Background(), connectStatus{
			ID: "openrouter", Name: "OpenRouter", Auth: connect.AuthKey,
		})
		done <- answer
	}()

	raised := waitForAsk(t, asks, EventQuestion)
	if !raised.Question.SecretAnswer() {
		t.Fatalf("the connect key question does not say its answer is a secret: %+v", raised.Question.Input)
	}
	ref := raised.Question.Ref

	if err := agent.ResolveQuestion(Answer{
		Kind: QuestionConnect, Ref: ref, Change: theKey,
		// AND THE REASON BOX TOO. A person pasting a key has no idea which
		// field the engine reads it out of, and a key in the wrong one is
		// still a key on disk.
		Why: theKey,
	}); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}

	// THE LANE GOT IT. The whole point of keeping the words off every other road
	// is that the one road they were meant for still carries them; a test that
	// only proved the key was gone would pass with connect broken.
	got := <-done
	if got.key != theKey {
		t.Fatalf("the connect lane was handed %q, want the key it asked for", got.key)
	}

	// 1. THE RECORD ON DISK.
	file, err := os.ReadFile(DecisionsPath(dir))
	if err != nil {
		t.Fatalf("reading the record: %v", err)
	}
	if strings.Contains(string(file), theKey) {
		t.Fatalf("the key was written to decisions.jsonl:\n%s", file)
	}
	// AND THE RECORD IS STILL A RECORD. Something was decided and the file says
	// so — dropping the line altogether would be the other way to pass this test
	// and would lose the fact that an account was connected at all.
	decisions := agent.Decisions()
	if len(decisions) == 0 || !strings.Contains(decisions[len(decisions)-1].Head, "OpenRouter") {
		t.Fatalf("the answer left no record at all: %+v", decisions)
	}

	// 2. THE QUESTIONS LANE, which is also the `--host` wire frame: internal/remote
	// puts this event on the wire whole (its questionlane.go), so the bytes a
	// watcher reads here are the bytes a remote surface reads.
	answered := waitForAsk(t, asks, EventQuestionAnswered)
	frame, err := json.Marshal(answered)
	if err != nil {
		t.Fatalf("marshalling the event: %v", err)
	}
	if strings.Contains(string(frame), theKey) {
		t.Fatalf("the key rode the questions lane:\n%s", frame)
	}

	// 3. THE NEXT REQUEST. message[0] carries a snapshot of the record, so a key
	// in the record is a key in front of every message there is.
	agent.mu.Lock()
	agent.recordShown = ""
	agent.refreshSystemLocked()
	prompt := messageText(agent.messages[0])
	held := agent.recordText
	agent.mu.Unlock()
	if strings.Contains(prompt, theKey) {
		t.Fatalf("the key reached message[0]:\n%s", prompt)
	}
	if strings.Contains(held, theKey) {
		t.Fatalf("the key reached the record the prompt is rendered from:\n%s", held)
	}

	// 4. THE WHOLE SESSION FOLDER, which catches the journal and anything else a
	// lane writes beside the record without this test having to name it.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the session folder: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		body, err := os.ReadFile(dir + "/" + entry.Name())
		if err != nil {
			continue
		}
		if strings.Contains(string(body), theKey) {
			t.Fatalf("the key was written to %s", entry.Name())
		}
	}
}

package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/redact"
)

// theToken is shaped like no provider's key on purpose: internal/redact's shapes
// would take a recognisable one out by themselves, and this test must prove the
// registration covers a value nothing could recognise.
const theToken = "Zq7-runner-cred-4f81c0e2b9a7d3561fe8"

// secretHome points codeaf's state root at a folder of this test's own, so the
// kept file is a file this test can inspect and nobody else's.
func secretHome(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	return root
}

// raiseSecret starts one request_secret call and hands back the question it
// put on screen and the channel its result arrives on.
func raiseSecret(t *testing.T, agent *Agent, asks <-chan Event, args string) (Question, <-chan string) {
	t.Helper()
	done := make(chan string, 1)
	go func() {
		out, _, _ := agent.executeRequestSecret(context.Background(), json.RawMessage(args))
		done <- out
	}()
	return *waitForAsk(t, asks, EventQuestion).Question, done
}

func secretSession(t *testing.T, id string) (*Agent, string, <-chan Event) {
	t.Helper()
	agent, dir := questionSession(t, id, func(config *Config) { config.Interactive = true })
	hub := watched(agent)
	go func() {
		for range hub {
		}
	}()
	asks, stop := agent.WatchQuestions()
	t.Cleanup(stop)
	return agent, dir, asks
}

func waitResult(t *testing.T, done <-chan string) string {
	t.Helper()
	select {
	case out := <-done:
		return out
	case <-time.After(5 * time.Second):
		t.Fatal("request_secret never came back after its answer")
		return ""
	}
}

// THE MODEL GETS A PATH AND NEVER THE VALUE, and the value is where the path
// says, readable by this user alone, for a day.
func TestRequestSecretKeepsTheValueOutOfTheConversation(t *testing.T) {
	root := secretHome(t)
	agent, dir, asks := secretSession(t, "rsec1111rsec1111")

	q, done := raiseSecret(t, agent, asks, `{"label":"Repository-scoped runner credential","why":"Create a fine-grained GitHub token for codeaf-desk with Administration read and write.","placeholder":"Fine-grained GitHub token"}`)
	if !q.SecretAnswer() || q.Input.Kind != InputText {
		t.Fatalf("the question is not a masked text box: %+v", q.Input)
	}
	if q.Input.Prompt != "Fine-grained GitHub token" {
		t.Fatalf("the box's words are %q", q.Input.Prompt)
	}
	if len(q.Options) != 1 || q.Options[0].Key != SecretDeclineKey || q.Options[0].Label != SecretDeclineLabel {
		t.Fatalf("a secret question offers %+v, want the one Decline", q.Options)
	}
	if q.Pick != nil || q.Policy.Kind != PolicyAsk || !q.Deadline.IsZero() {
		t.Fatalf("a secret question carries a pick, a clock or a policy that answers for the person: %+v %+v", q.Pick, q.Policy)
	}

	if err := agent.ResolveQuestion(Answer{Kind: QuestionAsk, ID: q.ID, Change: "  " + theToken + "\n"}); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}
	out := waitResult(t, done)
	if strings.Contains(out, theToken) {
		t.Fatalf("the model was handed the value:\n%s", out)
	}
	if !strings.HasPrefix(out, "saved · ") {
		t.Fatalf("the result does not open with saved:\n%s", out)
	}

	dirSecrets := filepath.Join(root, SecretsDirName)
	entries, err := os.ReadDir(dirSecrets)
	if err != nil || len(entries) != 1 {
		t.Fatalf("want one kept file in %s, got %v (%v)", dirSecrets, entries, err)
	}
	path := filepath.Join(dirSecrets, entries[0].Name())
	if !strings.Contains(out, path) || !strings.Contains(out, `"$(cat `+path+`)"`) {
		t.Fatalf("the result does not name the file and how to read it:\n%s", out)
	}
	if !strings.HasPrefix(entries[0].Name(), "repository-scoped-runner-credential-") {
		t.Fatalf("the file is named %q, not after its label", entries[0].Name())
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != theToken {
		t.Fatalf("the file holds %q (%v), want exactly the typed value without its surrounding space", body, err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("the file is %v, want 0600", info.Mode().Perm())
	}
	if info, _ := os.Stat(dirSecrets); info.Mode().Perm() != 0o700 {
		t.Fatalf("the folder is %v, want 0700", info.Mode().Perm())
	}
	if !strings.Contains(out, "36 characters") {
		t.Fatalf("the result does not say how many characters arrived:\n%s", out)
	}

	// A COMMAND THAT PRINTS IT PRINTS [redacted]. Every tool result goes through
	// redact.Secrets before any copy is made.
	if printed := redact.Secrets("token=" + theToken); strings.Contains(printed, theToken) {
		t.Fatalf("a printed value survives redaction: %s", printed)
	}

	// AND NOTHING THE SESSION WROTE CARRIES IT: the record, the journal, the
	// presence file, all of it.
	filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if data, _ := os.ReadFile(path); strings.Contains(string(data), theToken) {
			t.Errorf("the value was written to %s", path)
		}
		return nil
	})
	answered := waitForAsk(t, asks, EventQuestionAnswered)
	if frame, _ := json.Marshal(answered); strings.Contains(string(frame), theToken) {
		t.Fatalf("the value rode the questions lane:\n%s", frame)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	for _, message := range agent.messages {
		if strings.Contains(messageText(message), theToken) {
			t.Fatalf("the value is in the conversation:\n%s", messageText(message))
		}
	}
}

// DECLINE KEEPS NOTHING and says so.
func TestRequestSecretDeclinedKeepsNothing(t *testing.T) {
	root := secretHome(t)
	agent, _, asks := secretSession(t, "rsec2222rsec2222")
	q, done := raiseSecret(t, agent, asks, `{"label":"Deploy key","why":"For the release job."}`)
	if err := agent.ResolveQuestion(Answer{Kind: QuestionAsk, ID: q.ID, Key: SecretDeclineKey}); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}
	if out := waitResult(t, done); !strings.HasPrefix(out, "declined · ") {
		t.Fatalf("a decline reads:\n%s", out)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, SecretsDirName)); len(entries) != 0 {
		t.Fatalf("a decline kept %d files", len(entries))
	}
}

// `ask` WITH input.secret GETS THE FILE TOO. Before request_secret, the typed
// key came back in the answer's JSON as `change` — straight into the
// transcript and to the provider on the next request.
func TestAskWithASecretBoxHandsBackAPathNotTheKey(t *testing.T) {
	secretHome(t)
	agent, _, asks := secretSession(t, "rsec3333rsec3333")
	done := make(chan string, 1)
	go func() {
		out, _, _ := agent.executeAsk(context.Background(), json.RawMessage(`{
			"head":"Paste the staging API key","kind":"clarification","reason":"the deploy script needs it","stakes":"reversible",
			"input":{"kind":"text","secret":true}}`))
		done <- out
	}()
	q := *waitForAsk(t, asks, EventQuestion).Question
	if err := agent.ResolveQuestion(Answer{Kind: QuestionAsk, ID: q.ID, Change: theToken}); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}
	out := waitResult(t, done)
	if strings.Contains(out, theToken) || !strings.HasPrefix(out, "saved · ") {
		t.Fatalf("ask's secret answer reads:\n%s", out)
	}
}

// A SECRET ASKED BEFORE IS ASKED AGAIN. The record refuses a question it has
// already seen answered, and a token that expired is not a decision.
func TestRequestSecretMayBeAskedAgain(t *testing.T) {
	secretHome(t)
	agent, _, asks := secretSession(t, "rsec4444rsec4444")
	args := `{"label":"Runner token","why":"For the runner."}`
	for round := range 2 {
		q, done := raiseSecret(t, agent, asks, args)
		if err := agent.ResolveQuestion(Answer{Kind: QuestionAsk, ID: q.ID, Change: theToken}); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		if out := waitResult(t, done); !strings.HasPrefix(out, "saved · ") {
			t.Fatalf("round %d reads:\n%s", round, out)
		}
		waitForAsk(t, asks, EventQuestionAnswered)
	}
}

// NOBODY TO TYPE, NO VERB: a headless belt does not carry request_secret, and
// an interactive one does (shelved or not, it is this build's verb).
func TestRequestSecretIsOnlyWhereSomebodyCanAnswer(t *testing.T) {
	headless, _ := questionSession(t, "rsec5555rsec5555", func(config *Config) { config.Interactive = false })
	if hasTool(headless, RequestSecretToolName) {
		t.Fatal("a headless belt carries request_secret")
	}
	present, _ := questionSession(t, "rsec6666rsec6666", func(config *Config) { config.Interactive = true })
	if !hasTool(present, RequestSecretToolName) {
		t.Fatal("an interactive belt has no request_secret")
	}
}

// A DAY IS A DAY: the sweep removes a file past it and leaves one inside it.
func TestSecretsPastTheirDayAreSwept(t *testing.T) {
	root := secretHome(t)
	now := time.Now()
	kept, err := keepSecret("old one", "value-old-123456", now)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := keepSecret("new one", "value-new-123456", now)
	if err != nil {
		t.Fatal(err)
	}
	if !kept.Until.Equal(now.Add(SecretKeepFor)) {
		t.Fatalf("until is %v, want a day on", kept.Until)
	}
	old := now.Add(-SecretKeepFor - time.Minute)
	if err := os.Chtimes(kept.File, old, old); err != nil {
		t.Fatal(err)
	}
	sweepSecrets(now)
	if _, err := os.Stat(kept.File); !os.IsNotExist(err) {
		t.Fatalf("a file past its day survived the sweep (%v)", err)
	}
	if _, err := os.Stat(fresh.File); err != nil {
		t.Fatalf("a file inside its day was swept: %v", err)
	}
	if !strings.HasPrefix(fresh.File, filepath.Join(root, SecretsDirName)) {
		t.Fatalf("kept outside the state root: %s", fresh.File)
	}
}

func TestSecretSlug(t *testing.T) {
	for label, want := range map[string]string{
		"Repository-scoped runner credential": "repository-scoped-runner-credential",
		"  GitHub  token!! ":                  "github-token",
		"→→→":                                 "secret",
		strings.Repeat("abcdefghij", 6):       strings.Repeat("abcdefghij", 4),
	} {
		if got := secretSlug(label); got != want {
			t.Errorf("secretSlug(%q) = %q, want %q", label, got, want)
		}
	}
}

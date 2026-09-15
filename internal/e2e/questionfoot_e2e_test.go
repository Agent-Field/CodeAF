//go:build e2e

// questionfoot_e2e_test.go is #840's stub, and the one place the suite stages
// what the issue caught only by luck: a task that raises a question while its
// row is selected on the roster. The reported failure needed a real model to
// happen to call `ask` before the row was inspected — one run in four — so the
// door hint could never be tested under a waiting question without leaving the
// assertion to the model's mood. Here a scripted router answers the task
// worker's own request with an `ask` call, every run, and the roster's foot is
// read for BOTH facts at once: the row's door still standing and the question
// that waits, on the one line the foot draws them on.
//
// The needles are the suite's own law ([say], tuiwords_test.go) and the two
// halves are the pair the fix chose — the door FIRST, the note behind it — so
// this wait can never starve on a line a question displaces.
package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// The stub is addressed as a model this build already knows how to dial, the
// same road the refusal scenario takes: the state root names it, the endpoints
// page the session fetches is served by the stub, and the base URL moves the
// whole router onto it.
const questionFootModel = "stub/doorstub"

// questionFootBrain is the scripted wire: every request that was OFFERED TOOLS
// and carries the task's brief gets one `ask` call — a question about the file
// the work is to write, which is what makes it a question the TASK raised — and
// every other call gets a plain answer. A session runs calls nobody scripted
// (the conversation's title, a name for the task), and a stub that refused them
// would fail this run for reasons it is not about.
type questionFootBrain struct {
	mu     sync.Mutex
	asked  int
	server *httptest.Server
}

func newQuestionFootBrain() *questionFootBrain {
	b := &questionFootBrain{}
	mux := http.NewServeMux()
	// THE PATHS ARE THE PRODUCT'S OWN (client.go joins BaseURL with
	// `/chat/completions`, the models document with `/models`), and a stub
	// that answered on some other road would be a stub this build never
	// dialled.
	mux.HandleFunc("/models", b.models)
	mux.HandleFunc("/chat/completions", b.completions)
	mux.HandleFunc("/", http.NotFound)
	b.server = httptest.NewServer(mux)
	return b
}

func (b *questionFootBrain) close()      { b.server.Close() }
func (b *questionFootBrain) url() string { return b.server.URL }

func (b *questionFootBrain) models(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"data":[{"id":%q,"canonical_slug":%q,"name":"Scripted stub",`+
		`"context_length":200000,`+
		`"architecture":{"input_modalities":["text"],"output_modalities":["text"]},`+
		`"pricing":{"prompt":"0","completion":"0","request":"0","input_cache_read":"0"},`+
		`"supported_parameters":["tools","tool_choice","max_tokens"]}]}`,
		questionFootModel, questionFootModel)
}

func (b *questionFootBrain) completions(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "unreadable", http.StatusBadRequest)
		return
	}
	var body struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(raw, &body)

	ask := ""
	for at := len(body.Messages) - 1; at >= 0; at-- {
		if strings.EqualFold(body.Messages[at].Role, "user") {
			ask = messageText(body.Messages[at].Content)
			break
		}
	}

	// ONE QUESTION, RAISED BY THE WORK ITSELF. Only the request that carries
	// both the task's brief and a tool belt gets the `ask` call, and it gets it
	// once: the wire's answer when the person answers is a plain close, so the
	// run ends and the row settles with the question still waiting on it — the
	// exact shape the reported failure was made of, staged rather than chanced.
	b.mu.Lock()
	first := b.asked == 0
	b.asked++
	b.mu.Unlock()

	if first && strings.Contains(ask, "hello.txt") && strings.Contains(string(raw), `"tools":[`) {
		writeSteerStream(w, "ask",
			`{"head":"Should hello.txt land at the folder root or beside its work?",`+
				`"kind":"choice","form":"line",`+
				`"reason":"the brief named a file and not a place",`+
				`"stakes":"reversible",`+
				`"options":[{"key":"1","label":"folder root"},{"key":"2","label":"beside its work"}]}`,
			"")
		return
	}
	writeSteerStream(w, "", "", "ok")
}

// TestTaskQuestionFootKeepsTheRowDoor is #840's acceptance, staged: a task that
// DOES raise a question carries the door hint and the waiting note together on
// the roster's foot, and the pair — not a line one of them can displace — is
// what this waits for.
func TestTaskQuestionFootKeepsTheRowDoor(t *testing.T) {
	// No provider key is needed — the wire is the stub in this process — but a
	// terminal and the built binary are, and skipping rather than failing on
	// a machine without them is this suite's own law.
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux on PATH: this suite drives the real binary in a real terminal")
	}
	binary(t)

	brain := newQuestionFootBrain()
	t.Cleanup(brain.close)

	home := newHome(t, map[string]any{
		"model.talk":         questionFootModel,
		"setup_seen_at":      time.Now().UTC().Format(time.RFC3339Nano),
		"tools.approvalMode": "allow",
	})
	ws := newWorkspace(t, "qfootws", false)
	r := startWithEnv(t,
		[]string{"OPENROUTER_API_KEY=stub-key", "CODEAF_BASE_URL=" + brain.url()},
		"afe2e_qfoot", home, ws, tuiPlain, tuiShortRows, "chat", "--one-model", "--no-host")
	statesPastTheDoor(t, r)

	// The task whose run raises the question, started the way the reported
	// failure started it: from the conversation, so the roster's row is work
	// THIS conversation holds and its door is the room.
	r.lit("/task solo write a file called hello.txt containing the word hello")
	r.keys("Enter")
	// The task record is the whole synchronisation: once the worker has asked
	// its question the node is recorded on the roster whatever its state word.
	waitForRecord(t, home, 2*time.Minute)

	// The roster, onto the task's own row, where the question is a fact about
	// the work and the door is the row's own key.
	r.lit("/history")
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	r.keys("Down")

	// THE PAIR, AND NOTHING NARROWER. Either half alone is the line the
	// reported run starved on: a wait for the door dies when the note has
	// displaced it, and a wait for the note dies when the model raised
	// nothing. Both together say what the fix chose — one line, door first,
	// note behind it.
	foot := r.waitFor(30*time.Second,
		say(t, "tasksEnterRoomWord"), say(t, "questionWaitingWord"))
	t.Logf("the row keeps its door with a question waiting behind it:\n%s", foot)
	if !strings.Contains(foot, say(t, "tasksEnterRoomWord")) {
		t.Fatalf("a waiting question took the row's door off the foot:\n%s", foot)
	}
	if !strings.Contains(foot, say(t, "questionWaitingWord")) {
		t.Fatalf("the foot lost the waiting note it was holding:\n%s", foot)
	}
}

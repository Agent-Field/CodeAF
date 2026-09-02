//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// STEERING A TASK, END TO END, AGAINST A SCRIPTED MODEL.
//
// The rest of this package drives a real model, which is right for questions
// about judgment and wrong for this one: what is under test is a mark the engine
// writes and a row the surface draws, and both have to be the same on every run
// or the assertion is a coin toss. So this scenario stands a scripted endpoint in
// front of the real binary — the same dialect internal/provider speaks, the same
// shape test/remote/stub answers in — and drives the product through tmux
// exactly as a person's keyboard would.
//
// The journey is the one the issue is about (#252):
//
//	/task solo <brief>      one worker, started from the box
//	ctrl+t, enter           into that node's page
//	<a correction>, enter   the person bending work that is already running
//
// and the three things it proves: the correction lands as an elbow with the
// engine's own delivery clause, the node's record keeps it MARKED as a
// correction, and the page reopened afterwards draws it as an elbow again
// instead of as a second brief.

const (
	// steerBrief is what the node is sent to do. The marker is what the scripted
	// endpoint reads, so the script is keyed off the person's own words.
	steerBrief = "PROBE-STEER write a file called hello.txt containing the word hello"
	// steerLine is the correction. It is the commonest steer there is — "no, the
	// other place" — which is also the one the elbow's own file names.
	steerLine = "the config lives under etc/"
	// steerModel is the one row the scripted catalog serves.
	steerModel = "stub/scripted"
)

func TestARoomSteerIsAnElbowAndTheRecordKeepsIt(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux on PATH: this scenario drives the real binary in a real terminal")
	}
	brain := newSteerBrain(t)
	defer brain.close()

	home := newHome(t, map[string]any{
		"model.talk":              steerModel,
		"models.tiers.low":        steerModel,
		"models.tiers.high":       steerModel,
		"models.tiers.worker":     steerModel,
		"models.tiers.reflex":     steerModel,
		"models.tiers.mastermind": steerModel,
		"tools.approvalMode":      "allow",
		// The setup screen is a first-run gate that asks about spending, and a
		// scenario that had to answer it would be a scenario about setup. The row
		// it writes is written here instead, so the rig opens on the conversation.
		"setup_seen_at": time.Now().UTC().Format(time.RFC3339),
		// AND THE COLUMN IS PINNED OPEN. [newHome] copies the person's own profile,
		// and whether the roster column stands is one of the rows it carries — so a
		// developer who had pressed ctrl+g in their own aforge made this scenario
		// wait for a row on a column that was not drawn.
		"ui.task_column": true,
	})
	ws := newWorkspace(t, "steerws", false)
	r := startWithEnv(t, []string{
		"OPENROUTER_API_KEY=stub-key-not-a-secret",
		"AFORGE_BASE_URL=" + brain.server.URL + "/api/v1",
		"AFORGE_PROFILE_DIR=",
	}, "afe2e_roomsteer", home, ws, tuiPlain, 44)

	// The first launch on a state root built one minute ago sets itself up before
	// it draws anything, so this waits for whichever door it opens on rather than
	// for a fixed number of seconds.
	r.waitForAny(2*time.Minute, say(t, "homeFootWord"), say(t, "starterTaskWord"))
	r.keys("Escape")
	r.waitFor(30*time.Second, "steerws · ")

	// ── the work starts ──────────────────────────────────────────────────────
	r.lit("/task solo " + steerBrief)
	r.keys("Enter")
	// THE WAIT IS FOR THE WORKER'S OWN STEP, not for the brief coming back on
	// screen: the person's words are echoed the instant they press enter, and a
	// node that has not started yet has nobody inside it to read a correction.
	// The roster draws the running call, which is the first thing only a live
	// worker can put there.
	screen := r.waitFor(4*time.Minute, "bash sleep 25")
	t.Logf("the task is running, with a step in flight:\n%s", screen)

	// ── into its page ────────────────────────────────────────────────────────
	openRoom(t, r)
	// The placeholder in front of the caret names the node it is talking to, and
	// that is the one row on the frame that says the next thing typed is a steer.
	r.waitFor(20*time.Second, "(esc: main)")

	// ── one correction, typed into running work ──────────────────────────────
	r.lit(steerLine)
	r.keys("Enter")
	live := r.waitFor(30*time.Second, steerLine)
	t.Logf("the correction, live on the node's page:\n%s", live)

	elbow := elbowLine(live, steerLine)
	if elbow == "" {
		t.Fatalf("the correction is not drawn as an elbow:\n%s", live)
	}
	if !strings.Contains(elbow, session.SteerDelivered(false)) &&
		!strings.Contains(elbow, session.SteerDelivered(true)) {
		t.Fatalf("the correction carries no delivery clause: %q\n%s", elbow, live)
	}
	t.Logf("the elbow and its clause, verbatim: %q", elbow)
	if strings.Contains(live, "› "+steerLine) {
		t.Fatalf("the correction was drawn as a question of its own:\n%s", live)
	}

	// ── the record ───────────────────────────────────────────────────────────
	// The mark is written where the line is written down, which is the node's
	// next step boundary — so this waits for the record rather than reading it
	// once and calling an absence a failure.
	mark := waitForSteerMark(t, home, steerLine, 3*time.Minute)
	t.Logf("the node's own record says: at=%s consumed=%v landing=%q",
		mark.At.Format(time.RFC3339), mark.Consumed, mark.Landing)
	if !mark.Consumed || mark.At.IsZero() {
		t.Fatalf("the record does not say the line was delivered when it was sent: %+v", mark)
	}
	if mark.Landing != session.SteerDelivered(false) && mark.Landing != session.SteerDelivered(true) {
		t.Fatalf("the record's landing is %q, want the engine's own sentence", mark.Landing)
	}

	// ── the page, reopened ───────────────────────────────────────────────────
	// Out of the room and back in: the page is built again from the record, and
	// the correction has to come back as a correction.
	r.keys("Escape")
	r.waitFor(20*time.Second, "steerws · ")
	openRoom(t, r)
	again := r.waitFor(30*time.Second, steerLine)
	t.Logf("the node's page, reopened:\n%s", again)

	replayed := elbowLine(again, steerLine)
	if replayed == "" {
		t.Fatalf("the reopened page draws the correction as something else:\n%s", again)
	}
	t.Logf("the replayed elbow, verbatim: %q", replayed)
	if strings.Contains(replayed, session.SteerDelivered(false)) ||
		strings.Contains(replayed, session.SteerDelivered(true)) {
		t.Fatalf("a correction from a minute ago still wears its receipt: %q", replayed)
	}
	r.quit()
}

// The two spellings of "the column has the keyboard", quoted from where they are
// drawn (internal/tui3's task.go railHoldKeys and tasksettle.go roomSettleHint).
// The hint slot names the keys THAT ROW can answer, so which of the two stands
// is a fact about the work, not about the column.
const (
	railHoldWalk      = "↑↓ move"
	railHoldNeedsLook = "a accept · l look again · n not right"
)

// openRoom walks into the first node's page the way a keyboard does: the roster
// takes the keyboard, `enter` opens the row's room, and `esc` hands the keyboard
// back to the box — which is what makes the next thing typed a steer rather than
// a filter on the column.
func openRoom(t *testing.T, r *rig) {
	t.Helper()
	// THE COLUMN TAKES THE KEYBOARD FIRST, then `enter` opens the focused row's
	// page. What is waited on is the column SAYING it has the keyboard, and there
	// are two honest spellings of that because the hint slot belongs to the row
	// under the cursor (internal/tui3's [app.railHoldHintWord]): an ordinary held
	// row offers the walk, and a row whose work has landed and wants a look offers
	// the answers to that question instead.
	//
	// A SLEEP HERE ASSERTED NOTHING AND MISREAD THE SURFACE. Waiting a second and
	// carrying on made a run's outcome a function of how loaded the machine was,
	// and waiting for the walk's own words alone read the settle row — a perfectly
	// ordinary frame — as the column having failed to take the keyboard.
	r.keys("C-t")
	r.waitForAny(20*time.Second, railHoldWalk, railHoldNeedsLook)
	r.keys("Enter")
	r.waitFor(30*time.Second, "esc/← main")
	// AND THE KEYBOARD GOES BACK TO THE BOX. The column keeps it after it opens a
	// room, and a correction typed while it holds it is a filter on the column
	// rather than a line to the node.
	r.keys("Escape")
	r.waitFor(20*time.Second, "esc/← main")
}

// AND THE SAME JOURNEY AGAINST THE MODEL THE PRODUCT SHIPS ON.
//
// The scenario above is scripted so that its assertions are the same on every
// run. This one is the same walk with nothing scripted at all — the shipped
// default model, a real worker, a real step to interrupt — because a surface
// that only behaves against a stub is a surface nobody has watched work.
//
// THE BRIEF IS THE CHEAPEST ONE THAT STILL LEAVES A STEP IN FLIGHT: a wait and a
// file, which any model answers with one long call and then one short one. It
// costs a couple of calls on a small row.
func TestARoomSteerIsAnElbowAgainstTheShippedModel(t *testing.T) {
	requireTmuxAndKey(t)
	// EVERY SEAT IS THE SHIPPED DEFAULT, and that is the point of this run rather
	// than a convenience. [newHome] copies the person's own profile, so a machine
	// whose crew pins a bigger row for work would have this scenario approved
	// against a model the product does not ship on — the first run of it landed on
	// `deepseek/deepseek-v4-pro` for exactly that reason. Pinning every seat to
	// [config.DefaultModel] is what an untouched install resolves to, and it is
	// also the only way to stop an escalation quietly moving the answer.
	home := newHome(t, map[string]any{
		"tools.approvalMode":          "allow",
		"setup_seen_at":               time.Now().UTC().Format(time.RFC3339),
		"ui.task_column":              true,
		"model.talk":                  config.DefaultModel,
		config.KeyTierWorkerModel:     config.DefaultModel,
		config.KeyTierLowModel:        config.DefaultModel,
		config.KeyTierHighModel:       config.DefaultModel,
		config.KeyTierReflexModel:     config.DefaultModel,
		config.KeyTierMastermindModel: config.DefaultModel,
	})
	ws := newWorkspace(t, "steerlivews", false)
	r := start(t, "afe2e_roomsteerlive", home, ws, tuiPlain, 44)

	r.waitForAny(2*time.Minute, say(t, "homeFootWord"), say(t, "starterTaskWord"))
	r.keys("Escape")
	r.waitFor(30*time.Second, "steerlivews · ")

	r.lit("/task solo run `sleep 120` with bash to let a slow service settle, then write a file called hello.txt containing the word hello")
	r.keys("Enter")
	screen := r.waitFor(6*time.Minute, "bash sleep")
	t.Logf("the task is running, with a step in flight:\n%s", screen)

	openRoom(t, r)
	r.waitFor(20*time.Second, "(esc: main)")
	turns := roomTurnGlyphs(r.capture())
	r.lit(steerLine)
	r.keys("Enter")
	live := r.waitFor(60*time.Second, steerLine)
	t.Logf("the correction, live on the node's page:\n%s", live)

	elbow := elbowLine(live, steerLine)
	if elbow == "" {
		t.Fatalf("the correction is not drawn as an elbow:\n%s", live)
	}
	t.Logf("the elbow and its clause, verbatim: %q", elbow)
	if !strings.Contains(elbow, session.SteerDelivered(false)) &&
		!strings.Contains(elbow, session.SteerDelivered(true)) {
		t.Fatalf("the correction carries no delivery clause: %q", elbow)
	}
	// AND THE PAGE OPENED NO TURN FOR IT. A task's page is one question with
	// corrections hanging off it, so the count of the person's own turn glyphs is
	// what it was before they typed.
	if got := roomTurnGlyphs(live); got != turns {
		t.Fatalf("steering opened a turn: %d turn glyphs, want %d", got, turns)
	}

	mark := waitForSteerMark(t, home, steerLine, 5*time.Minute)
	t.Logf("the node's own record says: at=%s consumed=%v landing=%q",
		mark.At.Format(time.RFC3339), mark.Consumed, mark.Landing)
	if !mark.Consumed || mark.Landing == "" {
		t.Fatalf("the record does not keep the correction as one: %+v", mark)
	}

	r.keys("Escape")
	r.waitFor(30*time.Second, "steerlivews · ")
	openRoom(t, r)
	again := r.waitFor(60*time.Second, steerLine)
	t.Logf("the node's page, reopened:\n%s", again)
	replayed := elbowLine(again, steerLine)
	if replayed == "" {
		t.Fatalf("the reopened page draws the correction as something else:\n%s", again)
	}
	t.Logf("the replayed elbow, verbatim: %q", replayed)
	if strings.Contains(replayed, session.SteerDelivered(false)) ||
		strings.Contains(replayed, session.SteerDelivered(true)) {
		t.Fatalf("a correction from a minute ago still wears its receipt: %q", replayed)
	}
	r.quit()
}

// roomTurnGlyphs counts the person's own question marks on the page. A task page
// draws exactly one — the instruction it was given — however many corrections
// hang off it.
func roomTurnGlyphs(screen string) int {
	count := 0
	for _, line := range strings.Split(screen, "\n") {
		if strings.HasPrefix(strings.TrimRight(line, " "), "› ") {
			count++
		}
	}
	return count
}

// elbowLine is the drawn row the correction is on, or "" when no row on the
// screen opens with the elbow's own glyph.
func elbowLine(screen, words string) string {
	for _, line := range strings.Split(screen, "\n") {
		trimmed := strings.TrimRight(line, " ")
		if strings.HasPrefix(trimmed, "└ ") && strings.Contains(trimmed, words) {
			return trimmed
		}
	}
	return ""
}

// waitForSteerMark reads every record under this rig's state root through the
// engine's own door until one of them holds the correction, marked.
//
// IT USES THE DOOR THIS CHANGE ADDED, deliberately: the assertion is about what
// a page reading somebody else's record gets back, and that is exactly what
// [session.ReadTranscript] answers.
func waitForSteerMark(t *testing.T, home, words string, within time.Duration) session.SteerMark {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		var seen []string
		_ = filepath.WalkDir(home, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".jsonl") {
				return nil
			}
			seen = append(seen, path)
			return nil
		})
		for _, path := range seen {
			for _, entry := range session.ReadTranscript(path).Entries {
				if strings.TrimSpace(entry.Text) == words && entry.Steer != nil {
					return *entry.Steer
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no record under %s marks %q as a correction; read %d files", home, words, len(seen))
		}
		time.Sleep(pollEvery)
	}
}

// ── the scripted endpoint ───────────────────────────────────────────────────
//
// It answers the two paths internal/provider and internal/catalog reach for, in
// the dialect test/remote/stub's own header describes. The script turns on two
// facts: what the last user message said, and whether a tool has answered since.

type steerBrain struct {
	t      *testing.T
	server *httptest.Server

	mu    sync.Mutex
	calls int
	waits int
}

// steerMaxSteps is how many waits the scripted worker will take before it gives
// up and answers. It is a STOP and not a schedule: the scenario ends when the
// correction arrives, and this only keeps a run that never steers from spinning.
const steerMaxSteps = 12

// steps counts one more scripted wait and answers its number.
func (b *steerBrain) steps() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.waits++
	return b.waits
}

func newSteerBrain(t *testing.T) *steerBrain {
	t.Helper()
	brain := &steerBrain{t: t}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/models", brain.models)
	mux.HandleFunc("/api/v1/chat/completions", brain.completions)
	mux.HandleFunc("/", http.NotFound)
	brain.server = httptest.NewServer(mux)
	return brain
}

func (b *steerBrain) close() { b.server.Close() }

func (b *steerBrain) models(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"data":[{"id":%q,"canonical_slug":%q,"name":"Scripted stub",`+
		`"context_length":200000,`+
		`"architecture":{"input_modalities":["text"],"output_modalities":["text"]},`+
		`"pricing":{"prompt":"0","completion":"0","request":"0","input_cache_read":"0"},`+
		`"supported_parameters":["tools","tool_choice","max_tokens"]}]}`, steerModel, steerModel)
}

func (b *steerBrain) completions(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "unreadable", http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()

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

	switch {
	// THE CORRECTION ENDS THE WORK, and nothing else does: this arm is what makes
	// the node land, so the window in which it can be steered is as long as the
	// person needs rather than as long as one scripted step happens to be.
	case strings.Contains(ask, steerLine):
		writeSteerStream(w, "", "", "Understood — reading etc/ instead. Nothing else to do here.")

	// UNTIL THEN THE WORKER ALWAYS HAS A STEP IN FLIGHT, which is the whole
	// fixture: a node in the middle of a call is a node somebody can steer, and
	// that call's own ending is the boundary at which the correction is read and
	// written down. Each wait is under this build's thirty-second backgrounding
	// rule, so it stays in the foreground where the node is waiting on it, and
	// each carries its own number so a run of them is not one call repeated.
	//
	// It is asked only of a request that was OFFERED TOOLS, which is what tells a
	// worker's own step apart from the errands a session runs beside it — naming
	// the task, titling the conversation — that carry the same words and cannot
	// call anything.
	case strings.Contains(ask, "PROBE-STEER") && strings.Contains(string(raw), `"tools":[`) &&
		b.steps() <= steerMaxSteps:
		writeSteerStream(w, "bash", fmt.Sprintf(`{"command":"sleep 25 # step %d"}`, b.steps()), "")
	default:
		// EVERY OTHER CALL STILL GETS A VALID ANSWER. A session makes calls
		// nobody scripted — the conversation's title, a name for the task, a
		// reflex — and a stub that refused them would fail the run for reasons
		// this scenario is not about.
		writeSteerStream(w, "", "", "ok")
	}
}

func messageText(raw json.RawMessage) string {
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		return plain
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var out strings.Builder
	for _, part := range parts {
		out.WriteString(part.Text)
	}
	return out.String()
}

// writeSteerStream is internal/provider's sse.go read backwards: `data: ` lines
// of the chunk shape that file declares, ended by `[DONE]`.
func writeSteerStream(w http.ResponseWriter, tool, args, text string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "the stub needs a flushable writer", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	send := func(payload string) {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
	}
	head := `{"id":"stub","object":"chat.completion.chunk","created":0,"model":"` + steerModel +
		`","provider":"stub","choices":[{"index":0,"delta":`
	send(head + `{"role":"assistant","content":""},"finish_reason":null}]}`)
	reason := "stop"
	if tool != "" {
		send(head + fmt.Sprintf(
			`{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":%q,"arguments":%q}}]},"finish_reason":null}]}`,
			tool, args))
		reason = "tool_calls"
	} else {
		send(head + fmt.Sprintf(`{"content":%q},"finish_reason":null}]}`, text))
	}
	send(head + fmt.Sprintf(`{},"finish_reason":%q}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"cost":0}}`, reason))
	send("[DONE]")
}

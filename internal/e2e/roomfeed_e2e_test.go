//go:build e2e

package e2e

// A TASK'S PAGE, IN A REAL TERMINAL, SAYING WHAT A CALL TOOK.
//
// This is the end-to-end evidence for the change that put the task room on the
// conversation's own reducer (docs/design/lens/DESIGN.md, Decision 1). The
// defect it holds shut is exactly the one the design names: a node's row never
// carried a duration, because EventToolFinished was not in room.go's switch at
// all — a fact the chat had and a task's page did not.
//
// IT IS SCRIPTED, NOT MODELLED, and that is the honest shape for this change.
// Nothing here touches the wire: the whole change is surface work over events
// the engine already sends, so a real model would add cost and flakiness and
// prove nothing this stub does not. The endpoint below speaks the same
// OpenAI-compatible dialect internal/provider writes and reads, exactly as
// test/remote/stub does for the two-machine harness, and it answers from a
// script — one batch of two `bash` calls of different lengths, then a plain
// reply.
//
// TWO CALLS OF ONE NAME IN ONE BATCH IS THE WHOLE ASSERTION, and it is what
// makes this a gate rather than a screenshot. The results of a batch land
// TOGETHER, so a page that timed a row from its own begin to its own end writes
// the SLOWEST call's span on every row in the batch. Only the call's own finish
// — session.EventToolFinished, the event room.go's copy never had — can say that
// one of them took a second and the other took four. And the two rows are told
// apart by their arguments, which is ruling 3 of the same design.
//
//	go test -tags e2e -count=1 -run TestATaskPageShowsWhatACallTook -v ./internal/e2e/

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The words this run turns on. The command is what the node is scripted to run
// and what its row is found by; the sleep is what makes the duration a fact
// rather than a rounding.
const (
	// Short commands on purpose: the tool row sheds duration before the target
	// (toolview.go's [app.toolTail]), and a long `sleep N; echo PROBE-…` ate the
	// whole budget so the figure this test exists to catch never reached the
	// screen even when the entry held it.
	roomProbeQuick = "sleep 3; echo A"
	roomProbeSlow  = "sleep 7; echo B"
	roomProbeBrief = "run both probes and report what they printed"
	roomProbeModel = "stub/scripted"
	// What each of them is spelled as (timestamps.go's tookWord: one decimal
	// under ten seconds). Chosen so neither collides with the room header's
	// running clock (`working · bash · Ns`), which is whole seconds.
	roomQuickTook = "3.0s"
	roomSlowTook  = "7.0s"
	roomQuickMark = "echo A"
	roomSlowMark  = "echo B"
)

func TestATaskPageShowsWhatACallTook(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux on PATH: this test drives the real binary in a real terminal")
	}
	brain := startScriptedBrain(t, false)
	// EVERY SETUP STEP IS ANSWERED IN THE PROFILE, so the run opens on the
	// conversation rather than on the greeting: a key, a crew tier, and a daily
	// ceiling are the three things firstrun.go asks for (setupStepsFor).
	home := newHome(t, map[string]any{
		"model.talk": roomProbeModel, "model.work": roomProbeModel, "model.plan": roomProbeModel,
		"models.tiers.worker": roomProbeModel, "models.tiers.high": roomProbeModel,
		"tools.approvalMode": "allow",
		"daily_budget_usd":   0,
	})
	ws := newWorkspace(t, "probe", false)

	// The key is a string and not a secret: config.Load refuses to build a
	// session with none at all, and nothing behind this endpoint checks it.
	rig := startWithEnv(t, []string{
		"OPENROUTER_API_KEY=test-key",
		"CODEAF_BASE_URL=" + brain.URL + "/api/v1",
	}, "roomfeed", home, ws, 120, 40)

	// ONE WORKER AND NO SIZING CALL, which is what `solo` means: the node starts
	// from the brief with nothing asked of the model first, so everything below
	// is the node's own turn and not the conversation's.
	rig.lit("/task solo " + roomProbeBrief)
	rig.keys("Enter")

	// The node has to reach the call before there is anything to read. The
	// roster's row is the door and the room is behind it.
	rig.waitFor(90*time.Second, roomProbeBrief)
	openTheOnlyRoom(t, rig)
	revealCallRows(t, rig)

	// THE ROWS COME FIRST AND THE FIGURES COME AFTER THEM — both calls may still
	// be running when the page opens, which is the point: the wait is for the
	// slower one's figure, so a page that drew the rows and never the figures
	// fails here rather than passing on the frame before the answer. A landing
	// that folds the stretch mid-wait is reopened rather than treated as a
	// missing figure — the figures are still on the page behind the chip.
	page := waitForCallFigures(t, rig, 90*time.Second)
	tookOn(t, page, roomQuickMark, roomQuickTook)
	tookOn(t, page, roomSlowMark, roomSlowTook)
	t.Logf("the task page, captured:\n%s", page)
}

// tookOn asserts that the row carrying one call says what that call took. It
// reads the ROW and not the page, because a page holding "1.0s" somewhere is
// not a page that put the figure on the right call.
func tookOn(t *testing.T, page, command, took string) {
	t.Helper()
	for _, line := range strings.Split(page, "\n") {
		if !strings.Contains(line, command) {
			continue
		}
		if strings.Contains(line, took) {
			return
		}
		t.Fatalf("the row for %s says %q, want %q — a batch's rows are being timed "+
			"from the batch and not from each call's own finish:\n%s",
			command, strings.TrimSpace(line), took, page)
	}
	t.Fatalf("no row for %s on the page:\n%s", command, page)
}

// openTheOnlyRoom walks into the one node on the roster.
func openTheOnlyRoom(t *testing.T, r *rig) {
	t.Helper()
	// THE ROSTER MAY BE HIDDEN, and its own foot says which: `ctrl+g tasks`
	// under the transcript is the column asking to be shown, `ctrl+g hide` is
	// the column already there (tasks.go). A run that pressed blindly would
	// close the roster on half the launches and then walk an empty column.
	if strings.Contains(r.capture(), "ctrl+g tasks") {
		r.keys("C-g")
		mustSee(t, r, 10*time.Second, "ctrl+g hide")
	}
	// CLICK THE RAIL ROW rather than alt+t then Enter. After a landing the
	// settle strip owns Enter (`[enter] take it`), and the roster's own hold
	// legend (`↑↓ move`) is replaced by the settle keys while the card is up —
	// so a keyboard open either settles the card or waits forever for a hint
	// that will not return. A click on the row is the person-facing door that
	// does not compete with the strip.
	clickRailTask(t, r)
	// THE ROOM IS OPEN WHEN ITS WAY BACK IS ON THE FRAME. At this suite's
	// 120×40 with the rail showing, roompanel.go's [app.roomOrganized] is true:
	// the focus header carries `esc/← main` (roomBackWord) and the legend's
	// left end is empty — so a wait for the compact legend spelling
	// `room · esc/←← main` never fires on a real open page. The back word is
	// the one string every room shape shares (tuiwords' roomBackWord).
	mustSee(t, r, 20*time.Second, say(t, "roomBackWord"))
}

// clickRailTask presses the roster's first task row. The row carries `#1` (or
// the node's number) on the right column; the transcript's own prose must not
// steal the click.
func clickRailTask(t *testing.T, r *rig) {
	t.Helper()
	rows := strings.Split(strings.TrimRight(r.capture(), "\n"), "\n")
	for i, line := range rows {
		if strings.Contains(line, "#1") || strings.Contains(line, "▌") {
			r.mouseTo(100, i+1)
			r.mouseClick(100, i+1)
			return
		}
	}
	t.Fatalf("no rail task row to open:\n%s", r.capture())
}

// revealCallRows puts each call's own row on the room page. After a landing the
// stretch sits behind `▸ worked` — a click opens the chip (workfold.go), and a
// shut caption underneath opens the calls. ctrl+e is NOT used here: a settle
// strip under the room owns the keyboard, and the chord would land on the
// strip rather than on the chip.
func revealCallRows(t *testing.T, r *rig) {
	t.Helper()
	if strings.Contains(r.capture(), roomQuickMark) {
		return
	}
	if strings.Contains(r.capture(), "▸ worked") {
		clickLineContaining(t, r, "▸ worked")
		mustSee(t, r, 8*time.Second, "▾ worked")
	}
	if strings.Contains(r.capture(), roomQuickMark) {
		return
	}
	if strings.Contains(r.capture(), "▸ ran ") || strings.Contains(r.capture(), "▸ running ") {
		clickLineContaining(t, r, "▸ ran ")
	}
	if strings.Contains(r.capture(), roomQuickMark) || strings.Contains(r.capture(), "bash") {
		return
	}
	// Still folded or not yet drawn — leave waitForCallFigures to keep trying.
}

// clickLineContaining presses the first body row that carries needle.
func clickLineContaining(t *testing.T, r *rig, needle string) {
	t.Helper()
	rows := strings.Split(strings.TrimRight(r.capture(), "\n"), "\n")
	for i, line := range rows {
		if strings.Contains(line, needle) {
			r.mouseTo(12, i+1)
			r.mouseClick(12, i+1)
			return
		}
	}
	t.Fatalf("no line carrying %q:\n%s", needle, r.capture())
}

// waitForCallFigures waits until both probes' own durations are on their rows.
func waitForCallFigures(t *testing.T, r *rig, within time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		screen := r.capture()
		if callTook(screen, roomQuickMark, roomQuickTook) && callTook(screen, roomSlowMark, roomSlowTook) {
			return screen
		}
		if strings.Contains(screen, "▸ worked") ||
			(strings.Contains(screen, "▾ worked") && !strings.Contains(screen, roomQuickMark)) {
			revealCallRows(t, r)
		}
		if time.Now().After(deadline) {
			t.Fatalf("waited %s for per-call figures and never saw them. the screen was:\n%s",
				within, screen)
		}
		time.Sleep(pollEvery)
	}
}

func callTook(page, command, took string) bool {
	for _, line := range strings.Split(page, "\n") {
		if strings.Contains(line, command) && strings.Contains(line, took) {
			return true
		}
	}
	return false
}

// mustSee is waitFor that fails the test at once. waitFor itself only Errorf's
// so a caller can keep gathering, and that turned a missed room open into a
// ninety-second wait for a figure behind a folded chip.
func mustSee(t *testing.T, r *rig, within time.Duration, want ...string) string {
	t.Helper()
	deadline := time.Now().Add(within)
	screen := ""
	for {
		screen = r.capture()
		ok := true
		for _, sub := range want {
			if !strings.Contains(screen, sub) {
				ok = false
				break
			}
		}
		if ok {
			return screen
		}
		if time.Now().After(deadline) {
			t.Fatalf("waited %s for %q and never saw it. the screen was:\n%s", within, want, screen)
		}
		time.Sleep(pollEvery)
	}
}

// ── the scripted endpoint ───────────────────────────────────────────────────

// startScriptedBrain stands in for every model call this run makes.
//
// THE SCRIPT IS ONE RULE. A turn that has not run a tool yet is answered with
// the probe call; a turn whose last word is a tool's result is answered in
// words. That terminates every turn the engine opens — the node's own, and the
// pass that checks what it left — without this file having to know how many
// there will be.
func startScriptedBrain(t *testing.T, cutFirst bool) *httptest.Server {
	t.Helper()
	var streams, cuts atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"data":[{"id":%q,"canonical_slug":%q,"name":"Scripted stub",
			"context_length":200000,
			"architecture":{"input_modalities":["text"],"output_modalities":["text"]},
			"pricing":{"prompt":"0","completion":"0","request":"0","input_cache_read":"0"},
			"supported_parameters":["tools","tool_choice","max_tokens"]}]}`,
			roomProbeModel, roomProbeModel)
	})
	mux.HandleFunc("/api/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		ranSomething := false
		for _, m := range body.Messages {
			if strings.EqualFold(m.Role, "tool") {
				ranSomething = true
			}
		}
		n := streams.Add(1)
		// A STREAM THAT STOPS MID-REPLY, once, on the turn where the node is
		// about to report what its calls found. The body ends with no finish and
		// no [DONE], which is what a provider that went quiet looks like from
		// this side, and internal/provider's streamguard answers it by cutting
		// the attempt and asking again.
		//
		// IT IS THE NODE'S SECOND TURN AND NOT THE FIRST REQUEST OF THE RUN,
		// because the conversation makes calls of its own before a node exists —
		// naming the work, sizing it — and a cut there would be a retry nobody
		// is standing in a room to see.
		if cutFirst && ranSomething && cuts.Add(1) == 1 {
			writeCutStream(r.Context(), w, n)
			return
		}
		writeScriptedStream(w, n, ranSomething)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// writeScriptedStream is internal/provider's sse.go read backwards.
func writeScriptedStream(w http.ResponseWriter, n int64, ranSomething bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "the script needs a flushable writer", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	id := fmt.Sprintf("script-%d", n)
	send := func(payload string) {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
	}
	frame := func(delta, reason string) string {
		if reason == "" {
			reason = "null"
		} else {
			reason = fmt.Sprintf("%q", reason)
		}
		return fmt.Sprintf(`{"id":%q,"object":"chat.completion.chunk","created":%d,"model":%q,`+
			`"provider":"stub","choices":[{"index":0,"delta":%s,"finish_reason":%s}],`+
			`"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"cost":0}}`,
			id, time.Now().Unix(), roomProbeModel, delta, reason)
	}
	send(frame(`{"role":"assistant","content":""}`, ""))
	if !ranSomething {
		send(frame(fmt.Sprintf(`{"tool_calls":[%s,%s]}`,
			bashCall(0, fmt.Sprintf("a%d", n), roomProbeQuick),
			bashCall(1, fmt.Sprintf("b%d", n), roomProbeSlow)), ""))
		send(frame(`{}`, "tool_calls"))
		send("[DONE]")
		return
	}
	send(frame(`{"content":"Both probes printed."}`, ""))
	send(frame(`{}`, "stop"))
	send("[DONE]")
}

// bashCall is one entry of a streamed tool_calls array.
func bashCall(index int, id, command string) string {
	args, _ := json.Marshal(map[string]string{"command": command})
	quoted, _ := json.Marshal(string(args))
	return fmt.Sprintf(`{"index":%d,"id":"call_%s","type":"function",`+
		`"function":{"name":"bash","arguments":%s}}`, index, id, quoted)
}

// ── THE SAME PAGE, AGAINST A REAL MODEL ─────────────────────────────────────

// The scripted run above is the gate: deterministic, free, and it fails at
// origin/dev. This one is the APPROVAL — the shipped default model, the real
// binary, a real terminal, and a person's own sentence — because unit green and
// a stubbed endpoint together still do not prove that the thing a person opens
// says the thing.
//
// It asserts less than the scripted run on purpose. A real model chooses its
// own calls, so what is checked is the shape rather than the script: a `bash`
// row on a node's page carrying a duration. It costs a fraction of a cent.
func TestATaskPageShowsWhatACallTookAgainstARealModel(t *testing.T) {
	requireTmuxAndKey(t)
	home := newHome(t, map[string]any{
		"model.talk": realProbeModel, "model.work": realProbeModel,
		"models.tiers.worker": realProbeModel, "models.tiers.high": realProbeModel,
		"tools.approvalMode": "allow",
		"daily_budget_usd":   1,
	})
	ws := newWorkspace(t, "realprobe", false)
	rig := start(t, "roomfeedreal", home, ws, 120, 40)

	rig.lit("/task solo " + realProbeBrief)
	rig.keys("Enter")
	// THE NODE HAS TO EXIST BEFORE THERE IS A ROOM TO WALK INTO. The line the
	// conversation writes when it starts one is the honest signal; the card
	// above it is still shaping and has no page behind it (app.go's task note).
	rig.waitFor(180*time.Second, "task 1 started")
	openTheOnlyRoom(t, rig)

	// The call's row, whichever branch glyph the tree gives it.
	page := rig.waitFor(180*time.Second, "▶ bash")
	deadline := time.Now().Add(180 * time.Second)
	for {
		if line, took := timedCallRow(page); took {
			t.Logf("a node's call row, against %s:\n  %s", realProbeModel, strings.TrimSpace(line))
			t.Logf("the task page, captured:\n%s", page)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no call row on the node's page carries a duration:\n%s", page)
		}
		time.Sleep(pollEvery)
		page = rig.capture()
	}
}

const (
	realProbeModel = "~deepseek/deepseek-v4-flash-latest"
	realProbeBrief = "with the shell, sleep for two seconds and list the files here; " +
		"then read README.md; then say in one line what it contains. do nothing else."
)

// takenWord is a duration as a finished call's row spells it (timestamps.go's
// tookWord): `1.4s`, `12s`, `2m04s`. It is anchored to the end of the row
// because that is where the figure sits, which is what keeps it from matching a
// second in the command the row is about.
var takenWord = regexp.MustCompile(`(\d+\.\d+s|\d+s|\d+m\d\ds)\s*$`)

// timedCallRow is the first `bash` row on the page that carries a figure.
func timedCallRow(page string) (string, bool) {
	for _, line := range strings.Split(page, "\n") {
		if !strings.Contains(line, "bash") {
			continue
		}
		// The rail is drawn to the right of the transcript, so a row's own end
		// is where the seam is (room.go) — everything past it belongs to the
		// roster and never to this call.
		row := line
		if at := strings.IndexAny(row, "│▌"); at >= 0 {
			row = row[:at]
		}
		if takenWord.MatchString(strings.TrimRight(row, " ")) {
			return row, true
		}
	}
	return "", false
}

// writeCutStream is an attempt that speaks once and then goes quiet.
//
// IT DOES NOT SIMPLY END. A body that closes cleanly is a stream that finished,
// and the engine takes the words as the answer — which is a different thing
// from what this test is about. What it does instead is hold the connection
// open saying nothing, which is what a provider that stopped mid-reply looks
// like from this side, until the stream guard's mid-stream silence bound
// (internal/provider's midStreamGapBound, 45s) cuts the attempt and the engine
// asks again. The handler leaves when the cut cancels the request.
func writeCutStream(ctx context.Context, w http.ResponseWriter, n int64) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "the script needs a flushable writer", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, "data: {\"id\":\"cut-%d\",\"object\":\"chat.completion.chunk\",\"created\":%d,"+
		"\"model\":%q,\"provider\":\"stub\",\"choices\":[{\"index\":0,"+
		"\"delta\":{\"role\":\"assistant\",\"content\":\"I will start by\"},\"finish_reason\":null}]}\n\n",
		n, time.Now().Unix(), roomProbeModel)
	flusher.Flush()
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Minute):
	}
}

// A RETRY INSIDE A TASK IS VISIBLE, which it was not: EventRetrying never
// reached room.go's switch, so the half-answer of a cut attempt sat on a node's
// page above the live one with nothing to explain it.
//
// The words are internal/session's own (loop.go's cutNotice), so this asserts on
// the half that is a fact about the page rather than on the whole sentence.
func TestATaskPageSaysWhenAnAttemptWasCutAndAskedAgain(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux on PATH: this test drives the real binary in a real terminal")
	}
	brain := startScriptedBrain(t, true)
	home := newHome(t, map[string]any{
		"model.talk": roomProbeModel, "model.work": roomProbeModel, "model.plan": roomProbeModel,
		"models.tiers.worker": roomProbeModel, "models.tiers.high": roomProbeModel,
		"tools.approvalMode": "allow",
		"daily_budget_usd":   0,
	})
	ws := newWorkspace(t, "cutprobe", false)
	rig := startWithEnv(t, []string{
		"OPENROUTER_API_KEY=test-key",
		"CODEAF_BASE_URL=" + brain.URL + "/api/v1",
	}, "roomfeedcut", home, ws, 120, 40)

	rig.lit("/task solo " + roomProbeBrief)
	rig.keys("Enter")
	rig.waitFor(90*time.Second, "task 1 started")
	openTheOnlyRoom(t, rig)

	// The cut is the guard's mid-stream silence bound, so the wait has to be
	// longer than it (internal/provider's midStreamGapBound).
	page := rig.waitFor(4*time.Minute, "asking again")
	if strings.Contains(page, "I will start by") {
		t.Fatalf("the dead attempt's words are still on the node's page:\n%s", page)
	}
	t.Logf("the task page, captured:\n%s", page)
}

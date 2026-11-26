package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// doorServer stands in for an organization's server: the routes the terminal
// door reads and writes, answering as one person, with the task's states
// played back one read at a time.
type doorServer struct {
	t          *testing.T
	mu         sync.Mutex
	runner     string
	workstream []map[string]any
	states     []factory.WorkstreamTask // each GET of the task answers the next; the last repeats
	reads      int
	created    map[string]any
	answered   map[string]string
	answers    int
	token      string
	// refuseSignIn, when set, is the status every password sign-in answers,
	// with refuseSentence as the server's sentence.
	refuseSignIn   int
	refuseSentence string
	// answerFails is how many answers are refused (500, or failStatus)
	// before one is taken.
	answerFails int
	failStatus  int
	// answerStatus, when set, is the status every answer is refused with.
	answerStatus int
	// name is the organization's name /v1/about and the sign-in answer.
	name string
}

func (d *doorServer) orgName() string {
	if d.name != "" {
		return d.name
	}
	return "Northwind"
}

func (d *doorServer) handler() http.Handler {
	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	signedIn := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer "+d.token {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in"})
			return false
		}
		return true
	}
	mux.HandleFunc("GET /v1/about", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"name": d.orgName(), "sign_in": []string{"password"}})
	})
	mux.HandleFunc("POST /v1/auth/password", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Username, Password string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if d.refuseSignIn != 0 {
			writeJSON(w, d.refuseSignIn, map[string]string{"error": d.refuseSentence})
			return
		}
		if in.Username != "bob" || in.Password != "correct horse battery" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "no"})
			return
		}
		writeJSON(w, 200, map[string]any{
			"factory": map[string]string{"name": d.orgName()}, "access_token": d.token, "token_type": "bearer",
			"expires_in": 3600, "person": map[string]string{"handle": "bob", "name": "Bob Lee"},
			"me": map[string]any{"handle": "bob", "session_id": "ses_1"},
		})
	})
	mux.HandleFunc("GET /v1/settings", func(w http.ResponseWriter, r *http.Request) {
		if signedIn(w, r) {
			writeJSON(w, 200, map[string]any{"agent_runner": d.runner})
		}
	})
	mux.HandleFunc("GET /v1/workstreams", func(w http.ResponseWriter, r *http.Request) {
		if signedIn(w, r) {
			writeJSON(w, 200, map[string]any{"workstreams": d.workstream})
		}
	})
	mux.HandleFunc("POST /v1/workstreams/{ws}/tasks", func(w http.ResponseWriter, r *http.Request) {
		if !signedIn(w, r) {
			return
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		_ = json.NewDecoder(r.Body).Decode(&d.created)
		writeJSON(w, http.StatusCreated, d.states[0])
	})
	mux.HandleFunc("GET /v1/workstreams/{ws}/tasks/{tid}", func(w http.ResponseWriter, r *http.Request) {
		if !signedIn(w, r) {
			return
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		if r.PathValue("tid") != d.states[0].ID {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such task"})
			return
		}
		at := min(d.reads, len(d.states)-1)
		d.reads++
		writeJSON(w, 200, d.states[at])
	})
	mux.HandleFunc("POST /v1/workstreams/{ws}/tasks/{tid}/answer", func(w http.ResponseWriter, r *http.Request) {
		if !signedIn(w, r) {
			return
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.answerStatus != 0 {
			writeJSON(w, d.answerStatus, map[string]string{"error": "that Ask was already answered\x1b[2J"})
			return
		}
		if d.answerFails > 0 {
			d.answerFails--
			status := d.failStatus
			if status == 0 {
				status = http.StatusInternalServerError
			}
			writeJSON(w, status, map[string]string{"error": "the store blinked"})
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&d.answered)
		d.answers++
		at := min(d.reads, len(d.states)-1)
		d.reads++
		writeJSON(w, 200, d.states[at])
	})
	mux.HandleFunc("GET /v1/workstreams/{ws}/files", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"files": []map[string]string{{"id": "fl_1", "name": "Release notes"}}})
	})
	return mux
}

// doorTest puts the door in a throwaway home with its terminal captured: what
// it prints, what is typed at it, and whether each end is a terminal.
func doorTest(t *testing.T, typed string, inTerminal, outTerminal bool) *bytes.Buffer {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	out := &bytes.Buffer{}
	oldIn, oldOut, oldInT, oldOutT, oldLines, oldEvery := doorIn, doorOut, doorInIsTerminal, doorOutIsTerminal, doorLines, doorEvery
	oldErr := usageErr
	doorIn, doorOut, doorLines, doorEvery = strings.NewReader(typed), out, nil, time.Millisecond
	usageErr = out
	doorInIsTerminal = func() bool { return inTerminal }
	doorOutIsTerminal = func() bool { return outTerminal }
	t.Cleanup(func() {
		doorIn, doorOut, doorInIsTerminal, doorOutIsTerminal, doorLines, doorEvery = oldIn, oldOut, oldInT, oldOutT, oldLines, oldEvery
		usageErr = oldErr
	})
	return out
}

func newDoorServer(t *testing.T, states ...factory.WorkstreamTask) (*doorServer, *httptest.Server) {
	d := &doorServer{t: t, runner: "server", token: "tok_secret_value_123", states: states,
		workstream: []map[string]any{
			{"id": "ws_1", "name": "Spring launch", "my_role": "edit"},
			{"id": "ws_2", "name": "Customer onboarding", "my_role": "comment"},
		}}
	srv := httptest.NewServer(d.handler())
	t.Cleanup(srv.Close)
	return d, srv
}

// signIn signs the door in to srv as bob, through the password door.
func signIn(t *testing.T, srv *httptest.Server) {
	t.Helper()
	oldIn, oldLines, oldTerminal := doorIn, doorLines, doorInIsTerminal
	doorIn, doorLines = strings.NewReader("correct horse battery\n"), nil
	doorInIsTerminal = func() bool { return false }
	defer func() { doorIn, doorLines, doorInIsTerminal = oldIn, oldLines, oldTerminal }()
	if err := runOrgLogin([]string{srv.URL, "--user", "bob"}); err != nil {
		t.Fatalf("sign-in: %v", err)
	}
}

func doorTask(id, state string, mutate ...func(*factory.WorkstreamTask)) factory.WorkstreamTask {
	t := factory.WorkstreamTask{
		ID: id, WS: "ws_1", Title: "Write the release notes for 0.9", State: state,
		Owner:     factory.TaskOwner{Kind: factory.TaskOwnerPerson, ID: "bob", Name: "Bob Lee"},
		DoneBy:    factory.TaskDoneBy{Kind: factory.TaskByAfi},
		CreatedAt: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano),
	}
	for _, m := range mutate {
		m(&t)
	}
	return t
}

func TestDoorSignInKeepsThePersonTokenPrivateAndUnprinted(t *testing.T) {
	out := doorTest(t, "correct horse battery\n", false, false)
	d, srv := newDoorServer(t, doorTask("tk_1", factory.TaskRunning))
	if err := runOrgLogin([]string{srv.URL, "--user", "bob"}); err != nil {
		t.Fatalf("sign-in: %v", err)
	}
	info, err := os.Stat(doorStatePath())
	if err != nil {
		t.Fatalf("the sign-in was not kept: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("the sign-ins file is %o; it holds a token and must be 0600", mode)
	}
	body, _ := os.ReadFile(doorStatePath())
	if !strings.Contains(string(body), d.token) {
		t.Errorf("the token was not kept:\n%s", body)
	}
	if strings.Contains(out.String(), d.token) || strings.Contains(out.String(), "correct horse") {
		t.Errorf("the door printed a secret:\n%s", out)
	}
	for _, want := range []string{"Signed in to Northwind as Bob Lee (bob)", doorMadeWith} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("sign-in said %q, want %q in it", out, want)
		}
	}

	if err := runOrgLogout(nil); err != nil {
		t.Fatalf("sign-out: %v", err)
	}
	body, _ = os.ReadFile(doorStatePath())
	if strings.Contains(string(body), d.token) {
		t.Errorf("sign-out kept the token:\n%s", body)
	}
}

func TestDoorSignInRefusesAWrongPassword(t *testing.T) {
	doorTest(t, "wrong\n", false, false)
	_, srv := newDoorServer(t, doorTask("tk_1", factory.TaskRunning))
	err := runOrgLogin([]string{srv.URL, "--user", "bob"})
	if err == nil || !strings.Contains(err.Error(), "don't match") {
		t.Fatalf("a wrong password answered %v", err)
	}
	if _, err := os.Stat(doorStatePath()); err == nil {
		t.Error("a refused sign-in left a sign-ins file")
	}
}

// Afi does it when the organization's agents run on its server: the task is
// made running in the only workstream the person can add to, owned by them
// (no owner is sent, so the server makes the asker the owner), and followed to
// Done with what it made. Without a terminal a line is printed only when what
// is happening changes, and the last line says Made with WorkAF.
func TestDoorStartsAfiInTheOnlyWorkstreamAndFollowsItToDone(t *testing.T) {
	out := doorTest(t, "", false, false)
	running := func(now string) func(*factory.WorkstreamTask) {
		return func(t *factory.WorkstreamTask) { t.Run = &factory.TaskRunNow{Live: true, Now: now} }
	}
	d, srv := newDoorServer(t,
		doorTask("tk_1", factory.TaskRunning),
		doorTask("tk_1", factory.TaskRunning, running("read the workstream")),
		doorTask("tk_1", factory.TaskRunning, running("read the workstream")),
		doorTask("tk_1", factory.TaskRunning, running("made the page Release notes")),
		doorTask("tk_1", factory.TaskDone, func(t *factory.WorkstreamTask) {
			t.Results = []factory.TaskResult{
				{Kind: "url", URL: "https://github.com/acme/app/pull/42", Label: "Pull request 42"},
				{Kind: "file", FileID: "fl_1"},
			}
		}),
	)
	signIn(t, srv)
	out.Reset()
	if err := runTask([]string{"Write the release notes for 0.9"}); err != nil {
		t.Fatalf("task: %v\n%s", err, out)
	}
	if d.created["state"] != factory.TaskRunning {
		t.Errorf("the task was made %v, want running (Afi starts it)", d.created["state"])
	}
	if by, _ := d.created["done_by"].(map[string]any); by["kind"] != factory.TaskByAfi {
		t.Errorf("done_by was %v, want Afi", d.created["done_by"])
	}
	if _, sent := d.created["owner"]; sent {
		t.Errorf("an owner was sent (%v); the person who asks owns the task", d.created["owner"])
	}
	text := out.String()
	if n := strings.Count(text, "read the workstream"); n != 1 {
		t.Errorf("an unchanged step was printed %d times, want once:\n%s", n, text)
	}
	for _, want := range []string{
		"Started in Spring launch: Write the release notes for 0.9 (tk_1) · Afi does it",
		"Running · made the page Release notes",
		"Done · ",
		"Pull request 42  https://github.com/acme/app/pull/42",
		"Release notes (in the workstream's Files)",
		"Open it in WorkAF: Spring launch › Write the release notes for 0.9",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(text), doorMadeWith) {
		t.Errorf("the door's message does not end with %q:\n%s", doorMadeWith, text)
	}
}

func TestDoorAsksForAWorkstreamByNameWithoutATerminal(t *testing.T) {
	out := doorTest(t, "", false, false)
	d, srv := newDoorServer(t, doorTask("tk_1", factory.TaskDone))
	d.workstream = append(d.workstream, map[string]any{"id": "ws_3", "name": "Press kit", "my_role": "full"})
	signIn(t, srv)
	err := runTask([]string{"Write the release notes"})
	if err == nil || !strings.Contains(err.Error(), `"Spring launch", "Press kit"`) || strings.Contains(err.Error(), "Customer onboarding") {
		t.Fatalf("with two workstreams and no terminal, the door answered %v (want the two you can add to named)", err)
	}
	if d.created != nil {
		t.Error("a task was made without knowing where")
	}
	out.Reset()
	if err := runTask([]string{"Write the release notes", "--workstream", "press", "--no-follow"}); err != nil {
		t.Fatalf("--workstream press: %v", err)
	}
	if !strings.Contains(out.String(), "Started in Press kit") || !strings.Contains(out.String(), doorMadeWith) {
		t.Errorf("--workstream by the start of its name did not start it there:\n%s", out)
	}
}

// --me is the person's own task: done by them, not started, not followed. And
// an organization whose agents run on someone's computer gets a person task by
// default.
func TestDoorMeMakesThePersonsOwnTask(t *testing.T) {
	out := doorTest(t, "", false, false)
	d, srv := newDoorServer(t, doorTask("tk_1", factory.TaskUpNext))
	d.runner = "driver"
	signIn(t, srv)
	if err := runTask([]string{"Book the venue"}); err != nil {
		t.Fatalf("task: %v", err)
	}
	by, _ := d.created["done_by"].(map[string]any)
	if by["kind"] != factory.TaskByPerson || by["id"] != "bob" || d.created["state"] != nil {
		t.Errorf("made %v, want an up-next task done by bob", d.created)
	}
	if !strings.Contains(out.String(), "yours, for you to do") || d.reads != 0 {
		t.Errorf("a person's task was followed, or not said to be theirs:\n%s", out)
	}
}

// A quick Ask of the person's own (reversible, with a recommendation) is put
// to them on the terminal, and Enter answers with the recommended option.
func TestDoorAnswersAQuickAskInlineWithEnter(t *testing.T) {
	out := doorTest(t, "\n", true, false)
	ask := &factory.TaskAsk{ID: "ask_1", Title: "Tone: formal or casual?", Reversible: true,
		By:      &factory.TaskEventBy{Kind: factory.TaskByAfi},
		Options: []factory.TaskAskOption{{ID: "o1", Label: "Formal"}, {ID: "o2", Label: "Casual", Recommended: true}}}
	d, srv := newDoorServer(t,
		doorTask("tk_1", factory.TaskRunning),
		doorTask("tk_1", factory.TaskNeeds, func(t *factory.WorkstreamTask) { t.Ask = ask }),
		doorTask("tk_1", factory.TaskDone),
	)
	signIn(t, srv)
	if err := runTask([]string{"Pick a tagline", "--workstream", "ws_1"}); err != nil {
		t.Fatalf("task: %v\n%s", err, out)
	}
	if d.answered["option"] != "o2" || d.answered["ask"] != "ask_1" {
		t.Errorf("Enter answered %v, want the recommended option o2 of ask_1", d.answered)
	}
	for _, want := range []string{"Afi asks: Tone: formal or casual?", "2. Casual (recommended)", "Enter takes 2", "Answered: Casual"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// Anything that is not a quick Ask of yours is never answered in the door: a
// decision says to open WorkAF, an Ask someone else decides names them, and
// the door keeps following until the task ends.
func TestDoorPointsADecisionAtWorkAFAndKeepsFollowing(t *testing.T) {
	out := doorTest(t, "1\n", true, false)
	decision := &factory.TaskAsk{ID: "ask_1", Title: "Send it to journalists today?",
		Options: []factory.TaskAskOption{{ID: "o1", Label: "Wait", Recommended: true}, {ID: "o2", Label: "Send"}}}
	theirs := factory.TaskAsk{ID: "ask_2", Title: "Approve the budget?", Reversible: true,
		Decider: &factory.PersonRef{ID: "ann", Name: "Ann"},
		Options: []factory.TaskAskOption{{ID: "o1", Label: "Yes", Recommended: true}}}
	words := factory.TaskAsk{ID: "ask_3", Title: "Which dates?", Reversible: true,
		Decider: &factory.PersonRef{ID: "@Bob", Name: "Bob Lee"},
		Options: []factory.TaskAskOption{{ID: "o1", Label: "These", Recommended: true, Words: true}}}
	d, srv := newDoorServer(t,
		doorTask("tk_1", factory.TaskRunning),
		doorTask("tk_1", factory.TaskNeeds, func(t *factory.WorkstreamTask) {
			t.Ask, t.Asks = decision, []factory.TaskAsk{theirs, words}
		}),
		doorTask("tk_1", factory.TaskNeeds, func(t *factory.WorkstreamTask) {
			t.Ask, t.Asks = decision, []factory.TaskAsk{theirs, words}
		}),
		doorTask("tk_1", factory.TaskStopped, func(t *factory.WorkstreamTask) { t.StoppedBy = &factory.PersonRef{ID: "ann", Name: "Ann"} }),
	)
	signIn(t, srv)
	err := runTask([]string{"Announce the press kit", "--workstream", "Spring launch"})
	if status, ok := err.(exitStatus); !ok || status != 1 {
		t.Fatalf("a stopped task left with %v, want exit 1", err)
	}
	if d.answered != nil {
		t.Errorf("the door answered %v; none of these may be answered in a door", d.answered)
	}
	text := out.String()
	for _, want := range []string{
		"This one is a decision: open WorkAF to answer it (Spring launch › Write the release notes for 0.9).",
		"Waiting on Ann to answer: Approve the budget?",
		"Afi asks: Which dates?\nThis one is a decision",
		"Stopped by Ann",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if n := strings.Count(text, "Send it to journalists today?\nThis one"); n != 1 {
		t.Errorf("the decision was said %d times, want once:\n%s", n, text)
	}
}

// `codeaf task follow <id>` finds a task this terminal started without being
// told its workstream, and redraws one line in place on a terminal.
func TestDoorFollowReattachesToAKeptTaskOnOneLine(t *testing.T) {
	out := doorTest(t, "", false, true)
	_, srv := newDoorServer(t,
		doorTask("tk_9", factory.TaskRunning),
		doorTask("tk_9", factory.TaskRunning, func(t *factory.WorkstreamTask) { t.Run = &factory.TaskRunNow{Now: "read a file"} }),
		doorTask("tk_9", factory.TaskDone),
	)
	signIn(t, srv)
	s, _ := readDoorState()
	s.remember(doorTaskRef{Org: srv.URL, WS: "ws_1", Task: "tk_9"})
	if err := writeDoorState(s); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runTaskFollow([]string{"tk_9"}); err != nil {
		t.Fatalf("follow: %v\n%s", err, out)
	}
	text := out.String()
	if !strings.Contains(text, "\r\x1b[KRunning · read a file · ") || !strings.Contains(text, "\r\x1b[KDone · ") {
		t.Errorf("the line was not redrawn in place:\n%q", text)
	}
	if strings.Count(text, "\n") > 4 {
		t.Errorf("following on a terminal printed line after line:\n%q", text)
	}
	if err := runTaskFollow([]string{"tk_unknown"}); err == nil || !strings.Contains(err.Error(), "no task tk_unknown") {
		t.Errorf("an unknown task answered %v", err)
	}
}

func TestDoorSaysHowToSignInWhenTheSessionEnded(t *testing.T) {
	doorTest(t, "", false, false)
	d, srv := newDoorServer(t, doorTask("tk_1", factory.TaskRunning))
	signIn(t, srv)
	d.token = "a_new_token"
	err := runTask([]string{"Anything", "--workstream", "ws_1"})
	if err == nil || !strings.Contains(err.Error(), "codeaf org login "+srv.URL) {
		t.Fatalf("an ended sign-in answered %v", err)
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	if err := runTask([]string{"Anything"}); err == nil || !strings.Contains(err.Error(), "codeaf org login <server-url>") {
		t.Fatalf("a terminal never signed in answered %v", err)
	}
}

func TestDoorLineWords(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	made := now.Add(-(3*time.Minute + 5*time.Second)).Format(time.RFC3339Nano)
	for _, row := range []struct {
		task factory.WorkstreamTask
		want string
	}{
		{factory.WorkstreamTask{State: factory.TaskUpNext, CreatedAt: made, Queue: &factory.TaskQueue{Place: 2}}, "Queued · 2nd · 3m 05s"},
		{factory.WorkstreamTask{State: factory.TaskRunning, CreatedAt: made, Run: &factory.TaskRunNow{Now: "made the page\nRelease notes"}}, "Running · made the page Release notes · 3m 05s"},
		{factory.WorkstreamTask{State: factory.TaskFailed, CreatedAt: made, StateReason: "the model key ran out"}, "Failed · the model key ran out · 3m 05s"},
		{factory.WorkstreamTask{State: factory.TaskStopped, StoppedBy: &factory.PersonRef{ID: "bob"}}, "Stopped by you"},
		{factory.WorkstreamTask{State: factory.TaskNoReport}, "Didn't report back"},
	} {
		if got := doorLine(row.task, "bob", now); got != row.want {
			t.Errorf("doorLine = %q, want %q", got, row.want)
		}
	}
	if got := doorDuration(2*time.Hour + 4*time.Minute); got != "2h 04m" {
		t.Errorf("doorDuration = %q", got)
	}
	if title, request := doorTitle("first line\nsecond line"); title != "first line" || request != "first line\nsecond line" {
		t.Errorf("doorTitle split %q / %q", title, request)
	}
}

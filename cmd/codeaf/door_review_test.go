package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// Only a 401 is a wrong password. A 403 is the server refusing the sign-in for
// a reason of its own, and the person reads the server's sentence.
func TestDoorSignInPassesAForbiddenSentenceThrough(t *testing.T) {
	doorTest(t, "correct horse battery\n", false, false)
	d, srv := newDoorServer(t, doorTask("tk_1", factory.TaskRunning))
	d.refuseSignIn, d.refuseSentence = http.StatusForbidden, "passwords are only taken over https from another machine"
	err := runOrgLogin([]string{srv.URL, "--user", "bob"})
	if err == nil || strings.Contains(err.Error(), "don't match") || !strings.Contains(err.Error(), d.refuseSentence) {
		t.Fatalf("a 403 sign-in answered %v, want the server's sentence", err)
	}
}

// A plain http address on another machine is refused before the password is
// asked for, or anything is sent to it.
func TestDoorSignInRefusesPlainHTTPOffThisMachine(t *testing.T) {
	out := doorTest(t, "", true, false)
	asked := false
	old := doorReadPassword
	doorReadPassword = func() ([]byte, error) { asked = true; return []byte("pw"), nil }
	t.Cleanup(func() { doorReadPassword = old })
	err := runOrgLogin([]string{"http://org.example.com:8080", "--user", "bob"})
	if err == nil || !strings.Contains(err.Error(), "https://") {
		t.Fatalf("plain http to another machine answered %v, want a refusal that says to use https", err)
	}
	if asked || strings.Contains(out.String(), "Password") {
		t.Errorf("the password was asked for before the refusal:\n%s", out)
	}
	for _, host := range []string{"localhost", "127.0.0.1", "::1", "app.localhost"} {
		if !doorLoopback(host) {
			t.Errorf("%s is this machine; plain http there is fine", host)
		}
	}
	for _, base := range []string{"https://org.example.com", "http://127.0.0.1:9000", "http://[::1]:9000"} {
		if err := doorPlainHTTPRefused(base); err != nil {
			t.Errorf("%s was refused: %v", base, err)
		}
	}
}

// A sign-ins file that cannot be read is refused, never replaced by the one
// new sign-in: it may hold every other organization this person signed in to.
func TestDoorSignInRefusesToReplaceAnUnreadableSignInsFile(t *testing.T) {
	doorTest(t, "correct horse battery\n", false, false)
	_, srv := newDoorServer(t, doorTask("tk_1", factory.TaskRunning))
	if err := os.MkdirAll(filepath.Dir(doorStatePath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(doorStatePath(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runOrgLogin([]string{srv.URL, "--user", "bob"}); err == nil {
		t.Fatal("a sign-in over an unreadable sign-ins file succeeded")
	}
	body, _ := os.ReadFile(doorStatePath())
	if string(body) != "{not json" {
		t.Errorf("the unreadable sign-ins file was replaced:\n%s", body)
	}
}

// Door commands that finish at once each keep their change: the sign-ins are
// read again under the lock, never written from a stale copy.
func TestDoorStateUpdatesDoNotWriteOverEachOther(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- updateDoorState(func(s *doorState) {
				s.remember(doorTaskRef{Org: "https://org", WS: "ws_1", Task: fmt.Sprintf("tk_%d", i)})
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	s, err := readDoorState()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Tasks) != 20 {
		t.Errorf("kept %d of 20 tasks remembered at once", len(s.Tasks))
	}
}

// Words from the server never reach the terminal as control sequences.
func TestDoorTextStripsControlAndFormatCharacters(t *testing.T) {
	for _, row := range []struct{ in, line, block string }{
		{"plain words", "plain words", "plain words"},
		{"red\x1b[31m text\x07", "red[31m text", "red[31m text"},
		{"title\x1b]0;pwned\x07", "title]0;pwned", "title]0;pwned"},
		{"a\tb\r\nc\rd", "a b cd", "a b\ncd"},
		{"csi\u009b2Jhere", "csi2Jhere", "csi2Jhere"},
		{"rtl‮evil​", "rtlevil", "rtlevil"},
		{"line one\nline two", "line one line two", "line one\nline two"},
	} {
		if got := doorText(row.in); got != row.line {
			t.Errorf("doorText(%q) = %q, want %q", row.in, got, row.line)
		}
		if got := doorBlock(row.in); got != row.block {
			t.Errorf("doorBlock(%q) = %q, want %q", row.in, got, row.block)
		}
	}
	// The followed line, an Ask and the finish all print through it.
	out := doorTest(t, "\n", true, false)
	ask := &factory.TaskAsk{ID: "ask_1", Title: "Tone\x1b[2J?", Detail: "first\x1b]0;x\x07\nsecond", Reversible: true,
		Options: []factory.TaskAskOption{{ID: "o1", Label: "Casual\x1b[0m", Recommended: true}}}
	_, srv := newDoorServer(t,
		doorTask("tk_1", factory.TaskRunning),
		doorTask("tk_1", factory.TaskRunning, func(t *factory.WorkstreamTask) { t.Run = &factory.TaskRunNow{Now: "reading\x1b[1A\x1b[K"} }),
		doorTask("tk_1", factory.TaskNeeds, func(t *factory.WorkstreamTask) { t.Ask = ask }),
		doorTask("tk_1", factory.TaskDone, func(t *factory.WorkstreamTask) {
			t.Results = []factory.TaskResult{{Kind: "url", URL: "https://x.test/\x1b[8m", Label: "PR\x9b"}}
		}),
	)
	signIn(t, srv)
	out.Reset()
	if err := runTask([]string{"Pick a tagline", "--workstream", "ws_1"}); err != nil {
		t.Fatalf("task: %v\n%s", err, out)
	}
	if strings.ContainsAny(out.String(), "\x1b\x07\u009b") {
		t.Errorf("server text reached the terminal raw:\n%q", out)
	}
	if !strings.Contains(out.String(), "  first]0;x\n  second") {
		t.Errorf("a multi-line detail lost its lines:\n%q", out)
	}
}

// An Ask whose answer the server did not take is asked again, and one that is
// answered, closes, and opens again (its answer undone) is asked again too.
func TestDoorAsksAgainAfterAFailedAnswerAndAReopenedAsk(t *testing.T) {
	out := doorTest(t, "\n\n\n", true, false)
	ask := &factory.TaskAsk{ID: "ask_1", Title: "Tone?", Reversible: true,
		Options: []factory.TaskAskOption{{ID: "o1", Label: "Casual", Recommended: true}}}
	needs := func(t *factory.WorkstreamTask) { t.Ask = ask }
	d, srv := newDoorServer(t,
		doorTask("tk_1", factory.TaskRunning),
		doorTask("tk_1", factory.TaskNeeds, needs),
		doorTask("tk_1", factory.TaskNeeds, needs), // read after the failed answer
		doorTask("tk_1", factory.TaskRunning),      // the answer's reply
		doorTask("tk_1", factory.TaskNeeds, needs), // undone: open again
		doorTask("tk_1", factory.TaskDone),         // the second answer's reply
	)
	d.answerFails = 1
	signIn(t, srv)
	if err := runTask([]string{"Pick a tagline", "--workstream", "ws_1"}); err != nil {
		t.Fatalf("task: %v\n%s", err, out)
	}
	if d.answers != 2 {
		t.Errorf("the server took %d answers, want 2 (after the failed one, and after the undo):\n%s", d.answers, out)
	}
	if n := strings.Count(out.String(), "Afi asks: Tone?"); n != 3 {
		t.Errorf("the Ask was put %d times, want 3:\n%s", n, out)
	}
}

// The person decides an Ask whose decider is their handle written another way,
// and one the server says they cover as the workstream's fallback.
func TestDoorAnswersAsTheDeciderByHandleAndAsTheFallback(t *testing.T) {
	out := doorTest(t, "\n\n", true, false)
	asked := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339Nano)
	spelled := factory.TaskAsk{ID: "ask_2", Title: "Spelled?", Reversible: true, AskedAt: asked,
		Decider: &factory.PersonRef{ID: "@Bob", Name: "Bob Lee"},
		Options: []factory.TaskAskOption{{ID: "o1", Label: "Yes", Recommended: true}}}
	covered := factory.TaskAsk{ID: "ask_3", Title: "Covered?", Reversible: true, AskedAt: asked,
		Decider:   &factory.PersonRef{ID: "ann", Name: "Ann"},
		CoveredBy: &factory.PersonRef{ID: "bob", Name: "Bob Lee"},
		Options:   []factory.TaskAskOption{{ID: "o1", Label: "Yes", Recommended: true}}}
	d, srv := newDoorServer(t,
		doorTask("tk_1", factory.TaskRunning),
		doorTask("tk_1", factory.TaskNeeds, func(t *factory.WorkstreamTask) {
			t.Owner = factory.TaskOwner{Kind: factory.TaskOwnerPerson, ID: "cat", Name: "Cat"}
			t.Asks = []factory.TaskAsk{spelled, covered}
		}),
		doorTask("tk_1", factory.TaskNeeds, func(t *factory.WorkstreamTask) {
			t.Owner = factory.TaskOwner{Kind: factory.TaskOwnerPerson, ID: "cat", Name: "Cat"}
			t.Asks = []factory.TaskAsk{covered}
		}),
		doorTask("tk_1", factory.TaskDone),
	)
	signIn(t, srv)
	if err := runTask([]string{"Pick a tagline", "--workstream", "ws_1"}); err != nil {
		t.Fatalf("task: %v\n%s", err, out)
	}
	if d.answers != 2 || d.answered["ask"] != "ask_3" {
		t.Errorf("answered %d Asks, last %v; want both, ask_3 last:\n%s", d.answers, d.answered, out)
	}
	if strings.Contains(out.String(), "Waiting on Ann") {
		t.Errorf("an Ask the person may answer was said to wait on someone else:\n%s", out)
	}
}

// An answer the server refuses (answered meanwhile, not theirs, not taken)
// says the same thing the next time, so it is left for WorkAF, not asked
// again at every read.
func TestDoorLeavesARefusedAnswerForWorkAF(t *testing.T) {
	out := doorTest(t, "\n\n\n", true, false)
	ask := &factory.TaskAsk{ID: "ask_1", Title: "Tone?", Reversible: true,
		Options: []factory.TaskAskOption{{ID: "o1", Label: "Casual", Recommended: true}}}
	needs := func(t *factory.WorkstreamTask) { t.Ask = ask }
	d, srv := newDoorServer(t,
		doorTask("tk_1", factory.TaskRunning),
		doorTask("tk_1", factory.TaskNeeds, needs),
		doorTask("tk_1", factory.TaskNeeds, needs),
		doorTask("tk_1", factory.TaskNeeds, needs),
		doorTask("tk_1", factory.TaskDone),
	)
	d.answerStatus = http.StatusConflict
	signIn(t, srv)
	if err := runTask([]string{"Pick a tagline", "--workstream", "ws_1"}); err != nil {
		t.Fatalf("task: %v\n%s", err, out)
	}
	if n := strings.Count(out.String(), "Afi asks: Tone?"); n != 1 {
		t.Errorf("a refused answer was put %d times, want once:\n%s", n, out)
	}
	if !strings.Contains(out.String(), "Answer it in WorkAF") || strings.Contains(out.String(), "\x1b[2J") {
		t.Errorf("the refusal was not left for WorkAF, or printed raw:\n%q", out)
	}
}

// Sign-in, the org listing and sign-out print the server's words cleaned.
func TestDoorOrgPrintsServerWordsCleaned(t *testing.T) {
	out := doorTest(t, "correct horse battery\n", false, false)
	d, srv := newDoorServer(t, doorTask("tk_1", factory.TaskRunning))
	d.name = "North\x1b]0;pwned\x07wind"
	if err := runOrgLogin([]string{srv.URL, "--user", "bob"}); err != nil {
		t.Fatalf("sign-in: %v", err)
	}
	if err := runOrg(nil); err != nil {
		t.Fatal(err)
	}
	if err := runOrgLogout(nil); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out.String(), "\x1b\x07") || !strings.Contains(out.String(), "Signed in to North]0;pwnedwind") {
		t.Errorf("the organization's name was printed raw:\n%q", out)
	}

	doorTest(t, "correct horse battery\n", false, false)
	d, srv = newDoorServer(t, doorTask("tk_1", factory.TaskRunning))
	d.refuseSignIn, d.refuseSentence = http.StatusForbidden, "use https\x1b[2J"
	err := runOrgLogin([]string{srv.URL, "--user", "bob"})
	if err == nil || strings.Contains(err.Error(), "\x1b") || !strings.Contains(err.Error(), "use https") {
		t.Fatalf("a 403's sentence came back %q", err)
	}
}

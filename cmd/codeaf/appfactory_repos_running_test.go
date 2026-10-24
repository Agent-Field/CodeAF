package main

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── turning off a repository with work under way ───────────────────────────

func repoRunning(marked bool, words string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		if marked {
			w.Header().Set(api.PhaseHeader, api.PhasePreMutation)
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error":"`+words+`","reason":"has_running"}`)
	}
}

// THE FACTORY'S SENTENCE IS THE ANSWER: has_running names the jobs under way,
// and it is definite with or without the phase marker, because the factory
// sends it only before it saves the list.
func TestRepoSetHasRunningCarriesTheFactorysSentence(t *testing.T) {
	for _, marked := range []bool{true, false} {
		stub := newRepoStub(t, teamOperator())
		stub.put = repoRunning(marked, "octo/hello has 1 job running (hello#12) and 1 waiting on you (hello#7).")
		r := newRepoWire(t, stub, false, api.FactoryTeam, repoToken)
		_, err := r.set("octo/hello", false)
		w := repoWantCode(t, err, appwire.CodeHasRunning)
		if w.Message != "octo/hello has 1 job running (hello#12) and 1 waiting on you (hello#7)." {
			t.Fatalf("marked=%v message %q", marked, w.Message)
		}
		if stub.puts() != 1 || stub.bodies[0] != `{"on":false}` {
			t.Fatalf("marked=%v sent %d puts, %q", marked, stub.puts(), stub.bodies)
		}
	}
}

// The person's yes rides the one write as "stop", and nothing else changes.
func TestRepoSetStopIsSentOnTheOneWrite(t *testing.T) {
	stub := newRepoStub(t, teamOperator())
	r := newRepoWire(t, stub, false, api.FactoryTeam, repoToken)
	off := false
	got, err := r.backend.FactoryRepoSet(context.Background(), appwire.FactoryRepoSetParams{Factory: r.id, Repo: "octo/hello", On: &off, Stop: true})
	if err != nil {
		t.Fatal(err)
	}
	if got != (appwire.FactoryRepoSetDone{Factory: r.id, Repo: "octo/hello", On: false}) {
		t.Fatalf("receipt %+v", got)
	}
	if stub.puts() != 1 || stub.bodies[0] != `{"on":false,"stop":true}` {
		t.Fatalf("sent %d puts, %q", stub.puts(), stub.bodies)
	}
}

func TestRepoSetAgainstTheRealFactoryAsksBeforeStoppingWork(t *testing.T) {
	f := startAdminFactory(t)
	if err := f.st.SetRepos([]string{"octo/hello"}); err != nil {
		t.Fatal(err)
	}
	job, err := f.st.Create(factory.Item{Repo: "octo/hello", Num: 12, Title: "work", State: factory.StateRunning,
		Stream: &factory.Stream{Cur: 0, Phases: []factory.Phase{{Name: "write", State: factory.PhaseRunning}}}})
	if err != nil {
		t.Fatal(err)
	}
	alice := f.signIn(t, 1, "alice")
	msg := alice.w.mustFail("factory.repo_set", map[string]any{"factory": alice.conn.ID, "repo": "octo/hello", "on": false}, appwire.CodeHasRunning)
	if msg != "octo/hello has 1 job running (hello#12)." {
		t.Fatalf("message %q", msg)
	}
	if repos, _ := f.st.Repos(); len(repos) != 1 {
		t.Fatalf("a refused switch changed the list: %v", repos)
	}
	var done appwire.FactoryRepoSetDone
	alice.w.must("factory.repo_set", map[string]any{"factory": alice.conn.ID, "repo": "octo/hello", "on": false, "stop": true}, &done)
	if repos, _ := f.st.Repos(); len(repos) != 0 || done.On {
		t.Fatalf("after stop and off: %v %+v", repos, done)
	}
	got, err := f.st.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if factory.UnderWay(got) || got.StopUndo == nil || got.StopUndo.By != "alice" {
		t.Fatalf("the job was not stopped as alice's Stop: %+v", got)
	}
}

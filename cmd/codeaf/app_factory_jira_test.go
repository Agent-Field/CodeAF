package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/factory/jira/jirastub"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

const (
	jiraStubEmail = "ada@example.com"
	jiraStubToken = "jira-secret-token-0123456789"
)

func jiraConnectParams(site, token string) map[string]any {
	return map[string]any{"factory": appLocalID, "site": site, "email": jiraStubEmail, "token": token, "jql": "project = FL"}
}

// THE INTEGRATION: a real engine behind the real wire connects this
// machine's factory to Jira (the stub), keeps the token where the GitHub
// token is kept, answers its status without the token, and disconnects. The
// connect starts a Jira poll exactly when the factory's read loops are on.
func TestJiraConnectsThroughTheWireAgainstTheStub(t *testing.T) {
	s := newLocalSetup(t)
	stub := jirastub.New(jiraStubEmail, jiraStubToken)
	t.Cleanup(stub.Close)

	s.mustFail("factory.jira_status", map[string]any{"factory": appLocalID}, appwire.CodeNotFound)
	s.start()
	s.mustFail("factory.jira_status", map[string]any{"factory": "factory_elsewhere"}, appwire.CodeNotFound)

	var st appwire.FactoryJiraStatus
	s.must("factory.jira_status", map[string]any{"factory": appLocalID}, &st)
	if st.Connected || st.Site != "" {
		t.Fatalf("a fresh factory: %+v", st)
	}
	msg := s.mustFail("factory.jira_connect", jiraConnectParams(stub.URL, "not-the-token"), appwire.CodeBadParams)
	if !strings.Contains(msg, "Jira did not accept") {
		t.Fatalf("a wrong token said %q", msg)
	}
	s.mustFail("factory.jira_connect", jiraConnectParams("http://127.0.0.1:1", jiraStubToken), appwire.CodeUnreachable)

	s.must("factory.jira_connect", jiraConnectParams(stub.URL, jiraStubToken), &st)
	if !st.Connected || st.Site != stub.URL || st.Email != jiraStubEmail || st.JQL != "project = FL" || st.Account != "Stub Person" {
		t.Fatalf("connected: %+v", st)
	}
	if got := config.JiraTokenAt(s.backend.localProfile()); got != jiraStubToken {
		t.Fatal("the token is not kept where the GitHub token is")
	}
	s.must("factory.jira_status", map[string]any{"factory": appLocalID}, &st)
	if !st.Connected || st.JQL != "project = FL" {
		t.Fatalf("status: %+v", st)
	}
	host, err := s.backend.jiraHost(appLocalID)
	if err != nil {
		t.Fatal(err)
	}
	host.loopMu.Lock()
	started, loopsOn := host.jira.on, host.loopsOn
	host.loopMu.Unlock()
	if want := map[bool]int{false: 0, true: 1}[loopsOn]; started != want {
		t.Fatalf("read loops on %v, and the connect started %d Jira polls, want %d", loopsOn, started, want)
	}

	st = appwire.FactoryJiraStatus{}
	s.must("factory.jira_disconnect", map[string]any{"factory": appLocalID}, &st)
	if st.Connected || st.Site != "" || config.JiraTokenAt(s.backend.localProfile()) != "" {
		t.Fatalf("after a disconnect: %+v", st)
	}
	s.neverSaid(jiraStubToken)
}

// The Jira poll starts with the read loops only while connected, starts
// again on a connect and stops on a disconnect; and, once the inbox is
// built, a ticket in the stub becomes a job within one poll.
func TestTheJiraPollFollowsTheConnection(t *testing.T) {
	s := newLocalSetup(t)
	stub := jirastub.New(jiraStubEmail, jiraStubToken)
	t.Cleanup(stub.Close)
	s.start()
	host, err := s.backend.jiraHost(appLocalID)
	if err != nil {
		t.Fatal(err)
	}
	host.jira.every = 20 * time.Millisecond
	afters := make(chan struct{}, 64)
	after := func(context.Context) error {
		select {
		case afters <- struct{}{}:
		default:
		}
		return nil
	}
	polls := func() int {
		host.loopMu.Lock()
		defer host.loopMu.Unlock()
		return host.jira.on
	}
	// The read loops turn on (as factory.local_run does) with no connection:
	// nothing starts.
	host.loopMu.Lock()
	host.loopsOn = true
	host.startJiraLocked(after)
	host.loopMu.Unlock()
	if polls() != 0 {
		t.Fatal("a factory with no Jira connection started a Jira poll")
	}
	key := stub.CreateIssue(jirastub.Issue{Project: "FL", Summary: "Make the button blue", Priority: "Highest"})
	s.must("factory.jira_connect", jiraConnectParams(stub.URL, jiraStubToken), nil)
	if polls() != 1 {
		t.Fatalf("a connect started %d polls, want 1", polls())
	}
	s.must("factory.jira_connect", jiraConnectParams(stub.URL, jiraStubToken), nil)
	if polls() != 2 {
		t.Fatalf("a second connect did not restart the poll: %d", polls())
	}

	if _, err := inbox.Add(context.Background(), mustStore(t), inbox.Entry{Source: "probe", ExternalID: "1", Title: "x"}, time.Now()); errors.Is(err, inbox.ErrNotBuilt) {
		t.Log("inbox.Poll and inbox.Add are still the contract's stubs on this base; the ticket-to-job half is skipped")
	} else {
		deadline := time.Now().Add(5 * time.Second)
		for !floorHasJob(host.floor, "jira", key) {
			if time.Now().After(deadline) {
				t.Fatalf("ticket %s never became a job", key)
			}
			time.Sleep(20 * time.Millisecond)
		}
		select {
		case <-afters:
		case <-time.After(5 * time.Second):
			t.Fatal("the poll never ran the intake")
		}
	}

	s.must("factory.jira_disconnect", map[string]any{"factory": appLocalID}, nil)
	host.loopMu.Lock()
	stopped := host.jira.stop == nil
	host.loopMu.Unlock()
	if !stopped || polls() != 2 {
		t.Fatalf("a disconnect left a poll running (started %d)", polls())
	}
}

func mustStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// floorHasJob says some job on st came from (source, id).
func floorHasJob(st *store.Store, source, id string) bool {
	items, err := st.List()
	if err != nil {
		return false
	}
	for _, it := range items {
		if strings.Contains(it.SourceRef, source) && strings.Contains(it.SourceRef, id) {
			return true
		}
	}
	return false
}

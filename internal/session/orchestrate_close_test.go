package session

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// This uses the real run door and a real child agent. The child has received
// cancellation but deliberately holds its last return: closing the session
// must wait for that return, rather than just cancelling its root scheduler.
func TestCloseJoinsAdaptiveWorkerAfterCancellation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	client := &closeRunCompleter{started: make(chan struct{}), cut: make(chan struct{}), release: make(chan struct{}), exited: make(chan struct{})}
	agent, _ := newTestAgent(t, client, nil)
	var released sync.Once
	release := func() { released.Do(func() { close(client.release) }) }
	t.Cleanup(release)
	if _, err := agent.RunOrchestrate(context.Background(), "read release notes", "", 5); err != nil {
		t.Fatal(err)
	}
	waitCloseRun(t, client.started, "worker start")
	closed := make(chan struct{})
	go func() { _ = agent.Close(); close(closed) }()
	waitCloseRun(t, client.cut, "worker cancellation")
	select {
	case <-closed:
		t.Fatal("Close returned while an adaptive worker still owned its journal")
	case <-time.After(25 * time.Millisecond):
	}
	release()
	waitCloseRun(t, closed, "session close after worker return")
	select {
	case <-client.exited:
	default:
		t.Fatal("Close did not join the worker")
	}
	if _, err := agent.RunOrchestrate(context.Background(), "read more notes", "", 5); err != errAgentClosed {
		t.Fatalf("closed run admission = %v, want closed", err)
	}
}

// Setup writes the root row before the run enters the registry. Close must
// own that interval too, and then shut the graph that setup created.
func TestCloseJoinsAdaptiveAdmissionBeforeRegistration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	entered, release := make(chan struct{}), make(chan struct{})
	var armed atomic.Bool
	var once, released sync.Once
	unblock := func() { released.Do(func() { close(release) }) }
	agent, _ := newTestAgent(t, replier(func([]ai.Message) string { return "{}" }), func(c *Config) {
		c.RolesSource = func(string) (string, bool) {
			if armed.Load() {
				once.Do(func() { close(entered); <-release })
			}
			return "", false
		}
	})
	t.Cleanup(unblock)
	armed.Store(true)
	started := make(chan error, 1)
	go func() {
		_, err := agent.RunOrchestrate(context.Background(), "read release notes", "", 5)
		started <- err
	}()
	waitCloseRun(t, entered, "accepted setup")
	closed := make(chan struct{})
	go func() { _ = agent.Close(); close(closed) }()
	// Observe the admission barrier itself, rather than assuming the close
	// goroutine has been scheduled when this test examines its result.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		agent.mu.Lock()
		closing := agent.closed
		agent.mu.Unlock()
		if closing {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("Close never reached its admission barrier")
		case <-time.After(time.Millisecond):
		}
	}
	select {
	case <-closed:
		t.Fatal("Close forgot accepted setup that had not entered the registry")
	case <-time.After(25 * time.Millisecond):
	}
	unblock()
	waitCloseRun(t, closed, "setup join")
	if err := <-started; err != errAgentClosed {
		t.Fatalf("setup crossing Close returned %v, want closed", err)
	}
	agent.mu.Lock()
	graph := agent.tasks
	agent.mu.Unlock()
	if graph != nil {
		graph.mu.Lock()
		quitting := graph.quitting
		graph.mu.Unlock()
		if !quitting {
			t.Fatal("the graph created during accepted setup was not closed")
		}
	}
}

// A finished run still wants its useful name. The name is cancelled at the
// session boundary, not at the earlier moment the final snapshot lands.
func TestCloseCancelsAndJoinsTheFinishedRunsName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	client := &closeRunNameCompleter{started: make(chan struct{}), cut: make(chan struct{}), release: make(chan struct{})}
	agent, _ := newTestAgent(t, client, nil)
	var released sync.Once
	release := func() { released.Do(func() { close(client.release) }) }
	t.Cleanup(release)
	id, err := agent.RunOrchestrate(context.Background(), "read the release notes carefully", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	waitCloseRun(t, client.started, "the root name")
	waitForRun(t, agent, id)
	select {
	case <-client.cut:
		t.Fatal("normal run completion cancelled its useful name")
	default:
	}
	closed := make(chan struct{})
	go func() { _ = agent.Close(); close(closed) }()
	waitCloseRun(t, client.cut, "the name's session cancellation")
	select {
	case <-closed:
		t.Fatal("Close forgot the finished run's outstanding name")
	case <-time.After(25 * time.Millisecond):
	}
	release()
	waitCloseRun(t, closed, "name completion")
}

type closeRunNameCompleter struct {
	started, cut, release chan struct{}
}

func (c *closeRunNameCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	if isNameCall(messages) {
		close(c.started)
		<-ctx.Done()
		close(c.cut)
		<-c.release
		return nil, ctx.Err()
	}
	if isPlannerCall(messages) {
		return textResponse(`{}`), nil
	}
	return textResponse("read"), nil
}

type closeRunCompleter struct {
	started, cut, release, exited chan struct{}
}

func (c *closeRunCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	if isPlannerCall(messages) {
		return textResponse(`{"add":[{"id":"read-notes","title":"release notes","goal":"read release notes"}]}`), nil
	}
	close(c.started)
	<-ctx.Done()
	close(c.cut)
	<-c.release
	defer close(c.exited)
	return nil, ctx.Err()
}

func waitCloseRun(t *testing.T, signal <-chan struct{}, why string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", why)
	}
}

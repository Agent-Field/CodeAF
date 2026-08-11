package chat

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2"
)

// notice is one interruption this surface decided to spend.
type notice struct {
	kind tui2.AttentionKind
	body string
}

// watchNotices replaces the shell's notifier with a recorder. The real one
// suppresses almost everything — an unnegotiated terminal, a focused window, a
// repeat — which is right, and which makes "did this surface decide to
// interrupt" unobservable from the outside.
func watchNotices(app *App) *[]notice {
	seen := &[]notice{}
	app.notify = func(kind tui2.AttentionKind, _, body string) tea.Cmd {
		*seen = append(*seen, notice{kind: kind, body: body})
		return nil
	}
	return seen
}

// 10.5.27's progress channel, and its whole width: busy while a turn streams in
// this room, cleared when it stops. It never becomes a notification.
func TestTheProgressChannelTracksTheLiveTurn(t *testing.T) {
	backend := &fakeBackend{}
	app := newTestApp(backend, &fakeCommander{}, nil)
	seen := watchNotices(app)
	if app.shell.Busy() {
		t.Fatal("an idle window reported a turn in flight")
	}

	stream(app, StreamEvent{Kind: StreamStarted, Session: testSession})
	if !app.shell.Busy() {
		t.Fatal("a streaming turn did not raise the progress channel")
	}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent, Body: "done"})
	poll(t, app)
	if app.shell.Busy() {
		t.Fatal("the progress channel stayed raised after the turn settled")
	}
	if len(*seen) != 0 {
		t.Fatalf("progress became a notification: %#v", *seen)
	}
}

// The count in the terminal's title is the count the footer paints amber, read
// from the same place so the two cannot disagree.
func TestTheTitleCarriesTheSameAttentionCountAsTheFooter(t *testing.T) {
	backend := &fakeBackend{}
	app := newTestApp(backend, &fakeCommander{}, nil)
	if app.shell.Attention() != 0 {
		t.Fatalf("an idle window claims %d things waiting", app.shell.Attention())
	}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent,
		Body: "which branch?", Options: []store.QuestionOption{{Label: "main"}, {Label: "chat-v2"}}})
	poll(t, app)
	if app.shell.Attention() != app.status.attention || app.shell.Attention() == 0 {
		t.Fatalf("title says %d, footer says %d", app.shell.Attention(), app.status.attention)
	}
}

// The needs-input event fires where the ask ARRIVES — once, when it is asked.
// Driven off the standing count it would fire again on every poll for as long
// as nobody answered, which is the budget spent on one event forever.
func TestAQuestionInterruptsOnceAndNotPerPoll(t *testing.T) {
	backend := &fakeBackend{}
	app := newTestApp(backend, &fakeCommander{}, nil)
	seen := watchNotices(app)
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent,
		Body: "which branch?", Options: []store.QuestionOption{{Label: "main"}, {Label: "chat-v2"}}})
	poll(t, app)
	if len(*seen) != 1 || (*seen)[0].kind != tui2.AttentionNeedsInput {
		t.Fatalf("one arriving question raised %#v", *seen)
	}
	if !strings.Contains((*seen)[0].body, "which branch") {
		t.Fatalf("the notice does not say what was asked: %q", (*seen)[0].body)
	}
	backend.journal++
	poll(t, app)
	if len(*seen) != 1 {
		t.Fatalf("the same unanswered question interrupted again: %#v", *seen)
	}
}

// The reader's own turn never interrupts them.
func TestTheReadersOwnTurnRaisesNothing(t *testing.T) {
	backend := &fakeBackend{}
	app := newTestApp(backend, &fakeCommander{}, nil)
	seen := watchNotices(app)
	backend.add(store.Message{SessionID: testSession, Role: store.RoleUser, Body: "hello?"})
	poll(t, app)
	if len(*seen) != 0 {
		t.Fatalf("the reader's own message interrupted them: %#v", *seen)
	}
}

// Delivery and failure are TRANSITIONS. A window that announced the board it
// found on attach would have spent the budget on history.
func TestDeliveryAndFailureFireOnTheChangeAndNotOnTheState(t *testing.T) {
	app, backend := boardApp(t)
	seen := watchNotices(app)
	// The board already holds a settled job. The first pass is silent about it.
	backend.journal++
	poll(t, app)
	if len(*seen) != 0 {
		t.Fatalf("attaching to a board announced its history: %#v", *seen)
	}

	for i := range backend.nodes {
		if backend.nodes[i].ID == "job-1" {
			backend.nodes[i].Status = store.Failed
		}
	}
	backend.journal++
	poll(t, app)
	if len(*seen) != 1 || (*seen)[0].kind != tui2.AttentionFailure {
		t.Fatalf("a job that failed raised %#v", *seen)
	}
	if !strings.Contains((*seen)[0].body, "wisp-parity") {
		t.Fatalf("the failure notice does not name the work: %q", (*seen)[0].body)
	}

	backend.journal++
	poll(t, app)
	if len(*seen) != 1 {
		t.Fatalf("a job that was already failed interrupted again: %#v", *seen)
	}
}

// The hook contract's hard rule (shell.go): a returned command must be returned
// onwards, because the command IS the write. The queue exists so no branch can
// drop one, and Update is the one door that drains it.
func TestEveryHookCommandLeavesThroughUpdate(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	app.hooks = append(app.hooks, func() tea.Msg { return nil })
	if _, cmd := app.Update(pollTickMsg{}); cmd == nil {
		t.Fatal("Update dropped a queued hook command")
	}
	if len(app.hooks) != 0 {
		t.Fatalf("the hook queue was not drained: %d left", len(app.hooks))
	}
}

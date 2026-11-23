package main

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// steeringConversation is the fake conversation with a door for a marked
// correction, as a hosted engine that says it keeps one has.
type steeringConversation struct {
	*fakeConversation
	steers []session.Submission
}

func (s *steeringConversation) SteerMessage(message session.Submission) (<-chan session.Event, error) {
	s.mu.Lock()
	s.steers = append(s.steers, message)
	s.mu.Unlock()
	return s.fakeConversation.Steer(message.Text)
}

// olderHostConversation is a conversation whose host said in its welcome that
// it keeps no mark on any door.
type olderHostConversation struct{ *steeringConversation }

func (olderHostConversation) MessageMarksKnown() bool { return false }
func (olderHostConversation) SteerMarksKnown() bool   { return false }

// markBackend is workBackend over any conversation.
func markBackend(t *testing.T, conv appConversation) (*appBackend, *appRecorder, appwire.Agent) {
	t.Helper()
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: appFakeTranscript, Release: func() {}}, nil
	})
	t.Cleanup(func() { backend.Close() })
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return backend, rec, agent
}

// appSubmissionsOf is what the fake's Submit door was handed, copied.
func appSubmissionsOf(conv *fakeConversation) ([]session.Submission, []string) {
	conv.mu.Lock()
	defer conv.mu.Unlock()
	return append([]session.Submission(nil), conv.submissions...), append([]string(nil), conv.texts...)
}

// appMarkedSubmission is the submission a message with said and reads is.
func appMarkedSubmission(t *testing.T, text, said string, reads ...appwire.MessageRead) session.Submission {
	t.Helper()
	kept, err := appKeptReads(reads)
	if err != nil {
		t.Fatal(err)
	}
	return session.Submission{Text: text, Said: &said, Reads: kept}
}

// A QUEUED MESSAGE KEEPS ITS WHOLE MARK AND SHOWS ONLY THE PERSON'S WORDS. The
// queue shows the person's own words and a copy of the reads, never the
// expanded words; a refused replacement — a read that lies, an attachment —
// leaves the queue exactly as it was; a marked edit replaces the whole mark; a
// plain edit drops it. When the turn ends the marked message goes by the
// marked door with the expanded words, the plain one by Submit, and the
// accepted user entry shows the person's words and reads.
func TestAppQueuedMessageKeepsItsMarkAndShowsThePersonsWords(t *testing.T) {
	hold := make(chan struct{})
	conv := &fakeConversation{hold: hold}
	backend, rec, agent := workBackend(t, conv)
	if _, err := backend.Send(agent.ID, "first", ""); err != nil {
		t.Fatal(err)
	}
	said := "then the specs"
	body := "then the specs\n\nSpecs:\n- docs/a.md\n- https://example.com/b"
	queued, err := backend.QueueMessage(agent.ID, appwire.AgentMessage{Text: body, Said: &said, ClientID: "q-1", Reads: []appwire.MessageRead{appSpecsRead()}})
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != 1 || queued[0].Text != said || !reflect.DeepEqual(queued[0].Reads, []appwire.MessageRead{appSpecsRead()}) {
		t.Fatalf("the queue shows %+v", queued)
	}
	first := queued[0].ID
	queued[0].Reads[0].Items[0].Path = "changed by the app"
	queued[0].Reads[0].Warnings[0] = "changed by the app"
	if again := backend.queued(backend.agent(agent.ID)); !reflect.DeepEqual(again[0].Reads, []appwire.MessageRead{appSpecsRead()}) {
		t.Fatalf("the queue's reads were the app's to change: %+v", again[0].Reads)
	}
	queued, err = backend.Queue(agent.ID, "and tag it", "q-2")
	if err != nil {
		t.Fatal(err)
	}
	second := queued[1].ID
	before := backend.queued(backend.agent(agent.ID))

	lying := appCutRead()
	lying.Complete = true
	for _, refused := range []appwire.AgentMessage{
		{Text: "x", Said: &said, Reads: []appwire.MessageRead{lying}},
		{Text: "x", Said: &said, Attachments: []string{"at1"}},
		{Text: "x", Reads: make([]appwire.MessageRead, session.MaxMessageReads+1)},
	} {
		if _, err := backend.EditQueuedMessage(agent.ID, first, refused); wireCode(err) != appwire.CodeBadParams {
			t.Fatalf("a refused replacement answered %v", err)
		}
		if _, err := backend.QueueMessage(agent.ID, refused); wireCode(err) != appwire.CodeBadParams {
			t.Fatalf("a refused queued message answered %v", err)
		}
	}
	if after := backend.queued(backend.agent(agent.ID)); !reflect.DeepEqual(after, before) {
		t.Fatalf("a refusal changed the queue from %+v to %+v", before, after)
	}

	none := ""
	links := "Links:\n- https://example.com/c"
	if queued, err = backend.EditQueuedMessage(agent.ID, second, appwire.AgentMessage{Text: links, Said: &none, Reads: []appwire.MessageRead{appCutRead()}}); err != nil {
		t.Fatal(err)
	}
	if queued[1].Text != "" || !reflect.DeepEqual(queued[1].Reads, []appwire.MessageRead{appCutRead()}) {
		t.Fatalf("the marked edit shows %+v", queued[1])
	}
	if queued, err = backend.EditQueued(agent.ID, first, "then the README"); err != nil {
		t.Fatal(err)
	}
	if queued[0].Text != "then the README" || queued[0].Reads != nil {
		t.Fatalf("a plain edit kept the old mark: %+v", queued[0])
	}

	conv.mu.Lock()
	conv.hold = nil
	conv.mu.Unlock()
	close(hold)
	marked := appLiveUser(t, rec, "q-2")
	if !marked.Queued || marked.Text != "" || !reflect.DeepEqual(marked.Reads, []appwire.MessageRead{appCutRead()}) {
		t.Fatalf("the queued message went live as %+v", marked)
	}
	submissions, texts := appSubmissionsOf(conv)
	if want := []string{"first", "then the README", links}; !reflect.DeepEqual(texts, want) {
		t.Fatalf("the engine was sent %q", texts)
	}
	// Every one of these is the person's own, so each states an explicit none
	// (the first send included), and an edit keeps it.
	if len(submissions) != 3 || submissions[0].Text != "first" || submissions[0].Creation == nil || submissions[0].Creation.Team != "" {
		t.Fatalf("the person's first send was handed as %+v", submissions)
	}
	submissions = submissions[1:]
	want := appQueuedSubmission(appMarkedSubmission(t, links, "", appCutRead()))
	want.ClientID, want.Creation = "q-2", &session.CreationContext{}
	if len(submissions) != 2 || submissions[0].Text != "then the README" || !submissions[0].Queued || !reflect.DeepEqual(submissions[1], want) {
		t.Fatalf("the marked door was handed %+v", submissions)
	}
	if _, err := backend.EditQueuedMessage(agent.ID, second, appwire.AgentMessage{Text: "too late", Said: &none, Reads: []appwire.MessageRead{appCutRead()}}); wireCode(err) != appwire.CodeSent {
		t.Fatalf("an edit after the send answered %v", err)
	}
	if left := backend.queued(backend.agent(agent.ID)); len(left) != 0 {
		t.Fatalf("an edit after the send queued %+v", left)
	}
}

// AN EDIT RACING THE TURN'S END IS ONE OF TWO THINGS AND NEVER BOTH: it lands
// before the queue sends, and the replacement is what is sent; or the message
// was already sent, the edit answers sent, and the original is what went. No
// race sends two messages, a mix of the two, or leaves one behind.
func TestAppQueuedEditRacingTheSendIsEitherSentOrRefused(t *testing.T) {
	landed, refused := 0, 0
	for i := range 24 {
		hold := make(chan struct{})
		conv := &fakeConversation{hold: hold}
		backend, rec, agent := workBackend(t, conv)
		if _, err := backend.Send(agent.ID, "first", ""); err != nil {
			t.Fatal(err)
		}
		oldSaid, newSaid := "old", "new"
		queued, err := backend.QueueMessage(agent.ID, appwire.AgentMessage{Text: "old body", Said: &oldSaid, ClientID: "q", Reads: []appwire.MessageRead{appSpecsRead()}})
		if err != nil {
			t.Fatal(err)
		}
		conv.mu.Lock()
		conv.hold = nil
		conv.mu.Unlock()
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			close(hold)
		}()
		_, editErr := backend.EditQueuedMessage(agent.ID, queued[0].ID, appwire.AgentMessage{Text: "new body", Said: &newSaid, Reads: []appwire.MessageRead{appCutRead()}})
		wg.Wait()
		live := appLiveUser(t, rec, "q")
		submissions, texts := appSubmissionsOf(conv)
		// The person's first send states its explicit none too; the race is
		// about the queued message after it.
		if len(submissions) == 0 || submissions[0].Text != "first" {
			t.Fatalf("run %d: the first send was handed as %+v", i, submissions)
		}
		submissions = submissions[1:]
		switch {
		case editErr == nil:
			landed++
			want := appQueuedSubmission(appMarkedSubmission(t, "new body", "new", appCutRead()))
			want.ClientID, want.Creation = "q", &session.CreationContext{}
			if len(submissions) != 1 || !reflect.DeepEqual(submissions[0], want) || live.Text != "new" {
				t.Fatalf("run %d: the edit landed and %+v was sent, %+v went live", i, submissions, live)
			}
		case wireCode(editErr) == appwire.CodeSent:
			refused++
			want := appQueuedSubmission(appMarkedSubmission(t, "old body", "old", appSpecsRead()))
			want.ClientID, want.Creation = "q", &session.CreationContext{}
			if len(submissions) != 1 || !reflect.DeepEqual(submissions[0], want) || live.Text != "old" {
				t.Fatalf("run %d: the edit was refused and %+v was sent, %+v went live", i, submissions, live)
			}
		default:
			t.Fatalf("run %d: the racing edit answered %v", i, editErr)
		}
		if len(texts) != 2 || len(backend.queued(backend.agent(agent.ID))) != 0 {
			t.Fatalf("run %d: the engine was sent %q and the queue holds %+v", i, texts, backend.queued(backend.agent(agent.ID)))
		}
	}
	t.Logf("%d edits landed before the send, %d were refused as sent", landed, refused)
}

// A MARKED CORRECTION GOES BY ITS OWN DOOR AND SHOWS THE PERSON'S WORDS: the
// steer entry, pending and consumed, carries said and reads; one that falls
// through starts a turn whose user entry carries the same; the engine is
// handed the expanded words. A conversation with no door for a marked
// correction refuses it before the turn hears anything.
func TestAppMarkedSteerIsConsumedOrFallsThroughWithItsMark(t *testing.T) {
	hold := make(chan struct{})
	conv := &steeringConversation{fakeConversation: &fakeConversation{script: []session.Event{{Kind: session.EventToolBegin, CallID: "c1", Tool: "bash", Args: `{"command":"go test ./..."}`}}, hold: hold}}
	backend, rec, agent := markBackend(t, conv)
	said := "use the specs"
	body := "use the specs\n\nSpecs:\n- docs/a.md"
	if err := backend.SteerMessage(agent.ID, appwire.AgentMessage{Text: body, Said: &said, Reads: []appwire.MessageRead{appSpecsRead()}}); wireCode(err) != appwire.CodeBusy {
		t.Fatalf("a correction with nothing running answered %v", err)
	}
	if _, err := backend.Send(agent.ID, "run the suite", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool { _, ok := data.(appwire.TurnTool); return ok })
	if err := backend.SteerMessage(agent.ID, appwire.AgentMessage{Text: body, Said: &said, Attachments: []string{"at1"}}); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("a correction with attachments answered %v", err)
	}

	first := make(chan session.Event, 4)
	conv.mu.Lock()
	conv.steer = first
	conv.mu.Unlock()
	if err := backend.SteerMessage(agent.ID, appwire.AgentMessage{Text: body, Said: &said, ClientID: "s-1", Reads: []appwire.MessageRead{appSpecsRead()}}); err != nil {
		t.Fatal(err)
	}
	pending := entryWith(rec, t, func(e appwire.Entry) bool { return e.Kind == appwire.EntrySteer && e.ClientID == "s-1" })
	if pending.State != appwire.SteerPending || pending.Text != said || !reflect.DeepEqual(pending.Reads, []appwire.MessageRead{appSpecsRead()}) {
		t.Fatalf("the pending correction = %+v", pending)
	}
	first <- session.Event{Kind: session.EventSteerConsumed}
	close(first)
	consumed := entryWith(rec, t, func(e appwire.Entry) bool { return e.ID == pending.ID && e.State == appwire.SteerConsumed })
	if consumed.Text != said || !reflect.DeepEqual(consumed.Reads, []appwire.MessageRead{appSpecsRead()}) {
		t.Fatalf("the consumed correction = %+v", consumed)
	}

	late := make(chan session.Event, 8)
	conv.mu.Lock()
	conv.steer = late
	conv.mu.Unlock()
	none := ""
	links := "Links:\n- https://example.com/c"
	if err := backend.SteerMessage(agent.ID, appwire.AgentMessage{Text: links, Said: &none, ClientID: "s-2", Reads: []appwire.MessageRead{appCutRead()}}); err != nil {
		t.Fatal(err)
	}
	late <- session.Event{Kind: session.EventSteerFellThrough}
	late <- session.Event{Kind: session.EventTextDelta, Text: "Read them."}
	late <- session.Event{Kind: session.EventTurnDone}
	close(late)
	close(hold)
	user := entryWith(rec, t, func(e appwire.Entry) bool { return e.Kind == appwire.EntryUser && e.ClientID == "s-2" })
	if user.Text != "" || user.Turn == 0 || !reflect.DeepEqual(user.Reads, []appwire.MessageRead{appCutRead()}) {
		t.Fatalf("the fallen-through correction = %+v", user)
	}
	conv.mu.Lock()
	steers, steered := append([]session.Submission(nil), conv.steers...), append([]string(nil), conv.steered...)
	conv.mu.Unlock()
	want := []session.Submission{appMarkedSubmission(t, body, said, appSpecsRead()), appMarkedSubmission(t, links, "", appCutRead())}
	if !reflect.DeepEqual(steers, want) || !reflect.DeepEqual(steered, []string{body, links}) {
		t.Fatalf("the engine was handed %+v and heard %q", steers, steered)
	}

	doorless := &fakeConversation{hold: make(chan struct{})}
	plainBackend, _, plainAgent := workBackend(t, doorless)
	if _, err := plainBackend.Send(plainAgent.ID, "run", ""); err != nil {
		t.Fatal(err)
	}
	doorless.mu.Lock()
	doorless.steer = make(chan session.Event)
	doorless.mu.Unlock()
	if err := plainBackend.SteerMessage(plainAgent.ID, appwire.AgentMessage{Text: body, Said: &said}); wireCode(err) != appwire.CodeUnknownMethod {
		t.Fatalf("a conversation without the door answered %v", err)
	}
	doorless.mu.Lock()
	heard := len(doorless.steered)
	doorless.mu.Unlock()
	if heard != 0 {
		t.Fatal("a refused correction reached the turn")
	}
	close(doorless.hold)
}

// A HOST THAT SAID IT KEEPS NO MARK IS NEVER ASKED TO: a marked correction, a
// marked queued message, a marked edit and a marked send are refused as
// unknown before the engine, the queue or the turn changes, and plain words
// still go every one of those roads.
func TestAppOlderHostIsRefusedEveryMarkedMessageBeforeAnything(t *testing.T) {
	hold := make(chan struct{})
	inner := &fakeConversation{hold: hold}
	conv := olderHostConversation{&steeringConversation{fakeConversation: inner}}
	backend, rec, agent := markBackend(t, conv)
	if _, err := backend.Send(agent.ID, "first", ""); err != nil {
		t.Fatal(err)
	}
	inner.mu.Lock()
	inner.steer = make(chan session.Event, 1)
	inner.mu.Unlock()
	said := "mine"
	marked := appwire.AgentMessage{Text: "mine\n\n- docs/a.md", Said: &said, ClientID: "m", Reads: []appwire.MessageRead{appSpecsRead()}}
	if err := backend.SteerMessage(agent.ID, marked); wireCode(err) != appwire.CodeUnknownMethod {
		t.Fatalf("a marked correction answered %v", err)
	}
	if _, err := backend.QueueMessage(agent.ID, marked); wireCode(err) != appwire.CodeUnknownMethod {
		t.Fatalf("a marked queued message answered %v", err)
	}
	queued, err := backend.Queue(agent.ID, "plain words", "p")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.EditQueuedMessage(agent.ID, queued[0].ID, marked); wireCode(err) != appwire.CodeUnknownMethod {
		t.Fatalf("a marked edit answered %v", err)
	}
	if after := backend.queued(backend.agent(agent.ID)); len(after) != 1 || after[0].Text != "plain words" || after[0].Reads != nil {
		t.Fatalf("a refused edit changed the queue to %+v", after)
	}
	if err := backend.Steer(agent.ID, "plain correction", ""); err != nil {
		t.Fatal(err)
	}
	inner.mu.Lock()
	steers, steered := len(conv.steers), append([]string(nil), inner.steered...)
	inner.hold = nil
	inner.mu.Unlock()
	if steers != 0 || !reflect.DeepEqual(steered, []string{"plain correction"}) {
		t.Fatalf("the older host was handed %d marked corrections and heard %q", steers, steered)
	}
	close(hold)
	inner.mu.Lock()
	close(inner.steer)
	inner.mu.Unlock()
	appLiveUser(t, rec, "p")
	deadline := time.Now().Add(30 * time.Second)
	for {
		_, err := backend.SendMessage(agent.ID, marked)
		if wireCode(err) == appwire.CodeUnknownMethod {
			break
		}
		if wireCode(err) != appwire.CodeBusy || time.Now().After(deadline) {
			t.Fatalf("a marked send answered %v", err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if submissions, texts := appSubmissionsOf(inner); len(submissions) != 0 || !reflect.DeepEqual(texts, []string{"first", "plain words"}) {
		t.Fatalf("the older host was handed %+v and sent %q", submissions, texts)
	}
}

func appQueuedSubmission(s session.Submission) session.Submission { s.Queued = true; return s }

func TestAppRetryQueuedMessagePreservesReferenceMarks(t *testing.T) {
	conv := &fakeConversation{}
	backend, _, agent := markBackend(t, conv)
	reads, err := appKeptReads([]appwire.MessageRead{appSpecsRead()})
	if err != nil {
		t.Fatal(err)
	}
	a := backend.agent(agent.ID)
	backend.locked(func() {
		a.failed = "expanded body"
		a.failedShown = &appSending{text: "mine", reads: reads, queued: true}
	})
	if _, err := backend.Retry(agent.ID); err != nil {
		t.Fatal(err)
	}
	submissions, _ := appSubmissionsOf(conv)
	want := appMarkedSubmission(t, "expanded body", "mine", appSpecsRead())
	if len(submissions) != 1 || !reflect.DeepEqual(submissions[0], want) {
		t.Fatalf("retry lost its reference mark or retained its queued flag: %+v", submissions)
	}
}

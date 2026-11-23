package main

// A NOTE'S OUTCOME IS THE ENGINE'S TYPED ANSWER, AND A NOTE IS NEVER SENT TWICE.
//
// tasks.note reads [session.AppTaskReceipt]: delivered and held wait for a read
// joined on the note's client id; a refusal goes back with its code, and only
// the refusal that the task is not listed may try the handed-over door; an
// unknown outcome is `uncertain` with the note's entry and is never retried.
// The real engine behind the receipt is proven in internal/session's
// task_note_remote_test.go; here the conversation answers what a test tells it.

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// noteEvents is every note.changed the recorder holds.
func noteEvents(rec *appRecorder) []appwire.NoteChanged {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var out []appwire.NoteChanged
	for _, data := range rec.data {
		if changed, ok := data.(appwire.NoteChanged); ok {
			out = append(out, changed)
		}
	}
	return out
}

// THROUGH THE REGISTERED SERVER: an unknown outcome reaches the app as
// `uncertain` carrying the note's entry under its key, nothing waits, nothing is
// sent again by any door — and when the page later shows the key read, the read
// is the producer's and is said for that entry.
func TestAnUnknownNoteIsUncertainOverTheServerAndNeverSentAgain(t *testing.T) {
	conv := &fakeConversation{
		appTasks: []session.AppTaskView{{ID: "4", Title: "t", State: "running"}},
		receipts: []session.AppTaskReceipt{{Outcome: session.AppTaskUnknown, Reason: "it is not known whether that correction was kept", ClientID: "k-1"}},
	}
	backend, rec, agent := workBackend(t, conv)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	server := appwire.NewServer(outW)
	go func() {
		_ = server.Serve(context.Background(), inR, appwire.Hello{Version: "test"}, backend)
		outW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	scanner := bufio.NewScanner(outR)
	scanner.Buffer(make([]byte, 4<<20), 4<<20)
	if !scanner.Scan() {
		t.Fatal("no hello")
	}
	if _, err := io.WriteString(inW, `{"id":1,"method":"tasks.note","params":{"id":"`+agent.ID+`:4","text":"same words","client_id":"k-1"}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	var answer struct {
		ID    int `json:"id"`
		Error *struct {
			Code   string        `json:"code"`
			Detail appwire.Entry `json:"detail"`
		} `json:"error"`
	}
	for scanner.Scan() {
		if err := json.Unmarshal(scanner.Bytes(), &answer); err == nil && answer.ID == 1 {
			break
		}
	}
	if answer.Error == nil || answer.Error.Code != appwire.CodeUncertain {
		t.Fatalf("answer = %s", scanner.Text())
	}
	if d := answer.Error.Detail; d.ClientID != "k-1" || d.State != appwire.NoteSending || d.Text != "same words" || d.ID == "" {
		t.Fatalf("the uncertain note's entry = %+v", d)
	}
	conv.mu.Lock()
	asked, handed := len(conv.intents), len(conv.handed)
	conv.mu.Unlock()
	if asked != 1 || handed != 0 {
		t.Fatalf("an unknown note was sent again: %d engine asks, %d handed", asked, handed)
	}
	for _, changed := range noteEvents(rec) {
		if changed.State == appwire.NoteWaiting {
			t.Fatalf("an unknown note was said to be waiting: %+v", changed)
		}
	}
	step := 4
	conv.mu.Lock()
	conv.appTasks[0].Notes = []session.AppTaskNote{{Text: "same words", ClientID: "k-1", ReadAtStep: &step}}
	conv.mu.Unlock()
	i := rec.waitFor(t, 6*time.Second, func(_ string, data any) bool {
		changed, ok := data.(appwire.NoteChanged)
		return ok && changed.State == appwire.NoteRead
	})
	rec.mu.Lock()
	read := rec.data[i].(appwire.NoteChanged)
	rec.mu.Unlock()
	if read.ID != answer.Error.Detail.ID || read.ClientID != "k-1" || *read.ReadAtStep != 4 {
		t.Fatalf("read = %+v", read)
	}
}

// ONLY A TYPED REFUSAL THAT THE TASK IS NOT LISTED TAKES THE HANDED-OVER DOOR.
// Any other refusal goes back with nothing else tried; a held note waits; and
// the handed door's own unknown is uncertain, carrying the entry.
func TestOnlyANotListedRefusalTakesTheHandedDoor(t *testing.T) {
	conv := &fakeConversation{
		appTasks: []session.AppTaskView{{ID: "4", Title: "t", State: "running"}},
		receipts: []session.AppTaskReceipt{
			{Outcome: session.AppTaskRefused, Reason: "no such task", ClientID: "k-1"},
			{Outcome: session.AppTaskRefused, Reason: "task 9 is done, not running", ClientID: "k-2"},
			{Outcome: session.AppTaskHeld, ClientID: "k-3"},
			{Outcome: session.AppTaskRefused, Reason: "no such task", ClientID: "k-4"},
		},
	}
	backend, _, agent := workBackend(t, conv)
	entry, err := backend.NoteTask(agent.ID+":9", "handed words", "k-1")
	if err != nil || entry.State != appwire.NoteWaiting || entry.ClientID != "k-1" {
		t.Fatalf("handed note = %+v %v", entry, err)
	}
	if _, err := backend.NoteTask(agent.ID+":9", "too late", "k-2"); err == nil {
		t.Fatal("a refusal came back as a note")
	}
	conv.mu.Lock()
	handed := append([]string(nil), conv.handed...)
	conv.mu.Unlock()
	if len(handed) != 1 || handed[0] != "handed words" {
		t.Fatalf("handed door asked %q, want only the not-listed note", handed)
	}
	held, err := backend.NoteTask(agent.ID+":4", "hold this", "k-3")
	if err != nil || held.State != appwire.NoteWaiting || held.ClientID != "k-3" {
		t.Fatalf("held note = %+v %v", held, err)
	}
	conv.mu.Lock()
	conv.handErr = session.ErrSendUnanswered
	conv.mu.Unlock()
	_, err = backend.NoteTask(agent.ID+":9", "lost words", "k-4")
	wire, ok := err.(*appwire.Error)
	if !ok || wire.Code != appwire.CodeUncertain {
		t.Fatalf("a handed note nobody answered = %v", err)
	}
	if detail, ok := wire.Detail.(appwire.Entry); !ok || detail.ClientID != "k-4" {
		t.Fatalf("the uncertain handed note's detail = %+v", wire.Detail)
	}
	backend.mu.Lock()
	pending := 0
	for _, held := range backend.agents {
		if held.live != nil {
			pending += len(held.live.notes[agent.ID+":9"])
		}
	}
	backend.mu.Unlock()
	if pending != 0 {
		t.Fatalf("a note on a task with no page is watched (%d)", pending)
	}
}

// A NOTE WITH NO KEY IS NEVER READ BY ITS POSITION. The page grows a read note
// exactly where the unkeyed one would have been counted, and only the keyed
// note sent beside it is said read. Equal words under other keys stay apart,
// and the reads arrive in the page's order, not the send's.
func TestAnUnkeyedNoteIsNeverReadByPositionAndKeysReadOutOfOrder(t *testing.T) {
	conv := &fakeConversation{appTasks: []session.AppTaskView{{ID: "4", Title: "t", State: "running"}}}
	backend, rec, agent := workBackend(t, conv)
	unkeyed, err := backend.NoteTask(agent.ID+":4", "same words", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := backend.NoteTask(agent.ID+":4", "same words", "k-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := backend.NoteTask(agent.ID+":4", "same words", "k-b")
	if err != nil {
		t.Fatal(err)
	}
	three, seven := 3, 7
	conv.mu.Lock()
	conv.appTasks[0].Notes = []session.AppTaskNote{
		{Text: "same words", ReadAtStep: &three},
		{Text: "same words", ClientID: "k-b", ReadAtStep: &three},
		{Text: "same words", ClientID: "k-a"},
	}
	conv.mu.Unlock()
	rec.waitFor(t, 6*time.Second, func(_ string, data any) bool {
		changed, ok := data.(appwire.NoteChanged)
		return ok && changed.State == appwire.NoteRead && changed.ID == second.ID
	})
	conv.mu.Lock()
	conv.appTasks[0].Notes[2].ReadAtStep = &seven
	conv.mu.Unlock()
	rec.waitFor(t, 6*time.Second, func(_ string, data any) bool {
		changed, ok := data.(appwire.NoteChanged)
		return ok && changed.State == appwire.NoteRead && changed.ID == first.ID
	})
	reads := map[string]appwire.NoteChanged{}
	for _, changed := range noteEvents(rec) {
		if changed.State == appwire.NoteRead {
			if _, twice := reads[changed.ID]; twice {
				t.Fatalf("a note was read twice: %+v", changed)
			}
			reads[changed.ID] = changed
		}
	}
	if _, read := reads[unkeyed.ID]; read {
		t.Fatalf("an unkeyed note was read by its position: %+v", reads[unkeyed.ID])
	}
	if a, b := reads[first.ID], reads[second.ID]; a.ClientID != "k-a" || *a.ReadAtStep != 7 || b.ClientID != "k-b" || *b.ReadAtStep != 3 {
		t.Fatalf("reads = %+v / %+v", a, b)
	}
}

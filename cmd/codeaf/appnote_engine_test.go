package main

// A NOTE CROSSES EVERY REAL HALF AND IS READ BY ITS KEY.
//
// The registered app server answers `tasks.note`; the backend sends it through
// the remote socket (remote.Loopback) to a real session engine built the public
// way (session.New) and running a task admitted the public way (StartTask)
// against a local model that holds its first request; the task's worker reads
// the notes at its next request, which is the producer stamping the step; the
// watcher says `note.changed read` for the key and only for the key; and an
// engine opened again on the same session file holds the same keys on the same
// directions. The only fake in the chain is the model's HTTP answer.

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestATaskNoteCrossesTheAppServerAndTheSocketToTheRealNodeAndIsReadByItsKey(t *testing.T) {
	model := newHeldModel(t)
	sources := modelsource.NewSet(modelsource.Connected{
		Source: modelsource.DefaultSource(model.server.URL), Key: "test-key", Address: model.server.URL,
	})
	sessionFile := filepath.Join(t.TempDir(), "session.jsonl")
	config := session.Config{Workspace: t.TempDir(), Model: "openai/gpt-4.1-mini", Sources: sources, SessionFile: sessionFile}
	engine, err := session.New(config)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			close(model.release)
			_ = engine.Close()
		}
	})
	id, _, _, err := engine.StartTask(context.Background(), "do the thing", true)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-model.arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("the task's worker never reached the model")
	}
	local := strconv.FormatUint(id, 10)

	loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
		return &remote.Engine{Agent: engine}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	near := loop.Client.Agent()

	backend, rec, agent := workBackend(t, &fakeConversation{})
	backend.locked(func() { backend.agents[agent.ID].live.conn.Conv = near })
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
	next := 0
	note := func(text, key string) appwire.Entry {
		t.Helper()
		next++
		params, _ := json.Marshal(map[string]string{"id": agent.ID + ":" + local, "text": text, "client_id": key})
		if _, err := io.WriteString(inW, `{"id":`+strconv.Itoa(next)+`,"method":"tasks.note","params":`+string(params)+"}\n"); err != nil {
			t.Fatal(err)
		}
		var answer struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		for scanner.Scan() {
			if err := json.Unmarshal(scanner.Bytes(), &answer); err == nil && answer.ID == next {
				break
			}
		}
		if answer.Error != nil {
			t.Fatalf("tasks.note %q refused: %+v", key, *answer.Error)
		}
		var result appwire.TaskNoteResult
		if err := json.Unmarshal(answer.Result, &result); err != nil || result.Entry == nil {
			t.Fatalf("tasks.note result %s: %v", answer.Result, err)
		}
		return *result.Entry
	}

	// Equal words under two keys, and once with no key at all.
	first := note("same words", "k-1")
	second := note("same words", "k-2")
	unkeyed := note("same words", "")
	for _, entry := range []appwire.Entry{first, second, unkeyed} {
		if entry.State != appwire.NoteWaiting {
			t.Fatalf("a taken note is %q, want waiting: %+v", entry.State, entry)
		}
	}
	if first.ClientID != "k-1" || second.ClientID != "k-2" || unkeyed.ClientID != "" {
		t.Fatalf("keys on the entries: %q %q %q", first.ClientID, second.ClientID, unkeyed.ClientID)
	}
	for _, changed := range noteEvents(rec) {
		if changed.State == appwire.NoteRead {
			t.Fatalf("a note was read before the worker took another step: %+v", changed)
		}
	}

	// The worker's held request answers; its next request is the producer's read.
	close(model.release)
	closed = true
	read := map[string]appwire.NoteChanged{}
	deadline := time.Now().Add(30 * time.Second)
	for len(read) < 2 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		for _, changed := range noteEvents(rec) {
			if changed.State == appwire.NoteRead {
				read[changed.ClientID] = changed
			}
		}
	}
	for key, entry := range map[string]appwire.Entry{"k-1": first, "k-2": second} {
		got, ok := read[key]
		if !ok || got.ReadAtStep == nil || got.ID != entry.ID {
			t.Fatalf("no read event for %q from the real producer: %+v", key, read)
		}
	}
	// With no key nothing may join the note: not its words, not its place.
	time.Sleep(2*appNoteBeat + 200*time.Millisecond)
	for _, changed := range noteEvents(rec) {
		if changed.State == appwire.NoteRead && (changed.ClientID == "" || changed.ID == unkeyed.ID) {
			t.Fatalf("an unkeyed note was said to be read: %+v", changed)
		}
	}

	// The restart: an engine opened again on the same file holds the same keys.
	view, found, err := near.AppTask(local)
	if err != nil || !found {
		t.Fatalf("page over the socket: %v %v", found, err)
	}
	keys := func(notes []session.AppTaskNote) string {
		out := ""
		for _, n := range notes {
			if n.From == session.AppNoteFromPerson {
				out += n.ClientID + "|"
			}
		}
		return out
	}
	if got := keys(view.Notes); got != "k-1|k-2||" {
		t.Fatalf("page keys over the socket = %q", got)
	}
	_ = loop.Close()
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := session.New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	again, found, err := reopened.AppTask(local)
	if err != nil || !found {
		t.Fatalf("page after the restart: %v %v", found, err)
	}
	if got := keys(again.Notes); got != keys(view.Notes) {
		t.Fatalf("keys after the restart = %q, before %q", got, keys(view.Notes))
	}
}

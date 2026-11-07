package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE MID-RUN NOTE, THROUGH THE REAL SESSION ENGINE: while the model is
// answering, a note lands in --steer's file; it is put into the running turn,
// the model's next call reads it and changes the answer, and --events is the
// conversation's own record, byte for byte, with the note in it.
func TestChatOnceTakesAMidRunNoteAndStreamsItsRecord(t *testing.T) {
	appDayProfile(t)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "ledger.txt"), []byte("1182: +40.00, +40.00\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	steerPath, eventsPath := filepath.Join(dir, "steer"), filepath.Join(dir, "events.jsonl")
	const note = "check negative amounts too"
	var mu sync.Mutex
	calls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "no catalog for a test", http.StatusServiceUnavailable)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var env recordedRequest
		_ = json.Unmarshal(raw, &env)
		if len(env.Tools) == 0 {
			writeText(w, env.Stream, "ok")
			return
		}
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			// The person's note arrives while this call is still answering.
			_ = os.WriteFile(steerPath, []byte(`{"text":"`+note+`"}`+"\n"), 0o600)
			time.Sleep(900 * time.Millisecond)
			writeToolCall(w, env.Stream, "c1", "read", `{"path":"ledger.txt"}`)
			return
		}
		answer := "order 1182 was charged twice"
		for _, m := range env.Messages {
			if m.Role == "user" && strings.Contains(string(m.Content), note) {
				answer = "order 1182 was charged twice, and no amount is negative"
			}
		}
		writeText(w, env.Stream, answer)
	}))
	t.Cleanup(model.Close)

	file, err := appMint(workspace)
	if err != nil {
		t.Fatal(err)
	}
	sources := modelsource.NewSet(modelsource.Connected{Source: modelsource.DefaultSource(model.URL), Key: "test-key", Address: model.URL})
	agent, err := session.New(session.Config{Workspace: workspace, Place: session.Place{Dir: session.DirOf(file)}, SessionFile: file, Model: "openai/gpt-4.1-mini", Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	tail := copyTail(file, eventsPath, stop)
	reply, stderr := &lockedWriter{}, &lockedWriter{}
	var steers sync.WaitGroup
	steerTail(agent, steerPath, reply, stderr, &steers, stop)
	events, err := agent.Submit(context.Background(), "why was order 1182 charged twice?")
	if err != nil {
		t.Fatal(err)
	}
	if err := drainOnceEvents(events, reply, stderr); err != nil {
		t.Fatal(err)
	}
	steers.Wait()
	close(stop)
	<-tail
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(reply.String(), "no amount is negative") {
		t.Fatalf("the note did not change the answer:\nreply: %s\nstderr: %s", reply.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "steered: "+note) {
		t.Fatalf("the note was not put into the turn: %s", stderr.String())
	}
	record, _ := os.ReadFile(file)
	streamed, _ := os.ReadFile(eventsPath)
	if !bytes.Equal(record, streamed) {
		t.Fatalf("--events is not the record byte for byte:\nrecord:\n%s\nevents:\n%s", record, streamed)
	}
	if !bytes.Contains(streamed, []byte(note)) {
		t.Fatalf("the record does not carry the note: %s", streamed)
	}
}

// --json's one object is `codeaf exec --json`'s shape.
func TestChatOnceJSONEnvelope(t *testing.T) {
	var ok, bad bytes.Buffer
	writeOnceEnvelope(&ok, " the answer \n", "deepseek/deepseek-v4.1-flash", nil)
	writeOnceEnvelope(&bad, "", "m", io.ErrUnexpectedEOF)
	if got := strings.TrimSpace(ok.String()); got != `{"ok":true,"stop":"done","answer":"the answer","model":"deepseek/deepseek-v4.1-flash"}` {
		t.Fatalf("ok envelope: %s", got)
	}
	if got := strings.TrimSpace(bad.String()); got != `{"ok":false,"stop":"error","model":"m","error":"unexpected EOF"}` {
		t.Fatalf("error envelope: %s", got)
	}
}

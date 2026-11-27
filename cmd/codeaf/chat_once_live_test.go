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

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A NOTE THAT LANDS AS THE RUN ENDS IS READ, NEVER LOST: the steer file is
// read once more after the last turn, and a note waiting there gets the one
// more turn it asks for before chat --once exits. Without that last read, a
// note written between the reader's last look and the exit was taken by
// nobody, and the step ended as though it had never been sent.
func TestChatOnceReadsANoteThatLandsAsTheRunEnds(t *testing.T) {
	onceTestProfile(t)
	workspace := t.TempDir()
	dir := t.TempDir()
	steerPath := filepath.Join(dir, "steer")
	const note = "also say which file you read"
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "no catalog for a test", http.StatusServiceUnavailable)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var env recordedRequest
		_ = json.Unmarshal(raw, &env)
		answer := "the ledger balances"
		for _, m := range env.Messages {
			if m.Role == "user" && strings.Contains(string(m.Content), note) {
				answer = "the note was read: ledger.txt"
			}
		}
		writeText(w, env.Stream, answer)
	}))
	t.Cleanup(model.Close)

	file, err := onceTestMint(workspace)
	if err != nil {
		t.Fatal(err)
	}
	sources := modelsource.NewSet(modelsource.Connected{Source: modelsource.DefaultSource(model.URL), Key: "test-key", Address: model.URL})
	agent, err := session.New(session.Config{Workspace: workspace, Place: session.Place{Dir: filepath.Dir(file)}, SessionFile: file, Model: "openai/gpt-4.1-mini", Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	reply, stderr := &lockedWriter{}, &lockedWriter{}
	var steers sync.WaitGroup
	finishSteer, finishEvents := startOnceLive(agent, file, onceLive{steer: steerPath}, reply, stderr, &steers)
	defer finishEvents()
	events, err := agent.Submit(context.Background(), "does the ledger balance?")
	if err != nil {
		t.Fatal(err)
	}
	if err := drainOnceEvents(events, reply, stderr); err != nil {
		t.Fatal(err)
	}
	// The note lands the moment the turn has ended, before the reader's next
	// look, and the run then exits.
	if err := os.WriteFile(steerPath, []byte(`{"text":"`+note+`"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	finishSteer()
	if !strings.Contains(reply.String(), "the note was read: ledger.txt") || !strings.Contains(stderr.String(), "steered: "+note) {
		t.Fatalf("the note that landed as the run ended was lost:\nreply: %s\nstderr: %s", reply.String(), stderr.String())
	}
}

// THE MID-RUN NOTE, THROUGH THE REAL SESSION ENGINE: while the model is
// answering, a note lands in --steer's file; it is put into the running turn,
// the model's next call reads it and changes the answer, and --events is the
// conversation's own record, byte for byte, with the note in it.
func TestChatOnceTakesAMidRunNoteAndStreamsItsRecord(t *testing.T) {
	onceTestProfile(t)
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

	file, err := onceTestMint(workspace)
	if err != nil {
		t.Fatal(err)
	}
	sources := modelsource.NewSet(modelsource.Connected{Source: modelsource.DefaultSource(model.URL), Key: "test-key", Address: model.URL})
	agent, err := session.New(session.Config{Workspace: workspace, Place: session.Place{Dir: filepath.Dir(file)}, SessionFile: file, Model: "openai/gpt-4.1-mini", Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	reply, stderr := &lockedWriter{}, &lockedWriter{}
	var steers sync.WaitGroup
	finishSteer, finishEvents := startOnceLive(agent, file, onceLive{events: eventsPath, steer: steerPath}, reply, stderr, &steers)
	events, err := agent.Submit(context.Background(), "why was order 1182 charged twice?")
	if err != nil {
		t.Fatal(err)
	}
	if err := drainOnceEvents(events, reply, stderr); err != nil {
		t.Fatal(err)
	}
	finishSteer()
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	// A line the close writes after the answer — the live run's helper usage
	// row did exactly this — still reaches the watcher.
	late := `{"type":"usage","usage":{"model":"openai/gpt-4.1-mini","aux":true,"role":"reflex"}}` + "\n"
	if f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o600); err != nil {
		t.Fatal(err)
	} else {
		_, _ = f.WriteString(late)
		_ = f.Close()
	}
	finishEvents()

	if !strings.Contains(reply.String(), "no amount is negative") {
		t.Fatalf("the note did not change the answer:\nreply: %s\nstderr: %s", reply.String(), stderr.String())
	}
	// THE STEERED ANSWER IS SAID ONCE. The steer's own stream is a view of the
	// turn the run is already printing, and printing both doubled the reply.
	if got := strings.Count(reply.String(), "order 1182 was charged twice"); got != 1 {
		t.Fatalf("the answer was printed %d times, want once:\nreply: %s", got, reply.String())
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
	writeOnceEnvelope(&ok, " the answer \n", "deepseek/deepseek-v4.1-flash", envelopeUsage{}, nil)
	writeOnceEnvelope(&bad, "", "m", envelopeUsage{}, io.ErrUnexpectedEOF)
	if got := strings.TrimSpace(ok.String()); got != `{"ok":true,"stop":"done","answer":"the answer","model":"deepseek/deepseek-v4.1-flash","spend_usd":0,"tokens":{"in":0,"out":0}}` {
		t.Fatalf("ok envelope: %s", got)
	}
	if got := strings.TrimSpace(bad.String()); got != `{"ok":false,"stop":"error","model":"m","error":"unexpected EOF","spend_usd":0,"tokens":{"in":0,"out":0}}` {
		t.Fatalf("error envelope: %s", got)
	}
}

// onceTestProfile gives a test its own profile and codeaf home, with no daily
// limit carried in from the machine running it.
func onceTestProfile(t *testing.T) string {
	t.Helper()
	profile := t.TempDir()
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	return profile
}

// onceTestMint is a fresh conversation's transcript path in workspace.
func onceTestMint(workspace string) (string, error) {
	place, err := v3NextSession(session.Place{}, workspace)
	if err != nil {
		return "", err
	}
	return place.Transcript(), nil
}

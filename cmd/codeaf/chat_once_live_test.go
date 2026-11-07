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

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A QUESTION ONLY A PERSON CAN ANSWER SAYS SO ON --events: the node's line
// carries the step's mark, and the factory that reads it back keeps it, so a
// question asked on another machine is as much the person's as one asked here.
func TestChatOnceMarksAPersonOnlyQuestionOnEvents(t *testing.T) {
	for _, person := range []bool{true, false} {
		events := filepath.Join(t.TempDir(), "events.jsonl")
		asker := newOnceAsker(events)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _ = asker.Ask(ctx, factory.Asked{Question: "Ship it today?", Options: []string{"yes", "no"}, Person: person})
		raw, err := os.ReadFile(events)
		if err != nil {
			t.Fatal(err)
		}
		first, _, _ := bytes.Cut(raw, []byte("\n"))
		q, ok := afAskLineOf(first)
		if !ok || q.Question != "Ship it today?" || q.Person != person {
			t.Fatalf("person=%t: the line %s read back as %+v", person, first, q)
		}
	}
}

// A NOTE THAT LANDS AS THE RUN ENDS IS READ, NEVER LOST: the steer file is
// read once more after the last turn, and a note waiting there gets the one
// more turn it asks for before chat --once exits. Without that last read, a
// note written between the reader's last look and the exit was taken by
// nobody, and the step ended as though it had never been sent.
func TestChatOnceReadsANoteThatLandsAsTheRunEnds(t *testing.T) {
	appDayProfile(t)
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

	file, err := appMint(workspace)
	if err != nil {
		t.Fatal(err)
	}
	sources := modelsource.NewSet(modelsource.Connected{Source: modelsource.DefaultSource(model.URL), Key: "test-key", Address: model.URL})
	agent, err := session.New(session.Config{Workspace: workspace, Place: session.Place{Dir: session.DirOf(file)}, SessionFile: file, Model: "openai/gpt-4.1-mini", Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	reply, stderr := &lockedWriter{}, &lockedWriter{}
	var steers sync.WaitGroup
	finishSteer, finishEvents := startOnceLive(agent, file, onceLive{steer: steerPath}, nil, reply, stderr, &steers)
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
	reply, stderr := &lockedWriter{}, &lockedWriter{}
	var steers sync.WaitGroup
	finishSteer, finishEvents := startOnceLive(agent, file, onceLive{events: eventsPath, steer: steerPath}, nil, reply, stderr, &steers)
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

// A QUESTION ON A LIVE RUN REACHES THE PERSON, THROUGH THE REAL SESSION
// ENGINE: the model asks; the question is one line of its own on --events;
// the run waits; the answer arrives on --steer as an answer, not a steer; the
// model reads it; and the question is taken down. Under --yolo with nobody
// to ask, the same call would have taken its default.
func TestChatOnceAsksOverEventsAndTakesTheAnswerFromSteer(t *testing.T) {
	appDayProfile(t)
	workspace := t.TempDir()
	dir := t.TempDir()
	steerPath, eventsPath := filepath.Join(dir, "steer"), filepath.Join(dir, "events.jsonl")
	const ask = `{"head":"Which storage shape should this use?","kind":"choice","reason":"two shapes fit","stakes":"reversible","options":[{"key":"1","label":"sqlite"},{"key":"2","label":"jsonl"}],"pick":{"key":"1","reason":"the readers are sqlite"}}`
	var mu sync.Mutex
	calls := 0
	var toolResult string
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
			writeToolCall(w, env.Stream, "c0", "load_capability", `{"group":"questions"}`)
			return
		}
		if n == 2 {
			writeToolCall(w, env.Stream, "c1", "ask", ask)
			return
		}
		for _, m := range env.Messages {
			if m.Role == "tool" && strings.Contains(string(m.Content), "answered") {
				mu.Lock()
				toolResult = string(m.Content)
				mu.Unlock()
			}
		}
		writeText(w, env.Stream, "using jsonl")
	}))
	t.Cleanup(model.Close)

	file, err := appMint(workspace)
	if err != nil {
		t.Fatal(err)
	}
	asker := newOnceAsker(eventsPath)
	sources := modelsource.NewSet(modelsource.Connected{Source: modelsource.DefaultSource(model.URL), Key: "test-key", Address: model.URL})
	agent, err := session.New(session.Config{Workspace: workspace, Place: session.Place{Dir: session.DirOf(file)}, SessionFile: file, Model: "openai/gpt-4.1-mini", Sources: sources,
		Unattended: true, Asker: asker})
	if err != nil {
		t.Fatal(err)
	}
	reply, stderr := &lockedWriter{}, &lockedWriter{}
	var steers sync.WaitGroup
	finishSteer, finishEvents := startOnceLive(agent, file, onceLive{events: eventsPath, steer: steerPath}, asker, reply, stderr, &steers)
	events, err := agent.Submit(context.Background(), "store the ledger")
	if err != nil {
		t.Fatal(err)
	}
	drained := make(chan error, 1)
	go func() { drained <- drainOnceEvents(events, reply, stderr) }()

	// The question is on --events, one line of its own.
	var asked onceAskLine
	deadline := time.Now().Add(10 * time.Second)
	for asked.ID == "" {
		if time.Now().After(deadline) {
			t.Fatalf("no question on --events; stderr: %s", stderr.String())
		}
		raw, _ := os.ReadFile(eventsPath)
		for _, l := range bytes.Split(raw, []byte("\n")) {
			var line onceAskLine
			if json.Unmarshal(l, &line) == nil && line.Type == "ask" {
				asked = line
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if asked.Question != "Which storage shape should this use? (two shapes fit)" || strings.Join(asked.Options, ",") != "sqlite,jsonl" || !strings.HasPrefix(asked.Pick, "sqlite") {
		t.Fatalf("asked = %+v", asked)
	}
	// The answer, as the SDK writes a steer.
	note, _ := json.Marshal(map[string]string{"text": `{"answer":"` + asked.ID + `","text":"jsonl, it is append-only","by":"person"}`})
	if err := os.WriteFile(steerPath, append(note, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-drained:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the run never went on after the answer")
	}
	finishSteer()
	_ = agent.Close()
	finishEvents()

	mu.Lock()
	got := toolResult
	mu.Unlock()
	if !strings.Contains(got, "the person answered: jsonl, it is append-only") {
		t.Fatalf("the model read %q", got)
	}
	if strings.Contains(stderr.String(), "steered: ") {
		t.Fatalf("the answer was steered into the turn: %s", stderr.String())
	}
	raw, _ := os.ReadFile(eventsPath)
	if !bytes.Contains(raw, []byte(`"type":"ask_done","id":"`+asked.ID+`"`)) {
		t.Fatalf("the question was never taken down:\n%s", raw)
	}
}

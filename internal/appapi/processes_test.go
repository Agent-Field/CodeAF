package appapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

type fakeProcessEngine struct {
	*fakeAgentEngine
	processMu sync.Mutex
	rows      []session.JobNotice
	events    chan session.Event
	canceled  []string
	reruns    int
}

func (f *fakeProcessEngine) JobNotices() ([]session.JobNotice, error) {
	f.processMu.Lock()
	defer f.processMu.Unlock()
	return append([]session.JobNotice{}, f.rows...), nil
}
func (f *fakeProcessEngine) Cancel(id string) (string, error) {
	f.processMu.Lock()
	defer f.processMu.Unlock()
	f.canceled = append(f.canceled, id)
	for i := range f.rows {
		if id == "job:"+strconv.Itoa(f.rows[i].ID) {
			f.rows[i].State = session.JobStopped
			if f.events != nil {
				row := f.rows[i]
				f.events <- session.Event{Kind: session.EventJobUpdate, Job: &row}
			}
		}
	}
	return "Stopped", nil
}

func (f *fakeProcessEngine) RerunProcess(id int) (session.JobNotice, error) {
	f.processMu.Lock()
	defer f.processMu.Unlock()
	f.reruns++
	for _, n := range f.rows {
		if n.ID == id {
			n.ID = len(f.rows) + 1
			n.State = session.JobRunning
			f.rows = append(f.rows, n)
			if f.events != nil {
				f.events <- session.Event{Kind: session.EventJobUpdate, Job: &n}
			}
			return n, nil
		}
	}
	return session.JobNotice{}, os.ErrNotExist
}
func processFixture(t *testing.T) (*agentFixture, Agent, *fakeProcessEngine, string) {
	t.Helper()
	f := newAgentFixture(t)
	a := readAgent(t, f.request(t, "POST", "/app/v1/agents", `{"start":"empty"}`), 201)
	live := f.registry.live[a.ID]
	path := filepath.Join(filepath.Dir(live.conn.SessionFile), "logs", "jobs", "1.log")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("ready localhost:4321\nlast line\n"), 0600); err != nil {
		t.Fatal(err)
	}
	engine := &fakeProcessEngine{fakeAgentEngine: f.engines[0], rows: []session.JobNotice{{ID: 1, Name: "docs server", Command: "npm run dev", Kind: session.JobKindCommand, State: session.JobRunning, LogPath: path, Started: f.registry.now()}}}
	live.conn.Engine = engine
	f.registry.watchProcesses(a.ID, &live.conn)
	return f, a, engine, path
}
func TestProcessesRoutesAndOwnership(t *testing.T) {
	f, a, e, _ := processFixture(t)
	pid := processID(a.ID, 1)
	list := f.request(t, "GET", "/app/v1/processes?state=running&q=docs", "")
	var rows ProcessesResponse
	if list.Code != 200 || json.Unmarshal(list.Body.Bytes(), &rows) != nil || len(rows) != 1 || rows[0].ID != pid || rows[0].URL != "localhost:4321" || rows[0].ExitCode != nil {
		t.Fatalf("list: %d %s", list.Code, list.Body)
	}
	output := f.request(t, "GET", "/app/v1/processes/"+pid+"/output?tail=10", "")
	if output.Code != 200 || !strings.Contains(output.Body.String(), `"text":"last line\n"`) || !strings.Contains(output.Body.String(), `"truncated":true`) {
		t.Fatalf("output: %d %s", output.Code, output.Body)
	}
	for _, path := range []string{"/app/v1/processes?state=no", "/app/v1/processes?q=unknown:x", "/app/v1/processes/" + pid + "/output?tail=-1", "/app/v1/processes/" + pid + "/output?follow=yes"} {
		if w := f.request(t, "GET", path, ""); w.Code != 422 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
	}
	if w := f.request(t, "POST", "/app/v1/processes/"+pid+"/stop", `{"expect":{"state":"done"}}`); w.Code != 409 {
		t.Fatal(w.Body)
	}
	if len(e.canceled) != 0 {
		t.Fatal("stale request stopped work")
	}
	for _, body := range []string{"", `null`, `{} {}`, `{"command":"touch other"}`, `{"expect":{"state":4}}`} {
		if w := f.request(t, "POST", "/app/v1/processes/"+pid+"/stop", body); w.Code != 422 {
			t.Fatalf("%q: %d %s", body, w.Code, w.Body)
		}
	}
	if w := f.request(t, "POST", "/app/v1/processes/"+pid+"/rerun", `{}`); w.Code != 409 {
		t.Fatal(w.Body)
	}
	if w := f.request(t, "POST", "/app/v1/processes/"+pid+"/stop", `{"expect":{"state":"running","version":1}}`); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if len(e.canceled) != 1 || e.canceled[0] != "job:1" {
		t.Fatalf("wrong stop address: %v", e.canceled)
	}
	headers := bearer()
	headers["Idempotency-Key"] = []string{"run-again"}
	for range 2 {
		w := request(t, f.server, "POST", "/app/v1/processes/"+pid+"/rerun", `{}`, headers, "127.0.0.1:4000")
		if w.Code != 200 {
			t.Fatal(w.Body)
		}
	}
	if e.reruns != 1 {
		t.Fatal("idempotent rerun executed twice")
	}
	if w := f.request(t, "POST", "/app/v1/agents/"+a.ID+"/processes/stop-all", `{}`); w.Code != 200 {
		t.Fatal(w.Body)
	}
	other := readAgent(t, f.request(t, "POST", "/app/v1/agents", `{"start":"empty"}`), 201)
	if w := f.request(t, "POST", "/app/v1/processes/"+processID(other.ID, 1)+"/stop", `{}`); w.Code != 404 {
		t.Fatal(w.Body)
	}
	if w := f.request(t, "GET", "/app/v1/processes/"+pid+"/stop", ""); w.Code != 405 {
		t.Fatal(w.Body)
	}
	if w := request(t, f.server, "GET", "/app/v1/processes", "", nil, "127.0.0.1:4000"); w.Code != 401 {
		t.Fatal("missing auth")
	}
}
func TestProcessesFollowFlushesLiveAndFinalOutput(t *testing.T) {
	f, a, e, path := processFixture(t)
	server := httptest.NewServer(f.server)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/app/v1/processes/"+processID(a.ID, 1)+"/output?follow=1", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("follow: %d", resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)
	frame := ""
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		frame += line
		if line == "\n" {
			break
		}
	}
	if !strings.Contains(frame, "process.output") || !strings.Contains(frame, "ready localhost:4321") {
		t.Fatal(frame)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteString("final output\n")
	file.Close()
	e.processMu.Lock()
	e.rows[0].State = session.JobDone
	e.processMu.Unlock()
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rest), "final output") || strings.Contains(string(rest), "ready localhost") {
		t.Fatalf("final frame: %s", rest)
	}
}
func TestProcessesCloseQuitSleepAndOneEndNotice(t *testing.T) {
	for _, how := range []string{"close", "quit", "sleep"} {
		t.Run(how, func(t *testing.T) {
			f, a, e, _ := processFixture(t)
			switch how {
			case "close":
				if w := f.request(t, "POST", "/app/v1/agents/"+a.ID+"/close", `{}`); w.Code != 409 {
					t.Fatal("closing running processes did not ask")
				}
				if w := f.request(t, "POST", "/app/v1/agents/"+a.ID+"/close", `{"stop_running":true}`); w.Code != 200 {
					t.Fatal(w.Body)
				}
			case "quit":
				if w := f.request(t, "POST", "/app/v1/shutdown", `{}`); w.Code != 200 {
					t.Fatal(w.Body)
				}
			case "sleep":
				if err := f.registry.StopAllProcesses(); err != nil {
					t.Fatal(err)
				}
			}
			rows, _ := e.JobNotices()
			batch, _, _, _, _ := f.server.events.read(0)
			notices := 0
			for _, event := range batch {
				if event.kind == EventTurnEntry {
					notices++
				}
			}
			if notices != 1 {
				t.Fatalf("%s published %d end notices", how, notices)
			}
			if rows[0].State != session.JobStopped {
				t.Fatalf("%s left a running process", how)
			}
		})
	}
	f, a, e, _ := processFixture(t)
	e.processMu.Lock()
	n := e.rows[0]
	n.State = session.JobFailed
	n.ExitCode = 3
	e.rows[0] = n

	e.processMu.Unlock()
	observer := f.registry.live[a.ID].conn.processes
	observer.publish(f.server.events, a.ID, []session.JobNotice{n})
	observer.publish(f.server.events, a.ID, []session.JobNotice{n})
	batch, _, _, _, _ := f.server.events.read(0)
	notices := 0
	for _, record := range batch {
		if record.kind == EventTurnEntry {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("%d end notices", notices)
	}
	entries := f.registry.endedProcessEntries(a.ID, "")
	if len(entries) != 1 || !strings.Contains(entries[0].Text, "exit 3") || entries[0].Link.ID != processID(a.ID, 1) {
		t.Fatalf("end notice: %+v", entries)
	}
}
func TestProcessesSavedShelfAndLogConfinement(t *testing.T) {
	f, a, e, path := processFixture(t)
	e.processMu.Lock()
	rows := append([]session.JobNotice{}, e.rows...)
	e.processMu.Unlock()
	file := f.registry.live[a.ID].conn.SessionFile
	raw, _ := json.Marshal(rows)
	if err := os.WriteFile(filepath.Join(filepath.Dir(file), "processes.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	// A different launch reads Ended without opening an engine or reviving commands.
	second := NewAgentRegistry(f.home, nil)
	defer second.Close()
	server := New(testToken, Options{Agents: second})
	w := request(t, server, "GET", "/app/v1/processes?state=stopped", "", bearer(), "127.0.0.1:4000")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"command":"npm run dev"`) {
		t.Fatalf("restart: %d %s", w.Code, w.Body)
	}
	outside := filepath.Join(t.TempDir(), "secret")
	_ = os.WriteFile(outside, []byte("secret bytes"), 0600)
	_ = os.Remove(path)
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	w = f.request(t, "GET", "/app/v1/processes/"+processID(a.ID, 1)+"/output", "")
	if w.Code != 404 || strings.Contains(w.Body.String(), "secret bytes") {
		t.Fatalf("output escaped: %d %s", w.Code, w.Body)
	}
	if _, err := openProcessLog(file, "../../secret"); err == nil {
		t.Fatal("traversal accepted")
	}
}
func TestProcessesOutputCursorSurvivesCompaction(t *testing.T) {
	f, a, _, path := processFixture(t)
	row, err := f.server.findProcess(context.Background(), processID(a.ID, 1))
	if err != nil {
		t.Fatal(err)
	}
	var cursor processOutputCursor
	raw, err := cursor.read(row, 10)
	if err != nil || string(raw) != "last line\n" {
		t.Fatalf("initial: %q %v", raw, err)
	}
	// The session trims in place, so its inode stays but the offset moves.
	_ = os.WriteFile(path, []byte("ready localhost:4321\nlast line\nnew line\n"), 0600)
	raw, err = cursor.read(row, 10)
	if err != nil || string(raw) != "new line\n" {
		t.Fatalf("append: %q %v", raw, err)
	}
	_ = os.WriteFile(path, []byte("last line\nnew line\nafter trim\n"), 0600)
	raw, err = cursor.read(row, 10)
	if err != nil || !strings.Contains(string(raw), "after trim") {
		t.Fatalf("trim: %q %v", raw, err)
	}
}

func TestProcessesFollowPreservesSplitUTF8(t *testing.T) {
	f, a, e, path := processFixture(t)
	_ = os.WriteFile(path, []byte{0xe2}, 0600)
	server := httptest.NewServer(f.server)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/app/v1/processes/"+processID(a.ID, 1)+"/output?follow=1", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte{0x82, 0xac, '\n'})
	file.Close()
	e.processMu.Lock()
	e.rows[0].State = session.JobDone
	e.processMu.Unlock()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "€") || strings.Contains(string(raw), "�") {
		t.Fatalf("split rune: %s", raw)
	}
}

func TestProcessesSnapshotKeepsOneTypedEndNotice(t *testing.T) {
	f, a, e, _ := processFixture(t)
	e.processMu.Lock()
	n := e.rows[0]
	n.State = session.JobDone
	e.rows[0] = n
	e.processMu.Unlock()
	record := agentRecord{Agent: a}
	notice := processEndEntry(a.ID, n)
	merged := mergeProcessEndEntries([]DisplayEntry{{ID: "old-entry", Kind: "notice", Role: "aside", Text: "job 1 exited 0: ready"}}, []DisplayEntry{notice})
	if len(merged) != 1 || merged[0].ID != notice.ID || merged[0].Link.ID != notice.Link.ID {
		t.Fatalf("duplicate engine ending: %+v", merged)
	}
	f.registry.mu.Lock()
	snapshot := f.registry.turnSnapshot(record)
	f.registry.mu.Unlock()
	if len(snapshot.Entries) != 1 || snapshot.Entries[0].Kind != "notice" || snapshot.Entries[0].ID != notice.ID {
		t.Fatalf("snapshot: %+v", snapshot.Entries)
	}
}

func TestProcessesStopAllRefusesRequestsFollowingShutdown(t *testing.T) {
	f, a, _, _ := processFixture(t)
	f.server.Close()
	opened := len(f.opens)
	w := f.request(t, "POST", "/app/v1/agents/"+a.ID+"/processes/stop-all", `{}`)
	if w.Code != 503 || len(f.opens) != opened {
		t.Fatalf("stop-all reopened a stopped engine: %d %s opens=%d", w.Code, w.Body, len(f.opens))
	}
}

package desktopbridge

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// jobsAgent is an engine that can list and stop its jobs. A plain fakeAgent
// is one that cannot, which is how the 409 sentences are reached.
type jobsAgent struct {
	*fakeAgent
	rows     []session.JobNotice
	listErr  error
	listed   atomic.Int32
	cancelID string
	line     string
	stopErr  error
	stopped  atomic.Int32
}

func (a *jobsAgent) JobNotices() ([]session.JobNotice, error) {
	a.listed.Add(1)
	return a.rows, a.listErr
}

func (a *jobsAgent) Cancel(id string) (string, error) {
	a.stopped.Add(1)
	a.cancelID = id
	return a.line, a.stopErr
}

type jobsRig struct {
	b       *Bridge
	a       *jobsAgent
	id      string
	fetched []string
	fetches atomic.Int32
	file    remote.FetchedFile
	fileErr error
}

func newJobsRig(t *testing.T, file string) *jobsRig {
	t.Helper()
	rig := &jobsRig{a: &jobsAgent{fakeAgent: &fakeAgent{events: make(chan session.Event, 8), model: Model}}}
	rig.b = New(testToken, func(string) (Connection, error) {
		return Connection{
			Agent: rig.a,
			Welcome: remote.Welcome{
				SessionFile: file, Workspace: "/project", Model: Model, Persistent: true,
				Launch: &remote.LaunchShape{OneModel: true},
			},
			FetchFile: func(path string) (remote.FetchedFile, error) {
				rig.fetches.Add(1)
				rig.fetched = append(rig.fetched, path)
				if rig.fileErr != nil {
					return remote.FetchedFile{}, rig.fileErr
				}
				return rig.file, nil
			},
			Close: func() {},
		}, nil
	})
	t.Cleanup(rig.b.Close)
	w := request(rig.b, "POST", "/api/engine/sessions", "{}")
	if w.Code != 200 {
		t.Fatalf("open: %d %s", w.Code, w.Body.String())
	}
	var snap Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	rig.id = snap.ID
	return rig
}

func (rig *jobsRig) call(method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://bridge"+path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	var rest []string
	if trimmed := strings.Trim(r.URL.Path, "/"); trimmed != "" {
		rest = strings.Split(trimmed, "/")
	}
	rig.b.sessions[rig.id].jobsRoute(w, r, rest)
	return w
}

func TestJobsListRelaysTheEngine(t *testing.T) {
	started := time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)
	rig := newJobsRig(t, "/places/chat-9/session.jsonl")
	// The engine's order is the order the route answers. Newer-first is the
	// engine's habit, not something this route sorts into.
	rig.a.rows = []session.JobNotice{
		{ID: 2, Name: "build", Command: "make build", Kind: session.JobKindCommand, State: session.JobDone, Started: started, Elapsed: 90 * time.Second, ExitCode: 0, LogPath: "/places/chat-9/jobs/2.log"},
		{ID: 9, Name: "nightly bench", Command: "make bench", Detail: "every 10s", Kind: session.JobKindWatch, State: session.JobRunning, Started: started, Elapsed: 4 * time.Second, Ticks: 3, LogPath: "/places/chat-9/jobs/9.log"},
		{ID: 1, Command: "sleep 1", Kind: session.JobKindCommand, State: session.JobStopped, Started: started, LogPath: "/places/chat-9/jobs/1.log"},
	}
	w := rig.call(http.MethodGet, "/", "")
	if w.Code != 200 {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	var got []jobWire
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != 2 || got[1].ID != 9 || got[2].ID != 1 {
		t.Fatalf("order: %+v", got)
	}
	if got[0].Name != "build" || got[0].Command != "make build" || got[0].Kind != "command" || got[0].State != "done" || got[0].StartedAt != "2026-10-09T15:00:00Z" || got[0].ElapsedMs != 90000 || got[0].ExitCode == nil || *got[0].ExitCode != 0 || got[0].LogPath != "/places/chat-9/jobs/2.log" {
		t.Fatalf("done job: %+v", got[0])
	}
	if got[1].Detail != "every 10s" || got[1].Kind != "watch" || got[1].State != "running" || got[1].Ticks != 3 || got[1].ElapsedMs != 4000 || got[1].ExitCode != nil {
		t.Fatalf("watch: %+v", got[1])
	}
	if strings.Contains(w.Body.String(), `"exitCode"`) && strings.Contains(w.Body.String(), `"id":1`) {
		// The stopped job is last. Its object must not carry an exit code; the
		// done job's zero is a real exit and stays.
		stopped := w.Body.String()[strings.LastIndex(w.Body.String(), `"id":1`):]
		if strings.Contains(stopped, "exitCode") {
			t.Fatalf("a stopped job reported an exit code: %s", stopped)
		}
	}
	if got[2].Name != "" || got[2].ExitCode != nil || got[2].Ticks != 0 {
		t.Fatalf("stopped job invented a fact: %+v", got[2])
	}

	rig.a.rows = nil
	if w := rig.call(http.MethodGet, "/", ""); w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("empty shelf: %d %s", w.Code, w.Body.String())
	}
	rig.a.listErr = errors.New("no such method")
	if w := rig.call(http.MethodGet, "/", ""); w.Code != 409 || !strings.Contains(w.Body.String(), "no such method") {
		t.Fatalf("list error: %d %s", w.Code, w.Body.String())
	}
	if w := rig.call(http.MethodPost, "/", "{}"); w.Code != 405 {
		t.Fatalf("list POST: %d", w.Code)
	}

	plain := newJobsRig(t, "/places/chat-9/session.jsonl")
	plain.b.sessions[plain.id].conn.Agent = plain.a.fakeAgent
	w = plain.call(http.MethodGet, "/", "")
	if w.Code != 409 || !strings.Contains(w.Body.String(), "this engine cannot list its jobs") {
		t.Fatalf("no door: %d %s", w.Code, w.Body.String())
	}
}

func TestStopCancelsTheNamedJob(t *testing.T) {
	rig := newJobsRig(t, "/places/chat-9/session.jsonl")
	rig.a.line = "stopped job 3 (nightly bench) — its log is kept"
	w := rig.call(http.MethodPost, "/3/stop", "{}")
	if w.Code != 200 || rig.a.cancelID != "job:3" || !strings.Contains(w.Body.String(), `"accepted":true`) || !strings.Contains(w.Body.String(), rig.a.line) {
		t.Fatalf("stop: %d %s cancel %q", w.Code, w.Body.String(), rig.a.cancelID)
	}
	rig.a.stopErr = errors.New("there is no job 8 in this session")
	w = rig.call(http.MethodPost, "/8/stop", "{}")
	if w.Code != 409 || !strings.Contains(w.Body.String(), "there is no job 8 in this session") || rig.a.cancelID != "job:8" {
		t.Fatalf("refusal: %d %s cancel %q", w.Code, w.Body.String(), rig.a.cancelID)
	}
	if w := rig.call(http.MethodGet, "/3/stop", ""); w.Code != 405 {
		t.Fatalf("stop GET: %d", w.Code)
	}

	plain := newJobsRig(t, "/places/chat-9/session.jsonl")
	plain.b.sessions[plain.id].conn.Agent = plain.a.fakeAgent
	w = plain.call(http.MethodPost, "/3/stop", "{}")
	if w.Code != 409 || !strings.Contains(w.Body.String(), "this engine cannot stop a job") {
		t.Fatalf("no door: %d %s", w.Code, w.Body.String())
	}
}

func TestJobLogIsTailedAndConfinedToTheSession(t *testing.T) {
	dir := t.TempDir()
	chat := filepath.Join(dir, "chat")
	if err := os.MkdirAll(filepath.Join(chat, "jobs"), 0o755); err != nil {
		t.Fatal(err)
	}
	sessionFile := filepath.Join(chat, "session.jsonl")
	if err := os.WriteFile(sessionFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(dir, "secret.log")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(chat, "jobs", "escape.log")); err != nil {
		t.Fatal(err)
	}
	rig := newJobsRig(t, sessionFile)
	inside := filepath.Join(chat, "jobs", "3.log")
	rig.a.rows = []session.JobNotice{{ID: 3, State: session.JobRunning, LogPath: inside}}
	rig.file = remote.FetchedFile{Bytes: []byte("\x1b[32mok\x1b[0m\r\nprogress 10%\rprogress 100%\r\ndone\r\n")}

	w := rig.call(http.MethodGet, "/3/log?path=/etc/passwd", "")
	if w.Code != 200 {
		t.Fatalf("log: %d %s", w.Code, w.Body.String())
	}
	var got struct {
		Text      string `json:"text"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Text != "ok\nprogress 100%\ndone" || got.Truncated || len(rig.fetched) != 1 || rig.fetched[0] != filepath.Clean(inside) {
		t.Fatalf("tail: %+v fetched %v", got, rig.fetched)
	}

	rig.fetched = nil
	rig.file.Bytes = []byte("abcdefghij")
	w = rig.call(http.MethodGet, "/3/log?tail=4", "")
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || got.Text != "ghij" || !got.Truncated {
		t.Fatalf("short tail: %d %+v", w.Code, got)
	}

	big := make([]byte, jobLogCap+100)
	for i := range big {
		big[i] = 'a'
	}
	copy(big[len(big)-4:], "TAIL")
	rig.file.Bytes = big
	w = rig.call(http.MethodGet, "/3/log", "")
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Truncated || len(got.Text) != jobLogCap || !strings.HasSuffix(got.Text, "TAIL") {
		t.Fatalf("cap: truncated %v len %d suffix %q", got.Truncated, len(got.Text), suffix(got.Text))
	}

	if w := rig.call(http.MethodGet, "/3/log?tail=no", ""); w.Code != 400 || !strings.Contains(w.Body.String(), "tail must be a number of bytes") {
		t.Fatalf("bad tail: %d %s", w.Code, w.Body.String())
	}

	before := rig.fetches.Load()
	rig.a.rows = []session.JobNotice{{ID: 4, State: session.JobRunning, LogPath: secret}}
	w = rig.call(http.MethodGet, "/4/log", "")
	if w.Code != 403 || !strings.Contains(w.Body.String(), "outside this conversation") || rig.fetches.Load() != before {
		t.Fatalf("workspace path: %d %s fetches %d", w.Code, w.Body.String(), rig.fetches.Load())
	}
	rig.a.rows = []session.JobNotice{{ID: 5, State: session.JobRunning, LogPath: filepath.Join(chat, "jobs", "escape.log")}}
	w = rig.call(http.MethodGet, "/5/log", "")
	if w.Code != 403 || rig.fetches.Load() != before {
		t.Fatalf("symlink: %d %s fetches %d", w.Code, w.Body.String(), rig.fetches.Load())
	}
	rig.a.rows = []session.JobNotice{{ID: 6, State: session.JobRunning, LogPath: filepath.Join(chat, "jobs", "..", "..", "secret.log")}}
	w = rig.call(http.MethodGet, "/6/log", "")
	if w.Code != 403 || rig.fetches.Load() != before {
		t.Fatalf("dotdot: %d %s", w.Code, w.Body.String())
	}
	rig.a.rows = []session.JobNotice{{ID: 7, State: session.JobDone}}
	w = rig.call(http.MethodGet, "/7/log", "")
	if w.Code != 404 || !strings.Contains(w.Body.String(), "no log") || rig.fetches.Load() != before {
		t.Fatalf("missing log: %d %s", w.Code, w.Body.String())
	}
	w = rig.call(http.MethodGet, "/99/log", "")
	if w.Code != 404 || !strings.Contains(w.Body.String(), "there is no job 99") || rig.fetches.Load() != before {
		t.Fatalf("unknown job: %d %s", w.Code, w.Body.String())
	}
}

func suffix(s string) string {
	if len(s) > 8 {
		return s[len(s)-8:]
	}
	return s
}

func TestAJobUpdatePublishesAJobsRecord(t *testing.T) {
	started := time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)
	rig := newJobsRig(t, "/places/chat-9/session.jsonl")
	rig.a.rows = []session.JobNotice{
		{ID: 4, Name: "nightly bench", Command: "make bench", Kind: session.JobKindWatch, State: session.JobRunning, Started: started, Elapsed: 4 * time.Second, Ticks: 2, LogPath: "/places/chat-9/jobs/4.log"},
		{ID: 2, Name: "build", Command: "make build", Kind: session.JobKindCommand, State: session.JobDone, Started: started, Elapsed: time.Minute, ExitCode: 0, LogPath: "/places/chat-9/jobs/2.log"},
	}
	w := request(rig.b, "POST", "/api/engine/sessions/"+rig.id+"/turn", `{"text":"hello"}`)
	if w.Code != 200 {
		t.Fatalf("turn: %d %s", w.Code, w.Body.String())
	}
	notice := rig.a.rows[0]
	rig.a.events <- session.Event{Kind: session.EventJobUpdate, Job: &notice}
	got := waitJobsRecord(t, rig.b)
	if got.ChatID != "chat-9" || got.Running != 1 || len(got.Jobs) != 2 || got.Jobs[0].ID != 4 || got.Jobs[0].State != "running" || got.Jobs[1].ID != 2 || got.Jobs[1].ExitCode == nil || *got.Jobs[1].ExitCode != 0 {
		t.Fatalf("rollup: %+v", got)
	}

	// The same news arriving again (the turn and the standing lane both hear
	// one update) does not publish a second copy.
	rig.a.events <- session.Event{Kind: session.EventJobUpdate, Job: &notice}
	waitFor(t, "the second job update", func() bool {
		s := rig.b.sessions[rig.id]
		s.mu.Lock()
		defer s.mu.Unlock()
		n := 0
		for _, rec := range s.records {
			if rec.Event != nil && rec.Event.Raw.Job != nil {
				n++
			}
		}
		return n >= 2
	})
	if n := countJobsRecords(rig.b); n != 1 {
		t.Fatalf("duplicate roll-ups: %d", n)
	}
}

func waitJobsRecord(t *testing.T, b *Bridge) jobsRollup {
	t.Helper()
	var got jobsRollup
	waitFor(t, "a jobs record", func() bool {
		f := b.worldFeed()
		f.mu.Lock()
		defer f.mu.Unlock()
		for i := len(f.ring) - 1; i >= 0; i-- {
			if f.ring[i].Type != jobsWorldKind {
				continue
			}
			if err := json.Unmarshal(f.ring[i].Payload, &got); err != nil {
				t.Fatal(err)
			}
			return true
		}
		return false
	})
	return got
}

func countJobsRecords(b *Bridge) int {
	f := b.worldFeed()
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, rec := range f.ring {
		if rec.Type == jobsWorldKind {
			n++
		}
	}
	return n
}

func TestJobsRoutesNeedTheToken(t *testing.T) {
	rig := newJobsRig(t, "/places/chat-9/session.jsonl")
	rig.a.rows = []session.JobNotice{{ID: 3, State: session.JobRunning, LogPath: "/places/chat-9/jobs/3.log"}}
	paths := []struct{ method, path string }{
		{http.MethodGet, "/api/engine/sessions/" + rig.id + "/jobs"},
		{http.MethodPost, "/api/engine/sessions/" + rig.id + "/jobs/3/stop"},
		{http.MethodGet, "/api/engine/sessions/" + rig.id + "/jobs/3/log"},
	}
	for _, p := range paths {
		for _, auth := range []string{"", "Bearer nope"} {
			r := httptest.NewRequest(p.method, p.path, strings.NewReader("{}"))
			r.Header.Set("Content-Type", "application/json")
			if auth != "" {
				r.Header.Set("Authorization", auth)
			}
			w := httptest.NewRecorder()
			rig.b.ServeHTTP(w, r)
			if w.Code != 401 {
				t.Fatalf("%s %s with %q: %d", p.method, p.path, auth, w.Code)
			}
		}
	}
	if rig.a.listed.Load() != 0 || rig.a.stopped.Load() != 0 || rig.fetches.Load() != 0 {
		t.Fatalf("an unauthenticated request reached the engine: list %d stop %d fetch %d", rig.a.listed.Load(), rig.a.stopped.Load(), rig.fetches.Load())
	}
}

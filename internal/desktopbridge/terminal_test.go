package desktopbridge

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

type termRig struct {
	srv    *httptest.Server
	bridge *Bridge
	base   string
}

func newTermRig(t *testing.T) *termRig {
	t.Helper()
	a := &fakeAgent{events: make(chan session.Event, 8), model: Model}
	dir := t.TempDir()
	b := New(testToken, func(string) (Connection, error) {
		return Connection{Agent: a, Welcome: remote.Welcome{SessionFile: "s.jsonl", Workspace: dir, Model: Model, Persistent: true, Launch: &remote.LaunchShape{OneModel: true}}, Close: func() {}}, nil
	})
	srv := httptest.NewServer(b)
	t.Cleanup(func() { srv.Close(); b.Close() })
	r := &termRig{srv: srv, bridge: b}
	var snap Snapshot
	r.do(t, "POST", "/api/engine/sessions", `{}`, &snap)
	r.base = "/api/engine/sessions/" + snap.ID + "/terminals"
	return r
}

func (r *termRig) req(method, path, body, token string) (*http.Response, error) {
	req, _ := http.NewRequest(method, r.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return http.DefaultClient.Do(req)
}

func (r *termRig) do(t *testing.T, method, path, body string, out any) int {
	t.Helper()
	resp, err := r.req(method, path, body, testToken)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func (r *termRig) start(t *testing.T, body string) TerminalInfo {
	t.Helper()
	var info TerminalInfo
	if code := r.do(t, "POST", r.base, body, &info); code != 200 {
		t.Fatalf("start: %d", code)
	}
	return info
}

func (r *termRig) send(t *testing.T, id, text string) {
	t.Helper()
	in := base64.StdEncoding.EncodeToString([]byte(text))
	if code := r.do(t, "POST", r.base+"/"+id+"/input", `{"dataBase64":"`+in+`"}`, nil); code != 200 {
		t.Fatal(code)
	}
}

// collect reads the stream until want appears, the exit record arrives, or wait passes.
func (r *termRig) collect(t *testing.T, id string, after uint64, want string, wait time.Duration) (string, *TerminalInfo) {
	t.Helper()
	req, _ := http.NewRequest("GET", r.srv.URL+r.base+"/"+id+"/stream?after="+strconv.FormatUint(after, 10), nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	timer := time.AfterFunc(wait, func() { resp.Body.Close() })
	defer timer.Stop()
	var got strings.Builder
	var exit *TerminalInfo
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var rec termRecord
		_ = json.Unmarshal([]byte(line[6:]), &rec)
		if rec.Type == "exit" {
			exit = rec.Info
			break
		}
		b, _ := base64.StdEncoding.DecodeString(rec.DataBase64)
		got.Write(b)
		if want != "" && strings.Contains(got.String(), want) {
			break
		}
	}
	return got.String(), exit
}

func (r *termRig) lookup(t *testing.T, id string) *terminal {
	t.Helper()
	r.bridge.mu.Lock()
	defer r.bridge.mu.Unlock()
	for _, s := range r.bridge.sessions {
		if found := s.terminals().get(id); found != nil {
			return found
		}
	}
	t.Fatal("terminal not found")
	return nil
}

func TestTerminalRoutesNeedTheEngineToken(t *testing.T) {
	r := newTermRig(t)
	info := r.start(t, `{}`)
	id := r.base + "/" + info.ID
	for _, token := range []string{"", "wrong"} {
		for _, p := range []struct{ m, path string }{{"GET", r.base}, {"POST", r.base}, {"GET", id}, {"GET", id + "/stream"}, {"GET", id + "/output"}, {"POST", id + "/input"}, {"POST", id + "/resize"}, {"POST", id + "/close"}, {"POST", id + "/remove"}} {
			resp, err := r.req(p.m, p.path, `{}`, token)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 401 {
				t.Fatalf("%s %s with token %q: %d, want 401", p.m, p.path, token, resp.StatusCode)
			}
		}
	}
}

func TestTerminalSpawnInputStreamAndPlainOutput(t *testing.T) {
	r := newTermRig(t)
	info := r.start(t, `{"cols":90,"rows":20}`)
	if info.Kind != "terminal" || info.State != "running" || info.Cols != 90 || info.Rows != 20 {
		t.Fatalf("%+v", info)
	}
	r.send(t, info.ID, "echo marker-$((6*7))\n")
	got, _ := r.collect(t, info.ID, 0, "marker-42", 5*time.Second)
	if !strings.Contains(got, "marker-42") {
		t.Fatalf("no output: %q", got)
	}
	var out struct{ Text string }
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline) && !strings.Contains(out.Text, "marker-42"); {
		r.do(t, "GET", r.base+"/"+info.ID+"/output", "", &out)
	}
	if strings.Contains(out.Text, "\x1b") || !strings.Contains(out.Text, "marker-42") {
		t.Fatalf("plain output: %q", out.Text)
	}
}

func TestTerminalRunsInTheWorkspace(t *testing.T) {
	r := newTermRig(t)
	info := r.start(t, `{"command":"pwd"}`)
	got, _ := r.collect(t, info.ID, 0, "", 5*time.Second)
	if !strings.Contains(got, info.Cwd) || info.Cwd == "" {
		t.Fatalf("cwd %q not in %q", info.Cwd, got)
	}
}

func TestTerminalResize(t *testing.T) {
	r := newTermRig(t)
	info := r.start(t, `{}`)
	var after TerminalInfo
	if code := r.do(t, "POST", r.base+"/"+info.ID+"/resize", `{"cols":50,"rows":12}`, &after); code != 200 || after.Cols != 50 || after.Rows != 12 {
		t.Fatalf("%d %+v", code, after)
	}
	r.send(t, info.ID, "stty size\n")
	got, _ := r.collect(t, info.ID, 0, "12 50", 5*time.Second)
	if !strings.Contains(got, "12 50") {
		t.Fatalf("stty did not see the new size: %q", got)
	}
}

func TestJobKeepsItsLogAndExitStatus(t *testing.T) {
	r := newTermRig(t)
	info := r.start(t, `{"command":"echo nightly-done; sleep 0.2; exit 3","title":"nightly-bench"}`)
	if info.Kind != "job" || info.Title != "nightly-bench" {
		t.Fatalf("%+v", info)
	}
	_, exit := r.collect(t, info.ID, 0, "", 8*time.Second)
	if exit == nil || exit.State != "exited" || exit.ExitCode == nil || *exit.ExitCode != 3 || exit.DurationMs < 150 || exit.EndedAt == "" {
		t.Fatalf("%+v", exit)
	}
	var list []TerminalInfo
	r.do(t, "GET", r.base, "", &list)
	if len(list) != 1 || list[0].ID != info.ID {
		t.Fatalf("job should stay listed: %+v", list)
	}
	got, _ := r.collect(t, info.ID, 0, "", 3*time.Second)
	if !strings.Contains(got, "nightly-done") {
		t.Fatalf("replay: %q", got)
	}
}

func TestScrollbackIsBoundedAndReplays(t *testing.T) {
	r := newTermRig(t)
	info := r.start(t, `{"command":"head -c 700000 /dev/zero | tr '\\0' x; echo END"}`)
	_, exit := r.collect(t, info.ID, 0, "", 15*time.Second)
	if exit == nil || exit.Bytes <= scrollbackBytes {
		t.Fatalf("expected more than the bound to flow: %+v", exit)
	}
	got, _ := r.collect(t, info.ID, 0, "", 5*time.Second)
	if len(got) != scrollbackBytes || !strings.Contains(got, "END") {
		t.Fatalf("replayed %d bytes", len(got))
	}
	// A reader that already holds everything gets only the exit record.
	if tail, _ := r.collect(t, info.ID, exit.Bytes, "", 5*time.Second); tail != "" {
		t.Fatalf("resume from the end replayed %d bytes", len(tail))
	}
}

func TestCloseKillsTheProcessGroupByItsOwnPid(t *testing.T) {
	r := newTermRig(t)
	info := r.start(t, `{"command":"sleep 300 & sleep 300 & wait"}`)
	pgid := r.lookup(t, info.ID).cmd.Process.Pid
	time.Sleep(300 * time.Millisecond)
	var closed TerminalInfo
	if code := r.do(t, "POST", r.base+"/"+info.ID+"/close", `{}`, &closed); code != 200 {
		t.Fatal(code)
	}
	if closed.State != "closed" || closed.ExitCode == nil {
		t.Fatalf("%+v", closed)
	}
	for i := 0; i < 20 && syscall.Kill(-pgid, 0) == nil; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if syscall.Kill(-pgid, 0) == nil {
		t.Fatal("process group still alive")
	}
}

func TestClosingAnInteractiveTerminalDropsIt(t *testing.T) {
	r := newTermRig(t)
	info := r.start(t, `{}`)
	r.do(t, "POST", r.base+"/"+info.ID+"/close", `{}`, nil)
	if code := r.do(t, "GET", r.base+"/"+info.ID, "", nil); code != 404 {
		t.Fatalf("%d", code)
	}
}

func TestTerminalDoesNotInheritEngineSecrets(t *testing.T) {
	t.Setenv("CODEAF_DESKTOP_TOKEN", "leak-canary-123")
	t.Setenv("CODEAF_PLAIN_SETTING", "kept-canary")
	r := newTermRig(t)
	info := r.start(t, `{"command":"env"}`)
	got, _ := r.collect(t, info.ID, 0, "", 5*time.Second)
	if strings.Contains(got, "leak-canary-123") || !strings.Contains(got, "kept-canary") || !strings.Contains(got, "TERM=xterm-256color") {
		t.Fatalf("environment: %q", got)
	}
}

func TestPlainOutputCollapsesRedrawsAndEscapes(t *testing.T) {
	got := plainOutput([]byte("\x1b[32mok\x1b[0m\r\nprogress 10%\rprogress 100%\r\ndone\r\n"))
	if got != "ok\nprogress 100%\ndone" {
		t.Fatalf("%q", got)
	}
}

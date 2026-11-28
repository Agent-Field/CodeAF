package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

const hostedTestOrg = "factory_0123456789abcdef"

const hostedTestQuestion = "What should I do next?"

type hostedTestRow struct {
	appwire.Agent
	Hosted *struct {
		Org, Session, Owner, Role, Note string
		Verbs                           []string
	} `json:"hosted"`
}

type hostedTestServer struct {
	server  *httptest.Server
	bearer  string
	calls   chan remote.Frame
	closed  chan struct{}
	session string
	mu      sync.Mutex
	list    string
	status  int
	refusal string
	guest   string
	made    []string
	seen    []string
}

func newHostedTestServer(t *testing.T) *hostedTestServer {
	t.Helper()
	f := &hostedTestServer{
		bearer: rand.Text(), calls: make(chan remote.Frame, 64), closed: make(chan struct{}, 8),
		session: orgID('a'), list: `{"sessions":[]}`, made: acl.Verbs,
	}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seen = append(f.seen, r.Method+" "+r.URL.RequestURI())
		list, status, refusal, guest, made := f.list, f.status, f.refusal, f.guest, f.made
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+f.bearer {
			t.Error("the organization did not receive its held sign-in in Authorization")
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(r.URL.String(), f.bearer) || strings.Contains(string(body), f.bearer) {
			t.Error("the sign-in appeared outside Authorization")
		}
		for name, values := range r.Header {
			if name != "Authorization" && strings.Contains(strings.Join(values, " "), f.bearer) {
				t.Errorf("the sign-in appeared in the %s header", name)
			}
		}
		if status != 0 {
			if status == http.StatusNotFound && refusal == "" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": refusal})
			return
		}
		switch r.URL.Path {
		case "/v1/hosted/sessions":
			if r.Method == http.MethodPost {
				_ = json.NewEncoder(w).Encode(map[string]any{"id": f.session, "state": "running", "verbs": made})
			} else {
				_, _ = io.WriteString(w, list)
			}
		case "/v1/hosted/sessions/" + f.session + "/wire":
			f.wire(t, w, r, guest)
		default:
			t.Errorf("unexpected organization request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *hostedTestServer) wire(t *testing.T, w http.ResponseWriter, r *http.Request, guest string) {
	socket, err := websocket.Accept(w, r, nil)
	if err != nil {
		t.Error(err)
		return
	}
	conn := websocket.NetConn(context.Background(), socket, websocket.MessageBinary)
	defer func() {
		_ = conn.Close()
		f.closed <- struct{}{}
	}()
	decoder, encoder := json.NewDecoder(conn), json.NewEncoder(conn)
	send := func(kind string, id uint64, value any, refusal string) {
		payload, _ := json.Marshal(value)
		_ = encoder.Encode(remote.Frame{Kind: kind, ID: id, Payload: payload, Error: refusal})
	}
	for {
		var frame remote.Frame
		if decoder.Decode(&frame) != nil {
			return
		}
		if strings.Contains(string(frame.Payload), f.bearer) {
			t.Error("the sign-in appeared on the conversation wire")
		}
		if frame.Kind == "hello" {
			f.calls <- frame
			users := 1
			send("welcome", 0, remote.Welcome{Version: remote.Version, Title: "Hosted chat", UserMessages: &users}, "")
			continue
		}
		switch frame.Method {
		case remote.MethodTranscript:
			send("result", frame.ID, []session.DisplayEntry{{Role: "user", Text: "From this chat's history."}, {Role: "assistant", Text: "Its reply."}}, "")
		case remote.MethodQuestionWatch:
			send("result", frame.ID, nil, "")
			if guest != "" {
				q := &session.Question{Kind: session.QuestionAsk, ID: 4, Head: hostedTestQuestion}
				send("question", 0, remote.WireEvent(session.Event{Kind: session.EventQuestion, Question: q}), "")
			}
		case remote.MethodQuestionResolve:
			f.calls <- frame
			send("result", frame.ID, nil, guest)
		case remote.MethodSubmit:
			f.calls <- frame
			if guest != "" {
				send("result", frame.ID, nil, guest)
				continue
			}
			send("result", frame.ID, remote.StreamRef{Stream: frame.ID}, "")
			send("closed", frame.ID, nil, "")
		case remote.MethodInterrupt:
			f.calls <- frame
			send("result", frame.ID, nil, guest)
		default:
			send("result", frame.ID, nil, "")
		}
	}
}

func (f *hostedTestServer) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.seen)
}

func hostedTestHome(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("CODEAF_HOME", filepath.Join(root, "state"))
	t.Setenv(config.ProfileDirEnv, filepath.Join(root, "profile"))
}

func hostedTestBackend(t *testing.T, base, token string) (*actionWire, *appRecorder) {
	t.Helper()
	out := newAppRecorder()
	b := newFakeBackend(out, func(appOpenAsk) (*appConn, error) {
		t.Error("hosted chat reached the local opener")
		return nil, errors.New("unexpected local open")
	})
	t.Cleanup(func() {
		_ = b.Close()
		if token == "" {
			return
		}
		out.mu.Lock()
		defer out.mu.Unlock()
		for i, data := range out.data {
			body, err := json.Marshal(data)
			if err != nil {
				t.Error(err)
			}
			if strings.Contains(string(body), token) {
				t.Errorf("the sign-in appeared in the %s event", out.events[i])
			}
		}
	})
	b.mint = func(string, ...func(string) error) (string, error) {
		t.Error("hosted chat reached the local mint")
		return "", errors.New("unexpected local mint")
	}
	h := b.factoryHub()
	h.mu.Lock()
	h.rows = []appFactoryRow{{ID: hostedTestOrg, URL: base, Mode: "team"}}
	h.loaded = true
	h.mu.Unlock()
	if token != "" {
		h.hold(hostedTestOrg, token, time.Hour)
	}
	return newActionWire(t, b), out
}

func hostedTestCall[T any](t *testing.T, w *actionWire, method string, input any) T {
	t.Helper()
	raw, err := w.call(method, input)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return result
}

func hostedTestPlaces(t *testing.T) map[string]bool {
	t.Helper()
	tree := map[string]bool{}
	err := filepath.WalkDir(session.PlacesRoot(), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		tree[path] = d.IsDir()
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return tree
}

func hostedTestNext[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("the server did not see the expected wire action")
		var zero T
		return zero
	}
}

func TestAppHostedV1CreateOnOrganizationServer(t *testing.T) {
	hostedTestHome(t)
	f := newHostedTestServer(t)
	// A policy whose owner role cannot steer: the chat just made shows what the server said.
	f.made = []string{"view", "comment", "run", "answer"}
	w, _ := hostedTestBackend(t, f.server.URL, f.bearer)
	places := hostedTestPlaces(t)
	workspace := filepath.Join(t.TempDir(), "untouched")
	const first = "The first message for this organization."
	got := hostedTestCall[struct{ Agent hostedTestRow }](t, w, "agents.create", map[string]string{
		"org": hostedTestOrg, "workspace": workspace, "text": first,
	}).Agent
	if got.ID != f.session+"_"+hostedTestOrg || got.Hosted == nil || got.Hosted.Org != hostedTestOrg || got.Workspace != "" {
		t.Fatalf("hosted create = %+v, hosted = %+v", got.Agent, got.Hosted)
	}
	if !slices.Equal(got.Hosted.Verbs, f.made) {
		t.Fatalf("created chat verbs = %v, the server said %v", got.Hosted.Verbs, f.made)
	}
	if !slices.Contains(w.hello, "agents.hosted") {
		t.Fatal("the app did not advertise hosted chats")
	}
	var hello remote.Hello
	frame := hostedTestNext(t, f.calls)
	if frame.Kind != "hello" || json.Unmarshal(frame.Payload, &hello) != nil || hello.Launch == nil || !hello.Launch.Interactive || hello.Workspace != "" {
		t.Fatal("the hosted wire did not receive the desktop launch")
	}
	var submit remote.SubmitArgs
	frame = hostedTestNext(t, f.calls)
	if frame.Method != remote.MethodSubmit || json.Unmarshal(frame.Payload, &submit) != nil || submit.Text != first {
		t.Fatal("the server conversation did not receive the first message")
	}
	local := hostedTestCall[appwire.AgentsResult](t, w, "agents.list", struct{}{})
	if len(local.Agents) != 0 || !reflect.DeepEqual(places, hostedTestPlaces(t)) {
		t.Fatal("creating a hosted chat changed the local inventory or places")
	}
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatalf("the local workspace was touched: %v", err)
	}
	if want := []string{"POST /v1/hosted/sessions", "GET /v1/hosted/sessions/" + f.session + "/wire"}; !slices.Equal(want, f.requests()) {
		t.Fatalf("requests = %v, want %v", f.requests(), want)
	}
}

func TestAppHostedV2ListOnlyOwnAndShared(t *testing.T) {
	hostedTestHome(t)
	f := newHostedTestServer(t)
	f.list = `{"sessions":[
		{"id":"` + orgID('a') + `","state":"running","last_active":7,"verbs":["view","comment","edit"]},
		{"id":"` + orgID('b') + `","state":"stopped","created":6},
		{"id":"bad","state":"running"}
	],"shared":[
		{"id":"` + orgID('c') + `","state":"running","last_active":8,"access":"shared","role":"edit","owner":"noor","verbs":["view","run"]},
		{"id":"` + orgID('d') + `","state":"running","last_active":9,"access":"operator"}
	],"hosting":{"local_sessions":true}}`
	w, _ := hostedTestBackend(t, f.server.URL, f.bearer)
	type listed struct{ Agents []hostedTestRow }
	got := hostedTestCall[listed](t, w, "agents.hosted_list", map[string]string{"org": hostedTestOrg}).Agents
	if len(got) != 3 {
		t.Fatalf("listed %d chats, want three", len(got))
	}
	for i, session := range []string{orgID('c'), orgID('a'), orgID('b')} {
		if got[i].Hosted == nil || got[i].Hosted.Session != session || got[i].Title != "" {
			t.Fatalf("row %d = %+v", i, got[i])
		}
	}
	// The verbs are the server's own words. This organization's policy gives its edit role run
	// and not steer or answer, and holds back everything but three verbs from the owner, which
	// the default roles would not have done.
	shared := got[0].Hosted
	if shared.Owner != "noor" || shared.Role != "edit" || !slices.Equal(shared.Verbs, []string{"view", "run"}) {
		t.Fatalf("shared access = %+v", shared)
	}
	if got[1].State != "idle" || got[2].State != "stopped" || !slices.Equal(got[1].Hosted.Verbs, []string{"view", "comment", "edit"}) {
		t.Fatalf("owner rows = %+v, %+v", got[1], got[2])
	}
	// A row with no verbs is view only, for another person's chat and for the person's own.
	if !slices.Equal(got[2].Hosted.Verbs, []string{"view"}) {
		t.Fatalf("own row with no verbs = %+v", got[2].Hosted)
	}
	for name, list := range map[string]string{
		"a role name the program has never heard of": `{"shared":[{"id":"` + orgID('c') + `","access":"shared","role":"new-role"}]}`,
		"a familiar role name and no verbs":          `{"shared":[{"id":"` + orgID('c') + `","access":"shared","role":"edit"}]}`,
		"verbs that name nothing it knows":           `{"shared":[{"id":"` + orgID('c') + `","access":"shared","verbs":["bogus"]}]}`,
		"verbs empty":                                `{"sessions":[{"id":"` + orgID('c') + `","verbs":[]}]}`,
	} {
		f.mu.Lock()
		f.list = list
		f.mu.Unlock()
		got = hostedTestCall[listed](t, w, "agents.hosted_list", map[string]string{"org": hostedTestOrg}).Agents
		if len(got) != 1 || got[0].Hosted == nil || !slices.Equal(got[0].Hosted.Verbs, []string{"view"}) {
			t.Fatalf("%s: rows = %+v", name, got)
		}
	}
	// Verbs come back once each, in the fixed order, and only the ones the program knows.
	f.mu.Lock()
	f.list = `{"shared":[{"id":"` + orgID('c') + `","access":"shared","verbs":["steer","view","bogus","view","answer"]}]}`
	f.mu.Unlock()
	got = hostedTestCall[listed](t, w, "agents.hosted_list", map[string]string{"org": hostedTestOrg}).Agents
	if len(got) != 1 || got[0].Hosted == nil || !slices.Equal(got[0].Hosted.Verbs, []string{"view", "steer", "answer"}) {
		t.Fatalf("messy verbs rows = %+v", got)
	}
	f.mu.Lock()
	f.list = ""
	f.mu.Unlock()
	got = hostedTestCall[listed](t, w, "agents.hosted_list", map[string]string{"org": hostedTestOrg}).Agents
	if got == nil || len(got) != 0 {
		t.Fatalf("empty list = %#v", got)
	}
}

func TestAppHostedV3ReopenHistoryAndDetach(t *testing.T) {
	hostedTestHome(t)
	f := newHostedTestServer(t)
	// The server says its owner holds every verb here. Without that word the chat would read view only.
	held, _ := json.Marshal(acl.Verbs)
	f.list = `{"sessions":[{"id":"` + f.session + `","state":"running","verbs":` + string(held) + `}]}`
	w, _ := hostedTestBackend(t, f.server.URL, f.bearer)
	p := map[string]string{"id": f.session + "_" + hostedTestOrg}
	places := hostedTestPlaces(t)
	for i := 0; i < 2; i++ {
		got := hostedTestCall[struct{ Agent hostedTestRow }](t, w, "agents.open", p).Agent
		if got.ID != p["id"] || got.Title != "Hosted chat" || got.Hosted == nil || !slices.Equal(got.Hosted.Verbs, acl.Verbs) {
			t.Fatalf("opened chat = %+v, hosted = %+v", got.Agent, got.Hosted)
		}
		page := hostedTestCall[appwire.EntriesResult](t, w, "agents.entries", p)
		if len(page.Entries) != 2 || page.Entries[0].Text != "From this chat's history." || page.Entries[1].Text != "Its reply." {
			t.Fatalf("hosted history = %+v", page)
		}
		older := hostedTestCall[appwire.EntriesResult](t, w, "agents.entries", map[string]any{
			"id": p["id"], "before": page.Entries[1].ID, "limit": 1,
		})
		if len(older.Entries) != 1 || older.More || older.Entries[0].ID != page.Entries[0].ID {
			t.Fatalf("older hosted history = %+v", older)
		}
		_, err := w.call("agents.rename", map[string]string{"id": p["id"], "title": "A local title"})
		if err == nil || err.Message != "That is not available for a chat on "+orgHostName(f.server.URL)+"." {
			t.Fatalf("local action on hosted chat = %v", err)
		}
		_ = hostedTestCall[appwire.Empty](t, w, "agents.close", p)
		hostedTestNext(t, f.closed)
		hello := hostedTestNext(t, f.calls)
		if hello.Kind != "hello" {
			t.Fatalf("attach %d sent %q", i, hello.Kind)
		}
	}
	upgrades := 0
	for _, request := range f.requests() {
		if !strings.HasPrefix(request, "GET ") {
			t.Fatalf("open and close sent %s", request)
		}
		if strings.HasSuffix(request, "/wire") {
			upgrades++
		}
	}
	if upgrades != 2 || len(f.calls) != 0 || !reflect.DeepEqual(places, hostedTestPlaces(t)) {
		t.Fatal("reopen and detach did not stay on the two organization wires")
	}
}

func TestAppHostedV5GuestVerbsAndExactRefusal(t *testing.T) {
	hostedTestHome(t)
	f := newHostedTestServer(t)
	const sentence = "You can read this chat but not send a message."
	f.guest = sentence
	f.list = `{"shared":[{"id":"` + f.session + `","access":"shared","owner":"noor","role":"view","verbs":["view"]}]}`
	w, out := hostedTestBackend(t, f.server.URL, f.bearer)
	p := map[string]string{"id": f.session + "_" + hostedTestOrg, "text": "A guest's message."}
	rows := hostedTestCall[appwire.AgentsResult](t, w, "agents.hosted_list", map[string]string{"org": hostedTestOrg}).Agents
	if len(rows) != 1 || rows[0].Hosted == nil || !slices.Equal(rows[0].Hosted.Verbs, []string{"view"}) {
		t.Fatalf("view role in list = %+v", rows)
	}
	got := hostedTestCall[struct{ Agent hostedTestRow }](t, w, "agents.open", p).Agent
	if got.Hosted == nil || !slices.Equal(got.Hosted.Verbs, []string{"view"}) {
		t.Fatalf("view role = %+v", got.Hosted)
	}
	_, err := w.call("agents.send", p)
	if err == nil || err.Message != sentence {
		t.Fatalf("guest send = %v, want %q", err, sentence)
	}
	_ = hostedTestNext(t, f.calls)
	if frame := hostedTestNext(t, f.calls); frame.Method != remote.MethodSubmit {
		t.Fatalf("the guest's send did not reach the server: %s", frame.Method)
	}
	_, err = w.call("agents.stop", p)
	if err == nil || err.Message != sentence {
		t.Fatalf("guest stop = %v, want %q", err, sentence)
	}
	_ = hostedTestNext(t, f.calls)
	out.waitFor(t, 5*time.Second, func(name string, data any) bool {
		return name == appwire.EventAskOpened && data.(appwire.AskOpened).Ask.Title == hostedTestQuestion
	})
	asks := hostedTestCall[appwire.AsksResult](t, w, "asks.list", map[string]string{"agent": got.ID}).Asks
	if len(asks) != 1 || asks[0].Title != hostedTestQuestion {
		t.Fatalf("guest asks = %+v", asks)
	}
	_, err = w.call("asks.answer", map[string]string{"agent": got.ID, "id": asks[0].ID, "text": "Continue."})
	if err == nil || err.Message != sentence {
		t.Fatalf("guest answer = %v, want %q", err, sentence)
	}
	if frame := hostedTestNext(t, f.calls); frame.Method != remote.MethodQuestionResolve {
		t.Fatalf("the guest's answer did not reach the server: %s", frame.Method)
	}
}

func TestAppHostedV6FailuresNeverRunLocally(t *testing.T) {
	for _, name := range []string{"unreachable", "signed out", "revoked", "forbidden", "refused", "attachments"} {
		t.Run(name, func(t *testing.T) {
			hostedTestHome(t)
			f := newHostedTestServer(t)
			token, host := f.bearer, orgHostName(f.server.URL)
			params := map[string]any{"org": hostedTestOrg, "text": "Run on the server."}
			code, words := appwire.CodeSignedOut, "Sign in to "+host+" first."
			switch name {
			case "unreachable":
				f.server.Close()
				code, words = appwire.CodeUnreachable, host+" is unreachable."
			case "signed out":
				token = ""
			case "revoked":
				f.status, f.refusal = http.StatusUnauthorized, "This sign-in has expired."
			case "forbidden":
				f.status = http.StatusForbidden
			case "refused":
				f.status = http.StatusConflict
				f.refusal = "This chat cannot start. " + f.bearer
				code, words = appwire.CodeConflict, "This chat cannot start."
			case "attachments":
				params["attachments"] = []map[string]string{{"kind": "file", "path": filepath.Join(t.TempDir(), "untouched")}}
				code, words = appwire.CodeBadParams, "Attachments cannot go to a chat on "+host+" yet."
			}
			w, _ := hostedTestBackend(t, f.server.URL, token)
			places := hostedTestPlaces(t)
			_, err := w.call("agents.create", params)
			if err == nil || err.Code != code || err.Message != words || strings.Contains(err.Message, f.bearer) {
				t.Fatalf("error = %v, want %s and %q", err, code, words)
			}
			if name == "attachments" && len(f.requests()) != 0 {
				t.Fatal("an attachment reached the organization before refusal")
			}
			local := hostedTestCall[appwire.AgentsResult](t, w, "agents.list", struct{}{})
			if len(local.Agents) != 0 || !reflect.DeepEqual(places, hostedTestPlaces(t)) {
				t.Fatal("a refused hosted chat changed local places")
			}
		})
	}
}

func TestAppHostedV8ListAvailabilityAndHostingPolicy(t *testing.T) {
	const refusal = "This chat list is unavailable to you."
	const policyWords = "  Your organization runs its chats on its own server.  "
	for _, tc := range []struct {
		name      string
		status    int
		refusal   string
		list      string
		available bool
		hosting   map[string]string
		errorCode string
	}{
		{name: "enabled with policy", available: true,
			list:    `{"sessions":[],"hosting":{"local_sessions":"refuse","words":"` + policyWords + `"}}`,
			hosting: map[string]string{"local_sessions": "refuse", "words": policyWords}},
		{name: "enabled with empty policy values", available: true,
			list:    `{"sessions":[],"hosting":{"local_sessions":"","words":""}}`,
			hosting: map[string]string{"local_sessions": "", "words": ""}},
		{name: "enabled without policy", available: true, list: `{"sessions":[]}`},
		{name: "disabled", status: http.StatusNotFound},
		{name: "written 404 refusal", status: http.StatusNotFound, refusal: refusal, errorCode: appwire.CodeConflict},
		{name: "unauthorized", status: http.StatusUnauthorized, errorCode: appwire.CodeSignedOut},
		{name: "forbidden", status: http.StatusForbidden, errorCode: appwire.CodeSignedOut},
		{name: "unreachable", errorCode: appwire.CodeUnreachable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hostedTestHome(t)
			f := newHostedTestServer(t)
			f.status, f.refusal, f.list = tc.status, tc.refusal, tc.list
			if tc.errorCode == appwire.CodeUnreachable {
				f.server.Close()
			}
			w, _ := hostedTestBackend(t, f.server.URL, f.bearer)
			raw, err := w.call("agents.hosted_list", map[string]string{"org": hostedTestOrg})
			if tc.errorCode != "" {
				if err == nil || err.Code != tc.errorCode || (tc.refusal != "" && err.Message != tc.refusal) {
					t.Fatalf("error = %v, want %s and any server refusal unchanged", err, tc.errorCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("list failed: %v", err)
			}
			var got struct {
				Agents    []appwire.Agent `json:"agents"`
				Available *bool           `json:"available"`
				Hosting   json.RawMessage `json:"hosting"`
			}
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if got.Agents == nil || len(got.Agents) != 0 || got.Available == nil || *got.Available != tc.available {
				t.Fatalf("list = %s, want empty agents and available = %t", raw, tc.available)
			}
			if tc.hosting == nil {
				if got.Hosting != nil {
					t.Fatalf("hosting = %s, want absent", got.Hosting)
				}
				return
			}
			var hosting map[string]string
			if err := json.Unmarshal(got.Hosting, &hosting); err != nil || !reflect.DeepEqual(hosting, tc.hosting) {
				t.Fatalf("hosting = %s, want %v (decode: %v)", got.Hosting, tc.hosting, err)
			}
		})
	}
}

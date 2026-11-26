//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/session"
)

const (
	orgAppSessions = "/v1/hosted/sessions"
	orgAppSession  = "hs_0123456789abcdef01234567"
	orgAppToken    = "org-app-door-stub-token"
	orgAppQuestion = "hello hosted"
	orgAppReply    = "ok"
	orgAppRefusal  = `{"error":"Sign in again."}`
	orgAppCallWait = 30 * time.Second
)

// TestOrgAppDoorE2E drives codeaf app over JSON lines so agents.entries must
// return the hosted engine's exact scripted answer "ok" after the app restarts.
func TestOrgAppDoorE2E(t *testing.T) {
	bin := binary(t)
	home, login := t.TempDir(), t.TempDir()
	t.Chdir(login)
	t.Setenv("CODEAF_HOME", home)
	// The durability brain answers a turn outside its probe script with "ok".
	brain := newBrain(t)
	server := newOrgAppServer(t, bin, brain.url())
	env := orgAppEnv(t, home, login, brain.url())
	start := func() *fleEngine {
		logDir := t.TempDir()
		app := startFLEEngine(t, bin, home, env, logDir)
		t.Cleanup(func() {
			if stderr, ok := app.cmd.Stderr.(*os.File); ok {
				_ = stderr.Close()
			}
			if t.Failed() {
				if stderr, err := os.ReadFile(filepath.Join(logDir, "engine.stderr")); err == nil && len(stderr) > 0 {
					t.Logf("codeaf app stderr:\n%s", stderr)
				}
			}
		})
		return app
	}
	app := start()
	defer func() {
		if app != nil {
			app.close()
		}
	}()

	connection := orgAppAttach(t, app, server)
	t.Logf("factory.attach returned connection %q with state %q", connection.ID, connection.State)
	places := orgAppPlaces(t)
	type agentResult struct{ Agent appwire.Agent }
	created := orgAppCall[agentResult](t, app, "agents.create", appwire.CreateAsk{
		Org: connection.ID, Text: orgAppQuestion, ClientID: "org-app-door-first-turn",
	}).Agent
	remoteID, org, ok := appwire.ParseHostedAgentID(created.ID)
	if !ok || remoteID != orgAppSession || org != connection.ID || created.Hosted == nil || created.Hosted.Org != connection.ID {
		t.Fatalf("agents.create returned id %q and hosted facts %+v, want hosted session %q on %q", created.ID, created.Hosted, orgAppSession, connection.ID)
	}
	orgAppWaitForTurn(t, app, created.ID)
	orgAppCheckHistory(t, app, created.ID)
	t.Logf("agents.entries returned answer %q after %q", orgAppReply, orgAppQuestion)

	listParams := map[string]string{"org": connection.ID}
	orgAppCheckLocal(t, app, places)
	hosted := orgAppCall[appwire.HostedListResult](t, app, "agents.hosted_list", listParams)
	if !hosted.Available || len(hosted.Agents) != 1 || hosted.Agents[0].ID != created.ID {
		t.Fatalf("agents.hosted_list = %+v, want available = true and hosted id %q", hosted, created.ID)
	}
	if facts := hosted.Agents[0].Hosted; facts == nil || !slices.Equal(facts.Verbs, acl.Verbs) {
		t.Fatalf("agents.hosted_list returned hosted facts %+v, want owner verbs %v", facts, acl.Verbs)
	}
	orgAppCheckRequests(t, server, 1)
	t.Logf("agents.hosted_list returned available = true and owner verbs %v, with one POST %s and unchanged local places", acl.Verbs, orgAppSessions)

	id := appwire.IDParams{ID: created.ID}
	orgAppCall[appwire.Empty](t, app, "agents.close", id)
	orgAppCheckRequests(t, server, 1)
	if opened := orgAppCall[agentResult](t, app, "agents.open", id).Agent; opened.ID != created.ID {
		t.Fatalf("agents.open on the same app returned id %q, want %q", opened.ID, created.ID)
	}
	orgAppCheckRequests(t, server, 2)
	orgAppCheckHistory(t, app, created.ID)
	app.close()
	app = nil
	app = start()
	if attached := orgAppAttach(t, app, server); attached.ID != connection.ID {
		t.Fatalf("factory.attach after app restart returned id %q, want %q", attached.ID, connection.ID)
	}
	if opened := orgAppCall[agentResult](t, app, "agents.open", id).Agent; opened.ID != created.ID {
		t.Fatalf("agents.open after app restart returned id %q, want %q", opened.ID, created.ID)
	}
	orgAppCheckHistory(t, app, created.ID)
	orgAppCheckRequests(t, server, 3)
	t.Logf("agents.entries returned answer %q on both reopens, with three wire upgrades, one create, and zero stops or deletes", orgAppReply)

	address, err := url.Parse(server.http.URL)
	if err != nil {
		t.Fatal(err)
	}
	server.close()
	unreachable := address.Hostname() + " is unreachable."
	orgAppCheckError(t, app, "agents.hosted_list", listParams, appwire.CodeUnreachable, unreachable)
	orgAppCheckError(t, app, "agents.create", listParams, appwire.CodeUnreachable, unreachable)
	orgAppCheckLocal(t, app, places)
	t.Logf("agents.hosted_list and agents.create returned %q / %q and left local places unchanged", appwire.CodeUnreachable, unreachable)

	revoked := newOrgAppServer(t, bin, brain.url())
	revokedConnection := orgAppAttach(t, app, revoked)
	revoked.mu.Lock()
	revoked.revoked = true
	revoked.mu.Unlock()
	signIn := "Sign in to " + address.Hostname() + " first."
	orgAppCheckError(t, app, "agents.hosted_list", map[string]string{"org": revokedConnection.ID}, appwire.CodeSignedOut, signIn)
	if got := revoked.count("GET " + orgAppSessions); got != 1 {
		t.Fatalf("revoked server saw %d GET %s requests, want 1", got, orgAppSessions)
	}
	t.Logf("agents.hosted_list returned %q / %q for HTTP 401 %s", appwire.CodeSignedOut, signIn, orgAppRefusal)
}

type orgAppServer struct {
	http       *httptest.Server
	cancelWire context.CancelFunc

	mu      sync.Mutex
	seen    []string
	created bool
	revoked bool
	wireMu  sync.Mutex
}

func newOrgAppServer(t *testing.T, bin, modelURL string) *orgAppServer {
	t.Helper()
	fake := &orgAppServer{}
	home, login, workspace := t.TempDir(), t.TempDir(), t.TempDir()
	env := append(orgAppEnv(t, home, login, modelURL), "OPENAI_API_KEY=stub", "CODEAF_MODEL="+durabilityModel)
	orgAppPinModels(t, home)
	wireCtx, cancel := context.WithCancel(context.Background())
	fake.cancelWire = cancel
	fake.http = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := r.Method + " " + r.URL.Path
		fake.mu.Lock()
		fake.seen = append(fake.seen, request)
		revoked, created := fake.revoked, fake.created
		fake.mu.Unlock()
		// The address a person pastes is probed before any sign-in exists, so the discovery answer is public.
		if request == "GET /v1/about" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"contract": 1, "name": "Hosted chat fixture"})
			return
		}
		authorized := r.Header.Get("Authorization") == "Bearer "+orgAppToken
		if !authorized || revoked {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, orgAppRefusal)
			if !authorized {
				t.Errorf("organization %s lacked the expected bearer and returned HTTP 401 %s", request, orgAppRefusal)
			}
			return
		}
		if request == "GET "+orgAppSessions+"/"+orgAppSession+"/wire" && created {
			// One pipe engine at a time can own the conversation in this home.
			fake.wireMu.Lock()
			defer fake.wireMu.Unlock()
			orgAppBridge(t, wireCtx, w, r, bin, home, workspace, env)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// The stand-in says what the organization's server says: its owner holds every verb.
		row := map[string]any{"id": orgAppSession, "state": "running", "verbs": acl.Verbs}
		var result any
		switch request {
		case "GET /v1/about":
			result = map[string]any{"contract": 1, "name": "Hosted chat fixture"}
		case "GET /v1/me":
			result = map[string]any{"handle": "owner", "mode": "team", "factory": map[string]string{"name": "Hosted chat fixture"}}
		case "GET " + orgAppSessions:
			sessions := []map[string]any{}
			if created {
				sessions = append(sessions, row)
			}
			result = map[string]any{"sessions": sessions, "shared": []any{}, "hosting": map[string]string{"local_sessions": "allow"}}
		case "POST " + orgAppSessions:
			fake.mu.Lock()
			fake.created = true
			fake.mu.Unlock()
			result = row
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	}))
	t.Cleanup(fake.close)
	return fake
}

func (f *orgAppServer) close() {
	f.cancelWire()
	f.http.Close()
	// A hijacked WebSocket is outside httptest's own shutdown wait.
	f.wireMu.Lock()
	f.wireMu.Unlock()
}

func (f *orgAppServer) count(request string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, got := range f.seen {
		if got == request {
			count++
		}
	}
	return count
}

func orgAppBridge(t *testing.T, parent context.Context, w http.ResponseWriter, r *http.Request, bin, home, workspace string, env []string) {
	t.Helper()
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		t.Errorf("accept hosted wire: %v", err)
		return
	}
	defer ws.CloseNow()
	ctx, cancel := context.WithTimeout(parent, 3*time.Minute)
	defer cancel()
	tunnel := websocket.NetConn(ctx, ws, websocket.MessageBinary)
	ws.SetReadLimit(4 << 20)
	var stderr bytes.Buffer
	cmd := guardedCommand(t, ctx, home, env, bin, "engine", "--no-host", "--workspace", workspace)
	cmd.Dir, cmd.Stdout, cmd.Stderr, cmd.WaitDelay = workspace, tunnel, &stderr, 5*time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Errorf("hosted engine stdin: %v", err)
		return
	}
	defer stdin.Close()
	if err := cmd.Start(); err != nil {
		t.Errorf("start hosted engine: %v", err)
		return
	}
	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		defer stdin.Close()
		_, _ = io.Copy(stdin, tunnel)
	}()
	err = cmd.Wait()
	_ = ws.CloseNow()
	<-inputDone
	if err != nil && ctx.Err() == nil {
		t.Logf("codeaf engine exited with %v\n%s", err, stderr.Bytes())
	}
}

func orgAppEnv(t *testing.T, home, login, modelURL string) []string {
	t.Helper()
	// The guarded child receives only these settings so the test uses the
	// loopback model and keeps profile, transfer, and update work isolated.
	env := []string{
		"HOME=" + login, "CODEAF_HOME=" + home, "TMPDIR=" + t.TempDir(),
		"CODEAF_TELEMETRY=off", "DO_NOT_TRACK=1", "CODEAF_NO_UPDATE_CHECK=1",
		"CODEAF_SYNC_URL=off", "CODEAF_MODEL_POOL=off", "CODEAF_BASE_URL=" + modelURL,
	}
	if furrow := os.Getenv("CODEAF_FURROW"); furrow != "" {
		env = append(env, "CODEAF_FURROW="+furrow)
	}
	return env
}

func orgAppPinModels(t *testing.T, home string) {
	t.Helper()
	rows := map[string]any{"model.plan": durabilityModel, config.KeyModelPool: "off", config.KeyDailyBudget: 0}
	for _, key := range []string{
		config.KeyChatModel, config.KeyTaskModel, config.KeyModelFallbacks,
		config.KeyTierWorkerModel, config.KeyTierLowModel, config.KeyTierHighModel,
		config.KeyTierReflexModel, config.KeyTierMastermindModel,
	} {
		rows[key] = durabilityModel
	}
	var pins []string
	for _, role := range textRoles {
		pins = append(pins, string(role)+":"+durabilityModel)
	}
	rows[config.KeyModelRoles] = strings.Join(pins, ",")
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func orgAppCall[T any](t *testing.T, app *fleEngine, method string, params any) T {
	t.Helper()
	raw, err := app.call(method, params, orgAppCallWait)
	if err != nil {
		t.Fatal(err)
	}
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode %s result %s: %v", method, raw, err)
	}
	return result
}

func orgAppAttach(t *testing.T, app *fleEngine, server *orgAppServer) appwire.FactoryConnection {
	t.Helper()
	result := orgAppCall[appwire.FactoryConnection](t, app, "factory.attach", appwire.FactoryAttachParams{URL: server.http.URL, Token: orgAppToken})
	if result.ID == "" || result.State != appwire.FactoryOK {
		t.Fatalf("factory.attach returned id %q and state %q, want a connection with state %q", result.ID, result.State, appwire.FactoryOK)
	}
	return result
}

func orgAppWaitForTurn(t *testing.T, app *fleEngine, id string) {
	t.Helper()
	deadline := time.NewTimer(orgAppCallWait)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		raw, err := os.ReadFile(app.events.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range bytes.Split(raw, []byte{'\n'}) {
			var event struct {
				Name string           `json:"event"`
				Data appwire.TurnDone `json:"data"`
			}
			if json.Unmarshal(line, &event) == nil && event.Name == appwire.EventTurnDone && event.Data.Agent == id {
				if !event.Data.OK {
					t.Fatalf("%s for %q failed: %s", event.Name, id, event.Data.Error)
				}
				return
			}
		}
		select {
		case <-tick.C:
		case <-app.dead:
			t.Fatalf("codeaf app exited while waiting for %s for %q", appwire.EventTurnDone, id)
		case <-deadline.C:
			t.Fatalf("no %s event for %q within %s", appwire.EventTurnDone, id, orgAppCallWait)
		}
	}
}

func orgAppCheckHistory(t *testing.T, app *fleEngine, id string) {
	t.Helper()
	page := orgAppCall[appwire.EntriesResult](t, app, "agents.entries", appwire.EntriesParams{ID: id})
	question, answer := false, false
	for _, entry := range page.Entries {
		question = question || (entry.Kind == appwire.EntryUser && entry.Text == orgAppQuestion)
		answer = answer || (entry.Kind == appwire.EntryAnswer && entry.Text == orgAppReply)
	}
	if !question || !answer {
		t.Fatalf("agents.entries = %+v, want user %q and answer %q", page.Entries, orgAppQuestion, orgAppReply)
	}
}

func orgAppPlaces(t *testing.T) []string {
	t.Helper()
	root := session.PlacesRoot()
	var names []string
	err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		if path == root && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err == nil && path != root {
			names = append(names, strings.TrimPrefix(path, root+string(filepath.Separator)))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func orgAppCheckLocal(t *testing.T, app *fleEngine, places []string) {
	t.Helper()
	local := orgAppCall[appwire.AgentsResult](t, app, "agents.list", appwire.AgentsListParams{State: "all"})
	if len(local.Agents) != 0 {
		t.Fatalf("agents.list = %+v, want an empty local list", local.Agents)
	}
	if got := orgAppPlaces(t); !slices.Equal(got, places) {
		t.Fatalf("local places entries = %q, want unchanged %q", got, places)
	}
}

func orgAppCheckRequests(t *testing.T, server *orgAppServer, wires int) {
	t.Helper()
	for request, want := range map[string]int{
		"POST " + orgAppSessions:                                 1,
		"GET " + orgAppSessions + "/" + orgAppSession + "/wire":  wires,
		"POST " + orgAppSessions + "/" + orgAppSession + "/stop": 0,
		"DELETE " + orgAppSessions + "/" + orgAppSession:         0,
	} {
		if got := server.count(request); got != want {
			t.Errorf("organization saw %d %s requests, want %d", got, request, want)
		}
	}
}

func orgAppCheckError(t *testing.T, app *fleEngine, method string, params any, code, message string) {
	t.Helper()
	result, err := app.call(method, params, orgAppCallWait)
	var wireErr *fleWireError
	if !errors.As(err, &wireErr) || wireErr.Code != code || wireErr.Message != message {
		t.Fatalf("%s returned result %s and error %v, want %q / %q", method, result, err, code, message)
	}
}

package main

// THE WHEN-TO-TAKE RULE, asked every way a launch can meet it.
//
// The rule decides which door a person's `codeaf chat` goes through. The host
// road is the ordinary one — it is what makes the work outlive the terminal —
// and the launches that keep the in-process door keep it because something real
// about them lives in this process: onboarding, --once, --debug, --no-host.

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/connect"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/modelsource/sourcestub"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// Contract 6.1: A plain launch reads a closed team's report from the engine profile on this machine.
func TestPlainLaunchReadsClosedTeamReportFromEngineProfile(t *testing.T) {
	engineProfile := t.TempDir()
	surfaceProfile := t.TempDir()
	t.Setenv("CODEAF_HOME", surfaceProfile)
	t.Setenv("CODEAF_PROFILE_DIR", surfaceProfile)
	t.Setenv("HOME", t.TempDir())
	t.Setenv(config.APIKeyEnv, "not-a-real-key")
	t.Cleanup(func() { stopPoolErrands(surfaceProfile) })
	const harbor = "0a0a0a0a0a0a"
	if err := teamstore.Save(engineProfile, []teamstore.Team{{ID: harbor, Name: "harbor", Manager: "hm",
		Members: []teamstore.Member{{Key: "hm", Handle: "boss"}}}}); err != nil {
		t.Fatal(err)
	}
	p, err := teamstore.Raise(engineProfile, teamstore.Packet{Team: teamstore.Person, Origin: harbor,
		Kind: teamstore.PacketClosing, RaisedBy: teamstore.FromManager, Question: "close harbor?",
		Options: []teamstore.Option{{ID: teamstore.OptionClose, Label: "Close", Consequence: "the team closes"}},
		Report:  &teamstore.ClosingReport{Done: "the parser"}})
	if err != nil {
		t.Fatal(err)
	}
	p, err = teamstore.Decide(engineProfile, p.ID, teamstore.Person, teamstore.OptionClose, "")
	if err != nil {
		t.Fatal(err)
	}
	if closed, err := teamstore.AcceptClosing(engineProfile, p); err != nil || !closed {
		t.Fatalf("close on report: %v, %v", closed, err)
	}
	welcome := remote.Welcome{Version: remote.Version, Workspace: "/srv/app", ProfileDir: engineProfile,
		Teams: true, Delegation: true, WrapUp: true}
	fleet := onePipeFleet("", hostedClient(t))
	t.Cleanup(fleet.closeAll)
	options, settings := hostOptions(fleet, welcome, false)
	if options.Teams.Load == nil {
		t.Fatal("the engine did not hand teams to the plain launch")
	}
	localDoors(&options, welcome, settings)
	if options.Teams.History == nil {
		t.Fatal("the plain launch has no history door")
	}
	got, err := options.Teams.History(harbor)
	if err != nil || len(got) != 1 || got[0].ID != p.ID || got[0].Report == nil || got[0].Report.Done != "the parser" {
		t.Fatalf("closed team's report from engine profile: %+v, %v", got, err)
	}
}

// consentSource is an OpenAI-shaped fake whose turn always asks for the same
// harmless bash command and then finishes after the tool result comes back.
func consentSource(t *testing.T, command string) *httptest.Server {
	t.Helper()
	var call int
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var envelope struct {
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
			Tools  []json.RawMessage `json:"tools"`
			Stream bool              `json:"stream"`
		}
		if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		toolTurn := len(envelope.Tools) > 0 && (len(envelope.Messages) == 0 || envelope.Messages[len(envelope.Messages)-1].Role != "tool")
		content := `"content":"done"`
		finish := "stop"
		if toolTurn {
			call++
			arguments, _ := json.Marshal(map[string]string{"command": command})
			content = `"tool_calls":[{"index":0,"id":"call-` + strconv.Itoa(call) + `","type":"function","function":{"name":"bash","arguments":` + strconv.Quote(string(arguments)) + `}}]`
			finish = "tool_calls"
		}
		body := `{"id":"consent-probe","choices":[{"index":0,"delta":{"role":"assistant",` + content + `},"finish_reason":"` + finish + `"}]}`
		if envelope.Stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = writer.Write([]byte("data: " + body + "\n\ndata: [DONE]\n\n"))
			return
		}
		_, _ = writer.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`))
	}))
}

func drainConsentRoad(t *testing.T, events <-chan session.Event, answer func(session.Event)) int {
	t.Helper()
	asked := 0
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event, open := <-events:
			if !open {
				return asked
			}
			if event.Kind == session.EventConsentRequest {
				asked++
				answer(event)
			}
		case <-deadline:
			t.Fatal("the consent probe did not finish")
			return asked
		}
	}
}

// shortStateRoot is a state root a unix socket can be named in.
//
// It is NOT t.TempDir, for internal/enginehost's own stated reason: Go names a
// temp directory after the test, this tree's test names are sentences, and a
// socket path has about a hundred bytes to spend. A test that ran out of them
// would be measuring the honest refusal a deep CODEAF_HOME gets rather than the
// rule.
func shortStateRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", "afl")
	if err != nil {
		t.Fatalf("make a state root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Setenv("CODEAF_HOME", root)
	return root
}

// answeringHost is a socket where this workspace's host would be, and nothing
// behind it. The rule asks one question — is anybody there — so a listener is
// the whole of what it takes to be somebody.
func answeringHost(t *testing.T, workspace string) {
	t.Helper()
	// [enginehost.Dir] and not SocketPath alone: naming the socket makes
	// nothing now, and a host makes the directory it listens in itself.
	if _, err := enginehost.Dir(workspace); err != nil {
		t.Fatalf("make somewhere for a host to live: %v", err)
	}
	socket, err := enginehost.SocketPath(workspace)
	if err != nil {
		t.Fatalf("name this workspace's socket: %v", err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("stand in for a host: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
}

// The ordinary launch takes the host road. That is the default, and it is what
// makes work outlive the terminal it was started in.
func TestAnOrdinaryLaunchTakesTheHostRoad(t *testing.T) {
	shortStateRoot(t)
	workspace := t.TempDir()
	if !v3TakeHostRoad(workspace, v3HostChoice{}) {
		t.Fatal("an ordinary launch opened its conversation in this process")
	}
	if v3TakeHostRoad("", v3HostChoice{}) {
		t.Fatal("a workspace that could not be resolved sent the launch down the host road")
	}
}

// The ordinary road borrows the remote surface builder, but its engine and
// surface are on this machine. Every door backed by this machine's profile or
// registry is therefore present, while the builder by itself keeps the far
// road's deliberate absences.
func TestAPlainLaunchKeepsThisMachinesDoorsWhileAHostLaunchDoesNot(t *testing.T) {
	engineProfile := t.TempDir()
	surfaceProfile := t.TempDir()
	t.Setenv("CODEAF_HOME", surfaceProfile)
	t.Setenv("CODEAF_PROFILE_DIR", surfaceProfile)
	t.Setenv("HOME", t.TempDir())
	t.Setenv(config.APIKeyEnv, "not-a-real-key")
	// hostOptions wires the pool's start-up errands on the terminal's profile;
	// the doors exit the process behind them, this test does not, so it closes
	// them through the seam the process's own close uses (poolindex.go) before
	// the profile is removed.
	t.Cleanup(func() { stopPoolErrands(surfaceProfile) })
	if err := config.WriteSources(engineProfile, []config.PersistedSource{{
		ID: "custom", Written: "engine-service", Address: "https://engine.example/v1", Key: "engine-key", Order: 1,
	}}); err != nil {
		t.Fatalf("seed the engine profile's service: %v", err)
	}
	credential := []byte(`{"stripe":{"auth":"key","key":"not-a-real-key"}}`)
	for _, dir := range []string{engineProfile, surfaceProfile} {
		if err := os.WriteFile(filepath.Join(dir, connect.StoreFileName), credential, 0o600); err != nil {
			t.Fatalf("seed credentials in %q: %v", dir, err)
		}
	}

	client := hostedClient(t)
	welcome := remote.Welcome{Version: remote.Version, Workspace: "/srv/app", ProfileDir: engineProfile}
	local, settings := hostOptions(onePipeFleet("", client), welcome, false)
	localDoors(&local, welcome, settings)

	if local.Connections == nil || local.Harnesses == nil || local.SaveApproval == nil ||
		local.SaveBashApproval == nil || local.SaveModel == nil || local.Sources.Empty() ||
		local.ApplyModelSources == nil || local.ConnectCodex == nil || local.ReadCredits == nil {
		t.Fatalf("the plain launch was handed incomplete local doors: %+v", local)
	}
	if local.ApplyApprovals != nil {
		t.Fatal("the local engine road claimed a take-back door it does not have")
	}
	if local.ProfileDir != engineProfile || settings.ProfileDir != surfaceProfile {
		t.Fatalf("surface writes to %q and local settings resolved %q, want engine %q and terminal %q", local.ProfileDir, settings.ProfileDir, engineProfile, surfaceProfile)
	}
	if _, ok := local.Sources.ByID("custom"); !ok {
		t.Fatalf("the surface read model services from its own profile instead of the engine's: %+v", local.Sources.All())
	}
	if err := local.SaveModel("vendor/remembered"); err != nil {
		t.Fatalf("save model through the local door: %v", err)
	}
	if got := config.ChatModelAt(engineProfile); got != "vendor/remembered" {
		t.Fatalf("the model was written through engine profile %q as %q", engineProfile, got)
	}
	if err := local.SaveApproval("read"); err != nil {
		t.Fatalf("save a tool approval through the local door: %v", err)
	}
	if got := config.ToolApprovalsAt(engineProfile); got != "read:allow" {
		t.Fatalf("the tool approval was written through engine profile %q as %q", engineProfile, got)
	}
	if err := local.SaveBashApproval("git status --short"); err != nil {
		t.Fatalf("save a command approval through the local door: %v", err)
	}
	if got := config.BashApprovalsAt(engineProfile); got != "allow git status --short" {
		t.Fatalf("the command approval was written through engine profile %q as %q", engineProfile, got)
	}
	if config.ChatModelAt(surfaceProfile) != "" || config.ToolApprovalsAt(surfaceProfile) != "" || config.BashApprovalsAt(surfaceProfile) != "" {
		t.Fatal("a local write landed in the terminal's profile instead of the engine's")
	}
	if err := local.Connections.Disconnect("stripe"); err != nil {
		t.Fatalf("disconnect through the local account door: %v", err)
	}
	for dir, wantStripe := range map[string]bool{engineProfile: false, surfaceProfile: true} {
		raw, err := os.ReadFile(filepath.Join(dir, connect.StoreFileName))
		if err != nil {
			t.Fatalf("read credentials in %q: %v", dir, err)
		}
		var entries map[string]json.RawMessage
		if err := json.Unmarshal(raw, &entries); err != nil {
			t.Fatalf("decode credentials in %q: %v", dir, err)
		}
		if _, gotStripe := entries["stripe"]; gotStripe != wantStripe {
			t.Fatalf("credentials in %q have stripe=%t, want %t", dir, gotStripe, wantStripe)
		}
	}

	hosted, _ := hostOptions(onePipeFleet("devbox", client), welcome, false)
	if hosted.Connections != nil || hosted.Harnesses != nil || hosted.SaveApproval != nil ||
		hosted.SaveBashApproval != nil || hosted.SaveModel != nil || !hosted.Sources.Empty() ||
		hosted.ApplyModelSources != nil || hosted.ConnectCodex != nil || hosted.ReadCredits != nil || hosted.ImplicitTalk {
		t.Fatalf("the --host builder grew this machine's doors: %+v", hosted)
	}
}

// A DAMAGED ACCOUNT STORE TAKES AWAY ONLY THE ACCOUNTS. Model services live in
// the profile's config.json, so their group still draws and the connection
// panel has a useful, non-panicking shape.
func TestADamagedCredentialsStoreLeavesAccountsAbsentAndModelsPresent(t *testing.T) {
	profileDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(profileDir, connect.StoreFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write damaged credentials: %v", err)
	}
	sources := modelsource.NewSet(modelsource.Connected{
		Source: modelsource.DefaultSource(config.DefaultBaseURL), Key: "default-key", Address: config.DefaultBaseURL,
	})
	options := tui3.Options{}
	localDoors(&options, remote.Welcome{ProfileDir: profileDir}, config.Config{ProfileDir: profileDir, Sources: sources})
	if options.Connections != nil {
		t.Fatal("a damaged credentials.json was presented as an accounts store")
	}
	if options.Sources.Empty() {
		t.Fatal("a damaged credentials.json took the models group away with the accounts")
	}
}

// A service connected by the local surface is picked up by the real engine on
// the model-set call already on the wire. The next turn must therefore reach
// the new address with the new key, not the client the session opened on.
func TestAConnectedModelServiceIsLiveInTheRunningEngineConversation(t *testing.T) {
	defaultServer := sourcestub.New("openai/gpt-4.1-mini")
	defer defaultServer.Close()
	directServer := sourcestub.New("direct-chat")
	defer directServer.Close()

	profileDir := t.TempDir()
	defaultSource := modelsource.DefaultSource(defaultServer.URL())
	initial := modelsource.NewSet(modelsource.Connected{
		Source: defaultSource, Key: "default-key", Address: defaultServer.URL(),
	})
	agent, err := session.New(session.Config{
		Workspace: t.TempDir(), Model: "openai/gpt-4.1-mini", Sources: initial,
	})
	if err != nil {
		t.Fatalf("open the real session agent: %v", err)
	}
	proc := &v3Process{
		Settings:   config.Config{APIKey: "default-key", BaseURL: defaultServer.URL(), Sources: initial},
		ProfileDir: profileDir,
	}
	proc.track(agent)
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{
		Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{
				Agent: agent, Workspace: t.TempDir(),
				RefreshModelSources: proc.refreshModelSources,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("open the real engine road: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	options := tui3.Options{Agent: loop.Client.Agent(), ProfileDir: profileDir}
	localDoors(&options, remote.Welcome{ProfileDir: profileDir}, config.Config{ProfileDir: profileDir, Sources: initial})
	var custom modelsource.Source
	for _, source := range modelsource.Vendored() {
		if source.ID == "custom" {
			custom = source
			break
		}
	}
	if custom.ID == "" {
		t.Fatal("the model-service catalog has no Custom OpenAI-compatible API door")
	}
	outcome, err := config.ConnectService(context.Background(), profileDir, config.PersistedSource{
		ID: custom.ID, Written: "localhost", Address: directServer.URL(), Key: "direct-key", Order: 1,
	}, custom, nil)
	if err != nil || outcome.Kind != modelsource.OutcomeConnected {
		t.Fatalf("connect the direct service: outcome=%+v err=%v", outcome, err)
	}
	connected := config.ResolveSources(profileDir, "default-key", defaultServer.URL())
	options.ApplyModelSources(connected)
	options.Agent.SetModel("localhost/direct-chat")
	events, err := options.Agent.Submit(context.Background(), "use the connected service")
	if err != nil {
		t.Fatalf("submit through the engine road: %v", err)
	}
	for range events {
	}

	var completions []sourcestub.Request
	for _, request := range directServer.Requests() {
		if request.Method == "POST" {
			completions = append(completions, request)
		}
	}
	if len(completions) != 1 {
		t.Fatalf("direct service received %d completion requests: %+v", len(completions), directServer.Requests())
	}
	var body struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(completions[0].Body, &body); err != nil {
		t.Fatalf("decode direct request: %v", err)
	}
	if completions[0].Bearer != "Bearer direct-key" || completions[0].Host == "" || body.Model != "direct-chat" {
		t.Fatalf("direct request used bearer %q host %q model %q", completions[0].Bearer, completions[0].Host, body.Model)
	}
	for _, request := range defaultServer.Requests() {
		if request.Method == "POST" {
			t.Fatalf("the session's old service received the switched turn: %+v", request)
		}
	}
}

// A RULE BANKED BY THE LOCAL SURFACE REACHES THE RUNNING ENGINE'S GATE before
// the answer releases the call. The next matching command therefore runs
// without asking the person a second time.
func TestABankedBashRuleReachesTheRunningEngineBeforeTheAnswer(t *testing.T) {
	profileDir := v3Profile(t, map[string]any{"tools.approvalMode": "prompt"})
	workspace := t.TempDir()
	command := "printf banked-rule-probe"
	server := consentSource(t, command)
	defer server.Close()
	sources := modelsource.NewSet(modelsource.Connected{
		Source: modelsource.DefaultSource(server.URL + "/v1"), Key: "test-key", Address: server.URL + "/v1",
	})
	policy, err := v3Policy(workspace, profileDir, false)
	if err != nil {
		t.Fatalf("build the initial approval gate: %v", err)
	}
	agent, err := session.New(session.Config{
		Workspace: workspace, Model: "openai/gpt-4.1-mini", Sources: sources,
		System: "SYSTEM", AskConsent: true, ApprovalPolicy: policy,
	})
	if err != nil {
		t.Fatalf("open the real session agent: %v", err)
	}
	defer agent.Close()
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{
		Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{
				Agent: agent, Workspace: workspace,
				RefreshApprovals: func() { refreshV3Policy(agent, workspace, profileDir, false) },
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("open the real consent road: %v", err)
	}
	defer loop.Close()
	options := tui3.Options{Agent: loop.Client.Agent(), ProfileDir: profileDir}
	localDoors(&options, remote.Welcome{ProfileDir: profileDir}, config.Config{ProfileDir: profileDir, Sources: sources})

	first, err := options.Agent.Submit(context.Background(), "run the probe")
	if err != nil {
		t.Fatalf("submit the first probe: %v", err)
	}
	asked := drainConsentRoad(t, first, func(event session.Event) {
		if err := options.SaveBashApproval(command); err != nil {
			t.Fatalf("bank the command: %v", err)
		}
		options.Agent.ResolveConsentRemember(event.ID, true, session.ConsentRule)
	})
	second, err := options.Agent.Submit(context.Background(), "run the probe again")
	if err != nil {
		t.Fatalf("submit the second probe: %v", err)
	}
	asked += drainConsentRoad(t, second, func(event session.Event) {
		options.Agent.ResolveConsent(event.ID, true)
	})
	if asked != 1 {
		t.Fatalf("the person was asked %d times, want once", asked)
	}
}

// A machine that is not set up yet stays here. Connecting a provider is a
// conversation with the person at this terminal (internal/tui3's firstrun.go) and
// a host has no terminal to have it in, so a launch that might need onboarding
// must not daemonize before there is a key.
func TestAnUnconfiguredMachineKeepsTheInProcessDoor(t *testing.T) {
	shortStateRoot(t)
	workspace := t.TempDir()
	if v3TakeHostRoad(workspace, v3HostChoice{setup: true}) {
		t.Fatal("a launch that may still have to set this machine up spawned a host")
	}
}

// And that answer is read off the machine rather than assumed: with no key
// anywhere the setup gate is on, and with one it is off.
func TestTheSetupGateReadsWhetherThisMachineCanTalkToAModel(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	if v3MachineIsSetUp() {
		t.Fatal("a machine with no key anywhere said it was set up")
	}
	t.Setenv("OPENROUTER_API_KEY", "sk-test-not-a-real-key")
	if !v3MachineIsSetUp() {
		t.Fatal("a machine with a key said it still had to be set up")
	}
}

// --debug records the model calls THIS PROCESS makes; over a socket the calls
// are the host's, and it was never told to record. So the flag keeps the launch
// where it is real rather than writing nothing.
func TestDebugKeepsTheInProcessDoor(t *testing.T) {
	shortStateRoot(t)
	workspace := t.TempDir()
	if v3TakeHostRoad(workspace, v3HostChoice{debug: true}) {
		t.Fatal("--debug went down a road where the record would have been empty")
	}
}

// --once never starts a host — a resident process left behind by a headless
// command is a surprise — but it joins one that is there, so a scripted message
// lands in the conversation a person is actually in.
func TestOnceRetriesAHostInTheStaleSocketWindow(t *testing.T) {
	shortStateRoot(t)
	workspace := t.TempDir()
	if _, err := enginehost.Dir(workspace); err != nil {
		t.Fatal(err)
	}
	socket, err := enginehost.SocketPath(workspace)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	unixStale, ok := stale.(*net.UnixListener)
	if !ok {
		t.Fatal("unix listener had an unexpected type")
	}
	unixStale.SetUnlinkOnClose(false)
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}

	// A real host holds its lock across the whole remove-to-listen window, and the
	// retry is gated on that lock, so the test holds it as the host would.
	lockPath, err := enginehost.LockPath(workspace)
	if err != nil {
		t.Fatal(err)
	}
	hostLock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer hostLock.Close()
	if err := filelock.Lock(hostLock, true, true); err != nil {
		t.Fatal(err)
	}

	accepted := make(chan error, 1)
	go func() {
		time.Sleep(50 * time.Millisecond)
		if err := os.Remove(socket); err != nil {
			accepted <- err
			return
		}
		listener, err := net.Listen("unix", socket)
		if err != nil {
			accepted <- err
			return
		}
		defer listener.Close()
		if unix, ok := listener.(*net.UnixListener); ok {
			_ = unix.SetDeadline(time.Now().Add(time.Second))
		}
		conn, err := listener.Accept()
		if err == nil {
			err = conn.Close()
		}
		accepted <- err
	}()

	if !v3HostAnswers(workspace) {
		t.Fatal("a headless message missed a host replacing a stale socket")
	}
	if err := <-accepted; err != nil {
		t.Fatal(err)
	}
}

func TestOnceJoinsAHostButNeverStartsOne(t *testing.T) {
	root := shortStateRoot(t)
	workspace := t.TempDir()
	if v3TakeHostRoad(workspace, v3HostChoice{once: true}) {
		t.Fatal("a headless message started a session host")
	}
	// And the question left nothing on disk where a host would live.
	if entries, err := os.ReadDir(filepath.Join(root, "v3", "hosts")); err == nil && len(entries) > 0 {
		t.Fatalf("asking whether a host is there made %d directories", len(entries))
	}
	answeringHost(t, workspace)
	if !v3TakeHostRoad(workspace, v3HostChoice{once: true}) {
		t.Fatal("a host was answering and the headless message opened its own conversation")
	}
}

// --no-host is the escape hatch and it beats everything: it is what a person
// types on the day the host itself is the thing that is wrong.
func TestNoHostKeepsTheInProcessDoor(t *testing.T) {
	shortStateRoot(t)
	workspace := t.TempDir()
	answeringHost(t, workspace)
	if v3TakeHostRoad(workspace, v3HostChoice{noHost: true}) {
		t.Fatal("--no-host still went looking for a session host")
	}
}

// The per-launch posture travels, and a launch that asked for nothing sends
// nothing: nil is the engine's own defaults, which is what every remote surface
// sends and what the engine reads.
func TestTheLaunchShapeIsCarriedOnlyWhenSomethingWasAskedFor(t *testing.T) {
	if shape := v3LaunchShape(false, false, false, 0, 0, false); shape != nil {
		t.Fatalf("a bare launch carried a shape: %+v", shape)
	}
	if bare := v3LaunchShape(false, false, false, 0, 0, true); bare == nil || !bare.Interactive {
		t.Fatal("bare interactive launch lost its mode")
	}
	shape := v3LaunchShape(true, false, true, 0, 12.5, true)
	if shape == nil || !shape.Yolo || !shape.OneModel || !shape.Interactive || shape.MaxCost != 12.5 || shape.NoCompact {
		t.Fatalf("the shape carried %+v", shape)
	}
	if shape.Same(nil) {
		t.Fatal("a shape with flags in it read as the engine's defaults")
	}
	// A --once probe is the one dial that is not steered, and the shape is
	// where the engine learns it: Same would otherwise read a probe and a
	// surface as one conversation.
	probe := v3LaunchShape(true, false, true, 0, 12.5, false)
	if probe.Same(shape) {
		t.Fatal("a --once probe and a steered surface read as the same posture")
	}
	if said := launchShapeWords(shape); !strings.Contains(said, "interactive chat") {
		t.Fatalf("an interactive shape is not named in the words a person reads: %q", said)
	}
}

// A conversation already open keeps the shape it was opened with, and the
// sentence a person reads names both postures: the one running and the one they
// asked for. Neither half alone is actionable.
func TestTheShapeRefusalNamesWhatIsRunningAndWhatWasAsked(t *testing.T) {
	said := hostShapeSentence(v3LaunchShape(true, false, false, 0, 0, true), nil)
	for _, want := range []string{"--yolo", "the default posture", "ends when this terminal does"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the sentence %q does not say %q", said, want)
		}
	}
}

// A FALLBACK NOTICE SPEAKS TO THE PERSON, NOT ABOUT THE HOST MACHINERY. The
// path refusal names its limit and its way out, a host that never appeared says
// what happened plainly, and a sentence already written for a person is not
// rewritten on the way to the screen.
func TestALongStatePathIsSaidInWordsAndNotInTheEnginesOwn(t *testing.T) {
	tooLong := hostFallbackReason(enginehost.ErrSocketPathTooLong)
	if !strings.Contains(tooLong, "CODEAF_HOME moves it somewhere shorter") {
		t.Fatalf("the path refusal offers no way out: %q", tooLong)
	}
	if !strings.Contains(tooLong, strconv.Itoa(enginehost.SocketLimit)) {
		t.Fatalf("the path refusal does not name the %d-byte limit: %q", enginehost.SocketLimit, tooLong)
	}
	noHost := hostFallbackReason(enginehost.ErrNoHostAnswered)
	for name, said := range map[string]string{"too-long refusal": tooLong, "no-host refusal": noHost} {
		if strings.Contains(said, "engine host") || strings.Contains(said, "no host answered") {
			t.Fatalf("the %s speaks in machinery words: %q", name, said)
		}
	}
	written := busyEngineHostSentence(true)
	if got := hostFallbackReason(errors.New(written)); got != written {
		t.Fatalf("a sentence already written for a person became %q, want %q", got, written)
	}
}

// --no-host is about the session host on THIS machine, so naming it beside a
// machine is two different statements about where the conversation is. A flag
// that could not honour itself is a refusal in this tree and never a shrug.
func TestNoHostAndAFarMachineAreRefusedRatherThanIgnored(t *testing.T) {
	for _, flags := range [][]string{
		{"--no-host", "--host", "devbox"},
		{"--no-host", "--at", "otter-lamp-42"},
	} {
		err := openChatV3("chat", flags, false)
		if err == nil {
			t.Fatalf("%v opened a conversation", flags)
		}
		if !strings.Contains(err.Error(), "--no-host") {
			t.Fatalf("%v was refused with %q, which does not name the flag", flags, err)
		}
	}
}

// AND THE USAGE LINE OFFERS IT, because a flag a person cannot discover is a
// flag they cannot reach on the day the host is the thing that is wrong.
func TestChatAndResumeBothOfferNoHostInTheirUsage(t *testing.T) {
	for _, name := range []string{"chat", "resume"} {
		err := openChatV3(name, []string{"a-word-no-flag-takes"}, name == "resume")
		if err == nil || !strings.Contains(err.Error(), "--no-host") {
			t.Fatalf("codeaf %s's usage line reads %v", name, err)
		}
	}
}

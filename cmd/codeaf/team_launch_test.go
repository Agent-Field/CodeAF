package main

import (
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
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// A CONVERSATION MADE A TEAM'S MANAGER ON THE ORDINARY LAUNCH KNOWS IT IS ONE.
//
// Every team test in internal/session built its agent with Config.ProfileDir
// set to a temporary directory, and every one of them passed while the feature
// was dead for everybody: the ordinary launch exports no CODEAF_PROFILE_DIR, so
// the engine daemon that builds the session hands it an empty ProfileDir, and
// the session read the empty string as "no profile, no team". The manager's
// transcript on the machine where it was found carried no team note, no team
// verb and no cursor file, and asked what was happening it described the home
// folder.
//
// So this test goes through the door the product goes through, with nothing
// set that a person does not set: an empty CODEAF_PROFILE_DIR, the engine's own
// [bootEngine] building the conversation, a model endpoint that only records
// what it is sent, and a teams.json where the ordinary launch keeps it (the
// state root, which this test moves to a directory of its own so the person's
// real teams are never read). It asserts on the wire: the first request of the
// first turn carries the manager's verbs and the manager's role, stated as
// codeaf's instruction rather than as a fact beside the work.
func TestTheOrdinaryLaunchTellsAManagerItIsOne(t *testing.T) {
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	model := newRecordingModel(t)
	t.Setenv("CODEAF_BASE_URL", model.server.URL)
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workspace)
	freshEngineProcess(t)

	engine, err := bootEngine(remote.Hello{Version: remote.Version, Workspace: workspace}, "", "")
	if err != nil {
		t.Fatalf("the engine door did not open: %v", err)
	}
	defer func() { _ = engine.Agent.Close() }()
	transcript := strings.TrimSpace(engine.SessionFile)
	if transcript == "" {
		t.Fatal("the engine named no transcript, so there is no key to make a manager of")
	}

	// THE TEAM, WRITTEN WHERE THE INTERFACE WRITES IT ON THE ORDINARY LAUNCH:
	// an empty profile directory, which internal/teams resolves to the state
	// root this test chose.
	teamID := teams.NewID()
	manager := filepath.Clean(transcript)
	web := filepath.Join(root, "elsewhere", "transcript.jsonl")
	err = teams.Update("", func(file *teams.File) error {
		file.Teams = append(file.Teams, teams.Team{ID: teamID, Name: "harbor"})
		for _, member := range []teams.Member{
			{Key: manager, File: manager, Word: "harbor manager"},
			{Key: web, File: web, Word: "web frontend", Handle: "web"},
		} {
			if err := file.AddMember(teamID, member); err != nil {
				return err
			}
		}
		return file.SetManager(teamID, manager)
	})
	if err != nil {
		t.Fatalf("make the team: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	events, err := engine.Agent.Submit(ctx, "can you tell me what's happening here?")
	if err != nil {
		t.Fatalf("the turn did not start: %v", err)
	}
	for range events {
	}

	first, ok := model.firstTurnRequest()
	if !ok {
		t.Fatal("the model was never asked anything with a belt, so there is no turn to read")
	}
	for _, verb := range []string{"team_status", "team_read", "team_send", "team_stop", "team_start"} {
		if !first.carriesTool(verb) {
			t.Errorf("the manager's first request carried no %s", verb)
		}
	}
	role := first.messageContaining(`manager of the team "harbor"`)
	if role == "" {
		t.Fatalf("the manager's first request never told it it manages harbor:\n%s", first.allText())
	}
	if !strings.Contains(role, "@web") {
		t.Errorf("the manager's role does not name its member @web:\n%s", role)
	}
	if !strings.HasPrefix(role, "Instructions from codeaf") || strings.Contains(role, "Facts, not requests") {
		t.Errorf("the manager's role rode as a fact note, not as codeaf's instruction:\n%s", role)
	}
}

// resolvedTempDir is t.TempDir with its symlinks resolved, so the path a
// conversation keys itself by and the path this test writes into teams.json
// are one spelling on a Mac, where /var is /private/var.
func resolvedTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// freshEngineProcess empties the engine's once-per-process memo for this test
// and again after it, because the memo holds the process of whichever test
// booted an engine first, with that test's model endpoint and profile.
func freshEngineProcess(t *testing.T) {
	t.Helper()
	reset := func() {
		closeEngineProcess()
		engineProcess.once = sync.Once{}
		engineProcess.proc = nil
		engineProcess.err = nil
	}
	reset()
	t.Cleanup(reset)
}

// recordingModel is an OpenAI-shaped endpoint that answers every completion
// with "done" and keeps every request it was sent.
type recordingModel struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []recordedRequest
}

type recordedRequest struct {
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
	Tools []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
	Stream bool `json:"stream"`
}

func newRecordingModel(t *testing.T) *recordingModel {
	t.Helper()
	model := &recordingModel{}
	model.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasSuffix(request.URL.Path, "/chat/completions") {
			http.Error(writer, "no catalog for a test", http.StatusServiceUnavailable)
			return
		}
		raw, _ := io.ReadAll(request.Body)
		var envelope recordedRequest
		if err := json.Unmarshal(raw, &envelope); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		model.record(envelope)
		if envelope.Stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = writer.Write([]byte(`data: {"id":"team-probe","choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"))
			return
		}
		_, _ = writer.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(model.server.Close)
	return model
}

func (m *recordingModel) record(request recordedRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, request)
}

// firstTurnRequest is the first request sent with a belt: a turn's, and not a
// title's or a namer's, which carry no tools.
func (m *recordingModel) firstTurnRequest() (recordedRequest, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, request := range m.requests {
		if len(request.Tools) > 0 {
			return request, true
		}
	}
	return recordedRequest{}, false
}

func (r recordedRequest) carriesTool(name string) bool {
	for _, tool := range r.Tools {
		if tool.Function.Name == name {
			return true
		}
	}
	return false
}

// texts is every message's text, whether its content is a string or parts.
func (r recordedRequest) texts() []string {
	var out []string
	for _, message := range r.Messages {
		var plain string
		if json.Unmarshal(message.Content, &plain) == nil {
			out = append(out, plain)
			continue
		}
		var parts []struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(message.Content, &parts) == nil {
			var joined []string
			for _, part := range parts {
				joined = append(joined, part.Text)
			}
			out = append(out, strings.Join(joined, ""))
		}
	}
	return out
}

func (r recordedRequest) messageContaining(want string) string {
	for _, text := range r.texts() {
		if strings.Contains(text, want) {
			return text
		}
	}
	return ""
}

func (r recordedRequest) allText() string {
	var b strings.Builder
	for _, text := range r.texts() {
		b.WriteString("--- message\n")
		if len(text) > 600 {
			text = text[:600] + "…"
		}
		b.WriteString(text)
		b.WriteString("\n")
	}
	return b.String()
}

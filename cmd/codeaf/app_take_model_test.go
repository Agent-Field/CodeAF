package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/config"
)

// E10 T1/T2/T3: moving a chat and setting up its model are separate actions.
// Both computers use real app commands; only the provider is scripted.
func TestAppTakeModelT1T2T3NoUsableDefault(t *testing.T) {
	celltest.RequireEngine(t)
	_, workspace := wakeEnvironment(t, e2TempDir(t))
	model := appTwoMessageSource(t, "echo model-ready")
	t.Cleanup(model.Close)
	var calls atomic.Int64
	var usedDefault atomic.Bool
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			calls.Add(1)
			raw, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(raw))
			var request struct{ Model string }
			_ = json.Unmarshal(raw, &request)
			if request.Model == "fixture/default" {
				usedDefault.Store(true)
			}
			if strings.HasPrefix(request.Model, "ollama/") {
				http.Error(w, "unavailable model reached the router", http.StatusBadRequest)
				return
			}
		}
		model.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(endpoint.Close)
	t.Setenv("CODEAF_BASE_URL", endpoint.URL)
	relay := e2WireRelay(t)
	dirA, dirB := outageHomes(t, relay.URL)
	if err := config.WriteSources(dirA, []config.PersistedSource{{ID: "ollama", Written: "ollama", Address: endpoint.URL}}); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{dirA, dirB} {
		if err := config.WriteChatModel(dir, "ollama/llama3.2:latest"); err != nil {
			t.Fatal(err)
		}
	}
	a, b := e2StartApp(t, dirA), e2StartApp(t, dirB)
	created := machineCall[struct {
		Agent appwire.Agent `json:"agent"`
	}](t, a.w, "agents.create", map[string]any{"workspace": workspace, "model": "ollama/llama3.2:latest"}).Agent
	e2Turn(t, a, created.ID, "from A")
	e2Quit(t, a)
	row := outageChatRow(t, b, "", func(r appwire.ChatRow) bool { return r.Title != "" && r.CanTake })
	taken := machineCall[appwire.TakeResult](t, b.w, "agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen}).Agent
	if taken.Location == nil || !taken.Location.Here {
		t.Fatalf("T1 take did not complete here: %+v", taken)
	}
	b.wait(t, func(f e2Frame) bool {
		var changed appwire.ChatChanged
		return f.Event == appwire.EventChatChanged && json.Unmarshal(f.Data, &changed) == nil && changed.Chat.Cell == row.Cell && changed.Chat.Here && changed.Chat.LocalAgent == taken.ID
	})
	local := outageChatRow(t, b, row.Cell, func(r appwire.ChatRow) bool { return r.Here && r.LocalAgent == taken.ID })
	if local.Seen.Device != taken.Location.Holder.ID || local.Seen.Fence != taken.Location.Seen.Fence {
		t.Fatalf("T2 row/agent disagree: %+v / %+v", local, taken.Location)
	}
	before := calls.Load()
	turn := machineCall[struct {
		Turn uint64 `json:"turn"`
	}](t, b.w, "agents.send", map[string]any{"id": taken.ID, "text": "before setup", "client_id": "before-setup"}).Turn
	const reason = "This chat used llama3.2 (Ollama), which isn't set up on this computer. Set up a model in Settings, then send again."
	b.wait(t, func(f e2Frame) bool {
		var done appwire.TurnDone
		if f.Event != appwire.EventTurnDone || json.Unmarshal(f.Data, &done) != nil || done.Turn != turn {
			return false
		}
		if done.OK || done.Error != reason || done.ErrorKind == "provider" {
			t.Fatalf("T1/T3 first send: %s", f.Data)
		}
		return true
	})
	if calls.Load() != before {
		t.Fatal("T1 unavailable model reached a provider")
	}
	// The ordinary profile-model door is what Settings uses for this choice.
	machineCall[any](t, b.w, "settings.set", map[string]any{"default_model": "fixture/default"})
	e2Turn(t, b, taken.ID, "after setup")
	e2Turn(t, b, taken.ID, "second ready turn")
	if !usedDefault.Load() {
		t.Fatal("T1 the next send did not use B's usable default")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	failures, notices := 0, 0
	for _, f := range b.events {
		var added appwire.EntryAdded
		if f.Event != appwire.EventEntryAdded || json.Unmarshal(f.Data, &added) != nil || added.Entry.Agent != taken.ID {
			continue
		}
		if added.Entry.Text == reason {
			failures++
		}
		if strings.Contains(added.Entry.Text, "isn't set up here") && strings.Contains(added.Entry.Text, "fixture/default") {
			notices++
		}
	}
	if failures != 1 || notices != 1 {
		t.Fatalf("T1/T3 model reasons = %d, recovery notices = %d; want one each", failures, notices)
	}
}

func TestAppTakeT3ActionableMessageAndTechnicalDetail(t *testing.T) {
	missing := &continuedModelUnavailable{model: "ollama/llama3.2:latest", provider: "ollama"}
	for _, cause := range []error{
		fmt.Errorf("engine: %w", missing),
		&appwire.Error{Code: appwire.CodeEngine, Message: missing.Error(), Detail: "engine: engine: model ollama/llama3.2:latest: no usable default"},
		&appwire.Error{Code: appwire.CodeEngine, Message: "engine: engine: " + missing.Error(), Detail: "engine: model ollama/llama3.2:latest: no usable default"},
	} {
		var wire *appwire.Error
		if !errors.As(appTakeError(cause), &wire) || wire.Message != missing.Error() {
			t.Fatalf("T3 actionable message lost: %+v", wire)
		}
		detail, _ := wire.Detail.(string)
		if !strings.Contains(detail, "ollama/llama3.2:latest") || strings.Contains(detail, "engine:") {
			t.Fatalf("T3 technical detail lost or prefixed: %q", detail)
		}
	}
}

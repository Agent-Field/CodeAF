package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// A HEADLESS SHAPING TURN APPLIES THE MANAGER'S EDIT (the owner's run of
// 2026-10-09, where every `factory_run` a shaping turn made was refused with
// `needs approval but no resolver is attached: default` and the recipe stood
// by accident). The door is the real one, over a real store: the manager
// conversation is made by the Talk door's maker, the turn is opened headless
// from the launch's own config ([openManagerTurn]) because no window holds it,
// the model endpoint is scripted to call `factory_run` once, and the edit is
// on the item's steps afterwards, marked the manager's.
func TestAHeadlessShapingTurnAppliesTheManagersEdit(t *testing.T) {
	model := newShapingModel(t)
	t.Setenv("CODEAF_BASE_URL", model.server.URL)
	proc := v3TestProcess(t)
	workspace := resolvedTempDir(t)
	launch, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: workspace})
	if err != nil {
		t.Fatalf("the launch did not open: %v", err)
	}
	profileDir := os.Getenv("CODEAF_PROFILE_DIR")

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{Repo: "acme/web", Kind: factory.KindIssue, Title: "fix the ledger double count",
		State: factory.StateNew, Stages: factory.CopyStages(factory.DefaultRecipe().For(factory.KindIssue))})
	if err != nil {
		t.Fatal(err)
	}
	turns := factoryShapeTurns(st, workspace, profileDir, launch.Config)
	shape := factoryShapeDoor(st, workspace, profileDir, turns)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	line, err := shape(ctx, it.ID)
	if err != nil {
		t.Fatalf("the shaping turn failed: %q, %v\nthe model saw:\n%s", line, err, model.transcript())
	}
	if !strings.HasPrefix(line, "manager set review: read it for security") {
		t.Fatalf("line = %q\nthe model saw:\n%s", line, model.transcript())
	}
	got, err := st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	at := factory.StageIndex(got.Stages, "review")
	if at < 0 || got.Stages[at].Ask != "read it for security" || got.Stages[at].By != factory.ByManager {
		t.Fatalf("the edit is not on the item: %+v", got.Stages)
	}
	if got.Stream != nil || got.State != factory.StateNew {
		t.Fatalf("shaping ran something: state %s, stream %v", got.State, got.Stream)
	}
	if strings.Contains(model.transcript(), "no resolver is attached") {
		t.Fatalf("a manager verb was refused on the headless turn:\n%s", model.transcript())
	}
}

// shapingModel is an OpenAI-shaped endpoint that answers the first request
// carrying tools with one `factory_run` call and every other request with a
// line, recording what it was sent.
type shapingModel struct {
	server *httptest.Server
	mu     sync.Mutex
	turn   int
	seen   []string
}

func newShapingModel(t *testing.T) *shapingModel {
	t.Helper()
	m := &shapingModel{}
	edit := `{"set":[{"stage":"review","ask":"read it for security"}],"why":"touches the ledger"}`
	m.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
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
		m.mu.Lock()
		m.seen = append(m.seen, string(raw))
		step := -1
		if len(envelope.Tools) > 0 {
			step = m.turn
			m.turn++
		}
		m.mu.Unlock()
		if step == 0 {
			writeToolCall(writer, envelope.Stream, "s1", "factory_run", edit)
			return
		}
		writeText(writer, envelope.Stream, "set review to read for security.")
	}))
	t.Cleanup(m.server.Close)
	return m
}

// transcript is every request body the model was sent, for a failure message.
func (m *shapingModel) transcript() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return strings.Join(m.seen, "\n---\n")
}

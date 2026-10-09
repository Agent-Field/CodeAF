package desktopbridge

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// movableAgent records the model swaps the bridge asks of an open chat.
type movableAgent struct {
	*fakeAgent
	swaps  []string
	levels []string
}

func (a *movableAgent) SetModel(model string) { a.swaps = append(a.swaps, model); a.model = model }
func (a *movableAgent) SetReasoningFor(model, level string) {
	a.levels = append(a.levels, model+":"+level)
}

func modelsFixture(t *testing.T, catalog func(context.Context) ([]CatalogModel, error)) (*Bridge, *movableAgent, string) {
	t.Helper()
	agent := &movableAgent{fakeAgent: &fakeAgent{events: make(chan session.Event, 8), model: Model}}
	b := New(testToken, func(string) (Connection, error) {
		return Connection{Agent: agent, Welcome: remote.Welcome{SessionFile: "s.jsonl", Workspace: "/p", Model: Model, Persistent: true, Launch: &remote.LaunchShape{OneModel: true}}, Close: func() {}}, nil
	})
	t.Cleanup(b.Close)
	profile := t.TempDir()
	b.UseModels(&Models{ProfileDir: profile, Catalog: catalog})
	if w := request(b, "POST", "/api/engine/sessions", "{}"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	return b, agent, profile
}

func goodCatalog(context.Context) ([]CatalogModel, error) {
	return []CatalogModel{{ID: "z/model", Name: "Z"}, {ID: Model, Name: "Flash"}}, nil
}

func TestEveryRoleStartsOnTheDefaultModel(t *testing.T) {
	b, _, _ := modelsFixture(t, goodCatalog)
	w := request(b, "GET", "/api/engine/models/roles", "")
	var view RolesView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || w.Code != 200 {
		t.Fatalf("%d %v %s", w.Code, err, w.Body.String())
	}
	if len(view.Roles) < 5 || view.Default != Model {
		t.Fatalf("view: %+v", view)
	}
	for _, role := range view.Roles {
		if role.Model != Model || role.Chosen || role.Name == "" || role.Controls == "" {
			t.Fatalf("role %+v", role)
		}
	}
}

func TestAChosenModelIsPersistedAndReadBackByTheEngineSource(t *testing.T) {
	b, _, profile := modelsFixture(t, goodCatalog)
	w := request(b, "PUT", "/api/engine/models/roles/tasks", `{"model":"z/model","effort":"high"}`)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	source := config.DesktopRolesSource(profile)
	if got, ok := source("roles.worker"); !ok || got != "z/model:high" {
		t.Fatalf("engine reads %q %v", got, ok)
	}
	if got, ok := source("roles.title"); !ok || got != Model {
		t.Fatalf("an unchosen role must read the default, got %q %v", got, ok)
	}
	if w := request(b, "PUT", "/api/engine/models/roles/tasks", `{"model":""}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if got, _ := source("roles.worker"); got != Model {
		t.Fatalf("reset left %q", got)
	}
}

func TestTheConversationRoleMovesOpenChatsLive(t *testing.T) {
	b, agent, profile := modelsFixture(t, goodCatalog)
	w := request(b, "PUT", "/api/engine/models/roles/conversation", `{"model":"z/model","effort":"low"}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if len(agent.swaps) != 1 || agent.swaps[0] != "z/model" || len(agent.levels) != 1 || agent.levels[0] != "z/model:low" {
		t.Fatalf("swaps=%v levels=%v", agent.swaps, agent.levels)
	}
	if got := config.ChatModelAt(profile); got != "z/model" {
		t.Fatalf("a new chat would open on %q", got)
	}
}

func TestBadRoleChoicesAreRefused(t *testing.T) {
	b, _, _ := modelsFixture(t, goodCatalog)
	for name, call := range map[string][3]string{
		"unknown role":   {"PUT", "/api/engine/models/roles/nope", `{"model":"z/model"}`},
		"unlisted model": {"PUT", "/api/engine/models/roles/tasks", `{"model":"not/listed"}`},
		"bad effort":     {"PUT", "/api/engine/models/roles/tasks", `{"model":"z/model","effort":"loud"}`},
		"wrong verb":     {"POST", "/api/engine/models/roles/tasks", `{}`},
	} {
		if w := request(b, call[0], call[1], call[2]); w.Code < 400 {
			t.Fatalf("%s answered %d", name, w.Code)
		}
	}
}

func TestAnUnreachableCatalogFallsBackToTheModelsInUse(t *testing.T) {
	b, _, _ := modelsFixture(t, func(context.Context) ([]CatalogModel, error) { return nil, errors.New("down") })
	w := request(b, "GET", "/api/engine/models", "")
	var view ModelsView
	_ = json.Unmarshal(w.Body.Bytes(), &view)
	if !view.Fallback || len(view.Models) != 1 || view.Models[0].ID != Model {
		t.Fatalf("%+v", view)
	}
}

func TestTheCatalogIsFetchedOnceWhileFresh(t *testing.T) {
	calls := 0
	b, _, _ := modelsFixture(t, func(ctx context.Context) ([]CatalogModel, error) { calls++; return goodCatalog(ctx) })
	request(b, "GET", "/api/engine/models", "")
	request(b, "GET", "/api/engine/models", "")
	if calls != 1 {
		t.Fatalf("fetched %d times", calls)
	}
}

func TestModelRoutesNeedTheToken(t *testing.T) {
	b, _, _ := modelsFixture(t, goodCatalog)
	b.token = "other-token"
	if w := request(b, "GET", "/api/engine/models/roles", ""); w.Code != 401 {
		t.Fatalf("%d", w.Code)
	}
}

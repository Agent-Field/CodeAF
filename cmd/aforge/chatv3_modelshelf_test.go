package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/modelsource"
	"github.com/Agent-Field/aforge-v2/internal/modelsource/sourcestub"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
)

// shelfRouter is a router with no network behind it: it lists rows, or fails
// the way a dead connection fails.
func shelfRouter(rows string, fail error) *http.Client {
	return &http.Client{Transport: mediaRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if fail != nil {
			return nil, fail
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"data":[` + rows + `]}`)), Request: request}, nil
	})}
}

func TestAConnectedServiceRefreshLandsOnTheProcessShelf(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	defaultHost := sourcestub.New("openai/gpt-4.1-mini")
	defer defaultHost.Close()
	directHost := sourcestub.New("fake-small", "fake-large")
	defer directHost.Close()
	dir := t.TempDir()
	defaultSource := modelsource.DefaultSource(defaultHost.URL())
	defaultService := modelsource.Connected{Source: defaultSource, Key: "default-key", Address: defaultHost.URL()}
	discovery := catalog.Options{BaseURL: defaultHost.URL(), APIKey: defaultService.Key, Dir: dir}
	launch := catalog.Load(t.Context(), discovery)
	shelf := newV3ModelShelf(launch, discovery)
	custom := modelsource.Vendored()[6]
	outcome, err := config.ConnectService(t.Context(), dir, config.PersistedSource{
		ID: custom.ID, Written: "localhost", Address: directHost.URL(), Key: "direct-key", Order: 1,
	}, custom, nil)
	if err != nil || outcome.Kind != modelsource.OutcomeConnected || outcome.Models != 2 {
		t.Fatalf("Something else connection = %+v, %v", outcome, err)
	}
	sources := config.ResolveSources(dir, defaultService.Key, defaultHost.URL())
	directService, ok := sources.ByID("custom")
	if !ok {
		t.Fatal("the persisted Something else service did not resolve")
	}
	shelf.setSources(sources)
	seed := make([]tui3.Model, 0, len(outcome.ModelIDs))
	for _, id := range outcome.ModelIDs {
		seed = append(seed, tui3.Model{ID: id})
	}
	rows, err := shelf.refreshService(t.Context(), directService, seed)
	if err != nil {
		t.Fatal(err)
	}
	if got := shelf.modelsForService(directService); len(rows) != 2 || len(got) != 2 || got[0].ID != "fake-small" {
		t.Fatalf("process shelf rows = %+v (refresh %+v)", got, rows)
	}
	if cached := tui3.CachedModelsFor("custom", directHost.URL()); len(cached) != 2 {
		t.Fatalf("picker cache rows = %+v", cached)
	}
	owner := catalog.CacheKey(custom.ID, directHost.URL())
	if _, err := os.Stat(filepath.Join(dir, "model-catalog-"+owner+".json")); err != nil {
		t.Fatalf("the connected service wrote no source-scoped catalog cache: %v", err)
	}
	if _, err := os.Stat(tui3.ModelCachePathFor(custom.ID, directHost.URL())); err != nil {
		t.Fatalf("the connected service wrote no source-scoped picker cache: %v", err)
	}

	agent, err := session.New(session.Config{Workspace: t.TempDir(), Model: "localhost/fake-small", Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	events, err := agent.Submit(t.Context(), "answer")
	if err != nil {
		t.Fatal(err)
	}
	for event := range events {
		if event.Kind == session.EventError {
			t.Fatal(event.Err)
		}
	}
	requests := directHost.Requests()
	foundTurn := false
	for _, request := range requests {
		if request.Method == http.MethodPost && request.Bearer == "Bearer direct-key" {
			var envelope struct {
				Model string `json:"model"`
			}
			if err := json.Unmarshal(request.Body, &envelope); err != nil {
				t.Fatal(err)
			}
			foundTurn = envelope.Model == "fake-small"
		}
	}
	if !foundTurn {
		t.Fatalf("the real agent did not reach the connected host with its bearer: %+v", requests)
	}
	for _, request := range defaultHost.Requests() {
		if request.Method == http.MethodPost {
			t.Fatalf("the default host received the direct turn: %+v", defaultHost.Requests())
		}
	}
}

func TestAReboundDoorReplacesTheShelfAndSeedsItsFixedCatalogWithoutAFetch(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		oldAddress string
		oldDoor    string
	}{
		{name: "address changed", oldAddress: "https://metered.example/v1", oldDoor: "coding-plan"},
		{name: "door changed on one host", oldAddress: "https://plan.example/v1", oldDoor: "metered"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			shelf := &v3ModelShelf{direct: map[string]serviceCompartment{
				"z-ai": {
					address: testCase.oldAddress, door: testCase.oldDoor,
					models: []tui3.Model{{ID: "metered-one"}, {ID: "metered-two"}},
				},
			}}
			plan := modelsource.Door{
				ID: "coding-plan", Name: "coding plan", Address: "https://plan.example/v1",
				Models: []string{"glm-5.3", "glm-5.3-flash", "glm-5.3[1m]", "glm-5.3-flash[1m]"},
			}
			service := modelsource.Connected{
				Source:  modelsource.Source{ID: "z-ai", Written: "z-ai", Listing: modelsource.ListingModels},
				Address: plan.Address, Door: plan,
			}
			shelf.setSources(modelsource.NewSet(
				modelsource.Connected{Source: modelsource.DefaultSource("https://router.example/v1")},
				service,
			))

			got := shelf.modelsForService(service)
			if len(got) != 4 || got[0].ID != "glm-5.3" || got[3].ID != "glm-5.3-flash[1m]" {
				t.Fatalf("rebound shelf = %+v, want only the fixed plan catalog", got)
			}
			held := shelf.direct["z-ai"]
			if held.address != plan.Address || held.door != plan.ID {
				t.Fatalf("compartment identity = %+v", held)
			}
		})
	}
}

const shelfRow = `{"id":"vendor/old","architecture":{"input_modalities":["text"],"output_modalities":["text"]}}`
const shelfNewRow = `{"id":"vendor/shipped-this-morning","architecture":{"input_modalities":["text","image"],"output_modalities":["text"]}}`

// A LANDED REFRESH IS ON THE SHELF, IN THE PICKER'S CACHE AND DATED; A FAILED
// ONE CHANGES NOTHING AND SAYS WHY IN THE TRANSPORT'S OWN WORDS. The shelf is
// what /model's list and the vision gate read, so a model the router shipped
// this morning is both listed and able to see the moment the refresh lands.
func TestTheShelfTakesTodaysListAndKeepsYesterdaysOnFailure(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	dir := t.TempDir()
	launch := catalog.Load(context.Background(), catalog.Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: dir, HTTPClient: shelfRouter(shelfRow, nil),
	})

	dead := newV3ModelShelf(launch, catalog.Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: dir,
		HTTPClient: shelfRouter("", errors.New("no such host")),
	})
	if rows, _, err := dead.refresh(context.Background()); err == nil || err.Error() != "no such host" || rows != nil {
		t.Fatalf("a dead router answered %v rows, error %v — want the transport's own reason", len(rows), err)
	}
	if got := v3Models(dead); len(got) != 1 || got[0].ID != "vendor/old" {
		t.Fatalf("a failed refresh changed the shelf: %+v", got)
	}

	live := newV3ModelShelf(launch, catalog.Options{
		BaseURL: "https://openrouter.example/api/v1", Dir: dir,
		HTTPClient: shelfRouter(shelfRow+","+shelfNewRow, nil),
	})
	rows, at, err := live.refresh(context.Background())
	if err != nil || len(rows) != 2 || at.IsZero() {
		t.Fatalf("a landed refresh answered %d rows at %v, error %v", len(rows), at, err)
	}
	if !v3SeesImages(live)("vendor/shipped-this-morning") {
		t.Fatal("the vision gate does not know the model the refresh brought")
	}
	cached := tui3.CachedModels()
	if len(cached) != 2 || cached[1].ID != "vendor/shipped-this-morning" {
		t.Fatalf("~/.aforge/v3/models.json is not today's list: %+v", cached)
	}
}

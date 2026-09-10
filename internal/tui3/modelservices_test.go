package tui3

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	modelcatalog "github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/modelsource"
	"github.com/Agent-Field/aforge-v2/internal/modelsource/sourcestub"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

func installModelServiceShelf(a *app, dir string) {
	held := make(map[string][]Model)
	a.modelsForService = func(service modelsource.Connected) []Model {
		return append([]Model(nil), held[service.Source.ID]...)
	}
	a.serviceModelRefresh = func(ctx context.Context, service modelsource.Connected) ([]Model, error) {
		models, err := modelcatalog.Refresh(ctx, modelcatalog.Options{
			Source: service.Source.ID, BaseURL: service.Address, APIKey: service.Key, Dir: dir,
		})
		if err != nil {
			return nil, err
		}
		rows := surfaceModels(models.ModelsNow())
		held[service.Source.ID] = append([]Model(nil), rows...)
		return rows, nil
	}
}

func modelServiceTestApp(t *testing.T, dir string, model string, sources modelsource.Set, models []Model) *app {
	return modelServiceTestAppWithAgent(t, dir, model, sources, models, &fakeAgent{model: model})
}

func modelServiceTestAppWithAgent(t *testing.T, dir string, model string, sources modelsource.Set, models []Model, agent Agent) *app {
	t.Helper()
	for _, env := range []string{"DEEPSEEK_API_KEY", "ZHIPU_API_KEY", "MOONSHOT_API_KEY"} {
		t.Setenv(env, "")
	}
	t.Setenv("AFORGE_HOME", t.TempDir())
	options := Options{
		Agent: agent, ProfileDir: dir, Sources: sources,
		Models: func() []Model { return models },
	}
	if live, ok := agent.(*session.Agent); ok {
		options.ApplyModelSources = live.SetSources
	}
	a := newApp(t.Context(), options)
	a.width, a.height = 100, 30
	a.pal = newPalette(tokens.ANSI256, false)
	return a
}

func testDefaultService(key string) modelsource.Connected {
	source := modelsource.DefaultSource(config.DefaultBaseURL)
	return modelsource.Connected{Source: source, Key: key, Address: source.Address}
}

func testDirectService(address string) modelsource.Connected {
	for _, source := range modelsource.Vendored() {
		if source.ID == "deepseek" {
			source.Written = "deepseek-direct"
			source.Address = address
			return modelsource.Connected{Source: source, Key: "sk-direct-1234567890", Address: address}
		}
	}
	return modelsource.Connected{}
}

func TestAConnectedServicesModelsAppearGroupedWithoutARestart(t *testing.T) {
	defaultServer := sourcestub.New("openai/gpt-4.1-mini")
	defer defaultServer.Close()
	server := sourcestub.New("deepseek-chat", "deepseek-reasoner")
	defer server.Close()
	dir := t.TempDir()
	defaults := []Model{{ID: "openai/gpt-4.1-mini"}}
	defaultService := testDefaultService("sk-default-1234567890")
	defaultService.Source.Address, defaultService.Address = defaultServer.URL(), defaultServer.URL()
	sources := modelsource.NewSet(defaultService)
	agent, err := session.New(session.Config{Workspace: t.TempDir(), Model: defaults[0].ID, Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	a := modelServiceTestAppWithAgent(t, dir, defaults[0].ID, sources, defaults, agent)
	installModelServiceShelf(a, dir)
	a.openConnect()
	rowAt := -1
	for at := range a.connPanel.hits {
		row, ok := a.connPanel.at(at)
		if ok && row.ID == modelConnectionID("custom") {
			rowAt = at
			break
		}
	}
	if rowAt < 0 {
		t.Fatal("the models group did not contain Something else")
	}
	a.connPanel.cursor = rowAt
	if cmd := a.connectAct(rowAt); cmd != nil || a.connPanel.entry == nil {
		t.Fatal("enter on Something else did not open the address box")
	}
	a.connPanel.entry.box.setText(server.URL())
	if cmd := a.connectEntryKey(key("enter")); cmd != nil || a.connPanel.entry == nil {
		t.Fatal("the completed address did not open the key box")
	}
	a.connPanel.entry.box.setText("sk-direct-1234567890")
	cmd := a.connectEntryKey(key("enter"))
	if cmd == nil {
		t.Fatal("the completed key did not start the connection")
	}
	msg := cmd()
	if _, follow := a.Update(msg); follow != nil {
		t.Fatal("a settled direct connection unexpectedly started another command")
	}

	list := a.modelList()
	if len(list) != 3 {
		t.Fatalf("model list = %+v", list)
	}
	if list[0].ID != "openai/gpt-4.1-mini" || strings.HasPrefix(list[0].ID, "openrouter/") {
		t.Fatalf("the default row was qualified: %+v", list[0])
	}
	connected, ok := a.sources.ByID("custom")
	if !ok {
		t.Fatal("the custom service was not live after connection")
	}
	written := connected.Source.Written
	for _, want := range []string{written + "/deepseek-chat", written + "/deepseek-reasoner"} {
		found := false
		for _, row := range list {
			found = found || row.ID == want
		}
		if !found {
			t.Fatalf("the connected service did not add %q: %+v", want, list)
		}
	}
	a.openPicker()
	rendered := strings.Join(a.pick.rows(100, a.pick.height(100), a.pal, -1, a.reasoningFor), "\n")
	for _, want := range []string{"openrouter", written, written + "/deepseek-chat"} {
		if !strings.Contains(plain(rendered), want) {
			t.Fatalf("the grouped picker did not draw %q:\n%s", want, plain(rendered))
		}
	}
	if got := noteSaying(t, a, "is connected"); got != strings.ToLower(written)+" is connected · 2 models" {
		t.Fatalf("connection note = %q", got)
	}
	a.switchModel(written+"/deepseek-chat", 0)
	drainModelServiceTurn(t, agent, "use the newly connected service")
	if got := completionRequests(server); len(got) != 1 || got[0].Bearer != "Bearer sk-direct-1234567890" || got[0].Host == "" || completionModel(t, got[0]) != "deepseek-chat" {
		t.Fatalf("direct service requests = %+v", got)
	}
	if got := completionRequests(defaultServer); len(got) != 0 {
		t.Fatalf("the old service received the switched turn: %+v", got)
	}
}

func drainModelServiceTurn(t *testing.T, agent *session.Agent, text string) {
	t.Helper()
	events, err := agent.Submit(t.Context(), text)
	if err != nil {
		t.Fatal(err)
	}
	for event := range events {
		if event.Kind == session.EventError {
			t.Fatalf("turn failed: %v", event.Err)
		}
	}
}

func completionRequests(server *sourcestub.Server) []sourcestub.Request {
	var found []sourcestub.Request
	for _, request := range server.Requests() {
		if request.Method == "POST" && strings.HasSuffix(request.Path, modelsource.ChatCompletionsPath) {
			found = append(found, request)
		}
	}
	return found
}

func completionModel(t *testing.T, request sourcestub.Request) string {
	t.Helper()
	var envelope struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(request.Body, &envelope); err != nil {
		t.Fatalf("decode completion request: %v", err)
	}
	return envelope.Model
}

func TestOneServiceDrawsThePickerExactlyAsItDidBefore(t *testing.T) {
	models := []Model{{ID: "openai/gpt-4.1-mini", ContextLength: 1_000_000}, {ID: "gpt-5-classic"}}
	pal := newPalette(tokens.ANSI256, false)

	a := modelServiceTestApp(t, t.TempDir(), models[0].ID,
		modelsource.NewSet(testDefaultService("sk-default-1234567890")), models)
	if got := a.modelList(); !reflect.DeepEqual(got, models) {
		t.Fatalf("one-service list changed: got %+v want %+v", got, models)
	}
	a.openPicker()
	got := a.pick.rows(100, a.pick.height(100), pal, -1, func(string) string { return "" })
	want := "› openai/gpt-4.1-mini                                                                             1M\n" +
		"  gpt-5-classic"
	if rendered := plain(strings.Join(got, "\n")); rendered != want {
		t.Fatalf("one-service picker changed:\ngot  %q\nwant %q", rendered, want)
	}
}

func TestAServiceWithNoListingDrawsNoCount(t *testing.T) {
	outcome := modelsource.Outcome{Kind: modelsource.OutcomeConnected, Models: 0, Listed: false}
	if got := serviceConnectedWord("z-ai", outcome); got != "z-ai is connected" {
		t.Fatalf("listing-less connection = %q", got)
	}
	if strings.Contains(serviceConnectedWord("z-ai", outcome), "0 models") {
		t.Fatal("a listing-less service drew a zero count")
	}
	if got := serviceConnectedWord("custom", modelsource.Outcome{Kind: modelsource.OutcomeConnected, Listed: true}); got != "custom is connected" {
		t.Fatalf("an answered empty listing drew a count: %q", got)
	}
	defaultServer := sourcestub.New("openai/gpt-4.1-mini")
	defer defaultServer.Close()
	listingless := sourcestub.New("glm-4.6")
	listingless.Listingless()
	defer listingless.Close()
	zai := modelsource.Vendored()[1]
	zai.Address = listingless.URL()
	defaultService := testDefaultService("sk-default-1234567890")
	defaultService.Source.Address, defaultService.Address = defaultServer.URL(), defaultServer.URL()
	sources := modelsource.NewSet(defaultService, modelsource.Connected{Source: zai, Key: "zai-key", Address: zai.Address})
	agent, err := session.New(session.Config{Workspace: t.TempDir(), Model: "openai/gpt-4.1-mini", Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	a := modelServiceTestAppWithAgent(t, t.TempDir(), "openai/gpt-4.1-mini", sources,
		[]Model{{ID: "openai/gpt-4.1-mini"}}, agent)
	a.modelsForService = func(modelsource.Connected) []Model { return nil }
	list := a.modelList()
	if len(list) != 2 || !list[1].Unavailable || list[1].ID != noServiceModelListWord {
		t.Fatalf("listing-less picker rows = %+v", list)
	}
	a.openPicker()
	rendered := plain(strings.Join(a.pick.rows(100, a.pick.height(100), a.pal, -1, a.reasoningFor), "\n"))
	if !strings.Contains(rendered, "z-ai") || !strings.Contains(rendered, noServiceModelListWord) {
		t.Fatalf("listing-less group was not drawn:\n%s", rendered)
	}
	a.switchModel("z-ai/glm-4.6", 0)
	drainModelServiceTurn(t, agent, "use the listing-less service")
	if got := completionRequests(listingless); len(got) != 1 || got[0].Bearer != "Bearer zai-key" || completionModel(t, got[0]) != "glm-4.6" {
		t.Fatalf("listing-less service request = %+v", got)
	}
	if got := completionRequests(defaultServer); len(got) != 0 {
		t.Fatalf("default service received the direct turn: %+v", got)
	}
}

func TestTheModelServiceWordsAreExactAndVendorWordsStopAtAWordBoundary(t *testing.T) {
	tests := []struct {
		outcome modelsource.Outcome
		want    string
	}{
		{modelsource.Outcome{Kind: modelsource.OutcomeConnected, Listed: true, Models: 6}, "deepseek is connected · 6 models"},
		{modelsource.Outcome{Kind: modelsource.OutcomeConnected}, "deepseek is connected"},
		{modelsource.Outcome{Kind: modelsource.OutcomeRefused, VendorSaid: "Authentication Fails, Your api key is invalid"}, "deepseek refused that key — Authentication Fails, Your api key is invalid"},
		{modelsource.Outcome{Kind: modelsource.OutcomeUnanswered}, "deepseek did not answer · nothing was saved"},
		{modelsource.Outcome{Kind: modelsource.OutcomeWrongShape}, "that is not the shape of a deepseek key — they start with sk-"},
		{modelsource.Outcome{Kind: modelsource.OutcomeCollides, Suggestion: "deepseek-direct"}, "deepseek is a model author on openrouter · connect this as deepseek-direct"},
	}
	for _, testCase := range tests {
		if got := serviceOutcomeWord("deepseek", testCase.outcome); got != testCase.want {
			t.Errorf("word = %q, want %q", got, testCase.want)
		}
	}
	if got := serviceAnsweringWord("deepseek"); got != "deepseek is answering right now · try again in a moment" {
		t.Errorf("answering word = %q", got)
	}
	if got := serviceDisconnectedWord("deepseek"); got != "deepseek is disconnected · its models are gone from the picker" {
		t.Errorf("disconnected word = %q", got)
	}
	if got := serviceMovedWord("deepseek-direct/deepseek-v4-pro", "~deepseek/deepseek-v4-flash-latest"); got != "this conversation was on deepseek-direct/deepseek-v4-pro · it is now on ~deepseek/deepseek-v4-flash-latest" {
		t.Errorf("moved word = %q", got)
	}
	if got := serviceStrandedWord("deepseek-direct/deepseek-v4-pro"); got != "this conversation was on deepseek-direct/deepseek-v4-pro and nothing else here can take it · connect a service or pick a model" {
		t.Errorf("stranded word = %q", got)
	}
	words := strings.Repeat("word ", 30) + "tail"
	got := truncateVendorWords(words, 120)
	if len(got) > 120 || strings.HasSuffix(got, "wor") {
		t.Fatalf("vendor words were not cut at a word boundary: %q", got)
	}
}

func TestDisconnectingAServiceLeavesTheConversationOnSomethingItCanReach(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		defaultKey string
		wantModel  string
		want       string
	}{
		{
			name: "the default service can take it", defaultKey: "sk-default-1234567890",
			wantModel: config.DefaultModel,
			want:      "this conversation was on deepseek-direct/deepseek-v4-pro · it is now on " + config.DefaultModel,
		},
		{
			name: "nothing else can take it", wantModel: "deepseek-direct/deepseek-v4-pro",
			want: "this conversation was on deepseek-direct/deepseek-v4-pro and nothing else here can take it · connect a service or pick a model",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			row := config.PersistedSource{ID: "deepseek", Written: "deepseek-direct", Key: "sk-direct-1234567890", Order: 1}
			if err := config.WriteSources(dir, []config.PersistedSource{row}); err != nil {
				t.Fatal(err)
			}
			defaultServer := sourcestub.New(config.DefaultModel)
			defer defaultServer.Close()
			directServer := sourcestub.New("deepseek-v4-pro")
			defer directServer.Close()
			base := testDefaultService(testCase.defaultKey)
			base.Source.Address, base.Address = defaultServer.URL(), defaultServer.URL()
			direct := testDirectService(directServer.URL())
			sources := modelsource.NewSet(base, direct)
			agent, err := session.New(session.Config{
				Workspace: t.TempDir(), Model: "deepseek-direct/deepseek-v4-pro", Sources: sources,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer agent.Close()
			drainModelServiceTurn(t, agent, "before disconnect")
			a := modelServiceTestAppWithAgent(t, dir, agent.Model(), sources,
				[]Model{{ID: config.DefaultModel}}, agent)
			a.disconnectModelService("deepseek")
			if got := noteSaying(t, a, "is disconnected"); got != "deepseek-direct is disconnected · its models are gone from the picker" {
				t.Fatalf("disconnect acknowledgement = %q", got)
			}
			if got := noteSaying(t, a, "this conversation was on"); got != testCase.want {
				t.Fatalf("disconnect note = %q, want %q", got, testCase.want)
			}
			if a.model != testCase.wantModel || a.agent.Model() != testCase.wantModel {
				t.Fatalf("conversation model = %q/%q, want %q", a.model, a.agent.Model(), testCase.wantModel)
			}
			if rows := config.PersistedSources(dir); len(rows) != 0 {
				t.Fatalf("the profile still holds the service: %+v", rows)
			}
			if got := completionRequests(directServer); len(got) != 1 || got[0].Bearer != "Bearer sk-direct-1234567890" || completionModel(t, got[0]) != "deepseek-v4-pro" {
				t.Fatalf("disconnected service requests = %+v", got)
			}
			if testCase.defaultKey != "" {
				drainModelServiceTurn(t, agent, "after disconnect")
				if got := completionRequests(defaultServer); len(got) != 1 || got[0].Bearer != "Bearer "+testCase.defaultKey || completionModel(t, got[0]) != config.DefaultModel {
					t.Fatalf("replacement service request = %+v", got)
				}
			} else if got := completionRequests(defaultServer); len(got) != 0 {
				t.Fatalf("unreachable default service received a turn: %+v", got)
			}
		})
	}
}

func TestDisconnectingAnUnusedServiceDrawsOnlyItsAcknowledgement(t *testing.T) {
	dir := t.TempDir()
	row := config.PersistedSource{ID: "deepseek", Written: "deepseek-direct", Key: "sk-direct-1234567890", Order: 1}
	if err := config.WriteSources(dir, []config.PersistedSource{row}); err != nil {
		t.Fatal(err)
	}
	defaultServer := sourcestub.New("openai/gpt-4.1-mini")
	defer defaultServer.Close()
	directServer := sourcestub.New("deepseek-v4-pro")
	defer directServer.Close()
	base := testDefaultService("sk-default-1234567890")
	base.Source.Address, base.Address = defaultServer.URL(), defaultServer.URL()
	direct := testDirectService(directServer.URL())
	sources := modelsource.NewSet(base, direct)
	agent, err := session.New(session.Config{Workspace: t.TempDir(), Model: "openai/gpt-4.1-mini", Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	a := modelServiceTestAppWithAgent(t, dir, agent.Model(), sources, []Model{{ID: agent.Model()}}, agent)
	a.disconnectModelService("deepseek")
	if got := noteSaying(t, a, "is disconnected"); got != "deepseek-direct is disconnected · its models are gone from the picker" {
		t.Fatalf("disconnect acknowledgement = %q", got)
	}
	for _, note := range noteTexts(a) {
		if strings.Contains(note, "this conversation was on") {
			t.Fatalf("an unused service drew a move sentence: %q", note)
		}
	}
	drainModelServiceTurn(t, agent, "after disconnect")
	if got := completionRequests(defaultServer); len(got) != 1 || got[0].Bearer != "Bearer sk-default-1234567890" || completionModel(t, got[0]) != "openai/gpt-4.1-mini" {
		t.Fatalf("remaining service request = %+v", got)
	}
	if got := completionRequests(directServer); len(got) != 0 {
		t.Fatalf("unused removed service received a turn: %+v", got)
	}
}

func TestACustomServiceUsesItsWrittenNameOnRefusalAndSuccess(t *testing.T) {
	server := sourcestub.New("fake-small", "fake-large")
	defer server.Close()
	server.Refuse(401, `{"error":{"message":"bad key"}}`)
	defaultServer := sourcestub.New("openai/gpt-4.1-mini")
	defer defaultServer.Close()
	base := testDefaultService("sk-default-1234567890")
	base.Source.Address, base.Address = defaultServer.URL(), defaultServer.URL()
	agent, err := session.New(session.Config{Workspace: t.TempDir(), Model: "openai/gpt-4.1-mini", Sources: modelsource.NewSet(base)})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	a := modelServiceTestAppWithAgent(t, t.TempDir(), agent.Model(), modelsource.NewSet(base), []Model{{ID: agent.Model()}}, agent)
	draft := modelConnectDraft{
		source: modelsource.Vendored()[4],
		row:    config.PersistedSource{ID: "custom", Written: "localhost", Address: server.URL(), Key: "a-custom-key", Order: 1},
	}
	msg := a.beginModelConnect(draft)().(modelConnectResultMsg)
	a.adoptModelConnectResult(msg)
	if got := noteSaying(t, a, "refused that key"); got != "localhost refused that key — bad key" {
		t.Fatalf("custom refusal = %q", got)
	}
	server.Healthy()
	installModelServiceShelf(a, a.profileDir)
	msg = a.beginModelConnect(draft)().(modelConnectResultMsg)
	a.adoptModelConnectResult(msg)
	if got := noteSaying(t, a, "is connected"); got != "localhost is connected · 2 models" {
		t.Fatalf("custom success = %q", got)
	}
	a.switchModel("localhost/fake-small", 0)
	drainModelServiceTurn(t, agent, "use custom")
	if got := completionRequests(server); len(got) != 1 || got[0].Bearer != "Bearer a-custom-key" || completionModel(t, got[0]) != "fake-small" {
		t.Fatalf("custom service request = %+v", got)
	}
	if got := completionRequests(defaultServer); len(got) != 0 {
		t.Fatalf("default service received the custom turn: %+v", got)
	}
}

func TestADirectModelKeepsItsServiceInStatusAndCarriesNoLane(t *testing.T) {
	dir := t.TempDir()
	defaultServer := sourcestub.New("openai/gpt-4.1-mini")
	defer defaultServer.Close()
	directServer := sourcestub.New("fake-small")
	defer directServer.Close()
	base := testDefaultService("sk-default-1234567890")
	base.Source.Address, base.Address = defaultServer.URL(), defaultServer.URL()
	direct := testDirectService(directServer.URL())
	direct.Source.ID, direct.Source.Written = "custom", "localhost"
	sources := modelsource.NewSet(base, direct)
	agent, err := session.New(session.Config{Workspace: t.TempDir(), Model: "localhost/fake-small", Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	a := modelServiceTestAppWithAgent(t, dir, agent.Model(), sources, []Model{{ID: "openai/gpt-4.1-mini"}}, agent)
	a.sourceModels["custom"] = []Model{{ID: "fake-small"}}
	if err := config.SetLane(dir, laneSlotFor(a.model), "akashml"); err != nil {
		t.Fatal(err)
	}
	if got := a.modelWord(); got != "localhost/fake-small" {
		t.Fatalf("status model = %q", got)
	}
	a.openPicker()
	rendered := plain(strings.Join(a.pick.rows(100, a.pick.height(100), a.pal, -1, a.reasoningFor), "\n"))
	if strings.Contains(rendered, "via akashml") {
		t.Fatalf("direct row borrowed the default service's lane:\n%s", rendered)
	}
	for at, row := range a.pick.list {
		if row.lane == laneNone && a.pick.all[a.pick.hits[row.hit]].ID == "localhost/fake-small" {
			a.pick.cursor = at
			if a.pick.unfoldHere() {
				t.Fatal("a direct service opened a lane sheet")
			}
			drainModelServiceTurn(t, agent, "use direct")
			if got := completionRequests(directServer); len(got) != 1 || got[0].Bearer != "Bearer sk-direct-1234567890" || completionModel(t, got[0]) != "fake-small" {
				t.Fatalf("direct service request = %+v", got)
			}
			if got := completionRequests(defaultServer); len(got) != 0 {
				t.Fatalf("default service received the direct turn: %+v", got)
			}
			return
		}
	}
	t.Fatal("the direct model was not in the picker")
}

func TestDisconnectingReplacesTheLiveClientBeforeTheNextRequest(t *testing.T) {
	defaultServer := sourcestub.New(config.DefaultModel)
	defer defaultServer.Close()
	directServer := sourcestub.New("deepseek-v4-pro")
	defer directServer.Close()
	dir := t.TempDir()
	row := config.PersistedSource{ID: "deepseek", Written: "deepseek-direct", Key: "sk-direct-1234567890", Order: 1}
	if err := config.WriteSources(dir, []config.PersistedSource{row}); err != nil {
		t.Fatal(err)
	}
	defaultService := testDefaultService("sk-default-1234567890")
	defaultService.Source.Address, defaultService.Address = defaultServer.URL(), defaultServer.URL()
	direct := testDirectService(directServer.URL())
	sources := modelsource.NewSet(defaultService, direct)
	agent, err := session.New(session.Config{
		Workspace: t.TempDir(), Model: "deepseek-direct/deepseek-v4-pro", Sources: sources,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	drainModelServiceTurn(t, agent, "before disconnect")
	a := modelServiceTestAppWithAgent(t, dir, agent.Model(), sources, []Model{{ID: config.DefaultModel}}, agent)
	a.disconnectModelService("deepseek")
	drainModelServiceTurn(t, agent, "after disconnect")
	if got := completionRequests(directServer); len(got) != 1 || got[0].Bearer != "Bearer sk-direct-1234567890" {
		t.Fatalf("removed service received another request or wrong bearer: %+v", got)
	}
	if got := completionRequests(defaultServer); len(got) != 1 || got[0].Bearer != "Bearer sk-default-1234567890" {
		t.Fatalf("replacement service request = %+v", got)
	}
}

func TestADisconnectIsRefusedWhileThatServiceIsAnswering(t *testing.T) {
	dir := t.TempDir()
	row := config.PersistedSource{ID: "deepseek", Written: "deepseek-direct", Key: "sk-direct-1234567890", Order: 1}
	if err := config.WriteSources(dir, []config.PersistedSource{row}); err != nil {
		t.Fatal(err)
	}
	direct := testDirectService("https://api.deepseek.com/v1")
	a := modelServiceTestApp(t, dir, "deepseek-direct/deepseek-v4-pro",
		modelsource.NewSet(testDefaultService("sk-default-1234567890"), direct), nil)
	a.state = stateWorking
	a.disconnectModelService("deepseek")
	if got := noteSaying(t, a, "answering right now"); got != "deepseek-direct is answering right now · try again in a moment" {
		t.Fatalf("refusal = %q", got)
	}
	if len(config.PersistedSources(dir)) != 1 || a.agent.(*fakeAgent).stops != 0 {
		t.Fatal("the refused disconnect removed the key or cut the stream")
	}
}

func TestConnectedServicesAppearUnderProvidersAndEmptinessDrawsNothing(t *testing.T) {
	dir := t.TempDir()
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini",
		modelsource.NewSet(testDefaultService("sk-default-1234567890")), nil)
	a.raiseSettings()
	for i, tab := range settingTabs {
		if tab == tabProviders {
			a.sheet.tab = i
		}
	}
	a.sheet.build()
	for _, item := range a.sheet.items {
		if item.head == "services" || item.service != nil {
			t.Fatal("an empty profile drew the services section")
		}
	}

	row := config.PersistedSource{ID: "deepseek", Written: "deepseek-direct", Key: "sk-direct-1234567890", Order: 1}
	if err := config.WriteSources(dir, []config.PersistedSource{row}); err != nil {
		t.Fatal(err)
	}
	a.sources = modelsource.NewSet(testDefaultService("sk-default-1234567890"), testDirectService("https://api.deepseek.com/v1"))
	a.raiseSettings()
	for i, tab := range settingTabs {
		if tab == tabProviders {
			a.sheet.tab = i
		}
	}
	a.sheet.build()
	foundHead, foundRow := false, false
	keyAt, headAt, rowAt := -1, -1, -1
	for at, item := range a.sheet.items {
		foundHead = foundHead || item.head == "services"
		if item.row.Key == config.KeyAPIKey {
			keyAt = at
		}
		if item.head == "services" {
			headAt = at
		}
		if item.service != nil && item.service.name == "deepseek-direct" {
			foundRow, rowAt = true, at
			if item.service.value != "key · order 1" {
				t.Fatalf("the service row does not carry key and order: %q", item.service.value)
			}
		}
	}
	if !foundHead || !foundRow {
		t.Fatalf("Providers did not draw the connected service: %+v", a.sheet.items)
	}
	if keyAt < 0 || headAt != keyAt+1 || rowAt != headAt+1 {
		t.Fatalf("the services section is not immediately under the openrouter key: key=%d head=%d row=%d", keyAt, headAt, rowAt)
	}
}

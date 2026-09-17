package tui3

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	modelcatalog "github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/modelsource/sourcestub"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

func installModelServiceShelf(a *app, dir string) {
	held := make(map[string][]Model)
	a.modelsForService = func(service modelsource.Connected) []Model {
		return append([]Model(nil), held[service.Source.ID]...)
	}
	a.serviceModelRefresh = func(ctx context.Context, service modelsource.Connected, seed []Model) ([]Model, error) {
		if len(seed) > 0 {
			held[service.Source.ID] = append([]Model(nil), seed...)
		}
		models, err := modelcatalog.Refresh(ctx, modelcatalog.Options{
			Source: service.Source.ID, BaseURL: service.Address, APIKey: service.Key, Dir: dir,
		})
		if err != nil {
			return nil, err
		}
		rows := surfaceModels(models.ModelsNow())
		held[service.Source.ID] = append([]Model(nil), rows...)
		if err := WriteModelCacheFor(service.Source.ID, service.Address, rows); err != nil {
			return nil, err
		}
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
	t.Setenv("CODEAF_HOME", t.TempDir())
	options := Options{
		Agent: agent, ProfileDir: dir, Sources: sources,
		Models:    func() []Model { return models },
		SaveModel: func(model string) error { return config.WriteChatModel(dir, model) },
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

func testModelSource(t *testing.T, id string) modelsource.Source {
	t.Helper()
	for _, source := range modelsource.Vendored() {
		if source.ID == id {
			return source
		}
	}
	t.Fatalf("there is no vendored model service %q", id)
	return modelsource.Source{}
}

func installTestModelSource(t *testing.T, a *app, source modelsource.Source) {
	t.Helper()
	for index := range a.modelCatalog {
		if a.modelCatalog[index].ID == source.ID {
			a.modelCatalog[index] = source
			return
		}
	}
	t.Fatalf("the surface catalog has no model service %q", source.ID)
}

func connectZAIFromPanel(t *testing.T, a *app, apiKey string) {
	t.Helper()
	a.openConnect()
	rowAt := -1
	for at := range a.connPanel.hits {
		row, ok := a.connPanel.at(at)
		if ok && row.ID == modelConnectionID("z-ai") {
			rowAt = at
			break
		}
	}
	if rowAt < 0 {
		t.Fatal("the models group did not contain Z.ai")
	}
	a.connPanel.cursor = rowAt
	if cmd := a.connectAct(rowAt); cmd != nil || a.connPanel.entry == nil || !a.connPanel.entry.choosing() {
		t.Fatal("enter on Z.ai did not open the region choice")
	}
	if cmd := a.connectEntryKey(key("enter")); cmd != nil || a.connPanel.entry == nil || !a.connPanel.entry.secret {
		t.Fatal("the chosen region did not open the key box")
	}
	a.connPanel.entry.box.setText(apiKey)
	cmd := a.connectEntryKey(key("enter"))
	if cmd == nil {
		t.Fatal("the completed key did not start the connection")
	}
	if _, follow := a.Update(cmd()); follow != nil {
		t.Fatal("the settled connection unexpectedly started another command")
	}
}

func newZAIConnectTestApp(t *testing.T, defaultKey string) (*app, string) {
	t.Helper()
	for _, pin := range []string{config.APIKeyEnv, "OPENAI_API_KEY", "ZHIPU_API_KEY"} {
		t.Setenv(pin, "")
	}
	plan := sourcestub.New("plan-probe")
	t.Cleanup(plan.Close)
	metered := sourcestub.New("metered-probe")
	t.Cleanup(metered.Close)
	source := testModelSource(t, "z-ai")
	source.Doors[0].Address = plan.URL()
	source.Doors[1].Address = metered.URL()
	dir := t.TempDir()
	opening := "~deepseek/deepseek-v4-flash-latest"
	a := modelServiceTestApp(t, dir, opening,
		modelsource.NewSet(testDefaultService(defaultKey)),
		[]Model{{ID: opening}, {ID: "z-ai/existing-author"}})
	installTestModelSource(t, a, source)
	return a, dir
}

func TestConnectingAServiceMovesTheConversationOntoItsPreferredModel(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		defaultKey string
	}{
		{name: "with the default service connected", defaultKey: "sk-default-1234567890"},
		{name: "with no default service key"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			a, dir := newZAIConnectTestApp(t, testCase.defaultKey)
			opening := a.model
			connectZAIFromPanel(t, a, "plan-test-key")

			want := "z-ai-direct/glm-5.3"
			if a.model != want || a.agent.Model() != want || a.modelWord() != want {
				t.Fatalf("conversation model = %q/%q, status = %q, want %q", a.model, a.agent.Model(), a.modelWord(), want)
			}
			if got := noteSaying(t, a, "is connected"); got != "z-ai-direct is connected · coding plan · 4 models" {
				t.Fatalf("connection note = %q", got)
			}
			if got := noteSaying(t, a, "this conversation was on"); got != "this conversation was on "+opening+" · it is now on "+want {
				t.Fatalf("move note = %q", got)
			}
			if got := config.ChatModelAt(dir); got != want {
				t.Fatalf("the profile holds %q, want the moved model %q", got, want)
			}
		})
	}
}

func TestConnectingAServiceFromProvidersMovesTheConversationOntoItsPreferredModel(t *testing.T) {
	a, dir := newZAIConnectTestApp(t, "")
	source, ok := a.modelSource("z-ai")
	if !ok {
		t.Fatal("the test surface lost Z.ai")
	}
	row := config.PersistedSource{
		ID: source.ID, Written: "z-ai-direct", Region: "intl", Key: "old-plan-key", Door: source.Doors[0].ID, Order: 1,
	}
	if err := config.WriteSources(dir, []config.PersistedSource{row}); err != nil {
		t.Fatal(err)
	}
	connectedSource := source
	connectedSource.Written = row.Written
	connectedSource.Address = source.Doors[0].Address
	a.sources = modelsource.NewSet(a.sources.Default(), modelsource.Connected{
		Source: connectedSource, Key: row.Key, Address: source.Doors[0].Address, Door: source.Doors[0],
	})
	a.openSettings()
	toProviders(t, a)
	found := false
	for at, item := range a.sheet.items {
		if item.service != nil && item.service.id == "z-ai" {
			a.sheet.cursor = at
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Providers did not draw the connected Z.ai service")
	}
	drive(t, a, key("enter"))
	drive(t, a, key("enter"))
	if a.sheet.conn.entry == nil || !a.sheet.conn.entry.secret {
		t.Fatal("the Providers road did not reach the key box")
	}
	a.sheet.conn.entry.box.setText("new-plan-key")
	drive(t, a, key("enter"))

	wantModel := "z-ai-direct/glm-5.3"
	wantMessage := "z-ai-direct is connected · coding plan · 4 models\n" +
		"this conversation was on ~deepseek/deepseek-v4-flash-latest · it is now on " + wantModel
	if a.model != wantModel || a.modelWord() != wantModel {
		t.Fatalf("Providers left the conversation on %q with status %q", a.model, a.modelWord())
	}
	if a.sheet.msg != wantMessage {
		t.Fatalf("Providers message = %q, want %q", a.sheet.msg, wantMessage)
	}
}

func TestAConnectDuringATurnMovesTheModelWhenTheTurnEnds(t *testing.T) {
	a, _ := newZAIConnectTestApp(t, "")
	opening := a.model
	a.state = stateWorking
	connectZAIFromPanel(t, a, "plan-test-key")

	if a.model != opening || a.modelWord() != opening {
		t.Fatalf("a running turn moved from %q to %q before it ended", opening, a.model)
	}
	for _, note := range noteTexts(a) {
		if strings.Contains(note, "this conversation was on") {
			t.Fatalf("the running turn drew its deferred move early: %q", note)
		}
	}
	if a.deferredModelServiceModel != "z-ai-direct/glm-5.3" {
		t.Fatalf("deferred model = %q", a.deferredModelServiceModel)
	}
	a.settle()
	if a.model != "z-ai-direct/glm-5.3" || a.state != stateIdle || a.deferredModelServiceModel != "" {
		t.Fatalf("settled model/state/deferred = %q/%v/%q", a.model, a.state, a.deferredModelServiceModel)
	}
	if got := noteSaying(t, a, "this conversation was on"); got != "this conversation was on "+opening+" · it is now on z-ai-direct/glm-5.3" {
		t.Fatalf("settled move note = %q", got)
	}
}

func TestDisconnectingAServiceBeforeTheTurnEndsClearsItsDeferredMove(t *testing.T) {
	a, _ := newZAIConnectTestApp(t, "")
	opening := a.model
	a.state = stateWorking
	connectZAIFromPanel(t, a, "plan-test-key")
	a.disconnectModelService("z-ai")
	a.settle()

	if a.model != opening || a.deferredModelServiceModel != "" {
		t.Fatalf("the disconnected service left model/deferred %q/%q", a.model, a.deferredModelServiceModel)
	}
	for _, note := range noteTexts(a) {
		if strings.Contains(note, "this conversation was on") {
			t.Fatalf("the disconnected deferred service still moved the conversation: %q", note)
		}
	}
}

func TestTheMovedModelIsTheOneTheNextLaunchOpensOn(t *testing.T) {
	a, dir := newZAIConnectTestApp(t, "")
	connectZAIFromPanel(t, a, "plan-test-key")
	want := "z-ai-direct/glm-5.3"
	if got := config.ChatModelAt(dir); got != want {
		t.Fatalf("the profile holds %q, want %q", got, want)
	}

	nextSources := config.ResolveSources(dir, "", config.DefaultBaseURL)
	next := newApp(t.Context(), Options{
		Agent:      &fakeAgent{model: config.ChatModelAt(dir)},
		Workspace:  t.TempDir(),
		ProfileDir: dir,
		Sources:    nextSources,
		Models:     func() []Model { return []Model{{ID: config.DefaultModel}} },
		SaveModel:  func(model string) error { return config.WriteChatModel(dir, model) },
	})
	next.width, next.height = 100, 30
	next.pal = newPalette(tokens.ANSI256, false)
	next.openPicker()
	chosen, ok := next.pick.choice()
	if next.model != want || !ok || chosen.ID != want {
		t.Fatalf("next launch model/picker = %q/%q (found=%t), want %q", next.model, chosen.ID, ok, want)
	}
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
		t.Fatal("the completed address did not open the name box")
	}
	// THE NAME STEP IS PART OF THE FLOW: the connection's Written word is the
	// routing prefix of every model id it qualifies, and the box opens
	// pre-filled with the host slug. A typed name is what the connection is
	// called everywhere below (the assertions derive `written` from it).
	a.connPanel.entry.box.setText("localhost")
	if cmd := a.connectEntryKey(key("enter")); cmd != nil || a.connPanel.entry == nil || !a.connPanel.entry.secret {
		t.Fatal("the completed name did not open the key box")
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

func TestAnUndocumentedListingGetsAListingServicesPickerAndCacheImmediately(t *testing.T) {
	server := sourcestub.New("glm-5.3", "glm-5.3-flash")
	defer server.Close()
	dir := t.TempDir()
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini",
		modelsource.NewSet(testDefaultService("sk-default-1234567890")), []Model{{ID: "openai/gpt-4.1-mini"}})
	installModelServiceShelf(a, dir)
	source := modelsource.Vendored()[6]
	source.Listing = modelsource.ListingNone
	draft := modelConnectDraft{source: source, row: config.PersistedSource{
		ID: "custom", Written: "localhost", Address: server.URL(), Key: "a-custom-key", Order: 1,
	}}
	msg := a.beginModelConnect(draft)().(modelConnectResultMsg)
	a.adoptModelConnectResult(msg)

	connected, ok := a.sources.ByID("custom")
	if !ok || connected.Source.Listing != modelsource.ListingModels {
		t.Fatalf("discovered listing did not become a listing service: %+v, found=%t", connected, ok)
	}
	list := a.modelList()
	for _, want := range []string{"localhost/glm-5.3", "localhost/glm-5.3-flash"} {
		found := false
		for _, row := range list {
			found = found || row.ID == want
		}
		if !found {
			t.Fatalf("picker missed %q after discovery: %+v", want, list)
		}
	}
	if cached := CachedModelsFor("custom", server.URL()); len(cached) != 2 {
		t.Fatalf("discovered listing did not reach the picker cache: %+v", cached)
	}
}

func TestAPaymentRefusalConnectsTheAuthenticatedAccount(t *testing.T) {
	server := sourcestub.New()
	defer server.Close()
	server.Listingless()
	server.RefuseCompletion(http.StatusTooManyRequests, `{"code":"1113","message":"Insufficient balance or no resource package. Please recharge."}`)
	dir := t.TempDir()
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini",
		modelsource.NewSet(testDefaultService("sk-default-1234567890")), []Model{{ID: "openai/gpt-4.1-mini"}})
	source := modelsource.Vendored()[6]
	source.Listing = modelsource.ListingNone
	source.ProbeModel = "probe-model"
	draft := modelConnectDraft{source: source, row: config.PersistedSource{
		ID: "custom", Written: "localhost", Address: server.URL(), Key: "a-custom-key", Order: 1,
	}}
	msg := a.beginModelConnect(draft)().(modelConnectResultMsg)
	a.adoptModelConnectResult(msg)

	if _, ok := a.sources.ByID("custom"); !ok {
		t.Fatal("the authenticated account was stored but did not become connected")
	}
	if got := noteSaying(t, a, "accepted the key"); got != "localhost accepted the key but the account cannot pay — Insufficient balance or no resource package. Please recharge." {
		t.Fatalf("payment connection note = %q", got)
	}
}

func TestARenameCarriesTheModelIdsAlreadyPicked(t *testing.T) {
	server := sourcestub.New("glm-5.3", "glm-5.3-flash")
	defer server.Close()
	dir := t.TempDir()
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini",
		modelsource.NewSet(testDefaultService("sk-default-1234567890")), []Model{{ID: "openai/gpt-4.1-mini"}})
	installModelServiceShelf(a, dir)
	source := modelsource.Vendored()[6]
	source.Listing = modelsource.ListingNone
	draft := modelConnectDraft{source: source, row: config.PersistedSource{
		ID: "custom", Written: "mybox", Address: server.URL(), Key: "a-custom-key", Order: 1,
	}}
	msg := a.beginModelConnect(draft)().(modelConnectResultMsg)
	a.adoptModelConnectResult(msg)

	a.switchModel("mybox/glm-5.3", 0)
	// Role pins, the fallback chain and a capability slot are picked under the
	// old name too; the rename has to carry them or they misroute at send.
	registry := config.NewSettings(config.SettingsOptions{ProfileDir: dir})
	roles, ok := registry.Row(config.KeyModelRoles)
	if !ok {
		t.Fatal("the roles row is missing")
	}
	if err := roles.Apply("planner:mybox/glm-5.3"); err != nil {
		t.Fatal(err)
	}
	fallbacks, ok := registry.Row(config.KeyModelFallbacks)
	if !ok {
		t.Fatal("the fallbacks row is missing")
	}
	if err := fallbacks.Apply("mybox/glm-5.3-flash, openai/gpt-4.1-mini"); err != nil {
		t.Fatal(err)
	}
	image, ok := registry.Row(config.ModelSettingKey("image"))
	if !ok {
		t.Fatal("the image slot row is missing")
	}
	if err := image.Apply("mybox/glm-5.3"); err != nil {
		t.Fatal(err)
	}

	// THE RENAME: an edit draft whose Written moved, exactly what the name
	// step builds on an answer that differs from the stored one.
	persisted := config.PersistedSources(dir)
	if len(persisted) != 1 {
		t.Fatalf("the connect did not persist exactly one row: %+v", persisted)
	}
	renamed := persisted[0]
	renamed.Written = "renamed-box"
	renamedDraft := modelConnectDraft{
		source:      modelsource.Source{ID: "custom", Written: "renamed-box", Listing: modelsource.ListingNone},
		row:         renamed,
		renamedFrom: "mybox",
		entryID:     modelConnectionID("custom"),
		editing:     true,
	}
	msg = a.beginModelConnect(renamedDraft)().(modelConnectResultMsg)
	a.adoptModelConnectResult(msg)

	if a.model != "renamed-box/glm-5.3" {
		t.Fatalf("the rename left the conversation on %q", a.model)
	}
	if got := config.ChatModelAt(dir); got != "renamed-box/glm-5.3" {
		t.Fatalf("the persisted slot did not follow the rename: %q", got)
	}
	if roles, _ := registry.Row(config.KeyModelRoles); roles.Value() != "planner:renamed-box/glm-5.3" {
		t.Fatalf("the role pin did not follow the rename: %q", roles.Value())
	}
	if fallbacks, _ := registry.Row(config.KeyModelFallbacks); fallbacks.Value() != "renamed-box/glm-5.3-flash, openai/gpt-4.1-mini" {
		t.Fatalf("the fallback chain did not follow the rename: %q", fallbacks.Value())
	}
	if image, _ := registry.Row(config.ModelSettingKey("image")); image.Value() != "renamed-box/glm-5.3" {
		t.Fatalf("the capability slot did not follow the rename: %q", image.Value())
	}
	// A NAME THAT WAS NEVER THE OLD ONE COMES BACK UNTOUCHED.
	if fallbacks, _ := registry.Row(config.KeyModelFallbacks); strings.Contains(fallbacks.Value(), "mybox/") {
		t.Fatalf("the old prefix survived the rename: %q", fallbacks.Value())
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
	if len(list) != 2 || !list[1].Unavailable || list[1].ID != "" || list[1].Notice != noServiceModelListWord {
		t.Fatalf("listing-less picker rows = %+v", list)
	}
	a.openPicker()
	placeholder := -1
	for at, hit := range a.pick.hits {
		if a.pick.all[hit].Unavailable {
			placeholder = at
			break
		}
	}
	if placeholder < 0 || a.pick.unfoldAt(placeholder, "", timeNow()) {
		t.Fatal("the empty-group notice reached the lane-sheet door")
	}
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
		{modelsource.Outcome{Kind: modelsource.OutcomeConnected, Listed: true, Models: 1}, "deepseek is connected · 1 model"},
		{modelsource.Outcome{Kind: modelsource.OutcomeConnected, Listed: true, Models: 10, Door: modelsource.Door{Name: "coding plan"}}, "deepseek is connected · coding plan · 10 models"},
		{modelsource.Outcome{Kind: modelsource.OutcomeConnected, Listed: true, Models: 10, Door: modelsource.Door{Name: "pay-as-you-go"}}, "deepseek is connected · pay-as-you-go · 10 models"},
		{modelsource.Outcome{
			Kind: modelsource.OutcomeConnected, Listed: true, Models: 4,
			Door: modelsource.Door{Name: "coding plan"}, PlanPaused: true, PlanReset: "18:30 UTC",
			Overflow: &modelsource.Door{Name: "pay-as-you-go", Metered: true},
		}, "deepseek is connected · coding plan · 4 models · plan paused · resets at 18:30 UTC · /connect can switch to pay-as-you-go"},
		{modelsource.Outcome{Kind: modelsource.OutcomeConnected}, "deepseek is connected"},
		{modelsource.Outcome{Kind: modelsource.OutcomeRefused, VendorSaid: "Authentication Fails, Your api key is invalid"}, "deepseek refused that key — Authentication Fails, Your api key is invalid"},
		{modelsource.Outcome{Kind: modelsource.OutcomeAccountCannotPay, VendorSaid: "Insufficient balance or no resource package. Please recharge."}, "deepseek accepted the key but the account cannot pay — Insufficient balance or no resource package. Please recharge."},
		{modelsource.Outcome{Kind: modelsource.OutcomeUnanswered}, "deepseek did not answer · nothing was saved"},
		{modelsource.Outcome{Kind: modelsource.OutcomeWrongShape}, "that is not the shape of a deepseek key — they start with sk-"},
	}
	for _, testCase := range tests {
		if got := serviceOutcomeWord("deepseek", testCase.outcome); got != testCase.want {
			t.Errorf("word = %q, want %q", got, testCase.want)
		}
	}
	for service, want := range map[string]string{
		"ollama":          "ollama is connected · 1 model",
		"something-local": "something-local is connected · 1 model",
	} {
		if got := serviceConnectedWord(service, modelsource.Outcome{Kind: modelsource.OutcomeConnected, Listed: true, Models: 1}); got != want {
			t.Errorf("one-door word = %q, want %q", got, want)
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
	if got := engineVariableWord("DEEPSEEK_API_KEY"); got != "the engine process reads $DEEPSEEK_API_KEY from its own environment" {
		t.Errorf("engine variable word = %q", got)
	}
	words := strings.Repeat("word ", 30) + "tail"
	got := truncateVendorWords(words, 120)
	if len(got) > 120 || strings.HasSuffix(got, "wor") {
		t.Fatalf("vendor words were not cut at a word boundary: %q", got)
	}
}

func TestACollidingServiceNameConnectsOnTheFirstAttemptUnderTheSuggestedName(t *testing.T) {
	plan := sourcestub.New("too-wide")
	metered := sourcestub.New("metered")
	defer plan.Close()
	defer metered.Close()
	source := modelsource.Vendored()[1]
	source.Doors[0].Address = plan.URL()
	source.Doors[1].Address = metered.URL()
	dir := t.TempDir()
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini",
		modelsource.NewSet(testDefaultService("default-test-key")),
		[]Model{{ID: "openai/gpt-4.1-mini"}, {ID: "z-ai/glm-5.3"}})
	message := a.beginModelConnect(modelConnectDraft{
		source: source,
		row: config.PersistedSource{
			ID: source.ID, Written: source.Written, Region: "intl", Key: "plan-test-key", Order: 1,
		},
	})()
	if _, follow := a.Update(message); follow != nil {
		t.Fatal("the settled connection unexpectedly started another command")
	}
	if got := noteSaying(t, a, "is connected"); got != "z-ai-direct is connected · coding plan · 4 models" {
		t.Fatalf("connection note = %q", got)
	}
}

func TestNoSourceAsksForASecondAttemptAfterAServiceNameCollision(t *testing.T) {
	err := filepath.WalkDir("../..", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), "connect this as") {
			t.Errorf("%s still asks the person to connect a colliding name twice", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestThePlanCatalogDoesNotBelieveTheWiderListing(t *testing.T) {
	plan := sourcestub.New("glm-5.3", "glm-5.3-flash", "metered-only")
	metered := sourcestub.New("metered-only")
	defer plan.Close()
	defer metered.Close()
	var source modelsource.Source
	for _, candidate := range modelsource.Vendored() {
		if candidate.ID == "z-ai" {
			source = candidate
			break
		}
	}
	source.Doors[0].Address = plan.URL()
	source.Doors[1].Address = metered.URL()
	a := modelServiceTestApp(t, t.TempDir(), "openai/gpt-4.1-mini",
		modelsource.NewSet(testDefaultService("default-test-key")), nil)
	refreshed := false
	a.serviceModelRefresh = func(context.Context, modelsource.Connected, []Model) ([]Model, error) {
		refreshed = true
		return nil, nil
	}
	message, ok := a.beginModelConnect(modelConnectDraft{
		source: source,
		row: config.PersistedSource{
			ID: source.ID, Written: source.Written, Key: "plan-test-key", Order: 1,
		},
	})().(modelConnectResultMsg)
	want := []string{"glm-5.3", "glm-5.3-flash", "glm-5.3[1m]", "glm-5.3-flash[1m]"}
	if !ok || message.err != nil || message.outcome.Door.ID != "coding-plan" ||
		!reflect.DeepEqual(message.outcome.ModelIDs, want) || refreshed {
		t.Fatalf("fixed plan catalog: message=%T door=%s models=%v refreshed=%t",
			message, message.outcome.Door.ID, message.outcome.ModelIDs, refreshed)
	}
	if len(plan.Requests()) != 1 || len(metered.Requests()) != 0 {
		t.Fatalf("catalog connection requests: plan=%d metered=%d", len(plan.Requests()), len(metered.Requests()))
	}
}

func TestAnEngineRoadConnectionSaysWhoseEnvironmentReadsItsVariable(t *testing.T) {
	base := testDefaultService("default-key")
	engine := modelServiceTestApp(t, t.TempDir(), "openai/gpt-4.1-mini", modelsource.NewSet(base), []Model{{ID: "openai/gpt-4.1-mini"}})
	engine.engineRoad = true
	msg := modelConnectResultMsg{
		service: "deepseek", written: "deepseek-direct", keyEnv: "DEEPSEEK_API_KEY",
		outcome: modelsource.Outcome{Kind: modelsource.OutcomeConnected},
	}
	engine.adoptModelConnectResult(msg)
	want := "deepseek-direct is connected · the engine process reads $DEEPSEEK_API_KEY from its own environment"
	if got := noteSaying(t, engine, "engine process reads"); got != want {
		t.Fatalf("engine-road receipt = %q, want %q", got, want)
	}

	inProcess := modelServiceTestApp(t, t.TempDir(), "openai/gpt-4.1-mini", modelsource.NewSet(base), []Model{{ID: "openai/gpt-4.1-mini"}})
	inProcess.adoptModelConnectResult(msg)
	if got := noteSaying(t, inProcess, "is connected"); got != "deepseek-direct is connected" {
		t.Fatalf("in-process receipt grew an engine clause: %q", got)
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
		source: modelsource.Vendored()[6],
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
			for _, request := range directServer.Requests() {
				if request.Method != "POST" || !strings.HasSuffix(request.Path, modelsource.ChatCompletionsPath) {
					t.Fatalf("direct service received lane-sheet or probe traffic: %+v", directServer.Requests())
				}
			}
			if got := completionRequests(defaultServer); len(got) != 0 {
				t.Fatalf("default service received the direct turn: %+v", got)
			}
			return
		}
	}
	t.Fatal("the direct model was not in the picker")
}

func TestATurnOnAConnectedServiceSendsWithNoDefaultProviderKey(t *testing.T) {
	for _, pin := range []string{config.APIKeyEnv, "OPENAI_API_KEY"} {
		t.Setenv(pin, "")
	}
	dir := t.TempDir()
	base := testDefaultService("")
	direct := testDirectService("https://direct.example/v1")
	sources := modelsource.NewSet(base, direct)
	agent := &fakeAgent{model: "deepseek-direct/deepseek-v4-pro"}
	a := modelServiceTestAppWithAgent(t, dir, agent.model, sources, nil, agent)
	a.routerConnect = func(context.Context) (OpenRouterFlow, error) { return nil, nil }
	a.input.setText("hello")
	runSubmit(t, a.enter())

	if len(agent.sent) != 1 || agent.sent[0] != "hello" {
		t.Fatalf("connected service was sent %v, want the turn", agent.sent)
	}
	if a.setup.open {
		t.Fatal("enter opened the default provider over a connected service")
	}
	for _, line := range noteTexts(a) {
		if strings.Contains(line, "openrouter is not connected") {
			t.Fatalf("connected service drew the default-provider note: %q", line)
		}
	}
}

func TestAConnectedServiceKeepsSetupSilentAboutTheDefaultProvider(t *testing.T) {
	for _, pin := range []string{config.APIKeyEnv, "OPENAI_API_KEY", "CODEAF_DAILY_BUDGET"} {
		t.Setenv(pin, "")
	}
	dir := t.TempDir()
	base := testDefaultService("")
	direct := testDirectService("https://direct.example/v1")
	a := newApp(t.Context(), Options{
		Agent:      &fakeAgent{model: "deepseek-direct/deepseek-v4-pro"},
		Workspace:  t.TempDir(),
		ProfileDir: dir,
		Sources:    modelsource.NewSet(base, direct),
		Setup:      true,
		ConnectOpenRouter: func(context.Context) (OpenRouterFlow, error) {
			return nil, nil
		},
	})
	if !a.setup.open || len(a.setup.steps) != 1 || a.setup.step() != setupControls {
		t.Fatalf("the direct-only setup asked %+v, want only controls", a.setup.steps)
	}
	pressSetup(a, key("esc"))
	for _, line := range noteTexts(a) {
		if strings.Contains(line, "openrouter") {
			t.Fatalf("direct-only setup left an OpenRouter note: %q", line)
		}
	}
}

func TestAKeyOptionalServiceCarriesATurnWithNoDefaultProviderKey(t *testing.T) {
	for _, pin := range []string{config.APIKeyEnv, "OPENAI_API_KEY"} {
		t.Setenv(pin, "")
	}
	base := testDefaultService("")
	local := modelsource.Connected{
		Source:  modelsource.Source{ID: "ollama", Written: "ollama", KeyOptional: true},
		Address: "http://localhost:11434/v1",
	}
	agent := &fakeAgent{model: "ollama/llama3.2"}
	a := modelServiceTestAppWithAgent(t, t.TempDir(), agent.model, modelsource.NewSet(base, local), nil, agent)
	a.routerConnect = func(context.Context) (OpenRouterFlow, error) { return nil, nil }
	a.input.setText("hello locally")
	runSubmit(t, a.enter())
	if len(agent.sent) != 1 || agent.sent[0] != "hello locally" || a.setup.open {
		t.Fatalf("key-optional service sent %v with setup open=%v", agent.sent, a.setup.open)
	}
}

func TestATurnOnTheDefaultServiceStillOpensSetupWithNoDefaultProviderKey(t *testing.T) {
	for _, pin := range []string{config.APIKeyEnv, "OPENAI_API_KEY"} {
		t.Setenv(pin, "")
	}
	dir := t.TempDir()
	base := testDefaultService("")
	direct := testDirectService("https://direct.example/v1")
	agent := &fakeAgent{model: config.DefaultModel}
	a := modelServiceTestAppWithAgent(t, dir, agent.model, modelsource.NewSet(base, direct), nil, agent)
	a.routerConnect = func(context.Context) (OpenRouterFlow, error) { return nil, nil }
	a.input.setText("keep these words")

	if cmd := a.enter(); cmd != nil {
		t.Fatal("the missing default provider submitted a turn")
	}
	if !a.setup.open || a.setup.step() != setupKey {
		t.Fatalf("the default provider's setup did not open: %+v", a.setup)
	}
	if len(agent.sent) != 0 || a.input.String() != "keep these words" {
		t.Fatalf("the gate sent %v and kept draft %q", agent.sent, a.input.String())
	}
}

func TestDirectOnlyCommandsInventNoDefaultProviderUsage(t *testing.T) {
	for _, pin := range []string{config.APIKeyEnv, "OPENAI_API_KEY"} {
		t.Setenv(pin, "")
	}
	base := testDefaultService("")
	direct := testDirectService("https://direct.example/v1")
	a := modelServiceTestApp(t, t.TempDir(), "deepseek-direct/deepseek-v4-pro",
		modelsource.NewSet(base, direct), []Model{{ID: config.DefaultModel}})
	a.sourceModels[direct.Source.ID] = []Model{{ID: "deepseek-v4-pro"}}

	for _, command := range []string{"/status", "/cost"} {
		a.slash(command)
		answer := strings.ToLower(lastNote(t, a))
		for _, invented := range []string{"$0.00", "0 tok", "openrouter"} {
			if strings.Contains(answer, invented) {
				t.Fatalf("%s invented %q in %q", command, invented, answer)
			}
		}
	}
	a.openPicker()
	rendered := plain(strings.Join(a.pick.rows(100, a.pick.height(100), a.pal, -1, a.reasoningFor), "\n"))
	if !strings.Contains(rendered, "deepseek-direct") || !strings.Contains(rendered, "deepseek-direct/deepseek-v4-pro") {
		t.Fatalf("/model lost the connected service and its model:\n%s", rendered)
	}
	if !strings.Contains(rendered, "openrouter") || !strings.Contains(rendered, config.DefaultModel) {
		t.Fatalf("/model lost the keyless default service's existing group or model:\n%s", rendered)
	}
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

func TestThePlanDoorSaysItsPositionAndDefaultsToWait(t *testing.T) {
	dir := t.TempDir()
	row := config.PersistedSource{
		ID: "z-ai", Written: "z-ai", Key: "plan-test-key", Door: "coding-plan", Order: 1,
	}
	if err := config.WriteSources(dir, []config.PersistedSource{row}); err != nil {
		t.Fatal(err)
	}
	var source modelsource.Source
	for _, candidate := range modelsource.Vendored() {
		if candidate.ID == "z-ai" {
			source = candidate
			break
		}
	}
	if len(source.Doors) != 2 {
		t.Fatal("the z-ai billing doors are missing")
	}
	overflow := source.Doors[1]
	connected := modelsource.Connected{
		Source: source, Key: row.Key, Address: source.Doors[0].Address,
		Door: source.Doors[0], Overflow: &overflow, PlanPaused: config.PlanPausedWait,
	}
	sources := modelsource.NewSet(testDefaultService("default-test-key"), connected)
	rows := modelServiceRows(dir, sources)
	if len(rows) != 2 || rows[0].name != "z-ai" ||
		!strings.Contains(rows[0].value, "coding plan") ||
		!strings.Contains(rows[0].value, "Zhipu lists the tools its plan covers; codeaf is not listed, and its request has been drafted but not sent.") ||
		rows[1].name != "when the plan is paused" || rows[1].value != config.PlanPausedWait {
		t.Fatalf("the plan rows do not say their billing position: count=%d", len(rows))
	}

	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini", sources, nil)
	a.raiseSettings()
	a.cyclePlanPause("z-ai")
	written := config.PersistedSources(dir)
	if len(written) != 1 || written[0].PlanPaused != config.PlanPausedUseMeter {
		t.Fatal("the plan-pause setting did not opt in to metered overflow")
	}
	a.cyclePlanPause("z-ai")
	written = config.PersistedSources(dir)
	if len(written) != 1 || written[0].PlanPaused != "" {
		t.Fatal("the default wait value was not stored as the default")
	}
}

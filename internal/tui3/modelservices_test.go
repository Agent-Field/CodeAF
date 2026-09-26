package tui3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	modelcatalog "github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/connect"
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
			HTTPClient: config.CatalogHTTPClient(service),
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

type panelCodexFlow struct {
	url       string
	tokens    codexauth.Tokens
	cancelled bool
}

func (f *panelCodexFlow) URL() string { return f.url }

func (f *panelCodexFlow) Wait(context.Context) (codexauth.Tokens, error) {
	return f.tokens, nil
}

func (f *panelCodexFlow) Cancel() { f.cancelled = true }

func TestC12C18ConnectCodexBrowserRowUsesTheRealPanelAndMovesToTheListedModel(t *testing.T) {
	// C12: the real /connect row adopts the account list and moves to codex/gpt-5.5.
	// C18: the panel never asks for or displays a token; the backend sees only the real bearer.
	dir := t.TempDir()
	var authorization string
	requests := 0
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		authorization = request.Header.Get("Authorization")
		if request.URL.Path != "/models" || request.URL.Query().Get("client_version") == "" {
			t.Fatalf("panel listing request = %s", request.URL.String())
		}
		if authorization != "Bearer panel-access" || authorization == "Bearer "+codexauth.Sentinel {
			t.Fatalf("panel listing authorization = %q", authorization)
		}
		if request.Header.Get("chatgpt-account-id") != "acct-panel" || request.Header.Get("originator") != codexauth.Originator {
			t.Fatalf("panel listing account headers = %v", request.Header)
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"models": []any{
			map[string]any{"slug": "gpt-5.5", "visibility": "list"},
			map[string]any{"slug": "hidden", "visibility": "hide"},
		}})
	}))
	defer backend.Close()
	t.Setenv("CODEAF_CODEX_BACKEND", backend.URL)

	a := modelServiceTestApp(t, dir, "~deepseek/deepseek-v4-flash-latest",
		modelsource.NewSet(testDefaultService("sk-default-1234567890")),
		[]Model{{ID: "~deepseek/deepseek-v4-flash-latest"}})
	flow := &panelCodexFlow{
		url: "https://auth.example/authorize?state=panel",
		tokens: codexauth.Tokens{
			AccessToken: "panel-access", RefreshToken: "panel-refresh", IDToken: "panel-identity",
			AccountID: "acct-panel", Email: "person@example.com", Plan: "pro", ExpiresAt: time.Now().Add(time.Hour),
		},
	}
	a.codexConnect = func(context.Context) (CodexFlow, error) { return flow, nil }
	opened := ""
	wasOpener := processOpener
	processOpener = func(target string) error { opened = target; return nil }
	t.Cleanup(func() { processOpener = wasOpener })

	a.openConnect()
	rowAt := -1
	var row connect.Status
	for at := range a.connPanel.hits {
		candidate, ok := a.connPanel.at(at)
		if ok && candidate.ID == modelConnectionID("codex") {
			rowAt, row = at, candidate
			break
		}
	}
	if rowAt < 0 || row.Name != "Codex" || modelServiceTag(row) != "browser" {
		t.Fatalf("codex row = %+v at %d", row, rowAt)
	}
	begin := a.connectAct(rowAt)
	if begin == nil || a.connPanel.open {
		t.Fatal("enter on Codex did not leave the panel for the browser road")
	}
	_, wait := a.Update(begin())
	if wait == nil || opened != flow.url || len(a.entries) == 0 || !a.connectLinkable(len(a.entries)-1) {
		t.Fatalf("waiting card/opened = %v/%q entries=%d", wait != nil, opened, len(a.entries))
	}
	if _, ok := a.connectLinkPress(len(a.entries) - 1); !ok || !a.entries[len(a.entries)-1].conn.copied {
		t.Fatal("the Codex waiting card did not expose the copy affordance")
	}
	if _, follow := a.Update(wait()); follow != nil {
		t.Fatal("the settled Codex connection unexpectedly started another command")
	}
	if requests != 1 || authorization != "Bearer panel-access" {
		t.Fatalf("panel listing requests/authorization = %d/%q", requests, authorization)
	}
	if a.model != "codex/gpt-5.5" || a.agent.Model() != "codex/gpt-5.5" {
		t.Fatalf("panel left model at %q/%q", a.model, a.agent.Model())
	}
	var card *connectCard
	var cardEntry *entry
	for index := len(a.entries) - 1; index >= 0; index-- {
		if a.entries[index].conn != nil && a.entries[index].conn.service == "codex" {
			card, cardEntry = a.entries[index].conn, &a.entries[index]
			break
		}
	}
	if card == nil || card.state != connectConnected || card.result != "codex connected · person@example.com · pro plan" {
		t.Fatalf("settled browser card = %+v", card)
	}
	if strings.Contains(plain(strings.Join(a.connectRows(cardEntry, 100), "\n")), codexauth.Sentinel) {
		t.Fatal("the sentinel appeared on the settled panel card")
	}
	if !flow.cancelled {
		t.Fatal("the settled browser road left its callback listener open")
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
	// ENTER ON A CONNECTED SERVICE OPENS ITS FOUR-ACTION MENU; "change key" is
	// the third verb, and it is the road to the key box.
	drive(t, a, key("down"), key("down"), key("enter"))
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
		t.Fatal("the models group did not contain Custom OpenAI-compatible API")
	}
	a.connPanel.cursor = rowAt
	if cmd := a.connectAct(rowAt); cmd != nil || a.connPanel.entry == nil {
		t.Fatal("enter on Custom OpenAI-compatible API did not open the address box")
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
	firstRowsReadHome(t, a)
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
	source := testModelSource(t, modelsource.CustomID)
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
	source := testModelSource(t, modelsource.CustomID)
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
	source := testModelSource(t, modelsource.CustomID)
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
	// THE CREW'S TIER ROWS are stored ids under the old name too, and the one
	// the rename has to carry or the planner and the worker keep answering on
	// a service that no longer exists. The level suffix is the row's own
	// notation and moves with the id.
	worker, ok := registry.Row(config.KeyTierWorkerModel)
	if !ok {
		t.Fatal("the worker tier row is missing")
	}
	if err := worker.Apply("mybox/glm-5.3:low"); err != nil {
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
	if got := config.TierModelAt(dir, config.ModelTierWorker); got != "renamed-box/glm-5.3:low" {
		t.Fatalf("the crew's worker tier did not follow the rename: %q", got)
	}
	// A NAME THAT WAS NEVER THE OLD ONE COMES BACK UNTOUCHED.
	if fallbacks, _ := registry.Row(config.KeyModelFallbacks); strings.Contains(fallbacks.Value(), "mybox/") {
		t.Fatalf("the old prefix survived the rename: %q", fallbacks.Value())
	}
}

// groupedModelIDs is every id the picker shows under one connection's group,
// which is the qualified spelling the person actually selects (modelsFor is
// the single door that calls Connected.Qualify).
func groupedModelIDs(a *app, group string) []string {
	var ids []string
	for _, model := range a.modelList() {
		if !model.Unavailable && strings.EqualFold(model.Group, group) {
			ids = append(ids, model.ID)
		}
	}
	return ids
}

// renamedRelistedApp connects a custom connection written `homelab` whose stub
// lists the one model, renames it to `lab`, and lists it AGAIN from the renamed
// connection — the road ctrl+r on the row and a reconnect both take
// ([app.reconnectModelService] -> [app.beginModelConnect]). The re-listing is
// the moment nothing covered before: a listing path that qualified an id that
// was already qualified would draw lab/homelab/... or lab/lab/....
func renamedRelistedApp(t *testing.T, model string) *app {
	t.Helper()
	server := sourcestub.New(model)
	t.Cleanup(server.Close)
	dir := t.TempDir()
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini",
		modelsource.NewSet(testDefaultService("sk-default-1234567890")), []Model{{ID: "openai/gpt-4.1-mini"}})
	installModelServiceShelf(a, dir)

	source := testModelSource(t, modelsource.CustomID)
	first := modelConnectDraft{source: source, row: config.PersistedSource{
		ID: "custom", Written: "homelab", Address: server.URL(), Key: "a-custom-key", Order: 1,
	}}
	a.adoptModelConnectResult(a.beginModelConnect(first)().(modelConnectResultMsg))
	a.switchModel("homelab/"+model, 0)

	// THE RENAME: an edit draft whose Written moved, exactly what the name step
	// builds on an answer that differs from the stored one.
	persisted := config.PersistedSources(dir)
	if len(persisted) != 1 {
		t.Fatalf("the connect did not persist exactly one row: %+v", persisted)
	}
	renamed := persisted[0]
	renamed.Written = "lab"
	renamedSource := source
	renamedSource.Written = "lab"
	renamedDraft := modelConnectDraft{
		source: renamedSource, row: renamed, renamedFrom: "homelab",
		entryID: modelConnectionID("custom"), editing: true,
	}
	a.adoptModelConnectResult(a.beginModelConnect(renamedDraft)().(modelConnectResultMsg))
	if a.model != "lab/"+model {
		t.Fatalf("the rename left the conversation on %q", a.model)
	}

	// THE RE-LISTING: the row's own road, carrying no renamedFrom at all.
	cmd := a.reconnectModelService("custom")
	if cmd == nil {
		t.Fatal("the renamed connection did not offer a re-listing")
	}
	a.adoptModelConnectResult(cmd().(modelConnectResultMsg))
	return a
}

// A RE-LISTING AFTER A RENAME CARRIES THE PREFIX ONCE. The written name is the
// routing prefix of every model id the connection qualifies, so a re-listing
// must draw the id the connection's own name spells — never the previous name
// and never its own name twice.
func TestReListingARenamedConnectionCarriesOnePrefix(t *testing.T) {
	a := renamedRelistedApp(t, "qwen-local")

	ids := groupedModelIDs(a, "lab")
	if len(ids) != 1 || ids[0] != "lab/qwen-local" {
		t.Fatalf("the re-listed group = %q, want exactly [lab/qwen-local]", ids)
	}
	for _, id := range ids {
		if id == "lab/homelab/qwen-local" || id == "lab/lab/qwen-local" || strings.Count(id, "/") != 1 {
			t.Fatalf("the re-listing qualified the prefix more than once: %q", id)
		}
	}
	if a.model != "lab/qwen-local" {
		t.Fatalf("the conversation model after the re-listing = %q, want lab/qwen-local", a.model)
	}
}

// THE STORED LIST HOLDS THE BARE IDS THE SERVER ANSWERED. The prefix is added
// at ONE door ([app.modelsFor] calling Connected.Qualify) and must not be baked
// into the cache: a cache that stored lab/qwen-local would be qualified again
// on the next read and read lab/lab/qwen-local.
func TestReListingStoresTheBareModelIDsTheServerAnswered(t *testing.T) {
	a := renamedRelistedApp(t, "qwen-local")

	got := a.sourceModels["custom"]
	if len(got) != 1 || got[0].ID != "qwen-local" {
		t.Fatalf("the stored list = %+v, want the bare qwen-local the server answered", got)
	}
}

// TWO CONNECTIONS DO NOT BORROW EACH OTHER'S PREFIX. Each connection's own
// listing is qualified with its OWN written name, including a bare id whose
// spelling could be read as the other connection's prefix.
func TestTwoConnectionsDoNotBorrowEachOthersPrefix(t *testing.T) {
	labServer := sourcestub.New("qwen-local", "shared")
	defer labServer.Close()
	studioServer := sourcestub.New("qwen-local", "shared")
	defer studioServer.Close()
	dir := t.TempDir()
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini",
		modelsource.NewSet(testDefaultService("sk-default-1234567890")), []Model{{ID: "openai/gpt-4.1-mini"}})
	installModelServiceShelf(a, dir)

	template := testModelSource(t, modelsource.CustomID)
	labSource := template
	labSource.ID, labSource.Written = "custom", "lab"
	a.adoptModelConnectResult(a.beginModelConnect(modelConnectDraft{source: labSource, row: config.PersistedSource{
		ID: "custom", Written: "lab", Address: labServer.URL(), Key: "a-custom-key", Order: 1,
	}})().(modelConnectResultMsg))
	studioSource := template
	studioSource.ID, studioSource.Written = "custom-studio", "studio"
	a.adoptModelConnectResult(a.beginModelConnect(modelConnectDraft{source: studioSource, row: config.PersistedSource{
		ID: "custom-studio", Written: "studio", Address: studioServer.URL(), Key: "a-custom-key2", Order: 2,
	}})().(modelConnectResultMsg))

	for _, want := range []string{"lab/qwen-local", "lab/shared", "studio/qwen-local", "studio/shared"} {
		found := false
		for _, model := range a.modelList() {
			found = found || model.ID == want
		}
		if !found {
			t.Fatalf("the picker missed %q: %+v", want, a.modelList())
		}
	}
	for _, model := range a.modelList() {
		if strings.HasPrefix(model.ID, "lab/studio/") || strings.HasPrefix(model.ID, "studio/lab/") {
			t.Fatalf("a connection borrowed the other's prefix: %q", model.ID)
		}
	}
}

// A RENAME DURING A WORKING TURN FREEZES THE LIVE PICK AND CARRIES THE PENDING
// MOVE UNDER THE NEW NAME. reprefixRenamedModel leaves a.model alone while a
// turn is working — the answering turn is frozen to its model until it settles
// — and rewrites the deferred move instead, spending it through
// applyDeferredModelServiceMove once the turn settles. The pending move
// follows the rename under its new name (homelab/b becomes lab/b) and the
// frozen live pick does not displace it: the move is what the person last
// asked for, and the re-spelled live pick may claim the slot only when it is
// empty.
func TestARenameWhileATurnIsWorkingCarriesThePendingMoveUnderTheNewName(t *testing.T) {
	server := sourcestub.New("glm-5.3", "glm-5.3-flash")
	defer server.Close()
	dir := t.TempDir()
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini",
		modelsource.NewSet(testDefaultService("sk-default-1234567890")), []Model{{ID: "openai/gpt-4.1-mini"}})
	installModelServiceShelf(a, dir)
	source := testModelSource(t, modelsource.CustomID)
	source.Listing = modelsource.ListingNone
	draft := modelConnectDraft{source: source, row: config.PersistedSource{
		ID: "custom", Written: "homelab", Address: server.URL(), Key: "a-custom-key", Order: 1,
	}}
	msg := a.beginModelConnect(draft)().(modelConnectResultMsg)
	a.adoptModelConnectResult(msg)

	a.model = "homelab/a"
	a.state = stateWorking
	a.deferredModelServiceModel = "homelab/b"

	// THE RENAME: an edit draft whose Written moved, exactly what the name
	// step builds on an answer that differs from the stored one.
	persisted := config.PersistedSources(dir)
	if len(persisted) != 1 {
		t.Fatalf("the connect did not persist exactly one row: %+v", persisted)
	}
	renamed := persisted[0]
	renamed.Written = "lab"
	renamedDraft := modelConnectDraft{
		source:      modelsource.Source{ID: "custom", Written: "lab", Listing: modelsource.ListingNone},
		row:         renamed,
		renamedFrom: "homelab",
		entryID:     modelConnectionID("custom"),
		editing:     true,
	}
	msg = a.beginModelConnect(renamedDraft)().(modelConnectResultMsg)
	a.adoptModelConnectResult(msg)

	// THE LIVE PICK IS FROZEN while the turn works, and the pending move
	// follows the rename under the new name: the earlier deferred id
	// (homelab/b) was re-spelled to lab/b, and the frozen live pick does not
	// displace it.
	if a.model != "homelab/a" {
		t.Fatalf("the rename moved the working conversation's model to %q", a.model)
	}
	if a.deferredModelServiceModel != "lab/b" {
		t.Fatalf("the pending move did not follow the rename: %q", a.deferredModelServiceModel)
	}

	// THE TURN SETTLES: the deferred move is spent, and the conversation lands
	// on its own model under the new name.
	a.state = stateIdle
	a.applyDeferredModelServiceMove()
	if !strings.HasPrefix(a.model, "lab/") {
		t.Fatalf("the settled conversation stayed on %q", a.model)
	}
	if a.deferredModelServiceModel != "" {
		t.Fatalf("the pending move was not spent: %q", a.deferredModelServiceModel)
	}
}

// A RENAME DURING A WORKING TURN WITH NO MOVE PENDING DEFERS THE RE-SPELLED
// LIVE PICK: the answering turn is frozen to its model until it settles, so
// the slot — empty here — takes the conversation's own model under the new
// name and spends it when the turn settles, never leaving the settled
// conversation on a dead old-name id.
func TestARenameWhileATurnIsWorkingAndNoMoveIsPendingDefersTheRespelledLivePick(t *testing.T) {
	server := sourcestub.New("glm-5.3", "glm-5.3-flash")
	defer server.Close()
	dir := t.TempDir()
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini",
		modelsource.NewSet(testDefaultService("sk-default-1234567890")), []Model{{ID: "openai/gpt-4.1-mini"}})
	installModelServiceShelf(a, dir)
	source := testModelSource(t, modelsource.CustomID)
	source.Listing = modelsource.ListingNone
	draft := modelConnectDraft{source: source, row: config.PersistedSource{
		ID: "custom", Written: "homelab", Address: server.URL(), Key: "a-custom-key", Order: 1,
	}}
	msg := a.beginModelConnect(draft)().(modelConnectResultMsg)
	a.adoptModelConnectResult(msg)

	a.model = "homelab/a"
	a.state = stateWorking

	persisted := config.PersistedSources(dir)
	if len(persisted) != 1 {
		t.Fatalf("the connect did not persist exactly one row: %+v", persisted)
	}
	renamed := persisted[0]
	renamed.Written = "lab"
	msg = a.beginModelConnect(modelConnectDraft{
		source:      modelsource.Source{ID: "custom", Written: "lab", Listing: modelsource.ListingNone},
		row:         renamed,
		renamedFrom: "homelab",
		entryID:     modelConnectionID("custom"),
		editing:     true,
	})().(modelConnectResultMsg)
	a.adoptModelConnectResult(msg)

	// THE SLOT WAS EMPTY, so the re-spelled live pick takes it: the frozen
	// turn still answers on homelab/a, and the settle moves the conversation
	// to the same model under the new name instead of leaving it resolving to
	// a dead one.
	if a.model != "homelab/a" {
		t.Fatalf("the rename moved the working conversation's model to %q", a.model)
	}
	if a.deferredModelServiceModel != "lab/a" {
		t.Fatalf("the empty slot did not take the re-spelled live pick: %q", a.deferredModelServiceModel)
	}

	a.state = stateIdle
	a.applyDeferredModelServiceMove()
	if a.model != "lab/a" {
		t.Fatalf("the settled conversation stayed on %q", a.model)
	}
	if a.deferredModelServiceModel != "" {
		t.Fatalf("the deferred pick was not spent: %q", a.deferredModelServiceModel)
	}
}

// A RENAME DURING A WORKING TURN DOES NOT EAT A PENDING SWITCH TO ANOTHER
// CONNECTION. The person pressed enter on the switcher row, was told the move
// to the second connection waits for the turn to finish, and only then renamed
// the connection this conversation is on. The re-spelling of the live pick is
// bookkeeping — the pending move is what was last asked for and promised out
// loud — so the deferred slot keeps it and the settle spends it. Rewriting the
// slot here would drop the promised move silently: nothing else records it.
func TestARenameDuringAWorkingTurnLeavesAPendingSwitchAlone(t *testing.T) {
	mybox := sourcestub.New("a", "a-flash")
	defer mybox.Close()
	homelab := sourcestub.New("b", "b-flash")
	defer homelab.Close()
	dir := t.TempDir()
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini",
		modelsource.NewSet(testDefaultService("sk-default-1234567890")), []Model{{ID: "openai/gpt-4.1-mini"}})
	installModelServiceShelf(a, dir)
	source := testModelSource(t, modelsource.CustomID)
	source.Listing = modelsource.ListingNone

	// TWO CONNECTIONS, minted the way the surface mints them: the first keeps
	// the vendored id, the second takes custom-homelab.
	first := config.PrepareCustomSource(dir, mybox.URL(), "mybox")
	first.Key = "a-custom-key"
	msg := a.beginModelConnect(modelConnectDraft{source: source, row: first})().(modelConnectResultMsg)
	a.adoptModelConnectResult(msg)
	second := config.PrepareCustomSource(dir, homelab.URL(), "homelab")
	second.Key = "another-custom-key"
	msg = a.beginModelConnect(modelConnectDraft{source: source, row: second})().(modelConnectResultMsg)
	a.adoptModelConnectResult(msg)
	if second.ID == first.ID {
		t.Fatalf("both connections minted the id %q", second.ID)
	}

	// A WORKING TURN on mybox, with the promised move to homelab pending.
	a.model = "mybox/a"
	a.state = stateWorking
	a.deferredModelServiceModel = "homelab/b"

	// THE RENAME of the connection the conversation is on: an edit draft whose
	// Written moved, exactly what the name step builds.
	renamed, found := config.PersistedSource{}, false
	for _, row := range config.PersistedSources(dir) {
		if row.ID == first.ID {
			renamed, found = row, true
		}
	}
	if !found {
		t.Fatalf("the first connection did not persist: %+v", config.PersistedSources(dir))
	}
	renamed.Written = "newname"
	msg = a.beginModelConnect(modelConnectDraft{
		source:      modelsource.Source{ID: first.ID, Written: "newname", Listing: modelsource.ListingNone},
		row:         renamed,
		renamedFrom: "mybox",
		entryID:     modelConnectionID(first.ID),
		editing:     true,
	})().(modelConnectResultMsg)
	a.adoptModelConnectResult(msg)

	if a.deferredModelServiceModel != "homelab/b" {
		t.Fatalf("the rename overwrote the pending move with %q", a.deferredModelServiceModel)
	}
	if a.model != "mybox/a" {
		t.Fatalf("the rename moved the working conversation's model to %q", a.model)
	}

	// THE TURN SETTLES: the conversation lands on what the person asked for,
	// the second connection, and not on the re-spelled current model.
	a.state = stateIdle
	a.applyDeferredModelServiceMove()
	if a.model != "homelab/b" {
		t.Fatalf("the settled conversation landed on %q, not the pending move", a.model)
	}
	if a.deferredModelServiceModel != "" {
		t.Fatalf("the pending move was not spent: %q", a.deferredModelServiceModel)
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
	// THE ROWS ARE ALPHABETICAL, which is every table's opening order on this
	// surface (pickersort.go) — so `gpt-5-classic` stands above the model in use.
	// The mark is still on the model in use, which is what this test is about.
	want := "  gpt-5-classic\n" +
		"› openai/gpt-4.1-mini                                                                             1M\n" +
		"  + add a provider"
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
	if placeholder < 0 || a.pick.unfoldAt(placeholder, timeNow()) {
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
	if got := serviceStrandedWord("deepseek-direct/deepseek-v4-pro"); got != "this conversation was on deepseek-direct/deepseek-v4-pro and nothing else here can take it · connect a provider or pick a model" {
		t.Errorf("stranded word = %q", got)
	}
	if got := engineVariableWord("DEEPSEEK_API_KEY"); got != "the engine process reads $DEEPSEEK_API_KEY from its own environment" {
		t.Errorf("engine variable word = %q", got)
	}
	words := strings.Repeat("word ", 30) + "tail"
	got := config.ConnectionDetailWords(words, 120)
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
			want: "this conversation was on deepseek-direct/deepseek-v4-pro and nothing else here can take it · connect a provider or pick a model",
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
		source: testModelSource(t, modelsource.CustomID),
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
	// THE EMPTY PROFILE KEEPS THE DOOR AND DRAWS NOTHING ELSE: no services head,
	// no switcher, no connection row — but the add row stands, because a profile
	// with no custom connection yet is the one that needs the door (customAddRow).
	addRows, connectionRows := 0, 0
	for _, item := range a.sheet.items {
		if item.head == "providers" {
			t.Fatal("an empty profile drew the providers head")
		}
		if item.service != nil && item.service.switcher {
			t.Fatal("an empty profile drew the switcher row")
		}
		if item.service != nil && !item.service.addCustom {
			connectionRows++
		}
		if item.service != nil && item.service.addCustom {
			addRows++
		}
	}
	if addRows != 1 || connectionRows != 0 {
		t.Fatalf("an empty profile drew add=%d connection=%d rows, want one add row and no connection row", addRows, connectionRows)
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
		foundHead = foundHead || item.head == "providers"
		if item.row.Key == config.KeyAPIKey {
			keyAt = at
		}
		if item.head == "providers" {
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
	if keyAt < 0 || headAt != keyAt+1 || rowAt != headAt+2 {
		t.Fatalf("the providers section is not immediately under the openrouter key: key=%d head=%d row=%d", keyAt, headAt, rowAt)
	}
	// THE DEFAULT PROVIDER LEADS THE SECTION: the head, then openrouter with
	// its address and key status, then the persisted connections.
	defaultRow := a.sheet.items[headAt+1].service
	if defaultRow == nil || defaultRow.id != modelsource.DefaultID {
		t.Fatalf("the default provider does not lead the section: %+v", a.sheet.items[headAt+1])
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

// A MOVE A TURN SPENT MUST BE A MOVE ITS PANELS SEE: the deferred switch is
// recorded while the turn works (moveConversationOrDefer) and spent when it
// settles (applyDeferredModelServiceMove), and the /connect panel's switch
// row reads the conversation's model to say what it answers on
// (switchReading) — so a move that lands under an open panel would leave the
// row naming the old target until something else redrew it.
func TestSpendingADeferredMoveReadoptsTheOpenConnectPanelSoItsSwitchRowFollowsTheMove(t *testing.T) {
	dir := t.TempDir()
	customs := connectionWriteSources(t, dir, "homelab")
	sources := modelsource.NewSet(append([]modelsource.Connected{connectionDefaultService()}, customs...)...)
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini", sources, []Model{{ID: config.DefaultModel}})
	a.modelsForService = func(service modelsource.Connected) []Model {
		return []Model{{ID: "qwen-local"}}
	}
	a.state = stateWorking
	a.openConnect()
	row, _, ok := connectionFindRow(t, a, connectionSwitchRowID)
	if !ok {
		t.Fatal("the /connect panel drew no active-connection row with a custom connection connected")
	}
	if !strings.Contains(row.Blurb, "answering on openrouter") {
		t.Fatalf("the panel's switch row before the switch said %q", row.Blurb)
	}
	// The switch is pressed while the turn works: the move waits in the
	// deferred slot (moveConversationOrDefer) and the row keeps naming the
	// service the conversation is still answering on.
	a.moveConversationOrDefer("homelab/qwen-local", "homelab")
	if a.deferredModelServiceModel != "homelab/qwen-local" || a.model != "openai/gpt-4.1-mini" {
		t.Fatalf("the working switch did not defer: model %q, deferred %q", a.model, a.deferredModelServiceModel)
	}
	// The settling turn is idle before it spends the move (app.settle), so the
	// move here goes through the road a spent move really takes.
	a.state = stateIdle
	a.applyDeferredModelServiceMove()
	if a.model != "homelab/qwen-local" || a.deferredModelServiceModel != "" {
		t.Fatalf("the deferred move did not spend: model %q, deferred %q", a.model, a.deferredModelServiceModel)
	}
	row, _, ok = connectionFindRow(t, a, connectionSwitchRowID)
	if !ok {
		t.Fatal("the /connect panel drew no active-connection row after the move")
	}
	if !strings.Contains(row.Blurb, "answering on homelab") || strings.Contains(row.Blurb, "answering on openrouter") {
		t.Fatalf("the panel's switch row did not follow the spent move: %q", row.Blurb)
	}
}

// A SETTLE WITH NOTHING TO SPEND LEAVES THE PANEL WHERE IT WAS:
// applyDeferredModelServiceMove runs on every settled turn (app.settle), and
// its refresh is for the panels to follow a move that actually happened. With
// no deferred move the spend is a no-op, so the open /connect panel must not
// be re-adopted: a re-adopt re-ranks the list — cursor and scroll back to the
// top (connectPanel.rank) — and clears the second-enter disconnect
// confirmation standing on a row (connectPanel.adopt clears armed).
func TestASettleWithNothingToSpendLeavesTheConnectPanelWhereItWas(t *testing.T) {
	dir := t.TempDir()
	customs := connectionWriteSources(t, dir, "homelab")
	sources := modelsource.NewSet(append([]modelsource.Connected{connectionDefaultService()}, customs...)...)
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini", sources, []Model{{ID: config.DefaultModel}})
	a.openConnect()
	row, _, ok := connectionFindRow(t, a, "custom")
	if !ok {
		t.Fatal("the /connect panel drew no row for the connected custom connection")
	}
	if len(a.connPanel.hits) < 2 {
		t.Fatalf("the /connect panel drew %d rows, want at least 2", len(a.connPanel.hits))
	}
	// The cursor sits on the LAST row and the connected row carries an armed
	// disconnect: both are positions a re-adopt loses.
	a.connPanel.cursor = len(a.connPanel.hits) - 1
	a.connPanel.armed = row.ID

	a.applyDeferredModelServiceMove()

	if a.connPanel.cursor != len(a.connPanel.hits)-1 {
		t.Fatalf("a settle with nothing to spend moved the panel's cursor to %d", a.connPanel.cursor)
	}
	if a.connPanel.armed != row.ID {
		t.Fatalf("a settle with nothing to spend dropped the armed disconnect on %q", row.ID)
	}
	if a.model != "openai/gpt-4.1-mini" {
		t.Fatalf("a settle with nothing to spend moved the conversation to %q", a.model)
	}
}

// customConnectionApp is the picker-grouping fixture: the default service plus
// the two custom connections homelab (id custom) and studio (id custom-studio),
// in persisted order, with each connection's own model list served from held by
// the connection id. A connection missing from held has no model list, which is
// the empty group the notice row stands for.
func customConnectionApp(t *testing.T, held map[string][]Model) *app {
	t.Helper()
	dir := t.TempDir()
	customs := connectionWriteSources(t, dir, "homelab", "studio")
	sources := modelsource.NewSet(append([]modelsource.Connected{connectionDefaultService()}, customs...)...)
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini", sources, []Model{{ID: "openai/gpt-4.1-mini"}})
	a.modelsForService = func(service modelsource.Connected) []Model {
		return append([]Model(nil), held[service.Source.ID]...)
	}
	return a
}

// groupPickerLines is the drawn list as a reader sees it, with room for every
// service heading and its rows: [pickerLines] hands the picker the row count as
// the line budget, which is right when every row is one line but cuts a list
// with headings off partway through.
func groupPickerLines(a *app) []string {
	lines := a.pick.rows(a.width, a.pick.height(a.width), a.pal, -1, a.reasoningFor)
	plainLines := make([]string, len(lines))
	for at, line := range lines {
		plainLines[at] = plain(line)
	}
	return plainLines
}

// lineIndex is the first line whose trimmed text satisfies match, -1 when none.
func lineIndex(lines []string, match func(string) bool) int {
	for at, line := range lines {
		if match(strings.TrimSpace(line)) {
			return at
		}
	}
	return -1
}

// headingLines counts the drawn service headings for group. A heading is a dim
// line that is the group's name alone, or — since the heads name the address
// and the model count too ([serviceGroupHead]) — the name followed by the head
// line's wide separator. A model's own row never carries either, so this counts
// headings and never rows.
func headingLines(lines []string, group string) int {
	count := 0
	for _, line := range lines {
		if t := strings.TrimSpace(line); t == group || strings.HasPrefix(t, group+"   ") {
			count++
		}
	}
	return count
}

// TWO CUSTOM CONNECTIONS GROUP APART: the default service and two custom
// connections each carry their own heading, in persisted order, and every custom
// model's id is qualified with its own connection's name so no row can sit under
// the wrong heading. The drawn picker shows each heading once, above its models.
func TestTwoCustomConnectionsGroupApartInThePicker(t *testing.T) {
	a := customConnectionApp(t, map[string][]Model{
		"custom":        {{ID: "qwen-local"}},
		"custom-studio": {{ID: "mistral-local"}},
	})

	list := a.modelList()
	want := []struct {
		id, group string
		order     int
	}{
		{"openai/gpt-4.1-mini", "openrouter", 0},
		{"homelab/qwen-local", "homelab", 1},
		{"studio/mistral-local", "studio", 2},
	}
	if len(list) != len(want) {
		t.Fatalf("model list = %+v, want %d rows", list, len(want))
	}
	for at, w := range want {
		row := list[at]
		if row.ID != w.id || row.Group != w.group || row.GroupOrder != w.order || row.Unavailable {
			t.Fatalf("row %d = %+v, want id=%q group=%q order=%d", at, row, w.id, w.group, w.order)
		}
		// NO MODEL APPEARS UNDER THE WRONG HEADING: a custom row's own group is
		// the Written prefix its id carries, and the default row is bare.
		if w.order > 0 && !strings.HasPrefix(row.ID, row.Group+"/") {
			t.Fatalf("row %d is not qualified by its own heading: %+v", at, row)
		}
	}

	a.openPicker()
	lines := groupPickerLines(a)
	drawn := strings.Join(lines, "\n")
	for _, group := range []string{"openrouter", "homelab", "studio"} {
		if got := headingLines(lines, group); got != 1 {
			t.Fatalf("the picker drew the %q heading %d times, want once:\n%s", group, got, drawn)
		}
	}
	for _, id := range []string{"homelab/qwen-local", "studio/mistral-local"} {
		group := id[:strings.Index(id, "/")]
		headAt := lineIndex(lines, func(line string) bool { return line == group || strings.HasPrefix(line, group+"   ") })
		modelAt := lineIndex(lines, func(line string) bool { return strings.Contains(line, id) })
		if headAt < 0 || modelAt < 0 || headAt > modelAt {
			t.Fatalf("the %q heading did not stand above %q:\n%s", group, id, drawn)
		}
	}
}

// A RENAME MOVES THE HEADING: after a custom connection is renamed through the
// same edit draft the name step builds, the next picker build draws the new name
// as the heading and qualifies its models under the new prefix, and no heading
// with the old name remains.
func TestRenamingACustomConnectionMovesItsPickerHeading(t *testing.T) {
	a := customConnectionApp(t, map[string][]Model{
		"custom":        {{ID: "qwen-local"}},
		"custom-studio": {{ID: "mistral-local"}},
	})
	if list := a.modelList(); len(list) != 3 || list[1].ID != "homelab/qwen-local" {
		t.Fatalf("the fixture did not start on homelab/qwen-local: %+v", list)
	}

	persisted := config.PersistedSources(a.profileDir)
	if len(persisted) != 2 || persisted[0].Written != "homelab" {
		t.Fatalf("the profile does not hold the two custom connections: %+v", persisted)
	}
	renamed := persisted[0]
	renamed.Written = "lab"
	// THE RENAME: an edit draft whose Written moved, exactly what the name step
	// builds on an answer that differs from the stored one. The probe is empty,
	// so the connect persists the renamed row without a network read.
	msg := a.beginModelConnect(modelConnectDraft{
		source:      modelsource.Source{ID: "custom", Written: "lab", Listing: modelsource.ListingModels},
		row:         renamed,
		renamedFrom: "homelab",
		entryID:     modelConnectionID("custom"),
		editing:     true,
	})().(modelConnectResultMsg)
	a.adoptModelConnectResult(msg)

	list := a.modelList()
	if len(list) != 3 || list[1].ID != "lab/qwen-local" || list[1].Group != "lab" {
		t.Fatalf("the renamed connection's models did not move to lab: %+v", list)
	}
	if list[2].ID != "studio/mistral-local" || list[2].Group != "studio" {
		t.Fatalf("the rename touched the other connection: %+v", list[2])
	}
	for _, row := range list {
		if row.Group == "homelab" || strings.HasPrefix(row.ID, "homelab/") {
			t.Fatalf("the old name survived the rename: %+v", row)
		}
	}

	a.openPicker()
	lines := groupPickerLines(a)
	drawn := strings.Join(lines, "\n")
	if got := headingLines(lines, "lab"); got != 1 {
		t.Fatalf("the picker drew the %q heading %d times, want once:\n%s", "lab", got, drawn)
	}
	if got := headingLines(lines, "homelab"); got != 0 {
		t.Fatalf("a heading with the old name remained:\n%s", drawn)
	}
	if !strings.Contains(drawn, "lab/qwen-local") || strings.Contains(drawn, "homelab/") {
		t.Fatalf("the picker did not draw the models under the new prefix:\n%s", drawn)
	}
}

// A CONNECTION WITH NO LIST KEEPS ITS PLACE: one of the two custom connections
// holds no model list, and its heading still draws with the notice row, between
// the others in persisted order.
func TestACustomConnectionWithNoListKeepsItsPlaceInThePicker(t *testing.T) {
	a := customConnectionApp(t, map[string][]Model{
		"custom": {{ID: "qwen-local"}},
	})

	list := a.modelList()
	if len(list) != 3 {
		t.Fatalf("model list = %+v", list)
	}
	if list[1].ID != "homelab/qwen-local" || list[1].Group != "homelab" || list[1].Unavailable {
		t.Fatalf("the listing connection did not sit second: %+v", list[1])
	}
	placeholder := list[2]
	if !placeholder.Unavailable || placeholder.ID != "" || placeholder.Group != "studio" ||
		placeholder.GroupOrder != 2 || placeholder.Notice != noServiceModelListWord {
		t.Fatalf("the listing-less connection did not keep its place: %+v", placeholder)
	}

	a.openPicker()
	lines := groupPickerLines(a)
	drawn := strings.Join(lines, "\n")
	if got := headingLines(lines, "studio"); got != 1 {
		t.Fatalf("the listing-less service drew its heading %d times, want once:\n%s", got, drawn)
	}
	if !strings.Contains(drawn, noServiceModelListWord) {
		t.Fatalf("the listing-less service drew no notice row:\n%s", drawn)
	}
	homelabAt := lineIndex(lines, func(line string) bool { return line == "homelab" || strings.HasPrefix(line, "homelab   ") })
	studioAt := lineIndex(lines, func(line string) bool { return line == "studio" || strings.HasPrefix(line, "studio   ") })
	if homelabAt < 0 || studioAt < 0 || homelabAt > studioAt {
		t.Fatalf("the listing-less service was drawn out of order:\n%s", drawn)
	}
}

// THE UNCONNECTED CUSTOM ROW IS CALLED WHAT THE MANUAL CALLS IT. The catalog
// template's Written is the bare id `custom`, so a rename that reads Written for
// every custom id drew the row a person has not connected yet as `custom` — and
// the manual, the README and #1107 all send them looking for `Custom
// OpenAI-compatible API`. Only a connected instance is called what the person
// called it.
func TestTheUnconnectedCustomRowKeepsItsVendoredNameAndAConnectedOneTakesThePersons(t *testing.T) {
	const vendored = "Custom OpenAI-compatible API"
	defaults := []Model{{ID: "openai/gpt-4.1-mini"}}
	customRow := func(a *app) (connect.Status, bool) {
		for _, row := range a.modelConnectionRows() {
			if row.ID == modelConnectionID("custom") {
				return row, true
			}
		}
		return connect.Status{}, false
	}

	dir := t.TempDir()
	bare := modelServiceTestApp(t, dir, defaults[0].ID, modelsource.NewSet(testDefaultService("sk-default-1234567890")), defaults)
	installModelServiceShelf(bare, dir)
	row, ok := customRow(bare)
	if !ok {
		t.Fatal("the models group did not offer the custom row")
	}
	if row.Connected {
		t.Fatalf("nothing is connected, yet the custom row reads connected: %+v", row)
	}
	if row.Name != vendored {
		t.Fatalf("the unconnected custom row is drawn %q, want %q", row.Name, vendored)
	}

	named := modelsource.Connected{
		Source:  modelsource.Source{ID: "custom", Written: "alpha", Name: vendored, Address: "http://127.0.0.1:1/v1"},
		Key:     "sk-alpha-1234567890",
		Address: "http://127.0.0.1:1/v1",
	}
	dir2 := t.TempDir()
	held := modelServiceTestApp(t, dir2, defaults[0].ID, modelsource.NewSet(testDefaultService("sk-default-1234567890"), named), defaults)
	installModelServiceShelf(held, dir2)
	row, ok = customRow(held)
	if !ok {
		t.Fatal("the connected custom instance has no row")
	}
	if !row.Connected || row.Name != "alpha" {
		t.Fatalf("a connected custom instance the person named alpha is drawn %q (connected=%t), want alpha", row.Name, row.Connected)
	}
}

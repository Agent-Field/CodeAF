package tui3

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	modelcatalog "github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/connect"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// Model-service rows share the connection panel's row grammar without sharing
// the connected-account namespace. A prefix that cannot be persisted by either
// package keeps a model service named "google" separate from a Google account.
const modelConnectionPrefix = "model-service:"

const noServiceModelListWord = "no list from this service · type a model id"

type modelConnectStep uint8

const (
	modelConnectRegion modelConnectStep = iota
	modelConnectAddress
	modelConnectName
	modelConnectKey
)

// modelConnectDraft is the one service answer being assembled in the panel or
// on the Providers tab. The profile is not touched until every answer is here
// and internal/config has accepted the service.
type modelConnectDraft struct {
	source modelsource.Source
	row    config.PersistedSource
	step   modelConnectStep
	sheet  bool
	// entryID is the row the flow's answer boxes hang under, fixed at draft
	// time. A custom connection that is still being minted has no persisted id
	// of its own until the name answer lands, so the boxes cannot look the row
	// up again mid-flow.
	entryID string
	// editing marks a draft that started from a row already in the profile, so
	// the answers REWRITE that row: the id is kept, an empty key keeps the
	// stored one, and a changed name is a rename the result carries.
	editing bool
	// renamedFrom is the Written name the row carried when the draft opened.
	// A rename re-prefixes every already-picked model id, or the picker rows
	// strand onto the default service under the old name.
	renamedFrom string
}

func modelConnectionID(id string) string { return modelConnectionPrefix + strings.TrimSpace(id) }

func modelConnectionSource(id string) (string, bool) {
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, modelConnectionPrefix) {
		return "", false
	}
	return strings.TrimPrefix(id, modelConnectionPrefix), true
}

// prepareModelServices makes the existing profile visible without spending a
// network call. A connected service's picker rows come only from the small
// source-scoped cache Round A provided; an absent cache remains an empty group.
func (a *app) prepareModelServices() {
	if a.sources.Empty() {
		// An empty set means this door does not own model-service configuration.
		// Local v3 launches always pass the resolved set; hosted and older seams
		// pass nothing and must keep their connection surface byte-identical.
		a.sourceModels = make(map[string][]Model)
		return
	}
	a.modelCatalog = modelsource.Vendored()
	a.sourceModels = make(map[string][]Model)
	for _, service := range a.sources.All() {
		if strings.EqualFold(service.Source.ID, modelsource.DefaultID) || service.Source.Listing != modelsource.ListingModels {
			continue
		}
		a.sourceModels[service.Source.ID] = a.modelsForConnectedService(service)
	}
}

// modelsForConnectedService reads the process shelf first, then the small disk
// cache for a surface whose door predates the shelf seam. It never fetches: the
// connect command and ctrl+r are the only network doors onto model lists.
func (a *app) modelsForConnectedService(service modelsource.Connected) []Model {
	if service.Source.Listing != modelsource.ListingModels {
		return nil
	}
	if a.modelsForService != nil {
		if models := cleanModels(a.modelsForService(service)); len(models) > 0 {
			return models
		}
	}
	if models := cleanModels(a.sourceModels[service.Source.ID]); len(models) > 0 {
		return models
	}
	return a.cachedModelsFor(service.Source.ID, service.Address)
}

func (a *app) reloadModelSources() {
	base := a.sources.Default()
	address := base.Address
	if strings.TrimSpace(address) == "" {
		address = config.DefaultBaseURL
	}
	a.sources = config.ResolveSources(a.profileDir, base.Key, address)
	a.refreshCreditWarnings()
	if a.applyModelSources != nil {
		a.applyModelSources(a.sources)
	}
	if a.at(pageSettings) {
		a.sheet.sources = a.sources
	}
}

func (a *app) modelSource(id string) (modelsource.Source, bool) {
	if connected, ok := a.sources.ByID(id); ok {
		return connected.Source, true
	}
	for _, source := range a.modelCatalog {
		if strings.EqualFold(source.ID, id) {
			return source, true
		}
	}
	return modelsource.Source{}, false
}

// connectionRows is the one reading both renderings use. The models group is
// made from modelsource.Vendored and the live profile, then the account catalog
// follows unchanged.
func (a *app) connectionRows() []connect.Status {
	rows := a.modelConnectionRows()
	if a.conns != nil {
		rows = append(rows, a.conns.Services()...)
	}
	return rows
}

func (a *app) modelConnectionRows() []connect.Status {
	connected := make(map[string]modelsource.Connected)
	for _, service := range a.sources.All() {
		if !strings.EqualFold(service.Source.ID, modelsource.DefaultID) {
			connected[strings.ToLower(service.Source.ID)] = service
		}
	}
	rows := make([]connect.Status, 0, len(a.modelCatalog)+len(connected))
	seen := make(map[string]bool)
	appendSource := func(source modelsource.Source) {
		key := strings.ToLower(strings.TrimSpace(source.ID))
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		service, held := connected[key]
		if held {
			source = service.Source
		}
		rows = append(rows, modelConnectionStatus(source, held))
	}
	for _, source := range a.modelCatalog {
		appendSource(source)
	}
	for _, service := range a.sources.All() {
		if !strings.EqualFold(service.Source.ID, modelsource.DefaultID) {
			appendSource(service.Source)
		}
	}
	// THE PANEL'S ADD ROW: once one custom connection is connected, the
	// catalog's own row has become that instance's edit door, and a second
	// instance would be unreachable from here without a row that always mints
	// (startCustomAdd). An empty profile needs no row: the catalog row is
	// still the unconnected door onto the first connection. The gate is the
	// CUSTOM count and not the switcher's ring, which always holds the default
	// service and would put a second door onto the first connection here.
	if len(customInstances(a.sources)) > 0 {
		rows = append(rows, connect.Status{Service: connect.Service{
			ID: modelConnectionID(customAddRowID), Name: "add custom connection",
			Blurb: "address · key", Auth: connect.AuthKey, Category: "models",
		}})
	}
	// THE PANEL CARRIES THE SWITCHER TOO: the Providers tab's active-connection
	// row and this one read the same ring and answer to the same enter
	// (switchActiveConnection), so neither door is the only door. One row per
	// reading; the sentence is the tab's own (switchSentence). The gate is the
	// ring alone and not the add row's: the row is offered when there is
	// somewhere to move to, whatever the panel's other doors are.
	if reading, ok := switchReading(a.conversationModel(), a.sources); ok {
		rows = append(rows, connect.Status{Service: connect.Service{
			ID: modelConnectionID(connectionSwitchRowID), Name: "active connection",
			Blurb: reading.sentence,
			Auth:  connect.AuthKey, Category: "models",
		}})
	}
	return rows
}

func modelConnectionStatus(source modelsource.Source, held bool) connect.Status {
	need := "key"
	switch {
	case source.ID == "ollama":
		need = ""
	case source.ID == "codex":
		need = "browser"
	case modelsource.IsCustomID(source.ID):
		need = "address · key"
	case len(source.Regions) > 0:
		need = "region · key"
	}
	// A CONNECTED INSTANCE IS CALLED WHAT THE PERSON CALLED IT. The vendored
	// template's own name stops being true the moment a connection has a
	// name; without this every instance reads identically on the panel. THE
	// UNCONNECTED ROW KEEPS THE VENDORED NAME: the catalog template's Written
	// is the bare id, and a person who has connected nothing yet finds the
	// row by the name the manual and the README spell, `Custom
	// OpenAI-compatible API`, never by `custom`.
	name := source.Name
	if held && modelsource.IsCustomID(source.ID) {
		if written := strings.TrimSpace(source.Written); written != "" {
			name = written
		}
	}
	service := connect.Service{
		ID: modelConnectionID(source.ID), Name: name, Blurb: need,
		Auth: connect.AuthKey, Category: "models",
	}
	if source.ID == "ollama" {
		service.Auth = "none"
	} else if source.ID == "codex" {
		service.Auth = connect.AuthBrowser
	}
	return connect.Status{Service: service, Connected: held, Account: source.Written, KeyEnv: source.KeyEnv}
}

func modelServiceTag(row connect.Status) string {
	id, ok := modelConnectionSource(row.ID)
	if !ok {
		return ""
	}
	switch {
	case id == "ollama":
		return ""
	case id == "codex":
		return "browser"
	case id == connectionSwitchRowID:
		// The switch row's sentence is its own Blurb, drawn as the row's
		// value; a tag would say it twice.
		return ""
	case modelsource.IsCustomID(id):
		return "address · key"
	}
	if strings.TrimSpace(row.Blurb) != "" {
		return row.Blurb
	}
	return keyTag
}

func (a *app) startModelConnect(row connect.Status, fromSheet bool) tea.Cmd {
	id, ok := modelConnectionSource(row.ID)
	if !ok {
		return nil
	}
	source, ok := a.modelSource(id)
	if !ok {
		return nil
	}
	persisted := config.PersistedSource{ID: source.ID, Written: source.Written, Order: a.nextModelServiceOrder()}
	editing := false
	for _, existing := range config.PersistedSources(a.profileDir) {
		if strings.EqualFold(existing.ID, source.ID) {
			// A reconnect starts with the row that actually landed, so an automatic
			// name such as z-ai-direct stays put instead of being suggested again.
			// For a custom row the same match is what makes the flow an EDIT: the
			// id is kept, the answers prefill, and a changed name is a rename.
			persisted = existing
			editing = true
			break
		}
	}
	draft := &modelConnectDraft{
		source: source, row: persisted, sheet: fromSheet, entryID: row.ID,
		editing: editing, renamedFrom: strings.TrimSpace(persisted.Written),
	}
	if !editing {
		draft.renamedFrom = ""
	}
	a.modelDraft = draft
	switch {
	case len(source.Regions) > 0:
		draft.step = modelConnectRegion
		a.showModelEntry(newModelChoiceEntry(row.ID, source.Name, "region", regionChoices(source)), fromSheet)
		return nil
	case modelsource.IsCustomID(source.ID):
		draft.step = modelConnectAddress
		entry := newModelEntry(row.ID, source.Name, "base URL", nil, false)
		if editing {
			entry.box.setText(persisted.Address)
		}
		a.showModelEntry(entry, fromSheet)
		return nil
	case source.ID == "ollama":
		return a.beginModelConnect(*draft)
	case source.ID == "codex":
		a.modelDraft = nil
		if !fromSheet {
			a.connPanel.close()
		}
		return a.beginCodexConnect(*draft)
	default:
		draft.step = modelConnectKey
		a.showModelEntry(newModelEntry(row.ID, source.Name, "key", nil, true), fromSheet)
		return nil
	}
}

func (a *app) beginCodexConnect(draft modelConnectDraft) tea.Cmd {
	connect, ctx := a.codexConnect, a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return func() tea.Msg {
		if connect == nil {
			return codexFlowMsg{draft: draft, err: errors.New("codex browser sign-in is unavailable here")}
		}
		flow, err := connect(ctx)
		return codexFlowMsg{draft: draft, flow: flow, err: err}
	}
}

// adoptCodexFlow puts the sign-in address on the same waiting block every
// browser connection uses before it waits. The result then rejoins the ordinary
// model-service adoption path, so the picker, live sources and preferred-model
// move have one implementation.
func (a *app) adoptCodexFlow(msg codexFlowMsg) tea.Cmd {
	if msg.err != nil || msg.flow == nil {
		reason := "the browser sign-in did not start"
		if msg.err != nil {
			reason = codexFailureReason(msg.err)
		}
		a.modelServiceMessage("codex did not connect · " + reason)
		return nil
	}
	if a.codexFlow != nil {
		a.codexFlow.Cancel()
	}
	a.codexFlow = msg.flow
	a.openConnectFlow("codex", "codex", msg.flow.URL())
	flow, ctx, dir := msg.flow, a.ctx, a.profileDir
	if ctx == nil {
		ctx = context.Background()
	}
	return func() tea.Msg {
		defer flow.Cancel()
		tokens, err := flow.Wait(ctx)
		if err != nil {
			return modelConnectResultMsg{
				service: "codex", name: "Codex", written: "codex", browser: true,
				word: "codex did not connect · " + codexFailureReason(err), err: err,
			}
		}
		outcome, err := config.ConnectCodex(ctx, dir, tokens)
		models := modelsFromListedIDs(outcome.ModelIDs)
		if err == nil {
			models = codexConnectedModels(dir, models)
		}
		word := config.CodexConnectionWord(tokens.Email, tokens.Plan, outcome)
		if err != nil {
			word = "codex did not connect · " + codexFailureReason(err)
		}
		return modelConnectResultMsg{
			service: "codex", name: "Codex", written: "codex", outcome: outcome,
			models: models, err: err, browser: true, word: word,
		}
	}
}

// codexConnectedModels is the Codex rows this surface keeps once a sign-in
// lands: the catalog [config.ConnectCodex] just remembered, WINDOWS INCLUDED,
// and the same rows written to this service's model cache so the next launch
// opens with them.
//
// THE OUTCOME CARRIES IDS AND NOTHING ELSE, which is right for the card and
// wrong for this. Kept as bare ids, a surface with no process shelf behind it —
// the engine road's, where the rows a model switch reads are this surface's
// own — switched onto `codex/gpt-5.5` with no window to tell anyone, and the
// status line went on showing the previous model's figure (#1383). It is the
// same cache write a key service's connection makes on the way back
// ([app.beginModelConnect]); the Codex road had simply never made it. When the
// remembered catalog cannot be read, the ids are what there is.
func codexConnectedModels(dir string, listed []Model) []Model {
	connected, found := config.ResolveSources(dir, "", "").ByID("codex")
	if !found {
		return listed
	}
	models := listed
	if remembered := codexCatalogModels(dir); len(remembered) > 0 {
		models = remembered
	}
	_ = WriteModelCacheFor(connected.Source.ID, connected.Address, models)
	return models
}

// codexCatalogModels is the Codex catalog this profile remembers, as the rows
// this surface draws, each with its window ([config.CodexRememberedModels]);
// nil when Codex is not connected here.
func codexCatalogModels(dir string) []Model {
	connected, found := config.ResolveSources(dir, "", "").ByID("codex")
	if !found {
		return nil
	}
	return surfaceModels(config.CodexRememberedModels(connected, dir))
}

func codexFailureReason(err error) string {
	if err == nil {
		return "the browser sign-in did not finish"
	}
	reason := strings.TrimSpace(err.Error())
	if _, tail, found := strings.Cut(reason, ": "); found {
		reason = strings.TrimSpace(tail)
	}
	if reason == "" {
		return "the browser sign-in did not finish"
	}
	return reason
}

func newModelEntry(id, name, blank string, answers []string, secret bool) *keyEntry {
	return &keyEntry{id: id, name: strings.ToLower(name), blank: blank, answers: answers, secret: secret}
}

func newModelChoiceEntry(id, name, blank string, choices []entryChoice) *keyEntry {
	return &keyEntry{id: id, name: strings.ToLower(name), blank: blank, choices: choices}
}

func regionChoices(source modelsource.Source) []entryChoice {
	choices := make([]entryChoice, 0, len(source.Regions))
	for _, region := range source.Regions {
		choices = append(choices, entryChoice{ID: region.ID, Name: region.Name})
	}
	return choices
}

func (a *app) showModelEntry(entry *keyEntry, inSheet bool) {
	if inSheet {
		a.sheet.conn.entry = entry
		a.sheet.build()
		return
	}
	a.connPanel.entry = entry
}

func (a *app) nextModelServiceOrder() int {
	next := 1
	for _, row := range config.PersistedSources(a.profileDir) {
		if row.Order >= next {
			next = row.Order + 1
		}
	}
	return next
}

// startCustomAdd is the Providers tab's own door onto a NEW custom
// connection. The panel's custom row doubles as the first instance's edit
// door once one is connected, so the tab carries the add row that always
// mints: the same PrepareCustomSource and ConnectService path, never a second
// implementation of either.
func (a *app) startCustomAdd(inSheet bool) tea.Cmd {
	source, ok := a.modelSource(modelsource.CustomID)
	if !ok {
		for _, candidate := range modelsource.Vendored() {
			if candidate.ID == modelsource.CustomID {
				source, ok = candidate, true
				break
			}
		}
	}
	if !ok {
		return nil
	}
	id := customAddRowID
	draft := &modelConnectDraft{
		source: source, row: config.PersistedSource{Order: a.nextModelServiceOrder()},
		sheet: inSheet, entryID: modelConnectionID(id), step: modelConnectAddress,
	}
	a.modelDraft = draft
	a.showModelEntry(newModelEntry(modelConnectionID(id), source.Name, "base URL", nil, false), inSheet)
	// A COMMAND, ALWAYS. The box is up over the list and the keyboard belongs to
	// it, which is a change of state a keypress produced — the same act the
	// panel's other activations answer with a command, and a nil here read as
	// "enter started nothing" by the caller's own test for that.
	return a.frameTick()
}

// modelEntryAnswer advances a region/address answer to the key box, or starts
// the checked connection once the last answer has been supplied.
func (a *app) modelEntryAnswer(entry *keyEntry) tea.Cmd {
	draft := a.modelDraft
	if draft == nil {
		return nil
	}
	answer := entry.value()
	if answer == "" && draft.step != modelConnectName {
		a.showModelEntry(entry, draft.sheet)
		return nil
	}
	switch draft.step {
	case modelConnectRegion:
		// The answer is picked from this source's own region list, so there is
		// nothing left to validate or refuse here.
		draft.row.Region = answer
		draft.step = modelConnectKey
		a.showModelEntry(newModelEntry(draft.entryID, draft.source.Name, "key", nil, true), draft.sheet)
		return nil
	case modelConnectAddress:
		parsed, err := url.Parse(answer)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			a.modelServiceMessage("that is not a base URL")
			a.showModelEntry(entry, draft.sheet)
			return nil
		}
		draft.row.Address = strings.TrimRight(answer, "/")
		draft.step = modelConnectName
		// THE NAME IS ASKED, NOT ASSUMED. The default is the host's own slug,
		// shared with config through modelsource.SourceSlug so every surface
		// spells a host the same way; an edit starts from the name it already
		// has, because changing it is a rename with consequences downstream.
		name := modelsource.SourceSlug(modelsource.AddressHost(answer))
		if draft.editing && draft.renamedFrom != "" {
			name = draft.renamedFrom
		}
		nameEntry := newModelEntry(draft.entryID, draft.source.Name, "name", nil, false)
		nameEntry.box.setText(name)
		a.showModelEntry(nameEntry, draft.sheet)
		return nil
	case modelConnectName:
		// AN EMPTY ANSWER MEANS WHAT THE BOX ALREADY SHOWED: a new connection
		// takes the host slug the surface itself suggested, and an edit keeps
		// the name the row already has, because re-slugging the host would be
		// a rename the person never asked for.
		answer = strings.TrimSpace(answer)
		if answer == "" {
			answer = strings.TrimSpace(draft.row.Written)
		}
		if answer == "" {
			answer = modelsource.SourceSlug(modelsource.AddressHost(draft.row.Address))
		}
		// THE NAME IS THE ROUTING PREFIX. It becomes the first segment of
		// every model id this connection qualifies, so a / in it would give
		// modelsource.Split two breaks where it expects one, and a space would
		// travel into every id and into the persisted row. The box stays open
		// on a refused name.
		if fault := connectionNameFault(answer); fault != "" {
			a.modelServiceMessage(fault)
			a.showModelEntry(entry, draft.sheet)
			return nil
		}
		if draft.editing {
			draft.row.Written = answer
		} else {
			// The instance id is minted HERE, on the shared path, so a second
			// custom connection keeps the first: the first keeps the vendored id,
			// later ones take custom-<slug> with a numeric tiebreak.
			draft.row = config.PrepareCustomSource(a.profileDir, draft.row.Address, answer)
		}
		draft.step = modelConnectKey
		a.showModelEntry(newModelEntry(draft.entryID, draft.source.Name, "key", nil, true), draft.sheet)
		return nil
	case modelConnectKey:
		if answer == "" && draft.editing && (draft.row.Key != "" || draft.row.KeyEnv != "") {
			// An edit that leaves the key box empty keeps the stored key: a
			// rename or an address fix is not a reason to re-type a secret.
			copy := *draft
			a.modelDraft = nil
			return a.beginModelConnect(copy)
		}
		if env, ok := modelKeyEnvironment(answer); ok {
			draft.row.KeyEnv, draft.row.Key = env, ""
		} else {
			draft.row.Key, draft.row.KeyEnv = answer, ""
		}
		copy := *draft
		a.modelDraft = nil
		return a.beginModelConnect(copy)
	}
	return nil
}

// connectionNameFault says why a name cannot be a connection's Written word,
// empty when it can. The name becomes the first segment of every model id the
// connection qualifies and part of the persisted row, so the / that separates
// connection from model and the spaces that would travel into both are the
// two characters it cannot carry.
func connectionNameFault(name string) string {
	if strings.Contains(name, "/") {
		return "a connection name cannot contain / · the slash is what separates connection from model"
	}
	if strings.ContainsFunc(name, unicode.IsSpace) {
		return "a connection name cannot contain spaces · they would travel into every model id"
	}
	return ""
}

func modelKeyEnvironment(answer string) (string, bool) {
	word := strings.TrimPrefix(strings.TrimSpace(answer), "$")
	if word == "" || !strings.Contains(word, "_") {
		return "", false
	}
	for _, r := range word {
		if !(unicode.IsUpper(r) || unicode.IsDigit(r) || r == '_') {
			return "", false
		}
	}
	return word, true
}

func (a *app) beginModelConnect(draft modelConnectDraft) tea.Cmd {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	dir := a.profileDir
	instance := draft.row.ID
	authors := modelAuthorSegments(a.defaultServiceModels())
	refresh := a.serviceModelRefresh
	return func() tea.Msg {
		outcome, err := config.ConnectService(ctx, dir, draft.row, draft.source, authors)
		models := modelsFromListedIDs(outcome.ModelIDs)
		if err == nil && outcome.Kind == modelsource.OutcomeConnected && outcome.Listed {
			// Resolve the row back through config after ConnectService writes it.
			// That is the one door which owns key and address precedence; rebuilding
			// a Connected here would create a second, subtly different account door.
			// THE ROW'S OWN ID is what resolves, never the template's: a minted
			// custom instance persists under custom-<slug>, and looking the template
			// id up would find the first instance or nothing at all.
			connected, found := config.ResolveSources(dir, "", "").ByID(instance)
			if found {
				fixedDoorCatalog := len(outcome.Door.Models) > 0
				if fixedDoorCatalog {
					seed := make([]modelcatalog.Model, 0, len(models))
					for _, model := range models {
						seed = append(seed, modelcatalog.Model{ID: model.ID})
					}
					_ = modelcatalog.Remember(config.CatalogOptionsFor(connected, dir), seed)
					_ = WriteModelCacheFor(instance, connected.Address, models)
				} else if refresh != nil {
					if refreshed, refreshErr := refresh(ctx, connected, models); refreshErr == nil && len(refreshed) > 0 {
						models = refreshed
					}
				} else {
					seed := make([]modelcatalog.Model, 0, len(models))
					for _, model := range models {
						seed = append(seed, modelcatalog.Model{ID: model.ID})
					}
					options := config.CatalogOptionsFor(connected, dir)
					_ = modelcatalog.Remember(options, seed)
					catalog, refreshErr := modelcatalog.Refresh(ctx, options)
					if refreshed := surfaceModels(catalog.ModelsNow()); refreshErr == nil && len(refreshed) > 0 {
						models = refreshed
					}
					_ = WriteModelCacheFor(instance, connected.Address, models)
				}
			}
		}
		return modelConnectResultMsg{
			service: instance, name: draft.source.Name, written: draft.row.Written,
			keyEnv: draft.row.KeyEnv, outcome: outcome, models: models, err: err,
			renamedFrom: draft.renamedFrom,
		}
	}
}

func modelsFromListedIDs(ids []string) []Model {
	models := make([]Model, 0, len(ids))
	for _, id := range ids {
		models = append(models, Model{ID: id})
	}
	return cleanModels(models)
}

func surfaceModels(rows []modelcatalog.Model) []Model {
	models := make([]Model, 0, len(rows))
	for _, row := range rows {
		model := Model{
			ID: row.ID, ContextLength: row.ContextLength, ArenaElo: row.ArenaElo,
			Output: row.OutputModalities, Input: row.InputModalities,
			Reasoning: row.Reasons() || row.ReasoningLevels(),
		}
		if !row.PriceUnknown {
			model.PromptPrice = row.PromptPrice
			model.CompletionPrice = row.CompletionPrice
			model.CacheReadPrice = row.CacheReadPrice
		}
		models = append(models, model)
	}
	return cleanModels(models)
}

func modelAuthorSegments(models []Model) []string {
	seen := map[string]bool{}
	var authors []string
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		at := strings.Index(id, "/")
		if at <= 0 {
			continue
		}
		author := strings.ToLower(strings.TrimSpace(id[:at]))
		if author != "" && !seen[author] {
			seen[author] = true
			authors = append(authors, author)
		}
	}
	return authors
}

func (a *app) defaultServiceModels() []Model {
	if a.models != nil {
		if models := cleanModels(a.models()); len(models) > 0 {
			return models
		}
	}
	if models := a.cachedModels(); len(models) > 0 {
		return models
	}
	return BuiltinModels()
}

func (a *app) adoptModelConnectResult(msg modelConnectResultMsg) {
	if msg.err != nil {
		if msg.browser {
			a.codexFlow = nil
			a.settleConnectWord("codex", msg.word, true)
			if a.at(pageSettings) {
				a.modelServiceMessage(msg.word)
			}
			return
		}
		a.modelServiceMessage(msg.err.Error())
		return
	}
	// A CONNECTION THAT LANDED WROTE THAT SERVICE'S CACHE ON THE WAY BACK
	// ([app.connectModelService] above), so whatever the memo holds under that
	// pair is a reading taken before the file existed (models.go).
	if address, ok := a.sources.ByID(msg.service); ok {
		a.forgetModelList(msg.service, address.Address)
	}
	service := strings.TrimSpace(msg.written)
	if service == "" {
		service = msg.service
	}
	if source, ok := a.modelSource(msg.service); ok && service == msg.service && strings.TrimSpace(source.Written) != "" {
		service = source.Written
	}
	service = strings.ToLower(service)
	line := ""
	nextModel := ""
	renamedNext := ""
	switch msg.outcome.Kind {
	case modelsource.OutcomeConnected, modelsource.OutcomeAccountCannotPay:
		a.reloadModelSources()
		connected, found := a.sources.ByID(msg.service)
		wasRenamed := false
		if found {
			// A RENAME IS CARRIED BEFORE ANYTHING ELSE MOVES. Routing keys on
			// the Written prefix of a model id, so the conversation's picked
			// id has to follow the new name before anything else reads it, or
			// it answers on the default service while the picker shows it
			// nowhere. The RESOLVED Written is what carries: Collides inside
			// ConnectService may have adjusted the name that was typed, and a
			// reconnect that kept its name is not a rename at all.
			oldWritten := strings.TrimSpace(msg.renamedFrom)
			wasRenamed = oldWritten != "" && !strings.EqualFold(oldWritten, strings.TrimSpace(connected.Source.Written))
			renamedNext = a.reprefixRenamedModel(oldWritten, connected.Source.Written)
			if wasRenamed {
				a.reprefixStoredModels(oldWritten, connected.Source.Written)
			}
			service = strings.ToLower(connected.Source.Written)
		}
		if msg.outcome.Kind == modelsource.OutcomeConnected {
			a.sourceModels[msg.service] = cleanModels(msg.models)
			// THE PREFERRED-MODEL MOVE BELONGS TO A FRESH CONNECT, and to a
			// reconnect that kept its name: connecting a service is wanting to
			// answer on it. A RENAME is a change of label, and it keeps the
			// model the person already picked, re-prefixed above, instead of
			// replacing it with the connection's preferred one.
			if found && !wasRenamed {
				if modelUsesService(a.deferredModelServiceModel, connected.Source.Written) {
					a.deferredModelServiceModel = ""
				}
				preferred := connected.Source.PreferredModel(connected.Door, listedModelIDs(a.sourceModels[msg.service]))
				if next := connected.Qualify(preferred); next != "" && next != strings.TrimSpace(a.model) {
					if a.state == stateWorking {
						a.deferredModelServiceModel = next
					} else {
						nextModel = next
					}
				}
			}
		}
		line = serviceOutcomeWord(service, msg.outcome)
		if msg.outcome.Kind == modelsource.OutcomeConnected && a.engineRoad && strings.TrimSpace(msg.keyEnv) != "" {
			line += " · " + engineVariableWord(msg.keyEnv)
		}
	default:
		line = serviceOutcomeWord(service, msg.outcome)
	}
	if msg.browser {
		a.codexFlow = nil
		if strings.TrimSpace(msg.word) != "" {
			line = msg.word
		}
		a.settleConnectWord("codex", line, false)
		if a.at(pageSettings) {
			a.modelServiceMessage(line)
		}
	} else {
		a.modelServiceMessage(line)
	}
	if nextModel != "" {
		a.moveConversationToConnectedModel(nextModel)
	} else if renamedNext != "" {
		a.moveConversationToConnectedModel(renamedNext)
	}
	if a.connPanel.open {
		a.connPanel.adopt(a.connectionRows())
	}
	if a.at(pageSettings) {
		a.sheet.sources = a.sources
		a.sheet.rows = a.sheet.registry.Rows()
		a.sheet.build()
	}
	a.touch()
}

// reprefixRenamedModel carries a rename down to the model ids already picked
// under the old name, and returns the conversation's new id, empty when this
// conversation was not on the old name. Routing keys on the Written prefix of
// a model id, so a connection renamed from mybox to newbox leaves every
// mybox/... pick pointing at a service that no longer exists: they resolve to
// no service and answer on the default one instead. The pick follows the
// rename under its own id — the SAME model, spelled the new way — and never
// the connection's preferred one, because a rename is a change of label and
// not a change of mind.
func (a *app) reprefixRenamedModel(oldWritten, newWritten string) string {
	oldWritten, newWritten = strings.TrimSpace(oldWritten), strings.TrimSpace(newWritten)
	if oldWritten == "" || newWritten == "" || strings.EqualFold(oldWritten, newWritten) {
		return ""
	}
	if modelUsesService(a.deferredModelServiceModel, oldWritten) {
		a.deferredModelServiceModel = config.ReprefixModelID(a.deferredModelServiceModel, oldWritten, newWritten)
	}
	if !modelUsesService(a.model, oldWritten) {
		return ""
	}
	next := config.ReprefixModelID(a.model, oldWritten, newWritten)
	if a.state == stateWorking {
		// A working turn's model is frozen until it settles; the move waits
		// with it ([app.applyDeferredModelServiceMove]). The pick is rewritten,
		// not cleared: it names the same model under the new prefix, and
		// dropping it would leave the settle to invent its own answer.
		//
		// BUT IT MAY ONLY TAKE A FREE SLOT. Whatever is pending — a move to
		// another connection, or the same move already carried under the new
		// name by the branch above — is what the person last asked for and
		// was promised out loud ([deferredMoveWord]); this re-spelling is
		// bookkeeping, and bookkeeping never outranks what the person last
		// asked for.
		if strings.TrimSpace(a.deferredModelServiceModel) == "" {
			a.deferredModelServiceModel = next
		}
		return ""
	}
	return next
}

// reprefixStoredModels carries a rename across every STORED model id that
// carries the old Written name. The conversation slot's id is the one that
// answers the next turn ([app.reprefixRenamedModel]); these are the stored
// ones: the reasoning levels the surface learned, the role pins, the fallback
// chain, and the capability slots the registry writes into the profile. Miss
// one and it resolves to no service, so the whole string hands itself to the
// default service as a model id: a silent misroute that only fails at send.
func (a *app) reprefixStoredModels(oldWritten, newWritten string) {
	if oldWritten == "" || newWritten == "" || strings.EqualFold(oldWritten, newWritten) {
		return
	}
	// The reasoning table is memory keyed by model id. A stale key costs only
	// a relearn, but the pair moves together and moving it costs nothing.
	for key, level := range a.levels {
		if next := config.ReprefixModelID(key, oldWritten, newWritten); next != key {
			delete(a.levels, key)
			a.levels[next] = level
		}
	}
	for key := range a.levelWanted {
		if next := config.ReprefixModelID(key, oldWritten, newWritten); next != key {
			delete(a.levelWanted, key)
			a.levelWanted[next] = true
		}
	}
	// THE RENAME MOVES EVERY STORED ID THE PROFILE HOLDS: the crew's five tier
	// rows, the fallback chain, the role pins and the capability slots
	// ([config.RenameConnectionModels]). Node pins on already-created tasks and
	// journaled role bindings are history — what ran, not what to run — and
	// are not rewritten.
	changed, err := config.RenameConnectionModels(a.profileDir, oldWritten, newWritten)
	if err != nil {
		a.modelServiceMessage("could not move every model to " + newWritten + " · " + err.Error())
		return
	}
	// THE REASONING TABLE MOVES WITH THE RENAME, and so does everything else
	// the rename moved: the fallback chain, the role pins and the capability
	// slots are drawn on the same sheet as the tier rows, so a refresh that
	// waited for a TIER key would leave the panel naming yesterday's rows
	// while the profile holds today's.
	if len(changed) > 0 {
		a.refreshSettings()
	}
}

// conversationModel is the model THIS conversation runs — the one the
// switcher rows read the active connection from. IT IS THE LIVE MODEL AND NOT
// THE PROFILE SLOT: [config.ChatModelAt] is the LAST model any conversation
// settled on, written asynchronously by the engine host, so two tabs on
// different connections would read each other's; and while a turn is working,
// the move this conversation is waiting on ([app.deferredModelServiceModel])
// is where it is going, which is the answer the row owes (switchActiveConnection,
// modelConnectionRows).
func (a *app) conversationModel() string {
	if strings.TrimSpace(a.deferredModelServiceModel) != "" {
		return a.deferredModelServiceModel
	}
	return a.model
}

// listedModelIDs keeps the ids in the service's own order for modelsource's
// single preference rule. The surface metadata stays here; only ids cross the
// package boundary that owns the ordering.
func listedModelIDs(models []Model) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}

// moveConversationToConnectedModel takes the same model-change road a person
// takes through /model, then uses the disconnect receipt's sentence so the two
// automatic moves cannot drift into two accounts of what happened.
func (a *app) moveConversationToConnectedModel(next string) {
	next = strings.TrimSpace(next)
	if next == "" || next == strings.TrimSpace(a.model) {
		return
	}
	was := a.model
	a.switchModel(next, 0)
	a.modelServiceFollowup(serviceMovedWord(was, next))
}

// applyDeferredModelServiceMove spends the one pending move only after the
// answering turn has settled. Clearing it first makes the second settle event
// free and prevents a failed later path from replaying an old connection.
func (a *app) applyDeferredModelServiceMove() {
	next := a.deferredModelServiceModel
	a.deferredModelServiceModel = ""
	was := a.model
	a.moveConversationToConnectedModel(next)
	if a.model == was {
		return
	}
	// THE REFRESH FOLLOWS A MOVE THAT ACTUALLY HAPPENED: an open /connect
	// panel and the Providers tab each read the conversation's model for their
	// active-connection row ([app.connectionRows], [sheet.build]), and a move
	// they did not see would leave the row naming the old target until
	// something else redrew it. A settle with nothing to spend — or whose
	// deferred id is the model already running — changed nothing, and this
	// function runs on every settled turn: a re-adopt there re-ranks the open
	// panel (connectPanel.rank sends the cursor and the scroll back to the
	// top) and clears the second-enter disconnect confirmation standing on a
	// row (connectPanel.adopt clears armed).
	if a.connPanel.open {
		a.connPanel.adopt(a.connectionRows())
	}
	if a.at(pageSettings) {
		a.sheet.build()
	}
}

// moveConversationOrDefer is the ONE move-or-defer step the switcher makes:
// while a turn is working the model is frozen until it settles, so the move
// waits in [app.deferredModelServiceModel] (recorded only when it is a
// change) and the answering turn spends it ([app.applyDeferredModelServiceMove])
// — and the person who pressed enter hears that the press landed
// ([deferredMoveWord]); once idle, the move goes through the one road a model
// change takes ([app.moveConversationToConnectedModel]). A rename keeps its
// own quieter deferral ([app.reprefixRenamedModel]).
func (a *app) moveConversationOrDefer(id, written string) {
	id = strings.TrimSpace(id)
	if a.state == stateWorking {
		if !strings.EqualFold(id, strings.TrimSpace(a.deferredModelServiceModel)) {
			a.deferredModelServiceModel = id
		}
		a.modelServiceFollowup(deferredMoveWord(written))
		return
	}
	// Routing folds case, so an id differing from the running model only in
	// case is the same pick — the deferred guard above already compares with
	// EqualFold, and the idle road does too.
	if !strings.EqualFold(id, strings.TrimSpace(a.model)) {
		a.moveConversationToConnectedModel(id)
	}
}

func serviceOutcomeWord(service string, outcome modelsource.Outcome) string {
	// The terminal command and panel report the same check. Config owns the
	// sentence so neither surface can acquire a private spelling of the outcome.
	return config.ConnectionOutcomeWord(service, outcome)
}

func serviceConnectedWord(service string, outcome modelsource.Outcome) string {
	// Older panel call sites ask only for success. They still go through the
	// shared formatter rather than keeping a second successful-case sentence.
	outcome.Kind = modelsource.OutcomeConnected
	return config.ConnectionOutcomeWord(service, outcome)
}

func engineVariableWord(name string) string {
	return "the engine process reads $" + strings.TrimSpace(name) + " from its own environment"
}

// serviceCannotPayWord sends a turn-time payment refusal through the same
// formatter as the connection result that first proved the account.
func serviceCannotPayWord(service, vendorSaid string) string {
	return config.ConnectionOutcomeWord(service, modelsource.Outcome{
		Kind: modelsource.OutcomeAccountCannotPay, VendorSaid: vendorSaid,
	})
}

// serviceWordFor is the name a person calls the service that serves model —
// its written segment, and the default service's own name for an unqualified
// id. Empty only when no service can be resolved at all.
func (a *app) serviceWordFor(model string) string {
	if a.sources.Empty() {
		return ""
	}
	service, _ := a.sources.For(model)
	return strings.TrimSpace(service.Source.Written)
}

func serviceAnsweringWord(service string) string {
	return service + " is answering right now · try again in a moment"
}

func serviceDisconnectedWord(service string) string {
	return service + " is disconnected · its models are gone from the picker"
}

func serviceMovedWord(was, next string) string {
	return "this conversation was on " + was + " · it is now on " + next
}

// deferredMoveWord is what a move that has to wait says: the answering turn
// freezes the model until it settles (app.applyDeferredModelServiceMove
// spends the move), and a person pressing enter deserves to know the press
// landed rather than vanished.
func deferredMoveWord(written string) string {
	return "the move to " + written + " waits for this turn to finish"
}

func serviceStrandedWord(was string) string {
	return "this conversation was on " + was + " and nothing else here can take it · connect a service or pick a model"
}

func (a *app) modelServiceMessage(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if a.at(pageSettings) {
		a.sheet.msg = line
		return
	}
	a.note(line)
}

func (a *app) modelServiceFollowup(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if a.at(pageSettings) {
		if a.sheet.msg == "" {
			a.sheet.msg = line
		} else {
			a.sheet.msg += "\n" + line
		}
		return
	}
	a.note(line)
}

func (a *app) disconnectModelService(id string) {
	connected, ok := a.sources.ByID(id)
	if !ok || strings.EqualFold(id, modelsource.DefaultID) {
		return
	}
	written := strings.ToLower(strings.TrimSpace(connected.Source.Written))
	if a.state == stateWorking && modelUsesService(a.model, connected.Source.Written) {
		a.modelServiceMessage(serviceAnsweringWord(written))
		return
	}
	was := a.model
	conversationUsed := modelUsesService(was, connected.Source.Written)
	if err := config.DisconnectService(a.profileDir, id); err != nil {
		a.modelServiceMessage(err.Error())
		return
	}
	if modelUsesService(a.deferredModelServiceModel, connected.Source.Written) {
		a.deferredModelServiceModel = ""
	}
	delete(a.sourceModels, id)
	a.reloadModelSources()
	a.modelServiceMessage(serviceDisconnectedWord(written))
	if !conversationUsed {
		// The disconnect acknowledgement is the whole answer.
	} else if next, ok := a.reachableModelAfterDisconnect(); ok {
		a.switchModel(next, 0)
		a.modelServiceFollowup(serviceMovedWord(was, next))
	} else {
		a.modelServiceFollowup(serviceStrandedWord(was))
	}
	if a.connPanel.open {
		a.connPanel.adopt(a.connectionRows())
	}
	if a.at(pageSettings) {
		a.sheet.sources = a.sources
		a.sheet.build()
	}
}

func (a *app) modelIsDirect(model string) bool {
	if a.sources.Empty() {
		return false
	}
	service, _ := a.sources.For(model)
	return service.Source.ID != "" && !strings.EqualFold(service.Source.ID, modelsource.DefaultID)
}

// defaultServiceHasKey is whether the default service can answer a turn: the
// profile carries a key for it, the connected default holds one of its own, or
// the service itself accepts a blank. It is the ONE spelling of that rule, so
// the switcher's refusal ([switchActiveConnection]) and the prerequisite
// reading below cannot drift apart.
func (a *app) defaultServiceHasKey() bool {
	if config.APIKeyConfigured(a.profileDir) {
		return true
	}
	service := a.sources.Default()
	return strings.TrimSpace(service.Key) != "" || service.Source.KeyOptional
}

// defaultProviderNeeded is the ONE answer to "does this person still owe us an
// OpenRouter key before they can say anything". A CONNECTED SERVICE THAT CAN
// CARRY THE CONVERSATION IS THE PROVIDER: opening the default service's browser
// door over it would stop a working turn in order to collect a key that turn
// does not use. Blank is usable only when the service says so explicitly, which
// is how a local Ollama seat remains a real seat rather than a broken key row.
func (a *app) defaultProviderNeeded() bool {
	if a.routerConnect == nil || a.defaultServiceHasKey() {
		return false
	}
	return !a.connectedServiceCarriesModel()
}

// connectedServiceCarriesModel is the service half of the prerequisite: the
// current model resolves away from the default service and that account can
// answer. Keeping it named lets the non-browser setup seam describe its own
// missing key without teaching that older seam a second version of this rule.
func (a *app) connectedServiceCarriesModel() bool {
	service, _ := a.sources.For(a.model)
	if service.Source.ID == "" || strings.EqualFold(service.Source.ID, modelsource.DefaultID) {
		return false
	}
	return strings.TrimSpace(service.Key) != "" || service.Source.KeyOptional
}

func modelUsesService(model, written string) bool {
	model = strings.TrimSpace(model)
	written = strings.TrimSpace(written)
	return written != "" && strings.HasPrefix(strings.ToLower(model), strings.ToLower(written)+"/")
}

func (a *app) reachableModelAfterDisconnect() (string, bool) {
	services := a.sources.All()
	if len(services) == 0 {
		return "", false
	}
	if strings.TrimSpace(services[0].Key) != "" {
		return config.ChatDefaultAt(a.profileDir), true
	}
	for _, service := range services[1:] {
		if strings.TrimSpace(service.Key) == "" && service.Source.ID != "ollama" {
			continue
		}
		for _, model := range a.sourceModels[service.Source.ID] {
			if chatModel(model) {
				return service.Qualify(model.ID), true
			}
		}
	}
	return "", false
}

// modelServiceRows is the Providers tab's compact reading: one ordinary sheet
// row per connected service, with the key's safe spelling, region, and order.
func modelServiceRows(profileDir string, sources modelsource.Set) []*modelServiceRow {
	byID := make(map[string]modelsource.Connected)
	for _, service := range sources.All() {
		byID[strings.ToLower(service.Source.ID)] = service
	}
	rows := config.PersistedSources(profileDir)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Order < rows[j].Order })
	out := make([]*modelServiceRow, 0, len(rows))
	for _, persisted := range rows {
		service, ok := byID[strings.ToLower(persisted.ID)]
		if !ok {
			continue
		}
		parts := make([]string, 0, 5)
		if service.Door.Name != "" {
			parts = append(parts, service.Door.Name)
		}
		switch {
		case persisted.KeyEnv != "":
			parts = append(parts, "$"+persisted.KeyEnv)
		case persisted.Key != "":
			parts = append(parts, "key")
		}
		if persisted.Region != "" {
			parts = append(parts, persisted.Region)
		}
		parts = append(parts, "order "+itoa(persisted.Order))
		if persisted.ID == "z-ai" && service.Door.ID == "coding-plan" {
			parts = append(parts, "Zhipu lists the tools its plan covers; codeaf is not listed, and its request has been drafted but not sent.")
		}
		out = append(out, &modelServiceRow{
			id: persisted.ID, name: service.Source.Written, value: strings.Join(parts, " · "),
		})
		if service.Overflow != nil && !service.Door.Metered {
			out = append(out, &modelServiceRow{
				id: persisted.ID, name: "when the plan is paused", value: service.PlanPaused, planPause: true,
			})
		}
	}
	return out
}

type modelServiceRow struct {
	id        string
	name      string
	value     string
	planPause bool
	// addCustom marks the Providers tab's add row, the door onto a NEW custom
	// connection that always mints an instance (startCustomAdd). Enter on an
	// ordinary service row is that row's EDIT; this row is the only one that
	// mints.
	addCustom bool
	// switcher marks the active-connection row: one reading of which service
	// this conversation answers on, and the one enter that moves it to the
	// next (switchActiveConnection).
	switcher bool
}

// customAddRowID is the add row's sentinel id. A SENTINEL ROW ID LIVES OUTSIDE
// THE custom/custom- NAMESPACE, so modelsource.IsCustomID is false for it and
// config.PrepareCustomSource — which mints custom or custom-<name> — can never
// mint it: cursor and armed state are keyed by row id, and an id that could
// also be a connection named `add` would make one row answer for two
// (customAddRow, modelConnectionRows, connectAct).
const customAddRowID = "new-custom-connection"

// connectionSwitchRowID is the /connect panel's active-connection row. A
// SENTINEL ID BY THE SHARED CONTRACT: it does not start with `custom` and it
// equals no vendored id, so modelsource.IsCustomID is false for it and
// config.PrepareCustomSource can never mint it.
const connectionSwitchRowID = "switch-connection"

// customAddRow is the Providers tab's add row. It stands whether or not any
// connection exists, because a profile with no custom connection yet is the
// one that needs the door; the flow behind it is the ONE flow /connect runs,
// never a second implementation of the mint or the connect.
func customAddRow() *modelServiceRow {
	return &modelServiceRow{
		id: customAddRowID, name: "add custom connection",
		value: "an OpenAI-compatible base URL · a name of your own", addCustom: true,
	}
}

// conversationModel is the sheet's reading of this conversation's model,
// through the door raiseSettings handed it. A sheet with no door is a sheet
// nobody is drawing — [sheet.liveModel] is set on the way up and the drop
// clears the whole sheet — so the empty answer is the never-drawn case
// rather than a fallback.
func (s *sheet) conversationModel() string {
	if s.liveModel == nil {
		return ""
	}
	return s.liveModel()
}

// switchReading is the one reading the three switcher sites share: which
// service this conversation answers on ([Connected], hasActive), which one
// enter would take it to ([nextConnection]), and the sentence the row says
// ([switchSentence]). The gate is the ring's own — false when it holds fewer
// than two services, because a ring of one has nowhere to move to
// (connectionSwitcherRow, modelConnectionRows, switchActiveConnection).
type connectionSwitch struct {
	active    modelsource.Connected
	hasActive bool
	next      modelsource.Connected
	sentence  string
}

func switchReading(model string, sources modelsource.Set) (connectionSwitch, bool) {
	ring := switchableConnections(sources)
	if len(ring) < 2 {
		return connectionSwitch{}, false
	}
	active, hasActive := config.ActiveConnectionFor(model, sources)
	next := nextConnection(ring, active, hasActive)
	reading := connectionSwitch{
		active: active, hasActive: hasActive,
		next:     next,
		sentence: switchSentence(hasActive, active, next),
	}
	return reading, true
}

// connectionSwitcherRow is the Providers tab's active-connection row, nil
// when no custom connection is connected, because a row that could never do
// anything is a row that only says there is nothing here. THE ACTIVE
// CONNECTION IS DERIVED FROM THIS CONVERSATION'S MODEL, NEVER STORED
// (config.ActiveConnectionFor): the model this conversation runs — or the
// move it is waiting out a working turn on — already carries the answer in
// its Written prefix, and a stored key would be a second source of truth that
// can disagree with the model actually in use. The sheet has no app, so the
// model reaches it the way `sources` does, as a door handed over on the way
// up ([sheet.conversationModel], raiseSettings): a snapshot taken when the
// panel opened would go on naming the model the conversation left behind.
func (s *sheet) connectionSwitcherRow() *modelServiceRow {
	reading, ok := switchReading(s.conversationModel(), s.sources)
	if !ok {
		return nil
	}
	return &modelServiceRow{name: "active connection", value: reading.sentence, switcher: true}
}

// serviceWrittenWord is the name a person calls a service in the switcher's
// sentence: its Written spelling, and the service's own display name when the
// Written word is empty. It is lowercased because the sentence reads as
// speech, the way every word below a service row does.
func serviceWrittenWord(service modelsource.Connected) string {
	word := strings.TrimSpace(service.Source.Written)
	if word == "" {
		word = strings.TrimSpace(service.Source.Name)
	}
	return strings.ToLower(word)
}

// switchSentence is what the active-connection row says: where the
// conversation is answering and where enter takes it.
func switchSentence(hasActive bool, active, next modelsource.Connected) string {
	to := serviceWrittenWord(next)
	if !hasActive {
		return "enter moves this conversation onto " + to
	}
	return "answering on " + serviceWrittenWord(active) + " · enter moves it to " + to
}

// switchableConnections is the ring the switcher walks: the default service
// first, then the custom connections [customInstances] answers — the same
// walk, spelled once, with the default service put in front of it. THE
// SWITCHER IS THE CUSTOM-CONNECTION FEATURE'S DOOR, so its ring is the default
// service and the connections a person added; vendored non-custom services
// (deepseek, ollama and the rest) are not in it — they are reached by
// connecting them and then picking a model, which /model is the door for, and
// a switcher that wrapped through every vendored service would turn one enter
// into a walk across doors /model already owns.
func switchableConnections(sources modelsource.Set) []modelsource.Connected {
	if sources.Empty() {
		return nil
	}
	customs := customInstances(sources)
	ring := make([]modelsource.Connected, 0, len(customs)+1)
	ring = append(ring, sources.Default())
	return append(ring, customs...)
}

// nextConnection is the service a switcher enter lands on: the one after the
// active one by id, wrapping; the first ring entry that is not the active
// service when the conversation is on nothing, or on something that is not in
// the ring. With a ring holding one service the entry is that service itself,
// which is why the row is offered only when the ring holds two or more
// (connectionSwitcherRow, modelConnectionRows).
func nextConnection(ring []modelsource.Connected, active modelsource.Connected, hasActive bool) modelsource.Connected {
	for at, service := range ring {
		if hasActive && strings.EqualFold(strings.TrimSpace(service.Source.ID), strings.TrimSpace(active.Source.ID)) {
			return ring[(at+1)%len(ring)]
		}
	}
	return ring[0]
}

// customInstances is the custom connections a person has added, in the set's
// own order. It is the add row's gate and NOT the switcher's ring
// (switchableConnections), which always holds the default service too and
// would show a second door onto the first connection on a profile that has
// only the catalog's unconnected row (modelConnectionRows).
func customInstances(sources modelsource.Set) []modelsource.Connected {
	out := make([]modelsource.Connected, 0, 2)
	for _, service := range sources.All() {
		if modelsource.IsCustomID(service.Source.ID) {
			out = append(out, service)
		}
	}
	return out
}

// switchActiveConnection is the switcher row's answer: move this conversation
// onto the next service in the ring's preferred model, through a.switchModel
// — the ONE road a model change takes and the same write the /model picker
// makes. The active connection is derived from the slot's model, so rewriting
// the slot IS the switch; there is nothing else to store.
func (a *app) switchActiveConnection() {
	reading, ok := switchReading(a.conversationModel(), a.sources)
	if !ok {
		return
	}
	next := reading.next
	written := serviceWrittenWord(next)
	// A DEFAULT SERVICE WITH NO USABLE KEY CANNOT TAKE THE CONVERSATION: the
	// model list may be cached, so the move would succeed here and the next
	// send would open a key door nobody asked for. The refusal keeps the
	// conversation where it is and names the door a key goes through
	// (defaultServiceHasKey); a custom connection's key was checked when it
	// connected.
	if !modelsource.IsCustomID(next.Source.ID) && !a.defaultServiceHasKey() {
		a.modelServiceMessage(written + " has no key yet · add one in /connect first")
		return
	}
	var preferred string
	if modelsource.IsCustomID(next.Source.ID) {
		preferred = next.Source.PreferredModel(next.Door, listedModelIDs(a.modelsForConnectedService(next)))
	} else {
		preferred = next.Source.PreferredModel(next.Door, listedModelIDs(a.defaultServiceModels()))
	}
	if strings.TrimSpace(preferred) == "" {
		a.modelServiceMessage("no model list for " + written + " yet · reconnect it (ctrl+r on its row) or type a model id in /model")
		return
	}
	id := preferred
	if modelsource.IsCustomID(next.Source.ID) {
		id = next.Qualify(preferred)
	}
	// THE DEFAULT SERVICE'S OWN IDS ARE ALREADY BARE: [Connected.Qualify] says
	// so, and it is spelled out here because the two roads below look like
	// they might differ. The id is rewritten only when it IS a change; a
	// conversation that answers on the ring's next service already keeps the
	// pick it has.
	a.moveConversationOrDefer(id, written)
	if a.at(pageSettings) {
		a.sheet.build()
	}
	a.touch()
}

func (a *app) cyclePlanPause(id string) {
	rows := config.PersistedSources(a.profileDir)
	for index := range rows {
		if !strings.EqualFold(rows[index].ID, id) {
			continue
		}
		if strings.TrimSpace(rows[index].PlanPaused) == config.PlanPausedUseMeter {
			rows[index].PlanPaused = config.PlanPausedWait
		} else {
			rows[index].PlanPaused = config.PlanPausedUseMeter
		}
		if err := config.WriteSources(a.profileDir, rows); err != nil {
			a.modelServiceMessage(err.Error())
			return
		}
		a.reloadModelSources()
		a.sheet.sources = a.sources
		a.sheet.build()
		return
	}
}

func (a *app) reconnectModelService(id string) tea.Cmd {
	source, ok := a.modelSource(id)
	if !ok {
		return nil
	}
	for _, row := range config.PersistedSources(a.profileDir) {
		if strings.EqualFold(row.ID, id) {
			return a.beginModelConnect(modelConnectDraft{source: source, row: row, sheet: a.at(pageSettings)})
		}
	}
	return nil
}

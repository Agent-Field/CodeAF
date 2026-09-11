package tui3

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	modelcatalog "github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/connect"
	"github.com/Agent-Field/aforge-v2/internal/modelsource"
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
		a.modelSuggestions = make(map[string]string)
		return
	}
	a.modelCatalog = modelsource.Vendored()
	a.sourceModels = make(map[string][]Model)
	a.modelSuggestions = make(map[string]string)
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
	return rows
}

func modelConnectionStatus(source modelsource.Source, held bool) connect.Status {
	need := "key"
	switch {
	case source.ID == "ollama":
		need = ""
	case source.ID == "custom":
		need = "address · key"
	case len(source.Regions) > 0:
		need = "region · key"
	}
	service := connect.Service{
		ID: modelConnectionID(source.ID), Name: source.Name, Blurb: need,
		Auth: connect.AuthKey, Category: "models",
	}
	if source.ID == "ollama" {
		service.Auth = "none"
	}
	return connect.Status{Service: service, Connected: held, Account: source.Written, KeyEnv: source.KeyEnv}
}

func modelServiceTag(row connect.Status) string {
	id, ok := modelConnectionSource(row.ID)
	if !ok {
		return ""
	}
	switch id {
	case "ollama":
		return ""
	case "custom":
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
	for _, existing := range config.PersistedSources(a.profileDir) {
		if strings.EqualFold(existing.ID, source.ID) {
			persisted = existing
			break
		}
	}
	if suggestion := a.modelSuggestions[strings.ToLower(source.ID)]; suggestion != "" {
		persisted.Written = suggestion
	}
	draft := &modelConnectDraft{source: source, row: persisted, sheet: fromSheet}
	a.modelDraft = draft
	switch {
	case len(source.Regions) > 0:
		draft.step = modelConnectRegion
		a.showModelEntry(newModelEntry(row.ID, source.Name, "region", regionAnswers(source), false), fromSheet)
		return nil
	case source.ID == "custom":
		draft.step = modelConnectAddress
		a.showModelEntry(newModelEntry(row.ID, source.Name, "base URL", nil, false), fromSheet)
		return nil
	case source.ID == "ollama":
		return a.beginModelConnect(*draft)
	default:
		draft.step = modelConnectKey
		a.showModelEntry(newModelEntry(row.ID, source.Name, "key", nil, true), fromSheet)
		return nil
	}
}

func newModelEntry(id, name, blank string, answers []string, secret bool) *keyEntry {
	return &keyEntry{id: id, name: strings.ToLower(name), blank: blank, answers: answers, secret: secret}
}

func regionAnswers(source modelsource.Source) []string {
	answers := make([]string, 0, len(source.Regions))
	for _, region := range source.Regions {
		answers = append(answers, region.Name)
	}
	return answers
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

// modelEntryAnswer advances a region/address answer to the key box, or starts
// the checked connection once the last answer has been supplied.
func (a *app) modelEntryAnswer(entry *keyEntry) tea.Cmd {
	draft := a.modelDraft
	if draft == nil {
		return nil
	}
	answer := entry.value()
	if answer == "" {
		a.showModelEntry(entry, draft.sheet)
		return nil
	}
	switch draft.step {
	case modelConnectRegion:
		region := ""
		for _, candidate := range draft.source.Regions {
			if strings.EqualFold(answer, candidate.ID) || strings.EqualFold(answer, candidate.Name) {
				region = candidate.ID
				break
			}
		}
		if region == "" {
			a.modelServiceMessage("pick one of: " + strings.Join(regionAnswers(draft.source), ", "))
			a.showModelEntry(entry, draft.sheet)
			return nil
		}
		draft.row.Region = region
		draft.step = modelConnectKey
		a.showModelEntry(newModelEntry(modelConnectionID(draft.source.ID), draft.source.Name, "key", nil, true), draft.sheet)
		return nil
	case modelConnectAddress:
		parsed, err := url.Parse(answer)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			a.modelServiceMessage("that is not a base URL")
			a.showModelEntry(entry, draft.sheet)
			return nil
		}
		draft.row.Address = strings.TrimRight(answer, "/")
		draft.row.Written = modelServiceSlug(parsed.Hostname())
		draft.step = modelConnectKey
		a.showModelEntry(newModelEntry(modelConnectionID(draft.source.ID), draft.source.Name, "key", nil, true), draft.sheet)
		return nil
	case modelConnectKey:
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

func modelServiceSlug(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	parts := strings.Split(host, ".")
	if len(parts) > 2 {
		parts = parts[:len(parts)-1]
		host = parts[len(parts)-1]
	} else if len(parts) > 0 {
		host = parts[0]
	}
	var out []rune
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
		case len(out) > 0 && out[len(out)-1] != '-':
			out = append(out, '-')
		}
	}
	word := strings.Trim(string(out), "-")
	if word == "" {
		return "custom"
	}
	return word
}

func (a *app) beginModelConnect(draft modelConnectDraft) tea.Cmd {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	dir := a.profileDir
	authors := modelAuthorSegments(a.defaultServiceModels())
	refresh := a.serviceModelRefresh
	return func() tea.Msg {
		outcome, err := config.ConnectService(ctx, dir, draft.row, draft.source, authors)
		models := modelsFromListedIDs(outcome.ModelIDs)
		if err == nil && outcome.Kind == modelsource.OutcomeConnected && outcome.Listed {
			// Resolve the row back through config after ConnectService writes it.
			// That is the one door which owns key and address precedence; rebuilding
			// a Connected here would create a second, subtly different account door.
			connected, found := config.ResolveSources(dir, "", "").ByID(draft.source.ID)
			if found {
				if refresh != nil {
					if refreshed, refreshErr := refresh(ctx, connected, models); refreshErr == nil && len(refreshed) > 0 {
						models = refreshed
					}
				} else {
					seed := make([]modelcatalog.Model, 0, len(models))
					for _, model := range models {
						seed = append(seed, modelcatalog.Model{ID: model.ID})
					}
					_ = modelcatalog.Remember(modelcatalog.Options{
						Source: draft.source.ID, BaseURL: connected.Address, Dir: dir,
					}, seed)
					catalog, refreshErr := modelcatalog.Refresh(ctx, modelcatalog.Options{
						Source: draft.source.ID, BaseURL: connected.Address, APIKey: connected.Key, Dir: dir,
					})
					if refreshed := surfaceModels(catalog.ModelsNow()); refreshErr == nil && len(refreshed) > 0 {
						models = refreshed
					}
					_ = WriteModelCacheFor(draft.source.ID, connected.Address, models)
				}
			}
		}
		return modelConnectResultMsg{
			service: draft.source.ID, name: draft.source.Name, written: draft.row.Written,
			outcome: outcome, models: models, err: err,
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
	switch msg.outcome.Kind {
	case modelsource.OutcomeConnected, modelsource.OutcomeAccountCannotPay:
		a.reloadModelSources()
		if connected, ok := a.sources.ByID(msg.service); ok {
			service = strings.ToLower(connected.Source.Written)
		}
		if msg.outcome.Kind == modelsource.OutcomeConnected {
			a.sourceModels[msg.service] = cleanModels(msg.models)
		}
		line = serviceOutcomeWord(service, msg.outcome)
	case modelsource.OutcomeCollides:
		a.modelSuggestions[strings.ToLower(msg.service)] = msg.outcome.Suggestion
		line = serviceOutcomeWord(service, msg.outcome)
	default:
		line = serviceOutcomeWord(service, msg.outcome)
	}
	a.modelServiceMessage(line)
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

func serviceOutcomeWord(service string, outcome modelsource.Outcome) string {
	switch outcome.Kind {
	case modelsource.OutcomeConnected:
		return serviceConnectedWord(service, outcome)
	case modelsource.OutcomeRefused:
		line := service + " refused that key"
		if said := truncateVendorWords(outcome.VendorSaid, 120); said != "" {
			line += " — " + said
		}
		return line
	case modelsource.OutcomeAccountCannotPay:
		return serviceCannotPayWord(service, outcome.VendorSaid)
	case modelsource.OutcomeUnanswered:
		return service + " did not answer · nothing was saved"
	case modelsource.OutcomeWrongShape:
		return "that is not the shape of a " + service + " key — they start with sk-"
	case modelsource.OutcomeCollides:
		return service + " is a model author on openrouter · connect this as " + outcome.Suggestion
	}
	return ""
}

func serviceConnectedWord(service string, outcome modelsource.Outcome) string {
	line := service + " is connected"
	if !outcome.Listed || outcome.Models <= 0 {
		return line
	}
	return line + " · " + itoa(outcome.Models) + " " + plural("model", outcome.Models)
}

// serviceCannotPayWord is the ONE sentence for an authenticated account with no
// funds, and it is shared by the two moments a person meets it: the connect
// row, and a turn that a vendor refused for the same reason.
//
// IT NAMES THE SERVICE AND NEVER THE STATUS. `error: API error (429): …` is
// what the turn drew before this existed — three pieces of machinery vocabulary
// on a line a person reads, and a number that tells them nothing they can act
// on. The vendor's own words are the only part that says what to do, and they
// go through verbatim.
func serviceCannotPayWord(service, vendorSaid string) string {
	line := strings.TrimSpace(service) + " accepted the key but the account cannot pay"
	if said := truncateVendorWords(vendorSaid, 120); said != "" {
		line += " — " + said
	}
	return line
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

func serviceStrandedWord(was string) string {
	return "this conversation was on " + was + " and nothing else here can take it · connect a service or pick a model"
}

func truncateVendorWords(words string, limit int) string {
	words = strings.TrimSpace(words)
	runes := []rune(words)
	if limit <= 0 || len(runes) <= limit {
		return words
	}
	cut := limit
	for cut > 0 && !unicode.IsSpace(runes[cut]) {
		cut--
	}
	if cut == 0 {
		cut = limit
	}
	return strings.TrimSpace(string(runes[:cut]))
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
		return config.DefaultModel, true
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
		parts := make([]string, 0, 3)
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
		out = append(out, &modelServiceRow{
			id: persisted.ID, name: service.Source.Written, value: strings.Join(parts, " · "),
		})
	}
	return out
}

type modelServiceRow struct {
	id    string
	name  string
	value string
}

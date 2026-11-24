package config

import (
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/claudemodel"
	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// DefaultSlot names the kind of work a built-in default model is chosen for.
//
// A default is only a default if it can answer. Every built-in model id in this
// package is an OpenRouter id, and an id without a connected prefix is sent to
// the OpenRouter row, which is always present, key or none. So a profile that
// connected only ChatGPT, or only Ollama, opened its conversations and its
// fallback on models nobody could reach, and every one of them failed with "no
// API key". Titles, routing and the Library follow the conversation model
// (internal/roles' background.go), so a reachable conversation model reaches
// them too. [ReachableDefaultAt] keeps the built-in model
// whenever a connected service can send it, and otherwise picks the model a
// connected service offers that fits the slot.
type DefaultSlot string

const (
	// SlotTalk is the model a conversation opens on: the provider's own
	// preferred model when it lists one, else its strongest.
	SlotTalk DefaultSlot = "talk"
	// SlotFallback is the second model a conversation hops to: a different
	// model, strong enough to finish the work, on the same provider as the talk
	// model where it can be.
	SlotFallback DefaultSlot = "fallback"
)

// ReachableDefaultAt is builtIn when OpenRouter is connected or a connected
// service can send it, else the model a connected service offers for slot.
//
// An OpenRouter key keeps every built-in exactly as it was. The built-ins are
// OpenRouter's models, chosen for it; with OpenRouter connected nothing here
// second-guesses them, and nothing past the key check is read, which keeps the
// common profile's cost to one source resolution.
//
// With no connected service at all it is builtIn unchanged, so a profile that
// has not been set up keeps naming the model setup will connect, and the
// first-run screens keep working. It is never "" when builtIn is not: when the
// connected services can say nothing about their models, builtIn is kept.
func ReachableDefaultAt(profileDir string, slot DefaultSlot, builtIn string) string {
	key := APIKeyAt(profileDir)
	if app, ok := appKey(modelsource.DefaultID); ok {
		key = app
	}
	if strings.TrimSpace(key) != "" {
		return strings.TrimSpace(builtIn)
	}
	sources := ResolveSources(profileDir, key, DefaultBaseURL)
	if openRouterConnected(sources) {
		return strings.TrimSpace(builtIn)
	}
	// Each service's models are read once per call, however many slots and
	// checks below ask for them.
	read := map[string][]catalog.Model{}
	modelsOf := func(service modelsource.Connected) []catalog.Model {
		id := strings.ToLower(strings.TrimSpace(service.Source.ID))
		if rows, ok := read[id]; ok {
			return rows
		}
		rows := serviceModelsAt(profileDir, service)
		read[id] = rows
		return rows
	}
	return chooseDefault(sources, slot, builtIn, ChatModelAt(profileDir), modelsOf)
}

// chooseDefault is [ReachableDefaultAt] over a set of services and a reader of
// each one's models, so it can be checked without a profile on disk.
func chooseDefault(sources modelsource.Set, slot DefaultSlot, builtIn, chosenTalk string, modelsOf func(modelsource.Connected) []catalog.Model) string {
	builtIn = strings.TrimSpace(builtIn)
	if openRouterConnected(sources) {
		return builtIn
	}
	connected := connectedServices(sources)
	if len(connected) == 0 {
		return builtIn
	}
	// With no saved conversation model, the one chosen below is the provider
	// every other slot follows.
	follow := chosenTalk
	if strings.TrimSpace(follow) == "" || !offered(sources, follow, modelsOf) {
		follow = reachableDefault(sources, connected, SlotTalk, "", modelsOf)
	}
	if builtIn != "" && offered(sources, builtIn, modelsOf) && sameProvider(sources, builtIn, follow, modelsOf) {
		return builtIn
	}
	if chosen := reachableDefault(sources, connected, slot, chosenTalk, modelsOf); chosen != "" {
		return chosen
	}
	return builtIn
}

// sameProvider reports whether builtIn is on the provider of the conversation
// model (the saved one, else the one chosen for it), or there is none to
// follow. A built-in another connected account happens to list is not taken
// while the conversation runs elsewhere, so the fallback stays on the
// conversation's provider.
func sameProvider(sources modelsource.Set, builtIn, chosenTalk string, modelsOf func(modelsource.Connected) []catalog.Model) bool {
	chosenTalk = strings.TrimSpace(chosenTalk)
	if chosenTalk == "" || !offered(sources, chosenTalk, modelsOf) {
		return true
	}
	talk, _ := sources.For(stripLevel(chosenTalk))
	builtInService, _ := sources.For(stripLevel(builtIn))
	return strings.EqualFold(talk.Source.ID, builtInService.Source.ID)
}

// openRouterConnected reports whether the OpenRouter row has a key.
func openRouterConnected(sources modelsource.Set) bool {
	service, ok := sources.ByID(modelsource.DefaultID)
	return ok && service.HasCredentials()
}

// reachableDefault is the file-free half of [ReachableDefaultAt]: it chooses
// among the connected services, reading each one's models through modelsOf.
//
// Every slot follows one provider: the provider of the conversation model. That
// is the model the person saved when a connected service can send it; else it
// is chosen here, from a hosted provider (a plan or a keyed service) before a
// local server, because a model on this machine is the one most likely to be
// too small or too slow for a conversation, and in the person's own order
// within each group. The fallback is then taken from that provider first, so a
// profile that connected ChatGPT and Ollama does not answer in ChatGPT and fall
// back to a local model.
func reachableDefault(sources modelsource.Set, connected []modelsource.Connected, slot DefaultSlot, chosenTalk string, modelsOf func(modelsource.Connected) []catalog.Model) string {
	talk, talkService := "", ""
	if chosenTalk = strings.TrimSpace(chosenTalk); chosenTalk != "" && offered(sources, chosenTalk, modelsOf) {
		service, _ := sources.For(stripLevel(chosenTalk))
		talk, talkService = chosenTalk, service.Source.ID
	} else {
		for _, service := range hostedFirst(connected) {
			if models := usableModels(modelsOf(service)); len(models) > 0 {
				talk, talkService = service.Qualify(pickTalk(service, models).ID), service.Source.ID
				break
			}
		}
	}
	if talk == "" {
		return ""
	}
	ordered := preferService(hostedFirst(connected), talkService)
	switch slot {
	case SlotTalk:
		return talk
	case SlotFallback:
		for _, service := range ordered {
			models := usableModels(modelsOf(service))
			if len(models) == 0 {
				continue
			}
			if strings.EqualFold(service.Source.ID, talkService) {
				if fallback := pickFallback(service, models, talk); fallback != "" {
					return fallback
				}
				continue
			}
			// The talk provider offers one model: another provider's own talk model.
			return service.Qualify(pickTalk(service, models).ID)
		}
		// No other model anywhere: the fallback names the conversation model
		// itself. A fallback row naming only the current model keeps the answer
		// on that model, where an empty one would let the catalog suggest a
		// model on a provider nobody connected.
		return talk
	}
	return ""
}

// hostedFirst puts the services that run elsewhere ahead of the ones on this
// machine, keeping the person's order within each group.
func hostedFirst(services []modelsource.Connected) []modelsource.Connected {
	out := make([]modelsource.Connected, 0, len(services))
	for _, local := range []bool{false, true} {
		for _, service := range services {
			if isLocalService(service) == local {
				out = append(out, service)
			}
		}
	}
	return out
}

// isLocalService is Ollama, or a custom endpoint whose address is this machine.
func isLocalService(service modelsource.Connected) bool {
	if strings.EqualFold(service.Source.ID, "ollama") {
		return true
	}
	if !modelsource.IsCustomID(service.Source.ID) {
		return false
	}
	address := strings.ToLower(service.Address)
	for _, host := range []string{"://localhost", "://127.", "://[::1]", "://0.0.0.0"} {
		if strings.Contains(address, host) {
			return true
		}
	}
	return false
}

// connectedServices is every service in the set that can authenticate: the
// OpenRouter row only with a key, every other persisted row with its key or
// sign-in, and a key-optional local server (Ollama) as soon as it is persisted.
func connectedServices(sources modelsource.Set) []modelsource.Connected {
	var out []modelsource.Connected
	for _, service := range sources.All() {
		if service.HasCredentials() {
			out = append(out, service)
		}
	}
	return out
}

// offered reports whether model routes to a connected service that can serve
// it. Routing alone is not enough: a built-in `deepseek/…` id routes to a
// connected DeepSeek account by its prefix, and that account's own list need
// not carry the id. So a service whose models are known must list the model;
// one that can say nothing about its models, and the OpenRouter row, whose
// catalog may still be cold, are trusted on routing alone.
func offered(sources modelsource.Set, model string, modelsOf func(modelsource.Connected) []catalog.Model) bool {
	service, bare := sources.For(stripLevel(model))
	if !service.HasCredentials() {
		return false
	}
	if strings.EqualFold(service.Source.ID, modelsource.DefaultID) {
		return true
	}
	models := modelsOf(service)
	if len(models) == 0 {
		return true
	}
	for _, listed := range models {
		if strings.EqualFold(strings.TrimSpace(listed.ID), bare) {
			return true
		}
	}
	return false
}

func preferService(services []modelsource.Connected, id string) []modelsource.Connected {
	out := make([]modelsource.Connected, 0, len(services))
	for _, service := range services {
		if strings.EqualFold(service.Source.ID, id) {
			out = append(out, service)
		}
	}
	for _, service := range services {
		if !strings.EqualFold(service.Source.ID, id) {
			out = append(out, service)
		}
	}
	return out
}

// stripLevel removes a thinking level (`model:high`) so the id routes, with the
// same rule every call uses ([roles.SplitEffort]); a free suffix (`…:free`) is
// part of the id and stays.
func stripLevel(model string) string {
	bare, _ := roles.SplitEffort(model)
	return bare
}

// serviceModelsAt is what one connected service is known to offer, without
// reaching the network: the coding-agent plan's three vendored models, ChatGPT's
// remembered account list (or its built-in rows), the catalog a listing service
// last returned, and a plan door's documented ids. A service that can say
// nothing but its preferred model offers that one.
func serviceModelsAt(profileDir string, service modelsource.Connected) []catalog.Model {
	switch strings.ToLower(strings.TrimSpace(service.Source.ID)) {
	case claudemodel.ID:
		return claudemodel.Models()
	case "codex":
		if rows := recallCached(CatalogOptionsFor(service, profileDir)); len(rows) > 0 {
			return rows
		}
		rows := make([]catalog.Model, 0, len(codexauth.FallbackModels))
		for _, model := range codexauth.FallbackModels {
			rows = append(rows, codexCatalogRow(model))
		}
		return rows
	}
	if rows := recallCached(CatalogOptionsFor(service, profileDir)); len(rows) > 0 {
		return rows
	}
	return namedModels(service)
}

// recalled is one remembered model list and the cache file's metadata when it
// was read.
type recalled struct {
	size     int64
	modified time.Time
	rows     []catalog.Model
}

var (
	recalledMu sync.Mutex
	recalledAt = map[string]recalled{}
	// listReads and listRanks count the remembered-list decodes and the
	// rankings the defaults have done, without a clock (PERF.md, "Default
	// model lookups").
	listReads atomic.Int64
	listRanks atomic.Int64
)

// recallCached is [catalog.Recall]'s rows, read again only when the cache file
// changes. The defaults are asked for on every paint of the settings sheet and
// under the model-client lock, where a full read and decode of a provider's
// list is not affordable; an unchanged file costs one stat. The rows are shared
// and must not be modified.
func recallCached(options catalog.Options) []catalog.Model {
	path := catalog.CacheFile(options)
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	recalledMu.Lock()
	held, ok := recalledAt[path]
	recalledMu.Unlock()
	if ok && held.size == info.Size() && held.modified.Equal(info.ModTime()) {
		return held.rows
	}
	rows := catalog.Recall(options).ModelsNow()
	listReads.Add(1)
	recalledMu.Lock()
	recalledAt[path] = recalled{size: info.Size(), modified: info.ModTime(), rows: rows}
	recalledMu.Unlock()
	return rows
}

// NoteModelListChanged tells the settings readers that a connected provider's
// model list was written, so a snapshot built from the defaults
// ([ReachableDefaultAt]) is built again: a default read while a listing was
// still arriving must not keep the model it settled on then.
func NoteModelListChanged() { bumpSettingsGeneration() }

// FollowsProviders reports whether a settings row's blank default depends on
// the connected providers, so a surface comparing it against an untouched
// profile's reading would mark it changed when nobody changed it.
//
// Only the fallback models row does: the knowledge, reflex and small-work rows
// read empty until written and follow the model the person chose.
func FollowsProviders(key string) bool {
	return key == KeyModelFallbacks
}

// WrittenAt reports whether the profile holds a value of its own for key,
// blank included.
func WrittenAt(profileDir, key string) bool {
	_, ok := persistedString(profileDir, key)
	return ok
}

// DefaultFallbackAt is codeaf's default fallback for the profile, whatever the
// fallback models row says: the built-in one with OpenRouter connected, else a
// model the connected providers offer.
func DefaultFallbackAt(profileDir string) string {
	return ReachableDefaultAt(profileDir, SlotFallback, DefaultFallbackModel)
}

// namedModels is what a service says it serves without a listing: a plan
// door's documented ids, else its preferred model and the model its connection
// check is sent to.
func namedModels(service modelsource.Connected) []catalog.Model {
	if len(service.Door.Models) > 0 {
		rows := make([]catalog.Model, 0, len(service.Door.Models))
		for _, id := range service.Door.Models {
			rows = append(rows, catalog.Model{ID: id})
		}
		return rows
	}
	// A service that publishes no list still names two models it serves: the
	// preferred one and the one its connection check is sent to, which is a
	// small model on purpose (Qwen's `qwen3.8-flash` beside `qwen3.7-plus`).
	// Offering both gives the fallback a second model to move to.
	var rows []catalog.Model
	for _, id := range []string{service.Source.Preferred, service.Source.ProbeModel} {
		if id = strings.TrimSpace(id); id != "" && (len(rows) == 0 || !strings.EqualFold(rows[0].ID, id)) {
			rows = append(rows, catalog.Model{ID: id})
		}
	}
	return rows
}

// codexCatalogRow keeps a ChatGPT listing row's window and its thinking levels,
// which are the one strength signal that listing gives. The levels are kept for
// ranking only: Known stays false, so the row does not claim a published
// reasoning profile and nothing about how a ChatGPT request is sent changes.
func codexCatalogRow(model codexauth.Model) catalog.Model {
	row := catalog.Model{ID: strings.TrimSpace(model.ID), ContextLength: model.ContextLength, PriceUnknown: true}
	if len(model.ReasoningLevels) > 0 {
		row.Reasoning = catalog.ReasoningProfile{Efforts: append([]string(nil), model.ReasoningLevels...)}
	}
	return row
}

// usableModels keeps the models a conversation can talk to: text in, text out,
// and not an embedding, speech, image or moderation model.
func usableModels(models []catalog.Model) []catalog.Model {
	if len(models) == 0 {
		return nil
	}
	key := listKey{first: &models[0], n: len(models)}
	viewMu.Lock()
	held, ok := usableMemo[key]
	viewMu.Unlock()
	if ok {
		return held
	}
	out := filterUsable(models)
	viewMu.Lock()
	if len(usableMemo) > maxListMemo {
		usableMemo = map[listKey][]catalog.Model{}
	}
	usableMemo[key] = out
	viewMu.Unlock()
	return out
}

// listKey names one list by its backing array and length. The lists the
// defaults rank are shared and never modified ([recallCached]), so an
// unchanged provider list is filtered and ranked once rather than on every
// call; holding the key keeps that array alive, so the address is not reused
// while the entry stands.
type listKey struct {
	first *catalog.Model
	n     int
}

// rankedView is one list ranked from lightest to strongest, with each model's
// strength key in the list's own order.
type rankedView struct {
	ranked []catalog.Model
	keys   [][5]float64
}

const maxListMemo = 256

var (
	viewMu     sync.Mutex
	usableMemo = map[listKey][]catalog.Model{}
	rankMemo   = map[listKey]rankedView{}
)

func filterUsable(models []catalog.Model) []catalog.Model {
	out := make([]catalog.Model, 0, len(models))
	for _, model := range models {
		id := strings.ToLower(strings.TrimSpace(model.ID))
		if id == "" || notChatModel.MatchString(id) {
			continue
		}
		if len(model.OutputModalities) > 0 && !containsFold(model.OutputModalities, "text") {
			continue
		}
		out = append(out, model)
	}
	return out
}

var notChatModel = regexp.MustCompile(`embed|whisper|tts|transcri|(^|[-_/])asr|speech|audio|image|dall-e|moderation|guard|rerank`)

func containsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

// pickTalk is, for a provider that names a starting model (a plan door's
// documented list, or a preferred model), the model a connection starts on by
// the same rule the picker and the terminal's connect use
// ([modelsource.Source.PreferredModel]). A provider that names none (Ollama, a
// custom endpoint) lists its models in no order of strength, so for it, and
// whenever that rule names nothing offered, it is the strongest model offered.
func pickTalk(service modelsource.Connected, models []catalog.Model) catalog.Model {
	listed := make([]string, 0, len(models))
	for _, model := range models {
		listed = append(listed, strings.TrimSpace(model.ID))
	}
	names := len(service.Door.Models) > 0 || strings.TrimSpace(service.Source.Preferred) != ""
	if preferred := service.Source.PreferredModel(service.Door, listed); names && preferred != "" {
		for _, model := range models {
			if strings.EqualFold(strings.TrimSpace(model.ID), preferred) {
				return model
			}
		}
	}
	ranked := rankByStrength(models)
	return ranked[len(ranked)-1]
}

// pickFallback is the strongest model on service other than main, qualified,
// or "" when service offers only main. A variant of main (`glm-5.3[1m]`, the
// same model with a longer window) is used only when nothing else is offered,
// because a fallback is for when main itself is failing.
func pickFallback(service modelsource.Connected, models []catalog.Model, main string) string {
	ranked := rankByStrength(models)
	mainID := strings.ToLower(stripLevel(main))
	variant := ""
	for index := len(ranked) - 1; index >= 0; index-- {
		id := service.Qualify(ranked[index].ID)
		lower := strings.ToLower(id)
		if lower == mainID {
			continue
		}
		if withoutVariant(lower) == withoutVariant(mainID) {
			if variant == "" {
				variant = id
			}
			continue
		}
		return id
	}
	return variant
}

// withoutVariant drops a bracketed variant suffix (`[1m]`) from a model id.
func withoutVariant(id string) string {
	if open := strings.Index(id, "["); open > 0 && strings.HasSuffix(id, "]") {
		return id[:open]
	}
	return id
}

// rankByStrength orders models from lightest to strongest. Strength is read
// from the best signal the provider published for every model in the list, in
// this order: an intelligence index, then a price, then what the names and the
// listing say (family words such as mini, flash or haiku against pro, opus or
// max; a parameter count such as 8b or 70b; how many thinking levels a model
// takes). A published signal counts only when every model carries it: a model
// whose price or index is unknown is not thereby the cheapest or the weakest.
// Models that cannot be told apart keep the provider's own order, the first one
// listed counting as the stronger, since providers list their flagship first.
func rankByStrength(models []catalog.Model) []catalog.Model {
	return rankView(models).ranked
}

// rankView is [rankByStrength] with each model's key, kept per list.
func rankView(models []catalog.Model) rankedView {
	if len(models) == 0 {
		return rankedView{}
	}
	key := listKey{first: &models[0], n: len(models)}
	viewMu.Lock()
	held, ok := rankMemo[key]
	viewMu.Unlock()
	if ok {
		return held
	}
	view := computeRank(models)
	viewMu.Lock()
	if len(rankMemo) > maxListMemo {
		rankMemo = map[listKey]rankedView{}
	}
	rankMemo[key] = view
	viewMu.Unlock()
	return view
}

func computeRank(models []catalog.Model) rankedView {
	listRanks.Add(1)
	useIndex, usePrice := rankingSignals(models)
	type scored struct {
		model catalog.Model
		index int
		key   [5]float64
	}
	rows := make([]scored, len(models))
	keys := make([][5]float64, len(models))
	for index, model := range models {
		keys[index] = strengthKey(model, useIndex, usePrice)
		rows[index] = scored{model: model, index: index, key: keys[index]}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		for k := range rows[i].key {
			if rows[i].key[k] != rows[j].key[k] {
				return rows[i].key[k] < rows[j].key[k]
			}
		}
		return rows[i].index > rows[j].index
	})
	out := make([]catalog.Model, len(rows))
	for index := range rows {
		out[index] = rows[index].model
	}
	return rankedView{ranked: out, keys: keys}
}

// rankingSignals reports which published signals every model in the list
// carries, so a ranking reads only those.
func rankingSignals(models []catalog.Model) (useIndex, usePrice bool) {
	useIndex, usePrice = true, true
	for _, model := range models {
		useIndex = useIndex && model.IntelligenceIndex > 0
		usePrice = usePrice && knownPrice(model) > 0
	}
	return useIndex, usePrice
}

// knownPrice is a model's published price per token, or zero when it is
// unknown or the model is a free route.
func knownPrice(model catalog.Model) float64 {
	if model.PriceUnknown || strings.HasSuffix(strings.ToLower(model.ID), ":free") {
		return 0
	}
	return model.PromptPrice + model.CompletionPrice
}

func strengthKey(model catalog.Model, useIndex, usePrice bool) [5]float64 {
	var key [5]float64
	if useIndex {
		key[0] = model.IntelligenceIndex
	}
	if usePrice {
		key[1] = knownPrice(model)
	}
	key[2] = float64(familyWeight(model.ID))
	key[3] = parameterBillions(model.ID)
	key[4] = float64(len(model.Reasoning.Efforts))
	return key
}

// familyWeight is the hand-written half of the ranking: words providers use for
// their light and their strong models. It matches families, not exact ids, so a
// new model in a known family ranks without a code change.
func familyWeight(id string) int {
	words := modelWords(id)
	weight := 0
	for _, word := range words {
		switch word {
		case "nano", "tiny", "micro", "lightning", "instant":
			weight -= 2
		case "mini", "small", "lite", "light", "flash", "haiku", "air", "fast", "turbo", "luna", "chat":
			weight--
		case "opus", "max", "ultra", "large", "pro", "plus", "reasoner", "thinking", "sol", "terra":
			weight++
		}
	}
	return weight
}

func modelWords(id string) []string {
	id = strings.ToLower(id)
	if slash := strings.LastIndex(id, "/"); slash >= 0 {
		id = id[slash+1:]
	}
	return strings.FieldsFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.')
	})
}

var parameterCount = regexp.MustCompile(`(?:^|[^a-z0-9.])(\d+(?:\.\d+)?)b(?:$|[^a-z])`)

// parameterBillions is the parameter count a local or open model's name says
// (qwen3:8b, llama-3.1-70b), on a log scale, or zero when it says none.
func parameterBillions(id string) float64 {
	match := parameterCount.FindStringSubmatch(strings.ToLower(id))
	if match == nil {
		return 0
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil || value <= 0 {
		return 0
	}
	return math.Log2(value + 1)
}

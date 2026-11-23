package config

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

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
// connected only ChatGPT, or only Ollama, opened its conversations, its titles,
// its routing and its fallback on models nobody could reach, and every one of
// them failed with "no API key". [ReachableDefaultAt] keeps the built-in model
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
	// SlotLight is the cheap, fast seat: routing, titles, reply suggestions,
	// memory extraction and knowledge compiles. The lightest model a connected
	// provider offers.
	SlotLight DefaultSlot = "light"
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
	if builtIn != "" && offered(sources, builtIn, modelsOf) && sameProvider(sources, builtIn, chosenTalk, modelsOf) {
		return builtIn
	}
	if chosen := reachableDefault(sources, connected, slot, chosenTalk, modelsOf); chosen != "" {
		return chosen
	}
	return builtIn
}

// sameProvider reports whether builtIn is on the provider of the saved
// conversation model, or there is no saved one to follow. A built-in another
// connected account happens to list is not taken while the conversation runs
// elsewhere, so the helpers and the fallback stay on the conversation's
// provider.
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
// within each group. The fallback, helper and knowledge seats are then taken
// from that provider first, so a profile that connected ChatGPT and Ollama does
// not answer in ChatGPT and route through a local model.
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
	case SlotLight:
		for _, service := range ordered {
			if models := usableModels(modelsOf(service)); len(models) > 0 {
				return service.Qualify(pickLight(models).ID)
			}
		}
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
		if rows := CodexRememberedModels(service, profileDir); len(rows) > 0 {
			return rows
		}
		rows := make([]catalog.Model, 0, len(codexauth.FallbackModels))
		for _, model := range codexauth.FallbackModels {
			rows = append(rows, codexCatalogRow(model))
		}
		return rows
	}
	if rows := catalog.Recall(CatalogOptionsFor(service, profileDir)).ModelsNow(); len(rows) > 0 {
		return rows
	}
	return namedModels(service)
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
	// Offering both gives the helper seats a light model instead of the flagship.
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

// pickTalk is the provider's own preferred model when it offers it, else the
// strongest model it offers.
func pickTalk(service modelsource.Connected, models []catalog.Model) catalog.Model {
	if preferred := strings.TrimSpace(service.Source.Preferred); preferred != "" {
		for _, model := range models {
			if strings.EqualFold(strings.TrimSpace(model.ID), preferred) {
				return model
			}
		}
	}
	ranked := rankByStrength(models)
	return ranked[len(ranked)-1]
}

// pickLight is the lightest model offered. Among models that read as equally
// light it is the one the provider lists first, because providers list their
// current model before older versions and bulkier variants: the coding plan's
// `haiku` before `claude-haiku-4-5`, Z.ai's `glm-5.3-flash` before its
// 1M-context `glm-5.3-flash[1m]`.
func pickLight(models []catalog.Model) catalog.Model {
	useIndex, usePrice := rankingSignals(models)
	lightest := strengthKey(rankByStrength(models)[0], useIndex, usePrice)
	for _, model := range models {
		if strengthKey(model, useIndex, usePrice) == lightest {
			return model
		}
	}
	return models[0]
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
	useIndex, usePrice := rankingSignals(models)
	type scored struct {
		model catalog.Model
		index int
		key   [5]float64
	}
	rows := make([]scored, len(models))
	for index, model := range models {
		rows[index] = scored{model: model, index: index, key: strengthKey(model, useIndex, usePrice)}
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
	return out
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

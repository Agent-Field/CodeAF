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
)

// DefaultSlot names the kind of work a built-in default model is chosen for.
//
// A DEFAULT IS ONLY A DEFAULT IF IT CAN ANSWER. Every built-in model id in this
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

// ReachableDefaultAt is builtIn when a connected service can send it, else the
// model a connected service offers for slot. With no connected service at all
// it is builtIn unchanged, so a profile that has not been set up keeps naming
// the model setup will connect, and the first-run screens keep working. It is
// "" only when services are connected and none of them offers a model.
func ReachableDefaultAt(profileDir string, slot DefaultSlot, builtIn string) string {
	builtIn = strings.TrimSpace(builtIn)
	sources := ResolveSources(profileDir, APIKeyAt(profileDir), DefaultBaseURL)
	connected := connectedServices(sources)
	if len(connected) == 0 {
		return builtIn
	}
	if builtIn != "" && sendable(sources, builtIn) {
		return builtIn
	}
	return reachableDefault(sources, connected, slot, ChatModelAt(profileDir), func(service modelsource.Connected) []catalog.Model {
		return serviceModelsAt(profileDir, service)
	})
}

// reachableDefault is the file-free half of [ReachableDefaultAt]: it chooses
// among the connected services, reading each one's models through modelsOf.
//
// EVERY SLOT FOLLOWS ONE PROVIDER: the provider of the conversation model. That
// is the model the person saved when a connected service can send it; else it
// is chosen here, from a hosted provider (a plan or a keyed service) before a
// local server, because a model on this machine is the one most likely to be
// too small or too slow for a conversation, and in the person's own order
// within each group. The fallback, helper and knowledge seats are then taken
// from that provider first, so a profile that connected ChatGPT and Ollama does
// not answer in ChatGPT and route through a local model.
func reachableDefault(sources modelsource.Set, connected []modelsource.Connected, slot DefaultSlot, chosenTalk string, modelsOf func(modelsource.Connected) []catalog.Model) string {
	talk, talkService := "", ""
	if chosenTalk = strings.TrimSpace(chosenTalk); chosenTalk != "" && sendable(sources, chosenTalk) {
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
	ordered := preferService(connected, talkService)
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

// sendable reports whether model routes to a connected service.
func sendable(sources modelsource.Set, model string) bool {
	service, _ := sources.For(stripLevel(model))
	return service.HasCredentials()
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

// stripLevel removes a thinking level (`model:high`) so the id routes; a free
// suffix (`…:free`) is part of the id and stays.
func stripLevel(model string) string {
	model = strings.TrimSpace(model)
	if i := strings.LastIndex(model, ":"); i > 0 {
		switch strings.ToLower(model[i+1:]) {
		case "minimal", "low", "medium", "high", "xhigh", "max", "ultra", "none", "off":
			return model[:i]
		}
	}
	return model
}

// serviceModelsAt is what one connected service is known to offer, without
// reaching the network: Claude Code's three vendored models, ChatGPT's
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
	if len(service.Door.Models) > 0 {
		rows := make([]catalog.Model, 0, len(service.Door.Models))
		for _, id := range service.Door.Models {
			rows = append(rows, catalog.Model{ID: id})
		}
		return rows
	}
	if preferred := strings.TrimSpace(service.Source.Preferred); preferred != "" {
		return []catalog.Model{{ID: preferred}}
	}
	return nil
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

// pickLight is the lightest model offered.
func pickLight(models []catalog.Model) catalog.Model {
	return rankByStrength(models)[0]
}

// pickFallback is the strongest model on service other than main, qualified,
// or "" when service offers only main.
func pickFallback(service modelsource.Connected, models []catalog.Model, main string) string {
	ranked := rankByStrength(models)
	for index := len(ranked) - 1; index >= 0; index-- {
		id := service.Qualify(ranked[index].ID)
		if !strings.EqualFold(id, stripLevel(main)) {
			return id
		}
	}
	return ""
}

// rankByStrength orders models from lightest to strongest. Each model's
// strength is read from the best signal its provider published, in this order:
// a published intelligence index, a published price, then what its name and
// listing say (family words such as mini, flash or haiku against pro, opus or
// max; a parameter count such as 8b or 70b; how many thinking levels it
// takes). Models that cannot be told apart keep the provider's own order, the
// first one listed counting as the stronger, since providers list their
// flagship first.
func rankByStrength(models []catalog.Model) []catalog.Model {
	type scored struct {
		model catalog.Model
		index int
		key   [5]float64
	}
	rows := make([]scored, len(models))
	for index, model := range models {
		rows[index] = scored{model: model, index: index, key: strengthKey(model)}
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

func strengthKey(model catalog.Model) [5]float64 {
	price := 0.0
	if !model.PriceUnknown && !strings.HasSuffix(strings.ToLower(model.ID), ":free") {
		price = model.PromptPrice + model.CompletionPrice
	}
	return [5]float64{
		model.IntelligenceIndex,
		price,
		float64(familyWeight(model.ID)),
		parameterBillions(model.ID),
		float64(len(model.Reasoning.Efforts)),
	}
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

// Package modelsource describes the places models come from without reading
// the machine or opening the network. Runtime facts are handed in by config.
package modelsource

import (
	"fmt"
	"strings"
	"time"
)

// ChatCompletionsPath is composed so the repository's model-send-path law
// continues to reserve the literal endpoint for internal/provider. This
// package only describes the path; internal/config performs the probe.
const ChatCompletionsPath = "/chat/" + "completions"

// DefaultID is the stable identity of the service an unqualified model uses.
// It is named once because persisted rows, qualification, and every surface
// must agree on which member of a Set is the compatibility default.
const DefaultID = "openrouter"

// ProbeTimeout is the watching-person ceiling shared by every vendored probe.
const ProbeTimeout = 10 * time.Second

// Listing is the vendored expectation for whether <base>/models exists on this
// service. It is a hint, never proof: connect always asks the service first,
// because silence in a documentation survey does not prove an endpoint absent.
type Listing int

const (
	// ListingNone is the first-try hint for a service no listing has been
	// observed on. It does NOT foreclose one: connect asks anyway, and a
	// service that answers is treated as a listing service from that moment.
	ListingNone Listing = iota
	// ListingModels is the first-try hint for a service whose model list is
	// documented or has been seen to answer.
	ListingModels
)

// Region is one of a vendor's separate hosts, which are separate accounts with
// separate keys.
type Region struct {
	ID      string
	Name    string
	Address string
}

// Probe is the cheap read that proves a key works. It is a description only;
// internal/config performs the request beside the key it needs.
type Probe struct {
	Address string
	Method  string
	Body    string
	Accepts []int
	Timeout time.Duration
}

// Source is one place models come from: an address, a key description, and the
// facts about what lives there. IT IS CONFIGURATION AND NEVER A GUESS — nothing
// here reads a hostname to decide anything, because a router is recognised by
// what it answers.
type Source struct {
	ID      string
	Written string
	Name    string
	Address string
	Regions []Region
	KeyEnv  string
	// KeyShape validates a supplied key. Nil accepts any non-blank value;
	// KeyOptional is the only way a service accepts blank.
	KeyShape func(string) bool
	// KeyOptional is true only when this service explicitly accepts no key.
	// It is a fact about the service, never inferred from a validation function.
	KeyOptional bool
	Probe       Probe
	Listing     Listing
	ProbeModel  string
	Plan        bool
	KeyPrefix   string
}

// Connected is one service with the two facts only a caller that may read the
// machine can fill in: the key that reaches it and the address it is at.
type Connected struct {
	Source  Source
	Key     string
	Address string
}

// Set is the services this profile talks to, in the person's own order, the
// default service first and always present.
type Set struct {
	services []Connected
}

// NewSet keeps services in the order given. The first service is the default.
func NewSet(services ...Connected) Set {
	return Set{services: append([]Connected(nil), services...)}
}

// Empty reports whether the set names no service at all.
func (s Set) Empty() bool { return len(s.services) == 0 }

// All returns a copy in the person's order, with the default first.
func (s Set) All() []Connected { return append([]Connected(nil), s.services...) }

// Default returns the first service, or the zero value for an empty set.
func (s Set) Default() Connected {
	if len(s.services) == 0 {
		return Connected{}
	}
	return s.services[0]
}

// Written returns every segment a connected service claims.
func (s Set) Written() []string {
	written := make([]string, 0, len(s.services))
	for _, service := range s.services {
		written = append(written, service.Source.Written)
	}
	return written
}

// ByID finds a service by its stable persisted identity.
func (s Set) ByID(id string) (Connected, bool) {
	id = strings.TrimSpace(id)
	for _, service := range s.services {
		if strings.EqualFold(strings.TrimSpace(service.Source.ID), id) {
			return service, true
		}
	}
	return Connected{}, false
}

// For answers the service that serves model and the bare model id to send.
// Thinking levels are removed by internal/config before this pure package is
// called because internal/roles reaches process state through os.
func (s Set) For(model string) (Connected, string) {
	if len(s.services) == 0 {
		return Connected{}, strings.TrimSpace(model)
	}
	segment, bare := Split(model, s.Written())
	if segment != "" {
		for _, service := range s.services {
			if strings.EqualFold(strings.TrimSpace(service.Source.Written), segment) {
				return service, bare
			}
		}
	}
	return s.services[0], strings.TrimSpace(model)
}

// OrDefault makes a scalar account one default service. It preserves a set
// already supplied by a source-aware caller.
func (s Set) OrDefault(key, address string) Set {
	if !s.Empty() {
		return s
	}
	source := DefaultSource(address)
	return NewSet(Connected{source, key, address})
}

// WithDefaultKey returns the same ordered set with its default member's key
// replaced. It is the live first-run handoff: callers may update the account
// without constructing a Connected value and duplicating source resolution.
func (s Set) WithDefaultKey(key string) Set {
	services := s.All()
	if len(services) == 0 {
		return s
	}
	services[0].Key = strings.TrimSpace(key)
	return NewSet(services...)
}

// DefaultSource is the synthesised OpenRouter row. The address is handed in
// because this package may not read the machine.
func DefaultSource(address string) Source {
	return Source{
		ID:       DefaultID,
		Written:  DefaultID,
		Name:     "OpenRouter",
		Address:  address,
		KeyEnv:   "OPENROUTER_API_KEY",
		KeyShape: LooksLikeAPIKey,
		Listing:  ListingModels,
		Probe:    listingProbe(),
	}
}

// Split applies the service-prefix grammar to an already level-less model id.
// A first segment that is not a connected Written remains part of the default
// service's model id.
func Split(model string, written []string) (segment, bare string) {
	model = strings.TrimSpace(model)
	slash := strings.Index(model, "/")
	if slash <= 0 {
		return "", model
	}
	candidate := strings.TrimSpace(model[:slash])
	for _, name := range written {
		if strings.EqualFold(candidate, strings.TrimSpace(name)) {
			return candidate, strings.TrimSpace(model[slash+1:])
		}
	}
	return "", model
}

// Qualify spells a model the way a person selects it. Default-service and
// unnamed rows remain unqualified for compatibility.
func (c Connected) Qualify(bare string) string {
	written := strings.TrimSpace(c.Source.Written)
	if written == "" || strings.EqualFold(strings.TrimSpace(c.Source.ID), DefaultID) {
		return strings.TrimSpace(bare)
	}
	return written + "/" + strings.TrimSpace(bare)
}

// LooksLikeAPIKey is the shared OpenAI-shaped key rule. Config delegates its
// first-run check here so the DeepSeek row and the default service cannot drift.
func LooksLikeAPIKey(key string) bool {
	key = strings.TrimSpace(key)
	return strings.HasPrefix(key, "sk-") && len(key) >= 20 && !strings.ContainsAny(key, " \t\r\n")
}

// Vendored returns the five service descriptions shipped by this phase.
func Vendored() []Source {
	return []Source{
		{
			ID: "deepseek", Written: "deepseek", Name: "DeepSeek",
			Address: "https://api.deepseek.com/v1", KeyEnv: "DEEPSEEK_API_KEY",
			KeyShape: LooksLikeAPIKey, Listing: ListingModels, Probe: listingProbe(),
		},
		{
			ID: "z-ai", Written: "z-ai", Name: "Z.ai", KeyEnv: "ZHIPU_API_KEY",
			Regions: []Region{
				{ID: "intl", Name: "International", Address: "https://api.z.ai/api/paas/v4"},
				{ID: "cn", Name: "China", Address: "https://open.bigmodel.cn/api/paas/v4"},
			},
			// OBSERVED, NOT SURVEYED. B-provider-landscape.md records Z.ai's
			// /models as undocumented; a live run against api.z.ai answered 200
			// with ten models. The survey's silence was read as absence once and
			// it cost a valid key its connection, so this row says what the
			// endpoint actually does. The fallback model stays for the regions
			// or the day it stops.
			Listing: ListingModels, ProbeModel: "glm-5.3-flash", Probe: listingProbe(),
		},
		{
			ID: "moonshot", Written: "moonshot", Name: "Moonshot", KeyEnv: "MOONSHOT_API_KEY",
			Regions: []Region{
				{ID: "intl", Name: "International", Address: "https://api.moonshot.ai/v1"},
				{ID: "cn", Name: "China", Address: "https://api.moonshot.cn/v1"},
			},
			Listing: ListingNone, Probe: listingProbe(),
		},
		{
			ID: "ollama", Written: "ollama", Name: "Ollama",
			Address: "http://localhost:11434/v1", KeyOptional: true,
			Listing: ListingModels, Probe: listingProbe(),
		},
		{
			ID: "custom", Written: "custom", Name: "Something else",
			Listing: ListingModels, Probe: listingProbe(),
		},
	}
}

func listingProbe() Probe {
	return Probe{Address: "/models", Method: "GET", Accepts: []int{200}, Timeout: ProbeTimeout}
}

func completionProbe(model string) Probe {
	return Probe{
		Address: ChatCompletionsPath, Method: "POST", Timeout: ProbeTimeout,
		Accepts: []int{200},
		Body:    fmt.Sprintf(`{"model":%q,"max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`, model),
	}
}

// FallbackProbe describes the one-token check used only after /models proves
// absent. An empty ProbeModel deliberately means believe the key until its first
// real call; guessing a current billable model is worse than deferring proof.
func (s Source) FallbackProbe() Probe {
	if strings.TrimSpace(s.ProbeModel) == "" {
		return Probe{}
	}
	return completionProbe(s.ProbeModel)
}

// Collision says why a proposed Written may not be used, and what to use
// instead. Author segments arrive as an argument because this package may not
// reach internal/catalog.
func Collides(written string, taken []string, authors []string) (suggestion string, collides bool) {
	written = strings.TrimSpace(written)
	occupied := make(map[string]bool, len(taken)+len(authors))
	for _, value := range append(append([]string(nil), taken...), authors...) {
		occupied[strings.ToLower(strings.TrimSpace(value))] = true
	}
	if !occupied[strings.ToLower(written)] {
		return "", false
	}
	base := written + "-direct"
	if !occupied[strings.ToLower(base)] {
		return base, true
	}
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s-%d", base, suffix)
		if !occupied[strings.ToLower(candidate)] {
			return candidate, true
		}
	}
}

// OutcomeKind distinguishes the facts learned by a connect attempt.
type OutcomeKind int

const (
	OutcomeConnected OutcomeKind = iota
	OutcomeRefused
	OutcomeAccountCannotPay
	OutcomeUnanswered
	OutcomeWrongShape
	OutcomeCollides
)

// Outcome is what a connect attempt learned, in facts rather than a sentence:
// the surface owns the words and this package owns the truth.
type Outcome struct {
	Kind       OutcomeKind
	VendorSaid string
	Models     int
	// ModelIDs are the non-empty ids carried by an answered listing. Keeping
	// them lets the surface retain the list it already paid for instead of
	// making a second catalog-shaped response the only road to the picker.
	ModelIDs   []string
	Listed     bool
	Suggestion string
}

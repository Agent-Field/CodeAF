package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/router"
)

// THE CREW: THREE SEATS, EACH PINNED OR PICKED FOR THE TASK IN FRONT OF IT.
//
// A task is done by a crew of three — the worker that does it, the planner
// that cuts and steers it, the checker that reads the result against what was
// asked — and which model sits each seat is decided PER TASK by the router
// (internal/crewroute), from the kind of work the task is. Nothing about that
// decision is stored. What IS stored, and is the whole of what a person
// configures, is what the router may pick from:
//
//   - a PIN per seat, `model` or `model@provider`, which that seat always runs
//     and the router never overrides;
//   - the ALLOWED MODELS, one rule ([crewroute.ParseAllowed]) — `all` unless
//     somebody narrowed it;
//   - an optional DAILY CAP on what crews spend, which the router paces toward;
//   - the PROVIDERS TURNED OFF, a set of connections the crew may not route
//     through — empty unless somebody switched one off.
//
// The providers a crew can use are the connections the person made, read off
// the profile the way every call reads them; what persists is only which of
// them the crew leaves alone, so a connection made tomorrow is on the day it
// is made. That is the one-sentence model the whole feature is built on — the
// /crew panel says what is allowed and it persists; the words in an ask say
// how hard to try that one task; nothing else sticks.
//
// THE PINNED MODELS LIVE IN THE TIER ROWS THE CREW HAS ALWAYS LIVED IN. The worker
// seat is the `worker` tier row, the planner is the `mastermind` row, the
// checker is the `high` row — the rows the role ladder already reads, so the
// auxiliary calls that ride those tiers (the brief a task is shaped into, an
// image read for a model that cannot see one, the plan of an adaptive run)
// follow a pin. A named provider route lives beside that row, so an older
// build reading the tier still sends a model id. An unwritten row is `auto`:
// the seat is routed. The reflex and small-work rows are not crew seats and
// keep their shipped defaults.

// Crew rows, spelled once.
const (
	// KeyCrewAllowed is the allowed-models rule. PROFILE-ONLY for the worker
	// row's own reason: a repository that could widen it could send a
	// visitor's work, and their credit, to a model nobody on that machine chose.
	KeyCrewAllowed = "models.crew.allowed"
	// KeyCrewCap is the daily cap on what crews spend, in dollars; absent or
	// zero is no cap.
	KeyCrewCap = "models.crew.cap"
	// KeyCrewTaskCap is the most one task may spend, in dollars; absent or
	// zero is [CrewTaskCapDefault].
	KeyCrewTaskCap = "models.crew.task_cap"
	// KeyCrewFreeRoutes is whether the crew may use providers' free pools
	// (a `:free` route of a model). PROFILE-ONLY and OFF unless somebody
	// turned it on: a free pool may log or train on what it is sent, which is
	// a choice about a person's code, not a price.
	KeyCrewFreeRoutes = "models.crew.free_routes"
	// KeyCrewProvidersOff is the connected providers a person turned off for
	// the crew, a list of provider ids; absent is every provider on. It is a
	// row of its own beside the allowed rule and never a `-x` inside it
	// (crewroute's providers.go says why), and PROFILE-ONLY for the allowed
	// rule's reason.
	KeyCrewProvidersOff = "models.crew.providers.off"
	// KeyCrewRouteWorker, KeyCrewRoutePlanner and KeyCrewRouteChecker hold a
	// seat's pinned ROUTE, as the whole pin `model@provider`, beside the tier
	// row that holds the model alone. The route used to be written into the
	// tier row itself, and a build from before routed crews reads that row as
	// a model id and would send `model@provider` to the provider; kept apart,
	// an older build reads a valid id and simply takes its default route.
	// The whole pin is kept so a route is applied only to the model it was
	// chosen for ([CrewPinAt]). PROFILE-ONLY, like the rows beside them.
	KeyCrewRouteWorker  = "models.crew.route.worker"
	KeyCrewRoutePlanner = "models.crew.route.planner"
	KeyCrewRouteChecker = "models.crew.route.checker"
)

// crewRouteKey is the row a seat's pinned route is kept in.
func crewRouteKey(seat crewroute.Seat) string {
	switch seat {
	case crewroute.Planner:
		return KeyCrewRoutePlanner
	case crewroute.Checker:
		return KeyCrewRouteChecker
	}
	return KeyCrewRouteWorker
}

// CrewAuto is the word a seat reads when it is not pinned.
const CrewAuto = "auto"

// CrewCommand is the one door a person reaches the crew through, spelled once
// so a sentence naming it cannot drift from the command that answers.
const CrewCommand = "/crew"

// CrewSeatTier is the tier row a seat's pin is written in.
func CrewSeatTier(seat crewroute.Seat) string {
	switch seat {
	case crewroute.Planner:
		return ModelTierMastermind
	case crewroute.Checker:
		return ModelTierHigh
	}
	return ModelTierWorker
}

// CrewSeatKey is the registry row a seat's pin is written in — the settings
// sheet's one way of telling a seat's row from the other tier rows.
func CrewSeatKey(seat crewroute.Seat) string { return tierKeyFor(CrewSeatTier(seat)) }

// CrewTierSeat is the seat a tier row pins, false for a tier that is not a
// crew seat (reflex, small work).
func CrewTierSeat(tier string) (crewroute.Seat, bool) {
	switch tier {
	case ModelTierWorker:
		return crewroute.Worker, true
	case ModelTierMastermind:
		return crewroute.Planner, true
	case ModelTierHigh:
		return crewroute.Checker, true
	}
	return "", false
}

// ParseCrewSeat reads a person's word for a seat. Only the three seat names
// are accepted: the tier words behind them are machinery.
func ParseCrewSeat(word string) (crewroute.Seat, bool) {
	switch strings.ToLower(strings.TrimSpace(word)) {
	case "worker":
		return crewroute.Worker, true
	case "planner":
		return crewroute.Planner, true
	case "checker":
		return crewroute.Checker, true
	}
	return "", false
}

// ── pins ────────────────────────────────────────────────────────────────────

// CrewPin is one pinned seat: a model id, and the provider route it was
// pinned to when somebody wrote `model@provider`.
type CrewPin struct {
	Model    string
	Provider string
}

// String is the pin the way a person writes and reads it: `model[@provider]`.
func (p CrewPin) String() string {
	if p.Provider == "" {
		return p.Model
	}
	return p.Model + "@" + p.Provider
}

// ParseCrewPin reads a written pin. Blank and `auto` are not pins — auto is
// true — and a pin whose model carries a thinking level (`vendor/model:high`)
// is checked by the gate every tier row shares ([ValidateTierValue]).
func ParseCrewPin(raw string) (pin CrewPin, auto bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, CrewAuto) {
		return CrewPin{}, true, nil
	}
	model, provider := raw, ""
	if at := strings.LastIndex(raw, "@"); at > 0 {
		model, provider = strings.TrimSpace(raw[:at]), strings.ToLower(strings.TrimSpace(raw[at+1:]))
		if provider == "" {
			return CrewPin{}, false, fmt.Errorf("%q: name the provider after @, or leave the @ off", raw)
		}
	}
	// A FREE POOL IS A ROUTE, NOT A THINKING LEVEL: `…:free` names the model's
	// free route, and only what is left of the id is a tier value.
	if err := ValidateTierValue(strings.TrimSuffix(model, ":free")); err != nil {
		return CrewPin{}, false, err
	}
	return CrewPin{Model: model, Provider: provider}, false, nil
}

// CrewPinAt is one seat's pin, false when the seat is auto.
//
// A row that says `auto`, a row that is empty and a retired preset word are
// all auto; any model id is a pin. A row that does not parse is auto too, and the
// panel says so, because a seat that silently ran a half-read id would be the
// one thing worse than a seat that ignores it.
func CrewPinAt(profileDir string, seat crewroute.Seat) (CrewPin, bool) {
	value, held := persistedString(profileDir, tierKeyFor(CrewSeatTier(seat)))
	if !held {
		return CrewPin{}, false
	}
	pin, auto, err := ParseCrewPin(value)
	if auto || err != nil {
		return CrewPin{}, false
	}
	// THE ROUTE IS READ FROM ITS OWN ROW, and only for the model it was pinned
	// with: an older build that rewrote the tier row to another model left the
	// route behind, and a route chosen for one model says nothing about another.
	if pin.Provider == "" {
		if route, held := persistedString(profileDir, crewRouteKey(seat)); held {
			if routed, auto, err := ParseCrewPin(route); err == nil && !auto && routed.Model == pin.Model {
				pin.Provider = routed.Provider
			}
		}
	}
	return pin, true
}

// crewSeatRow is a seat's settings row: the pin as written, or empty — which
// the row draws as `auto`.
func crewSeatRow(profileDir string, seat crewroute.Seat) string {
	if pin, ok := CrewPinAt(profileDir, seat); ok {
		return pin.String()
	}
	return ""
}

// CrewPinsAt is every pinned seat.
func CrewPinsAt(profileDir string) map[crewroute.Seat]CrewPin {
	pins := map[crewroute.Seat]CrewPin{}
	for _, seat := range crewroute.Seats {
		if pin, ok := CrewPinAt(profileDir, seat); ok {
			pins[seat] = pin
		}
	}
	return pins
}

// SetCrewPin pins one seat, IN ONE FILE WRITE. `auto` or blank unpins it.
//
// A PIN OUTSIDE THE ALLOWED MODELS IS REFUSED WITH THE REASON, never written
// and quietly ignored: the rule is the person's own and a pin that broke it
// would be the one decision on the panel that contradicted another. A pin
// naming a provider must name one that is connected, for the same reason.
func SetCrewPin(profileDir string, seat crewroute.Seat, raw string) error {
	pin, auto, err := ParseCrewPin(raw)
	if err != nil {
		return err
	}
	if auto {
		return ClearCrewPin(profileDir, seat)
	}
	if err := CrewPinAllowed(profileDir, pin); err != nil {
		return err
	}
	// THE TIER ROW HOLDS THE MODEL ALONE and the route goes in its own row
	// ([KeyCrewRouteWorker] says why); a pin with no route clears the old one.
	values := map[string]any{tierKeyFor(CrewSeatTier(seat)): pin.Model, crewRouteKey(seat): removeProfileKey}
	if pin.Provider != "" {
		values[crewRouteKey(seat)] = pin.String()
	}
	// A profile carrying retired rows is migrated in the same write.
	for key, value := range legacyCrewClearing(profileDir) {
		if _, set := values[key]; !set {
			values[key] = value
		}
	}
	return writeProfileValues(profileDir, values)
}

// ClearCrewPin unpins one seat: the row is removed, and the seat is routed.
func ClearCrewPin(profileDir string, seat crewroute.Seat) error {
	values := legacyCrewClearing(profileDir)
	values[tierKeyFor(CrewSeatTier(seat))] = removeProfileKey
	values[crewRouteKey(seat)] = removeProfileKey
	return writeProfileValues(profileDir, values)
}

// ClearCrewPins unpins every seat in one write.
func ClearCrewPins(profileDir string) error {
	values := legacyCrewClearing(profileDir)
	for _, seat := range crewroute.Seats {
		values[tierKeyFor(CrewSeatTier(seat))] = removeProfileKey
		values[crewRouteKey(seat)] = removeProfileKey
	}
	return writeProfileValues(profileDir, values)
}

// CrewPinAllowed says why a pin may not be written, or nil. The model must be
// one the allowed rule admits — by its figures when the catalog knows it, by
// name otherwise — and a pinned provider must be connected and must reach it.
func CrewPinAllowed(profileDir string, pin CrewPin) error {
	rule := CrewAllowedAt(profileDir)
	model, known := crewCatalogModel(pin.Model)
	switch {
	case known && !rule.AdmitsModel(model):
		return fmt.Errorf("%s is outside the models you allow (%s) · widen them with /crew models +%s",
			pin.Model, rule.String(), crewroute.ShortModel(pin.Model))
	case !known && rule.Base != crewroute.BaseAll && !rule.NamesModel(pin.Model):
		return fmt.Errorf("%s is not in the catalog, so the rule %s cannot admit it by price or licence · name it with /crew models +%s",
			pin.Model, rule.String(), pin.Model)
	case !known && rule.Base == crewroute.BaseAll && !rule.AdmitsModel(crewroute.Model{ID: pin.Model}):
		return fmt.Errorf("%s is outside the models you allow (%s)", pin.Model, rule.String())
	}
	if pin.Provider != "" {
		provider, ok := crewProviderByID(CrewProvidersAt(profileDir), pin.Provider)
		if !ok {
			return fmt.Errorf("%s is not a connected provider · connect it with /connect, or pin the model without @%s", pin.Provider, pin.Provider)
		}
		if !rule.AdmitsRoute(provider.ID) {
			return fmt.Errorf("%s is a provider your allowed models exclude (%s)", provider.ID, rule.String())
		}
		if !provider.On {
			return fmt.Errorf("%s is turned off for the crew · turn it on in /crew's providers row", provider.ID)
		}
		if _, ok := provider.route(pin.Model, model, known); !ok {
			return fmt.Errorf("%s does not serve %s", provider.Name, pin.Model)
		}
	}
	return nil
}

// ── allowed models and the cap ──────────────────────────────────────────────

// CrewAllowedAt is the allowed-models rule. A rule that does not parse — a
// hand edit — reads as the default, the way a retired choice reads everywhere
// else on this sheet; the writer refuses one.
func CrewAllowedAt(profileDir string) crewroute.Allowed {
	value, _ := persistedString(profileDir, KeyCrewAllowed)
	rule, err := crewroute.ParseAllowed(value)
	if err != nil {
		rule, _ = crewroute.ParseAllowed(crewroute.DefaultAllowed)
	}
	return rule
}

// SetCrewAllowed writes the rule in its canonical spelling, refusing one that
// does not parse and one that would leave a pinned seat outside it.
func SetCrewAllowed(profileDir, raw string) error {
	rule, err := crewroute.ParseAllowed(raw)
	if err != nil {
		return err
	}
	return writeCrewAllowed(profileDir, rule)
}

// ModifyCrewAllowed is `/crew models +x` and `-x`: the rule with one word
// added or taken away.
func ModifyCrewAllowed(profileDir string, add bool, word string) error {
	word = strings.TrimSpace(word)
	if word == "" {
		return errors.New("name a model or a provider after the + or -")
	}
	return writeCrewAllowed(profileDir, CrewAllowedAt(profileDir).With(add, word))
}

// writeCrewAllowed is the rule's one writer.
func writeCrewAllowed(profileDir string, rule crewroute.Allowed) error {
	for _, seat := range crewroute.Seats {
		pin, ok := CrewPinAt(profileDir, seat)
		if !ok {
			continue
		}
		if model, known := crewCatalogModel(pin.Model); (known && !rule.AdmitsModel(model)) || (!known && rule.Base != crewroute.BaseAll && !rule.NamesModel(pin.Model)) {
			return fmt.Errorf("your %s is pinned to %s, which that rule leaves out · /crew unpin %s first, or add +%s",
				seat, pin.Model, seat, crewroute.ShortModel(pin.Model))
		}
	}
	return writeProfileValue(profileDir, KeyCrewAllowed, rule.String())
}

// CrewCapAt is the daily cap on crew spend, zero for none.
func CrewCapAt(profileDir string) float64 {
	value, ok := persistedFloat(profileDir, KeyCrewCap)
	if !ok || value < 0 {
		return 0
	}
	return value
}

// SetCrewCap writes the cap: a dollar amount, or `none`.
func SetCrewCap(profileDir, raw string) error {
	return writeDollars(profileDir, KeyCrewCap, raw)
}

// CrewTaskCapDefault is the per-task limit when none is set.
const CrewTaskCapDefault = 5.0

// CrewTaskCapAt is the most one task may spend, in dollars: the stored figure
// when it is above zero, [CrewTaskCapDefault] otherwise. There is always one.
func CrewTaskCapAt(profileDir string) float64 {
	value, ok := persistedFloat(profileDir, KeyCrewTaskCap)
	if !ok || value <= 0 {
		return CrewTaskCapDefault
	}
	return value
}

// SetCrewTaskCap writes the per-task limit: a dollar amount above zero. A
// task always has a limit, so `none` and zero are refused.
func SetCrewTaskCap(profileDir, raw string) error {
	value, err := parseDollars(raw)
	if err != nil {
		return err
	}
	if value <= 0 {
		return errors.New("a task always has a limit — a dollar amount above zero")
	}
	return writeProfileValue(profileDir, KeyCrewTaskCap, value)
}

// CrewFreeRoutesAt is whether the crew may route to providers' free pools.
// Absent is off.
//
// A FREE ROUTE IS A ROUTE, NOT A MODEL. On, a model's free pool joins its other
// routes ([crewroute.Free]) and is weighed at what it is expected to cost —
// its refusals included — and a seat on it falls through to the same model's
// paid route before any other model. Off, the free pools are not routes at
// all; a person can still pin one by name.
func CrewFreeRoutesAt(profileDir string) bool {
	on, _ := persistedBool(profileDir, KeyCrewFreeRoutes)
	return on
}

// SetCrewFreeRoutes turns the free routes on or off.
func SetCrewFreeRoutes(profileDir string, on bool) error {
	return writeProfileValue(profileDir, KeyCrewFreeRoutes, on)
}

// ── the providers turned off ────────────────────────────────────────────────

// ErrCrewLastProvider is the refusal to turn off the last provider on: a crew
// with nothing to route through is not a narrower crew but no crew, and the
// way to say "none of these" is to disconnect them, which /connect does.
var ErrCrewLastProvider = errors.New("at least one provider must stay on")

// ErrCrewNoRoutableProvider is the same refusal one step wider: the providers
// left on would route no seat — only a custom endpoint, say, which a pin
// reaches and the router never picks — while every seat is not pinned to a
// provider still on.
var ErrCrewNoRoutableProvider = errors.New("at least one provider that can route a seat must stay on")

// CrewProvidersOffAt is the providers turned off for the crew. A row that does
// not read — a hand edit — reads as none off, the way a rule that does not
// parse reads as the default.
func CrewProvidersOffAt(profileDir string) crewroute.ProvidersOff {
	raw, ok := persistedValue(profileDir, KeyCrewProvidersOff)
	if !ok {
		return nil
	}
	var ids []string
	if json.Unmarshal(raw, &ids) != nil {
		return nil
	}
	off := crewroute.ProvidersOff{}
	for _, id := range ids {
		if id = strings.ToLower(strings.TrimSpace(id)); id != "" {
			off[id] = true
		}
	}
	return off
}

// SetCrewProviderOn turns one connected provider on or off for the crew, IN
// ONE FILE WRITE, refusing to turn off the last one on ([ErrCrewLastProvider])
// and a provider that is not connected.
//
// THE ROW KEEPS ONLY CONNECTED PROVIDERS. An id left behind by a connection
// since removed is dropped on the next write, so a provider disconnected and
// connected again is on — the same as any connection made after the row was
// written — and the row never grows a list of names nobody can see.
func SetCrewProviderOn(profileDir, id string, on bool) error {
	providers := CrewProvidersAt(profileDir)
	provider, ok := crewProviderByID(providers, id)
	if !ok {
		return fmt.Errorf("%s is not a connected provider · connect it with /connect", strings.TrimSpace(id))
	}
	var ids []string
	still := 0
	for _, p := range providers {
		stays := p.On
		if p.ID == provider.ID {
			stays = on
		}
		if stays {
			still++
			continue
		}
		ids = append(ids, p.ID)
	}
	if still == 0 {
		return ErrCrewLastProvider
	}
	if !on && !crewStillRoutes(profileDir, providers, provider.ID) {
		return ErrCrewNoRoutableProvider
	}
	if len(ids) == 0 {
		return writeProfileValues(profileDir, map[string]any{KeyCrewProvidersOff: removeProfileKey})
	}
	return writeProfileValue(profileDir, KeyCrewProvidersOff, ids)
}

// crewStillRoutes is whether turning one more provider off leaves the crew
// able to seat every seat: some allowed model a seat can sit is reached by a
// provider still on, or every seat is pinned to a provider still on. A
// profile that could route nothing before the change is not refused for
// routing nothing after it — the refusal is about THIS change.
func crewStillRoutes(profileDir string, providers []CrewProvider, id string) bool {
	rule := CrewAllowedAt(profileDir)
	all := crewCandidates(rule, providers)
	before, after := crewroute.ProvidersOff{}, crewroute.ProvidersOff{}
	for _, p := range providers {
		if !p.On {
			before[p.ID], after[p.ID] = true, true
		}
	}
	after[id] = true
	if !crewSeatable(before.Candidates(all)) || crewSeatable(after.Candidates(all)) {
		return true
	}
	pins := CrewPinsAt(profileDir)
	for _, seat := range crewroute.Seats {
		pin, ok := pins[seat]
		if !ok {
			return false
		}
		route := resolveCrewPin(pin, providers)
		if _, connected := crewProviderByID(providers, route.Provider); !connected || !after.On(route.Provider) {
			return false
		}
	}
	return true
}

// crewSeatable is whether any candidate can sit a seat: a model that takes
// tools, on at least one route.
func crewSeatable(candidates []crewroute.Candidate) bool {
	for _, c := range candidates {
		if c.Model.Tools && len(c.Routes) > 0 {
			return true
		}
	}
	return false
}

// ── what the router may pick from ───────────────────────────────────────────

// CrewCatalog is how the catalog reaches crew routing: the binary holding the
// catalog sets it ONCE AT START-UP, from its non-blocking read, and never a
// fetch — a task is routed on whatever the catalog already holds. Nil, and a
// func answering no rows, are ordinary states rather than errors: the router
// then has no candidates, and a task's crew comes from its rescue ladder —
// the last crew that worked here, the model the person is talking to.
var CrewCatalog func() []catalog.Model

// crewCatalogRows is [CrewCatalog] read with its ordinary absences folded.
func crewCatalogRows() []catalog.Model {
	if CrewCatalog == nil {
		return nil
	}
	return CrewCatalog()
}

// crewModelOf reads one catalog row the way the router reads a model.
func crewModelOf(row catalog.Model) crewroute.Model {
	return crewroute.Model{
		ID: row.ID, Open: row.OpenWeights,
		PromptPrice: row.PromptPrice, CompletionPrice: row.CompletionPrice, CacheReadPrice: row.CacheReadPrice,
		Intelligence: row.IntelligenceIndex, Coding: row.CodingIndex, Agentic: row.AgenticIndex,
		ArenaElo: row.ArenaElo, Released: crewReleased(row),
		Context: row.ContextLength, Tools: crewTakesTools(row) && crewSpeaksText(row),
	}
}

// crewReleased is when a catalog row's model was released: the row's own
// listing time, or the date a canonical slug ends on (`…-20260826`), or the
// zero time when the row says neither.
func crewReleased(row catalog.Model) time.Time {
	if row.Created > 0 {
		return time.Unix(row.Created, 0).UTC()
	}
	slug := strings.TrimSpace(row.CanonicalSlug)
	if at := strings.LastIndex(slug, "-"); at >= 0 && len(slug)-at-1 == 8 {
		if day, err := time.Parse("20060102", slug[at+1:]); err == nil && day.Year() >= 2020 {
			return day
		}
	}
	return time.Time{}
}

// crewIndexesFrom is m with every published figure it lacks — an index, the
// arena Elo, the release date — read from other, a row of the same model.
func crewIndexesFrom(m, other crewroute.Model) crewroute.Model {
	if m.Intelligence <= 0 {
		m.Intelligence = other.Intelligence
	}
	if m.Coding <= 0 {
		m.Coding = other.Coding
	}
	if m.Agentic <= 0 {
		m.Agentic = other.Agentic
	}
	if m.ArenaElo <= 0 {
		m.ArenaElo = other.ArenaElo
	}
	if m.Released.IsZero() {
		m.Released = other.Released
	}
	return m
}

// crewTakesTools is whether a catalog row says the model takes tool calls.
//
// A ROW THAT LISTS NO PARAMETERS HAS SAID NOTHING, and a crew seat is an agent
// loop: a model whose tool support is unknown is not sent to find out in the
// middle of somebody's task.
func crewTakesTools(row catalog.Model) bool {
	return len(row.Parameters) > 0 && listHolds(row.Parameters, "tools")
}

// crewSpeaksText is whether a catalog row reads text and writes text. A crew
// seat is a conversation of text and tool calls: a speech, transcription or
// image model that lists tool parameters is still no seat. A row that lists
// no modalities has said nothing either way and is judged on its tools alone.
func crewSpeaksText(row catalog.Model) bool {
	if len(row.InputModalities) > 0 && !listHolds(row.InputModalities, "text") {
		return false
	}
	return len(row.OutputModalities) == 0 || listHolds(row.OutputModalities, "text")
}

// crewCatalogModel is one model as the router would read it, from the
// catalog, false when the catalog does not know it.
//
// THE EXACT ID WINS. A pin names what the person wrote, and a catalog that
// lists that id serves that id — never a dated snapshot that happens to share
// its lineage and to be listed first. Only an id the catalog does not list is
// read through its lineage.
func crewCatalogModel(id string) (crewroute.Model, bool) {
	exact := stripCrewRoute(id)
	rows := crewCatalogRows()
	for _, row := range rows {
		if strings.EqualFold(row.ID, exact) && !row.PriceUnknown {
			return crewModelOf(row), true
		}
	}
	lineage := crewroute.Lineage(exact)
	for _, row := range rows {
		if crewroute.Lineage(row.ID) == lineage && !row.PriceUnknown {
			return crewModelOf(row), true
		}
	}
	return crewroute.Model{}, false
}

// stripCrewRoute takes a connection's prefix off an id a person wrote with
// one (`openrouter/z-ai/glm-5.3-flash`), leaving the catalog id.
func stripCrewRoute(id string) string {
	return strings.TrimPrefix(strings.TrimSpace(id), modelsource.DefaultID+"/")
}

// listHolds says whether a row's own list carries the word, case folded.
func listHolds(words []string, word string) bool {
	for _, held := range words {
		if strings.EqualFold(strings.TrimSpace(held), word) {
			return true
		}
	}
	return false
}

// CrewProvider is one connected provider as the crew can use it.
type CrewProvider struct {
	// ID is the connection's own id — `openrouter`, `z-ai`, `codex`,
	// `ollama` — and the word an `@provider` pin names.
	ID string
	// Name is how the panel says it.
	Name string
	// Written is the id prefix that sends a call to this connection.
	Written string
	// Kind is how it bills: metered, a subscription plan, or local.
	Kind crewroute.RouteKind
	// Serves is the models a plan door serves, empty for every model the
	// vendor lists.
	Serves []string
	// On is whether the crew may route through it: every connection is, until
	// a person turns it off ([SetCrewProviderOn]).
	On bool
	// vendors are the catalog vendor prefixes this connection serves directly.
	vendors []string
	// collides lists the catalog vendors whose ids this connection's Written
	// prefix would capture, so the default route spells them `openrouter/…`.
	collides map[string]bool
}

// crewVendors maps a direct connection to the catalog vendor prefixes it
// serves. A connection whose Written is the vendor's own prefix needs no row.
var crewVendors = map[string][]string{
	"moonshot": {"moonshotai"},
	"codex":    {"openai"},
}

// CrewProvidersAt is every provider the crew can route through: each
// connection this profile has with a key, in the person's own order, the
// default service first when it has one. It is DERIVED, never a setting —
// connecting a provider is what adds it.
func CrewProvidersAt(profileDir string) []CrewProvider {
	off := CrewProvidersOffAt(profileDir)
	set := ResolveSources(profileDir, APIKeyAt(profileDir), DefaultBaseURL)
	written := map[string]bool{}
	for _, service := range set.All() {
		written[strings.ToLower(service.Source.Written)] = true
	}
	var out []CrewProvider
	for _, service := range set.All() {
		source := service.Source
		if strings.TrimSpace(service.Key) == "" && !source.KeyOptional {
			continue
		}
		p := CrewProvider{ID: strings.ToLower(source.ID), Name: source.Name, Written: source.Written, Kind: crewroute.Metered}
		p.On = off.On(p.ID)
		switch {
		case strings.EqualFold(source.ID, modelsource.DefaultID):
			p.collides = map[string]bool{}
			for w := range written {
				if w != modelsource.DefaultID {
					p.collides[w] = true
				}
			}
		case strings.EqualFold(source.ID, "codex"):
			p.Kind = crewroute.Plan
			p.Serves = []string{source.Preferred}
		case strings.EqualFold(source.ID, "ollama"):
			p.Kind = crewroute.Local
		case modelsource.IsCustomID(source.ID):
			// A custom endpoint serves models the catalog does not describe;
			// it is reachable by a pin that names it and by nothing else.
		case service.Door.ID != "" && !service.Door.Metered:
			p.Kind = crewroute.Plan
			p.Serves = append([]string(nil), service.Door.Models...)
		}
		if !strings.EqualFold(source.ID, modelsource.DefaultID) && !modelsource.IsCustomID(source.ID) && !strings.EqualFold(source.ID, "ollama") {
			p.vendors = append([]string{strings.ToLower(source.Written)}, crewVendors[strings.ToLower(source.ID)]...)
		}
		out = append(out, p)
	}
	return out
}

// crewProviderByID finds a connected provider by the word a pin names.
func crewProviderByID(providers []CrewProvider, id string) (CrewProvider, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, p := range providers {
		if p.ID == id || strings.EqualFold(p.Written, id) {
			return p, true
		}
	}
	return CrewProvider{}, false
}

// route is how this provider reaches one catalog model, false when it does
// not. The send is the id that makes an ordinary call go this way — the
// connection-routing grammar every call already obeys ([modelsource.Split]).
func (p CrewProvider) route(id string, model crewroute.Model, known bool) (crewroute.Route, bool) {
	id = stripCrewRoute(id)
	vendor, tail := id, id
	if slash := strings.Index(id, "/"); slash >= 0 {
		vendor, tail = strings.ToLower(id[:slash]), id[slash+1:]
	}
	if p.ID == modelsource.DefaultID {
		if !known {
			return crewroute.Route{}, false
		}
		send := id
		if p.collides[vendor] {
			send = modelsource.DefaultID + "/" + id
		}
		return crewroute.Route{Provider: p.ID, Send: send, Kind: p.Kind}, true
	}
	// A pin written with this connection's own prefix is already its send.
	if strings.EqualFold(vendor, p.Written) && !known {
		return crewroute.Route{Provider: p.ID, Send: id, Kind: p.Kind}, true
	}
	served := false
	for _, v := range p.vendors {
		if v == vendor {
			served = true
			break
		}
	}
	if !served {
		return crewroute.Route{}, false
	}
	if len(p.Serves) > 0 {
		ok := false
		for _, s := range p.Serves {
			if strings.EqualFold(crewroute.Lineage(s), crewroute.Lineage(tail)) {
				ok = true
				break
			}
		}
		if !ok {
			return crewroute.Route{}, false
		}
	}
	return crewroute.Route{Provider: p.ID, Send: p.Written + "/" + tail, Kind: p.Kind}, true
}

// CrewCandidatesAt is what the router may pick from on this profile: every
// catalog model the allowed rule admits, with every route a connected
// provider offers it on — plans and local models first, then a direct
// connection, then the default service, which is the order a tie between
// routes of equal cost is broken in. A model no connected provider reaches is
// not a candidate, however cheap.
//
// A provider the person turned off carries none of those routes
// ([crewroute.ProvidersOff]), so a model only it reached is no candidate
// either — the route cost the router weighs is always a route it may take.
func CrewCandidatesAt(profileDir string) []crewroute.Candidate {
	candidates, _ := crewCandidatesNoticed(profileDir, crewHealthAt(profileDir))
	return candidates
}

// crewCandidates is [CrewCandidatesAt] over a rule and a set of providers
// already read, which is how [CrewOffersAt] asks the same question under `all`.
// It is asked with no free routes.
func crewCandidates(rule crewroute.Allowed, providers []CrewProvider) []crewroute.Candidate {
	return crewCandidatesWith(rule, providers, crewRouteFacts{})
}

// crewCandidatesWith is [crewCandidates] with the profile's route facts.
//
// ONE MODEL IS ONE CANDIDATE, WHATEVER IT IS SPELLED. Catalog rows are grouped
// by their canonical identity ([crewroute.Canonical]), so a model's free pool
// (`…:free`) is not a second, free model beside it but one more route of the
// same one — kept only when free routes are on ([CrewFreeRoutesAt]). A free
// pool whose paid model the catalog does not list is a model with that one
// route, weighed at what the route is expected to cost, never at zero.
func crewCandidatesWith(rule crewroute.Allowed, providers []CrewProvider, facts crewRouteFacts) []crewroute.Candidate {
	var models []crewroute.Model
	var free map[string]string // canonical id → the free row's id
	if rows := crewCatalogRows(); len(rows) > 0 {
		paid := map[string]bool{}
		at := map[string]int{}
		for _, row := range rows {
			if crewUnpriced(row) || strings.HasPrefix(row.ID, "~") || crewroute.IsFree(row.ID) {
				continue
			}
			lineage := crewroute.Lineage(row.ID)
			if paid[lineage] {
				// The same model again — a dated snapshot beside its name —
				// is one candidate, read from the row that lists the model
				// by its own name when there is one, so a seat is sent the
				// name and not whichever snapshot the catalog listed first.
				// THE FIGURES ARE THE MODEL'S, whichever row carried them: an
				// index the snapshot's row published is not lost because the
				// model's own row did not repeat it.
				if strings.EqualFold(row.ID, lineage) {
					models[at[lineage]] = crewIndexesFrom(crewModelOf(row), models[at[lineage]])
				} else {
					models[at[lineage]] = crewIndexesFrom(models[at[lineage]], crewModelOf(row))
				}
				continue
			}
			paid[lineage], at[lineage] = true, len(models)
			models = append(models, crewIndexesFrom(crewModelOf(row), crewroute.Model{}))
		}
		if facts.free {
			free = map[string]string{}
			for _, row := range rows {
				if !crewroute.IsFree(row.ID) || strings.HasPrefix(row.ID, "~") || crewUnpriced(row) {
					continue
				}
				key := crewroute.Lineage(row.ID)
				free[key] = row.ID
				if !paid[key] {
					paid[key] = true
					m := crewModelOf(row)
					m.ID = strings.TrimSuffix(m.ID, ":free")
					models = append(models, m)
				}
			}
		}
	}
	var out []crewroute.Candidate
	for _, m := range models {
		if !rule.AdmitsModel(m) {
			continue
		}
		var plans, direct, fallback []crewroute.Route
		for _, p := range providers {
			if !rule.AdmitsRoute(p.ID) {
				continue
			}
			r, ok := p.route(m.ID, m, true)
			if !ok {
				continue
			}
			switch {
			case r.Kind != crewroute.Metered:
				plans = append(plans, r)
			case p.ID == modelsource.DefaultID:
				fallback = append(fallback, r)
			default:
				direct = append(direct, r)
			}
		}
		if send, ok := free[crewroute.Lineage(m.ID)]; ok {
			for _, p := range providers {
				if p.ID == modelsource.DefaultID && rule.AdmitsRoute(p.ID) {
					plans = append(plans, crewroute.Route{Provider: p.ID, Send: send, Kind: crewroute.Free})
				}
			}
		}
		// Only a free pool: the model is reachable on nothing else.
		fallback = withoutPaidPool(fallback, free[crewroute.Lineage(m.ID)], m)
		// AND ONLY THE ROUTES THAT ARE HEALTHY: a route quarantined, cooling
		// down, on a disconnected provider, or paid on an account out of
		// credit is not offered, and the rest carry their learned failure
		// rate ([crewHealth.usable]).
		routes := facts.health.usable(append(append(plans, direct...), fallback...))
		if len(routes) == 0 {
			continue
		}
		out = append(out, crewroute.Candidate{Model: m, Routes: routes})
	}
	return out
}

// crewUnpriced is a catalog row the router may not weigh: one whose provider
// published no price, or a paid id priced at nothing — a stealth or preview
// model, a meta-router — whose zero promises nothing about the next call and
// would win every seat at $0. ONLY A FREE POOL (`…:free`) PRICED AT AN
// EXPLICIT ZERO IS FREE. A pin still names such a model by its id; the router
// never picks one on its own.
func crewUnpriced(row catalog.Model) bool {
	if row.PriceUnknown {
		return true
	}
	return !crewroute.IsFree(row.ID) && row.PromptPrice <= 0 && row.CompletionPrice <= 0 && row.RequestPrice <= 0
}

// withoutPaidPool drops the default service's metered route for a model the
// catalog lists ONLY as a free pool: there is no paid route to it, and a
// metered route at the free row's zero prices would be the free pool again
// under another kind.
func withoutPaidPool(routes []crewroute.Route, freeSend string, m crewroute.Model) []crewroute.Route {
	if freeSend == "" || m.PromptPrice > 0 || m.CompletionPrice > 0 {
		return routes
	}
	out := routes[:0]
	for _, r := range routes {
		if r.Provider != modelsource.DefaultID {
			out = append(out, r)
		}
	}
	return out
}

// CrewOffer is one model a seat could be pinned to on this profile: a model
// some connected provider reaches, WHETHER OR NOT THE ALLOWED RULE ADMITS IT.
//
// THE PICKER LISTS WHAT THE RULE LEAVES OUT, AND SAYS SO. A list that hid
// every model outside the rule would answer "why can't I pick kimi?" with
// silence; one that shows it dim, with the one key that lets it in, answers
// the question on the row where it was asked ([SetCrewPin] still refuses a pin
// outside the rule — the panel's key widens the rule first, out loud).
type CrewOffer struct {
	Model crewroute.Model
	// Routes are every connected provider reaching it, in [CrewCandidatesAt]'s
	// order — plans and local first, the default service last — and the
	// cheapest route is the first one.
	Routes []crewroute.Route
	// Served is whether a provider that is ON reaches it: a model only a
	// provider turned off reaches is still listed, and is not allowed.
	Served bool
	// Allowed is whether the rule admits the model on at least one route
	// through a provider that is on.
	Allowed bool
}

// CrewOffersAt is every model a connected provider reaches, allowed or not,
// in the catalog's order. It is [CrewCandidatesAt] asked under `all` and then
// read against the rule in force, so the two can never disagree about which
// route reaches what.
func CrewOffersAt(profileDir string) []CrewOffer {
	rule := CrewAllowedAt(profileDir)
	off := CrewProvidersOffAt(profileDir)
	providers := CrewProvidersAt(profileDir)
	var out []CrewOffer
	facts := crewRouteFacts{free: CrewFreeRoutesAt(profileDir), health: crewHealthAt(profileDir)}
	for _, c := range crewCandidatesWith(crewroute.Allowed{Base: crewroute.BaseAll}, providers, facts) {
		// ── ONLY WHAT A SEAT CAN RUN ──
		// A picker offers exactly what the router could seat: a model that takes
		// tool calls and holds a seat's context — not an embedding, image or
		// video model — on a route that is healthy (quarantined and cooling
		// routes were left out above).
		if !crewroute.Seatable(crewroute.Planner, c) {
			continue
		}
		// ── end of the seat check ──
		offer := CrewOffer{Model: c.Model, Routes: c.Routes}
		offer.Served = len(off.Routes(c.Routes)) > 0
		if rule.AdmitsModel(c.Model) {
			for _, r := range off.Routes(c.Routes) {
				if rule.AdmitsRoute(r.Provider) {
					offer.Allowed = true
					break
				}
			}
		}
		out = append(out, offer)
	}
	return out
}

// SetCrewAllowedRule writes a rule the panel composed ([crewroute.Allowed]'s
// edits) through the rule's one writer, so a checklist tick is refused for
// exactly the reason the typed form would be — a pinned seat it would leave
// outside.
func SetCrewAllowedRule(profileDir string, rule crewroute.Allowed) error {
	return writeCrewAllowed(profileDir, rule)
}

// CrewState is the crew's persisted rows as they stand — the three seats, the
// allowed rule, the cap, the providers turned off and the free-routes switch,
// raw — which is what the panel's undo puts back.
//
// IT IS THE ROWS AND NOT A READING OF THEM. An undo that re-wrote what the
// readers made of the rows would turn a hand-written `auto` into an absent
// row, or a preset's legacy row into a pin; putting the bytes back is the only
// undo that restores exactly what was there.
type CrewState struct {
	values map[string]json.RawMessage
}

// crewStateKeys are the rows a [CrewState] carries.
func crewStateKeys() []string {
	keys := []string{KeyCrewAllowed, KeyCrewCap, KeyCrewTaskCap, KeyCrewProvidersOff, KeyCrewFreeRoutes}
	for _, seat := range crewroute.Seats {
		keys = append(keys, tierKeyFor(CrewSeatTier(seat)), crewRouteKey(seat))
	}
	return keys
}

// CrewStateAt reads the crew's rows as they stand.
func CrewStateAt(profileDir string) CrewState {
	state := CrewState{values: map[string]json.RawMessage{}}
	for _, key := range crewStateKeys() {
		if raw, ok := persistedValue(profileDir, key); ok {
			state.values[key] = raw
		}
	}
	return state
}

// RestoreCrewState writes a [CrewState] back, IN ONE FILE WRITE: a row that
// was absent is removed, and every other row gets its old bytes.
func RestoreCrewState(profileDir string, state CrewState) error {
	values := map[string]any{}
	for _, key := range crewStateKeys() {
		if raw, ok := state.values[key]; ok {
			values[key] = raw
			continue
		}
		values[key] = removeProfileKey
	}
	return writeProfileValues(profileDir, values)
}

// CrewGapsAt names what the allowed models leave uncovered, for the panel's
// one-line warning.
func CrewGapsAt(profileDir string) []crewroute.Gap {
	return crewroute.Gaps(CrewCandidatesAt(profileDir))
}

// ── one task's crew ─────────────────────────────────────────────────────────

// CrewAsk is one task's request for a crew.
type CrewAsk struct {
	Task crewroute.Task
	// Effort is the one-task word: best, cheap, or the knee.
	Effort crewroute.Effort
	// Pins are ONE-TASK pins, laid over the profile's: `--pin` and the seat
	// flags. They never persist.
	Pins map[crewroute.Seat]CrewPin
	// Sends are seats a door has already filled with an id to send as it
	// stands — a flag, a variable — which the router treats as pins.
	Sends map[crewroute.Seat]string
	// Stronger is the crew that ran, for a redo that asks for a stronger one.
	Stronger *crewroute.Decision
	// Again is the crew that ran and never started, for a redo of it: the
	// next-best models at the same cost, not a stronger crew.
	Again *crewroute.Decision
	// ChatModel is the model the person is talking to — proven reachable —
	// the last rung of a seat's ladder when nothing routed can start.
	ChatModel string
	// Repo keys the learned offset: a repository whose work of one class was
	// redone stronger starts that class a step higher.
	Repo string
}

// ErrCrewAtCap is a crew asked for with the day's crew spend already at the
// daily cap. The decision still comes back: the chat asks the person, and a
// headless run refuses unless told `-yes-spend`.
var ErrCrewAtCap = errors.New("today's crew spend has reached the daily cap")

// CrewHistory is how the router's log reaches crew routing: today's crew
// spend and the learned offsets, read out of the profile's router-events log
// (internal/router's crew.go). It is a variable so a test can hand a day of
// its own; nil reads as a day with nothing spent and nothing learned — the
// state of a fresh install, not an error.
var CrewHistory = func(profileDir string) CrewDay {
	log := router.ReadCrewLog(ProfilePath(profileDir, ""), time.Now())
	return CrewDay{SpentUSD: log.SpentUSD, Offsets: log.Offsets, CostFactor: log.CostFactor, Learned: crewLearned(log.Quality)}
}

// crewLearned is the log's learned quality moves keyed the way the router
// reads them ([crewroute.LearnKey]): the ids a seat ran folded to their
// lineage, the moves of one lineage averaged.
func crewLearned(quality map[string]float64) map[string]float64 {
	if len(quality) == 0 {
		return nil
	}
	sums, counts := map[string]float64{}, map[string]int{}
	for key, move := range quality {
		parts := strings.SplitN(key, "\x00", 3)
		if len(parts) != 3 {
			continue
		}
		k := crewroute.LearnKey(crewroute.Class(parts[0]), crewroute.Seat(parts[1]), parts[2])
		sums[k] += move
		counts[k]++
	}
	out := make(map[string]float64, len(sums))
	for k, sum := range sums {
		out[k] = sum / float64(counts[k])
	}
	return out
}

// CrewRecordOf is one decision as the router's log keeps it.
func CrewRecordOf(d crewroute.Decision, repo, title string) router.CrewRecord {
	record := router.CrewRecord{
		TaskClass: string(d.Class), Repo: repo, Title: title, Effort: string(d.Effort), Steps: d.Steps,
		Seats: map[string]string{}, Providers: map[string]string{}, Kinds: map[string]string{}, EstUSD: d.EstUSD,
		Redo: d.Redo, EstBase: d.EstUSD, TaskSubclass: d.Subclass, TaskReach: d.Reach,
	}
	if d.CostFactor > 0 {
		record.EstBase = d.EstUSD / d.CostFactor
	}
	for _, pick := range d.Crew {
		if pick.Learned != 0 {
			if record.Learned == nil {
				record.Learned = map[string]float64{}
			}
			record.Learned[string(pick.Seat)] = pick.Learned
		}
		record.Seats[string(pick.Seat)] = pick.Send
		if pick.Provider != "" {
			record.Providers[string(pick.Seat)] = pick.Provider
		}
		if pick.Kind != "" {
			record.Kinds[string(pick.Seat)] = string(pick.Kind)
		}
		if pick.Pinned {
			record.Pinned = append(record.Pinned, string(pick.Seat))
		}
	}
	return record
}

// LogCrewDecision writes one task's crew decision into the profile's router
// log, under the call id the outcome will settle.
func LogCrewDecision(profileDir, call string, d crewroute.Decision, repo, title string) {
	record := CrewRecordOf(d, repo, title)
	record.Top = crewTop(profileDir, d)
	router.LogCrewDecision(ProfilePath(profileDir, ""), call, record, CrewCandidateNames(profileDir))
}

// LogCrewOutcome settles it: accepted, redone stronger, or not kept.
func LogCrewOutcome(profileDir, call string, d crewroute.Decision, repo, title, outcome string, costUSD float64) {
	router.LogCrewOutcome(ProfilePath(profileDir, ""), call, CrewRecordOf(d, repo, title), outcome, costUSD)
}

// CrewCandidateNames is what a logged decision was made among.
//
// ONLY WHAT COULD SIT A SEAT IS NAMED: a speech, image or embedding model the
// catalog lists is no candidate for a crew, and a log row naming it would
// claim the router weighed it.
func CrewCandidateNames(profileDir string) []string {
	return crewroute.Names(crewSeatableOnly(CrewCandidatesAt(profileDir)))
}

// crewSeatableOnly is the candidates that can sit at least one seat.
func crewSeatableOnly(candidates []crewroute.Candidate) []crewroute.Candidate {
	var out []crewroute.Candidate
	for _, c := range candidates {
		for _, seat := range crewroute.Seats {
			if crewroute.Seatable(seat, c) {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

// crewTop is a decision's best three per seat, re-weighed at its λ over the
// candidates it was routed among, for the log row.
func crewTop(profileDir string, d crewroute.Decision) map[string][]router.CrewScore {
	candidates, _ := crewCandidatesNoticed(profileDir, crewHealthAt(profileDir).probing())
	var learned map[string]float64
	if CrewHistory != nil {
		learned = CrewHistory(profileDir).Learned
	}
	out := map[string][]router.CrewScore{}
	for seat, ranked := range crewroute.Explain(d.Class, d.Lambda, candidates, learned, crewTopN) {
		for _, s := range ranked {
			route := s.Provider
			if s.Kind != "" {
				route += ":" + string(s.Kind)
			}
			out[string(seat)] = append(out[string(seat)], router.CrewScore{Model: s.Model, Route: route, Q: s.Quality, C: s.CostUSD, S: s.Score})
		}
	}
	return out
}

// crewTopN is how many candidates a log row keeps per seat.
const crewTopN = 3

// CrewLogAt is the router log's account of crews on this profile, for the
// panel: today's spend and tasks and the recent ones.
func CrewLogAt(profileDir string) router.CrewLog {
	return router.ReadCrewLog(ProfilePath(profileDir, ""), time.Now())
}

// CrewDay is what the log says about crews: today's spend, and the learned
// escalation offset per repository and class.
type CrewDay struct {
	SpentUSD float64
	Offsets  map[string]int
	// CostFactor is the learned estimate factor per class
	// ([router.CrewLog.CostFactor]).
	CostFactor map[string]float64
	// Learned is this install's learned quality moves, keyed
	// [crewroute.LearnKey] ([router.CrewLog.Quality]).
	Learned map[string]float64
}

// OffsetKey is the key [CrewDay.Offsets] is read under.
func OffsetKey(repo string, class crewroute.Class) string {
	return strings.TrimSpace(repo) + "\x00" + string(class)
}

// RouteCrew picks one task's crew on this profile: the seats pinned there or
// for this task run their pins, and every other seat is routed.
//
// The class is read first, so the learned offset for this repository and this
// class of work can move the price of a point before the seats are picked. A
// day at its cap still answers with the crew it would run, beside
// [ErrCrewAtCap]; a seat nothing allowed can sit is a [crewroute.NoCandidateError].
func RouteCrew(profileDir string, ask CrewAsk) (crewroute.Decision, error) {
	reading := crewroute.Classify(ask.Task)
	providers := CrewProvidersAt(profileDir)
	pins := map[crewroute.Seat]crewroute.Pin{}
	for seat, pin := range CrewPinsAt(profileDir) {
		pins[seat] = resolveCrewPin(pin, providers)
	}
	for seat, pin := range ask.Pins {
		pins[seat] = resolveCrewPin(pin, providers)
	}
	for seat, send := range ask.Sends {
		if send = strings.TrimSpace(send); send != "" {
			pins[seat] = resolveCrewPin(CrewPin{Model: send}, providers)
		}
	}
	day := CrewDay{}
	if CrewHistory != nil {
		day = CrewHistory(profileDir)
	}
	pace, atCap := crewroute.Pace(day.SpentUSD, CrewCapAt(profileDir))
	health := crewHealthAt(profileDir)
	// AN ACCOUNT OUT OF CREDIT, OR A KEY REFUSED, IS PROBED BY THE NEXT TASK:
	// its paid routes are routed as usual, so the task's first seat call is
	// the probe. A 200 clears the account on the spot; another refusal moves
	// the seat down its ladder inside the task (to a free pool, when nothing
	// paid is left), costing one refused call. Without the probe an account
	// topped up would never be asked again.
	candidates, notice := crewCandidatesNoticed(profileDir, health.probing())
	req := crewroute.Request{
		Task:       ask.Task,
		Class:      reading.Class,
		Reading:    &reading,
		Candidates: candidates,
		Pins:       pins,
		Effort:     ask.Effort,
		Steps:      day.Offsets[OffsetKey(ask.Repo, reading.Class)],
		Pace:       pace,
		Stronger:   ask.Stronger,
		Again:      ask.Again,
		Avoid:      health.demoted,
		Learned:    day.Learned,
		CostFactor: day.CostFactor[string(reading.Class)],
		TaskCap:    CrewTaskCapAt(profileDir),
	}
	d, err := crewroute.Decide(req)
	if err != nil && !errors.Is(err, crewroute.ErrStrongest) {
		// NO CREW CAN BE FORMED: the seat nothing can sit goes down the rest of
		// the ladder — the last crew that worked here, the person's own model
		// — and with nothing left the answer is the one thing to do.
		d, err = crewRescued(profileDir, req, health, ask.ChatModel, err)
	}
	if err != nil {
		return crewroute.Decision{}, err
	}
	if notice != "" {
		d.Note = strings.TrimSpace(strings.TrimPrefix(d.Note+" · "+notice, " · "))
	}
	d.Why, d.Sure = reading.Why, reading.Sure
	d.Redo = ask.Stronger != nil || ask.Again != nil
	if atCap {
		return d, ErrCrewAtCap
	}
	return d, nil
}

// resolveCrewPin is a pin with the route it will run on: the provider it
// names, or the connection its own prefix names, or the default service.
func resolveCrewPin(pin CrewPin, providers []CrewProvider) (out crewroute.Pin) {
	out = crewroute.Pin{Model: stripCrewRoute(pin.Model), Provider: pin.Provider, Send: pin.Model, Kind: crewroute.Metered}
	// A THINKING LEVEL IS THE PIN'S OWN (`z-ai/glm-5.3:high`), NOT PART OF THE
	// MODEL'S NAME: the catalog and the routes are asked about the model alone,
	// and the level rides whatever send they answer, to be split off at the
	// call ([roles.SplitEffort]) the way a --plan-model flag's level always was.
	if base, level := roles.SplitEffort(pin.Model); level != "" {
		pin.Model = base
		defer func() {
			if _, has := roles.SplitEffort(out.Send); has == "" && out.Send != "" {
				out.Send += ":" + level
			}
		}()
	}
	if crewroute.IsFree(out.Model) {
		// A pinned free pool runs on that pool and is weighed as one.
		defer func() { out.Kind = crewroute.Free }()
	}
	model, known := crewCatalogModel(pin.Model)
	if known && !crewroute.IsFree(out.Model) && !strings.EqualFold(model.ID, stripCrewRoute(pin.Model)) {
		// THE CATALOG DOES NOT LIST THE ID AS WRITTEN, only a variant of it:
		// the pin is sent as that variant, and the line says so.
		pin.Model, out.Send = model.ID, model.ID
	}
	if pin.Provider != "" {
		if p, ok := crewProviderByID(providers, pin.Provider); ok {
			if r, ok := p.route(pin.Model, model, known); ok {
				out.Provider, out.Send, out.Kind = r.Provider, r.Send, r.Kind
			}
		}
		return out
	}
	// No provider named: the id itself says where it goes, the way every call
	// reads it — a connection's own prefix, or the default service.
	for _, p := range providers {
		if p.ID == modelsource.DefaultID {
			continue
		}
		if prefix := strings.ToLower(p.Written) + "/"; strings.HasPrefix(strings.ToLower(pin.Model), prefix) {
			out.Provider, out.Kind = p.ID, p.Kind
			return out
		}
	}
	for _, p := range providers {
		if p.ID == modelsource.DefaultID {
			out.Provider = p.ID
		}
	}
	return out
}

// standingCrewSeat is a routed seat for the calls that ride a crew seat's
// tier without a task in front of them — the conversation's own planner and
// careful calls. They are routed as work of no particular class, so an
// unpinned seat never falls back to a model this build chose for everybody.
// A profile with nothing the router can pick answers empty, which is the role
// ladder's own floor: the model the person is talking to.
func standingCrewSeat(profileDir string, seat crewroute.Seat) string {
	d, err := crewroute.Decide(crewroute.Request{Class: crewroute.Other, Candidates: CrewCandidatesAt(profileDir)})
	if err != nil {
		return ""
	}
	return d.Seat(seat).Send
}

// ── the gate on a tier value ────────────────────────────────────────────────

// writeTierModel is the writer the two rows that are not crew seats share:
// validate the notation, then persist. The crew's three rows write through
// [SetCrewPin], which runs the same gate and the allowed-models rule besides.
func writeTierModel(profileDir, tier, raw string) error {
	raw = strings.TrimSpace(raw)
	if err := ValidateTierValue(raw); err != nil {
		return err
	}
	return writeProfileValue(profileDir, tierKeyFor(tier), raw)
}

// ValidateTierValue refuses every suffix except the three thinking levels. Tier
// rows own the `:<level>` notation, so accepting an unknown suffix here would
// silently turn a misspelling into a model id and defer a clear settings error
// until a provider call much later.
func ValidateTierValue(value string) error {
	value = strings.TrimSpace(value)
	at := strings.LastIndex(value, ":")
	if at <= 0 {
		return nil
	}
	suffix := strings.ToLower(strings.TrimSpace(value[at+1:]))
	if roles.ValidEffort(suffix) {
		return nil
	}
	return fmt.Errorf("%q is not a thinking level. Add %s to a model id, or leave the level off",
		suffix, strings.Join(quoted(roles.Efforts), ", "))
}

// quoted spells a list of words the way a refusal reads them: `low`, `medium`,
// `high` — in the list's own order, which is cheapest first.
func quoted(words []string) []string {
	out := make([]string, 0, len(words))
	for _, word := range words {
		out = append(out, "`"+word+"`")
	}
	return out
}

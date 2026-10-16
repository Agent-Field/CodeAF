package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// THE TASK MODELS AS A PERSON CONFIGURES THEM: EVERY MODEL ON EVERY ROUTE.
//
// A row is one model on one connected provider, which is the unit a person
// ticks: GLM 5.3 through OpenRouter and GLM 5.3 through Z.ai are two rows and
// two ticks ([crewroute.Allowed.AdmitsOn]). Every fact on a row is read, never
// estimated:
//
//   - THE PRICE IS THAT PROVIDER'S OWN. OpenRouter's rows carry OpenRouter's
//     catalog price; a direct provider's carry only what its own listing
//     published (a cached listing — nothing is fetched to answer); a plan, a
//     local model or a free pool has no per-token price. A row with none says
//     none. The router's estimates never appear here.
//   - ALLOWED is the rule's verdict on that route, AVAILABLE is whether the
//     route answers right now (connected, on, credit, not resting), and AUTO
//     ELIGIBLE is whether the router would consider it for a task today.
//   - A PROVIDER WHOSE KEY WAS REFUSED, OR WHOSE CREDIT RAN OUT, KEEPS ITS ROWS
//     and their ticks: it is unavailable, not forgotten, and comes back by
//     itself when it answers again.

// ModelPrice is a provider's own price, dollars per million tokens.
type ModelPrice struct {
	InPerM  float64
	OutPerM float64
}

// TaskModelRow is one model on one connected provider's route.
type TaskModelRow struct {
	Model    crewroute.Model
	Name     string
	Provider string
	Kind     crewroute.RouteKind
	Send     string
	// Price is nil when the provider published none.
	Price        *ModelPrice
	Allowed      bool
	Available    bool
	AutoEligible bool
	// Reason is why Auto would not pick the row today; empty when it would.
	Reason crewroute.RejectReason
	// Seats are the roles the model can do on this route.
	Seats map[crewroute.Seat]bool
}

// The provider states and reasons, as a person reads them.
const (
	ProviderStateConnected   = "connected"
	ProviderStateExpired     = "expired"
	ProviderStateNoKey       = "no_key"
	ProviderStateUnavailable = "unavailable"

	ProviderReasonKeyRefused  = "key_refused"
	ProviderReasonSignedOut   = "signed_out"
	ProviderReasonOutOfCredit = "out_of_credit"
	ProviderReasonTurnedOff   = "turned_off"
)

// The role warnings beyond the decision's own ([crewroute.WarningKind]).
const (
	TaskWarnPinUnknown = "pin_unknown"
	TaskWarnNoEligible = "no_eligible"
)

// TaskModelProvider is one provider's header.
type TaskModelProvider struct {
	ID, Name  string
	Connected bool
	Kind      crewroute.RouteKind
	State     string
	Reason    string
	Models    int
	Allowed   int
}

// TaskModelRole is one role as configured.
type TaskModelRole struct {
	Pin      *CrewPin
	Warning  string
	Eligible int
}

// TaskModelsState is the whole configuration, read at one moment.
type TaskModelsState struct {
	// ObservedAt is when the read that produced this state BEGAN, so a slower
	// read that started earlier can never carry a later time than a read, or
	// a write, that started after it.
	ObservedAt time.Time
	Revision   string
	Mode       crewroute.Mode
	One        *CrewPin
	OneWarning string
	Roles      map[crewroute.Seat]TaskModelRole
	Providers  []TaskModelProvider
}

// ErrTaskModelsRead is a read that could not finish inside its context.
var ErrTaskModelsRead = errors.New("task models read timed out")

// ErrTaskModelsMoving is a read the profile kept changing under: every try saw
// a different revision at its end than at its start. No part of any try is
// answered.
var ErrTaskModelsMoving = errors.New("task models kept changing while they were read · read them again")

// taskModelsReadTries is how many times a read starts again when the rows
// moved under it.
const taskModelsReadTries = 3

// taskModelsClock stamps an observation; taskModelsMidRead runs between the
// revision read at the start of a try and the one at its end. Both exist for a
// test to put a write exactly there; production leaves the hook nil.
var (
	taskModelsClock   = time.Now
	taskModelsMidRead func()
)

// taskModelRowCheck is how many rows are built between two looks at the
// context.
const taskModelRowCheck = 256

// TaskModelRowsAt is every row, providers in the order the router reads them,
// each provider's models in the catalog's order.
func TaskModelRowsAt(ctx context.Context, profileDir string) ([]TaskModelRow, error) {
	return taskModelRows(ctx, profileDir)
}

// taskModelRows builds the rows; a nil context is never cut.
func taskModelRows(ctx context.Context, profileDir string) ([]TaskModelRow, error) {
	return taskModelRowsWith(ctx, profileDir, crewHealthAt(profileDir))
}

// taskModelRowsWith is [taskModelRows] under one health reading already made.
func taskModelRowsWith(ctx context.Context, profileDir string, health crewHealth) ([]TaskModelRow, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	providers := CrewProvidersAt(profileDir)
	rule := CrewAllowedAt(profileDir)
	facts := crewRouteFacts{free: CrewFreeRoutesAt(profileDir), health: health}
	off := CrewProvidersOffAt(profileDir)
	eligible := map[string]bool{}
	for _, c := range off.Candidates(crewCandidatesWith(rule, providers, facts)) {
		for _, r := range c.Routes {
			eligible[crewroute.Lineage(c.Model.ID)+"\x00"+r.Provider] = true
		}
	}
	models := taskCatalogModels()
	var out []TaskModelRow
	built := 0
	for _, p := range providers {
		if err := ctx.Err(); err != nil {
			return nil, ErrTaskModelsRead
		}
		for _, entry := range models {
			if built++; built%taskModelRowCheck == 0 && ctx.Err() != nil {
				return nil, ErrTaskModelsRead
			}
			m := entry.model
			r, ok := p.route(m.ID, m, true)
			if !ok {
				continue
			}
			row := TaskModelRow{Model: m, Name: entry.name, Provider: p.ID, Kind: r.Kind, Send: r.Send, Seats: map[crewroute.Seat]bool{}}
			candidate := crewroute.Candidate{Model: m, Routes: []crewroute.Route{r}}
			for _, seat := range crewroute.Seats {
				row.Seats[seat] = crewroute.Seatable(seat, candidate)
			}
			if !row.Seats[crewroute.Worker] && !row.Seats[crewroute.Planner] && !row.Seats[crewroute.Checker] {
				// Only what can do a role is a choice: not an image, speech or
				// embedding model, not one too small to hold a role's context.
				continue
			}
			var priced bool
			row.Price, priced = taskRowPrice(p, r, entry)
			row.Allowed = rule.AdmitsOn(m, p.ID)
			_, resting := health.blocked[r.Send]
			providerDown := !p.On || health.disconnected[p.ID] || (r.Kind == crewroute.Metered && health.unaffordable[p.ID])
			row.Available = !providerDown && !resting
			row.AutoEligible = eligible[crewroute.Lineage(m.ID)+"\x00"+p.ID]
			switch {
			case !row.Allowed:
				row.Reason = crewroute.RejectNotAllowed
			case !priced:
				row.Reason = crewroute.RejectNoPrice
			case providerDown:
				row.Reason = crewroute.RejectProviderUnavailable
			case resting:
				row.Reason = crewroute.RejectRouteUnavailable
			case !row.AutoEligible:
				row.Reason = crewroute.RejectRouteUnavailable
			}
			if row.Reason != "" {
				row.AutoEligible = false
			}
			out = append(out, row)
		}
	}
	return out, nil
}

// taskCatalogModel is one model of the catalog, once per lineage.
type taskCatalogModel struct {
	model  crewroute.Model
	name   string
	priced bool
	// in and out are the catalog's own prices, per token: OpenRouter's.
	in, out float64
}

// taskCatalogModels is every model the catalog lists, once each, PRICED OR
// NOT: a model with no price is a row a person may tick and pin, which the
// router alone never weighs. Floating aliases and free pools are not models of
// their own.
func taskCatalogModels() []taskCatalogModel {
	var out []taskCatalogModel
	at := map[string]int{}
	for _, row := range crewCatalogRows() {
		if strings.HasPrefix(row.ID, "~") || crewroute.IsFree(row.ID) {
			continue
		}
		lineage := crewroute.Lineage(row.ID)
		entry := taskCatalogModel{model: crewModelOf(row), name: row.Name, priced: !crewUnpriced(row)}
		if entry.priced {
			entry.in, entry.out = row.PromptPrice, row.CompletionPrice
		}
		if i, seen := at[lineage]; seen {
			// The model's own row wins over a dated snapshot of it, and keeps
			// whatever figures the snapshot published.
			if strings.EqualFold(row.ID, lineage) {
				entry.model = crewIndexesFrom(entry.model, out[i].model)
				out[i] = entry
			} else {
				out[i].model = crewIndexesFrom(out[i].model, entry.model)
			}
			continue
		}
		at[lineage] = len(out)
		out = append(out, entry)
	}
	return out
}

// taskRowPrice is a row's price as ITS OWN provider published it, nil when
// it published none, and whether the route has a price Auto may weigh. The
// default service's price is its catalog's; a direct metered route's is its
// own listing's ([CrewProvider.ownPrice]) — a published zero is a price of
// zero, an absence is no price; a plan, a local model or a free pool bills no
// tokens and needs none.
func taskRowPrice(p CrewProvider, r crewroute.Route, entry taskCatalogModel) (*ModelPrice, bool) {
	if fact, needs := p.ownPrice(r); needs {
		if !fact.Known {
			return nil, false
		}
		return &ModelPrice{InPerM: fact.Prompt * 1e6, OutPerM: fact.Completion * 1e6}, true
	}
	if r.Kind != crewroute.Metered {
		return nil, entry.priced
	}
	if !entry.priced {
		return nil, false
	}
	return &ModelPrice{InPerM: entry.in * 1e6, OutPerM: entry.out * 1e6}, true
}

// TaskModelProvidersAt is every provider a person could use for tasks: each
// connected one, then each one this build can connect that has no key yet.
func TaskModelProvidersAt(profileDir string, rows []TaskModelRow) []TaskModelProvider {
	return taskModelProviders(profileDir, rows, crewHealthAt(profileDir))
}

// taskModelProviders is [TaskModelProvidersAt] under one health reading.
func taskModelProviders(profileDir string, rows []TaskModelRow, health crewHealth) []TaskModelProvider {
	connected := CrewProvidersAt(profileDir)
	var out []TaskModelProvider
	seen := map[string]bool{}
	for _, p := range connected {
		view := TaskModelProvider{ID: p.ID, Name: p.Name, Connected: true, Kind: p.Kind, State: ProviderStateConnected}
		switch {
		case health.disconnected[p.ID] && p.Kind == crewroute.Plan:
			view.State, view.Reason = ProviderStateExpired, ProviderReasonSignedOut
		case health.disconnected[p.ID]:
			view.State, view.Reason = ProviderStateExpired, ProviderReasonKeyRefused
		case health.unaffordable[p.ID] && p.Kind == crewroute.Metered:
			view.State, view.Reason = ProviderStateUnavailable, ProviderReasonOutOfCredit
		case !p.On:
			view.State, view.Reason = ProviderStateUnavailable, ProviderReasonTurnedOff
		}
		for _, row := range rows {
			if row.Provider == p.ID {
				view.Models++
				if row.Allowed {
					view.Allowed++
				}
			}
		}
		out = append(out, view)
		seen[p.ID] = true
	}
	candidates := append([]modelsource.Source{modelsource.DefaultSource(DefaultBaseURL)}, modelsource.Vendored()...)
	for _, source := range candidates {
		id := strings.ToLower(source.ID)
		if seen[id] || source.KeyOptional || modelsource.IsCustomID(source.ID) || id == "codex" {
			continue
		}
		seen[id] = true
		out = append(out, TaskModelProvider{ID: id, Name: source.Name, State: ProviderStateNoKey})
	}
	return out
}

// taskModelsKeys are every row the task models are read from — the pins, the
// routes, the rule, the providers turned off, the free routes, the mode, the
// one model and the retired rows a write migrates — so a revision moves when
// any of them does. THE CAPS ARE NOT AMONG THEM: they are the Spending
// settings' rows, and a budget change is not a change to the task models.
func taskModelsKeys() []string {
	keys := []string{KeyCrewAllowed, KeyCrewProvidersOff, KeyCrewFreeRoutes, KeyTaskModelsMode, KeyTaskModelsOne,
		legacyKeyCrew, legacyKeyCrewPick, legacyKeyCrewSource}
	for _, seat := range crewroute.Seats {
		keys = append(keys, tierKeyFor(CrewSeatTier(seat)), crewRouteKey(seat))
	}
	sort.Strings(keys)
	return keys
}

// taskModelsSnapshot is those rows' bytes as they stand; an absent row is nil.
func taskModelsSnapshot(profileDir string) map[string]json.RawMessage {
	snapshot := map[string]json.RawMessage{}
	for _, key := range taskModelsKeys() {
		raw, ok := persistedValue(profileDir, key)
		if ok {
			snapshot[key] = append(json.RawMessage(nil), raw...)
		} else {
			snapshot[key] = nil
		}
	}
	return snapshot
}

// taskModelsRevision names a snapshot.
func taskModelsRevision(snapshot map[string]json.RawMessage) string {
	h := sha256.New()
	for _, key := range taskModelsKeys() {
		raw, ok := snapshot[key]
		h.Write([]byte(key))
		if ok && raw != nil {
			h.Write([]byte{1})
			h.Write([]byte(strings.TrimSpace(string(raw))))
		}
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// TaskModelsRevisionAt is the revision of the rows as they stand.
func TaskModelsRevisionAt(profileDir string) string {
	return taskModelsRevision(taskModelsSnapshot(profileDir))
}

// TaskModelsAt is the whole configuration with its rows, read inside ctx, as
// ONE CONSISTENT OBSERVATION: the rows' revision is read when a try starts and
// again when it ends, and a try the profile moved under is thrown away whole
// and started again — never answered half old, half new. Route health is read
// once per try, so every row, header and warning of one answer agree.
func TaskModelsAt(ctx context.Context, profileDir string) (TaskModelsState, []TaskModelRow, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for try := 0; try < taskModelsReadTries; try++ {
		if ctx.Err() != nil {
			return TaskModelsState{}, nil, ErrTaskModelsRead
		}
		observed := taskModelsClock()
		revision := TaskModelsRevisionAt(profileDir)
		state, rows, err := taskModelsOnce(ctx, profileDir, revision)
		if err != nil {
			return TaskModelsState{}, nil, err
		}
		if taskModelsMidRead != nil {
			taskModelsMidRead()
		}
		if TaskModelsRevisionAt(profileDir) == revision {
			state.ObservedAt = observed
			return state, rows, nil
		}
	}
	return TaskModelsState{}, nil, ErrTaskModelsMoving
}

// taskModelsOnce is one try of [TaskModelsAt].
func taskModelsOnce(ctx context.Context, profileDir, revision string) (TaskModelsState, []TaskModelRow, error) {
	health := crewHealthAt(profileDir)
	rows, err := taskModelRowsWith(ctx, profileDir, health)
	if err != nil {
		return TaskModelsState{}, nil, err
	}
	state := TaskModelsState{Revision: revision, Mode: TaskModelsModeAt(profileDir), Roles: map[crewroute.Seat]TaskModelRole{}}
	providers := CrewProvidersAt(profileDir)
	rule := CrewAllowedAt(profileDir)
	if one, ok := TaskOneModelAt(profileDir); ok {
		state.One = &one
		state.OneWarning = taskPinWarning(providers, rows, one, nil, health)
	}
	saved := CrewPinsAt(profileDir)
	for _, seat := range crewroute.Seats {
		role := TaskModelRole{}
		seen := map[string]bool{}
		for _, row := range rows {
			if row.AutoEligible && row.Seats[seat] && !seen[crewroute.Lineage(row.Model.ID)] {
				seen[crewroute.Lineage(row.Model.ID)] = true
				role.Eligible++
			}
		}
		if pin, ok := saved[seat]; ok {
			role.Pin = &pin
			role.Warning = taskPinWarning(providers, rows, pin, &rule, health)
		}
		if role.Eligible == 0 && role.Pin == nil {
			role.Warning = TaskWarnNoEligible
		}
		state.Roles[seat] = role
	}
	state.Providers = taskModelProviders(profileDir, rows, health)
	return state, rows, nil
}

// taskPinWarning is what is wrong with a saved pin, or nothing. A rule is
// asked for a role's pin and not for the one model, which the allowed models
// do not govern.
func taskPinWarning(providers []CrewProvider, rows []TaskModelRow, pin CrewPin, rule *crewroute.Allowed, health crewHealth) string {
	route := resolveCrewPin(pin, providers)
	if rule != nil && !crewPinAdmitted(*rule, pin.Model, route.Provider) {
		return string(crewroute.PinNotAllowed)
	}
	if _, ok := crewPinReachable(providers, route, health); !ok {
		return string(crewroute.PinProviderUnavailable)
	}
	if len(rows) > 0 {
		if _, ok := taskRowFor(rows, pinBase(pin.Model), route.Provider); !ok {
			return TaskWarnPinUnknown
		}
		if row, _ := taskRowFor(rows, pinBase(pin.Model), route.Provider); !row.Available {
			return string(crewroute.PinProviderUnavailable)
		}
	}
	return ""
}

// taskRowFor finds a model's row on a provider; an empty provider is the
// model's first row.
func taskRowFor(rows []TaskModelRow, id, provider string) (TaskModelRow, bool) {
	lineage := crewroute.Lineage(stripCrewRoute(id))
	provider = strings.ToLower(strings.TrimSpace(provider))
	for _, row := range rows {
		if crewroute.Lineage(row.Model.ID) == lineage && (provider == "" || row.Provider == provider) {
			return row, true
		}
	}
	return TaskModelRow{}, false
}

// ── one change, written whole ───────────────────────────────────────────────

// TaskModelsChange is one change to the task models: any part may be absent.
type TaskModelsChange struct {
	// Revision, when set, is the revision the change was made against.
	Revision string
	Mode     *crewroute.Mode
	One      *CrewPin
	// Pins holds a role only when the change names it; nil is Auto.
	Pins map[crewroute.Seat]*CrewPin
	// Ticks are rows allowed or not, each one model on one provider.
	Ticks []TaskModelTick
}

// TaskModelTick is one row ticked or unticked.
type TaskModelTick struct {
	Model    string
	Provider string
	Allowed  bool
}

// ErrTaskModelsStale is a change made against a revision that is no longer
// current.
var ErrTaskModelsStale = errors.New("task models changed since they were read · read them again")

// TaskModelsRefusal is a change refused for what it asked, with the reason.
type TaskModelsRefusal struct{ Reason string }

func (e TaskModelsRefusal) Error() string { return e.Reason }

func refuse(format string, args ...any) error {
	return TaskModelsRefusal{Reason: fmt.Sprintf(format, args...)}
}

// taskModelsWriteTries is how many times a change is recomputed when another
// write lands between its read and its own.
const taskModelsWriteTries = 3

// ApplyTaskModels checks EVERY PART of a change before it writes anything, and
// writes the whole change in ONE profile write, onto exactly the rows it was
// checked against: a write that came in between is not overwritten — the
// change is checked again on top of it, or refused as stale when it named the
// revision it was made against.
func ApplyTaskModels(ctx context.Context, profileDir string, change TaskModelsChange) error {
	for try := 0; ; try++ {
		snapshot := taskModelsSnapshot(profileDir)
		if change.Revision != "" && change.Revision != taskModelsRevision(snapshot) {
			return ErrTaskModelsStale
		}
		values, err := taskModelsValues(ctx, profileDir, change)
		if err != nil {
			return err
		}
		if len(values) == 0 {
			return nil
		}
		err = writeProfileValuesExpecting(profileDir, snapshot, values)
		if !errors.Is(err, errProfileMoved) {
			return err
		}
		if change.Revision != "" {
			return ErrTaskModelsStale
		}
		if try+1 >= taskModelsWriteTries {
			return ErrTaskModelsStale
		}
	}
}

// taskModelsValues is a change as the rows it writes, every part checked.
func taskModelsValues(ctx context.Context, profileDir string, change TaskModelsChange) (map[string]any, error) {
	health := crewHealthAt(profileDir)
	rows, err := taskModelRowsWith(ctx, profileDir, health)
	if err != nil {
		return nil, err
	}
	providers := CrewProvidersAt(profileDir)
	values := map[string]any{}
	before := CrewAllowedAt(profileDir)
	rule := before
	for _, tick := range change.Ticks {
		row, ok := taskRowFor(rows, tick.Model, tick.Provider)
		if !ok || tick.Provider == "" {
			return nil, refuse("%s is not a model %s lists", tick.Model, taskProviderName(providers, tick.Provider))
		}
		rule = rule.RowToggled(row.Model, row.Provider, tick.Allowed)
	}
	if rule.String() != before.String() {
		values[KeyCrewAllowed] = rule.String()
	}
	one, haveOne := TaskOneModelAt(profileDir)
	if change.One != nil {
		pin, err := taskModelsPin(rows, providers, *change.One, "the one model")
		if err != nil {
			return nil, err
		}
		route := resolveCrewPin(pin, providers)
		if _, ok := crewPinReachable(providers, route, health); !ok {
			return nil, refuse("%s is not reachable on %s · connect it or turn it on first", pin.Model, taskProviderName(providers, route.Provider))
		}
		one, haveOne = pin, true
		values[KeyTaskModelsOne] = pin.String()
	}
	if change.Mode != nil {
		switch *change.Mode {
		case crewroute.ModeOne:
			if !haveOne {
				return nil, refuse("one model for every task needs the model · send one_model")
			}
			if _, ok := crewPinReachable(providers, resolveCrewPin(one, providers), health); !ok {
				return nil, refuse("%s is not reachable right now · choose another one model", one.Model)
			}
			values[KeyTaskModelsMode] = string(crewroute.ModeOne)
		case crewroute.ModeAuto:
			values[KeyTaskModelsMode] = removeProfileKey
		default:
			return nil, refuse("mode is auto or one")
		}
	}
	pinned := false
	for _, seat := range crewroute.Seats {
		ref, named := change.Pins[seat]
		if !named {
			continue
		}
		pinned = true
		tier, route := tierKeyFor(CrewSeatTier(seat)), crewRouteKey(seat)
		if ref == nil {
			values[tier], values[route] = removeProfileKey, removeProfileKey
			continue
		}
		pin, err := taskModelsPin(rows, providers, *ref, "the "+string(seat)+" pin")
		if err != nil {
			return nil, err
		}
		// A PIN MAY ONLY BE SET TO A MODEL THE ALLOWED MODELS ADMIT, as they
		// read after this change: an allowed pin is the person's word, a
		// disallowed one is only ever an old pin the rule moved away from.
		if !crewPinAdmitted(rule, pin.Model, resolveCrewPin(pin, providers).Provider) {
			return nil, refuse("%s is not allowed on %s · allow it before pinning it", pin.Model, taskProviderName(providers, resolveCrewPin(pin, providers).Provider))
		}
		values[tier], values[route] = pin.Model, removeProfileKey
		if pin.Provider != "" {
			values[route] = pin.String()
		}
	}
	if pinned || values[KeyCrewAllowed] != nil {
		// A profile carrying retired crew rows is migrated in the same write,
		// as every crew writer does.
		for key, value := range legacyCrewClearing(profileDir) {
			if _, set := values[key]; !set {
				values[key] = value
			}
		}
	}
	return values, nil
}

// taskModelsPin is a reference checked against the rows: a model a connected
// provider lists, on the provider named.
func taskModelsPin(rows []TaskModelRow, providers []CrewProvider, ref CrewPin, what string) (CrewPin, error) {
	id := strings.TrimSpace(ref.Model)
	if err := ValidateTierValue(id); err != nil {
		return CrewPin{}, refuse("%s: %v", what, err)
	}
	provider := strings.ToLower(strings.TrimSpace(ref.Provider))
	if provider != "" {
		if _, ok := crewProviderByID(providers, provider); !ok {
			return CrewPin{}, refuse("%s: %s is not a connected provider", what, provider)
		}
	}
	row, ok := taskRowFor(rows, pinBase(id), provider)
	if !ok {
		if provider == "" {
			return CrewPin{}, refuse("%s: %s is not a model any connected provider lists", what, id)
		}
		return CrewPin{}, refuse("%s: %s is not a model %s lists", what, id, taskProviderName(providers, provider))
	}
	return CrewPin{Model: row.Model.ID, Provider: provider}, nil
}

// taskProviderName is a provider as a refusal names it.
func taskProviderName(providers []CrewProvider, id string) string {
	if p, ok := crewProviderByID(providers, id); ok && p.Name != "" {
		return p.Name
	}
	if strings.TrimSpace(id) == "" {
		return "any connected provider"
	}
	return id
}

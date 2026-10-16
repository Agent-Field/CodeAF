package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/paymentrefusal"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/trace"
)

const keyModelSources = "model_sources"

const (
	PlanPausedWait     = "wait"
	PlanPausedUseMeter = "use pay-as-you-go"
)

// PersistedSource is one non-default service in the profile. Address is kept
// only for custom services; a vendored row derives it from its region.
type PersistedSource struct {
	ID      string `json:"id"`
	Written string `json:"written"`
	Region  string `json:"region"`
	Address string `json:"address,omitempty"`
	Key     string `json:"key,omitempty"`
	KeyEnv  string `json:"key_env,omitempty"`
	// KeyOptional records an explicitly checked anonymous custom endpoint.
	// Vendored providers never inherit this per-instance choice.
	KeyOptional bool `json:"key_optional,omitempty"`
	// Door is the billing road explicitly proved at connect time. Empty belongs
	// to a pre-door row and resolves to its old metered address, never to a new
	// subscription road that has not been proved for that key.
	Door string `json:"door,omitempty"`
	// PlanPaused is the person's per-service answer. Empty is wait, preserving
	// the fixed-price account unless they deliberately opt into metered spend.
	PlanPaused string `json:"when_plan_paused,omitempty"`
	// Listed records what the service itself answered at connect time. Nil is an
	// older row that still follows the vendored hint; false and true override it.
	Listed *bool `json:"listed,omitempty"`
	Order  int   `json:"order"`
}

// PersistedSources returns no services for every unreadable profile shape. A
// newer or damaged row must not prevent an older build from starting.
func PersistedSources(profileDir string) []PersistedSource {
	values, err := readProfileConfig(profileDir)
	if err != nil {
		return nil
	}
	return persistedSourcesFrom(values)
}

func persistedSourcesFrom(values map[string]json.RawMessage) []PersistedSource {
	encoded, ok := values[keyModelSources]
	if !ok {
		return nil
	}
	var rows []PersistedSource
	if json.Unmarshal(encoded, &rows) != nil {
		return nil
	}
	return rows
}

// WriteSources atomically replaces the non-default service rows while keeping
// every unrelated profile setting.
func WriteSources(profileDir string, rows []PersistedSource) error {
	// A row's key is a credential: what it changes is recorded as it is
	// written ([crewCredentialWrite]).
	return crewCredentialWrite(profileDir, func() error { return writeSources(profileDir, rows) })
}

func writeSources(profileDir string, rows []PersistedSource) error {
	cleaned := make([]PersistedSource, 0, len(rows))
	for _, row := range rows {
		row.ID = strings.TrimSpace(row.ID)
		row.Written = strings.TrimSpace(row.Written)
		row.Region = strings.TrimSpace(row.Region)
		row.KeyEnv = strings.TrimSpace(row.KeyEnv)
		row.Door = strings.TrimSpace(row.Door)
		if strings.TrimSpace(row.PlanPaused) != PlanPausedUseMeter {
			row.PlanPaused = ""
		}
		if !modelsource.IsCustomID(row.ID) {
			// A vendored row keeps an address only where the service is the
			// person's to place ([modelsource.Source.AddressOwned]); every other
			// vendored row derives its address from its region and door.
			if addressOwned(row.ID) {
				row.Address = strings.TrimRight(strings.TrimSpace(row.Address), "/")
			} else {
				row.Address = ""
			}
			row.KeyOptional = false
		} else {
			row.Address = strings.TrimSpace(row.Address)
		}
		if row.KeyEnv != "" {
			row.Key = ""
		} else {
			row.Key = strings.TrimSpace(row.Key)
		}
		if row.ID == "" || row.Written == "" {
			continue
		}
		cleaned = append(cleaned, row)
	}
	return writeProfileValue(profileDir, keyModelSources, cleaned)
}

// addressOwned reports whether a vendored row's service is one whose address
// the person gives ([modelsource.Source.AddressOwned]), read off the vendored
// table so the fact is spelled once, beside the service it describes.
func addressOwned(id string) bool {
	for _, source := range modelsource.Vendored() {
		if strings.EqualFold(source.ID, strings.TrimSpace(id)) {
			return source.AddressOwned
		}
	}
	return false
}

// KeepAppSource keeps one service row for codeaf app, which proved it
// with [ProveService] and keeps the key itself: the row replaces the one it
// names, and every other row stays as it was. It is the app's half of what
// [ConnectService] persists.
//
// THE APP WRITES NO KEY AND TAKES NONE AWAY. Whatever key the row is handed is
// dropped; whatever key or key variable the profile already held for that id
// stays, because it is the terminal's, written on a profile both may share, and
// the app's own key outranks it in the app's process anyway.
func KeepAppSource(profileDir string, row PersistedSource) error {
	row.Key, row.KeyEnv = "", ""
	for _, existing := range PersistedSources(profileDir) {
		if strings.EqualFold(strings.TrimSpace(existing.ID), strings.TrimSpace(row.ID)) {
			row.Key, row.KeyEnv = existing.Key, existing.KeyEnv
			break
		}
	}
	if strings.TrimSpace(row.ID) == "" {
		return errors.New("a service row needs an id")
	}
	if strings.TrimSpace(row.Written) == "" {
		row.Written = strings.TrimSpace(row.ID)
	}
	if row.Order == 0 {
		// A row already in the profile keeps its place; a new one goes last.
		last, held := 0, false
		for _, existing := range PersistedSources(profileDir) {
			if strings.EqualFold(strings.TrimSpace(existing.ID), strings.TrimSpace(row.ID)) {
				row.Order, held = existing.Order, true
				break
			}
			last = max(last, existing.Order)
		}
		if !held {
			row.Order = last + 1
		}
	}
	return persistConnectedSource(profileDir, row)
}

// SourceKeyAt is APIKeyAt's generalisation. The default service keeps its
// existing three-rung implementation; another service reads its conventional
// variable, its stored key, then the variable a person named.
func SourceKeyAt(profileDir string, row PersistedSource, src modelsource.Source) string {
	if key, ok := appKey(src.ID); ok {
		return key
	}
	if strings.EqualFold(strings.TrimSpace(src.ID), modelsource.DefaultID) {
		return APIKeyAt(profileDir)
	}
	if strings.EqualFold(strings.TrimSpace(src.ID), "codex") {
		if codexauth.Connected(profileDir) {
			return codexauth.Sentinel
		}
		return ""
	}
	return sourceKeyFromRow(row, src)
}

// CatalogHTTPClient is the one door onto a model service's listing transport.
// Codex needs the profile's rotating bearer and account headers; every ordinary
// OpenAI-compatible service keeps the catalog's default client by answering nil.
func CatalogHTTPClient(service modelsource.Connected) *http.Client {
	if strings.EqualFold(strings.TrimSpace(service.Source.ID), "codex") {
		return codexauth.Client(service.Home)
	}
	return nil
}

// CatalogOptionsFor is the one construction door for a connected service's
// catalog compartment. Keeping the source identity, address, key, profile and
// account-aware client together makes it impossible for a new listing road to
// send Codex's persisted sentinel through the generic HTTP client.
func CatalogOptionsFor(service modelsource.Connected, profileDir string) catalog.Options {
	return catalog.Options{
		Source: service.Source.ID, BaseURL: service.Address, APIKey: service.Key,
		Dir: profileDir, HTTPClient: CatalogHTTPClient(service),
	}
}

func sourceKeyFromRow(row PersistedSource, src modelsource.Source) string {
	return strings.TrimSpace(firstNonEmpty(env.Value(strings.TrimSpace(src.KeyEnv)), row.Key, env.Value(strings.TrimSpace(row.KeyEnv))))
}

// ResolveSources builds the whole registry: the synthesised default service
// first, then every persisted row this build still knows.
func ResolveSources(profileDir, defaultKey, defaultBase string) modelsource.Set {
	set := resolveSources(defaultKey, defaultBase, PersistedSources(profileDir), func(row PersistedSource, source modelsource.Source) string {
		return SourceKeyAt(profileDir, row, source)
	})
	return sourceHomes(set, profileDir)
}

func sourceHomes(set modelsource.Set, profileDir string) modelsource.Set {
	services := set.All()
	for index := range services {
		services[index].Home = profileDir
		if strings.EqualFold(services[index].Source.ID, "codex") {
			services[index].Address = codexauth.Backend()
			services[index].Source.Address = services[index].Address
		}
	}
	return modelsource.NewSet(services...)
}

// resolveSources is the file-free half of ResolveSources. Load hands it the
// rows from the profile snapshot it already read for api_key, while callers
// that need a fresh reading keep using ResolveSources.
func resolveSources(defaultKey, defaultBase string, rows []PersistedSource, keyAt func(PersistedSource, modelsource.Source) string) modelsource.Set {
	if key, ok := appKey(modelsource.DefaultID); ok {
		defaultKey = key
	}
	rows = appSourceRows(rows)
	defaultSource := modelsource.DefaultSource(defaultBase)
	connected := []modelsource.Connected{{Source: defaultSource, Key: defaultKey, Address: defaultBase}}
	vendored := modelsource.Vendored()
	known := make(map[string]modelsource.Source, len(vendored))
	for _, source := range vendored {
		known[source.ID] = source
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Order < rows[j].Order })
	for _, row := range rows {
		source, ok := known[strings.TrimSpace(row.ID)]
		if !ok {
			// A custom instance is not a row of its own in the vendored table: it
			// resolves onto the vendored custom service as a TEMPLATE, and the row
			// it was persisted under is stamped back on below. Without the stamp
			// every instance would collapse into one Connected, and Set.ByID,
			// the model-service rows, the model groups and the disconnect path
			// would all see one connection where the person has two.
			if !modelsource.IsCustomID(row.ID) {
				continue
			}
			template, ok := known[modelsource.CustomID]
			if !ok {
				continue
			}
			source, ok = template, true
		}
		source.ID = strings.TrimSpace(row.ID)
		if modelsource.IsCustomID(source.ID) {
			source.KeyOptional = row.KeyOptional
		}
		if written := strings.TrimSpace(row.Written); written != "" {
			source.Written = written
		}
		if row.Listed != nil {
			if *row.Listed {
				source.Listing = modelsource.ListingModels
			} else {
				source.Listing = modelsource.ListingNone
			}
		}
		door := resolvedSourceDoor(row, source)
		address := resolvedSourceAddress(row, source)
		var overflow *modelsource.Door
		if metered, ok := source.MeteredDoor(); ok && door.ID != "" && !door.Metered {
			metered.Address = resolvedDoorAddress(row, source, metered)
			overflow = &metered
		}
		source.Address = address
		connected = append(connected, modelsource.Connected{
			Source: source, Key: keyAt(row, source), Address: address,
			Door: door, Overflow: overflow, PlanPaused: resolvedPlanPaused(row),
		})
	}
	return modelsource.NewSet(connected...)
}

func resolvedSourceAddress(row PersistedSource, source modelsource.Source) string {
	if strings.EqualFold(strings.TrimSpace(source.ID), "codex") {
		return codexauth.Backend()
	}
	if modelsource.IsCustomID(source.ID) {
		return strings.TrimRight(strings.TrimSpace(row.Address), "/")
	}
	if address := strings.TrimRight(strings.TrimSpace(row.Address), "/"); source.AddressOwned && address != "" {
		return address
	}
	if door := resolvedSourceDoor(row, source); door.ID != "" {
		return resolvedDoorAddress(row, source, door)
	}
	return resolvedRegionAddress(row, source)
}

// CodexRememberedModels is the Codex catalog this profile remembers for the
// connected service, every row with the window it accepts.
//
// A ROW REMEMBERED BEFORE #1383 CARRIES NO WINDOW, because the connection that
// wrote it dropped the listing's `context_window`. For a model the fallback list
// names, the fallback's figure stands in: it is the same observation of the same
// account listing that [ConnectCodex] remembers when the listing cannot be
// reached, so it is a fact this build already holds about that id rather than a
// guess about a model nobody has heard back from. Any other id keeps its zero,
// which every reader already treats as "nobody can say". It is the one door both
// readers of a Codex catalog take — the process shelf an engine's sessions ask
// for a window, and the surface's own rows — so a profile connected by an older
// build reads 272k the next time it opens, without connecting again.
func CodexRememberedModels(service modelsource.Connected, profileDir string) []catalog.Model {
	rows := catalog.Recall(CatalogOptionsFor(service, profileDir)).ModelsNow()
	known := make(map[string]int, len(codexauth.FallbackModels))
	for _, model := range codexauth.FallbackModels {
		known[strings.ToLower(model.ID)] = model.ContextLength
	}
	for index := range rows {
		if rows[index].ContextLength <= 0 {
			rows[index].ContextLength = known[strings.ToLower(strings.TrimSpace(rows[index].ID))]
		}
	}
	return rows
}

// ConnectCodex keeps a completed browser sign-in, persists its service row and
// seeds the picker from the account's own visible model list.
//
// IT IS THE ONE SIGN-IN WHOSE CREDENTIALS LIVE IN THE PROFILE, in codeaf app's
// process too. Its tokens rotate on every refresh, so a copy handed in at launch
// would be stale within the hour; the token file stays owner-only, exactly as the
// terminal keeps it.
func ConnectCodex(ctx context.Context, profileDir string, tokens codexauth.Tokens) (modelsource.Outcome, error) {
	if err := ctx.Err(); err != nil {
		return modelsource.Outcome{}, err
	}
	if err := crewCredentialWrite(profileDir, func() error { return codexauth.Save(profileDir, tokens) }); err != nil {
		return modelsource.Outcome{}, err
	}
	listed := true
	row := PersistedSource{ID: "codex", Written: "codex", Listed: &listed}
	// Account tokens stay in the existing owner-only Codex store. App profiles
	// never write a key or sentinel into their generic provider rows.
	if !AppMode() {
		row.Key = codexauth.Sentinel
	}
	if err := persistConnectedSource(profileDir, row); err != nil {
		_ = codexauth.Remove(profileDir)
		return modelsource.Outcome{}, err
	}
	models, listErr := codexauth.List(ctx, profileDir, codexauth.Options{})
	refreshed := listErr == nil
	if !refreshed {
		models = append([]codexauth.Model(nil), codexauth.FallbackModels...)
	}
	outcome := modelsource.Outcome{Kind: modelsource.OutcomeConnected, Listed: true, Refreshed: refreshed}
	// EACH ROW KEEPS ITS WINDOW. The catalog remembered here is what the picker,
	// the status line's meter and the session's compaction all read for a Codex
	// model, and a row remembered without one left a conversation that moved onto
	// it on its previous model's window (#1383). The figure is the account
	// listing's own, or the fallback rows' one observed figure when the listing
	// could not be reached.
	remembered := make([]catalog.Model, 0, len(models))
	for _, model := range models {
		if id := strings.TrimSpace(model.ID); id != "" {
			outcome.ModelIDs = append(outcome.ModelIDs, id)
			remembered = append(remembered, catalog.Model{ID: id, ContextLength: model.ContextLength, PriceUnknown: true})
		}
	}
	outcome.Models = len(outcome.ModelIDs)
	service, found := ResolveSources(profileDir, "", DefaultBaseURL).ByID("codex")
	if !found {
		_ = DisconnectService(profileDir, "codex")
		return modelsource.Outcome{}, errors.New("codex connection was not saved")
	}
	if err := catalog.Remember(CatalogOptionsFor(service, profileDir), remembered); err != nil {
		_ = DisconnectService(profileDir, "codex")
		return modelsource.Outcome{}, err
	}
	return outcome, nil
}

func resolvedRegionAddress(row PersistedSource, source modelsource.Source) string {
	if address := selectedRegionAddress(row, source); address != "" {
		return address
	}
	return strings.TrimRight(strings.TrimSpace(source.Address), "/")
}

func selectedRegionAddress(row PersistedSource, source modelsource.Source) string {
	for _, region := range source.Regions {
		if strings.EqualFold(strings.TrimSpace(region.ID), strings.TrimSpace(row.Region)) {
			return strings.TrimRight(strings.TrimSpace(region.Address), "/")
		}
	}
	return ""
}

func resolvedSourceDoor(row PersistedSource, source modelsource.Source) modelsource.Door {
	for _, door := range source.Doors {
		if strings.EqualFold(strings.TrimSpace(door.ID), strings.TrimSpace(row.Door)) {
			door.Address = resolvedDoorAddress(row, source, door)
			return door
		}
	}
	// A row connected before billing doors existed was on the metered region.
	// Keeping it there is both backward compatibility and the money law: a
	// reload may not silently move an account to a different billing product.
	if strings.TrimSpace(row.Door) == "" {
		for _, door := range source.Doors {
			if door.Metered {
				door.Address = resolvedDoorAddress(row, source, door)
				return door
			}
		}
	}
	return modelsource.Door{}
}

func resolvedDoorAddress(row PersistedSource, source modelsource.Source, door modelsource.Door) string {
	if door.Metered {
		if region := selectedRegionAddress(row, source); region != "" {
			return region
		}
	}
	return strings.TrimRight(strings.TrimSpace(door.Address), "/")
}

func resolvedPlanPaused(row PersistedSource) string {
	if strings.TrimSpace(row.PlanPaused) == PlanPausedUseMeter {
		return PlanPausedUseMeter
	}
	return PlanPausedWait
}

// ConnectService proves a key and says what it reaches, and writes nothing on
// any answer but yes. Ten seconds; a person is watching this one.
//
// IT IS [ProveService] AND ONE WRITE. The proving half is shared with codeaf
// app, which keeps no key in the profile and so may not take this door at all: it proves with the same requests in the same order, and
// keeps the row it was answered with through [KeepAppSource].
func ConnectService(ctx context.Context, profileDir string, row PersistedSource, src modelsource.Source, authors []string) (modelsource.Outcome, error) {
	if AppMode() {
		return modelsource.Outcome{}, ErrAppKeys
	}
	row, src = PrepareServiceRow(profileDir, row, src, authors)
	key := SourceKeyAt(profileDir, row, src)
	outcome, kept, keep := ProveService(ctx, row, src, key)
	if !keep {
		return outcome, nil
	}
	if err := persistSourceUnlessCancelled(ctx, profileDir, kept); err != nil {
		return modelsource.Outcome{}, err
	}
	return outcome, nil
}

// PrepareServiceRow settles the two things about a row that are decided before
// any request: its id and Written name, taking the suggested spelling when the
// person's would collide with another connection's, and — for a custom
// connection — whether this instance was checked as answering with no key.
func PrepareServiceRow(profileDir string, row PersistedSource, src modelsource.Source, authors []string) (PersistedSource, modelsource.Source) {
	if strings.TrimSpace(row.ID) == "" {
		row.ID = src.ID
	}
	if strings.TrimSpace(row.Written) == "" {
		row.Written = src.Written
	}
	var taken []string
	for _, service := range ResolveSources(profileDir, "", DefaultBaseURL).All() {
		if !strings.EqualFold(strings.TrimSpace(service.Source.ID), strings.TrimSpace(row.ID)) {
			taken = append(taken, service.Source.Written)
		}
	}
	if suggestion, collides := modelsource.Collides(row.Written, taken, authors); collides {
		// A NAME THAT CANNOT BE USED IS NOT A QUESTION TO ASK A PERSON TWICE.
		// The program knows the available spelling, so the first attempt takes it
		// and the connection line says which name it used.
		row.Written = suggestion
	}
	if written := strings.TrimSpace(row.Written); written != "" {
		src.Written = written
	}
	if modelsource.IsCustomID(row.ID) && modelsource.IsCustomID(src.ID) {
		src.KeyOptional = row.KeyOptional
	}
	return row, src
}

// ProveService is the requests that prove a key reaches a service, and
// nothing else: it reads no profile and writes nothing. It answers what was
// learned, the row that learning amounts to (the billing door the key proved,
// whether the service listed its models), and whether that row is a connection
// to keep — true for a yes, and for an account that authenticated but cannot
// pay, which is still the person's account.
//
// The key is handed in rather than looked up, because codeaf app proves a
// key it has not handed over yet and the key readers would find the old one.
func ProveService(ctx context.Context, row PersistedSource, src modelsource.Source, key string) (modelsource.Outcome, PersistedSource, bool) {
	if key != "" {
		// THE SECRET ARRIVES BEFORE THE RESULT. A service may echo a submitted
		// key in either a refusal or a success body, and the connection result is
		// recordable as soon as this function returns. Registering here closes
		// that interval for keys connected after process startup.
		trace.Secret(key)
	}
	if key == "" && !src.KeyOptional {
		return modelsource.Outcome{Kind: modelsource.OutcomeWrongShape}, row, false
	}
	if src.KeyShape != nil && !src.KeyShape(key) {
		return modelsource.Outcome{Kind: modelsource.OutcomeWrongShape}, row, false
	}
	if len(src.Doors) > 1 {
		return proveServiceDoors(ctx, row, src, key)
	}
	address := resolvedSourceAddress(row, src)
	listing := src.Probe
	if listing.Method == "" && listing.Address == "" {
		return modelsource.Outcome{Kind: modelsource.OutcomeConnected}, row, true
	}
	status, body, answered := runServiceProbe(ctx, address, key, listing)
	if !answered {
		return modelsource.Outcome{Kind: modelsource.OutcomeUnanswered}, row, false
	}
	if acceptsStatus(listing.Accepts, status) {
		outcome, ok := listedOutcome(body)
		if !ok {
			return modelsource.Outcome{Kind: modelsource.OutcomeUnanswered}, row, false
		}
		listed := true
		row.Listed = &listed
		return outcome, row, true
	}
	if paymentrefusal.Matches(status, body) {
		return modelsource.Outcome{Kind: modelsource.OutcomeAccountCannotPay, VendorSaid: withoutExactSecret(vendorWords(body), key)}, row, true
	}
	if !listingIsAbsent(status) {
		return modelsource.Outcome{Kind: modelsource.OutcomeRefused, VendorSaid: withoutExactSecret(vendorWords(body), key), Status: status}, row, false
	}

	listed := false
	row.Listed = &listed
	fallback := src.FallbackProbe()
	if fallback.Method == "" && fallback.Address == "" {
		return modelsource.Outcome{Kind: modelsource.OutcomeConnected}, row, true
	}
	status, body, answered = runServiceProbe(ctx, address, key, fallback)
	if !answered {
		return modelsource.Outcome{Kind: modelsource.OutcomeUnanswered}, row, false
	}
	if paymentrefusal.Matches(status, body) {
		return modelsource.Outcome{Kind: modelsource.OutcomeAccountCannotPay, VendorSaid: withoutExactSecret(vendorWords(body), key)}, row, true
	}
	if !acceptsStatus(fallback.Accepts, status) {
		return modelsource.Outcome{Kind: modelsource.OutcomeRefused, VendorSaid: withoutExactSecret(vendorWords(body), key), Status: status}, row, false
	}
	return modelsource.Outcome{Kind: modelsource.OutcomeConnected}, row, true
}

func proveServiceDoors(ctx context.Context, row PersistedSource, src modelsource.Source, key string) (modelsource.Outcome, PersistedSource, bool) {
	probe := src.DoorProbe()
	if probe.Method == "" || probe.Address == "" {
		return modelsource.Outcome{Kind: modelsource.OutcomeUnanswered}, row, false
	}
	lastWords := ""
	unanswered := false
	for _, door := range src.OrderedDoors(key) {
		address := resolvedDoorAddress(row, src, door)
		status, body, answered := runServiceProbe(ctx, address, key, probe)
		if !answered {
			unanswered = true
			continue
		}
		classification := paymentrefusal.Classify(status, body)
		if classification == paymentrefusal.NoPlan || classification == paymentrefusal.Payment {
			lastWords = withoutExactSecret(vendorWords(body), key)
			continue
		}
		if classification == paymentrefusal.WindowExhausted {
			// A spent window proves this door: the key authenticated and the plan
			// exists. Walking onward would make a paid probe and persist a metered
			// binding even though the person's setting still says wait.
			return proveAtDoor(ctx, row, src, key, door, true, paymentrefusal.ResetAt(body))
		}
		if !acceptsStatus(probe.Accepts, status) {
			return modelsource.Outcome{Kind: modelsource.OutcomeRefused, VendorSaid: withoutExactSecret(vendorWords(body), key), Status: status}, row, false
		}

		return proveAtDoor(ctx, row, src, key, door, false, "")
	}
	if lastWords != "" && !unanswered {
		return modelsource.Outcome{Kind: modelsource.OutcomeAccountCannotPay, VendorSaid: lastWords}, row, false
	}
	return modelsource.Outcome{Kind: modelsource.OutcomeUnanswered}, row, false
}

func proveAtDoor(ctx context.Context, row PersistedSource, src modelsource.Source, key string, door modelsource.Door, paused bool, reset string) (modelsource.Outcome, PersistedSource, bool) {
	address := resolvedDoorAddress(row, src, door)
	row.Door = door.ID
	outcome := modelsource.Outcome{
		Kind: modelsource.OutcomeConnected, Door: door,
		PlanPaused: paused, PlanReset: strings.TrimSpace(reset),
	}
	if metered, ok := src.MeteredDoor(); ok && !door.Metered {
		metered.Address = resolvedDoorAddress(row, src, metered)
		outcome.Overflow = &metered
	}
	if len(door.Models) > 0 {
		listed := true
		row.Listed = &listed
		outcome.Listed = true
		outcome.ModelIDs = append([]string(nil), door.Models...)
		outcome.Models = len(outcome.ModelIDs)
	} else if listing := src.Probe; listing.Method != "" || listing.Address != "" {
		listStatus, listBody, listAnswered := runServiceProbe(ctx, address, key, listing)
		if listAnswered && acceptsStatus(listing.Accepts, listStatus) {
			if listed, ok := listedOutcome(listBody); ok {
				outcome = listed
				outcome.Door = door
				outcome.PlanPaused = paused
				outcome.PlanReset = strings.TrimSpace(reset)
				if metered, found := src.MeteredDoor(); found && !door.Metered {
					metered.Address = resolvedDoorAddress(row, src, metered)
					outcome.Overflow = &metered
				}
				value := true
				row.Listed = &value
			}
		} else if listAnswered && listingIsAbsent(listStatus) {
			value := false
			row.Listed = &value
		}
	}
	return outcome, row, true
}

func runServiceProbe(ctx context.Context, address, key string, probe modelsource.Probe) (int, []byte, bool) {
	request, err := http.NewRequestWithContext(ctx, probe.Method, strings.TrimRight(address, "/")+probe.Address, bytes.NewBufferString(probe.Body))
	if err != nil {
		return 0, nil, false
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", provider.DirectUserAgent)
	if probe.Body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	response, err := (&http.Client{Timeout: probe.Timeout}).Do(request)
	if err != nil {
		return 0, nil, false
	}
	defer response.Body.Close()
	// A successful model list must be read whole. The old refusal-sized read
	// cut ai&'s valid 6,920-byte catalog into invalid JSON before admission.
	// One byte beyond discovery's bound distinguishes an oversized response
	// from a complete document that happens to end exactly at the limit.
	listing := probe.Method == http.MethodGet && probe.Address == "/models" && acceptsStatus(probe.Accepts, response.StatusCode)
	limit := int64(4 << 10)
	if listing {
		limit = catalog.MaxCatalogBytes + 1
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit))
	if err != nil {
		return 0, nil, false
	}
	if listing && len(body) > catalog.MaxCatalogBytes {
		return response.StatusCode, nil, false
	}
	return response.StatusCode, body, true
}

func listedOutcome(body []byte) (modelsource.Outcome, bool) {
	var listing struct {
		Data []json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &listing) != nil {
		return modelsource.Outcome{}, false
	}
	outcome := modelsource.Outcome{Kind: modelsource.OutcomeConnected, Listed: true, Models: len(listing.Data)}
	for _, raw := range listing.Data {
		var item struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &item) == nil {
			if id := strings.TrimSpace(item.ID); id != "" {
				outcome.ModelIDs = append(outcome.ModelIDs, id)
			}
		}
	}
	return outcome, true
}

func listingIsAbsent(status int) bool {
	return status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status == http.StatusNotImplemented
}

func withoutExactSecret(words, secret string) string {
	if secret = strings.TrimSpace(secret); secret == "" {
		return words
	}
	return strings.ReplaceAll(words, secret, "[redacted]")
}

func acceptsStatus(accepted []int, status int) bool {
	if len(accepted) == 0 {
		return status >= http.StatusOK && status < http.StatusMultipleChoices
	}
	for _, candidate := range accepted {
		if candidate == status {
			return true
		}
	}
	return false
}

func vendorWords(body []byte) string {
	var decoded any
	if json.Unmarshal(body, &decoded) == nil {
		var words []string
		collectVendorWords(decoded, &words)
		if len(words) > 0 {
			return strings.Join(words, "; ")
		}
	}
	return strings.TrimSpace(string(body))
}

func collectVendorWords(value any, words *[]string) {
	switch typed := value.(type) {
	case string:
		*words = append(*words, typed)
	case []any:
		for _, item := range typed {
			collectVendorWords(item, words)
		}
	case map[string]any:
		// JSON object order is not meaning. Prefer conventional human-readable
		// fields before walking an unfamiliar vendor envelope deterministically.
		wordCount := len(*words)
		for _, key := range []string{"message", "detail", "error", "description"} {
			if item, ok := typed[key]; ok {
				collectVendorWords(item, words)
				delete(typed, key)
			}
		}
		// A conventional code is useful to classification, not to the sentence a
		// person reads when the same object already carried the vendor's words.
		// Payment classification reads the untouched body before this formatter.
		if len(*words) > wordCount {
			delete(typed, "code")
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			collectVendorWords(typed[key], words)
		}
	}
}

func persistConnectedSource(profileDir string, row PersistedSource) error {
	rows := PersistedSources(profileDir)
	replaced := false
	for index := range rows {
		if strings.EqualFold(strings.TrimSpace(rows[index].ID), strings.TrimSpace(row.ID)) {
			rows[index] = row
			replaced = true
			break
		}
	}
	if !replaced {
		rows = append(rows, row)
	}
	return WriteSources(profileDir, rows)
}

// DisconnectService removes one service row and its stored credential. Codex's
// rotating tokens live in their owner-only sibling file and leave with it.
func DisconnectService(profileDir, id string) error {
	rows := PersistedSources(profileDir)
	kept := rows[:0]
	for _, row := range rows {
		if !strings.EqualFold(strings.TrimSpace(row.ID), strings.TrimSpace(id)) {
			kept = append(kept, row)
		}
	}
	if err := WriteSources(profileDir, kept); err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(id), "codex") {
		return codexauth.Remove(profileDir)
	}
	return nil
}

// A cancelled connection must not save an answer from a network trip it left.
func persistSourceUnlessCancelled(ctx context.Context, dir string, row PersistedSource) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return persistConnectedSource(dir, row)
}

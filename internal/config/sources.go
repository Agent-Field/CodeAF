package config

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"

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
			row.Address = ""
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

// SourceKeyAt is APIKeyAt's generalisation. The default service keeps its
// existing three-rung implementation; another service reads its conventional
// variable, its stored key, then the variable a person named.
func SourceKeyAt(profileDir string, row PersistedSource, src modelsource.Source) string {
	if strings.EqualFold(strings.TrimSpace(src.ID), modelsource.DefaultID) {
		return APIKeyAt(profileDir)
	}
	return sourceKeyFromRow(row, src)
}

func sourceKeyFromRow(row PersistedSource, src modelsource.Source) string {
	return strings.TrimSpace(firstNonEmpty(env.Value(strings.TrimSpace(src.KeyEnv)), row.Key, env.Value(strings.TrimSpace(row.KeyEnv))))
}

// ResolveSources builds the whole registry: the synthesised default service
// first, then every persisted row this build still knows.
func ResolveSources(profileDir, defaultKey, defaultBase string) modelsource.Set {
	return resolveSources(defaultKey, defaultBase, PersistedSources(profileDir), func(row PersistedSource, source modelsource.Source) string {
		return SourceKeyAt(profileDir, row, source)
	})
}

// resolveSources is the file-free half of ResolveSources. Load hands it the
// rows from the profile snapshot it already read for api_key, while callers
// that need a fresh reading keep using ResolveSources.
func resolveSources(defaultKey, defaultBase string, rows []PersistedSource, keyAt func(PersistedSource, modelsource.Source) string) modelsource.Set {
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
	if modelsource.IsCustomID(source.ID) {
		return strings.TrimRight(strings.TrimSpace(row.Address), "/")
	}
	if door := resolvedSourceDoor(row, source); door.ID != "" {
		return resolvedDoorAddress(row, source, door)
	}
	return resolvedRegionAddress(row, source)
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
func ConnectService(ctx context.Context, profileDir string, row PersistedSource, src modelsource.Source, authors []string) (modelsource.Outcome, error) {
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
	key := SourceKeyAt(profileDir, row, src)
	if key != "" {
		// THE SECRET ARRIVES BEFORE THE RESULT. A service may echo a submitted
		// key in either a refusal or a success body, and the connection result is
		// recordable as soon as this function returns. Registering here closes
		// that interval for keys connected after process startup.
		trace.Secret(key)
	}
	if key == "" && !src.KeyOptional {
		return modelsource.Outcome{Kind: modelsource.OutcomeWrongShape}, nil
	}
	if src.KeyShape != nil && !src.KeyShape(key) {
		return modelsource.Outcome{Kind: modelsource.OutcomeWrongShape}, nil
	}
	if len(src.Doors) > 1 {
		return connectServiceDoors(ctx, profileDir, row, src, key)
	}
	address := resolvedSourceAddress(row, src)
	listing := src.Probe
	if listing.Method == "" && listing.Address == "" {
		if err := persistConnectedSource(profileDir, row); err != nil {
			return modelsource.Outcome{}, err
		}
		return modelsource.Outcome{Kind: modelsource.OutcomeConnected}, nil
	}
	status, body, answered := runServiceProbe(ctx, address, key, listing)
	if !answered {
		return modelsource.Outcome{Kind: modelsource.OutcomeUnanswered}, nil
	}
	if acceptsStatus(listing.Accepts, status) {
		outcome, ok := listedOutcome(body)
		if !ok {
			return modelsource.Outcome{Kind: modelsource.OutcomeUnanswered}, nil
		}
		listed := true
		row.Listed = &listed
		if err := persistConnectedSource(profileDir, row); err != nil {
			return modelsource.Outcome{}, err
		}
		return outcome, nil
	}
	if paymentrefusal.Matches(status, body) {
		if err := persistConnectedSource(profileDir, row); err != nil {
			return modelsource.Outcome{}, err
		}
		return modelsource.Outcome{Kind: modelsource.OutcomeAccountCannotPay, VendorSaid: withoutExactSecret(vendorWords(body), key)}, nil
	}
	if !listingIsAbsent(status) {
		return modelsource.Outcome{Kind: modelsource.OutcomeRefused, VendorSaid: withoutExactSecret(vendorWords(body), key)}, nil
	}

	listed := false
	row.Listed = &listed
	fallback := src.FallbackProbe()
	if fallback.Method == "" && fallback.Address == "" {
		if err := persistConnectedSource(profileDir, row); err != nil {
			return modelsource.Outcome{}, err
		}
		return modelsource.Outcome{Kind: modelsource.OutcomeConnected}, nil
	}
	status, body, answered = runServiceProbe(ctx, address, key, fallback)
	if !answered {
		return modelsource.Outcome{Kind: modelsource.OutcomeUnanswered}, nil
	}
	if paymentrefusal.Matches(status, body) {
		if err := persistConnectedSource(profileDir, row); err != nil {
			return modelsource.Outcome{}, err
		}
		return modelsource.Outcome{Kind: modelsource.OutcomeAccountCannotPay, VendorSaid: withoutExactSecret(vendorWords(body), key)}, nil
	}
	if !acceptsStatus(fallback.Accepts, status) {
		return modelsource.Outcome{Kind: modelsource.OutcomeRefused, VendorSaid: withoutExactSecret(vendorWords(body), key)}, nil
	}
	if err := persistConnectedSource(profileDir, row); err != nil {
		return modelsource.Outcome{}, err
	}
	return modelsource.Outcome{Kind: modelsource.OutcomeConnected}, nil
}

func connectServiceDoors(ctx context.Context, profileDir string, row PersistedSource, src modelsource.Source, key string) (modelsource.Outcome, error) {
	probe := src.DoorProbe()
	if probe.Method == "" || probe.Address == "" {
		return modelsource.Outcome{Kind: modelsource.OutcomeUnanswered}, nil
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
			return connectAtDoor(ctx, profileDir, row, src, key, door, true, paymentrefusal.ResetAt(body))
		}
		if !acceptsStatus(probe.Accepts, status) {
			return modelsource.Outcome{Kind: modelsource.OutcomeRefused, VendorSaid: withoutExactSecret(vendorWords(body), key)}, nil
		}

		return connectAtDoor(ctx, profileDir, row, src, key, door, false, "")
	}
	if lastWords != "" && !unanswered {
		return modelsource.Outcome{Kind: modelsource.OutcomeAccountCannotPay, VendorSaid: lastWords}, nil
	}
	return modelsource.Outcome{Kind: modelsource.OutcomeUnanswered}, nil
}

func connectAtDoor(ctx context.Context, profileDir string, row PersistedSource, src modelsource.Source, key string, door modelsource.Door, paused bool, reset string) (modelsource.Outcome, error) {
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
	if err := persistConnectedSource(profileDir, row); err != nil {
		return modelsource.Outcome{}, err
	}
	return outcome, nil
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
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<10))
	if err != nil {
		return 0, nil, false
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

// DisconnectService removes one service row and its stored key atomically.
func DisconnectService(profileDir, id string) error {
	rows := PersistedSources(profileDir)
	kept := rows[:0]
	for _, row := range rows {
		if !strings.EqualFold(strings.TrimSpace(row.ID), strings.TrimSpace(id)) {
			kept = append(kept, row)
		}
	}
	return WriteSources(profileDir, kept)
}

package config

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/modelsource"
	"github.com/Agent-Field/aforge-v2/internal/trace"
)

const keyModelSources = "model_sources"

// PersistedSource is one non-default service in the profile. Address is kept
// only for custom services; a vendored row derives it from its region.
type PersistedSource struct {
	ID      string `json:"id"`
	Written string `json:"written"`
	Region  string `json:"region"`
	Address string `json:"address,omitempty"`
	Key     string `json:"key,omitempty"`
	KeyEnv  string `json:"key_env,omitempty"`
	Order   int    `json:"order"`
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
		if row.ID != "custom" {
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
	return strings.TrimSpace(firstNonEmpty(os.Getenv(strings.TrimSpace(src.KeyEnv)), row.Key, os.Getenv(strings.TrimSpace(row.KeyEnv))))
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
			continue
		}
		if written := strings.TrimSpace(row.Written); written != "" {
			source.Written = written
		}
		address := resolvedSourceAddress(row, source)
		source.Address = address
		connected = append(connected, modelsource.Connected{
			Source: source, Key: keyAt(row, source), Address: address,
		})
	}
	return modelsource.NewSet(connected...)
}

func resolvedSourceAddress(row PersistedSource, source modelsource.Source) string {
	if source.ID == "custom" {
		return strings.TrimRight(strings.TrimSpace(row.Address), "/")
	}
	for _, region := range source.Regions {
		if strings.EqualFold(strings.TrimSpace(region.ID), strings.TrimSpace(row.Region)) {
			return strings.TrimRight(strings.TrimSpace(region.Address), "/")
		}
	}
	return strings.TrimRight(strings.TrimSpace(source.Address), "/")
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
		return modelsource.Outcome{Kind: modelsource.OutcomeCollides, Suggestion: suggestion}, nil
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
	address := resolvedSourceAddress(row, src)
	probe := src.Probe
	if probe.Method == "" && probe.Address == "" {
		if err := persistConnectedSource(profileDir, row); err != nil {
			return modelsource.Outcome{}, err
		}
		return modelsource.Outcome{Kind: modelsource.OutcomeConnected}, nil
	}
	request, err := http.NewRequestWithContext(ctx, probe.Method, strings.TrimRight(address, "/")+probe.Address, bytes.NewBufferString(probe.Body))
	if err != nil {
		return modelsource.Outcome{Kind: modelsource.OutcomeUnanswered}, nil
	}
	request.Header.Set("Accept", "application/json")
	if probe.Body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{Timeout: probe.Timeout}
	response, err := client.Do(request)
	if err != nil {
		return modelsource.Outcome{Kind: modelsource.OutcomeUnanswered}, nil
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<10))
	if err != nil {
		return modelsource.Outcome{Kind: modelsource.OutcomeUnanswered}, nil
	}
	if !acceptsStatus(probe.Accepts, response.StatusCode) {
		return modelsource.Outcome{Kind: modelsource.OutcomeRefused, VendorSaid: withoutExactSecret(vendorWords(body), key)}, nil
	}
	outcome := modelsource.Outcome{Kind: modelsource.OutcomeConnected}
	if src.Listing == modelsource.ListingModels {
		var listing struct {
			Data []json.RawMessage `json:"data"`
		}
		if json.Unmarshal(body, &listing) != nil {
			return modelsource.Outcome{Kind: modelsource.OutcomeUnanswered}, nil
		}
		outcome.Listed = true
		outcome.Models = len(listing.Data)
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
	}
	if err := persistConnectedSource(profileDir, row); err != nil {
		return modelsource.Outcome{}, err
	}
	return outcome, nil
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
		for _, key := range []string{"message", "detail", "error", "description"} {
			if item, ok := typed[key]; ok {
				collectVendorWords(item, words)
				delete(typed, key)
			}
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

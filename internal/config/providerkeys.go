package config

import (
	"encoding/json"
	"maps"
	"strings"
)

// ProviderKeys is every key a profile's config.json stores on disk and nothing
// else: the keys of the secret settings rows (the model key, the search keys,
// the app secret) and the key of each model service that holds one inline. It is
// what a person's second machine needs to run a moved chat, and it leaves out
// the rest of the file (budgets, rails, model picks), which belongs to the
// machine it was set on. A key that comes from an environment variable is not
// in the file, so it is never in here; a service that names a variable
// (key_env) has no inline key and contributes nothing.
type ProviderKeys struct {
	Rows    map[string]string `json:"rows,omitempty"`    // secret settings row key -> value
	Sources map[string]string `json:"sources,omitempty"` // model service id -> inline key
}

// Empty reports whether the profile holds no key of its own.
func (k ProviderKeys) Empty() bool { return len(k.Rows) == 0 && len(k.Sources) == 0 }

// secretRowKeys names the config.json field of every secret settings row. It is
// read from the registry, so a secret row added later travels without this file
// being edited.
func secretRowKeys() []string {
	var names []string
	for _, row := range NewSettings(SettingsOptions{}).Rows() {
		if row.Secret {
			names = append(names, row.Key)
		}
	}
	return names
}

// ReadProviderKeys collects the profile's stored keys from the file alone. A
// config.json that cannot be read is an error, never an empty answer, so a
// damaged file is not mistaken for a person who removed their keys.
func ReadProviderKeys(profileDir string) (ProviderKeys, error) {
	values, err := readProfileConfig(profileDir)
	if err != nil {
		return ProviderKeys{}, err
	}
	return providerKeysFrom(values), nil
}

func providerKeysFrom(values map[string]json.RawMessage) ProviderKeys {
	var keys ProviderKeys
	for _, name := range secretRowKeys() {
		keys.Rows = putNonEmpty(keys.Rows, name, storedString(values, name))
	}
	for _, row := range persistedSourcesFrom(values) {
		keys.Sources = putNonEmpty(keys.Sources, row.ID, row.Key)
	}
	return keys
}

func putNonEmpty(into map[string]string, name, value string) map[string]string {
	if value == "" {
		return into
	}
	if into == nil {
		into = map[string]string{}
	}
	into[name] = value
	return into
}

func storedString(values map[string]json.RawMessage, name string) string {
	var value string
	if json.Unmarshal(values[name], &value) != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

// WriteProviderKeys makes the profile's stored keys equal to keys: a key that
// is absent from keys is removed. Use [ChangeProviderKeys] to change some keys
// and leave the rest as they are.
func WriteProviderKeys(profileDir string, keys ProviderKeys) error {
	return ChangeProviderKeys(profileDir, func(ProviderKeys) ProviderKeys { return keys })
}

// ChangeProviderKeys hands change the keys the profile stores AT THE MOMENT OF
// THE WRITE and stores the keys it answers: a key absent from the answer is
// removed, and every other field of config.json, and every other field of a
// service row, is left as it was. The stored keys are read inside the profile's
// own locked, atomic write, so a key another writer stored a moment ago is in
// what change sees and cannot be removed by a set built from an earlier reading.
// It writes nothing when the file already says the same. A service row this
// machine does not have is skipped, since a key alone cannot make one.
func ChangeProviderKeys(profileDir string, change func(ProviderKeys) ProviderKeys) error {
	return editProfile(profileDir, "provider keys", func(held map[string]json.RawMessage) (profileChange, error) {
		return encodeChange(providerKeyUpdates(held, change(providerKeysFrom(held))))
	})
}

// providerKeyUpdates is the rows to set and remove, and the service list to
// write, that bring the stored keys (in held) to want.
func providerKeyUpdates(held map[string]json.RawMessage, want ProviderKeys) map[string]any {
	stored := providerKeysFrom(held)
	updates := rowKeyUpdates(stored.Rows, want.Rows)
	if !maps.Equal(stored.Sources, want.Sources) {
		updates[keyModelSources] = sourcesWithKeys(persistedSourcesFrom(held), want.Sources)
	}
	return updates
}

func rowKeyUpdates(held, want map[string]string) map[string]any {
	updates := map[string]any{}
	for _, name := range secretRowKeys() {
		switch value, ok := want[name]; {
		case held[name] == want[name]:
		case ok:
			updates[name] = value
		default:
			updates[name] = removeProfileKey
		}
	}
	return updates
}

// sourcesWithKeys is rows with each inline key set to the one wanted for that
// service, in the cleaned form the file keeps.
func sourcesWithKeys(rows []PersistedSource, want map[string]string) []PersistedSource {
	for i := range rows {
		if rows[i].KeyEnv == "" {
			rows[i].Key = want[rows[i].ID]
		}
	}
	return cleanSources(rows)
}

// ProviderKeysHeldBy is keys cut down to what this profile can hold: a service
// key is kept only for a service row the profile has and that stores its key
// inline. It is what ReadProviderKeys answers after WriteProviderKeys(keys).
func ProviderKeysHeldBy(profileDir string, keys ProviderKeys) ProviderKeys {
	var kept ProviderKeys
	for _, name := range secretRowKeys() {
		kept.Rows = putNonEmpty(kept.Rows, name, keys.Rows[name])
	}
	for _, row := range PersistedSources(profileDir) {
		if row.KeyEnv == "" {
			kept.Sources = putNonEmpty(kept.Sources, row.ID, keys.Sources[row.ID])
		}
	}
	return kept
}

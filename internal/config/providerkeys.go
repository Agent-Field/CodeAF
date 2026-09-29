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
// is absent from keys is removed, and every other field of config.json, and
// every other field of a service row, is left as it was. It goes through the
// profile's own locked, atomic writer, and it writes nothing when the file
// already says the same. A service row this machine does not have is skipped,
// since a key alone cannot make one.
func WriteProviderKeys(profileDir string, keys ProviderKeys) error {
	held, err := ReadProviderKeys(profileDir)
	if err != nil {
		return err
	}
	if err := writeRowKeys(profileDir, held.Rows, keys.Rows); err != nil {
		return err
	}
	return writeSourceKeys(profileDir, held.Sources, keys.Sources)
}

func writeRowKeys(profileDir string, held, want map[string]string) error {
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
	return writeProfileValues(profileDir, updates)
}

func writeSourceKeys(profileDir string, held, want map[string]string) error {
	if maps.Equal(held, want) {
		return nil
	}
	rows := PersistedSources(profileDir)
	for i := range rows {
		if rows[i].KeyEnv == "" {
			rows[i].Key = want[rows[i].ID]
		}
	}
	return WriteSources(profileDir, rows)
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

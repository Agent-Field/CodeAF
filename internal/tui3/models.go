package tui3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// The model list the picker shows, and the one law about where it comes from:
// NOTHING here touches the network.
//
// A person who types /model is asking a question about names they already half
// know, and the answer has to be on screen in the same frame. So the list is
// resolved from what is already known, in this order:
//
//  1. the catalog, when it can answer without a fetch — the door passes it in
//     as [Options.Models] (see cmd/aforge/chatv3.go);
//  2. this package's own cache, ~/.aforge/v3/models.json, written whenever a
//     catalog fetch elsewhere succeeded;
//  3. [BuiltinModels], five names this build remembers.
//
// The third rung is what makes the first launch on a cold machine still open a
// picker rather than an empty box, and the second is what makes the launch
// after it show the whole catalog instantly.
//
// [app.modelList] is where that order is actually applied; the pieces live here.

// Model is one row of the picker: a model id, and how much context it takes.
//
// ContextLength is zero when nobody said — an id from [BuiltinModels], a cache
// written before the field existed, a provider that publishes no figure. Zero
// is absence and never a tiny model: the row draws no figure at all, and the
// session keeps whatever window it was configured with (design-law-v2 §16
// EMPTINESS, and the same rule catalog.Model.ContextLength states).
type Model struct {
	ID            string `json:"id"`
	ContextLength int    `json:"context_length,omitempty"`
}

// modelCacheName is the file under the aforge state root. It is v3's own list
// and deliberately NOT internal/catalog's cache: this one holds the two fields
// a picker draws, so reading it costs a kilobyte or two rather than the whole
// six-hundred-row catalog, and a schema change on either side cannot break the
// other.
var modelCacheName = []string{"v3", "models.json"}

// ModelCachePath is ~/.aforge/v3/models.json, moved wholesale by AFORGE_HOME
// the way every other file aforge writes is.
func ModelCachePath() string { return home.Join(modelCacheName...) }

// modelCache is the file's shape. The list is wrapped in an object so a field
// can be added later without the file becoming unreadable by the build that
// wrote it.
type modelCache struct {
	Models []Model `json:"models"`
}

// CachedModels reads the cache, and answers nil for every way that can fail —
// no file, no home directory, a half-written file, an empty list. A picker with
// no cache falls through to the built-ins; a picker that reported a parse error
// would be answering "which model" with a filesystem complaint.
func CachedModels() []Model {
	raw, err := os.ReadFile(ModelCachePath())
	if err != nil {
		return nil
	}
	var cached modelCache
	if json.Unmarshal(raw, &cached) != nil {
		return nil
	}
	return cleanModels(cached.Models)
}

// WriteModelCache replaces the cache with models. It is called from the door
// after a catalog fetch has succeeded — never from the picker, which must not
// spend I/O on the keystroke path — and it writes through a temporary file so a
// process that dies mid-write leaves the previous list readable rather than
// half a JSON document.
func WriteModelCache(models []Model) error {
	models = cleanModels(models)
	if len(models) == 0 {
		// Refusing to write an empty list is what keeps a bad fetch from
		// erasing a good cache.
		return nil
	}
	path := ModelCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(modelCache{Models: models})
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// BuiltinModels is the last rung: names this build remembers, in the order a
// person is most likely to want them. No context lengths — these are not rows
// anybody fetched, and inventing a window for a model this process has never
// heard back from is exactly the guess [Model.ContextLength]'s zero exists to
// avoid.
func BuiltinModels() []Model {
	return []Model{
		{ID: "deepseek/deepseek-v4-flash"},
		{ID: "openai/gpt-4.1-mini"},
		{ID: "anthropic/claude-sonnet-4.5"},
		{ID: "google/gemini-2.5-flash"},
		{ID: "moonshotai/kimi-k3"},
	}
}

// cleanModels drops blank and duplicate ids, keeping the first of each and the
// order it arrived in. Order is meaning here — it is what an empty filter box
// shows — so nothing is sorted.
func cleanModels(models []Model) []Model {
	seen := make(map[string]bool, len(models))
	cleaned := make([]Model, 0, len(models))
	for _, model := range models {
		model.ID = strings.TrimSpace(model.ID)
		if model.ID == "" || seen[model.ID] {
			continue
		}
		seen[model.ID] = true
		if model.ContextLength < 0 {
			model.ContextLength = 0
		}
		cleaned = append(cleaned, model)
	}
	return cleaned
}

// contextWord is a window in the shortest form that stays honest: "1M", "164k",
// "512". Empty when nobody said, because a row is more readable with a gap in
// it than with a zero that has to be explained.
func contextWord(tokens int) string {
	switch {
	case tokens <= 0:
		return ""
	case tokens >= 1_000_000:
		return strconv.Itoa(tokens/1_000_000) + "M"
	case tokens >= 1_000:
		return strconv.Itoa(tokens/1_000) + "k"
	default:
		return strconv.Itoa(tokens)
	}
}

package tui3

import (
	"encoding/json"
	"math"
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

	// The three prices are PER TOKEN in US dollars, as the catalog publishes
	// them. They are here — on a row a picker draws — because of one line this
	// surface has to be able to write: what a turn's cache reads SAVED, which is
	// cached tokens × (PromptPrice − CacheReadPrice) and cannot be computed from
	// anything the session knows. A session knows tokens; only a catalog knows
	// what a token costs.
	//
	// Zero is "nobody published a figure" and never "free" — the same rule
	// catalog.Model states, and the reason the note falls back to showing only
	// the cached token count rather than a saving of $0.00.
	PromptPrice     float64 `json:"prompt_price,omitempty"`
	CompletionPrice float64 `json:"completion_price,omitempty"`
	CacheReadPrice  float64 `json:"cache_read_price,omitempty"`

	// ArenaElo is the best Design Arena Elo the catalog carries for this model,
	// zero when it carries none. It rides the same fetch and the same cache file
	// as the prices, and a row that has to be re-fetched to answer a question a
	// picker will obviously ask next is a row that was written too thin.
	ArenaElo float64 `json:"arena_elo,omitempty"`
	// Reasoning says the provider accepts a reasoning knob on this model —
	// `supported_parameters` carrying "reasoning", "include_reasoning" or
	// "reasoning_effort" (catalog.Model.Reasons and ReasoningLevels).
	//
	// It is what gates ctrl+t on a row (palette.go): a level asked for on a model
	// whose endpoint does not take one is a 400 the person did not do anything
	// to earn. FALSE IS ALSO "NOBODY SAID" — a built-in row, a cache written
	// before this field existed — so the gate is conservative in the one
	// direction it can afford to be: the knob is missing on a model that might
	// have taken it, rather than offered on one that would refuse.
	Reasoning bool `json:"reasoning,omitempty"`

	// Output is what the model ANSWERS IN, as the catalog publishes it
	// (architecture.output_modalities): "text", "image", "speech", "music",
	// "video". It rides the cache so the fact survives a restart, and it is what
	// [chatModels] reads to keep a picker row a model somebody can talk to.
	//
	// Empty is "nobody said" and not "answers in nothing" — a built-in row, a
	// cache written before this field existed. See [answersText] for what that
	// silence costs.
	Output []string `json:"output_modalities,omitempty"`

	// Input is what the model READS, from the same place (architecture.
	// input_modalities): "text", "image", "audio", "file". It is the other half
	// of the question every slot on this surface asks — a slot is answered by a
	// model that can take what it will be handed — and without it two whole
	// families lie to the picker: a transcription model (audio in, text out)
	// passes the output law with room to spare, and a model that CANNOT see is
	// indistinguishable from one that can.
	//
	// Empty is "nobody said" here too, and the two sides read their silence
	// separately: see [readsText] and [seesImages].
	Input []string `json:"input_modalities,omitempty"`
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
		model.Output = cleanModalities(model.Output)
		model.Input = cleanModalities(model.Input)
		cleaned = append(cleaned, model)
	}
	return cleaned
}

// cleanModalities folds a modality list to lower case and drops the blanks, so
// every comparison after it is a plain string equality. Nil in, nil out — the
// absence has to survive, because absence is a state [answersText] reads.
func cleanModalities(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.ToLower(strings.TrimSpace(value)); value != "" {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ── who belongs in a picker: TEXT OUT, AND TEXT IN ──────────────────────────
//
// A row in the model list is a model somebody is about to TALK TO. The catalog
// carries hundreds of rows that are real models and answer in something else —
// pictures, speech, music, video — and every one of them in this list is a name
// a person has to read past to find the one they meant, or worse, picks and
// then watches the conversation break against.
//
// The rule is text out and nothing but text. The looser reading — "text is
// somewhere in the output list" — is what let the picker fill with image
// models: google/gemini-3.1-flash-image publishes ["image","text"], because it
// captions what it draws. It is a drawing model that also writes, not a model
// you hold a conversation with, and the reading that keeps it is the reading
// that keeps every drawing model on the market.
//
// AND THE RULE HAS A SECOND SIDE, because output alone cannot see the whole
// question: a transcription model answers in text and nothing but text, and
// takes SOUND. It is a chat row by the output law and a dead end in practice —
// the first thing anybody sends it is a sentence it cannot read. So a chat row
// must also take text in ([readsText]), and a slot that wants something else —
// the "looking" row, which wants a model that reads PICTURES — asks its own
// question through the same mechanism ([modelFilter]) rather than through a
// second list.

// ── THE PREDICATE: ONE MECHANISM, ONE QUESTION PER SLOT ─────────────────────
//
// A picker is opened to answer a SLOT, and a slot is a question about
// modalities: the conversation asks for a model you can talk to, the "looking"
// row asks for one that can see. Those are two questions and there is one
// mechanism for them — a [modelFilter] handed to the picker at the moment it
// opens ([picker.startFor]) — because the alternative is what this surface had
// before: one hard-wired rule inside the list, and every other slot showing the
// whole catalog and hoping.
//
// The filter is chosen from the ROW, in one place ([filterFor] in settings.go),
// so a new slot is a new line there and never a new list here.

// modelFilter is one slot's question, asked of one row.
type modelFilter func(Model) bool

// keepModels is the filter applied. It is the only place a list is narrowed.
func keepModels(models []Model, keep modelFilter) []Model {
	if keep == nil {
		return models
	}
	out := make([]Model, 0, len(models))
	for _, model := range models {
		if keep(model) {
			out = append(out, model)
		}
	}
	return out
}

// chatModels is the list with everything you cannot talk to taken out. It is
// applied to EVERY rung of the source order ([app.modelList]) — the door's
// catalog, the disk cache and the built-ins alike — because the rule is about
// what a row IS and not about where it came from.
func chatModels(models []Model) []Model { return keepModels(models, chatModel) }

// chatModel is THE GENERAL CHAT LAW, and it is two-sided: a model somebody can
// hold a conversation with answers in text and reads text. The output side is
// the older half ([answersText]); the input side ([readsText]) is what keeps a
// transcription model out — whisper answers in text alone and would sail
// through a rule that only looked at what comes back.
func chatModel(model Model) bool { return answersText(model) && readsText(model) }

// seesImages is the VISION slot's question: can this model look at a picture.
//
// SILENCE FALLS THROUGH, which is the opposite of what the door's own vision
// gate does with it (cmd/aforge's v3ReadsImages, where the cost of guessing
// wrong is a photo sent to a model that cannot read one). Here the cost is a
// name missing from a list somebody is choosing from, and the rows that publish
// nothing are the cache written before this field travelled — hiding all of
// them would leave the row unanswerable. The row's own blank still means
// "aforge picks one that can see", so nothing here has to guess for it.
func seesImages(model Model) bool {
	if len(model.Input) == 0 {
		return true
	}
	return hasModality(model.Input, "image")
}

func hasModality(modalities []string, want string) bool {
	for _, modality := range modalities {
		if modality == want {
			return true
		}
	}
	return false
}

// answersText is the rule for one row, in two rungs.
//
// THE PUBLISHED ANSWER WINS. A row that says what it answers in is taken at its
// word: text, and only text, or it is not a chat model.
//
// A row that says NOTHING is read by its id, and that rung exists because of
// exactly one thing: the caches and the door rows written before Output
// travelled carry no modalities at all, and "silence is a yes" on those is the
// defect this rule was written to close. It is deliberately narrow — the marks
// below are the generation families whose names mean one thing — and it never
// overrides a row that did publish. A model that says "text" is a chat model
// whatever it is called.
func answersText(model Model) bool {
	if len(model.Output) > 0 {
		text := false
		for _, modality := range model.Output {
			if modality != "text" {
				return false
			}
			text = true
		}
		return text
	}
	return !generatorID(model.ID)
}

// readsText is the input side of the law, in the same two rungs.
//
// THE PUBLISHED ANSWER WINS AGAIN. A row that lists what it reads is taken at
// its word: text has to be in it, or the model cannot be handed a sentence.
// That one line is what excludes the transcription family — whisper publishes
// ["audio"] in and ["text"] out, so it passes [answersText] and fails here,
// which is exactly the shape of the defect: a picker full of models that answer
// in text and cannot be spoken to.
//
// A row that says NOTHING is read by its id, against a WIDER vocabulary than
// the output rung's ([sidecarMarks]): a silent row is a cache line written
// before modalities travelled, and by then the only witness left is the name.
func readsText(model Model) bool {
	if len(model.Input) > 0 {
		return hasModality(model.Input, "text")
	}
	return !sidecarID(model.ID)
}

// generationMarks are the id words that mean "this model makes a picture, a
// voice or a film" — the OUTPUT rung's vocabulary, and deliberately the narrow
// one: it decides [answersText] alone, where a word that is as often an input
// as an output would hide a chat model on the strength of its name.
//
// They are matched as WHOLE HYPHEN-SEPARATED WORDS of the id, never as
// substrings: "image" catches google/gemini-3.1-flash-image and
// openai/gpt-5-image-mini, and cannot catch a chat model whose name merely
// contains the letters.
var generationMarks = map[string]bool{
	"image": true, "imagen": true, "images": true,
	"tts": true, "dalle": true, "sora": true, "veo": true,
}

// sidecarMarks are the words the generation list leaves out, and they are the
// families a CHAT list has no room for whichever direction they run in: speech
// and music and film in either direction, and the three kinds of model that
// answer with a vector or a verdict rather than with a sentence.
//
// They are read for the input side only ([readsText]), so the two rungs cannot
// disagree with each other: "qwen3-audio-instruct" still ANSWERS in text — that
// is a true fact about it and [answersText] keeps saying so — it is simply not
// a row a person choosing a conversation should have to read past.
var sidecarMarks = map[string]bool{
	"audio": true, "voice": true, "whisper": true, "lyria": true,
	"music": true, "video": true,
	"embedding": true, "embeddings": true,
	"moderation": true, "rerank": true, "reranker": true,
}

// generatorID reads the id for a generation family. See [generationMarks].
func generatorID(id string) bool { return markedID(id, generationMarks) }

// sidecarID reads it for anything that is not a conversation: the generation
// families and [sidecarMarks] together, since a model that draws is no more a
// chat row than one that transcribes.
func sidecarID(id string) bool {
	return markedID(id, generationMarks) || markedID(id, sidecarMarks)
}

// markedID reports whether any whole hyphen-separated word of the id's last
// segment is in the table.
func markedID(id string, marks map[string]bool) bool {
	id = strings.ToLower(id)
	if at := strings.LastIndexByte(id, '/'); at >= 0 {
		id = id[at+1:]
	}
	for _, word := range strings.Split(id, "-") {
		if marks[word] {
			return true
		}
	}
	return false
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

// ── what a row says about a model, past its name ────────────────────────────
//
// A picker row used to carry the window and nothing else, which answered
// exactly one of the three questions somebody scrolling six hundred names is
// actually asking: how much can it hold, what does it cost, is it any good. The
// other two were a browser tab away, so the row was a list of names and the
// choosing happened somewhere else.
//
// All three are facts the catalog already fetched (models.json holds them), so
// carrying them costs no request and no wait. EVERY ONE OF THEM HIDES WHEN
// NOBODY PUBLISHED IT — a gap in a row is readable, and a zero that has to be
// explained is not (design-law-v2 §16 EMPTINESS).

// modelNote is the dim tail of one picker row: "128k · $0.08/$0.15 per M ·
// elo 1243", with each part left out when the catalog never said. Empty when
// nothing is known, which is what a built-in row answers.
func modelNote(model Model) string {
	parts := make([]string, 0, 3)
	if window := contextWord(model.ContextLength); window != "" {
		parts = append(parts, window)
	}
	if price := priceWord(model.PromptPrice, model.CompletionPrice); price != "" {
		parts = append(parts, price)
	}
	if elo := eloWord(model.ArenaElo); elo != "" {
		parts = append(parts, elo)
	}
	return strings.Join(parts, " · ")
}

// priceWord is what a million tokens cost, prompt then completion:
// "$0.08/$0.15 per M".
//
// PER MILLION and not per token, because per token is the unit the catalog
// publishes and nobody reads: $0.00000008 is eight zeros a person has to count
// to compare two rows. Per million is the unit every provider's own price page
// quotes, so the figure on the row is the figure somebody already has in mind.
//
// Both halves must be known. Zero is "nobody published a figure" and never
// "free" ([Model]'s own rule), so half a price is not a cheaper model — it is a
// row that cannot answer, and a row that cannot answer says nothing.
func priceWord(prompt, completion float64) string {
	if prompt <= 0 || completion <= 0 {
		return ""
	}
	return "$" + perMillion(prompt) + "/$" + perMillion(completion) + " per M"
}

// perMillion renders one per-token price as dollars per million tokens, to two
// significant figures with the trailing zeros trimmed: 0.08, 0.15, 3, 15, 150.
//
// Two figures because that is the precision the choice actually turns on — the
// difference between $3 and $15 decides something and the difference between
// $3.00 and $3.02 decides nothing — and because a column of eight-digit
// fractions is a column nobody compares down.
func perMillion(perToken float64) string {
	value := perToken * 1_000_000
	if value <= 0 {
		return ""
	}
	// 'e' with one digit after the point IS two significant figures, and going
	// back through ParseFloat is what applies the rounding before the decimal
	// form is chosen — so 153 rounds to 150 rather than being printed whole.
	rounded, err := strconv.ParseFloat(strconv.FormatFloat(value, 'e', 1, 64), 64)
	if err != nil || rounded <= 0 {
		return ""
	}
	// The decimals are however many it takes for the second significant figure
	// to survive: 0.08 needs three, 15 needs none. %g cannot be used for this —
	// it turns 150 into 1.5e+02 exactly when the price is worth reading.
	decimals := 1 - int(math.Floor(math.Log10(rounded)))
	if decimals < 0 {
		decimals = 0
	}
	if decimals > 8 {
		decimals = 8
	}
	text := strconv.FormatFloat(rounded, 'f', decimals, 64)
	if strings.Contains(text, ".") {
		text = strings.TrimRight(text, "0")
		text = strings.TrimRight(text, ".")
	}
	return text
}

// eloWord is the arena score as "elo 1243", empty when the catalog carries
// none. It is spelled out rather than left as a bare number because a bare
// four-digit figure beside a price and a window is a fourth number nobody can
// name.
func eloWord(elo float64) string {
	if elo <= 0 {
		return ""
	}
	return "elo " + strconv.Itoa(int(math.Round(elo)))
}

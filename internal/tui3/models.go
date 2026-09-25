package tui3

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/config"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	modelcatalog "github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/home"
)

// The model list the picker shows, and the one law about where it comes from:
// NOTHING here touches the network.
//
// A person who types /model is asking a question about names they already half
// know, and the answer has to be on screen in the same frame. So the list is
// resolved from what is already known, in this order:
//
//  1. the catalog, when it can answer without a fetch — the door passes it in
//     as [Options.Models] (see cmd/codeaf/chatv3.go);
//  2. this package's own cache, ~/.codeaf/v3/models.json, written whenever a
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
	// Zero alone is not evidence of a free tariff: PriceKnown records whether
	// the catalog published these figures. An absent figure never becomes a
	// claimed saving of $0.00.
	PromptPrice     float64 `json:"prompt_price,omitempty"`
	CompletionPrice float64 `json:"completion_price,omitempty"`
	RequestPrice    float64 `json:"request_price,omitempty"`
	PriceKnown      bool    `json:"price_known,omitempty"`
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

	// Group is the connected service heading this row sits under. It is empty
	// on the single-service path, which keeps that picker's output unchanged.
	Group       string `json:"-"`
	GroupHead   string `json:"-"`
	GroupOrder  int    `json:"-"`
	Unavailable bool   `json:"-"`
	AddProvider bool   `json:"-"`
	// Notice is display text for an unavailable group row. Such a row has no
	// ID: a sentence explaining an empty service is not a model and therefore
	// cannot be selected, pinned, unfolded, or handed to a wire-facing path.
	Notice string `json:"-"`
	// Direct is true when the row belongs to a connected non-default service.
	// Such a service has one road, so router lane facts and controls do not
	// belong on its row.
	Direct bool `json:"-"`
}

// modelCacheName is the file under the codeaf state root. It is v3's own list
// and deliberately NOT internal/catalog's cache: this one holds the two fields
// a picker draws, so reading it costs a kilobyte or two rather than the whole
// six-hundred-row catalog, and a schema change on either side cannot break the
// other.
var modelCacheName = []string{"v3", "models.json"}

// ModelCachePath is ~/.codeaf/v3/models.json, moved wholesale by CODEAF_HOME
// the way every other file codeaf writes is.
func ModelCachePath() string { return home.Join(modelCacheName...) }

// ModelCachePathFor returns the cache owned by one service-and-base pair. The
// default pair keeps the legacy path so an ordinary upgrade stays warm.
func ModelCachePathFor(source, base string) string {
	key := modelcatalog.CacheKey(source, base)
	if key == "" {
		return ModelCachePath()
	}
	return home.Join("v3", "models-"+key+".json")
}

// modelCache is the file's shape. The list is wrapped in an object so a field
// can be added later without the file becoming unreadable by the build that
// wrote it.
type modelCache struct {
	Models []Model `json:"models"`
	Owner  string  `json:"owner,omitempty"`
}

// CachedModels reads the cache, and answers nil for every way that can fail —
// no file, no home directory, a half-written file, an empty list. A picker with
// no cache falls through to the built-ins; a picker that reported a parse error
// would be answering "which model" with a filesystem complaint.
func CachedModels() []Model {
	return CachedModelsFor("", modelcatalog.DefaultBaseURL)
}

// CachedModelsFor reads only the cache owned by the requested service and
// base. An unstamped legacy file belongs only to the default pair.
func CachedModelsFor(source, base string) []Model {
	owner := modelcatalog.CacheKey(source, base)
	raw, err := os.ReadFile(ModelCachePathFor(source, base))
	if err != nil {
		return nil
	}
	var cached modelCache
	if json.Unmarshal(raw, &cached) != nil {
		return nil
	}
	if cached.Owner == "" {
		if owner != "" {
			return nil
		}
	} else if cached.Owner != owner {
		return nil
	}
	return cleanModels(cached.Models)
}

// ── THE CACHE AS A FACT THE LOOP LEARNED ────────────────────────────────────
//
// [CachedModelsFor] is a file read, and a frame that has to name a model reaches
// it — through [app.modelsForDefault] and [app.modelsForConnectedService] —
// whenever the door handed this surface no catalog. That is the first-run
// screen, every window opened with no key, and every window whose fetch has not
// landed yet: the ones where the list matters most were the ones re-reading the
// whole file thirty times a second.
//
// So the read is a memo now, on the surface's ONE mechanism for a fact the loop
// learned and the frame reads (learned.go). The name a reading is filed under is
// the service and the base it belongs to, joined, because that pair is what
// decides which file on disk answers and whose rows are in it.

// modelCacheName joins the pair into the one name the memo files a reading
// under. It is a function rather than a format string at each site because the
// name is also what [readModelCacheName] takes apart again, and a join and a
// split that disagree would serve one service's rows under another's name.
func modelCacheNameFor(source, base string) string { return source + "\x00" + base }

// readModelCacheName is the memo's one reading: the pair, taken apart, read off
// the disk. It is installed on [app.modelLists] at construction and called from
// nowhere else.
func readModelCacheName(name string) []Model {
	source, base, _ := strings.Cut(name, "\x00")
	return CachedModelsFor(source, base)
}

// cachedModelsFor is THE FRAME'S DOOR onto a service's cached rows: the memo,
// and nothing else. An empty answer is "nobody has read that file yet" as much
// as it is "the file holds nothing", and both draw the same thing — the rung
// below, which is the built-ins (see this file's head). The loop fills the memo
// at `open` ([app.learnModelLists]), on the pulse's beat and whenever a fetch
// rewrites a file ([app.forgetModelList]).
func (a *app) cachedModelsFor(source, base string) []Model {
	models, _ := a.modelLists.of(modelCacheNameFor(source, base))
	return models
}

// cachedModels is the same door for the default pair, which is what a surface
// with no connected service asks about.
func (a *app) cachedModels() []Model {
	return a.cachedModelsFor("", modelcatalog.DefaultBaseURL)
}

// learnModelLists is `open`'s reading: the default pair and every service this
// profile has connected, read before the first frame asks about any of them.
func (a *app) learnModelLists() {
	a.modelLists.learn(modelCacheNameFor("", modelcatalog.DefaultBaseURL))
	for _, service := range a.sources.All() {
		name := modelCacheNameFor(service.Source.ID, service.Address)
		a.modelLists.learn(name)
		if strings.EqualFold(service.Source.ID, "codex") {
			// A CODEX LIST IS A COPY OF THE CATALOG, and it is brought back into
			// step with the catalog on open whenever the two differ. A connection
			// made before #1383 wrote no list for this surface and a catalog with
			// no windows in it, so a surface with no process shelf behind it — the
			// engine road's — had no row to read a Codex model's window from and
			// kept the previous model's figure. And a list written from the
			// fallback's figure must give way when a later listing, taken by any
			// door, remembers a different one: the terminal's `codeaf connect`
			// rewrites the catalog and never this surface's copy. A list already
			// in step is left alone, so an ordinary open writes nothing.
			if rows := codexCatalogModels(a.profileDir); len(rows) > 0 && !sameModelRows(rows, a.cachedModelsFor(service.Source.ID, service.Address)) {
				_ = WriteModelCacheFor(service.Source.ID, service.Address, rows)
				a.modelLists.forget(name)
				a.modelLists.learn(name)
			}
		}
	}
}

// sameModelRows reports whether two lists name the same models, in the same
// order, with the same windows.
func sameModelRows(left, right []Model) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].ID != right[index].ID || left[index].ContextLength != right[index].ContextLength {
			return false
		}
	}
	return true
}

// forgetModelList is for the one caller who KNOWS the file under a pair just
// changed — a fetch that wrote it, ctrl+r asking for a fresh list — and will not
// wait for the beat to find out.
func (a *app) forgetModelList(source, base string) {
	a.modelLists.forget(modelCacheNameFor(source, base))
}

// serviceModelsLanded is [Options.OnServiceModels]'s body: one provider's
// listing was stocked behind the frame (a launch warm or a ctrl+r walk). The
// memo under that pair is a reading from before the fetch, so it is dropped;
// an open picker is restocked so its group fills WITHOUT a reopen.
func (a *app) serviceModelsLanded(source, address string) {
	a.forgetModelList(source, address)
	if a.pick.open {
		a.pick.restock(a.modelPickerList())
	}
	a.touch()
}

// WriteModelCache replaces the cache with models. It is called from the door
// after a catalog fetch has succeeded — never from the picker, which must not
// spend I/O on the keystroke path — and it writes through a temporary file so a
// process that dies mid-write leaves the previous list readable rather than
// half a JSON document.
func WriteModelCache(models []Model) error {
	return WriteModelCacheFor("", modelcatalog.DefaultBaseURL, models)
}

// WriteModelCacheFor replaces the cache for one service-and-base pair.
func WriteModelCacheFor(source, base string, models []Model) error {
	models = cleanModels(models)
	if len(models) == 0 {
		// Refusing to write an empty list is what keeps a bad fetch from
		// erasing a good cache.
		return nil
	}
	path := ModelCachePathFor(source, base)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(modelCache{Models: models, Owner: modelcatalog.CacheKey(source, base)})
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

// KeepForSlot is one settings row's question, asked of a list from outside this
// package: [config.ModelSettingKey]'s key in, the rows that could answer that
// slot out.
//
// It exists because the door and the surface now share one supply and have to
// be testable against each other. Since Decision 6 the door hands over the WHOLE
// catalog and every list narrows it where it is drawn, which means the question
// "did the drawing slot's picker actually get a drawing model" spans two
// packages — and the only honest way to ask it is to run a real catalog through
// the door's own list-builder and then through this. Injecting hand-written rows
// would test the filter against a fixture rather than against the product.
func KeepForSlot(models []Model, settingsKey string) []Model {
	return keepModels(models, filterFor(settingsKey))
}

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

// chatModels is the list with everything you cannot talk to taken out — the
// general chat law, named, for a caller that has a list rather than a slot.
//
// The list a slot actually draws is resolved by [app.modelsFor], which applies
// that slot's own predicate to EVERY rung of the source order — the door's
// catalog, the disk cache and the built-ins alike — because the rule is about
// what a row IS and not about where it came from.
func chatModels(models []Model) []Model { return keepModels(models, chatModel) }

// ChatModels is that same law for the DOOR, which since Decision 6 hands this
// package the whole catalog and has its own list to narrow: the models a task
// may be handed to (cmd/codeaf's v3TaskModels).
//
// It is exported rather than copied because a second spelling of "a model you
// can talk to" is a second spelling that drifts — the picker would offer a row
// the task argument refused, or the reverse, and neither surface could say why.
func ChatModels(models []Model) []Model { return chatModels(models) }

// chatModel is THE GENERAL CHAT LAW, and it is two-sided: a model somebody can
// hold a conversation with answers in text and reads text. The output side is
// the older half ([answersText]); the input side ([readsText]) is what keeps a
// transcription model out — whisper answers in text alone and would sail
// through a rule that only looked at what comes back.
func chatModel(model Model) bool { return answersText(model) && readsText(model) }

// seesImages is HALF the vision slot's question ([inspectsImages] is the whole
// of it): can this model look at a picture.
//
// SILENCE IS NO, and it is the SAME no the door's vision gate gives it
// (cmd/codeaf's v3ReadsImages) — THE ONE SILENCE LAW, docs/MULTIMODAL.md
// Decision 6: an unpublished modality list means text-in/text-out and nothing
// more, so a media capability is never assumed, only published.
//
// This predicate used to read that silence the other way, and the two halves of
// one feature then disagreed about one row: a silent model was offered in the
// looking picker as something that could see, chosen, and then refused every
// photo by the gate that actually decides. A list that offers what the gate
// will reject is worse than a shorter list.
//
// The id-word marks are the last resort, exactly as [makesModality] uses them:
// a row that published nothing is read by its name, against the narrow
// vocabulary that means sight and nothing else. And the slot's own blank still
// means "codeaf picks one that can see", so nothing here has to guess for it.
func seesImages(model Model) bool {
	if len(model.Input) > 0 {
		return hasModality(model.Input, "image")
	}
	return markedID(model.ID, sightMarks)
}

// inspectsImages is what the VISION SLOT actually asks, and it is [seesImages]
// AND [chatModel] because the slot is an inspection proxy: codeaf hands it a
// picture and reads back a sentence about one (config's ResolveVisionModel, and
// the view_image tool behind it). Image input alone is half the question — it
// keeps google/gemini-3.1-flash-image, which reads pictures and answers in
// pictures, and it keeps every silent row the chat law already reads by name as
// a transcriber or an embedder. Neither can answer "what is in this photo".
//
// The two halves stay separate predicates because they are separately true:
// [seesImages] is the modality question on its own and is tested as one, and a
// later slot that wants sight without speech asks it directly.
func inspectsImages(model Model) bool { return seesImages(model) && chatModel(model) }

func hasModality(modalities []string, want string) bool {
	for _, modality := range modalities {
		if modality == want {
			return true
		}
	}
	return false
}

// ── THE MEDIA SLOTS: WHAT A ROW MAKES ───────────────────────────────────────
//
// The Providers tab has five slots that are not conversations — drawing,
// speaking, composing, filming, and the one that HEARS you — and every one of
// them was opened over the chat list. That is not a narrow list, it is an EMPTY
// one by construction: [chatModel] keeps exactly the rows these slots cannot
// use and drops exactly the rows they need. A person opening "drawing" was
// offered six hundred models, none of which draws.
//
// So each slot asks its own question, and the questions are the ones
// [config.ModelCandidates] already answers for the rest of the product: image
// out, speech out, music out, video out. The voice slot is the one reading that
// differs, and only in its witness — the catalog publishes a "transcription"
// output modality and OpenRouter's rows do not, so here the same slot is read as
// SOUND IN AND WORDS BACK, which is the same model either way.
//
// THE SILENCE RUNG RUNS THE OTHER WAY HERE, and that is the whole difference
// from [answersText]. A chat row that publishes nothing is KEPT — a cache
// written before modalities travelled is full of chat models and hiding them
// all would empty the picker. A media row that publishes nothing is DROPPED
// unless its name says what it makes, because the same silence over the same
// cache would fill a drawing picker with chat models, which is the defect this
// closes rather than a milder version of it. The cost is a silent drawing model
// nobody named — krea-2-medium-turbo says nothing about itself and is not in
// the marks — and that is the honest direction to be wrong in: a name missing
// from a list somebody can still type into, rather than a list that answers the
// wrong question.

// makesModality is the media law for one row: what it PUBLISHES if it published
// anything, and otherwise what its name says.
func makesModality(model Model, want string, marks map[string]bool) bool {
	if len(model.Output) > 0 {
		return hasModality(model.Output, want)
	}
	return markedID(model.ID, marks)
}

// drawsImages is the "drawing" slot's question.
func drawsImages(model Model) bool { return makesModality(model, "image", imageMarks) }

// speaksAloud is the "speaking" slot's question.
func speaksAloud(model Model) bool { return makesModality(model, "speech", speechMarks) }

// composesMusic is the "composing" slot's question.
//
// internal/config drops the recognizable TTS rows from this slot because the
// catalog files speech under music; the marks below are narrow enough that a
// silent TTS row cannot reach it, and a row that PUBLISHED "music" is taken at
// its word the way every other row on this surface is.
func composesMusic(model Model) bool { return makesModality(model, "music", musicMarks) }

// filmsVideo is the "filming" slot's question.
func filmsVideo(model Model) bool { return makesModality(model, "video", videoMarks) }

// hearsSpeech is the "voice" slot's question, and it is the only media slot
// asked on the INPUT side: the row is not a model that makes a sound, it is the
// one that hears you make one. Sound in, words back — a model that takes audio
// and answers in audio is a speaker, not an ear, so the output law is asked too.
func hearsSpeech(model Model) bool {
	if !answersText(model) {
		return false
	}
	if len(model.Input) > 0 {
		return hasModality(model.Input, "audio")
	}
	return markedID(model.ID, voiceMarks)
}

// The per-family id vocabularies, read only when a row published nothing.
//
// They are separate tables from [generationMarks] because they answer a
// different question: that one asks "is this NOT a conversation", which is
// deliberately the narrow reading, and these ask "is this EXACTLY this family",
// which has to name the family's own products. Every mark is a whole
// hyphen-separated word of the id ([markedID]), so a chat model that merely
// carries the letters is never caught.
var (
	imageMarks  = map[string]bool{"image": true, "imagen": true, "images": true, "dalle": true, "flux": true, "sdxl": true}
	speechMarks = map[string]bool{"tts": true, "speech": true, "kokoro": true}
	musicMarks  = map[string]bool{"music": true, "lyria": true, "suno": true}
	videoMarks  = map[string]bool{"video": true, "sora": true, "veo": true, "seedance": true}
	voiceMarks  = map[string]bool{"asr": true, "stt": true, "whisper": true, "transcribe": true, "transcription": true}

	// sightMarks is the INPUT side's own vocabulary, read only by [seesImages]
	// and only for a row that published nothing. It is the narrowest table
	// here on purpose: these are the two words a vendor puts in a slug to say
	// "this one has eyes", and every other word that might mean sight — multi,
	// omni, flash — means it often enough to be a guess and not a witness.
	sightMarks = map[string]bool{"vl": true, "vision": true}
)

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

// modelNote is the dim tail of one picker row on a frame with room to spare —
// "coreweave · ▲0.4s · $0.08/$0.15 per M · 128k · elo 1243" — with each part
// left out when the catalog never said. Empty when nothing is known, which is
// what a built-in row answers.
//
// It is [rowAll] over [modelFields], and every narrower frame is the same
// fields through the same fitter with an edge on it (rowfit.go).
func modelNote(model Model) string { return modelNoteVia(model, "", config.RoutingLatency) }

// modelNoteVia is that tail with the caller's own knowledge of which machine
// this model is pinned to — empty when it is not pinned or when the caller has
// no profile to ask — and of the routing row in force, which decides whether a
// machine may be predicted at all ([laneAuto]).
func modelNoteVia(model Model, pin, routing string) string {
	return rowAll(modelFields(model, pin, routing))
}

// ── THE PICKER ROW'S DATA HIERARCHY ─────────────────────────────────────────
//
// modelFields is one model as the row's facts, RANKED — and the ranking is the
// whole design, because on a sixty-cell frame the row can only carry three of
// them and which three is not a detail (rowfit.go states how a ranked tail is
// spent).
//
// The order is WHAT A PERSON CHOOSES ON, from the front:
//
//	1  the name          who it is — the primary, and it is never given up
//	2  the lane          WHICH MACHINE will answer: the same model served by
//	                     two providers is two different experiences, and this
//	                     is the one fact on the row that the person's own pin
//	                     changed. It reads `via coreweave` while there is room
//	                     for the lead and `coreweave` after that.
//	3  the first token   will it answer NOW. The wait before the first word is
//	                     the whole felt difference between two models, and it
//	                     is the number this surface measured itself.
//	4  the price out     what it costs — completion first, since that is the
//	                     half a long answer spends. `$0.08/$0.15 per M` →
//	                     `$0.15/M` → `$0.15`.
//	5  the window        how much it can hold. It ranks under price because a
//	                     window is a ceiling somebody meets once a week and a
//	                     price is a figure they pay every turn.
//	6  the throughput    how fast it writes once it has started — a real fact,
//	                     and one that changes a choice far less often than the
//	                     wait before the first word does.
//	7  the arena score   a stranger's opinion, and the first of these a person
//	                     has ever acted on twice.
//	8  what it can do    `sees · draws`, which matters enormously to the few
//	                     rows it is true of and not at all to the rest — so it
//	                     is last, and it is the field a narrow frame drops
//	                     first.
//
// AND THE MODALITIES HAVE NO SHORT SPELLING. A glyph alphabet for "sees" and
// "draws" would be a second vocabulary to learn for the rarest field on the
// row, and this row already has one mark to explain ([laneUpMark]). A field
// that is last to be drawn is a field that should be said in words or not at
// all.
func modelFields(model Model, pin, routing string) []rowField {
	facts := modelFactsOf(model, pin, routing)
	lane, first, rate := rowField{}, rowField{}, rowField{}
	if facts.via != "" {
		lane = rowSay("via "+facts.via, facts.via)
	}
	if facts.first != "" {
		first = rowSay(laneUpMark+facts.first, facts.first)
	}
	if facts.rate != "" {
		rate = rowSay(facts.rate + laneRateUnit)
	}
	// THE TWO MODALITY SIDES ARE TWO FIELDS, exactly as they are two columns,
	// so the tail gives up `outputs` before `inputs` the way a narrow table does
	// and the two shapes rank the same facts the same way. Each spells its own
	// side, because a tail has no head to spell it ([ModalityWord]).
	//
	inputs, outputs := rowField{}, rowField{}
	if facts.inputs != "" {
		inputs = rowSay(modalityInputsLead + " " + facts.inputs)
	}
	if facts.outputs != "" {
		outputs = rowSay(modalityOutputsLead + " " + facts.outputs)
	}
	return []rowField{
		lane,
		first,
		facts.priceField(),
		rowSay(facts.window),
		rate,
		rowSay(facts.eloWord()),
		inputs,
		outputs,
	}
}

// ── ONE READING OF A MODEL, FOR BOTH SHAPES OF ROW ──────────────────────────
//
// modelFacts is what a row says about a model past its name, each fact in the
// BARE spelling — the figure with no unit on it and no word in front of it.
//
// It exists because this surface now draws those facts two ways. The ranked
// tail says the unit on every row, because a fact standing alone in a sentence
// of facts has to name itself: `$0.09/$0.18 per M · 1M · elo 1424`. The table
// says it once, in the column's head, and a row under it carries the figure
// alone. Both are right for what they are, and both must be the same reading of
// the same model — a price that meant dollars per million in one and dollars
// per thousand in the other would be the exact defect CLAUDE.md's
// one-source-of-truth rule is written against.
//
// So the reading happens HERE, once, and each shape dresses it: [modelFields]
// puts the units back on, [modelFacts.cells] leaves them off and lets
// [modelColumns] carry them.
type modelFacts struct {
	// via is the machine that would serve this model, lowercased and with no
	// `via ` in front of it.
	via string
	// first is the wait before the first word, `0.8s`, with no [laneUpMark].
	first string
	// in and out are what a million prompt and completion tokens cost, `$0.09`
	// and `$0.18`. BOTH ARE SET OR NEITHER IS ([priceWord] states why: zero is
	// "nobody published a figure" and never "free", so half a price is a row
	// that cannot answer).
	in, out string
	// window is how much it holds, `128k` or `1M`.
	window string
	// rate is how fast it writes once it has started, `58`, with no unit.
	rate string
	// elo is the arena score as a bare number, `1424`, with no `elo ` on it.
	elo string
	// inputs and outputs are what the model takes in and gives back BEYOND text,
	// in the catalog's own nouns: `image file`, `speech`. They are the two facts
	// here that are already words rather than figures, and the two that a head
	// can only name by side — which is why they are a pair rather than one fact
	// ([modalityInputs], [modalityOutputs]).
	inputs, outputs string
}

// modelFactsOf reads one model. pin is the machine this conversation is held
// to when the row is this conversation's, and routing is the row in force —
// the two things a lane fact cannot be read without.
func modelFactsOf(model Model, pin, routing string) modelFacts {
	facts := modelFacts{
		window:  contextWord(model.ContextLength),
		elo:     eloBare(model.ArenaElo),
		inputs:  modalityInputs(model.Input),
		outputs: modalityOutputs(model.Output),
	}
	// BOTH HALVES OR NEITHER, which is [priceWord]'s rule read once here rather
	// than asked again by everything that draws half a price.
	if model.PromptPrice > 0 && model.CompletionPrice > 0 {
		facts.in = "$" + perMillion(model.PromptPrice)
		facts.out = "$" + perMillion(model.CompletionPrice)
	}
	// A CONNECTED SERVICE HAS ONE ROAD, so router lane facts do not belong on
	// its row and are not even read for it.
	if model.Direct {
		return facts
	}
	via, best, known := modelLaneReading(model, pin, routing)
	facts.via = via
	// THE NUMBERS BELONG TO THE LANE THE ROW NAMES ([laneShown] states why).
	if known {
		facts.first = laneSecondsWord(best.TTFT)
		facts.rate = laneRateBare(best.Rate)
	}
	return facts
}

// modelLaneReading is the lane a model's row NAMES and the figures that row draws
// from it: the name as `via` spells it, the view, and whether it carries timing at
// all.
//
// ── ONE READING, FOR THE CELL AND FOR THE SORT ──────────────────────────────
//
// It is its own function because two things ask it. [modelFactsOf] draws the
// cells; pickersort.go's [pickerRankOf] orders the rows BY those cells, and it
// used to ask [bestLane] instead — which answers a different question. `bestLane`
// is the fastest-feeling lane in the ledger; this is the lane the row actually
// SAYS, which is the pin when there is one, otherwise whatever the chooser would
// send to, and NOTHING AT ALL under a routing row where codeaf does not choose
// ([laneAutoSaid]). So a row whose `first` cell was blank could still carry a real
// number into the sort, and the blanks stopped landing together: the list was
// ordered by a figure that was not on the screen.
//
// THE CLOCK IS READ HERE AND NOT PASSED IN because ageing a belief by a few
// milliseconds cannot change a figure rounded to a tenth of a second, and
// [laneAuto] asks typically — posterior means, no Thompson draw — so the clock
// cannot re-sample a `via` either. Threading a moment through every list on this
// surface to prove that would be a parameter nobody could ever see the effect of.
func modelLaneReading(model Model, pin, routing string) (string, laneView, bool) {
	// A CONNECTED SERVICE HAS ONE ROAD, so router lane facts do not belong on its
	// row and are not even read for it — the same early return the cells take.
	if model.Direct {
		return "", laneView{}, false
	}
	now := timeNow()
	views := laneViews(model.ID, now)
	via := pin
	if via == "" {
		via = laneAuto(routing, model.ID, views, now)
	}
	best, known := laneShown(routing, views, via)
	return strings.ToLower(via), best, known
}

// priceField is the price as the ranked tail's one fact, in three spellings —
// see [priceField]'s own account of why the short ones drop the prompt half.
func (f modelFacts) priceField() rowField {
	if f.in == "" || f.out == "" {
		return rowField{}
	}
	return rowSay(f.in+"/"+f.out+" per M", f.out+"/M", f.out)
}

// eloWord is the arena score with the word that names it, for a tail where no
// column head can.
func (f modelFacts) eloWord() string {
	if f.elo == "" {
		return ""
	}
	return "elo " + f.elo
}

// ── WHAT A MODEL TAKES IN AND GIVES BACK ────────────────────────────────────
//
// Since the door stopped narrowing the list (docs/MULTIMODAL.md Decision 6),
// every picker is a filtered view of one catalog, and a filtered list is only
// explicable if the rows say what they were filtered ON. Six hundred names with
// no capability on them is a list where "why is this one here" has no answer on
// screen.
//
// THESE ARE THE CATALOG'S OWN NOUNS AND NOT A VOCABULARY OF OURS. The row used
// to say `sees · hears · watches · draws`, one invented verb per modality, and
// two things were wrong with it. The first is that a verb has to say the side
// as well as the thing — `sees` is "image, on the way in" — so six words had to
// be learned before a row could be read, and the words were the only place the
// side was written down. A table has a head over every column, and a head can
// say the side for the whole list: under `reads` and `makes`, `image` needs no
// verb at all and no learning.
//
// The second is that the verbs were LOSSY where the catalog is not. `speaks`
// was `speech`, `audio` and `music` folded into one word, so a row that answers
// in music and a row that answers in speech read identically — and the two are
// different products. The noun is what was published, so it cannot fold.
//
// THE EMPTINESS LAW DECIDES WHAT IS SAID: `text` is dropped from both sides,
// because a plain text chat model — text in, text out, the overwhelming
// majority of every list — says NOTHING NEW, and `text` on five hundred rows is
// furniture rather than information. A row that published nothing says nothing
// either: silence is text-in/text-out by the one silence law, which is exactly
// the case that earns no words.

// modalityOrderIn and modalityOrderOut are the order the words are said in, and
// they are OURS rather than the catalog's for one reason: the catalog does not
// have one. The live rows publish the same set three ways — `text, image, file`,
// `file, image, text` and `image, text, file` are all in today's catalog for
// models that read the same things — so a column that echoed the published
// order would put the same fact in a different place on three neighbouring rows,
// which is the exact defect the table was built to end.
//
// TEXT IS NOT IN EITHER LIST. Every model on every list here reads and writes
// it — that is what makes them models you can talk to — so the word would be the
// same five cells on five hundred rows, and a column is as wide as its widest
// row. It was tried the other way for one wave and taken back out: what the
// owner wanted from a cell is what the model can do BEYOND the ordinary, and
// `text` on every row is the ordinary.
//
// The order is what a person is shopping for, commonest first: sight before
// sound before video, and attachments last because a model that takes a file
// usually takes a picture too.
var (
	modalityOrderIn  = []string{"image", "audio", "video", "file"}
	modalityOrderOut = []string{"image", "speech", "audio", "music", "video"}
)

// modalityInputs is what a model takes in beyond text: "image file".
func modalityInputs(input []string) string { return modalitySay(input, modalityOrderIn) }

// modalityOutputs is what it gives back beyond text: "image", "speech".
func modalityOutputs(output []string) string { return modalitySay(output, modalityOrderOut) }

// modalitySay is one side of a model in the catalog's own words, in the given
// order, with `text` left out of both by not being in either order.
//
// THE EMPTINESS LAW DECIDES WHAT IS SAID: a plain text chat model — text in,
// text out, the overwhelming majority of every list — says NOTHING here, and a
// row that published nothing says nothing either, because silence is
// text-in/text-out by the one silence law (docs/MULTIMODAL.md Decision 6) and
// that is exactly the case that earns no words. What a cell is for is the thing
// the model can do BEYOND holding a conversation.
//
// A WORD THIS BUILD HAS NEVER HEARD OF IS STILL SAID, after the ones it knows
// and in sorted order so two rows carrying it agree. The catalog publishes
// `embeddings`, `transcription` and `rerank` today and will publish something
// else tomorrow; a surface that drew only the words it was compiled with would
// answer "text in, text out" for a whole family of models, which is the one
// answer that cannot be told from the truth.
func modalitySay(modalities []string, order []string) string {
	said := make([]string, 0, len(modalities))
	for _, want := range order {
		if hasModality(modalities, want) {
			said = append(said, want)
		}
	}
	rest := make([]string, 0, len(modalities))
	for _, modality := range modalities {
		if modality == "" || modality == "text" || hasModality(order, modality) {
			continue
		}
		rest = append(rest, modality)
	}
	sort.Strings(rest)
	said = append(said, slices.Compact(rest)...)
	return strings.Join(said, modalityJoin)
}

// modalityJoin is what separates two modalities INSIDE one cell, and it is a
// SPACE. A comma was three cells wider on the widest row, and a column is as
// wide as its widest row — three cells that decided, at a hundred columns,
// whether the column was drawn at all. Under a head that names the side, the
// words read as a set without help.
const modalityJoin = " "

// ModalityWord is both sides as ONE string, for a row with no head to lean on:
// "inputs image file · outputs image".
//
// THE SIDE IS SAID IN WORDS HERE BECAUSE THERE IS NOTHING ELSE TO SAY IT. In
// the table a head carries it (modeltable.go's `inputs` and `outputs` columns)
// and the cell is the words alone; in a ranked tail, on a phone, and on
// `codeaf models` there is no head, so the tail spells the side out.
//
// It is exported for `codeaf models`, which draws the same tail beside the same
// facts (cmd/codeaf's models.go). One spelling, in one place.
func ModalityWord(input, output []string) string {
	said := make([]string, 0, 2)
	if inputs := modalityInputs(input); inputs != "" {
		said = append(said, modalityInputsLead+" "+inputs)
	}
	if outputs := modalityOutputs(output); outputs != "" {
		said = append(said, modalityOutputsLead+" "+outputs)
	}
	return strings.Join(said, rowSep)
}

// modalityInputsLead and modalityOutputsLead are the one word that names each
// side. They are the SAME words the table's heads carry ([modelColumns]), so a
// person who read `inputs` over a column and a person who read `inputs text
// image` on a phone have learned one thing and not two.
const (
	modalityInputsLead  = "inputs"
	modalityOutputsLead = "outputs"
)

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

// eloBare is the arena score as a bare number, "1424", and empty when the
// catalog carries none. The word that names it is put back on by whichever
// shape of row draws it — [modelFacts.eloWord] for the tail, the column's own
// head for the table — because a bare four-digit figure beside a price and a
// window is a fourth number nobody can name until something names it.
func eloBare(elo float64) string {
	if elo <= 0 {
		return ""
	}
	return strconv.Itoa(int(math.Round(elo)))
}

// Package config holds the harness's process-wide defaults. It is the only
// place a model slug or an endpoint is written down, so changing the default
// model is a one-line edit rather than a search across the tree.
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/calllog"
	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/router"
)

const (
	// DefaultModel is the harness's default. DeepSeek V4 Flash Latest is the
	// floating alias, so a new dated snapshot is picked up without a code change.
	// 1M context, ~$0.09/M in and ~$0.18/M out, and it advertises both
	// structured_outputs and reasoning on OpenRouter.
	DefaultModel = "~deepseek/deepseek-v4-flash-latest"

	// DefaultVoiceModel is the independent speech-to-text slot. Voice never
	// enters the talk/work router: OpenRouter exposes it through the dedicated
	// audio transcription endpoint.
	DefaultVoiceModel = "qwen/qwen3-asr-flash-2026-02-10"

	// DefaultBaseURL is OpenRouter's OpenAI-compatible endpoint.
	DefaultBaseURL = "https://openrouter.ai/api/v1"

	// DefaultSiteURL, DefaultSiteName and DefaultSiteCategories are the
	// OpenRouter app-attribution values this binary reports under
	// (HTTP-Referer, X-OpenRouter-Title, X-OpenRouter-Categories). They are
	// INTERPOLATED FROM internal/provider AND NOT RE-SPELLED, because the
	// package that writes the headers is the one place the values may live;
	// two copies of an app's identity is how one product's usage ends up on
	// two dashboard pages. Nothing resolves them from settings or the
	// environment — the app a request names is a fact about the product, not
	// an operator's preference.
	DefaultSiteURL        = provider.AppURL
	DefaultSiteName       = provider.AppName
	DefaultSiteCategories = provider.AppCategories
	// Preserve the names master exposed before the chat-v2 rollout, for any
	// caller still reaching them by the old spelling.
	OpenRouterAppURL  = DefaultSiteURL
	OpenRouterAppName = DefaultSiteName

	// DefaultDocumentEngine walks the deliberate local -> free -> rail-gated
	// OCR ladder. The other accepted values pin one rung and never fall through.
	DefaultDocumentEngine = "auto"

	DefaultTemperature = 0.2

	// DefaultMaxTokens has to cover reasoning tokens, not just the visible
	// answer. This model routinely spends more of its budget thinking than
	// writing, and a cap that only fits the answer produces an empty reply
	// rather than a short one. 16k was still not enough once the executor ran
	// with reasoning restored: a hard turn thinks past it, gets truncated, and
	// returns an empty message with no tool calls. The cap is a ceiling, not a
	// spend — room costs nothing on the turns that do not use it. Load takes
	// the live value from ctxbudget.CompletionReserve (AFORGE_COMPLETION_RESERVE,
	// default 65536); this constant remains the floor no configuration may
	// sink below.
	DefaultMaxTokens = 32768

	// DefaultTimeout is generous because a reasoning pass can run for minutes on
	// a wide task even when the answer is short.
	DefaultTimeout = 300 * time.Second

	// DefaultReasoning is off — for planning calls only. Planning is structuring
	// work, not thinking work: with reasoning on, an outline that is 150 tokens
	// of answer costs 2,400 tokens of deliberation and twenty seconds of wall
	// clock, for an outline of the same shape. Set AFORGE_REASONING=low|medium|
	// high to buy it back for a plan that genuinely needs it.
	DefaultReasoning = provider.EffortOff

	// DefaultExecReasoning is off, and unlike the planning default this one was
	// settled by a controlled experiment rather than a latency argument. The
	// same real coding task, same model, same limits: reflex mode finished in
	// 6 minutes with the suite green both with and without a contract, while
	// reasoning mode was 3x slower, 4x dearer, and finished worse or not at
	// all. An earlier run had blamed reasoning-off for a 139-turn zero-file
	// disaster; the ablation proved the harness was at fault — memory decay
	// was erasing the agent's only state, and once observations survive, the
	// loop does not need a thinking pass to converge. Set
	// AFORGE_EXEC_REASONING=low|medium|high to buy deliberation back for a
	// task class that turns out to need it.
	DefaultExecReasoning = provider.EffortOff

	// DefaultSpineSamples draws the spine more than once. It is the only call
	// whose framing every later pass inherits, so an unlucky draw does not
	// degrade the graph slightly — it replaces it. The samples run at the same
	// time, so this costs no wall clock and a fraction of a cent.
	DefaultSpineSamples = 3

	// DefaultMaxDepth bounds recursion. Depth is the only cost of decomposition
	// that is genuinely serial — a whole level expands in four call-rounds
	// however wide it is — so this is the guard that actually protects latency.
	DefaultMaxDepth = 2

	// DefaultNodeBudget is the ceiling the model cannot argue with. Every other
	// stop condition is pressure applied through a prompt; this one is
	// arithmetic, and it is what guarantees the recursion terminates.
	DefaultNodeBudget = 60

	// DefaultDailyBudgetUSD is the policy rail across every task using the
	// resident store. Token slices shape leaves internally; dollars decide when
	// new work needs the user's word. Zero disables the rail.
	DefaultDailyBudgetUSD = 20.0

	// preferredImageModel is the drawing default the operator asked for, with
	// the previous leader kept underneath for a catalog that does not
	// advertise it.
	preferredImageModel = "bytedance-seed/seedream-5-0-pro"
	fallbackImageModel  = "krea/krea-2-medium-turbo"

	// preferredSpeechModel is the voice the speech slot reaches for first. A
	// constant called "preferred" must be the one actually preferred, so this
	// name moves whenever the choice does rather than the lists reordering
	// around a stale one. priorSpeechModel is the Fish Audio voice it
	// replaced, kept as the next rung so a catalog that only advertises the
	// older row still speaks in the house voice.
	preferredSpeechModel = "fish-audio/s2.1-pro"
	priorSpeechModel     = "fish-audio/s1"

	// kokoroSpeechModel and hostedSpeechModel are the pair the speech slot ran
	// on before Fish Audio led it, kept underneath for a catalog that
	// advertises neither Fish row. They deliberately rank differently in the
	// two lists that read them: [Config.ResolveSpeechModel] keeps Kokoro ahead
	// because it is the cheap voice that is always there, while
	// [bestMediaPreferences] keeps the hosted OpenAI voice ahead because the
	// word "best" has asked to be spent on.
	kokoroSpeechModel = "hexgrad/kokoro-82m"
	hostedSpeechModel = "openai/gpt-4o-mini-tts"

	// preferredMusicModel and fallbackMusicModel are the two Lyria 3 rows. The
	// CLIP row leads — background music is the thing actually asked of this
	// slot, and the clip length is the operator's chosen default — with the
	// pro row underneath for a catalog that only advertises the longer one.
	preferredMusicModel = "google/lyria-3-clip-preview"
	fallbackMusicModel  = "google/lyria-3-pro-preview"

	// preferredVideoModel and fallbackVideoModel are the two Seedance rows,
	// the newer generation first and the one it replaced underneath it.
	preferredVideoModel = "bytedance/seedance-2.5"
	fallbackVideoModel  = "bytedance/seedance-2.0-mini"

	// preferredVisionModel is the remembered pair of eyes, and it earns its
	// keep by being BORING: a stable, paid, multi-endpoint row. Its previous
	// value (qwen/qwen3.5-vl-32b-instruct) quietly left the catalog, the
	// preference missed, and the election fell to the newest row in catalog
	// order — an -exp model whose only endpoints the account's data policy
	// refused, so every look failed with a 404 about privacy settings. The
	// fallback list in models.go keeps a second name under this one.
	preferredVisionModel = "google/gemini-3.7-flash"
	// fallbackVisionModel is the second pair of eyes, from a different vendor
	// on different endpoints, for a catalog where Google's row is missing.
	fallbackVisionModel = "qwen/qwen3.8-27b"

	// preferredPerceptionModel is the remembered name for the two slots that
	// hand a chat model a sound or a film and read back a sentence about it. It
	// is one name for both because it is one fact about one model: Gemini Flash
	// is the widely advertised row that takes audio and video INPUT and still
	// answers in words, which is exactly the shape both slots ask for.
	preferredPerceptionModel = "google/gemini-2.5-flash"

	// DefaultPracticeBudgetUSD is the daily carve-out reserved for self-origin
	// curiosity work. The global rail remains an additional ceiling.
	DefaultPracticeBudgetUSD = 2.0

	DefaultPracticeIdle = 20 * time.Minute

	// DefaultBriefAfter keeps ordinary short breaks silent. A longer absence
	// earns one folded arrival summary when background life actually happened.
	DefaultBriefAfter = 4 * time.Hour

	// DefaultSwarm is whether cooperative decomposition is armed with nobody
	// having said anything about it. It is TRUE from the swarm-road wave on;
	// [Config.Swarm] carries the whole argument, and `AFORGE_SWARM=0` is the
	// escape hatch.
	DefaultSwarm = true
)

// Config is the resolved runtime configuration.
type Config struct {
	APIKey  string
	BaseURL string
	Model   string
	// PlanModel is the model that structures work — the task graph, replans,
	// contracts, the delivery gate. Empty means the work model plans too, which
	// is the default and the kill switch: with no plan slot configured exactly
	// one client exists and nothing about the single-model path changes.
	PlanModel  string
	VoiceModel string
	// Media model fields are capability slots. Empty means resolve at use
	// from the live catalog, rather than trusting a floating default slug.
	ImageModel        string
	SpeechModel       string
	MusicModel        string
	VideoModel        string
	VisionModel       string
	DocumentEngine    string
	Temperature       float64
	MaxTokens         int
	Timeout           time.Duration
	Reasoning         provider.Effort
	ExecReasoning     provider.Effort
	SpineSamples      int
	MaxDepth          int
	NodeBudget        int
	DailyBudgetUSD    float64
	PracticeBudgetUSD float64
	// PlanConsentUSD is the estimate above which a planned job asks before it
	// starts. Zero never asks.
	PlanConsentUSD float64
	PracticeIdle   time.Duration
	BriefAfter     time.Duration

	// Attribution admits the standing attribution law into a worker's contract:
	// the trailer on commits it authors, the footer on pull requests and issues
	// it opens. Off is the law's absence, not an instruction to hide.
	Attribution bool

	// Swarm is the cooperative-decomposition mode, and it is ON by default
	// ([DefaultSwarm]). On, a worker gains a verb for handing work back when
	// its brief turns out to hold more than one worker's share: the resident's
	// leaves get `request_split` and may end their run early by naming two or
	// more ownable parts plus the evidence that revealed them, and a v3 task's
	// worker gets `divide_work`, which splits the task into parts under it and
	// keeps the coordination (internal/session's task_divide.go). On also feeds
	// measured capacity statistics (overrun base rates from the journal) into
	// the sizing and split judgments.
	//
	// WHY ON IS THE DEFAULT NOW. It shipped off because nobody had measured
	// what a division costs when it does not pay. The bench corpus then
	// measured it (bench/swarm/AB-REPORT.md), and the answer was that the
	// expense is not division — it is division of work that was never wide. So
	// the wave that landed the mode also landed the gate that refuses narrow
	// work (internal/splitgate), and with the gate in front of it the mode
	// costs nothing below the width floor and is worth 1.15×–1.95× wall clock
	// above it. A capability that is free when it does not apply belongs on.
	//
	// OFF IS STILL EXACTLY TODAY'S BEHAVIOUR, and `AFORGE_SWARM=0` is how
	// somebody asks for it: workers run their brief to settlement and growth is
	// failure-driven only (overrun, revision, JIT). Everything gated by Swarm
	// is inert when it is off.
	Swarm bool

	// Quorum is the two-verifier gate: when a deliverable passes the judge,
	// two cheap validators independently verify it against the original ask.
	// Both must ACCEPT; any REJECT buys one revision round, then the result
	// commits unconditionally. Off (the default) is the judge's pass as the
	// final word — byte-identical to before this existed.
	Quorum bool

	// Panel is the set of models a run may route across, from AFORGE_MODELS. An
	// empty panel is the default and is the kill switch: with no panel the
	// harness builds the same single adapter it always did and no routing code
	// runs at all.
	Panel router.Panel

	// ProfileDir holds measured executor behaviour. Empty means ~/.aforge.
	ProfileDir string

	// Models is the model catalog every adapter built from this config consults
	// before it shapes a request — today, to decide whether a reasoning knob may
	// travel at all. Nil is honest and safe: the adapter then knows nothing
	// about any model and sends only what the operator asked for explicitly,
	// which is what every caller did before the catalog was wired in.
	//
	// It is set by the surfaces that load a catalog anyway (chat, run, doctor)
	// rather than loaded here, because a config that fetched would make building
	// a client a network operation.
	Models *catalog.Catalog
}

// ErrNoAPIKey is what [Load] returns when nothing — the two variables, the
// profile file — holds a key. It is a value rather than a sentence so that a
// door can tell "no key" from "config broken": the first is a person who has
// not set up yet, and the interactive chat opens anyway and asks them
// ([LoadKeyless]); the second stops the launch, whoever is watching.
var ErrNoAPIKey = errors.New(APIKeyEnv + " (or OPENAI_API_KEY) is required")

// Load resolves configuration from the environment, falling back to the
// defaults above. Only the API key has no default; everything else runs
// unconfigured.
func Load() (Config, error) { return load(true) }

// LoadKeyless is [Load] for a launch that can collect the key itself: the
// interactive chat, whose first-run setup asks for one (internal/tui3's
// firstrun.go). Everything else resolves exactly as Load resolves it, and APIKey
// is simply empty until the person hands one over. A door that has nobody to
// ask — --once, an engine, a pipe — has no business calling this.
func LoadKeyless() (Config, error) { return load(false) }

func load(requireKey bool) (Config, error) {
	config := Config{
		APIKey:            APIKeyAt(os.Getenv("AFORGE_PROFILE_DIR")),
		BaseURL:           firstNonEmpty(os.Getenv("AFORGE_BASE_URL"), DefaultBaseURL),
		Model:             firstNonEmpty(os.Getenv("AFORGE_MODEL"), DefaultModel),
		PlanModel:         strings.TrimSpace(os.Getenv("AFORGE_PLAN_MODEL")),
		Temperature:       DefaultTemperature,
		MaxTokens:         DefaultMaxTokens,
		Timeout:           DefaultTimeout,
		Reasoning:         DefaultReasoning,
		ExecReasoning:     DefaultExecReasoning,
		SpineSamples:      DefaultSpineSamples,
		MaxDepth:          DefaultMaxDepth,
		NodeBudget:        DefaultNodeBudget,
		DailyBudgetUSD:    DefaultDailyBudgetUSD,
		PracticeBudgetUSD: DefaultPracticeBudgetUSD,
		PracticeIdle:      DefaultPracticeIdle,
		BriefAfter:        DefaultBriefAfter,
		Swarm:             DefaultSwarm,
		ProfileDir:        os.Getenv("AFORGE_PROFILE_DIR"),
	}
	if config.APIKey == "" && requireKey {
		return Config{}, ErrNoAPIKey
	}
	// Every user-tunable knob below resolves through the settings registry's
	// one order — environment, then the profile's config.json, then the
	// default — so a value changed in the settings sheet is read back here on
	// the next launch without a second lookup path.
	engine, err := DocumentEngineAt(config.ProfileDir)
	if err != nil {
		return Config{}, err
	}
	config.DocumentEngine = engine
	// What earlier runs learned about how models answer, back into the adapter
	// before it shapes its first request. Nothing here fails: a profile with no
	// memo costs one rejected call per quirk, which is how the memo was written
	// in the first place.
	provider.LoadQuirks(config.ProfileDir)
	// And the model-call log, beside the memo, because the two are the same
	// kind of thing: what this process knows about how its calls actually went
	// (internal/calllog). It opens no file here — the first call does — so a
	// process that never talks to a model leaves nothing behind.
	calllog.Open(config.ProfileDir)
	config.VisionModel = VisionModelAt(config.ProfileDir)
	// THE FIVE CAPABILITY SLOTS ARE ONE KNOB EACH, and this is the older
	// surfaces' end of it (docs/MULTIMODAL.md Decision 5). They used to read
	// their environment variable and nothing else, so a model chosen in the
	// settings sheet was a row nobody read; now both surfaces resolve the same
	// key through [MediaSlotModelAt], environment first, in the one order every
	// persisted knob above resolves in.
	config.ImageModel = MediaSlotModelAt(config.ProfileDir, "image")
	config.SpeechModel = MediaSlotModelAt(config.ProfileDir, "speech")
	config.MusicModel = MediaSlotModelAt(config.ProfileDir, "music")
	config.VideoModel = MediaSlotModelAt(config.ProfileDir, "video")
	// Voice is the one with a built-in name behind it: transcription runs on a
	// dedicated endpoint and there is no catalog ladder that reaches it, so an
	// unset slot keeps the model this build knows works.
	config.VoiceModel = firstNonEmpty(MediaSlotModelAt(config.ProfileDir, "voice"), DefaultVoiceModel)
	config.Attribution = AttributionAt(config.ProfileDir)
	// The context law's knobs, handed to the one package that spends them.
	// The reserve also floors the wire ceiling: a reasoning pass that thinks
	// past a small MaxTokens returns an empty reply, so the ceiling is never
	// allowed below the room the law promised.
	ctxbudget.Configure(contextLaw(config.ProfileDir))
	config.MaxTokens = max(config.MaxTokens, ctxbudget.CompletionReserve())
	if config.PracticeIdle, err = PracticeIdleAt(config.ProfileDir); err != nil {
		return Config{}, err
	}
	if config.BriefAfter, err = BriefAfterAt(config.ProfileDir); err != nil {
		return Config{}, err
	}
	if config.PracticeBudgetUSD, err = PracticeBudgetUSDAt(config.ProfileDir); err != nil {
		return Config{}, err
	}
	if config.DailyBudgetUSD, err = DailyBudgetUSDAt(config.ProfileDir); err != nil {
		return Config{}, err
	}
	if config.PlanConsentUSD, err = PlanConsentUSDAt(config.ProfileDir); err != nil {
		return Config{}, err
	}
	if raw := strings.TrimSpace(os.Getenv("AFORGE_REASONING")); raw != "" {
		effort, ok := provider.ParseEffort(raw)
		if !ok {
			return Config{}, fmt.Errorf("AFORGE_REASONING: unknown effort %q (off, low, medium, high)", raw)
		}
		config.Reasoning = effort
	}
	if raw := strings.TrimSpace(os.Getenv("AFORGE_EXEC_REASONING")); raw != "" {
		effort, ok := provider.ParseEffort(raw)
		if !ok {
			return Config{}, fmt.Errorf("AFORGE_EXEC_REASONING: unknown effort %q (off, low, medium, high)", raw)
		}
		config.ExecReasoning = effort
	}
	for _, knob := range []struct {
		name   string
		target *int
	}{
		{"AFORGE_SPINE_SAMPLES", &config.SpineSamples},
		{"AFORGE_MAX_DEPTH", &config.MaxDepth},
		{"AFORGE_NODE_BUDGET", &config.NodeBudget},
	} {
		raw := strings.TrimSpace(os.Getenv(knob.name))
		if raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			return Config{}, fmt.Errorf("%s: want a non-negative integer, got %q", knob.name, raw)
		}
		*knob.target = value
	}
	if raw := strings.TrimSpace(os.Getenv("AFORGE_MECHANISM")); raw == "quorum" {
		config.Quorum = true
	}
	// THE SENSE OF THIS SWITCH TURNED OVER. It was the arming switch for a wave
	// that was off by default; now the wave is the default and this is the way
	// out of it — `AFORGE_SWARM=0`. The reading is unchanged, because
	// [strconv.ParseBool] already answered both directions; what changed is
	// which direction anybody has a reason to write.
	if raw := strings.TrimSpace(os.Getenv("AFORGE_SWARM")); raw != "" {
		swarm, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("AFORGE_SWARM: want 0 or 1, got %q", raw)
		}
		config.Swarm = swarm
	}
	panel, err := router.LoadPanel(os.Getenv("AFORGE_MODELS"))
	if err != nil {
		return Config{}, err
	}
	config.Panel = panel
	return config, nil
}

// ResolveImageModel applies the runtime preference order: an explicit slot,
// Seedream 5.0 Pro then Krea 2 Medium Turbo as each is advertised, then the
// catalog's first image output model. Catalog's offline fallbacks make the
// last resort usable while keeping the ordinary path free of unverified
// defaults.
func (c Config) ResolveImageModel(models *catalog.Catalog) string {
	if configured := strings.TrimSpace(c.ImageModel); configured != "" {
		return configured
	}
	return resolveOutputModel(models, "image", preferredImageModel, fallbackImageModel)
}

// ResolveSpeechModel prefers Fish Audio S2.1 Pro, then Fish Audio S1, then
// Kokoro, then OpenAI mini TTS as each is advertised, then the first
// speech-output model. The person's own saved row wins over all of it: a
// default is only a default.
func (c Config) ResolveSpeechModel(models *catalog.Catalog) string {
	if configured := strings.TrimSpace(c.SpeechModel); configured != "" {
		return configured
	}
	return resolveOutputModel(models, "speech", preferredSpeechModel, priorSpeechModel, kokoroSpeechModel, hostedSpeechModel)
}

// ResolveMusicModel prefers the Lyria 3 clip row, then Lyria 3 Pro, then the
// first music/audio model that is not recognizably a TTS model. Unlike speech
// and image, a verified Lyria endpoint is also the final built-in fallback when
// discovery has no music row at all.
func (c Config) ResolveMusicModel(models *catalog.Catalog) string {
	if configured := strings.TrimSpace(c.MusicModel); configured != "" {
		return configured
	}
	candidates := ModelCandidates(models, "music")
	for _, preferred := range []string{preferredMusicModel, fallbackMusicModel} {
		for _, candidate := range candidates {
			if candidate.ID == preferred {
				return candidate.ID
			}
		}
	}
	if len(candidates) > 0 {
		return candidates[0].ID
	}
	return preferredMusicModel
}

// ModelCandidates is the shared capability gate for every slot in the model
// palette. Keeping music's TTS exclusion here makes discovery and runtime
// resolution agree about what can occupy that slot.
func ModelCandidates(models *catalog.Catalog, slot string) []catalog.Model {
	if models == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(slot)) {
	case "talk", "work", "boost":
		candidates := models.ModelsWithInput("text")
		filtered := make([]catalog.Model, 0, len(candidates))
		for _, candidate := range candidates {
			if models.Supports(candidate.ID, "output", "text") {
				filtered = append(filtered, candidate)
			}
		}
		return filtered
	case "voice", "transcribe":
		// THE VOCABULARY FIX, and it is ADDITIVE because the old reading was not
		// wrong so much as alone. This asked only for models publishing an
		// OUTPUT modality called "transcription" — a real vocabulary that some
		// providers use and that the catalog's own tests pin — and no row on
		// OpenRouter has ever carried it, so on the catalog this product
		// actually reads the voice slot's candidate list was EMPTY, everywhere
		// it was asked, silently.
		//
		// An ear is SOUND IN AND WORDS BACK, which is what those rows do
		// publish, so that reading is added beside the first rather than in
		// place of it: a provider that says "transcription" is still taken at
		// its word, and one that describes the same model in modalities is
		// finally findable. internal/tui3's hearsSpeech reads the same slot the
		// same way, so the picker and this list cannot disagree.
		found := models.ModelsWithOutput("transcription")
		seen := make(map[string]bool, len(found))
		for _, candidate := range found {
			seen[candidate.ID] = true
		}
		for _, candidate := range models.ModelsWithInput("audio") {
			if seen[candidate.ID] {
				continue
			}
			if models.Supports(candidate.ID, "output", "text") {
				found = append(found, candidate)
			}
		}
		return found
	case "vision":
		// The inspection proxy: aforge hands it a picture and reads back a
		// sentence about one, so image input ALONE is half the question. A
		// drawing model that reads pictures and answers in pictures passes the
		// input half and cannot answer "what is in this photo".
		return chatCandidatesWithInput(models, "image")
	case "listen":
		// A chat model that can be handed a sound file, for the perception belt
		// rather than the transcription endpoint.
		return chatCandidatesWithInput(models, "audio")
	case "watch":
		return chatCandidatesWithInput(models, "video")
	case "image":
		return models.ModelsWithOutput("image")
	case "speech":
		return models.ModelsWithOutput("speech")
	case "music":
		candidates := models.ModelsWithOutput("music")
		filtered := make([]catalog.Model, 0, len(candidates))
		for _, candidate := range candidates {
			if !recognizableTTS(candidate) {
				filtered = append(filtered, candidate)
			}
		}
		return filtered
	case "video":
		return models.ModelsWithOutput("video")
	default:
		return nil
	}
}

// ResolveVideoModel prefers Seedance 2.5, then Seedance 2.0 Mini as each is
// advertised, then the catalog's first exact video-output model.
func (c Config) ResolveVideoModel(models *catalog.Catalog) string {
	if configured := strings.TrimSpace(c.VideoModel); configured != "" {
		return configured
	}
	return resolveOutputModel(models, "video", preferredVideoModel, fallbackVideoModel)
}

// ResolveVisionModel applies the inspection-proxy order at the moment a leaf
// starts: an explicit environment slot, the live talk and work choices when
// each advertises image input, Qwen VL when advertised, then the first model
// with image input. Unlike generation slots, an unadvertised default is never
// invented: no result means view_image can preserve its calm refusal.
func (c Config) ResolveVisionModel(models *catalog.Catalog, talkModel, workModel string) string {
	if configured := strings.TrimSpace(c.VisionModel); configured != "" {
		return configured
	}
	if models == nil {
		return ""
	}
	for _, candidate := range []string{talkModel, workModel, preferredVisionModel} {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" && models.Supports(candidate, "input", "image") {
			return candidate
		}
	}
	candidates := models.ModelsWithInput("image")
	if len(candidates) > 0 {
		return candidates[0].ID
	}
	return ""
}

// chatCandidatesWithInput is the three PERCEPTION slots' shared question: a
// model that takes this kind of material AND can still hold a conversation
// about it. The second half is the whole point — a model that takes sound and
// answers in sound is a speaker, not a listener, and one that takes pictures and
// answers in pictures is a painter, not a pair of eyes.
func chatCandidatesWithInput(models *catalog.Catalog, modality string) []catalog.Model {
	candidates := models.ModelsWithInput(modality)
	filtered := make([]catalog.Model, 0, len(candidates))
	for _, candidate := range candidates {
		if models.Supports(candidate.ID, "output", "text") && !generatesMedia(candidate) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

// generatesMedia says a row answers in something besides text. It is read
// alongside a published "text" rather than instead of it, so a row that
// captions what it draws — ["image","text"] out — is excluded from every
// perception slot, which is the same reading internal/tui3's chat law applies.
func generatesMedia(model catalog.Model) bool {
	for _, output := range model.OutputModalities {
		if !strings.EqualFold(strings.TrimSpace(output), "text") {
			return true
		}
	}
	return false
}

func recognizableTTS(model catalog.Model) bool {
	for _, output := range model.OutputModalities {
		if strings.EqualFold(strings.TrimSpace(output), "speech") {
			return true
		}
	}
	identity := strings.ToLower(model.ID + " " + model.Name)
	for _, marker := range []string{"tts", "text-to-speech", "speech synthesis", "kokoro"} {
		if strings.Contains(identity, marker) {
			return true
		}
	}
	return false
}

func resolveOutputModel(models *catalog.Catalog, modality string, preferences ...string) string {
	if models == nil {
		return ""
	}
	candidates := models.ModelsWithOutput(modality)
	for _, preferred := range preferences {
		for _, candidate := range candidates {
			if candidate.ID == preferred {
				return candidate.ID
			}
		}
	}
	if len(candidates) > 0 {
		return candidates[0].ID
	}
	return ""
}

func validateDailyBudgetValue(raw, source string) (float64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("%s: want a non-negative dollar amount, got %q", source, raw)
	}
	return validateDailyBudget(value, source)
}

// DailyBudgetUSD resolves the dollar rail without requiring a provider key.
// Status-only commands use it even when they never construct a model client.
func DailyBudgetUSD() (float64, error) {
	return DailyBudgetUSDAt(os.Getenv("AFORGE_PROFILE_DIR"))
}

// Context stamps the run's provider knobs onto ctx: the effort the operator
// chose, and a cache key derived from the task so every call in one run asks
// for the same warm instance.
func (c Config) Context(ctx context.Context, task string) context.Context {
	ctx = provider.WithCacheKey(ctx, provider.RunCacheKey(task, c.Model))
	// Configured rather than inferred: the operator asked for this level, so it
	// travels even to a model the catalog cannot vouch for.
	return provider.WithConfiguredReasoningEffort(ctx, c.Reasoning)
}

// ExecContext layers the executor's reasoning level over a planning context.
// Planning and execution are different kinds of call — one structures, the
// other works — so the economy that makes planning fast must not travel into
// the loop, where a model with reasoning suppressed stops writing anything
// down and never converges.
func (c Config) ExecContext(ctx context.Context) context.Context {
	return provider.WithConfiguredReasoningEffort(ctx, c.ExecReasoning)
}

// Client builds what the planner and the executor call.
//
// With no panel configured this is the single adapter it has always been, built
// exactly as before — that is the kill switch, and it is the default. With a
// panel it is a router over one adapter per model, which satisfies the same
// interface, so nothing above this line changes.
func (c Config) Client() (router.Client, error) {
	if len(c.Panel.Models) == 0 {
		return provider.NewClient(c.providerConfig(c.Model))
	}
	return router.New(c.Panel, c.providerConfig(c.Model), c.ProfileDir)
}

// PlanModelResolved is the model planning-class calls run on: the plan slot
// when the operator set one, otherwise the work model.
func (c Config) PlanModelResolved() string {
	return firstNonEmpty(c.PlanModel, c.Model)
}

// PlanSplit reports whether planning runs on a different model than the work.
func (c Config) PlanSplit() bool {
	resolved := c.PlanModelResolved()
	return resolved != "" && resolved != c.Model
}

// ClientFor builds a client for an explicitly chosen model. With no panel it is
// exactly the single-model adapter it always was. With a panel the choice is
// pinned as the router's opener, so chat keeps its runtime picker without
// bypassing observation and escalation.
func (c Config) ClientFor(model string) (router.Client, error) {
	if len(c.Panel.Models) > 0 {
		return router.NewPinned(c.Panel, c.providerConfig(model), c.ProfileDir, model)
	}
	return provider.NewClient(c.providerConfig(model))
}

// MediaClient builds the non-chat OpenRouter endpoint client with the same
// bearer key, base URL, attribution, timeout, and transport configuration.
func (c Config) MediaClient() (*provider.MediaClient, error) {
	return provider.NewMediaClient(c.providerConfig(c.Model))
}

// VisionClient is deliberately direct rather than panel-routed. view_image
// resolves one capability-qualified model and overrides this client's default
// per call; routing it again could substitute a text-only model and would make
// the proxy attribution dishonest.
func (c Config) VisionClient() (*provider.Client, error) {
	return provider.NewClient(c.providerConfig(c.Model))
}

// DocumentClient is direct for the same reason as VisionClient: read_document
// selects an explicit parser engine and model at the leaf boundary, and a
// second router substitution would make both capability and cost opaque.
func (c Config) DocumentClient() (*provider.Client, error) {
	return provider.NewClient(c.providerConfig(c.Model))
}

func (c Config) providerConfig(model string) provider.Config {
	return provider.Config{
		APIKey:      c.APIKey,
		BaseURL:     c.BaseURL,
		Model:       model,
		Temperature: c.Temperature,
		MaxTokens:   c.MaxTokens,
		Timeout:     c.Timeout,
		// The published answer to "does this model take this field", from rows
		// already in memory. A nil catalog and a catalog still warming both say
		// "unknown", which the adapter treats as "send nothing on your own
		// initiative" — never as permission.
		SupportsParameter: c.Models.SupportsParameter,
		// And what the row says about the model's thinking pass, under the
		// same contract, translated into the adapter's words at this seam.
		ReasoningProfile: ReasoningProfileSeam(c.Models),
		// And the model's own list price, which is what the adapter bounds a
		// latency-sorted request against. Same contract: never blocks, and
		// "nobody published one" sends no ceiling at all.
		ModelPrice: c.Models.PriceNow,
	}
}

// ReasoningProfileSeam hands the catalog's published reasoning profile to the
// adapter in the adapter's own vocabulary. A nil catalog answers "unknown",
// exactly as its SupportsParameter does.
func ReasoningProfileSeam(models *catalog.Catalog) func(string) (provider.ReasoningProfile, bool) {
	if models == nil {
		return nil
	}
	return func(model string) (provider.ReasoningProfile, bool) {
		row, known := models.ReasoningProfile(model)
		if !known {
			return provider.ReasoningProfile{}, false
		}
		profile := provider.ReasoningProfile{Mandatory: row.Mandatory, Default: provider.Effort(row.DefaultEffort)}
		for _, word := range row.Efforts {
			profile.Efforts = append(profile.Efforts, provider.Effort(word))
		}
		return profile, true
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// Package config holds the harness's process-wide defaults. It is the only
// place a model slug or an endpoint is written down, so changing the default
// model is a one-line edit rather than a search across the tree.
package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/calllog"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/ctxbudget"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/router"
)

const (
	// DefaultModel is the harness's default. DeepSeek V4 Flash Latest is the
	// floating alias, so a new dated snapshot is picked up without a code change.
	// 1M context, ~$0.09/M in and ~$0.18/M out, and it advertises both
	// structured_outputs and reasoning on OpenRouter.
	//
	// A first-prompt stall on this default names `/model` rather than hanging
	// silent (F42). The slug itself is not swapped for a more expensive one:
	// the cheap default stays, and the stall is what must recover visibly.
	DefaultModel = "~deepseek/deepseek-v4-flash-latest"

	// DefaultVoiceModel is the independent speech-to-text slot. Voice never
	// enters the talk/work router: OpenRouter exposes it through the dedicated
	// audio transcription endpoint.
	DefaultVoiceModel = "qwen/qwen3-asr-flash-2026-02-10"

	// DefaultBaseURL is OpenRouter's OpenAI-compatible endpoint.
	DefaultBaseURL = catalog.DefaultBaseURL

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

	// DefaultTimeout is generous because a reasoning pass can run for minutes on
	// a wide task even when the answer is short.
	DefaultTimeout = 300 * time.Second

	// DefaultReasoning IS ABSENCE, and so is [DefaultExecReasoning]. Nothing
	// about how hard a model thinks travels on a request this harness was not
	// told to shape: [provider.EffortNone] sends no `reasoning` object at all
	// and the model answers at its own published default.
	//
	// It used to be [provider.EffortOff], which is not silence but a REQUEST —
	// `{"reasoning":{"enabled":false}}` — chosen here on a latency argument
	// about planning and on an executor ablation about convergence. Both were
	// measurements of two models on two task shapes, and neither is a fact
	// about the model an operator points this binary at today: the same field
	// is a 400 on an endpoint that cannot switch thinking off, and a silent
	// downgrade on one that can. A harness that has not been asked for a level
	// asks for none.
	//
	// CODEAF_REASONING and CODEAF_EXEC_REASONING are unchanged and still take
	// off|low|medium|high — including `off`, which is how somebody who wants
	// the thinking pass actually suppressed says so and gets exactly the
	// request this default used to make on their behalf.
	DefaultReasoning = provider.EffortNone

	// DefaultExecReasoning is absence for the reason above: planning and
	// execution are different calls, and neither of them is a call this harness
	// has an opinion about the depth of.
	DefaultExecReasoning = provider.EffortNone

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
	//
	// IT IS DELIBERATELY LARGE — a backstop against a runaway, never a budget.
	// It sat at $20 and a single ordinary day of agent work reached it, so the
	// rail stopped being the thing that catches a loop and became the thing
	// that interrupts work: a person who had chosen no number at all was being
	// asked to raise a ceiling they never set. A rail nobody chose must only
	// fire where nobody would defend the spend, and $500 in one day is that
	// place. The number a person actually budgets with is the one they write
	// into the row themselves — and 0 there removes the rail entirely.
	DefaultDailyBudgetUSD = 500.0

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
	//
	// IT IS A CARVE-OUT AND NOT A RAIL, which is why 0 reads the opposite way
	// here than it does on every other money row in this file: 0 is no practice
	// at all (internal/resident's WithPracticeLoop switches the loop off), never
	// unbounded practice. There is no way to spell "practice without a ceiling"
	// and that is deliberate — self-origin work runs while nobody is watching,
	// so it is the one pocket that always has a bottom. Raised with the rail
	// above so the carve-out is a real slice of a real day.
	DefaultPracticeBudgetUSD = 50.0

	DefaultPracticeIdle = 20 * time.Minute

	// DefaultBriefAfter keeps ordinary short breaks silent. A longer absence
	// earns one folded arrival summary when background life actually happened.
	DefaultBriefAfter = 4 * time.Hour

	// DefaultSwarm is whether cooperative decomposition is armed with nobody
	// having said anything about it. It is TRUE from the swarm-road wave on;
	// [Config.Swarm] carries the whole argument, and `CODEAF_SWARM=0` is the
	// escape hatch.
	DefaultSwarm = true
)

// Config is the resolved runtime configuration.
//
// IT CARRIES NO GENERATION KNOB IT WAS NOT GIVEN. There is no output cap here
// and no sampling field: a request this config builds names the model, the
// messages and what the call needs to work, and every generation parameter the
// operator did not ask for is ABSENT from the body, so the provider's own
// default answers. The one field that looks like an exception is not one —
// [Config.Reasoning] and [Config.ExecReasoning] default to
// [provider.EffortNone], which sends nothing, and carry a level only when
// CODEAF_REASONING or CODEAF_EXEC_REASONING said so.
//
// AN OPERATOR OR EMBEDDER MAY STILL SIZE A CALL, with ai.WithMaxTokens on that
// one request — the seam is open and the adapter honours it (internal/provider).
// What is gone is this file deciding a ceiling for every call in the process,
// and, with it, every errand in this tree that used to reach for that option on
// the harness's behalf.
type Config struct {
	APIKey  string
	BaseURL string
	// UnreadProfileKeys is the sorted result of the profile reader registry check.
	UnreadProfileKeys []string
	// Sources is the resolved service set. Empty preserves every scalar
	// construction that predates services through [modelsource.Set.OrDefault].
	Sources modelsource.Set
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
	// OFF IS STILL EXACTLY TODAY'S BEHAVIOUR, and `CODEAF_SWARM=0` is how
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

	// Panel is the set of models a run may route across, from CODEAF_MODELS. An
	// empty panel is the default and is the kill switch: with no panel the
	// harness builds the same single adapter it always did and no routing code
	// runs at all.
	Panel router.Panel

	// ProfileDir holds measured executor behaviour. Empty means ~/.codeaf.
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

// ProfileDirEnv is the variable that moves the whole profile — the key, the
// settings file, the measured behaviour — somewhere else. It is what an
// isolated run sets, and it is spelled here once so every reader of the profile
// asks the same question.
const ProfileDirEnv = "CODEAF_PROFILE_DIR"

// ProfileDir is the profile this process reads and writes. Empty is the ordinary
// answer and means the state root's own profile; the readers below take it as
// such, so a caller never has to know what the default expands to.
func ProfileDir() string { return env.Get(ProfileDirEnv) }

// Load resolves configuration from the environment, falling back to the
// defaults above. Only the API key has no default; everything else runs
// unconfigured.
func Load() (Config, error) { return load(true) }

// LoadKeyless is [Load] for a launch that can collect the key itself: the
// interactive chat, whose provider screen connects OpenRouter or takes a pasted
// key (internal/tui3's firstrun.go). Everything else resolves exactly as Load
// resolves it, and APIKey is simply empty until the person hands one over.
//
// AND FOR A PASS THAT WILL PROBABLY DO NOTHING, which is the second legitimate
// caller and the one nobody expects: the standing tick (cmd/codeaf's
// v3StandingTicker), run every five minutes by whichever of a window or the
// operating system's timer gets there first. Most passes decline the lock or
// find every item asleep, and a pass that will do nothing costs nothing and
// needs nothing — so it builds keyless and carries whatever key was there
// through to the one moment a judgment actually calls a model, where a machine
// with none says so on that item's own row.
//
// A door that has nobody to ask AND something to spend — --once, an engine, a
// pipe — still has no business calling this: it would fail on its first request
// instead of at the door, where the sentence can be read.
func LoadKeyless() (Config, error) { return load(false) }

// nonSettingProfileFields are the top-level config.json keys the loader and
// first run write that are not settings-registry rows. They are consumed even
// though NewSettings(...).Rows() does not list them.
var nonSettingProfileFields = []string{
	KeySetupSeen,
	// The talk lane's borrow row sits beside its lane row and is read by
	// [LaneBorrowAt], but it is set from the lane page and not from a settings
	// row of its own. Missing here, every profile the lane page had written
	// was told at launch that a key codeaf reads was unread.
	LaneBorrowKey(LaneSlotTalk),
	KeySplitPct,
	KeyStandingBackground,
	KeyResponseAttempts,
	KeyResponseLiftAfter,
	KeyResponseLiftCap,
	keyModelSources,
	// THE TALK LANE'S BORROW FLAG. It is no row of its own — it rides the
	// `provider` row's words (`pinned: cloudflare, borrow when slow`) — but
	// [WriteLaneRow] writes it on EVERY save of that row, `false` included, and
	// [LaneBorrowAt] reads it. Left off this list, every launch after the row
	// was saved told the person their profile carried an ignored key.
	LaneBorrowKey(LaneSlotTalk),
	// THE CREW'S STANDING ROWS. They are written by /crew's shortcuts and
	// panel and read by the router (crew.go), and no settings-registry row owns
	// them: the panel is where a rule, a cap and the providers turned off are
	// read beside the day's spend.
	KeyCrewAllowed,
	KeyCrewCap,
	KeyCrewTaskCap,
	// And the free-routes switch beside them, which the crew's providers list
	// turns on and off.
	KeyCrewFreeRoutes,
	KeyCrewProvidersOff,
}

// retiredProfileKeys are top-level config.json keys that a shipped version once
// wrote as a real settings-registry row but that nothing reads at head. The
// unread check skips them silently so a person on an older profile is not told
// about an ignored key they never typed. Each was confirmed by a static history
// sweep (a shipped registry row at introduction, no reader at head), dated by
// the commit that removed its last reader. A key earns a place here only on
// that evidence; TestProfileKeyLedgerLaw keeps the set honest against the
// writer ledger.
var retiredProfileKeys = map[string]bool{
	"linear_mode":          true, // reader removed by 74e16993c
	"nerd_font":            true, // reader removed by 74e16993c
	"rail_state":           true, // reader removed by 74e16993c
	"work.workers":         true, // reader removed by 05b995376
	"memory.consolidation": true, // reader removed by 10dcdfdd4
	"practice_demand_pct":  true, // reader removed by 84ba8503e
	"propose_new_skills":   true, // reader removed by 84ba8503e
	"attribution":          true, // reader removed on 2026-09-23: signing has no off
	// The retired crew rows are read once more, by the migration that removes
	// them and says so in its own line (crewmigrate.go's [MigrateCrew]); the
	// unread check must not say it a second time in a worse sentence.
	legacyKeyCrew:       true, // presets retired on 2026-09-24: seats are routed per task
	legacyKeyCrewSource: true, // family row retired on 2026-09-24: `open` became the allowed rule
	legacyKeyCrewPick:   true, // pick row retired on 2026-09-24
}

// retiredRowNotes are the retired keys a person set ON PURPOSE, each with the
// plain sentence they are told instead of the silence the rest of
// [retiredProfileKeys] gets.
//
// SILENCE IS RIGHT FOR A KEY NOBODY TYPED and wrong for one somebody did. A
// profile holding `attribution: false` holds a person's decision not to sign,
// and a build that stopped reading it without a word would be signing their
// work behind their back — while one that kept obeying it would be a setting
// the product says it no longer has. So such a key is reported unread, like a
// key the loader never knew, and the surface prints this sentence in place of
// the generic one ([RetiredRowNote]).
//
// The row's environment spelling counts as the row: `CODEAF_ATTRIBUTION` set in
// a shell is the same decision made in a different place, and it is told the
// same sentence ([retiredRowEnv]).
var retiredRowNotes = map[string]string{
	"attribution": "the attribution row and CODEAF_ATTRIBUTION are gone: codeaf always signs " +
		"the commits, pull requests, issues and comments it writes. The one part you can turn off " +
		"is the model's name in the Assisted-by line, with the " + KeyAttributionModel + " row.",
}

// retiredRowEnv is the environment spelling each told retired row had.
var retiredRowEnv = map[string]string{
	"attribution": "CODEAF_ATTRIBUTION",
}

// RetiredRowNote is the sentence a surface prints for a key the loader reported
// unread because the row it belonged to is gone, and empty for every other
// key: an unknown key still gets the surface's generic sentence.
func RetiredRowNote(key string) string { return retiredRowNotes[key] }

// consumedProfileKeys is every top-level config.json key a reader consumes at
// head: every settings-registry row plus the non-setting loader and first run
// fields. It is the one definition the unread check and the ledger law both
// read, so the two cannot drift.
func consumedProfileKeys(profileDir string) map[string]bool {
	consumed := make(map[string]bool)
	for _, key := range nonSettingProfileFields {
		consumed[key] = true
	}
	for _, row := range NewSettings(SettingsOptions{ProfileDir: profileDir}).Rows() {
		consumed[row.Key] = true
	}
	return consumed
}

var warnedProfileConfigs sync.Map

func warnUnreadProfileKeys(profileDir string, values map[string]json.RawMessage) []string {
	path := BudgetConfigPath(profileDir)
	consumed := consumedProfileKeys(profileDir)
	var unread []string
	for key := range values {
		if consumed[key] || (retiredProfileKeys[key] && retiredRowNotes[key] == "") {
			continue
		}
		unread = append(unread, key)
	}
	// A TOLD ROW SET IN THE SHELL IS THE SAME ROW. It is reported once, under
	// the row's own key, whether the profile, the variable or both said it.
	for key, name := range retiredRowEnv {
		if strings.TrimSpace(env.Get(name)) != "" && !slices.Contains(unread, key) {
			unread = append(unread, key)
		}
	}
	sort.Strings(unread)
	if len(unread) > 0 {
		if _, warned := warnedProfileConfigs.LoadOrStore(path, struct{}{}); !warned {
			log.Printf("codeaf: %s has unread top-level config key(s): %s", path, strings.Join(unread, ", "))
			for _, key := range unread {
				if note := RetiredRowNote(key); note != "" {
					log.Printf("codeaf: %s", note)
				}
			}
		}
	}
	return unread
}

func load(requireKey bool) (Config, error) {
	profileDir := ProfileDir()
	// The default key and model_sources live in the same object. Read that
	// object once here, then resolve both facts from the snapshot so adding an
	// empty model_sources field does not add a launch-path read.
	profileValues, _ := readProfileConfig(profileDir)
	unreadProfileKeys := warnUnreadProfileKeys(profileDir, profileValues)
	apiKey := apiKeyFrom(profileValues)
	baseURL := firstNonEmpty(env.Get("CODEAF_BASE_URL"), DefaultBaseURL)
	config := Config{
		APIKey:            apiKey,
		BaseURL:           baseURL,
		UnreadProfileKeys: unreadProfileKeys,
		Model:             firstNonEmpty(env.Get(ModelEnv), ChatDefaultAt(profileDir)),
		PlanModel:         strings.TrimSpace(env.Get(PlanModelEnv)),
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
		ProfileDir:        profileDir,
	}
	config.Sources = sourceHomes(resolveSources(config.APIKey, config.BaseURL, persistedSourcesFrom(profileValues), func(row PersistedSource, source modelsource.Source) string {
		return SourceKeyAt(profileDir, row, source)
	}), profileDir)
	if config.APIKey == "" && requireKey {
		hasService := false
		for _, service := range config.Sources.All() {
			if !strings.EqualFold(service.Source.ID, modelsource.DefaultID) && (strings.TrimSpace(service.Key) != "" || service.Source.KeyOptional) {
				hasService = true
				break
			}
		}
		if !hasService {
			return Config{}, ErrNoAPIKey
		}
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
	// The lane pin and guard are process-wide facts from the same profile, so
	// they are installed beside the other profile facts here. Every door loads
	// through this point, which closes the headless paths that used to send
	// their first request without the lane row the person had chosen.
	InstallLaneRows(config.ProfileDir)
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
	// The context law's knobs, handed to the one package that spends them.
	//
	// THE RESERVE IS ROOM IN THE CONTEXT WINDOW AND NEVER A FIELD ON THE WIRE.
	// It used to also floor an output cap this file put on every request; the
	// cap is gone (nothing here sends max_tokens — see the Config type), and
	// the reserve goes on doing the one job it was written for: keeping the
	// prompt small enough that the answer and its reasoning still fit.
	ctxbudget.Configure(contextLaw(config.ProfileDir))
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
	if raw := strings.TrimSpace(env.Get("CODEAF_REASONING")); raw != "" {
		effort, ok := provider.ParseEffort(raw)
		if !ok {
			return Config{}, fmt.Errorf("CODEAF_REASONING: unknown effort %q (off, low, medium, high)", raw)
		}
		config.Reasoning = effort
	}
	if raw := strings.TrimSpace(env.Get("CODEAF_EXEC_REASONING")); raw != "" {
		effort, ok := provider.ParseEffort(raw)
		if !ok {
			return Config{}, fmt.Errorf("CODEAF_EXEC_REASONING: unknown effort %q (off, low, medium, high)", raw)
		}
		config.ExecReasoning = effort
	}
	for _, knob := range []struct {
		name   string
		target *int
	}{
		{"CODEAF_SPINE_SAMPLES", &config.SpineSamples},
		{"CODEAF_MAX_DEPTH", &config.MaxDepth},
		{"CODEAF_NODE_BUDGET", &config.NodeBudget},
	} {
		raw := strings.TrimSpace(env.Value(knob.name))
		if raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			return Config{}, fmt.Errorf("%s: want a non-negative integer, got %q", knob.name, raw)
		}
		*knob.target = value
	}
	if raw := strings.TrimSpace(env.Get("CODEAF_MECHANISM")); raw == "quorum" {
		config.Quorum = true
	}
	// THE SENSE OF THIS SWITCH TURNED OVER. It was the arming switch for a wave
	// that was off by default; now the wave is the default and this is the way
	// out of it — `CODEAF_SWARM=0`. The reading is unchanged, because
	// [strconv.ParseBool] already answered both directions; what changed is
	// which direction anybody has a reason to write.
	if raw := strings.TrimSpace(env.Get("CODEAF_SWARM")); raw != "" {
		swarm, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("CODEAF_SWARM: want 0 or 1, got %q", raw)
		}
		config.Swarm = swarm
	}
	panel, err := router.LoadPanel(env.Get("CODEAF_MODELS"))
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
// first music/audio model that is not recognizably a TTS model. AN UNAVAILABLE
// CAPABILITY STAYS OFF THE BELT: with no published row it returns nothing.
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
	return ""
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
		// The inspection proxy: codeaf hands it a picture and reads back a
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
	return DailyBudgetUSDAt(ProfileDir())
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
		return c.Adapter(c.Model)
	}
	panel := c.Panel
	panel.ClientConfig = c.ClientConfig
	return router.New(panel, c.ClientConfig(c.Model), c.ProfileDir)
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
// exactly the single-model adapter it always was. With a panel the bare model
// id is pinned as the router's opener while the whole value builds every
// adapter, so a thinking level stays with the seat without becoming part of the
// provider's slug.
func (c Config) ClientFor(model string) (router.Client, error) {
	// The pin is a model id like any other and is stripped of its level for the
	// same reason ClientConfig strips one: a router pinned to a slug nobody
	// publishes never opens on the model it was pinned to.
	bare, _ := roles.SplitEffort(model)
	if len(c.Panel.Models) > 0 {
		panel := c.Panel
		panel.ClientConfig = c.ClientConfig
		return router.NewPinned(panel, c.ClientConfig(model), c.ProfileDir, bare)
	}
	return c.Adapter(model)
}

// Adapter is the ONE place a model client is built. The model id decides the
// source; the source decides the key and the address. Nothing above this line
// knows either.
func (c Config) Adapter(model string) (*provider.Client, error) {
	return provider.NewClient(c.ClientConfig(model))
}

// MediaClient builds the non-chat OpenRouter endpoint client with the same
// bearer key, base URL, attribution, timeout, and transport configuration.
func (c Config) MediaClient() (*provider.MediaClient, error) {
	return provider.NewMediaClient(c.ClientConfig(c.Model))
}

// VisionClient is deliberately direct rather than panel-routed. view_image
// resolves one capability-qualified model and overrides this client's default
// per call; routing it again could substitute a text-only model and would make
// the proxy attribution dishonest.
func (c Config) VisionClient() (*provider.Client, error) {
	return provider.NewClient(c.ClientConfig(c.Model))
}

// DocumentClient is direct for the same reason as VisionClient: read_document
// selects an explicit parser engine and model at the leaf boundary, and a
// second router substitution would make both capability and cost opaque.
func (c Config) DocumentClient() (*provider.Client, error) {
	return provider.NewClient(WithoutSeatPin(c.ClientConfig(c.Model)))
}

// ClientConfigFor assembles the provider settings for one model out of the
// services a caller already holds.
//
// IT IS THE ONE PLACE A KEY AND A BASE URL BECOME A provider.Config, and that
// is the law rather than a convenience: six sites used to compose that literal
// themselves, so a level that belongs beside the slug travelled inside it and
// a second service would have reached none of them. A caller that holds a whole
// profile wants [Config.ClientConfig]; this door is for internal/session, whose
// own Config carries the account and nothing else. THE MODEL ID NOW DECIDES THE
// ACCOUNT, and nothing above this line chooses a key or a base URL.
func ClientConfigFor(sources modelsource.Set, model string) provider.Config {
	// THE THINKING LEVEL IS NOT PART OF A MODEL ID, and this is the one place
	// that has to know it. `moonshotai/kimi-k3:low` is how a tier row, a
	// --plan-model flag and CODEAF_PLAN_MODEL all say "that model, thinking a
	// little"; the level is applied per call by the role ladder
	// (internal/session's roleRequest), and the slug that goes on the wire is
	// the model alone. Sent whole it is a slug no provider publishes, which is
	// a 404 on every planning call — the flag path has had that bug for as long
	// as it has taken a level.
	//
	// The split has two outputs and both travel from here: the bare slug in
	// Model, and the level beside it in Effort. Keeping the second half on the
	// client is what lets a headless seat apply its own pin to every request
	// without putting the suffix back onto the provider's model id.
	model, level := roles.SplitEffort(model)
	effort, _ := provider.ParseEffort(level)
	service, bare := sources.For(model)
	configured := provider.Config{
		APIKey:      service.Key,
		BaseURL:     service.Address,
		Model:       bare,
		Direct:      !strings.EqualFold(strings.TrimSpace(service.Source.ID), modelsource.DefaultID),
		KeyOptional: service.Source.KeyOptional,
		BillingDoor: service.Door.Name,
		Effort:      effort,
	}
	if strings.EqualFold(strings.TrimSpace(service.Source.ID), "codex") {
		configured.APIKey = codexauth.Sentinel
		configured.HTTPClient = codexauth.Client(service.Home)
		configured.BillingDoor = ""
		configured.ModelPrice = func(string) (float64, float64, bool) { return 0, 0, false }
	}
	if service.Overflow != nil {
		configured.PlanOverflow = service.Overflow.Address
		configured.PlanOverflowDoor = service.Overflow.Name
		configured.OverflowOnPlanPause = service.PlanPaused == PlanPausedUseMeter
	}
	return configured
}

// WithoutSeatPin is the settings a raw request path takes: the same account,
// the same model, and the seat's thinking level dropped.
//
// The document path builds its own raw body with no reasoning object, so a seat
// pin left on the client would make the raw request and the model-call row
// describe different calls. This door is exported because internal/session
// reads documents through the same shape and may not spell the adapter's effort
// vocabulary itself: its effort law (effortguard_test.go) treats a file that
// names provider.Effort as claiming a depth, while the document path is
// declining to carry one.
func WithoutSeatPin(configured provider.Config) provider.Config {
	configured.Effort = provider.EffortNone
	return configured
}

// ClientConfig is this profile's answer to "how do I talk to that model". It
// is what every client outside this package is built from; a caller sets only
// what legitimately differs — a timeout, a routing strategy, or seams its own
// surface owns — never APIKey and never BaseURL.
func (c Config) ClientConfig(model string) provider.Config {
	configured := ClientConfigFor(c.Sources.OrDefault(c.APIKey, c.BaseURL), model)
	configured.Timeout = c.Timeout
	// The published answer to "does this model take this field", from rows
	// already in memory. A nil catalog and a catalog still warming both say
	// "unknown", which the adapter treats as "send nothing on your own
	// initiative" — never as permission.
	configured.SupportsParameter = c.Models.SupportsParameter
	// And what the row says about the model's thinking pass, under the same
	// contract, translated into the adapter's words at this seam.
	configured.ReasoningProfile = ReasoningProfileSeam(c.Models)
	// And the model's own list price, which is what the adapter bounds a
	// latency-sorted request against. Same contract: never blocks, and
	// "nobody published one" sends no ceiling at all.
	if configured.ModelPrice == nil {
		configured.ModelPrice = c.Models.PriceNow
	}
	return configured
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

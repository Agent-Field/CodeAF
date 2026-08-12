// Package command is the surface's hand on the machine.
//
// Everything a window can do that is not reading the thread lives here: the
// model slots and the palette that picks between them, the durable commands a
// key press journals, the notebook reads and retractions, the workspace paths a
// rendered link resolves through, the voice handles, and the session identity
// all three of those agree about. The TUI reaches every one of them through
// interfaces it declares; this is the one object that satisfies them.
//
// It was the largest single thing inside cmd/aforge/chat.go, which meant a
// capability could only be reached by a process that had built a terminal.
// Nothing in this package draws anything or knows what a frame is — it takes
// the store, the clients, and the seams its host owns, and answers questions.
package command

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/head"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
	"github.com/Agent-Field/aforge-v2/internal/tui"
	"github.com/Agent-Field/aforge-v2/internal/voice"
)

// Prefs persists the surface's model choices across launches. It lives
// beside the graph database so the whole resident state moves as one
// directory.
type Prefs struct {
	ChatModel string `json:"chat_model,omitempty"`
	TaskModel string `json:"task_model,omitempty"`
	// PlanModel empty means the plan slot follows the work model live —
	// the same contract as an empty boost slot.
	PlanModel   string `json:"plan_model,omitempty"`
	BoostModel  string `json:"boost_model,omitempty"`
	VoiceModel  string `json:"voice_model,omitempty"`
	ImageModel  string `json:"image_model,omitempty"`
	SpeechModel string `json:"speech_model,omitempty"`
	MusicModel  string `json:"music_model,omitempty"`
	VideoModel  string `json:"video_model,omitempty"`

	// SplitPct is the chat pane's share of the terminal width in percent,
	// set by dragging the divider (or [ and ]) in the TUI.
	SplitPct int `json:"split_pct,omitempty"`
}

var fallbackModels = []string{
	"~deepseek/deepseek-v4-flash-latest",
	"moonshotai/kimi-k2.6",
	"qwen/qwen3-30b-a3b",
	"google/gemma-3-12b-it",
}

// MediaModels is the resolved media configuration seen by new leaves.
// Each leaf takes one value snapshot, preserving the same "next job/leaf"
// boundary used by the hot-swappable work model.
type MediaModels struct {
	// fill supplies the slot defaults that have to be asked of the model
	// catalog. It runs at most once, on the first read, so a cold catalog is
	// paid for by the first leaf that needs a media model rather than by the
	// user waiting for the surface to appear.
	once  sync.Once
	fill  func(*exec.MediaTools)
	mu    sync.RWMutex
	tools exec.MediaTools
}

// NewMediaModels seats the media configuration new leaves are cut from: the
// tools resolved at construction, and the fill that asks the catalog for the
// slot defaults it owns. fill runs at most once, on the first read.
func NewMediaModels(tools exec.MediaTools, fill func(*exec.MediaTools)) *MediaModels {
	return &MediaModels{fill: fill, tools: tools}
}

func (m *MediaModels) Snapshot() exec.MediaTools {
	if m == nil {
		return exec.MediaTools{}
	}
	m.resolve()
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tools
}

func (m *MediaModels) resolve() {
	m.once.Do(func() {
		if m.fill == nil {
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		m.fill(&m.tools)
	})
}

func (m *MediaModels) Set(role, slug string, models *catalog.Catalog) {
	if m == nil {
		return
	}
	// A user's choice must land on top of the discovered defaults, never
	// underneath a fill that has not run yet.
	m.resolve()
	m.mu.Lock()
	defer m.mu.Unlock()
	switch role {
	case "image":
		m.tools.ImageModel = slug
	case "speech":
		m.tools.SpeechModel = slug
	case "music":
		m.tools.MusicModel = slug
	case "video":
		m.tools.VideoModel = slug
		m.tools.VideoPrice = 0
		if model, ok := models.Model(slug); ok {
			m.tools.VideoPrice = model.RequestPrice
		}
	}
}

// Commander bridges surface commands to the two hot-swappable clients
// and the durable command journal. Session state lives here so /new and a
// subsequent /cancel always agree about which thread owns the request.
type Commander struct {
	settings      config.Config
	database      string
	prefsDir      string
	workspaceRoot string

	chatClient *pool.Client
	taskClient *pool.Client
	planClient *pool.Client
	store      *store.Store
	// head is the loop that answers, and the only thing the surface asks of it
	// is to stop. A window that is not the brain has none and says so.
	head *head.Head

	mu               sync.Mutex
	prefs            Prefs
	sessionID        string
	streamEvents     <-chan tui.StreamEvent
	voiceRecorder    voice.Recorder
	voiceTranscriber voice.Transcriber
	attachSession    func(string) error

	// residency is the window's account of which process runs the brain. Nil
	// for an embedded or test commander, which is simply never a second
	// window and says nothing about it.
	residency Residency

	// jobID resolves the top-level job a node belongs to, which is what names
	// its workspace directory. It is the host's law rather than this package's:
	// the same answer decides where a leaf works, so there is one owner of it
	// and this holds a handle rather than a second copy. Nil answers with the
	// node's own id, which is the safe reading — an isolated directory always
	// is, a shared one is not.
	jobID func(store.Node) string
	// installRuler is what a work-model switch means to the measured profile:
	// the capability being sized has changed, so that model's own ruler has to
	// be installed before the next planning call can observe the new client.
	// The rulers belong to whoever built the executor, so this is a handle too.
	installRuler func(model string)

	catalogOnce    sync.Once
	catalogChoices []tui.ModelChoice
	slotCatalogMu  sync.Mutex
	slotCatalog    map[string][]tui.ModelChoice
	models         *catalog.Catalog
	mediaModels    *MediaModels
}

// Residency is the window's own account of which process runs the brain. The
// surface asks the commander on every poll; the commander asks this. It is an
// interface because the answer is the host's — a lease, a lock, a promotion in
// flight — and none of that is a command.
type Residency interface {
	Poll() (tui.Residency, tui.Commander)
}

// Options is everything a commander is built from. It is a struct rather than
// a parameter list because a resident's commander and a visitor's differ by
// which of these are present, not by which constructor was called: a visitor
// carries no clients, no head and no catalog, and every capability that would
// have spoken to a provider degrades to the recorded preference instead.
type Options struct {
	Settings      config.Config
	Database      string
	PrefsDir      string
	WorkspaceRoot string

	ChatClient *pool.Client
	TaskClient *pool.Client
	PlanClient *pool.Client
	Store      *store.Store
	Head       *head.Head

	Prefs            Prefs
	SessionID        string
	StreamEvents     <-chan tui.StreamEvent
	VoiceRecorder    voice.Recorder
	VoiceTranscriber voice.Transcriber
	AttachSession    func(string) error

	Models      *catalog.Catalog
	MediaModels *MediaModels

	// JobID and InstallRuler are the host's two seams — see the fields they
	// land on. Both are optional and both degrade to doing nothing surprising.
	JobID        func(store.Node) string
	InstallRuler func(model string)
}

// New builds a commander over one window's seams.
func New(options Options) *Commander {
	return &Commander{
		settings:         options.Settings,
		database:         options.Database,
		prefsDir:         options.PrefsDir,
		workspaceRoot:    options.WorkspaceRoot,
		chatClient:       options.ChatClient,
		taskClient:       options.TaskClient,
		planClient:       options.PlanClient,
		store:            options.Store,
		head:             options.Head,
		prefs:            options.Prefs,
		sessionID:        options.SessionID,
		streamEvents:     options.StreamEvents,
		voiceRecorder:    options.VoiceRecorder,
		voiceTranscriber: options.VoiceTranscriber,
		attachSession:    options.AttachSession,
		models:           options.Models,
		mediaModels:      options.MediaModels,
		jobID:            options.JobID,
		installRuler:     options.InstallRuler,
	}
}

// SetHead installs the loop that answers. The head is built after the
// commander because it needs the surface's own handles, and the surface needs
// somewhere to keep the handle that stops it.
func (c *Commander) SetHead(loop *head.Head) {
	if c == nil {
		return
	}
	c.head = loop
}

// SetResidency installs the window's account of the resident role. It is set
// after construction because a window learns what it is — and learns it again
// every time the role moves — after its commander exists.
func (c *Commander) SetResidency(residency Residency) {
	if c == nil {
		return
	}
	c.residency = residency
}

// DailyBudgetUSD is the rail this process is currently holding jobs to. It
// moves when /budget default lands or a settings row is applied, which is why
// it is read under the lock rather than off the settings the commander was
// built with.
func (c *Commander) DailyBudgetUSD() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.settings.DailyBudgetUSD
}

// ProfileDir is where every persisted setting is written.
func (c *Commander) ProfileDir() string {
	return c.profileDir()
}

// Session is which conversation this commander speaks for. It is the whole of
// the session id's critical section together with setSession: everything
// downstream — attaching, posting, cancelling — happens outside the lock,
// because those are store calls and a store call must never hold it.
func (c *Commander) Session() string {
	return c.session()
}

// jobDir names the workspace directory a node's job works in.
func (c *Commander) jobDir(node store.Node) string {
	if c.jobID == nil {
		return node.ID
	}
	return c.jobID(node)
}

// KeepAttachment copies what the person attached into the resident's
// content-addressed store, so the durable message carries our own immutable
// copy rather than a pointer at a file they may move tomorrow.
func (c *Commander) KeepAttachment(path string) (string, error) {
	return exec.KeepAttachment(AttachmentStoreRoot(c.database), path)
}

// AttachmentStoreRoot is the same cas/ directory the store spills folds into:
// one profile, one blob store.
func AttachmentStoreRoot(database string) string {
	return filepath.Join(filepath.Dir(database), "cas")
}

func (c *Commander) StreamEvents() <-chan tui.StreamEvent { return c.streamEvents }

// Interrupt stops the turn the head is answering right now and hands it the
// words the reader has already seen, so the durable line that ends the turn is
// that same reply marked where it stopped rather than a fresh apology.
func (c *Commander) Interrupt(partial string) bool {
	if c == nil || c.head == nil {
		return false
	}
	return c.head.Interrupt(partial)
}

// Residency lets the surface say what this window is, and hands it a
// replacement commander at the moment that answer changes. It runs on the
// poll's goroutine every cycle, so everything expensive behind it is a
// goroutine the residency starts for itself.
func (c *Commander) Residency() (tui.Residency, tui.Commander) {
	if c == nil || c.residency == nil {
		return tui.Residency{}, nil
	}
	return c.residency.Poll()
}

func (c *Commander) VoiceRecorder() voice.Recorder       { return c.voiceRecorder }
func (c *Commander) VoiceTranscriber() voice.Transcriber { return c.voiceTranscriber }

func (c *Commander) RecordVoiceUsage(cost float64) {
	if c == nil || c.store == nil || cost <= 0 {
		return
	}
	_ = c.store.RecordUsage(store.NodeUsage{NodeID: store.RootID, Cost: cost})
}

func (c *Commander) Models() []string {
	candidates := make([]string, 0, len(c.settings.Panel.Models)+7)
	for _, spec := range c.settings.Panel.Models {
		candidates = append(candidates, spec.Slug)
	}
	candidates = append(candidates, c.settings.Model)
	if c.chatClient != nil {
		candidates = append(candidates, c.chatClient.Model())
	}
	if c.taskClient != nil {
		candidates = append(candidates, c.taskClient.Model())
	}
	if c.planClient != nil {
		candidates = append(candidates, c.planClient.Model())
	}
	if boost := strings.TrimSpace(c.CurrentModel("boost")); boost != "" {
		candidates = append(candidates, boost)
	}
	models := dedupeModels(candidates)
	if len(models) < 4 {
		models = dedupeModels(append(models, fallbackModels...))
	}
	return models
}

func (c *Commander) Catalog() []tui.ModelChoice {
	c.catalogOnce.Do(func() {
		if c.models != nil {
			for _, model := range config.ModelCandidates(c.models, "talk") {
				c.catalogChoices = append(c.catalogChoices, tui.ModelChoice{
					Slug: model.ID, Name: model.Name,
					Price: formatModelPrice(model.PromptPrice, model.CompletionPrice),
					Note:  catalog.ReasoningWord(model, provider.ReasoningMandatory(model.ID)),
				})
			}
		}
		if len(c.catalogChoices) < 4 {
			seen := make(map[string]bool, len(c.catalogChoices))
			for _, choice := range c.catalogChoices {
				seen[choice.Slug] = true
			}
			for _, model := range c.Models() {
				if seen[model] {
					continue
				}
				c.catalogChoices = append(c.catalogChoices, tui.ModelChoice{Slug: model})
				seen[model] = true
			}
		}
	})
	return append([]tui.ModelChoice(nil), c.catalogChoices...)
}

// CatalogModels is the v2 window's read of the same catalog Catalog serves:
// the full capability-filtered rows for the talk-shaped slots, as catalog
// rows rather than tui.ModelChoice — the v1 window is the surface being
// replaced, and its choice type drops the window, the price figures and the
// published score that the v2 picker draws. A commander with no catalog
// answers nil, and the caller keeps whatever narrower read it has.
func (c *Commander) CatalogModels() []catalog.Model {
	if c.models == nil {
		return nil
	}
	return config.ModelCandidates(c.models, "talk")
}

// CatalogFor keys the shared searchable picker by palette slot. Capability
// filtering lives in config.ModelCandidates so music discovery uses the exact
// same recognizable-TTS exclusion as runtime resolution.
func (c *Commander) CatalogFor(role string) []tui.ModelChoice {
	if role == "talk" || role == "work" || role == "plan" || role == "boost" {
		return c.Catalog()
	}
	if cached, ok := c.cachedSlotCatalog(role); ok {
		return cached
	}

	choices := make([]tui.ModelChoice, 0)
	for _, model := range config.ModelCandidates(c.models, role) {
		choices = append(choices, tui.ModelChoice{
			Slug: model.ID, Name: model.Name,
			Price: formatModelPrice(model.PromptPrice, model.CompletionPrice),
			Note:  catalog.ReasoningWord(model, provider.ReasoningMandatory(model.ID)),
		})
	}
	if len(choices) == 0 {
		if current := strings.TrimSpace(c.CurrentModel(role)); current != "" {
			choices = []tui.ModelChoice{{Slug: current}}
		}
	}
	c.cacheSlotCatalog(role, choices)
	return choices
}

// cachedSlotCatalog hands back a copy, never the stored slice: the picker is
// free to sort what it is given.
func (c *Commander) cachedSlotCatalog(role string) ([]tui.ModelChoice, bool) {
	c.slotCatalogMu.Lock()
	defer c.slotCatalogMu.Unlock()
	choices, ok := c.slotCatalog[role]
	if !ok {
		return nil, false
	}
	return append([]tui.ModelChoice(nil), choices...), true
}

func (c *Commander) cacheSlotCatalog(role string, choices []tui.ModelChoice) {
	c.slotCatalogMu.Lock()
	defer c.slotCatalogMu.Unlock()
	if c.slotCatalog == nil {
		c.slotCatalog = make(map[string][]tui.ModelChoice)
	}
	c.slotCatalog[role] = append([]tui.ModelChoice(nil), choices...)
}

func (c *Commander) CurrentModel(role string) string {
	switch role {
	case "work":
		if c.taskClient != nil {
			return c.taskClient.Model()
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.prefs.TaskModel
	case "plan":
		if c.planClient != nil {
			return c.planClient.Model()
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.prefs.PlanModel
	case "boost":
		if boost := c.boostPreference(); boost != "" {
			return boost
		}
		if c.taskClient != nil {
			return c.taskClient.Model()
		}
		return ""
	case "voice":
		c.mu.Lock()
		defer c.mu.Unlock()
		return firstNonEmptyString(c.prefs.VoiceModel, c.settings.VoiceModel)
	case "image":
		c.mu.Lock()
		defer c.mu.Unlock()
		return firstNonEmptyString(c.prefs.ImageModel, c.settings.ResolveImageModel(c.models))
	case "speech":
		c.mu.Lock()
		defer c.mu.Unlock()
		return firstNonEmptyString(c.prefs.SpeechModel, c.settings.ResolveSpeechModel(c.models))
	case "music":
		c.mu.Lock()
		defer c.mu.Unlock()
		return firstNonEmptyString(c.prefs.MusicModel, c.settings.ResolveMusicModel(c.models))
	case "video":
		c.mu.Lock()
		defer c.mu.Unlock()
		return firstNonEmptyString(c.prefs.VideoModel, c.settings.ResolveVideoModel(c.models))
	}
	if c.chatClient != nil {
		return c.chatClient.Model()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prefs.ChatModel
}

// boostPreference is the boost slot's own critical section: empty means the
// user never chose one and the work model stands in.
func (c *Commander) boostPreference() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.TrimSpace(c.prefs.BoostModel)
}

func (c *Commander) ImageInputSupportFor(role string) (string, bool) {
	model := c.CurrentModel(role)
	return model, c.models != nil && c.models.Supports(model, "input", "image")
}

// ContextWindow is how many tokens the model in a role slot will accept.
//
// It is the denominator of the context figure on a room's meta strip; the
// numerator is journaled by the calls themselves (store.RoomSpend.HeadPrompt)
// and needs no help from here. The two halves live apart because they are
// different kinds of fact: what a turn actually sent is history, and how much
// the model would have held is a property of the model, true before the turn
// started and unchanged by it.
//
// The bool is the honest half of a slot whose model the catalog cannot size —
// a catalog that never loaded, a visitor commander that holds none, a slug
// cached before the field was kept. Zero is returned with it, and a caller
// that renders a percentage anyway would be inventing a health signal that
// errs toward calm: the gauge would read "plenty of room" for a model whose
// window nobody knows.
func (c *Commander) ContextWindow(role string) (int, bool) {
	if c == nil || c.models == nil {
		return 0, false
	}
	model := strings.TrimSpace(c.CurrentModel(role))
	if model == "" {
		return 0, false
	}
	tokens := c.models.ContextLength(model)
	if tokens <= 0 {
		return 0, false
	}
	return tokens, true
}

func (c *Commander) ModelFollows(role string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch role {
	case "boost":
		return strings.TrimSpace(c.prefs.BoostModel) == ""
	case "plan":
		// An environment pin is an explicit choice too: the slot only follows
		// the work model when neither the prefs nor AFORGE_PLAN_MODEL name one.
		return strings.TrimSpace(c.prefs.PlanModel) == "" && strings.TrimSpace(c.settings.PlanModel) == ""
	}
	return false
}

func (c *Commander) SetModel(role, slug string) error {
	switch role {
	case "talk":
		if c.chatClient == nil {
			return fmt.Errorf("talk model switching is unavailable in visitor mode")
		}
		if err := c.chatClient.SetModel(slug); err != nil {
			return err
		}
	case "work":
		if c.taskClient == nil {
			return fmt.Errorf("work model switching is unavailable in visitor mode")
		}
		if err := c.taskClient.SetModel(slug); err != nil {
			return err
		}
		// A model switch changes the capability being sized; install that model's
		// own ruler before the next planning call can observe the new client.
		if c.installRuler != nil {
			c.installRuler(c.taskClient.Model())
		}
		// A following plan slot moves with the work model, live — the next
		// planning call sees the new model without the user touching the slot.
		if c.planClient != nil && c.ModelFollows("plan") {
			if err := c.planClient.SetModel(c.taskClient.Model()); err != nil {
				return err
			}
		}
		// And the work already in flight is told, through the one funnel every
		// other mutation walks. Repointing the client alone was a lie with a
		// receipt: the next head turn ran on the new model while every live leaf
		// went on running on the old one, and the person had been shown a
		// confirmation either way.
		c.journalWorkModel(c.taskClient.Model())
	case "plan":
		if c.planClient == nil {
			return fmt.Errorf("plan model switching is unavailable in visitor mode")
		}
		// Empty is meaningful for plan, like boost: it resumes following the
		// work model live.
		target := strings.TrimSpace(slug)
		if target == "" && c.taskClient != nil {
			target = c.taskClient.Model()
		}
		if err := c.planClient.SetModel(target); err != nil {
			return err
		}
	case "boost":
		// Empty is meaningful for boost: it restores live inheritance from work.
	case "voice", "image", "speech", "music", "video":
		if strings.TrimSpace(slug) == "" {
			return fmt.Errorf("%s model cannot be empty", role)
		}
	default:
		return fmt.Errorf("unknown model role %q", role)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if role == "talk" {
		c.prefs.ChatModel = c.chatClient.Model()
	} else if role == "work" {
		c.prefs.TaskModel = c.taskClient.Model()
	} else if role == "plan" {
		c.prefs.PlanModel = strings.TrimSpace(slug)
		// The user cleared the slot by hand: their choice to follow the work
		// model outranks the environment seed for the rest of this session.
		if c.prefs.PlanModel == "" {
			c.settings.PlanModel = ""
		}
	} else if role == "boost" {
		c.prefs.BoostModel = strings.TrimSpace(slug)
	} else if role == "voice" {
		c.prefs.VoiceModel = strings.TrimSpace(slug)
	} else if role == "image" {
		c.prefs.ImageModel = strings.TrimSpace(slug)
	} else if role == "speech" {
		c.prefs.SpeechModel = strings.TrimSpace(slug)
	} else if role == "music" {
		c.prefs.MusicModel = strings.TrimSpace(slug)
	} else if role == "video" {
		c.prefs.VideoModel = strings.TrimSpace(slug)
	}
	if err := SavePrefs(c.prefsDir, c.prefs); err != nil {
		return fmt.Errorf("save chat model preference: %w", err)
	}
	if role == "image" || role == "speech" || role == "music" || role == "video" {
		c.mediaModels.Set(role, strings.TrimSpace(slug), c.models)
	}
	return nil
}

// journalWorkModel makes a work-slot change true for the work that is already
// running, by asking for it the way everything else asks: one typed command per
// live job, through RequestCommand, applied by the reconciler's own arm.
//
// It is per job rather than one command over everything because the funnel
// refuses a command aimed at the permanent spine — "everything" is not a target
// this journal has — and because the arm it lands on re-points a subtree. Only
// the jobs this conversation owns move: a model choice made in one room is a
// statement about that room's work, and a headless errand running beside it was
// never part of the sentence.
//
// Every failure here is a note in the log and nothing more. The local repoint
// has already happened and the preference is about to be saved; a job that
// settled between the read and the write has simply nothing left to move, and
// refusing the whole /model over it would be the worse lie.
func (c *Commander) journalWorkModel(model string) {
	model = strings.TrimSpace(model)
	if c == nil || c.store == nil || model == "" {
		return
	}
	session := c.session()
	nodes, err := c.store.ActiveNodes()
	if err != nil {
		log.Printf("note: could not read the live work to re-point it at %s: %v", model, err)
		return
	}
	for _, node := range nodes {
		if !liveJobOfSession(node, session) {
			continue
		}
		if _, err := c.store.RequestCommand(store.Command{
			SessionID:   session,
			Kind:        store.CommandSetModel,
			Target:      node.ID,
			Instruction: model,
		}); err != nil {
			log.Printf("note: could not ask %s to move to %s: %v", node.ID, model, err)
		}
	}
}

// liveJobOfSession is the funnel's own admissibility rule, asked before the
// command is written rather than after it is refused: a top-level job of this
// conversation, still to be done, and executable work rather than a charter or
// a territory.
func liveJobOfSession(node store.Node, session string) bool {
	if node.Parent != store.RootID || node.ID == store.RootID || node.Folded {
		return false
	}
	if node.Provenance.SessionID != session {
		return false
	}
	if store.IsOrganizationalGroup(node.Group) || node.Group == "charter" {
		return false
	}
	switch node.Status {
	case store.Pending, store.Claimed, store.Running:
		return true
	}
	return false
}

// SplitPct and SaveSplitPct persist the TUI's chat/graph divider position in
// the same prefs file as the model choices.
func (c *Commander) SplitPct() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prefs.SplitPct
}

func (c *Commander) SaveSplitPct(pct int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prefs.SplitPct = pct
	_ = SavePrefs(c.prefsDir, c.prefs)
}

// session and setSession are the whole of the session id's critical section.
// Everything downstream — attaching, posting, cancelling — happens outside the
// lock, because those are store calls and a store call must never hold it.
func (c *Commander) session() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionID
}

func (c *Commander) setSession(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionID = sessionID
}

func (c *Commander) NewSession() (string, error) {
	sessionID := NewSessionID()
	c.setSession(sessionID)
	if c.attachSession != nil {
		if err := c.attachSession(sessionID); err != nil {
			return "", err
		}
	}
	if c.store != nil {
		if _, err := c.store.TouchSeen("tui", sessionID, store.SeenAttached); err != nil {
			return "", err
		}
	}
	return sessionID, nil
}

func (c *Commander) Cancel(nodeID string) error {
	_, err := c.store.RequestCommand(store.Command{
		SessionID:   c.session(),
		Kind:        store.CommandCancel,
		Target:      nodeID,
		Instruction: "cancelled from the TUI",
	})
	return err
}

// Restart is the settled node's forward door, journaled as the identical
// command "restart that" would journal. Cancel's twin in every respect: one
// typed command, one durable receipt, and the reconciler's own splice behind
// it — the key press adds no second control path for a sentence to miss.
func (c *Commander) Restart(nodeID string) error {
	_, err := c.store.RequestCommand(store.Command{
		SessionID:   c.session(),
		Kind:        store.CommandRestart,
		Target:      nodeID,
		Instruction: "restarted from the TUI",
	})
	return err
}

// ConfirmSurgery hands the key path the conversational path's own gates. The
// head is in this process, so there is nothing to route: the same
// surgeryNeedsConfirmation, the same durable question, the same encoded option
// that replays the command when the answer comes back. A window with no head
// behind it has no gate to consult and says so by answering "not asked" — it
// also has no reconciler, so nothing it journals is applied here anyway.
func (c *Commander) ConfirmSurgery(kind store.CommandKind, nodeID string) (bool, error) {
	if c == nil || c.head == nil {
		return false, nil
	}
	return c.head.ConfirmSurgery(c.session(), kind, nodeID)
}

// SubtreeReceipts is the window's door onto what each level of a tree cost.
//
// It is the read 13.11 filed as missing — "TopLevelJobUsage answers per JOB
// ROOT; what is needed is one read of the same shape keyed by node, for one
// subtree" — and it is exposed here for the same reason NodeTraceTail is: a
// surface must be able to draw a receipt beside a row without importing the
// store's internals or learning what a usage row is.
//
// ONE READ PER ROOM ENTERED, not one per row and not one per poll. The ledger
// it returns answers every level inside the subtree from what is already in
// hand — [store.SubtreeLedger.Rollup] takes any member — so expanding and
// collapsing a parent costs nothing. That is the same bargain [Subtrees] struck
// in 257800f, and for the same reason: paying for every subtree on the board to
// fill the one room a reader is standing in is the wrong trade.
//
// A window with no store answers with the empty ledger and no error. That is
// not a failure and not a zero: an empty ledger holds no receipts, every lookup
// in it reports absent, and absent renders as — (8.2.20). A window that cannot
// see the journal must say it does not know, never that the work was free.
func (c *Commander) SubtreeReceipts(root string) (store.SubtreeLedger, error) {
	if c == nil || c.store == nil || strings.TrimSpace(root) == "" {
		return store.SubtreeLedger{}, nil
	}
	return c.store.SubtreeReceipts(root)
}

// NodeModels is the window's door onto WHO DID THE WORK: every model that
// billed a run anywhere in root's subtree, deduped, most expensive first.
//
// It sits beside [Commander.SubtreeReceipts] because it is the same read one
// column over — that ledger says what a subtree cost and this says what spent
// it — and design law §5 asks a record header for both: "elapsed · $cost · Nk
// tok plus the models that did the work (deduped, dim)". Until this passed
// through, the only model word any surface could reach was the one the rail
// resolved for the job's own row, which on an escalating job names the rung the
// work STARTED on and not the one that finished it.
//
// A window with no store answers with no models and no error, which renders as
// absence. That is honest: a surface that cannot see the journal does not know
// what ran, and §16's emptiness rule says an unknown value is drawn as nothing
// rather than guessed.
func (c *Commander) NodeModels(nodeID string) ([]string, error) {
	if c == nil || c.store == nil || strings.TrimSpace(nodeID) == "" {
		return nil, nil
	}
	return c.store.NodeModels(nodeID)
}

// SubtreeRollup is the one-figure form of the same read, for a card that draws
// a whole job and never expands it. The middle return is presence: a root this
// window has never heard of has no rollup, which must not be drawn as a job
// that did nothing.
func (c *Commander) SubtreeRollup(root string) (store.SubtreeRollup, bool, error) {
	if c == nil || c.store == nil || strings.TrimSpace(root) == "" {
		return store.SubtreeRollup{}, false, nil
	}
	return c.store.SubtreeRollup(root)
}

func (c *Commander) NodeTrace(nodeID string, maxBytes int) string {
	text, _, _ := c.NodeTraceSince(nodeID, maxBytes, tui.NodeTraceStamp{})
	return text
}

// NodeTraceSince is the old window's door onto [Commander.NodeTraceTail],
// spelled in that window's own stamp type. It carries no logic of its own: one
// reader, two vocabularies, because two readers is how the surfaces end up
// disagreeing about what a worker did.
func (c *Commander) NodeTraceSince(
	nodeID string, maxBytes int, since tui.NodeTraceStamp,
) (string, tui.NodeTraceStamp, bool) {
	text, size, mod, fresh := c.NodeTraceTail(nodeID, maxBytes, since.Size, since.Mod)
	return text, tui.NodeTraceStamp{Size: size, Mod: mod}, fresh
}

// NodeTraceTail tails the worker's recorder only when the file has moved.
//
// The window asks for the trace on every poll — the executor appends to it
// outside the journal, so nothing else can say whether it changed — and the
// stat that decides where to seek is already on the path to the read. Handing
// its answer back turns a worker that is thinking rather than writing into an
// open and a stat, instead of 64KB read, allocated, and compared against the
// copy the window already holds. The stamp is (size, modtime); a caller that
// has never read hands in a zero time and gets everything.
//
// THE PATH COMES FROM [exec.TracePath] AND NOT FROM HERE, and that is a repair
// rather than tidiness. This function used to spell `.obs/<seq>.trace.log` by
// hand. The recorder MOVED to `.aforge/trace/` — deliberately, so a leaf
// listing its own workspace could not read its siblings' transcripts — and
// exec/workspace.go's comment on that move predicted this exact aftermath in
// these words: "A reader left behind does not fail: it opens nothing, renders
// empty, and looks exactly like a worker that is thinking rather than writing."
// That is what it did. Measured on a real profile: every job run since the move
// has its trace under `.aforge/trace/` and no `.obs` directory at all, so the
// node page had been drawing an empty execution feed for every recent run and
// saying nothing was wrong. TracePath is the one spelling, and it already falls
// back to the pre-move location so a run recorded before it stays readable.
//
// It is exported without the old window's stamp type because internal/tui is
// the surface being replaced, and a second surface that had to import the first
// one's structs to read a log file would be a dependency on a thing whose whole
// purpose is to be deleted.
func (c *Commander) NodeTraceTail(
	nodeID string, maxBytes int, sinceSize int64, sinceMod time.Time,
) (string, int64, time.Time, bool) {
	var noTime time.Time
	if c == nil || c.store == nil || c.workspaceRoot == "" || nodeID == "" || maxBytes <= 0 {
		return "", 0, noTime, true
	}
	node, ok, err := c.store.Node(nodeID)
	if err != nil || !ok {
		return "", 0, noTime, true
	}
	jobDir := filepath.Join(c.workspaceRoot, c.jobDir(node))
	// The node's own id, which is what the worker filed its recorder under. The
	// creation sequence is the fallback and nothing more: it belongs to the whole
	// splice, so before the recorders were named per node one file held every
	// sibling's turns — and every run written that way is still on disk and still
	// worth reading.
	path := exec.TracePath(jobDir, node.ID)
	if _, statErr := os.Stat(path); statErr != nil {
		path = exec.TracePath(jobDir, strconv.FormatInt(node.CreatedSeq, 10))
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, noTime, true
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", 0, noTime, true
	}
	size, mod := info.Size(), info.ModTime()
	if size == sinceSize && mod.Equal(sinceMod) && !sinceMod.IsZero() {
		return "", size, mod, false
	}
	offset := size - int64(maxBytes)
	if offset < 0 {
		offset = 0
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return "", 0, noTime, true
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)))
	if err != nil {
		return "", 0, noTime, true
	}
	text := string(data)
	if offset > 0 {
		// A tail cut at a byte is a tail cut mid-line, and the fragment it
		// begins with is a different fragment on every poll: the window's first
		// block is re-keyed each cycle, the reader's anchor with it, and the
		// parser's kept prefix can never hold. The head moves to the next line
		// boundary so what arrives is always whole lines.
		if at := strings.IndexByte(text, '\n'); at >= 0 {
			text = text[at+1:]
		}
	}
	return text, size, mod, true
}

func (c *Commander) ResolveMediaPath(nodeID, relative string) (string, bool) {
	if filepath.IsAbs(relative) {
		if info, err := os.Stat(relative); err == nil && !info.IsDir() {
			return relative, true
		}
		return "", false
	}
	return c.ResolveWorkspacePath(nodeID, relative)
}

// ResolveWorkspacePath is the shared safety boundary for every deliverable
// link. A node resolves to its top-level job directory; relative traversal may
// not escape that directory, and only existing files become links.
func (c *Commander) ResolveWorkspacePath(nodeID, relative string) (string, bool) {
	if c == nil || c.store == nil || c.workspaceRoot == "" || nodeID == "" || strings.TrimSpace(relative) == "" {
		return "", false
	}
	if filepath.IsAbs(relative) {
		return "", false
	}
	node, ok, err := c.store.Node(nodeID)
	if err != nil || !ok {
		return "", false
	}
	root := filepath.Join(c.workspaceRoot, c.jobDir(node))
	target := filepath.Join(root, filepath.Clean(relative))
	inside, err := filepath.Rel(root, target)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(os.PathSeparator)) {
		return "", false
	}
	info, err := os.Stat(target)
	return target, err == nil && !info.IsDir()
}

// WorkspacePath resolves the directory itself for the settled-card and
// history affordances. The directory must already exist; rendering never
// creates workspaces or guesses at a missing job.
func (c *Commander) WorkspacePath(nodeID string) (string, bool) {
	if c == nil || c.store == nil || c.workspaceRoot == "" || nodeID == "" {
		return "", false
	}
	node, ok, err := c.store.Node(nodeID)
	if err != nil || !ok {
		return "", false
	}
	target := filepath.Join(c.workspaceRoot, c.jobDir(node))
	info, err := os.Stat(target)
	return target, err == nil && info.IsDir()
}

func (c *Commander) Notebook(limit int) []store.Fact {
	if c == nil || c.store == nil {
		return nil
	}
	facts, err := c.store.Facts(limit)
	if err != nil {
		return nil
	}
	if limit > 0 && len(facts) > limit {
		facts = facts[:limit]
	}
	return facts
}

func (c *Commander) SearchNotebook(terms string, limit int) []store.Fact {
	if c == nil || c.store == nil {
		return nil
	}
	facts, err := c.store.SearchFactsUncounted(store.FactQuery{
		Terms: strings.TrimSpace(terms), Limit: limit,
	})
	if err != nil {
		return nil
	}
	return facts
}

func (c *Commander) NotebookEvidence(seq int64) []string {
	if c == nil || c.store == nil || seq <= 0 {
		return nil
	}
	target, found, err := c.store.FactBySeq(seq)
	if err != nil || !found {
		return nil
	}
	all, err := c.store.Facts(0)
	if err != nil {
		return nil
	}
	bySeq := make(map[int64]store.Fact, len(all))
	for _, fact := range all {
		bySeq[fact.Seq] = fact
	}
	evidence := make([]store.Fact, 0)
	if target.NodeID != "" {
		evidence = append(evidence, target)
	}
	for _, fact := range all {
		if fact.EvidenceSeq == target.Seq {
			evidence = append(evidence, fact)
		}
	}
	if target.Unsettled != nil {
		for _, approach := range target.Unsettled.Approaches {
			for _, evidenceSeq := range approach.Evidence {
				if fact, ok := bySeq[evidenceSeq]; ok {
					evidence = append(evidence, fact)
				}
			}
		}
	}
	seen := make(map[string]bool)
	refs := make([]string, 0, len(evidence))
	for _, fact := range evidence {
		ref := strings.TrimSpace(fact.NodeID)
		if ref == "" || ref == store.RootID {
			ref = "#" + strconv.FormatInt(fact.Seq, 10)
		}
		if !seen[ref] {
			seen[ref] = true
			refs = append(refs, ref)
		}
	}
	return refs
}

func (c *Commander) RetractNotebook(seq int64) error {
	if c == nil || c.store == nil {
		return fmt.Errorf("notebook store unavailable")
	}
	fact, found, err := c.store.FactBySeq(seq)
	if err != nil {
		return err
	}
	if !found || fact.Status != store.FactActive {
		return fmt.Errorf("belief #%d is not active", seq)
	}
	if err := c.store.QuarantineFact(seq, 0, store.FactOriginUser); err != nil {
		return err
	}
	_, err = thread.Post(c.store, store.Message{
		SessionID: c.session(),
		Role:      store.RoleSystem,
		Body:      "· let go — " + firstLine(fact.Body),
	})
	return err
}

func (c *Commander) DatabasePath() string { return c.database }

func formatModelPrice(promptPrice, completionPrice float64) string {
	if promptPrice < 0 || completionPrice < 0 || promptPrice == 0 && completionPrice == 0 {
		return ""
	}
	return fmt.Sprintf("$%s/M in · $%s/M out",
		formatMillionPrice(promptPrice*1_000_000),
		formatMillionPrice(completionPrice*1_000_000),
	)
}

func formatMillionPrice(price float64) string {
	formatted := strings.TrimRight(strings.TrimRight(strconv.FormatFloat(price, 'f', 6, 64), "0"), ".")
	if formatted == "" {
		return "0"
	}
	return formatted
}

func dedupeModels(candidates []string) []string {
	seen := make(map[string]bool, len(candidates))
	models := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		models = append(models, candidate)
	}
	return models
}

func prefsPath(dir string) string { return filepath.Join(dir, "settings.json") }

func LoadPrefs(dir string) Prefs {
	var prefs Prefs
	raw, err := os.ReadFile(prefsPath(dir))
	if err != nil {
		return prefs
	}
	_ = json.Unmarshal(raw, &prefs)
	// A ":batch" pick can linger in a settings file written before the
	// catalog stopped offering batch-only endpoints; the base slug is the
	// same model on the interactive endpoint.
	for _, slot := range []*string{&prefs.ChatModel, &prefs.TaskModel,
		&prefs.PlanModel, &prefs.BoostModel} {
		*slot = strings.TrimSuffix(*slot, ":batch")
	}
	return prefs
}

func SavePrefs(dir string, prefs Prefs) error {
	raw, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(prefsPath(dir), raw, 0o600)
}

// NewVisitor builds the commander a visitor drives. It deliberately
// carries no model clients and no live catalog: a visitor owns no head, so
// every capability that would speak to a provider must degrade to the recorded
// preference rather than reach for a client that does not exist here.
func NewVisitor(path, sessionID string, graph *store.Store,
	attachSession func(string) error) *Commander {
	return &Commander{
		database:      path,
		prefsDir:      filepath.Dir(path),
		workspaceRoot: filepath.Join(filepath.Dir(path), "workspace"),
		store:         graph,
		prefs:         LoadPrefs(filepath.Dir(path)),
		sessionID:     sessionID,
		attachSession: attachSession,
	}
}

// NewSessionID mints a fresh conversation id. It is random rather than
// sequential because a session id is a name, not an order: two windows opened
// in the same second must not collide, and nothing downstream reads it as a
// number.
func NewSessionID() string {
	var random [4]byte
	if _, err := rand.Read(random[:]); err == nil {
		return hex.EncodeToString(random[:])
	}
	return fmt.Sprintf("%08x", time.Now().UnixNano())
}

func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return text[:index]
	}
	return text
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// The surface's whole vocabulary, in two lines. It used to take five, one per
// capability shard, because internal/tui declared a small interface per method
// and found this object behind each of them with a type assertion — and the
// five were only the shards that had names. The collapse (chat-rebuild 4.5's
// Backend/Commander/Streams) folded all of them into tui.Commander, so a
// removed or renamed method is now a compile error here, in the package that
// owns it, rather than a capability that quietly stops being offered.
//
// Streams is named separately although Commander embeds it: it is the seam the
// assembly wave wires the live region to, and a window that has just lost the
// resident role reaches for it alone.
var (
	_ tui.Commander = (*Commander)(nil)
	_ tui.Streams   = (*Commander)(nil)
)

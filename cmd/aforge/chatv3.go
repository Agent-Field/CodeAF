package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/history"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/search"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
)

// runChatV3 is the v3 door: one session agent over this directory, and the
// linear surface that shows it. It opens no graph database, claims no
// residency and starts no runner — the tasker attaches later (docs/CHAT-V3.md
// milestone V3-1), and until it does, pretending to boot it would only buy the
// person a slower start and a rail full of nothing.
func runChatV3(args []string) error {
	flags := flag.NewFlagSet("chat", flag.ContinueOnError)
	model := flags.String("model", "", "model slug for this session; beats the configured default")
	once := flags.String("once", "", "run one message non-interactively, print the reply, and exit")
	file := flags.String("session", "", "session transcript to resume; empty resumes this directory's most recent")
	noCompact := flags.Bool("no-compact", false, "never compact automatically")
	yolo := flags.Bool("yolo", false, "run every tool without asking: the approval default becomes allow")
	reasoning := flags.String("reasoning", "", "how hard this session's model is asked to think: off, low, medium or high")
	if err := flags.Parse(reorder(args, map[string]bool{
		"model": true, "once": true, "session": true, "reasoning": true,
	})); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf(`usage: aforge chat [--model slug] [--reasoning level] [--session path] [--once "text"] [--no-compact] [--yolo]`)
	}
	// The level is validated HERE, before anything is opened, so a typo is a
	// usage error and not a knob that silently did nothing for a whole session.
	// It is a one-shot: ctrl+t in the picker overrides it from that moment on,
	// and /new — a new agent — starts with no level at all.
	level, ok := session.ParseReasoning(*reasoning)
	if !ok {
		return fmt.Errorf("--reasoning %q: use off, low, medium or high", *reasoning)
	}

	// The same resolution every other surface does: environment and the
	// profile file, one place, one error message when there is no key.
	settings, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "aforge chat needs a model to talk with.")
		fmt.Fprintln(os.Stderr, "export OPENROUTER_API_KEY (or OPENAI_API_KEY) and run it again.")
		return err
	}
	chosen := strings.TrimSpace(*model)
	if chosen == "" {
		chosen = settings.Model
	}
	workspace, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}

	transcript, resumed, err := resolveV3Session(strings.TrimSpace(*file), workspace)
	if err != nil {
		return err
	}

	// Model discovery starts here and is waited for NOWHERE on this path. On a
	// cold cache resolving it is a network round-trip, and everything it feeds
	// — the /model picker's list, the context window — has a good answer
	// without it: the picker falls back to disk and then to its built-ins, and
	// an unknown window leaves the session on its conservative default.
	models := catalog.LoadLazy(context.Background(), catalog.Options{
		BaseURL: settings.BaseURL, APIKey: settings.APIKey, Dir: settings.ProfileDir,
	})

	cfg := session.Config{
		Workspace:      workspace,
		Model:          chosen,
		APIKey:         settings.APIKey,
		BaseURL:        settings.BaseURL,
		CompactEnabled: !*noCompact,
		SessionFile:    transcript,
		// The window the model this session STARTS on actually accepts, when
		// anybody can say so without waiting. Zero keeps session's own
		// conservative default, and the warm-up below corrects it in place the
		// moment the catalog resolves.
		ContextWindow: v3Window(models, chosen),
		// Whether the model in use can LOOK at a picture, from the catalog's
		// published input modalities. It is a closure rather than a value
		// because the answer is about the model the NEXT turn rides, and this
		// session's model changes under /model (see [v3SeesImages]).
		SupportsImages: v3SeesImages(models),
	}

	// What this session may do without asking, which model answers its
	// auxiliary calls, and what it may spend. All three are settings rows, and a
	// row that cannot be read stops the launch here rather than downstream.
	cfg, err = applyV3Governance(cfg, settings.ProfileDir, *yolo)
	if err != nil {
		return err
	}

	if text := strings.TrimSpace(*once); text != "" {
		// Nobody is watching a --once run, so nobody can answer a question. The
		// policy's "prompt" therefore refuses the call with a result the model
		// can act on (internal/session's consent.go), and a person who wants
		// that work to run says so with --yolo. NON-INTERACTIVE WORK NEEDS AN
		// EXPLICIT POSTURE: the two honest answers are "refuse what you would
		// have asked about" and "I have said in advance that this may run", and
		// neither of them is a gate that quietly opens because the terminal
		// happens to be a pipe.
		cfg.AskConsent = false
		return runChatV3Once(cfg, text, level, resumed)
	}
	// Interactive: there is a surface, and it answers (internal/tui3's
	// consent.go). This is the ONLY path that sets it.
	cfg.AskConsent = true

	agent, cfg, notice, err := openV3Agent(cfg, workspace)
	if err != nil {
		return err
	}
	if notice != "" {
		// The session file moved under us, so everything downstream that names
		// it — /help, /new, the resumed line — has to name the new one.
		transcript, resumed = cfg.SessionFile, false
	}
	// The boot override lands on the model this session starts on, which is the
	// only model it can be about: --reasoning names a strength, not a model, and
	// the level is kept per model from here on (internal/session's agent.go).
	agent.SetReasoning(level)
	// Close is the surface's to call — /quit and ctrl+c both go through it —
	// but a Run that returns by any other road must still flush the file.
	defer func() { _ = agent.Close() }()

	guard.Go("chatv3/models", func() { warmV3Models(models, agent, chosen) })

	// The two things this surface keeps on the person's behalf rather than the
	// session's: what they have typed before, and what they have half-typed
	// now. Both are settings (internal/config), both are off by one row, and
	// neither is ever waited for — a history file that cannot be opened costs
	// the up arrow and nothing else.
	//
	// They read through the project layer like every other row: a repository
	// that says "record nothing from this directory" is answering about ITS
	// directory, which is the whole reason the layer is per-workspace. The
	// broken-file error cannot actually arrive here — governance above read the
	// same file and would have stopped the launch — and it is still returned
	// rather than swallowed, because the only wrong thing to do with an
	// unreadable settings file is carry on as though it said nothing.
	keepHistory, err := config.ProjectBoolAt(workspace, settings.ProfileDir, config.KeyHistoryEnabled)
	if err != nil {
		return err
	}
	keepDraft, err := config.ProjectBoolAt(workspace, settings.ProfileDir, config.KeyDraftPersist)
	if err != nil {
		return err
	}
	var recall tui3.History
	if keepHistory {
		if dir, err := v3Dir(); err == nil {
			store := history.New(filepath.Join(dir, "history.jsonl"))
			defer func() { _ = store.Close() }()
			recall = store
		}
	}
	draft := ""
	if keepDraft {
		if dir, err := v3Dir(); err == nil {
			draft = tui3.DraftFile(dir, workspace)
		}
	}

	return tui3.Run(context.Background(), tui3.Options{
		Agent: agent,
		// Asked at the moment the picker opens, never at boot: a catalog that
		// resolved while the person was reading is a catalog the picker can
		// use, and one that has not resolved answers nil instead of waiting.
		Models: func() []tui3.Model { return v3Models(models) },
		Fresh: func() (tui3.Agent, string, error) {
			next, err := newV3SessionFile(workspace)
			if err != nil {
				return nil, "", err
			}
			fresh := cfg
			fresh.SessionFile = next
			replacement, err := session.New(fresh)
			if err != nil {
				return nil, "", err
			}
			return replacement, next, nil
		},
		Workspace:     workspace,
		SessionFile:   transcript,
		Resumed:       resumed,
		Notice:        notice,
		ContextWindow: cfg.ContextWindow,
		History:       recall,
		DraftFile:     draft,
	})
}

// openV3Agent opens the session this run writes, and NEVER crashes on a
// contended one.
//
// A resumed transcript is this directory's most recent, which is exactly the
// file a second window in the same directory is already holding open. Session
// answers that with [session.ErrSessionLocked] — the right answer, since two
// writers on one journal is a corrupted journal — and the right thing to do
// with it is not to refuse to start. A person who opened a second window asked
// for a second conversation; they get one, in a new file, and are told which
// road they came down. The alternative is a launcher that fails on the second
// terminal for a reason nobody outside this tree could guess.
//
// It returns the config as it ended up, because the session file may have moved.
func openV3Agent(cfg session.Config, workspace string) (*session.Agent, session.Config, string, error) {
	agent, err := session.New(cfg)
	if err == nil {
		return agent, cfg, "", nil
	}
	if !errors.Is(err, session.ErrSessionLocked) {
		return nil, cfg, "", err
	}
	next, nameErr := newV3SessionFile(workspace)
	if nameErr != nil {
		return nil, cfg, "", err
	}
	cfg.SessionFile = next
	agent, err = session.New(cfg)
	if err != nil {
		return nil, cfg, "", err
	}
	return agent, cfg, "session open elsewhere — started a new one", nil
}

// ── governance: what a session may do, on whose models, for how much ────────
//
// The settings rows and one flag reach internal/session here, and this is the
// ONLY place they do. Each is a seam that already exists on the other side —
// [approval.Policy], [roles.Source], the spend rail, [search.Resolve]'s pair —
// so the mapping is a translation and never a second policy.
//
// EVERY PARSE ERROR STOPS THE LAUNCH, with the row named. A tool gate that
// silently ignored the line it could not read would be a gate that opens for
// exactly the reason nobody would think to check: a typo in the file that was
// meant to close it.
//
// THE PROJECT LAYER ENTERS HERE. cfg.Workspace is the directory this session
// runs in, so <workspace>/.openaf/config.json is the repository's own answer to
// these rows, and every read below resolves project → profile → default
// (internal/config's projectconfig.go). A caller with no workspace — a test, a
// door that has not resolved a directory — gets an empty layer rather than a
// lookup in whatever directory the process happens to be sitting in.
func applyV3Governance(cfg session.Config, profileDir string, yolo bool) (session.Config, error) {
	workspace := strings.TrimSpace(cfg.Workspace)
	policy, err := v3Policy(workspace, profileDir, yolo)
	if err != nil {
		return cfg, err
	}
	source, err := v3RolesSource(workspace, profileDir)
	if err != nil {
		return cfg, err
	}
	rail, err := config.ProjectFloatAt(workspace, profileDir, config.KeySpendRail)
	if err != nil {
		return cfg, err
	}
	cfg.ApprovalPolicy = policy
	cfg.RolesSource = source
	cfg.SpendRailUSD = rail
	// The guardian (internal/session's guardian.go) reads PROFILE-ONLY, unlike
	// the two rows above it, and the reason is the one that keeps the search keys
	// out of the project layer too: a repository that could turn this on would be
	// appointing a stand-in for a visitor who never agreed to have one. The rules
	// a repository may state are still the rules — it can say what to ask about;
	// it cannot say who answers.
	cfg.Guardian = config.GuardianEnabledAt(profileDir)
	cfg.SearchProvider, cfg.SearchFetcher = v3Search(profileDir)
	return cfg, nil
}

// v3Search resolves the web-search pair this session's belt calls through: the
// three settings rows in, [search.Resolve]'s answer out.
//
// IT RETURNS NO ERROR, and that is a statement about the layer rather than an
// omission. Every rung of the resolution ladder ends in a plug that needs no
// key (internal/search states this and tests it), so there is no configuration
// — no key, a stale pin, a garbled row — that can leave a person unable to look
// something up. A missing key is not a failure but a rung; a pin naming a plug
// this build does not have falls through to auto rather than taking search
// away. The only outcome this call cannot produce is a launch that fails
// because of search, which is the correct set of outcomes for an accessory.
//
// A nil half is therefore not an error either. It is what a build whose
// registry is empty answers, and internal/session reads it as "leave that tool
// off the belt" — a model that is never told about a tool it cannot reach.
//
// THE ROWS DO NOT GO THROUGH THE PROJECT LAYER, unlike the governance rows
// above, and the keys are why: a repository that could answer search.exaKey
// could spend a visitor's Exa credit, and one that could answer search.provider
// could redirect where a visitor's questions are sent by being cloned. Both are
// the PERSON's rows in the sense internal/config's allowlist means it, so they
// resolve profile-and-environment only.
func v3Search(profileDir string) (search.Provider, search.Fetcher) {
	return search.Resolve(v3SearchOptions(profileDir))
}

// v3SearchOptions is the mapping itself, split out so it can be read and tested
// without a registry: the pin from the choice row (auto meaning no pin, which
// internal/search spells as the empty string), and the two credentials from the
// environment or the sheet.
func v3SearchOptions(profileDir string) search.Options {
	pin := config.SearchProviderAt(profileDir)
	if pin == config.SearchProviderAuto {
		pin = ""
	}
	return search.Options{
		Provider: pin,
		ExaKey:   config.ExaKeyAt(profileDir),
		JinaKey:  config.JinaKeyAt(profileDir),
	}
}

// v3Policy builds the tool gate from the two approval rows.
//
// The mode row cannot fail — an unreadable value there reads as the strictest
// of the three (internal/config), which is the one direction a garbled setting
// may be wrong in. The exceptions row can, and does, loudly.
//
// --yolo replaces the DEFAULT and nothing else. A person who wrote
// `bash:prompt` still gets asked about bash: the flag is "stop asking me about
// the ordinary things", not "forget what I wrote down".
func v3Policy(workspace, profileDir string, yolo bool) (*approval.Policy, error) {
	mode, err := config.ProjectStringAt(workspace, profileDir, config.KeyToolApprovalMode)
	if err != nil {
		return nil, err
	}
	if yolo {
		mode = string(approval.ActionAllow)
	}
	raw := map[string]any{"default": mode}
	text, err := config.ProjectStringAt(workspace, profileDir, config.KeyToolApprovals)
	if err != nil {
		return nil, err
	}
	// The repository's exceptions REPLACE the person's, whole (internal/config
	// states the merge law): a rule set assembled from two files is a rule set
	// neither file's reader could read back.
	if text != "" {
		tools, err := config.ParseToolApprovals(text)
		if err != nil {
			return nil, fmt.Errorf("settings row %q: %w", config.KeyToolApprovals, err)
		}
		exceptions := make(map[string]any, len(tools))
		for tool, action := range tools {
			exceptions[tool] = action
		}
		raw["tools"] = exceptions
	} else {
		// A fresh install asks about what can change the system and nothing
		// else: pure reads (read, grep, find, ls) are allowed, so the blanket
		// mode's prompt lands on bash/edit/write and the critical-command
		// table still overrides. The person's own exceptions row replaces
		// these built-ins wholesale — their rules, their responsibility.
		raw["tools"] = map[string]any{
			"read": "allow", "grep": "allow", "find": "allow", "ls": "allow",
			// jobs list/output are reads on the person's own processes; kill
			// inherits the blanket mode, which asks.
			"jobs": "allow",
		}
	}
	policy, err := approval.Load(raw)
	if err != nil {
		return nil, fmt.Errorf("settings rows %q and %q: %w",
			config.KeyToolApprovalMode, config.KeyToolApprovals, err)
	}
	return &policy, nil
}

// v3RolesSource is the closure internal/roles reads its ladder through: the two
// tier models under [roles.TierKey], the per-role pins under [roles.PinKey].
//
// It is resolved ONCE, at boot, rather than read per call: an auxiliary model
// that changed under a running session would make two calls in one conversation
// answer to different settings, and the row itself says a change lands on the
// next session.
func v3RolesSource(workspace, profileDir string) (func(string) (string, bool), error) {
	low, err := config.ProjectStringAt(workspace, profileDir, config.KeyTierLowModel)
	if err != nil {
		return nil, err
	}
	high, err := config.ProjectStringAt(workspace, profileDir, config.KeyTierHighModel)
	if err != nil {
		return nil, err
	}
	values := map[string]string{
		roles.TierKey(roles.TierLow):  low,
		roles.TierKey(roles.TierHigh): high,
	}
	text, err := config.ProjectStringAt(workspace, profileDir, config.KeyModelRoles)
	if err != nil {
		return nil, err
	}
	// The pins replace wholesale for the reason the approvals do, one line up.
	if text != "" {
		pins, err := config.ParseModelRoles(text)
		if err != nil {
			return nil, fmt.Errorf("settings row %q: %w", config.KeyModelRoles, err)
		}
		for role, model := range pins {
			values[roles.PinKey(roles.Role(role))] = model
		}
	}
	return func(key string) (string, bool) {
		value, ok := values[key]
		if !ok || strings.TrimSpace(value) == "" {
			return "", false
		}
		return value, true
	}, nil
}

// v3Dir is ~/.aforge/v3: the directory this surface keeps its own files in —
// the model cache, the history list, the drafts. The sessions live one level
// under it, per workspace (see [v3SessionDir]).
func v3Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	dir := filepath.Join(home, ".aforge", "v3")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create session directory: %w", err)
	}
	return dir, nil
}

// v3Catalog is the ONE question this door asks a model catalog, and it is a
// question that never waits. It is an interface rather than *catalog.Catalog so
// a test can answer it with rows of its own — no cache file, no fetch, no
// fifteen-second timeout in a unit test.
type v3Catalog interface {
	// ModelsNow is the catalog's rows if it already has them, and nil while a
	// lazily loaded one is still warming.
	ModelsNow() []catalog.Model
}

// v3Models is the catalog as the v3 surface wants it: id, window, the three
// per-token prices and the arena score, in the catalog's own order, and only the
// models that can hold a conversation.
//
// The prices ride along because the surface has to price something the session
// cannot: what a turn's prompt-cache reads saved, which is cached tokens times
// the gap between the prompt price and the cache-read price (internal/tui3's
// savings note). They come off the /models fetch that already happens, so
// carrying them costs one more field per row in a file that is already written.
//
// Nil while the catalog is warming — which is not a failure but the picker's
// cue to read ~/.aforge/v3/models.json and then its built-ins (internal/tui3
// models.go states that order and applies it).
func v3Models(models v3Catalog) []tui3.Model {
	if models == nil {
		return nil
	}
	rows := models.ModelsNow()
	out := make([]tui3.Model, 0, len(rows))
	for _, row := range rows {
		if !v3AnswersText(row.OutputModalities) {
			continue
		}
		model := tui3.Model{
			ID:            row.ID,
			ContextLength: row.ContextLength,
			ArenaElo:      row.ArenaElo,
			Output:        row.OutputModalities,
			// The published answer to "may this call carry a reasoning knob",
			// and the only thing that lets the picker offer ctrl+t on a row.
			// Either spelling counts: a model that takes `reasoning` can be
			// asked to think, and one that takes `reasoning_effort` can be told
			// how hard — the surface offers levels on both and lets the adapter
			// resolve what actually travels (catalog's ReasoningWord states the
			// same three states in words).
			Reasoning: row.Reasons() || row.ReasoningLevels(),
		}
		// PriceUnknown is OpenRouter's "-1", which it uses for its own routers:
		// they charge whatever the model they pick charges, and nobody yet knows
		// what that is. Passing the parsed numbers through anyway would tell the
		// surface a router's prompt token is free and let it report a saving that
		// is not a fact (catalog.Model.PriceUnknown).
		if !row.PriceUnknown {
			model.PromptPrice = row.PromptPrice
			model.CompletionPrice = row.CompletionPrice
			model.CacheReadPrice = row.CacheReadPrice
		}
		out = append(out, model)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// v3AnswersText keeps the models a chat surface can actually talk to. The
// image, speech, music and video rows are real models and belong to other
// tools; in this list they would be hundreds of names nobody can pick.
//
// A row that declares nothing is KEPT: silence is a row cached before
// modalities were recorded, not a model that answers in nothing.
func v3AnswersText(outputs []string) bool {
	if len(outputs) == 0 {
		return true
	}
	// Text-out and NOTHING else: an image model that captions what it draws
	// publishes ["image","text"], and letting it through puts a drawing model
	// in a chat picker. The door's law is "a model you can talk to", and a
	// published modality list is the only honest witness to it.
	for _, modality := range outputs {
		if !strings.EqualFold(strings.TrimSpace(modality), "text") {
			return false
		}
	}
	return true
}

// v3SeesImages is the vision gate: the closure internal/session asks before it
// assembles a message with pictures in it (session.Config.SupportsImages, and
// internal/tui3's attachment tray is what makes one askable).
//
// It answers from the catalog's published `architecture.input_modalities` and
// from nothing else — no id patterns, no vendor guesses. A model that says it
// reads images reads images; anything else is a "no" this door can defend.
//
// IT NEVER WAITS, for the reason every other question this file asks a catalog
// never waits: the rows are read through [v3Catalog.ModelsNow], which is nil
// while a lazily loaded catalog is still warming. That is why it is a closure
// read per message rather than a value resolved at boot — a cold cache would
// otherwise pin "cannot see" onto a session for its whole life, and the answer
// is wanted at the moment somebody attaches a photo, which is minutes later.
func v3SeesImages(models v3Catalog) func(string) bool {
	return func(model string) bool {
		if models == nil {
			return false
		}
		model = strings.TrimSpace(model)
		if model == "" {
			return false
		}
		for _, row := range models.ModelsNow() {
			if strings.EqualFold(strings.TrimSpace(row.ID), model) {
				return v3ReadsImages(row.InputModalities)
			}
		}
		return false
	}
}

// v3ReadsImages is the modality test itself.
//
// SILENCE IS NO, which is the opposite of what [v3AnswersText] does with it and
// deliberately so. There, a row that declares nothing is kept because the cost
// of being wrong is a name missing from a picker. Here the cost of being wrong
// is a message assembled with a base64 photo in it, sent to a model that cannot
// read one, and answered with a provider error about a content part — so an
// unknown modality is a refusal the person can act on ("switch to a model with
// vision"), which is the honest thing to say when nobody has said otherwise.
func v3ReadsImages(inputs []string) bool {
	for _, modality := range inputs {
		if strings.EqualFold(strings.TrimSpace(modality), "image") {
			return true
		}
	}
	return false
}

// v3Window fills session.Config.ContextWindow: how many tokens this session's
// model accepts, according to whatever the catalog can say WITHOUT a fetch.
//
// Zero when nobody can say — a cold cache, an id the catalog has never carried,
// a row that published no figure — and zero is the right answer to hand the
// session, which then keeps its own conservative default rather than sizing
// compaction off a guess. [warmV3Models] corrects it in place once the catalog
// resolves.
func v3Window(models v3Catalog, model string) int {
	return v3ContextWindow(v3Models(models), model)
}

func v3ContextWindow(models []tui3.Model, model string) int {
	model = strings.TrimSpace(model)
	for _, candidate := range models {
		if strings.EqualFold(candidate.ID, model) {
			return candidate.ContextLength
		}
	}
	return 0
}

// warmV3Models is the after-the-fact half, and it runs off the launch path
// because it WAITS: the catalog's own resolution, which on a cold cache is a
// network round-trip.
//
// It does two things when that lands. The session learns the real window of the
// model it started on, so a 1M-token model stops compacting at 128k — but only
// if the person has not already switched models, because a later choice is a
// better fact than this one. And the picker's cache is refreshed, so the NEXT
// launch on this machine opens the full list in its first frame.
//
// The cache is written only for rows that actually came off the network
// (FetchedAt is zero for the built-in fallbacks), which is what keeps a machine
// that has never reached OpenRouter from caching five hardcoded names as if
// they were the catalog.
func warmV3Models(models *catalog.Catalog, agent *session.Agent, started string) {
	if models == nil || agent == nil {
		return
	}
	if window := models.ContextLength(started); window > 0 && agent.Model() == started {
		agent.SetContextWindow(window)
	}
	if models.FetchedAt().IsZero() {
		return
	}
	_ = tui3.WriteModelCache(v3Models(models))
}

// runChatV3Once is the smoke-test door: one message, plain text out, no
// terminal ownership. Everything the surface would draw as chrome goes to
// stderr and only what the model said goes to stdout, so a probe can compare
// stdout with the sentence it asked for.
func runChatV3Once(cfg session.Config, text, level string, resumed bool) error {
	if resumed && cfg.SessionFile != "" {
		fmt.Fprintln(os.Stderr, "resumed "+cfg.SessionFile)
	}
	agent, cfg, notice, err := openV3Agent(cfg, cfg.Workspace)
	if err != nil {
		return err
	}
	agent.SetReasoning(level)
	if notice != "" {
		fmt.Fprintln(os.Stderr, notice+": "+cfg.SessionFile)
	}
	defer func() { _ = agent.Close() }()

	events, err := agent.Submit(context.Background(), text)
	if err != nil {
		return err
	}
	// wrote tracks whether the reply has begun, so a tool line never opens the
	// output with a stray blank line and never lands mid-sentence.
	wrote := false
	newline := func() {
		if wrote {
			fmt.Println()
			wrote = false
		}
	}
	var failure error
	for event := range events {
		switch event.Kind {
		case session.EventTextDelta:
			if event.Text == "" {
				continue
			}
			fmt.Print(event.Text)
			wrote = !strings.HasSuffix(event.Text, "\n")

		case session.EventToolBegin:
			newline()
			fmt.Fprintln(os.Stderr, "tool: "+tui3.ToolGloss(event.Tool, event.Hint))

		case session.EventToolFailed:
			newline()
			reason := event.Hint
			if reason == "" && event.Err != nil {
				reason = event.Err.Error()
			}
			fmt.Fprintln(os.Stderr, "tool: "+event.Tool+" failed: "+reason)

		case session.EventCompacted:
			newline()
			fmt.Fprintln(os.Stderr, "compacted: "+event.Hint)

		case session.EventError:
			failure = event.Err
			if failure == nil {
				failure = fmt.Errorf("session: the turn failed without a reason")
			}
		}
	}
	newline()
	return failure
}

// resolveV3Session answers which transcript this run writes and whether it was
// found rather than made. An explicit --session is taken as given (a path a
// person named is a path they mean, existing or not); otherwise this
// directory's most recent transcript is resumed, and a directory that has
// never held a session gets a new one.
func resolveV3Session(explicit, workspace string) (string, bool, error) {
	if explicit != "" {
		path, err := expandHome(explicit)
		if err != nil {
			return "", false, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", false, fmt.Errorf("create session directory: %w", err)
		}
		_, err = os.Stat(path)
		return path, err == nil, nil
	}
	dir, err := v3SessionDir(workspace)
	if err != nil {
		return "", false, err
	}
	if latest := latestV3Session(dir); latest != "" {
		return latest, true, nil
	}
	path, err := newV3SessionFile(workspace)
	return path, false, err
}

// v3SessionDir is where this directory's transcripts live:
// ~/.aforge/v3/sessions/<cwd with its separators turned to dashes>. The
// encoding keeps one flat level per workspace and stays readable — the point
// of a session file is that a person can find it.
func v3SessionDir(workspace string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	dir := filepath.Join(home, ".aforge", "v3", "sessions", encodeWorkspace(workspace))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create session directory: %w", err)
	}
	return dir, nil
}

func encodeWorkspace(workspace string) string {
	encoded := strings.ReplaceAll(filepath.Clean(workspace), string(filepath.Separator), "-")
	encoded = strings.ReplaceAll(encoded, ":", "-")
	if !strings.HasPrefix(encoded, "-") {
		encoded = "-" + encoded
	}
	return encoded
}

// newV3SessionFile names a fresh transcript: the moment it began and a short
// random tail, so two windows opened in the same second in the same directory
// never write the same file.
func newV3SessionFile(workspace string) (string, error) {
	dir, err := v3SessionDir(workspace)
	if err != nil {
		return "", err
	}
	tail := make([]byte, 3)
	if _, err := rand.Read(tail); err != nil {
		return "", fmt.Errorf("name session file: %w", err)
	}
	name := time.Now().Format("20060102-150405") + "_" + hex.EncodeToString(tail) + ".jsonl"
	return filepath.Join(dir, name), nil
}

// latestV3Session is the most recently written transcript in dir, or "".
func latestV3Session(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	type candidate struct {
		path string
		at   time.Time
	}
	var found []candidate
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		found = append(found, candidate{path: filepath.Join(dir, entry.Name()), at: info.ModTime()})
	}
	if len(found) == 0 {
		return ""
	}
	sort.Slice(found, func(i, j int) bool { return found[i].at.After(found[j].at) })
	return found[0].path
}

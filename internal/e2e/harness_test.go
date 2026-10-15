//go:build e2e

// Package e2e drives the engine — internal/session's Agent and the work it
// hands out — against a REAL model, assembled the way cmd/codeaf assembles it.
//
// WHY IT IS A PACKAGE OF ITS OWN AND NOT ANOTHER FILE IN internal/session.
// Everything below drives the engine through the doors a surface has: New,
// Submit, StartTask, ResolveConsent, Transcript, Close. Nothing here touches an
// unexported field, so a scenario that passes here is a scenario the product
// can actually reach — which is the whole point of an end-to-end lane, and the
// one thing an in-package test cannot promise.
//
// The lanes that run on this file's [world] are the manual (manual_e2e_test.go
// and manual_wire_test.go), conversation search, task families, the worktree
// contract, custody and the parked job. The tmux suite beside them drives the
// built binary instead and has its own fixture (tmux_test.go).
//
// WHAT IT COSTS. Every turn rides deepseek/deepseek-v4-flash through
// OpenRouter: the throwaway profile below pins the talk row and both auxiliary
// tiers to the same model, so a lane is a few cents. The build tag keeps it
// out of `go test ./...` and the key gate keeps it out of a machine with none.
//
//	go test -tags e2e -run TestManual -v -timeout 30m ./internal/e2e/
package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
)

// e2eModel is the model the person asked for. The transcripts spell it
// "~deepseek/deepseek-v4-flash-latest"; the leading "~" is OpenRouter's own
// alias marker and is dropped everywhere in this build (internal/catalog's
// normalizeID, internal/provider's normalizeModel), so the plain slug is the
// same model and is what the settings row takes.
var e2eModel = liveVerificationModel()

// One explicit override reaches the conversation, worker, auxiliary-role and
// fallback pins together. A lane must still verify its recorded model receipts.
func liveVerificationModel() string {
	if model := strings.TrimSpace(os.Getenv("CODEAF_E2E_MODEL")); model != "" {
		return model
	}
	return "deepseek/deepseek-v4-flash"
}

// personConfig is the credentials this lane borrows: the profile in the
// person's OWN codeaf home, read before the throwaway one is put in front of
// it. It is COPIED into that throwaway CODEAF_HOME and never read out loud,
// because it holds the person's key.
//
// IT IS RESOLVED, NOT SPELLED. It was a constant naming one machine's home
// directory, and on every other machine every lane skipped with "no provider
// credentials", which reads as a key that was never set rather than as a path
// that was never yours. Resolving through [home.InheritedDir] honours the same
// override the binary does, so a person who runs codeaf out of CODEAF_HOME
// runs this lane out of it too.
//
// It is the INHERITED root and not [home.Dir], which hands a test binary a
// throwaway root of its own so that a suite cannot write into a person's state
// (internal/home/undertest.go). Reading a credential the person already has is
// the one thing this lane genuinely wants from that root, and Dir would have
// pointed it at an empty directory — every model-driven subtest skipping with
// "no provider credentials" on a machine that has them.
func personConfig() string {
	return filepath.Join(home.InheritedDir(), "config.json")
}

// ── the throwaway machine ───────────────────────────────────────────────────

// world is one disposable codeaf home with the person's provider credentials
// in it: the settings the door loads and the roles source the door resolves
// auxiliary models through.
//
// IT KEEPS NO AMBIENT STORE. It held the standing orders' store until
// automations replaced them, and no lane on it is about automations: those are
// read off a real screen, store and clock in automations_e2e_test.go.
type world struct {
	t        *testing.T
	home     string
	settings config.Config
	// spent is what every agent in this run has cost, summed by [world.bill]
	// so the report can quote one figure.
	mu    sync.Mutex
	spent float64
}

// newWorld builds it. CODEAF_HOME is the one seam that moves every path
// (internal/home), so the profile, the projects and the artifacts index all
// land under a directory the test owns.
func newWorld(t *testing.T) *world {
	t.Helper()
	// THE KEY IS RESOLVED THE WAY THE PRODUCT RESOLVES IT (#576): the two
	// variables and then the profile's own `api_key` row, which is where a key
	// pasted into the first-run setup lives and the one road a gate on a single
	// variable could not see.
	key := liveKey(t)
	// The person's own profile is located BEFORE the override lands: after the
	// Setenv below, home.Dir is the throwaway.
	profile := personConfig()
	dir := t.TempDir()
	t.Setenv(home.EnvVar, dir)
	// CODEAF_PROFILE_DIR is the narrow override that would move the profile
	// out from under CODEAF_HOME. Cleared, so config.BudgetConfigPath("")
	// answers <dir>/config.json — the file copied one line down.
	t.Setenv("CODEAF_PROFILE_DIR", "")

	// A PROFILE THAT IS NOT THERE IS NO LONGER A SKIP. It was, and that made a
	// second way for this lane to go green without running: a machine whose key
	// is exported in the shell and has never written a profile file is a machine
	// the product runs on perfectly well.
	if raw, err := os.ReadFile(profile); err == nil {
		if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o600); err != nil {
			t.Fatalf("copy the profile: %v", err)
		}
	} else if !os.IsNotExist(err) {
		t.Fatalf("read the profile at %s: %v", profile, err)
	}
	// And the throwaway home carries the key itself, whichever road it came
	// down, so every door this lane opens under it authenticates the same way.
	if err := config.WriteAPIKey("", key); err != nil {
		t.Fatalf("write the key into the throwaway profile: %v", err)
	}

	// The model, chosen the way a person chooses one: the model.talk row, which
	// is what cmd/codeaf's v3TalkModel reads before it falls back to the
	// environment. The LOW and HIGH tiers are pinned to the same model, so every
	// auxiliary call a lane's turn makes rides the model this run is about
	// rather than whatever the person happens to have there.
	// The talk row goes through its own writer: the registry's model slots
	// refuse without a live SetModel seam, because changing the model a RUNNING
	// conversation rides is /model's job and not a settings write.
	if err := config.WriteChatModel("", e2eModel); err != nil {
		t.Fatalf("write %s: %v", config.KeyChatModel, err)
	}
	registry := config.NewSettings(config.SettingsOptions{})
	for _, key := range []string{config.KeyTierLowModel, config.KeyTierHighModel} {
		row, found := registry.Row(key)
		if !found {
			t.Fatalf("the settings registry has no row %q", key)
		}
		if err := row.Apply(e2eModel); err != nil {
			t.Fatalf("write %s: %v", key, err)
		}
	}

	// AND THE MARKS ARE PINNED TO THE PLAIN TIER. A default terminal now draws
	// the vocabulary's Font Awesome icons (internal/tui2/tokens' nerd-font
	// tier, which tokens.DetectGlyphSet turns on for anything that is not a
	// Linux console, Apple Terminal or a CJK locale — and tmux's
	// TERM=xterm-256color is none of those). Those are private-use codepoints:
	// a capture-pane of one is a byte nobody reading this suite could recognize
	// and no needle could honestly pin. The plain floor is what this suite
	// asserts against, and a person can see it by choosing `plain` in the same
	// Display row.
	if row, found := registry.Row(config.KeyIcons); found {
		if err := row.Apply(config.IconsPlain); err != nil {
			t.Fatalf("write %s: %v", config.KeyIcons, err)
		}
	} else {
		t.Fatalf("the settings registry has no row %q", config.KeyIcons)
	}

	settings, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	// v3TalkModel's own order, mirrored: what was named, then the saved row,
	// then the environment's default.
	if chosen := config.ChatModelAt(settings.ProfileDir); chosen != e2eModel {
		t.Fatalf("the door would open on %q, not %q", chosen, e2eModel)
	}
	t.Logf("CODEAF_HOME=%s  model=%s", dir, e2eModel)
	return &world{t: t, home: dir, settings: settings}
}

// bill adds one agent's spend to the run's total.
func (w *world) bill(what string, usd float64) {
	w.mu.Lock()
	w.spent += usd
	total := w.spent
	w.mu.Unlock()
	w.t.Logf("SPEND %s $%.6f (run total $%.6f)", what, usd, total)
}

// rolesSource mirrors cmd/codeaf's v3Crew.snapshot: the two project-layer tiers,
// the two profile-only ones, and the pins on top.
func (w *world) rolesSource(workspace string) roles.Source {
	t := w.t
	low, err := config.ProjectStringAt(workspace, w.settings.ProfileDir, config.KeyTierLowModel)
	if err != nil {
		t.Fatalf("read %s: %v", config.KeyTierLowModel, err)
	}
	high, err := config.ProjectStringAt(workspace, w.settings.ProfileDir, config.KeyTierHighModel)
	if err != nil {
		t.Fatalf("read %s: %v", config.KeyTierHighModel, err)
	}
	values := map[string]string{
		roles.TierKey(roles.TierReflex):     config.TierModelAt(w.settings.ProfileDir, config.ModelTierReflex),
		roles.TierKey(roles.TierMastermind): config.TierModelAt(w.settings.ProfileDir, config.ModelTierMastermind),
		roles.TierKey(roles.TierLow):        low,
		roles.TierKey(roles.TierHigh):       high,
	}
	text, err := config.ProjectStringAt(workspace, w.settings.ProfileDir, config.KeyModelRoles)
	if err != nil {
		t.Fatalf("read %s: %v", config.KeyModelRoles, err)
	}
	if strings.TrimSpace(text) != "" {
		pins, err := config.ParseModelRoles(text)
		if err != nil {
			t.Fatalf("parse %s: %v", config.KeyModelRoles, err)
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
	}
}

// policy mirrors cmd/codeaf's v3Policy: the blanket mode, the built-in floor,
// and whatever the person's own rows say on top of it.
func (w *world) policy(workspace string) *approval.Policy {
	t := w.t
	mode, err := config.ProjectStringAt(workspace, w.settings.ProfileDir, config.KeyToolApprovalMode)
	if err != nil {
		t.Fatalf("read %s: %v", config.KeyToolApprovalMode, err)
	}
	exceptions := map[string]any{
		"read": "allow", "grep": "allow", "find": "allow", "ls": "allow",
		"jobs":     "allow",
		"remember": "allow", "track": "allow", "recall": "allow",
		"manual": "allow", "settings": "allow",
	}
	raw := map[string]any{"default": mode, "tools": exceptions}
	policy, err := approval.Load(raw)
	if err != nil {
		t.Fatalf("approval.Load: %v", err)
	}
	return &policy
}

// place mints one session folder under the throwaway home, meta.json first, the
// way cmd/codeaf's v3MintSession does.
func (w *world) place(bucket, workspace string) session.Place {
	t := w.t
	id := session.NewSessionID()
	dir := filepath.Join(bucket, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mint a session folder: %v", err)
	}
	place := session.Place{Dir: dir, Workspace: workspace}
	_ = session.SaveMeta(dir, session.Meta{
		ID:        id,
		Workspace: workspace,
		Created:   time.Now(),
	})
	return place
}

// projectBucket is where an ordinary conversation of one workspace lives: the
// projects root, and the workspace with its separators turned to dashes —
// cmd/codeaf's encodeWorkspace, spelled again here because that one is in
// package main. Nothing in this package reads the name back; it is the
// product's so that the throwaway home is laid out like a real one.
func (w *world) projectBucket(workspace string) string {
	key := strings.ReplaceAll(filepath.Clean(strings.TrimSpace(workspace)), string(filepath.Separator), "-")
	key = strings.ReplaceAll(key, ":", "-")
	if !strings.HasPrefix(key, "-") {
		key = "-" + key
	}
	bucket := filepath.Join(home.Join("v3", "projects"), key)
	if err := os.MkdirAll(bucket, 0o700); err != nil {
		w.t.Fatalf("make a project bucket: %v", err)
	}
	return bucket
}

// conversationConfig is one live conversation, assembled the way openV3Launch
// plus applyV3Governance assemble one: the person's models and keys, their
// approval rules, their roles, and AskConsent — the door's own fact that
// somebody is watching.
//
// THE AUTOMATIONS SEAM IS LEFT OFF. Nil is no `automation` tool on the belt,
// and nothing a lane on this file asks is about one; a scenario that wanted the
// card would set the seam through its own mutate and answer the card itself.
func (w *world) conversationConfig(workspace string, place session.Place) session.Config {
	return session.Config{
		Workspace:      workspace,
		Model:          config.ChatModelAt(w.settings.ProfileDir),
		APIKey:         w.settings.APIKey,
		BaseURL:        w.settings.BaseURL,
		CompactEnabled: true,
		SessionFile:    place.Transcript(),
		Place:          place,
		WorktreeRoot:   place.Trees(),
		ArtifactsIndex: home.Join("v3", "artifacts.jsonl"),
		ProfileDir:     w.settings.ProfileDir,
		ApprovalPolicy: w.policy(workspace),
		RolesSource:    w.rolesSource(workspace),
		// Somebody is watching: this is what makes a card a question rather
		// than a refusal.
		AskConsent: true,
	}
}

// open builds one live conversation in a fresh folder of the project's bucket,
// and answers the folder too: the folder's name IS the session id (place.go,
// and openSessionFile's header), which is the only way a test outside this
// package can name a conversation.
func (w *world) open(workspace string, mutate func(*session.Config)) (*session.Agent, session.Place) {
	place := w.place(w.projectBucket(workspace), workspace)
	return w.openAt(workspace, place, mutate), place
}

// openAt builds one live conversation on a folder the caller already made —
// what reopening a conversation does.
func (w *world) openAt(workspace string, place session.Place, mutate func(*session.Config)) *session.Agent {
	t := w.t
	cfg := w.conversationConfig(workspace, place)
	if mutate != nil {
		mutate(&cfg)
	}
	agent, err := session.New(cfg)
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	t.Cleanup(func() {
		w.bill("agent "+agent.Model(), agent.Usage().CostUSD)
		// Close is idempotent, and a scenario that shut this conversation on
		// purpose has already had its effect on the live registry.
		_ = agent.Close()
	})
	return agent
}

// ── driving a turn ──────────────────────────────────────────────────────────

// call is one tool call as a surface saw it.
type call struct {
	Name   string
	Args   string
	Output string
	Failed bool
}

// turn is everything one turn put in front of a person.
type turn struct {
	Reply string
	Calls []call
	Err   error
}

// names is the call order, for a log line and for "did it shell out first".
func (r turn) names() []string {
	out := make([]string, 0, len(r.Calls))
	for _, one := range r.Calls {
		out = append(out, one.Name)
	}
	return out
}

// say drives one turn: submit, answer whatever question comes up, log
// everything.
func (w *world) say(agent *session.Agent, text string) turn {
	t := w.t
	t.Logf("YOU → %s", text)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	events, err := agent.Submit(ctx, text)
	if err != nil {
		t.Fatalf("submit %q: %v", text, err)
	}
	return w.drain(agent, "turn", events)
}

// drain reads one event stream to its end.
//
// NOBODY IS AT A KEYBOARD, AND A QUESTION NOBODY ANSWERS IS A HANG. So every
// question a turn raises is answered here, the way the person's own rules
// already would: a consent request is allowed, and an automation card — which
// no conversation on this file has the seam to raise unless a scenario gave it
// one — saves nothing.
func (w *world) drain(agent *session.Agent, what string, events <-chan session.Event) turn {
	t := w.t
	var out turn
	var reply strings.Builder
	for event := range events {
		switch event.Kind {
		case session.EventTextDelta:
			reply.WriteString(event.Text)
		case session.EventToolEnd:
			out.Calls = append(out.Calls, call{Name: event.Tool, Args: event.Args, Output: event.Output})
			t.Logf("  CALL %s %s\n    → %s", event.Tool, shorten(event.Args, 600), shorten(event.Output, 800))
		case session.EventToolFailed:
			out.Calls = append(out.Calls, call{Name: event.Tool, Args: event.Args, Output: event.Output, Failed: true})
			t.Logf("  CALL(failed) %s %s\n    → %s", event.Tool, shorten(event.Args, 400), shorten(event.Output, 600))
		case session.EventConsentRequest:
			// The person's own rules allow everything this lane does, so this is
			// belt and braces rather than a policy the test invented.
			t.Logf("  CONSENT asked about %s (%s) — allowing", event.Tool, event.Rule)
			agent.ResolveConsent(event.ID, true)
		case session.EventAutomationProposal:
			if card := event.Automation; card != nil {
				t.Logf("  AUTOMATION card id=%d %q — saving nothing", card.ID, card.Automation.Title)
				agent.ResolveAutomation(card.ID, session.AutomationAnswer{})
			}
		case session.EventError:
			out.Err = event.Err
			t.Logf("  ERROR %v", event.Err)
		}
	}
	out.Reply = strings.TrimSpace(reply.String())
	t.Logf("%s SAID → %s", strings.ToUpper(what), out.Reply)
	t.Logf("  calls: %v", out.names())
	return out
}

// ── small readers ───────────────────────────────────────────────────────────

// transcriptHas answers whether any entry of the conversation carries the text.
func transcriptHas(agent *session.Agent, needle string) (session.DisplayEntry, bool) {
	for _, entry := range agent.Transcript() {
		if strings.Contains(entry.Text, needle) {
			return entry, true
		}
	}
	return session.DisplayEntry{}, false
}

func shorten(text string, limit int) string {
	text = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "\r", " "), "\n", " ⏎ "))
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}

//go:build e2e

// Package e2e drives the ambient side — internal/session's `stand` tool and
// internal/standing's store, ticker, sentinel and runner — against a REAL
// model, assembled the way cmd/aforge assembles it.
//
// WHY IT IS A PACKAGE OF ITS OWN AND NOT ANOTHER FILE IN internal/session.
// Everything below drives the engine through the doors a surface has: New,
// Submit, ResolveStanding, Wakes, Transcript, Close. Nothing here touches an
// unexported field, so a scenario that passes here is a scenario the product
// can actually reach — which is the whole point of an end-to-end lane, and the
// one thing an in-package test cannot promise.
//
// WHAT IT COSTS. Every turn rides deepseek/deepseek-v4-flash through
// OpenRouter, and so does the sentinel: the throwaway profile below pins the
// LOW tier to the same model, so a run of the whole file is a few cents. The
// build tag keeps it out of `go test ./...` and the t.Skip keeps it out of a
// machine with no key.
//
//	go test -tags e2e -run TestStandingE2E -v -timeout 30m ./internal/e2e/
package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// e2eModel is the model the person asked for. The transcripts spell it
// "~deepseek/deepseek-v4-flash-latest"; the leading "~" is OpenRouter's own
// alias marker and is dropped everywhere in this build (internal/catalog's
// normalizeID, internal/provider's normalizeModel), so the plain slug is the
// same model and is what the settings row takes.
const e2eModel = "deepseek/deepseek-v4-flash"

// personConfig is the credentials this lane borrows. It is COPIED into a
// throwaway AFORGE_HOME and never read out loud: it holds the person's key.
const personConfig = "/home/santosh/.aforge/config.json"

// ── the throwaway machine ───────────────────────────────────────────────────

// world is one disposable aforge home with the person's provider credentials
// in it: the settings the door loads, the standing store the door opens, and
// the roles source the door resolves auxiliary models through.
type world struct {
	t        *testing.T
	home     string
	settings config.Config
	store    *standing.Store
	// spent is what every agent and every sentinel call in this run has cost,
	// summed by [world.bill] so the report can quote one figure.
	mu    sync.Mutex
	spent float64
}

// newWorld builds it. AFORGE_HOME is the one seam that moves every path
// (internal/home), so the store, the profile and the artifacts index all land
// under a directory the test owns.
func newWorld(t *testing.T) *world {
	t.Helper()
	if strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")) == "" {
		t.Skip("no OPENROUTER_API_KEY: this lane drives a real model")
	}
	dir := t.TempDir()
	t.Setenv(home.EnvVar, dir)
	// AFORGE_PROFILE_DIR is the narrow override that would move the profile
	// out from under AFORGE_HOME. Cleared, so config.BudgetConfigPath("")
	// answers <dir>/config.json — the file copied one line down.
	t.Setenv("AFORGE_PROFILE_DIR", "")

	raw, err := os.ReadFile(personConfig)
	if err != nil {
		t.Skipf("no provider credentials at %s: %v", personConfig, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o600); err != nil {
		t.Fatalf("copy the profile: %v", err)
	}

	// The model, chosen the way a person chooses one: the model.talk row, which
	// is what cmd/aforge's v3TalkModel reads before it falls back to the
	// environment. The LOW tier is pinned to the same model so the sentinel —
	// registered at roles.TierLow in standing_run.go — rides the model this run
	// is about rather than whatever the person happens to have there.
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

	settings, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	// v3TalkModel's own order, mirrored: what was named, then the saved row,
	// then the environment's default.
	if chosen := config.ChatModelAt(settings.ProfileDir); chosen != e2eModel {
		t.Fatalf("the door would open on %q, not %q", chosen, e2eModel)
	}
	store, err := standing.Open(home.Join("v3", "standing"))
	if err != nil {
		t.Fatalf("standing.Open: %v", err)
	}
	t.Logf("AFORGE_HOME=%s  model=%s  standing root=%s", dir, e2eModel, store.Root())
	return &world{t: t, home: dir, settings: settings, store: store}
}

// bill adds one agent's spend to the run's total.
func (w *world) bill(what string, usd float64) {
	w.mu.Lock()
	w.spent += usd
	total := w.spent
	w.mu.Unlock()
	w.t.Logf("SPEND %s $%.6f (run total $%.6f)", what, usd, total)
}

// rolesSource mirrors cmd/aforge's v3Crew.snapshot: the two project-layer tiers,
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

// policy mirrors cmd/aforge's v3Policy: the blanket mode, the built-in floor,
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
// way cmd/aforge's v3MintSession does.
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

// projectBucket is where an ordinary conversation of one workspace lives. The
// exact spelling of the bucket name is cmd/aforge's business and nothing here
// reads it back, so a stable one-way key is enough.
func (w *world) projectBucket(workspace string) string {
	bucket := filepath.Join(home.Join("v3", "projects"), standing.ProjectKey(workspace))
	if err := os.MkdirAll(bucket, 0o700); err != nil {
		w.t.Fatalf("make a project bucket: %v", err)
	}
	return bucket
}

// conversationConfig is one live conversation, assembled the way openV3Launch
// plus applyV3Governance assemble one: the person's models and keys, their
// approval rules, their roles, the ambient seam, and AskConsent — the door's
// own fact that somebody is watching.
func (w *world) conversationConfig(workspace string, place session.Place) session.Config {
	rail, err := config.DailyBudgetUSDAt(w.settings.ProfileDir)
	if err != nil || rail < 0 {
		rail = 0
	}
	return session.Config{
		Workspace:      workspace,
		Model:          config.ChatModelAt(w.settings.ProfileDir),
		APIKey:         w.settings.APIKey,
		BaseURL:        w.settings.BaseURL,
		SiteURL:        w.settings.SiteURL,
		SiteName:       w.settings.SiteName,
		SiteCategories: w.settings.SiteCategories,
		CompactEnabled: true,
		SessionFile:    place.Transcript(),
		Place:          place,
		WorktreeRoot:   place.Trees(),
		ArtifactsIndex: home.Join("v3", "artifacts.jsonl"),
		ProfileDir:     w.settings.ProfileDir,
		ApprovalPolicy: w.policy(workspace),
		RolesSource:    w.rolesSource(workspace),
		// THE AMBIENT SEAM. Nil here is the whole feature off — no `stand` on
		// the belt — so every scenario below depends on this line.
		Standing: &session.Standing{Store: w.store, DailyRailUSD: rail},
		// Somebody is watching: this is what makes a card a question rather
		// than a refusal (tools_standing.go's askStanding).
		AskConsent: true,
	}
}

// open builds one live conversation in a fresh folder of the project's bucket,
// and answers the folder too: the folder's name IS the session id every firing
// is addressed to (place.go, and openSessionFile's header), which is the only
// way a test outside this package can name a conversation.
func (w *world) open(workspace string, mutate func(*session.Config)) (*session.Agent, session.Place) {
	place := w.place(w.projectBucket(workspace), workspace)
	return w.openAt(workspace, place, mutate), place
}

// openAt builds one live conversation on a folder the caller already made —
// what home's `ask here` does, whose folder is under the standing root, and
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

// ── the ticker, as the door builds one ──────────────────────────────────────

// posture mirrors cmd/aforge's v3StandingPosture: the person's models, keys and
// banked rules, resolved against their HOME rather than any project, with
// AskConsent false and Standing nil — nobody is watching a firing and nothing
// that fires may arm anything else.
func (w *world) posture() session.Config {
	root := w.home
	return session.Config{
		Workspace:      root,
		Model:          config.ChatModelAt(w.settings.ProfileDir),
		APIKey:         w.settings.APIKey,
		BaseURL:        w.settings.BaseURL,
		SiteURL:        w.settings.SiteURL,
		SiteName:       w.settings.SiteName,
		SiteCategories: w.settings.SiteCategories,
		CompactEnabled: true,
		ProfileDir:     w.settings.ProfileDir,
		ArtifactsIndex: home.Join("v3", "artifacts.jsonl"),
		ApprovalPolicy: w.policy(root),
		RolesSource:    w.rolesSource(root),
	}
}

// ticker mirrors cmd/aforge's v3StandingTicker: one store, one sentinel, one
// runner, one idle reader. now is the clock a test holds still or pushes past a
// due moment.
func (w *world) ticker(now func() time.Time) *standing.Ticker {
	posture := w.posture()
	rail, err := config.DailyBudgetUSDAt(w.settings.ProfileDir)
	if err != nil || rail < 0 {
		rail = 0
	}
	return &standing.Ticker{
		Store:        w.store,
		Sentinel:     w.billedSentinel(session.NewStandingSentinel(posture)),
		Runner:       session.NewStandingRunner(posture, w.store.Root()),
		Idle:         session.StandingIdle(),
		DailyRailUSD: rail,
		Now:          now,
	}
}

// billedSentinel is the sentinel with a log line around it, so the report can
// quote what the judgment actually said and what it cost.
func (w *world) billedSentinel(inner standing.Sentinel) standing.Sentinel {
	return func(ctx context.Context, judgment standing.Judgment) (bool, string, float64, error) {
		yes, line, usd, err := inner(ctx, judgment)
		w.t.Logf("SENTINEL %q → yes=%v line=%q err=%v", shorten(judgment.Item.Words, 60), yes, line, err)
		if usd > 0 {
			w.bill("sentinel", usd)
		}
		return yes, line, usd, err
	}
}

// tick runs one pass at a given moment and logs what it decided.
func (w *world) tick(at time.Time) standing.Pass {
	t := w.t
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pass, err := w.ticker(func() time.Time { return at }).Tick(ctx)
	if err != nil {
		t.Fatalf("tick at %s: %v", at.Format(time.RFC3339), err)
	}
	t.Logf("TICK at %s → examined=%d checked=%d fired=%d said=%d needs=%d skipped=%d errors=%d notes=%v",
		at.Format("15:04:05"), pass.Examined, pass.Checked, pass.Fired, pass.Said,
		pass.NeedsYou, pass.Skipped, pass.Errors, pass.Notes)
	return pass
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
	Reply     string
	Calls     []call
	Proposals []session.StandingNotice
	Updates   []session.StandingNotice
	Err       error
}

// stood answers the tool call named, or the zero value.
func (r turn) named(name string) (call, bool) {
	for _, one := range r.Calls {
		if one.Name == name {
			return one, true
		}
	}
	return call{}, false
}

// names is the call order, for a log line and for "did it shell out first".
func (r turn) names() []string {
	out := make([]string, 0, len(r.Calls))
	for _, one := range r.Calls {
		out = append(out, one.Name)
	}
	return out
}

// answerYes is the surface pressing yes on every card it is shown.
func answerYes(session.StandingNotice) session.StandingAnswer {
	return session.StandingAnswer{Approved: true}
}

// answerNo is what a WOKEN turn's cards get. A turn the session started on its
// own is a firing being read out, and nobody asked for a second standing thing:
// a surface that said yes there would arm a duplicate off a line the model
// misread. Saying no is what a person would do, and it keeps the store's
// contents a fact the scenario can still assert on.
func answerNo(session.StandingNotice) session.StandingAnswer {
	return session.StandingAnswer{Approved: false}
}

// say drives one turn: submit, answer whatever card comes up, log everything.
func (w *world) say(agent *session.Agent, text string, answer func(session.StandingNotice) session.StandingAnswer) turn {
	t := w.t
	t.Logf("YOU → %s", text)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	events, err := agent.Submit(ctx, text)
	if err != nil {
		t.Fatalf("submit %q: %v", text, err)
	}
	return w.drain(agent, "turn", events, answer)
}

// drain reads one event stream to its end. It is shared by a submitted turn and
// by a WOKEN one ([Agent.Wakes]), because the two are the same shape.
func (w *world) drain(agent *session.Agent, what string, events <-chan session.Event, answer func(session.StandingNotice) session.StandingAnswer) turn {
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
			// Nobody is at a keyboard, and a question nobody answers is a hang.
			// The person's own rules allow everything this lane does, so this is
			// belt and braces rather than a policy the test invented.
			t.Logf("  CONSENT asked about %s (%s) — allowing", event.Tool, event.Rule)
			agent.ResolveConsent(event.ID, true)
		case session.EventStandingProposal:
			notice := *event.Standing
			out.Proposals = append(out.Proposals, notice)
			t.Logf("  CARD id=%d words=%q when=%q cost=%q guessed=%v kind=%s does=%s rails=%.2f/%d",
				notice.ID, notice.Item.Words, notice.WhenWords, notice.CostWords, notice.Guessed,
				notice.Item.When.Kind, notice.Item.Does.Kind,
				notice.Item.Rails.PerRunUSD, notice.Item.Rails.MaxPerDay)
			reply := session.StandingAnswer{Approved: true}
			if answer != nil {
				reply = answer(notice)
			}
			t.Logf("  CARD answered approved=%v once=%v change=%q", reply.Approved, reply.Once, reply.Change)
			agent.ResolveStanding(notice.ID, reply)
		case session.EventStandingUpdate:
			out.Updates = append(out.Updates, *event.Standing)
			t.Logf("  UPDATE %s: %s", event.Standing.Update, event.Standing.Item.Words)
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

// wokenTurn waits for a turn the SESSION started on its own — what a firing
// steered into a live conversation produces — and drains it.
func (w *world) wokenTurn(agent *session.Agent, lane <-chan (<-chan session.Event), wait time.Duration) (turn, bool) {
	select {
	case events, open := <-lane:
		if !open {
			return turn{}, false
		}
		return w.drain(agent, "woken turn", events, answerNo), true
	case <-time.After(wait):
		return turn{}, false
	}
}

// ── small readers ───────────────────────────────────────────────────────────

// onlyItem is the one item in the store, or a failure naming what is there.
func (w *world) onlyItem() standing.Item {
	t := w.t
	items, err := w.store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("the store holds %d items, want 1: %+v", len(items), items)
	}
	return items[0]
}

// itemFile is the document as it is ON DISK, read raw, so an assertion about
// the file is about the file and not about a value the test kept in a variable.
func (w *world) itemFile(id string) standing.Item {
	t := w.t
	raw, err := os.ReadFile(w.store.ItemPath(id))
	if err != nil {
		t.Fatalf("read %s: %v", w.store.ItemPath(id), err)
	}
	var item standing.Item
	if err := json.Unmarshal(raw, &item); err != nil {
		t.Fatalf("parse %s: %v", w.store.ItemPath(id), err)
	}
	return item
}

// wakeLog is every line the passes have written.
func (w *world) wakeLog() []string {
	raw, err := os.ReadFile(w.store.WakeLogPath())
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// ledgerLines is today's ledger, one decoded entry per line.
func (w *world) ledgerLines(day time.Time) []standing.Entry {
	raw, err := os.ReadFile(w.store.LedgerPath(day))
	if err != nil {
		return nil
	}
	var out []standing.Entry
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry standing.Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		out = append(out, entry)
	}
	return out
}

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

// lastLine is the final non-empty line of a reply — the sentence a person's eye
// actually lands on, which is what a closing line has to be.
func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for at := len(lines) - 1; at >= 0; at-- {
		if line := strings.TrimSpace(lines[at]); line != "" {
			return line
		}
	}
	return ""
}

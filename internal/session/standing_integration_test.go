package session

// Integration tests for the session half of the standing lane: durable-first
// delivery with the pending identity, real adapter errors, the arm-at-yes
// baseline, the strict tri-state verdict reader, and the per-item pinned model
// the background pass must honour. They use the real internal/standing inbox on
// disk and the real store where the test needs a store, and the fake store only
// where a failure has to be injected (standing_test.go's fakeStanding).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/standing"
)

// offlineItem is one item born in an ordinary conversation with no window open
// anywhere: the inbox road, and the only road a restart has.
func offlineItem(dir string) standing.Item {
	return standing.Item{
		ID:    "item-offline",
		Words: "tell me when CI goes red",
		Origin: standing.Origin{
			SessionID:  "nobody-has-this-open",
			Transcript: filepath.Join(dir, "transcript.jsonl"),
		},
	}
}

// DURABLE FIRST, AND THE IDENTITY IS WHAT DEDUPS. A delivery is appended with
// the Pending id before it is acknowledged, a second append of the same id is
// one note, and a replay after the drain is recognised as spent rather than
// shown again.
func TestDeliverDedupsByDurableIdentityAcrossARetryAndARestart(t *testing.T) {
	dir := t.TempDir()
	runner := &standingRunner{}
	item := offlineItem(dir)
	pending := standing.Pending{ID: "delivery-1", Kind: standing.ActionSay, Text: "the last run on main failed"}

	for attempt := 0; attempt < 2; attempt++ {
		if _, err := runner.Deliver(context.Background(), item, pending); err != nil {
			t.Fatalf("Deliver attempt %d: %v", attempt, err)
		}
	}
	// Two attempts, two lines on disk, ONE note to the person: readInbox dedups
	// on the identity.
	notes, err := standing.Drain(dir)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(notes) != 1 || notes[0].ID != "delivery-1" {
		t.Fatalf("a retried delivery reached the person as %+v", notes)
	}

	// A REPLAY AFTER THE DRAIN IS SPENT. The acknowledgement may have been lost;
	// the same identity must not put the line in front of them again.
	if _, err := runner.Deliver(context.Background(), item, pending); err != nil {
		t.Fatalf("replayed Deliver: %v", err)
	}
	again, err := standing.Drain(dir)
	if err != nil {
		t.Fatalf("Drain after replay: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("a settled delivery was shown again: %+v", again)
	}
}

// A REAL INBOX WRITE FAILURE IS REPORTED, NOT SWALLOWED. inbox.jsonl is a
// directory, so the append the real internal/standing code makes fails; the
// error must reach the caller that owns the durable intent.
func TestDeliverPropagatesARealInboxWriteFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(standing.InboxPath(dir), 0o700); err != nil {
		t.Fatalf("seed: %v", err)
	}
	runner := &standingRunner{}
	item := offlineItem(dir)
	if _, err := runner.Deliver(context.Background(), item, standing.Pending{ID: "delivery-2", Kind: standing.ActionSay, Text: "x"}); err == nil {
		t.Fatal("a delivery that could not be written was acknowledged anyway")
	}
	if _, err := runner.Say(context.Background(), item, "x"); err == nil {
		t.Fatal("Say swallowed the inbox write failure")
	}
}

// NO ADDRESS IS AN ERROR. Nothing is open and there is no transcript folder, so
// the line has nowhere a person reads; reporting it keeps the intent alive.
func TestDeliveryWithNoAddressIsReportedNotSwallowed(t *testing.T) {
	runner := &standingRunner{}
	item := standing.Item{ID: "item-bare", Words: "tell me when CI goes red"}
	if _, err := runner.Say(context.Background(), item, "the last run failed"); err == nil {
		t.Fatal("a delivery with nowhere to land read as success")
	}
	if _, err := runner.Deliver(context.Background(), item, standing.Pending{ID: "d", Kind: standing.ActionSay, Text: "x"}); err == nil {
		t.Fatal("Deliver with nowhere to land read as success")
	}
}

// A WINDOW THAT CLOSES BETWEEN THE OFFER AND THE READING CANNOT LOSE THE LINE.
// The live offer is on top of the durable note: after the window is forgotten
// (the restart), the note is still there to fold.
func TestLiveDeliveryIsDurableSoAWindowClosingCannotLoseIt(t *testing.T) {
	workspace := t.TempDir()
	room := standingLiveAgent(t, workspace, nil)
	dir := t.TempDir()
	item := standing.Item{
		ID:        "item-live",
		Words:     "remind me in 1 minute to drink water",
		Workspace: workspace,
		Origin:    standing.Origin{SessionID: room.id, Transcript: filepath.Join(dir, "transcript.jsonl")},
	}
	runner := &standingRunner{}
	if _, err := runner.Deliver(context.Background(), item, standing.Pending{ID: "live-1", Kind: standing.ActionSay, Text: "Time to drink water"}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	// The room heard it.
	if queued := standingQueued(room); len(queued) != 1 {
		t.Fatalf("the open window has %d queued lines", len(queued))
	}
	// The window closes; the durable note survives.
	forgetLiveSession(room)
	notes, err := standing.Drain(dir)
	if err != nil || len(notes) != 1 || notes[0].ID != "live-1" {
		t.Fatalf("a line offered to a window that then closed is %+v (err %v)", notes, err)
	}
}

// ARMING AT THE YES IS WHAT THE SESSION CALLS, and an incomplete scan is visible
// rather than silently deferred to the first wake.
func TestArmingAFileWatchHappensAtTheYes(t *testing.T) {
	store := newFakeStanding(t)
	watch := standing.Item{Words: "tell me when the migration lands", Workspace: t.TempDir(), Rails: standing.Rails{PerRunUSD: 1, MaxPerDay: 5}, Does: standing.Action{Kind: standing.ActionSay, Say: "{{evidence}}"}, When: standing.When{Kind: standing.WhenFile, Glob: "*.sql"}}
	created, err := store.Create(watch)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	agent := &Agent{}
	armed, note := agent.standingArmBaseline(store, created)
	if note != "" {
		t.Fatalf("a complete arm said %q", note)
	}
	if len(store.armed) != 1 || store.armed[0] != created.ID {
		t.Fatalf("the store was armed %v, want exactly %q", store.armed, created.ID)
	}
	if armed.Fingerprint == "" {
		t.Fatal("the armed item came back without a baseline")
	}
	// A NON-FILE ITEM IS NOT ARMED, and a hold is not either.
	rule := standing.Item{Words: "always use tabs", Workspace: t.TempDir(), When: standing.When{Kind: standing.WhenHold}}
	made, err := store.Create(rule)
	if err != nil {
		t.Fatalf("Create rule: %v", err)
	}
	if _, note := agent.standingArmBaseline(store, made); note != "" || len(store.armed) != 1 {
		t.Fatalf("a hold was armed (%q, %v)", note, store.armed)
	}
}

// AND AN INCOMPLETE SCAN IS VISIBLE: a bounded retry, no baseline invented, and
// a NeedsPerson flag the person can read on the item.
func TestAnIncompleteBaselineIsVisibleAndNotSilentLegacy(t *testing.T) {
	store := newFakeStanding(t)
	store.armErr = standing.ErrBaselineIncomplete
	watch := standing.Item{Words: "tell me when the migration lands", Workspace: t.TempDir(), Rails: standing.Rails{PerRunUSD: 1, MaxPerDay: 5}, Does: standing.Action{Kind: standing.ActionSay, Say: "{{evidence}}"}, When: standing.When{Kind: standing.WhenFile, Glob: "*.sql"}}
	created, err := store.Create(watch)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	agent := &Agent{}
	armed, note := agent.standingArmBaseline(store, created)
	if note == "" {
		t.Fatal("an incomplete arm was silent")
	}
	if armed.Fingerprint != "" {
		t.Fatalf("an incomplete arm invented a baseline %q", armed.Fingerprint)
	}
	back, err := store.Get(created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !standing.IsBaselineLine(back.NeedsPerson) {
		t.Fatalf("the item does not carry the visible baseline flag: %q", back.NeedsPerson)
	}
	// THE RETRY IS BOUNDED. Three attempts, and it then gives up visibly rather
	// than scanning forever.
	if len(store.armed) != 0 {
		t.Fatalf("an incomplete arm recorded a baseline: %v", store.armed)
	}
}

// STRICT TRI-STATE PARSING. The verdict is the FIRST WHOLE WORD; a reply that
// merely opens with the letters of one ("yesterday", "nothing") is unknown, not
// yes and not a decided no.
func TestSentinelVerdictReadsWholeTokensNotPrefixes(t *testing.T) {
	cases := []struct {
		reply string
		want  standing.Verdict
	}{
		{"yes \u2014 the last run on main failed", standing.VerdictYes},
		{"Yes. The run failed.", standing.VerdictYes},
		{"no \u2014 nothing has changed", standing.VerdictNo},
		{"No. Nothing changed.", standing.VerdictNo},
		{"unknown \u2014 the log was unreadable", standing.VerdictUnknown},
		{"yesterday the run failed", standing.VerdictUnknown},
		{"nothing has changed since yesterday", standing.VerdictUnknown},
		{"", standing.VerdictUnknown},
		{"maybe", standing.VerdictUnknown},
	}
	for _, one := range cases {
		got, _ := standingVerdictThree(one.reply)
		if got != one.want {
			t.Errorf("standingVerdictThree(%q) = %v, want %v", one.reply, got, one.want)
		}
	}
	// The binary reader keeps mapping only an exact yes to true.
	if yes, _ := standingVerdict("yesterday the run failed"); yes {
		t.Error("the binary reader read a prefix as yes")
	}
	if yes, _ := standingVerdict("yes, it failed"); !yes {
		t.Error("the binary reader missed a whole-word yes")
	}
}

// reloadedPosture is the pass's own config: the profile's role pins, tier
// answers and fallback ladder, all of which differ from the ratifying session.
func reloadedPosture() Config {
	return Config{
		Model: "profile-default",
		RolesSource: func(key string) (string, bool) {
			switch key {
			case "roles.sentinel":
				return "role-pinned-model", true
			case "tiers.low":
				return "tier-low-model", true
			}
			return "", false
		},
		ModelFallbacks: []string{"fallback-a", "fallback-b"},
		NearestModels:  func(string) []string { return []string{"nearest-c"} },
	}
}

// THE PINNED ITEM OVERRIDES THE RELOADED POSTURE ENTIRELY: its own model, no
// role pin, no tier, no fallback chain, no nearest-model guess.
func TestAPinnedItemKeepsTheRatifyingSessionsModelInAReloadedPass(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	parent := reloadedPosture()
	item := standing.Item{
		Words:  "remind me in 1 min to eat medicines",
		Origin: standing.Origin{OneModel: true, PinnedModel: "deepseek/deepseek-v4.1-flash"},
	}

	// WITHOUT THE PIN the ladder hands the judgment a different model \u2014 the
	// role pin \u2014 which is exactly the drift this test exists to refuse.
	unpinned := standing.Item{Words: item.Words, Origin: standing.Origin{}}
	if got, err := standingSentinelModel(parent, unpinned); err != nil || got != "role-pinned-model" {
		t.Fatalf("without a pin the sentinel ran on %q (err %v), want the profile's role pin", got, err)
	}

	cfg := standingPinnedConfig(parent, item)
	if !cfg.OneModel || cfg.Model != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("the pinned posture is %+v", cfg)
	}
	if cfg.RolesSource != nil || cfg.ModelFallbacks != nil || cfg.NearestModels != nil || cfg.RouteCrew != nil {
		t.Fatal("the pinned posture kept a ladder a pinned seat must not carry")
	}
	if got, err := standingSentinelModel(cfg, item); err != nil || got != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("the pinned sentinel ran on %q (err %v)", got, err)
	}
	// AND THE SEAT ITSELF CARRIES NO FALLBACK. This is the client the call is
	// actually made on, and the two ways a one-model run stops being one are
	// both absent from it.
	settings := standingSeatSettings(cfg, cfg.Model)
	if len(settings.Fallbacks) != 0 || settings.NearestModels != nil {
		t.Fatalf("a pinned seat kept a fallback ladder: %+v", settings)
	}
	// THE UNPINNED SEAT ON THE SAME MODEL KEEPS THE PROFILE'S LADDER, so the two
	// are genuinely different clients and cannot share one cache entry.
	unpinnedSettings := standingSeatSettings(parent, "deepseek/deepseek-v4.1-flash")
	if len(unpinnedSettings.Fallbacks) == 0 {
		t.Fatal("the unpinned seat lost the profile's fallbacks")
	}
	if standingSeatKey("same-model", true) == standingSeatKey("same-model", false) {
		t.Fatal("a pinned and an unpinned seat on one model share a client cache key")
	}
	if standingSeatKey(settings.Model, true) == standingSeatKey(unpinnedSettings.Model, false) {
		t.Fatal("the pinned seat's key collides with the unpinned seat's after the wire model is resolved")
	}
}

// countingModels is a local fake provider: it records every model named on the
// wire and answers a verdict. No other model is ever called, so a test can prove
// what the real [NewStandingSentinelVerdict] actually sent.
func countingModels(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var (
		mu     sync.Mutex
		models []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !answersChatOnly(w, r) {
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		models = append(models, body.Model)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","model":"sim","choices":[{"index":0,"finish_reason":"stop",` +
			`"message":{"role":"assistant","content":"yes the last run on main failed"}}]}`))
	}))
	t.Cleanup(server.Close)
	return server, &models
}

// THE REAL SENTINEL CLIENT HONOURS THE PIN ON THE WIRE. Unpinned, the reloaded
// posture's role pin answers; pinned, the item's own frozen model answers and no
// other model is ever named — not the role pin, not the fallback chain, not the
// catalog's nearest guess.
func TestThePinnedSentinelNeverSendsAnotherModel(t *testing.T) {
	server, models := countingModels(t)
	parent := Config{
		Model:   "shared-model",
		APIKey:  "test",
		BaseURL: server.URL,
		RolesSource: func(key string) (string, bool) {
			if key == "roles.sentinel" {
				return "role-pinned-alt", true
			}
			return "", false
		},
		ModelFallbacks: []string{"fallback-alt"},
		NearestModels:  func(string) []string { return []string{"nearest-alt"} },
	}
	sentinel := NewStandingSentinelVerdict(parent)
	ctx, cancel := deadline(10 * time.Second)
	defer cancel()

	unpinned := standing.Item{ID: "u", Words: "tell me when CI goes red"}
	verdict, _, _, err := sentinel(ctx, standing.Judgment{Item: unpinned, Evidence: "the run is red"})
	if err != nil {
		t.Fatalf("unpinned sentinel: %v", err)
	}
	if verdict != standing.VerdictYes {
		t.Fatalf("the fake provider's yes read as %v", verdict)
	}
	if (*models)[0] != "role-pinned-alt" {
		t.Fatalf("the unpinned seat did not use the reloaded role pin: %v", *models)
	}

	pinned := standing.Item{ID: "p", Words: unpinned.Words,
		Origin: standing.Origin{OneModel: true, PinnedModel: "shared-model"}}
	before := len(*models)
	if _, _, _, err := sentinel(ctx, standing.Judgment{Item: pinned, Evidence: "the run is red"}); err != nil {
		t.Fatalf("pinned sentinel: %v", err)
	}
	seen := (*models)[before:]
	if len(seen) != 1 || seen[0] != "shared-model" {
		t.Fatalf("the pinned seat named %v, want only the ratifying conversation's model", seen)
	}
	for _, alt := range []string{"role-pinned-alt", "fallback-alt", "nearest-alt"} {
		for _, got := range seen {
			if got == alt {
				t.Fatalf("a pinned judgment called the alternative model %q", alt)
			}
		}
	}
}

// AND THE CLIENT CACHE DOES NOT LEAK A LADDER. An unpinned seat on the SAME
// model is built first, with the profile's fallback seams; a later pinned seat on
// that same model must be a different client and must still send only the pinned
// model.
func TestAnUnpinnedSeatOnTheSameModelDoesNotLeakIntoAPinnedOne(t *testing.T) {
	server, models := countingModels(t)
	parent := Config{
		Model:          "shared-model",
		APIKey:         "test",
		BaseURL:        server.URL,
		ModelFallbacks: []string{"fallback-alt"},
		NearestModels:  func(string) []string { return []string{"nearest-alt"} },
	}
	sentinel := NewStandingSentinelVerdict(parent)
	ctx, cancel := deadline(10 * time.Second)
	defer cancel()

	unpinned := standing.Item{ID: "u", Words: "keep main green"}
	if _, _, _, err := sentinel(ctx, standing.Judgment{Item: unpinned}); err != nil {
		t.Fatalf("unpinned: %v", err)
	}
	pinned := standing.Item{ID: "p", Words: unpinned.Words,
		Origin: standing.Origin{OneModel: true, PinnedModel: "shared-model"}}
	if _, _, _, err := sentinel(ctx, standing.Judgment{Item: pinned}); err != nil {
		t.Fatalf("pinned: %v", err)
	}
	for _, got := range *models {
		if got != "shared-model" {
			t.Fatalf("a same-model pinned/unpinned pair named %q, want only shared-model", got)
		}
	}
}

// AND A REFUSAL STILL NEVER CALLS ANOTHER MODEL. When the pinned model's
// endpoints will not take the request, the judgment is unknown — not a fallback
// to the role pin, the chain or the catalog's guess. The server refuses
// everything and records what was asked, so this proves what the real client
// sent rather than what a config says it would.
func TestAPinnedSentinelRefusalNeverCallsAnotherModel(t *testing.T) {
	var (
		mu     sync.Mutex
		models []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !answersChatOnly(w, r) {
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		models = append(models, body.Model)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"no allowed providers are available for the selected model"}}`))
	}))
	defer server.Close()

	parent := Config{
		Model:   "shared-model",
		APIKey:  "test",
		BaseURL: server.URL,
		RolesSource: func(key string) (string, bool) {
			if key == "roles.sentinel" {
				return "role-pinned-alt", true
			}
			return "", false
		},
		ModelFallbacks: []string{"fallback-alt"},
		NearestModels:  func(string) []string { return []string{"nearest-alt"} },
	}
	sentinel := NewStandingSentinelVerdict(parent)
	ctx, cancel := deadline(10 * time.Second)
	defer cancel()

	pinned := standing.Item{ID: "p", Words: "keep main green",
		Origin: standing.Origin{OneModel: true, PinnedModel: "shared-model"}}
	verdict, _, _, err := sentinel(ctx, standing.Judgment{Item: pinned, Evidence: "x"})
	if err == nil {
		t.Fatal("a refused pinned judgment answered as if it succeeded")
	}
	if verdict != standing.VerdictUnknown {
		t.Fatalf("a refused pinned judgment read as %v", verdict)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(models) == 0 {
		t.Fatal("the refusal was never actually asked")
	}
	for _, got := range models {
		if got != "shared-model" {
			t.Fatalf("a refused pinned judgment asked %q, not only the pinned model", got)
		}
	}
}

// A TASK FIRING INHERITS THE PIN, so its children and every text seat beneath
// them run under the same model. standingRunConfig is the one place a firing's
// config is assembled.
func TestARunOfAPinnedItemInheritsThePinForItsChildren(t *testing.T) {
	parent := reloadedPosture()
	item := standing.Item{
		ID:        "item-pinned",
		Words:     "keep main green",
		Workspace: t.TempDir(),
		Does:      standing.Action{Kind: standing.ActionTask, Brief: "check main"},
		Origin:    standing.Origin{OneModel: true, PinnedModel: "deepseek/deepseek-v4.1-flash"},
	}
	cfg, err := standingRunConfig(parent, item, filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatalf("standingRunConfig: %v", err)
	}
	if !cfg.OneModel || strings.TrimSpace(cfg.Model) != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("the run's posture is %+v, want the pinned model and one-model on", cfg)
	}
	if cfg.RolesSource != nil || cfg.ModelFallbacks != nil || cfg.NearestModels != nil || cfg.RouteCrew != nil {
		t.Fatal("the run kept a ladder its pinned children must not inherit")
	}
	// AN UNPINNED ITEM IS UNTOUCHED: the ordinary order keeps the profile's
	// routing exactly as it was.
	plain := item
	plain.Origin = standing.Origin{}
	got, err := standingRunConfig(parent, plain, filepath.Join(t.TempDir(), "run2"))
	if err != nil {
		t.Fatalf("standingRunConfig plain: %v", err)
	}
	if got.OneModel || got.Model != parent.Model || got.RolesSource == nil {
		t.Fatal("an ordinary order had its routing changed")
	}
}

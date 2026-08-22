package main

import (
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The seams the wiring audit found hanging: the pair generate_image is built
// from, the profile the settings panel writes into, and the model a launch
// opens on. Each of them was wired at one door and not the other, or resolved
// on one side and read on none.

// ── the painting pair ───────────────────────────────────────────────────────

// A client is a HAND and the belt's law is that a hand with nothing behind it
// is absent rather than refusing (internal/session's tools_image.go). The door
// therefore has exactly two honest answers, and the interesting half is the nil
// one: a *provider.MediaClient nil put into an interface is a non-nil interface
// holding nothing, which the belt would read as "there is a painter" and then
// panic on the first call.
func TestTheImageGeneratorIsAWholeHandOrNoneAtAll(t *testing.T) {
	settings := config.Config{APIKey: "sk-test", BaseURL: config.DefaultBaseURL}
	painter := v3ImageGen(settings)
	if painter == nil {
		t.Fatal("a keyed profile got no painter, so generate_image can never be on the belt")
	}

	// No key is no client, and it must be a TRUE nil — the belt tests this with
	// a plain nil check.
	keyless := v3ImageGen(config.Config{BaseURL: config.DefaultBaseURL})
	if keyless != nil {
		t.Fatalf("a keyless profile handed over %T, want nothing at all", keyless)
	}
	if none := v3ImageGen(config.Config{APIKey: "sk-test"}); none != nil {
		t.Fatalf("a profile with no base URL handed over %T", none)
	}
}

// And the model half, which the door does NOT resolve: the environment slot is
// passed through, and an empty one is passed through as empty so the session
// falls back to the person's own pin. The tier ladder is deliberately not
// consulted — a chat model in tiers.high is not a statement about painting.
func TestThePinnedPainterReachesTheSessionAndTheTiersDoNot(t *testing.T) {
	pinned := v3Profile(t, map[string]any{
		"models.roles":      "imagegen:paint/model",
		"models.tiers.high": "some/chat-model",
	})
	cfg, err := applyV3Governance(session.Config{Model: "session/model"}, pinned, false, false)
	if err != nil {
		t.Fatalf("the rows did not load: %v", err)
	}
	model, ok := roles.Pinned(roles.Source(cfg.RolesSource), roles.RoleImageGen)
	if !ok || model != "paint/model" {
		t.Fatalf("the imagegen pin reached the session as %q (ok=%v)", model, ok)
	}

	// No pin is no painter, whatever the tiers say. This is the case that keeps
	// generate_image off an unconfigured belt.
	tiers := v3Profile(t, map[string]any{"models.tiers.high": "some/chat-model"})
	cfg, err = applyV3Governance(session.Config{Model: "session/model"}, tiers, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if model, ok := roles.Pinned(roles.Source(cfg.RolesSource), roles.RoleImageGen); ok {
		t.Fatalf("a tier answered for the painter with %q", model)
	}
}

// ── the profile both doors write into ───────────────────────────────────────

// The settings panel writes where the door tells it to, and the door has to
// tell it the SAME directory it read every governance row out of. The --host
// door has always said so; the local one did not, and with AFORGE_PROFILE_DIR
// set the panel wrote into ~/.aforge/config.json while the session went on
// reading the profile the variable named.
//
// The construction sites sit past a real settings load and a live agent, so
// this reads the wiring, the way the attribution seam is read one file over.
func TestBothV3DoorsHandTheSurfaceAProfileDirectory(t *testing.T) {
	for _, name := range []string{"chatv3.go", "chatv3_host.go"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "ProfileDir:") {
			t.Fatalf("%s opens the v3 surface without telling it which profile to write into", name)
		}
	}
}

// ── the model a launch opens on ─────────────────────────────────────────────

// A model picked in /model used to last exactly as long as the session did. The
// order below is the whole fix, and the middle rung is the one worth pinning: a
// saved choice beats AFORGE_MODEL, because the settings row treats that
// variable as a default that seeds an unanswered row rather than a pin that
// freezes it — so a launch letting the variable win would revert a pick the
// sheet still showed as editable.
func TestTheTalkModelResolvesFlagThenChoiceThenEnvironment(t *testing.T) {
	dir := t.TempDir()
	settings := config.Config{ProfileDir: dir, Model: "from/the-environment"}

	if got := v3TalkModel("", settings); got != "from/the-environment" {
		t.Fatalf("an unchosen profile opened on %q", got)
	}
	if err := config.WriteChatModel(dir, "chosen/model"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := v3TalkModel("", settings); got != "chosen/model" {
		t.Fatalf("the launch opened on %q, want the model that was chosen", got)
	}
	// --model is for this session and beats everything, without disturbing what
	// is written down.
	if got := v3TalkModel("  flag/model  ", settings); got != "flag/model" {
		t.Fatalf("--model resolved to %q", got)
	}
	if got := config.ChatModelAt(dir); got != "chosen/model" {
		t.Fatalf("a one-session flag rewrote the profile to %q", got)
	}
}

package tui3

// THE ORDINARY LAUNCH, WHICH IS THE ONE NOBODY HAD A TEST FOR.
//
// AN EMPTY PROFILE DIRECTORY IS THE NORMAL CASE, NOT THE ABSENT CASE, AND
// ABSENCE IS A HOSTED WINDOW. [config.ProfileDir] carries AFORGE_PROFILE_DIR,
// which almost nobody exports, so what reaches this package on very nearly
// every launch is the empty string — and internal/config has always resolved
// that to this process's own profile in the state root ([config.ProfilePath]).
// Three sites here read it the other way round, and each of the three decided
// something a status segment does not: whether the front door opens at all,
// whether the install's rung is drawn, and whether the status line may say the
// tool gate is open (#322).
//
// EVERY TEST IN THIS FILE IS WRITTEN FROM THE LAUNCH AND NOT FROM THE FIELD. It
// names no profile directory, exactly as `aforge` bare on a terminal does, and
// then asks what a person sitting in front of it would see. The suite already
// had thorough tests of all three behaviours and every one of them named a
// profile directory first — which is how a class of defects that made the
// product unusable on a fresh install stayed green for months.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ordinaryLaunch is a surface opened the way `aforge` bare on a TTY opens it: a
// state root of its own, no provider key anywhere the door would look, and NO
// PROFILE DIRECTORY NAMED.
//
// IT PRE-CREATES NOTHING. The whole class this file is about is emptiness read
// as absence, so a helper that seeded a config file — or even a profile
// directory — would hide the exact failure it exists to catch. The state root
// is an empty temporary directory and the first thing to write into it is the
// product. `seed` runs after the environment is pinned and before the surface
// is built, which is the only order in which a test can say "this profile
// already held that" about a decision [newApp] makes on its way up.
func ordinaryLaunch(t *testing.T, opts Options, seed func()) *app {
	t.Helper()
	// Everything the key resolution and the rails would otherwise read out of
	// the developer's own shell. Empty reads as unset everywhere in
	// internal/config.
	for _, pin := range []string{config.APIKeyEnv, "OPENAI_API_KEY", "AFORGE_DAILY_BUDGET", config.ProfileDirEnv} {
		t.Setenv(pin, "")
	}
	t.Setenv("AFORGE_HOME", t.TempDir())
	if seed != nil {
		seed()
	}
	opts.Agent = &fakeAgent{model: "openai/gpt-4.1-mini"}
	opts.Workspace = "/tmp/lab"
	a := newApp(t.Context(), opts)
	a.width, a.height = 90, 30
	a.pal = newPalette(tokens.ANSI256, false)
	a.touch()
	return a
}

// ordinaryConnect is the default provider's browser door as the real launch
// hands it over: a local interactive session on the built-in OpenRouter
// endpoint gets one (cmd/aforge's v3OpenRouterConnection). Nothing here calls
// it — its presence is the whole fact the key step reads.
func ordinaryConnect(context.Context) (OpenRouterFlow, error) { return nil, nil }

// writeOrdinaryRow writes one settings row into THE PROFILE AN ORDINARY LAUNCH
// USES, through the registry every surface writes settings with — so what these
// tests set is byte-for-byte what a person setting it in /settings leaves
// behind, in the same file.
func writeOrdinaryRow(t *testing.T, key, value string) {
	t.Helper()
	row, ok := config.NewSettings(config.SettingsOptions{}).Row(key)
	if !ok {
		t.Fatalf("no settings row named %q", key)
	}
	if err := row.Apply(value); err != nil {
		t.Fatalf("write %s: %v", key, err)
	}
}

// ordinaryScreen is the frame as words, so a claim about a sentence does not
// depend on where the block wrapped.
func ordinaryScreen(a *app) string {
	frame, _, _ := a.frame()
	return strings.Join(strings.Fields(plain(frame)), " ")
}

// ── 1. the front door ───────────────────────────────────────────────────────

// THE FIRST-RUN AND PROVIDER SCREENS OPEN ON AN ORDINARY LAUNCH.
//
// This is the sharp half of #322 and it is the front door: a fresh install with
// no key, started the normal way, was never shown the screen that connects a
// provider. It met whatever the empty-key path does instead, and nobody noticed
// because everybody who has ever tested it already had a key in their shell.
//
// It is asked at 80 and at 120 columns because the screen centres its block in
// the window, and a claim about a sentence must not depend on where that landed.
func TestTheSetupOpensOnAnOrdinaryLaunchWithNoKey(t *testing.T) {
	for _, width := range []int{80, 120} {
		t.Run(itoa(width)+" columns", func(t *testing.T) {
			a := ordinaryLaunch(t, Options{Setup: true, ConnectOpenRouter: ordinaryConnect}, nil)
			a.width, a.height = width, 30
			if !a.setup.open {
				t.Fatal("a fresh install launched the ordinary way was shown no setup at all")
			}
			screen := ordinaryScreen(a)
			if !strings.Contains(screen, "connect openrouter") {
				t.Fatalf("the provider-connection step is not on the screen:\n%s", screen)
			}
			if !strings.Contains(screen, "setting up") {
				t.Fatalf("the setup's own title is not on the screen:\n%s", screen)
			}
		})
	}
}

// AND IT ASKS ONLY WHAT IS MISSING, on the ordinary launch too. A key in the
// shell drops the provider step and leaves the two first-run preferences — the
// law [TestAKeyInTheShellSkipsTheKeyStepSilently] states over a named profile,
// asked here of the launch nearly everybody actually takes.
func TestAKeyInTheShellDropsTheProviderStepOnAnOrdinaryLaunch(t *testing.T) {
	a := ordinaryLaunch(t, Options{Setup: true, ConnectOpenRouter: ordinaryConnect}, func() {
		t.Setenv(config.APIKeyEnv, "sk-or-v1-from-the-shell-0123456789")
	})
	if !a.setup.open || len(a.setup.steps) != 2 || a.setup.steps[0] != setupCrew {
		t.Fatalf("with the key in the shell only the crew and the ceiling are asked, got open=%v steps=%v",
			a.setup.open, a.setup.steps)
	}
	if screen := ordinaryScreen(a); strings.Contains(screen, "connect openrouter") {
		t.Fatalf("a launch with a key was asked to connect a provider:\n%s", screen)
	}
}

// AND A PROFILE THAT HAS ANSWERED EVERYTHING IS ASKED NOTHING. This is the
// other end of the same launch — a key, and the marker saying the questions
// were put once — and it is what keeps the fix from becoming a greeting every
// morning.
func TestAnOrdinaryLaunchWithEverythingAnsweredOpensNoSetup(t *testing.T) {
	a := ordinaryLaunch(t, Options{Setup: true, ConnectOpenRouter: ordinaryConnect}, func() {
		t.Setenv(config.APIKeyEnv, "sk-or-v1-from-the-shell-0123456789")
		// The marker lands in the profile an ordinary launch uses, which is the
		// one an ordinary launch must then read it back out of.
		if err := config.MarkSetupSeen("", time.Now()); err != nil {
			t.Fatal(err)
		}
	})
	if a.setup.open {
		t.Fatalf("a profile with a key and a marker was asked again: %v", a.setup.steps)
	}
}

// AND THE HOSTED WINDOW IS ASKED NOTHING, WHICH IS THE ONE REAL ABSENCE.
//
// What this screen writes — the key, the crew, three spending rails, the marker
// saying it was shown — lands in the profile of the machine the AGENT is on,
// and over --host that machine is not this one. The answers would be this
// laptop's answers about somebody else's session, which is the form that would
// lie, so a connection gets no setup even with no key anywhere in sight.
func TestAHostedWindowIsAskedNothingOnAnOrdinaryLaunch(t *testing.T) {
	a := ordinaryLaunch(t, Options{Setup: true, ConnectOpenRouter: ordinaryConnect, Host: "devbox"}, nil)
	if a.setup.open {
		t.Fatalf("a connection was asked to set up this laptop's profile: %v", a.setup.steps)
	}
}

// ── 2. the install's own rung ───────────────────────────────────────────────

// THE INSTALL'S EFFORT RUNG IS READ ON AN ORDINARY LAUNCH.
//
// The card's facts line states the rung work started from it would think at
// (place_home.go's [app.homeCardFacts], SCREEN 1d). It reached the profile
// through a guard that answered "no profile" on the ordinary launch, so the
// clause was drawn on the rare launch that exported AFORGE_PROFILE_DIR and for
// nobody else.
func TestTheInstallsRungIsReadOnAnOrdinaryLaunch(t *testing.T) {
	a := ordinaryLaunch(t, Options{}, nil)
	dir, ok := a.effortProfile()
	if !ok {
		t.Fatal("an ordinary launch was told it has no profile to read a rung from")
	}
	if dir != a.profileDir {
		t.Fatalf("effortProfile answered %q for a window whose profile is %q", dir, a.profileDir)
	}
	if got := effortClause(config.DefaultEffortAt(dir)); got != effortClauseWord+effort.Ship.String() {
		t.Fatalf("the install's rung reads %q on an ordinary launch, want the shipped rung", got)
	}
	// AND A ROW MOVED IN THAT PROFILE IS THE ROW READ BACK, which is the whole
	// point of resolving the path rather than guarding on its emptiness.
	writeOrdinaryRow(t, config.KeyEffort, effort.Low.String())
	if got := effortClause(config.DefaultEffortAt(dir)); got != effortClauseWord+effort.Low.String() {
		t.Fatalf("the rung a person set reads %q on the launch they set it from", got)
	}
}

// AND THE HOSTED WINDOW STATES NO RUNG. The install whose rung that clause names
// is the one the work runs on, and over --host that is the other machine's.
func TestAHostedWindowStatesNoInstallRung(t *testing.T) {
	a := ordinaryLaunch(t, Options{Host: "devbox"}, nil)
	if _, ok := a.effortProfile(); ok {
		t.Fatal("a connection offered to state this laptop's rung for somebody else's machine")
	}
}

// ── 3. the approval posture, which is a safety claim ────────────────────────

// THE YOLO SEGMENT REPORTS THE POSTURE IN FORCE.
//
// THIS ONE IS TESTED SEPARATELY BECAUSE IT IS NOT A STATUS SEGMENT, IT IS A
// SAFETY CLAIM. The segment is drawn only when the gate is open (render.go's
// NEGATIVE-SPACE SAFETY), so its ABSENCE is the claim that every tool call will
// be asked about — and the gate it is claiming about is the one cmd/aforge
// builds from [config.ToolApprovalModeAt] on the same profile directory
// (chatv3.go's v3Policy). Before #322 the surface's reading answered "" on an
// empty profile directory while the policy's reading resolved it to the state
// root, so a person who had turned the asking OFF was shown nothing at all on
// every ordinary launch: the one posture this segment exists to remind them of
// was the one it never mentioned.
//
// Four cases, which is the whole matrix: the ordinary launch and the hosted
// window, under both postures.
func TestTheYoloSegmentMatchesThePostureInForce(t *testing.T) {
	t.Run("an ordinary launch that asks first says nothing", func(t *testing.T) {
		a := ordinaryLaunch(t, Options{}, nil)
		if force := config.ToolApprovalModeAt(""); force != config.DefaultToolApprovalMode {
			t.Fatalf("the gate in force on a fresh profile is %q, want the strict default", force)
		}
		if got := a.yoloSegment(); got != "" {
			t.Fatalf("the status line drew %q over a gate that asks first", got)
		}
	})

	t.Run("an ordinary launch with the asking off says so", func(t *testing.T) {
		a := ordinaryLaunch(t, Options{}, func() {
			writeOrdinaryRow(t, config.KeyToolApprovalMode, "allow")
		})
		if force := config.ToolApprovalModeAt(""); force != "allow" {
			t.Fatalf("the gate in force is %q, want the posture just written", force)
		}
		if got := a.yoloSegment(); got != "YOLO" {
			t.Fatalf("the status line drew %q while every tool call runs without asking", got)
		}
		if screen := ordinaryScreen(a); !strings.Contains(screen, "YOLO") {
			t.Fatalf("the segment never reached the frame:\n%s", screen)
		}
	})

	// THIS LAPTOP SAYS ALLOW AND THE ENGINE SAYS ASK, in both hosted cases: a
	// badge drawn from this side would be a safety claim about a machine nobody
	// consulted, and the engine's own answer travelled once on the welcome.
	t.Run("a hosted window whose engine asks first says nothing", func(t *testing.T) {
		a := ordinaryLaunch(t, Options{Host: "devbox", ApprovalMode: config.DefaultToolApprovalMode}, func() {
			writeOrdinaryRow(t, config.KeyToolApprovalMode, "allow")
		})
		if got := a.yoloSegment(); got != "" {
			t.Fatalf("a connection to an engine that asks first drew %q from this laptop's profile", got)
		}
	})

	t.Run("a hosted window whose engine has the asking off says so", func(t *testing.T) {
		a := ordinaryLaunch(t, Options{Host: "devbox", ApprovalMode: "allow"}, nil)
		if got := a.yoloSegment(); got != "YOLO" {
			t.Fatalf("a connection to an engine with the asking off drew %q", got)
		}
	})
}

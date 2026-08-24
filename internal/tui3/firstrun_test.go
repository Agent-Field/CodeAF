package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The first-run setup's own tests (firstrun.go). Every claim about what was
// written is checked by READING THE PROFILE BACK through internal/config, never
// by trusting the screen: the screen is the part a person sees, and the file is
// the part they live with.

// setupApp is a surface opened the way `aforge` bare on a TTY opens it, over a
// profile `seed` has prepared first. It is [sheetApp] with the setup allowed
// and the seed run BEFORE the app, because the setup is decided inside newApp
// and a file written afterwards would be a file it never saw.
func setupApp(t *testing.T, seed func(dir string)) (*app, string, *[]string) {
	t.Helper()
	for _, pin := range []string{config.APIKeyEnv, "OPENAI_API_KEY", "AFORGE_DAILY_BUDGET", "AFORGE_PROFILE_DIR"} {
		t.Setenv(pin, "")
	}
	t.Setenv("AFORGE_HOME", t.TempDir())
	dir := t.TempDir()
	if seed != nil {
		seed(dir)
	}
	handed := &[]string{}
	a := newApp(t.Context(), Options{
		Agent:       &fakeAgent{model: "openai/gpt-4.1-mini"},
		Workspace:   "/tmp/lab",
		ProfileDir:  dir,
		Setup:       true,
		ApplyAPIKey: func(key string) error { *handed = append(*handed, key); return nil },
	})
	a.width, a.height = 90, 30
	a.pal = newPalette(tokens.ANSI256, false)
	a.touch()
	return a, dir, handed
}

// pressSetup feeds one key through Update and drops the command: the setup's
// keys start nothing but the welcome box's clock, which these tests read as a
// value rather than run.
func pressSetup(a *app, msgs ...tea.Msg) tea.Cmd {
	var last tea.Cmd
	for _, msg := range msgs {
		_, last = a.Update(msg)
	}
	return last
}

// setupScreen is the frame as words: the block wraps its sentences to its own
// width, and a claim about a sentence must not depend on where the wrap fell.
func setupScreen(a *app) string {
	frame, _, _ := a.frame()
	return strings.Join(strings.Fields(plain(frame)), " ")
}

func TestTheSetupOpensOverAnEmptyProfileAndNotOverAConfiguredOne(t *testing.T) {
	a, _, _ := setupApp(t, nil)
	if !a.setup.open {
		t.Fatal("a profile with nothing in it must be asked")
	}
	if got := a.setup.steps; len(got) != 3 || got[0] != setupKey || got[1] != setupCrew || got[2] != setupBudget {
		t.Fatalf("a fresh profile asks all three in order, got %v", got)
	}
	screen := setupScreen(a)
	for _, want := range []string{"setting up · 1 of 3", "your openrouter key", "https://openrouter.ai/settings/keys", "esc skips setup"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the first screen must say %q; got:\n%s", want, screen)
		}
	}

	b, dir, _ := setupApp(t, func(dir string) {
		if err := config.WriteAPIKey(dir, "sk-or-v1-0123456789abcdef"); err != nil {
			t.Fatal(err)
		}
		if err := config.ApplyCrew(dir, config.CrewFrugal); err != nil {
			t.Fatal(err)
		}
		if err := config.WriteDailyBudgetUSD(dir, 9); err != nil {
			t.Fatal(err)
		}
	})
	if b.setup.open {
		t.Fatal("a profile with all three answered must not be asked")
	}
	if config.SetupSeenAt(dir).IsZero() {
		t.Fatal("a launch with nothing to ask still records that the setup was met")
	}
}

func TestEnterThreeTimesLandsTheDefaultsInTheProfile(t *testing.T) {
	a, dir, handed := setupApp(t, nil)
	pressSetup(a, key("enter"))
	if a.setup.step() != setupCrew {
		t.Fatal("enter on an empty key box goes on to the crew")
	}
	if !strings.Contains(setupScreen(a), "the model you talk to is a separate choice, made with /model") {
		t.Fatalf("the crew step must say the crew is not the model you talk to; got:\n%s", setupScreen(a))
	}
	pressSetup(a, key("enter"))
	if a.setup.step() != setupBudget {
		t.Fatal("enter on the crew goes on to the ceiling")
	}
	if !strings.Contains(setupScreen(a), "$20") {
		t.Fatalf("the ceiling step must show the default it will keep; got:\n%s", setupScreen(a))
	}
	pressSetup(a, key("enter"))
	if a.setup.open {
		t.Fatal("enter on the last step closes the setup")
	}

	if !config.CrewConfigured(dir) || config.CrewAt(dir) != config.CrewBalanced {
		t.Fatalf("enter on the crew must write balanced; profile reads %q", config.CrewAt(dir))
	}
	if !config.DailyBudgetConfigured(dir) {
		t.Fatal("enter on the ceiling must write the default into the profile")
	}
	if rail, err := config.DailyBudgetUSDAt(dir); err != nil || rail != config.DefaultDailyBudgetUSD {
		t.Fatalf("the ceiling read back as %v (%v), want %v", rail, err, config.DefaultDailyBudgetUSD)
	}
	if config.SetupSeenAt(dir).IsZero() {
		t.Fatal("finishing must write the marker")
	}
	if config.APIKeyConfigured(dir) || len(*handed) != 0 {
		t.Fatal("an empty key box writes no key and hands none to the session")
	}
	if got := lastNote(t, a); !strings.Contains(got, "/settings") || !strings.Contains(got, config.APIKeyEnv) {
		t.Fatalf("a setup that ended keyless leaves one line pointing at /settings, got %q", got)
	}
	// And it never returns.
	b, _, _ := setupApp(t, func(next string) {
		_ = config.MarkSetupSeen(next, a.now())
	})
	if b.setup.open {
		t.Fatal("a profile that has met the setup is not asked again")
	}
}

func TestATypedOrPastedKeyIsWrittenAndHandedToTheSession(t *testing.T) {
	a, dir, handed := setupApp(t, nil)
	const pasted = "sk-or-v1-0123456789abcdef0123456789abcdef"
	pressSetup(a, tea.PasteMsg{Content: pasted + "\n"})
	screen := setupScreen(a)
	if strings.Contains(screen, "0123456789") || !strings.Contains(screen, "•") || !strings.Contains(screen, "cdef") {
		t.Fatalf("the key must draw masked with its tail in the clear; got:\n%s", screen)
	}
	pressSetup(a, key("enter"))
	if got := config.PersistedAPIKey(dir); got != pasted {
		t.Fatalf("the profile holds %q, want the pasted key", got)
	}
	if len(*handed) != 1 || (*handed)[0] != pasted {
		t.Fatalf("the running session must be handed the key once, got %v", *handed)
	}
	pressSetup(a, key("down"), key("enter"))
	if config.CrewAt(dir) != config.CrewMax {
		t.Fatalf("↓ then enter takes the next preset; profile reads %q", config.CrewAt(dir))
	}
	for _, r := range "7.5" {
		pressSetup(a, key(string(r)))
	}
	pressSetup(a, key("enter"))
	if rail, _ := config.DailyBudgetUSDAt(dir); rail != 7.5 {
		t.Fatalf("a typed ceiling replaces the default, profile reads %v", rail)
	}
	if a.setup.open {
		t.Fatal("the flow closes after its last step")
	}
	for _, e := range a.entries {
		if e.kind == entryNote && strings.Contains(e.text, "no openrouter key") {
			t.Fatal("a profile with a key gets no note about a missing one")
		}
	}
}

func TestAKeyOfTheWrongShapeIsRefusedAndTheStepStays(t *testing.T) {
	a, dir, _ := setupApp(t, nil)
	typeSetup(a, "not a key at all")
	pressSetup(a, key("enter"))
	if a.setup.step() != setupKey {
		t.Fatal("a refused key keeps the step")
	}
	if !strings.Contains(setupScreen(a), setupKeyShapeWord) {
		t.Fatalf("the refusal must be on the screen; got:\n%s", setupScreen(a))
	}
	if config.APIKeyConfigured(dir) {
		t.Fatal("a refused key is not written")
	}
	pressSetup(a, key("backspace"))
	if strings.Contains(setupScreen(a), setupKeyShapeWord) {
		t.Fatal("the next keystroke clears the refusal")
	}
}

func TestEscSkipsTheWholeFlowAndWritesNothingButTheMarker(t *testing.T) {
	a, dir, _ := setupApp(t, nil)
	if !a.welcome.open {
		t.Fatal("the box is decided under the setup, ready to arrive when it closes")
	}
	a.welcome.step = welcomeFrames
	cmd := pressSetup(a, key("esc"))
	if a.setup.open {
		t.Fatal("esc closes the setup")
	}
	if cmd == nil || a.welcome.step != 0 {
		t.Fatal("the box's arrival starts from its first frame when the setup goes")
	}
	if config.CrewConfigured(dir) || config.DailyBudgetConfigured(dir) || config.APIKeyConfigured(dir) {
		t.Fatal("skipping writes none of the three")
	}
	if config.SetupSeenAt(dir).IsZero() {
		t.Fatal("skipping counts as shown")
	}
	if got := lastNote(t, a); !strings.Contains(got, "/settings") {
		t.Fatalf("a skipped setup with no key leaves the one line, got %q", got)
	}
	// Typing afterwards goes to the draft, not to a screen that is gone.
	pressSetup(a, key("h"))
	if a.setup.open || a.input.empty() {
		t.Fatal("after esc the keyboard is the conversation's")
	}
}

func TestAKeyInTheShellSkipsTheKeyStepSilently(t *testing.T) {
	t.Setenv(config.APIKeyEnv, "sk-or-v1-from-the-shell-0123456789")
	for _, pin := range []string{"OPENAI_API_KEY", "AFORGE_DAILY_BUDGET", "AFORGE_PROFILE_DIR"} {
		t.Setenv(pin, "")
	}
	t.Setenv("AFORGE_HOME", t.TempDir())
	dir := t.TempDir()
	a := newApp(t.Context(), Options{Agent: &fakeAgent{model: "openai/gpt-4.1-mini"}, Workspace: "/tmp/lab", ProfileDir: dir, Setup: true})
	a.width, a.height = 90, 30
	a.pal = newPalette(tokens.ANSI256, false)
	if !a.setup.open || len(a.setup.steps) != 2 || a.setup.steps[0] != setupCrew {
		t.Fatalf("with the key in the shell only the crew and the ceiling are asked, got %v", a.setup.steps)
	}
	if !strings.Contains(setupScreen(a), "setting up · 1 of 2") {
		t.Fatalf("the count is the count of what is asked; got:\n%s", setupScreen(a))
	}
}

func TestTheSetupStaysAwayFromEveryLaunchThatIsNotAPersonArriving(t *testing.T) {
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("AFORGE_HOME", t.TempDir())
	open := func(opts Options) bool {
		opts.Agent = &fakeAgent{model: "openai/gpt-4.1-mini"}
		opts.Workspace = "/tmp/lab"
		if opts.ProfileDir == "" {
			opts.ProfileDir = t.TempDir()
		}
		return newApp(t.Context(), opts).setup.open
	}
	if open(Options{}) {
		t.Fatal("a door that did not say a person is here gets no setup")
	}
	if open(Options{Setup: true, Resumed: true}) {
		t.Fatal("a resumed conversation is not a person arriving")
	}
	if open(Options{Setup: true, Host: "devbox"}) {
		t.Fatal("a hosted session's profile is the other machine's")
	}
	busy := newApp(t.Context(), Options{
		Setup: true, Workspace: "/tmp/lab", ProfileDir: t.TempDir(),
		Agent: &fakeAgent{model: "openai/gpt-4.1-mini", past: []session.DisplayEntry{{Role: "user", Text: "hello"}}},
	})
	if busy.setup.open {
		t.Fatal("a conversation with anything in it is not asked")
	}
}

func TestTheSettingsRowHandsAKeyToTheRunningSession(t *testing.T) {
	a, dir, handed := setupApp(t, nil)
	pressSetup(a, key("esc"))
	row, ok := a.registry().Row(config.KeyAPIKey)
	if !ok {
		t.Fatal("the registry this surface edits has no key row")
	}
	if err := row.Apply("sk-or-v1-0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	if got := config.PersistedAPIKey(dir); got != "sk-or-v1-0123456789abcdef" {
		t.Fatalf("the row wrote %q", got)
	}
	if len(*handed) != 1 {
		t.Fatalf("a key written in the row reaches the session in the same breath, got %v", *handed)
	}
}

func typeSetup(a *app, text string) {
	for _, r := range text {
		pressSetup(a, key(string(r)))
	}
}

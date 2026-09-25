package tui3

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

type setupOpenRouterFlow struct {
	url       string
	key       string
	err       error
	cancelled bool
}

func (f *setupOpenRouterFlow) URL() string { return f.url }
func (f *setupOpenRouterFlow) Wait(context.Context) (string, error) {
	return f.key, f.err
}
func (f *setupOpenRouterFlow) Cancel() { f.cancelled = true }

// The first-run setup's own tests (firstrun.go). Every claim about what was
// written is checked by READING THE PROFILE BACK through internal/config, never
// by trusting the screen: the screen is the part a person sees, and the file is
// the part they live with.

// setupApp is a surface opened the way `codeaf` bare on a TTY opens it, over a
// profile `seed` has prepared first. It is [sheetApp] with the setup allowed
// and the seed run BEFORE the app, because the setup is decided inside newApp
// and a file written afterwards would be a file it never saw.
func setupApp(t *testing.T, seed func(dir string)) (*app, string, *[]string) {
	t.Helper()
	for _, pin := range []string{config.APIKeyEnv, "OPENAI_API_KEY", "CODEAF_DAILY_BUDGET", "CODEAF_PROFILE_DIR"} {
		t.Setenv(pin, "")
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	dir := t.TempDir()
	if seed != nil {
		seed(dir)
	}
	handed := &[]string{}
	var connect func(context.Context) (OpenRouterFlow, error)
	if config.SetupSeenAt(dir).IsZero() {
		connect = func(context.Context) (OpenRouterFlow, error) { return nil, nil }
	}
	a := newApp(t.Context(), Options{
		Agent:             &fakeAgent{model: "openai/gpt-4.1-mini"},
		Workspace:         "/tmp/lab",
		ProfileDir:        dir,
		Setup:             true,
		ConnectOpenRouter: connect,
		ApplyAPIKey:       func(key string) error { *handed = append(*handed, key); return nil },
	})
	// The browser seam is present while the real launch decides which screens
	// exist. Most tests below exercise the paste road, so it is taken back off
	// before they press a key; the browser test installs its recording flow.
	a.routerConnect = nil
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
	if got := a.setup.steps; len(got) != 2 || got[0] != setupKey || got[1] != setupControls {
		t.Fatalf("a fresh profile asks the key and then the controls, got %v", got)
	}
	screen := setupScreen(a)
	for _, want := range []string{"setting up · 1 of 2", "your openrouter key", "https://openrouter.ai/settings/keys", "esc skips setup"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the first screen must say %q; got:\n%s", want, screen)
		}
	}

	b, dir, _ := setupApp(t, func(dir string) {
		if err := config.WriteAPIKey(dir, "sk-or-v1-0123456789abcdef"); err != nil {
			t.Fatal(err)
		}
		if err := config.WriteDailyBudgetUSD(dir, 9); err != nil {
			t.Fatal(err)
		}
	})
	if b.setup.open {
		t.Fatal("a profile with the key and the limit answered must not be asked")
	}
	if config.SetupSeenAt(dir).IsZero() {
		t.Fatal("a launch with nothing to ask still records that the setup was met")
	}
}

func TestC19FirstrunRendersOpenRouterAndNoCodexOffer(t *testing.T) {
	// C19: this is the frame a person sees on a fresh profile, not merely a
	// constructor seam. Codex belongs behind /connect and is absent here.
	a, _, _ := setupApp(t, nil)
	screen := setupScreen(a)
	if !strings.Contains(screen, "openrouter") {
		t.Fatalf("first-run setup lost its OpenRouter offer:\n%s", screen)
	}
	if strings.Contains(strings.ToLower(screen), "codex") {
		t.Fatalf("first-run setup exposed Codex:\n%s", screen)
	}
}

func TestEnterConnectsOpenRouterInTheBrowserAndHandsTheKeyToThisProcess(t *testing.T) {
	a, dir, handed := setupApp(t, nil)
	flow := &setupOpenRouterFlow{
		url: "https://openrouter.example/auth?proof=one",
		key: "sk-or-v1-from-the-browser-0123456789",
	}
	a.routerConnect = func(context.Context) (OpenRouterFlow, error) { return flow, nil }

	opened := ""
	was := processOpener
	processOpener = func(target string) error { opened = target; return nil }
	t.Cleanup(func() { processOpener = was })

	screen := setupScreen(a)
	for _, want := range []string{"connect openrouter", "default service", "sign in once in your browser", "enter connects in browser", "paste a key"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the browser connection must say %q; got:\n%s", want, screen)
		}
	}
	begin := pressSetup(a, key("enter"))
	if begin == nil || !a.setup.authStarting {
		t.Fatal("enter must put up the connecting state and start the listener")
	}
	_, wait := a.Update(begin())
	if wait == nil || opened != flow.url {
		t.Fatalf("the ready flow opened %q and returned wait %v", opened, wait != nil)
	}
	if screen := setupScreen(a); !strings.Contains(screen, "finish connecting openrouter") || !strings.Contains(screen, flow.url) {
		t.Fatalf("the wait must carry the browser address; got:\n%s", screen)
	}
	// A key landing here goes on to the controls screen, and the ONE command that
	// comes back is the example panel's first beat: the arrival is one of the two
	// deliberate acts its demonstration plays for (onboarding.go). Nothing else is
	// started — no fetch, no second listener, no clock that keeps running.
	_, next := a.Update(wait())
	if a.setup.step() != setupControls {
		t.Fatalf("the key must go on to the controls; step = %v", a.setup.step())
	}
	if next == nil {
		t.Fatal("arriving on the controls armed no beat for the example panel")
	}
	// The beat is longer than the harness clock's budget, so it is waited out
	// deliberately rather than asked of a clock that answers polls with nothing.
	beat := waitOut(next)
	if _, ok := beat.(setupDemoMsg); !ok {
		t.Fatalf("the key started something other than the panel's beat: %T", beat)
	}
	if got := config.PersistedAPIKey(dir); got != flow.key {
		t.Fatalf("profile key = %q, want browser key", got)
	}
	if len(*handed) != 1 || (*handed)[0] != flow.key {
		t.Fatalf("running process was handed %v", *handed)
	}
	if a.setup.step() != setupControls {
		t.Fatalf("browser success did not advance to the controls; step = %v", a.setup.step())
	}
}

func TestAMissingDefaultProviderReturnsOverAResumedProfileAndKeepsTheDraft(t *testing.T) {
	for _, pin := range []string{config.APIKeyEnv, "OPENAI_API_KEY", "CODEAF_DAILY_BUDGET", "CODEAF_PROFILE_DIR"} {
		t.Setenv(pin, "")
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	dir := t.TempDir()
	if err := config.MarkSetupSeen(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	flow := &setupOpenRouterFlow{url: "https://openrouter.example/auth", key: "sk-or-v1-later-0123456789"}
	a := newApp(t.Context(), Options{
		Agent:      &fakeAgent{model: "openai/gpt-4.1-mini", past: []session.DisplayEntry{{Role: "user", Text: "earlier"}}},
		Workspace:  "/tmp/lab",
		ProfileDir: dir,
		Resumed:    true,
		ConnectOpenRouter: func(context.Context) (OpenRouterFlow, error) {
			return flow, nil
		},
	})
	a.width, a.height = 90, 30
	a.pal = newPalette(tokens.ANSI256, false)
	if !a.setup.open || len(a.setup.steps) != 1 || a.setup.step() != setupKey {
		t.Fatalf("a resumed profile with no model key must get the one-step provider door, got %+v", a.setup)
	}
	pressSetup(a, key("esc"))
	if a.setup.open {
		t.Fatal("not now must reveal the resumed conversation")
	}
	a.input.setText("keep these exact words")
	if cmd := a.enter(); cmd != nil {
		t.Fatal("the keyless draft must not be submitted")
	}
	if !a.setup.open || a.input.String() != "keep these exact words" {
		t.Fatalf("enter must reopen the provider without clearing the draft; open=%v draft=%q", a.setup.open, a.input.String())
	}
	if got := noteSaying(t, a, "connects in a browser"); !strings.Contains(got, config.APIKeyEnv) {
		t.Fatalf("the not-now note must name both direct roads, got %q", got)
	}
}

func TestEscCancelsAnOpenRouterBrowserTripWithoutSkippingTheKeyStep(t *testing.T) {
	a, _, _ := setupApp(t, nil)
	flow := &setupOpenRouterFlow{url: "https://openrouter.example/auth", key: "sk-or-v1-unused-0123456789"}
	a.routerConnect = func(context.Context) (OpenRouterFlow, error) { return flow, nil }
	was := processOpener
	processOpener = func(string) error { return nil }
	t.Cleanup(func() { processOpener = was })

	begin := pressSetup(a, key("enter"))
	_, wait := a.Update(begin())
	if wait == nil {
		t.Fatal("the browser trip did not start waiting")
	}
	pressSetup(a, key("esc"))
	if flow.cancelled != true || !a.setup.open || a.setup.step() != setupKey {
		t.Fatalf("esc must cancel and stay on the key step; cancelled=%v open=%v", flow.cancelled, a.setup.open)
	}
	if !strings.Contains(setupScreen(a), setupConnectCancelledWord) {
		t.Fatalf("the cancelled connection must say how to retry; got:\n%s", setupScreen(a))
	}
}

// TAKING THE SCREEN AS IT STANDS LANDS EXACTLY WHAT IT DREW.
//
// The whole bargain of the controls screen is that every row opens on the value
// already in force, so somebody who reads it and agrees is agreeing to what is on
// the screen and not to a hidden default. This walks the shortest road through
// it — enter past the key, tab to the way out, enter — and reads the profile back.
func TestTakingTheControlsAsTheyStandLandsTheDefaultsInTheProfile(t *testing.T) {
	a, dir, handed := setupApp(t, nil)
	pressSetup(a, key("enter"))
	if a.setup.step() != setupControls {
		t.Fatal("enter on an empty key box goes on to the controls")
	}
	screen := setupScreen(a)
	for _, want := range []string{
		"Models and spending", "Keep these choices or change them.",
		"Daily limit", "Chat model", "Start a conversation",
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the controls screen must say %q; got:\n%s", want, screen)
		}
	}
	// The figure is read from the constant every other reader of the rail
	// resolves to, so a moved default moves the test with it rather than leaving
	// it pinning a number nobody ships.
	if want := "$" + setupBudgetDefault(); !strings.Contains(screen, want) {
		t.Fatalf("the limit must show %s, the amount it will keep; got:\n%s", want, screen)
	}
	// THE ONE-LINE EXPLANATIONS ARE ON THE SCREEN, both at once, because they
	// are what makes unfamiliar words into decisions.
	for _, want := range []string{controlLimitWord, controlModelWord} {
		if !strings.Contains(screen, strings.Join(strings.Fields(want), " ")) {
			t.Fatalf("the controls screen must explain itself with %q; got:\n%s", want, screen)
		}
	}

	// tab down to `Start a conversation`, and take it.
	pressSetup(a, key("tab"), key("tab"), key("tab"))
	if a.setup.control != controlStart {
		t.Fatalf("three tabs from the limit reach the way out, got %v", a.setup.control)
	}
	pressSetup(a, key("enter"))
	if a.setup.open {
		t.Fatal("enter on `Start a conversation` closes the setup")
	}

	// AND THE CREW IS NOT WRITTEN: it is auto, picked per task, and nothing
	// on this screen answers it.
	if pins := config.CrewPinsAt(dir); len(pins) != 0 {
		t.Fatalf("taking the controls pinned a seat: %v", pins)
	}
	if !config.DailyBudgetConfigured(dir) {
		t.Fatal("taking the limit as it stands must write it into the profile")
	}
	if rail, err := config.DailyBudgetUSDAt(dir); err != nil || rail != config.DefaultDailyBudgetUSD {
		t.Fatalf("the ceiling read back as %v (%v), want %v", rail, err, config.DefaultDailyBudgetUSD)
	}
	// AND THE TWO RAILS THIS SCREEN NO LONGER ASKS ABOUT ARE UNTOUCHED AND STILL
	// RESOLVE. They keep the defaults docs/LIMITS.md states and /budget changes
	// them; onboarding writing them down was the thing this wave removed.
	if plan, err := config.PlanConsentUSDAt(dir); err != nil || plan != config.DefaultPlanConsentUSD {
		t.Fatalf("the plan rail read back as %v (%v), want %v", plan, err, config.DefaultPlanConsentUSD)
	}
	if rail := config.SpendRailUSDAt(dir); rail != config.DefaultSpendRailUSD {
		t.Fatalf("the conversation ceiling read back as %v, want %v", rail, config.DefaultSpendRailUSD)
	}
	if config.SetupSeenAt(dir).IsZero() {
		t.Fatal("finishing must write the marker")
	}
	if config.APIKeyConfigured(dir) || len(*handed) != 0 {
		t.Fatal("an empty key box writes no key and hands none to the session")
	}
	if got := noteSaying(t, a, config.APIKeyEnv); !strings.Contains(got, "/settings") {
		t.Fatalf("a setup that ended keyless leaves a line pointing at /settings, got %q", got)
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
	// And on the controls screen a typed amount replaces the figure that was
	// drawn.
	if a.setup.step() != setupControls {
		t.Fatalf("a written key goes on to the controls; step = %v", a.setup.step())
	}
	for _, r := range "7.5" {
		pressSetup(a, key(string(r)))
	}
	pressSetup(a, key("enter"))
	if rail, _ := config.DailyBudgetUSDAt(dir); rail != 7.5 {
		t.Fatalf("a typed ceiling replaces the default, profile reads %v", rail)
	}
	pressSetup(a, key("tab"), key("tab"), key("tab"), key("enter"))
	if a.setup.open {
		t.Fatalf("`Start a conversation` did not finish the flow: %s", a.setup.refusal)
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
	if len(config.CrewPinsAt(dir)) != 0 || config.DailyBudgetConfigured(dir) || config.APIKeyConfigured(dir) {
		t.Fatal("skipping writes none of them")
	}
	if config.SetupSeenAt(dir).IsZero() {
		t.Fatal("skipping counts as shown")
	}
	if got := noteSaying(t, a, "/settings"); !strings.Contains(got, config.APIKeyEnv) {
		t.Fatalf("a skipped setup with no key leaves the line naming the key, got %q", got)
	}
	// Typing afterwards goes to the draft, not to a screen that is gone.
	pressSetup(a, key("h"))
	if a.setup.open || a.input.empty() {
		t.Fatal("after esc the keyboard is the conversation's")
	}
}

func TestAKeyInTheShellSkipsTheKeyStepSilently(t *testing.T) {
	t.Setenv(config.APIKeyEnv, "sk-or-v1-from-the-shell-0123456789")
	for _, pin := range []string{"OPENAI_API_KEY", "CODEAF_DAILY_BUDGET", "CODEAF_PROFILE_DIR"} {
		t.Setenv(pin, "")
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	dir := t.TempDir()
	a := newApp(t.Context(), Options{Agent: &fakeAgent{model: "openai/gpt-4.1-mini"}, Workspace: "/tmp/lab", ProfileDir: dir, Setup: true})
	a.width, a.height = 90, 30
	a.pal = newPalette(tokens.ANSI256, false)
	if !a.setup.open || len(a.setup.steps) != 1 || a.setup.steps[0] != setupControls {
		t.Fatalf("with the key in the shell only the controls are asked, got %v", a.setup.steps)
	}
	// ONE STEP SHOWS NO COUNT. `setup · 1 of 1` is the screen counting to one at
	// somebody, which is furniture drawn to mark the absence of a second step.
	if screen := setupScreen(a); strings.Contains(screen, "setup · 1 of 1") {
		t.Fatalf("a one-step setup drew a count; got:\n%s", screen)
	}
}

func TestTheSetupStaysAwayFromEveryLaunchThatIsNotAPersonArriving(t *testing.T) {
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("CODEAF_HOME", t.TempDir())
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

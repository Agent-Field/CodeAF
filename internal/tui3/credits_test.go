package tui3

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestSurfaceCancelsAndJoinsAnInFlightCreditRead(t *testing.T) {
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	reader := func(ctx context.Context) (credits.Reading, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		<-release
		return credits.Reading{}, ctx.Err()
	}
	read, closeReads := ownedCreditReader(context.Background(), reader)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		_, _ = read(context.Background())
	}()
	<-started
	closed := make(chan struct{})
	go func() {
		closeReads()
		close(closed)
	}()
	<-canceled
	select {
	case <-closed:
		t.Fatal("the surface returned while its balance read was still running")
	default:
	}
	close(release)
	<-closed
	<-readDone
	if _, err := read(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("a read after close returned %v, want cancellation", err)
	}
}

func seedLowCredits(t *testing.T, a *app) {
	t.Helper()
	if err := config.WriteAPIKey(a.profileDir, "credit-test-key"); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteCreditsReading(a.profileDir, "credit-test-key", credits.Reading{Known: true, Low: true}); err != nil {
		t.Fatal(err)
	}
}

func TestLowCreditsWarningFollowsThePaidModelOnBothBoxes(t *testing.T) {
	a := placeApp(t)
	a.width = 200
	a.readCredits = func(context.Context) (credits.Reading, error) { return credits.Reading{Known: true, Low: true}, nil }
	seedLowCredits(t, a)
	a.creditsLow = true
	a.refreshCreditWarnings()
	if !strings.Contains(plain(a.hintRow(200)), lowCreditsWarning) || !strings.Contains(placeFrameText(a), lowCreditsWarning) {
		t.Fatal("paid conversation and home boxes did not show the low-credit warning")
	}
	for _, line := range []string{plain(a.hintRow(200)), placeFrameText(a)} {
		for _, row := range strings.Split(line, "\n") {
			// RIGHT-ALIGNED: the line ends one cell in from the edge, or — where
			// the row carries the project at its right end — the group gap and the
			// project are all that follow it.
			at := strings.Index(row, lowCreditsWarning)
			if at < 0 {
				continue
			}
			after := strings.TrimRight(row[at+len(lowCreditsWarning):], " ")
			if ansi.StringWidth(strings.TrimRight(row, " ")) > 199 ||
				(after != "" && !strings.HasPrefix(after, groupGapRun+targetProjectLead)) {
				t.Fatalf("warning was not right-aligned: %q", row)
			}
		}
	}
	a.pinTargetModel(config.FreeChatModel)
	if strings.Contains(placeFrameText(a), lowCreditsWarning) {
		t.Fatal("free home model still warned")
	}
	a.switchModel(config.FreeChatModel, 0)
	if strings.Contains(plain(a.hintRow(200)), lowCreditsWarning) {
		t.Fatal("free conversation model still warned")
	}
	a.switchModel("openai/gpt-4", 0)
	if !strings.Contains(plain(a.hintRow(200)), lowCreditsWarning) {
		t.Fatal("paid model did not restore warning")
	}
	if strings.Contains(plain(a.hintRow(70)), lowCreditsWarning[:20]) {
		t.Fatal("narrow row cut the warning")
	}
}

func TestFailedCreditReadKeepsThePreviousSurfaceState(t *testing.T) {
	a := placeApp(t)
	a.readCredits = func(context.Context) (credits.Reading, error) { return credits.Reading{}, errors.New("offline") }
	seedLowCredits(t, a)
	a.creditsLow = true
	a.refreshCreditWarnings()
	before, model := a.chatCreditWarning, a.model
	a.tookCredits(creditReadMsg{err: errors.New("offline")})
	if a.creditsLow != true || a.chatCreditWarning != before || a.model != model {
		t.Fatal("failed balance read changed the model or warning")
	}
}

func TestLowCreditReadSwapsOnlyAnUntouchedImplicitConversation(t *testing.T) {
	a := placeApp(t)
	a.readCredits = func(context.Context) (credits.Reading, error) { return credits.Reading{Known: true, Low: true}, nil }
	seedLowCredits(t, a)
	a.implicitTalk = true
	a.model = config.DefaultModel
	a.tookCredits(creditReadMsg{reading: credits.Reading{Known: true, Low: true}})
	if a.model != config.FreeChatModel {
		t.Fatalf("fresh default stayed on %q", a.model)
	}
	rows, _, _ := a.setupControlsFrame(120, 30)
	if !strings.Contains(plain(strings.Join(rows, "\n")), modelWord(config.FreeChatModel)) {
		t.Fatal("setup controls did not show the newly selected free model")
	}
}

func TestLowCreditReadKeepsUsedAndChosenConversationModels(t *testing.T) {
	for _, tc := range []struct {
		name           string
		used, implicit bool
	}{
		{"sent a message", true, true},
		{"explicit talk choice", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := placeApp(t)
			a.model = config.DefaultModel
			seedLowCredits(t, a)
			a.implicitTalk = tc.implicit
			if tc.used {
				a.turn = 1
			}
			a.tookCredits(creditReadMsg{reading: credits.Reading{Known: true, Low: true}})
			if a.model != config.DefaultModel {
				t.Fatalf("chosen or used conversation moved to %q", a.model)
			}
		})
	}
}

func TestCreditReaderIsAbsentOnAnUnwiredDoorAndDirectModelsNeverWarn(t *testing.T) {
	a := placeApp(t)
	a.creditsLow = true
	a.refreshCreditWarnings()
	if a.launchCredits() != nil || strings.Contains(plain(a.hintRow(180)), lowCreditsWarning) {
		t.Fatal("unwired door read credits or warned")
	}
	a.readCredits = func(context.Context) (credits.Reading, error) { return credits.Reading{Known: true, Low: true}, nil }
	seedLowCredits(t, a)
	a.sources = modelsource.NewSet(testDefaultService("key"), testDirectService("https://direct.example/v1"))
	a.model = "deepseek-direct/deepseek-v4-pro"
	a.refreshCreditWarnings()
	if strings.Contains(plain(a.hintRow(180)), lowCreditsWarning) {
		t.Fatal("direct model inherited OpenRouter warning")
	}
}

func TestSwitchToPaidModelRequestsRefreshOnlyWhileLow(t *testing.T) {
	a := placeApp(t)
	a.readCredits = func(context.Context) (credits.Reading, error) { return credits.Reading{}, nil }
	a.creditWake = newDoorbell(creditWakeMsg{})
	a.creditTrigger = credits.NewTrigger(time.Now)
	seedLowCredits(t, a)
	a.creditsLow = true
	a.switchModel("openai/gpt-4", 0)
	if a.creditPending.Swap(0) == 0 {
		t.Fatal("paid switch while low did not request a read")
	}
	a.switchModel(config.FreeChatModel, 0)
	if a.creditPending.Load() != 0 {
		t.Fatal("free switch requested a read")
	}
	if err := config.WriteCreditsReading(a.profileDir, "credit-test-key", credits.Reading{Known: true}); err != nil {
		t.Fatal(err)
	}
	a.creditsLow = false
	a.switchModel("openai/gpt-4", 0)
	if a.creditPending.Load() != 0 {
		t.Fatal("healthy paid switch requested a read")
	}
}

func TestAFreeCatalogPriceClearsTheWarningWhenTheListArrives(t *testing.T) {
	a := placeApp(t)
	a.model = "vendor/zero"
	a.readCredits = func(context.Context) (credits.Reading, error) { return credits.Reading{}, nil }
	seedLowCredits(t, a)
	a.creditsLow = true
	var models []Model
	a.models = func() []Model { return models }
	a.refreshCreditWarnings()
	if a.chatCreditWarning == "" {
		t.Fatal("unknown price was claimed free")
	}
	models = []Model{{ID: "vendor/zero", PriceKnown: true}}
	a.modelsFetched(modelsFetchedMsg{rows: models, at: time.Now()})
	if a.chatCreditWarning != "" {
		t.Fatal("a published zero tariff did not clear the warning")
	}
}

func TestEngineRecordRefreshesAtTurnEndAndTextRefusalRequestsRead(t *testing.T) {
	a := placeApp(t)
	a.readCredits = func(context.Context) (credits.Reading, error) { return credits.Reading{Known: true, Low: true}, nil }
	a.creditWake = newDoorbell(creditWakeMsg{})
	a.creditTrigger = credits.NewTrigger(time.Now)
	a.model = "openai/gpt-4"
	seedLowCredits(t, a)
	a.creditsLow = false
	a.event(session.Event{Kind: session.EventTurnDone})
	if !strings.Contains(plain(a.hintRow(180)), lowCreditsWarning) {
		t.Fatal("engine record written behind the surface was missed at turn end")
	}
	prefix := config.ConnectionOutcomeWord("OpenRouter", modelsource.Outcome{Kind: modelsource.OutcomeAccountCannotPay})
	a.event(session.Event{Kind: session.EventError, Err: errors.New(prefix + " — top up")})
	if a.creditPending.Swap(0) == 0 {
		t.Fatal("engine's plain-text cannot-pay ending did not request a read")
	}
}

func TestHomeMessageKeepsTheWholeCreditWarning(t *testing.T) {
	a := placeApp(t)
	a.width = 200
	a.readCredits = func(context.Context) (credits.Reading, error) { return credits.Reading{}, nil }
	seedLowCredits(t, a)
	a.refreshCreditWarnings()
	a.pageMsg = "open in another window — enter again to move it here"
	row := placeFrameText(a)
	if !strings.Contains(row, "open in another window") || !strings.Contains(row, lowCreditsWarning) {
		t.Fatal("home message hid its credit warning")
	}
	for _, line := range strings.Split(row, "\n") {
		if at := strings.Index(line, lowCreditsWarning); at >= 0 && ansi.StringWidth(line[:at+len(lowCreditsWarning)]) != 199 {
			t.Fatalf("home message moved the warning off the right edge: %q", line)
		}
	}
}

func TestHomePaidTargetAndSourceReloadRefreshCredits(t *testing.T) {
	a := placeApp(t)
	a.readCredits = func(context.Context) (credits.Reading, error) { return credits.Reading{}, nil }
	a.creditWake = newDoorbell(creditWakeMsg{})
	a.creditTrigger = credits.NewTrigger(time.Now)
	seedLowCredits(t, a)
	a.pinTargetModel("openai/gpt-4")
	if a.creditPending.Swap(0) == 0 {
		t.Fatal("paid Home target did not ask for a balance read")
	}
	if !strings.Contains(placeFrameText(a), lowCreditsWarning) {
		t.Fatal("paid Home target did not show the low-credit warning")
	}
	a.sources = modelsource.NewSet(testDefaultService("key"), testDirectService("https://direct.example/v1"))
	a.model = "deepseek-direct/deepseek-v4-pro"
	a.refreshCreditWarnings()
	if a.chatCreditWarning != "" {
		t.Fatal("direct model warned")
	}
	a.reloadModelSources()
	if a.chatCreditWarning == "" {
		t.Fatal("source reload did not recompute warning")
	}
	if err := config.WriteCreditsReading(a.profileDir, "credit-test-key", credits.Reading{Known: true}); err != nil {
		t.Fatal(err)
	}
	a.tookCredits(creditReadMsg{reading: credits.Reading{Known: true}})
	if strings.Contains(placeFrameText(a), lowCreditsWarning) {
		t.Fatal("Home kept the stale warning after a top-up read")
	}
}

func TestCreditRecordBehindSurfaceArrivesAtAttachSwitchAndHomeBeat(t *testing.T) {
	for _, moment := range []string{"attach", "switch", "home beat"} {
		t.Run(moment, func(t *testing.T) {
			a := placeApp(t)
			a.readCredits = func(context.Context) (credits.Reading, error) { return credits.Reading{}, nil }
			a.model = "openai/gpt-4"
			seedLowCredits(t, a)
			a.creditsLow = false
			switch moment {
			case "attach":
				a.attachConversation(Conversation{Agent: a.agent, Workspace: a.workspace, SessionFile: a.file}, nil)
			case "switch":
				a.switchModel("openai/gpt-4", 0)
			case "home beat":
				a.homeBeat(a.homeGen)
			}
			if !a.creditsLow || a.homeCreditWarning == "" {
				t.Fatal("record written by the engine was missed")
			}
		})
	}
}

func TestNewKeyClearsTheOldKeysWarningBeforeItsRead(t *testing.T) {
	a := placeApp(t)
	a.readCredits = func(context.Context) (credits.Reading, error) { return credits.Reading{}, nil }
	seedLowCredits(t, a)
	a.refreshCreditWarnings()
	if a.homeCreditWarning == "" {
		t.Fatal("low key did not warn")
	}
	if err := config.WriteAPIKey(a.profileDir, "replacement-key"); err != nil {
		t.Fatal(err)
	}
	a.refreshCreditWarnings()
	if a.homeCreditWarning != "" || a.creditsLow {
		t.Fatal("old key's warning survived a replacement key")
	}
}

// C6 on a relaunch after a top-up (#1439, found by hand on the ordinary road):
// the engine opens the first conversation from the record as it stood — low —
// a moment before the launch read comes back healthy. A conversation nobody has
// used and nobody chose a model for is the build's default, so it follows the
// default back; one that sent a message, or whose model was chosen, stays put.
func TestAHealthyReadMovesAnUntouchedImplicitConversationBackToTheDefault(t *testing.T) {
	for _, tc := range []struct {
		name           string
		used, implicit bool
		want           string
	}{
		{"untouched and implicit", false, true, config.DefaultModel},
		{"sent a message", true, true, config.FreeChatModel},
		{"model chosen", false, false, config.FreeChatModel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := placeApp(t)
			a.readCredits = func(context.Context) (credits.Reading, error) { return credits.Reading{Known: true}, nil }
			if err := config.WriteAPIKey(a.profileDir, "credit-test-key"); err != nil {
				t.Fatal(err)
			}
			if err := config.WriteCreditsReading(a.profileDir, "credit-test-key", credits.Reading{Known: true}); err != nil {
				t.Fatal(err)
			}
			a.implicitTalk = tc.implicit
			a.model = config.FreeChatModel
			if tc.used {
				a.turn = 1
			}
			a.tookCredits(creditReadMsg{reading: credits.Reading{Known: true}})
			if a.model != tc.want {
				t.Fatalf("after a healthy read the conversation is on %q, want %q", a.model, tc.want)
			}
			if saved := config.ChatModelAt(a.profileDir); saved != "" {
				t.Fatalf("following the default wrote a talk model: %q", saved)
			}
		})
	}
}

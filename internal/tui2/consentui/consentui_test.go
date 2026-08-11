package consentui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/consent"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The tests speak in the vocabulary the desk actually uses. Every fixture below
// is built from internal/consent's own exported answer constants rather than
// from strings retyped here, so a change to the desk's wording breaks this file
// instead of silently making the dialog answer a question nobody asked.

func consentRow() store.AgentQuestion {
	allowFree := false
	options := []store.QuestionOption{
		{Label: consent.Approve},
		{Label: consent.Hold, Hint: "the plan stays as it is; cancel the parts you don't want, then say go"},
	}
	return store.AgentQuestion{
		Seq:       41,
		SessionID: "s1",
		Category:  consent.Category,
		Class:     store.QuestionConsent,
		Urgency:   store.QuestionBlocking,
		Text: store.QuestionMessageBody(
			"refactor auth comes to 48 steps, about $2.10 at what work like this has cost here. Start it, or trim it first?",
			options, store.QuestionConfig{
				Kind: store.QuestionConfirm, Category: consent.Category,
				Default: "1", AllowFree: &allowFree,
			}),
		Options:       options,
		DefaultAnswer: "1",
	}
}

func consentQuestion() Question {
	return FromStore(consentRow(), Presentation{
		Consequence: "cancel 4 running workers, ~$2.10 in flight",
		Reject:      consent.Hold,
		DetailTitle: "plan.md",
		Detail: []DetailLine{
			{Kind: DetailContext, Text: "  step 3: migrate schema"},
			{Kind: DetailAdd, Text: "step 4: backfill users"},
			{Kind: DetailDel, Text: "step 5: drop legacy table"},
		},
	})
}

type harness struct {
	model    *Model
	results  []Result
	closes   int
	invalids int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{}
	h.model = New(Options{
		OnAnswer:   func(r Result) tea.Cmd { h.results = append(h.results, r); return nil },
		OnClose:    func() tea.Cmd { h.closes++; return nil },
		Invalidate: func() { h.invalids++ },
	})
	h.model.Focus(true)
	return h
}

// press feeds one key the way Bubble Tea would. A single printable rune has to
// arrive with its Text set, because that is what the mnemonic path reads.
func press(t *testing.T, m *Model, name string) tea.Cmd {
	t.Helper()
	key := tea.Key{Code: tea.KeyEnter}
	switch name {
	case "enter":
	case "esc":
		key = tea.Key{Code: tea.KeyEscape}
	case "up":
		key = tea.Key{Code: tea.KeyUp}
	case "down":
		key = tea.Key{Code: tea.KeyDown}
	case "backspace":
		key = tea.Key{Code: tea.KeyBackspace}
	case "tab":
		key = tea.Key{Code: tea.KeyTab}
	default:
		r := []rune(name)
		if len(r) != 1 {
			t.Fatalf("press: %q is not one key", name)
		}
		key = tea.Key{Code: r[0], Text: name}
	}
	return m.Key(tea.KeyPressMsg(key))
}

func typeText(t *testing.T, m *Model, text string) {
	t.Helper()
	for _, r := range text {
		press(t, m, string(r))
	}
}

// -- the durable half ---------------------------------------------------------

func TestFromStoreKeepsTheQuestionVerbatimAndDropsOnlyTheMachineCopy(t *testing.T) {
	q := consentQuestion()
	const asked = "refactor auth comes to 48 steps, about $2.10 at what work like this has cost here. Start it, or trim it first?"
	if q.Prompt != asked {
		t.Fatalf("prompt is not the question as asked:\n got %q\nwant %q", q.Prompt, asked)
	}
	if strings.Contains(q.Prompt, "```") || strings.Contains(q.Prompt, "\"kind\"") {
		t.Fatalf("the machine payload leaked into the prompt: %q", q.Prompt)
	}
	if len(q.Options) != 2 ||
		q.Options[0].Label != consent.Approve || q.Options[1].Label != consent.Hold {
		t.Fatalf("options are not the durable ones: %+v", q.Options)
	}
	if q.Default != 1 {
		t.Fatalf("default = %d, want the durable 1", q.Default)
	}
	if q.Options[0].Rejecting || !q.Options[1].Rejecting {
		t.Fatalf("the refusal is not the one the caller named: %+v", q.Options)
	}
}

func TestPromptOfKeepsACodeBlockThatIsNotThePayload(t *testing.T) {
	body := "run this?\n\n```\nrm -rf build\n```"
	if got := PromptOf(body); got != body {
		t.Fatalf("a quoted code block was eaten:\n got %q\nwant %q", got, body)
	}
}

// -- mnemonics ----------------------------------------------------------------

func TestMnemonicsComeOutOfTheQuestionsOwnLabels(t *testing.T) {
	q := consentQuestion()
	if q.Options[0].Key != "y" || q.Options[1].Key != "h" {
		t.Fatalf("consent letters = %q/%q, want y/h from the desk's own labels",
			q.Options[0].Key, q.Options[1].Key)
	}
}

func TestAnAllowSessionDenyVocabularyLandsOnASD(t *testing.T) {
	// 10.4.17's example letters, arrived at by the rule rather than hardcoded.
	q := Question{Options: []Option{
		{Label: "allow"}, {Label: "allow for this session"}, {Label: "deny"},
	}}.normalize()
	got := q.Options[0].Key + q.Options[1].Key + q.Options[2].Key
	if got != "asd" {
		t.Fatalf("letters = %q, want asd", got)
	}
}

func TestMnemonicsNeverShadowTheDialogsOwnKeys(t *testing.T) {
	q := Question{Options: []Option{{Label: "toggle"}, {Label: "finish"}}}.normalize()
	for _, option := range q.Options {
		if option.Key == "t" || option.Key == "f" {
			t.Fatalf("option %q took the dialog's own key %q", option.Label, option.Key)
		}
	}
}

func TestALetterAnswersAndAdvancesTheQueue(t *testing.T) {
	h := newHarness(t)
	h.model.Push(consentQuestion())
	press(t, h.model, "y")

	if len(h.results) != 1 {
		t.Fatalf("results = %d, want 1", len(h.results))
	}
	got := h.results[0]
	if got.Body != consent.Approve {
		t.Fatalf("body = %q, want the durable label %q", got.Body, consent.Approve)
	}
	if got.QuestionSeq != 41 || got.SessionID != "s1" {
		t.Fatalf("result does not name its question: %+v", got)
	}
	if got.Rejected || got.Steering != "" {
		t.Fatalf("an approval must carry no rejection and no steering: %+v", got)
	}
	if h.model.Open() || h.model.Pending() != 0 {
		t.Fatalf("the queue did not drain")
	}
	if h.closes != 1 {
		t.Fatalf("closes = %d, want 1", h.closes)
	}
}

func TestADigitAnswersTheSameWayAsALetter(t *testing.T) {
	h := newHarness(t)
	h.model.Push(consentQuestion())
	press(t, h.model, "2")
	// Option 2 is the refusal, so this opens steering rather than settling.
	if h.model.mode != modeSteer {
		t.Fatalf("mode = %v, want the steering path", h.model.mode)
	}
}

// -- typed rejection is steering (10.4.16) ------------------------------------

func TestRejectingOpensSteeringAndTheRedirectRidesOut(t *testing.T) {
	h := newHarness(t)
	h.model.Push(consentQuestion())

	press(t, h.model, "h")
	if h.model.mode != modeSteer {
		t.Fatalf("rejecting did not open the redirect")
	}
	if len(h.results) != 0 {
		t.Fatalf("a rejection settled before the redirect was typed")
	}
	typeText(t, h.model, "trim it to the schema step only")
	press(t, h.model, "enter")

	if len(h.results) != 1 {
		t.Fatalf("results = %d, want 1", len(h.results))
	}
	got := h.results[0]
	if !got.Rejected {
		t.Fatalf("the refusal did not report itself: %+v", got)
	}
	if got.Body != consent.Hold {
		t.Fatalf("body = %q, want %q", got.Body, consent.Hold)
	}
	if got.Steering != "trim it to the schema step only" {
		t.Fatalf("steering = %q", got.Steering)
	}
}

func TestADeniedActionStillCarriesWhatWasRejected(t *testing.T) {
	h := newHarness(t)
	h.model.Push(consentQuestion())
	press(t, h.model, "h")
	press(t, h.model, "enter")

	if len(h.results) != 1 {
		t.Fatalf("results = %d, want 1", len(h.results))
	}
	got := h.results[0]
	if len(got.Detail) != 3 || got.DetailTitle != "plan.md" {
		t.Fatalf("the rejected detail did not survive the denial: %+v", got)
	}
}

func TestAnEmptyRedirectIsStillARejection(t *testing.T) {
	h := newHarness(t)
	h.model.Push(consentQuestion())
	press(t, h.model, "h")
	press(t, h.model, "enter")
	if len(h.results) != 1 || !h.results[0].Rejected {
		t.Fatalf("a bare no was refused: %+v", h.results)
	}
}

// -- scope shown before it is granted (10.4.18) -------------------------------

func scopedQuestion() Question {
	return Question{
		Seq: 7, SessionID: "s1",
		Prompt: "run git status?",
		Options: []Option{
			{Label: "allow once"},
			{Label: "allow always", Scope: []string{"bash(git status:*)", "bash(git diff:*)"}},
			{Label: "deny", Rejecting: true},
		},
	}.normalize()
}

func TestAllowAlwaysListsItsPatternsBeforeItIsGranted(t *testing.T) {
	h := newHarness(t)
	h.model.Push(scopedQuestion())

	press(t, h.model, "2")
	if h.model.mode != modeScope {
		t.Fatalf("an always-grant went straight through without showing its scope")
	}
	if len(h.results) != 0 {
		t.Fatalf("the grant was taken before it was shown")
	}
	frame := h.model.Render(100, 30)
	for _, pattern := range []string{"bash(git status:*)", "bash(git diff:*)"} {
		if !strings.Contains(frame, pattern) {
			t.Fatalf("the frame does not list %q:\n%s", pattern, frame)
		}
	}
	press(t, h.model, "enter")
	if len(h.results) != 1 {
		t.Fatalf("results = %d, want 1", len(h.results))
	}
	if strings.Join(h.results[0].Scope, ",") != "bash(git status:*),bash(git diff:*)" {
		t.Fatalf("scope = %v", h.results[0].Scope)
	}
}

func TestScopeEditingIsBehindTheFullscreenEscalation(t *testing.T) {
	h := newHarness(t)
	h.model.Push(scopedQuestion())
	press(t, h.model, "2")
	if h.model.full {
		t.Fatalf("the scope list started fullscreen; the fast path must not")
	}
	press(t, h.model, "e")
	if h.model.mode != modeScopeEdit || !h.model.full {
		t.Fatalf("editing did not escalate to the full frame: mode=%v full=%v", h.model.mode, h.model.full)
	}
	press(t, h.model, "backspace")
	press(t, h.model, "enter")
	if h.model.scope[0] != "bash(git status:*" {
		t.Fatalf("the edit did not commit: %v", h.model.scope)
	}
	press(t, h.model, "enter")
	if len(h.results) != 1 || h.results[0].Scope[0] != "bash(git status:*" {
		t.Fatalf("the confirmed scope is not the edited one: %+v", h.results)
	}
	if scopedQuestion().Options[1].Scope[0] != "bash(git status:*)" {
		t.Fatalf("the edit reached back into the durable option")
	}
}

func TestEscInTheScopeListGrantsNothing(t *testing.T) {
	h := newHarness(t)
	h.model.Push(scopedQuestion())
	press(t, h.model, "2")
	press(t, h.model, "esc")
	if h.model.mode != modeAnswer {
		t.Fatalf("esc did not return to the answers")
	}
	if len(h.results) != 0 {
		t.Fatalf("esc granted something: %+v", h.results)
	}
	if !h.model.Open() {
		t.Fatalf("esc in the scope list closed the whole dialog")
	}
}

// -- fullscreen threshold (10.4.17) -------------------------------------------

func TestFullscreenIsForcedBelowTheStatedThreshold(t *testing.T) {
	for _, tc := range []struct {
		w, h  int
		want  bool
		label string
	}{
		{100, 40, false, "roomy"},
		{71, 40, true, "one column under the width threshold"},
		{100, 19, true, "one row under the height threshold"},
		{72, 20, false, "exactly at both thresholds"},
	} {
		if got := ForcedFullscreen(tc.w, tc.h); got != tc.want {
			t.Fatalf("%s: ForcedFullscreen(%d,%d) = %v, want %v", tc.label, tc.w, tc.h, got, tc.want)
		}
	}
}

func TestBelowTheThresholdTheFullscreenKeyIsNotAdvertised(t *testing.T) {
	h := newHarness(t)
	h.model.Push(consentQuestion())
	if !strings.Contains(h.model.Render(100, 30), "f full") {
		t.Fatalf("a roomy frame did not offer the fullscreen key")
	}
	if strings.Contains(h.model.Render(60, 30), "f full") {
		t.Fatalf("a forced-fullscreen frame advertised a key that cannot do anything")
	}
}

// -- esc and the draft law (8.2.21) -------------------------------------------

func TestEscClosesTheDialogAndStashesTypedSteering(t *testing.T) {
	h := newHarness(t)
	h.model.Push(consentQuestion())
	press(t, h.model, "h")
	typeText(t, h.model, "use the cheaper model")
	press(t, h.model, "esc") // back to the answers, draft still in hand
	if h.model.Draft(41) != "use the cheaper model" {
		t.Fatalf("backing out of the redirect lost the draft: %q", h.model.Draft(41))
	}
	press(t, h.model, "esc") // close the dialog
	if h.model.Open() {
		t.Fatalf("esc did not close the dialog")
	}
	if len(h.results) != 0 {
		t.Fatalf("esc answered the question: %+v", h.results)
	}
	if h.model.Draft(41) != "use the cheaper model" {
		t.Fatalf("esc destroyed the stashed draft: %q", h.model.Draft(41))
	}
	h.model.Reopen()
	if !h.model.Open() || h.model.Pending() != 1 {
		t.Fatalf("reopening lost the question")
	}
	press(t, h.model, "h")
	press(t, h.model, "enter")
	if len(h.results) != 1 || h.results[0].Steering != "use the cheaper model" {
		t.Fatalf("the restored draft did not become the redirect: %+v", h.results)
	}
}

// -- the queue ----------------------------------------------------------------

func secondQuestion() Question {
	return Question{
		Seq: 42, SessionID: "s1", Prompt: "deploy to production?",
		Options: []Option{{Label: "ship it"}, {Label: "not yet", Rejecting: true}},
	}
}

func TestAnsweringAdvancesTheQueueAndTheCountFalls(t *testing.T) {
	h := newHarness(t)
	h.model.Push(consentQuestion())
	h.model.Push(secondQuestion())
	if h.model.Pending() != 2 {
		t.Fatalf("pending = %d, want 2", h.model.Pending())
	}
	if !strings.Contains(h.model.Render(100, 30), "2 waiting") {
		t.Fatalf("the waiting count is not on screen:\n%s", h.model.Render(100, 30))
	}

	press(t, h.model, "y")
	current, ok := h.model.Current()
	if !ok || current.Seq != 42 {
		t.Fatalf("the queue did not advance to the next question: %+v", current)
	}
	if h.model.Pending() != 1 || h.closes != 0 {
		t.Fatalf("the dialog closed with a question still waiting")
	}
	if strings.Contains(h.model.Render(100, 30), "waiting") {
		t.Fatalf("a lone question still advertises a count")
	}
	press(t, h.model, "s")
	if len(h.results) != 2 || h.results[1].Body != "ship it" {
		t.Fatalf("second answer = %+v", h.results)
	}
	if h.model.Open() || h.closes != 1 {
		t.Fatalf("the drained queue did not close exactly once")
	}
}

func TestPushingTheSameQuestionTwiceDoesNotStackIt(t *testing.T) {
	h := newHarness(t)
	h.model.Push(consentQuestion())
	h.model.Push(consentQuestion())
	if h.model.Pending() != 1 {
		t.Fatalf("pending = %d, want 1", h.model.Pending())
	}
}

func TestRemoveDropsAQuestionAnsweredSomewhereElse(t *testing.T) {
	h := newHarness(t)
	h.model.Push(consentQuestion())
	h.model.Push(secondQuestion())
	h.model.Remove(41)
	current, ok := h.model.Current()
	if !ok || current.Seq != 42 {
		t.Fatalf("remove did not advance: %+v", current)
	}
	h.model.Remove(42)
	if h.model.Open() || h.model.Pending() != 0 {
		t.Fatalf("remove left the dialog open with nothing to show")
	}
	if len(h.results) != 0 {
		t.Fatalf("remove answered something: %+v", h.results)
	}
}

// -- detail toggle ------------------------------------------------------------

func TestTTogglesTheDetailViewAndIsInertWithoutOne(t *testing.T) {
	h := newHarness(t)
	h.model.Push(consentQuestion())
	if strings.Contains(h.model.Render(100, 40), "backfill users") {
		t.Fatalf("the detail view was open before it was asked for")
	}
	press(t, h.model, "t")
	frame := h.model.Render(100, 40)
	for _, want := range []string{"plan.md", "backfill users", "drop legacy table"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("detail row %q missing:\n%s", want, frame)
		}
	}
	press(t, h.model, "t")
	if strings.Contains(h.model.Render(100, 40), "backfill users") {
		t.Fatalf("t did not close the detail view")
	}

	h2 := newHarness(t)
	h2.model.Push(secondQuestion())
	press(t, h2.model, "t")
	if h2.model.detail {
		t.Fatalf("t opened a detail view on a question that has none")
	}
	if strings.Contains(h2.model.Render(100, 30), "t detail") {
		t.Fatalf("the strip offered a detail key on a question with no detail")
	}
}

// -- what is on screen --------------------------------------------------------

func TestTheFrameNamesTheBlastRadiusAndTheAnswers(t *testing.T) {
	h := newHarness(t)
	h.model.Push(consentQuestion())
	frame := h.model.Render(100, 30)
	for _, want := range []string{
		"refactor auth comes to 48 steps",
		"cancel 4 running workers, ~$2.10 in flight",
		consent.Approve,
		consent.Hold,
		"esc later",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("frame is missing %q:\n%s", want, frame)
		}
	}
}

func TestASelectedRowIsABandAndNeverABox(t *testing.T) {
	h := newHarness(t)
	h.model.Push(consentQuestion())
	frame := h.model.Render(100, 30)
	for _, banned := range []string{"┌", "└", "│", "─", "╭"} {
		if strings.Contains(frame, banned) {
			t.Fatalf("the dialog drew a box (%q):\n%s", banned, frame)
		}
	}
}

// -- the production bar -------------------------------------------------------

func TestRenderNeverPanicsAndNeverOverrunsAcrossAWidthSweep(t *testing.T) {
	questions := []Question{consentQuestion(), scopedQuestion(), secondQuestion()}
	modes := []struct {
		name string
		keys []string
	}{
		{"answer", nil},
		{"detail", []string{"t"}},
		{"scope", []string{"2"}},
		{"scope-edit", []string{"2", "e"}},
		{"steer", []string{"h"}},
		{"steer-typed", []string{"h", "a", "b", "c"}},
	}
	for _, q := range questions {
		for _, mode := range modes {
			for width := 0; width <= 140; width++ {
				for _, height := range []int{0, 1, 2, 3, 5, 8, 13, 21, 40} {
					h := newHarness(t)
					h.model.Push(q)
					for _, key := range mode.keys {
						press(t, h.model, key)
					}
					frame := h.model.Render(width, height)
					if frame == "" {
						continue
					}
					rows := strings.Split(frame, "\n")
					if len(rows) > height {
						t.Fatalf("%s q%d %dx%d: %d rows overrun the frame",
							mode.name, q.Seq, width, height, len(rows))
					}
					for i, row := range rows {
						if got := blocks.Width(row); got > width {
							t.Fatalf("%s q%d %dx%d: row %d is %d cells wide\n%q",
								mode.name, q.Seq, width, height, i, got, row)
						}
					}
				}
			}
		}
	}
}

func TestAmberIsSpentOnlyOnNeedsAHuman(t *testing.T) {
	h := newHarness(t)
	h.model.style = tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	h.model.Push(consentQuestion())
	h.model.Push(secondQuestion())
	frame := h.model.Render(100, 30)

	amber := tokens.Amber.Fg(tokens.TrueColor, tokens.FocusNormal)
	if !strings.Contains(frame, amber+tokens.GlyphNeedsHuman) {
		t.Fatalf("the ? glyph is not amber:\n%q", frame)
	}
	if !strings.Contains(frame, amber+" "+tokens.GlyphSeparator+" 2 waiting") {
		t.Fatalf("the waiting count is not amber:\n%q", frame)
	}
	// Exactly two amber runs — the glyph and the count. A third would mean the
	// one word that means "a human is needed" had started meaning something
	// else (5.16).
	if got := strings.Count(frame, amber); got != 2 {
		t.Fatalf("amber runs = %d, want 2:\n%q", got, frame)
	}
	// And all three grey tiers are in use: the question, its consequence, and
	// the chrome (5.13).
	for _, tier := range []tokens.Token{tokens.TextPrimary, tokens.TextSecondary, tokens.TextTertiary} {
		if !strings.Contains(frame, tier.Fg(tokens.TrueColor, tokens.FocusNormal)) {
			t.Fatalf("tier %v is unused:\n%q", tier, frame)
		}
	}
}

func TestASelectionIsAlwaysVisible(t *testing.T) {
	// A band made of spaces is not a selection. Wherever the raised ground
	// cannot be drawn — no colour, linear mode, an unfocused pane — the accent
	// rail marks the row instead (5.21).
	for _, tc := range []struct {
		name  string
		setup func(*Model)
	}{
		{"no styler", func(*Model) {}},
		{"no colour", func(m *Model) { m.style = tokens.NewStyler(tokens.NoColor, tokens.FocusNormal) }},
		{"linear", func(m *Model) {
			m.style = tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
			m.linear = true
		}},
		{"unfocused", func(m *Model) {
			m.style = tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
			m.Focus(false)
		}},
	} {
		h := newHarness(t)
		tc.setup(h.model)
		h.model.Push(consentQuestion())
		frame := h.model.Render(100, 30)
		if !strings.Contains(frame, tokens.GlyphAccentRail+" y") {
			t.Fatalf("%s: the selection is invisible:\n%s", tc.name, frame)
		}
	}

	h := newHarness(t)
	h.model.style = tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	h.model.Push(consentQuestion())
	if strings.Contains(h.model.Render(100, 30), tokens.GlyphAccentRail) {
		t.Fatalf("a focused colour terminal drew the fallback marker instead of the band")
	}
}

func TestATightFrameGivesUpTheHintBeforeTheQuestion(t *testing.T) {
	h := newHarness(t)
	h.model.Push(consentQuestion())
	press(t, h.model, "tab") // select the option that carries a hint
	roomy := h.model.Render(76, 20)
	if !strings.Contains(roomy, "the plan stays as it is") {
		t.Fatalf("a roomy frame dropped the hint:\n%s", roomy)
	}
	tight := h.model.Render(40, 8)
	if strings.Contains(tight, "the plan stays as it is") {
		t.Fatalf("a tight frame kept the hint:\n%s", tight)
	}
	if !strings.Contains(tight, "refactor auth comes to 48 steps") {
		t.Fatalf("the question lost rows to the hint:\n%s", tight)
	}
	for _, want := range []string{consent.Approve, consent.Hold} {
		if !strings.Contains(tight, want) {
			t.Fatalf("a tight frame dropped the answer %q:\n%s", want, tight)
		}
	}
}

func TestATrimmedQuestionSaysThatItWasTrimmed(t *testing.T) {
	h := newHarness(t)
	h.model.Push(consentQuestion())
	frame := h.model.Render(40, 4)
	if !strings.Contains(frame, tokens.GlyphTruncated) && !strings.Contains(frame, "…") {
		t.Fatalf("a shortened consent question hid the fact:\n%s", frame)
	}
}

func TestKeysOnAClosedDialogDoNothing(t *testing.T) {
	h := newHarness(t)
	if cmd := press(t, h.model, "y"); cmd != nil {
		t.Fatalf("a closed dialog answered a key")
	}
	if _, ok := h.model.Current(); ok {
		t.Fatalf("an empty dialog reported a current question")
	}
	if h.model.Render(80, 24) != "" {
		t.Fatalf("an empty dialog drew something")
	}
}

package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func structuredQuestionCard(t *testing.T, body string, at time.Time) jobCard {
	t.Helper()
	command := store.Command{
		Seq: 41, Time: at.Add(-time.Minute), SessionID: "structured-questions",
		Kind: store.CommandSplice, Instruction: "ask a structured question", Status: store.CommandRejected,
	}
	message := store.Message{
		Seq: 42, Time: at, SessionID: command.SessionID, Role: store.RoleAgent,
		CommandSeq: command.Seq, Body: body,
	}
	cards := deriveJobCards(command.SessionID, store.Snapshot{}, []store.Message{message}, nil, nil,
		map[int64]store.Command{command.Seq: command})
	return requireCard(t, cards, fmt.Sprintf("command:%d", command.Seq))
}

func answerFlowCard(id string, birth int64, kind questionKind, defaultOption string) jobCard {
	return jobCard{
		ID: id, State: cardQuestion, QuestionKind: kind, Question: "Choose for " + id,
		BirthSeq: birth, Default: defaultOption, Options: []questionOption{
			{Number: 1, Key: id + "-one", Label: "one", Reply: id + "-reply-one"},
			{Number: 2, Key: id + "-two", Label: "two", Reply: id + "-reply-two"},
		},
	}
}

func requireQuestionPost(t *testing.T, model *Model, backend *fakeBackend, key tea.KeyMsg) store.Message {
	t.Helper()
	_, command := model.Update(key)
	if command == nil {
		t.Fatal("answer key did not return a post command")
	}
	_, _ = model.Update(command())
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.posted) != 1 {
		t.Fatalf("posted messages = %#v, want exactly one", backend.posted)
	}
	return backend.posted[0]
}

func TestQuestionDigitAnswersWithoutNavigationFromEveryFocusZone(t *testing.T) {
	for _, test := range []struct {
		name     string
		focus    paneFocus
		nodeView bool
	}{
		{name: "input", focus: focusInput},
		{name: "thread", focus: focusChat},
		{name: "rail", focus: focusGraph},
		{name: "worker activity", focus: focusInput, nodeView: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := &fakeBackend{}
			model := New(backend, "zero-navigation")
			model.cards = []jobCard{answerFlowCard("question", 1, questionChoose, "")}
			model.focus = test.focus
			model.inputFocused = test.focus == focusInput
			if model.inputFocused {
				_ = model.input.Focus()
			} else {
				model.input.Blur()
			}
			if test.focus == focusGraph {
				model.graphOpen = true
			}
			if test.nodeView {
				model.nodeViewID = "worker"
			}
			model.setSize(110, 30)

			posted := requireQuestionPost(t, model, backend,
				tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
			if posted.Role != store.RoleUser || posted.Body != "question-reply-two" {
				t.Fatalf("posted answer = %+v", posted)
			}
			if model.questionSelection["question"] != 1 {
				t.Fatalf("acted question selection = %d, want second option flash", model.questionSelection["question"])
			}
		})
	}
}

// This is the reported journey that the old routing broke: once the visible
// question and the keyboard focus lived in different zones, a bare digit was
// offered only to the rail viewport and never reached the option card.
func TestRegressionVisibleQuestionAcceptsFirstOptionWhileRailHasFocus(t *testing.T) {
	backend := &fakeBackend{}
	model := New(backend, "reported-journey")
	model.cards = []jobCard{answerFlowCard("fresh", 1, questionChoose, "")}
	model.toggleGraph()
	if model.focus != focusGraph || model.inputFocused {
		t.Fatalf("precondition focus=%v inputFocused=%t", model.focus, model.inputFocused)
	}
	posted := requireQuestionPost(t, model, backend,
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if posted.Body != "fresh-reply-one" {
		t.Fatalf("first option posted %q", posted.Body)
	}
}

func TestQuestionDigitTargetsNewestUnlessPendingCardIsFocused(t *testing.T) {
	oldQuestion := answerFlowCard("old", 1, questionChoose, "")
	newQuestion := answerFlowCard("new", 2, questionChoose, "")

	newestBackend := &fakeBackend{}
	newestModel := New(newestBackend, "newest-question")
	newestModel.cards = []jobCard{oldQuestion, newQuestion}
	posted := requireQuestionPost(t, newestModel, newestBackend,
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if posted.Body != "new-reply-two" || newestModel.questionSelection["new"] != 1 {
		t.Fatalf("unfocused target posted=%q selections=%v", posted.Body, newestModel.questionSelection)
	}

	focusedBackend := &fakeBackend{}
	focusedModel := New(focusedBackend, "focused-question")
	focusedModel.cards = []jobCard{oldQuestion, newQuestion}
	focusedModel.focus = focusCards
	focusedModel.inputFocused = false
	focusedModel.input.Blur()
	focusedModel.selectedCardID = "old"
	posted = requireQuestionPost(t, focusedModel, focusedBackend,
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if posted.Body != "old-reply-two" || focusedModel.questionSelection["old"] != 1 {
		t.Fatalf("focused target posted=%q selections=%v", posted.Body, focusedModel.questionSelection)
	}
}

func TestQuestionDigitWithDraftContentKeepsTyping(t *testing.T) {
	backend := &fakeBackend{}
	model := New(backend, "draft-digit")
	model.cards = []jobCard{answerFlowCard("question", 1, questionChoose, "")}
	model.input.SetValue("draft")
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if command != nil || model.input.Value() != "draft1" {
		t.Fatalf("digit with draft command=%v value=%q", command, model.input.Value())
	}
	if len(backend.posted) != 0 {
		t.Fatalf("digit with draft posted %#v", backend.posted)
	}
}

func TestQuestionExactNumberDraftResolvesLocallyButOtherDraftsStayFreeText(t *testing.T) {
	localBackend := &fakeBackend{}
	localModel := New(localBackend, "local-number")
	localModel.cards = []jobCard{answerFlowCard("question", 1, questionChoose, "")}
	localModel.input.SetValue("2")
	posted := requireQuestionPost(t, localModel, localBackend, tea.KeyMsg{Type: tea.KeyEnter})
	if posted.Body != "question-reply-two" || posted.Body == "2" || localModel.input.Value() != "" {
		t.Fatalf("exact-number post=%+v input=%q", posted, localModel.input.Value())
	}

	freeBackend := &fakeBackend{}
	freeModel := New(freeBackend, "free-text")
	freeModel.cards = []jobCard{answerFlowCard("question", 1, questionChoose, "")}
	freeModel.input.SetValue("2 but change the title")
	posted = requireQuestionPost(t, freeModel, freeBackend, tea.KeyMsg{Type: tea.KeyEnter})
	if posted.Body != "2 but change the title" {
		t.Fatalf("non-matching draft posted %q", posted.Body)
	}
}

func TestConfirmEnterAcceptsMarkedDefaultFromEveryFocusZone(t *testing.T) {
	for _, test := range []struct {
		name  string
		focus paneFocus
	}{
		{name: "input", focus: focusInput},
		{name: "thread", focus: focusChat},
		{name: "rail", focus: focusGraph},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := &fakeBackend{}
			model := New(backend, "confirm-default")
			model.cards = []jobCard{answerFlowCard("confirm", 1, questionConfirm, "confirm-two")}
			model.focus = test.focus
			model.inputFocused = test.focus == focusInput
			if !model.inputFocused {
				model.input.Blur()
			}
			if test.focus == focusGraph {
				model.graphOpen = true
			}
			posted := requireQuestionPost(t, model, backend, tea.KeyMsg{Type: tea.KeyEnter})
			if posted.Body != "confirm-reply-two" || model.questionSelection["confirm"] != 1 {
				t.Fatalf("default post=%q selections=%v", posted.Body, model.questionSelection)
			}
		})
	}
}

func TestPendingOptionQuestionSwapsInputPlaceholderInAndOut(t *testing.T) {
	model := New(&fakeBackend{}, "question-placeholder")
	model.cards = []jobCard{answerFlowCard("question", 1, questionChoose, "")}
	withQuestion := ansi.Strip(model.renderInput())
	if !strings.Contains(withQuestion, "1–2 to answer · or type your own") ||
		strings.Contains(withQuestion, "Ask the graph…") {
		t.Fatalf("pending-question input = %q", withQuestion)
	}

	model.cards[0].State = cardSettled
	withoutQuestion := ansi.Strip(model.renderInput())
	baseline := ansi.Strip(New(&fakeBackend{}, "question-placeholder-baseline").renderInput())
	if !strings.Contains(withoutQuestion, "Ask the graph…") ||
		strings.Contains(withoutQuestion, "to answer · or type your own") || withoutQuestion != baseline {
		t.Fatalf("resolved-question input = %q, baseline %q", withoutQuestion, baseline)
	}
}

func TestQuestionShortcutsDoNotCaptureInsideHelpOrPalette(t *testing.T) {
	t.Run("help", func(t *testing.T) {
		backend := &fakeBackend{}
		model := New(backend, "question-help-modal")
		model.cards = []jobCard{answerFlowCard("question", 1, questionChoose, "")}
		model.openHelp()
		_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
		if command != nil || len(backend.posted) != 0 || len(model.questionSelection) != 0 {
			t.Fatalf("help digit command=%v posts=%v selections=%v", command, backend.posted, model.questionSelection)
		}
	})

	t.Run("palette", func(t *testing.T) {
		backend := &fakeBackend{}
		model := NewWithCommander(backend, "question-palette-modal", newFakeCommander())
		model.cards = []jobCard{answerFlowCard("question", 1, questionChoose, "")}
		_ = model.openModelsPalette()
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
		if len(backend.posted) != 0 || len(model.questionSelection) != 0 || model.palette != paletteModel {
			t.Fatalf("palette digit posts=%v selections=%v palette=%v", backend.posted, model.questionSelection, model.palette)
		}
	})
}

func TestResolvedQuestionReleasesDigitToInput(t *testing.T) {
	backend := &fakeBackend{}
	model := New(backend, "resolved-question")
	resolved := answerFlowCard("resolved", 1, questionChoose, "")
	resolved.State = cardSettled
	model.cards = []jobCard{resolved}
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if command != nil || model.input.Value() != "1" || len(backend.posted) != 0 {
		t.Fatalf("resolved digit command=%v input=%q posts=%v", command, model.input.Value(), backend.posted)
	}
}

func TestStructuredChooseQuestionRendersHintsAndHandlesKeysAndClicks(t *testing.T) {
	now := time.Now()
	card := structuredQuestionCard(t, "metadata follows\n```json\n"+
		`{"kind":"choose","prompt":"Pick a route","options":[`+
		`{"key":1,"label":"fast","hint":"lowest latency"},`+
		`{"key":"2","label":"careful","hint":"extra verification"}],`+
		`"default":"2","allowFree":false}`+"\n```\n▸ 9 fallback must lose", now)
	if card.QuestionKind != questionChoose || card.Question != "Pick a route" || len(card.Options) != 2 ||
		card.Options[0].Hint != "lowest latency" || card.AllowFree {
		t.Fatalf("structured choose decoded as %#v", card)
	}

	backend := &fakeBackend{}
	model := New(backend, "structured-questions")
	model.cards = []jobCard{card}
	model.setSize(90, 30)
	dock := ansi.Strip(model.renderCardDock(true))
	for _, want := range []string{"Pick a route", "▸ 1 fast · lowest latency", "▸ 2 careful · extra verification"} {
		if !strings.Contains(dock, want) {
			t.Fatalf("choose component is missing %q:\n%s", want, dock)
		}
	}
	if got := model.questionSelection[card.ID]; got != 1 {
		t.Fatalf("default selection = %d, want option 2", got)
	}

	// A number shortcut follows the structured key, while an ordinary draft
	// still takes the normal chat path even when allowFree is false.
	_, numberPost := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if numberPost == nil {
		t.Fatal("structured choose number key did not post")
	}
	_ = numberPost()
	model.input.SetValue("my own answer")
	_, freePost := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if freePost == nil {
		t.Fatal("non-empty draft did not keep the free-text-always-wins path")
	}
	_ = freePost()

	// Re-render to establish row bounds, then click the second option.
	model.input.Reset()
	_ = model.View()
	var second cardOptionRow
	for _, row := range model.cardOptionRows {
		if row.cardID == card.ID && row.optionIndex == 1 {
			second = row
		}
	}
	_, clickPost := model.Update(tea.MouseMsg{
		X:      model.activityBarBounds.x + second.startX,
		Y:      model.activityBarBounds.y + second.line,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if clickPost == nil {
		t.Fatal("clicking a choose option did not post")
	}
	_ = clickPost()

	backend.mu.Lock()
	defer backend.mu.Unlock()
	want := []string{"1", "my own answer", "2"}
	if len(backend.posted) != len(want) {
		t.Fatalf("posted replies = %#v, want %v", backend.posted, want)
	}
	for index, body := range want {
		if backend.posted[index].Body != body {
			t.Fatalf("posted reply %d = %q, want %q", index, backend.posted[index].Body, body)
		}
	}
}

func TestConfirmQuestionIsInlineAndEnterAcceptsDefault(t *testing.T) {
	card := structuredQuestionCard(t,
		`{"kind":"confirm","prompt":"Ship it?","options":[`+
			`{"key":"yes","label":"yes"},{"key":"no","label":"no"}],"default":"no","allowFree":true}`,
		time.Now())
	backend := &fakeBackend{}
	model := New(backend, "structured-questions")
	model.cards = []jobCard{card}
	model.setSize(90, 30)
	dock := ansi.Strip(model.renderCardDock(true))
	if !strings.Contains(dock, "▸ 1 yes · ▸ 2 no") {
		t.Fatalf("confirm component is not compact and inline:\n%s", dock)
	}
	if got := model.questionSelection[card.ID]; got != 1 {
		t.Fatalf("confirm default selection = %d, want no", got)
	}
	_, post := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if post == nil {
		t.Fatal("enter on an empty confirm did not accept its default")
	}
	_ = post()
	backend.mu.Lock()
	posted := append([]store.Message(nil), backend.posted...)
	backend.mu.Unlock()
	if len(posted) != 1 || posted[0].Body != "no" {
		t.Fatalf("confirm posted %#v, want structured default key no", posted)
	}

	clickBackend := &fakeBackend{}
	clickModel := New(clickBackend, "structured-questions")
	clickModel.cards = []jobCard{card}
	clickModel.setSize(90, 30)
	_ = clickModel.View()
	var yes cardOptionRow
	for _, row := range clickModel.cardOptionRows {
		if row.cardID == card.ID && row.optionIndex == 0 {
			yes = row
		}
	}
	_, clickPost := clickModel.Update(tea.MouseMsg{
		X:      clickModel.activityBarBounds.x + yes.startX,
		Y:      clickModel.activityBarBounds.y + yes.line,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if clickPost == nil {
		t.Fatal("clicking an inline confirm option did not post")
	}
	_ = clickPost()
	clickBackend.mu.Lock()
	clickPosted := append([]store.Message(nil), clickBackend.posted...)
	clickBackend.mu.Unlock()
	if len(clickPosted) != 1 || clickPosted[0].Body != "yes" {
		t.Fatalf("clicked confirm posted %#v, want semantic key yes", clickPosted)
	}
}

func TestTextQuestionLabelsInputAndEscOrClickDismisses(t *testing.T) {
	card := structuredQuestionCard(t,
		`{"kind":"text","prompt":"Name the release branch","options":[],"allowFree":true}`,
		time.Now())
	backend := &fakeBackend{}
	model := New(backend, "structured-questions")
	model.cards = []jobCard{card}
	model.setSize(80, 24)
	view := ansi.Strip(model.View())
	if !strings.Contains(view, "answering: Name the release branch") || strings.Contains(view, "▸ 1") {
		t.Fatalf("text component did not label the ordinary input:\n%s", view)
	}
	model.input.SetValue("release/v2")
	_, post := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if post == nil {
		t.Fatal("text question did not submit the next ordinary message")
	}
	_ = post()
	backend.mu.Lock()
	if len(backend.posted) != 1 || backend.posted[0].Body != "release/v2" {
		t.Fatalf("text question posted %#v", backend.posted)
	}
	backend.mu.Unlock()

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if command != nil || model.questionDismissed[card.ID] == false {
		t.Fatal("esc did not dismiss text-answer mode without quitting")
	}
	if view := ansi.Strip(model.View()); strings.Contains(view, "answering: Name the release branch") {
		t.Fatalf("dismissed text prompt is still visible:\n%s", view)
	}

	delete(model.questionDismissed, card.ID)
	_ = model.View()
	_, _ = model.Update(tea.MouseMsg{
		X: model.textQuestionDismissBounds.x, Y: model.textQuestionDismissBounds.y,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if !model.questionDismissed[card.ID] {
		t.Fatal("clicking the text-question ⟨×⟩ did not dismiss it")
	}
}

func TestStuckQuestionPromotesButFreshQuestionKeepsDockOrder(t *testing.T) {
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	working := jobCard{ID: "working", State: cardWorking, Title: "First work", BirthSeq: 1}
	question := jobCard{
		ID: "question", State: cardQuestion, QuestionKind: questionText,
		Title: "Needs answer", Question: "Which branch?", BirthSeq: 2,
	}

	question.QuestionAt = now.Add(-stuckQuestionAfter + time.Second)
	fresh := dockJobCards([]jobCard{working, question}, now)
	if fresh[0].ID != working.ID {
		t.Fatalf("fresh question promoted to dock position 0: %#v", fresh)
	}

	question.QuestionAt = now.Add(-stuckQuestionAfter - time.Second)
	stuck := dockJobCards([]jobCard{working, question}, now)
	if stuck[0].ID != question.ID {
		t.Fatalf("stuck question position 0 = %q, want %q", stuck[0].ID, question.ID)
	}
	model := New(&fakeBackend{}, "questions")
	model.standingNow = func() time.Time { return now }
	model.cards = []jobCard{
		working,
		{ID: "working-2", State: cardWorking, Title: "Second work", BirthSeq: 3},
		{ID: "working-3", State: cardWorking, Title: "Third work", BirthSeq: 4},
		{ID: "working-4", State: cardWorking, Title: "Fourth work", BirthSeq: 5},
		question,
	}
	model.setSize(80, 24)
	firstLine := strings.Split(ansi.Strip(model.renderCardDock(true)), "\n")[0]
	if !strings.Contains(firstLine, "?") || !strings.Contains(firstLine, "Needs answer") {
		t.Fatalf("stuck question chip lacks the quiet violet question treatment: %q", firstLine)
	}
}

func TestHeaderQuestionDotFocusesPendingCard(t *testing.T) {
	model := New(&fakeBackend{}, "questions")
	model.cards = []jobCard{
		{ID: "work", State: cardWorking, Title: "Work"},
		{ID: "question", State: cardQuestion, QuestionKind: questionText, Title: "Waiting", Question: "Answer?"},
	}
	model.setSize(100, 30)
	view := ansi.Strip(model.View())
	if !strings.Contains(view, "⟨tasks ● ▸⟩") || model.headerQuestionBounds.width != 1 {
		t.Fatalf("header does not expose a pending-question dot:\n%s", view)
	}
	_, _ = model.Update(tea.MouseMsg{
		X: model.headerQuestionBounds.x, Y: model.headerQuestionBounds.y,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if model.focus != focusCards || model.selectedCardID != "question" {
		t.Fatalf("header dot focused (%v, %q), want question card", model.focus, model.selectedCardID)
	}
	model.focus = focusHeader
	model.headerFocusIndex = 1
	model.selectedCardID = "work"
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.focus != focusCards || model.selectedCardID != "question" {
		t.Fatalf("keyboard header dot focused (%v, %q), want question card", model.focus, model.selectedCardID)
	}
}

func TestHeaderModelPaletteOpensPickerFiltersClicksAndEscapesByRung(t *testing.T) {
	commander := newFakeCommander()
	commander.catalog = []ModelChoice{
		{Slug: "openai/gpt-text", Name: "GPT Text"},
		{Slug: "openai/gpt-vision", Name: "GPT Vision"},
		{Slug: "anthropic/claude", Name: "Claude"},
	}
	model := NewWithCommander(&fakeBackend{}, "models", commander)
	model.setSize(100, 30)
	_ = model.View()
	fetch, _ := model.updateMouseClick(model.headerModelsBounds.x, model.headerModelsBounds.y)
	_ = model.View()
	if model.palette != paletteModels || fetch != nil || len(model.modelSlotRows) != 8 {
		t.Fatalf("models header control opened palette=%v fetch=%v rows=%d", model.palette, fetch, len(model.modelSlotRows))
	}
	work := model.modelSlotRows[1]
	fetch, _ = model.updateMouseClick(work.bounds.x+1, work.bounds.y)
	if model.palette != paletteModel || model.modelRole != "work" || fetch == nil {
		t.Fatalf("work palette row opened palette=%v role=%q fetch=%v", model.palette, model.modelRole, fetch)
	}
	_, _ = model.Update(fetch())
	typeIntoModel(model, "vision")
	view := ansi.Strip(model.View())
	if got := lipgloss.Height(model.View()); got != model.height {
		t.Fatalf("overlaid model dropdown changed frame height to %d, want %d", got, model.height)
	}
	if !strings.Contains(view, "filter: vision") || !strings.Contains(view, "openai/gpt-vision") ||
		strings.Contains(view, "anthropic/claude") {
		t.Fatalf("header dropdown filter is wrong:\n%s", view)
	}
	if len(model.modelPickerRows) != 1 {
		t.Fatalf("filtered clickable model rows = %#v", model.modelPickerRows)
	}
	row := model.modelPickerRows[0]
	_, _ = model.Update(tea.MouseMsg{
		X: row.bounds.x + 1, Y: row.bounds.y,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	if commander.setRole != "work" || commander.setModel != "openai/gpt-vision" || model.palette != paletteNone {
		t.Fatalf("clicked model selection = (%q, %q), palette %v", commander.setRole, commander.setModel, model.palette)
	}

	_ = model.View()
	_, _ = model.updateMouseClick(model.headerModelsBounds.x, model.headerModelsBounds.y)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.palette != paletteModel {
		t.Fatalf("enter on selected palette row did not open picker: %v", model.palette)
	}
	_, quit := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if quit != nil || model.palette != paletteModels {
		t.Fatal("first esc did not return from picker to palette")
	}
	_, quit = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if quit != nil || model.palette != paletteNone || model.focus != focusHeader {
		t.Fatal("second esc did not return from palette to header")
	}
}

func TestModelPickerFallsBackToCommandRequestAndOptimisticHeader(t *testing.T) {
	backend := &standingActionBackend{fakeBackend: &fakeBackend{}}
	model := New(backend, "models")
	model.modelCatalog = []ModelChoice{{Slug: "vendor/model-next"}}
	if command := model.openModelPicker("talk"); command != nil {
		t.Fatal("cached fallback catalog unexpectedly started a fetch")
	}
	command := model.applyModel("talk", "vendor/model-next")
	if command == nil || model.currentModel("talk") != "vendor/model-next" {
		t.Fatal("fallback selection was not requested and rendered optimistically")
	}
	_, _ = model.Update(command())
	if len(backend.requested) != 1 {
		t.Fatalf("fallback model command requests = %#v", backend.requested)
	}
	request := backend.requested[0]
	if request.Kind != store.CommandAmend || request.Target != store.RootID ||
		request.Instruction != "use vendor/model-next for talk" {
		t.Fatalf("fallback request = %#v", request)
	}
}

func TestHeadEmittedQuestionBodyParsesIntoChooseComponent(t *testing.T) {
	// The head and resident emit selectable questions through
	// store.QuestionMessageBody; the TUI's tolerant reader must accept that
	// exact spelling, keys aligned so a plain "N" reply selects options[N-1].
	body := store.QuestionMessageBody("Stand this charter up?", []store.QuestionOption{
		{Label: "yes, stand this up", Value: "charter:ratify:charter-7"},
		{Label: "change the cadence", Value: "charter:cadence:charter-7"},
		{Label: "once, not standing", Value: "charter:once:charter-7"},
	})
	component, ok := readQuestionComponent(body)
	if !ok || component.Kind != questionChoose || component.Prompt != "Stand this charter up?" {
		t.Fatalf("emitted question decoded as %#v ok=%t", component, ok)
	}
	if len(component.Options) != 3 || !component.AllowFree {
		t.Fatalf("emitted options decoded as %#v", component.Options)
	}
	for index, want := range []string{"yes, stand this up", "change the cadence", "once, not standing"} {
		option := component.Options[index]
		if option.Label != want || option.Key != fmt.Sprint(index+1) || option.Reply != option.Key {
			t.Fatalf("option %d decoded as %#v, want label %q keyed %d", index, option, want, index+1)
		}
	}
}

func TestAmbiguousSurgeryAskbackParsesIntoChooseComponent(t *testing.T) {
	body := store.QuestionMessageBody("Which job do you mean?", []store.QuestionOption{
		{Label: "English audio", Hint: "running · 14m", Value: "surgery:select:cancel:audio-en:x"},
		{Label: "French audio", Hint: "pending · 2m", Value: "surgery:select:cancel:audio-fr:x"},
	})
	component, ok := readQuestionComponent(body)
	if !ok || component.Kind != questionChoose || component.Prompt != "Which job do you mean?" ||
		len(component.Options) != 2 {
		t.Fatalf("surgery askback decoded as %#v ok=%t", component, ok)
	}
	if component.Options[0].Label != "English audio" || component.Options[0].Hint != "running · 14m" ||
		component.Options[1].Label != "French audio" || component.Options[1].Hint != "pending · 2m" {
		t.Fatalf("surgery options decoded as %#v", component.Options)
	}
}

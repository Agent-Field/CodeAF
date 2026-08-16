package tui

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func pressBoost(model *Model) {
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}, Alt: true})
}

func runPost(t *testing.T, command tea.Cmd) {
	t.Helper()
	if command == nil {
		t.Fatal("submission returned no command")
	}
	_ = command()
}

func TestBoostStateMachineKeyAndClickCycle(t *testing.T) {
	model := NewWithCommander(&fakeBackend{}, "boost-cycle", newFakeCommander())
	offView := model.View()
	if model.boost != boostOff || keyBindings.boost != "alt+b" ||
		keyBindings.boost == keyBindings.graph || keyBindings.boost == keyBindings.voice {
		t.Fatalf("initial/collision state = %v bindings=%+v", model.boost, keyBindings)
	}
	pressBoost(model)
	if model.boost != boostArmed {
		t.Fatalf("first press = %v, want armed", model.boost)
	}
	_ = model.View()
	if model.boostBounds.width == 0 {
		t.Fatal("armed boost indicator has no click target")
	}
	_, handled := model.updateMouseClick(model.boostBounds.x, model.boostBounds.y)
	if !handled || model.boost != boostPinned {
		t.Fatalf("indicator click handled=%t state=%v, want pinned", handled, model.boost)
	}
	_ = model.View()
	_, handled = model.updateMouseClick(model.boostBounds.x, model.boostBounds.y)
	if !handled || model.boost != boostOff {
		t.Fatalf("second indicator click handled=%t state=%v, want off", handled, model.boost)
	}
	if cycled := model.View(); cycled != offView {
		t.Fatalf("off rendering changed after a full boost cycle:\n--- before ---\n%s\n--- after ---\n%s", offView, cycled)
	}
}

func TestBoostArmedAutoRevertsExactlyOnceOnSubmission(t *testing.T) {
	backend := &fakeBackend{}
	model := NewWithCommander(backend, "boost-once", newFakeCommander())
	pressBoost(model)
	typeIntoModel(model, "hard question")
	_, post := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.boost != boostOff {
		t.Fatalf("armed submission left state %v", model.boost)
	}
	runPost(t, post)

	typeIntoModel(model, "ordinary follow-up")
	_, post = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	runPost(t, post)
	if len(backend.posted) != 2 || backend.posted[0].Model != "gamma/model-three" || backend.posted[1].Model != "" {
		t.Fatalf("posted message lanes = %+v", backend.posted)
	}
}

func TestBoostPinnedPersistsAcrossMessages(t *testing.T) {
	backend := &fakeBackend{}
	model := NewWithCommander(backend, "boost-pinned", newFakeCommander())
	pressBoost(model)
	pressBoost(model)
	for _, body := range []string{"first hard question", "second hard question"} {
		typeIntoModel(model, body)
		_, post := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		runPost(t, post)
		if model.boost != boostPinned {
			t.Fatalf("pinned submission changed state to %v", model.boost)
		}
	}
	if len(backend.posted) != 2 || backend.posted[0].Model != "gamma/model-three" || backend.posted[1].Model != "gamma/model-three" {
		t.Fatalf("pinned message lanes = %+v", backend.posted)
	}
}

func TestBoostRenderingAtResponsiveWidths(t *testing.T) {
	commander := newFakeCommander()
	commander.current["boost"] = "anthropic/claude-opus-5"
	for _, width := range []int{80, 100, 120} {
		model := NewWithCommander(&fakeBackend{}, "responsive", commander)
		model.setSize(width, 30)
		off := ansi.Strip(model.View())
		if strings.Contains(off, "boost opus-5") || !strings.Contains(ansi.Strip(model.renderInput()), "› ") {
			t.Fatalf("width %d off rendering changed:\n%s", width, off)
		}

		model.boost = boostArmed
		armed := ansi.Strip(model.View())
		if !strings.Contains(armed, "» ") || !strings.Contains(armed, "boost claude-opus-5 · next message") || strings.Contains(strings.Split(armed, "\n")[0], "talk »") {
			t.Fatalf("width %d armed rendering:\n%s", width, armed)
		}

		model.boost = boostPinned
		pinned := ansi.Strip(model.View())
		if !strings.Contains(pinned, "» ") || !strings.Contains(pinned, "boost claude-opus-5 · pinned") {
			t.Fatalf("width %d pinned rendering:\n%s", width, pinned)
		}
		header := strings.Split(pinned, "\n")[0]
		if width >= 100 && !strings.Contains(header, "talk » claude-opus-5") {
			t.Fatalf("width %d pinned header lacks marker: %q", width, header)
		}
		for _, line := range strings.Split(model.View(), "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("width %d rendered %d-cell line: %q", width, lipgloss.Width(line), ansi.Strip(line))
			}
		}
	}
}

func TestBoostPaletteFollowWorkOverrideAndReturn(t *testing.T) {
	commander := newFakeCommander()
	model := NewWithCommander(&fakeBackend{}, "boost-palette", commander)
	_ = model.openModelsPalette()
	view := ansi.Strip(model.View())
	if len(modelSlots) != 9 || len(model.modelSlotRows) != 9 || !strings.Contains(view, "boost (work)") || !strings.Contains(view, "model-three") {
		t.Fatalf("default boost palette rows=%d slots=%d:\n%s", len(model.modelSlotRows), len(modelSlots), view)
	}
	_, fetch := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'9'}})
	if model.palette != paletteModel || model.modelRole != "boost" || fetch == nil {
		t.Fatalf("9 opened palette=%v role=%q fetch=%v", model.palette, model.modelRole, fetch)
	}
	_, _ = model.Update(fetch())
	choices := model.filteredModelChoices()
	if len(choices) < 2 || choices[0].Slug != followWorkModel || choices[0].Name != "use work model" {
		t.Fatalf("boost choices = %+v", choices)
	}

	_ = model.applyModel("boost", "beta/model-two")
	if model.modelFollowsWork() || model.currentModel("boost") != "beta/model-two" {
		t.Fatalf("explicit boost = follows %t model %q", model.modelFollowsWork(), model.currentModel("boost"))
	}
	_ = model.openModelsPalette()
	if explicit := ansi.Strip(model.View()); strings.Contains(explicit, "boost (work)") {
		t.Fatalf("explicit boost retained work annotation:\n%s", explicit)
	}

	_ = model.openModelsPalette()
	_ = model.View()
	boostRow := model.modelSlotRows[8]
	_, _ = model.updateMouseClick(boostRow.bounds.x+1, boostRow.bounds.y)
	_ = model.View()
	if len(model.modelPickerRows) == 0 || model.modelPickerRows[0].index != 0 {
		t.Fatalf("follow-work row is not first/clickable: %+v", model.modelPickerRows)
	}
	followRow := model.modelPickerRows[0]
	_, handled := model.updateMouseClick(followRow.bounds.x+1, followRow.bounds.y)
	if !handled {
		t.Fatal("follow-work click was not handled")
	}
	if !model.modelFollowsWork() || model.currentModel("boost") != commander.current["work"] {
		t.Fatalf("restored follow-work = follows %t model %q", model.modelFollowsWork(), model.currentModel("boost"))
	}
}

func TestModelBoostSlashCycles(t *testing.T) {
	model := NewWithCommander(&fakeBackend{}, "boost-slash", newFakeCommander())
	for index, want := range []boostMode{boostArmed, boostPinned, boostOff} {
		model.input.SetValue("/model boost")
		model.syncPalette()
		_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if command != nil || model.boost != want {
			t.Fatalf("slash cycle %d = state %v command=%v, want %v", index, model.boost, command != nil, want)
		}
	}
}

func TestBoostPromptCoexistsWithQuestionAttachmentsAndVoice(t *testing.T) {
	model := NewWithCommander(&fakeBackend{}, "boost-layers", newFakeCommander())
	model.cards = []jobCard{{
		ID: "question", State: cardQuestion, QuestionKind: questionText,
		Question: "Which conclusion should lead?",
	}}
	model.attachments = []string{"/tmp/evidence.png"}
	model.voicePending = "provisional voice words"
	model.input.SetValue("draft answer")
	model.boost = boostArmed
	model.setSize(80, 30)
	rendered := ansi.Strip(model.renderInput())
	for _, want := range []string{"answering: Which conclusion should lead?", "evidence.png", "draft answer", "provisional voice words", "» "} {
		if !strings.Contains(rendered, want) {
			t.Errorf("layered input missing %q:\n%s", want, rendered)
		}
	}
	if len(model.attachmentBounds) != 1 || model.micBounds.y <= model.attachmentBounds[0].y || model.textQuestionDismissBounds.y != model.inputBounds.y {
		t.Fatalf("layer bounds question=%+v attachment=%+v mic=%+v", model.textQuestionDismissBounds, model.attachmentBounds, model.micBounds)
	}
	for _, line := range strings.Split(model.renderInput(), "\n") {
		if lipgloss.Width(line) > 80 {
			t.Fatalf("layered input widened to %d cells: %q", lipgloss.Width(line), ansi.Strip(line))
		}
	}
}

func TestBoostReplyAttributionUsesDurableMessageModel(t *testing.T) {
	message := store.Message{Role: store.RoleAgent, Body: "hard answer", Model: "anthropic/claude-opus-5"}
	model := New(&fakeBackend{}, "attribution")
	model.messages = []store.Message{message}
	thread := ansi.Strip(model.renderMessages())
	if !strings.Contains(thread, "aforge") || !strings.Contains(thread, "claude-opus-5") || !strings.Contains(thread, "hard answer") {
		t.Fatalf("boost attribution thread = %q", thread)
	}
}

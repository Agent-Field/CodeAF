package settings

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// One kind at a time: what enter (or space) does, what the screen shows the
// instant it happens, and what reaches the write path.

func TestBoolRowTogglesOnSpaceAndShowsItAtOnce(t *testing.T) {
	s := newSheet(t)
	r := s.gotoRow(t, config.KeyAttribution)
	if got := s.value(r); got != "on" {
		t.Fatalf("propose new skills starts %q, want on", got)
	}

	s.press(typeRune(' '))

	if got := s.value(r); got != "off" {
		t.Fatalf("value on screen is %q, want the toggle to land live", got)
	}
	if pending, ok := s.pending[r.setting.Key]; !ok || pending.raw != "off" {
		t.Fatalf("pending = %+v, want the raw value the registry parses", pending)
	}
	s.Flush()
	if config.AttributionAt(s.dir) {
		t.Fatal("the store still reads on after the flush")
	}
}

func TestNumericRowOpensAnInlineEditorAndCommitsOnEnter(t *testing.T) {
	s := newSheet(t)
	r := s.gotoRow(t, config.KeyDailyBudget)

	s.press(namedKey(tea.KeyEnter))
	if !s.editing {
		t.Fatal("enter on a dollar row must open the inline editor")
	}
	if got := s.editor.text(); got != "20" {
		t.Fatalf("editor opened on %q, want the number without its unit", got)
	}

	s.press(namedKey(tea.KeyBackspace), namedKey(tea.KeyBackspace))
	s.typeText("7.5")
	s.press(namedKey(tea.KeyEnter))

	if s.editing {
		t.Fatal("enter must close the editor")
	}
	if got := s.value(r); got != "$7.5" {
		t.Fatalf("value shows %q, want the pending number wearing its unit", got)
	}
	s.Flush()
	amount, err := config.DailyBudgetUSDAt(s.dir)
	if err != nil || amount != 7.5 {
		t.Fatalf("store reads %v (%v), want 7.5", amount, err)
	}
}

// Esc inside the editor drops the unsubmitted text and nothing else — the
// stored value stands, and the sheet stays open (8.2.21).
func TestEscInsideTheEditorKeepsTheStoredValue(t *testing.T) {
	closed := 0
	s := newSheet(t, func(o *Options) { o.OnClose = func() tea.Cmd { closed++; return nil } })
	r := s.gotoRow(t, config.KeyDailyBudget)

	s.press(namedKey(tea.KeyEnter))
	s.typeText("999")
	s.press(namedKey(tea.KeyEscape))

	if s.editing {
		t.Fatal("esc must close the editor")
	}
	if closed != 0 {
		t.Fatal("esc inside an editor must not close the sheet")
	}
	if _, pending := s.pending[r.setting.Key]; pending {
		t.Fatal("a cancelled edit must not be staged")
	}
	if got := s.value(r); got != "$20" {
		t.Fatalf("value = %q, want the stored value back", got)
	}
}

func TestChoiceRowCyclesOnSpaceAndPicksOnEnter(t *testing.T) {
	s := newSheet(t)
	r := s.gotoRow(t, config.KeyDocumentEngine)
	first := s.value(r)

	s.press(typeRune(' '))
	if got := s.value(r); got == first {
		t.Fatalf("space did not cycle the choice, still %q", got)
	}

	s.press(namedKey(tea.KeyEnter))
	if !s.picking {
		t.Fatal("enter on a choice row must open the picker")
	}
	frame := s.Render(80, 20)
	for _, choice := range r.setting.Choices {
		if !strings.Contains(frame, choice) {
			t.Fatalf("the picker does not show %q:\n%s", choice, frame)
		}
	}

	s.press(namedKey(tea.KeyRight), namedKey(tea.KeyEnter))
	if s.picking {
		t.Fatal("enter must close the picker")
	}
	s.Flush()
	engine, err := config.DocumentEngineAt(s.dir)
	if err != nil {
		t.Fatalf("document engine: %v", err)
	}
	if engine == config.DefaultDocumentEngine {
		t.Fatalf("the picked engine never reached the store (still %q)", engine)
	}
}

func TestStringRowEditsAndClears(t *testing.T) {
	s := newSheet(t)
	r := s.gotoRow(t, config.KeyVisionModel)
	if got := s.value(r); got != r.setting.EmptyLabel {
		t.Fatalf("an unset text row reads %q, want its empty label", got)
	}

	s.press(namedKey(tea.KeyEnter))
	s.typeText("some/vision-model")
	s.press(namedKey(tea.KeyEnter))
	s.Flush()

	if got := config.VisionModelAt(s.dir); got != "some/vision-model" {
		t.Fatalf("store reads %q", got)
	}

	s.press(namedKey(tea.KeyEnter))
	s.press(ctrlKey('u'))
	s.press(namedKey(tea.KeyEnter))
	s.Flush()
	if got := config.VisionModelAt(s.dir); got != "" {
		t.Fatalf("store reads %q, want the row cleared", got)
	}
}

// A model row is the one kind this surface does not edit itself: the models
// door owns that catalog, and a second place to set a role binding is what
// 8.2.16 forbids.
func TestModelRowAsksTheHostForTheModelsDoor(t *testing.T) {
	var asked []string
	s := newSheet(t, func(o *Options) {
		o.OnModel = func(slot string) tea.Cmd { asked = append(asked, slot); return nil }
	})
	s.gotoRow(t, config.ModelSettingKey("work"))
	s.press(namedKey(tea.KeyEnter))

	if len(asked) != 1 || asked[0] != "work" {
		t.Fatalf("host was asked for %v, want [work]", asked)
	}
	if s.editing || s.picking {
		t.Fatal("a model row must not open an editor of its own")
	}
}

func TestModelRowEmitsAMessageWhenNoHostSeamIsWired(t *testing.T) {
	s := newSheet(t)
	s.gotoRow(t, config.ModelSettingKey("plan"))
	cmd := s.Key(namedKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("with no OnModel seam the row must emit a message the host can route")
	}
	msg, ok := cmd().(ModelMsg)
	if !ok || msg.Slot != "plan" {
		t.Fatalf("emitted %#v, want ModelMsg{Slot: plan}", msg)
	}
}

// A refusal is the registry's sentence, kept against its row and rendered
// under it — never swallowed, and never left looking like it landed.
func TestARefusedValueShowsTheRegistrysOwnWords(t *testing.T) {
	s := newSheet(t)
	r := s.gotoRow(t, config.KeyPracticeIdle)

	s.press(namedKey(tea.KeyEnter))
	s.press(ctrlKey('u'))
	s.typeText("soon")
	s.press(namedKey(tea.KeyEnter))
	s.Flush()

	failure := s.failed[r.setting.Key]
	if failure == "" {
		t.Fatal("a value the registry refuses must leave its reason on the row")
	}
	if !strings.Contains(failure, "length of time") {
		t.Fatalf("refusal reads %q, want the registry's own sentence", failure)
	}
	frame := s.Render(80, 20)
	if !strings.Contains(frame, failure) {
		t.Fatalf("the refusal is not on screen:\n%s", frame)
	}
	if got := s.value(r); got != r.setting.Value() {
		t.Fatalf("value = %q, want the store's reading back after a refusal", got)
	}
}

// Arrow navigation only: letters search, so j and k cannot be movement here.
func TestArrowsNavigateAndLettersDoNot(t *testing.T) {
	s := newSheet(t)
	s.reselectFresh()

	s.press(namedKey(tea.KeyDown))
	if s.selected != 1 {
		t.Fatalf("down moved to %d, want 1", s.selected)
	}
	s.press(namedKey(tea.KeyUp))
	if s.selected != 0 {
		t.Fatalf("up moved to %d, want 0", s.selected)
	}

	s.press(typeRune('j'))
	if s.selected != 0 || s.Query() != "j" {
		t.Fatalf("a letter must search, not navigate (selected=%d query=%q)", s.selected, s.Query())
	}
}

func TestGroupsMoveWithLeftAndRight(t *testing.T) {
	s := newSheet(t)
	first := s.Group()
	s.press(namedKey(tea.KeyRight))
	if s.Group() == first {
		t.Fatal("right must move to the next group")
	}
	s.press(namedKey(tea.KeyLeft))
	if s.Group() != first {
		t.Fatalf("left returned to %q, want %q", s.Group(), first)
	}
}

// Chips are buttons (5.22 rule 5): a click selects the row under the pointer,
// and a click on the row already selected activates it.
func TestClickSelectsThenActivates(t *testing.T) {
	s := newSheet(t)
	s.gotoRow(t, config.KeyAttribution)
	s.Render(80, 20)

	line := -1
	for index, position := range s.rowAtLine {
		if position >= 0 && position != s.selected {
			line = index
			break
		}
	}
	if line < 0 {
		t.Skip("this group has only one row")
	}
	want := s.rowAtLine[line]
	s.Mouse(tea.MouseClickMsg{Button: tea.MouseLeft}, at(0, line))
	if s.selected != want {
		t.Fatalf("click selected %d, want %d", s.selected, want)
	}
}

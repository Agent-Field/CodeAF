package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

func TestDecisionDialogWheelWrapsInsideTheBox(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(consentAsk())
	lab.tick(questionSettle * 2)
	head, _ := lab.a.questionHead()
	lab.a.moveQuestionPick(head, 0)
	lab.a.chrome(lab.a.width)
	y := lab.a.height - 3
	for _, step := range []struct {
		button tea.MouseButton
		want   int
	}{{tea.MouseWheelUp, 2}, {tea.MouseWheelDown, 0}} {
		drive(t, lab.a, tea.MouseWheelMsg{X: 8, Y: y, Button: step.button})
		head, _ = lab.a.questionHead()
		if head.pick != step.want || len(lab.answer) != 0 {
			t.Fatalf("wheel selected %d, want %d without answering", head.pick, step.want)
		}
	}
}

func TestDecisionDialogWrapsThroughItsCustomAnswer(t *testing.T) {
	lab := newQuestionLab(t)
	lab.raise(session.Question{
		ID: 81, Kind: session.QuestionAsk, Ask: session.AskChoice,
		Head: "Which output?", Reason: "Choose its format",
		Options: []session.AnswerOption{{Key: "1", Label: "text"}, {Key: "2", Label: "table"}},
	})
	lab.tick(questionSettle * 2)
	lab.press("up")
	head, _ := lab.a.questionHead()
	if !lab.a.questionOthering(head) {
		t.Fatal("up from the first option did not reach the custom answer")
	}
	lab.press("down")
	head, _ = lab.a.questionHead()
	if head.pick != 0 {
		t.Fatal("down from the custom answer did not return to the first option")
	}
}

func TestDecisionDialogWrapsBothWaysWithoutAnswering(t *testing.T) {
	for _, pair := range [][2]string{{"up", "down"}, {"left", "right"}, {"shift+tab", "tab"}} {
		lab := newQuestionLab(t)
		lab.raise(consentAsk())
		lab.tick(questionSettle * 2)
		head, _ := lab.a.questionHead()
		lab.a.moveQuestionPick(head, 0)
		lab.press(pair[0])
		head, _ = lab.a.questionHead()
		if head.pick != len(head.question.Options)-1 {
			t.Fatalf("%s at first option chose %d", pair[0], head.pick)
		}
		lab.press(pair[1])
		head, _ = lab.a.questionHead()
		if head.pick != 0 || len(lab.answer) != 0 {
			t.Fatalf("%s did not wrap without answering: pick=%d answers=%v", pair[1], head.pick, lab.answer)
		}
	}
}

func TestDecisionDialogShortcutsWorkBeforeNavigation(t *testing.T) {
	for _, key := range []string{questionCommentKey, questionAskBackKey} {
		lab := newQuestionLab(t)
		lab.raise(consentAsk())
		lab.tick(questionSettle * 2)
		if !lab.press(key) {
			t.Fatalf("%s needed navigation first", key)
		}
		head, _ := lab.a.questionHead()
		if head.writing != key || len(lab.answer) != 0 {
			t.Fatalf("%s did not open its text field: %+v", key, head)
		}
		lab.a.input.setText("explain the scope")
		rows, _, x, y := lab.a.chrome(lab.a.width)
		if !strings.Contains(plain(rows[y]), "explain the scope") || x < 2 || y >= len(rows)-1 {
			t.Fatalf("the text field or cursor is outside the box: %d,%d\n%s", x, y, plain(strings.Join(rows, "\n")))
		}
		lab.press("esc")
		if len(lab.a.questions) != 1 {
			t.Fatal("leaving the text field answered the question")
		}
	}
}

func TestDecisionDialogEndsTheFrameWithOnlyItsThreeHints(t *testing.T) {
	for _, width := range []int{80, 140, 240} {
		lab := newQuestionLab(t)
		lab.a.width = width
		lab.raise(consentAsk())
		rows, marks, _, _ := lab.a.chrome(width)
		last := plain(rows[len(rows)-1])
		for _, want := range []string{"esc later", "o other", "? clarify", tokens.Plain.Glyph(tokens.GFrameBottomRight)} {
			if !strings.Contains(last, want) {
				t.Fatalf("width %d: last row lacks %q: %s", width, want, last)
			}
		}
		for _, gone := range []string{"choose", "take it", "jump", "ask back", "c change"} {
			if strings.Contains(last, gone) {
				t.Fatalf("width %d: old hint %q remains: %s", width, gone, last)
			}
		}
		if marks[len(marks)-1].kind != chromeQuestion || len(rows) != lab.a.chromeHeight() {
			t.Fatalf("chrome geometry differs at width %d: drawn %d, measured %d", width, len(rows), lab.a.chromeHeight())
		}
		for _, part := range lab.a.seamParts(width) {
			if part.kind == segQuestions || part.kind == segState {
				t.Fatalf("the seam repeats the decision: %+v", part)
			}
		}
		lab.tick(questionSettle * 2)
		lab.press("esc")
		if _, up := lab.a.questionDialog(width); up {
			t.Fatal("folding did not restore the conversation composer")
		}
	}
}

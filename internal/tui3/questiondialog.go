package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

// questionDialog identifies a framed decision that owns the bottom of the
// conversation. Folded questions and full pages keep their ordinary composer.
func (a *app) questionDialog(width int) (questionShown, bool) {
	head, ok := a.questionHead()
	if !ok {
		return head, false
	}
	view := a.questionViewOf(head, width)
	return head, view == viewPanel || view == viewSplit
}

// questionDialogWheel walks only the decision under the pointer; scrolling
// the transcript above it still belongs to the conversation.
func (a *app) questionDialogWheel(msg tea.MouseWheelMsg) bool {
	width, _ := a.size()
	head, up := a.questionDialog(width)
	if !up || a.questionPanelTyping(head) {
		return false
	}
	row, hit := a.chromeAt(msg.Mouse().Y)
	if !hit || row.kind != chromeQuestion {
		return false
	}
	key := ""
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		key = "up"
	case tea.MouseWheelDown:
		key = "down"
	default:
		return false
	}
	_, took := a.questionKeyOn(head, questionNavigationPress(key))
	return took
}

// questionNavigationPress uses the same arrow routing for wheel and keyboard.
func questionNavigationPress(key string) tea.KeyPressMsg {
	code := tea.KeyDown
	if key == "up" {
		code = tea.KeyUp
	}
	return tea.KeyPressMsg{Code: code}
}

// questionDialogKeys keeps the three ways out of the offered answers on the
// lower boundary. Navigation and answer keys still work without a second hint.
func questionDialogKeys(keys []questionVerb) []questionVerb {
	out := make([]questionVerb, 0, 3)
	for _, key := range []string{questionLaterKey, questionCommentKey, questionAskBackKey} {
		for _, verb := range keys {
			if verb.key == key {
				out = append(out, verb)
				break
			}
		}
	}
	return out
}

// questionPanelTyping puts words inside the decision only when there is a
// reason to type. Existing drafts remain visible and retain their routing.
func (a *app) questionPanelTyping(q questionShown) bool {
	return q.writing != "" || q.question.Input.Kind == session.InputText || strings.TrimSpace(a.input.String()) != ""
}

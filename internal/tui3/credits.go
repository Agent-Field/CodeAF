package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
)

const lowCreditsWarning = "Your OpenRouter account is low on credits — some models may not be available"

type creditWakeMsg struct{}
type creditReadMsg struct {
	reading credits.Reading
	err     error
}

// askCredits may be called by a provider goroutine. The doorbell lets the
// update loop decide when to read without ever waiting on that goroutine.
func (a *app) askCredits(reason credits.Reason) {
	if a.readCredits == nil || a.creditWake == nil {
		return
	}
	priority := uint32(reason) + 1
	if reason == credits.KeyChanged {
		priority = 5
	}
	for {
		old := a.creditPending.Load()
		if old >= priority || a.creditPending.CompareAndSwap(old, priority) {
			break
		}
	}
	a.creditWake.ring()
}

func (a *app) launchCredits() tea.Cmd {
	if a.readCredits == nil || !config.CreditsNeedRead(a.profileDir, config.APIKeyAt(a.profileDir)) {
		return nil
	}
	return a.beginCredits(credits.Launch)
}

func (a *app) takeCreditWake() tea.Cmd {
	next := a.creditPending.Swap(0)
	if next == 0 {
		return nil
	}
	if next == 5 {
		return a.beginCredits(credits.KeyChanged)
	}
	return a.beginCredits(credits.Reason(next - 1))
}

func (a *app) beginCredits(reason credits.Reason) tea.Cmd {
	if a.readCredits == nil || a.creditTrigger == nil {
		return nil
	}
	if !a.creditTrigger.Begin(reason) {
		// A changed key must be read after the current request settles. The
		// result handler consumes this pending event without another wake loop.
		if reason == credits.KeyChanged {
			a.creditPending.Store(5)
		}
		return nil
	}
	return func() tea.Msg {
		reading, err := a.readCredits(a.ctx)
		return creditReadMsg{reading: reading, err: err}
	}
}

func (a *app) tookCredits(msg creditReadMsg) tea.Cmd {
	if a.creditTrigger != nil {
		a.creditTrigger.End()
	}
	if msg.err == nil {
		a.refreshCreditWarnings()
	}
	return a.takeCreditWake()
}

// refreshCreditWarnings reads the memoized record at state changes and Home's
// beat. Both foot renderers read the resulting strings without disk or map work.
func (a *app) refreshCreditWarnings() {
	if a.readCredits != nil {
		a.creditsLow = config.CreditsLowAt(a.profileDir)
	}
	// AN UNTOUCHED CONVERSATION FOLLOWS THE DEFAULT, BOTH WAYS. One that has sent
	// nothing, on the build's own default, with no model chosen anywhere, is not
	// a conversation anybody is on yet: it moves to the free default when the
	// account reads low and back to the paid one when it reads healthy. The
	// second half is the relaunch after a top-up, where the engine opens the
	// first conversation from the record as it stood — low — a moment before the
	// launch read says otherwise. A conversation that has sent anything keeps its
	// model either way, and nothing here writes a talk row.
	want := config.DefaultModel
	if a.creditsLow {
		want = config.FreeChatModel
	}
	if a.readCredits != nil && !a.creditSwitching && a.implicitTalk && a.model != want &&
		(a.model == config.DefaultModel || a.model == config.FreeChatModel) &&
		a.freshAndEmpty() && config.ChatModelAt(a.profileDir) == "" {
		a.creditSwitching = true
		a.switchModel(want, 0)
		a.creditSwitching = false
	}
	a.chatCreditWarning, a.homeCreditWarning = "", ""
	if a.readCredits == nil || !a.creditsLow {
		return
	}
	var models []Model
	if a.models != nil {
		models = a.models()
	}
	if len(models) == 0 {
		models = a.cachedModels()
	}
	if a.paidCreditModel(a.model, models) {
		a.chatCreditWarning = lowCreditsWarning
	}
	if a.paidCreditModel(a.targetModel(), models) {
		a.homeCreditWarning = lowCreditsWarning
	}
}

// creditRefusalEnded accepts the typed local refusal and the exact sentence
// prefix sent over the engine wire, which rebuilds errors as plain text.
func (a *app) creditRefusalEnded(err error) bool {
	if err == nil || a.readCredits == nil || a.modelIsDirect(a.model) {
		return false
	}
	if refusal, ok := provider.RefusalFrom(err); ok && refusal.AccountCannotPay() {
		return true
	}
	prefix := config.ConnectionOutcomeWord(modelsource.DefaultSource("").Name, modelsource.Outcome{Kind: modelsource.OutcomeAccountCannotPay})
	return strings.HasPrefix(err.Error(), prefix)
}

func (a *app) paidCreditModel(id string, models []Model) bool {
	if strings.TrimSpace(id) == "" || a.modelIsDirect(id) {
		return false
	}
	if config.IsFreeModel(id, false, 0, 0, 0) {
		return false
	}
	bare, _ := roles.SplitEffort(strings.TrimPrefix(id, "~"))
	for _, row := range models {
		if row.ID == bare && config.IsFreeModel(id, row.PriceKnown, row.PromptPrice, row.CompletionPrice, row.RequestPrice) {
			return false
		}
	}
	return true
}

func (a *app) creditPlaceHint(width int, pal palette) string {
	hint := a.placeHint()
	warning := a.homeCreditWarning
	if !a.at(pageHome) || layoutTier(width) == tierPhone || ansi.StringWidth(warning)+3 > width {
		warning = ""
	}
	room := width - 2
	if warning != "" {
		room -= ansi.StringWidth(warning) + 1
	}
	left := " " + paintHint(hintFit(hint, room), pal, pal.dim)
	if warning == "" {
		return left
	}
	return left + strings.Repeat(" ", max(1, width-ansi.StringWidth(left)-ansi.StringWidth(warning)-1)) + pal.warn(warning) + " "
}

// creditPlaceMessage keeps a complete warning on Home's message row. A place
// message keeps its usual shape whenever the two fit side by side.
func (a *app) creditPlaceMessage(width int, msg string, pal palette) string {
	warning := a.homeCreditWarning
	if !a.at(pageHome) || layoutTier(width) == tierPhone || ansi.StringWidth(warning)+3 > width || warning == "" {
		return msg
	}
	if ansi.StringWidth(msg)+ansi.StringWidth(warning)+1 > width {
		msg = " "
	}
	return msg + strings.Repeat(" ", max(1, width-ansi.StringWidth(msg)-ansi.StringWidth(warning)-1)) + pal.warn(warning) + " "
}

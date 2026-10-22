package tui3

import (
	"slices"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

const modelCycleKey = "f3"

// nextRotationModel keeps the person's order stable, including when a picker
// selection temporarily takes the conversation outside that order.
func (a *app) nextRotationModel() string {
	if len(a.modelRotation) < 2 {
		return ""
	}
	at := slices.Index(a.modelRotation, a.model)
	return a.modelRotation[(at+1)%len(a.modelRotation)]
}

// modelCycleSuffix belongs only to the conversation; a task keeps its own door.
// Fewer than two models offer no exchange, so the button is absent.
func (a *app) modelCycleSuffix() string {
	if !a.at(pageNone) || a.roomOpen() || a.linear || len(a.modelRotation) < 2 {
		return ""
	}
	position := ""
	if at := slices.Index(a.modelRotation, a.model); len(a.modelRotation) > 2 && at >= 0 {
		position = strconv.Itoa(at+1) + "/" + strconv.Itoa(len(a.modelRotation)) + " "
	}
	return " " + position + a.icon(tokens.GActionTransfer)
}

func (a *app) legendModelCyclePress(x, y int) bool {
	if a.copy.on || a.pick.open || a.modelCycleSuffix() == "" {
		return false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeLegend || !a.seamModelCycleSpan.holds(x) {
		return false
	}
	a.cycleChatModel()
	return true
}

// toggleRotationModel writes on each explicit selection. Merely browsing never
// changes the current chat model, and removing then adding moves an entry last.
func (a *app) toggleRotationModel(sel *sheetSelect) {
	model, ok := sel.choice()
	if !ok {
		return
	}
	row, ok := a.sheet.registry.Row(config.KeyChatModelRotation)
	if !ok {
		return
	}
	models := config.ChatModelRotationAt(a.profileDir)
	if at := slices.Index(models, model); at >= 0 {
		models = slices.Delete(models, at, at+1)
	} else {
		models = append(models, model)
	}
	meta, _ := settingMetaFor(row)
	a.applySetting(sheetItem{row: row, meta: meta}, strings.Join(models, ", "))
}

// openModelRotationSettings takes an unconfigured switch straight to its row.
func (a *app) openModelRotationSettings() {
	a.openSettings()
	a.sheet.query.reset()
	for i, tab := range settingTabs {
		if tab == tabProviders {
			a.sheet.tab = i
			break
		}
	}
	a.sheet.build()
	for i, item := range a.sheet.items {
		if item.row.Key == config.KeyChatModelRotation {
			a.sheet.cursor = i
			break
		}
	}
}

func (a *app) cycleChatModel() {
	if next := a.nextRotationModel(); next != "" {
		if warning := a.nonChatWarning(next); warning != "" {
			a.note(warning)
			return
		}
		a.switchModel(next, a.windowFor(next))
	} else {
		a.openModelRotationSettings()
	}
}

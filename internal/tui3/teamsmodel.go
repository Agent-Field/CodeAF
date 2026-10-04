package tui3

import (
	"github.com/charmbracelet/x/ansi"
)

// Cards read the same model identity as the conversation. Live agents take
// precedence over saved rows, which the Teams beat refreshes for other windows.
// Painting never opens a transcript or substitutes this window's default model.
func (a *app) teamsConversationModel(key, file string) string {
	if key != "" && key == a.frontTabKey() {
		return a.modelIdentity(a.model)
	}
	if agent, live := a.wallAgentFor(key); live {
		return a.modelIdentity(agent.Model())
	}
	return a.modelIdentity(a.tp.world[file].Model)
}

// The alias keeps its ordinary ink while the separator and model are grey.
// Only the model yields space until the card cannot fit the alias itself.
func (a *app) teamsConversationLabel(alias, key, file string, width int) string {
	name := a.pal.ink(fit(alias, width))
	model := a.teamsConversationModel(key, file)
	room := width - ansi.StringWidth(alias) - 3
	if model == "" || room < 4 {
		return name
	}
	return name + a.pal.dim(" "+a.teamsDot()+" "+ansi.Truncate("~"+model, room, "..."))
}

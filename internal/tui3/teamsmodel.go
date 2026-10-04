package tui3

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

// Overview team tiles share the same alias-and-model metadata without
// borrowing the conversation title into their separate team-summary row.
func (a *app) teamsConversationLabel(alias, key, file string, width int) string {
	return a.teamsCardMetadata(teamsCrewRow{title: alias, key: key, file: file}, width)[0]
}

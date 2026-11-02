package config

var teamsShareChoices = []string{"off", "on"}

// TeamsShareAt reads the local opt-in at use time, so a running round or chat
// stops sharing as soon as this computer turns it off. Missing or damaged
// settings leave the existing local-only behavior in place.
func TeamsShareAt(profileDir string) bool {
	value, _ := persistedString(profileDir, KeyTeamsShare)
	return value == "on"
}

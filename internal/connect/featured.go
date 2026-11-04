package connect

type FeaturedWords struct {
	What string `json:"what"`
	Does string `json:"does"`
}

func Featured() ([]string, map[string]FeaturedWords) {
	return []string{"google", "slack", "notion", "sentry"}, map[string]FeaturedWords{
		"google": {"Gmail and Calendar", "Agents search and read your mail, see your calendar and add events. Sending mail and inviting people ask first."},
		"slack":  {"Channels, threads and DMs", "Agents search and read your Slack across the workspaces you sign in to. Sending a message asks first."},
		"notion": {"Pages, databases and search", "Notion's own tools. Agents read and search your workspace; anything that changes a page asks first."},
		"sentry": {"Issues, events and releases", "Sentry's own tools. Agents read the errors behind a bug and check a release; anything that acts asks first."},
	}
}

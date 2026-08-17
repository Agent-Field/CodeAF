package connect

// The five tool servers this build ships, and where each one answers.
//
// ── THIS FILE IS DATA ──
//
// It is kept apart from the machinery for the reason category.go is kept apart
// from catalog.go: a list of facts about other people's products is a thing you
// edit, argue about and regenerate, and none of the code that uses it should
// have to move when a vendor changes an address. Adding a sixth is one entry
// below and nothing else — no switch, no wiring, no screen.
//
// ── EVERY ADDRESS WAS READ OFF THE VENDOR'S OWN PAGE ──
//
// Not off a directory, an aggregator or somebody's blog post: a wrong address
// here is a person sent to sign in to a service that is not the one they asked
// for. Each entry carries the page it was read from and the date it was read.
// An address nobody could confirm from the vendor is not shipped.
//
// ── AND WHY GITHUB IS NOT HERE ──
//
// GitHub runs one of these too, and it is deliberately left out of this wave:
// its sign-in does not let a program introduce itself, so connecting it would
// need an application registered by hand in a console first — which is the one
// thing this half of the package exists to avoid. It comes back the day that
// changes, as one entry below.

// mcpEntry is one line of the list: what goes on a menu, and where the service
// answers.
type mcpEntry struct {
	service Service
	address string
}

// mcpCatalog is the list, in no particular order — [sortPlugs] puts them in the
// order a person reads them.
//
// Each blurb says what connecting buys in the person's own words. It does not
// say how any of it works, and the test beside this file holds that line.
func mcpCatalog() []mcpEntry {
	return []mcpEntry{
		{
			// https://developers.notion.com/guides/mcp/get-started-with-mcp,
			// read 2026-08-17: "https://mcp.notion.com/mcp", the address
			// Notion recommends for new clients.
			service: Service{
				ID:       "notion",
				Name:     "Notion",
				Category: categoryProductivity,
				Blurb:    "Notion's own tools — your pages, databases and search — signed in in your browser.",
			},
			address: "https://mcp.notion.com/mcp",
		},
		{
			// https://linear.app/docs/mcp, read 2026-08-17: "Read-write access
			// is provided through https://mcp.linear.app/mcp by default."
			service: Service{
				ID:       "linear",
				Name:     "Linear",
				Category: categoryDeveloper,
				Blurb:    "Linear's own tools — your issues, projects and cycles — signed in in your browser.",
			},
			address: "https://mcp.linear.app/mcp",
		},
		{
			// https://mcp.sentry.dev/ (where https://docs.sentry.io/product/
			// sentry-mcp/ now redirects), read 2026-08-17: "https://
			// mcp.sentry.dev/mcp" is the base address, and a path may narrow it
			// to one organisation or project. The base is what is shipped: a
			// person who has one organisation should not have to name it.
			service: Service{
				ID:       "sentry",
				Name:     "Sentry",
				Category: categoryDeveloper,
				Blurb:    "Sentry's own tools — your issues, events and releases — signed in in your browser.",
			},
			address: "https://mcp.sentry.dev/mcp",
		},
		{
			// https://support.atlassian.com/atlassian-rovo-mcp-server/docs/
			// setting-up-ides/, read 2026-08-17: "We recommend updating any
			// configured custom clients to point to /mcp: https://
			// mcp.atlassian.com/v1/mcp/authv2". The older /v1/sse address was
			// retired on 30 June 2026 and is deliberately not shipped.
			service: Service{
				ID:       "atlassian",
				Name:     "Atlassian",
				Category: categoryDeveloper,
				Blurb:    "Atlassian's own tools — Jira issues and Confluence pages — signed in in your browser.",
			},
			address: "https://mcp.atlassian.com/v1/mcp/authv2",
		},
		{
			// https://docs.slack.dev/ai/slack-mcp-server/, read 2026-08-17:
			// "All requests should be sent to: https://mcp.slack.com/mcp".
			//
			// THE SECOND SENTENCE OF THE BLURB IS THERE ON PURPOSE. The same
			// page says a workspace admin approves these connections, so a
			// person whose sign-in is refused has been told why before they
			// try, in one dim line and without drama.
			service: Service{
				ID:       "slack",
				Name:     "Slack",
				Category: categoryCommunication,
				Blurb:    "Slack's own tools — your channels, messages and search — signed in in your browser. Your workspace admin may have to approve it first.",
			},
			address: "https://mcp.slack.com/mcp",
		},
	}
}

// init puts every one of them on the registry, exactly as google.go and
// catalog.go put theirs there.
func init() {
	for _, entry := range mcpCatalog() {
		registerToolServer(entry.service, entry.address)
	}
}

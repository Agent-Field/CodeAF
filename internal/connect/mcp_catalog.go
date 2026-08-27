package connect

// The 27 tool servers this build ships, and where each one answers.
//
// ── THIS FILE IS DATA ──
//
// It is kept apart from the machinery for the reason category.go is kept apart
// from catalog.go: a list of facts about other people's products is a thing you
// edit, argue about and regenerate, and none of the code that uses it should
// have to move when a vendor changes an address. Adding another is one entry
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
// thing this half of the package exists to avoid. On 2026-05-27 a maintainer
// closed github/github-mcp-server issue #2532, "Support Dynamic Client
// Registration", saying this introduction will never be implemented on
// GitHub's sign-in server. GitHub therefore comes back only with an application
// registered by hand in a console, in a later wave.
//
// Slack is absent for the same concrete reason. https://docs.slack.dev/ai/
// slack-mcp-server/, read 2026-08-24, says "We do not support SSE-based
// connections or Dynamic Client Registration at this time", and its live
// sign-in description had no place to introduce a program on that date. It
// comes back when Slack allows that introduction again, or with an application
// registered by hand in a later wave.

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
			// https://docs.gitlab.com/user/model_context_protocol/mcp_server/,
			// read 2026-08-24: the page publishes https://gitlab.com/api/v4/mcp
			// and says its sign-in "supports OAuth 2.0 Dynamic Client
			// Registration". An admin must also allow the AI features and MCP
			// access, which is why the second sentence is here.
			service: Service{
				ID:       "gitlab",
				Name:     "GitLab",
				Category: categoryDeveloper,
				Blurb:    "GitLab's own tools — your projects, issues and merge requests — signed in in your browser. Your GitLab admin may have to turn its AI features on first.",
			},
			address: "https://gitlab.com/api/v4/mcp",
		},
		{
			// https://developers.cloudflare.com/agents/model-context-protocol/
			// mcp-servers-for-cloudflare/, read 2026-08-24: "url":
			// "https://mcp.cloudflare.com/mcp"; the page says the browser trip
			// is where a person chooses what the account may do.
			service: Service{
				ID:       "cloudflare",
				Name:     "Cloudflare",
				Category: categoryDeveloper,
				Blurb:    "Cloudflare's own tools — your zones, DNS records and Workers — signed in in your browser.",
			},
			address: "https://mcp.cloudflare.com/mcp",
		},
		{
			// https://supabase.com/docs/guides/ai-tools/mcp, read 2026-08-24:
			// "The hosted Supabase MCP server is available at
			// `https://mcp.supabase.com/mcp`" and introduces clients itself by
			// default.
			service: Service{
				ID:       "supabase",
				Name:     "Supabase",
				Category: categoryDeveloper,
				Blurb:    "Supabase's own tools — your projects, tables and queries — signed in in your browser.",
			},
			address: "https://mcp.supabase.com/mcp",
		},
		{
			// https://neon.com/docs/ai/neon-mcp-server, read 2026-08-24: "If
			// your client still points at `/sse`, change the URL to
			// `https://mcp.neon.tech/mcp`." The old /sse address is deprecated
			// for removal on 2026-10-01 and is deliberately not shipped.
			service: Service{
				ID:       "neon",
				Name:     "Neon",
				Category: categoryDeveloper,
				Blurb:    "Neon's own tools — your projects, branches and queries — signed in in your browser.",
			},
			address: "https://mcp.neon.tech/mcp",
		},
		{
			// https://docs.netlify.com/build/build-with-ai/netlify-mcp-server/,
			// read 2026-08-24: Netlify recommends its remote service with
			// "npx -y add-mcp https://netlify-mcp.netlify.app/mcp". The
			// netlify.app host is the first-party address its own page gives.
			service: Service{
				ID:       "netlify",
				Name:     "Netlify",
				Category: categoryDeveloper,
				Blurb:    "Netlify's own tools — your sites, deploys and domains — signed in in your browser.",
			},
			address: "https://netlify-mcp.netlify.app/mcp",
		},
		{
			// https://docs.railway.com/ai/mcp-server, read 2026-08-24:
			// "Remote MCP runs at `mcp.railway.com`." The vendor writes the
			// bare host, so the trailing slash is kept; /mcp answers 404 and is
			// deliberately not substituted.
			service: Service{
				ID:       "railway",
				Name:     "Railway",
				Category: categoryDeveloper,
				Blurb:    "Railway's own tools — your projects, services and deployments — signed in in your browser.",
			},
			address: "https://mcp.railway.com/",
		},
		{
			// https://grafana.com/docs/grafana-cloud/ai-tools/mcp-servers/
			// cloud-mcp/, read 2026-08-24: point the agent at
			// https://mcp.grafana.com/mcp, "authorize in your browser, and your
			// agent connects". This hosted address is for Grafana Cloud.
			service: Service{
				ID:       "grafana",
				Name:     "Grafana",
				Category: categoryDeveloper,
				Blurb:    "Grafana's own tools — your dashboards, queries and alerts — signed in in your browser.",
			},
			address: "https://mcp.grafana.com/mcp",
		},
		{
			// https://circleci.com/docs/guides/toolkit/circleci-mcp-overview/,
			// read 2026-08-24: "The hosted MCP server, available at
			// https://mcp.circleci.com/v1/mcp"; CircleCI runs it and offers a
			// browser sign-in there.
			service: Service{
				ID:       "circleci",
				Name:     "CircleCI",
				Category: categoryDeveloper,
				Blurb:    "CircleCI's own tools — your pipelines, workflows and build logs — signed in in your browser.",
			},
			address: "https://mcp.circleci.com/v1/mcp",
		},
		{
			// https://devcenter.heroku.com/articles/heroku-remote-mcp-server,
			// read 2026-08-24: "MCP clients must support web-based OAuth flow
			// to connect to `mcp.heroku.com`"; the remote streamable address
			// published on that page is https://mcp.heroku.com/mcp.
			service: Service{
				ID:       "heroku",
				Name:     "Heroku",
				Category: categoryDeveloper,
				Blurb:    "Heroku's own tools — your apps, dynos and add-ons — signed in in your browser.",
			},
			address: "https://mcp.heroku.com/mcp",
		},
		{
			// https://buildkite.com/docs/apis/mcp-server, read 2026-08-24:
			// "Endpoint: `https://mcp.buildkite.com/mcp`"; this is the
			// read-and-write browser address, not /mcp/readonly or the
			// key-bearing /direct variant.
			service: Service{
				ID:       "buildkite",
				Name:     "Buildkite",
				Category: categoryDeveloper,
				Blurb:    "Buildkite's own tools — your pipelines, builds and logs — signed in in your browser.",
			},
			address: "https://mcp.buildkite.com/mcp",
		},
		{
			// https://launchdarkly.com/docs/home/getting-started/mcp-hosted,
			// read 2026-08-24: "Server URL:
			// `https://mcp.launchdarkly.com/mcp/launchdarkly`". The second
			// launchdarkly path segment is part of the published address.
			service: Service{
				ID:       "launchdarkly",
				Name:     "LaunchDarkly",
				Category: categoryDeveloper,
				Blurb:    "LaunchDarkly's own tools — your feature flags, segments and environments — signed in in your browser.",
			},
			address: "https://mcp.launchdarkly.com/mcp/launchdarkly",
		},
		{
			// https://www.sanity.io/docs/ai/mcp-server, read 2026-08-24:
			// "https://mcp.sanity.io" and "The Sanity MCP server uses OAuth
			// by default to perform operations on your behalf." The bare host
			// is the address Sanity publishes.
			service: Service{
				ID:       "sanity",
				Name:     "Sanity",
				Category: categoryDeveloper,
				Blurb:    "Sanity's own tools — your content, datasets and schemas — signed in in your browser.",
			},
			address: "https://mcp.sanity.io",
		},
		{
			// https://huggingface.co/docs/hub/en/hf-mcp-server, read
			// 2026-08-24: "Hugging Face MCP Server:
			// https://huggingface.co/mcp". The account's settings decide which
			// tool groups this address serves.
			service: Service{
				ID:       "huggingface",
				Name:     "Hugging Face",
				Category: categoryDeveloper,
				Blurb:    "Hugging Face's own tools — your models, datasets and Spaces — signed in in your browser.",
			},
			address: "https://huggingface.co/mcp",
		},
		{
			// https://posthog.com/docs/model-context-protocol, read
			// 2026-08-24: "The server URL is
			// `https://mcp.posthog.com/mcp`." The same sign-in routes a person
			// to the right US or EU region.
			service: Service{
				ID:       "posthog",
				Name:     "PostHog",
				Category: categoryAnalytics,
				Blurb:    "PostHog's own tools — your events, insights and feature flags — signed in in your browser.",
			},
			address: "https://mcp.posthog.com/mcp",
		},
		{
			// https://learning.postman.com/docs/reference/postman-api/
			// postman-mcp-server/postman-mcp-remote-server, read 2026-08-27:
			// "Minimal (default)" answers at "https://mcp.postman.com/minimal",
			// and the hosted address "supports OAuth … including Dynamic Client
			// Registration (DCR), OAuth metadata, and PKCE". The same page says
			// "OAuth isn't supported for the EU Postman MCP server", so the EU
			// address (https://mcp.eu.postman.com/…) is deliberately not shipped:
			// it takes a key and nothing else, and a row that connected and then
			// failed at the first call would be worse than one that says so.
			service: Service{
				ID:       "postman",
				Name:     "Postman",
				Category: categoryDeveloper,
				Blurb:    "Postman's own tools — your collections, specs and environments — signed in in your browser. Postman's EU workspaces cannot be reached this way.",
			},
			address: "https://mcp.postman.com/minimal",
		},
		{
			// https://developer.clickup.com/docs/connect-an-ai-assistant-to-
			// clickups-mcp-server, read 2026-08-24:
			// "https://mcp.clickup.com/mcp". The page says browser sign-in is
			// the only supported way to connect this hosted address.
			service: Service{
				ID:       "clickup",
				Name:     "ClickUp",
				Category: categoryProductivity,
				Blurb:    "ClickUp's own tools — your tasks, lists and docs — signed in in your browser.",
			},
			address: "https://mcp.clickup.com/mcp",
		},
		{
			// https://support.airtable.com/docs/using-the-airtable-mcp-server,
			// read 2026-08-24: "claude mcp add --transport http airtable
			// https://mcp.airtable.com/mcp". An enterprise integration may
			// need its client id allowlisted, which is why the second sentence
			// is here.
			service: Service{
				ID:       "airtable",
				Name:     "Airtable",
				Category: categoryProductivity,
				Blurb:    "Airtable's own tools — your bases, tables and records — signed in in your browser. An enterprise admin may have to allow it first.",
			},
			address: "https://mcp.airtable.com/mcp",
		},
		{
			// https://www.todoist.com/help/articles/use-claude-code-with-
			// todoist-cli-and-mcp-b1USJ4HB3, read 2026-08-24: "claude mcp add
			// --transport http todoist https://ai.todoist.net/mcp". The
			// official address lives on ai.todoist.net, not todoist.com.
			service: Service{
				ID:       "todoist",
				Name:     "Todoist",
				Category: categoryProductivity,
				Blurb:    "Todoist's own tools — your tasks, projects and labels — signed in in your browser.",
			},
			address: "https://ai.todoist.net/mcp",
		},
		{
			// https://developers.miro.com/docs/connecting-to-miro-mcp, read
			// 2026-08-24: "https://mcp.miro.com/". The connection is scoped
			// to one Miro team; the bare host is the address Miro publishes.
			service: Service{
				ID:       "miro",
				Name:     "Miro",
				Category: categoryProductivity,
				Blurb:    "Miro's own tools — your boards, frames and notes — signed in in your browser.",
			},
			address: "https://mcp.miro.com",
		},
		{
			// https://www.canva.dev/docs/mcp/, read 2026-08-24:
			// "https://mcp.canva.com/mcp". Canva prefers a newer form of
			// introduction, but the page says dynamic registration remains
			// available for backward compatibility.
			service: Service{
				ID:       "canva",
				Name:     "Canva",
				Category: categoryProductivity,
				Blurb:    "Canva's own tools — your designs, folders and brand kits — signed in in your browser.",
			},
			address: "https://mcp.canva.com/mcp",
		},
		{
			// https://developer.calendly.com/calendly-mcp-server, read
			// 2026-08-24: "fully hosted by Calendly at
			// https://mcp.calendly.com". The page says no application or
			// application secrets need to be registered first.
			service: Service{
				ID:       "calendly",
				Name:     "Calendly",
				Category: categoryCalls,
				Blurb:    "Calendly's own tools — your event types, availability and scheduled meetings — signed in in your browser.",
			},
			address: "https://mcp.calendly.com",
		},
		{
			// https://developer.paypal.com/ai-tools/mcp-server, read
			// 2026-08-24: "Production HTTP: https://mcp.paypal.com/http". PayPal
			// splits the current /http transport from legacy /sse, and the
			// production host from mcp.sandbox.paypal.com.
			service: Service{
				ID:       "paypal",
				Name:     "PayPal",
				Category: categoryBilling,
				Blurb:    "PayPal's own tools — your payments, invoices and payouts — signed in in your browser.",
			},
			address: "https://mcp.paypal.com/http",
		},
		{
			// https://developers.klaviyo.com/en/docs/klaviyo_mcp_server, read
			// 2026-08-24: "https://mcp.klaviyo.com/mcp" and "OAuth (with
			// dynamic client registration)". The local key-bearing variant is
			// not the hosted address shipped here.
			service: Service{
				ID:       "klaviyo",
				Name:     "Klaviyo",
				Category: categoryMarketing,
				Blurb:    "Klaviyo's own tools — your campaigns, flows and audiences — signed in in your browser.",
			},
			address: "https://mcp.klaviyo.com/mcp",
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

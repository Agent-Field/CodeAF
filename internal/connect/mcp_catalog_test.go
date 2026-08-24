package connect

import (
	"net/url"
	"slices"
	"strings"
	"testing"
)

// machineryWords are the words that must never reach a person: the vocabulary of
// how a connection is made, in front of somebody who is connecting Notion.
//
// It is the same idiom catalog_test.go uses over the hundreds of lines nobody
// wrote by hand, with the words this half of the package could leak added to it.
var machineryWords = []string{
	"oauth", "api key", "bearer", "pkce", "endpoint",
	"mcp", "protocol", "server", "token", "client id", "registration",
}

// The 26, and the laws that hold for every one of them.
func TestTheToolServersAreUsableAsTheyStand(t *testing.T) {
	entries := mcpCatalog()
	want := []string{
		"airtable", "atlassian", "buildkite", "calendly", "canva", "circleci",
		"clickup", "cloudflare", "gitlab", "grafana", "heroku", "huggingface",
		"klaviyo", "launchdarkly", "linear", "miro", "neon", "netlify", "notion",
		"paypal", "posthog", "railway", "sanity", "sentry", "supabase", "todoist",
	}
	var got []string
	for _, entry := range entries {
		got = append(got, entry.service.ID)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("the list is %v, want %v", got, want)
	}

	for _, entry := range entries {
		service := entry.service
		if strings.TrimSpace(service.Name) == "" {
			t.Errorf("%s: nothing to call it", service.ID)
		}
		if !slices.Contains(categories, service.Category) {
			t.Errorf("%s: %q is not one of the words a catalog is browsed by", service.ID, service.Category)
		}
		if strings.TrimSpace(service.Blurb) == "" {
			t.Errorf("%s: no line to read next to the name", service.ID)
		}
		// The id is what a tool is named after, so it must be a word a tool
		// name can be built out of.
		if strings.ContainsAny(service.ID, " \t./:") {
			t.Errorf("%s: an id with punctuation in it cannot name a tool", service.ID)
		}
		// A browser service asks for no permissions here: what may be asked
		// for is a thing the service itself says at connect time.
		if len(service.Scopes) != 0 {
			t.Errorf("%s: %v is asked for before the service has been asked", service.ID, service.Scopes)
		}
		if service.Address != "" {
			t.Errorf("%s: a browser service has no one address, got %q", service.ID, service.Address)
		}
		address, err := url.Parse(entry.address)
		if err != nil || address.Scheme != "https" || address.Host == "" {
			t.Errorf("%s: %q is not an address a request can be made against", service.ID, entry.address)
		}
	}
}

func TestTheToolServerLinesUseNoMachineryVocabulary(t *testing.T) {
	for _, entry := range mcpCatalog() {
		for _, line := range []string{entry.service.Name, entry.service.Blurb} {
			lowered := strings.ToLower(line)
			for _, word := range machineryWords {
				if strings.Contains(lowered, word) {
					t.Errorf("%s says %q: %q", entry.service.ID, word, line)
				}
			}
		}
	}
}

// GitLab and Airtable put their one extra fact in front of the person before
// they try, because it is the reason their sign-in may be refused by somebody
// other than the person signing in.
func TestGitLabAndAirtableSayWhoElseHasToAgree(t *testing.T) {
	want := map[string]bool{"gitlab": true, "airtable": true}
	for _, entry := range mcpCatalog() {
		if !want[entry.service.ID] {
			continue
		}
		if !strings.Contains(strings.ToLower(entry.service.Blurb), "admin") {
			t.Errorf("%s: the line does not mention who else has to agree: %q", entry.service.ID, entry.service.Blurb)
		}
		delete(want, entry.service.ID)
	}
	if len(want) != 0 {
		t.Fatalf("the list is missing caveats for %v", want)
	}
}

// They are on the one registry with everybody else, they are browser services,
// and each one has the two sentences a person answers about it.
func TestTheToolServersAreOnTheOneRegistry(t *testing.T) {
	registered := map[string]Plug{}
	for _, plug := range Registered() {
		registered[plug.Service().ID] = plug
	}
	manager, err := NewManager(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	offered := map[string]Status{}
	for _, status := range manager.Services() {
		offered[status.ID] = status
	}

	for _, entry := range mcpCatalog() {
		id := entry.service.ID
		plug, found := registered[id]
		if !found {
			t.Errorf("%s did not register itself", id)
			continue
		}
		if plug.Service().Auth != AuthBrowser {
			t.Errorf("%s: Auth = %q, want %q", id, plug.Service().Auth, AuthBrowser)
		}
		if _, listed := offered[id]; !listed {
			t.Errorf("%s is not offered in a build configured with nothing", id)
		}
		if !manager.MCPService(id) {
			t.Errorf("%s brings its own tools and does not say so", id)
		}
		capabilities := manager.Capabilities(id)
		if len(capabilities) != 2 {
			t.Errorf("%s has %d sentences to answer, want the read/act pair", id, len(capabilities))
			continue
		}
		if capabilities[0].ID != CapabilityRead || capabilities[1].ID != CapabilityAct {
			t.Errorf("%s answers %+v", id, capabilities)
		}
	}
}

// The addresses are read off the vendors' own pages, and the exact strings are
// asserted here so that a change to one is a deliberate change to a test rather
// than a quiet edit nobody reviews.
func TestTheAddressesAreTheOnesTheVendorsPublish(t *testing.T) {
	want := map[string]string{
		"airtable":     "https://mcp.airtable.com/mcp",
		"atlassian":    "https://mcp.atlassian.com/v1/mcp/authv2",
		"buildkite":    "https://mcp.buildkite.com/mcp",
		"calendly":     "https://mcp.calendly.com",
		"canva":        "https://mcp.canva.com/mcp",
		"circleci":     "https://mcp.circleci.com/v1/mcp",
		"clickup":      "https://mcp.clickup.com/mcp",
		"cloudflare":   "https://mcp.cloudflare.com/mcp",
		"gitlab":       "https://gitlab.com/api/v4/mcp",
		"grafana":      "https://mcp.grafana.com/mcp",
		"heroku":       "https://mcp.heroku.com/mcp",
		"huggingface":  "https://huggingface.co/mcp",
		"klaviyo":      "https://mcp.klaviyo.com/mcp",
		"launchdarkly": "https://mcp.launchdarkly.com/mcp/launchdarkly",
		"linear":       "https://mcp.linear.app/mcp",
		"miro":         "https://mcp.miro.com",
		"neon":         "https://mcp.neon.tech/mcp",
		"netlify":      "https://netlify-mcp.netlify.app/mcp",
		"notion":       "https://mcp.notion.com/mcp",
		"paypal":       "https://mcp.paypal.com/http",
		"posthog":      "https://mcp.posthog.com/mcp",
		"railway":      "https://mcp.railway.com/",
		"sanity":       "https://mcp.sanity.io",
		"sentry":       "https://mcp.sentry.dev/mcp",
		"supabase":     "https://mcp.supabase.com/mcp",
		"todoist":      "https://ai.todoist.net/mcp",
	}
	for _, entry := range mcpCatalog() {
		if got := entry.address; got != want[entry.service.ID] {
			t.Errorf("%s answers at %q, want %q", entry.service.ID, got, want[entry.service.ID])
		}
	}
}

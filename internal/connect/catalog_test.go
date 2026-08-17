package connect

import (
	"net/url"
	"strings"
	"testing"

	amp "github.com/amp-labs/connectors/providers"
)

// The catalog is somebody else's list and it changes under us, so these tests
// assert LAWS about what comes out of it rather than counts or names: a count
// would fail the day Ampersand added a service, which is a day nothing broke.

func TestOnlyTheServicesAKeyOpensAreTaken(t *testing.T) {
	plugs := catalogPlugs()
	if len(plugs) < 50 {
		t.Fatalf("the catalog gave %d services, which is too few to be right", len(plugs))
	}
	taken := make(map[string]bool, len(plugs))
	for _, plug := range plugs {
		taken[plug.Service().ID] = true
	}
	for _, name := range amp.AllNames() {
		info, err := amp.ReadInfo(name)
		if err != nil {
			continue
		}
		keyed := info.AuthType == amp.ApiKey || info.AuthType == amp.Basic
		if !keyed && taken[string(name)] {
			t.Errorf("%s is not opened by a key and must not be on the menu", name)
		}
	}
}

func TestEveryCatalogServiceIsUsableAsItStands(t *testing.T) {
	for _, plug := range catalogPlugs() {
		service := plug.Service()
		if strings.TrimSpace(service.ID) == "" || strings.TrimSpace(service.Name) == "" {
			t.Errorf("a service with nothing to call it: %+v", service)
			continue
		}
		if service.Auth != AuthKey {
			t.Errorf("%s: Auth = %q, want %q", service.ID, service.Auth, AuthKey)
		}
		if strings.TrimSpace(service.Blurb) == "" {
			t.Errorf("%s: no line to read next to the name", service.ID)
		}
		if len(service.Scopes) != 0 {
			t.Errorf("%s: a key asks for no permissions, got %v", service.ID, service.Scopes)
		}
		holder, ok := plug.(keyService)
		if !ok {
			t.Fatalf("%s: a catalog service must be reachable with a key", service.ID)
		}
		// The address has to be a real one once whatever it is missing has
		// been given.
		address, err := holder.address("example")
		if err != nil {
			t.Errorf("%s: no address: %v", service.ID, err)
			continue
		}
		parsed, err := url.Parse(address)
		if err != nil || parsed.Host == "" || parsed.Scheme != "https" && parsed.Scheme != "http" {
			t.Errorf("%s: address %q is not one a request can be made against", service.ID, address)
		}
		// The id is what a tool is named after, so it must be a word a tool
		// name can be built out of.
		if strings.ContainsAny(service.ID, " \t./:") {
			t.Errorf("%s: an id with punctuation in it cannot name a tool", service.ID)
		}
	}
}

// The house rule holds over a few hundred lines nobody wrote by hand.
func TestCatalogLinesUseNoMachineryVocabulary(t *testing.T) {
	banned := []string{"oauth", "api key", "bearer", "pkce", "endpoint"}
	for _, plug := range catalogPlugs() {
		service := plug.Service()
		lowered := strings.ToLower(service.Blurb)
		for _, word := range banned {
			if strings.Contains(lowered, word) {
				t.Errorf("%s says %q: %q", service.ID, word, service.Blurb)
			}
		}
	}
}

// The catalog and google.go share one registry and one id space, and the
// hand-written plug wins if they ever collide.
func TestTheHandWrittenPlugWinsAnIdItShares(t *testing.T) {
	withPlugs(t,
		&keyPlug{service: Service{ID: "google", Name: "Google from a list", Auth: AuthKey}},
		google{},
		&keyPlug{service: Service{ID: "stripe", Name: "Stripe", Auth: AuthKey}},
	)
	plugs := Registered()
	if len(plugs) != 2 {
		t.Fatalf("two ids, got %d plugs", len(plugs))
	}
	for _, plug := range plugs {
		if plug.Service().ID != "google" {
			continue
		}
		if plug.Service().Auth != AuthBrowser {
			t.Errorf("the hand-written Google must win, got %+v", plug.Service())
		}
	}
}

// ── what is taken from one catalog entry, and what is refused ───────────────

func TestOneCatalogEntryBecomesOnePlug(t *testing.T) {
	header := &amp.ApiKeyOpts{
		AttachmentType: amp.Header,
		Header:         &amp.ApiKeyOptsHeader{Name: "X-Api-Key", ValuePrefix: "Token"},
	}
	query := &amp.ApiKeyOpts{
		AttachmentType: amp.Query,
		Query:          &amp.ApiKeyOptsQuery{Name: "api_key"},
	}

	cases := []struct {
		name string
		id   string
		info amp.ProviderInfo
		want bool
		// then what must be true of the plug that came out
		check func(t *testing.T, plug *keyPlug)
	}{
		{
			name: "a key on a header",
			id:   "example",
			info: amp.ProviderInfo{AuthType: amp.ApiKey, DisplayName: "Example", BaseURL: "https://api.example.com/v1/", ApiKeyOpts: header},
			want: true,
			check: func(t *testing.T, plug *keyPlug) {
				if plug.carry.kind != carryHeader || plug.carry.name != "X-Api-Key" || plug.carry.prefix != "Token" {
					t.Errorf("carry = %+v", plug.carry)
				}
				// The trailing slash goes, so that a path can be appended
				// without doubling it.
				if plug.base != "https://api.example.com/v1" {
					t.Errorf("base = %q", plug.base)
				}
			},
		},
		{
			name: "a key on a query parameter",
			id:   "example",
			info: amp.ProviderInfo{AuthType: amp.ApiKey, DisplayName: "Example", BaseURL: "https://api.example.com", ApiKeyOpts: query},
			want: true,
			check: func(t *testing.T, plug *keyPlug) {
				if plug.carry.kind != carryQuery || plug.carry.name != "api_key" {
					t.Errorf("carry = %+v", plug.carry)
				}
			},
		},
		{
			name: "a name and a password",
			id:   "example",
			info: amp.ProviderInfo{AuthType: amp.Basic, DisplayName: "Example", BaseURL: "https://api.example.com"},
			want: true,
			check: func(t *testing.T, plug *keyPlug) {
				if plug.carry.kind != carryBasic {
					t.Errorf("carry = %+v", plug.carry)
				}
			},
		},
		{
			name: "a blank the catalog can fill itself",
			id:   "example",
			info: amp.ProviderInfo{
				AuthType: amp.ApiKey, DisplayName: "Example", ApiKeyOpts: header,
				BaseURL: "https://{{.region}}.example.com",
				Metadata: &amp.ProviderMetadata{Input: []amp.MetadataItemInput{
					{Name: "region", DisplayName: "Region", DefaultValue: "eu"},
				}},
			},
			want: true,
			check: func(t *testing.T, plug *keyPlug) {
				if plug.blank.name != "" {
					t.Errorf("a blank with an ordinary answer must never be asked about, got %+v", plug.blank)
				}
				if plug.base != "https://eu.example.com" {
					t.Errorf("base = %q", plug.base)
				}
			},
		},
		{
			name: "a blank only the person knows",
			id:   "example",
			info: amp.ProviderInfo{
				AuthType: amp.ApiKey, DisplayName: "Example", ApiKeyOpts: header,
				BaseURL: "https://{{.workspace}}.example.com",
				Metadata: &amp.ProviderMetadata{Input: []amp.MetadataItemInput{
					{Name: "workspace", DisplayName: "Domain"},
				}},
			},
			want: true,
			check: func(t *testing.T, plug *keyPlug) {
				if plug.blank.name != "workspace" || plug.blank.label != "Domain" {
					t.Errorf("blank = %+v", plug.blank)
				}
				if plug.service.Address != "https://<domain>.example.com" {
					t.Errorf("the address a person reads = %q", plug.service.Address)
				}
				if !strings.Contains(plug.service.Blurb, "domain") {
					t.Errorf("the line must say what is wanted first: %q", plug.service.Blurb)
				}
			},
		},
		{
			name: "a browser service",
			id:   "example",
			info: amp.ProviderInfo{AuthType: amp.Oauth2, DisplayName: "Example", BaseURL: "https://api.example.com"},
			want: false,
		},
		{
			name: "nothing to call it",
			id:   "example",
			info: amp.ProviderInfo{AuthType: amp.ApiKey, BaseURL: "https://api.example.com", ApiKeyOpts: header},
			want: false,
		},
		{
			name: "an address that is not one",
			id:   "example",
			info: amp.ProviderInfo{AuthType: amp.ApiKey, DisplayName: "Example", BaseURL: "https", ApiKeyOpts: header},
			want: false,
		},
		{
			name: "two blanks and one answer",
			id:   "example",
			info: amp.ProviderInfo{
				AuthType: amp.ApiKey, DisplayName: "Example", ApiKeyOpts: header,
				BaseURL: "https://{{.workspace}}.{{.region}}.example.com",
			},
			want: false,
		},
		{
			name: "a key with nowhere to go",
			id:   "example",
			info: amp.ProviderInfo{AuthType: amp.ApiKey, DisplayName: "Example", BaseURL: "https://api.example.com"},
			want: false,
		},
	}

	for _, c := range cases {
		info := c.info
		plug, ok := catalogPlug(c.id, &info)
		if ok != c.want {
			t.Errorf("%s: taken = %v, want %v", c.name, ok, c.want)
			continue
		}
		if ok && c.check != nil {
			c.check(t, plug)
		}
	}
}

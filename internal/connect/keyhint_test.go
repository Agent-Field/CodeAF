package connect

import (
	"net/url"
	"sort"
	"strings"
	"testing"

	amp "github.com/Agent-Field/aforge-v2/internal/connect/ampcatalog"
)

// WHERE A PERSON GOES TO FIND THEIR KEY (keyhint.go, catalog.go's
// [catalogKeyHint]).

// EVERY SERVICE A KEY OPENS SAYS WHERE THE KEY IS. This is the test keyhint.go's
// opening paragraph promises: the catalog answers for most of them and this
// build answers for the rest, so "paste your key" is never an instruction a
// person cannot follow. A catalog that grows past the list is a build with a box
// that asks for something and does not say where it lives, and that is a thing
// to find here rather than there.
func TestEveryKeyServiceSaysWhereItsKeyIs(t *testing.T) {
	var silent []string
	for _, plug := range catalogPlugs() {
		service := plug.Service()
		if strings.TrimSpace(service.KeyHint) == "" {
			silent = append(silent, service.ID)
		}
	}
	if len(silent) > 0 {
		sort.Strings(silent)
		t.Errorf("%d key services say nothing about where their key is — add a line to keyhint.go: %s",
			len(silent), strings.Join(silent, ", "))
	}
}

// AND EVERY ONE OF THEM IS A WEB ADDRESS. A link that goes nowhere is worse than
// the emptiness it replaced, because a person follows it.
func TestEveryKeyHintIsAWebAddress(t *testing.T) {
	for _, plug := range catalogPlugs() {
		service := plug.Service()
		hint := service.KeyHint
		if hint == "" {
			continue
		}
		if hint != strings.TrimSpace(hint) {
			t.Errorf("%s: the address carries whitespace: %q", service.ID, hint)
		}
		parsed, err := url.Parse(hint)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			t.Errorf("%s: %q is not a web address", service.ID, hint)
		}
	}
	// The hand-written map as well, including any line for a service this
	// build's catalog happens not to bring.
	for id, hint := range serviceKeyHints {
		if !addressable(hint) {
			t.Errorf("%s: %q is not a web address", id, hint)
		}
	}
}

// THE HAND-WRITTEN LINE WINS AND THE CATALOG IS THE FLOOR. A line in keyhint.go
// is either a page the catalog does not know about or the screen the key is
// actually on; both beat a page describing what a key is.
func TestTheWrittenHintBeatsTheCatalogsOwn(t *testing.T) {
	info, err := amp.ReadInfo("stripe")
	if err != nil || info == nil {
		t.Skipf("this catalog does not bring stripe")
	}
	docs := catalogDocs(info)
	written := curatedKeyHint("stripe")
	if written == "" {
		t.Fatalf("stripe has no written line")
	}
	if docs == written {
		t.Skipf("the catalog and the written line agree, so there is nothing to prefer")
	}
	if got := catalogKeyHint("stripe", info); got != written {
		t.Errorf("catalogKeyHint(stripe) = %q, want the written line %q", got, written)
	}
	// And a service with nothing written takes the catalog's own.
	if info, err := amp.ReadInfo("apollo"); err == nil && info != nil && curatedKeyHint("apollo") == "" {
		if got, want := catalogKeyHint("apollo", info), catalogDocs(info); got != want {
			t.Errorf("catalogKeyHint(apollo) = %q, want the catalog's %q", got, want)
		}
	}
}

// A line for a service the catalog does not bring is a line nobody reads, and
// the day the catalog drops a service that is how it goes unnoticed — the same
// rule category.go keeps about its own map.
func TestNothingIsHintedThatTheCatalogDoesNotBring(t *testing.T) {
	brought := map[string]bool{}
	for _, plug := range catalogPlugs() {
		brought[plug.Service().ID] = true
	}
	for id := range serviceKeyHints {
		if !brought[id] {
			t.Errorf("%s has a key hint but the catalog does not bring it", id)
		}
	}
}

// A SERVICE CONNECTED IN A BROWSER HAS NO KEY TO GO AND FIND, so it says
// nothing — the emptiness law, on the field most likely to be filled in "for
// consistency".
func TestABrowserServiceSaysNothingAboutKeys(t *testing.T) {
	for _, plug := range Registered() {
		service := plug.Service()
		if service.Auth == AuthKey {
			continue
		}
		if service.KeyHint != "" {
			t.Errorf("%s is connected in a browser and points at a key page: %q",
				service.ID, service.KeyHint)
		}
	}
}

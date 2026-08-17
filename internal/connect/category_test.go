package connect

import (
	"sort"
	"strings"
	"testing"
)

// EVERY SERVICE THE CATALOG OPENS IS FILED. This is the test category.go's
// closing paragraph promises: a build whose catalog has grown past the list is a
// build with a heading reading "other" on somebody's screen, and that is a thing
// to find here rather than there.
func TestEveryCatalogServiceIsFiled(t *testing.T) {
	known := make(map[string]bool, len(categories))
	for _, word := range categories {
		known[word] = true
	}

	var unfiled []string
	seen := map[string]int{}
	for _, plug := range catalogPlugs() {
		service := plug.Service()
		word := service.Category
		if strings.TrimSpace(word) == "" {
			unfiled = append(unfiled, service.ID)
			continue
		}
		if !known[word] {
			t.Errorf("%s is filed under %q, which is not one of the thirteen words", service.ID, word)
		}
		seen[word]++
	}
	if len(unfiled) > 0 {
		sort.Strings(unfiled)
		t.Errorf("%d catalog services have no category: %s", len(unfiled), strings.Join(unfiled, ", "))
	}

	// And the list is the words this build actually uses. A constant nobody
	// files anything under is a heading that can never appear, which is a line
	// to delete rather than a category.
	for _, word := range categories {
		if seen[word] == 0 {
			t.Errorf("nothing is filed under %q", word)
		}
	}
}

// The other direction: a line in the map for a service the catalog does not
// bring is a line nobody reads, and the day the catalog drops a service that is
// how it goes unnoticed.
func TestNothingIsFiledThatTheCatalogDoesNotBring(t *testing.T) {
	brought := map[string]bool{}
	for _, plug := range catalogPlugs() {
		brought[plug.Service().ID] = true
	}
	for id := range serviceCategories {
		if !brought[id] {
			t.Errorf("%s is filed but the catalog does not bring it", id)
		}
	}
}

// The hand-written plug is filed too — a service this package wrote itself is
// not a different kind of row on a menu.
func TestTheBrowserPlugIsFiledAsWell(t *testing.T) {
	for _, plug := range Registered() {
		service := plug.Service()
		if service.Auth != AuthBrowser {
			continue
		}
		if strings.TrimSpace(service.Category) == "" {
			t.Errorf("%s has no category", service.ID)
		}
	}
}

// A CATALOG SERVICE SAYS THE SAME TWO SENTENCES AS ANY OTHER. The panel cannot
// tell it apart from a plug that declared four, and its one tool is owned by the
// half that acts — the stricter of the two, with the verb choosing the other at
// judging time (catalog.go's init).
func TestCatalogServicesDeclareTheReadActPair(t *testing.T) {
	manager, _ := testManager(t)

	for _, id := range []string{"stripe", "freshdesk", "brevo"} {
		capabilities := manager.Capabilities(id)
		if len(capabilities) != 2 {
			t.Fatalf("%s: got %d capabilities, want the pair", id, len(capabilities))
		}
		if capabilities[0].ID != CapabilityRead || capabilities[1].ID != CapabilityAct {
			t.Errorf("%s: got %q and %q, want %q and %q",
				id, capabilities[0].ID, capabilities[1].ID, CapabilityRead, CapabilityAct)
		}
		if got := manager.ToolCapability(id, id+"_request"); got != CapabilityAct {
			t.Errorf("ToolCapability(%s, %s_request): got %q, want %q", id, id, got, CapabilityAct)
		}
		// The defaults are today's behaviour here too: reading runs, acting is
		// asked about.
		if got := manager.CapabilityState(id, CapabilityRead); got != StateYes {
			t.Errorf("%s read: got %q, want %q", id, got, StateYes)
		}
		if got := manager.CapabilityState(id, CapabilityAct); got != StateAsk {
			t.Errorf("%s act: got %q, want %q", id, got, StateAsk)
		}
	}
}

// The hand-written declaration wins over the generic pair, whichever init runs
// first — the same collision [Registered] settles for the plugs themselves.
func TestAHandWrittenDeclarationBeatsTheGenericPair(t *testing.T) {
	for _, order := range []string{"generic first", "hand-written first"} {
		withCapabilities(t, func() {
			generic := func() {
				RegisterGenericCapabilities("ledger", map[string]string{"ledger_request": CapabilityAct})
			}
			written := func() {
				RegisterCapabilities("ledger",
					[]Capability{{ID: "post", Phrase: "post to your ledger", Acts: true}},
					map[string]string{"ledger_post": "post"})
			}
			if order == "generic first" {
				generic()
				written()
			} else {
				written()
				generic()
			}
		})
		manager, _ := testManager(t)
		capabilities := manager.Capabilities("ledger")
		if len(capabilities) != 1 || capabilities[0].ID != "post" {
			t.Errorf("%s: got %+v, want the hand-written declaration", order, capabilities)
		}
		if got := manager.ToolCapability("ledger", "ledger_post"); got != "post" {
			t.Errorf("%s: ToolCapability(ledger, ledger_post): got %q", order, got)
		}
	}
}

// Google's own declaration survived the catalog registering hundreds of generic
// pairs beside it, which is the real case the rule above exists for.
func TestGoogleKeepsItsOwnSentences(t *testing.T) {
	manager, _ := testManager(t)
	if got := len(manager.Capabilities("google")); got != 4 {
		t.Errorf("google: got %d capabilities, want its own 4", got)
	}
}

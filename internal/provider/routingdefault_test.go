package provider

import (
	"strings"
	"testing"
)

// nothingInstalled puts this process back to nobody having written a routing
// row, and restores whatever was there afterwards — the knob is process-wide
// ([InstallRouting]), so a test that left one set would route every test after
// it.
func nothingInstalled(t *testing.T) {
	t.Helper()
	before, ok := installedRoutingChoice()
	InstallRouting("")
	t.Cleanup(func() {
		if !ok {
			InstallRouting("")
			return
		}
		InstallRouting(before)
	})
}

// THE SHIPPED ROW IS THE WHOLE ALGORITHM, and this is the law that says so from
// the only place it can be checked honestly: the wire.
//
// A client nobody has routed — no [RoutingSource] handed to it, nothing
// installed by [InstallRouting] — puts NO provider object on the request at
// all. Not a sort word, not the ledger's order, not a price ceiling, not the
// endpoint that answered last time. Whatever this process believes about the
// machines behind the model, none of it is asked for, and OpenRouter's own
// default routing answers.
//
// It is written against a client built by hand rather than through
// [rankedRoad], because every other fixture in this package says which road it
// is about and this one is about saying nothing at all.
func TestAClientNobodyHasRoutedSendsNoProviderObject(t *testing.T) {
	server := prefRig(t)
	nothingInstalled(t)
	client, err := NewClient(Config{APIKey: "test-key", BaseURL: server.URL(), Model: plainModel})
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = newVelocityLedger()

	if got := client.routing(); got != DefaultRouting {
		t.Fatalf("a client handed no routing answers to %q, want the shipped %q", got, DefaultRouting)
	}
	if _, err := client.CompleteWithMessages(talking(), userMessages("hello")); err != nil {
		t.Fatalf("the unrouted turn failed: %v", err)
	}
	asks := server.Asks()
	if len(asks) != 1 {
		t.Fatalf("one turn produced %d asks, want exactly one — nothing hedges under the shipped row", len(asks))
	}
	bare := asks[0]
	if bare.Sort != "" || bare.Only != nil || bare.Order != nil || bare.Ignore != nil ||
		bare.MaxPrice != nil || bare.NoFallbacks {
		t.Fatalf("a client nobody routed carried %+v, want a bare request", bare)
	}
}

// AND A PERSON'S OWN PIN IS STILL THE WHOLE REQUEST THERE. The shipped row is
// not "send nothing"; it is "send what they asked for and nothing else", and
// with nobody having written a routing row the pin is the only thing anybody
// asked for.
func TestTheShippedRowStillSendsThePinAndNothingElse(t *testing.T) {
	server := prefRig(t)
	nothingInstalled(t)
	client, err := NewClient(Config{APIKey: "test-key", BaseURL: server.URL(), Model: plainModel})
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = newVelocityLedger()
	pinned(t, LanePin{Lane: "Harbor"})

	if _, err := client.CompleteWithMessages(talking(), userMessages("hello")); err != nil {
		t.Fatalf("the pinned turn failed: %v", err)
	}
	asks := server.Asks()
	if len(asks) != 1 {
		t.Fatalf("one turn produced %d asks, want exactly one", len(asks))
	}
	demand := asks[0]
	if len(demand.Only) != 1 || !strings.EqualFold(demand.Only[0], "Harbor") || !demand.NoFallbacks ||
		demand.Sort != "" || demand.MaxPrice != nil || demand.Order != nil {
		t.Fatalf("the pinned ask carried %+v, want only Harbor and fallbacks off", demand)
	}
}

// AN UNKNOWN WORD IS THE SHIPPED ROW AND NOT A ROAD NOBODY ASKED FOR. A row a
// later build spells, or a typo, must not put a session on the ranked road —
// which is what a fallback of `latency` did while `latency` was the default.
func TestAnUnreadableRoutingWordFallsToTheShippedRow(t *testing.T) {
	if got, known := ParseRoutingStrategy("quickest"); known || got != DefaultRouting {
		t.Fatalf("an unknown word parsed to %q (known=%v), want the shipped %q", got, known, DefaultRouting)
	}
	if got, known := ParseRoutingStrategy(""); known || got != DefaultRouting {
		t.Fatalf("an empty word parsed to %q (known=%v), want the shipped %q", got, known, DefaultRouting)
	}
}

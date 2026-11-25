package appwire

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

type crewRows struct {
	fakeEngine
	carry bool
}

func (c *crewRows) ChatRowsCarryCrews() bool { return c.carry }

// chats.crew is advertised only by an engine whose rows carry crew facts, and
// the facts are additive: a row in no crew says nothing about one.
func TestChatsCrewIsAdvertisedOnlyWhereRowsCarryCrews(t *testing.T) {
	if h := withChatsFeatures(Hello{}, &fakeEngine{}); slices.Contains(h.Features, FeatureChatsCrew) {
		t.Fatal("an engine without crew facts advertises chats.crew")
	}
	if h := withChatsFeatures(Hello{}, &crewRows{carry: false}); slices.Contains(h.Features, FeatureChatsCrew) {
		t.Fatal("an engine that says its rows carry no crews advertises chats.crew")
	}
	if h := withChatsFeatures(Hello{}, &crewRows{carry: true}); !slices.Contains(h.Features, FeatureChatsCrew) {
		t.Fatal("an engine whose rows carry crews does not advertise chats.crew")
	}
	loose, _ := json.Marshal(ChatRow{Cell: "c"})
	if strings.Contains(string(loose), "crew") {
		t.Fatalf("a row in no crew says %s", loose)
	}
	in, _ := json.Marshal(ChatRow{Cell: "c", Crew: "da48d479ea82", CrewName: "Todo crew", CrewManager: true, CrewKind: "factory"})
	for _, want := range []string{`"crew":"da48d479ea82"`, `"crew_name":"Todo crew"`, `"crew_manager":true`, `"crew_kind":"factory"`} {
		if !strings.Contains(string(in), want) {
			t.Fatalf("%s lacks %s", in, want)
		}
	}
}

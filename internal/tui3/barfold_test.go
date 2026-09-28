package tui3

import (
	"strings"
	"testing"
)

// AND THE MAP'S CLAUSE ABOUT `→` SAYS WHAT THE KEY DOES.
//
// It read `→ verbs on this row` on a line where every other clause names an act:
// `alt+1…9 go to a place`, `alt+enter send it off as a task`, `esc close`.
// `verbs` is the machinery's name for the strip, not anybody's word for what
// pressing the key gets them.
func TestTheMapNamesWhatTheArrowDoesAndNotWhatItIsCalled(t *testing.T) {
	if strings.Contains(placeMapVerbWords, "verbs") {
		t.Fatalf("the map's arrow clause reads %q, which names the machinery's category rather than the act", placeMapVerbWords)
	}
	// Every clause on that line is a key and then a VERB — the word right after
	// the key is something a person does.
	for _, clause := range strings.Split(placeMapWords, " · ") {
		fields := strings.Fields(clause)
		if len(fields) < 2 {
			t.Fatalf("the map's line has a clause with no verb in it: %q (in %q)", clause, placeMapWords)
		}
		if verb := fields[1]; !mapVerbs[verb] {
			t.Fatalf("the clause %q opens with %q, which is not something a person does; the line reads %q",
				clause, verb, placeMapWords)
		}
	}
}

// mapVerbs is what the map's line is allowed to say a key does — every one of
// them a plain act, which is the law this file's second test states.
var mapVerbs = map[string]bool{"go": true, "send": true, "show": true, "close": true, "open": true}

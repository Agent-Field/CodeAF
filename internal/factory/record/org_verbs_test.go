package record

import "testing"

// AN ORGANIZATION CHANGE WITH A VERB THE TABLE LACKS IS DROPPED IN SILENCE
// (the writers ignore the error), so every verb the Library and the
// organization's agents write is checked against the table here.
func TestTheLibraryAndAgentVerbsAreOrganizationChanges(t *testing.T) {
	for kind, verbs := range map[string][]string{
		LibraryChanged:  {"added library page", "changed library page", "removed library page"},
		AgentChanged:    {"asked for org agent", "approved org agent", "declined org agent", "added org agent", "removed org agent"},
		SpendingChanged: {"set space limit"},
	} {
		for _, verb := range verbs {
			e := Event{Kind: kind, Verb: verb, Object: "library:lb_x", Handle: "santosh", Actor: Actor{Kind: "person", Name: "@santosh"}}
			if !orgKinds[kind] {
				t.Fatalf("%s is not an organization kind", kind)
			}
			if err := validateOrg(e); err != nil {
				t.Fatalf("%s %q: %v", kind, verb, err)
			}
		}
	}
}

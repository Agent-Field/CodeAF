package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// THE LOOKUP READS THE PERSON'S OWN WORDS (#307).
//
// The model composes a query rather than passing on the question it was asked,
// and the manual is a few dozen short sections: two words nobody said move the
// ranking off the page. internal/manual's theirwords.go holds the retrieval side
// of that with no model in the loop; what is held HERE is the wiring — that the
// tool the model actually calls hands the person's sentence to the search, and
// hands it the current one.

// askManualOf calls the tool the way a model does, on a conversation that has
// heard the person say something.
func askManualOf(t *testing.T, agent *Agent, arguments map[string]string) string {
	t.Helper()
	encoded, err := json.Marshal(arguments)
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := agent.manualTool().Execute(context.Background(), encoded)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

const (
	// personsPrivacyQuestion is what the person typed, and modelsRewrite is what
	// deepseek/deepseek-v4-flash was measured sending instead (#307, #309).
	// Neither is a phrasing invented here.
	personsPrivacyQuestion = "who can see my files in aforge"
	modelsRewrite          = "who can see my files privacy file access"
	permissionsLabel       = "[permissions · "
)

func TestTheManualLookupReadsThePersonsWordsAndNotOnlyTheModels(t *testing.T) {
	// A conversation nobody has spoken in yet is the before picture, and it has
	// to MISS — otherwise the rewrite has stopped measuring anything and the
	// case below would pass on the corpus rather than on the wiring.
	alone := askManualOf(t, &Agent{}, map[string]string{"query": modelsRewrite})
	if strings.Contains(alone, permissionsLabel) {
		t.Fatalf("%q reaches the permissions page on its own now; this case needs a rewrite that still misses", modelsRewrite)
	}
	heard := askManualOf(t, &Agent{personAsk: personsPrivacyQuestion}, map[string]string{"query": modelsRewrite})
	if !strings.Contains(heard, permissionsLabel) {
		t.Errorf("the person asked %q, the model looked up %q, and no permissions section came back:\n%s",
			personsPrivacyQuestion, modelsRewrite, shortenResult(heard))
	}
}

// TestTheLookupReadsTheNEWESTThingThePersonSaid is what makes this the person's
// words rather than the conversation's opening. A steer lands mid-turn and moves
// [Agent.personAsk] (task_brief.go), so a lookup made after it must be about
// what they just said.
func TestTheLookupReadsTheNEWESTThingThePersonSaid(t *testing.T) {
	agent := &Agent{personAsk: "how do I set a spending limit"}
	agent.personAsk = personsPrivacyQuestion
	if heard := askManualOf(t, agent, map[string]string{"query": modelsRewrite}); !strings.Contains(heard, permissionsLabel) {
		t.Errorf("the lookup did not follow the person to their newest question:\n%s", shortenResult(heard))
	}
}

// TestAPageAskedForByNameIgnoresThePersonsWords is the other half of the tool.
// A named page is an exact request; a second question read into it would be the
// search this call exists to avoid.
func TestAPageAskedForByNameIgnoresThePersonsWords(t *testing.T) {
	agent := &Agent{personAsk: "how do I make a video"}
	named := askManualOf(t, agent, map[string]string{"page": "permissions"})
	quiet := askManualOf(t, &Agent{}, map[string]string{"page": "permissions"})
	if named != quiet {
		t.Errorf("reading the permissions page by name answered differently on a conversation that had said something (%d bytes) than on one that had not (%d bytes)",
			len(named), len(quiet))
	}
}

func shortenResult(text string) string {
	if len(text) <= 600 {
		return text
	}
	return text[:600] + "…"
}

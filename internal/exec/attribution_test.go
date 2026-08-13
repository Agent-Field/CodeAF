package exec

import (
	"context"
	"strings"
	"testing"
	"time"
)

// attributionStrings are the bytes that are the feature. They are pinned here
// rather than described, because a reworded trailer does not attribute and a
// footer that lost a utm parameter cannot be counted — either would pass a test
// that only looked for the word "attribution".
var attributionStrings = []string{
	"Co-Authored-By: aforge <agentfield-bot@users.noreply.github.com>",
	"Drafted with [agentfield ai](https://agentfield.ai/github?utm_source=github&utm_medium=pull_request&utm_campaign=drafted_with) · reviewed and owned by the author",
	"Drafted with [agentfield ai](https://agentfield.ai/github?utm_source=github&utm_medium=issue&utm_campaign=drafted_with) · reviewed and owned by the author",
}

func TestAttributionConstantsAreTheExactStrings(t *testing.T) {
	for index, want := range attributionStrings {
		got := []string{AttributionTrailer, AttributionPullFooter, AttributionIssueFooter}[index]
		if got != want {
			t.Fatalf("constant %d = %q, want %q", index, got, want)
		}
	}
	if AttributionSeparator != "—" {
		t.Fatalf("separator = %q, want an em dash", AttributionSeparator)
	}
}

func TestAttributionLawEntersTheContractOnlyWhenItIsOn(t *testing.T) {
	on := NewLinear(&scriptedCompleter{}, workspace(t), nil, 10, 1_000_000, time.Minute).
		WithAttribution(true).system(Task{NodeID: 1, Brief: "work"}, nil)
	for _, want := range attributionStrings {
		if !strings.Contains(on, want) {
			t.Fatalf("the contract is missing %q", want)
		}
	}
	for _, want := range []string{"CONTRIBUTING", "commit subject", "README"} {
		if !strings.Contains(on, want) {
			t.Fatalf("the contract does not say where attribution must not go: %q", want)
		}
	}

	off := NewLinear(&scriptedCompleter{}, workspace(t), nil, 10, 1_000_000, time.Minute).
		system(Task{NodeID: 1, Brief: "work"}, nil)
	for _, unwanted := range append(attributionStrings, "agentfield", "Co-Authored-By") {
		if strings.Contains(off, unwanted) {
			t.Fatalf("attribution is off and the contract still says %q", unwanted)
		}
	}
	if off != systemPrompt {
		t.Fatal("the default contract is no longer the plain system prompt")
	}
}

// The law is unconditional once on: a reflex micro-leaf and a contracted job
// carry it too, because nothing detects in advance whether a job will touch git.
func TestAttributionRidesEveryShapeOfLeafToTheModel(t *testing.T) {
	for _, task := range []Task{
		{NodeID: 1, Brief: "work"},
		{NodeID: 2, Brief: "work", Reflex: true},
		{NodeID: 3, Brief: "work", Contract: "read the diff first"},
	} {
		client := &scriptedCompleter{}
		linear := NewLinear(client, workspace(t), nil, 10, 1_000_000, time.Minute).WithAttribution(true)
		if _, err := linear.Run(context.Background(), task); err != nil {
			t.Fatal(err)
		}
		if len(client.seen) == 0 {
			t.Fatal("the model was never called")
		}
		system := client.seen[0][0].Content[0].Text
		for _, want := range attributionStrings {
			if !strings.Contains(system, want) {
				t.Fatalf("node %d never saw %q", task.NodeID, want)
			}
		}
	}
}

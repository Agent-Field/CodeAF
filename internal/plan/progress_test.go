package plan

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type progressPassClient struct{ passClient }

func (c *progressPassClient) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var system string
	for _, message := range messages {
		if message.Role == "system" {
			system += textOf(message)
		}
	}
	switch system {
	case groundPrompt:
		return textResponse("{\"settled\":[\"The three cities are Berlin, Lisbon, and Warsaw.\"],\"open\":[],\"evidence\":\"read and cite sources\"}"), nil
	case briefPrompt:
		return textResponse("Do this part and return its concrete result."), nil
	default:
		return c.passClient.CompleteWithMessages(ctx, messages, options...)
	}
}

func TestBuildProgressSequence(t *testing.T) {
	var got []string
	graph, err := Build(context.Background(), &progressPassClient{},
		"compare three cities and write the result", Options{
			Ensemble: EnsembleNever, SpineSamples: 3, MaxDepth: 1, Briefs: true,
			Progress: func(stage, detail string) {
				got = append(got, stage+": "+detail)
			},
		})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if graph == nil {
		t.Fatal("Build returned a nil graph")
	}

	var boundaries, briefCounts []string
	for _, event := range got {
		if strings.HasPrefix(event, "briefs: ") {
			briefCounts = append(briefCounts, event)
			continue
		}
		boundaries = append(boundaries, event)
	}
	wantBoundaries := []string{
		"grounding: settling what to look at",
		"spine: sample 1/3",
		"spine: sample 2/3",
		"spine: sample 3/3",
		"grounded: 3 cities settled",
		"spine: 2 stages",
		"fan-out: 3 nodes",
		"sizing: 3 nodes — 0 nodes to split",
		"audit: 0 links restored",
		"expand: nothing else needs splitting",
	}
	if !reflect.DeepEqual(boundaries, wantBoundaries) {
		t.Fatalf("progress boundaries = %#v\nwant %#v", boundaries, wantBoundaries)
	}
	if len(briefCounts) == 0 || briefCounts[len(briefCounts)-1] != "briefs: 3/3" {
		t.Fatalf("brief progress = %#v, want a final 3/3", briefCounts)
	}
}

func TestBuildNilProgressIsByteIdentical(t *testing.T) {
	options := Options{Ensemble: EnsembleNever, SpineSamples: 1, MaxDepth: 1}
	without, err := Build(context.Background(), &passClient{},
		"review the pull request and deliver REVIEW.md", options)
	if err != nil {
		t.Fatalf("Build without progress: %v", err)
	}
	var calls int
	options.Progress = func(string, string) { calls++ }
	with, err := Build(context.Background(), &passClient{},
		"review the pull request and deliver REVIEW.md", options)
	if err != nil {
		t.Fatalf("Build with progress: %v", err)
	}
	withoutJSON, err := without.JSON()
	if err != nil {
		t.Fatal(err)
	}
	withJSON, err := with.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(withoutJSON, withJSON) {
		t.Fatalf("progress changed graph bytes:\nwithout:\n%s\nwith:\n%s", withoutJSON, withJSON)
	}
	if calls == 0 {
		t.Fatal("non-nil progress callback was never called")
	}
}

func TestContractsReportParallelCompletionsInOrder(t *testing.T) {
	graph := &Graph{Goal: "prepare three findings", NextID: 1}
	for _, title := range []string{"First", "Second", "Third"} {
		graph.Add(Node{Title: title, Summary: "Return one finding", Kind: KindWork})
	}
	client := &stubClient{reply: func(string, string) string {
		return "{\"contract\":\"Check the evidence and return the finding.\"}"
	}}
	var got []string
	_, err := Contracts(context.Background(), client, graph, nil, func(stage, detail string) {
		got = append(got, stage+" "+detail)
	})
	if err != nil {
		t.Fatalf("Contracts: %v", err)
	}
	want := []string{"contracts 0/3", "contracts 1/3", "contracts 2/3", "contracts 3/3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("contract progress = %#v, want %#v", got, want)
	}
}

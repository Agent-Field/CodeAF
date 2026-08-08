package plan

import (
	"bytes"
	"context"
	"reflect"
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
	var got []ProgressUpdate
	graph, err := Build(context.Background(), &progressPassClient{},
		"compare three cities and write the result", Options{
			Ensemble: EnsembleNever, SpineSamples: 3, MaxDepth: 1, Briefs: true,
			Progress: func(update ProgressUpdate) {
				got = append(got, update)
			},
		})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if graph == nil {
		t.Fatal("Build returned a nil graph")
	}

	var phases []string
	var finalBrief ProgressUpdate
	for _, event := range got {
		phases = append(phases, event.Phase)
		if event.Phase == "writing the plan" {
			finalBrief = event
		}
	}
	for _, want := range []string{
		"reading the request", "exploring approaches", "choosing the shape",
		"breaking it into steps — 3", "writing the plan",
	} {
		if !containsString(phases, want) {
			t.Fatalf("progress phases = %#v, missing %q", phases, want)
		}
	}
	// Three city leaves and the node that gathers them: the deliverable owner is
	// written an instruction like any other leaf, and the count says so rather
	// than reading "4/3" while the fourth call runs.
	if finalBrief.Done != 4 || finalBrief.Total != 4 || finalBrief.Latest == "" {
		t.Fatalf("final plan-writing progress = %#v, want 4 of 4 with a real title", finalBrief)
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func TestUserProgressVocabularyMapsEveryPlannerStage(t *testing.T) {
	tests := []struct {
		stage  string
		detail string
		phase  string
		done   int
		total  int
	}{
		{"grounding", "settling what to look at", "reading the request", 0, 0},
		{"grounded", "3 cities settled", "reading the request", 0, 0},
		{"spine", "sample 2/3", "exploring approaches", 2, 3},
		{"spine", "3 stages", "choosing the shape", 0, 0},
		{"ensemble", "deciding", "choosing the shape", 0, 0},
		{"fan-out", "11 nodes", "choosing the shape", 0, 0},
		{"sizing", "11 nodes — 3 to split", "choosing the shape", 0, 0},
		{"audit", "0 links restored", "choosing the shape", 0, 0},
		{"expand", "3 nodes split", "choosing the shape", 0, 0},
		{"steps", "21", "breaking it into steps — 21", 0, 0},
		{"briefs", "12/18", "writing the plan", 12, 18},
		{"contracts", "3/18", "setting working standards", 3, 18},
	}
	for _, test := range tests {
		t.Run(test.stage+"/"+test.detail, func(t *testing.T) {
			got := userProgress(test.stage, test.detail, "A real title")
			if got.Phase != test.phase || got.Done != test.done || got.Total != test.total || got.Latest != "A real title" {
				t.Fatalf("userProgress(%q, %q) = %#v", test.stage, test.detail, got)
			}
		})
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
	options.Progress = func(ProgressUpdate) { calls++ }
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
	var got []ProgressUpdate
	_, err := Contracts(context.Background(), client, graph, nil, func(update ProgressUpdate) {
		got = append(got, update)
	})
	if err != nil {
		t.Fatalf("Contracts: %v", err)
	}
	var counts []int
	for _, update := range got {
		if update.Phase != "setting working standards" || update.Total != 3 {
			t.Fatalf("contract progress = %#v", got)
		}
		counts = append(counts, update.Done)
	}
	if want := []int{0, 1, 2, 3}; !reflect.DeepEqual(counts, want) {
		t.Fatalf("contract counts = %#v, want %#v", counts, want)
	}
	for _, update := range got[1:] {
		if update.Latest == "" {
			t.Fatalf("contract completion omitted its node title: %#v", got)
		}
	}
}

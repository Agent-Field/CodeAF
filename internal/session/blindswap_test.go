package session

// The transcript guard, from both doors: the model swapped mid-conversation
// (/model) and the session reopened on a model without eyes.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// writeableJournal is a path for a session file in a directory of this test's
// own, so a resume has something to reopen.
func writeableJournal(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "session.jsonl")
}

// mustSubmitImage is [Agent.SubmitImage] where the refusal would be the bug.
func mustSubmitImage(t *testing.T, agent *Agent, ctx context.Context, text string, images []Image) <-chan Event {
	t.Helper()
	events, err := agent.SubmitImage(ctx, text, images)
	if err != nil {
		t.Fatalf("SubmitImage: %v", err)
	}
	return events
}

// countImageParts is how many pictures the whole live transcript still carries.
func countImageParts(agent *Agent) int {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	count := 0
	for _, message := range agent.messages {
		count += len(imagePartURLs(message))
	}
	return count
}

// Switching onto a model without eyes turns the pictures into their
// placeholders — once, naming the file, with the person's own words left
// alone.
func TestSetModelScrubsImagePartsForAModelThatCannotSee(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.SupportsImages = func(model string) bool { return model == "test/model" }
		config.SessionFile = writeableJournal(t)
	})
	path := writeImage(t, workspace, "chart.png", "PHOTOBYTES")

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	collect(t, mustSubmitImage(t, agent, ctx, "what is wrong with this?", []Image{{Path: path}}))
	if countImageParts(agent) != 1 {
		t.Fatalf("the picture never reached the transcript")
	}

	agent.SetModel("vendor/blind")
	if count := countImageParts(agent); count != 0 {
		t.Fatalf("%d image parts survived the swap onto a blind model", count)
	}
	text := transcriptText(agent)
	if !strings.Contains(text, "[image "+path+" — this model cannot see images]") {
		t.Fatalf("the placeholder does not name the file:\n%s", text)
	}
	if !strings.Contains(text, "what is wrong with this?") {
		t.Fatalf("the person's own words were lost:\n%s", text)
	}
	if strings.Count(text, "— this model cannot see images]") != 1 {
		t.Fatalf("the picture was replaced more than once:\n%s", text)
	}

	// Idempotent: a second swap between blind models rewrites nothing, because
	// there is nothing left to rewrite.
	agent.SetModel("vendor/blind-two")
	if again := transcriptText(agent); again != text {
		t.Fatalf("a second swap rewrote the transcript:\n%s", again)
	}
}

// A model that CAN see is handed the conversation exactly as it stands.
func TestSetModelLeavesImagePartsAloneForAModelThatSees(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, withVision)

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	collect(t, mustSubmitImage(t, agent, ctx, "look", []Image{
		{Path: writeImage(t, workspace, "chart.png", "PHOTOBYTES")},
	}))

	agent.SetModel("vendor/also-sees")
	if count := countImageParts(agent); count != 1 {
		t.Fatalf("the transcript carries %d pictures, want the one that was attached", count)
	}
}

// THE JOURNAL IS THE RECORD and the scrub never touches it: the reference the
// file kept is what a resume rebuilds from.
func TestTheScrubLeavesTheJournalWhole(t *testing.T) {
	completer := &scriptedCompleter{}
	journal := writeableJournal(t)
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		withVision(config)
		config.SessionFile = journal
	})
	path := writeImage(t, workspace, "chart.png", "PHOTOBYTES")

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	collect(t, mustSubmitImage(t, agent, ctx, "what is this?", []Image{{Path: path}}))
	agent.SetModel("vendor/blind")
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	replayed, err := replaySessionFile(journal)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	pictures := 0
	for _, message := range replayed.messages {
		pictures += len(imagePartURLs(message))
	}
	if pictures != 1 {
		t.Fatalf("the journal replays %d pictures, want the one it recorded", pictures)
	}
}

// And the resume half of the same seam: a session reopened on a model that
// cannot see rebuilds the placeholder, never the bytes.
func TestResumingOnABlindModelScrubsTheReplayedPictures(t *testing.T) {
	journal := writeableJournal(t)
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		withVision(config)
		config.SessionFile = journal
	})
	path := writeImage(t, workspace, "chart.png", "PHOTOBYTES")

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	collect(t, mustSubmitImage(t, agent, ctx, "what is this?", []Image{{Path: path}}))
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	resumed, err := newAgent(Config{
		Workspace:      workspace,
		Model:          "vendor/blind",
		System:         "SYSTEM",
		SessionFile:    journal,
		SupportsImages: func(model string) bool { return model == "test/model" },
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	t.Cleanup(func() { _ = resumed.Close() })

	if count := countImageParts(resumed); count != 0 {
		t.Fatalf("%d image parts were replayed onto a blind model", count)
	}
	if text := transcriptText(resumed); !strings.Contains(text, "[image "+path+" — this model cannot see images]") {
		t.Fatalf("the resumed transcript does not name the picture:\n%s", text)
	}
}

// A picture the journal cannot name still loses its bytes — a memory-only
// session has no file to have written a path, and "no path" is not a reason to
// send base64 to a blind model.
func TestTheScrubDropsBytesEvenWithNoJournaledPath(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SupportsImages = func(model string) bool { return model == "test/model" }
	})
	agent.mu.Lock()
	agent.messages = append(agent.messages, ai.Message{Role: "user", Content: []ai.ContentPart{
		{Type: "text", Text: "look"},
		{Type: "image_url", ImageURL: &ai.ImageURLData{URL: "data:image/png;base64,QUJD"}},
	}})
	agent.mu.Unlock()

	agent.SetModel("vendor/blind")
	if count := countImageParts(agent); count != 0 {
		t.Fatalf("%d image parts survived", count)
	}
	if text := transcriptText(agent); !strings.Contains(text, "[image — this model cannot see images]") {
		t.Fatalf("the placeholder is missing:\n%s", text)
	}
}

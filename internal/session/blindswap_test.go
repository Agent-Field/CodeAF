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

// A model without eyes is SENT each picture's placeholder — once, naming the
// file, with the person's own words left alone — and the transcript keeps the
// picture.
//
// THE REQUEST IS WHERE THIS HAPPENS, not the transcript (blindswap.go states
// the whole design), so what the assertion reads is what the completer was
// handed.
func TestABlindModelIsSentThePlaceholderAndTheTranscriptKeepsThePicture(t *testing.T) {
	sent := make(chan []ai.Message, 2)
	answer := func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		sent <- append([]ai.Message(nil), messages...)
		return textResponse("read"), nil
	}
	completer := &scriptedCompleter{steps: []step{answer, answer, answer}}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.SeesImages = func(model string) (bool, bool) { return seesIf(model == "test/model") }
		config.SessionFile = writeableJournal(t)
	})
	path := writeImage(t, workspace, "chart.png", "PHOTOBYTES")

	ctx, cancel := deadline(10 * time.Second)
	defer cancel()
	collect(t, mustSubmitImage(t, agent, ctx, "what is wrong with this?", []Image{{Path: path}}))
	<-sent
	if countImageParts(agent) != 1 {
		t.Fatalf("the picture never reached the transcript")
	}

	agent.SetModel("vendor/blind")
	collect(t, mustSubmit(t, agent, "and now?"))
	messages := <-sent
	text := messagesText(messages)
	if !strings.Contains(text, "[image "+path+" — this model cannot see images]") {
		t.Fatalf("the placeholder the blind model was sent does not name the file:\n%s", text)
	}
	if !strings.Contains(text, "what is wrong with this?") {
		t.Fatalf("the person's own words were lost:\n%s", text)
	}
	if strings.Count(text, "— this model cannot see images]") != 1 {
		t.Fatalf("the picture was replaced more than once:\n%s", text)
	}
	pictures := 0
	for _, message := range messages {
		pictures += len(imagePartURLs(message))
	}
	if pictures != 0 {
		t.Fatalf("the blind model was sent %d pictures", pictures)
	}
	// AND THE BYTES ARE STILL THERE, which is what makes every one of these
	// answers reversible.
	if got := countImageParts(agent); got != 1 {
		t.Fatalf("the transcript holds %d pictures, want the one that was attached", got)
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
// cannot see KEEPS the replayed pictures and sends their placeholders.
//
// The rewrite used to happen once, at the door, against whatever model the agent
// happened to open with — so reopening on a blind model and then moving off it
// left a conversation whose pictures were gone for no reason anybody could see.
func TestResumingOnABlindModelKeepsThePicturesAndSendsPlaceholders(t *testing.T) {
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

	sent := make(chan []ai.Message, 2)
	answer := func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		sent <- append([]ai.Message(nil), messages...)
		return textResponse("read"), nil
	}
	resumed, err := newAgent(Config{
		Workspace:   workspace,
		Model:       "vendor/blind",
		System:      "SYSTEM",
		SessionFile: journal,
		SeesImages:  func(model string) (bool, bool) { return seesIf(model == "test/model") },
	}, &scriptedCompleter{steps: []step{answer, answer}})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	t.Cleanup(func() { _ = resumed.Close() })

	if count := countImageParts(resumed); count != 1 {
		t.Fatalf("the replay left %d pictures in the transcript, want the one the journal holds", count)
	}
	collect(t, mustSubmit(t, resumed, "and now?"))
	text := messagesText(<-sent)
	if !strings.Contains(text, "[image "+path+" — this model cannot see images]") {
		t.Fatalf("the blind model was not sent the placeholder:\n%s", text)
	}

	// AND MOVING ONTO A MODEL THAT SEES SHOWS THEM AGAIN.
	resumed.SetModel("test/model")
	collect(t, mustSubmit(t, resumed, "look again"))
	pictures := 0
	for _, message := range <-sent {
		pictures += len(imagePartURLs(message))
	}
	if pictures != 1 {
		t.Fatalf("the model that can see was sent %d pictures after the resume", pictures)
	}
}

// A picture the journal cannot name is still kept out of a blind model's
// request — a memory-only session has no file to have written a path, and "no
// path" is not a reason to send base64 to a model that cannot read it.
func TestAPictureWithNoJournaledPathIsStillHeldBack(t *testing.T) {
	sent := make(chan []ai.Message, 1)
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			sent <- append([]ai.Message(nil), messages...)
			return textResponse("blind"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.SeesImages = func(model string) (bool, bool) { return seesIf(model == "test/model") }
	})
	agent.mu.Lock()
	agent.messages = append(agent.messages, ai.Message{Role: "user", Content: []ai.ContentPart{
		{Type: "text", Text: "look"},
		{Type: "image_url", ImageURL: &ai.ImageURLData{URL: "data:image/png;base64,QUJD"}},
	}})
	agent.mu.Unlock()

	agent.SetModel("vendor/blind")
	collect(t, mustSubmit(t, agent, "and now?"))
	messages := <-sent
	pictures := 0
	for _, message := range messages {
		pictures += len(imagePartURLs(message))
	}
	if pictures != 0 {
		t.Fatalf("%d pictures went to the blind model", pictures)
	}
	if text := messagesText(messages); !strings.Contains(text, "[image — this model cannot see images]") {
		t.Fatalf("the placeholder is missing:\n%s", text)
	}
}

// seesIf is a fixture's yes-or-no as the oracle's pair ([Config.SeesImages]): a
// test that says a model sees or does not is a test that KNOWS, and the third
// answer — nobody has said — is written out where it is the subject.
func seesIf(sees bool) (bool, bool) { return sees, true }

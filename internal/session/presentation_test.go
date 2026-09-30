package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

func TestSteeredPresentationPreservesProviderContextAndHumanPartial(t *testing.T) {
	for _, cause := range []error{nil, errMarkCut} {
		for _, human := range []string{"", "[update] The first file is ready.", "[update] I am quoting `" + cutDroppedCallNote(nil) + "`."} {
			agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
			partial := &partialBuffer{}
			partial.write(human)
			agent.keepSteeredPartial(partial, &reasoningBuffer{}, true, nil, cutDroppedCallNote(cause))
			raw := messageContentText(agent.snapshot()[len(agent.snapshot())-1])
			if !strings.HasSuffix(raw, cutDroppedCallNote(cause)) || !strings.HasPrefix(raw, human) {
				t.Fatalf("provider content changed: %q", raw)
			}
			entries := agent.Transcript()
			got := entries[len(entries)-1]
			if human == "" {
				if got.Role != "aside" || got.Text != raw {
					t.Fatalf("bookkeeping is not retained as operational detail: %#v", got)
				}
			} else {
				want, _ := UserFacingUpdate(human)
				if got.Role != "assistant" || got.Text != want || got.Answer || !got.Addressed || !got.Interrupted {
					t.Fatalf("human partial changed: %#v", got)
				}
			}
		}
	}
}

func TestPresentationUsesOccurrenceIdentityAcrossJournalCompactionAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = path })
	raw := "A visible finding.\n\n" + cutDroppedCallNote(nil)
	marked := textMessage("assistant", raw)
	agent.recordPresentedAssistant(marked, provider.MessageReasoning{}, humanPresentation("A visible finding."))
	// Identical content from another response is still an ordinary human answer.
	agent.recordAssistant(marked, provider.MessageReasoning{})
	agent.recordAssistant(textMessage("assistant", cutDroppedCallNote(nil)), provider.MessageReasoning{})
	assert := func(es []DisplayEntry) {
		t.Helper()
		if len(es) != 3 || es[0].Text != "A visible finding." || es[1].Text != raw || es[2].Text != cutDroppedCallNote(nil) || es[2].Role != "assistant" {
			t.Fatalf("presentation crossed message identities: %#v", es)
		}
	}
	assert(agent.Transcript())
	window := append([]ai.Message(nil), agent.snapshot()[1:]...)
	// Compaction re-journals the same immutable messages, without content hashes.
	agent.file.appendCompaction(compactionPass{}, 0, window)
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assert(shapeEntries(replayed.messages, &sessionFile{presentation: replayed.presentation}))
	for i, message := range replayed.messages {
		if i < 2 && messageContentText(message) != raw {
			t.Fatal("reopen rewrote provider text")
		}
	}
}

func TestLegacyPresentationOnlyFoldsStandaloneReservedBookkeeping(t *testing.T) {
	for _, text := range []string{
		cutDroppedCallNote(nil), cutDroppedCallNote(errMarkCut),
		"Visible finding.\n\n" + cutDroppedCallNote(nil),
		"`" + cutDroppedCallNote(nil) + "`",
		"```\n" + cutDroppedCallNote(nil) + "\n```",
	} {
		entry := sessionEntry{Type: "message", Role: "assistant", Content: text}
		data, _ := json.Marshal(entry)
		path := filepath.Join(t.TempDir(), "legacy.jsonl")
		if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		replayed, err := replaySessionFile(path)
		if err != nil {
			t.Fatal(err)
		}
		entries := shapeEntries(replayed.messages, &sessionFile{presentation: replayed.presentation})
		want := "assistant"
		if text == cutDroppedCallNote(nil) || text == cutDroppedCallNote(errMarkCut) {
			want = "aside"
		}
		if len(entries) != 1 || entries[0].Role != want || entries[0].Text != text {
			t.Fatalf("legacy %q changed: %#v", text, entries)
		}
	}
}

func TestPresentationKeepsTaskReplyTagsForTheHumanAnswer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = path })
	note := userText("task 9 finished")
	note.authored = true
	note.replyTags = []TaskReplyTag{{ID: 9, Title: "Check report", Request: "Keep this exact request"}}
	agent.mu.Lock()
	agent.recordUserLocked(note)
	agent.mu.Unlock()
	agent.recordPresentedAssistant(textMessage("assistant", "internal handover"), provider.MessageReasoning{}, &messagePresentation{Audience: "operational"})
	agent.recordAssistant(textMessage("assistant", "The report passes."), provider.MessageReasoning{})
	assert := func(es []DisplayEntry) {
		t.Helper()
		if len(es) != 3 || es[1].Role != "aside" || len(es[1].ReplyTags) != 0 || len(es[2].ReplyTags) != 1 || es[2].ReplyTags[0].Request != "Keep this exact request" {
			t.Fatalf("operational message consumed source attribution: %#v", es)
		}
	}
	assert(agent.Transcript())
	agent.Close()
	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assert(shapeEntries(replayed.messages, &sessionFile{presentation: replayed.presentation, notes: replayed.notes, replyTags: replayed.replyTags}))
}

func TestPresentationSurvivesSteeringAndRewindWithoutCrossingOccurrences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = path })
	agent.record(textMessage("user", "Build the report"))
	partial := &partialBuffer{}
	partial.write("[update] The first file is ready.")
	agent.keepSteeredPartial(partial, &reasoningBuffer{}, true, nil, cutDroppedCallNote(nil))
	steer := userText("Use Decimal")
	steer.crossed = &SteerMark{Consumed: true, Landing: "stopped the reply here"}
	agent.mu.Lock()
	agent.recordUserLocked(steer)
	agent.mu.Unlock()
	agent.recordAssistant(textMessage("assistant", "The Decimal report is ready."), provider.MessageReasoning{})
	agent.record(textMessage("user", "Change the output again"))
	agent.recordPresentedAssistant(textMessage("assistant", "internal handover"), provider.MessageReasoning{}, &messagePresentation{Audience: "operational"})
	removed, err := agent.Rewind()
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 2 || removed[1].Role != "aside" {
		t.Fatalf("rewind projected removed bookkeeping as speech: %#v", removed)
	}
	assert := func(es []DisplayEntry) {
		t.Helper()
		if len(es) != 4 || es[1].Text != "The first file is ready." || es[1].Answer || !es[1].Addressed || !es[1].Interrupted || es[2].Text != "Use Decimal" || es[2].Steer == nil || es[3].Text != "The Decimal report is ready." {
			t.Fatalf("rewind lost update or steering: %#v", es)
		}
	}
	assert(agent.Transcript())
	agent.Close()
	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assert(shapeEntries(replayed.messages, &sessionFile{presentation: replayed.presentation, steers: replayed.steers}))
}

func TestHandoverPresentationKeepsHeldRemainderOperational(t *testing.T) {
	completer := &routeCompleter{handoff: custodyDraft}
	agent, _, _ := routeAgent(t, completer)
	agent.personAsk = routeAsk
	taken := Decision{Verb: DecideCarryOn, Brief: "finish the repository changes"}
	over := agent.handOverRunningTurn(context.Background(), newEventHub(), &Usage{}, time.Now(), agent.model,
		checkpointCeilingNote, checkpointSeamCeiling, 0, nil,
		routeVerdict{Work: true, Goal: routeAsk}, checkpointRead{ownRemainder: custodyHeldPart}, &taken)
	if !over.moved {
		t.Fatal("fixture did not hand over work")
	}
	raw := messageContentText(lastMessage(agent))
	if !strings.Contains(raw, checkpointOwnRemainderHead) {
		t.Fatal("provider lost held remainder")
	}
	es := agent.Transcript()
	if last := es[len(es)-1]; last.Role != "aside" || last.Text != raw {
		t.Fatalf("handover rendered as an answer: %#v", last)
	}
}

func TestInterruptedPresentationSurvivesReopenWithoutClaimingCompletion(t *testing.T) {
	for _, text := range []string{"[update] Partial human finding", "unfinished tool narration"} {
		path := filepath.Join(t.TempDir(), "session.jsonl")
		agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = path })
		agent.record(textMessage("user", "Check the report"))
		partial := &partialBuffer{}
		partial.write(text)
		agent.keepPartial(partial, nil)
		assert := func(es []DisplayEntry) {
			t.Helper()
			got := es[len(es)-1]
			visible, addressed := UserFacingUpdate(text)
			if !got.Interrupted || got.Answer || got.Addressed != addressed {
				t.Fatalf("interrupted response claimed completion or wrong audience: %#v", got)
			}
			if addressed && (got.Role != "assistant" || got.Text != visible) {
				t.Fatalf("lost human partial: %#v", got)
			}
			if !addressed && (got.Role != "aside" || got.Text != text) {
				t.Fatalf("promoted unfinished narration: %#v", got)
			}
		}
		assert(agent.Transcript())
		window := agent.snapshot()[1:]
		agent.file.appendCompaction(compactionPass{}, 0, window)
		agent.Close()
		replayed, err := replaySessionFile(path)
		if err != nil {
			t.Fatal(err)
		}
		assert(shapeEntries(replayed.messages, &sessionFile{presentation: replayed.presentation}))
	}
}

func TestInterruptedVisionPresentationRetainsHumanAnswerAndAttribution(t *testing.T) {
	for _, soup := range []bool{false, true} {
		name := "human answer"
		if soup {
			name = "degenerate response"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			text := "A red harbour is visible."
			if soup {
				text = strings.Repeat("</think>", 400)
			}
			completer := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				provider.Emit(ctx, provider.StreamDelta, text)
				cancel()
				return nil, ctx.Err()
			}}}
			path := filepath.Join(t.TempDir(), "session.jsonl")
			agent, workspace := newTestAgent(t, completer, func(c *Config) { blindWithVision(c); c.SessionFile = path })
			agent.SetModel("vendor/blind")
			photo := writeImage(t, workspace, "shot.png", "PHOTOBYTES")
			events, err := agent.SubmitImage(ctx, "Describe this image", []Image{{Path: photo}})
			if err != nil {
				t.Fatal(err)
			}
			collected := collect(t, events)
			for _, event := range collected {
				if event.Kind == EventTextDelta && !event.Addressed {
					t.Fatal("vision answer stream lost its declared audience")
				}
			}
			if soup {
				if lastMessage(agent).Role == "assistant" {
					t.Fatal("interrupted degenerate response was kept")
				}
				if _, ok := firstOfKind(collected, EventNotice); !ok {
					t.Fatal("discarded degenerate response was not explained")
				}
				return
			}
			assert := func(es []DisplayEntry) {
				t.Helper()
				got := es[len(es)-1]
				if got.Role != "assistant" || got.Text != "[vision: vendor/eyes]\n\n"+text || !got.Addressed || !got.Interrupted || got.Answer {
					t.Fatalf("vision partial changed audience or completion: %#v", got)
				}
			}
			assert(agent.Transcript())
			agent.Close()
			replayed, err := replaySessionFile(path)
			if err != nil {
				t.Fatal(err)
			}
			assert(shapeEntries(replayed.messages, &sessionFile{presentation: replayed.presentation}))
		})
	}
}

func TestPresentationHubDoesNotMergeDifferentAudiences(t *testing.T) {
	hub := newEventHub()
	hub.send(Event{Kind: EventTextDelta, Text: "internal "})
	hub.send(Event{Kind: EventTextDelta, Addressed: true, Text: "human "})
	hub.send(Event{Kind: EventTextDelta, Addressed: true, Text: "update"})
	stream, ok := hub.attach(nil)
	if !ok {
		t.Fatal("hub did not accept subscriber")
	}
	hub.close()
	events := collect(t, stream.out)
	if len(events) != 2 || events[0].Text != "internal " || events[0].Addressed || events[1].Text != "human update" || !events[1].Addressed {
		t.Fatalf("attach lost audience boundary: %#v", events)
	}
}

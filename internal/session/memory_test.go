package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The memory tests are written against BEHAVIOUR a person could observe: what
// reached the model's prompt, what is in the store afterwards, and — just as
// often — that no call was made at all.
//
// The reflex calls are told apart from the turn's own by their system prompt,
// which is how they really differ on the wire: the router, the extractor and
// the decider each open with a sentence internal/reflex wrote and nothing else
// in this package says. That is what lets one scripted completer answer a whole
// turn and its two reflexes in the order they actually happen.

type reflexScript struct {
	mu sync.Mutex

	route, extract, decide       string
	routeErr, extractErr, decErr error
	answer                       string

	routes, extracts, decides, turns int
	systems                          []string
}

func (r *reflexScript) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	system := ""
	if len(messages) > 0 {
		system = messageText(messages[0])
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.systems = append(r.systems, system)
	switch {
	case strings.Contains(system, "memory router"):
		r.routes++
		if r.routeErr != nil {
			return nil, r.routeErr
		}
		return textResponse(r.route), nil
	case strings.Contains(system, "worth remembering after this session ends"):
		r.extracts++
		if r.extractErr != nil {
			return nil, r.extractErr
		}
		return textResponse(r.extract), nil
	case strings.Contains(system, "one candidate memory and the lines already stored"):
		r.decides++
		if r.decErr != nil {
			return nil, r.decErr
		}
		return textResponse(r.decide), nil
	}
	r.turns++
	answer := r.answer
	if answer == "" {
		answer = "done"
	}
	return textResponse(answer), nil
}

func (r *reflexScript) counts() (routes, extracts, decides int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.routes, r.extracts, r.decides
}

// brainAgent is a session with a brain of its own, in a directory the test
// owns. The store is never the person's real one: a test that wrote into
// ~/.aforge would be a test that changes their next conversation.
func brainAgent(t *testing.T, completer Completer, mutate func(*Config)) (*Agent, *store.Store) {
	t.Helper()
	brain, err := store.Open(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = brain.Close() })
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Memory = brain
		if mutate != nil {
			mutate(config)
		}
	})
	return agent, brain
}

func remember(t *testing.T, brain *store.Store, title, text string) store.Memory {
	t.Helper()
	memory, err := brain.AddMemory(store.Memory{
		Type: store.MemoryFact, Scope: store.MemoryScopeUser, Title: title, Text: text,
	})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}
	return memory
}

func titles(memories []store.Memory) []string {
	names := make([]string, 0, len(memories))
	for _, memory := range memories {
		names = append(names, memory.Title)
	}
	return names
}

// ── the pre-turn block ──────────────────────────────────────────────────────

// AN EMPTY BRAIN IS NEVER A CALL. A fresh install remembers nothing, so there
// is nothing to route against, and a reflex that billed for that would bill for
// every turn of every first day.
func TestAnEmptyIndexIsNeverRouted(t *testing.T) {
	script := &reflexScript{}
	agent, _ := brainAgent(t, script, nil)

	if block := agent.memoryBlock(context.Background(), "how do I deploy this"); block != "" {
		t.Fatalf("an empty brain rendered %q", block)
	}
	if routes, _, _ := script.counts(); routes != 0 {
		t.Fatalf("the router was called %d times against an empty index", routes)
	}
}

// A continuation says nothing the index could be matched against. "yes" and "go
// on" are half the messages in a working conversation, and each one is a call
// nobody would get an answer out of.
func TestAContinuationIsNotRouted(t *testing.T) {
	script := &reflexScript{route: `{"inject":[],"cmd":null}`}
	agent, brain := brainAgent(t, script, nil)
	remember(t, brain, "prefers tabs", "prefers tabs over spaces in Go")

	if block := agent.memoryBlock(context.Background(), "yes"); block != "" {
		t.Fatalf("a two-word continuation rendered %q", block)
	}
	if routes, _, _ := script.counts(); routes != 0 {
		t.Fatalf("the router was called %d times on a continuation", routes)
	}

	// The exception, and the reason the rule is about words rather than length:
	// "forget that" is shorter than the floor and is the whole point of routing.
	if block := agent.memoryBlock(context.Background(), "forget that"); block != "" {
		t.Fatalf("the forget command rendered a block: %q", block)
	}
	if routes, _, _ := script.counts(); routes != 1 {
		t.Fatalf("a memory command was routed %d times, want once", routes)
	}
}

// What the router asks for is what the model reads — by title and text, in the
// router's own order, inside one <memory> block.
func TestTheRoutedMemoriesAreRenderedIntoTheBlock(t *testing.T) {
	script := &reflexScript{}
	agent, brain := brainAgent(t, script, nil)
	tabs := remember(t, brain, "prefers tabs", "prefers tabs over spaces in Go")
	remember(t, brain, "dark themes", "prefers dark themes everywhere")
	script.route = `{"inject":["` + tabs.ID + `"],"cmd":null}`

	block := agent.memoryBlock(context.Background(), "reformat this file for me")
	if !strings.Contains(block, "<memory>") || !strings.Contains(block, "</memory>") {
		t.Fatalf("the block is not a <memory> block:\n%s", block)
	}
	if !strings.Contains(block, "prefers tabs: prefers tabs over spaces in Go") {
		t.Fatalf("the routed memory is not in the block:\n%s", block)
	}
	if strings.Contains(block, "dark themes") {
		t.Fatalf("a memory the router did not ask for is in the block:\n%s", block)
	}
}

// AND IT REACHES THE ACTUAL REQUEST. The block is decided inside the turn — the
// router is a provider call and the prompt refresh runs under the session lock —
// so the thing worth asserting is what message[0] said when the turn went out.
func TestTheBlockIsInTheSystemPromptTheTurnRidesOn(t *testing.T) {
	script := &reflexScript{answer: "reformatted"}
	agent, brain := brainAgent(t, script, nil)
	tabs := remember(t, brain, "prefers tabs", "prefers tabs over spaces in Go")
	script.route = `{"inject":["` + tabs.ID + `"],"cmd":null}`
	script.extract = `{"mem":0}`

	collect(t, mustSubmit(t, agent, "reformat this file for me"))

	var found bool
	script.mu.Lock()
	for _, system := range script.systems {
		if strings.Contains(system, "SYSTEM") && strings.Contains(system, "prefers tabs over spaces in Go") {
			found = true
		}
	}
	script.mu.Unlock()
	if !found {
		t.Fatal("no request carried the routed memory in its system prompt")
	}
}

// A REFLEX FAILURE IS INVISIBLE. The router refused, and the turn is exactly
// the turn it would have been if this feature did not exist.
func TestARouterFailureLeavesTheTurnAlone(t *testing.T) {
	script := &reflexScript{routeErr: errors.New("provider is down"), answer: "here you go"}
	agent, brain := brainAgent(t, script, nil)
	remember(t, brain, "prefers tabs", "prefers tabs over spaces in Go")

	events := collect(t, mustSubmit(t, agent, "reformat this file for me"))
	var text strings.Builder
	for _, event := range events {
		if event.Kind == EventTextDelta {
			text.WriteString(event.Text)
		}
		if event.Kind == EventError {
			t.Fatalf("the turn failed: %v", event.Err)
		}
	}
	if agent.memoryText != "" {
		t.Fatalf("a failed router still rendered a block: %q", agent.memoryText)
	}
}

// The two instructions ABOUT memory, answered in one dim line each.
func TestTheRoutersRememberCommandWritesToTheStore(t *testing.T) {
	script := &reflexScript{route: `{"inject":[],"cmd":{"name":"remember","arg":"always deploys on Fridays"}}`}
	agent, brain := brainAgent(t, script, nil)
	remember(t, brain, "standup", "standup is at 9:15")

	hub := newEventHub()
	stream := hub.subscribe()
	var seen []string
	done := make(chan struct{})
	go func() {
		for event := range stream {
			if event.Kind == EventNotice {
				seen = append(seen, event.Text)
			}
		}
		close(done)
	}()
	agent.routedMemory(context.Background(), "remember that I always deploy on Fridays", hub, true)
	hub.close()
	<-done

	kept, err := brain.ListMemories("", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var landed *store.Memory
	for index, memory := range kept {
		if strings.Contains(memory.Text, "Fridays") {
			landed = &kept[index]
		}
	}
	if landed == nil {
		t.Fatalf("the command wrote nothing; the store holds %v", titles(kept))
	}
	// "always" is the whole heuristic: somebody saying how they want things done.
	if landed.Type != store.MemoryPreference {
		t.Errorf("the memory is a %q, want a preference", landed.Type)
	}
	if len(seen) != 1 || !strings.Contains(seen[0], "remembered") {
		t.Errorf("the confirmation was %v, want one line saying it was remembered", seen)
	}
}

func TestTheRoutersForgetCommandDropsTheMatchAndSaysWhich(t *testing.T) {
	script := &reflexScript{route: `{"inject":[],"cmd":{"name":"forget","arg":"standup"}}`}
	agent, brain := brainAgent(t, script, nil)
	remember(t, brain, "standup time", "standup is at 9:15")

	agent.routedMemory(context.Background(), "forget when standup is", nil, true)

	kept, err := brain.ListMemories("", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(kept) != 0 {
		t.Fatalf("the store still holds %v", titles(kept))
	}
}

// Nothing matched is an ANSWER, not a failure: the person said forget something
// and there was nothing to forget, and they are told so.
func TestForgettingWhatIsNotThereSaysSo(t *testing.T) {
	script := &reflexScript{route: `{"inject":[],"cmd":{"name":"forget","arg":"pineapples"}}`}
	agent, brain := brainAgent(t, script, nil)
	remember(t, brain, "standup time", "standup is at 9:15")

	hub := newEventHub()
	stream := hub.subscribe()
	var seen []string
	done := make(chan struct{})
	go func() {
		for event := range stream {
			if event.Kind == EventNotice {
				seen = append(seen, event.Text)
			}
		}
		close(done)
	}()
	agent.routedMemory(context.Background(), "forget everything about pineapples", hub, true)
	hub.close()
	<-done

	if len(seen) != 1 || !strings.Contains(seen[0], "nothing matched") {
		t.Fatalf("the answer was %v, want one line saying nothing matched", seen)
	}
	kept, _ := brain.ListMemories("", 10)
	if len(kept) != 1 {
		t.Fatalf("a no-match forget changed the store: %v", titles(kept))
	}
}

// MEMORY OFF IS ABSENT, NOT BROKEN. No store is no block, no call, and no verb.
func TestWithoutAStoreThereIsNoBlockNoCallAndNoTool(t *testing.T) {
	script := &reflexScript{}
	agent, _ := newTestAgent(t, script, nil)

	if block := agent.memoryBlock(context.Background(), "how do I deploy this"); block != "" {
		t.Fatalf("a session with no brain rendered %q", block)
	}
	if routes, extracts, decides := script.counts(); routes+extracts+decides != 0 {
		t.Fatalf("a session with no brain made %d/%d/%d reflex calls", routes, extracts, decides)
	}
	for _, tool := range agent.belt() {
		if tool.Name == "remember" {
			t.Fatal("remember is on the belt of a session that cannot remember")
		}
	}
	if _, err := agent.Remember("prefers tabs"); err == nil {
		t.Fatal("Remember succeeded on a session with no brain")
	}
}

func TestTheRememberToolIsOnTheBeltOfASessionWithABrain(t *testing.T) {
	agent, _ := brainAgent(t, &reflexScript{}, nil)
	var found bool
	for _, tool := range agent.belt() {
		if tool.Name == "remember" {
			found = true
		}
		if tool.Name == "note" || tool.Name == "forget" {
			t.Fatalf("the retired %s tool is still on the belt", tool.Name)
		}
	}
	if !found {
		t.Fatal("remember is not on the belt")
	}
}

// ── the post-turn pass ──────────────────────────────────────────────────────

// Nothing near it is not a question: the store has no opinion about this
// subject, so the candidate goes in as it stands and no decider is asked.
func TestAnExchangeWorthKeepingLandsWithNoDecisionToMake(t *testing.T) {
	script := &reflexScript{
		route:   `{"inject":[],"cmd":null}`,
		extract: `{"mem":1,"type":"preference","scope":"user","title":"deploys on Fridays","text":"deploys on Fridays","tags":[]}`,
	}
	agent, brain := brainAgent(t, script, nil)

	collect(t, mustSubmit(t, agent, "I always deploy on Fridays, remember that"))
	if err := agent.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	kept, err := brain.ListMemories("", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(kept) != 1 || kept[0].Text != "deploys on Fridays" {
		t.Fatalf("the store holds %v, want the extracted memory", titles(kept))
	}
	if _, _, decides := script.counts(); decides != 0 {
		t.Fatalf("the decider was asked %d times with nothing to decide against", decides)
	}
}

// And when there IS something near it, the decider is what settles it. The
// three operations are checked as one table because what is being tested is
// that each word reaches a different store call.
func TestANeighbourTurnsTheWriteIntoADecision(t *testing.T) {
	for _, probe := range []struct {
		name   string
		decide func(id string) string
		expect func(t *testing.T, kept []store.Memory)
	}{
		{
			name: "update refines the line that is already there",
			decide: func(id string) string {
				return `{"op":"update","target_id":"` + id + `","title":"standup time","text":"standup is at 9:30"}`
			},
			expect: func(t *testing.T, kept []store.Memory) {
				if len(kept) != 1 || kept[0].Text != "standup is at 9:30" {
					t.Fatalf("the store holds %d rows: %v", len(kept), titles(kept))
				}
			},
		},
		{
			name: "supersede retires it and admits the replacement",
			decide: func(id string) string {
				return `{"op":"supersede","target_id":"` + id + `","title":"standup time","text":"standup moved to 10:00"}`
			},
			expect: func(t *testing.T, kept []store.Memory) {
				if len(kept) != 1 || kept[0].Text != "standup moved to 10:00" {
					t.Fatalf("the store holds %d rows: %v", len(kept), titles(kept))
				}
			},
		},
		{
			name:   "skip writes nothing at all",
			decide: func(string) string { return `{"op":"skip"}` },
			expect: func(t *testing.T, kept []store.Memory) {
				if len(kept) != 1 || kept[0].Text != "standup is at 9:15" {
					t.Fatalf("a skip changed the store: %v", titles(kept))
				}
			},
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			script := &reflexScript{
				route:   `{"inject":[],"cmd":null}`,
				extract: `{"mem":1,"type":"fact","scope":"project","title":"standup time","text":"standup is at 9:30","tags":[]}`,
			}
			agent, brain := brainAgent(t, script, nil)
			existing := remember(t, brain, "standup time", "standup is at 9:15")
			script.mu.Lock()
			script.decide = probe.decide(existing.ID)
			script.mu.Unlock()

			collect(t, mustSubmit(t, agent, "standup has moved, note that down"))
			if err := agent.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}

			kept, err := brain.ListMemories("", 10)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			probe.expect(t, kept)
		})
	}
}

// mem 0 is the answer for most exchanges, and it costs nothing downstream.
func TestAnExchangeWithNothingInItWritesNothing(t *testing.T) {
	script := &reflexScript{route: `{"inject":[],"cmd":null}`, extract: `{"mem":0}`}
	agent, brain := brainAgent(t, script, nil)

	collect(t, mustSubmit(t, agent, "what is the capital of France"))
	_ = agent.Close()

	kept, _ := brain.ListMemories("", 10)
	if len(kept) != 0 {
		t.Fatalf("an exchange worth nothing wrote %v", titles(kept))
	}
	if _, _, decides := script.counts(); decides != 0 {
		t.Fatalf("the decider ran %d times after a mem 0", decides)
	}
}

func TestAFailedExtractionBreaksNothingAndWritesNothing(t *testing.T) {
	script := &reflexScript{route: `{"inject":[],"cmd":null}`, extractErr: errors.New("provider is down"), answer: "here you go"}
	agent, brain := brainAgent(t, script, nil)

	events := collect(t, mustSubmit(t, agent, "reformat this file for me"))
	for _, event := range events {
		if event.Kind == EventError {
			t.Fatalf("a failed extraction faulted the turn: %v", event.Err)
		}
	}
	_ = agent.Close()

	kept, _ := brain.ListMemories("", 10)
	if len(kept) != 0 {
		t.Fatalf("a failed extraction wrote %v", titles(kept))
	}
}

// Retrieval telemetry: a memory handed to a model was USED, whatever the
// extractor decides about the exchange afterwards.
func TestAnInjectedMemoryIsCountedAsUsed(t *testing.T) {
	script := &reflexScript{extract: `{"mem":0}`}
	agent, brain := brainAgent(t, script, nil)
	tabs := remember(t, brain, "prefers tabs", "prefers tabs over spaces in Go")
	script.route = `{"inject":["` + tabs.ID + `"],"cmd":null}`

	collect(t, mustSubmit(t, agent, "reformat this file for me"))
	_ = agent.Close()

	record, found, err := brain.MemoryRecord(tabs.ID)
	if err != nil || !found {
		t.Fatalf("read back: %v, found=%v", err, found)
	}
	if record.UseCount != 1 {
		t.Fatalf("the injected memory was used %d times, want 1", record.UseCount)
	}
}

// ── the three commands' engine ──────────────────────────────────────────────

func TestRememberForgetAndMemoriesAnswerByHand(t *testing.T) {
	script := &reflexScript{decide: `{"op":"add"}`}
	agent, brain := brainAgent(t, script, nil)

	title, err := agent.Remember("prefers tabs over spaces in Go")
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}
	if title == "" {
		t.Fatal("Remember answered with no title")
	}
	lines, err := agent.Memories("")
	if err != nil {
		t.Fatalf("Memories: %v", err)
	}
	if len(lines) != 1 || lines[0].Text != "prefers tabs over spaces in Go" {
		t.Fatalf("the list is %v", lines)
	}
	if lines[0].ID == "" {
		t.Fatal("a listed memory has no id to name it by")
	}

	// The search half: a query narrows the list.
	remember(t, brain, "standup time", "standup is at 9:15")
	found, err := agent.Memories("standup")
	if err != nil {
		t.Fatalf("Memories(query): %v", err)
	}
	if len(found) != 1 || !strings.Contains(found[0].Text, "standup") {
		t.Fatalf("the search answered %v", found)
	}

	dropped, err := agent.Forget("standup")
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if dropped != "standup time" {
		t.Fatalf("Forget dropped %q", dropped)
	}
	missed, err := agent.Forget("pineapples")
	if err != nil {
		t.Fatalf("Forget(no match): %v", err)
	}
	if missed != "" {
		t.Fatalf("Forget claimed to drop %q", missed)
	}
	if _, err := agent.Remember("   "); err == nil {
		t.Fatal("Remember accepted an empty line")
	}
}

// ── the legacy file ─────────────────────────────────────────────────────────

// A person's memory.md is CARRIED, not dropped, and it is carried exactly once.
func TestTheOldMemoryFileIsImportedOnceAndRenamed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.md")
	if err := os.WriteFile(path, []byte(
		"# my memory\n\n- prefers tabs over spaces in Go\n\n- deploys on Fridays\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	script := &reflexScript{}
	agent, brain := brainAgent(t, script, func(config *Config) { config.MemoryImport = path })

	agent.importMemoryFile(nil)

	kept, err := brain.ListMemories("", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(kept) != 2 {
		t.Fatalf("imported %d lines, want 2: %v", len(kept), titles(kept))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("memory.md is still there: %v", err)
	}
	if _, err := os.Stat(path + ".imported"); err != nil {
		t.Fatalf("the person's own copy was not kept: %v", err)
	}

	// A second session finds nothing to import and writes nothing.
	second, _ := newTestAgent(t, script, func(config *Config) {
		config.Memory = brain
		config.MemoryImport = path
	})
	second.importMemoryFile(nil)
	again, _ := brain.ListMemories("", 10)
	if len(again) != 2 {
		t.Fatalf("a second run imported again: %v", titles(again))
	}
}

// ── what a task node opens with ─────────────────────────────────────────────

// A node has no turn of its own to route against, so the conversation routes
// for it — against the brief — and hands down the WORDS.
func TestATaskNodeOpensWithTheMemoryItsBriefNeeded(t *testing.T) {
	script := &reflexScript{}
	agent, brain := brainAgent(t, script, nil)
	tabs := remember(t, brain, "prefers tabs", "prefers tabs over spaces in Go")
	script.route = `{"inject":["` + tabs.ID + `"],"cmd":null}`

	block := agent.memoryBlock(context.Background(), "reformat every Go file in internal/session")
	if !strings.Contains(block, "prefers tabs over spaces in Go") {
		t.Fatalf("the brief was routed to nothing:\n%s", block)
	}

	child, err := newAgent(Config{
		Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM", memoryBrief: block,
	}, script)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	child.mu.Lock()
	opening := messageText(child.messages[0])
	child.mu.Unlock()
	if !strings.Contains(opening, "prefers tabs over spaces in Go") {
		t.Fatalf("the node did not open with the block:\n%s", opening)
	}
	// AND IT KEEPS THEM. A node has no store to route against, so the per-turn
	// refresh has nothing to replace the block with — clearing it would take
	// away the one thing the node was given.
	collect(t, mustSubmit(t, child, "start on the first file"))
	child.mu.Lock()
	working := messageText(child.messages[0])
	child.mu.Unlock()
	if !strings.Contains(working, "prefers tabs over spaces in Go") {
		t.Fatalf("the node's first turn dropped the block:\n%s", working)
	}
	// AND IT DID NOT INHERIT THE COUNTING. The parent's telemetry is the
	// parent's; a node crediting its parent's memories with retrievals nobody
	// made would make the numbers a fiction.
	record, _, _ := brain.MemoryRecord(tabs.ID)
	if record.UseCount != 0 {
		t.Fatalf("the spawn seam counted %d retrievals", record.UseCount)
	}
}

// And a conversation with no brain spawns a node exactly as it always did.
func TestATaskNodeWithoutAStoreIsSkippedSilently(t *testing.T) {
	script := &reflexScript{}
	agent, _ := newTestAgent(t, script, nil)
	if block := agent.memoryBlock(context.Background(), "reformat every Go file"); block != "" {
		t.Fatalf("a brainless session routed a brief to %q", block)
	}
	if routes, _, _ := script.counts(); routes != 0 {
		t.Fatalf("a brainless session made %d router calls", routes)
	}
}

// ── the small judgements ────────────────────────────────────────────────────

func TestTheBlockDropsTheTailRatherThanTheHead(t *testing.T) {
	long := strings.Repeat("x", store.MemoryTextRunes)
	memories := []store.Memory{
		{ID: "a", Title: "first", Text: long},
		{ID: "b", Title: "second", Text: long},
		{ID: "c", Title: "third", Text: long},
		{ID: "d", Title: "fourth", Text: long},
		{ID: "e", Title: "fifth", Text: long},
		{ID: "f", Title: "sixth", Text: long},
		{ID: "g", Title: "seventh", Text: long},
		{ID: "h", Title: "eighth", Text: long},
		{ID: "i", Title: "ninth", Text: long},
		{ID: "j", Title: "tenth", Text: long},
	}
	block, kept := renderMemoryBlock(memories)
	if len(kept) == 0 || len(kept) == len(memories) {
		t.Fatalf("kept %d of %d — the cap did nothing", len(kept), len(memories))
	}
	if kept[0] != "a" {
		t.Fatalf("the first memory the router named was dropped; kept %v", kept)
	}
	if !strings.Contains(block, "first") || strings.Contains(block, "tenth") {
		t.Fatal("the block kept the tail rather than the head")
	}
}

func TestATitleIsTheFirstSixWords(t *testing.T) {
	if got := memoryTitleFrom("prefers tabs over spaces in Go always and forever"); got != "prefers tabs over spaces in Go" {
		t.Fatalf("title is %q", got)
	}
}

func TestAStatedPreferenceIsAPreferenceAndEverythingElseIsAFact(t *testing.T) {
	if got := memoryTypeOf("always deploys on Fridays"); got != store.MemoryPreference {
		t.Errorf("'always …' is a %q", got)
	}
	if got := memoryTypeOf("the repo is at github.com/example/thing"); got != store.MemoryFact {
		t.Errorf("a plain statement is a %q", got)
	}
}

// toolOutput is one named tool's result as the surface saw it. It reads the
// event rather than the transcript because a surface's copy is the one a person
// is shown, and the two must agree.
func toolOutput(t *testing.T, events []Event, tool string) string {
	t.Helper()
	for _, event := range events {
		if event.Tool != tool {
			continue
		}
		if event.Kind == EventToolEnd || event.Kind == EventToolFailed {
			return event.Output
		}
	}
	t.Fatalf("no result event for %s; events = %v", tool, kinds(events))
	return ""
}

// THE PERSON PAYS FOR THE REFLEX, SO THE BILL SAYS SO. It is the only auxiliary
// call made twice every turn, which makes it exactly the one that must not be
// invisible in /cost — and it is charged to the SESSION rather than to the turn,
// because no turn asked for it.
func TestTheReflexCallsAreOnTheSessionsBill(t *testing.T) {
	script := &reflexScript{extract: `{"mem":0}`}
	agent, brain := brainAgent(t, script, nil)
	tabs := remember(t, brain, "prefers tabs", "prefers tabs over spaces in Go")
	script.route = `{"inject":["` + tabs.ID + `"],"cmd":null}`

	events := collect(t, mustSubmit(t, agent, "reformat this file for me"))
	var turn Usage
	for _, event := range events {
		if event.Kind == EventTurnDone {
			turn = event.Usage
		}
	}
	_ = agent.Close()

	session := agent.Usage()
	if session.Input <= turn.Input {
		t.Fatalf("the session billed %d input tokens and the turn billed %d — the reflex calls are free", session.Input, turn.Input)
	}
	if turn.Turns != 1 {
		t.Fatalf("the reflex calls were counted as %d turns", turn.Turns)
	}
}

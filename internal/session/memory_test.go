package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/store"
)

// THE MEMORY LAWS, AS A CONVERSATION LIVES THEM. Every kept line rides
// message[0]; message[0] moves only when a line does; there is one write door
// with one scope default and receipts that say where a line holds; the model's
// forget never guesses; the pass after a turn may keep one line the person
// said, and says so with where to take it back; the person's switch governs
// that pass and nothing else; and a read-only posture reads and never writes.

// reflexScript answers a conversation's turns and the one call that follows
// each turn — the noticing call, which opens with a sentence internal/reflex
// wrote and nothing else in this package says.
type reflexScript struct {
	mu sync.Mutex
	// notice is the noticing call's answer, and noticeErr its failure.
	notice    string
	noticeErr error
	// hold, when set, keeps the noticing call's answer until it closes — the
	// way a test makes "the turn's stream has already closed" a fact rather
	// than a race.
	hold chan struct{}
	// answer is the turn's own reply.
	answer string
	// onTurn, when set, is called at each of the turn's own requests, before
	// it is answered and with no lock held.
	onTurn func()

	notices, turns int
	// asked records, per noticing call, whether it was asked about memory.
	asked []bool
	// shown is what each noticing call was shown.
	shown []string
	// systems is message[0] of every request the TURN made.
	systems []string
}

func (r *reflexScript) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	answerWhenQuiet(ctx, request)
	system := ""
	if len(messages) > 0 {
		system = messageText(messages[0])
	}
	if hold := r.hold; hold != nil && strings.HasPrefix(system, "You read one exchange between a person and an assistant") {
		<-hold
	}
	if r.onTurn != nil && !strings.HasPrefix(system, "You read one exchange between a person and an assistant") {
		r.onTurn()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.HasPrefix(system, "You read one exchange between a person and an assistant") {
		r.notices++
		r.asked = append(r.asked, strings.Contains(system, "FIRST, keep"))
		if len(messages) > 1 {
			r.shown = append(r.shown, messageText(messages[1]))
		}
		if r.noticeErr != nil {
			return nil, r.noticeErr
		}
		answer := r.notice
		if answer == "" {
			answer = `{"keep":""}`
		}
		return textResponse(answer), nil
	}
	r.turns++
	r.systems = append(r.systems, system)
	answer := r.answer
	if answer == "" {
		answer = "done"
	}
	return textResponse(answer), nil
}

func (r *reflexScript) counts() (notices, turns int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.notices, r.turns
}

func (r *reflexScript) system(turn int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.systems[turn]
}

// brainAgent is a conversation with a store of its own, in a directory the
// test owns — never the person's real one.
func brainAgent(t *testing.T, completer Completer, mutate func(*Config)) (*Agent, *store.Store) {
	t.Helper()
	brain, err := store.Open(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = brain.Close() })
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Memory = brain
		// A profile the test owns, so the noticing row is never read from the
		// machine's own configuration.
		config.ProfileDir = t.TempDir()
		if mutate != nil {
			mutate(config)
		}
	})
	return agent, brain
}

// remember puts one line in the store for the person, the seeder's way.
func remember(t *testing.T, brain *store.Store, title, text string) store.Memory {
	t.Helper()
	memory, err := brain.AddMemory(store.Memory{Scope: store.MemoryScopeUser, Title: title, Text: text})
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

// toolOutput is the result one tool call left in a turn's events.
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

// noticeLines is every memory line the person was shown in some events.
// allNotices is every notice a turn said.
func allNotices(events []Event) []string {
	var lines []string
	for _, event := range events {
		if event.Kind == EventNotice {
			lines = append(lines, event.Text)
		}
	}
	return lines
}

func noticeLines(events []Event) []string {
	var lines []string
	for _, event := range events {
		if event.Kind == EventNotice && (strings.Contains(event.Text, "remembered") || strings.Contains(event.Text, "forgot")) {
			lines = append(lines, event.Text)
		}
	}
	return lines
}

// settleMemory waits for the pass after a turn.
func settleMemory(agent *Agent) { agent.memoryJobs.Wait() }

func activeTexts(t *testing.T, brain *store.Store, owners ...string) []string {
	t.Helper()
	found, err := brain.MemoriesFor(owners)
	if err != nil {
		t.Fatal(err)
	}
	texts := make([]string, 0, len(found))
	for _, memory := range found {
		texts = append(texts, memory.Text)
	}
	return texts
}

// ── the block ───────────────────────────────────────────────────────────────

func TestEveryKeptLineRidesMessageZeroAndNoOtherProjectsLine(t *testing.T) {
	script := &reflexScript{}
	agent, brain := brainAgent(t, script, func(c *Config) { c.MemoryProjectKey = "alpha"; c.MemoryProjectName = "alpha-repo" })
	remember(t, brain, "", "prefers tabs over spaces")
	if _, err := brain.AddMemory(store.Memory{Owner: store.OwnerProject("alpha"), Text: "deploys on Tuesdays", ProjectName: "alpha-repo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := brain.AddMemory(store.Memory{Owner: store.OwnerProject("beta"), Text: "beta's own rule"}); err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, agent, "hello"))
	system := script.system(0)
	for _, want := range []string{"<memory>", memoryPreamble, "prefers tabs over spaces", "deploys on Tuesdays", "For this project (alpha-repo)"} {
		if !strings.Contains(system, want) {
			t.Fatalf("message[0] lacks %q:\n%s", want, system)
		}
	}
	if strings.Contains(system, "beta's own rule") {
		t.Fatal("another project's line reached this conversation")
	}
}

func TestMessageZeroHoldsStillUntilALineChanges(t *testing.T) {
	script := &reflexScript{}
	agent, brain := brainAgent(t, script, nil)
	remember(t, brain, "", "prefers tabs over spaces")
	collect(t, mustSubmit(t, agent, "one"))
	settleMemory(agent)
	collect(t, mustSubmit(t, agent, "two"))
	settleMemory(agent)
	if script.system(0) != script.system(1) {
		t.Fatalf("message[0] moved with nothing kept or forgotten:\n%s\n---\n%s", script.system(0), script.system(1))
	}
	if _, err := agent.Remember("the dev server answers on 8081"); err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, agent, "three"))
	if !strings.Contains(script.system(2), "the dev server answers on 8081") {
		t.Fatalf("a line kept between turns did not reach the next turn's message[0]:\n%s", script.system(2))
	}
}

func TestWithoutAStoreThereIsNoBlockNoNoticingAndNoVerbs(t *testing.T) {
	script := &reflexScript{}
	agent, _ := newTestAgent(t, script, nil)
	collect(t, mustSubmit(t, agent, "hello"))
	settleMemory(agent)
	if strings.Contains(script.system(0), "<memory>") {
		t.Fatal("a conversation with no store carried a memory block")
	}
	if notices, _ := script.counts(); notices != 0 {
		t.Fatalf("a conversation with no store made %d noticing calls", notices)
	}
	for _, tool := range agent.belt() {
		if tool.Name == "remember" || tool.Name == "forget" {
			t.Fatalf("a conversation with no store has the %s verb", tool.Name)
		}
	}
}

func TestAReadOnlyPostureReadsTheBlockAndNothingElse(t *testing.T) {
	script := &reflexScript{notice: `{"keep":"should never be kept","scope":"user"}`}
	agent, brain := brainAgent(t, script, func(c *Config) { c.bindingOnlyMemory = true })
	remember(t, brain, "", "prefers tabs over spaces")
	collect(t, mustSubmit(t, agent, "I always want tabs"))
	settleMemory(agent)
	if !strings.Contains(script.system(0), "prefers tabs over spaces") {
		t.Fatal("a read-only run did not carry the memory block")
	}
	if notices, _ := script.counts(); notices != 0 {
		t.Fatalf("a read-only run made %d noticing calls", notices)
	}
	for _, tool := range agent.belt() {
		if tool.Name == "remember" || tool.Name == "forget" {
			t.Fatalf("a read-only run has the %s verb", tool.Name)
		}
	}
	if texts := activeTexts(t, brain, store.OwnerUser); len(texts) != 1 {
		t.Fatalf("a read-only run changed memory: %v", texts)
	}
}

func TestALeanProfileCarriesTheBlockToo(t *testing.T) {
	script := &reflexScript{}
	agent, brain := brainAgent(t, script, func(c *Config) { c.profile = profileLean })
	remember(t, brain, "", "prefers tabs over spaces")
	if !agent.Remembers() {
		t.Fatal("a lean profile reported no memory, which is what made the page say memory was off")
	}
	collect(t, mustSubmit(t, agent, "hello"))
	if !strings.Contains(script.system(0), "prefers tabs over spaces") {
		t.Fatal("a lean profile dropped the person's lines")
	}
}

func TestTheBlockCarriesWhatTheStoreFits(t *testing.T) {
	line := strings.Repeat("x", 300)
	memories := make([]store.Memory, 30)
	for i := range memories {
		memories[i] = store.Memory{Owner: store.OwnerUser, Text: line, CreatedSeq: int64(i + 1)}
	}
	fit, left := store.FitMemories(memories)
	if left <= 0 || len(fit) == 0 {
		t.Fatalf("FitMemories kept %d and left %d, want some but not all of %d long lines", len(fit), left, len(memories))
	}
	block := renderMemory(memories, "")
	if got := strings.Count(block, line); got != len(fit) {
		t.Fatalf("the block carried %d lines and the store fit %d", got, len(fit))
	}
	if !strings.Contains(block, "more kept lines did not fit here") {
		t.Fatal("an overflowing block did not say lines were left out")
	}
}

// ── the write door ──────────────────────────────────────────────────────────

func TestRememberKeepsForThisProjectAndWidensWithoutOne(t *testing.T) {
	agent, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.MemoryProjectKey = "alpha"; c.MemoryProjectName = "alpha-repo" })
	receipt, err := agent.Remember("run make pr-ready first")
	if err != nil {
		t.Fatal(err)
	}
	if receipt != "remembered · run make pr-ready first · for this project" {
		t.Fatalf("receipt = %q", receipt)
	}
	got, _ := brain.MemoriesFor([]string{store.OwnerProject("alpha")})
	if len(got) != 1 || got[0].Origin != store.OriginPerson || got[0].ProjectName != "alpha-repo" {
		t.Fatalf("kept = %+v, want one person-kept line under the project's name", got)
	}
	receipt, err = agent.RememberAlways("no emoji anywhere", true)
	if err != nil || receipt != "remembered · no emoji anywhere · for you everywhere" {
		t.Fatalf("typed on home = %q %v, want it kept for the person everywhere", receipt, err)
	}
	again, err := agent.Remember("run make pr-ready first")
	if err != nil || again != "already remembered · run make pr-ready first · for this project" {
		t.Fatalf("the same words again = %q %v", again, err)
	}

	homeless, _ := brainAgent(t, &reflexScript{}, nil)
	receipt, err = homeless.Remember("prefers tabs")
	if err != nil || !strings.Contains(receipt, "since this conversation has no project of its own") {
		t.Fatalf("a conversation with no project = %q %v, want the widening said", receipt, err)
	}
	if project, _ := homeless.MemoryProject(); project != "" {
		t.Fatalf("a conversation with no project names %q as its project", project)
	}
}

func TestAFullBlockRefusesASaveAndSaysHow(t *testing.T) {
	agent, _ := brainAgent(t, &reflexScript{}, nil)
	line := strings.Repeat("word ", 90)
	for i := 0; ; i++ {
		_, err := agent.Remember(line + string(rune('a'+i%26)) + string(rune('a'+i/26)))
		if errors.Is(err, store.ErrMemoryFull) {
			if !strings.Contains(err.Error(), "Memories tab") {
				t.Fatalf("the refusal does not say how to make room: %v", err)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if i > 200 {
			t.Fatal("the block never filled")
		}
	}
}

func TestEveryDoorRedacts(t *testing.T) {
	agent, brain := brainAgent(t, &reflexScript{}, nil)
	if _, err := agent.Remember("the deploy token is xoxb-1234567890abcdef"); err != nil {
		t.Fatal(err)
	}
	for _, text := range activeTexts(t, brain, store.OwnerUser) {
		if strings.Contains(text, "xoxb-1234567890abcdef") {
			t.Fatalf("a credential was kept: %q", text)
		}
	}
}

// ── forgetting ──────────────────────────────────────────────────────────────

func TestForgetNamesOneLineOrListsTheMatchesAndNeverGuesses(t *testing.T) {
	agent, brain := brainAgent(t, &reflexScript{}, nil)
	for _, text := range []string{"deploys on Fridays", "deploy keys rotate monthly", "prefers tabs"} {
		remember(t, brain, "", text)
	}
	receipt, err := agent.forgetLine("deploy")
	if err != nil || !strings.Contains(receipt, "nothing was forgotten") || !strings.Contains(receipt, "deploys on Fridays") || !strings.Contains(receipt, "deploy keys rotate monthly") {
		t.Fatalf("an ambiguous forget = %q %v, want the matches listed and nothing forgotten", receipt, err)
	}
	if got := activeTexts(t, brain, store.OwnerUser); len(got) != 3 {
		t.Fatalf("an ambiguous forget forgot something: %v", got)
	}
	receipt, err = agent.forgetLine("the")
	if err != nil || !strings.HasPrefix(receipt, "nothing remembered matches") {
		t.Fatalf("a word in no line = %q %v", receipt, err)
	}
	receipt, err = agent.forgetLine("deploy keys")
	if err != nil || receipt != "forgot · deploy keys rotate monthly" {
		t.Fatalf("a unique forget = %q %v", receipt, err)
	}
}

// ── noticing ────────────────────────────────────────────────────────────────

func TestANoticedLineIsKeptAndSaysWhereToTakeItBack(t *testing.T) {
	script := &reflexScript{notice: `{"keep":"wants the change made directly","scope":"user"}`}
	agent, brain := brainAgent(t, script, nil)
	collect(t, mustSubmit(t, agent, "stop explaining, just make the change — everywhere"))
	settleMemory(agent)
	kept, _ := brain.MemoriesFor([]string{store.OwnerUser})
	if len(kept) != 1 || kept[0].Origin != store.OriginNoticed {
		t.Fatalf("kept = %+v, want one noticed line", kept)
	}
	events := collect(t, mustSubmit(t, agent, "thanks"))
	lines := noticeLines(events)
	want := "noticed and remembered · wants the change made directly · for you everywhere · " + memoryTakeBack
	if len(lines) == 0 || lines[0] != want {
		t.Fatalf("the person was told %q, want %q", lines, want)
	}
}

func TestANoticedReplacementReplacesAndTheOldLineCanComeBack(t *testing.T) {
	script := &reflexScript{notice: `{"keep":"deploys on Tuesdays","scope":"user","replaces":1}`}
	agent, brain := brainAgent(t, script, nil)
	remember(t, brain, "", "deploys on Fridays")
	collect(t, mustSubmit(t, agent, "actually we deploy on Tuesdays now"))
	settleMemory(agent)
	if got := activeTexts(t, brain, store.OwnerUser); len(got) != 1 || got[0] != "deploys on Tuesdays" {
		t.Fatalf("after the replacement = %v", got)
	}
	if !strings.Contains(script.shown[0], "1. deploys on Fridays") {
		t.Fatalf("the noticing call was not shown what is kept:\n%s", script.shown[0])
	}
	// THE OLD LINE IS LET GO, NOT DELETED: the Memories tab's put-back is the
	// store's restore, and it brings the old line back beside the new one.
	shelves, err := brain.MemorySnapshot(0)
	if err != nil {
		t.Fatal(err)
	}
	var old store.Memory
	for _, memory := range shelves.LetGo {
		if memory.Text == "deploys on Fridays" {
			old = memory
		}
	}
	if old.Status != store.MemorySuperseded {
		t.Fatalf("the replaced line is %q, want it let go as replaced", old.Status)
	}
	if err := brain.RestoreMemory(old.ID); err != nil {
		t.Fatal(err)
	}
	if got := activeTexts(t, brain, store.OwnerUser); len(got) != 2 {
		t.Fatalf("after putting the old line back = %v", got)
	}
}

// THE SWITCH GOVERNS NOTICING AND NOTHING ELSE, and it is read live.
func TestTheSwitchTurnsNoticingOffAndLeavesEverythingElse(t *testing.T) {
	script := &reflexScript{notice: `{"keep":"should not be kept","scope":"user","state":{"goal":"tidy the build"}}`}
	agent, brain := brainAgent(t, script, nil)
	remember(t, brain, "", "prefers tabs over spaces")
	writeProfileRow(t, agent.config.ProfileDir, config.KeyMemoryEnabled, config.MemoryOff)
	collect(t, mustSubmit(t, agent, "I always want tabs"))
	settleMemory(agent)
	if !strings.Contains(script.system(0), "prefers tabs over spaces") {
		t.Fatal("with noticing off the person's lines stopped riding the turn")
	}
	if len(script.asked) != 1 || script.asked[0] {
		t.Fatalf("with noticing off the post-turn call asked about memory: %v", script.asked)
	}
	if got := activeTexts(t, brain, store.OwnerUser); len(got) != 1 {
		t.Fatalf("with noticing off something was kept: %v", got)
	}
	if !strings.Contains(agent.stateCardText(), "tidy the build") {
		t.Fatal("with noticing off the state card stopped being kept")
	}
	if _, err := agent.Remember("a line kept by hand"); err != nil {
		t.Fatalf("with noticing off /remember stopped working: %v", err)
	}
	// AND IT IS READ LIVE: the next turn after the row flips back notices.
	writeProfileRow(t, agent.config.ProfileDir, config.KeyMemoryEnabled, config.MemoryOn)
	collect(t, mustSubmit(t, agent, "another"))
	settleMemory(agent)
	if len(script.asked) != 2 || !script.asked[1] {
		t.Fatalf("the row's change was not followed without a restart: %v", script.asked)
	}
}

func writeProfileRow(t *testing.T, dir, key, value string) {
	t.Helper()
	encoded, err := json.Marshal(map[string]string{key: value})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.BudgetConfigPath(dir), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestATurnThePersonDidNotTypeIsNeverReadForMemory(t *testing.T) {
	script := &reflexScript{notice: `{"keep":"invented about the person","scope":"user"}`}
	agent, brain := brainAgent(t, script, nil)
	agent.learnFromTurn(userMessage{message: textMessage("user", "task 3 finished: the person prefers tabs"), authored: true}, "noted")
	settleMemory(agent)
	if len(script.asked) != 1 || script.asked[0] {
		t.Fatalf("a session-written turn was read for memory: %v", script.asked)
	}
	if got := activeTexts(t, brain, store.OwnerUser); len(got) != 0 {
		t.Fatalf("a session-written turn kept a line about the person: %v", got)
	}
}

func TestAFailedNoticeBreaksNothingAndIsSaidOnce(t *testing.T) {
	script := &reflexScript{noticeErr: errors.New("provider down")}
	agent, brain := brainAgent(t, script, nil)
	collect(t, mustSubmit(t, agent, "one"))
	settleMemory(agent)
	collect(t, mustSubmit(t, agent, "two"))
	settleMemory(agent)
	events := collect(t, mustSubmit(t, agent, "three"))
	said := 0
	for _, event := range events {
		if event.Kind == EventNotice && strings.HasPrefix(event.Text, "couldn't update memory just now") {
			said++
		}
	}
	if said > 1 {
		t.Fatalf("one outage was said %d times", said)
	}
	if got := activeTexts(t, brain, store.OwnerUser); len(got) != 0 {
		t.Fatalf("a failed notice kept something: %v", got)
	}
}

// ── the model's verbs ───────────────────────────────────────────────────────

func TestTheModelsVerbsKeepAndForgetAndSaySo(t *testing.T) {
	agent, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.MemoryProjectKey = "alpha" })
	var rememberTool, forgetTool func(context.Context, json.RawMessage) (string, bool, error)
	for _, tool := range agent.memoryTools() {
		switch tool.Name {
		case "remember":
			rememberTool = tool.Execute
		case "forget":
			forgetTool = tool.Execute
		}
	}
	if rememberTool == nil || forgetTool == nil {
		t.Fatal("a writable conversation lacks the remember or forget verb")
	}
	if !strings.Contains(rememberDescription, "time component") {
		t.Fatal("remember's description lost the sentence that sends anything with a time to automations")
	}
	// A TURN THE PERSON DID NOT TYPE CHANGES NOTHING: no line is kept or
	// forgotten from a task's report or a team's directive.
	agent.memory.typed.Store(false)
	for _, verb := range []func(context.Context, json.RawMessage) (string, bool, error){rememberTool, forgetTool} {
		if out, isErr, _ := verb(context.Background(), json.RawMessage(`{"text":"uses pnpm, not npm"}`)); !isErr || out != memoryNotTypedWords {
			t.Fatalf("a verb on a turn the person did not type = %q %v", out, isErr)
		}
	}
	if got, _ := brain.MemoriesFor([]string{store.OwnerProject("alpha")}); len(got) != 0 {
		t.Fatalf("a turn the person did not type kept %+v", got)
	}
	agent.memory.typed.Store(true)
	out, isErr, err := rememberTool(context.Background(), json.RawMessage(`{"text":"uses pnpm, not npm"}`))
	if err != nil || isErr || out != "remembered for this project: uses pnpm, not npm" {
		t.Fatalf("remember = %q %v %v", out, isErr, err)
	}
	got, _ := brain.MemoriesFor([]string{store.OwnerProject("alpha")})
	if len(got) != 1 || got[0].Origin != store.OriginModel {
		t.Fatalf("kept = %+v, want one model-kept project line", got)
	}
	out, isErr, err = forgetTool(context.Background(), json.RawMessage(`{"text":"pnpm"}`))
	if err != nil || isErr || out != "forgot · uses pnpm, not npm" {
		t.Fatalf("forget = %q %v %v", out, isErr, err)
	}
	agent.memory.mu.Lock()
	held := append([]string(nil), agent.memory.said...)
	agent.memory.mu.Unlock()
	if len(held) != 2 || !strings.Contains(held[0], memoryTakeBack) || !strings.Contains(held[1], "put it back on the Memories tab") {
		t.Fatalf("the person was not told what the model did: %q", held)
	}
}

// A TURN THE PERSON TYPED IS WHAT ARMS THE VERBS, and a turn the session
// wrote for itself disarms them, through the real turn loop.
//
// The turns here are real: a task's landing wakes the idle conversation the way
// it does in use ([Agent.enqueueSteering]), and the person types into that woken
// turn while it is running.
func TestTheVerbsAreArmedOnlyByATurnThePersonTyped(t *testing.T) {
	script := &reflexScript{}
	agent, _ := brainAgent(t, script, nil)
	var mu sync.Mutex
	var armed []bool
	steered := false
	script.onTurn = func() {
		mu.Lock()
		armed = append(armed, agent.memory.typed.Load())
		steer := len(armed) == 2 && !steered
		steered = steered || steer
		mu.Unlock()
		if steer {
			// THE PERSON TYPES INTO THE WOKEN TURN while it is running.
			events, err := agent.Submit(context.Background(), "and keep it short")
			if err != nil {
				t.Errorf("steer the woken turn: %v", err)
				return
			}
			go func() {
				for range events { //nolint:revive // draining is the point
				}
			}()
		}
	}
	collect(t, mustSubmit(t, agent, "hello"))
	agent.enqueueSteering("task 1 finished: the build is green")
	waitFor(t, "the woken turn and the turn answering the steer", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(armed) >= 3
	})
	waitIdle(t, agent)
	mu.Lock()
	defer mu.Unlock()
	if !armed[0] {
		t.Fatal("a turn the person typed did not arm the memory verbs")
	}
	if armed[1] {
		t.Fatal("a turn a task's landing woke armed the memory verbs")
	}
	if !armed[len(armed)-1] {
		t.Fatalf("the line the person typed into the woken turn did not arm the verbs: %v", armed)
	}
}

// A LINE THE PERSON TYPED, DRAINED MID-TURN, ARMS THE REST OF THAT TURN and
// nothing after it: the next turn a task's landing wakes is not armed by a line
// the turn before already answered.
func TestASteerArmsOnlyTheTurnItLandsIn(t *testing.T) {
	agent, _ := brainAgent(t, &reflexScript{}, nil)
	agent.armMemoryVerbs(wakeNote("task 1 finished: the build is green"))
	agent.mu.Lock()
	agent.steering = append(agent.steering, userText("and keep it short"))
	agent.drainQueuedLocked(nil, false)
	agent.mu.Unlock()
	if !agent.memory.typed.Load() {
		t.Fatal("a line the person typed into a woken turn did not arm the verbs")
	}
	agent.endMemoryVerbs()
	agent.armMemoryVerbs(userMessage{wake: true})
	if agent.memory.typed.Load() {
		t.Fatal("a line its own turn answered armed the next woken turn")
	}
}

// A LINE NOTICED AFTER THE LAST TURN IS SAID WITHOUT WAITING FOR ANOTHER: a
// surface holds the standing lane for the life of the session, and the line
// goes there when the turn's own stream has closed.
func TestANoticedLineIsSaidOnTheStandingLane(t *testing.T) {
	script := &reflexScript{notice: `{"keep":"wants the change made directly","scope":"user"}`, hold: make(chan struct{})}
	agent, _ := brainAgent(t, script, nil)
	lane, stop := agent.WatchTaskUpdates()
	defer stop()
	collect(t, mustSubmit(t, agent, "stop explaining, just make the change"))
	// THE TURN'S STREAM HAS CLOSED; only now does the noticing call answer.
	close(script.hold)
	settleMemory(agent)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-lane:
			if event.Kind == EventNotice && strings.HasPrefix(event.Text, "noticed and remembered · wants the change made directly") {
				return
			}
		case <-deadline:
			t.Fatal("the noticed line never reached the standing lane")
		}
	}
}

// AN ASK-HERE ERRAND IS NOT NOTICED.
func TestAnErrandIsNotNoticed(t *testing.T) {
	script := &reflexScript{notice: `{"keep":"wants the change made directly","scope":"user"}`}
	agent, brain := brainAgent(t, script, func(c *Config) { c.Errand = true })
	collect(t, mustSubmit(t, agent, "stop explaining, just make the change"))
	settleMemory(agent)
	if got, _ := brain.MemoriesFor([]string{store.OwnerUser}); len(got) != 0 {
		t.Fatalf("an errand kept %+v", got)
	}
}

// A BLOCK AN EARLIER BUILD FILLED PAST THE BOUND IS SAID ONCE.
func TestAnOverflowingBlockIsSaidOnce(t *testing.T) {
	agent, brain := brainAgent(t, &reflexScript{}, nil)
	for i := 0; i < 70; i++ {
		if _, err := brain.AddMemory(store.Memory{Owner: store.OwnerUser, Type: store.MemoryFact, Text: fmt.Sprintf("line %02d %s", i, strings.Repeat("w", 80))}); err != nil {
			t.Fatal(err)
		}
	}
	first := noticeLines(collect(t, mustSubmit(t, agent, "hello")))
	said := 0
	for _, line := range first {
		if strings.Contains(line, "don't fit in front of the model here") {
			said++
		}
	}
	if said != 1 {
		t.Fatalf("the first turn said the overflow %d times: %q", said, first)
	}
	for _, line := range noticeLines(collect(t, mustSubmit(t, agent, "again"))) {
		if strings.Contains(line, "don't fit") {
			t.Fatalf("the overflow was said again: %q", line)
		}
	}
}

// A PERSON WHO HAD MEMORY OFF BEFORE THE UPGRADE IS TOLD ONCE PER MACHINE that
// their kept lines ride again — and nothing about the lines changes.
func TestAPersonWhoHadMemoryOffIsToldOncePerMachine(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	profile := t.TempDir()
	writeProfileRow(t, profile, config.KeyMemoryEnabled, config.MemoryOff)
	// The line is in the store before the conversation opens, as it is on a
	// machine upgrading from an earlier build.
	agent, brain := brainAgent(t, &reflexScript{}, func(c *Config) {
		c.ProfileDir = profile
		remember(t, c.Memory, "", "prefers tabs")
	})
	said := 0
	for _, line := range allNotices(collect(t, mustSubmit(t, agent, "hello"))) {
		if strings.HasPrefix(line, "1 kept memory line now rides every conversation it holds in") {
			said++
		}
	}
	if said != 1 {
		t.Fatal("a person who had memory off was not told their line rides again")
	}
	if _, err := os.Stat(memoryUpgradeMarker()); err != nil {
		t.Fatalf("the notice was said and not written down: %v", err)
	}
	if got := activeTexts(t, brain, store.OwnerUser); len(got) != 1 {
		t.Fatalf("the notice changed the lines: %v", got)
	}
	// A SECOND CONVERSATION ON THE SAME MACHINE IS NOT TOLD AGAIN.
	again, _ := brainAgent(t, &reflexScript{}, func(c *Config) { c.Memory = brain; c.ProfileDir = agent.config.ProfileDir })
	for _, line := range allNotices(collect(t, mustSubmit(t, again, "hello"))) {
		if strings.Contains(line, "now ride") || strings.Contains(line, "now rides") {
			t.Fatalf("a second conversation was told again: %q", line)
		}
	}
}

// AND A PERSON WHO HAD MEMORY ON AT THE FIRST START IS OWED NOTHING, then or
// after: turning noticing off later is not "had memory off before".
func TestAFirstStartWithMemoryOnClosesTheNotice(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	agent, brain := brainAgent(t, &reflexScript{}, nil)
	remember(t, brain, "", "prefers tabs")
	collect(t, mustSubmit(t, agent, "hello"))
	writeProfileRow(t, agent.config.ProfileDir, config.KeyMemoryEnabled, config.MemoryOff)
	later, _ := brainAgent(t, &reflexScript{}, func(c *Config) { c.Memory = brain; c.ProfileDir = agent.config.ProfileDir })
	for _, line := range allNotices(collect(t, mustSubmit(t, later, "hello"))) {
		if strings.Contains(line, "kept memory line") {
			t.Fatalf("turning noticing off after the first start told the person anyway: %q", line)
		}
	}
}

// ── where a line holds ──────────────────────────────────────────────────────

func TestTwoProjectsNeverSeeEachOthersLines(t *testing.T) {
	brainPath := filepath.Join(t.TempDir(), "shared.db")
	brain, err := store.Open(brainPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })
	alpha, _ := newTestAgent(t, &reflexScript{}, func(c *Config) { c.Memory = brain; c.MemoryProjectKey = "alpha" })
	beta, _ := newTestAgent(t, &reflexScript{}, func(c *Config) { c.Memory = brain; c.MemoryProjectKey = "beta" })
	if _, err := alpha.Remember("alpha deploys on Fridays"); err != nil {
		t.Fatal(err)
	}
	lines, err := beta.Memories("")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Fatalf("beta sees alpha's line: %+v", lines)
	}
	if receipt, _ := beta.forgetLine("Fridays"); !strings.HasPrefix(receipt, "nothing remembered matches") {
		t.Fatalf("beta could forget alpha's line: %q", receipt)
	}
}

func TestMovingALineIsResolvedByTheConversation(t *testing.T) {
	agent, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.MemoryProjectKey = "alpha"; c.MemoryProjectName = "alpha-repo" })
	line := remember(t, brain, "", "run make pr-ready first")
	receipt, err := agent.MoveMemory(line.ID, store.MemoryScopeProject)
	if err != nil || receipt != "now for this project" {
		t.Fatalf("move = %q %v", receipt, err)
	}
	got, _, _ := brain.MemoryRecord(line.ID)
	if got.Owner != store.OwnerProject("alpha") || got.ProjectName != "alpha-repo" {
		t.Fatalf("moved = %+v", got)
	}
	if project, _ := agent.MemoryProject(); project != store.OwnerProject("alpha") {
		t.Fatalf("MemoryProject = %q", project)
	}
}

func TestAWorkersBriefClosesOnTheSameSection(t *testing.T) {
	agent, brain := brainAgent(t, &reflexScript{}, nil)
	remember(t, brain, "", "prefers tabs over spaces")
	section := agent.memoryWorld()
	if !strings.Contains(section, "prefers tabs over spaces") || !strings.Contains(section, memoryReport) {
		t.Fatalf("a worker's brief section = %q", section)
	}
	if MemoryWorld(nil, MemoryOwners("")) != "" {
		t.Fatal("no store rendered a section")
	}
}

// ── the legacy file ─────────────────────────────────────────────────────────

func TestTheOldMemoryFileIsImportedOnceAndRenamed(t *testing.T) {
	legacy := filepath.Join(t.TempDir(), "memory.md")
	if err := os.WriteFile(legacy, []byte("# Memory\n\n- prefers tabs\n- uses pnpm\n<!-- note -->\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	agent, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.MemoryImport = legacy })
	collect(t, mustSubmit(t, agent, "hello"))
	settleMemory(agent)
	got := activeTexts(t, brain, store.OwnerUser)
	if len(got) != 2 {
		t.Fatalf("imported = %v, want the two lines", got)
	}
	if _, err := os.Stat(legacy + ".imported"); err != nil {
		t.Fatalf("the old file was not moved out of the way: %v", err)
	}
}

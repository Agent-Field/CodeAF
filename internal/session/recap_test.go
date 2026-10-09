package session

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/roles"
)

func isRecapCall(messages []ai.Message) bool {
	return len(messages) > 0 && messages[0].Role == "system" && messageContentText(messages[0]) == recapSystem
}

const goodRecap = `{"line":"Decided to keep strict mode as the default and fix it in the lexer","discussed":"We compared strict and lenient parsing. The lexer is where the leniency leaked in.","decided":[{"text":"Keep strict mode the default","by":"you","how":"accepted"}],"outcome":"The lexer fix is the plan."}`

// recapAgent is an agent that keeps recaps, with a recap-aware script: every
// recap ask is answered off the queue by its shape and counted, so the turn's
// own steps are never taken by the errand.
func recapAgent(t *testing.T, steps []step, answer string) (*Agent, string, *atomic.Int32) {
	t.Helper()
	completer, asks := recapCompleter(steps, answer)
	agent, dir := recapAgentOn(t, completer)
	return agent, dir, asks
}

func recapCompleter(steps []step, answer string) (*scriptedCompleter, *atomic.Int32) {
	asks := &atomic.Int32{}
	completer := &scriptedCompleter{steps: steps}
	completer.aside = func(messages []ai.Message) (*ai.Response, bool) {
		// The namer is answered promptly so that joining the session's errands
		// does not wait out an unscripted namer's retry ladder.
		if isTitleCall(messages) {
			return textResponse("a conversation"), true
		}
		if !isRecapCall(messages) {
			return nil, false
		}
		asks.Add(1)
		return textResponse(answer), true
	}
	return completer, asks
}

func recapAgentOn(t *testing.T, completer *scriptedCompleter) (*Agent, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	agent, _ := newTestAgent(t, completer, func(c *Config) {
		c.SessionFile = filepath.Join(dir, "transcript.jsonl")
		c.Place = Place{Dir: dir, Workspace: c.Workspace}
		c.Recaps = true
	})
	return agent, dir
}

func awaitRecap(t *testing.T, dir string, want func(*ConversationRecap) bool) *ConversationRecap {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if meta, _ := LoadMeta(dir); meta.Recap != nil && want(meta.Recap) {
			return meta.Recap
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the recap never landed in meta.json")
	return nil
}

func textStep(text string) step {
	return func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(text), nil }
}

func TestParseRecapIsTolerantAndBounded(t *testing.T) {
	long := strings.Repeat("word ", 400)
	for _, row := range []struct {
		name, raw string
		ok        bool
		check     func(t *testing.T, recap ConversationRecap)
	}{
		{"plain", goodRecap, true, func(t *testing.T, r ConversationRecap) {
			if r.Line == "" || len(r.Decided) != 1 || r.Decided[0].By != "you" || r.Decided[0].How != "accepted" {
				t.Fatalf("recap = %+v", r)
			}
		}},
		{"code fence and chatter", "Sure!\n```json\n" + goodRecap + "\n```\nHope that helps.", true, nil},
		{"bare string decision", `{"line":"Ruled out JSON5, because it also allows comments","decided":["Ruled out JSON5"]}`, true, func(t *testing.T, r ConversationRecap) {
			if len(r.Decided) != 1 || r.Decided[0].By != "" || r.Decided[0].Text != "Ruled out JSON5" {
				t.Fatalf("a bare decision must be kept with no invented author: %+v", r.Decided)
			}
		}},
		{"author words are narrowed", `{"line":"x y","decided":[{"text":"a","by":"The Assistant"},{"text":"b","by":"user"},{"text":"c","by":"somebody"}]}`, true, func(t *testing.T, r ConversationRecap) {
			if r.Decided[0].By != "" || r.Decided[1].By != "you" || r.Decided[2].By != "" {
				t.Fatalf("authors = %+v", r.Decided)
			}
		}},
		{"lengths are clipped", `{"line":"` + long + `","discussed":"` + long + `","outcome":"` + long + `"}`, true, func(t *testing.T, r ConversationRecap) {
			if len(r.Line) > recapLineLimit || len(r.Discussed) > recapDiscussedLimit || len(r.Outcome) > recapOutcomeLimit {
				t.Fatalf("not clipped: %d %d %d", len(r.Line), len(r.Discussed), len(r.Outcome))
			}
		}},
		{"too many decisions", `{"line":"x y","decided":["1","2","3","4","5","6","7","8","9","10"]}`, true, func(t *testing.T, r ConversationRecap) {
			if len(r.Decided) != recapDecisions {
				t.Fatalf("decisions = %d", len(r.Decided))
			}
		}},
		{"empty line", `{"line":"  ","discussed":"x"}`, false, nil},
		{"a count is no account", `{"line":"9 messages"}`, false, nil},
		{"a duration is no account", `{"line":"Worked 34s"}`, false, nil},
		{"not json", `no idea`, false, nil},
		{"control tokens", `{"line":"ok <|im_end|>"}`, false, nil},
	} {
		t.Run(row.name, func(t *testing.T) {
			recap, ok := parseRecap(row.raw)
			if ok != row.ok {
				t.Fatalf("parseRecap ok = %v, want %v (%+v)", ok, row.ok, recap)
			}
			if ok && row.check != nil {
				row.check(t, recap)
			}
		})
	}
}

func TestRecapFingerprintMovesWithTheConversation(t *testing.T) {
	a := []recapTurn{{"user", "hi"}, {"assistant", "hello"}}
	b := append(append([]recapTurn(nil), a...), recapTurn{"user", "more"})
	c := []recapTurn{{"user", "hi"}, {"assistant", "hello!"}}
	if recapFingerprint(a) != recapFingerprint(append([]recapTurn(nil), a...)) {
		t.Fatal("the same words must fingerprint the same")
	}
	if recapFingerprint(a) == recapFingerprint(b) || recapFingerprint(a) == recapFingerprint(c) {
		t.Fatal("a new message or an edited last message must change the fingerprint")
	}
	if recapFingerprint(nil) != "" {
		t.Fatal("no conversation has no fingerprint")
	}
}

func TestRecapAskIsBounded(t *testing.T) {
	var turns []recapTurn
	for i := 0; i < 200; i++ {
		turns = append(turns, recapTurn{"user", fmt.Sprintf("question %d %s", i, strings.Repeat("x", 5000))})
	}
	ask := recapAsk(turns, nil)
	if !strings.Contains(ask, "question 0 ") || !strings.Contains(ask, "question 199 ") || strings.Contains(ask, "question 100 ") {
		t.Fatal("the ask keeps the first message and the newest, and leaves the middle out")
	}
	if len(ask) > (recapTail+1)*(recapClip+40)+len(recapPrompt)+200 {
		t.Fatalf("ask is %d bytes — the window is not bounded", len(ask))
	}
}

func TestChangedFilesAreReadOffTheCallsAndCountedHonestly(t *testing.T) {
	files := newRecapFiles()
	none := func(string) bool { return false }
	made := func(path string) bool { return path == "/w/new.go" }
	edit := ai.ToolCall{ID: "1", Function: ai.ToolCallFunction{Name: "edit", Arguments: `{"path":"a.go","edits":[{"oldText":"x\ny\n","newText":"z\n"}]}`}}
	files.note(edit, "Successfully replaced 1 block", none, "/w")
	files.note(edit, "Successfully replaced 1 block", none, "/w")
	files.note(ai.ToolCall{ID: "2", Function: ai.ToolCallFunction{Name: "edit", Arguments: `{"path":"failed.go","oldText":"a","newText":"b"}`}}, "could not find the text", none, "/w")
	files.note(ai.ToolCall{ID: "3", Function: ai.ToolCallFunction{Name: "write", Arguments: `{"path":"new.go","content":"a\nb\nc\n"}`}}, "Successfully wrote 6 bytes", made, "/w")
	files.note(ai.ToolCall{ID: "4", Function: ai.ToolCallFunction{Name: "write", Arguments: `{"path":"old.go","content":"a\nb\nc\n"}`}}, "Successfully wrote 6 bytes", made, "/w")
	files.note(ai.ToolCall{ID: "5", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"ls"}`}}, "", none, "/w")
	got := files.list()
	want := []RecapFile{{"a.go", 2, 4}, {"new.go", 3, 0}, {"old.go", 0, 0}}
	if len(got) != len(want) {
		t.Fatalf("files = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("files = %+v, want %+v", got, want)
		}
	}
}

// The settle hook asks once for a changed conversation and not at all for an
// unchanged one, and the turn is never made to wait for it.
func TestARecapIsWrittenOncePerChangedConversation(t *testing.T) {
	completer, asks := recapCompleter([]step{textStep("The lexer is the right place."), textStep("Agreed, then.")}, goodRecap)
	agent, dir := recapAgentOn(t, completer)
	collect(t, mustSubmit(t, agent, "should strict mode stay the default?"))
	first := awaitRecap(t, dir, func(r *ConversationRecap) bool { return r.Messages == 2 })
	agent.waitForTitle()
	if got := asks.Load(); got != 1 {
		t.Fatalf("recap asked %d times after one turn, want 1", got)
	}
	if first.Line == "" || first.Fingerprint == "" || first.UpdatedAt.IsZero() || len(first.Decided) != 1 {
		t.Fatalf("recap = %+v", first)
	}

	// The same conversation settling again buys nothing: no new message.
	agent.maybeTitle(context.Background(), nil)
	agent.waitForTitle()
	if got := asks.Load(); got != 1 {
		t.Fatalf("an unchanged conversation was recapped again (%d asks)", got)
	}

	collect(t, mustSubmit(t, agent, "ok, so fix it in the lexer"))
	awaitRecap(t, dir, func(r *ConversationRecap) bool { return r.Messages == 4 })
	agent.waitForTitle()
	if got := asks.Load(); got != 2 {
		t.Fatalf("recap asked %d times after two turns, want 2", got)
	}

	// A reopened conversation whose stored recap already covers it pays nothing.
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := newAgent(Config{
		Workspace: agent.config.Workspace, Model: "test/model", System: "SYSTEM",
		SessionFile: agent.config.SessionFile, Place: agent.config.Place, Recaps: true,
	}, completer)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	reopened.maybeTitle(context.Background(), nil)
	reopened.waitForTitle()
	if got := asks.Load(); got != 2 {
		t.Fatalf("a reopened, already-recapped conversation was recapped again (%d asks)", got)
	}
}

func TestARecapIsNotAskedWhereItHasNoHome(t *testing.T) {
	// No door asked for recaps.
	off, dir, asks := recapAgent(t, oneTurn("fine."), goodRecap)
	off.config.Recaps = false
	collect(t, mustSubmit(t, off, "hello there"))
	off.waitForTitle()
	if asks.Load() != 0 {
		t.Fatal("a door that does not list recaps paid for one")
	}
	if meta, _ := LoadMeta(dir); meta.Recap != nil {
		t.Fatalf("recap = %+v", meta.Recap)
	}

	// A task node.
	node, _, nodeAsks := recapAgent(t, oneTurn("fine."), goodRecap)
	node.config.InTask = true
	collect(t, mustSubmit(t, node, "do the thing"))
	node.waitForTitle()
	if nodeAsks.Load() != 0 {
		t.Fatal("a task node was recapped")
	}

	// No answer yet.
	pending, _, pendingAsks := recapAgent(t, nil, goodRecap)
	pending.mu.Lock()
	pending.startRecapLocked()
	pending.mu.Unlock()
	pending.waitForTitle()
	if pendingAsks.Load() != 0 {
		t.Fatal("a conversation with no answer was recapped")
	}

	// No file.
	memory, _ := newTestAgent(t, &scriptedCompleter{steps: oneTurn("fine.")}, func(c *Config) { c.Recaps = true })
	collect(t, mustSubmit(t, memory, "hello"))
	memory.waitForTitle()
}

func TestAnUnusableRecapAnswerLeavesNothingAndNeverBreaksTheTurn(t *testing.T) {
	agent, dir, asks := recapAgent(t, oneTurn("An answer."), "I cannot do that.")
	events := collect(t, mustSubmit(t, agent, "a question"))
	agent.waitForTitle()
	if countKind(events, EventError) != 0 {
		t.Fatalf("a failed recap broke the turn: %v", kinds(events))
	}
	if asks.Load() == 0 {
		t.Fatal("the writer was never asked")
	}
	if meta, _ := LoadMeta(dir); meta.Recap != nil {
		t.Fatalf("an unusable answer was stored: %+v", meta.Recap)
	}
}

// The recap is one patch among several writers of meta.json. Neither a title
// that lands while it is written nor a spend stamp prepared before it may be
// lost, and it must not lose them.
func TestTheRecapSharesMetaJSONWithoutClobbering(t *testing.T) {
	a, dir := metadataTitleAgent(t)
	stale := a.metaSnapshot()
	recap := ConversationRecap{Line: "Decided a thing", Messages: 2, Fingerprint: "2:abc", UpdatedAt: time.Now()}
	a.updateMeta(dir, stale, func(m *Meta) { m.Recap = &recap })
	a.stampTitle("parser migration failures")
	a.writeSpendSnapshot(dir, stale, .5, 90)
	meta, err := LoadMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Recap == nil || meta.Recap.Line != "Decided a thing" || meta.Recap.Fingerprint != "2:abc" {
		t.Fatalf("a later stamp lost the recap: %+v", meta.Recap)
	}
	if meta.Title != "parser migration failures" || meta.SpentUSD != .5 {
		t.Fatalf("the recap write lost the other writers' fields: %+v", meta)
	}
	if err := SetArchived(dir, true); err != nil {
		t.Fatal(err)
	}
	if after, _ := LoadMeta(dir); after.Recap == nil || !after.Archived {
		t.Fatalf("archiving lost the recap: %+v", after)
	}
}

func TestReadConversationShowsOnlyWhatWasSaid(t *testing.T) {
	agent, dir, _ := recapAgent(t, []step{textStep("First answer."), textStep("Second answer.")}, goodRecap)
	collect(t, mustSubmit(t, agent, "first question"))
	collect(t, mustSubmit(t, agent, "second question"))
	agent.waitForTitle()
	messages := ReadConversation(filepath.Join(dir, "transcript.jsonl"))
	var got []string
	for _, message := range messages {
		got = append(got, message.Role+":"+message.Text)
		if message.At.IsZero() {
			t.Fatalf("a journaled message lost its time: %+v", message)
		}
	}
	want := []string{"user:first question", "assistant:First answer.", "user:second question", "assistant:Second answer."}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("conversation = %v, want %v", got, want)
	}
	if ReadConversation(filepath.Join(dir, "nowhere.jsonl")) != nil {
		t.Fatal("a missing journal answers nothing")
	}
}

// The files in a recap come from the calls the transcript holds, never from the
// writer's word: a file the model claims in its JSON but never touched is not
// in it, and a file the turn wrote is.
func TestTheRecapNamesTheFilesTheTurnReallyWrote(t *testing.T) {
	claimed := strings.Replace(goodRecap, `"outcome"`, `"files":["invented.go"],"outcome"`, 1)
	agent, dir, _ := recapAgent(t, []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("w1", "write", `{"path":"notes.txt","content":"a\nb\n"}`), nil
		},
		textStep("Wrote the notes."),
	}, claimed)
	collect(t, mustSubmit(t, agent, "write some notes"))
	recap := awaitRecap(t, dir, func(r *ConversationRecap) bool { return r.Messages == 2 })
	if len(recap.Files) != 1 || recap.Files[0].Path != "notes.txt" || recap.Files[0].Removed != 0 {
		t.Fatalf("files = %+v, want only notes.txt", recap.Files)
	}
}

// The recap writer is a registered role on the cheap tier with words a settings
// row can show, and the desktop's "Titles and summaries" role owns it so one
// choice moves it.
func TestTheRecapWriterIsACheapRoleOwnedByTitlesAndSummaries(t *testing.T) {
	tier, ok := roles.TierOf(roles.RoleRecap)
	if !ok || tier != roles.TierLow {
		t.Fatalf("TierOf(recap) = %q, %v, want low", tier, ok)
	}
	if strings.TrimSpace(roles.Describe(roles.RoleRecap)) == "" {
		t.Fatal("the recap role has no description for a settings row")
	}
	if !roles.Known("recap") {
		t.Fatal("recap is missing from the role vocabulary")
	}
	owner, found := "", false
	for _, role := range config.DesktopRoles() {
		for _, engine := range role.Engine {
			if engine == roles.RoleRecap {
				owner, found = role.ID, true
			}
		}
	}
	if !found || owner != "naming" {
		t.Fatalf("recap is owned by %q (found %v), want naming", owner, found)
	}
	source := config.DesktopRolesSource(t.TempDir())
	if model, ok := source(roles.PinKey(roles.RoleRecap)); !ok || model != config.DesktopDefaultModel {
		t.Fatalf("the recap writer rides %q (%v), want the desktop default", model, ok)
	}
}

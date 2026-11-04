package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

type clientToolsFakeDoor struct {
	version uint64
	sets    []ClientToolSet
	calls   []ClientToolCall
	answer  ClientToolAnswer
}

func clientToolSourceStep(id string) step {
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse(id, "app_note", `{}`), nil
	}
}

// Source contract (a): every submission door and a queued follow-up name their own line.
func TestClientToolsSourceSubmissionDoors(t *testing.T) {
	for _, kind := range []string{"plain", "message", "composed", "follow-up"} {
		t.Run(kind, func(t *testing.T) {
			door := clientToolsFixture()
			entered, release := make(chan struct{}), make(chan struct{})
			first := clientToolSourceStep("first")
			if kind == "follow-up" {
				first = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
						return nil, ctx.Err()
					}
					return clientToolSourceStep("first")(ctx, messages)
				}
			}
			a, _ := newTestAgent(t, &scriptedCompleter{steps: []step{first, finishingStep(), clientToolSourceStep("second"), finishingStep()}},
				func(c *Config) { c.ClientTools = door; c.PromptProfile = "full" })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var events, next <-chan Event
			var err error
			switch kind {
			case "plain", "follow-up":
				events, err = a.Submit(ctx, "first question")
			case "message":
				events, err = a.SubmitMessage(ctx, Submission{Text: "first question", ClientID: "another-window"})
			case "composed":
				_, events, err = a.SubmitComposed(ctx, Submission{Text: "first question"})
			}
			if err != nil {
				t.Fatal(err)
			}
			if kind == "follow-up" {
				waitSignal(t, entered, "the first provider step before queuing the follow-up")
				next, err = a.FollowUp("second question")
				close(release)
				if err != nil {
					t.Fatal(err)
				}
			}
			collect(t, events)
			if next == nil {
				next = mustSubmit(t, a, "second question")
			}
			collect(t, next)
			var ids []string
			for _, entry := range a.Transcript() {
				if entry.Role == "user" && entry.Person != nil && entry.Person.Composed != nil {
					ids = append(ids, entry.Person.Composed.MessageID)
				}
			}
			if len(ids) != 2 || ids[0] == "" || ids[0] == ids[1] || len(door.calls) != 2 {
				t.Fatalf("messages=%v calls=%+v", ids, door.calls)
			}
			for i, call := range door.calls {
				if call.SourceMessage != ids[i] {
					t.Errorf("call %d source=%q want %q", i, call.SourceMessage, ids[i])
				}
			}
		})
	}
}

// Source contract (b): takeover replay reuses the recorded id without changing crew provenance.
func TestClientToolsSourceStoppedTurnReplay(t *testing.T) {
	path := takenOverMidThought(t, func(a *Agent) { a.InterruptFor(StopByTakeover) })
	want := journalPersonIDs(t, path)[0]
	door := clientToolsFixture()
	var crewOpener string
	a, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			crewOpener = turnOriginFrom(ctx).turn
			return clientToolSourceStep("resumed")(ctx, messages)
		}, finishingStep(),
	}}, func(c *Config) { c.SessionFile = path; c.ClientTools = door; c.PromptProfile = "full" })
	events, resumed := a.ResumeStoppedTurn(context.Background())
	if !resumed {
		t.Fatal("stopped turn did not resume")
	}
	collect(t, events)
	if len(door.calls) != 1 || door.calls[0].SourceMessage != want || crewOpener != "" {
		t.Fatalf("calls=%+v crew opener=%q want message %q", door.calls, crewOpener, want)
	}
}

// Source contract (b), (d): resuming a legacy line never invents a message id.
func TestClientToolsSourceLegacyStoppedTurn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	journal := `{"type":"session","version":1,"id":"legacy"}` + "\n" +
		`{"type":"message","role":"user","content":"old question"}` + "\n" +
		`{"type":"error","error":{"door":"taken over","message":"turn ended: taken over"}}` + "\n"
	if err := os.WriteFile(path, []byte(journal), 0600); err != nil {
		t.Fatal(err)
	}
	door := clientToolsFixture()
	a, _ := newTestAgent(t, &scriptedCompleter{steps: []step{clientToolSourceStep("legacy"), finishingStep()}},
		func(c *Config) { c.SessionFile = path; c.ClientTools = door; c.PromptProfile = "full" })
	events, resumed := a.ResumeStoppedTurn(context.Background())
	if !resumed {
		t.Fatal("legacy stopped turn did not resume")
	}
	collect(t, events)
	if len(door.calls) != 1 || door.calls[0].SourceMessage != "" {
		t.Fatalf("legacy line gained identity: %+v", door.calls)
	}
}

// Source contract (b): quit and close continue only an exactly recorded person's run.
func TestClientToolsSourceCarryOnReplay(t *testing.T) {
	for _, closeKey := range []bool{false, true} {
		for _, wake := range []bool{false, true} {
			t.Run(fmt.Sprintf("close=%t/wake=%t", closeKey, wake), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "session.jsonl")
				started := make(chan struct{})
				steps := []step{thinkingStep(started)}
				if wake {
					steps = append([]step{finishingStep()}, steps...)
				}
				a, _ := newTestAgent(t, &scriptedCompleter{steps: steps}, func(c *Config) { c.SessionFile = path })
				t.Cleanup(a.Interrupt)
				var events <-chan Event
				if wake {
					collect(t, mustSubmit(t, a, "original question"))
					wakes, stop := a.WatchWakes()
					defer stop()
					if !a.enqueueNote(wakeNote("a job finished")) {
						t.Fatal("initial wake note refused")
					}
					events = awaitWallStream(t, wakes, 10*time.Second)
				} else {
					events = mustSubmit(t, a, "original question")
				}
				waitSignal(t, started, "the interrupted provider step")
				a.InterruptFor(StopByClosing)
				collect(t, events)
				if err := a.Close(); err != nil {
					t.Fatal(err)
				}
				want := ""
				if !wake {
					want = journalPersonIDs(t, path)[0]
				}
				door := clientToolsFixture()
				fresh, _ := newTestAgent(t, &scriptedCompleter{steps: []step{clientToolSourceStep("carry"), finishingStep()}},
					func(c *Config) { c.SessionFile = path; c.ClientTools = door; c.PromptProfile = "full" })
				wakes, stop := fresh.WatchWakes()
				defer stop()
				accepted := false
				if closeKey {
					accepted = fresh.ResumeAfterCloseKey("continuing")
				} else {
					accepted = fresh.ResumeAfterQuit()
				}
				if !accepted {
					t.Fatal("carry-on refused")
				}
				finishJobWake(t, wakes)
				if len(door.calls) != 1 || door.calls[0].SourceMessage != want {
					t.Fatalf("calls=%+v want source %q", door.calls, want)
				}
			})
		}
	}
}

// Source contract (c): only a wake answering one drained person's line names it.
func TestClientToolsSourceWakes(t *testing.T) {
	for _, kind := range []string{"note", "one person", "two people", "mixed", "coalesced"} {
		t.Run(kind, func(t *testing.T) {
			door := clientToolsFixture()
			var a *Agent
			ending := func(context.Context, []ai.Message) (*ai.Response, error) {
				var queued []userMessage
				switch kind {
				case "note":
					queued = []userMessage{wakeNote("a job finished")}
				case "one person":
					queued = []userMessage{userText("drained question")}
				case "two people":
					queued = []userMessage{userText("drained question"), userText("another question")}
				case "mixed":
					queued = []userMessage{userText("drained question"), wakeNote("a job finished")}
				case "coalesced":
					queued = []userMessage{wakeNote("a job finished"), wakeNote("another job finished")}
				}
				for i := range queued {
					if queued[i].wake {
						queued[i].authored = true
					}
				}
				// Land after the final request, through the same queue the end drain reads.
				a.mu.Lock()
				a.steering = append(a.steering, queued...)
				a.mu.Unlock()
				return textResponse("first answer"), nil
			}
			a, _ = newTestAgent(t, &scriptedCompleter{steps: []step{clientToolSourceStep("person"), ending, clientToolSourceStep("wake"), finishingStep()}},
				func(c *Config) { c.ClientTools = door; c.PromptProfile = "full" })
			wakes, stop := a.WatchWakes()
			defer stop()
			collect(t, mustSubmit(t, a, "original question"))
			finishJobWake(t, wakes)
			want := ""
			for _, entry := range a.Transcript() {
				if kind == "one person" && entry.Text == "drained question" && entry.Person != nil && entry.Person.Composed != nil {
					want = entry.Person.Composed.MessageID
				}
			}
			if kind == "one person" && want == "" {
				t.Fatal("drained message has no recorded id")
			}
			if len(door.calls) != 2 || door.calls[0].SourceMessage == "" || door.calls[1].SourceMessage != want {
				t.Fatalf("calls=%+v want wake source %q", door.calls, want)
			}
		})
	}
}

// Source contract (d): a steer and a runtime note cannot replace a running turn's source.
func TestClientToolsSourceSurvivesSteering(t *testing.T) {
	door := clientToolsFixture()
	entered, release := make(chan struct{}), make(chan struct{})
	first := func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return clientToolSourceStep("before")(ctx, messages)
	}
	a, _ := newTestAgent(t, &scriptedCompleter{steps: []step{first, clientToolSourceStep("after"), finishingStep()}},
		func(c *Config) { c.ClientTools = door; c.PromptProfile = "full" })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := a.Submit(ctx, "original question")
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, entered, "the first provider step before steering")
	steer := mustSubmit(t, a, "a correction")
	if !a.enqueueNote(wakeNote("a job finished")) {
		t.Fatal("runtime note refused")
	}
	close(release)
	collect(t, events)
	collect(t, steer)
	var ids []string
	for _, entry := range a.Transcript() {
		if entry.Role == "user" && entry.Person != nil && entry.Person.Composed != nil {
			ids = append(ids, entry.Person.Composed.MessageID)
		}
	}
	if len(ids) != 2 || ids[0] == ids[1] || len(door.calls) != 2 {
		t.Fatalf("ids=%v calls=%+v", ids, door.calls)
	}
	for _, call := range door.calls {
		if call.SourceMessage != ids[0] {
			t.Errorf("source=%q want opener %q, steer=%q", call.SourceMessage, ids[0], ids[1])
		}
	}
}

// Source contract (b), (d): carry-on uses recorded run identity, never the latest person.
func TestClientToolsSourceCarryOnRequiresExactRun(t *testing.T) {
	for _, kind := range []string{"exact", "legacy", "duplicate", "woken", "unmarked tail", "mixed notes"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			mark := `,"presentation":{"audience":"person","run_id":"run-person","composed":{"messageId":"msg-original"}}`
			if kind == "legacy" {
				mark = ""
			}
			journal := `{"type":"session","version":1,"id":"carry"}` + "\n" +
				`{"type":"message","role":"user","content":"original question"` + mark + "}\n"
			if kind == "duplicate" {
				journal += `{"type":"message","role":"user","content":"ambiguous opener","presentation":{"audience":"person","run_id":"run-person","composed":{"messageId":"msg-duplicate"}}}` + "\n"
			}
			// A later steer has an id but did not open the interrupted run.
			journal += `{"type":"message","role":"user","content":"newer correction","presentation":{"audience":"person","composed":{"messageId":"msg-steer"}}}` + "\n"
			run := "run-person"
			if kind == "woken" {
				run = "run-note"
			}
			tail := `,"presentation":{"audience":"human","run_id":"` + run + `","interrupted":true}`
			if kind == "unmarked tail" {
				tail = ""
			}
			journal += `{"type":"message","role":"assistant","content":"partial answer"` + tail + "}\n"
			if err := os.WriteFile(path, []byte(journal), 0600); err != nil {
				t.Fatal(err)
			}
			door := clientToolsFixture()
			a, _ := newTestAgent(t, &scriptedCompleter{steps: []step{clientToolSourceStep("carry"), finishingStep()}},
				func(c *Config) { c.SessionFile = path; c.ClientTools = door; c.PromptProfile = "full" })
			if kind == "mixed notes" {
				a.mu.Lock()
				note := wakeNote("unrelated job finished")
				note.authored = true
				a.steering = append(a.steering, note)
				a.mu.Unlock()
			}
			wakes, stop := a.WatchWakes()
			defer stop()
			if !a.ResumeAfterQuit() {
				t.Fatal("carry-on refused")
			}
			finishJobWake(t, wakes)
			want := ""
			if kind == "exact" {
				want = "msg-original"
			}
			if len(door.calls) != 1 || door.calls[0].SourceMessage != want {
				t.Fatalf("calls=%+v want source %q", door.calls, want)
			}
		})
	}
}

func (d *clientToolsFakeDoor) ClientToolSets() (uint64, []ClientToolSet) { return d.version, d.sets }
func (d *clientToolsFakeDoor) CallClientTool(_ context.Context, call ClientToolCall) (ClientToolAnswer, error) {
	d.calls = append(d.calls, call)
	return d.answer, nil
}

func clientToolsFixture() *clientToolsFakeDoor {
	return &clientToolsFakeDoor{version: 1, sets: []ClientToolSet{{ID: "notes", Instructions: "Use the app's notes.", DeadlineMS: ClientToolDefaultDeadlineMS,
		Tools: []ClientTool{{Name: "app_note", Description: "Read a note", InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}}}`), Category: ActionRead}}}}}
}

// Contract (a), (e): definitions and instructions follow only this conversation's sets.
func TestClientToolsTailInstructionsAndRemoval(t *testing.T) {
	door := clientToolsFixture()
	a := &Agent{config: Config{ClientTools: door, profile: profileFull}}
	builtins := []bare.Tool{{Name: "read", Schema: json.RawMessage(`{"type":"object"}`)}}
	a.tools = a.appendClientTools(builtins)
	a.definitions, _ = toolDefinitions(a.tools)
	defs := a.beltDefinitions()
	if len(defs) != 2 || a.tools[1].Name != "app_note" {
		t.Fatalf("client tool is not the tail: %+v", a.tools)
	}
	encoded, _ := json.Marshal(defs[1])
	for _, word := range []string{"app_note", "Read a note", "properties", "id"} {
		if !strings.Contains(string(encoded), word) {
			t.Errorf("definition lost %q: %s", word, encoded)
		}
	}
	page := renderSystem(a.config)
	if !strings.Contains(page, "## Tools from the app: notes\nUse the app's notes.") {
		t.Fatal("instructions absent")
	}
	if strings.Contains(renderSystem(Config{}), "Tools from the app") {
		t.Fatal("ordinary prompt gained client bytes")
	}
	if got := testing.AllocsPerRun(10, a.clientToolsBoundary); got != 0 {
		t.Fatalf("unchanged boundary allocated %v", got)
	}
	door.version++
	door.sets = nil
	a.clientToolsBoundary()
	if len(a.beltTools()) != 1 || len(defs) != 2 {
		t.Fatal("removal changed an old reader or kept the tool")
	}
	if strings.Contains(renderSystem(a.config), "Tools from the app") {
		t.Fatal("removed instructions remain")
	}
}

// Contract (b), (h): object arguments only, errors and provider ids cross the door.
func TestClientToolsExecuteArgumentsAndError(t *testing.T) {
	door := clientToolsFixture()
	a := &Agent{config: Config{ClientTools: door}}
	tool := a.appendClientTools(nil)[0]
	for _, args := range []string{"null", "[]", "1", `"x"`, `"{\"id\":3}"`, "{"} {
		text, failed, err := tool.Execute(context.Background(), json.RawMessage(args))
		if !failed || err != nil || !strings.Contains(text, "Invalid arguments") {
			t.Fatalf("%s: %q %v %v", args, text, failed, err)
		}
	}
	if len(door.calls) != 0 {
		t.Fatal("non-object reached the client")
	}
	door.answer = ClientToolAnswer{Text: "notes could not do app_note: unavailable", IsError: true}
	text, failed, err := tool.Execute(withCallID(context.Background(), "provider-7"), json.RawMessage(`{}`))
	if err != nil || !failed || text != door.answer.Text || door.calls[0].TurnCall != "provider-7" {
		t.Fatalf("answer/id lost: %q %+v", text, door.calls)
	}
}

// Source contract (a), (d): Execute reads only the immutable call context.
func TestClientToolsExecuteSourceMessage(t *testing.T) {
	door := clientToolsFixture()
	a := &Agent{config: Config{ClientTools: door}}
	tool := a.appendClientTools(nil)[0]
	for _, test := range []struct {
		ctx  context.Context
		want string
	}{
		{withTurnOrigin(context.Background(), turnOrigin{turn: "crew-opener", answers: "msg-answer"}), "msg-answer"},
		{withTurnOrigin(context.Background(), turnOrigin{turn: "crew-opener"}), ""},
		{context.Background(), ""},
	} {
		if _, failed, err := tool.Execute(test.ctx, json.RawMessage(`{}`)); failed || err != nil {
			t.Fatalf("execute: failed=%v err=%v", failed, err)
		}
		if got := door.calls[len(door.calls)-1].SourceMessage; got != test.want {
			t.Fatalf("source=%q want %q", got, test.want)
		}
	}
}

// Contract (h): classification is per conversation and client tools never start early.
func TestClientToolsCategoriesAndBuiltinWins(t *testing.T) {
	door := clientToolsFixture()
	a := &Agent{config: Config{ClientTools: door}}
	a.tools = a.appendClientTools(nil)
	a.definitions, _ = toolDefinitions(a.tools)
	if a.ActionCategoryForTool("app_note") != ActionRead || (&Agent{}).ActionCategoryForTool("app_note") != ActionWork {
		t.Fatal("category escaped the conversation")
	}
	otherDoor := clientToolsFixture()
	otherDoor.sets[0].Tools[0].Category = ActionCreate
	other := &Agent{config: Config{ClientTools: otherDoor}}
	other.tools = other.appendClientTools(nil)
	if other.ActionCategoryForTool("app_note") != ActionCreate || a.ActionCategoryForTool("app_note") != ActionRead {
		t.Fatal("two conversations shared a category declaration")
	}
	if earlyTools["app_note"] {
		t.Fatal("client tool can start speculatively")
	}
	old := a.beltTools()
	if _, err := a.armFamily([]bare.Tool{{Name: "app_note", Description: "Built-in", Schema: json.RawMessage(`{"type":"object"}`)}}); err != nil {
		t.Fatal(err)
	}
	if len(a.tools) != 1 || a.tools[0].Description != "Built-in" || old[0].Description == "Built-in" {
		t.Fatal("late built-in did not win with fresh arrays")
	}
}

// Contract (a), (h): newly armed built-ins keep their prefix ahead of the app tail.
func TestClientToolsLateBuiltinPrecedesTail(t *testing.T) {
	door := clientToolsFixture()
	a := &Agent{config: Config{ClientTools: door}}
	a.tools = a.appendClientTools([]bare.Tool{{Name: "read", Schema: json.RawMessage(`{"type":"object"}`)}})
	a.definitions, _ = toolDefinitions(a.tools)
	oldTools, oldDefinitions := a.beltTools(), a.beltDefinitions()
	if _, err := a.armFamily([]bare.Tool{{Name: "new_builtin", Schema: json.RawMessage(`{"type":"object"}`)}}); err != nil {
		t.Fatal(err)
	}
	tools := a.beltTools()
	if len(tools) != 3 || tools[0].Name != "read" || tools[1].Name != "new_builtin" || tools[2].Name != "app_note" {
		t.Fatalf("belt order: %+v", tools)
	}
	if len(oldTools) != 2 || oldTools[1].Name != "app_note" || len(oldDefinitions) != 2 {
		t.Fatal("arming rewrote an old request")
	}
}

// Contract (a), (h): automation looks arm a family without a complete definition cache.
func TestClientToolsArmFamilyWithMissingDefinitions(t *testing.T) {
	for _, withClient := range []bool{false, true} {
		for _, cached := range []string{"nil", "short prefix", "short tail"} {
			t.Run(fmt.Sprintf("client=%t/cache=%s", withClient, cached), func(t *testing.T) {
				a := &Agent{}
				a.tools = []bare.Tool{
					{Name: "read", Schema: json.RawMessage(`{"type":"object"}`)},
					{Name: "bash", Schema: json.RawMessage(`{"type":"object"}`)},
				}
				if withClient {
					a.config.ClientTools = clientToolsFixture()
					a.tools = a.appendClientTools(a.tools)
				}
				if cached != "nil" {
					from := a.tools[:1]
					if cached == "short tail" {
						from = a.tools[len(a.tools)-1:]
					}
					var err error
					a.definitions, err = toolDefinitions(from)
					if err != nil {
						t.Fatal(err)
					}
				}
				cachedCount := len(a.definitions)
				oldTools, oldDefinitions := a.beltTools(), a.beltDefinitions()
				names, err := a.armFamily([]bare.Tool{{Name: "account_read", Schema: json.RawMessage(`{"type":"object"}`)}})
				if err != nil || len(names) != 1 || names[0] != "account_read" {
					t.Fatalf("arming: %v, %v", names, err)
				}
				want := []string{"read", "bash", "account_read"}
				if withClient {
					want = append(want, "app_note")
				}
				tools, definitions := a.beltTools(), a.beltDefinitions()
				if len(tools) != len(want) || len(definitions) != len(want) {
					t.Fatalf("belt/cache lengths: %d/%d, want %d", len(tools), len(definitions), len(want))
				}
				for i, name := range want {
					if tools[i].Name != name || definitions[i].Function.Name != name {
						t.Fatalf("belt/cache at %d: %s/%s, want %s", i, tools[i].Name, definitions[i].Function.Name, name)
					}
				}
				if len(oldDefinitions) != cachedCount || oldTools[1].Name != "bash" || (withClient && oldTools[2].Name != "app_note") {
					t.Fatal("arming changed an earlier snapshot")
				}
			})
		}
	}
}

// Contract (a), (b): the ordinary provider loop offers definitions and consumes the answer.
func TestClientToolsModelRequestAndToolResult(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "result", true: "error"}[failed], func(t *testing.T) {
			door := clientToolsFixture()
			door.answer = ClientToolAnswer{Text: `{"note":"found"}`, IsError: failed}
			if failed {
				door.answer.Text = "notes could not do app_note: unavailable"
			}
			recorder := &blockRecorder{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return toolResponse("provider-7", "app_note", `{"id":"n"}`), nil
				},
				func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
			}}
			a, _ := newTestAgent(t, recorder, func(c *Config) { c.System = ""; c.ClientTools = door; c.PromptProfile = "full" })
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			events, err := a.Submit(ctx, "read the app note")
			if err != nil {
				t.Fatal(err)
			}
			collected := collect(t, events)
			if len(door.calls) != 1 || door.calls[0].TurnCall != "provider-7" {
				t.Fatalf("dispatches: %+v", door.calls)
			}
			if got := recorder.resultAt(t, 1, "provider-7"); got != door.answer.Text {
				t.Fatalf("model result: %q", got)
			}
			recorder.mu.Lock()
			block := recorder.blocks[0]
			page := messageText(recorder.sent[0][0])
			recorder.mu.Unlock()
			definition, _ := json.Marshal(block[len(block)-1])
			if !strings.Contains(string(definition), "app_note") || !strings.Contains(page, "## Tools from the app: notes") {
				t.Fatalf("request lost its tail/page: %s", definition)
			}
			ended := false
			for _, event := range collected {
				if event.CallID != "provider-7" {
					continue
				}
				if event.Kind == EventToolBegin && toolEventCategory(event) != ActionRead {
					t.Fatal("step lost the declared category")
				}
				if event.Kind == EventToolEnd || event.Kind == EventToolFailed {
					ended = true
					if (event.Kind == EventToolFailed) != failed {
						t.Fatalf("error flag lost: %+v", event)
					}
				}
			}
			if !ended {
				t.Fatal("tool result lifecycle absent")
			}
		})
	}
}

// Contract (e): instructions rerender only on this agent's own page at a boundary.
func TestClientToolsBoundaryRefreshesOwnedPageAndPreservesCallerPage(t *testing.T) {
	for _, supplied := range []string{"", "The caller's page"} {
		door := clientToolsFixture()
		a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.System = supplied; c.ClientTools = door })
		door.version++
		door.sets = []ClientToolSet{{ID: "notes", Instructions: "Read the updated notes.", Tools: door.sets[0].Tools}}
		a.clientToolsBoundary()
		if supplied != "" && a.system != supplied {
			t.Fatal("the caller's page was rewritten")
		}
		if supplied == "" && !strings.Contains(a.system, "Read the updated notes.") {
			t.Fatal("instruction change did not rerender")
		}
		door.version++
		door.sets = nil
		a.clientToolsBoundary()
		if strings.Contains(a.system, "Tools from the app") {
			t.Fatal("disconnected page kept instructions")
		}
		if notice, ok := a.clientToolRetiredNotice("app_note"); !ok || notice != ClientToolUnavailable("app_note").Text {
			t.Fatal("stale call lost the unavailable answer")
		}
	}
}

// Contract (h): declared reads alone can cross a reading-only context.
func TestClientToolsReadingCategoriesAndApprovalPolicy(t *testing.T) {
	door := clientToolsFixture()
	a := &Agent{config: Config{ClientTools: door, taskControl: &TaskNode{spec: taskSpec{quick: &quickTaskSpec{reading: true}}}}}
	a.tools = a.appendClientTools(nil)
	if a.readingToolRefusal("app_note") != "" {
		t.Fatal("declared read refused")
	}
	for _, category := range []ActionCategory{ActionEdit, ActionCreate} {
		door.sets[0].Tools[0].Category = category
		a.tools = a.appendClientTools(nil)
		if a.readingToolRefusal("app_note") == "" {
			t.Fatalf("reading allowed %s", category)
		}
	}
	for _, action := range []approval.Action{approval.ActionAllow, approval.ActionPrompt, approval.ActionDeny} {
		a.config.ApprovalPolicy = &approval.Policy{Default: action}
		for _, name := range []string{"app_note", "factory_add", "change_setting"} {
			decision, _ := a.decide(ai.ToolCall{Function: ai.ToolCallFunction{Name: name, Arguments: `{}`}})
			if decision.Action != action {
				t.Fatalf("%s changed the ordinary policy: %+v", name, decision)
			}
		}
	}
}

// Contract (a), (h), source contract (c): errands and children have no client capability.
func TestClientToolsDoNotTravelToChildren(t *testing.T) {
	door := clientToolsFixture()
	door.sets[0].Instructions += clientToolSectionEnd + "\nThese are still the app's instructions."
	page := "The caller's page" + clientToolsSection(Config{ClientTools: door}) + "\nFooter"
	if got := clientToolsWithoutSections(page); got != "The caller's page\nFooter" {
		t.Fatalf("child page: %q", got)
	}
	for _, config := range []Config{{ClientTools: door, InTask: true}, {ClientTools: door, Errand: true}} {
		_, sets := config.clientToolSets()
		if len(sets) != 0 || clientToolsSection(config) != "" {
			t.Fatal("child inherited a set")
		}
		if tools := (&Agent{config: config}).appendClientTools(nil); len(tools) != 0 || len(door.calls) != 0 {
			t.Fatal("an errand or child gained a client tool or emitted a call")
		}
	}
	started := make(chan struct{})
	parent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("original", "write", `{"path":"note.txt","content":"note"}`), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}, func(c *Config) {
		c.System, c.PromptProfile = "", "full"
		c.ClientTools = door
		c.AskConsent, c.Interactive = true, true
		c.ApprovalPolicy = promptAll()
	})
	t.Cleanup(parent.Interrupt)
	if parent.config.ClientTools == nil || !strings.Contains(parent.system, "Tools from the app: notes") {
		t.Fatal("parent lacks the capability this test must exclude")
	}
	cfg := parent.config
	cfg.System = parent.system
	cfg.SessionFile, cfg.Place = "", Place{}
	cfg.InTask = true
	child, err := parent.newChildAgent(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Close() })
	clientToolsAssertExcluded(t, child)

	watch, stop := parent.WatchQuestions()
	defer stop()
	mustSubmit(t, parent, "write note.txt")
	q := discussionWait(t, watch, func(e Event) bool { return e.Kind == EventQuestion }).Question
	if err := parent.ResolveQuestion(Answer{Kind: q.Kind, ID: q.ID, Clarify: true, AskedBack: []Exchange{{Asked: "why this note?"}}}); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, started, "the clarification provider step")
	parent.mu.Lock()
	clarification := parent.discussions["clarify-1"].child
	parent.mu.Unlock()
	clientToolsAssertExcluded(t, clarification)
}

func clientToolsAssertExcluded(t *testing.T, child *Agent) {
	t.Helper()
	if child.config.ClientTools != nil {
		t.Fatal("child inherited the client door")
	}
	for _, page := range []string{child.config.System, child.system} {
		if strings.Contains(page, "Tools from the app") || strings.Contains(page, "These are still the app's instructions.") {
			t.Fatal("child inherited app instructions")
		}
	}
	for _, definition := range child.beltDefinitions() {
		if definition.Function.Name == "app_note" {
			t.Fatal("child offers a client tool")
		}
	}
}

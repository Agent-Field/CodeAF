package session

// view_image: the looking verb, without a socket.
//
// The seer is the scripted completer, so what these tests are actually about is
// which model was asked, what it was sent, and what a person-facing answer says
// — plus the absence law, which is the one behaviour a test can pin and a
// reviewer cannot see by reading the tool.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// viewTool is the tool as the belt would carry it, or a failure: these tests
// call it directly because tools.go — where the belt lane registers viewTools —
// is not this lane's file.
func viewTool(t *testing.T, agent *Agent) bare.Tool {
	t.Helper()
	tools := agent.viewTools()
	if len(tools) != 1 {
		t.Fatalf("viewTools carries %d tools, want view_image", len(tools))
	}
	if tools[0].Name != "view_image" {
		t.Fatalf("the tool is named %q", tools[0].Name)
	}
	return tools[0]
}

func runViewImage(t *testing.T, agent *Agent, arguments map[string]any) (string, bool) {
	t.Helper()
	encoded, err := json.Marshal(arguments)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text, isError, err := viewTool(t, agent).Execute(context.Background(), json.RawMessage(encoded))
	if err != nil {
		t.Fatalf("view_image returned a harness error: %v", err)
	}
	return text, isError
}

// The whole happy path: one call, on the slot's model, carrying the question
// and the picture and nothing else, answered in a sentence that names who
// looked.
func TestViewImageLooksThroughTheSlotAndSaysWhoLooked(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("a bar chart with the legend cut off"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, withSlot(map[string]string{"vision": "vendor/slot-eyes"}))
	writeImage(t, workspace, "chart.png", "PHOTOBYTES")

	text, isError := runViewImage(t, agent, map[string]any{
		"path": "chart.png", "question": "is the legend cut off?",
	})
	if isError {
		t.Fatalf("view_image failed: %s", text)
	}
	if text != "seen by vendor/slot-eyes: a bar chart with the legend cut off" {
		t.Fatalf("result = %q", text)
	}
	if got := completer.model(0); got != "vendor/slot-eyes" {
		t.Fatalf("the look rode %q, want the slot's model", got)
	}

	sent := completer.request(0)
	if len(sent) != 1 || sent[0].Role != "user" {
		t.Fatalf("the seer was sent %d messages: %+v", len(sent), sent)
	}
	if len(sent[0].Content) != 2 {
		t.Fatalf("content = %+v, want the question and the picture", sent[0].Content)
	}
	if sent[0].Content[0].Text != "is the legend cut off?" {
		t.Fatalf("the question was %q", sent[0].Content[0].Text)
	}
	// base64("PHOTOBYTES"), with the media type read from the extension.
	if urls := imagePartURLs(sent[0]); len(urls) != 1 || urls[0] != "data:image/png;base64,UEhPVE9CWVRFUw==" {
		t.Fatalf("the picture reached the seer as %v", urls)
	}
	// The look is one shot: nothing about it enters the conversation.
	if roles := transcriptRoles(agent); len(roles) != 1 {
		t.Fatalf("transcript = %v, want the system message alone", roles)
	}
	// And it is paid for out of the session's pocket, not a turn's.
	if usage := agent.Usage(); usage.Input == 0 && usage.Output == 0 {
		t.Fatal("the look was not accounted")
	}
	if usage := agent.Usage(); usage.Turns != 0 {
		t.Fatalf("the look counted as %d turns; an auxiliary call is nobody's turn", usage.Turns)
	}
}

// No question is a full description, in the words internal/exec asks a picture
// in — one surface, one kind of answer.
func TestViewImageAsksForAFullDescriptionWhenNothingWasAsked(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, withSlot(map[string]string{"vision": "vendor/slot-eyes"}))
	writeImage(t, workspace, "shot.png", "BYTES")

	if _, isError := runViewImage(t, agent, map[string]any{"path": "shot.png"}); isError {
		t.Fatal("view_image failed with no question")
	}
	sent := completer.request(0)
	if sent[0].Content[0].Text != viewImageDefaultQuestion {
		t.Fatalf("the question was %q", sent[0].Content[0].Text)
	}
}

// A BELT WITH NO EYES HAS NO VERB. Nothing resolves, so the tool is absent
// rather than present and refusing.
func TestViewImageIsAbsentWhenNothingCanLook(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*Config)
	}{
		{"no resolver and no pin", func(*Config) {}},
		{"a resolver with nothing to name", withSlot(map[string]string{})},
		{"a slot set to blank", withSlot(map[string]string{"vision": "   "})},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			agent, _ := newTestAgent(t, &scriptedCompleter{}, testCase.mutate)
			if tools := agent.viewTools(); len(tools) != 0 {
				t.Fatalf("the belt grew %d looking tools with nothing behind them", len(tools))
			}
		})
	}
}

// Every refusal is a TOOL error the model can act on, and each names the file.
func TestViewImageRefusalsNameTheFile(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, withSlot(map[string]string{"vision": "vendor/slot-eyes"}))

	t.Run("missing file", func(t *testing.T) {
		text, isError := runViewImage(t, agent, map[string]any{"path": "nowhere.png"})
		if !isError {
			t.Fatalf("a missing file was accepted: %s", text)
		}
		if !strings.Contains(text, "could not read") || !strings.Contains(text, "nowhere.png") {
			t.Fatalf("refusal = %q", text)
		}
		// The refusal is the session's sentence without its package prefix: a
		// tool result is about a file, not about a Go package.
		if strings.Contains(text, "session:") {
			t.Fatalf("the refusal carries the package prefix: %q", text)
		}
	})

	t.Run("a format nobody reads", func(t *testing.T) {
		writeImage(t, workspace, "notes.tiff", "TIFF")
		text, isError := runViewImage(t, agent, map[string]any{"path": "notes.tiff"})
		if !isError {
			t.Fatalf("an unreadable format was accepted: %s", text)
		}
		if !strings.Contains(text, "notes.tiff is not an image this surface can send") {
			t.Fatalf("refusal = %q", text)
		}
	})

	t.Run("over the ceiling", func(t *testing.T) {
		path := filepath.Join(workspace, "huge.png")
		if err := os.WriteFile(path, make([]byte, maxImageBytes+1), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		text, isError := runViewImage(t, agent, map[string]any{"path": "huge.png"})
		if !isError {
			t.Fatalf("an oversize picture was accepted: %s", text)
		}
		if !strings.Contains(text, "is over the 10MB image limit") {
			t.Fatalf("refusal = %q", text)
		}
	})

	t.Run("no path", func(t *testing.T) {
		text, isError := runViewImage(t, agent, map[string]any{"question": "what is this?"})
		if !isError || !strings.Contains(text, "path is required") {
			t.Fatalf("result = %q (error=%v)", text, isError)
		}
	})

	// Nothing above reached the model.
	if completer.requests() != 0 {
		t.Fatalf("the seer was called %d times for refused looks", completer.requests())
	}
}

// A seer that says nothing is a refusal that names it, so the model knows to
// stop rather than to look again.
func TestViewImageSaysWhenTheSeerAnsweredNothing(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("   "), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, withSlot(map[string]string{"vision": "vendor/slot-eyes"}))
	writeImage(t, workspace, "shot.png", "BYTES")

	text, isError := runViewImage(t, agent, map[string]any{"path": "shot.png"})
	if !isError {
		t.Fatalf("an empty answer was accepted: %s", text)
	}
	// The refusal names the file WHOLE. A person whose terminal cannot draw the
	// picture has this sentence and nothing else, and "shot.png" alone does not
	// say which shot.png.
	want := "vendor/slot-eyes returned no answer for " + filepath.ToSlash(filepath.Join(workspace, "shot.png"))
	if !strings.Contains(text, want) {
		t.Fatalf("refusal = %q, want %q in it", text, want)
	}
}

// A LOOK THAT NEVER COMES BACK STILL ENDS. This is the bug the window exists
// for: the seer took the picture and went quiet, and because the turn's context
// carries no deadline the tool used to sit there for the life of the session —
// a journaled call with no result, which the room draws as a row still running.
// The window runs out, the tool answers, and the answer names who did not
// answer.
func TestViewImageAnswersWhenTheSeerNeverDoes(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			// The stalled provider, exactly: it holds the call until something
			// above it gives up, and returns whatever that was.
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}
	restore := viewLookWindow
	viewLookWindow = 20 * time.Millisecond
	t.Cleanup(func() { viewLookWindow = restore })

	agent, workspace := newTestAgent(t, completer, withSlot(map[string]string{"vision": "vendor/slot-eyes"}))
	writeImage(t, workspace, "render.png", "BYTES")

	done := make(chan struct{})
	var text string
	var isError bool
	go func() {
		defer close(done)
		text, isError = runViewImage(t, agent, map[string]any{"path": "render.png"})
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("view_image never returned: the look is unbounded again")
	}

	if !isError {
		t.Fatalf("a look that never answered was reported as an answer: %s", text)
	}
	want := "vendor/slot-eyes did not answer about " +
		filepath.ToSlash(filepath.Join(workspace, "render.png")) + " within "
	if !strings.Contains(text, want) {
		t.Fatalf("refusal = %q, want %q in it", text, want)
	}
}

// ── the row a person watches ────────────────────────────────────────────────

// The tool returning is only half of "the look ended". What a person watches is
// the EVENT, and a look whose row never closes is the same complaint whether the
// tool hung or the ending was never announced — so this drives a whole turn and
// reads the stream, on both endings.
//
// A successful look ends on EventToolEnd carrying the answer, and a look that
// never comes back ends on EventToolFailed carrying the refusal. Neither waits
// for the turn: both are sent as soon as the batch this call is in finishes,
// which for a single-call batch is the moment the tool returns.
func TestViewImageEndsItsRowOnBothEndings(t *testing.T) {
	restore := viewLookWindow
	viewLookWindow = 20 * time.Millisecond
	t.Cleanup(func() { viewLookWindow = restore })

	for _, testCase := range []struct {
		name string
		look step
		kind EventKind
		// wants is built from the workspace, because a refusal names the picture
		// by its WHOLE path and the workspace is a temporary directory this test
		// only learns the name of at run time.
		wants func(workspace string) string
	}{
		{
			name: "the seer answers",
			look: func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse("a bar chart with the legend cut off"), nil
			},
			kind: EventToolEnd,
			wants: func(string) string {
				return "seen by vendor/slot-eyes: a bar chart with the legend cut off"
			},
		},
		{
			// The stalled provider: it holds the call until the window above it
			// gives up. The row still closes, and it closes with a sentence.
			name: "the seer never answers",
			look: func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
			kind: EventToolFailed,
			wants: func(workspace string) string {
				return "vendor/slot-eyes did not answer about " +
					filepath.ToSlash(filepath.Join(workspace, "chart.png")) + " within "
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			completer := &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return toolResponse("call-look", "view_image", `{"path":"chart.png"}`), nil
				},
				testCase.look,
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return textResponse("that is what it says"), nil
				},
			}}
			agent, workspace := newTestAgent(t, completer, withSlot(map[string]string{"vision": "vendor/slot-eyes"}))
			writeImage(t, workspace, "chart.png", "PHOTOBYTES")

			collected := collect(t, mustSubmit(t, agent, "what does the chart say?"))

			begin, began := firstOfKind(collected, EventToolBegin)
			if !began || begin.Tool != "view_image" {
				t.Fatalf("no view_image began: %v", kinds(collected))
			}
			ended, ok := firstOfKind(collected, testCase.kind)
			if !ok {
				t.Fatalf("the look's row never closed: %v", kinds(collected))
			}
			if ended.Tool != "view_image" {
				t.Fatalf("the closing event is for %q", ended.Tool)
			}
			wants := testCase.wants(workspace)
			if !strings.Contains(ended.Output, wants) {
				t.Fatalf("closing output = %q, want %q in it", ended.Output, wants)
			}
			// And the model was told the same thing on the record, so the row and
			// the transcript cannot disagree about whether the look happened.
			agent.mu.Lock()
			transcript := append([]ai.Message(nil), agent.messages...)
			agent.mu.Unlock()
			answered := false
			for _, message := range transcript {
				if message.Role == "tool" && message.ToolCallID == "call-look" &&
					strings.Contains(messageText(message), wants) {
					answered = true
				}
			}
			if !answered {
				t.Fatalf("the look's call was left unanswered in the transcript: %v", rolesOf(transcript))
			}
		})
	}
}

// And the window is the one already written down. A second number here would be
// a second answer to "how long may one completion take" (agent.go).
func TestViewLookWindowIsTheProviderTimeout(t *testing.T) {
	if viewLookWindow != providerTimeout {
		t.Fatalf("the look window is %s, want providerTimeout (%s)", viewLookWindow, providerTimeout)
	}
}

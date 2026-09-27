package session

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/roles"
)

//go:embed prompts/runask.md
var runAskPrompt string
var _ embed.FS

type RunAskExchange struct {
	Question string
	Answer   string
}
type RunAskSource struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	StepStart int    `json:"step_start"`
	StepEnd   int    `json:"step_end"`
}
type RunAskAnswer struct {
	Text   string         `json:"text"`
	From   []RunAskSource `json:"from"`
	IsNote bool           `json:"-"`
	Note   string         `json:"note,omitempty"`
}

// THE ASK TURN IS BOUNDED BY ITS INPUT, the way the run summary is: it is one
// small errand on the worker model, and a run of two hundred tasks with long
// results must cost what a run of five does to ask about.
const (
	runAskMaxRows      = 60
	runAskNotesPerTask = 3
	runAskLineChars    = 160
	runAskBodyChars    = 4000
)

type runAskToolArgs struct {
	ID string `json:"id"`
}

func (a *Agent) AskRun(ctx context.Context, rootID, question string, earlier []RunAskExchange) (RunAskAnswer, error) {
	if note, ok := runAskSteer(question); ok {
		return RunAskAnswer{IsNote: true, Note: note}, nil
	}
	rows, err := a.runAskRows(rootID)
	if err != nil {
		return RunAskAnswer{}, err
	}
	var front strings.Builder
	front.WriteString("RUN ROWS AND NOTES\n")
	front.WriteString(rows)
	if len(earlier) > 3 {
		earlier = earlier[len(earlier)-3:]
	}
	for _, x := range earlier {
		fmt.Fprintf(&front, "\nEarlier question: %s\nEarlier answer: %s", x.Question, x.Answer)
	}
	fmt.Fprintf(&front, "\n\nQuestion: %s", question)
	messages := []ai.Message{{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: runAskPrompt}}}, {Role: "user", Content: []ai.ContentPart{{Type: "text", Text: front.String()}}}}
	answer, err := a.runAskCalls(ctx, rootID, messages)
	if err != nil {
		return RunAskAnswer{}, err
	}
	if answer.IsNote || len(answer.From) > 0 {
		return answer, nil
	}
	messages = append(messages, ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: answer.Text}}}, ai.Message{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "Refused: every answer must name its task source. Answer once more with a source from the run record, or say plainly that the record does not hold it."}}})
	return a.runAskCalls(ctx, rootID, messages)
}

func (a *Agent) runAskCalls(ctx context.Context, rootID string, messages []ai.Message) (RunAskAnswer, error) {
	tool := ai.ToolDefinition{Type: "function", Function: ai.ToolFunction{Name: "read_task", Description: "Read one task from this run", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, "required": []string{"id"}, "additionalProperties": false}}}
	for round := 0; round < 3; round++ {
		response, called, err := a.callRole(ctx, roles.RoleWorker, a.Model(), messages, ai.WithTools([]ai.ToolDefinition{tool}))
		if err != nil {
			return RunAskAnswer{}, err
		}
		// EVERY ROUND IS PAID FOR, the read_task round as much as the answer, and
		// is banked detached because a person asked a page, not a turn
		// (runsummary.go says the same of the card's lines).
		a.addDetachedUsageAs(response, called, 1, string(roles.RoleWorker))
		if len(response.Choices) == 0 {
			return RunAskAnswer{}, errors.New("run ask returned no answer")
		}
		msg := response.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			return decodeRunAsk(messageContentText(msg))
		}
		messages = append(messages, msg)
		call := msg.ToolCalls[0]
		var args runAskToolArgs
		var body string
		if call.Function.Name != "read_task" {
			body = `{"error":"only read_task is available"}`
		} else if json.Unmarshal([]byte(call.Function.Arguments), &args) != nil {
			body = `{"error":"invalid read_task arguments"}`
		} else {
			body = a.runAskTask(rootID, args.ID)
		}
		messages = append(messages, ai.Message{Role: "tool", ToolCallID: call.ID, Content: []ai.ContentPart{{Type: "text", Text: body}}})
	}
	return RunAskAnswer{Text: "The bounded read ended before the record yielded an answer."}, nil
}

// decodeRunAsk reads the one object the page asks for. A MODEL WRAPS JSON IN A
// FENCE OR A SENTENCE MORE OFTEN THAN IT DOES NOT, so the object is found by its
// braces; text that holds no object is the answer itself, with no source, and
// the caller refuses that once.
func decodeRunAsk(text string) (RunAskAnswer, error) {
	text = strings.TrimSpace(text)
	body := text
	if open, shut := strings.IndexByte(text, '{'), strings.LastIndexByte(text, '}'); open >= 0 && shut > open {
		body = text[open : shut+1]
	}
	var out RunAskAnswer
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return RunAskAnswer{Text: text}, nil
	}
	out.Text, out.Note = strings.TrimSpace(out.Text), strings.TrimSpace(out.Note)
	if out.Note != "" && out.Text == "" {
		out.IsNote = true
	}
	return out, nil
}

func (a *Agent) runAskRows(rootID string) (string, error) {
	store, plan, closeStore := a.openPlanHandle()
	if store == nil {
		return "", errPlanNoStore
	}
	defer closeStore()
	root := store.Task(planTaskID(rootID))
	if root == nil || root.Chat != plan.chat {
		return "", planNoTask(rootID)
	}
	members := map[string]bool{root.ID: true}
	rows := 0
	var b strings.Builder
	for _, task := range store.Tasks(plandb.Filter{Chat: plan.chat}) {
		if task.ID != root.ID && !members[task.ParentID] {
			continue
		}
		members[task.ID] = true
		if rows++; rows > runAskMaxRows {
			continue
		}
		fmt.Fprintf(&b, "%s · %s · %s [%s]\n", cutChars(task.Title, runAskLineChars), task.Status, summaryFirstLine(task.Result, runAskLineChars), "t-"+task.ID)
		notes := store.Notes(task.ID, 0)
		if len(notes) > runAskNotesPerTask {
			notes = notes[len(notes)-runAskNotesPerTask:]
		}
		for _, n := range notes {
			fmt.Fprintf(&b, "note on t-%s: %s\n", task.ID, summaryFirstLine(n.Body, runAskLineChars))
		}
	}
	return b.String(), nil
}

func (a *Agent) runAskTask(rootID, id string) string {
	store, plan, close := a.openPlanHandle()
	if store == nil {
		return `{"error":"task is not in this run"}`
	}
	task, root := store.Task(planTaskID(id)), store.Task(planTaskID(rootID))
	inside := task != nil && root != nil && task.Chat == plan.chat
	for cursor := task; inside && cursor.ID != root.ID; cursor = store.Task(cursor.ParentID) {
		if cursor.ParentID == "" {
			inside = false
			break
		}
	}
	close()
	if !inside {
		return `{"error":"task is not in this run"}`
	}
	page, ok := a.PlanTaskPage(id)
	if !ok {
		return `{"error":"task is not in this run"}`
	}
	store, _, closeStore := a.openPlanHandle()
	result := ""
	if store != nil {
		if task := store.Task(planTaskID(id)); task != nil {
			result = task.Result
		}
		closeStore()
	}
	steps := page.Steps
	if len(steps) > 12 {
		steps = steps[len(steps)-12:]
	}
	payload := struct {
		ID, Title, Description, Result string
		Notes                          []PlanTaskNote
		Steps                          []PlanStep
	}{page.Row.ID, page.Row.Title, cutChars(page.Description, runAskBodyChars), cutChars(result, runAskBodyChars), page.Notes, steps}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

func runAskSteer(q string) (string, bool) {
	s := strings.TrimSpace(q)
	lower := strings.ToLower(s)
	for _, prefix := range []string{"tell it to ", "ask it to ", "have it "} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(s[len(prefix):]), true
		}
	}
	return "", false
}
func (a *Agent) SendRunNote(rootID, text string) error { return a.PlanNote(rootID, text) }

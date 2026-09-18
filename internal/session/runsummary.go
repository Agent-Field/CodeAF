package session

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/roles"
)

//go:embed prompts/runsummary.md
var runSummaryPrompt string

const runSummaryContextKind = "run-summary"

type RunPlanSummary struct {
	What      string    `json:"what"`
	Since     string    `json:"since,omitempty"`
	Now       string    `json:"now,omitempty"`
	Next      string    `json:"next,omitempty"`
	WrittenAt time.Time `json:"written_at"`
}

type storedRunSummary struct {
	Summary RunPlanSummary `json:"summary"`
	Stamp   string         `json:"stamp"`
}

type runSummaryShape struct {
	Tasks     []runSummaryTask `json:"tasks"`
	Questions []string         `json:"questions,omitempty"`
}

type runSummaryTask struct{ ID, Status string }

// PlanRunSummary reads the last stored summary and says whether the store's
// current task-and-question shape has moved since it was written.
func (a *Agent) PlanRunSummary(rootID string) (RunPlanSummary, bool) {
	store, _, closeStore := a.openPlanHandle()
	if store == nil {
		return RunPlanSummary{}, false
	}
	defer closeStore()
	stored, ok := readRunSummary(store, rootID)
	if !ok {
		return RunPlanSummary{}, false
	}
	return stored.Summary, stored.Stamp != runSummaryStamp(store, rootID)
}

// RefreshRunSummary pays for one worker-tier call only when the run moved.
// Any unavailable or malformed answer preserves the last good reading.
func (a *Agent) RefreshRunSummary(ctx context.Context, rootID string, lastLook time.Time) (RunPlanSummary, bool) {
	store, _, closeStore := a.openPlanHandle()
	if store == nil {
		return RunPlanSummary{}, false
	}
	stored, had := readRunSummary(store, rootID)
	stamp := runSummaryStamp(store, rootID)
	if had && stored.Stamp == stamp {
		closeStore()
		return stored.Summary, true
	}
	input := runSummaryInput(store, rootID, lastLook, a.summaryNow(), stored.Summary)
	closeStore()
	response, _, err := a.callRole(ctx, roles.RoleWorker, a.model, []ai.Message{
		textMessage("system", runSummaryPrompt), textMessage("user", input),
	}, ai.WithMaxTokens(320))
	if err != nil || response == nil || len(response.Choices) == 0 {
		return stored.Summary, had
	}
	parsed, ok := parseRunSummary(messageTextValue(response.Choices[0].Message))
	if !ok {
		return stored.Summary, had
	}
	if parsed.What == "" {
		parsed.What = stored.Summary.What
	}
	parsed.WrittenAt = a.summaryNow()
	store, _, closeStore = a.openPlanHandle()
	if store == nil {
		return stored.Summary, had
	}
	defer closeStore()
	// Do not stamp over movement that occurred while the model was answering.
	stamp = runSummaryStamp(store, rootID)
	payload, err := json.Marshal(storedRunSummary{Summary: parsed, Stamp: stamp})
	if err != nil {
		return stored.Summary, had
	}
	if _, err := store.AddContext(rootID, runSummaryContextKind, string(payload)); err != nil {
		return stored.Summary, had
	}
	return parsed, true
}

func (a *Agent) summaryNow() time.Time {
	a.mu.Lock()
	clock := a.config.clock
	a.mu.Unlock()
	if clock != nil {
		return clock()
	}
	return time.Now()
}

func readRunSummary(store *plandb.Store, rootID string) (storedRunSummary, bool) {
	rows := store.Contexts(rootID, runSummaryContextKind, 1)
	if len(rows) == 0 {
		return storedRunSummary{}, false
	}
	var got storedRunSummary
	if json.Unmarshal([]byte(rows[0].Content), &got) != nil || got.Summary.What == "" {
		return storedRunSummary{}, false
	}
	return got, true
}

func runSummaryStamp(store *plandb.Store, rootID string) string {
	shape := currentRunSummaryShape(store, rootID)
	body, _ := json.Marshal(shape)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func currentRunSummaryShape(store *plandb.Store, rootID string) runSummaryShape {
	root := store.Task(rootID)
	filter := plandb.Filter{}
	if root != nil {
		filter = plandb.Filter{Project: root.Project, Chat: root.Chat}
	}
	shape := runSummaryShape{}
	for _, task := range store.Tasks(filter) {
		shape.Tasks = append(shape.Tasks, runSummaryTask{task.ID, string(task.Status)})
	}
	for _, q := range store.Contexts("", "question", 200, filter) {
		shape.Questions = append(shape.Questions, summaryFirstLine(q.Content, 120))
	}
	return shape
}

func runSummaryInput(store *plandb.Store, rootID string, lastLook, now time.Time, previous RunPlanSummary) string {
	root := store.Task(rootID)
	taskFilter := plandb.Filter{}
	taskAsk := ""
	if root != nil {
		taskAsk = cutChars(root.Description, 1500)
		taskFilter = plandb.Filter{Project: root.Project, Chat: root.Chat}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "PERSON'S ASK\n%s\n\nRUN ROWS\n", taskAsk)
	tasks := store.Tasks(taskFilter)
	if len(tasks) > 40 {
		tasks = tasks[:40]
	}
	for _, task := range tasks {
		fmt.Fprintf(&b, "%s · %s · %s\n", cutChars(task.Title, 120), task.Status, summaryFirstLine(task.Result, 120))
	}
	b.WriteString("\nOPEN QUESTIONS\n")
	for _, q := range store.Contexts("", "question", 200, taskFilter) {
		b.WriteString(summaryFirstLine(q.Content, 120) + "\n")
	}
	age := "never"
	if !lastLook.IsZero() {
		d := now.Sub(lastLook)
		if d < 0 {
			d = 0
		}
		age = d.Round(time.Minute).String() + " ago"
	}
	fmt.Fprintf(&b, "\nLAST LOOK\n%s\n\nLAST LINES\nwhat: %s\nsince: %s\nnow: %s\nnext: %s\n", age, previous.What, previous.Since, previous.Now, previous.Next)
	return b.String()
}

func parseRunSummary(text string) (RunPlanSummary, bool) {
	var got RunPlanSummary
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		label, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(label)) {
		case "what":
			got.What, seen["what"] = value, true
		case "since":
			if strings.EqualFold(value, "nothing") {
				value = ""
			}
			got.Since, seen["since"] = value, true
		case "now":
			got.Now, seen["now"] = value, true
		case "next":
			got.Next, seen["next"] = value, true
		}
	}
	return got, seen["what"] && seen["since"] && seen["now"] && seen["next"]
}

func messageTextValue(message ai.Message) string {
	var b strings.Builder
	for _, p := range message.Content {
		b.WriteString(p.Text)
	}
	return b.String()
}
func summaryFirstLine(s string, n int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return cutChars(strings.TrimSpace(s), n)
}
func cutChars(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

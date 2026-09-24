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
	family := runSummaryFamily(store, rootID)
	return stored.Summary, stored.Stamp != runSummaryStamp(family, a.runSummaryQuestions(family))
}

// RefreshRunSummary pays for one worker-tier call only when the run moved.
// Any unavailable or malformed answer preserves the last good reading.
func (a *Agent) RefreshRunSummary(ctx context.Context, rootID string, lastLook time.Time) (RunPlanSummary, bool) {
	store, _, closeStore := a.openPlanHandle()
	if store == nil {
		return RunPlanSummary{}, false
	}
	stored, had := readRunSummary(store, rootID)
	family := runSummaryFamily(store, rootID)
	// A RUN WITH NOTHING IN IT BUYS NO READING, and neither does a program's.
	// The first refresh of a run could arrive before its store held the task,
	// and a model was paid to summarise an empty ask and no rows. A program's
	// run holds one task and a live stage, and its page is its conversation with
	// codeaf, so four model-written lines about that one row would say again,
	// for money, what the row already says (plandb_program.go).
	if len(family) == 0 || a.planRootIsProgram(store, rootID) {
		closeStore()
		return stored.Summary, had
	}
	questions := a.runSummaryQuestions(family)
	stamp := runSummaryStamp(family, questions)
	if had && stored.Stamp == stamp {
		closeStore()
		return stored.Summary, true
	}
	// A RUN A PERSON STOPPED BUYS NO FURTHER READING. A stop moves the run's
	// rows, and a surface asks again whenever they move, so without this every
	// stop was followed by one more model call about work the person had just
	// ended the spend on (the usage ledger on the real binary, 2026-09-19). The
	// run's own task is cancelled by nothing but a person's stop
	// ([plandb.Store.StopRoot]), so its state is the whole test, and the last
	// reading the run had stands.
	if root := store.Task(planTaskID(rootID)); root != nil && root.Status == plandb.StatusCancelled {
		closeStore()
		return stored.Summary, had
	}
	// ONE READING AT A TIME FOR ONE RUN. Two surfaces that ask in the same
	// moment (a window's own refresh and a page it just opened) both found the
	// reading stale and both paid for one; the second keeps the last reading.
	if _, busy := a.runSummaryBusy.LoadOrStore(rootID, struct{}{}); busy {
		closeStore()
		return stored.Summary, had
	}
	defer a.runSummaryBusy.Delete(rootID)
	input := runSummaryInput(family, questions, rootID, lastLook, a.summaryNow(), stored.Summary)
	closeStore()
	response, called, err := a.callRole(ctx, roles.RoleWorker, a.model, []ai.Message{
		textMessage("system", runSummaryPrompt), textMessage("user", input),
	}, ai.WithMaxTokens(320))
	// THE READING IS PAID FOR WHETHER OR NOT IT CAN BE USED, so it is banked
	// before it is read, the way every other errand's answer is (caption.go,
	// title.go). It is DETACHED: a surface asked for it, not a turn, so it must
	// not move whichever turn happens to be running ([Agent.addDetachedUsageAs]).
	// Until this line the card's lines reached the journal and nothing else — a
	// stub service that billed every call found three unbanked calls on every
	// senior-dev run.
	if err == nil && response != nil {
		a.addDetachedUsageAs(response, called, 1, string(roles.RoleWorker))
	}
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
	family = runSummaryFamily(store, rootID)
	stamp = runSummaryStamp(family, a.runSummaryQuestions(family))
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
	if store == nil {
		return storedRunSummary{}, false
	}
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

func runSummaryStamp(tasks []*plandb.Task, questions []string) string {
	body, _ := json.Marshal(currentRunSummaryShape(tasks, questions))
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func currentRunSummaryShape(tasks []*plandb.Task, questions []string) runSummaryShape {
	shape := runSummaryShape{Questions: questions}
	for _, task := range tasks {
		shape.Tasks = append(shape.Tasks, runSummaryTask{task.ID, string(task.Status)})
	}
	return shape
}

// runSummaryFamily is the run a summary is about: the root and every task that
// reaches it through its parents, in the store's own order. A CONVERSATION
// HOLDS MORE THAN ONE RUN OVER ITS LIFE, and the page tells the model that
// every statement is about a task in the list, so a row out of an earlier run
// is a sentence about the wrong work, and its movement would make this run's
// lines stale for nothing.
func runSummaryFamily(store *plandb.Store, rootID string) []*plandb.Task {
	root := store.Task(rootID)
	if root == nil {
		return nil
	}
	all := store.Tasks(plandb.Filter{Project: root.Project, Chat: root.Chat})
	parent := make(map[string]string, len(all))
	for _, task := range all {
		parent[task.ID] = task.ParentID
	}
	var family []*plandb.Task
	for _, task := range all {
		id := task.ID
		for hops := 0; id != "" && hops <= len(all); hops++ {
			if id == rootID {
				family = append(family, task)
				break
			}
			id = parent[id]
		}
	}
	return family
}

// runSummaryQuestions is what the run is holding for the person: the session's
// own open questions whose subject is a live node working one of the family's
// tasks, each by its head. The session's questions are the one source of a
// question's words; the store keeps none, and a kind invented here would read
// empty forever.
func (a *Agent) runSummaryQuestions(family []*plandb.Task) []string {
	g := a.graph()
	if g == nil || len(family) == 0 {
		return nil
	}
	inFamily := make(map[string]bool, len(family))
	for _, task := range family {
		inFamily[task.ID] = true
	}
	holds := make(map[uint64]bool)
	g.mu.Lock()
	for _, node := range g.nodes {
		if node.spec.planID != "" && inFamily[planStoreID(node.spec.planID)] {
			holds[node.id] = true
		}
	}
	g.mu.Unlock()
	var out []string
	for _, question := range a.OpenQuestions() {
		if question.Subject.Kind == SubjectNode && holds[question.Subject.ID] {
			out = append(out, summaryFirstLine(question.Head, 120))
		}
	}
	return out
}

func runSummaryInput(tasks []*plandb.Task, questions []string, rootID string, lastLook, now time.Time, previous RunPlanSummary) string {
	taskAsk := ""
	for _, task := range tasks {
		if task.ID == rootID {
			taskAsk = cutChars(task.Description, 1500)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "PERSON'S ASK\n%s\n\nRUN ROWS\n", taskAsk)
	if len(tasks) > 40 {
		tasks = tasks[:40]
	}
	for _, task := range tasks {
		fmt.Fprintf(&b, "%s · %s · %s\n", cutChars(task.Title, 120), task.Status, summaryFirstLine(task.Result, 120))
	}
	b.WriteString("\nOPEN QUESTIONS\n")
	for _, q := range questions {
		b.WriteString(q + "\n")
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

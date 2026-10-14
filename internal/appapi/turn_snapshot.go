package appapi

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A receipt retains its position between transcript entries across new turns.
type turnSavedReceipt struct {
	Entry   DisplayEntry `json:"entry"`
	AtEntry int          `json:"at_entry"`
}

func (a *AgentRegistry) turnReceipt(record *agentRecord, text string, link *SourceRef) DisplayEntry {
	at := a.now().UTC()
	entry := DisplayEntry{ID: fmt.Sprintf("receipt_%d", len(record.Receipts)), Kind: "receipt", Role: "note", Text: text, By: "you", At: &at, Link: link}
	record.Receipts = append(record.Receipts, turnSavedReceipt{Entry: entry, AtEntry: len(a.turnBaseEntries(*record)) - len(record.Receipts)})
	return entry
}

func (a *AgentRegistry) moveBrief(record agentRecord) string {
	var brief strings.Builder
	brief.WriteString("Continue this work as a task. Preserve completed work and finish the original request.\n\n")
	brief.WriteString(turnText(*record.LastTurn))
	brief.WriteString("\n\nConversation so far:\n")
	for _, entry := range a.turnEntries(record) {
		fmt.Fprintf(&brief, "%s: %s\n", entry.Role, entry.Text)
		if entry.Output != "" {
			brief.WriteString(entry.Output)
			brief.WriteByte('\n')
		}
	}
	text := brief.String()
	if len(text) > turnContextBytes {
		text = text[:turnContextBytes]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	return text
}

// The codec is the only place session vocabulary becomes the app contract.
func turnEntry(entry session.DisplayEntry, index int) DisplayEntry {
	kind := "message"
	if entry.Tool != "" {
		kind = "tool"
	} else if entry.Role == "note" || entry.Role == "aside" {
		kind = "notice"
	}
	return DisplayEntry{ID: fmt.Sprintf("entry_%d", index), Kind: kind, Role: entry.Role, Text: entry.Text, Tool: entry.Tool, CallID: entry.CallID, Args: entry.Args, Output: entry.Output, Answered: entry.Answered}
}

// shownEntry keeps the rows a person reads. A tool row with no name is the
// wire's RESULT message: its call row already carries the output, and shown
// as a message it reads as the agent dumping raw tool output. An assistant row
// with no text is the shell of a message that only called tools.
func shownEntry(entry session.DisplayEntry) bool {
	if entry.Tool != "" {
		return true
	}
	switch entry.Role {
	case "tool":
		return false
	case "assistant":
		return strings.TrimSpace(entry.Text) != ""
	}
	return true
}
func (a *AgentRegistry) turnEntries(record agentRecord) []DisplayEntry {
	merged := mergeProcessEndEntries(a.turnBaseEntries(record), a.endedProcessEntries(record.Agent.ID, record.SessionFile))
	// A session-written aside is told to the model, not the person; the live
	// stream never draws it. Job ends were swapped for their notices above.
	shown := merged[:0]
	for _, entry := range merged {
		if entry.Kind != "notice" || entry.Role != "aside" {
			shown = append(shown, entry)
		}
	}
	return shown
}
func (a *AgentRegistry) turnBaseEntries(record agentRecord) []DisplayEntry {
	saved := session.ReadTranscript(record.SessionFile)
	entries, earlier, floor := saved.Entries, saved.Earlier, saved.Floor
	if live := a.live[record.Agent.ID]; live != nil {
		entries = live.conn.Engine.Transcript()
		if door, ok := live.conn.Engine.(interface{ EarlierHistory() session.EarlierHistory }); ok {
			history := door.EarlierHistory()
			earlier, floor = history.Entries, history.Floor
		}
	}
	if len(earlier) > 0 && floor >= 0 && floor <= len(entries) {
		entries = append(append([]session.DisplayEntry{}, earlier...), entries[floor:]...)
	}
	out := make([]DisplayEntry, 0, len(entries)+len(record.Receipts))
	for i := 0; i <= len(entries); i++ {
		for _, receipt := range record.Receipts {
			if min(receipt.AtEntry, len(entries)) == i {
				out = append(out, receipt.Entry)
			}
		}
		if i < len(entries) && shownEntry(entries[i]) {
			out = append(out, turnEntry(entries[i], len(out)))
		}
	}
	return out
}

func (a *AgentRegistry) turnSnapshot(record agentRecord) AgentSnapshot {
	snapshot := AgentSnapshot{Entries: a.turnEntries(record), Queue: copyTurnQueue(record.Queue), OpenAsks: []Ask{}, Tasks: TasksResponse{Groups: []TaskGroup{}, Tasks: []Task{}}}
	live := a.live[record.Agent.ID]
	if live != nil {
		usage := live.conn.Engine.Usage()
		snapshot.Usage = Usage{In: usage.Input, Out: usage.Output, Turns: usage.Turns, Calls: usage.Calls}
		if usage.CostUSD > 0 {
			snapshot.Usage.Cost = &Money{USD: fmt.Sprintf("%.2f", usage.CostUSD)}
		}
		for _, row := range live.conn.Engine.PlanTasks() {
			state := turnTaskState(row)
			task := Task{ID: row.ID, AgentID: record.Agent.ID, ParentID: row.Parent, Title: row.Title, State: state, WaitsOn: append([]string{}, row.Waits...), Step: row.Steps, Model: row.Model, Clock: TaskClock{State: "off"}, Files: []FileChange{}, Notes: []TaskNote{}, Origin: "asked"}
			if row.USD > 0 {
				task.Spent = &Money{USD: fmt.Sprintf("%.2f", row.USD)}
			}
			snapshot.Tasks.Tasks = append(snapshot.Tasks.Tasks, task)
			snapshot.Tasks.Progress.Total++
			if state == "done" {
				snapshot.Tasks.Progress.Done++
			}
			snapshot.Tasks.Steps += row.Steps
		}
		// A5 supplies the canonical tree through its optional engine interface.
		if door, ok := live.conn.Engine.(TaskEngine); ok {
			if rows, err := door.AppTasks(); err == nil {
				snapshot.Tasks = taskTreeFromRows(record.Agent.ID, rows)
			}
		}
		if door, ok := live.conn.Engine.(interface{ OpenQuestions() []session.Question }); ok {
			for _, q := range door.OpenQuestions() {
				ask := a.questionAsk(record.Agent.ID, q, snapshot.Entries).Ask
				_ = a.asks(false, func(d *askDisk) error {
					if saved, ok := d.Asks[ask.ID]; ok {
						ask = saved.Ask
					}
					return nil
				})
				if ask.State != "waiting" {
					continue
				}
				snapshot.OpenAsks = append(snapshot.OpenAsks, ask)
			}
		}
		snapshot.Tasks.Steps += waitingSteps(live.conn.Engine)
	}
	if len(snapshot.Entries) > turnTailEntries {
		snapshot.Entries = snapshot.Entries[len(snapshot.Entries)-turnTailEntries:]
	}
	return snapshot
}
func turnTaskState(row session.PlanTaskRow) string {
	if row.Stopped {
		return "stopped"
	}
	if row.Interrupted {
		return "failed"
	}
	switch row.Status {
	case "claimed", "running":
		return "running"
	case "pending", "ready", "queued":
		if row.Hold != "" {
			return "held"
		}
		return "waiting"
	case "complete", "completed", "done":
		return "done"
	case "failed":
		return "failed"
	case "paused":
		return "paused"
	case "proposed":
		return "proposed"
	case "your_call":
		return "your_call"
	case "declined":
		return "declined"
	case "stopped":
		return "stopped"
	default:
		return "waiting"
	}
}
func turnAsk(agent string, q session.Question) Ask {
	kind := "question"
	switch q.Kind {
	case session.QuestionTask:
		kind = "proposal"
	case session.QuestionLanding, session.QuestionConflict:
		kind = "your_call"
	case session.QuestionFuel:
		kind = "budget"
	case session.QuestionStanding:
		kind = "order_ask"
	}
	switch q.Ask {
	case session.AskPermission, session.AskConfirmation:
		kind = "permission"
	case session.AskClarification:
		kind = "clarification"
	case session.AskJudgement, session.AskLanding:
		kind = "your_call"
	}
	ask := Ask{ID: askID(agent, q), Kind: kind, Source: SourceRef{Kind: "agent", ID: agent}, Path: []SourceRef{{Kind: "agent", ID: agent}}, Title: q.Head, Options: []AskOption{}, Before: []DisplayEntry{}, State: "waiting", Irreversible: q.Stakes == session.StakesIrreversible}
	for _, option := range q.Options {
		ask.Options = append(ask.Options, AskOption{ID: option.Key, Label: option.Label, Detail: option.Body})
	}
	if q.Pick != nil {
		ask.Suggested = q.Pick.Key
	}
	return ask
}

// The caller selects a recorded call id, never a filesystem path. Full results
// are permitted only from this transcript's canonical stubs directory.
func (a *AgentRegistry) turnTool(record agentRecord, callID string) (ToolResponse, error) {
	for _, entry := range a.turnEntries(record) {
		if entry.Tool == "" || entry.CallID != callID {
			continue
		}
		if !entry.Answered && entry.Output == "" {
			return ToolResponse{}, agentError(409, "conflict", "this call has no recorded result yet")
		}
		output := entry.Output
		full := false
		if strings.HasPrefix(output, "[tool:") && strings.HasSuffix(output, "]") {
			if at := strings.LastIndex(output, " · full: "); at >= 0 {
				pointer := strings.TrimSpace(output[at+len(" · full: ") : len(output)-1])
				if !filepath.IsAbs(pointer) {
					pointer = filepath.Join(record.Workspace, pointer)
				}
				root, e := filepath.EvalSymlinks(filepath.Join(filepath.Dir(record.SessionFile), "logs", "stubs"))
				if e != nil {
					return ToolResponse{}, agentError(404, "not_found", "the saved tool result is unavailable")
				}
				path, e := filepath.EvalSymlinks(pointer)
				if e != nil {
					return ToolResponse{}, agentError(404, "not_found", "the saved tool result is unavailable")
				}
				if filepath.Dir(path) != root || !turnStubName(filepath.Base(path)) {
					return ToolResponse{}, agentError(409, "not_allowed", "the saved result is outside this conversation's tool records")
				}
				f, e := os.Open(path)
				if e != nil {
					return ToolResponse{}, agentError(404, "not_found", "the saved tool result is unavailable")
				}
				defer f.Close()
				data, e := io.ReadAll(io.LimitReader(f, turnToolBytes+1))
				if e != nil {
					return ToolResponse{}, e
				}
				output = string(data)
				full = len(data) <= turnToolBytes
			}
		}
		if len(output) > turnToolBytes {
			output = output[:turnToolBytes]
			for !utf8.ValidString(output) {
				output = output[:len(output)-1]
			}
			full = false
		}
		return ToolResponse{Output: output, Full: full}, nil
	}
	return ToolResponse{}, agentError(404, "not_found", "this tool call is not in this conversation")
}
func turnStubName(name string) bool {
	if len(name) != 20 || !strings.HasSuffix(name, ".txt") {
		return false
	}
	for _, r := range name[:16] {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

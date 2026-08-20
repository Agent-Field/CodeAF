package session

// THE MODEL'S WAY BACK TO OLD WORK.
//
// The "@" mention (internal/tui3) is the person's half of this: they remember a
// task, they point at it, and a pointer block goes into the prompt. This file is
// the other half, and it exists because THE COMMON CASE IS THAT THEY DO NOT
// POINT. "Do that thing we did to the reconciler last week, but for the
// scheduler" is a sentence with no id in it, no title in it and no path in it,
// and until this tool the only honest answer a model had was to ask.
//
// It is a BELT TOOL and not a hub op, and the choice is worth stating because
// the design this implements names a hub. This build has no hub: the model
// reaches everything it can reach through bare.Tool (tools.go), and the two
// nearest neighbours of this op — `jobs`, which looks at background work, and
// `watch`, which is told about it — are both belt tools with an action-shaped
// schema. A second entry-point mechanism for one op would be a second
// vocabulary for the model to learn and a second place for this build to keep
// its wire discipline. So `tasks` sits on the belt beside `jobs`, and reads the
// same way.
//
// A NODE GETS IT SCOPED TO ITS OWN FAMILY, and never wider. A node's brief is
// its whole world by contract (task_contract.go), so a node rummaging through
// the project's history is a node reading the conversation it was deliberately
// given none of. What a node DOES have a right to is the work it handed out
// itself (task.go's fan-out law): the pieces it split off, how they are going,
// what they found, and one line into one that is going the wrong way. So inside
// a node every op here answers about its own children and about nothing else —
// the search lists them, an id outside them is refused by name, and a node that
// handed nothing out gets an empty list rather than the project's.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

const tasksDescription = "Search this project's task history, look at ONE task, say something to a task that is still running, or resolve one nobody could verify. Every piece of work handed to propose_task is here — this conversation's and every earlier one's, plus whatever is working right now. Without id it SEARCHES: a query is matched against titles, ids and outcomes, and an empty query returns the most recent tasks. With id it reads that ONE task, and for a task that is still running the answer is its LIVE state, read off the running work itself: what it is doing this second, how long it has been doing it, how many steps it has taken, what it has spent so far, and the last lines of what it has said and called. With id and say it puts your words into that running task's loop — a correction or a fact it is missing, in your own voice; its brief and its acceptance never change. With id and resolve it settles a task that needs a look: one nobody could check, which is neither done nor failed and whose dependents are waiting on somebody to decide. Every row carries two URIs: the artifact (the task's worktree or its branch) and the transcript (the task's own session journal, which the read tool opens). Use it when the person refers to earlier work without pointing at it, and when you want to know how work you handed off is actually going instead of waiting for its report."

// The schema's `resolve` enum is INTERPOLATED from [TaskResolutions] rather
// than typed out, because the landing note offers the same three words to the
// same reader (task_run.go's [settleClause]) and a hand-kept second copy is a
// copy that drifts. The prose around it still spells each verb, because a
// description is where the model learns what they mean.
var tasksSchemaJSON = `{"type":"object","properties":{` +
	`"query":{"type":"string","description":"Words to match against task titles, ids and outcomes. Omit or leave empty for the most recent tasks."},` +
	`"limit":{"type":"number","description":"How many rows to return (default: 10, maximum: 50)"},` +
	`"id":{"type":"string","description":"One task's id (\"7\") or its name (\"fix-the-nil-map-crash\"), to read that task alone instead of searching. A task that is still running answers with its live state."},` +
	`"lines":{"type":"number","description":"How many recent lines of a running task's output to return with id (default: 40, maximum: 200)"},` +
	`"say":{"type":"string","description":"A line to say to the RUNNING task named by id: a correction, or a fact it is missing. It arrives in its loop as the person's words would. Its brief and its acceptance do not change — if the objective itself was wrong, propose the work again instead. With resolve, this is read as the REASON for the decision instead."},` +
	`"resolve":{"type":"string","enum":` + TaskResolveEnum() + `,"description":"Settle the task named by id that needs a look — one nobody could check. accept takes the work as done on your reading of it and merges its branch; reaudit sends a fresh checker at the same working copy and leaves the task waiting until that answers; refute fails it and its dependents. Only ask for accept or refute on evidence you actually have — read the diff or the transcript first — and prefer reaudit when the checker simply never answered."}` +
	`},"additionalProperties":false}`

// tasksArguments is the wire form. The id is RAW because a model that has just
// read "7 · fix-the-nil-map-crash" will send either `"7"` or `7`, and both of
// them mean task seven: a schema type is a request, not a guarantee, and
// refusing the number would be this tool failing a call that named exactly what
// it meant.
type tasksArguments struct {
	Query   string          `json:"query"`
	Limit   int             `json:"limit"`
	ID      json.RawMessage `json:"id"`
	Lines   int             `json:"lines"`
	Say     string          `json:"say"`
	Resolve string          `json:"resolve"`
}

// tasksTool is the window onto the project's task index (task_index.go) AND
// onto the work that is running right now. It is the ONLY way the model reaches
// either, for [Agent.jobsTool]'s reason: one vocabulary per kind of thing.
//
// ── THREE OPS, ONE NOUN ──
//
// The tool is shaped by what the model does with it, and what it does is a
// sequence: PULL the state of work it handed off, DECIDE whether it is going
// the right way, and STEER it if it is not. Those are one conversation about
// one task, so they are one tool with one id — not a search tool, a read tool
// and a steering tool, each with its own spelling of "which task".
//
//   - no id: the search, unchanged. The person referred to earlier work and
//     pointed at nothing.
//   - id: that one task. Running, and the answer is read off the GRAPH — the
//     same live source the surface's rail draws (task_live.go) — never off the
//     index file, which by construction holds only work that is over.
//   - id and say: the person's door into a running node ([Agent.SteerTask]),
//     opened for the model. It is the same door and the same law: talk to the
//     worker, never a new target.
func (a *Agent) tasksTool() bare.Tool {
	return bare.Tool{
		Name:        "tasks",
		Description: tasksDescription,
		Schema:      json.RawMessage(tasksSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed tasksArguments
			// An absent argument object is a valid call — "what has been going
			// on" takes no arguments — so only malformed bytes are an error.
			if len(args) > 0 {
				if err := json.Unmarshal(args, &parsed); err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
			}
			token := taskToken(parsed.ID)
			if token == "" {
				if strings.TrimSpace(parsed.Say) != "" {
					return "Invalid arguments: say needs an id — it goes to one running task, not to a search", true, nil
				}
				if strings.TrimSpace(parsed.Resolve) != "" {
					return "Invalid arguments: resolve needs an id — it settles one task that needs a look, not a search", true, nil
				}
				return taskRowsTextLimit(a.taskRows(), parsed.Query, parsed.Limit), false, nil
			}
			return a.oneTask(token, parsed)
		},
	}
}

// taskToken reads the id argument back as the handle a person or a model would
// say: `7`, `"7"`, `"@fix-the-nil-map-crash"`, `"task 7"`. Everything it strips
// is decoration around a name that was already right.
func taskToken(raw json.RawMessage) string {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return ""
	}
	if strings.HasPrefix(text, `"`) {
		var quoted string
		if err := json.Unmarshal(raw, &quoted); err == nil {
			text = quoted
		}
	}
	text = strings.TrimSpace(strings.ToLower(text))
	text = strings.TrimPrefix(text, "@")
	text = strings.TrimSpace(strings.TrimPrefix(text, "task "))
	return strings.TrimSpace(text)
}

// oneTask is the id half of the tool: read that task, say something to it, or
// resolve it.
//
// THE ROW COMES FROM THE INDEX AND THE PRESENT COMES FROM THE GRAPH, and the
// join is here because they answer different halves of one question. The index
// knows every task this project ever ran, including the ones from conversations
// this process never saw; the graph knows what is happening inside the ones
// running now, and knows nothing at all about last Tuesday's. A task from an
// earlier conversation therefore reads as a row and says so, rather than being
// reported as a task with nothing happening in it.
//
// RESOLVE IS READ BEFORE SAY, and they are not two ways of doing one thing: say
// talks to a worker that is still there, resolve decides about work that is
// over. A call carrying both means the second, and `say` is its reason.
func (a *Agent) oneTask(token string, parsed tasksArguments) (string, bool, error) {
	rows := a.taskRows()
	entry, found := LookupTask(rows, token)
	if !found {
		if a.config.taskID != 0 {
			// Scoped, so the miss is a different fact: the id may well name real
			// work, and what it does not name is anything this node handed out.
			return fmt.Sprintf("No task %q among the pieces you handed out. Call tasks with no arguments to see them; work you did not ask for is not yours to read from here.", token), true, nil
		}
		return fmt.Sprintf("No task %q in this project. Call tasks with no arguments to see the most recent ones.", token), true, nil
	}
	id, here := a.thisSessionTask(entry)
	if resolution := strings.TrimSpace(parsed.Resolve); resolution != "" {
		return a.resolveOneTask(entry, id, here, TaskResolution(strings.ToLower(resolution)), parsed.Say)
	}
	if say := strings.TrimSpace(parsed.Say); say != "" {
		if !here {
			return fmt.Sprintf("Task %s ran in an earlier conversation, so there is nobody left to say it to. Propose the work again if it needs doing differently.", entry.ID), true, nil
		}
		if err := a.SteerTask(id, say); err != nil {
			return capitalized(err.Error()) + ".", true, nil
		}
		return fmt.Sprintf("said to task %s: %s\nIt arrives in its loop as the person's own words. Its brief and its acceptance are unchanged — they were frozen when it started.", entry.ID, say), false, nil
	}
	if !here {
		// Its row, and the truth about why there is no more: the graph that ran
		// it died with the conversation that owned it, and its transcript is the
		// whole of what is left.
		if entry.Live() {
			return taskRowText(entry) + "\nIt was running when the conversation that owns it ended, so this session cannot see it work. Its transcript is the whole of it.\n", false, nil
		}
		return taskRowText(entry), false, nil
	}
	live, state, ok := a.taskLiveRead(id, taskTailLines(parsed.Lines))
	if !ok {
		return taskRowText(entry), false, nil
	}
	return taskLiveText(live, state), false, nil
}

// resolveOneTask is the resolve verb: the model settling a node whose auditor
// never gave a verdict ([Agent.ResolveUnverified]).
//
// IT IS THIS SESSION'S GRAPH OR NOTHING, exactly as steering is. An unverified
// node from a conversation that has ended is a row in the index and a branch in
// the repository; there is no graph left to move it in, no dependents left
// waiting on it, and telling the model it had settled something would be this
// tool inventing an effect it did not have.
func (a *Agent) resolveOneTask(entry TaskIndexEntry, id uint64, here bool, resolution TaskResolution, why string) (string, bool, error) {
	if !here {
		return fmt.Sprintf("Task %s ran in an earlier conversation, so there is no graph left to settle it in. Its work is at %s — read it, and propose what still needs doing.",
			entry.ID, taskWhereWord(entry)), true, nil
	}
	if err := a.ResolveUnverified(id, resolution, why); err != nil {
		return capitalized(err.Error()) + ".", true, nil
	}
	switch resolution {
	case TaskAccept:
		return fmt.Sprintf("accepted task %s as done on your reading of it, with nobody else's check behind it. Its branch has come home and anything waiting on it can start.", entry.ID), false, nil
	case TaskRefute:
		return fmt.Sprintf("refuted task %s. It is failed, its branch is kept, and anything waiting on it fails with it.", entry.ID), false, nil
	default:
		return fmt.Sprintf("a fresh checker is looking at task %s again. It stays waiting until that answer lands, and you will hear the outcome the way every other task lands.", entry.ID), false, nil
	}
}

// taskWhereWord names where a task's work is, for a reader who has just been
// told this session cannot touch it: the artifact if the row has one, else the
// transcript, else nothing worth pointing at.
func taskWhereWord(entry TaskIndexEntry) string {
	if entry.ArtifactURI != "" {
		return entry.ArtifactURI
	}
	if entry.TranscriptURI != "" {
		return entry.TranscriptURI
	}
	return "nowhere this session can point at"
}

// taskRows is what this tool answers from: the project's whole index in a
// conversation, and in a node ONLY THE CHILDREN IT HANDED OUT ITSELF.
//
// The scope is applied here, at the one door, rather than in each of the three
// ops: a search, a read, a steer and a resolve are four things to do with a
// task, and "which tasks can this agent see" is one answer for all of them.
func (a *Agent) taskRows() []TaskIndexEntry {
	parent := a.config.taskID
	if parent == 0 {
		return a.TaskIndex()
	}
	a.mu.Lock()
	session := a.sessionID()
	a.mu.Unlock()
	kids := a.tasker().children(parent)
	rows := make([]TaskIndexEntry, 0, len(kids))
	for _, kid := range kids {
		kid.graph.mu.Lock()
		rows = append(rows, kid.indexEntryLocked(session))
		kid.graph.mu.Unlock()
	}
	sortTaskIndex(rows)
	return rows
}

// thisSessionTask resolves a row to a node of THIS session's graph. A row from
// an earlier conversation, or one whose id this graph never held, is not one:
// ids restart with every conversation (see [TaskIndexEntry.ID]), so the pair is
// what identifies a node and matching on the number alone would read a live
// task 7 as last month's task 7.
func (a *Agent) thisSessionTask(entry TaskIndexEntry) (uint64, bool) {
	a.mu.Lock()
	session := a.sessionID()
	a.mu.Unlock()
	if entry.SessionID != session {
		return 0, false
	}
	id, err := strconv.ParseUint(strings.TrimSpace(entry.ID), 10, 64)
	if err != nil || a.taskNode(id) == nil {
		return 0, false
	}
	return id, true
}

// taskTailLines bounds one live read's tail, clamping rather than refusing for
// [parseWatchArguments]'s reason: a model asking for a thousand lines means "as
// much as I can have", and the nearest legal answer is what it meant.
func taskTailLines(asked int) int {
	switch {
	case asked <= 0:
		return taskLiveDefaultTail
	case asked > taskLiveMaxTail:
		return taskLiveMaxTail
	default:
		return asked
	}
}

// taskLiveText is one running task, in full: its row, then the tail of what it
// has been doing, then the one thing the reader can do about it.
//
//	7 · fix-the-nil-map-crash · running · running for 4m 12s · 2 files · $0.31
//	  Fix the nil-map crash in the reconciler
//	  live: bash go test ./internal/reconciler/… · 12s
//	  artifact file:///…/task-7 · transcript file:///…/20260816-101500_7.jsonl
//
//	its last 4 lines:
//	· read internal/reconciler/state.go
//	The map is written without the guard; I will add it and a test.
//	· edit internal/reconciler/state.go
//	· bash go test ./internal/reconciler/…
//
//	Steer it with tasks id 7 say "…". The whole story is in its transcript.
//
// THE TAIL IS NOT THE WORK. It is the last few lines of a node's narrative and
// its calls — enough to tell working from circling — and the transcript URI in
// the row above it is where the rest is. A tool that answered with everything a
// node had done would put a session's worth of somebody else's greps into a
// context window over the question "how is it going".
func taskLiveText(entry TaskIndexEntry, state taskLiveState) string {
	out := taskRowText(entry)
	if entry.Status != string(TaskRunning) {
		// Either it landed between the model deciding to ask and the read
		// happening — the row carries the outcome and the URIs, which is the
		// whole answer — or it has not started, and a node with no worker in it
		// has nothing to quote and nobody to steer.
		if entry.Status == string(TaskQueued) {
			return out + "\nIt has not started: it is waiting on the work it depends on, or on a slot.\n"
		}
		return out
	}
	var rendered strings.Builder
	rendered.WriteString(out)
	if len(state.Lines) == 0 {
		rendered.WriteString("\nIt has not said anything yet.\n")
	} else {
		fmt.Fprintf(&rendered, "\nits last %s:\n", countedLines(len(state.Lines)))
		for _, line := range state.Lines {
			rendered.WriteString(line + "\n")
		}
	}
	fmt.Fprintf(&rendered, "\nSteer it with tasks id %s say \"…\". The whole story is in its transcript.\n", entry.ID)
	return rendered.String()
}

// capitalized starts a sentence the way the rest of this tool's answers start.
// The doors in task_room.go phrase their refusals for a caller, lower case; a
// tool result is read as prose.
func capitalized(text string) string {
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:]
}

// taskRowsText is the answer, and it is written for a reader that has to decide
// what to open next.
//
// ROOTS KEEP THEIR FULL ROW. Children sit under their root in one line because
// the root's transcript names the family; a queried child also carries its own
// URI so the caller can go straight to the matching work. With no query a
// family is collapsed to its node count.
func taskRowsText(rows []TaskIndexEntry, query string) string {
	return taskRowsTextLimit(rows, query, taskSearchLimit)
}

func taskRowsTextLimit(rows []TaskIndexEntry, query string, limit int) string {
	query = strings.TrimSpace(query)
	matches := SearchTaskIndex(rows, query, limit)
	if len(matches) == 0 {
		if strings.TrimSpace(query) == "" {
			return "No tasks have run in this project yet."
		}
		return fmt.Sprintf("No task matches %q. Try fewer words, or call tasks with no query to see the most recent ones.", strings.TrimSpace(query))
	}
	byKey := make(map[string]TaskIndexEntry, len(rows))
	children := make(map[string][]TaskIndexEntry)
	for _, entry := range rows {
		key := taskFamilyKey(entry.SessionID, entry.ID)
		byKey[key] = entry
		if entry.Parent != "" {
			parent := taskFamilyKey(entry.SessionID, entry.Parent)
			children[parent] = append(children[parent], entry)
		}
	}

	// A query returns matching rows, but a matching child is read in the family
	// it belongs to. Roots are inserted once, immediately before their hits.
	var display []TaskIndexEntry
	seen := make(map[string]bool, len(matches))
	for _, entry := range matches {
		if entry.Parent != "" {
			parentKey := taskFamilyKey(entry.SessionID, entry.Parent)
			if root, ok := byKey[parentKey]; ok {
				if !seen[parentKey] {
					display = append(display, root)
					seen[parentKey] = true
				}
				if query == "" {
					continue
				}
			}
		}
		key := taskFamilyKey(entry.SessionID, entry.ID)
		if !seen[key] {
			display = append(display, entry)
			seen[key] = true
		}
	}

	var out strings.Builder
	for at, entry := range display {
		if at > 0 {
			out.WriteString("\n")
		}
		_, parentPresent := byKey[taskFamilyKey(entry.SessionID, entry.Parent)]
		if entry.Parent != "" && parentPresent {
			out.WriteString(taskChildRowText(entry, query != ""))
			continue
		}
		out.WriteString(taskRowText(entry))
		if query == "" {
			if count := len(children[taskFamilyKey(entry.SessionID, entry.ID)]); count > 0 {
				fmt.Fprintf(&out, "  … %s under it — tasks %s for the family\n", taskNodeCount(count), entry.ID)
			}
		}
	}
	return out.String()
}

func taskFamilyKey(session, id string) string { return session + "\x00" + id }

func taskNodeCount(count int) string {
	if count == 1 {
		return "1 node"
	}
	return strconv.Itoa(count) + " nodes"
}

func taskChildRowText(entry TaskIndexEntry, withURI bool) string {
	parts := []string{entry.Title, entry.Status}
	if word := taskWhenWord(entry); word != "" {
		parts = append(parts, word)
	}
	out := "  " + strings.Join(parts, " · ") + "\n"
	if withURI {
		var where []string
		if entry.ArtifactURI != "" {
			where = append(where, "artifact "+entry.ArtifactURI)
		}
		if entry.TranscriptURI != "" {
			where = append(where, "transcript "+entry.TranscriptURI)
		}
		if len(where) > 0 {
			out += "    " + strings.Join(where, " · ") + "\n"
		}
	}
	return out
}

// taskRowText is one task, in the shape a person would read out.
//
//	7 · fix-the-nil-map-crash · done · ended 3h ago · 4 files · 2m 10s · $0.31
//	  Added the guard and the regression test; the parser suite passes.
//	  artifact git:task/fix-the-nil-map-crash-9c1a2f · transcript file:///…/20260816-101500_7.jsonl
//
// A RUNNING ROW CARRIES ONE MORE LINE — what it is doing right now — and no
// landed row ever does: see [TaskIndexEntry.Activity].
//
// Every clause that has nothing to say is DROPPED rather than written empty. A
// row reading "· 0 files · · $0.00" is three facts this build does not have,
// stated as though it did.
func taskRowText(entry TaskIndexEntry) string {
	parts := []string{entry.ID, entry.Name, entry.Status}
	if word := taskWhenWord(entry); word != "" {
		parts = append(parts, word)
	}
	if entry.FilesChanged > 0 {
		parts = append(parts, taskFilesWord(entry.FilesChanged))
	}
	if entry.DurationMS > 0 {
		parts = append(parts, taskSpanWord(entry.Duration()))
	}
	if entry.Cost > 0 {
		parts = append(parts, "$"+strconv.FormatFloat(entry.Cost, 'f', 2, 64))
	}
	out := strings.Join(parts, " · ") + "\n  " + entry.Title
	if entry.Outcome != "" {
		out += "\n  " + entry.Outcome
	}
	// A RUNNING ROW SAYS WHAT IS HAPPENING IN IT, in the place a landed row says
	// what came of it. "running for 4m" is equally true of a node calling a tool
	// every second and of one stuck on the same `go test` since minute one, and
	// a model deciding whether to leave work alone cannot tell those apart from
	// a clock (task_live.go).
	if entry.Activity != "" {
		out += "\n  live: " + entry.Activity
	}
	var where []string
	if entry.ArtifactURI != "" {
		where = append(where, "artifact "+entry.ArtifactURI)
	}
	if entry.TranscriptURI != "" {
		where = append(where, "transcript "+entry.TranscriptURI)
	}
	if len(where) > 0 {
		out += "\n  " + strings.Join(where, " · ")
	}
	return out + "\n"
}

// taskWhenWord says when, in the tense the row is in: a live task has been
// running for a while, a landed one ended a while ago.
func taskWhenWord(entry TaskIndexEntry) string {
	if entry.Live() {
		// A QUEUED NODE HAS NO CLOCK YET. Its age is zero because it has not
		// started, and "running" beside a status that says "queued" is the row
		// contradicting itself in the space of four words.
		if entry.Status == string(TaskQueued) {
			return ""
		}
		if span := taskSpanWord(entry.Duration()); span != "" {
			return "running for " + span
		}
		return "running"
	}
	if entry.EndedAt.IsZero() {
		return ""
	}
	return "ended " + TaskAgeWord(time.Since(entry.EndedAt)) + " ago"
}

func taskFilesWord(n int) string {
	if n == 1 {
		return "1 file"
	}
	return strconv.Itoa(n) + " files"
}

// taskSpanWord spells how long something took: seconds, then minutes and
// seconds, then hours and minutes. It is [countUpWord]'s grammar in
// internal/tui3, restated here because that one is a surface's and this one goes
// on the wire.
func taskSpanWord(d time.Duration) string {
	switch {
	case d <= 0:
		return ""
	case d < time.Minute:
		return strconv.Itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m " + strconv.Itoa(int(d%time.Minute/time.Second)) + "s"
	default:
		return strconv.Itoa(int(d/time.Hour)) + "h " + strconv.Itoa(int(d%time.Hour/time.Minute)) + "m"
	}
}

// TaskAgeWord spells how long ago, coarsely, in the one unit that matters at
// that distance. It is exported because the "@" drop-up and the pointer block
// (internal/tui3) say the same ages about the same rows, and two spellings of
// "3h" on one screen is two vocabularies for one fact.
func TaskAgeWord(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "moments"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	case d < 30*24*time.Hour:
		return strconv.Itoa(int(d/(24*time.Hour))) + "d"
	default:
		return strconv.Itoa(int(d/(30*24*time.Hour))) + "mo"
	}
}

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
// A NODE DOES NOT GET IT. A task's own agent has a journal under
// ~/.aforge/v3/tasks/<session>/, not under the project's session directory, so
// the index it would read is the empty one beside its own transcript — and,
// more to the point, a node's brief is its whole world by contract
// (task_contract.go). A node rummaging through the project's history is a node
// reading the conversation it was deliberately given none of.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

const tasksDescription = "Search this project's task history — every piece of work handed to propose_task, in this conversation and in every earlier one, plus whatever is running right now. Matches a query against titles, ids and outcomes; an empty query returns the most recent tasks. Each row carries the id, the outcome, what it cost and two URIs: the artifact (the task's worktree or its branch) and the transcript (the task's own session journal, which the read tool opens). Use it when the person refers to earlier work without pointing at it — search first, then read the URI for the detail."

const tasksSchemaJSON = `{"type":"object","properties":{"query":{"type":"string","description":"Words to match against task titles, ids and outcomes. Omit or leave empty for the most recent tasks."},"limit":{"type":"number","description":"How many rows to return (default: 10, maximum: 50)"}},"additionalProperties":false}`

// tasksTool is the window onto the project's task index (task_index.go). It is
// the ONLY way the model reaches it, for [Agent.jobsTool]'s reason: one
// vocabulary per kind of thing.
func (a *Agent) tasksTool() bare.Tool {
	return bare.Tool{
		Name:        "tasks",
		Description: tasksDescription,
		Schema:      json.RawMessage(tasksSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			// An absent argument object is a valid call — "what has been going
			// on" takes no arguments — so only malformed bytes are an error.
			if len(args) > 0 {
				if err := json.Unmarshal(args, &parsed); err != nil {
					return "Invalid arguments: " + err.Error(), true, nil
				}
			}
			rows := SearchTaskIndex(a.TaskIndex(), parsed.Query, parsed.Limit)
			return taskRowsText(rows, parsed.Query), false, nil
		},
	}
}

// taskRowsText is the answer, and it is written for a reader that has to decide
// what to open next.
//
// THREE LINES PER TASK, and the third is the pair of URIs. The row says what the
// work was and what it came to; the URIs say where to go for the rest. That
// split is the whole discipline of this index — a tool that returned reports in
// full would put a session's worth of task prose into a context window over one
// question about last Tuesday.
func taskRowsText(rows []TaskIndexEntry, query string) string {
	if len(rows) == 0 {
		if strings.TrimSpace(query) == "" {
			return "No tasks have run in this project yet."
		}
		return fmt.Sprintf("No task matches %q. Try fewer words, or call tasks with no query to see the most recent ones.", strings.TrimSpace(query))
	}
	var out strings.Builder
	for at, entry := range rows {
		if at > 0 {
			out.WriteString("\n")
		}
		out.WriteString(taskRowText(entry))
	}
	return out.String()
}

// taskRowText is one task, in the shape a person would read out.
//
//	7 · fix-the-nil-map-crash · done · ended 3h ago · 4 files · 2m 10s · $0.31
//	  Added the guard and the regression test; the parser suite passes.
//	  artifact git:task/fix-the-nil-map-crash-9c1a2f · transcript file:///…/20260816-101500_7.jsonl
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

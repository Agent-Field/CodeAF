package main

// ── AN ORGANIZATION WORKSTREAM'S AGENT, RUN BY `codeaf factory serve` ──────
//
// internal/factory/serve keeps the server agent's conversation, its state,
// who may talk to it and what it spent (workstream_agent.go) and never calls a
// model. This is the turn itself: the engine's own conversation machinery
// (internal/session), run with the organization's key and one door onto the
// workstream, so the server's agent is the same agent a laptop runs, with a
// belt of exactly one tool (session's tools_workstream.go).
//
// ITS PROFILE IS ITS OWN FOLDER, NEVER THE SERVER USER'S. A conversation reads
// keys, memory and settings from its profile; the server's agent is given an
// empty one under the workstream's folder, the organization's key by hand, and
// ONE MODEL for every call, so nothing it does can reach a key or a memory that
// belongs to whoever runs the server.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

// factoryWorkstreamAgentBase is where the runner's model API is: tests point
// it at a fake; otherwise it is CODEAF_BASE_URL when the server's process was
// started with one (an operator's own proxy in front of OpenRouter), else
// OpenRouter's own address, the one provider an organization key is kept
// for. It is never empty: the provider client refuses an empty base URL, so
// an unset variable once failed every server turn with "provider base URL is
// required" (film defect D9). The server profile's own base_url is NOT read: it belongs
// to whoever signs in to the machine, and the organization's key must never
// be sent somewhere that profile chose.
var factoryWorkstreamAgentBase = ""

func factoryWorkstreamAgentBaseURL() string {
	if factoryWorkstreamAgentBase != "" {
		return factoryWorkstreamAgentBase
	}
	if base := strings.TrimSpace(env.Get("CODEAF_BASE_URL")); base != "" {
		return base
	}
	return config.DefaultBaseURL
}

// factoryWorkstreamAgent answers one message to a workstream's server agent.
func factoryWorkstreamAgent(st *store.Store, load func() (config.Config, error)) serve.AgentRunner {
	return func(ctx context.Context, turn serve.AgentTurn) (serve.AgentTurnEnd, error) {
		profile := filepath.Join(turn.Dir, "profile")
		cfg, err := applyV3Governance(session.Config{
			Workspace:   filepath.Join(turn.Dir, "work"),
			SessionFile: filepath.Join(turn.Dir, "talk.jsonl"),
			ProfileDir:  profile,
			Model:       factoryWorkstreamAgentModel(st, load, profile),
			APIKey:      turn.Key, BaseURL: factoryWorkstreamAgentBaseURL(),
			DeterministicNames: true, CompactEnabled: true,
			System:     factoryWorkstreamAgentTurnSystem(turn),
			Workstream: turn.Door,
		}, profile, true, true)
		// THE GATE IS THE DOOR, NOT AN APPROVAL ASK. Nobody sits in front of
		// the server to answer one, and the belt's one tool checks on every
		// write that the person being answered may change the workstream; so
		// the posture is allow (yolo), and ONE MODEL, as the folder's empty
		// profile would have it anyway.
		if err != nil {
			return serve.AgentTurnEnd{}, err
		}
		agent, err := session.New(cfg)
		if err != nil {
			return serve.AgentTurnEnd{}, err
		}
		defer agent.Close()
		before := agent.Usage().CostUSD
		events, err := agent.Submit(ctx, turn.Text)
		if err != nil {
			return serve.AgentTurnEnd{}, err
		}
		// A stop is the context ending; the conversation is interrupted the
		// way a person's stop interrupts it, and its stream then ends.
		stopped := make(chan struct{})
		defer close(stopped)
		guard.Go("factory workstream agent stop", func() {
			select {
			case <-ctx.Done():
				agent.Interrupt()
			case <-stopped:
			}
		})
		var said strings.Builder
		say := func() {
			if text := strings.TrimSpace(said.String()); text != "" {
				turn.Step(api.AgentEntryAgent, text, "", false)
			}
			said.Reset()
		}
		var failed error
		for e := range events {
			switch e.Kind {
			case session.EventTextDelta:
				said.WriteString(e.Text)
			case session.EventToolBegin:
				say()
			case session.EventToolEnd:
				turn.Step(api.AgentEntryStep, factoryAgentStepWords(e), e.Tool, false)
			case session.EventToolFailed:
				turn.Step(api.AgentEntryStep, factoryAgentStepWords(e), e.Tool, true)
			case session.EventError:
				if e.Err != nil {
					failed = e.Err
				}
			}
		}
		say()
		usd := max(agent.Usage().CostUSD-before, 0)
		if ctx.Err() != nil {
			return serve.AgentTurnEnd{USD: usd}, ctx.Err()
		}
		return serve.AgentTurnEnd{USD: usd}, failed
	}
}

// factoryAgentStepWords is one step as a person reads it ("read the
// workstream", "made the page Retry runbook"), never the call's arguments
// whole: a page's words are the page's, not the conversation's.
func factoryAgentStepWords(e session.Event) string {
	var args struct {
		Ask   string `json:"ask"`
		Title string `json:"title"`
	}
	if e.Tool == session.WorkstreamPagesTool && json.Unmarshal([]byte(e.Args), &args) == nil {
		switch args.Ask {
		case "read":
			return "read the workstream"
		case "page":
			return "read a page"
		case "write":
			return "wrote a page"
		case "new":
			if title := strings.TrimSpace(args.Title); title != "" {
				return "made the page " + title
			}
			return "made a page"
		case "decide":
			return "suggested a decision"
		}
	}
	if words, ok := factoryAgentWorkWords(e); ok {
		return words
	}
	if hint := strings.TrimSpace(e.Hint); hint != "" {
		return hint
	}
	return e.Tool
}

// factoryAgentWorkWords is a ws_* step as a person reads it. A tool's own
// hint names its first field, which for most of these is an id (tk_…, fl_…),
// and an id is never a word a person reads.
func factoryAgentWorkWords(e session.Event) (string, bool) {
	var args struct {
		Task  string `json:"task"`
		Title string `json:"title"`
		Name  string `json:"name"`
		Kind  string `json:"kind"`
		File  string `json:"file"`
	}
	_ = json.Unmarshal([]byte(e.Args), &args)
	named := func(what, name string) string {
		if name = strings.TrimSpace(name); name != "" {
			return what + " " + name
		}
		return what
	}
	switch e.Tool {
	case session.WSTasksTool:
		if args.Task != "" {
			return "read a task", true
		}
		return "read the tasks", true
	case session.WSTaskCreateTool:
		return named("made the task", args.Title), true
	case session.WSTaskUpdateTool:
		return "changed a task", true
	case session.WSTaskCommentTool:
		switch args.Kind {
		case "offer":
			return "offered help on a task", true
		case "request_note":
			return "noted the request on a task", true
		}
		return "wrote on a task", true
	case session.WSFilesTool:
		return "read the files", true
	case session.WSFileReadTool:
		return "read a file", true
	case session.WSFileCreateTool:
		if args.File != "" {
			return "rewrote a document", true
		}
		what := "made the document"
		switch args.Kind {
		case "folder":
			what = "made the folder"
		case "link":
			what = "added the link"
		}
		return named(what, args.Name), true
	case session.WSFileSuggestTool:
		return "suggested a change to a document", true
	}
	return "", false
}

// factoryWorkstreamAgentModel is the model the server's agent answers on: the
// factory's default model, else this server's own model, else the bottom rung
// every headless seat stands on, read for the agent's own profile.
func factoryWorkstreamAgentModel(st *store.Store, load func() (config.Config, error), profileDir string) string {
	if st != nil {
		if spending, err := st.Spending(); err == nil && strings.TrimSpace(spending.DefaultModel) != "" {
			return spending.DefaultModel
		}
	}
	if load != nil {
		if settings, err := load(); err == nil && strings.TrimSpace(settings.Model) != "" {
			return settings.Model
		}
	}
	return config.ChatDefaultAt(profileDir)
}

// factoryWorkstreamAgentSystem is what the server's agent is told it is.
// workstreamDoorKeepsWork says the server's door also keeps the workstream's
// tasks and files, so its agent carries the ws_* tools beside workstream_pages.
func workstreamDoorKeepsWork(door serve.AgentDoor) bool {
	_, ok := door.(session.WorkstreamWorkDoor)
	return ok
}

// factoryWorkstreamAgentTurnSystem is the prompt for one turn: a person's
// message, or one task Afi was started on.
func factoryWorkstreamAgentTurnSystem(turn serve.AgentTurn) string {
	system := factoryWorkstreamAgentSystem(turn.Name, turn.Asker, workstreamDoorKeepsWork(turn.Door), turn.Now, turn.TZ)
	if turn.Task == nil {
		return system
	}
	return system + "\n\n" + factoryWorkstreamAgentTaskRun(turn.Task.ID, turn.Task.Title, turn.Asker)
}

// factoryWorkstreamAgentTaskRun turns the prompt from a conversation into one
// task's work. Nobody reads a task run as a chat, so the only things a person
// ever sees of it are what it does to the task: results, a line in its
// thread, and its state. A run that ends without setting done or failed is
// left failed by the server.
func factoryWorkstreamAgentTaskRun(id, title, starter string) string {
	return "This time nobody is chatting with you: " + starter + " started you on one task, \"" + title + "\" (its id, for your tools only: " + id + "). " +
		"The message below is what the task asks. Do the work with your tools. Nobody reads what you say here; people see only what you do to the task. " +
		"Put anything you write in Files with ws_file_create and name it in the task's results. " +
		"When the work is done, post one plain line saying what you did with ws_task_comment, then set the task done with ws_task_update. " +
		"If a person must choose before you can go on, raise an Ask with ws_task_update and stop. " +
		"If you cannot do it, set the task failed with ws_task_update and a state_reason saying why in plain words."
}

func factoryWorkstreamAgentSystem(name, asker string, work bool, now time.Time, tz string) string {
	tools := "You have one tool, " + session.WorkstreamPagesTool + ": "
	if work {
		tools = "You have " + session.WorkstreamPagesTool + " and the workstream's task and file tools (ws_tasks, ws_task_create, ws_task_update, ws_task_comment, ws_files, ws_file_read, ws_file_create, ws_file_suggest). " +
			"Work is kept as tasks: when someone asks for work to be done, make a task with ws_task_create, never a page titled like a task; " +
			"before you answer who is doing what or what is waiting on someone, read ws_tasks. With " + session.WorkstreamPagesTool + " you "
	}
	system := "You are Afi, the assistant of the organization workstream \"" + name + "\", running on the organization's server. " +
		factoryWorkstreamAgentAudience + factoryWorkstreamAgentAsker(asker) + " " + factoryWorkstreamAgentToday(now, tz) + "\n\n" +
		tools + "read the workstream (ask read) before you answer about it, " +
		"read one page whole (ask page), write a page (ask write), or make a new page (ask new). " +
		"Decisions record themselves when an Ask is answered; never propose one or tell anyone to keep one, since there is no such step. " +
		"You write as the person whose message you are answering. You rewrite only a page you made that no person has edited; " +
		"your text for any other page waits as a suggestion for someone to take, and the tool's answer says so: tell the person plainly.\n\n" +
		factoryWorkstreamAgentChanges + "\n\n" +
		"You cannot run commands, read or change a repository, open files on a computer, start Factory work or use the web. " +
		"When a person asks for that, say that you cannot here, and say what in the workstream you can change.\n\n" +
		factoryWorkstreamAgentWords + "\n\n"
	if work {
		system += session.WorkstreamWorkRules + "\n\n"
	}
	return system + "Answer briefly, in plain words."
}

// factoryWorkstreamAgentToday tells the agent the date and time where the
// person is. The model has no clock, and without this line it took a date it
// read in a page for today and reckoned every deadline from it (L3 F-17). The
// time zone is the one the person's app sent; with none, or one this machine
// cannot load, it is the server's own, and the line says which it is.
func factoryWorkstreamAgentToday(now time.Time, tz string) string {
	if now.IsZero() {
		now = time.Now()
	}
	where := "the server's time"
	if loc, err := time.LoadLocation(tz); tz != "" && err == nil {
		now, where = now.In(loc), "the person's time, "+tz
	} else {
		now = now.Local()
	}
	return "It is now " + now.Format("Monday 2 January 2006, 15:04 MST") + " (" + where + "); reckon today, tomorrow and every deadline from that date, never from a date you read in a page."
}

// factoryWorkstreamAgentChanges is the rule for what a change says about
// itself. In L3 F-10 the agent was asked for one edit, also moved a rating
// from red to yellow, and reported only the edit.
const factoryWorkstreamAgentChanges = "Change only what the person asked you to change. " +
	"When you have changed something, list every change you made, each one, including any status, rating, owner or date you changed along the way; " +
	"never report fewer changes than you made."

// factoryWorkstreamAgentAudience says who reads this conversation.
// Each person has their own conversation with the server's agent (L1 F2).
const factoryWorkstreamAgentAudience = "This conversation is private to the one person you are talking to: nobody else in the workstream reads it, and you never repeat to them what another person said to you. " +
	"What you put in tasks, files, pages and decisions is shared with everyone in the workstream."

// factoryWorkstreamAgentWords is how the agent speaks to a person. THE
// MACHINERY VOCABULARY LAW (CLAUDE.md) holds for what the model says, not only
// for what the screens draw: the server answers a workstream's stage and its
// ids, and a model that reads them repeats them. TestServerAgentPromptKeepsMachineryOut
// pins the banned words out of the rest of the prompt.
const factoryWorkstreamAgentWords = "How you speak to people: name tasks, pages, files and people by their titles and names, never by an id " +
	"(ids such as tk_…, pg_…, fl_… and ws_… are for your tools only, and never appear in your answer). " +
	"Say a task is up next, running, waiting on someone, done or failed, and call Factory work a task. " +
	"Never describe where the workstream is by a phase or a step of a process; say what is done, what is running and what waits on whom."

// factoryWorkstreamAgentAsker names the person this conversation is with, so
// the one line a private request leaves on a task can say who asked.
func factoryWorkstreamAgentAsker(asker string) string {
	if asker = strings.TrimSpace(asker); asker == "" {
		return ""
	}
	return " You are talking to " + asker + "."
}

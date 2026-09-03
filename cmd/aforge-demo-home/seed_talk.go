package main

// seed_talk.go is the twelve conversations: their folders, their journals, the
// identity file a picker reads instead of the journal, the presence files the
// two live ones keep, and the copy of every turn that goes into the searchable
// index.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
)

// demoTurn is one exchange: what the person said and what came back.
type demoTurn struct{ said, answered string }

// demoTalk is one conversation on the demo machine.
type demoTalk struct {
	project string
	title   string
	// ago is how long since the PERSON last spoke in it, which is the ordering
	// law everywhere in this codebase (internal/session's Meta.LastUserAt).
	ago      time.Duration
	turns    []demoTurn
	spent    float64
	tokens   int
	model    string
	archived bool
	// asking is the question this conversation is stopped on. A conversation
	// with one is live and says `waiting on you`, which is what puts it at the
	// top of home and draws the `1 want you` clause on the tab bar.
	asking string
	// running is the work this conversation has out right now. It joins the
	// project's index on (session id, task id), and BOTH HALVES ARE NEEDED: a
	// `running` row in the index is only believed when a fresh presence file
	// names that very node (internal/session's world.go).
	running *session.PresenceTask
}

var demoConversations = []demoTalk{
	{
		project: firstProjectName, title: "The Tab Bar's Counts", ago: 40 * time.Minute,
		spent: 1.12, tokens: 84_000, model: "anthropic/claude-opus-4.1",
		asking: "run the sweep again so the report can be regenerated?",
		turns: []demoTurn{
			{"the tab bar's counts disagree with the list underneath it", "The bar and the list were taking two readings of the world a few hundred milliseconds apart. One reading, shared by every place, is the fix."},
			{"which of the two folds should stay open when I come back?", "The quiet fold, on the argument that it is the one somebody deliberately closed. I would rather ask than guess — may I regenerate the report to check?"},
		},
	},
	{
		project: firstProjectName, title: "Porting the Picker", ago: 2 * time.Minute,
		spent: 0.44, tokens: 31_500, model: "anthropic/claude-sonnet-4",
		turns: []demoTurn{
			{"port the picker onto the new list widget", "Done — it walks the same rows and keeps its own cursor, so nothing about the keys changed."},
			{"does it still fold the quiet ones?", "Yes, and the fold line is the list's rather than the picker's now, so the two cannot drift."},
		},
	},
	// THE CONVERSATION THE WORK HANGS OFF. Its own folder holds a checkpointed
	// task graph and three background jobs (seed_room.go), which is what puts
	// anything at all on the jobs section, a job's page and a task room — three
	// surfaces this fixture used to leave completely empty.
	{
		project: firstProjectName, title: roomTalkTitle, ago: time.Minute,
		spent: 1.07, tokens: 79_400, model: "anthropic/claude-opus-4.1",
		turns: []demoTurn{
			{"the frame budget is over the cap in four packages — take it back under, one package at a time", "Measured all eleven first so the order is a fact rather than a guess. The tab bar is under; the row fitter wants your eye before it lands, and the two behind it are parked on that."},
			{"what are the jobs doing?", "The dev server was still up when this window last closed, the tui3 suite came back clean, and `make check` stopped on a vet error in tasksplace.go — its log has the line."},
		},
	},
	// THE HOSTILE NAME. Wide CJK cells, an emoji, and an `e` with a COMBINING
	// acute after it — three different ways a rune's width is not one — in a
	// title that every list, tab and header on the surface has to fit. Nothing in
	// this fixture used to have one, so nothing on the demo could show what the
	// surface does when a cell count and a rune count disagree.
	{
		project: "pricing-site", title: "国際化とレイアウト幅 🌏 the café pricing page", ago: 90 * time.Minute,
		spent: 0.39, tokens: 29_600, model: "anthropic/claude-sonnet-4",
		turns: []demoTurn{
			{"the japanese pricing page wraps in the middle of a word — 全角の幅が合っていない", "The column was counting runes where it should count cells, so every full-width character was costing one instead of two. Counting cells fixes the wrap and the ellipsis together."},
			{"and the emoji in the header? 🌏", "Same fault, same fix — it is two cells wide and was budgeted as one."},
		},
	},
	{
		project: firstProjectName, title: "Why the Frame Jumps", ago: 3 * time.Hour,
		spent: 0.83, tokens: 62_000, model: "anthropic/claude-opus-4.1",
		turns: []demoTurn{
			{"the frame jumps sideways every time the card opens", "The card is measured before the list is folded, so the list is laid out twice at two widths. Measuring the fold first settles it."},
			{"show me where that measurement happens", "internal/tui3/home.go, in the body pass — the card's width is asked for before the fold has decided how many rows it is hiding."},
		},
	},
	{
		project: firstProjectName, title: "Standing Up the Watches", ago: 5 * time.Hour,
		spent: 0.29, tokens: 22_400, model: "anthropic/claude-sonnet-4",
		turns: []demoTurn{
			{"keep an eye on CI and tell me when master goes red", "Set up. It looks every five minutes, spends at most twenty-five cents a look, and asks before it does anything but tell you."},
			{"and sweep the repo every morning at nine", "That one is standing too. It has come back clean three mornings running."},
		},
	},
	{
		project: firstProjectName, title: "Reading the Ledger", ago: 26 * time.Hour,
		spent: 0.51, tokens: 40_100, model: "openai/gpt-5-mini",
		turns: []demoTurn{
			{"what did opus cost me this month?", "Most of it, and most of that went to two conversations rather than to work — the spend page has the split by model and by what it was for."},
			{"can I see it by day?", "Yes — the ledger keys every line to a local calendar day, so a Tuesday is your Tuesday."},
		},
	},
	{
		project: firstProjectName, title: "The Manual's Missing Page", ago: 3 * 24 * time.Hour,
		spent: 0.18, tokens: 14_800, model: "anthropic/claude-sonnet-4", archived: true,
		turns: []demoTurn{
			{"the chat says it cannot generate images and it can", "The page saying it was impossible outlived the wave that shipped it. Removed, and the gate that would have caught it now greps for the old denial."},
		},
	},
	{
		project: "infra", title: "The Certificate Rotation", ago: 20 * time.Minute,
		spent: 0.67, tokens: 51_300, model: "anthropic/claude-opus-4.1",
		running: &session.PresenceTask{
			ID: "1", Title: "Rotate the wildcard certificate", State: string(session.TaskRunning),
			Files: []string{"terraform/main.tf", "runbooks/certs.md"},
		},
		turns: []demoTurn{
			{"rotate the wildcard certificate before it expires on the first", "Running it now — the terraform change and the runbook go together, and I will not apply anything without asking."},
		},
	},
	{
		project: "infra", title: "The Backup Window", ago: 2 * 24 * time.Hour,
		spent: 0.36, tokens: 27_900, model: "anthropic/claude-sonnet-4",
		turns: []demoTurn{
			{"the nightly backup window is four hours and it should be twenty minutes", "It is four hours because it walks every snapshot rather than the changed ones. Landed a changed-only pass; the window is nineteen minutes."},
			{"does the restore still work?", "Restored a snapshot from three days back end to end before I called it done."},
		},
	},
	{
		project: "infra", title: "Rotating the Deploy Key", ago: 4 * 24 * time.Hour,
		spent: 0.22, tokens: 17_200, model: "openai/gpt-5-mini",
		turns: []demoTurn{
			{"rotate the deploy key and tell me what else has it", "Two runners and the release job. All three moved; the old key is revoked."},
		},
	},
	{
		project: "pricing-site", title: "Pricing Research", ago: 3 * time.Hour,
		spent: 0.94, tokens: 71_600, model: "anthropic/claude-opus-4.1",
		turns: []demoTurn{
			{"what does the enterprise ladder actually beat per-seat at?", "Above forty users, on the numbers in pricing.json. Below that per-seat is cheaper for the buyer and better for us."},
			{"write that down where I will find it again", "Remembered as a project decision, so the next conversation about the ladder starts knowing it."},
		},
	},
	{
		project: "pricing-site", title: "The Annual Toggle", ago: 5 * time.Hour,
		spent: 0.58, tokens: 44_800, model: "anthropic/claude-sonnet-4",
		turns: []demoTurn{
			{"put the annual toggle on the pricing page", "It is on, defaulting to annual, and the monthly price stays visible so nobody has to do the division."},
		},
	},
	{
		project: "pricing-site", title: "What the Discount Means", ago: 27 * time.Hour,
		spent: 0.31, tokens: 24_300, model: "openai/gpt-5-mini",
		turns: []demoTurn{
			{"is the annual discount two months free or seventeen percent off?", "They are the same number said two ways. Two months free is the one buyers read fastest."},
		},
	},
}

// writeConversation mints one session folder and everything in it, and answers
// the id it minted — which is what the project's task index and the spending
// ledger join against, so a row about a conversation names a conversation that
// is really there.
func writeConversation(project *demoProject, talk demoTalk, now time.Time, brain *store.Store) (string, error) {
	id := session.NewSessionID()
	dir := filepath.Join(project.bucket, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("make %s: %w", dir, err)
	}
	spoke := now.Add(-talk.ago)
	if err := writeTranscript(filepath.Join(dir, "transcript.jsonl"), id, project.dir, talk, spoke); err != nil {
		return "", err
	}
	if err := session.SaveMeta(dir, session.Meta{
		ID:         id,
		Title:      talk.title,
		Workspace:  project.dir,
		LaunchDir:  project.dir,
		Model:      talk.model,
		Created:    spoke.Add(-time.Duration(len(talk.turns)) * 12 * time.Minute),
		LastUserAt: spoke,
		SpentUSD:   talk.spent,
		Tokens:     talk.tokens,
		Archived:   talk.archived,
	}); err != nil {
		return "", fmt.Errorf("write the identity of %s: %w", dir, err)
	}
	if err := writeTalkPresence(dir, id, project.dir, talk, now); err != nil {
		return "", err
	}
	if err := indexTalk(brain, id, talk, spoke); err != nil {
		return "", err
	}
	return id, nil
}

// journalLine is one line of a session's journal, in the shape the engine
// writes and [session.Peek] reads.
//
// THIS IS THE ONE PLACE THIS PROGRAM SPELLS A FILE FORMAT, and it is worth
// saying why rather than hiding it. Every other shape here has an exported
// writer — session.SaveMeta, session.RecordUsage, session.RecordArtifact, the
// standing store, the memory store — and this one does not: a journal is
// written by a live agent holding a flock, through internal/session's
// unexported sessionEntry, and there is no seam for "write me a conversation
// that already happened". Two things keep the copy honest. The fields are the
// subset a reader actually reads (the header, the message lines, the title
// line, the usage seal), and the test beside this program reads the result back
// through session.Peek and session.ReadWorld, so a journal this program can
// write and the engine cannot read fails the build.
type journalLine struct {
	Type    string `json:"type"`
	Version int    `json:"version,omitempty"`
	ID      string `json:"id,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	Model   string `json:"model,omitempty"`
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
	// ToolCalls and ToolCallID are the two halves of one tool call: the calls
	// ride the assistant message that asked for them, and the result comes back
	// as its own `tool`-role line keyed by the same id. They are how a NODE's
	// journal reads as work rather than as prose (seed_room.go) — a room replays
	// them as expandable rows — and they are absent from every conversation this
	// program writes, which is what the ordinary conversations always were.
	ToolCalls  []ai.ToolCall `json:"toolCalls,omitempty"`
	ToolCallID string        `json:"toolCallId,omitempty"`
	// Reasoning is the assistant's working, kept beside the message rather than
	// inside it, exactly as the engine keeps it. A room folds it to
	// `thought for 6s`, which is a shape nothing in this fixture could draw
	// before.
	Reasoning string        `json:"reasoning,omitempty"`
	Title     string        `json:"title,omitempty"`
	Usage     *journalSpend `json:"usage,omitempty"`
	Timestamp string        `json:"timestamp"`
}

// journalSpend is what one completed turn cost, on the seal line that ends it.
type journalSpend struct {
	Model      string  `json:"model,omitempty"`
	Input      int     `json:"input,omitempty"`
	Output     int     `json:"output,omitempty"`
	CostUSD    float64 `json:"costUsd,omitempty"`
	Calls      int     `json:"calls,omitempty"`
	DurationMS int64   `json:"durationMs,omitempty"`
}

// writeTranscript writes one conversation's journal: the header, the turns in
// the order they happened with a seal after each, and the name the session
// settled on.
func writeTranscript(path, id, workspace string, talk demoTalk, spoke time.Time) error {
	turns := len(talk.turns)
	if turns == 0 {
		return fmt.Errorf("conversation %q has no turns", talk.title)
	}
	// The turns are spread backwards from when the person last spoke, twelve
	// minutes apart, so a journal reads as something that took a while.
	at := func(turn int) time.Time {
		return spoke.Add(-time.Duration(turns-1-turn) * 12 * time.Minute)
	}
	lines := []journalLine{{
		Type: "session", Version: 1, ID: id, Cwd: workspace, Model: talk.model,
		Timestamp: at(0).Add(-time.Minute).Format(time.RFC3339Nano),
	}}
	for index, turn := range talk.turns {
		when := at(index)
		lines = append(lines,
			journalLine{Type: "message", Role: "user", Content: turn.said, Timestamp: when.Format(time.RFC3339Nano)},
			journalLine{Type: "message", Role: "assistant", Content: turn.answered, Timestamp: when.Add(90 * time.Second).Format(time.RFC3339Nano)},
			journalLine{Type: "usage", Timestamp: when.Add(91 * time.Second).Format(time.RFC3339Nano), Usage: &journalSpend{
				Model:   talk.model,
				Input:   talk.tokens / (2 * turns),
				Output:  talk.tokens / (6 * turns),
				CostUSD: talk.spent / float64(turns),
				Calls:   2, DurationMS: 90_000,
			}},
		)
	}
	lines = append(lines, journalLine{
		Type: "title", Title: talk.title,
		Timestamp: at(0).Add(2 * time.Minute).Format(time.RFC3339Nano),
	})

	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer file.Close()
	for _, line := range lines {
		raw, err := json.Marshal(line)
		if err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		if _, err := file.Write(append(raw, '\n')); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return file.Close()
}

// writeTalkPresence writes what a live conversation says about itself, and
// nothing at all for a conversation that is not live — a presence file for an
// idle session would be a window claiming to be open that nobody is sitting in.
//
// The stamp is `now`, which is true for about fifteen seconds
// (internal/session's presenceWindow). beat.go is what keeps it true for as long
// as the demo is open.
func writeTalkPresence(dir, id, workspace string, talk demoTalk, now time.Time) error {
	if talk.asking == "" && talk.running == nil {
		return nil
	}
	row := session.SessionPresence{
		// The schema number is internal/session's presenceSchema, which is
		// unexported; a file carrying any other number is refused outright by
		// its reader. The test beside this program is what proves this one is
		// still the number that gets read.
		Schema:    1,
		SessionID: id,
		Workspace: workspace,
		PID:       os.Getpid(),
		UpdatedAt: now,
		State:     session.PresenceIdle,
	}
	if talk.running != nil {
		task := *talk.running
		task.StartedAt = now.Add(-9 * time.Minute)
		row.RunningTasks = []session.PresenceTask{task}
		row.State = session.PresenceWorking
	}
	if talk.asking != "" {
		// Waiting OUTRANKS working, which is the presence file's own law: a turn
		// stopped on a question is running in the sense that a process exists and
		// stopped in every sense a person cares about.
		row.State = session.PresenceWaiting
		row.Reason = talk.asking
		row.Question = session.PresenceQuestion{
			Kind:    session.QuestionConsent,
			ID:      7,
			Text:    talk.asking,
			Options: session.AnswerOptions(session.QuestionConsent),
			Asked:   now.Add(-4 * time.Minute),
		}
	}
	return writePresence(dir, row)
}

// indexTalk puts every turn into the searchable index, so the search place has
// something to rank and a hit can say which conversation it came out of.
func indexTalk(brain *store.Store, id string, talk demoTalk, spoke time.Time) error {
	if _, err := brain.OpenSession(id, talk.title, "chat"); err != nil {
		return fmt.Errorf("open the thread for %q: %w", talk.title, err)
	}
	for _, turn := range talk.turns {
		for _, message := range []store.Message{
			{SessionID: id, Role: store.RoleUser, Body: turn.said},
			{SessionID: id, Role: store.RoleAgent, Body: turn.answered, Model: talk.model},
		} {
			if _, err := thread.Post(brain, message); err != nil {
				return fmt.Errorf("index a turn of %q: %w", talk.title, err)
			}
		}
	}
	return nil
}

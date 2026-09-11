package session

// The session side of a firing: how a standing item touches the world, how a
// cheap judgment decides whether it should, and how the news gets back to the
// conversation that asked for it.
//
// internal/standing owns WHEN — the pass, the rails, the ledger, the files.
// This file owns WHAT HAPPENS, because all three of those things are a session:
// a probe is the belt's own bash or the belt's own tool, a line delivered is a
// note in a conversation, and a task run unattended is an agent with a brief.
// Building any of them inside internal/standing would be a second harness.
//
// ── THE LIVE-WINDOW REGISTRY, AND WHERE A FIRING ACTUALLY LANDS ──
//
// A FIRING THAT NOBODY READS DID NOT HAPPEN. That is the whole law this half of
// the file serves, and it was written the hard way: a reminder made from home's
// `ask here` box fired into the EXCHANGE that had asked for it — an agent still
// open in the pane of a window whose person was sitting in an ordinary
// conversation two panes over — and they were never told. Had the exchange been
// closed it would have been worse: the note would have gone to the exchange
// folder's inbox, which no screen on this product reads, because an exchange is
// deliberately not a row on home.
//
// So [standingRunner.deliver] walks four roads, in this order, and stops at the
// first one that ends at a person:
//
//  1. THE ORIGIN CONVERSATION, IF IT IS OPEN HERE. The line goes onto the owed
//     steering queue a task's landing and a job's ending ride, so a person
//     sitting in the room hears about it in the room.
//  2. ANY OTHER OPEN CONVERSATION OF THE SAME PROJECT, most recently touched
//     first. The origin may be closed, or may be an exchange — and the window
//     the person is actually sitting in is a better address than a file.
//  3. THE ORIGIN'S OWN INBOX, folded under one "while you were away" the next
//     time they open it ([Agent.drainStandingInbox]). This is the right answer
//     for an item born in an ordinary conversation, which is a row somebody
//     comes back to.
//  4. THE PROJECT'S INBOX, for an item whose origin was an exchange. An
//     exchange is not a row anywhere, so its folder is a dead letter office;
//     the project's inbox is read by home and drained by the next ordinary
//     conversation opened in that project ([standing.ProjectInboxPath]).
//
// The registry below is the whole of "is it open here": a map from session id to
// the agent and the moment it registered, written by [newAgent] and erased by
// [Agent.Close]. It is deliberately tiny and holds nothing but that — a second
// index of sessions would be a second truth beside the folders that world.go
// already reads.
//
// AN ERRAND IS NEVER IN IT. [Config.Errand] marks home's `ask here` exchange,
// and [registerLiveSession] refuses one: it is a pane that closes with the
// screen, not a room. Its own card still ratifies, because a card is answered
// through the agent the surface holds and never through this map.
//
// ── WHAT AN UNATTENDED RUN IS ──
//
// A firing's task is an agent with no consent lane, and that is not a
// convenience: it is the law internal/standing states as "unattended means what
// was already allowed". Anything that would have asked a person refuses instead,
// the run stops, and the item says it needs them. So the run agent is built
// InTask — the same posture a task node runs in — which is what turns "somebody
// would have to approve this" into one honest line rather than a hang.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/processgroup"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// standingProbeWindow bounds ONE look at the world. It is the watch tool's
	// own ceiling (tools_watch.go) for the same reason: a probe that outlives
	// the pass it runs in is a check that has stopped happening, which reads as
	// silence, and silence means "no news".
	standingProbeWindow = 60 * time.Second
	// standingRunSteps is how many tool calls one firing's work may take when
	// the item named no figure. It is far under a task node's allowance because
	// a firing is a recurring bill rather than a piece of work somebody asked
	// for once.
	standingRunSteps = 60
	// standingOutcomeClip bounds what a firing's work puts in front of a person:
	// a short account of what it came to, and no more. The whole story is in the
	// run's own transcript, which the note names.
	standingOutcomeClip = 400
)

// standingSentinelRole is the cheap yes/no judgment, registered as a ROLE so it
// resolves the way every other auxiliary call in this build resolves: the
// person's pin, then the low tier, then the conversation's own model
// (internal/roles). It sits low for the guardian's reason — it reads a few
// kilobytes and answers one binary question, and a wrong no costs a check that
// said nothing rather than money.
const standingSentinelRole roles.Role = "sentinel"

func init() {
	roles.Register(standingSentinelRole, roles.TierLow, "is this worth telling you about")
}

// ── the live-window registry ────────────────────────────────────────────────

var (
	liveSessionsMu sync.Mutex
	liveSessions   = map[string]liveWindow{}
)

// liveWindow is one open conversation in this process.
type liveWindow struct {
	agent *Agent
	// opened is when it registered. It is half of "which window is the person
	// actually in" — see [liveSessionTouched].
	opened time.Time
}

// registerLiveSession records that this process holds a conversation open.
//
// TWO KINDS OF AGENT ARE NOT ONE. A node's own agent has a journal but no
// person and nothing to deliver into ([Config.InTask]). An ERRAND — home's `ask
// here` exchange — has a person, but the pane it draws in closes with home and
// is not the room they are sitting in ([Config.Errand], and this file's header
// for the firing that proved it). Neither is ever steered into.
func registerLiveSession(agent *Agent) {
	if agent == nil || agent.config.InTask || agent.config.Errand {
		return
	}
	id := strings.TrimSpace(agent.id)
	if id == "" {
		return
	}
	liveSessionsMu.Lock()
	liveSessions[id] = liveWindow{agent: agent, opened: time.Now()}
	liveSessionsMu.Unlock()
}

// forgetLiveSession erases that record. It runs from Close, so a window that
// has gone takes its entry with it and the next firing writes an inbox line
// instead of into a session nobody is reading.
func forgetLiveSession(agent *Agent) {
	if agent == nil {
		return
	}
	id := strings.TrimSpace(agent.id)
	if id == "" {
		return
	}
	liveSessionsMu.Lock()
	if held, found := liveSessions[id]; found && held.agent == agent {
		delete(liveSessions, id)
	}
	liveSessionsMu.Unlock()
}

// someoneIsWatching reports whether this process holds a conversation somebody
// is sitting in front of.
//
// IT IS THE ONE READING OF "ATTENDED" THIS BUILD CAN HONESTLY MAKE, and it is
// this map because of what the map already refuses: a task node's own agent and
// an errand's pane both decline to register ([registerLiveSession]), so an
// entry here is a room with a person in it and nothing else is. A headless run
// opens no conversation and answers false, which is the correct reading of a
// process nobody is watching.
//
// WHAT IT IS FOR. λ — what a second of waiting is worth — is zero for work
// nobody is waiting on, and that is a true statement about a run whose owner
// has closed the window and a false one about a run they are watching land
// (internal/session's loop.go, and bench/lanelab/REPORT.md for what the false
// version costs). It is deliberately coarse: it says a person is HERE, not that
// they are looking at this particular node, which is a distinction no plan
// graph in this build can yet draw.
func someoneIsWatching() bool {
	liveSessionsMu.Lock()
	defer liveSessionsMu.Unlock()
	return len(liveSessions) > 0
}

// liveSession answers the open conversation with that id, or nil.
func liveSession(id string) *Agent {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	liveSessionsMu.Lock()
	defer liveSessionsMu.Unlock()
	return liveSessions[id].agent
}

// liveSessionIn answers the open conversation of one workspace that the person
// most recently touched, or nil. It is road 2 of the delivery order.
//
// EXCLUDING ONE IS THE CALLER'S BUSINESS: the origin has already been tried by
// id, and offering it again would put a firing into the same room twice when
// two windows of one project are open.
func liveSessionIn(workspace, exclude string) *Agent {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil
	}
	liveSessionsMu.Lock()
	windows := make([]liveWindow, 0, len(liveSessions))
	for id, window := range liveSessions {
		if id == strings.TrimSpace(exclude) {
			continue
		}
		windows = append(windows, window)
	}
	liveSessionsMu.Unlock()

	var best *Agent
	var bestAt time.Time
	for _, window := range windows {
		if window.agent.standingWorkspace() != workspace {
			continue
		}
		at := liveSessionTouched(window)
		if best == nil || at.After(bestAt) {
			best, bestAt = window.agent, at
		}
	}
	return best
}

// liveSessionTouched is when a person last had anything to do with one open
// window: the later of when they last SPOKE in it ([Meta.LastUserAt], the same
// stamp the resume law and [StandingIdle] read, and deliberately never a file
// mtime) and when the window opened.
//
// THE OPENING COUNTS BECAUSE A FRESH WINDOW IS SOMEBODY ARRIVING. A
// conversation opened one minute ago with nothing typed in it yet is more
// likely to be where the person is than one they last spoke in yesterday and
// left on screen, and a rule that read only the spoken stamp would deliver
// every firing into the stale one forever.
func liveSessionTouched(window liveWindow) time.Time {
	at := window.opened
	if dir := strings.TrimSpace(window.agent.config.Place.Dir); dir != "" {
		if meta, err := LoadMeta(dir); err == nil && meta.LastUserAt.After(at) {
			at = meta.LastUserAt
		}
	}
	return at
}

// ── the runner ──────────────────────────────────────────────────────────────

// standingRunner is [standing.Runner] over one launch's config: the models, the
// keys, the approval policy and the accounts a firing works with are the ones
// the person's own conversations run on, because a firing is their work done
// while they are not looking.
type standingRunner struct {
	parent Config
	// root is the standing root, and it is handed in rather than read off
	// [Config.Standing] because a firing's posture deliberately has none: a run
	// that could reach the seam could arm another standing item, which is the
	// one thing the ambient side forbids (chatv3_standing.go). It is needed for
	// exactly one thing — the project inbox, road 4 of this file's delivery
	// order — and an empty root simply means that road is closed.
	root string
	// child builds the headless session one firing's work runs in. It is [New]
	// in every real build and it is a field for [newAgent]'s reason: it is the
	// seam a test that wants a scripted child shares with the door that wants a
	// live provider, and nothing else about a firing changes between them.
	child func(Config) (*Agent, error)
}

// newChild is the runner's one way to make a firing's session.
func (r *standingRunner) newChild(cfg Config) (*Agent, error) {
	if r.child != nil {
		return r.child(cfg)
	}
	return New(cfg)
}

// NewStandingRunner is the seam a door fills [standing.Ticker.Runner] with.
// root is the store's own directory ([standing.Store.Root]).
func NewStandingRunner(parent Config, root string) standing.Runner {
	return &standingRunner{parent: parent, root: strings.TrimSpace(root)}
}

// Probe takes one look at the world and answers what it saw, clipped from the
// TAIL: a command's news is at the end of its output, and a probe clipped from
// the front would hand the judgment the banner and drop the failure.
func (r *standingRunner) Probe(ctx context.Context, item standing.Item) (string, error) {
	switch {
	case strings.TrimSpace(item.When.Probe.Command) != "":
		return r.probeCommand(ctx, item)
	case strings.TrimSpace(item.When.Probe.Tool) != "":
		return r.probeTool(ctx, item)
	}
	return "", errors.New("standing: this item has nothing to look at")
}

// probeCommand runs the shell the same way bash and a watch tick do: the same
// shell, the same process group, the same kill. A probe that could see or do
// anything bash could not would be a second set of hands nobody approved.
func (r *standingRunner) probeCommand(ctx context.Context, item standing.Item) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, standingProbeWindow)
	defer cancel()

	shell, shellArgs := jobShell()
	process := exec.CommandContext(probeCtx, shell, append(shellArgs, item.When.Probe.Command)...)
	process.Dir = item.Workspace
	process.Env = os.Environ()
	// A new SESSION, for the reason jobs.go states: the group-kill is unchanged
	// and a probe's child cannot reach the person's terminal.
	processgroup.ConfigureDetached(process)
	// The whole GROUP, not just the shell: a probe that ran `curl … | grep x`
	// leaves two processes, and killing the parent alone would leak the rest of
	// them once per check, forever (tools_watch.go's runTick).
	process.Cancel = func() error {
		_ = processgroup.Kill(process.Process.Pid)
		return nil
	}
	process.WaitDelay = 2 * time.Second

	output, err := process.CombinedOutput()
	seen := string(output)
	switch {
	case probeCtx.Err() == context.DeadlineExceeded:
		seen += fmt.Sprintf("\n(the look timed out after %s)", standingProbeWindow)
	case err != nil:
		// A FAILING COMMAND IS EVIDENCE AND NOT AN ERROR. "is CI red" is often
		// answered by a non-zero exit, and refusing the check would turn the
		// most informative outcome into nothing to judge.
		seen += "\n(the command failed: " + err.Error() + ")"
	}
	return standingTail(seen, standing.ProbeClip), nil
}

// probeTool runs one belt tool for the item's workspace, through the same
// chokepoint a turn's call goes through — the approval gate included, with
// nobody to ask, so a probe that would have needed a person refuses in words
// the judgment can read.
//
// The agent is a THROWAWAY with no provider behind it: nothing here calls a
// model, so it is assembled the way [newAgent] assembles a belt and nothing
// else. It is InTask because it is not a conversation — it must not hand work
// out, change a setting, or start a watch that dies with the check.
func (r *standingRunner) probeTool(ctx context.Context, item standing.Item) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, standingProbeWindow)
	defer cancel()

	cfg := r.parent
	cfg.Workspace = item.Workspace
	cfg.Place = Place{}
	// THE FOLDER GOES BECAUSE THE PROBE IS NOT THE SESSION; THE LITTER STAYS WITH
	// THE SESSION BECAUSE IT NEVER BELONGED TO ANY WORKSPACE. The probe's
	// workspace is the ITEM'S repository — some other project entirely — so a job
	// log resolved against a zero Place would be this program's droppings in
	// somebody's tree, made by a check they never watched run (landing.go).
	cfg.droppings = r.parent.droppingsPlace()
	cfg.SessionFile = ""
	cfg.AskConsent = false
	cfg.InTask = true
	cfg.Standing = nil
	cfg.standingItems = nil
	// AND THE STORE GOES, BECAUSE THIS AGENT HAS NO BRAIN TO READ IT WITH. The
	// throwaway below is assembled by hand rather than by newAgent, so nothing
	// builds the memory brain the parent's Config.Memory stands for — and a
	// config claiming a store the agent does not have is exactly the
	// disagreement between a belt and a page that [Config.hasStore] exists to
	// make impossible (beltfacts.go). The belt is unchanged either way: without
	// a brain there was never a `search_conversations` on it.
	cfg.Memory = nil

	agent := &Agent{config: cfg, model: cfg.Model, id: NewSessionID()}
	agent.jobs = newJobRegistry(cfg.Workspace, cfg.droppingsPlace(), agent.enqueueJobNote, agent.enqueueWatchNote)
	agent.connect = newConnectHub(cfg)
	agent.tools = agent.belt()

	call := ai.ToolCall{
		ID:   "standing-probe",
		Type: "function",
		Function: ai.ToolCallFunction{
			Name:      strings.TrimSpace(item.When.Probe.Tool),
			Arguments: standingProbeArgs(item.When.Probe.Args),
		},
	}
	result := agent.executeTool(probeCtx, agent.newEpisode(), nil, call, argsText(call))
	return standingTail(result.text, standing.ProbeClip), nil
}

// standingProbeArgs is the arguments as the wire wants them: an absent object
// is "{}" rather than empty bytes, because a tool asked to unmarshal nothing
// fails on a call that named exactly what it meant.
func standingProbeArgs(raw json.RawMessage) string {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return "{}"
	}
	return text
}

// Say delivers one line, and the two roads it can take are the whole of this
// file's header: the open window, or the inbox.
func (r *standingRunner) Say(ctx context.Context, item standing.Item, text string) (standing.Outcome, error) {
	line := strings.TrimSpace(text)
	if line == "" {
		line = item.Words
	}
	r.deliver(item, "said", line, "")
	return standing.Outcome{Kind: "said", Text: line}, nil
}

// deliver is the one door news comes through, so a firing's line and a firing's
// outcome cannot drift on where they land. The four roads and why they are in
// this order are this file's header; the code below is that list, in that order.
//
// THE STEERING LANE IS THE LIVE ONE. It is the same owed queue a task's landing
// and a job's ending ride ([Agent.enqueueSteering]), so a person sitting in the
// room hears about it in the room, and an idle session wakes and answers rather
// than banking a line nobody will read.
//
// THE NOTE IS THE SAME NOTE WHICHEVER ROAD IT TAKES. A person who was told in
// the room and a person who reads the fold tomorrow are owed the same sentence,
// so the line and the note are both built from the item's own words here and
// never assembled twice.
func (r *standingRunner) deliver(item standing.Item, kind, text, run string) {
	origin := strings.TrimSpace(item.Origin.SessionID)
	agent := liveSession(origin)
	if agent == nil {
		// The origin is closed, or was an exchange and was never a target at
		// all. The window the person is actually sitting in is a better address
		// than any file: any open conversation of the SAME PROJECT, most
		// recently touched first.
		agent = liveSessionIn(strings.TrimSpace(item.Workspace), origin)
	}
	if agent != nil {
		// THE ROW FIRST, THE MODEL SECOND. The person is owed the news itself —
		// one dim line in the conversation they are sitting in — and they are
		// owed it whether or not the model has anything to add and whether or
		// not the wake below is even allowed to start a turn (the rail, a turn
		// already running, a session mid-close all decline it). Drawing it here,
		// before the steering line goes on the queue, is what makes the order on
		// screen the order it happened in: the firing, then whatever is said
		// about it.
		agent.emitStandingNews(standingUpdateWord(kind), item, text)
		agent.enqueueSteering(standingSteeringLine(item, text))
		return
	}
	note := standing.Note{
		At:     time.Now(),
		ItemID: item.ID,
		Words:  item.Words,
		Kind:   kind,
		Text:   text,
		Run:    run,
	}
	// AN EXCHANGE'S FOLDER IS A DEAD LETTER OFFICE. Home lists what is under
	// v3/projects, which is precisely what an errand's folder is kept out of,
	// so a note written into it is a note no screen in this product ever opens.
	// The project's inbox is the address that IS read: home draws it under the
	// project, and the next ordinary conversation opened there folds it in.
	//
	// AND AN ITEM WITH NO CONVERSATION BEHIND IT IS THE SAME CASE. One the
	// person wrote whole at the terminal (`aforge standing add`) was never
	// asked for in a room, so it has no origin inbox at all — and a note with
	// no address is a firing nobody reads, which this file's header forbids.
	// The project's inbox is where its news waits.
	if (strings.TrimSpace(item.Origin.Exchange) != "" || standingSessionDir(item) == "") && r.root != "" {
		_ = standing.DeliverProject(r.root, item.Workspace, note)
		return
	}
	dir := standingSessionDir(item)
	if dir == "" {
		return
	}
	_ = standing.Deliver(dir, note)
}

// standingUpdateWord maps a firing's outcome onto the word a surface draws a
// row with ([StandingNotice.Update], and internal/tui3's standUpdateWord for
// the four shapes it turns into).
//
// TWO KINDS OF SUCCESS ARE ONE PIECE OF NEWS. A reminder that SAID something
// and overnight work that LANDED something are different work and the same
// sentence to the person reading the row — "it ran, here is what it came to" —
// so both wear "fired" and the text carries the difference.
func standingUpdateWord(kind string) string {
	switch strings.TrimSpace(kind) {
	case "needs-you":
		return "needs-you"
	case "failed":
		return "failed"
	}
	return "fired"
}

// standingSteeringLine is the shape a firing takes in a live conversation: the
// glyph every surface leads a standing row with, the person's own words, and
// what happened. It reads as one line of news and not as a machine reporting.
//
// ── AND IT SAYS, IN WORDS, THAT IT IS NEWS ──
//
// THE MODEL MUST NOT RE-PROPOSE ITS OWN FIRING. A bare `◦ remind me in 1 minute
// to drink water: 💧 Time to drink water!` arriving as a user-role message is,
// read cold, indistinguishable from somebody typing that sentence — and both
// end-to-end suites watched the model read it exactly that way and call `stand`
// again, so a one-off reminder proposed itself a second time the moment it
// fired. The fix belongs here, at the source, and not in a rule downstream: the
// text the engine injects is the only thing the model sees.
//
// The framing is for the MODEL ALONE. A surface draws the row from the item's
// own words and the outcome's text ([Agent.emitStandingNews]), never from this
// string, so nothing a person reads carries the brackets.
func standingSteeringLine(item standing.Item, text string) string {
	return standingNewsFrame + " ◦ " + item.Words + ": " + strings.TrimSpace(text) +
		"\n" + standingNewsRule
}

const (
	// standingNewsFrame opens the injected line, so the very first tokens of the
	// message say what kind of message it is.
	standingNewsFrame = "[something you set up fired]"
	// standingNewsRule is the one sentence under it: what to do, and what not to
	// do. It names the tool it is forbidding, because a model that has `stand`
	// on its belt reads a reminder as a request to make one.
	standingNewsRule = "— this already happened. Relay it to the person in one line. " +
		"Do not call stand again for it; it is already set up."
)

// standingSessionDir is the folder the origin conversation keeps its inbox in.
// It is derived from the transcript rather than stored, because the transcript
// is the one path [standing.Origin] carries and a session folder IS the
// directory its journal sits in (place.go).
func standingSessionDir(item standing.Item) string {
	transcript := strings.TrimSpace(item.Origin.Transcript)
	if transcript == "" {
		return ""
	}
	return filepath.Dir(transcript)
}

// Run is one firing's work: a fresh headless session in the run folder, a turn
// on the brief, and — for a firing that turned out to be wider than one pair of
// hands — the parts it handed out and the fold it makes of their reports. All of
// it bounded by the item's own rails.
//
// IT IS A SESSION AND NOT A WORKTREE. A firing runs in the project the person
// pointed it at, under the rules they have already banked, exactly as
// docs/AMBIENT.md says an unattended run does. What bounds it is not a governor
// somewhere else but the two numbers on the card: how many calls it may make,
// and how much it may spend before the turn is cut. A division does not put the
// money outside that: a part folds its bill into this session's own books
// ([Agent.foldTaskUsage]), so the per-run figure this reads is the whole family's
// and the pass writes down what the family cost ([standingWideWork] for why
// division reaches here at all).
func (r *standingRunner) Run(ctx context.Context, item standing.Item, runDir, evidence string) (standing.Outcome, error) {
	cfg, err := standingRunConfig(r.parent, item, runDir)
	if err != nil {
		return standing.Outcome{}, err
	}
	// THE CAUSE THE PASS WROTE BEFORE THIS RUN BEGAN (internal/standing's
	// occurrence.go), carried into the run's own journal so the first thing it
	// records says what started it. A folder with no record — a pass from an
	// older build, a test driving the runner directly — records no cause
	// rather than one guessed from timing.
	occurrence, occurred := standingOccurrence(runDir)
	if occurred {
		cfg.cause = &executionCause{
			Kind:    causeStandingOccurrence,
			ID:      occurrence.ID,
			Owner:   cfg.OrganizationRef,
			Spec:    occurrence.Spec,
			Receipt: filepath.Join(runDir, standing.OccurrenceFile),
		}
		if !occurrence.Due.IsZero() {
			due := occurrence.Due
			cfg.cause.Due = &due
		}
	}
	brief := standingEvidence(item.Does.Brief, evidence)
	if acceptance := strings.TrimSpace(item.Does.Acceptance); acceptance != "" {
		brief += "\n\nDONE WHEN: " + acceptance
	}
	if occurred {
		brief += "\n\n" + standingOccurrenceBlock(occurrence)
	}
	if report := strings.TrimSpace(item.Does.Report); report != "" {
		brief += "\n\n" + standingReportBlock(item, report)
	}
	// THE BRIEF IS BUILT BEFORE THE SESSION IS, because whether this firing may
	// discover it is wide is read off the brief and has to be settled while the
	// config can still carry the answer ([standingWideWork]).
	cfg, graph, root := standingWideWork(cfg, item, brief)

	agent, err := r.newChild(cfg)
	if err != nil {
		return standing.Outcome{}, err
	}
	defer func() { _ = agent.Close() }()
	if graph != nil {
		// The graph is finished now that there is a session to own it: its home
		// is this firing, and the parts it may hand out are worked by the agent
		// that named them ([TaskGraph.runOwned] reads the owner at start time,
		// so it was safe to wire before this line).
		graph.home = agent
	}

	events, err := agent.Submit(ctx, brief)
	if err != nil {
		return standing.Outcome{}, err
	}

	steps, limit := 0, item.Does.MaxSteps
	if limit <= 0 {
		limit = standingRunSteps
	}
	// end is what the run's turns came to, and the one thing the outcome and
	// the publication are read from (standing_publish.go).
	var end firingEnd
	report := strings.TrimSpace(item.Does.Report)
	drain := func(events <-chan Event) {
		var said, since strings.Builder
		cutAtLimit := false
		for event := range events {
			switch event.Kind {
			case EventTextDelta:
				said.WriteString(event.Text)
				since.WriteString(event.Text)
			case EventToolBegin, EventToolAnnounced:
				since.Reset()
				// A new line between one response and the next, so the last
				// line of one is never glued to the first line of the other.
				said.WriteString("\n")
			case EventError:
				// A reply streamed up to a deadline or a failure is half a report.
				if event.Err != nil {
					end.cut = event.Err
				} else {
					end.cut = errors.New("the turn ended abnormally")
				}
			case EventToolEnd:
				// A CALL THAT SAVED SOMETHING IS THE LANDING, and [producedAFile]
				// is the one place this build says which calls those are
				// (task_run.go). It is read on the END of a call and never on its
				// start: a `write` that failed saved nothing, and a run whose only
				// act was a refused write came to exactly nothing.
				//
				// AND IT IS ASKED OF THE CALL, NOT OF THE HAND. edit_video is on
				// the saving belt with one action that reads and three that write,
				// so a wordless firing whose whole night's work was
				// `{"action":"measure"}` used to report as landed — which is the
				// one outcome the sweep may never reap, so its run folder was kept
				// for ever by a call that left nothing in it.
				if producedAFile(event.Tool, event.Args) {
					end.saved = true
				}
			case EventToolFinished:
				steps++
				// THE STEP CAP IS A STOP AND NOT A REFUSAL. Whatever the run has
				// already done stands; what it does not get is another call.
				//
				// THE MONEY IS CHECKED HERE TOO, at the one boundary where checking
				// it can change anything: a turn's spend moves when a response
				// lands, and the only thing an interrupt can still prevent is the
				// NEXT request. Asking on every streamed delta would take this
				// agent's lock a thousand times to learn the same figure.
				//
				// AND THE TURN IT ENDS ENDS NORMALLY. The interrupt closes it like
				// any other turn, with no error, so it is this mark and not the
				// turn's ending that says the run did not finish.
				if steps >= limit || (item.Rails.PerRunUSD > 0 && agent.Usage().CostUSD >= item.Rails.PerRunUSD) {
					end.capped = true
					agent.InterruptFor(StopByWorkStopped)
				}
			case EventTurnDone:
				// THE TURN SAYS HOW IT ENDED. An answer the loop stopped asking the
				// rest of at the output limit ends its turn like any other, so it
				// is this mark and not the turn's ending that says the answer is
				// partial ([Event.Truncated]). It is kept with THIS turn's words
				// and report ([firingEnd.closeTurn]), never on its own.
				cutAtLimit = event.Truncated
			case EventToolFailed:
				// A FAILED WRITE OF THE RUN'S OWN REPORT IS NOT A QUESTION FOR
				// THE PERSON, however it failed. aforge publishes that one path
				// itself, so there is nothing a person could allow — and where
				// the writers are not granted at all ([Config.grants]) the call
				// is answered as a tool this run does not have, which is the
				// same attempt to publish on its own authority. It is recorded,
				// not asked: whether the run still published is the decision's
				// to say ([firingEnd.withheld]).
				line := standingRefusal(event)
				switch {
				case writesTheReport(event, item.Workspace, report):
					end.ownReportWrite = true
				case line == "":
				case end.needs == "":
					end.needs = line
				}
			}
		}
		// THE OUTCOME IS THE LAST THING THIS FIRING ACTUALLY SAID. A run that
		// divided opens by announcing that it split the work into three parts
		// and closes by saying what came of them, and the person reads ONE
		// clipped line ([standingOutcomeClip]) — so a later turn's words replace
		// an earlier turn's rather than queueing behind them. A turn that said
		// nothing replaces nothing: silence is not a newer account, and a run
		// whose last re-entry was wordless still came to what it said before it.
		end.closeTurn(said.String(), since.String(), cutAtLimit)
	}
	drain(events)

	// ── THE FIRING THAT HANDED PARTS OF ITS WORK OUT ────────────────────────
	//
	// A turn ends the moment the model has nothing left to say, and the parts it
	// just named are still working: `divide_work` hands the ids back at once and
	// tells the worker not to wait (task_divide.go). So THE TURN ENDING IS NOT
	// THE FIRING ENDING — and this is the same tail loop a task node's runner
	// holds open around exactly the same shape ([runTaskChild]), for the same
	// three reasons. The parts' reports have to reach the model that has to fold
	// them into one account. Their spend has to be in the ledger this run's
	// figure is read off ([Agent.foldTaskUsage] posts it to this agent, and this
	// agent is closed the moment Run returns). And an unattended run that
	// returned while its own parts were still spending would be the pass writing
	// a bill and a marker for work that had not happened yet.
	//
	// THE RAILS STILL BIND, and they bind across the whole firing rather than
	// per turn: the step count and the spend carry into the fold, and a firing
	// cut at either of them takes its unfinished parts down with it
	// ([TaskGraph.stopChildren]) rather than leaving them spending for a run
	// nobody is going to read.
	for graph != nil && !end.capped && ctx.Err() == nil {
		// The generation is taken BEFORE the question, so a report landing
		// between the two closes the channel this select is about to wait on.
		news := agent.taskNewsWait()
		// THE TWO FACTS ARE READ AS ONE, for the reason [runTaskChild]'s own park
		// gives and the one [Agent.deliverTaskNote] writes down: a part landing
		// between two separate reads is a delivery seen half-done, and this loop
		// would either buy a turn with nothing in it or leave a report unread.
		owed, working := agent.taskNewsStanding()
		if owed == 0 && !working {
			break
		}
		if owed == 0 {
			// The lane goes back for exactly as long as the wait lasts
			// ([TaskGraph.park]).
			root.park()
			select {
			case <-news:
			case <-ctx.Done():
			}
			root.unpark()
			continue
		}
		next := agent.resumeTurn(ctx)
		if next == nil {
			break
		}
		drain(next)
	}
	if graph != nil && (end.capped || ctx.Err() != nil) {
		graph.stopChildren(root.id)
	}

	// ONE ANSWER DECIDES WHAT THE RUN CAME TO AND WHETHER IT PUBLISHES
	// (standing_publish.go), and the outcome's line is that answer's line.
	withheld := end.withheld(report != "", ctx.Err())
	outcome := end.outcome(withheld, ctx.Err(), report != "")
	// THE REPORT IS PUBLISHED BY THE OWNER, and only a run that came to
	// something clean publishes one: a run that stopped on a question, was cut
	// off, was stopped at a limit or said nothing leaves the last good report
	// where it was (internal/standing's Action.Report).
	final := end.body()
	publish := report != "" && withheld == notWithheld && outcome.Kind == "landed"
	if publish {
		// AND IT IS READ AGAINST THE RULES THAT REACHED THE RUN FIRST
		// (standing_rules.go). A report that still breaks one after its one
		// correction is held back, and the run waits on the person instead.
		//
		// redo is the one correction turn, drained by the run's own reader so
		// its steps, spend and refusals count exactly as the first turn's did,
		// and answered by the same decision.
		redo := func(correction string) (string, error) {
			next, err := agent.Submit(ctx, correction)
			if err != nil {
				return "", err
			}
			end.beginCorrection()
			drain(next)
			if w := end.withheld(true, ctx.Err()); w != notWithheld {
				return "", errors.New(end.why(w, ctx.Err(), true))
			}
			return end.body(), nil
		}
		checked, held := r.checkAgainstRules(ctx, agent, runDir, final, redo, item)
		final = checked
		// THE ANSWER IS READ AGAIN after the correction, which may itself
		// have been stopped at a limit, cut off, or left its report unclosed.
		if w := end.withheld(true, ctx.Err()); w != notWithheld {
			publish, withheld = false, w
			outcome = end.outcome(w, ctx.Err(), true)
		} else if held != "" {
			publish, withheld = false, withheldByRules
			outcome.Kind = standing.OutcomeNeedsYou
			outcome.NeedsPerson = clip(held, standingOutcomeClip)
			outcome.Text = outcome.NeedsPerson
		}
		if occurred {
			// The check's record was written into the folder by the check;
			// this copy must not write over it.
			if current, err := standing.ReadOccurrence(runDir); err == nil {
				occurrence = current
			}
		}
	}
	outcome.USD = agent.Usage().CostUSD
	// ── aforge's OWN TWO ACTS, EACH FENCED WHERE IT HAPPENS ──
	//
	// A stop does not reach into a run that has already started: what the run
	// did with its own tools has been done, and nothing here undoes it. What
	// aforge has NOT done yet is its own last two acts — replacing the report
	// and delivering the note — and each is rechecked against the stop and the
	// pass's context at the moment it happens, not once before both
	// ([effectFence]). A PAUSE IS NOT A STOP: work admitted before a pause
	// finishes as it was admitted, report and all, and the pause holds back the
	// next occurrence (currentAdmission, in the pass).
	fence := r.fenceFor(ctx, item)
	published := false
	if publish {
		receipt, held, err := publishStandingReport(fence, item.Workspace, report, final, r.lastPublished(item, report))
		switch {
		case held == withheldStopped:
			return stoppedWhileItRan(outcome, end.saved, report, false), nil
		case held == withheldCutOff:
			withheld = held
			outcome.Kind = standing.OutcomeFailed
			outcome.Text = "the run was cut off before its report was published: " + oneLine(ctx.Err().Error()) + "; the previous report is unchanged"
		case held == withheldReportChanged:
			withheld = held
			outcome.Kind = standing.OutcomeNeedsYou
			outcome.NeedsPerson = clip(reportChangedLine(runDir, report, final), standingOutcomeClip)
			outcome.Text = outcome.NeedsPerson
		case err != nil:
			withheld = withheldUnwritten
			outcome.Kind = standing.OutcomeFailed
			outcome.Text = "could not publish the report to " + report + ": " + err.Error()
		default:
			published = true
			outcome.Published = receipt
			if occurred {
				// Recorded the moment it exists, so a process that dies before
				// the pass finishes still leaves the receipt behind — and the
				// pass that finds it records the occurrence rather than running
				// it again (internal/standing's finishedOccurrence).
				occurrence.Published = receipt
				_ = standing.WriteOccurrence(runDir, occurrence)
			}
			outcome.Text = clip("report updated: "+report+" — "+final, standingOutcomeClip)
		}
	}
	// A RUN THAT CAME TO NOTHING TELLS NOBODY, because there is nothing to tell:
	// no line, no landing, nothing waiting. Walking the delivery roads with an
	// empty sentence would put `◦ keep main green: ` into the conversation
	// somebody is sitting in, which is an interruption whose whole content is
	// that it was not worth interrupting for. It is still asked whether it was
	// stopped, so its record says so.
	//
	// IT IS STILL RECORDED. The pass writes the ledger row, the item's log line
	// and its `previous` list from this outcome whatever it says
	// (internal/standing's tick.go), so the money and the fact that it ran
	// survive the run folder the sweep will eventually reap.
	var note func() (reportWithheld, error)
	if outcome.Kind != standing.OutcomeNothing {
		kind, text := outcome.Kind, outcome.Text
		note = func() (reportWithheld, error) {
			r.deliver(item, kind, text, runDir)
			return notWithheld, nil
		}
	}
	switch held, _ := fence.do(note); held {
	case withheldStopped:
		return stoppedWhileItRan(outcome, end.saved, report, published), nil
	case withheldCutOff:
		if withheld == notWithheld {
			withheld = held
		}
		outcome.Text = clip(outcome.Text+"; the pass was cut off before its note was delivered", standingOutcomeClip)
	}
	// The answer is recorded beside the outcome, as a code (occurrence.json's
	// "withheld"), for whatever reads the record rather than the line.
	outcome.Withheld = withheld.code()
	return outcome, nil
}

// stoppedWhileItRan is the outcome of a run whose item the person stopped
// before one of aforge's own acts: the act did not happen, and neither did any
// after it. The person who said stop was told the work will not run again, so
// no note is sent. What the run already did stands, a report already published
// included.
func stoppedWhileItRan(outcome standing.Outcome, saved bool, report string, published bool) standing.Outcome {
	outcome.NeedsPerson = ""
	if !saved && !published {
		outcome.Kind = standing.OutcomeNothing
	}
	switch {
	case published:
		outcome.Text = "stopped while it ran: its report was published before the stop, and no note was sent"
	case report != "":
		outcome.Text = "stopped while it ran: its report was not published and no note was sent; the previous report is unchanged"
	default:
		outcome.Text = "stopped while it ran: no note was sent"
	}
	outcome.Withheld = withheldStopped.code()
	return outcome
}

// reportChangedLine is what a run whose report file changed under it waits on
// the person with. The draft is kept in the run's folder, as a held report is
// ([heldReportFile]); the person's file is never written over.
func reportChangedLine(runDir, report, draft string) string {
	line := "report held back, not published: " + report + " is not what aforge last published there — it was changed, or it was there before aforge wrote it — so aforge did not write over it"
	path := filepath.Join(runDir, heldReportFile)
	if err := os.WriteFile(path, []byte(strings.TrimSpace(draft)+"\n"), 0o600); err == nil {
		line += "; the draft is in " + path
	}
	return line + ". Move your copy aside to let the next run publish"
}

// fenceFor is the fence for this firing's acts ([effectFence]): the pass's
// context as the decision found it, and the item's store.
func (r *standingRunner) fenceFor(ctx context.Context, item standing.Item) effectFence {
	fence := effectFence{ctx: ctx, live: ctx.Err() == nil, id: item.ID}
	if r.root != "" && item.ID != "" {
		if store, err := standing.Open(r.root); err == nil {
			fence.store = store
		}
	}
	return fence
}

// lastPublished is the sha256 of the report this item last published to
// report, or "" when it has published none there (or keeps no store to ask).
func (r *standingRunner) lastPublished(item standing.Item, report string) string {
	if r.root == "" || item.ID == "" {
		return ""
	}
	store, err := standing.Open(r.root)
	if err != nil {
		return ""
	}
	last, err := store.LastPublication(item.ID, report)
	if err != nil || last == nil {
		return ""
	}
	return last.SHA256
}

// heldReportFile is the run-folder file a draft is kept in when the report was
// held back rather than published.
const heldReportFile = "held-report.md"

// checkAgainstRules reads the run's final report against the rules that reached
// the run (standing_rules.go) and answers the report to publish with "", or
// the line the run waits on the person with when it may not be published. On a
// finding the run is sent back ONCE through redo, in the same session, with
// every rule it does not keep, and the report becomes what it answered. The
// check is written into the occurrence record, found or not, rule by rule.
func (r *standingRunner) checkAgainstRules(ctx context.Context, agent *Agent, runDir, draft string, redo func(string) (string, error), item standing.Item) (string, string) {
	rules := agent.standingRules()
	if len(rules) == 0 {
		return draft, ""
	}
	check := &standing.RuleCheck{}
	for _, rule := range rules {
		check.Rules = append(check.Rules, rule.ID)
	}
	verdict := agent.checkStandingReport(ctx, rules, draft)
	overRail := item.Rails.PerRunUSD > 0 && agent.Usage().CostUSD >= item.Rails.PerRunUSD
	if verdict.answered && len(verdict.broken()) > 0 && !overRail && ctx.Err() == nil {
		check.First = verdict.finding()
		check.Rewrote = true
		corrected, err := redo(standingCorrection(verdict))
		switch {
		case err != nil:
			verdict = standingRuleVerdict{trouble: oneLine(err.Error())}
		case corrected == "":
			verdict = standingRuleVerdict{trouble: "the corrected report did not arrive"}
		default:
			draft = corrected
			verdict = agent.checkStandingReport(ctx, rules, draft)
		}
	}
	check.Verdict, check.Verdicts = verdict.summary(), verdict.recorded()
	held := ""
	switch {
	case !verdict.answered:
		check.Why = verdict.trouble
		held = "its check against the rules placed on this work gave no answer: " + verdict.trouble
	case len(verdict.broken()) > 0:
		first := verdict.broken()[0]
		check.Rule, check.Quote, check.Why = first.rule.Prompt(), first.quote, first.why
		held = "it does not keep a rule placed on this work — " + verdict.finding()
	}
	if held != "" {
		path := filepath.Join(runDir, heldReportFile)
		if err := os.WriteFile(path, []byte(strings.TrimSpace(draft)+"\n"), 0o600); err == nil {
			check.Held = path
		}
		held = "report held back, not published: " + held + ". The previous report is unchanged"
		if check.Held != "" {
			held += "; the draft is in " + check.Held
		}
	}
	if occurrence, err := standing.ReadOccurrence(runDir); err == nil {
		occurrence.RuleCheck = check
		_ = standing.WriteOccurrence(runDir, occurrence)
	}
	return draft, held
}

// standingCameTo decides what one firing's work came to, from the three things
// a headless run can leave behind.
//
// A RUN CAME TO NOTHING WHEN IT LEFT NOTHING: it saved no file, it stopped on
// nothing a person has to allow, and it ended with nothing to say. That is
// [standing.OutcomeNothing]'s own definition read back — "no line, no landing,
// nothing waiting for the person" — and it is the ONE outcome whose run folder
// the sweep is allowed to reap after [standing.RunKeep].
//
// WHY THE DELIVERABLES INDEX IS NOT ASKED. Every engine-side writer of a row in
// it (artifacts.go) is one of `generate_image`, `generate_video`, `speak` and
// `generate_music`, and every one of those calls already answers yes to
// [producedAFile] — so reading the index would be a second answer to a question
// one predicate already answers, and the first day the two disagreed the honest
// one would be whichever this function did not use (design-law §ONE SOURCE OF
// TRUTH).
//
// AND A SENTENCE COUNTS AS SOMETHING. A nightly job that changed no file and
// reported "the three flaky tests passed this time" delivered that report to
// the person ([standingRunner.deliver]), and a run whose words somebody read is
// not a run that came to nothing however little it touched.
func standingCameTo(saved bool, report, needs string) string {
	switch {
	case needs != "":
		return "needs-you"
	case saved || strings.TrimSpace(report) != "":
		return "landed"
	}
	return standing.OutcomeNothing
}

// standingOccurrence reads the record the pass wrote into this run folder.
func standingOccurrence(runDir string) (standing.Occurrence, bool) {
	occurrence, err := standing.ReadOccurrence(runDir)
	if err != nil {
		return standing.Occurrence{}, false
	}
	return occurrence, true
}

// standingOccurrenceBlock is what this run is told about why it is running:
// which version of the instructions, what woke it, when the last occurrence
// ran and what it came to, and whether an earlier attempt was cut off. It is
// the owner's record read back, never the model's guess about its own history.
func standingOccurrenceBlock(occurrence standing.Occurrence) string {
	var out strings.Builder
	out.WriteString("THIS OCCURRENCE:\n")
	if occurrence.Spec > 0 {
		fmt.Fprintf(&out, "- instructions: version %d\n", occurrence.Spec)
	}
	if because := strings.TrimSpace(occurrence.Because); because != "" {
		out.WriteString("- woken because: " + because + "\n")
	}
	if !occurrence.Due.IsZero() {
		out.WriteString("- scheduled for: " + occurrence.Due.Local().Format(time.RFC3339) + "\n")
	}
	if occurrence.PreviousFired.IsZero() {
		out.WriteString("- previous occurrence: none; this is the first\n")
	} else {
		out.WriteString("- previous occurrence: " + occurrence.PreviousFired.Local().Format(time.RFC3339) + "\n")
	}
	if occurrence.Attempt > 1 {
		fmt.Fprintf(&out, "- attempt %d: an earlier attempt at this same occurrence was interrupted before it finished; do the whole occurrence again\n", occurrence.Attempt)
	}
	return strings.TrimRight(out.String(), "\n")
}

// standingReportBlock is the one instruction a run with a report is given: its
// final reply IS the report, and aforge writes it. It says where the previous
// version is so the run can carry forward what still holds, and it says that
// the run must not write the file itself — an unattended write would be refused
// anyway, and a run that tried would spend its steps on the refusal.
func standingReportBlock(item standing.Item, report string) string {
	path := filepath.Join(item.Workspace, report)
	line := "REPORT: your FINAL REPLY is the complete report. aforge publishes it to " + report +
		" in the project, replacing the previous version. Write the whole report in Markdown between a line " + standingReportOpen + " and a line " + standingReportClose +
		"; only what is between them is published, so say nothing inside them about what you read or did. Do not write that file yourself."
	if _, err := os.Stat(path); err == nil {
		line += " The previous version is at " + report + "; read it if you need what it said."
	}
	return line
}

// publishStandingReport writes a run's report to its declared path inside the
// workspace, atomically, and answers the receipt — or the reason it did not:
// the fence held it back ([effectFence]), or the file was not what aforge last
// published there. last is that publication's sha256, "" when there was none.
//
// THE PATH IS RE-CHECKED AGAINST THE WORKSPACE HERE, not only at admission:
// the report's folder is resolved through the filesystem as it is NOW, and a
// symlink planted since the item was made must not carry the write outside the
// project the person pointed it at.
//
// AND THE FILE IS COMPARED BEFORE IT IS REPLACED (the scale audit's F1, law
// L1). A run lasts minutes, and the report is a file in the person's project
// that people also open and annotate; replacing it on the strength of a read
// taken before the run is a write that loses whatever they typed meanwhile. So
// the rename happens only if the file is still exactly what aforge last
// published there, or is absent. A file aforge never wrote — there before the
// first publication — is the person's the same way. Otherwise nothing is
// written over it and the run waits on the person with its draft kept.
func publishStandingReport(fence effectFence, workspace, report, text, last string) (*standing.Publication, reportWithheld, error) {
	root, target, err := reportTarget(workspace, report)
	if err != nil {
		return nil, notWithheld, err
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, notWithheld, err
	}
	// And once more after, for whatever changed between the two.
	if err := insideProject(root, dir); err != nil {
		return nil, notWithheld, err
	}
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, notWithheld, errors.New("the report path is a symbolic link")
	}
	body := strings.TrimSpace(text) + "\n"
	temp, err := os.CreateTemp(dir, ".report-*")
	if err != nil {
		return nil, notWithheld, err
	}
	name := temp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := temp.WriteString(body); err != nil {
		_ = temp.Close()
		return nil, notWithheld, err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return nil, notWithheld, err
	}
	if err := temp.Close(); err != nil {
		return nil, notWithheld, err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return nil, notWithheld, err
	}
	held, err := fence.do(func() (reportWithheld, error) {
		if !unchangedSince(target, last) {
			return withheldReportChanged, nil
		}
		return notWithheld, os.Rename(name, target)
	})
	if held != notWithheld || err != nil {
		return nil, held, err
	}
	return &standing.Publication{Path: report, SHA256: sha256Hex(body), Bytes: len(body), At: time.Now().UTC()}, notWithheld, nil
}

// reportTarget resolves where a report would be written — the project's real
// root and the target inside it — or refuses a report whose folder leads out of
// the project through the filesystem as it is NOW.
//
// THE CHECK COMES BEFORE ANY FOLDER IS MADE. The deepest part of the report's
// folder that already exists is resolved, so a symlinked parent pointing out of
// the project refuses the write before a single folder is created on the far
// side of it.
func reportTarget(workspace, report string) (root, target string, err error) {
	root, err = filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", "", err
	}
	target = filepath.Join(root, filepath.Clean(report))
	if err := insideProject(root, deepestExisting(filepath.Dir(target))); err != nil {
		return "", "", err
	}
	return root, target, nil
}

// CheckStandingReport refuses, at setup, a report that could never be
// published where it is declared: a folder that leads out of the project
// through a symbolic link, or a report path that is itself one. It is the same
// resolution the publish makes ([reportTarget]) asked early, so every door that
// sets up ongoing work — the chat's card and `aforge standing add` alike —
// refuses exactly what the run would later refuse. The lexical rules (inside
// the workspace, never inside its own watch) are [standing.Item.Validate]'s.
//
// It does not replace the check at the write: a link planted after setup is
// still refused there, because the filesystem can change in between.
func CheckStandingReport(workspace, report string) error {
	if strings.TrimSpace(report) == "" {
		return nil
	}
	_, target, err := reportTarget(workspace, report)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("the report path is a symbolic link")
	}
	return nil
}

// unchangedSince answers whether the file at target is still what aforge last
// published there (last, a sha256), or absent. A file that is there with no
// publication behind it is not aforge's to replace.
func unchangedSince(target, last string) bool {
	current, err := os.ReadFile(target)
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	return err == nil && last != "" && sha256Hex(string(current)) == last
}

func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// insideProject refuses a folder that resolves, through the filesystem as it
// is now, anywhere but inside root.
func insideProject(root, dir string) error {
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	if rel, err := filepath.Rel(root, resolved); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("the report folder resolves outside the project")
	}
	return nil
}

// standingRunConfig is the run's own session: the parent launch, pointed at a
// fresh folder in the item's project, with nobody to ask and nothing standing.
//
// STANDING IS OFF INSIDE A FIRING, and that is a law rather than a tidy-up: an
// item that could propose another item would be the ambient side arming itself,
// which is the one thing "nothing stands until the person says yes" forbids.
func standingRunConfig(parent Config, item standing.Item, runDir string) (Config, error) {
	if strings.TrimSpace(runDir) == "" {
		return Config{}, errors.New("standing: a run needs a folder")
	}
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return Config{}, err
	}
	place := Place{Dir: runDir, Workspace: item.Workspace}
	cfg := parent
	cfg.Workspace = item.Workspace
	cfg.Place = place
	cfg.SessionFile = place.Transcript()
	cfg.WorktreeRoot = place.Trees()
	cfg.AskConsent = false
	cfg.InTask = true
	cfg.Standing = nil
	cfg.standingItems = nil
	// WHOSE MONEY THIS IS. A firing runs in a folder of its own with a session id
	// of its own, so without this the machine's usage ledger would hold a growing
	// pile of one-run conversations and no way to say that they were all the same
	// promise, kept every morning for a month (usage_ledger.go).
	cfg.standingItemID = item.ID
	// AND WHAT IT MAY READ IS ITS PROJECT (readroot.go): nobody handed it this
	// run and nobody reads along, so aforge's own home, other conversations and
	// the rest of the machine are not its to go looking through.
	cfg.readRoot = item.Workspace
	cfg.OrganizationRef = workspace.Ref{Kind: workspace.StandingKind, ID: item.ID}
	if parent.Governing != nil {
		g := *parent.Governing
		g.Workspace, g.SessionID = item.Workspace, item.Origin.SessionID
		g.Owners = []workspace.Ref{cfg.OrganizationRef}
		cfg.Governing = &g
	} else if parent.Standing != nil && parent.Standing.Store != nil {
		cfg.Governing = &Governing{Reader: parent.Standing.Store, Workspace: item.Workspace, SessionID: item.Origin.SessionID, Owners: []workspace.Ref{cfg.OrganizationRef}}
	}
	if model := strings.TrimSpace(item.Does.Model); model != "" {
		cfg.Model = model
	}
	// ── how hard a firing thinks ────────────────────────────────────────────
	//
	// A FIRING ASKS FOR NOTHING UNLESS THE ITEM SAYS OTHERWISE. It runs
	// unattended, it runs on a schedule, and it runs forever, so the depth
	// somebody happened to dial in the conversation that set the item up has no
	// business travelling with it: an install on max must not quietly turn every
	// check on the machine into a deep pass.
	//
	// That is said by the two lines below and by nothing else. The role carried
	// a floor of its own until the generation-defaults wave — a `low` this
	// harness chose for a model it knew nothing about — and what replaced it is
	// absence: the item's own rung is the only thing that reaches a firing, so
	// an item that genuinely needs thinking says so once, on the card, and an
	// item that said nothing sends no reasoning field at all.
	cfg.EffortRole = effort.RoleStanding
	cfg.Effort = restoredRung(item.Does.Effort)
	// The conversation's dial does not reach here: a firing is not the
	// conversation that proposed it, and the person who dialled that
	// conversation has long since closed it.
	cfg.DefaultEffort = effort.None
	// The folder says what it is without anybody opening its journal, the way
	// every session folder does (place.go). It is a citation and never a
	// prerequisite, so a write that fails costs a row and not the run.
	_ = SaveMeta(runDir, Meta{
		ID:        place.ID(),
		Title:     item.Words,
		Workspace: item.Workspace,
		Model:     cfg.Model,
		Effort:    cfg.Effort.String(),
		Created:   time.Now(),
	})
	return cfg, nil
}

// ── whether an unattended firing may find it is wide ────────────────────────

// standingWideWork decides, once and before the session exists, whether this
// firing's work is allowed to discover that it is wider than one pair of hands
// — and, where it is, builds the one node it IS so that the parts have somewhere
// to be born.
//
// ── WHY THE PLACE NOBODY IS WATCHING IS THE PLACE THIS MATTERS MOST ──
//
// A firing is the person's own work done while they are asleep, and until this
// it was the ONE road on which width was unaddressable: `ActionTask` ran
// strictly sequentially inside a single turn, however many separate items the
// brief named. "Every night, bring the eleven adapters up to the new interface"
// is exactly the shape [splitgate] was measured on, and it was the one shape
// that could not take the road.
//
// ── ENUMERATION ONLY, AND THAT IS THE HONEST FLOOR FOR UNATTENDED WORK ──
//
// Ordinary work has three signals ([Agent.armDivision]) and a firing has one of
// them, because the other two are people. The sizing judge answers a sentence
// somebody has just typed and is waiting on; a chat model's `wide` is that same
// judgement made with the whole conversation in front of it. A firing has
// neither: its brief was compiled from a sentence the person said once, ratified
// on a card, and has been sitting in a file ever since. ASKING A MODEL HERE WAS
// CONSIDERED AND REFUSED — a sizing read is a model call, a firing is a
// recurring bill on a rhythm the person set rather than a piece of work they
// asked for now, and a call made every night forever to answer a question about
// a sentence that has not changed since the last time is a subscription nobody
// agreed to. What is left is the signal that is FREE and reads the work's own
// text, and it under-arms rather than over-arms: [splitgate] counts a number
// only where it stands beside one of the item-nouns it knows, so a brief has to
// have named its width in words before this says yes.
//
// AND THE GATES STILL DECIDE AT RUN TIME. Arming is only permission to ask: a
// firing that turns out to be narrow, or lands on a machine with no free lane,
// gets the same two answers any worker gets (task_divide.go), and a small firing
// that was never armed is byte-identical to what it was before this existed —
// no verb on its belt, nothing in its prompt, no graph.
//
// ── THE FIRING'S OWN WORK IS THE ROOT NODE, AND IT IS BORN RUNNING ──
//
// A part has to be a node under something, and there is no conversation here to
// be that something. So the firing's own work is one node of one graph, in the
// state it is actually in: this session is working it right now, so it is
// RUNNING and it holds a lane, and it never goes near the frontier, which would
// otherwise try to start in a worktree the work that is already under way. What
// the frontier is for in this graph is the parts — and it brings the person's
// own ceilings with it, so an unattended division on a loaded machine is
// admitted and WAITS exactly as an attended one does ([TaskGraph.runFrontier]
// holds it, [TaskGraph.armPoll] lifts it), rather than running the box into the
// ground while nobody is there to notice.
func standingWideWork(cfg Config, item standing.Item, brief string) (Config, *TaskGraph, *TaskNode) {
	if !cfg.Divide || !enumeratesWidth(item.Words, brief, item.Does.Acceptance) {
		return cfg, nil, nil
	}
	graph := newTaskGraph()
	// The two ceilings and the checkpoint, read off this run's own config
	// exactly as a conversation's graph reads them off its own ([Agent.graph]).
	// The checkpoint lands in the run folder, which is where everything else
	// this firing leaves behind lands.
	graph.limit = cfg.TaskParallel
	graph.governor = newAdmissionGovernor(cfg.TaskMaxLoad, cfg.TaskMinFreeMB)
	graph.store = newTaskStore(taskCheckpointPath(cfg.SessionFile))
	graph.run = graph.runOwned
	graph.report = graph.reportHome

	id := graph.reserve()
	root := &TaskNode{
		graph: graph,
		id:    id,
		done:  make(chan struct{}),
		spec: taskSpec{
			title: item.Words,
			// THE PERSON'S OWN SENTENCE, which is what a part opens on
			// ([Agent.taskRequest]): a worker in a worktree at 3am has nobody to
			// type one, and the words on the card are the nearest thing to the
			// person there is.
			request: item.Words,
			// ORIGIN IS EMPTY ON PURPOSE. This firing has a journal — the run
			// folder transcript — but no person turn behind it. A pointer at
			// that file would be a guessed address, and [Agent.startTheParts]
			// would inherit it onto every part. Unknown renders as nothing.
			brief:      brief,
			acceptance: item.Does.Acceptance,
			model:      cfg.Model,
			// Named already: this is the person's own sentence and not a
			// sentence the namer should have another go at (taskname.go).
			named: true,
			// Armed, because the line above this function is the whole of the
			// decision and re-asking it of a home that does not exist yet would
			// answer no. The word is [armedCounted] and could not honestly be
			// anything else: what armed it is [enumeratesWidth] over the firing's
			// own text, which is the ONE signal unattended work has. So a firing
			// whose parts are then refused on the floor is refused for free and
			// finally, like any other work the counter armed — the tiebreak is
			// for two readers disagreeing, and there is only one reader here
			// (task_divide.go).
			armed: armedCounted,
		},
		state:   TaskRunning,
		started: time.Now(),
	}
	graph.nodes[id] = root
	graph.order = append(graph.order, id)
	// IT HOLDS A LANE, and that is what makes the free-hand test mean something
	// here. A firing on a machine capped at one task at a time has no second
	// pair of hands to give parts to, and [TaskGraph.freeHands] answers that
	// correctly only if the work already under way is counted as under way.
	graph.running = 1

	cfg.tasker = graph
	cfg.taskID = id
	return cfg, graph, root
}

// standingEvidence folds what the probe found into the text that names it.
// An item with no {{evidence}} in its words gets the evidence appended under a
// heading instead, because a brief written without the placeholder is still a
// brief about something the look found.
func standingEvidence(text, evidence string) string {
	text = strings.TrimSpace(text)
	evidence = strings.TrimSpace(evidence)
	if strings.Contains(text, "{{evidence}}") {
		return strings.ReplaceAll(text, "{{evidence}}", evidence)
	}
	if evidence == "" {
		return text
	}
	return text + "\n\nWHAT THE CHECK FOUND:\n" + evidence
}

// standingRefusal reads a failed tool row and answers the one line the person
// is owed when the failure was "somebody would have had to allow this".
//
// The two sentences it matches are the two this build writes for a call that
// needed a person and had none (consent.go): the node's own words, and the
// headless one. Anything else is an ordinary tool failure, which is the run's
// business and not the person's.
func standingRefusal(event Event) string {
	line := strings.TrimSpace(event.Hint)
	if line == "" {
		line = strings.TrimSpace(firstLine(event.Output))
	}
	lower := strings.ToLower(line)
	if strings.Contains(lower, "nobody to ask") || strings.Contains(lower, "no resolver is attached") {
		return line
	}
	return ""
}

// standingTail keeps the LAST n bytes, on a line boundary where it can find
// one. See [standingRunner.Probe] for why the tail is the interesting end.
func standingTail(text string, n int) string {
	if len(text) <= n {
		return strings.TrimSpace(text)
	}
	cut := text[len(text)-n:]
	if at := strings.IndexByte(cut, '\n'); at >= 0 && at < len(cut)-1 {
		cut = cut[at+1:]
	}
	return strings.TrimSpace("…\n" + cut)
}

// ── the sentinel ────────────────────────────────────────────────────────────

// standingSentinelPrompt is the whole instruction. It is short because the
// answer is binary and the LINE IS READ BY A PERSON: it lands in their
// conversation and in the item's own log, so it has to be a sentence somebody
// would say rather than a verdict word.
const standingSentinelPrompt = `You are a sentinel. You are given something a person asked to be told about, in their own words, and the evidence one check gathered. You decide ONE thing: has it happened?

Answer with "yes" or "no" as the first word, then ONE plain sentence saying what you saw — the sentence a person reads, so write it as you would say it: "the last run on main failed", "nothing has changed since yesterday".

You are also shown what you said the last few times and what came of it. Do not raise the same thing again when it has already been said and nothing has moved.

When the evidence does not settle it, answer no. A wrong yes interrupts somebody for nothing.`

// NO CEILING TRAVELS WITH A SENTINEL ANSWER. There was one — 1024, already
// widened once from a figure sized for "yes plus a line" because on a model
// that reasons before it speaks the thinking came out of the same budget and
// every check read as no clear answer forever. Widening a guess is still a
// guess. The prompt asks for a verdict and a sentence.

// NewStandingSentinel is the seam a door fills [standing.Ticker.Sentinel] with.
// It builds its client ONCE and lazily: a machine with no key, or a pass with
// no probe to judge, must not pay for a connection nobody used.
func NewStandingSentinel(parent Config) standing.Sentinel {
	var (
		once   sync.Once
		client Completer
		model  string
		built  error
	)
	return func(ctx context.Context, judgment standing.Judgment) (bool, string, float64, error) {
		once.Do(func() {
			model, built = roles.Resolve(roles.Source(parent.RolesSource), standingSentinelRole, parent.Model)
			if built != nil {
				return
			}
			client, built = provider.NewClient(provider.Config{
				APIKey:  parent.APIKey,
				BaseURL: parent.BaseURL,
				Model:   model,
				Timeout: providerTimeout,
				Routing: provider.StaticRouting(parent.Routing),
			})
		})
		if built != nil {
			return false, "", 0, built
		}
		// THE SENTINEL ASKS THE LADDER, AND FOR AN ITEM NOBODY DIALLED THE
		// LADDER SAYS NOTHING.
		//
		// It is the one call in this package where deliberation buys least: a
		// yes-or-no on evidence somebody else already gathered, run on every
		// check of every item forever. It used to carry a `low` from the role's
		// own floor for exactly that reason; the floor is gone, because a rung
		// this harness picked for somebody else's model is a request nobody
		// made (internal/effort's roleFloor). What is left is the item's own
		// rung, which is a person having decided this particular judgment is
		// hard — and when there is none, no reasoning field travels.
		//
		// The stamp is the CONFIGURED setter because this client is built here,
		// without the catalog seam — a harness-default rung would be dropped
		// every time and the item's own choice would do nothing (provider's
		// requestedEffort).
		//
		// IntentBackground says the same thing to the router — nobody is
		// waiting, so route on price rather than on speed.
		//
		// And the role says both of those once, in the vocabulary the router and
		// the phase clock share: a standing run has no one in front of it, so
		// its wait is worth nothing and its stream is nobody's to watch
		// (internal/lane's roles.go).
		callCtx := provider.WithRole(
			provider.WithRoutingIntent(provider.WithoutStream(ctx), provider.IntentBackground),
			lane.RoleStanding)
		if rung := effort.Resolve(effort.Scope{
			Task: restoredRung(judgment.Item.Does.Effort),
			Role: effort.RoleSentinel,
		}); rung != effort.None {
			callCtx = provider.WithConfiguredEffortRung(callCtx, rung)
		}
		response, err := client.CompleteWithMessages(
			// WithoutStream for the reason the guardian and the route judge use
			// it: nobody is watching this, and a stream would be typing into a
			// room that is not open.
			callCtx,
			[]ai.Message{
				textMessage("system", standingSentinelPrompt),
				textMessage("user", standingSentinelQuestion(judgment)),
			},
			ai.WithModel(model))
		if err != nil {
			return false, "", 0, err
		}
		if response == nil {
			return false, "", 0, errors.New("standing: the sentinel answered nothing")
		}
		usd := 0.0
		if response.Usage != nil && response.Usage.Cost != nil {
			usd = *response.Usage.Cost
		}
		yes, line := standingVerdict(response.Text())
		return yes, line, usd, nil
	}
}

// standingSentinelQuestion is the judgment as the sentinel reads it: their
// words, the hint the model wrote when the item was proposed, the evidence, and
// the history — which is the only part that moves between checks, and the whole
// reason a declined firing is not proposed again every wake forever.
func standingSentinelQuestion(judgment standing.Judgment) string {
	var out strings.Builder
	out.WriteString("WHAT THEY ASKED FOR (their own words):\n")
	out.WriteString(strings.TrimSpace(judgment.Item.Words))
	if hint := strings.TrimSpace(judgment.Item.When.Hint); hint != "" {
		out.WriteString("\n\nWHAT A YES LOOKS LIKE:\n" + hint)
	}
	evidence := strings.TrimSpace(judgment.Evidence)
	if evidence == "" {
		evidence = "(nothing)"
	}
	out.WriteString("\n\nWHAT THE CHECK FOUND:\n" + evidence)
	if len(judgment.Previous) > 0 {
		out.WriteString("\n\nWHAT YOU SAID LAST TIME, NEWEST FIRST:\n")
		for _, line := range judgment.Previous {
			out.WriteString("- " + strings.TrimSpace(line) + "\n")
		}
	}
	out.WriteString("\nHas it happened? Answer yes or no, then one plain sentence.")
	return out.String()
}

// standingVerdict reads the answer. It is v1's parse (cmd/aforge/chat.go's
// checkSentinel) and keeps its law: the first word is the whole verdict, and a
// reply that says neither is NOT a yes.
//
// A model that explains itself instead of answering has not answered a binary
// contract, and reading a "yes" out of the middle of a paragraph is how a watch
// starts firing on the sentence "no, this is not yes".
func standingVerdict(reply string) (bool, string) {
	answer := strings.TrimSpace(reply)
	lower := strings.ToLower(answer)
	yes := strings.HasPrefix(lower, "yes")
	if !yes && !strings.HasPrefix(lower, "no") {
		return false, "there was no clear answer, so nothing was said"
	}
	line := answer
	if fields := strings.Fields(answer); len(fields) > 1 {
		line = strings.TrimSpace(strings.Join(fields[1:], " "))
	} else {
		line = ""
	}
	line = strings.TrimSpace(strings.TrimLeft(line, "—:-, "))
	return yes, line
}

// ── whether the machine is quiet ────────────────────────────────────────────

// StandingIdle is the seam a door fills [standing.Ticker.Idle] with: the world
// reader's answer to "has nobody been here for a while".
//
// TWO CONDITIONS, AND BOTH ARE ABOUT PEOPLE. Nothing may be working right now —
// a machine mid-build is not idle however long ago somebody typed — and the
// newest thing anybody SAID anywhere must be older than the span. The second is
// read from [Meta.LastUserAt] and deliberately not from file times, which is the
// ordering law everywhere in this codebase: a background write is not a person
// returning to a conversation.
func StandingIdle() standing.Idle {
	return func(quiet time.Duration) bool {
		world := ReadHome()
		var newest time.Time
		for _, project := range world.Projects {
			for _, row := range project.Sessions {
				if row.Live && row.Presence.State == PresenceWorking {
					return false
				}
				if row.At.After(newest) {
					newest = row.At
				}
			}
		}
		if newest.IsZero() {
			// Nobody has ever said anything on this machine. That is quiet.
			return true
		}
		return world.Read.Sub(newest) >= quiet
	}
}

// ── the fold: what arrived while the window was shut ────────────────────────

// drainStandingInbox empties this conversation's inbox into ONE ambient note.
//
// IT IS ONE NOTE AND NEVER A NOTE PER FIRING. A person who was away for a week
// comes back to a conversation, not to a mailbox: the fold is what
// docs/AMBIENT.md calls "while you were away", and six separate lines would be
// six separate things to read before the first sentence they came for.
//
// THE AMBIENT LANE AND NOT THE WAKING ONE ([Agent.enqueueAmbientNote]). This
// runs before anybody has said anything, and an account of what happened while
// they were gone is context for whatever they type next — not a reason for the
// session to start talking to itself about last night.
//
// IT RUNS TWICE OVER AND THAT IS THE POINT. Construction is one of the two
// moments a person arrives at a conversation; a SURFACE ATTACHING to one this
// process never let go of is the other, and since #653 the second is the
// ordinary one — the session host outlives the window and hands the next one
// the same agent. So [Agent.WatchTaskUpdates] asks for this too, and the drain
// is idempotent by construction: a note this agent already folded is not
// folded again ([Agent.foldPending], and the journal's own record).
//
// THE INBOX IS CONSUMED AFTER THE FOLD IS RECORDED, never before (the scale
// audit's law L3). The files are staged and left where they are; the fold
// carries one durable delivery per note ([durableDelivery]), whose id the
// journal writes with the line, and only when that line is on disk does the
// settle remove the files. A window closed before any turn carried the fold, or
// a process killed in between, leaves them for the next open — and a note the
// journal already holds, because the process died after the record and before
// the removal, is recognised by its id and not folded twice.
func (a *Agent) drainStandingInbox() {
	if a.config.InTask {
		return
	}
	var stages []standing.StagedInbox
	dir := strings.TrimSpace(a.config.Place.Dir)
	if dir == "" {
		dir = filepath.Dir(strings.TrimSpace(a.config.SessionFile))
	}
	if dir != "" && dir != "." {
		if stage, err := standing.StageInbox(dir); err == nil {
			stages = append(stages, stage)
		}
	}
	if stage, ok := a.stageProjectInbox(); ok {
		stages = append(stages, stage)
	}
	commit := sync.OnceFunc(func() {
		for _, stage := range stages {
			stage.Commit()
		}
	})
	var notes []standing.Note
	for _, stage := range stages {
		for _, note := range stage.Notes {
			if id := inboxDeliveryID(note); !a.hasRecorded(id) && !a.foldIsPending(id) {
				notes = append(notes, note)
			}
		}
	}
	if len(notes) == 0 {
		// Everything staged is already in this conversation's record, or there
		// was nothing: either way the files have done their work.
		if len(stages) > 0 && !a.foldIsPendingAny() {
			commit()
		}
		return
	}
	// TWO INBOXES, ONE FOLD, IN ONE ORDER. What arrived is what arrived: a
	// person who was away does not care which file a note waited in, and two
	// folds with two openings would be the mailbox this note exists to avoid.
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].At.Before(notes[j].At) })
	fold := userText(standingAwayNote(notes))
	for _, note := range notes {
		fold.delivered = append(fold.delivered, durableDelivery{id: inboxDeliveryID(note), settled: commit})
	}
	a.markFoldPending(fold.delivered)
	if !a.enqueueNote(fold) {
		return
	}
	a.queueStandingNews(notes)
}

// inboxDeliveryID is the durable delivery id one inbox note is folded under.
func inboxDeliveryID(note standing.Note) deliveryID {
	return deliveryID("inbox:" + note.ID)
}

// markFoldPending records the notes a fold carries until its record holds
// them.
func (a *Agent) markFoldPending(carried []durableDelivery) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.foldPending == nil {
		a.foldPending = map[deliveryID]bool{}
	}
	for _, delivery := range carried {
		a.foldPending[delivery.id] = true
	}
}

// foldIsPending answers whether this agent has folded the note and not yet
// recorded the fold.
func (a *Agent) foldIsPending(id deliveryID) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.foldPending[id] && !a.file.recorded(id)
}

// foldIsPendingAny answers whether any fold of this agent's is still waiting
// for its record — the files it came from are then not this drain's to remove.
func (a *Agent) foldIsPendingAny() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id := range a.foldPending {
		if !a.file.recorded(id) {
			return true
		}
	}
	return false
}

// queueStandingNews turns the fold into what the SCREEN reads: one dim row per
// thing that fired, in the order it fired, exactly as a firing into a live
// window draws ([Agent.emitStandingNews]).
//
// ONE FOLD FOR THE MODEL, ONE ROW PER FIRING FOR THE PERSON, and the two counts
// differ on purpose. The note above is context nobody asked for and it is one
// paragraph because six of them would be six things to read before the sentence
// the person came for. The rows are the conversation's own record of what
// happened in it, and a person scrolling back is owed the same line for a
// reminder that fired at 3am as for one that fired while they watched — the
// alternative is a screen on which "where did that come from" has two answers.
//
// It is QUEUED AND NOT SENT: this runs inside New, where nobody is subscribed
// yet (see [Agent.standingNews]).
func (a *Agent) queueStandingNews(notes []standing.Note) {
	news := make([]Event, 0, len(notes))
	for _, note := range notes {
		// A note with nothing to say keeps nothing to say. The row's own word for
		// that is "ran" (internal/tui3's standUpdateWord), and the run folder —
		// which the fold above does carry, because the model can open it — is a
		// path and not a sentence a person reads off a dim line.
		news = append(news, Event{Kind: EventStandingUpdate, Tool: "stand", Standing: &StandingNotice{
			// The note is all that survived the firing: the item itself may have
			// retired hours ago, so the row is built from the words and the id
			// the note kept rather than from a store lookup that can fail.
			Item:   standing.Item{ID: note.ItemID, Words: note.Words},
			Update: standingUpdateWord(note.Kind),
			Text:   strings.TrimSpace(note.Text),
		}})
	}
	if len(news) == 0 {
		return
	}
	a.mu.Lock()
	a.standingNews = append(a.standingNews, news...)
	a.mu.Unlock()
}

// stageProjectInbox takes the PROJECT's inbox — what fired for this workspace
// while no window of it was open, from an item whose own origin was an exchange
// and had nowhere else to land ([standingRunner.deliver], road 4).
//
// AN ERRAND DOES NOT TAKE IT. Home's `ask here` pane closes with the screen and
// is never reopened, so a fold drawn into one would be this build reading a
// person's news out to nobody. It waits for a conversation, which is a room
// they come back to.
func (a *Agent) stageProjectInbox() (standing.StagedInbox, bool) {
	if a.config.Errand {
		return standing.StagedInbox{}, false
	}
	store := a.standingItems()
	if store == nil {
		return standing.StagedInbox{}, false
	}
	root, workspace := strings.TrimSpace(store.Root()), a.standingWorkspace()
	if root == "" || workspace == "" {
		return standing.StagedInbox{}, false
	}
	stage, err := standing.StageProjectInbox(root, workspace)
	return stage, err == nil
}

// standingAwayNote renders that fold: one opening line, then one line per note
// — when, whose words, what happened, and where to open the whole story.
func standingAwayNote(notes []standing.Note) string {
	var out strings.Builder
	out.WriteString("while you were away")
	for _, note := range notes {
		parts := []string{note.At.Local().Format("Mon 15:04")}
		if words := strings.TrimSpace(note.Words); words != "" {
			parts = append(parts, words)
		}
		if text := strings.TrimSpace(note.Text); text != "" {
			parts = append(parts, text)
		}
		if run := strings.TrimSpace(note.Run); run != "" {
			parts = append(parts, run)
		}
		out.WriteString("\n" + strings.Join(parts, " · "))
	}
	return out.String()
}

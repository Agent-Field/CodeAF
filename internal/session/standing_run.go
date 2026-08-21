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
// ── THE LIVE-WINDOW REGISTRY ──
//
// News reaches a conversation in one of two ways and the difference is whether
// its window is open IN THIS PROCESS. Open, and the line goes onto the same
// steering queue a task's landing and a watch's delta ride, so the person sees
// it in the room they are sitting in. Closed, and it is appended to the
// session's inbox and folded under one "while you were away" the next time they
// open it ([Agent.drainStandingInbox]).
//
// The registry below is the whole of "is it open here": a map from session id to
// agent, written by [newAgent] and erased by [Agent.Close]. It is deliberately
// tiny and holds nothing but the pointer — a second index of sessions would be a
// second truth beside the folders that world.go already reads.
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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/standing"
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
	liveSessions   = map[string]*Agent{}
)

// registerLiveSession records that this process holds a conversation open. A
// node's own agent is not one: it has a journal but no person and nothing to
// deliver into.
func registerLiveSession(agent *Agent) {
	if agent == nil || agent.config.InTask {
		return
	}
	id := strings.TrimSpace(agent.id)
	if id == "" {
		return
	}
	liveSessionsMu.Lock()
	liveSessions[id] = agent
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
	if held, found := liveSessions[id]; found && held == agent {
		delete(liveSessions, id)
	}
	liveSessionsMu.Unlock()
}

// liveSession answers the open conversation with that id, or nil.
func liveSession(id string) *Agent {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	liveSessionsMu.Lock()
	defer liveSessionsMu.Unlock()
	return liveSessions[id]
}

// ── the runner ──────────────────────────────────────────────────────────────

// standingRunner is [standing.Runner] over one launch's config: the models, the
// keys, the approval policy and the accounts a firing works with are the ones
// the person's own conversations run on, because a firing is their work done
// while they are not looking.
type standingRunner struct{ parent Config }

// NewStandingRunner is the seam a door fills [standing.Ticker.Runner] with.
func NewStandingRunner(parent Config) standing.Runner { return &standingRunner{parent: parent} }

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
	process.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// The whole GROUP, not just the shell: a probe that ran `curl … | grep x`
	// leaves two processes, and killing the parent alone would leak the rest of
	// them once per check, forever (tools_watch.go's runTick).
	process.Cancel = func() error {
		signalGroup(process, syscall.SIGKILL)
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
	cfg.SessionFile = ""
	cfg.AskConsent = false
	cfg.InTask = true
	cfg.Standing = nil
	cfg.standingItems = nil

	agent := &Agent{config: cfg, model: cfg.Model, id: NewSessionID()}
	agent.jobs = newJobRegistry(cfg.Workspace, cfg.Place, agent.enqueueSteering)
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
	result := agent.executeTool(probeCtx, agent.newEpisode(), nil, call)
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
// outcome cannot drift on where they land.
//
// THE STEERING LANE IS THE LIVE ONE. It is the same queue a task's landing and
// a watch's delta ride ([Agent.enqueueSteering]), so a person sitting in the
// room hears about it in the room, and an idle session wakes and answers rather
// than banking a line nobody will read.
func (r *standingRunner) deliver(item standing.Item, kind, text, run string) {
	if agent := liveSession(item.Origin.SessionID); agent != nil {
		agent.enqueueSteering(standingSteeringLine(item, text))
		return
	}
	dir := standingSessionDir(item)
	if dir == "" {
		return
	}
	_ = standing.Deliver(dir, standing.Note{
		At:     time.Now(),
		ItemID: item.ID,
		Words:  item.Words,
		Kind:   kind,
		Text:   text,
		Run:    run,
	})
}

// standingSteeringLine is the shape a firing takes in a live conversation: the
// glyph every surface leads a standing row with, the person's own words, and
// what happened. It reads as one line of news and not as a machine reporting.
func standingSteeringLine(item standing.Item, text string) string {
	return "◦ " + item.Words + ": " + strings.TrimSpace(text)
}

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

// Run is one firing's work: a fresh headless session in the run folder, one
// turn on the brief, bounded by the item's own rails.
//
// IT IS A SESSION AND NOT A WORKTREE. A firing runs in the project the person
// pointed it at, under the rules they have already banked, exactly as
// docs/AMBIENT.md says an unattended run does. What bounds it is not a governor
// somewhere else but the two numbers on the card: how many calls it may make,
// and how much it may spend before the turn is cut.
func (r *standingRunner) Run(ctx context.Context, item standing.Item, runDir, evidence string) (standing.Outcome, error) {
	cfg, err := standingRunConfig(r.parent, item, runDir)
	if err != nil {
		return standing.Outcome{}, err
	}
	agent, err := New(cfg)
	if err != nil {
		return standing.Outcome{}, err
	}
	defer func() { _ = agent.Close() }()

	brief := standingEvidence(item.Does.Brief, evidence)
	if acceptance := strings.TrimSpace(item.Does.Acceptance); acceptance != "" {
		brief += "\n\nDONE WHEN: " + acceptance
	}
	events, err := agent.Submit(ctx, brief)
	if err != nil {
		return standing.Outcome{}, err
	}

	steps, limit := 0, item.Does.MaxSteps
	if limit <= 0 {
		limit = standingRunSteps
	}
	var reply strings.Builder
	needs := ""
	for event := range events {
		switch event.Kind {
		case EventTextDelta:
			reply.WriteString(event.Text)
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
			if steps >= limit || (item.Rails.PerRunUSD > 0 && agent.Usage().CostUSD >= item.Rails.PerRunUSD) {
				agent.Interrupt()
			}
		case EventToolFailed:
			if line := standingRefusal(event); line != "" && needs == "" {
				needs = line
			}
		}
	}

	outcome := standing.Outcome{
		Kind: "landed",
		Text: clip(strings.TrimSpace(reply.String()), standingOutcomeClip),
		USD:  agent.Usage().CostUSD,
	}
	if needs != "" {
		// NOTHING PRETENDS THIS LANDED. A run that stopped on something only a
		// person can allow is not a failure and is not a success; it is work
		// waiting for them, and home sorts on exactly that.
		outcome.Kind, outcome.NeedsPerson = "needs-you", needs
		if outcome.Text == "" {
			outcome.Text = needs
		}
	}
	r.deliver(item, outcome.Kind, outcome.Text, runDir)
	return outcome, nil
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
	if model := strings.TrimSpace(item.Does.Model); model != "" {
		cfg.Model = model
	}
	// The folder says what it is without anybody opening its journal, the way
	// every session folder does (place.go). It is a citation and never a
	// prerequisite, so a write that fails costs a row and not the run.
	_ = SaveMeta(runDir, Meta{
		ID:        place.ID(),
		Title:     item.Words,
		Workspace: item.Workspace,
		Model:     cfg.Model,
		Created:   time.Now(),
	})
	return cfg, nil
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

// standingSentinelTokens is what one answer may cost. It is generous for a
// sentence and deliberately not tight, for the reason v1 wrote down: on a model
// that reasons before it speaks the thinking comes out of the same budget, and
// a cap sized for "yes plus a line" reads as no clear answer on every check
// forever.
const standingSentinelTokens = 1024

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
				APIKey:         parent.APIKey,
				BaseURL:        parent.BaseURL,
				Model:          model,
				Timeout:        providerTimeout,
				SiteURL:        parent.SiteURL,
				SiteName:       parent.SiteName,
				SiteCategories: parent.SiteCategories,
				Routing:        provider.StaticRouting(parent.Routing),
			})
		})
		if built != nil {
			return false, "", 0, built
		}
		response, err := client.CompleteWithMessages(
			// WithoutStream for the reason the guardian and the route judge use
			// it: nobody is watching this, and a stream would be typing into a
			// room that is not open.
			provider.WithoutStream(ctx),
			[]ai.Message{
				textMessage("system", standingSentinelPrompt),
				textMessage("user", standingSentinelQuestion(judgment)),
			},
			ai.WithModel(model),
			ai.WithMaxTokens(standingSentinelTokens))
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
// runs at construction, before anybody has said anything, and an account of
// what happened while they were gone is context for whatever they type next —
// not a reason for the session to start talking to itself about last night.
func (a *Agent) drainStandingInbox() {
	if a.config.InTask {
		return
	}
	dir := strings.TrimSpace(a.config.Place.Dir)
	if dir == "" {
		dir = filepath.Dir(strings.TrimSpace(a.config.SessionFile))
	}
	if dir == "" || dir == "." {
		return
	}
	notes, err := standing.Drain(dir)
	if err != nil || len(notes) == 0 {
		return
	}
	a.enqueueAmbientNote(standingAwayNote(notes))
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

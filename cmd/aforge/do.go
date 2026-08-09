package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/lease"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// `aforge do` is one errand, start to finish, with nobody watching.
//
// It is deliberately not the plan/run pipeline. That path compiles a graph
// once, writes it to a file, and executes exactly what the file says — which is
// the right shape for inspecting or hand-editing a plan and the wrong shape for
// doing a job, because everything this system learned about doing jobs happens
// after the plan is written: the contract for the kind of work in front of it,
// the gate that asks whether the person would accept this, the round of new
// work a cited gap earns, the replan when a leaf runs out of room. A frozen
// graph cannot do any of that.
//
// So this runs the resident's own brain with the conversation removed. The task
// is journaled as a user command — it is verbatim, and a verbatim ask needs no
// head to close it — and from that command onward every mechanism is the one a
// chat window drives, because it is literally the same construction.
const (
	// defaultDoSeconds is a wall, not a schedule. Real work runs for minutes;
	// this is the length of rope at which a wedged run is more useful dead.
	defaultDoSeconds = 900
	// settlementBeat paces the watcher. It reads a watermark first and only
	// looks at the graph when the journal has moved, so an idle beat is one
	// integer read.
	settlementBeat = 200 * time.Millisecond
	// quietBeat is how long a run may say nothing before it has to account for
	// itself. A wedged run and a run thinking hard look identical from outside,
	// and a person watched a blank terminal for the full fifteen minutes of the
	// wall before being handed exit 2. This is the cheapest possible fix for
	// that: a structural read of the graph, no model call, one line.
	quietBeat = 30 * time.Second
	// headlessSurface names this lens wherever a surface is recorded.
	headlessSurface = "do"
)

// exitStatus ends the process with a particular code and nothing more said. The
// command has already written its result to the right stream; an "error:" line
// after an honest partial answer would only be noise.
type exitStatus int

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

const (
	exitFailed  exitStatus = 1
	exitTimeout exitStatus = 2
)

// headlessOutcome is what one errand came to, in the shape --json prints.
//
// Settled means the errand is over — nothing this process is waiting for can
// still move — and it is deliberately not a verdict on the work. The verdict is
// the exit code, and the two disagree in exactly one honest way: an errand
// stopped by a question is over (settled) and did nothing (exit 1). BlockedOn
// is what tells a machine caller which of those it is holding, and it is why
// the question never goes in Deliverable: a caller that read the deliverable
// field recorded an interactive charter card as the answer to a bank
// reconciliation and never learned the task was not attempted.
type headlessOutcome struct {
	Deliverable string   `json:"deliverable"`
	Artifacts   []string `json:"artifacts"`
	Spend       float64  `json:"spend"`
	Nodes       int      `json:"nodes"`
	Seconds     float64  `json:"seconds"`
	Settled     bool     `json:"settled"`
	// BlockedOn is the question this run could not answer, verbatim. It is
	// empty on every run that was not stopped by one, and non-empty only
	// alongside a non-zero exit code and an empty deliverable.
	BlockedOn string `json:"blocked_on,omitempty"`
	// Learned is the job's own board: what workers shared with each other
	// mid-flight — discoveries about the material, pitfalls, a sibling's
	// failure and why. An ephemeral store evaporates on exit, and these lines
	// are the one piece of what the run understood that would die with it.
	Learned []string `json:"learned,omitempty"`

	// status is what the process leaves with. It is decided where the outcome
	// is produced, because only there is the difference visible between a job
	// that failed, a price that was refused, and a wall that arrived first —
	// all three of which are "not a success" and none of which are each other.
	status exitStatus
}

func runDo(args []string) error {
	flags := flag.NewFlagSet("do", flag.ContinueOnError)
	database := flags.String("db", "", "work in this durable store instead of a private one")
	keep := flags.Bool("keep", false, "keep the private store instead of deleting it on the way out")
	workspace := flags.String("w", "", "the directory to work in, edited in place (default: the current directory)")
	timeout := flags.Int("timeout", defaultDoSeconds, "hard wall in seconds")
	asJSON := flags.Bool("json", false,
		"print one machine-readable object instead of the deliverable; settled says the errand is over, "+
			"the exit code says whether it worked, and blocked_on carries a question nobody was here to answer")
	yesSpend := flags.Bool("yes-spend", false, "approve a plan whose price crosses the consent threshold")
	model := flags.String("model", "", "work model for this run (default AFORGE_MODEL)")
	planModel := flags.String("plan-model", "", "model that plans, when different from the work model (default AFORGE_PLAN_MODEL)")
	if err := flags.Parse(reorder(args, map[string]bool{
		"db": true, "w": true, "timeout": true, "model": true, "plan-model": true,
	})); err != nil {
		return err
	}
	task, err := readText(flags.Args())
	if err != nil {
		return err
	}
	if *timeout <= 0 {
		return fmt.Errorf("do timeout must be positive")
	}
	return doErrand(doRequest{
		task: task, database: *database, keep: *keep, workspace: *workspace,
		timeout: time.Duration(*timeout) * time.Second, asJSON: *asJSON,
		yesSpend: *yesSpend, model: *model, planModel: *planModel,
		stdout: os.Stdout, stderr: os.Stderr,
	})
}

// doRequest is one invocation, with its streams named so a test drives the
// whole command rather than a piece of it.
type doRequest struct {
	task      string
	database  string
	keep      bool
	workspace string
	timeout   time.Duration
	asJSON    bool
	yesSpend  bool
	model     string
	planModel string
	stdout    io.Writer
	stderr    io.Writer
	// newClient scripts the provider. Nil is the real one.
	newClient func(config.Config, string) (*liveClient, error)
}

func doErrand(request doRequest) error {
	started := time.Now()
	path, home, ephemeral, err := headlessStore(request.database)
	if err != nil {
		return err
	}
	if ephemeral && !request.keep {
		defer func() { _ = os.RemoveAll(home) }()
	}
	if ephemeral && request.keep {
		fmt.Fprintf(request.stderr, "store kept at %s\n", home)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create the store directory: %w", err)
	}

	session := headlessSessionID()
	window, err := openChatWindow(path, path, session)
	if err != nil {
		return err
	}
	defer window.close()
	graph := window.graph

	spentBefore, _ := graph.SpendToday()

	// The command is the whole interface. A verbatim ask is referentially
	// closed by definition — there is no conversation for it to point back
	// into — so it goes straight into the journal the head would have written
	// to, and everything downstream cannot tell the difference.
	command, err := graph.RequestCommand(store.Command{
		SessionID:   session,
		Kind:        store.CommandSplice,
		Instruction: request.task,
	})
	if err != nil {
		return err
	}

	// A refused price is recorded rather than returned, because the desk is
	// consulted deep inside a worker goroutine and the answer has to reach the
	// watcher above it.
	refused := make(chan planEstimate, 1)
	preauthorized := spendPreauthorized(request.yesSpend, os.Getenv)
	consent := func(_ store.Node, estimate planEstimate) bool {
		if preauthorized {
			return true
		}
		select {
		case refused <- estimate:
		default:
		}
		return false
	}

	brain, release, err := headlessBrain(window, session, request, consent, ephemeral)
	if err != nil {
		return err
	}
	if release != nil {
		defer release()
	}
	if brain != nil {
		brain.start()
		defer brain.stop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), request.timeout)
	defer cancel()
	watcher := &settlementWatch{
		graph: graph, session: session, commandSeq: command.Seq,
		refused: refused, progress: request.stderr, started: started,
	}
	outcome, err := watcher.wait(ctx)
	if err != nil {
		return err
	}
	outcome.Seconds = time.Since(started).Seconds()
	if spentNow, spendErr := graph.SpendToday(); spendErr == nil {
		outcome.Spend = spentNow - spentBefore
	}
	return reportErrand(request, outcome)
}

// headlessBrain builds and returns the brain this process will run, or nothing
// at all when another process is already the brain for this store.
//
// The second case is not a failure and not a fight. The command is already in
// the journal; a live resident applies it on its next pass and does the work
// with its own head attached, and this process becomes exactly what a second
// chat window is — something watching the same journal for the answer.
func headlessBrain(window *chatWindow, session string, request doRequest,
	consent func(store.Node, planEstimate) bool, ephemeral bool) (*chatBrain, func(), error) {
	releaseLease, heldBy, err := lease.AcquireResident(window.dir, headlessSurface)
	if err != nil {
		return nil, nil, err
	}
	if releaseLease == nil && heldBy != nil && !heldBy.Stuck {
		fmt.Fprintf(request.stderr,
			"another aforge is resident (pid %d) — it will run this task; watching for the result\n", heldBy.PID)
		return nil, nil, nil
	}
	release := func() {}
	if releaseLease != nil {
		release = func() { _ = releaseLease() }
	}
	workspaceRoot, err := errandWorkspace(request.workspace)
	if err != nil {
		release()
		return nil, nil, err
	}
	brain, err := buildBrain(window, session, brainOptions{
		headless: true, ephemeral: ephemeral, workspaceRoot: workspaceRoot,
		sharedWorkspace: true,
		model:           request.model, planModel: request.planModel,
		consent: consent, newClient: request.newClient,
	})
	if err != nil {
		release()
		return nil, nil, err
	}
	return brain, release, nil
}

// errandWorkspace resolves the directory this errand works in.
//
// It is a directory, not a place to file output. Every other agent a person
// runs from a terminal treats the directory it was pointed at as the work —
// it opens what is there, edits it in place, and leaves nothing behind that
// the person did not ask for — and an errand that filed its results into a
// freshly created subdirectory was wrong in the two ways that matter: what it
// wrote landed somewhere nobody looks, and what it was sent to read was not
// there to be read, so it wrote a plausible file from nothing instead.
//
// The default is the current directory for the same reason: that is what every
// CLI in this shape means by saying nothing at all.
func errandWorkspace(named string) (string, error) {
	if trimmed := strings.TrimSpace(named); trimmed != "" {
		expanded, err := expandHome(trimmed)
		if err != nil {
			return "", err
		}
		return filepath.Abs(expanded)
	}
	here, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("find the current directory: %w", err)
	}
	return here, nil
}

// headlessStore decides where this errand lives. The default is a private home
// that is deleted on the way out, because isolation is the point of a one-shot:
// a task run this way must not inherit half a conversation's assumptions, and a
// store that survives it is what `aforge chat` already is.
func headlessStore(database string) (path, home string, ephemeral bool, err error) {
	if database = strings.TrimSpace(database); database != "" {
		path, err = expandHome(database)
		if err != nil {
			return "", "", false, err
		}
		return path, filepath.Dir(path), false, nil
	}
	home, err = os.MkdirTemp("", "aforge-do-")
	if err != nil {
		return "", "", false, fmt.Errorf("create a private store: %w", err)
	}
	return filepath.Join(home, "graph.db"), home, true, nil
}

// headlessSessionID names this errand's thread. It is a session because every
// read below the command journal is addressed to one — the deliverable, the
// questions, the receipts — and it is unique because two `do` runs against one
// durable store are two errands, not one conversation.
func headlessSessionID() string {
	var random [4]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "headless"
	}
	return "headless-" + hex.EncodeToString(random[:])
}

// settlementWatch reads the journal until the work this errand commanded has
// finished moving.
//
// It watches the watermark rather than the graph: an idle beat costs one
// integer, and the graph is only re-read on beats where something actually
// happened. That is the same bargain the reconciler's own change gate makes,
// and it is why this is a watcher rather than a poll over a hundred nodes.
type settlementWatch struct {
	graph      *store.Store
	session    string
	commandSeq int64
	refused    chan planEstimate
	progress   io.Writer
	started    time.Time

	watermark int64
	seen      map[string]store.Status
	// structured records that the "understood" line has been said. Without it
	// the first thing stderr ever carried was a leaf changing status, so a run
	// that compiled and then hung showed nothing at all.
	structured bool
	// moved and said are the two clocks the quiet line reads: when the journal
	// last changed, and when this watcher last admitted to being alive.
	lastMoved time.Time
	lastSaid  time.Time
	// quiet is how long silence may last. Zero is quietBeat; a test names a
	// shorter one rather than sitting through half a minute of nothing.
	quiet time.Duration
}

func (w *settlementWatch) quietInterval() time.Duration {
	if w.quiet > 0 {
		return w.quiet
	}
	return quietBeat
}

func (w *settlementWatch) wait(ctx context.Context) (headlessOutcome, error) {
	ticker := time.NewTicker(settlementBeat)
	defer ticker.Stop()
	w.lastMoved, w.lastSaid = time.Now(), time.Now()
	for {
		select {
		case estimate := <-w.refused:
			fmt.Fprintf(w.progress,
				"this plan comes to %d %s, about $%.2f at what work like this has cost here.\n",
				estimate.Leaves, plural(estimate.Leaves, "step"), estimate.Dollars)
			fmt.Fprintln(w.progress, "nothing was bought; rerun with --yes-spend to approve it.")
			outcome, err := w.survey()
			if err != nil {
				return headlessOutcome{}, err
			}
			outcome.Settled, outcome.status = false, exitFailed
			outcome.Deliverable = "The plan for this task crosses the spending threshold, so nothing was started."
			return outcome, nil
		case <-ctx.Done():
			// The wall. What exists is what the person gets, and the exit code
			// says it is not the whole answer.
			outcome, err := w.survey()
			if err != nil {
				return headlessOutcome{}, err
			}
			outcome.Settled, outcome.status = false, exitTimeout
			// A wall a question was standing behind is not a slow run. Saying
			// which of the two it was costs one read and is the difference
			// between a diagnosable timeout and fifteen minutes of nothing.
			asked, questionErr := w.blockingQuestion()
			if questionErr != nil {
				return headlessOutcome{}, questionErr
			}
			outcome.BlockedOn = asked
			if strings.TrimSpace(outcome.Deliverable) == "" && asked == "" {
				outcome.Deliverable = "The time limit was reached before anything finished."
			}
			return outcome, nil
		case <-ticker.C:
			moved, err := w.moved()
			if err != nil {
				return headlessOutcome{}, err
			}
			if !moved {
				if err := w.saySomethingIfQuiet(); err != nil {
					return headlessOutcome{}, err
				}
				continue
			}
			w.lastMoved = time.Now()
			outcome, settled, err := w.check()
			if err != nil {
				return headlessOutcome{}, err
			}
			if settled {
				return outcome, nil
			}
		}
	}
}

// moved reports that the journal has grown since the last look.
func (w *settlementWatch) moved() (bool, error) {
	latest, err := w.graph.LatestEventSeq()
	if err != nil {
		return false, err
	}
	if latest == w.watermark {
		return false, nil
	}
	w.watermark = latest
	return true, nil
}

// check answers the only question the watcher has: is this errand over?
//
// A command that was rejected is over immediately — the compiler asked
// something, or drafted a charter, and neither has an answer coming in a
// process with no one at the keyboard. Otherwise the errand is over when every
// node this session owns has stopped. Extensions are covered without a special
// case: a gate that buys another round splices before the node it is extending
// lands, so there is no instant at which the graph looks finished and is not.
func (w *settlementWatch) check() (headlessOutcome, bool, error) {
	command, found, err := w.graph.CommandBySeq(w.commandSeq)
	if err != nil {
		return headlessOutcome{}, false, err
	}
	if !found || command.Status == store.CommandPending {
		return headlessOutcome{}, false, nil
	}
	if command.Status == store.CommandRejected {
		outcome, err := w.survey()
		if err != nil {
			return headlessOutcome{}, false, err
		}
		outcome.Settled, outcome.status = true, exitFailed
		words := w.refusalWords(command)
		// A refusal that is a question is not a deliverable, and putting it
		// there is what made a three-second do-nothing run indistinguishable
		// from an answer. Asked and answerable are different things: this
		// process has no keyboard, so the question goes in its own field, the
		// deliverable stays empty, and reportErrand says so out loud.
		asked, err := w.blockingQuestion()
		if err != nil {
			return headlessOutcome{}, false, err
		}
		if asked != "" {
			words = asked
		}
		if asked != "" || rejectedForAnAnswer(command) {
			outcome.BlockedOn, outcome.Deliverable = words, ""
		} else {
			outcome.Deliverable = words
		}
		return outcome, true, nil
	}
	nodes, err := w.sessionNodes()
	if err != nil {
		return headlessOutcome{}, false, err
	}
	if len(nodes) == 0 {
		return headlessOutcome{}, false, nil
	}
	w.report(nodes)
	for _, node := range nodes {
		if !terminalStatus(node.Status) {
			return headlessOutcome{}, false, nil
		}
	}
	outcome := w.compose(nodes)
	outcome.Settled = true
	return outcome, true, nil
}

// refusalWords is what a rejected command has to say for itself. The receipt
// the reconciler posted is the real answer — a compiler question, a charter
// awaiting ratification — and the command's own result is the summary of it.
func (w *settlementWatch) refusalWords(command store.Command) string {
	words := strings.TrimSpace(command.Result)
	messages, err := w.graph.Messages(w.session, 0, 50)
	if err == nil {
		for index := len(messages) - 1; index >= 0; index-- {
			message := messages[index]
			if message.CommandSeq == command.Seq && strings.TrimSpace(message.Body) != "" {
				return strings.TrimSpace(message.Body)
			}
		}
	}
	if words == "" {
		words = "the request was not turned into work"
	}
	return words
}

// blockingQuestion is the card this errand is standing behind, if any.
//
// It is the whole of part two of the contract: no question may end a headless
// run in silence. Consent already fails fast through --yes-spend and the
// standing-versus-once classification is answered structurally by the verb, so
// what reaches here is a question the errand's own semantics genuinely cannot
// resolve — and the only honest thing to do with one of those is say it, loudly,
// and leave with a failure.
//
// Open means unanswered, surfaced or not; a question that resolves itself on a
// grace timer (service consent) has already left the set by the time an errand
// settles or walls, so nothing self-answering is reported as a block.
func (w *settlementWatch) blockingQuestion() (string, error) {
	questions, err := w.graph.OpenQuestions(w.session, 8)
	if err != nil {
		return "", err
	}
	lines := make([]string, 0, len(questions))
	for _, question := range questions {
		if text := strings.TrimSpace(question.Text); text != "" {
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, "\n\n"), nil
}

// rejectedForAnAnswer is the backstop for a refusal whose question did not
// survive as a durable row — expired between the rejection and this read, or
// written straight into the receipt. The reconciler records why it refused, and
// the two reasons that mean "waiting on a person" are exactly these. Anything
// else is a failure, and a failure's words belong in the deliverable, where a
// caller reads what went wrong.
func rejectedForAnAnswer(command store.Command) bool {
	result := strings.ToLower(strings.TrimSpace(command.Result))
	return strings.HasPrefix(result, "asked the user") || strings.Contains(result, "pending ratification")
}

// sessionNodes is every node this errand owns. The splice stamps one provenance
// on every node it admits and an extension inherits it, so the session id is
// the whole membership test — no id-prefix arithmetic, which is exactly the
// thing that breaks when a job splits.
func (w *settlementWatch) sessionNodes() ([]store.Node, error) {
	all, err := w.graph.Nodes()
	if err != nil {
		return nil, err
	}
	mine := make([]store.Node, 0, 8)
	for _, node := range all {
		if node.ID == store.RootID {
			continue
		}
		if strings.TrimSpace(node.Provenance.SessionID) == w.session {
			mine = append(mine, node)
		}
	}
	return mine, nil
}

// report writes one line per state change to stderr, so a person watching a
// long run can see the graph moving without the deliverable on stdout
// acquiring a single byte of it.
func (w *settlementWatch) report(nodes []store.Node) {
	if w.progress == nil {
		return
	}
	if w.seen == nil {
		w.seen = make(map[string]store.Status, len(nodes))
	}
	// The ask becoming work is the first thing that happens and used to be the
	// one thing never said. Everything below prints on a status change, and a
	// node's first status is Pending, which is skipped — so a run whose leaf was
	// never claimed printed nothing whatsoever for the whole of its life.
	if !w.structured {
		w.structured = true
		fmt.Fprintf(w.progress, "  · understood · %s %s\n",
			plural(len(nodes), "task"), time.Since(w.started).Round(time.Second))
	}
	for _, node := range nodes {
		if previous, ok := w.seen[node.ID]; ok && previous == node.Status {
			continue
		}
		w.seen[node.ID] = node.Status
		if node.Status == store.Pending {
			continue
		}
		fmt.Fprintf(w.progress, "  %s %-28s %s\n", statusMark(node.Status),
			clip(firstLine(nodeDisplay(node)), 28), time.Since(w.started).Round(time.Second))
	}
}

// saySomethingIfQuiet accounts for a run that has stopped producing evidence.
//
// It is a structural read and nothing else: how many of this errand's nodes are
// waiting, how many are running, and how long since anything last happened. A
// run thinking hard and a run wedged forever emit exactly the same silence, and
// the only honest difference a watcher can offer is to name what the silence is
// standing on. Nothing here spends money and nothing here is a new flag.
func (w *settlementWatch) saySomethingIfQuiet() error {
	interval := w.quietInterval()
	if w.progress == nil || time.Since(w.lastMoved) < interval || time.Since(w.lastSaid) < interval {
		return nil
	}
	w.lastSaid = time.Now()
	nodes, err := w.sessionNodes()
	if err != nil {
		return err
	}
	quiet := time.Since(w.lastMoved).Round(time.Second)
	if len(nodes) == 0 {
		fmt.Fprintf(w.progress, "  still waiting: the task is being turned into work — %s\n", quiet)
		return nil
	}
	var pending, running int
	for _, node := range nodes {
		switch {
		case node.Status == store.Running || node.Status == store.Claimed:
			running++
		case !terminalStatus(node.Status):
			pending++
		}
	}
	fmt.Fprintf(w.progress, "  still waiting: %s pending, %s — %s\n",
		plural(pending, "task"), runningWords(running), quiet)
	return nil
}

// runningWords says "none running" rather than "0 running", because the whole
// value of the line is that a person reads it at a glance and knows whether the
// run is stuck behind a worker or behind nothing at all.
func runningWords(running int) string {
	if running == 0 {
		return "none running"
	}
	return fmt.Sprintf("%d running", running)
}

func statusMark(status store.Status) string {
	switch status {
	case store.Running, store.Claimed:
		return "▶"
	case store.Done:
		return "✓"
	case store.Failed:
		return "✗"
	case store.Cancelled:
		return "·"
	default:
		return " "
	}
}

func terminalStatus(status store.Status) bool {
	return status == store.Done || status == store.Failed || status == store.Cancelled
}

// survey is compose over whatever exists right now. It is what a timeout and a
// refusal get: the honest partial rather than nothing.
func (w *settlementWatch) survey() (headlessOutcome, error) {
	nodes, err := w.sessionNodes()
	if err != nil {
		return headlessOutcome{}, err
	}
	return w.compose(nodes), nil
}

// compose picks the deliverable out of a settled graph.
//
// It is the last top-level node to move that is not handing over to another
// one. That is the same node the reconciler announces into a thread and for the
// same reason: an extended job continues as a fresh top-level node, so the
// first root's summary is a receipt saying "more is coming" and the last one's
// is the answer.
func (w *settlementWatch) compose(nodes []store.Node) headlessOutcome {
	outcome := headlessOutcome{Nodes: len(nodes), Artifacts: []string{}}
	roots := make([]store.Node, 0, 4)
	for _, node := range nodes {
		if node.Parent == store.RootID {
			roots = append(roots, node)
		}
	}
	sort.SliceStable(roots, func(i, j int) bool { return roots[i].UpdatedSeq < roots[j].UpdatedSeq })
	var final *store.Node
	for index := range roots {
		root := roots[index]
		if !terminalStatus(root.Status) {
			continue
		}
		if resident.SplitContinued(root.Summary) {
			continue
		}
		final = &roots[index]
	}
	if final == nil && len(roots) > 0 {
		final = &roots[len(roots)-1]
	}
	if final != nil {
		switch {
		case final.Status == store.Failed || final.Status == store.Cancelled:
			outcome.status = exitFailed
			outcome.Deliverable = strings.TrimSpace(final.Error)
			if outcome.Deliverable == "" {
				outcome.Deliverable = "It did not finish, and no reason was recorded."
			}
		case resident.SplitContinued(final.Summary):
			// The fallback lands here on a timeout mid-split: every root is a
			// receipt saying more was coming, and a receipt about scheduling is
			// not an answer — two GAIA questions delivered "[splitting the
			// remaining work — 6 pieces queued]" as their FINAL ANSWER before
			// this case existed. Say what actually happened instead.
			outcome.Deliverable = "The work was still mid-flight when time ran out: it had split into further pieces that never finished. Nothing here is the answer."
		default:
			outcome.Deliverable = strings.TrimSpace(final.Summary)
		}
	}
	outcome.Artifacts = errandArtifacts(nodes)
	// The board survives as the outcome's learned lines: what one worker told
	// the others is exactly what the caller would want to know about the
	// material, and in an ephemeral run this is its only way out.
	for _, root := range roots {
		messages, err := w.graph.NodeMessages(root.ID, 0, 40)
		if err != nil {
			continue
		}
		for _, message := range messages {
			if note, ok := jobNoteLine(message); ok {
				outcome.Learned = append(outcome.Learned, note)
			}
		}
	}
	return outcome
}

// errandArtifacts recovers the files this errand wrote from the only durable
// record of them: the summaries the workers handed on. Existence on disk is the
// filter — a path named in prose that is not there is not a file the person
// can open, and offering it would be worse than saying nothing.
func errandArtifacts(nodes []store.Node) []string {
	seen := make(map[string]bool)
	paths := make([]string, 0)
	for _, node := range nodes {
		for _, field := range strings.Fields(node.Summary + "\n" + node.Error) {
			path := strings.Trim(field, `"'(),;:.`)
			if !strings.HasPrefix(path, "/") || len(path) < 2 || seen[path] {
				continue
			}
			seen[path] = true
			if info, err := os.Stat(path); err != nil || info.IsDir() {
				continue
			}
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}

// reportErrand writes the answer and decides what the process leaves with.
// stdout is the deliverable and a short footer and nothing else, because the
// most common thing anyone does with a one-shot is pipe it somewhere.
func reportErrand(request doRequest, outcome headlessOutcome) error {
	sayBlocked(request.stderr, outcome)
	if request.asJSON {
		encoded, err := json.MarshalIndent(outcome, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(request.stdout, string(encoded))
		return errandStatus(outcome)
	}
	if body := strings.TrimSpace(outcome.Deliverable); body != "" {
		fmt.Fprintln(request.stdout, body)
	}
	fmt.Fprintln(request.stdout)
	if len(outcome.Artifacts) > 0 {
		fmt.Fprintln(request.stdout, "files:")
		for _, path := range outcome.Artifacts {
			fmt.Fprintln(request.stdout, "  "+path)
		}
	}
	if len(outcome.Learned) > 0 {
		fmt.Fprintln(request.stdout, "learned:")
		for _, line := range outcome.Learned {
			fmt.Fprintln(request.stdout, "  "+line)
		}
	}
	fmt.Fprintf(request.stdout, "%s · %s · $%.4f\n",
		time.Duration(outcome.Seconds*float64(time.Second)).Round(time.Second),
		plural(outcome.Nodes, "node"), outcome.Spend)
	return errandStatus(outcome)
}

// sayBlocked is the loud half. A run that ended on a question wrote nothing to
// stdout on purpose, and a pipeline reading only stdout would see a fast, cheap,
// empty success — which is precisely how a run that did zero work for three
// seconds and five thousandths of a cent went unnoticed. stderr carries the
// question verbatim and the one line that says nobody here could answer it.
func sayBlocked(stderr io.Writer, outcome headlessOutcome) {
	question := strings.TrimSpace(outcome.BlockedOn)
	if stderr == nil || question == "" {
		return
	}
	fmt.Fprintln(stderr, "it stopped to ask:")
	for _, line := range strings.Split(question, "\n") {
		fmt.Fprintln(stderr, "  "+line)
	}
	fmt.Fprintln(stderr,
		"headless mode cannot answer that — `aforge do` runs with nobody at the keyboard, so nothing was done.")
	fmt.Fprintln(stderr,
		"say the answer in the ask itself and run it again, or bring it to `aforge` where it can be answered.")
}

// errandStatus is the contract a script reads: nothing to say means it worked,
// 1 means the work failed or was refused, 2 means the wall came first and what
// is above is a partial.
func errandStatus(outcome headlessOutcome) error {
	if outcome.status == 0 {
		return nil
	}
	return outcome.status
}

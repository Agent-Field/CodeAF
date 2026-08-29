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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
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
	// defaultResidentWait bounds the one case where an errand is not the brain:
	// another process already holds the resident lock for this store. A resident
	// that is serving it picks the command up on its next pass, which is
	// seconds; anything past this is a resident that is never going to, and the
	// run says which process it was waiting for and stops. It exists because the
	// silent version of this wait spent 25-40 minutes at nodes:0 and $0.00.
	defaultResidentWait = 60 * time.Second
)

// exitStatus ends the process with a particular code and nothing more said. The
// command has already written its result to the right stream; an "error:" line
// after an honest partial answer would only be noise.
type exitStatus int

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

const (
	exitFailed exitStatus = 1
	// exitPartial is the third answer and the one the table always described:
	// something usable is above, and it is not the whole of what was asked for.
	// The wall is one way to get here and was for a long time the only one — the
	// other is a delivery that did not land whole, either because the delivery
	// gate stood by a rejection of it or because parts of the job failed. Both of
	// those printed their own shortfall to stdout under exit 0, which is the one
	// thing a harness reads: "Not all of this landed: 1 of 2 parts finished", and
	// $? = 0 under it.
	exitPartial exitStatus = 2
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
	// Spend is the whole bill and nothing less: every usage row this errand
	// caused, summed out of the journal after the work has stopped moving. It
	// is the number the usage table sums to on a private store, and it is that
	// deliberately — a receipt 36 % under its own ledger is worse than no
	// receipt, which is what the day-delta subtraction it replaced produced
	// whenever a leaf journaled its row on the way down from the wall.
	//
	// SpendWork and SpendOverhead are the two halves, named because they are
	// genuinely different questions. Work is what this errand's own nodes cost:
	// leaf executions and the structuring pass beside each one. Overhead is
	// what it cost to decide what those nodes should be — planning passes and
	// head structuring, which bill the root and belong to no node. A caller
	// comparing workers on a corpus wants Work; a caller paying the bill wants
	// Spend. The two always add up to it.
	Spend         float64 `json:"spend"`
	SpendWork     float64 `json:"spend_work"`
	SpendOverhead float64 `json:"spend_overhead"`

	Nodes   int     `json:"nodes"`
	Seconds float64 `json:"seconds"`
	Settled bool    `json:"settled"`
	// BlockedOn is the question this run could not answer, verbatim. It is
	// empty on every run that was not stopped by one, and non-empty only
	// alongside a non-zero exit code and an empty deliverable.
	BlockedOn string `json:"blocked_on,omitempty"`
	// Learned is the job's own board: what workers shared with each other
	// mid-flight — discoveries about the material, pitfalls, a sibling's
	// failure and why. An ephemeral store evaporates on exit, and these lines
	// are the one piece of what the run understood that would die with it.
	Learned []string `json:"learned,omitempty"`
	// Subharness is the worker that took the deliverable, read back from the
	// durable row rather than from what was asked for. It is always present and
	// never empty — "linear" is the generalist, and a caller comparing workers
	// on a corpus needs the default spelled out as much as the specialist, or an
	// absent field is indistinguishable from an older binary.
	Subharness string `json:"subharness"`
	// Error is the sentence a run that never reached an outcome left behind:
	// the store that would not open, the working directory that could not be
	// made, the resident that never picked the command up, a journal read that
	// failed mid-flight. It is empty on every run that produced an answer.
	//
	// It exists because --json's whole promise is one object on stdout, and a
	// promise that only holds when the work succeeds is not one a script can be
	// written against. Every one of those bail-outs used to print "error: ..."
	// on stderr and leave stdout EMPTY, which is byte-for-byte what a crashed
	// process looks like from the other side of a pipe.
	Error string `json:"error,omitempty"`

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
			"the exit code says whether it worked, blocked_on carries a question nobody was here to answer, "+
			"and error carries the sentence when the run could not start at all")
	yesSpend := flags.Bool("yes-spend", false, "approve a plan whose price crosses the consent threshold")
	model := flags.String("model", "", "work model for this run (default AFORGE_MODEL)")
	planModel := flags.String("plan-model", "", "model that plans, when different from the work model (default AFORGE_PLAN_MODEL)")
	subharness := flags.String("subharness", "", "force this errand onto one worker, for measuring workers against each other (default: let the compiler choose)")
	contextFill := flags.Int("context-fill", 0,
		"how full a model's context window may get before it is compacted, in percent (default 60, clamped 10-90); "+
			"sets AFORGE_CONTEXT_FILL_PCT for this run")
	completionReserve := flags.Int("completion-reserve", 0,
		"tokens every call keeps free for its answer and its reasoning (default 65536); "+
			"sets AFORGE_COMPLETION_RESERVE for this run")
	if err := flags.Parse(reorder(flags, args)); err != nil {
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
		subharness:  *subharness,
		contextFill: *contextFill, completionReserve: *completionReserve,
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
	// subharness forces this errand onto one worker. It is the benchmarking
	// path: an unknown name is a note on stderr and the default worker, so a
	// measurement run never dies at argument parsing.
	subharness string
	// contextFill and completionReserve are this run's two dials on the window
	// law (internal/ctxbudget). They are integers rather than a struct because
	// zero has to mean "not asked for": the law's own defaults are the answer
	// on every run that says nothing, and a flag that always wrote the
	// environment would make the default unreachable from a shell that had
	// already set it.
	contextFill       int
	completionReserve int
	stdout            io.Writer
	stderr            io.Writer
	// residentWait bounds how long this run defers to a resident that already
	// holds the lock for its store. Zero is defaultResidentWait; a test names a
	// shorter one rather than sitting through it.
	residentWait time.Duration
	// newClient scripts the provider. Nil is the real one.
	newClient func(config.Config, string) (*liveClient, error)
}

func (r doRequest) residentWaitOrDefault() time.Duration {
	if r.residentWait > 0 {
		return r.residentWait
	}
	return defaultResidentWait
}

// applyContextLaw puts this run's two window dials where the law reads them.
//
// internal/ctxbudget is deliberately environment-driven and imports nothing: it
// is asked the same question from a head turn, a planner pass, a leaf worker
// and a judge, none of which share a config object. So the flags do not carry a
// budget down through six call layers — they set the two variables the law
// already consults, once, before anything is built. A run that names neither
// flag touches the environment not at all, which is what keeps a harness that
// exports these variables in its shell in charge of its own campaign.
func applyContextLaw(fillPercent, completionReserve int) error {
	if fillPercent < 0 || completionReserve < 0 {
		return fmt.Errorf("--context-fill and --completion-reserve must not be negative")
	}
	if fillPercent > 0 {
		if err := os.Setenv("AFORGE_CONTEXT_FILL_PCT", strconv.Itoa(fillPercent)); err != nil {
			return fmt.Errorf("set the context fill for this run: %w", err)
		}
	}
	if completionReserve > 0 {
		if err := os.Setenv("AFORGE_COMPLETION_RESERVE", strconv.Itoa(completionReserve)); err != nil {
			return fmt.Errorf("set the completion reserve for this run: %w", err)
		}
	}
	return nil
}

// doErrand is the one exit every headless run leaves through.
//
// It is a wrapper around the run itself for a single reason: --json promises a
// machine-readable object and must keep that promise on the paths where nothing
// worked. Every bail-out inside errandRun — an unopenable store, a directory
// that will not be made, a resident that never picked the command up, a journal
// read that failed under the watcher — used to land in main's "error: ..." line
// with EMPTY stdout, and a caller reading stdout could not tell a store failure
// from a crash. So a --json run routes its failures through the same printer as
// its successes: one object, the sentence in its error field, and exit 1, which
// is what the table already promised for a run with nothing usable in it.
//
// A person at a terminal sees exactly what they always saw. The error goes back
// to main, which says it on stderr — an ordinary run has no object to put it in
// and never wanted one.
func doErrand(request doRequest) error {
	started := time.Now()
	outcome, err := errandRun(request, started)
	if err != nil {
		if !request.asJSON {
			return err
		}
		return reportErrand(request, failedErrand(err, started))
	}
	return reportErrand(request, outcome)
}

// failedErrand is what --json prints for a run that never got as far as an
// outcome of its own. Settled is false because nothing was ever waiting to
// move, and everything the run never learned — the deliverable, the bill, the
// worker that took it — stays at its zero rather than being filled in with a
// guess.
func failedErrand(err error, started time.Time) headlessOutcome {
	return headlessOutcome{
		Artifacts: []string{},
		Seconds:   time.Since(started).Seconds(),
		Error:     err.Error(),
		status:    exitFailed,
	}
}

// errandRun is the errand itself: everything from opening a store to composing
// what came of it. It reports nothing and decides no exit code — both belong to
// doErrand, so that a failure anywhere in here reaches the caller through the
// same door as an answer.
func errandRun(request doRequest, started time.Time) (headlessOutcome, error) {
	if err := applyContextLaw(request.contextFill, request.completionReserve); err != nil {
		return headlessOutcome{}, err
	}
	path, home, ephemeral, err := headlessStore(request.database)
	if err != nil {
		return headlessOutcome{}, err
	}
	if ephemeral && !request.keep {
		defer func() { _ = os.RemoveAll(home) }()
	}
	if ephemeral && request.keep {
		fmt.Fprintf(request.stderr, "store kept at %s\n", home)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return headlessOutcome{}, fmt.Errorf("create the store directory: %w", err)
	}

	session := headlessSessionID()
	window, err := openChatWindow(path, path, session)
	if err != nil {
		return headlessOutcome{}, err
	}
	defer window.close()
	graph := window.graph

	// Where the journal stood before this errand wrote a word. Every usage row
	// after it is this run's, and reading the bill off that window is what
	// replaced a subtraction of today's spend that was wrong three ways over
	// (see store.ErrandSpend).
	openedAt, _ := graph.LatestEventSeq()

	// The command is the whole interface. A verbatim ask is referentially
	// closed by definition — there is no conversation for it to point back
	// into — so it goes straight into the journal the head would have written
	// to, and everything downstream cannot tell the difference.
	//
	// It stays verbatim past the journal too. Chat's value is that it re-asks
	// the question better; `do`'s contract is that the text handed to it *is*
	// the task, so the reconciler this run builds keeps the compiled goal
	// byte-for-byte (resident.keepTheAskVerbatim). What arrives here is what
	// the work is held to.
	command, err := graph.RequestCommand(store.Command{
		SessionID:   session,
		Kind:        store.CommandSplice,
		Instruction: request.task,
	})
	if err != nil {
		return headlessOutcome{}, err
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

	// What the workers wrote, caught on its way past. Nothing fills it on a run
	// this process handed to a resident that already holds the store — that work
	// happens in another process, and the watcher falls back to prose there.
	produced := &errandRegistry{}
	brain, release, deferredTo, err := headlessBrain(window, session, request, consent, ephemeral, produced)
	if err != nil {
		return headlessOutcome{}, err
	}
	if deferredTo != nil {
		if err := awaitResidentPickup(graph, command.Seq, deferredTo, path,
			request.residentWaitOrDefault(), request.stderr); err != nil {
			return headlessOutcome{}, err
		}
	}
	if release != nil {
		defer release()
	}
	settle := func() {}
	if brain != nil {
		brain.start()
		// stop is idempotent, so this is both the ordinary unwind and the
		// deliberate one below: the receipt is read after the workers have
		// stopped, never beside them.
		defer brain.stop()
		settle = brain.stop
	}

	ctx, cancel := context.WithTimeout(context.Background(), request.timeout)
	defer cancel()
	watcher := &settlementWatch{
		graph: graph, session: session, commandSeq: command.Seq,
		refused: refused, progress: request.stderr, started: started,
		produced: produced,
	}
	outcome, err := watcher.wait(ctx)
	if err != nil {
		return headlessOutcome{}, err
	}
	outcome.Seconds = time.Since(started).Seconds()
	// The wall is the case that made this necessary. A leaf cancelled by the
	// timeout journals its usage row on the way down, which is after the
	// watcher has returned and — until this line moved the shutdown ahead of
	// the read — after the receipt had already been printed without it. One swe
	// leaf landing that late is the whole of the 36 % under-report.
	settle()
	priceErrand(graph, session, openedAt, &outcome)
	return outcome, nil
}

// priceErrand puts the journal's own answer on the outcome.
//
// It is a query and not a counter on purpose. The bill is whatever the usage
// table holds for this errand at settle time — every row, whatever wrote it and
// however late — because the one thing a receipt may never do is disagree with
// the ledger it is a receipt for. A read that fails leaves the figures at zero
// rather than at a guess.
func priceErrand(graph *store.Store, session string, openedAt int64, outcome *headlessOutcome) {
	spend, err := graph.SpendSinceSeq(session, openedAt)
	if err != nil {
		return
	}
	outcome.Spend = spend.Cost()
	outcome.SpendWork = spend.Work.Cost
	outcome.SpendOverhead = spend.Spine.Cost
}

// headlessBrain builds and returns the brain this process will run, or nothing
// at all when another process is already the brain for this store.
//
// The second case is not a failure and not a fight. The command is already in
// the journal; a live resident applies it on its next pass and does the work
// with its own head attached, and this process becomes exactly what a second
// chat window is — something watching the same journal for the answer. It is
// only ever allowed to be that on the strength of a resident that is actually
// serving this store: heldBy is handed back so the caller can hold the wait to
// a short bound and say who it is waiting for.
func headlessBrain(window *chatWindow, session string, request doRequest,
	consent func(store.Node, planEstimate) bool, ephemeral bool,
	produced *errandRegistry) (*chatBrain, func(), *lease.Resident, error) {
	releaseLease, heldBy, err := lease.AcquireResident(window.path, headlessSurface)
	if err != nil {
		return nil, nil, nil, err
	}
	if releaseLease == nil && heldBy != nil && !heldBy.Stuck {
		if err := residentServesThisStore(heldBy, window.path); err != nil {
			return nil, nil, nil, err
		}
		fmt.Fprintf(request.stderr,
			"waiting for the resident (pid %d on %s) to take this task — store %s\n",
			heldBy.PID, residentHost(heldBy), window.path)
		return nil, nil, heldBy, nil
	}
	release := func() {}
	if releaseLease != nil {
		release = func() { _ = releaseLease() }
	}
	workspaceRoot, err := errandWorkspace(request.workspace)
	if err != nil {
		release()
		return nil, nil, nil, err
	}
	brain, err := buildBrain(window, session, brainOptions{
		headless: true, ephemeral: ephemeral, workspaceRoot: workspaceRoot,
		sharedWorkspace: true,
		model:           request.model, planModel: request.planModel,
		subharness: request.subharness,
		consent:    consent, newClient: request.newClient,
		produced: produced.add,
	})
	if err != nil {
		release()
		return nil, nil, nil, err
	}
	return brain, release, nil, nil
}

// residentServesThisStore refuses the wait that has no end.
//
// A holder that names a different database is not going to do this errand: it
// is a brain over another journal, it will never read this command, and every
// second spent watching for an answer is wall burned at nodes:0 and $0.00. That
// is precisely what a directory-wide lock used to produce, in 25-40 minute
// silences, so the shape of the failure is spelled out here rather than left
// for someone to find twice with a benchmark grid.
func residentServesThisStore(holder *lease.Resident, path string) error {
	served := strings.TrimSpace(holder.Store)
	if served == "" {
		// An older binary wrote no store into the lock. It may well be serving
		// this one, so the bounded wait below is what decides.
		return nil
	}
	mine, err := filepath.Abs(path)
	if err != nil {
		mine = path
	}
	if sameStore(served, mine) {
		return nil
	}
	return fmt.Errorf("the resident lock for %s is held by pid %d on %s, which is serving %s — "+
		"that process will never see this task; give this errand its own store with --db in a separate directory, "+
		"or stop that process",
		mine, holder.PID, residentHost(holder), served)
}

// sameStore compares two store paths as plainly as a diagnostic needs to. It is
// deliberately not a stat of both: the answer is used to decide what to print
// and whether to wait, and a path that cannot be resolved must not turn into a
// crash on the way to a message.
func sameStore(left, right string) bool {
	if filepath.Clean(left) == filepath.Clean(right) {
		return true
	}
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}

func residentHost(holder *lease.Resident) string {
	if host := strings.TrimSpace(holder.Host); host != "" {
		return host
	}
	return "unknown host"
}

// awaitResidentPickup bounds the deference. A resident that is serving this
// store applies the command on its very next pass, so a command still pending
// after this bound means nobody is coming — an older build that does not know
// the verb, a wedged loop, a process that took the lock and died mid-pass — and
// the only useful thing this run can do is say so and stop, instead of holding
// the terminal for the whole wall with one repeated progress line.
func awaitResidentPickup(graph *store.Store, seq int64, holder *lease.Resident,
	path string, bound time.Duration, stderr io.Writer) error {
	deadline := time.Now().Add(bound)
	for {
		command, ok, err := graph.CommandBySeq(seq)
		if err != nil {
			return err
		}
		if ok && command.Status != store.CommandPending {
			fmt.Fprintf(stderr, "the resident (pid %d) took this task\n", holder.PID)
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("the resident (pid %d on %s) holding the lock for %s has not taken this task in %s — "+
				"it is not serving this errand; give this run its own store with --db in a separate directory, "+
				"or stop that process",
				holder.PID, residentHost(holder), path, bound.Round(time.Second))
		}
		time.Sleep(settlementBeat)
	}
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
	// produced is the registry the runner filled as leaves landed. Nil, or
	// empty, is the deferred run — the work happened in the resident's process
	// — and compose reads the workers' prose instead.
	produced *errandRegistry

	watermark int64
	seen      map[string]store.Status
	// noted remembers which nodes have already had their degradation said, so a
	// build missing a worker admits it once per node rather than once per beat.
	noted map[string]bool
	// structured records that the "understood" line has been said. Without it
	// the first thing stderr ever carried was a leaf changing status, so a run
	// that compiled and then hung showed nothing at all.
	structured bool
	// narrated is how far into the journal the within-node narration has read.
	// It starts at the present rather than at zero: a store with a year of
	// tenure holds a journal this run had nothing to do with, and catching up
	// through it two hundred rows at a time would delay the first line this run
	// actually has to say.
	narrated int64
	// phase remembers the last stage said for each node, because a progress row
	// is replaceable and repeats itself until it moves.
	phase map[string]string
	// moved and said are the two clocks the quiet line reads: when a node this
	// errand owns last changed state, and when this watcher last admitted to
	// being alive. The first is deliberately not the journal's own watermark —
	// a store with other work in it grows all day, and a run wedged beside that
	// growth is exactly the run this line exists for.
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
	// Narration reads forward from wherever the journal stands now. This
	// errand's own nodes do not exist yet — the reconciler creates them after
	// the command is admitted — so nothing this run will do is behind the
	// cursor, and everything that came before belongs to somebody else's work.
	if seq, err := w.graph.LatestEventSeq(); err == nil && w.narrated == 0 {
		w.narrated = seq
	}
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
			outcome.Settled, outcome.status = false, exitPartial
			// A wall a question was standing behind is not a slow run. Saying
			// which of the two it was costs one read and is the difference
			// between a diagnosable timeout and fifteen minutes of nothing.
			asked, questionErr := w.blockingQuestion()
			if questionErr != nil {
				return headlessOutcome{}, questionErr
			}
			outcome.BlockedOn = asked
			if strings.TrimSpace(outcome.Deliverable) == "" && asked == "" {
				outcome.Deliverable = wallWords(outcome.Artifacts)
			}
			return outcome, nil
		case <-ticker.C:
			moved, err := w.moved()
			if err != nil {
				return headlessOutcome{}, err
			}
			if moved {
				outcome, settled, err := w.check()
				if err != nil {
					return headlessOutcome{}, err
				}
				if settled {
					return outcome, nil
				}
			}
			// SILENCE IS ABOUT THE WORK, NOT ABOUT THE JOURNAL.
			//
			// This used to be the else-branch of the watermark: any event at
			// all reset the clock and the quiet line was never even reached.
			// But a journal that is moving is not a job that is moving — a
			// sibling billing usage rows every few seconds is enough to keep
			// the watermark climbing while the one node anybody cares about is
			// wedged, and that run printed nothing for the whole of its life.
			// So the clock is reset by check(), and only when a node this
			// errand owns actually changed state.
			if err := w.saySomethingIfQuiet(); err != nil {
				return headlessOutcome{}, err
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
			// A refusal usually means nothing ran, and then this changes
			// nothing. When something did run before the refusal, the files it
			// left are part of the honest answer.
			outcome.Deliverable = groundedInArtifacts(words, outcome.Artifacts)
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
	if w.report(nodes) {
		w.lastMoved = time.Now()
	}
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
//
// The membership test belongs in SQL, and used not to be: this ran up to five
// times a second against a full decode of every node the store has ever held,
// to keep the four that were this errand's.
func (w *settlementWatch) sessionNodes() ([]store.Node, error) {
	return w.graph.SessionMemberNodes(w.session)
}

// report writes one line per state change to stderr, so a person watching a
// long run can see the graph moving without the deliverable on stdout
// acquiring a single byte of it.
//
// It answers whether anything actually moved, because that is the only reading
// of "this run is still alive" the quiet line may trust: a node it has never
// seen, or one that is not where it was. Everything else in the journal belongs
// to somebody else's work.
func (w *settlementWatch) report(nodes []store.Node) bool {
	if w.progress == nil {
		return false
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
		// The ask became work, and on the rare errand that named a specialist it
		// says which one — once, in the line that already exists, in the same
		// breath as how much work it became. A generalist errand reads exactly as
		// it always did.
		worker := ""
		if named := errandWorker(nodes); named != "" {
			worker = " (" + named + ")"
		}
		fmt.Fprintf(w.progress, "  · understood · %s%s %s\n",
			plural(len(nodes), "task"), worker, time.Since(w.started).Round(time.Second))
	}
	w.noteDegradedWorkers(nodes)
	// Everything the journal knows that a status column cannot say. It runs
	// before the status lines so that the fault which caused an escalation is
	// read above the escalation, in the order the two things happened.
	w.narrate(nodes)
	changed := false
	for _, node := range nodes {
		if previous, ok := w.seen[node.ID]; ok && previous == node.Status {
			continue
		}
		changed = true
		w.seen[node.ID] = node.Status
		if node.Status == store.Pending {
			continue
		}
		fmt.Fprintf(w.progress, "  %s %-28s %s\n", statusMark(node.Status),
			clip(firstLine(nodeDisplay(node)), 28), time.Since(w.started).Round(time.Second))
	}
	return changed
}

// errandWorker is the specialist this errand was given, if it was given one. It
// reads the roots because the choice is a fact about the job rather than about
// one leaf, and it names nothing when the answer is the generalist — a run that
// took the default is the run everyone already knows how to read.
func errandWorker(nodes []store.Node) string {
	for _, node := range nodes {
		if node.Parent != store.RootID {
			continue
		}
		if worker := promisedWorker(node); worker != "" && worker != exec.LinearSubharness {
			return worker
		}
	}
	return ""
}

// noteDegradedWorkers says once, per node, that this build could not honor the
// worker the node was promised. The run continues on the generalist — that is
// the registry's promise and it is not changing — but a benchmark cell that
// silently became a default cell is a measurement of the wrong thing, and the
// only honest place to learn that was a profile file that may never be written.
func (w *settlementWatch) noteDegradedWorkers(nodes []store.Node) {
	if w.progress == nil {
		return
	}
	for _, node := range nodes {
		worker := promisedWorker(node)
		if !degradedWorker(worker) {
			continue
		}
		if w.noted == nil {
			w.noted = make(map[string]bool, 1)
		}
		if w.noted[node.ID] {
			continue
		}
		w.noted[node.ID] = true
		noteUnavailableWorker(w.progress, worker)
	}
}

// narrationLimit bounds one read of the journal. The watcher reads on every
// beat the journal moved, so 200 rows is several seconds of the busiest run
// there is, and the cursor carries the rest to the next beat rather than
// pulling an unbounded slice into memory on a store with years of tenure.
const narrationLimit = 200

// narrate says the things that happen INSIDE a node.
//
// The status column above it can only say pending, running, done, failed. That
// was the whole of the headless stream on 2026-08-28, and it is why a run that
// was recovering correctly was killed: a leaf faulted, the scheduler escalated
// it from bare to swe two seconds later, the swe engine started a seven-minute
// baseline — three facts, all of them in the journal, none of them in the
// stream, which said `still waiting: 0 tasks pending, 1 running — 10m57s`. The
// operator read it as a hang.
//
// A FAIL-SAFE PROPAGATES TO THE VERDICT THE PERSON READS. Four kinds of fact
// change what somebody watching should expect, so four kinds of fact get a line
// in the same register as ▶ and ✓: a caught fault, a change of worker, a
// subharness phase, and the delivery gate's judgement. Nothing here is a new
// flag and nothing here spends money — every one of them is already written
// down, and until now nobody read it.
//
// It is a read of event KINDS and payload fields, never of prose. A narrator
// that recognised its facts by the words they were phrased in would be one
// rewording behind forever.
func (w *settlementWatch) narrate(nodes []store.Node) {
	if w.progress == nil || w.graph == nil {
		return
	}
	events, err := w.graph.Events(w.narrated, narrationLimit)
	if err != nil {
		// A journal this reader cannot open is the settlement loop's problem to
		// report, and it will, on its own next read. Narration going quiet is
		// never worth ending a run over.
		return
	}
	member := make(map[string]store.Node, len(nodes))
	for _, node := range nodes {
		member[node.ID] = node
	}
	said := false
	for _, event := range events {
		w.narrated = event.Seq
		node, ours := member[event.NodeID]
		if !ours {
			continue
		}
		said = w.narrateOne(event, node, nodes) || said
	}
	// `still waiting` is what is printed when NOTHING is known. A fact just went
	// past, so the next interval has something better to say than silence, and
	// the quiet line stands down for exactly as long as it would have after any
	// other thing this watcher said.
	if said {
		w.lastSaid = time.Now()
	}
}

// narrateOne writes the line for one journal row, and answers whether it wrote
// anything. A kind with no line is the common case and costs one switch arm.
func (w *settlementWatch) narrateOne(event store.Event, node store.Node, nodes []store.Node) bool {
	switch event.Kind {
	case store.EventNodeFaulted:
		var fault struct {
			Fault string `json:"fault"`
		}
		if json.Unmarshal(event.Payload, &fault) != nil || strings.TrimSpace(fault.Fault) == "" {
			return false
		}
		w.say("✗", nodeDisplay(node), firstLine(fault.Fault))
		return true

	case store.EventNodeWorkerChanged:
		var change struct {
			Subharness string `json:"subharness"`
			Previous   string `json:"previous"`
			Reason     string `json:"reason"`
		}
		if json.Unmarshal(event.Payload, &change) != nil || strings.TrimSpace(change.Subharness) == "" {
			return false
		}
		w.say("↻", nodeDisplay(node), workerChangeWords(change.Previous, change.Subharness, change.Reason))
		return true

	case store.EventMessagePosted:
		var message struct {
			Progress *store.MessageProgress `json:"progress"`
		}
		if json.Unmarshal(event.Payload, &message) != nil || message.Progress == nil {
			return false
		}
		phase := strings.TrimSpace(message.Progress.Phase)
		if phase == "" {
			return false
		}
		// A replaceable row says the same thing until it changes. Printing every
		// repeat would bury the stream in a phase that has not moved, so a phase
		// is said once per node until a different one arrives.
		if w.phase == nil {
			w.phase = map[string]string{}
		}
		if w.phase[node.ID] == phase {
			return false
		}
		w.phase[node.ID] = phase
		subject := phase
		if worker := phaseWorker(node, nodes); worker != "" {
			subject = worker + ": " + phase
		}
		if message.Progress.Total > 0 {
			subject += fmt.Sprintf(" · %d of %d", message.Progress.Done, message.Progress.Total)
		}
		detail := strings.TrimSpace(message.Progress.Latest)
		if detail == "" {
			detail = phaseHint(phase)
		}
		w.note(subject, detail)
		return true

	case store.EventAcceptance:
		var acceptance store.Acceptance
		if json.Unmarshal(event.Payload, &acceptance) != nil || len(acceptance.Points) == 0 {
			return false
		}
		// Said once, as a count and not as a list. The person watching needs to
		// know the checklist EXISTS and how big it is — that is what makes a
		// later "no check exercises …" legible instead of arriving out of
		// nowhere — and forty behaviours printed one per line would bury every
		// other line in the stream.
		w.note("acceptance", acceptanceWords(len(acceptance.Points)))
		return true

	case store.EventDeliveryGate:
		var gate store.DeliveryGate
		if json.Unmarshal(event.Payload, &gate) != nil {
			return false
		}
		verdict, detail := gateWords(gate)
		w.note("gate: "+verdict, detail)
		return true
	}
	return false
}

// say writes one narration line about a node, in the register the status lines
// already use: two spaces, a mark, the subject in the same column, then what
// happened and the clock every other line in this file prints.
func (w *settlementWatch) say(mark, subject, detail string) {
	fmt.Fprintf(w.progress, "  %s %-28s — %s  %s\n", mark, clip(subject, 28), detail,
		time.Since(w.started).Round(time.Second))
}

// note writes a narration line about the run rather than about one node, and so
// carries no mark and no node column. A phase and a gate result are facts about
// where the work has got to; hanging a ▶ on them would say a node changed state
// when none did.
func (w *settlementWatch) note(subject, detail string) {
	elapsed := time.Since(w.started).Round(time.Second)
	if strings.TrimSpace(detail) == "" {
		fmt.Fprintf(w.progress, "  %s  %s\n", subject, elapsed)
		return
	}
	fmt.Fprintf(w.progress, "  %s — %s  %s\n", subject, detail, elapsed)
}

// workerChangeWords says which worker took the work over. An escalation is the
// usual case and it names both ends, because "escalated bare → swe" is the fact
// that explains why the next thing the run does looks nothing like the last.
// A node given a worker it did not previously have was not escalated from
// anything, and saying it was would invent a failure.
func workerChangeWords(previous, subharness, reason string) string {
	previous, subharness = strings.TrimSpace(previous), strings.TrimSpace(subharness)
	words := "handed to " + subharness
	if previous != "" {
		words = fmt.Sprintf("escalated %s → %s", previous, subharness)
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		words += ": " + firstLine(reason)
	}
	return words
}

// gateWords is the delivery gate's judgement in three words a person already
// knows. A refusal is said as a refusal rather than as a failure: the two mean
// different things to whoever is reading — one is work that fell short, the
// other is a round the run declined to buy — and collapsing them is how a
// refused repair came to look like a passed delivery.
//
// THE FINDING IS THE NEWS, AND THE REASON IS THE FOOTNOTE. For a while this
// printed the refusal sentence alone, so a person watching ten runs read
// "gate: refused — what the review asked for next is not in the request" ten
// times and never once learned what the review had said was missing. The one
// fact worth the line — "the deliverable does not contain the code that
// implements the feature schema persistence" — was in the journal and nowhere a
// person could see it. FAILSAFE clause 3: a fail-safe that does not propagate
// to the verdict the person reads is decoration.
func gateWords(gate store.DeliveryGate) (verdict, detail string) {
	gap := firstLine(strings.TrimSpace(gate.Gap))
	if refused := strings.TrimSpace(gate.Refused); refused != "" {
		detail = gap
		if detail == "" {
			return "refused", firstLine(refused)
		}
		return "refused", detail + " — " + firstLine(refused)
	}
	if gate.Pass {
		return "pass", ""
	}
	return "fail", gap
}

// acceptanceWords says how many behaviours the request states, in the register
// the rest of this stream uses: a fact about the run, in a person's words, with
// no machinery vocabulary in it.
func acceptanceWords(points int) string {
	if points == 1 {
		return "1 point from the request"
	}
	return fmt.Sprintf("%d points from the request", points)
}

// phaseWorker names the specialist a phase belongs to, and names nothing when it
// cannot be sure.
//
// Progress is anchored on the job's root node, so the root's own worker is the
// answer whenever the job has one. When the root is the generalist the phase
// still came from somewhere, and the only structural candidate is a leaf that is
// still running under a specialist: exactly one of those is an answer, two is a
// guess, and a guess would put the wrong worker's name on the wrong stage.
func phaseWorker(anchor store.Node, nodes []store.Node) string {
	if worker := promisedWorker(anchor); worker != "" && worker != exec.LinearSubharness {
		return worker
	}
	found := ""
	for _, node := range nodes {
		if terminalStatus(node.Status) {
			continue
		}
		worker := promisedWorker(node)
		if worker == "" || worker == exec.LinearSubharness {
			continue
		}
		if found != "" && found != worker {
			return ""
		}
		found = worker
	}
	return found
}

// phaseHint turns a phase name into an expectation, for the handful of stages
// whose whole problem is that they are long and silent.
//
// The LINE is structural: every phase the journal carries gets one, hint or no
// hint, so nothing here can leave a stage unreported. This is decoration on top
// of it — the sentence that stops somebody killing a seven-minute test run at
// minute four — and a phase that is not in the table simply prints without it.
func phaseHint(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "baseline":
		return "running the repository's own tests, this can take minutes"
	case "running the repository's own checks":
		return "the repository's own test suite, this can take minutes"
	case "preparing the repository":
		return "fetching and setting it up, this can take minutes"
	}
	return ""
}

// saySomethingIfQuiet accounts for a run that has stopped producing evidence.
//
// It is a structural read and nothing else: how many of this errand's nodes are
// waiting, how many are running, the model call the silence is standing on, and
// how long the run has been going. A run thinking hard and a run wedged forever
// emit exactly the same silence, and the only honest difference a watcher can
// offer is to name what the silence is standing on. Nothing here spends money
// and nothing here is a new flag.
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
	// The clock a person reads is the run's, not the gap's.
	//
	// This used to print the silence — time since the journal last moved — and
	// on a real run that reads as a stopwatch someone keeps resetting: 30s, 30s,
	// 1m0s, 30s. A number that goes backwards is not a duration, it is a puzzle,
	// and the thing anyone actually wants to know from a waiting line is how long
	// this has been going on. Since the errand started, which is the same clock
	// every other progress line in this file already prints, and the only one
	// that can never run backwards.
	elapsed := time.Since(w.started).Round(time.Second)
	call := w.lastCallWords()
	if len(nodes) == 0 {
		fmt.Fprintf(w.progress, "  still waiting: the task is being turned into work%s — %s\n", call, elapsed)
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
	fmt.Fprintf(w.progress, "  still waiting: %s pending, %s%s — %s\n",
		plural(pending, "task"), runningWords(running), call, elapsed)
	return nil
}

// lastCallWords names the model call this silence is standing on.
//
// A run that spent fifteen minutes inside one model call said "1 task pending,
// 1 running — 4m30s" and then left with exit 2, and nothing anywhere named the
// model or said when it had last been heard from — the two facts anyone looking
// at a wedged run wants first. The journal already knows both: every finished
// call writes a usage row carrying the model that served it, so the newest row
// this errand caused is the last thing that demonstrably happened.
//
// A run with no call recorded yet says nothing about calls at all, rather than
// "0s ago" or "none". A model that has not been reached and a model that
// answered a moment ago are different situations, and a zero invented for the
// first is how they stop being told apart. A read that fails says nothing for
// the same reason and never ends the run: this line is an account of the work,
// never a part of it.
func (w *settlementWatch) lastCallWords() string {
	if w.graph == nil {
		return ""
	}
	call, found, err := w.graph.LastNamedCallSinceSeq(w.session, w.commandSeq)
	if err != nil || !found {
		return ""
	}
	return fmt.Sprintf(" · last call %s %s ago", call.Model, time.Since(call.At).Round(time.Second))
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
	outcome := headlessOutcome{
		Nodes: len(nodes), Artifacts: []string{},
		// The generalist until a row says otherwise, which is what an empty
		// column has meant everywhere else since the day it was added.
		Subharness: exec.LinearSubharness,
	}
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
	// The artifact record is read before a word of narration is written, because
	// narration that has not seen it is free to contradict it — and did. The
	// registry is the record; prose is what is left when there is no registry.
	if paths := w.produced.list(); len(paths) > 0 {
		outcome.Artifacts = paths
	} else {
		outcome.Artifacts = errandArtifacts(nodes)
	}
	if final != nil {
		// What ran the deliverable, as the store settled it. A machine caller
		// asking "which worker took this issue" was reading the answer out of a
		// kept sqlite file until this line existed.
		if worker := promisedWorker(*final); worker != "" {
			outcome.Subharness = worker
		}
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
			outcome.Deliverable = midFlightWords(outcome.Artifacts)
		default:
			outcome.Deliverable = strings.TrimSpace(final.Summary)
			// The verdict has to agree with the page. A rejected delivery and a
			// job missing its own parts both wrote the shortfall into the
			// deliverable and then left exit 0 under it, so every harness that
			// reads the code — which is the contract, and the only thing a
			// pipeline reads — recorded them as work that stands.
			if !w.deliveredWhole(*final) {
				outcome.status = exitPartial
			}
		}
		outcome.Deliverable = groundedInArtifacts(outcome.Deliverable, outcome.Artifacts)
		// One list, once. Grounding has had its look at the narration as the
		// worker wrote it, so the worker's own file list has done its job and
		// comes back off before anything is printed.
		outcome.Deliverable = withoutSummaryFileList(outcome.Deliverable, outcome.Artifacts)
	}
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

// deliveredWhole answers the exit code's own question of a settled job: is what
// is above the whole of what was asked for?
//
// Two facts say no, and both were already written on the page before this
// existed. The delivery gate is the system's own reading of whether the person
// who asked would accept this, and a rejection it stood by is not a success. A
// part of the job that failed or was cancelled is the same shortfall stated
// structurally, and the deliverable already carries it in words — "Not all of
// this landed: 1 of 2 parts finished" — which is precisely the line that was
// measured going out over exit 0.
//
// A verdict the system itself overruled is not a rejection — but OVERRULED HAS
// TO MEAN CHECKED AGAINST THE WORLD, and for a while it meant any refusal at
// all. A gap the one polish pass closed delivers whole because the work was
// redone. A gap refused because the file it says is missing is on disk under the
// name the request used, or because the things it says are absent are in the
// text the person is about to read, delivers whole because the finding was
// weighed against the filesystem or against the deliverable and lost. Charging
// either of those a non-zero code would teach a harness to distrust the gate's
// own corrections. That is store.DeliveryGate.Overturned, and it is the only
// refusal that acquits.
//
// EVERY OTHER REFUSAL LEAVES THE FINDING STANDING. A citation refused for its
// provenance — "what the review asked for next is not in the request", "the same
// words were already worked on once" — has been checked against nothing in the
// world. It declines to BUY a round; it settles nothing about whether the work
// landed, because no ruling about where a review got its words makes missing
// work appear. A repair a governor would not fund, or that nothing could plan,
// or that the wall has no room for, is the same shape (store.DeliveryGate.
// Unclosed). So is a mechanical gap: a file the plan itself promised, missing or
// empty on disk, is a fact about the filesystem that no admission rule is
// competent to overturn.
//
// The measured cost of collapsing those into one field is the whole DeepSWE
// sweep: seven of eight graded runs exited 0 — "delivered whole" — with reward
// 0, each of them after its own review had named the missing work and been
// refused on provenance (2026-08-28, bench/deepswe/AUTOPSY.md; the reasoning is
// docs/design/gate/SETTLEMENT.md §2). Before that, one run shipped "Deliverable
// is empty - contains no implementation" over exit 0 for want of the Unclosed
// field, and another produced no file at all and reported settled, done, success
// under a note explaining that the review had overreached
// (2026-08-28, meta/muse-spark-1.1). Exit 2, partial, is the honest code for a
// job that delivered less than it promised.
//
// An unreadable store answers whole. This decides an exit code, not the work,
// and a failed read is not evidence of a shortfall.
func (w *settlementWatch) deliveredWhole(node store.Node) bool {
	if gate, ok, err := w.graph.DeliveryGateFor(node.ID); err == nil && ok &&
		!gate.Pass && !gate.PolishClosed && !gate.Overturned {
		return false
	}
	parts, err := w.graph.SubtreeNodes(node.ID)
	if err != nil {
		return true
	}
	for _, part := range parts {
		if part.ID == node.ID {
			continue
		}
		if part.Status == store.Failed || part.Status == store.Cancelled {
			return false
		}
	}
	return true
}

// artifactsNamed bounds how many paths a grounded closing line spells out. The
// rest are counted, because a person reads the first few and the footer under
// the deliverable already lists every one of them.
const artifactsNamed = 5

// groundedInArtifacts holds one closing line answerable to the artifact record.
//
// The rule this enforces, from the audit: a run's own summary is not admissible
// evidence about what the run produced. A judge that concluded failure, a
// template that fired on a wall, a worker's last half-sentence — any of them may
// say nothing came of this while five files sit on disk, and a reader (or a
// downstream judge consuming this text) has no way to know it is false. So when
// the record shows deliverables the narration never names, the record is
// appended to the narration rather than allowed to be contradicted by it. A
// deliverable that already names its files is left exactly as written.
func groundedInArtifacts(deliverable string, artifacts []string) string {
	if len(artifacts) == 0 || namesAnyArtifact(deliverable, artifacts) {
		return deliverable
	}
	body := strings.TrimSpace(deliverable)
	if body == "" {
		return producedWords(artifacts)
	}
	return body + "\n\n" + producedWords(artifacts)
}

// namesAnyArtifact reports that the narration already points at the record. One
// named path is enough: a summary that lists what it wrote is grounded, and
// restating the list under it is noise the footer already carries.
func namesAnyArtifact(deliverable string, artifacts []string) bool {
	for _, path := range artifacts {
		if path != "" && strings.Contains(deliverable, path) {
			return true
		}
	}
	return false
}

// producedWords is the record, in a sentence. It never judges the work — it
// says what exists and where, and tells the reader that this line is not the
// evidence, the files are.
func producedWords(artifacts []string) string {
	named := artifacts
	rest := ""
	if len(named) > artifactsNamed {
		rest = fmt.Sprintf(" (and %d more)", len(named)-artifactsNamed)
		named = named[:artifactsNamed]
	}
	return fmt.Sprintf("Whatever the account above says, %s reached disk and can be opened: %s%s. Read them rather than this summary — the files are the record.",
		plural(len(artifacts), "file"), strings.Join(named, ", "), rest)
}

// midFlightWords is what a run that ended inside a split has to say. It used to
// end "Nothing here is the answer" unconditionally, which is how a run that
// returned rc=0 and five correct files told its caller it had produced nothing.
// The blanket denial is only honest when the record is empty.
func midFlightWords(artifacts []string) string {
	const opening = "The work was still mid-flight when time ran out: it had split into further pieces that never finished."
	if len(artifacts) == 0 {
		return opening + " Nothing here is the answer."
	}
	return opening + " No closing summary was written, so this line is not the answer — but the run was not empty-handed. " +
		producedWords(artifacts)
}

// wallWords is the same honesty at the wall: "before anything finished" is a
// claim about the record, and it may only be made when the record agrees.
func wallWords(artifacts []string) string {
	if len(artifacts) == 0 {
		return "The time limit was reached before anything finished."
	}
	return "The time limit was reached before the work was summarised. " + producedWords(artifacts)
}

// errandRegistry is this errand's own record of what its workers wrote.
//
// The workspace already knows: it registers every deliverable file a leaf
// produces, under that leaf's key. The trouble is that it dies with the
// goroutine that ran the leaf, and until this existed the only thing left
// afterwards was prose — so the answer to "what did this run produce?" was a
// regex over a worker's summary, which credited nothing at all to a worker that
// wrote "hello.txt" instead of naming the whole path. A run that had written a
// file reported artifacts: [].
//
// So the runner hands the registry's own list here as each leaf lands, and this
// is what the footer prints and what --json carries. It is a set because a
// repair round re-states the whole list, and it is locked because leaves land
// in parallel.
type errandRegistry struct {
	mu    sync.Mutex
	seen  map[string]bool
	paths []string
}

// add takes absolute paths from a leaf that has just landed.
func (r *errandRegistry) add(paths ...string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen == nil {
		r.seen = make(map[string]bool, len(paths))
	}
	for _, path := range paths {
		if path == "" || r.seen[path] {
			continue
		}
		r.seen[path] = true
		r.paths = append(r.paths, path)
	}
}

// list is what the registry recorded, in path order and filtered to what is
// still there. A file a leaf wrote and a later step deleted is not something
// the person can open, and the same existence rule the prose scrape has always
// applied is the right one here.
func (r *errandRegistry) list() []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := make([]string, 0, len(r.paths))
	for _, path := range r.paths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			kept = append(kept, path)
		}
	}
	sort.Strings(kept)
	return kept
}

// errandArtifacts recovers the files this errand wrote by reading the workers'
// own prose. It is the fallback and not the answer: the registry above is what
// the workspace actually recorded, and this only runs when there is no registry
// to read — a run this process deferred to a resident holding the store's lock
// did its work in another process entirely, and its summaries are all that
// reaches here.
//
// Existence on disk is the filter — a path named in prose that is not there is
// not a file the person can open, and offering it would be worse than saying
// nothing. Absolute paths only, for the same reason: a bare word can be
// anything.
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

// withoutSummaryFileList takes the worker's own "Files:" block back off the
// deliverable, because the errand prints the same paths under it as its footer
// and a person reading stdout got the identical list twice, back to back.
//
// The footer is the right home and the summary is not. The footer is the
// errand's own record — it is what --json carries, it is one line per file, and
// it is there whether or not any worker thought to mention what it wrote. The
// block in the summary is a worker addressing a reader in prose, and it is a
// list in the middle of a sentence-shaped answer.
//
// It comes off HERE and not at the point the worker writes it, because the
// store's copy of that summary is load-bearing elsewhere: a node downstream of
// this one is given the files its dependency produced by reading absolute paths
// straight out of that text (store.summaryPaths), and a summary stripped of
// them would starve it. So the graph keeps the block and stdout does not.
//
// Only the errand's own files are dropped. A block naming something this run
// has no record of is left exactly as the worker wrote it — it is then telling
// the reader something the footer will not.
func withoutSummaryFileList(deliverable string, artifacts []string) string {
	start := strings.LastIndex(deliverable, summaryFileList)
	if start < 0 {
		return deliverable
	}
	ours := make(map[string]bool, len(artifacts))
	for _, path := range artifacts {
		ours[path] = true
	}
	lines := strings.Split(deliverable[start+len(summaryFileList):], "\n")
	dropped := 0
	for dropped < len(lines) && ours[strings.TrimSpace(lines[dropped])] {
		dropped++
	}
	if dropped == 0 {
		return deliverable
	}
	kept := strings.TrimSpace(strings.Join(lines[dropped:], "\n"))
	body := strings.TrimRight(deliverable[:start], "\n")
	if kept == "" {
		return body
	}
	return body + "\n\n" + kept
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
// 1 means the work failed or was refused, 2 means what is above is a partial —
// the wall came first, the delivery gate rejected it, or parts of it did not
// land.
func errandStatus(outcome headlessOutcome) error {
	if outcome.status == 0 {
		return nil
	}
	return outcome.status
}

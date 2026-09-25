package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/router"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/calllog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/ctxbudget"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/exec"
	"github.com/Agent-Field/codeaf/internal/head"
	homepkg "github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/lease"
	"github.com/Agent-Field/codeaf/internal/resident"
	"github.com/Agent-Field/codeaf/internal/revision"
	runengine "github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/trace"
)

// `codeaf do` is one errand, start to finish, with nobody watching.
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
	// defaultDoWall is a wall, not a schedule. Real work runs for minutes;
	// this is the length of rope at which a wedged run is more useful dead.
	//
	// It is the RUN's wall and not a leaf's room, so the leaf-room law in
	// internal/exec — one place sizes a worker's budget — does not reach it.
	// That the two figures happen to be the same fifteen minutes is a
	// coincidence of what a reasonable length of rope is, not a shared source.
	defaultDoWall = 15 * time.Minute
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

// headlessOutcome is what one errand came to. It is `codeaf do`'s own shape,
// and it is turned into the one machine contract every headless verb returns by
// [errandEnvelope] on the way out (envelope.go) — nothing marshals this struct.
//
// Settled means the errand is over — nothing this run is waiting for can still
// move — and it is deliberately not a verdict on the work. A run stopped by a
// question is settled and did nothing (exit 4); a run holding a tree its own
// checks could not collect is not settled, because making that tree build is
// still work waiting to move. BlockedOn tells a machine caller when the first
// happened, and unfinishedTree tells this function when the second did.
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
	// started is when this invocation opened, and coreDoneSeconds is how long
	// it took to finish the requested work: the first moment a delivery gate
	// found that work done. Both are unexported — they reach a caller only
	// through the envelope's `core_done_seconds`, which is the one spelling
	// every reader shares.
	started         time.Time
	coreDoneSeconds float64
	Settled         bool `json:"settled"`
	// unfinishedTree is the finished-tree reading's own sentence on the one run
	// that cannot be called settled: its checks failed to collect. It stays
	// unexported because the sentence leaves through Deliverable, while Settled
	// is already the machine signal and the JSON contract needs no second key.
	unfinishedTree string
	// recordKept is printed after the receipt's own stderr lines, so a kept
	// private run folder is always the last line a person sees there.
	recordKept string
	// machineHeld says BlockedOn is the machine's hold rather than a question:
	// the wall arrived while the machine held every start, so nothing began.
	// stderr already said so when the hold began ([machineHold]), and saying
	// "it stopped to ask" over it would name a question nobody asked.
	machineHeld bool
	// Run, Calls, Rounds and Redispatches are what a person went to
	// `calls.jsonl` to reconstruct: which run this was, how many model calls it
	// made, how many times it bought more work after looking at what it had,
	// and how many times it sent a node round again in place after the node ran
	// out of its room. They are unexported spellings of the envelope's own keys
	// — the receipt reaches a caller through [errandEnvelope] and nowhere else.
	run          string
	calls        int
	rounds       int
	redispatches int
	// tokensIn and tokensOut are the token half of the bill, summed out of the
	// same journal read that priced the run. They are unexported because they
	// reach a caller only through the envelope's `tokens` field, which is the
	// one spelling all three headless verbs share.
	tokensIn  int
	tokensOut int
	// workspace is the directory this errand worked in, absolute, as
	// errandWorkspace resolved it. It is the answer to "where did the work
	// go?", and it is on the outcome rather than read again at the end
	// because it is decided once, at the door, and a second resolution is a
	// second answer. Empty when this invocation never opened one or handed the
	// work to a resident whose actual directory it cannot establish.
	workspace string
	// BlockedOn is the question this run could not answer, verbatim. It is
	// empty on every run that was not stopped by one, and non-empty only
	// alongside a non-zero exit code and an empty deliverable.
	BlockedOn string `json:"blocked_on,omitempty"`
	// Learned is the job's own board: what workers shared with each other
	// mid-flight — discoveries about the material, pitfalls, a sibling's
	// failure and why. An ephemeral store evaporates on exit, and these lines
	// are the one piece of what the run understood that would die with it.
	Learned []string `json:"learned,omitempty"`
	// Model and PlanModel are the two seats this errand ran on, and the two
	// Source fields name the rung that chose each — `--model`, `CODEAF_MODEL`,
	// `pinned`, `routed` (config.ResolveSeats). They are here because the
	// defect that produced them was invisible from outside: a campaign that
	// believed its profile's crew was in force had no way to read back that the
	// run had resolved its models somewhere else entirely (#166). A caller
	// comparing two cells can now assert what actually ran instead of trusting
	// the shell it launched them from.
	//
	// PlanModel is empty on a run whose planning rode the work model, which is
	// the ordinary shape; the field is always present, because an absent key is
	// indistinguishable from an older binary.
	Model           string `json:"model"`
	PlanModel       string `json:"plan_model"`
	ModelSource     string `json:"model_source"`
	PlanModelSource string `json:"plan_model_source"`
	// crew is the router's decision for this run — the class it read the task
	// as, every seat's pick and the estimate — and nil when nothing routed.
	// The envelope carries it as `class`, `crew`, `est_usd` beside `spend`,
	// which is the actual (envelope.go's [legacyErrandFields]).
	crew *crewroute.Decision
	// checkModel and checkModelSource are the third seat, beside the two
	// above.
	checkModel, checkModelSource string
	// Subharness is the worker that took the deliverable, read back from the
	// durable row rather than from what was asked for. It is always present and
	// never empty, because an absent key is indistinguishable from an older
	// binary — and a graph written by one of those may still name a worker this
	// build does not have.
	Subharness string `json:"subharness"`
	// Unjudged is why NOTHING CHECKED THIS DELIVERY, in the gate's own words off
	// the journal, on the runs where nothing did. Empty on every run whose gate
	// answered, which is almost all of them.
	//
	// It is a field rather than a sentence folded into the deliverable because
	// the caller it is for is a machine. A rig comparing runs has to put an
	// unchecked delivery in its own column, and reef-145 shipped two of them
	// into a column of judged passes because the only trace was a line in the
	// log (#514). `stop` says "unchecked"; this says why, and the two travel
	// together.
	Unjudged string `json:"unjudged,omitempty"`
	// JudgedBy names the settled root's answered gate attempt, including an
	// unreadable answer. It does not prove the check passed. Missing root rows
	// omit it, so split jobs and runs that reached no gate can omit both it and
	// Unjudged; callers read stop and ok for the outcome.
	//
	// It exists because THE ERRAND ROAD'S OWN POSTURE WAS UNDISCOVERABLE FROM
	// OUTSIDE IT. `task.audit` is the row that governs a task the conversation
	// hands out (internal/session's task_audit.go), it is the only audit row
	// this program has, and it does not reach here — an errand's delivery is
	// judged by internal/revision's gate instead. A reviewer holding a settled
	// `--json` object had no field naming either, so the honest reading of a
	// clean envelope was "nothing is listed, so perhaps nothing checked it"
	// (#618).
	JudgedBy string `json:"judged_by,omitempty"`
	// KeptBranch names the branch the errand's own work is standing on, on the
	// runs that did not settle whole. It is the answer to "where is the work
	// this run would not land?" — the one question a non-verified run left a
	// reader to answer by hand. A `do` errand works in place, so this is the
	// workspace's own branch: the work is real and it is in that tree, on that
	// branch, and no field used to say so.
	//
	// It is empty on every run that settled whole, because that run's work is on
	// the branch its caller already reads. A run whose work is on no named branch
	// — a detached HEAD, a workspace that is not a repository, a run deferred to
	// another process — names none, exactly as a run that kept nothing does.
	KeptBranch string `json:"kept_branch,omitempty"`
	// Verdict is what left the work where KeptBranch names it, in the record's
	// own words: `failed` for a node the store settled failed or cancelled, and
	// `unverified` for one that ran and then nothing could say the work holds.
	//
	// IT IS NOT A SECOND `stop` UNDER A NEW NAME. `stop` names why THIS process
	// ended, in the envelope's one vocabulary; this names what the work's own
	// record says became of it, in the task record's. They travel together
	// because a script branching on either wants both — how much is wrong and
	// what the store decided — and neither can be read off the other.
	Verdict string `json:"verdict,omitempty"`
	// Checklist is what became of each thing the request asked for, on exactly
	// the runs whose journal carried a checklist. It is a field because machine
	// callers must never parse the bounded person's account, and it is never
	// clipped.
	Checklist []revision.PointOutcome `json:"checklist,omitempty"`
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

	// stop is WHY THIS RUN ENDED, and it is the only thing this file decides
	// about the ending. It is set where the outcome is produced, because only
	// there is the difference visible between a job that failed, a price that
	// was refused, a question nobody could answer and a wall that arrived first
	// — four things that are all "not a success" and none of which are each
	// other. What the process leaves with is not decided here at all: the one
	// ladder in envelope.go turns this word into a number.
	stop stopReason
	// wall says this run's own clock fired. It is the one ending that leaves
	// with 124 rather than a rung of the shared ladder, because the run engine
	// hands back the same incomplete word for a wall and for a leaf that failed
	// ([runengine.Supervisor.Run]) and a caller raising a timeout has to be able to
	// tell the two apart. The envelope's `stop` is still deadline — the wall's
	// word in the one vocabulary all three headless verbs speak — so the number
	// and the word agree that nothing stands, which is the whole of the
	// contract's promise about the two.
	wall bool
}

// resolvedStop is this outcome's ending with the one absent case filled in.
//
// EVERY ENDING THAT IS NOT A CLEAN ONE NAMES ITSELF — compose defaults to
// stopDone and each of the four other paths writes its own word — so an outcome
// that names nothing is a run in which nothing said it had gone wrong. The one
// exception is an outcome built by hand somewhere that only filled in Error,
// which is a run that never started.
//
// It is one function because the exit code and the envelope's `ok` must be the
// same fact, and they were read from two places in the shape this replaced.
func (o headlessOutcome) resolvedStop() stopReason {
	if o.stop != "" {
		return o.stop
	}
	if strings.TrimSpace(o.Error) != "" {
		return stopError
	}
	return stopDone
}

// status is what the process leaves with, read off the one exit ladder. There
// is no second reading of it anywhere in this binary.
func (o headlessOutcome) status() exitStatus {
	// THE WALL IS ITS OWN NUMBER, and it is the only ending that leaves the
	// ladder. 124 is the number the timeout(1) convention and the run bench both
	// spell a wall with ([bench/bashloop/door.go]), and a run engine reports a
	// wall and a failed leaf with the same word, so the difference has to be
	// taken here or not at all. Every other ending is the ladder's.
	if o.wall {
		return exitStatus(124)
	}
	return exitFor(o.resolvedStop())
}

func runDo(args []string) error {
	flags := commandFlags("do")
	database := flags.String("db", "", "work in this durable store instead of a private one "+
		"(older engine only; the run engine refuses it)")
	keep := flags.Bool("keep", false, "keep the run's store instead of deleting it on the way out, "+
		"and say where it is")
	workspace := flags.String("dir", "", "the directory to work in, edited in place (default: the current directory)")
	shorthandFlag(flags, "w", "dir")
	wall := wallFlag{wall: defaultDoWall}
	flags.Var(&wall, "timeout", "hard wall, as a duration such as 15m or 2h (a bare number is seconds, kept for one release)")
	asJSON := flags.Bool("json", false, jsonFlagHelp)
	yesSpend := flags.Bool("yes-spend", false, yesSpendFlagHelp)
	model := flags.String("model", "", modelFlagHelp)
	planModel := flags.String("plan-model", "", planModelFlagHelp)
	checkModel := flags.String("check-model", "", checkModelFlagHelp)
	// HOW HARD TO TRY THIS ONE TASK, said on the command line and sticking to
	// nothing: --best puts the strongest crew the allowed models make on it,
	// --cheap the cheapest, and --pin seats one seat for this run alone. The
	// three seat flags above are one-task pins too (config.ResolveSeats).
	best := flags.Bool("best", false, "run this task on the strongest crew your allowed models make")
	cheap := flags.Bool("cheap", false, "run this task on the cheapest crew your allowed models make")
	var pins pinFlags
	flags.Var(&pins, "pin", "use one model for this run only: worker=model[@provider], planner=… or checker=… (repeatable)")
	// A FLAG IS DOCUMENTED BY WHAT IT DOES, NOT BY WHAT IT SETS. These two said
	// "…; sets CODEAF_CONTEXT_FILL_PCT for this run", which is the
	// implementation, and hard-coded their defaults in prose while their own
	// DefValue was 0 — two spellings of one number, and one of them would drift.
	// The figures are interpolated from the constants that own them now.
	contextFill := flags.Int("context-fill", 0,
		"how full a model's context window may get before it is compacted, in percent "+
			"(default "+strconv.Itoa(ctxbudget.DefaultFillPercent)+", clamped 10-90)")
	completionReserve := flags.Int("completion-reserve", 0,
		"tokens every call keeps free for its answer and its reasoning "+
			"(default "+strconv.Itoa(ctxbudget.DefaultCompletionReserveTokens)+")")
	slotsRaw := flags.String("slots", "",
		"how many workers may run at once for this run; 0 is no limit "+
			"(default: your task.parallel setting, which is no limit)")
	debug := flags.Bool("debug", false, debugFlagHelp())
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	noteRenamedFlags(flags)
	slots, err := parseSlots(*slotsRaw)
	if err != nil {
		return err
	}
	// THE RUN ID IS MINTED AT THE DOOR, once per invocation and before anything
	// can make a call, so that every record this errand leaves names the same
	// run. The folder is announced on the way out and only when something was
	// actually written into it: a path to an empty room is a door sending
	// somebody to look at nothing.
	if *debug {
		trace.Enable()
	}
	ctx := openDebugRecord("do", *model, *workspace)
	defer trace.Announce(ctx, os.Stderr)
	// The id the door just minted is carried rather than re-read: it is what
	// the `--json` envelope publishes and what every row this run writes into
	// the model-call log carries, and a second reading could name a different
	// run in a process that had opened two.
	run := trace.RunFrom(ctx)
	task, err := readText(flags.Name(), flags.Args())
	if err != nil {
		return err
	}
	if *best && *cheap {
		return fmt.Errorf("--best and --cheap ask for two different crews · say one")
	}
	effort := crewroute.EffortKnee
	switch {
	case *best:
		effort = crewroute.EffortBest
	case *cheap:
		effort = crewroute.EffortCheap
	}
	return doErrand(doRequest{
		effort: effort, pins: pins.pins,
		task: task, run: run, database: *database, keep: *keep, workspace: *workspace,
		timeout: wall.wall, asJSON: *asJSON,
		yesSpend: *yesSpend, model: *model, planModel: *planModel, checkModel: *checkModel,
		contextFill: *contextFill, completionReserve: *completionReserve, slots: slots,
		stdout: os.Stdout, stderr: os.Stderr,
	})
}

// doRequest is one invocation, with its streams named so a test drives the
// whole command rather than a piece of it.
type doRequest struct {
	task string
	// run is the id this invocation minted at the door ([trace.Begin]). It goes
	// out on the `--json` envelope, where it is the join to the model-call log
	// and to the debug record's folder, both of which are named by it.
	run        string
	database   string
	keep       bool
	workspace  string
	timeout    time.Duration
	asJSON     bool
	yesSpend   bool
	model      string
	planModel  string
	checkModel string
	// effort and pins are the one-task crew words: --best or --cheap, and
	// every --pin. They move this run's crew and nothing after it.
	effort crewroute.Effort
	pins   map[crewroute.Seat]config.CrewPin
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
	// callWall is the structuring slots' wall on one completion. Zero is
	// pool.DefaultCallWall; a test names one it can reach (brainOptions.callWall).
	callWall time.Duration
	// costCap is the run road's own ceiling on what the run may spend, in
	// dollars, and nil is no ceiling at all — the ordinary invocation, which has
	// no flag for one and adds none. It exists so a caller that does want to hold
	// a run to a price can say so, and a ceiling of nothing is a run that may
	// spend nothing: the limit stopped it before a worker did.
	costCap *float64
	// slots bounds how many run-engine workers run at once. Nil is the
	// person's own `task.parallel` setting, the same row the chat door reads,
	// and a named 0 is no bound at all (the `--slots` flag); a test names one
	// it can watch.
	slots *int
	// newBeltCompleter scripts the run road's worker, the way newClient scripts
	// the legacy road's. Nil builds a real provider client per seat model, which
	// is what a live run does; a test hands back a [session.Completer] that
	// answers without a network.
	newBeltCompleter func(model string) session.Completer
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
		if err := os.Setenv("CODEAF_CONTEXT_FILL_PCT", strconv.Itoa(fillPercent)); err != nil {
			return fmt.Errorf("set the context fill for this run: %w", err)
		}
	}
	if completionReserve > 0 {
		if err := os.Setenv("CODEAF_COMPLETION_RESERVE", strconv.Itoa(completionReserve)); err != nil {
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
	// The two seats, resolved before anything is opened or built, so the run
	// says which models it is about to use and on whose authority — and says it
	// even on a run that dies before it reaches a provider.
	//
	// The catalog is seated under the ladder first, because a tier row may say
	// `auto` and that word is answered from the rows this process already holds
	// (useAutoSeats). The seating read is the one the RUN will use — the key
	// included, since the environment outranks the profile file — because
	// [newSharedCatalog] is built once: a catalog seated from a KEYLESS read
	// would stay keyless for the whole process, and a run that meant to call
	// with a key would resolve its seats against a catalog that never fetched
	// its rows. A run with no key anywhere still seats keyless here — the
	// keyless load is the second rung, kept so a profile with no key prints its
	// seat line before the missing-key sentence — and a warm cache is read with
	// or without a key.
	settings, err := config.Load()
	if err != nil {
		settings, err = config.LoadKeyless()
	}
	if err == nil {
		useAutoSeats(settings)
	}
	profileDir := config.ProfileDir()
	// A PROFILE WRITTEN BEFORE CREWS WERE ROUTED IS MIGRATED ONCE, and the one
	// line saying so is said here, on stderr, where a person reads the models
	// line (internal/config's crewmigrate.go).
	if line, _ := config.MigrateCrew(profileDir); line != "" {
		fmt.Fprintln(request.stderr, line)
	}
	// THE CREW IS ROUTED FOR THIS TASK: the flags and the environment are
	// one-task pins, a --pin is one too, and every seat nothing named is picked
	// for what the task reads as (config.ResolveSeats). THE CHECK SEAT IS ITS
	// OWN SEAT and never inherits the planner's model.
	repo := request.workspace
	if repo == "" {
		repo, _ = os.Getwd()
	}
	if abs, err := filepath.Abs(repo); err == nil {
		repo = abs
	}
	seats, err := config.ResolveSeats(profileDir, config.SeatFlags{
		Model: request.model, PlanModel: request.planModel, CheckModel: request.checkModel,
	}, config.CrewAsk{
		Task: crewroute.Task{Text: request.task}, Effort: request.effort, Pins: request.pins, Repo: repo,
	})
	if err != nil && !errors.Is(err, config.ErrCrewAtCap) {
		// A SEAT NOTHING ALLOWED CAN SIT, or a pin that will not route, is said
		// before anything is opened: there is no crew to run on.
		if !request.asJSON {
			return err
		}
		outcome := failedErrand(err, started)
		outcome.run = request.run
		return reportErrand(request, outcome)
	}
	if errors.Is(err, config.ErrCrewAtCap) && !request.yesSpend {
		// AT THE DAILY CAP A HEADLESS RUN REFUSES: nobody is there to ask, and a
		// cap that spends anyway is not a cap. -yes-spend is the one way past.
		capErr := fmt.Errorf("today's crew spend has reached the daily cap of %s · raise it or turn it off with `/crew cap`, pass -yes-spend, or wait until midnight",
			crewroute.Money(config.CrewCapAt(profileDir)))
		if !request.asJSON {
			return capErr
		}
		outcome := failedErrand(capErr, started)
		outcome.seated(seats)
		outcome.run = request.run
		return reportErrand(request, outcome)
	}
	fmt.Fprintln(request.stderr, seats.Report())
	call := router.CrewCallID(request.run)
	if seats.Crew != nil {
		config.LogCrewDecision(profileDir, call, *seats.Crew, repo, crewTitle(request.task))
	}
	outcome, err := errandRun(request, seats, started)
	if err != nil {
		if !request.asJSON {
			if outcome.recordKept != "" {
				fmt.Fprintf(request.stderr, "record kept at %s\n", outcome.recordKept)
			}
			if seats.Crew != nil {
				config.LogCrewOutcome(profileDir, call, *seats.Crew, repo, crewTitle(request.task), router.CrewNotKept, 0)
			}
			return err
		}
		recordKept := outcome.recordKept
		outcome = failedErrand(err, started)
		outcome.recordKept = recordKept
	}
	outcome.seated(seats)
	// THE CREW'S OUTCOME, beside its decision in the router's log: accepted
	// when the run came home done, not kept otherwise — and the summary line
	// with the actual beside the estimate.
	if seats.Crew != nil {
		settled := router.CrewNotKept
		if outcome.resolvedStop() == stopDone {
			settled = router.CrewAccepted
		}
		config.LogCrewOutcome(profileDir, call, *seats.Crew, repo, crewTitle(request.task), settled, outcome.Spend)
		fmt.Fprintln(request.stderr, "crew: "+seats.Crew.Line(config.PinMark, outcome.Spend))
	}
	// THE RUN NAMES ITSELF ON EVERY PATH, including the one where nothing
	// worked: the id is what joins this object to the rows the model-call log
	// wrote and to the debug record's folder, and a run that fell over after
	// making four calls is exactly the run somebody goes to that log about.
	// Both are read HERE, once, for the same reason the seats are — beside
	// every return is where one of them gets forgotten.
	outcome.run = request.run
	outcome.calls = calllog.CallsFor(request.run)
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
		// The same sentence a person would have read on stderr, held to the
		// same rule: the cause and what to do about it, and no wrapped Go
		// chain (plainwords.go). A caller reading --json and a caller reading
		// the error stream must not be told two different things.
		Error: plainWords(err.Error()),
		stop:  stopError,
	}
}

// seated writes the run's two seats onto the outcome --json prints. It is one
// place rather than beside every return, because the seats are decided once,
// before the errand starts, and belong on the object whether it ends in an
// answer or in a sentence about why there is none.
func (o *headlessOutcome) seated(seats config.Seats) {
	o.Model = seats.Work.Model
	o.PlanModel = seats.Plan.Model
	o.ModelSource = seats.Work.Rung()
	o.PlanModelSource = seats.Plan.Rung()
	o.checkModel, o.checkModelSource = seats.Check.Model, seats.Check.Rung()
	o.crew = seats.Crew
}

// crewTitle is the first line of a task, cut short: what the router's log
// names a headless task by.
func crewTitle(task string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(task), "\n")
	if runes := []rune(line); len(runes) > 80 {
		line = string(runes[:80]) + "…"
	}
	return line
}

// pinFlags is the repeatable --pin: seat=model[@provider], one per flag.
type pinFlags struct {
	pins map[crewroute.Seat]config.CrewPin
}

func (p *pinFlags) String() string {
	if p == nil || len(p.pins) == 0 {
		return ""
	}
	var said []string
	for _, seat := range crewroute.Seats {
		if pin, ok := p.pins[seat]; ok {
			said = append(said, string(seat)+"="+pin.String())
		}
	}
	return strings.Join(said, ",")
}

func (p *pinFlags) Set(raw string) error {
	seatWord, value, ok := strings.Cut(raw, "=")
	seat, known := config.ParseCrewSeat(seatWord)
	if !ok || !known {
		return fmt.Errorf("--pin takes seat=model[@provider], and the seat is worker, planner or checker")
	}
	pin, auto, err := config.ParseCrewPin(value)
	if err != nil {
		return err
	}
	if auto {
		return fmt.Errorf("--pin %s=auto pins nothing · leave the flag off to have the seat routed", seat)
	}
	if p.pins == nil {
		p.pins = map[crewroute.Seat]config.CrewPin{}
	}
	p.pins[seat] = pin
	return nil
}

// errandRun is the errand itself: everything from opening a store to composing
// what came of it. It reports nothing and decides no exit code — both belong to
// doErrand, so that a failure anywhere in here reaches the caller through the
// same door as an answer.
func errandRun(request doRequest, seats config.Seats, started time.Time) (outcome headlessOutcome, err error) {
	if err := applyContextLaw(request.contextFill, request.completionReserve); err != nil {
		return headlessOutcome{}, err
	}
	// THE RUN ENGINE IS THE DEFAULT ROAD, BEHIND THE SAME SWITCH AS THE BASH
	// BELT. With the belt on — every machine that has set nothing — the errand
	// is dispatched by the run engine over a private plan store rather
	// than by the resident's reconciler below. CODEAF_TASK_BELT set to one of
	// the words that turn the belt off is the only way onto the road below, and
	// with it set not one byte of that road moves.
	if session.BashBeltAsked() {
		return runErrand(request, seats)
	}
	path, home, ephemeral, err := headlessStore(request.database)
	if err != nil {
		return headlessOutcome{}, err
	}
	// Whether the private home outlives this run is decided at the END of it,
	// where the answer is known, rather than here where it is not — see
	// keepPrivateStore. The sentence naming the place is deferred with it for
	// the same reason: a home that is about to be deleted has no location worth
	// printing, and one that survives is only worth naming once there is a
	// reason it did.
	debugging := trace.Enabled()
	// AND A RECORD IS ANNOUNCED ONLY FOR A RUN THAT WAS ADMITTED — one whose ask
	// reached the journal. `record kept at <path>` used to print for runs that
	// never started at all: it stood directly above `permission denied` and
	// above the missing-key sentence, pointing somebody at an empty folder on
	// the exact line where they were already looking for the cause. A run that
	// got no further than its own door has nothing to keep, so the folder goes
	// and the line does not print — unless the person asked for it by name with
	// --keep or --debug, where an empty store is still the thing they asked for.
	admitted := false
	if ephemeral {
		defer func() {
			if !keepPrivateStore(request.keep, debugging, errandSucceeded(outcome, err)) {
				_ = os.RemoveAll(home)
				return
			}
			if !admitted && !request.keep && !debugging {
				_ = os.RemoveAll(home)
				return
			}
			fmt.Fprintf(request.stderr, "record kept at %s\n", home)
		}()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return headlessOutcome{}, fmt.Errorf("create the store directory: %w", err)
	}

	session := headlessSessionID()
	window, err := openChatWindow(path, session)
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
	preauthorized := spendPreauthorized(request.yesSpend, env.Value)
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

	// The workspace is resolved once at this door rather than inside
	// headlessBrain, so the ending can name it even when headlessBrain returns
	// early to a resident. Resolving it again would make a second answer.
	workspaceRoot, err := errandWorkspace(request.workspace)
	if err != nil {
		return headlessOutcome{}, err
	}
	// What the workers wrote, caught on its way past. Nothing fills it on a run
	// this process handed to a resident that already holds the store — that work
	// happens in another process, and the watcher falls back to prose there.
	produced := &errandRegistry{}
	brain, release, deferredTo, err := headlessBrain(window, session, request, seats, consent, ephemeral,
		workspaceRoot, produced)
	if err != nil {
		return headlessOutcome{}, err
	}
	// THE RUN IS ADMITTED HERE and not a line earlier. Everything above is the
	// door — the store, the journal row, the key — and a run that fell over at
	// the door left a folder with nothing in it. From here something is
	// actually working, so whatever happens next is worth keeping and worth
	// naming.
	admitted = true
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

	// AN INTERRUPT MUST LAND THE RUN, NOT VANISH IT — the same law `codeaf run`
	// keeps, and it is the keep-on-failure rule that made a headless errand need
	// it too. A Go process dies on Ctrl+C and on SIGTERM with nothing written,
	// which is indistinguishable from a crash; now that the store survives such
	// an ending, dying silently would leave a folder on disk that nothing ever
	// told the person about. Routed through the context, the watcher returns the
	// partial it returns for the wall, the workers are settled, and the closing
	// lines — the receipt and `record kept at` — still print.
	//
	// The handler is released the moment the wait is over, so a second signal
	// during the unwind kills the process the way it always did. SIGKILL is
	// outside all of this and stays correct by accident: no defer runs, so
	// nothing deletes the store either.
	signalled, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	// The person's own ending of the run is the one fact the usage counts
	// keep about how it ended: the interrupt word, not a failure word.
	// signalled cannot answer it — stopSignals cancels that context on the
	// way out too, so its Err reads the same for a clean end and a ctrl-c.
	// A channel only a signal fills can.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupts)
	defer func() {
		select {
		case <-interrupts:
			telemetryInterrupted = true
		default:
		}
	}()
	// A PERSON TYPED THIS, so its calls are made for somebody who is reading
	// them (exec.go's [typedDoorContext]): the talk pin rides them and a
	// refused pin is said to the one who is waiting.
	ctx, cancel := context.WithTimeout(typedDoorContext(signalled), request.timeout)
	defer cancel()
	watcher := &settlementWatch{
		graph: graph, session: session, commandSeq: command.Seq,
		refused: refused, progress: request.stderr, started: started,
		produced: produced,
		stopped:  func() bool { return signalled.Err() != nil },
	}
	if brain != nil {
		// A GATE ALWAYS PRECEDES THE WALL, and this is the half of that law
		// which does not depend on anybody asking. The governor stops a job
		// growing when the wall is near, but a job only asks to grow when
		// something in it ends; a job whose queued leaves keep starting never
		// asks, and it is exactly that job that reaches the wall unjudged. So
		// the watcher holds the same grip the governor does and uses it on the
		// clock alone. See settlementWatch.forceJudgement.
		watcher.closeOut = brain.runner.CloseOut
	}
	outcome, err = watcher.wait(ctx)
	stopSignals()
	if err != nil {
		return headlessOutcome{}, err
	}
	outcome.Seconds = time.Since(started).Seconds()
	outcome.started = started
	// The wall is the case that made this necessary. A leaf cancelled by the
	// timeout journals its usage row on the way down, which is after the
	// watcher has returned and — until this line moved the shutdown ahead of
	// the read — after the receipt had already been printed without it. One
	// leaf landing that late is the whole of the 36 % under-report.
	settle()
	// The watcher can return while a cancelled leaf is still registering its
	// last files. Read that record after shutdown, then describe what it holds.
	// A resident owns a different registry and cannot be spoken for here.
	if deferredTo == nil {
		outcome.workspace = workspaceRoot
		// AND WHERE THE WORK IT DID NOT LAND IS STANDING, which the outcome
		// cannot answer until there IS a workspace: it is read off the directory
		// the errand worked in, at the moment the run is over. A `do` errand
		// works IN PLACE — it edits the directory it was handed, on whichever
		// branch is checked out there — so that directory's own branch is where
		// its work is standing, and it is the only branch on this road the way a
		// chat `/task` has its own task/<slug> worktree. A run that settled whole
		// has no verdict and names no branch; a workspace that is not a
		// repository, or whose HEAD is detached, names none either — there is no
		// branch a person could check out.
		outcome.KeptBranch = errandKeptBranch(outcome)
		outcome = groundedAfterShutdown(outcome, produced)
	}
	priceErrand(graph, session, openedAt, &outcome)
	// THE ERRAND LEAVES A PENDING JUDGE RECORD AND NOTHING WAITS ON ONE. It is
	// written here, after priceErrand, so the run's own bill is settled first;
	// the judge that picks the row up later bills its own seat's row in the
	// usage ledger and never this envelope — the receipt above is a read of
	// SpendSinceSeq and may not disagree with it. A run this process handed to a
	// resident settles in that process instead, so it leaves no row here: the
	// work is not ours to describe, and the model and deliverable would be wrong.
	if deferredTo == nil {
		writeDoPendingLanding(request, seats, outcome)
	}
	return outcome, nil
}

// writeDoPendingLanding leaves the pending record `codeaf do` owns, for the
// Model Pool's judge to score on the next process that holds a live key — the
// way a chat task's landing reaches the live judge through
// session.Config.TaskLanded. The do door is not the chat door: it runs the
// resident's brain over a store journal and builds no session.Agent, so that
// seam never fires for it and the row is written by hand here instead.
//
// Nothing waits on the judge. The write is one append (poolrecord.go's
// writePendingLanding), the process exits at once, and a failure to write is
// debug-only — a landing nobody could score is an ordinary state and must not
// cost the run its result.
func writeDoPendingLanding(request doRequest, seats config.Seats, outcome headlessOutcome) {
	profileDir := config.ProfileDir()
	if !config.ModelPoolAt(profileDir).CanRead() {
		return
	}
	landing := session.TaskLanding{
		// THE ID IS MINTED FROM THE RUN'S START IN NANOSECONDS so two runs never
		// share one: the restart sweep dedups on it (poolrecord.go's judged
		// markers, keyed id-and-attempt), and a repeated id would swallow the
		// second run's judgement. The value is well past any node id this store
		// hands out, so a headless landing never collides with a chat one.
		ID:          uint64(outcome.started.UnixNano()),
		State:       session.TaskUnverified,
		Brief:       request.task,
		Deliverable: outcome.Deliverable,
		Wrote:       outcome.Artifacts,
		Changed:     len(outcome.Artifacts),
		Worker:      seats.Work.Model,
		Tokens:      outcome.tokensIn + outcome.tokensOut,
		CostUSD:     outcome.Spend,
	}
	if err := writePendingLanding(profileDir, headlessSurface, landing); err != nil && trace.Enabled() {
		log.Printf("do: pending landing: %v", err)
	}
}

// errandKeptBranch names the branch an errand's own work is standing on, and is
// empty on every run that needs no such name.
//
// IT IS COMPUTED ONLY WHERE IT MEANS SOMETHING. A run with no verdict settled
// whole — its work is on the branch its caller already reads, and a second name
// for it would be a fact dressed as a finding. And a directory that is not a
// repository, or whose HEAD is detached (`git rev-parse --abbrev-ref HEAD`
// answers the bare word `HEAD`), names no branch a person could check out, so
// it answers empty rather than a placeholder. Both are ordinary states and
// neither is an error: this decides how a run is described, and a git that
// cannot answer is not evidence about the work.
func errandKeptBranch(outcome headlessOutcome) string {
	if strings.TrimSpace(outcome.Verdict) == "" || strings.TrimSpace(outcome.workspace) == "" {
		return ""
	}
	return keptBranchIn(outcome.workspace)
}

// keptBranchIn names the branch a directory's own work is standing on, and is
// empty on every directory that names no branch a person could check out.
// It is the ONE reading of that question, shared by the three headless doors
// (#1182): a directory that is not a repository, and one whose HEAD is
// detached (`git rev-parse --abbrev-ref HEAD` answers the bare word `HEAD`),
// answer empty rather than a placeholder. Both are ordinary states and neither
// is an error: this decides how a run is described, and a git that cannot
// answer is not evidence about the work.
func keptBranchIn(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	out, err := gitIn(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(out)
	if branch == "" || branch == "HEAD" {
		return ""
	}
	return branch
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
	// AND THE TOKENS OFF THE SAME READ. A caller comparing two runs divides by
	// these, and taking them from a second query would be a second bill.
	outcome.tokensIn = spend.Work.PromptTokens + spend.Spine.PromptTokens
	outcome.tokensOut = spend.Work.CompletionTokens + spend.Spine.CompletionTokens
	// AND THE ROUNDS OFF THE SAME JOURNAL. How many times this run bought more
	// work is read from what was written down, for the reason the bill is: a
	// counter in this process could not see a round a resident spliced.
	outcome.rounds = errandRounds(graph, session)
	// AND THE RE-DISPATCHES OFF IT TOO. How many times this run sent a node
	// round again in place is read from the releases that handed work on, for
	// the same reason: the release is the record of a re-dispatch, and a
	// counter in this process could not see one a resident made.
	outcome.redispatches = errandRedispatches(graph, session)
	// AND WHEN THE REQUESTED WORK WAS FIRST FOUND DONE, off the same journal.
	outcome.coreDoneSeconds = errandCoreDoneSeconds(graph, session, outcome.started)
}

// errandCoreDoneSeconds is how long it took this errand to finish the requested
// work, measured from the run's start to the first delivery gate on one of its
// jobs that found the work done — a pass, or a coverage finding. It is the
// EARLIEST such moment across the errand's jobs, so a run that finished one part
// early and another late reports the first; a run whose gate never said so
// reports zero, which the envelope carries as an absent key rather than a
// fabricated instant.
func errandCoreDoneSeconds(graph *store.Store, session string, started time.Time) float64 {
	if graph == nil || started.IsZero() {
		return 0
	}
	nodes, err := graph.SessionMemberNodes(session)
	if err != nil {
		return 0
	}
	var earliest int64
	var at time.Time
	for _, node := range nodes {
		if node.Parent != store.RootID {
			continue
		}
		seq, when, ok, err := graph.DeliveryGateAnchor(node.ID)
		if err != nil || !ok {
			continue
		}
		if earliest == 0 || seq < earliest {
			earliest, at = seq, when
		}
	}
	if earliest == 0 {
		return 0
	}
	seconds := at.Sub(started).Seconds()
	if seconds < 0 {
		return 0
	}
	return seconds
}

// errandRounds is how many times this errand bought MORE WORK: every growth
// decision journaled against one of its jobs (store.JobGrowthRounds).
//
// It asks per job root rather than across the store because the journal keys
// growth by the job it grew, and a run sharing a durable store with another
// session must not count that session's rounds as its own — the same rule the
// bill is read under one function up. A read that fails leaves the count at
// zero rather than at a guess.
func errandRounds(graph *store.Store, session string) int {
	nodes, err := graph.SessionMemberNodes(session)
	if err != nil {
		return 0
	}
	rounds := 0
	for _, node := range nodes {
		if node.Parent != store.RootID {
			continue
		}
		grown, err := graph.JobGrowthRounds(node.ID)
		if err != nil {
			continue
		}
		rounds += len(grown)
	}
	return rounds
}

// errandRedispatches is how many times one of this errand's nodes was sent
// round again in place after running out of the room it was granted: every
// hand-on release its nodes journaled (store.NodeRedispatches).
//
// It asks per node rather than across the store because a release is journaled
// against the node that was re-dispatched, and the rule is the bill's and the
// rounds': a run sharing a durable store with another session must not count
// that session's re-dispatches as its own. A read that fails leaves the count
// at zero rather than at a guess.
func errandRedispatches(graph *store.Store, session string) int {
	nodes, err := graph.SessionMemberNodes(session)
	if err != nil {
		return 0
	}
	redispatches := 0
	for _, node := range nodes {
		counted, err := graph.NodeRedispatches(node.ID)
		if err != nil {
			continue
		}
		redispatches += counted
	}
	return redispatches
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
func headlessBrain(window *chatWindow, session string, request doRequest, seats config.Seats,
	consent func(store.Node, planEstimate) bool, ephemeral bool,
	workspaceRoot string, produced *errandRegistry) (*chatBrain, func(), *lease.Resident, error) {
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
	brain, err := buildBrain(window, session, brainOptions{
		ephemeral: ephemeral, workspaceRoot: workspaceRoot,
		sharedWorkspace: true,
		model:           request.model, planModel: request.planModel,
		seats:   &seats,
		consent: consent, newClient: request.newClient,
		callWall: request.callWall,
		produced: produced,
		// THE WALL THE WATCHER IS WATCHING IS THE WALL THE WORK RUNS UNDER.
		// Until this line the errand's timeout reached the settlement watcher
		// and nothing else, so the machinery that decides whether to buy
		// another round of work was running under context.Background() and
		// every "is there time left" rule in the program answered yes forever.
		// Two 5400-second runs ended at the wall mid-round with no gate cut
		// and settled: false, which is not a slow run — it is a run that was
		// never told when it had to be finished. See chatBrain.wall.
		wall: request.timeout,
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

// keepPrivateStore decides whether the private home a headless run made
// outlives the run that made it.
//
// A FAILURE KEEPS ITS OWN EVIDENCE WITHOUT BEING ASKED. A person discovers they
// wanted the record only after the run went wrong, and under the older rule —
// deleted on the way out, worked or not — that was always after it was gone;
// the only cure was to have passed --keep before knowing there would be
// anything to look at. So the home is deleted on a clean run and on nothing
// else: asked for on purpose, kept while the debug switch is on, and kept after
// any exit that was not a success.
func keepPrivateStore(asked, debugging, succeeded bool) bool {
	return asked || debugging || !succeeded
}

// errandSucceeded is the one reading of "this run worked": it reached an
// outcome of its own, and that outcome leaves with the status a script reads as
// nothing to say. A partial is not a success — something did not land, and why
// it did not is exactly what a person comes back for.
func errandSucceeded(outcome headlessOutcome, err error) bool {
	return err == nil && outcome.status() == exitDone
}

// headlessStore decides where this errand lives. The default is a private home
// that is deleted on a clean run, because isolation is the point of a one-shot:
// a task run this way must not inherit half a conversation's assumptions, and a
// store that survives it is what `codeaf chat` already is. What survives a run
// that did NOT go cleanly is keepPrivateStore's answer, not this one's.
//
// It is made under the state root's `runs/` and NOT in the operating system's
// temporary directory. Both were private and both were deleted on a clean run,
// so for as long as every run's home died with it the difference was invisible.
// It stops being invisible the moment a failed run keeps its own: a record in
// /tmp is a record the system's own reaper is entitled to delete out from under
// the person who was told where to find it, and on a machine that clears /tmp at
// boot the answer to "where is yesterday's failure" is nowhere. THE RECORD STAYS
// UNDER THE STATE ROOT, which is the one directory codeaf owns and nothing else
// prunes. CODEAF_HOME moves it with everything else, so a disposable run is
// still disposable in one word.
func headlessStore(database string) (path, home string, ephemeral bool, err error) {
	if database = strings.TrimSpace(database); database != "" {
		path, err = expandHome(database)
		if err != nil {
			return "", "", false, err
		}
		return path, filepath.Dir(path), false, nil
	}
	runs := homepkg.Join("runs")
	// 0700 for the reason every directory under the state root is: what a run
	// keeps is the person's own prompts, replies and deliverables, and a record
	// kept for their benefit must not become one the rest of the machine can read.
	if err = os.MkdirAll(runs, 0o700); err != nil {
		return "", "", false, fmt.Errorf("create a private store: %w", err)
	}
	home, err = os.MkdirTemp(runs, "codeaf-do-")
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

	// stopped reports whether a signal ended this run rather than its wall. It
	// is a function rather than a bool because the answer is only true at the
	// end, and nil is the ordinary case in every test that builds a watcher by
	// hand — see stoppedByHand.
	stopped func() bool

	watermark int64
	// saidStanding remembers that the closing reservation has been printed. The
	// compose that prints it is reached once on the settled path and once on the
	// timeout path, and a person told twice why their run was short reads the
	// second line as a second finding.
	saidStanding bool
	seen         map[string]store.Status
	// noted remembers which nodes have already had their degradation said, so a
	// build missing a worker admits it once per node rather than once per beat.
	noted map[string]bool
	// waiting is when this watcher first saw a node running with nobody named
	// as running it. The claim is granted and the row is stamped a fraction of
	// a second before the dispatch path builds the worker and writes it down,
	// so a poll can land in the gap; holding the line for that fraction is what
	// lets every ▶ say who. See runningWorkerGrace for what happens when the
	// fact never arrives.
	waiting map[string]time.Time
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
	// closeOut stops a job's outstanding work so that what has landed can be
	// judged. Nil is a run this process is not the brain of — the work is
	// happening in a resident, whose own governor holds the same grip — and it
	// forces nothing.
	closeOut func(jobRoot, keep, reason string) int
	// judged remembers the jobs this watcher has already driven to a verdict,
	// so a job that takes two beats to settle is not closed out twice.
	judged map[string]bool
}

// forcedJudgementReason is what a person reads on the parts that were stopped
// so the whole could be judged. It says the trade rather than the machinery: a
// run that spends its last minutes starting work the clock will kill has bought
// nothing and given up its only verdict.
const forcedJudgementReason = "there was not enough time left on this run to finish this, " +
	"so it was handed over to be checked as it stands"

// forceJudgement drives this errand's jobs to a verdict while the wall can still
// hold one.
//
// THE RUN MAY NOT END WITH NOTHING JUDGED. A gate is cut when a job settles, so
// a job still moving when the clock stops is a job nothing ever judged: ofetch
// v4-flash s13 spent 5407 seconds, 422 model calls and eight growth rounds and
// journaled ZERO delivery judgements, and its own governor had said twice, in
// the last ninety seconds, that the wall was too near for another round. The
// refusal stopped the growth; nothing stopped the queue.
//
// The distance is the job's own pace and not a number typed here — the median
// round it has been running plus the reading it takes of itself, which is the
// same quantity the governor refuses growth at, because what a job owes before
// a verdict exists is one more body of work and one more reading of the tree
// (resident.JobPace, PERF.md). A job that has shown no pace is not forced: with
// nothing measured there is no honest moment to choose, and stopping work early
// on a guess is the failure this whole mechanism exists to avoid.
//
// It fires only where NOTHING has been judged. A job whose lineage already
// carries a verdict has the record this exists to guarantee, and taking its
// last minutes away would buy a second opinion at the price of the work.
func (w *settlementWatch) forceJudgement(ctx context.Context) {
	if w.closeOut == nil {
		return
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return
	}
	nodes, err := w.sessionNodes()
	if err != nil {
		return
	}
	for _, node := range nodes {
		if node.Parent != store.RootID || terminalStatus(node.Status) || w.judged[node.ID] {
			continue
		}
		// The lineage and not the node: a repair round is a different node id
		// from the work it repairs, so a reader that asked the node would find
		// no verdict on a job that has already been judged three times.
		gates, err := w.graph.DeliveryGateLineage(node.ID)
		if err != nil || len(gates) > 0 {
			continue
		}
		pace := resident.JobPace(w.graph, node.ID)
		if pace <= 0 || time.Until(deadline) > pace {
			continue
		}
		if w.judged == nil {
			w.judged = make(map[string]bool)
		}
		w.judged[node.ID] = true
		stopped := w.closeOut(node.ID, "", forcedJudgementReason)
		if stopped > 0 && w.progress != nil {
			w.note("handing this over to be checked while there is still time", "")
		}
	}
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
			outcome.Settled, outcome.stop = false, stopPrice
			outcome.Deliverable = "The plan for this task crosses the spending threshold, so nothing was started."
			return outcome, nil
		case <-ctx.Done():
			// The wall. What exists is what the person gets, and the exit code
			// says it is not the whole answer.
			outcome, err := w.survey()
			if err != nil {
				return headlessOutcome{}, err
			}
			outcome.Settled, outcome.stop = false, stopDeadline
			// A wall a question was standing behind is not a slow run. Saying
			// which of the two it was costs one read and is the difference
			// between a diagnosable timeout and fifteen minutes of nothing.
			asked, questionErr := w.blockingQuestion()
			if questionErr != nil {
				return headlessOutcome{}, questionErr
			}
			outcome.BlockedOn = asked
			// AND A WALL WITH A QUESTION STANDING BEHIND IT IS THE QUESTION'S
			// ENDING, not the clock's. The clock is what it ran into while
			// waiting for an answer nobody was here to give, and "raise the
			// timeout" is the wrong remedy to hand somebody whose run needs a
			// sentence from them.
			if asked != "" {
				outcome.stop = stopQuestion
			}
			if strings.TrimSpace(outcome.Deliverable) == "" && asked == "" {
				outcome.Deliverable = wallWords(outcome.Artifacts, w.stoppedByHand())
			}
			// AND THE WALL SAYS WHAT THE GOVERNOR KNEW. A run that reaches its
			// deadline having already been told it stopped making progress must
			// not report the clock as the reason: the clock is what it ran into
			// afterwards. Said here as well as on the settled path because a
			// run killed mid-round never reaches compose at all.
			w.sayWallStanding()
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
			// And the wall, watched rather than waited for: a job that cannot
			// be judged after the clock stops is judged before it.
			w.forceJudgement(ctx)
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
		outcome.Settled, outcome.stop = true, stopIncomplete
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
			// A refusal that is a QUESTION is its own rung: the run needs an
			// answer and nobody was there to give one.
			outcome.stop = stopQuestion
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
	// A TREE THAT DOES NOT BUILD IS NEVER REPORTED SETTLED. Settled has always
	// meant that nothing this run is waiting for can still move, and repairing a
	// tree whose own checks could not collect is work still waiting to move even
	// after every node has stopped.
	outcome.Settled = outcome.unfinishedTree == ""
	return outcome, true, nil
}

// refusalWords is what a rejected command has to say for itself. The receipt
// the reconciler posted is the real answer — a compiler question, a charter
// awaiting ratification — and the command's own result is the summary of it.
//
// Whatever it is, it goes through [plainWords] on the way out. A refusal that
// happened deep in the stack arrives here as everything that wrapped it, and
// this is the last door before a person reads it (plainwords.go).
func (w *settlementWatch) refusalWords(command store.Command) string {
	words := strings.TrimSpace(command.Result)
	messages, err := w.graph.Messages(w.session, 0, 50)
	if err == nil {
		for index := len(messages) - 1; index >= 0; index-- {
			message := messages[index]
			if message.CommandSeq == command.Seq && strings.TrimSpace(message.Body) != "" {
				return plainWords(strings.TrimSpace(message.Body))
			}
		}
	}
	if words == "" {
		words = "the request was not turned into work"
	}
	return plainWords(words)
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
		fmt.Fprintf(w.progress, "  · understood · %s %s\n",
			plural(len(nodes), "task"), time.Since(w.started).Round(time.Second))
	}
	w.noteDegradedWorkers(nodes)
	// Everything the journal knows that a status column cannot say. It runs
	// before the status lines so that a fault is read above what it caused, in
	// the order the two things happened.
	w.narrate(nodes)
	changed := false
	for _, node := range nodes {
		if previous, ok := w.seen[node.ID]; ok && previous == node.Status {
			continue
		}
		if w.holdForWorker(node) {
			// Said on a later beat, with the worker in it. Not counted as
			// movement: the quiet line's whole job is to speak when nothing is
			// known, and a node whose record never arrives must not silence it.
			continue
		}
		changed = true
		w.seen[node.ID] = node.Status
		delete(w.waiting, node.ID)
		if node.Status == store.Pending {
			continue
		}
		fmt.Fprintf(w.progress, "  %s %-28s%s %s\n", statusMark(node.Status),
			clip(firstLine(nodeDisplay(node)), 28), ranWords(node),
			time.Since(w.started).Round(time.Second))
	}
	return changed
}

// runningWorkerGrace is how long a ▶ waits for the store to say who is running
// the node. A claim is granted, the row is stamped running, and the dispatch
// path writes the worker down a few milliseconds later, so a poll landing in
// that gap would otherwise print the one line that cannot answer the question
// the whole stream is there to answer.
//
// It is a grace and not a requirement, which is the point of having a figure at
// all: after it the line prints anyway, without the worker. A node whose record
// never arrives is a node that has still started, and losing its ▶ would trade
// a missing word for a missing line.
const runningWorkerGrace = 3 * time.Second

// holdForWorker answers whether this node's line should wait a beat. Only a
// node that has just started running waits, only while nobody has said what is
// running it, and only until the grace above runs out.
func (w *settlementWatch) holdForWorker(node store.Node) bool {
	if statusMark(node.Status) != "▶" {
		// A held claim goes back to pending and may be granted again later. The
		// grace is per run of the node and not per errand, so the clock is
		// dropped the moment the node stops running.
		delete(w.waiting, node.ID)
		return false
	}
	if strings.TrimSpace(node.Ran) != "" {
		return false
	}
	if w.waiting == nil {
		w.waiting = make(map[string]time.Time, 1)
	}
	first, seen := w.waiting[node.ID]
	if !seen {
		w.waiting[node.ID] = time.Now()
		return true
	}
	return time.Since(first) < runningWorkerGrace
}

// ranWords is the worker in parentheses, on every line this stream writes about
// a node. It says "linear" here for the same reason the store says it — an
// unnamed worker and a worker nobody recorded look identical, and telling them
// apart cost the s9 sweep a day.
func ranWords(node store.Node) string {
	if ran := strings.TrimSpace(node.Ran); ran != "" {
		return " (" + ran + ")"
	}
	return ""
}

// noteDegradedWorkers says once, per node, that a stored row names a worker this
// build does not have. The run continues on linear — that is the registry's
// promise and it is not changing — but a run that silently became something
// else is a measurement of the wrong thing, and the only honest place to learn
// that was a profile file that may never be written.
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
// was recovering correctly was killed: a leaf faulted, and the recovery that
// followed it took minutes — facts all in the journal, none in the stream,
// which said `still waiting: 0 tasks pending, 1 running — 10m57s`. The operator
// read it as a hang.
//
// A FAIL-SAFE PROPAGATES TO THE VERDICT THE PERSON READS. Four kinds of fact
// change what somebody watching should expect, so four kinds of fact get a line
// in the same register as ▶ and ✓: a caught fault, a change of worker in an
// older graph's journal, a phase, and the delivery gate's judgement. Nothing
// here is a new flag and nothing here spends money — every one of them is
// already written down, and until now nobody read it.
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
		// A REPAIR OF A MODEL CALL IS A FACT ABOUT THE RUN AND NOT ABOUT ONE
		// NODE, so it is the one kind read without asking whether the errand
		// owns the node it is filed under. It has to be: the planner's repairs
		// happen against the job root before a single node exists, which is
		// precisely the moment the s4 sweep's textual run died in silence with
		// nothing on the stream but "still waiting".
		if event.Kind == store.EventStructuredRepair {
			said = w.narrateRepair(event) || said
			continue
		}
		// So is the compile's receipt, filed against no node at all, and read
		// here for the one line of it that is news on this surface.
		if event.Kind == store.EventMessagePosted && w.narrateCompileNote(event) {
			said = true
			continue
		}
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

// narrateCompileNote says when the compile supplied no reading of its own.
//
// The compile's receipt is filed, never printed, on this surface — the goal
// here is the person's own words whatever the compiler wrote — so the one line
// of it that is news has to be said by itself. A substitution the person cannot
// see is the defect #311 and #314 closed, and this would be one in miniature
// (#335). The note is recognised by its one spelling, head.NoGlossNote, which
// is the same constant the receipt was written from.
func (w *settlementWatch) narrateCompileNote(event store.Event) bool {
	var message struct {
		Body string `json:"body"`
	}
	if json.Unmarshal(event.Payload, &message) != nil || !strings.Contains(message.Body, head.NoGlossNote) {
		return false
	}
	w.note("compile", "no reading of its own — your request stands as the goal, word for word")
	return true
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

	case store.EventLeafExhausted:
		var exhausted store.LeafExhausted
		if json.Unmarshal(event.Payload, &exhausted) != nil || strings.TrimSpace(exhausted.Reason) == "" {
			return false
		}
		// ⏳ and not ✗: nothing failed. An attempt that was still working when
		// its budget ended is the one ending this stream had no mark for, and
		// borrowing the fault mark would have said the opposite of the truth.
		w.say("⏳", nodeDisplay(node), exhausted.Reason)
		return true

	case store.EventLeafResumed:
		var resumed store.LeafResumed
		if json.Unmarshal(event.Payload, &resumed) != nil || resumed.Turns <= 0 {
			return false
		}
		w.say("↻", nodeDisplay(node), resumedWords(resumed))
		return true

	case store.EventLeafSelfClose:
		var closing store.LeafSelfClose
		if json.Unmarshal(event.Payload, &closing) != nil || len(closing.Kinds) == 0 {
			return false
		}
		// ONLY THE ARM THAT REOPENS THE WORK IS SAID. A leaf held back to fix
		// what its own reading found is still running when the person expected
		// it to be finished, and that is precisely the fact this register exists
		// to carry (FAILSAFE.md clause 3). The other arm — the finding stands
		// and the leaf lands with it — changes nothing about what happens next
		// that the gate line below does not already say in its own words, and
		// saying it twice would read as two findings. It is journaled either
		// way, which is where an autopsy reads it.
		if !closing.Closed {
			return false
		}
		// ↻ and not ✗: nothing failed. The leaf is picking its own work back up,
		// which is the same fact the mark already carries for a claim resuming
		// from a record.
		w.say("↻", nodeDisplay(node), selfCloseWords(closing))
		return true

	case store.EventNodeReleased:
		var release struct {
			Reason   string `json:"reason"`
			Recorded int    `json:"recorded"`
		}
		if json.Unmarshal(event.Payload, &release) != nil || strings.TrimSpace(release.Reason) == "" {
			return false
		}
		// Only a release that carries a reason is said. A worker handing its own
		// node back says everything by handing it back; a claim taken away from
		// one is the event a person watching a run restart needs, and it was
		// invisible four times on the ink run of 2026-08-29.
		//
		// ✗ IS THE FAULT REGISTER AND A REQUEUE IS NOT A FAULT. A release that
		// hands on a record is the ordinary end of a leaf that ran out of its
		// room — the ⏳ line directly above has already said so — and marking it
		// as a fault said the opposite of the truth twice in three lines. The
		// count is the release's own (store.releasePayload.Recorded), which is
		// the fact the next claim's resume seed is built from, so the two lines
		// cannot disagree about how much was picked up. ✗ is kept for the
		// release this mark was added for: a claim taken back over a worker that
		// never answered, which hands on nothing.
		//
		// AND THE LINE CARRIES ITS OWN WHY. It said only the count for as long
		// as the ⏳ that names the bound was directly above it, which is true of
		// every release written today and is an ordering assumption rather than
		// a guarantee — a person scrolling back to one ↻ read a bare restart.
		// The why is the release's own reason, read and not re-worded
		// (resident.ReleaseWhy takes off the clause about what survived, which
		// this line has already said in its own words: one number, once).
		if release.Recorded > 0 {
			w.say("↻", nodeDisplay(node), requeueWords(release.Recorded, release.Reason))
			return true
		}
		w.say("✗", nodeDisplay(node), "picked up again — "+firstLine(release.Reason))
		return true

	case store.EventDeliveryGate:
		var gate store.DeliveryGate
		if json.Unmarshal(event.Payload, &gate) != nil {
			return false
		}
		// A GATE NOBODY REACHED REFUSED NOTHING, so the stream does not say
		// "gate: refused" of it — that word is the RUN declining to buy a
		// judgement, and this is the judgement declining to arrive. gateWords
		// speaks for a gate that ANSWERED, and this row is the one where none
		// did.
		//
		// THIS LINE REPORTS THE EVENT, AND IT IS NOT THE RUN'S VERDICT. What is
		// true at this moment is that the gate gave up on this node; what the
		// run ends up handing over is not settled here and may still change —
		// another round, another node, a wall. So it says the event in the
		// stream's own register and takes NO closing flag: the sentence about
		// what was delivered belongs to sayStanding, at the end, where the
		// answer is, and FAILSAFE clause 3 says that is the line that may not be
		// missing. A person's last visible line is never a bare ✓ over
		// something the run believes nothing checked.
		if gate.Unjudged {
			w.note(unreachedStreamWords(gate), "")
			return true
		}
		verdict, detail := gateWords(gate)
		w.note("gate: "+verdict, detail)
		// AND A DELIVERY THAT ENDED BECAUSE WHAT WAS ASKED FOR IS IN HAND SAYS
		// SO, under the mark this stream already uses for work that finished.
		//
		// It is the one positive line the gate can write and it is the whole
		// repair of a silence that was being read as its opposite: a run that
		// had the answer at two minutes and then spent eleven more on rounds
		// ended on the word `partial`, and nothing anywhere said that the thing
		// the person asked for had been done. The receipt says which of the two
		// endings this was — the request met as stated, or the work's own checks
		// green over coverage nobody could measure — in the words the record
		// keeps. See revision.RequestMetWords and revision.CheckedNotMeasured.
		if receipt := strings.TrimSpace(gate.Receipt); receipt != "" {
			w.say("✓", nodeDisplay(node), receipt)
		}
		// The coverage finding gets its own line, because it is a different
		// fact from the verdict and it is the one the acceptance line above
		// promised. A FAIL-SAFE PROPAGATES TO THE VERDICT THE PERSON READS
		// (FAILSAFE clause 3): igel s6 printed "acceptance — 17 points from the
		// request" at 23 seconds and never said another word about them, while
		// three of the seventeen went to the end of the run unexercised. It
		// could not: the finding was a paragraph in the middle of the gap, and
		// the line above it is the gap's FIRST line.
		if words := unexercisedWords(gate.Unexercised); words != "" {
			w.note("no check exercises", words)
		}
		// And the behaviours a check names and no assertion weighs get theirs.
		// It is a separate line because it asks for a separate thing: not
		// another check, but an assertion on the identifier the request spelled.
		if words := unexercisedWords(gate.Unasserted); words != "" {
			w.note("asserted by no check", words)
		}
		// AND THE CHECKS THIS WORK WROTE THAT ARE RED GET THEIR OWN LINE, in
		// their own words. They used to be printed as `gate: fail — This work
		// broke checks that were passing before it: …`, which is a sentence
		// about a repository somebody damaged rather than about a leaf that has
		// not finished — and on happy-dom's nemotron n1 run it was said of
		// eighteen checks the run had written that hour, over a tree the grader
		// scored 9 of 9.
		if words := unexercisedWords(gate.OwnFailing); words != "" {
			w.note("the checks this work wrote fail", words)
		}
		return true
	}
	return false
}

// resumedWords is the line a resumed leaf opens with, and it is a count rather
// than a claim: "resumed" on its own is exactly the promise that was made and
// silently not kept, so the number the seed actually carries is the thing said.
// The files are the world's own reading of what the earlier attempts changed,
// and they are named up to a few because the point is that the workspace is not
// empty, not to reprint a diff.
// selfCloseWords says what a leaf found against its own work, as a kind and a
// count.
//
// The names are in the record and deliberately not in this line. A person
// watching a run needs to know the leaf caught something itself and is fixing
// it before handing over; WHICH three names is the diagnosis, and it belongs
// where a diagnosis is read — the journal, and the leaf's own note.
func selfCloseWords(closing store.LeafSelfClose) string {
	words := "closing its own finding: " + strings.Join(closing.Kinds, ", ")
	if len(closing.Names) > 0 {
		words += " (" + plural(len(closing.Names), "name") + ")"
	}
	return words
}

func resumedWords(resumed store.LeafResumed) string {
	words := fmt.Sprintf("resumed from %s", plural(resumed.Turns, "recorded turn"))
	if len(resumed.Files) > 0 {
		shown := resumed.Files
		more := 0
		if len(shown) > resumedFilesShown {
			more, shown = len(shown)-resumedFilesShown, shown[:resumedFilesShown]
		}
		names := make([]string, 0, len(shown))
		for _, path := range shown {
			names = append(names, filepath.Base(path))
		}
		words += ", already holding " + strings.Join(names, ", ")
		if more > 0 {
			words += fmt.Sprintf(" and %d more", more)
		}
	}
	return words
}

// resumedFilesShown is how many of the files an earlier attempt changed are
// named on the stream. Three, because the line is one line.
const resumedFilesShown = 3

// unexercisedWords is the coverage finding in one line: what it is short of,
// named once and counted after that.
//
// One behaviour spelled out and the rest counted is the same shape
// describeChecks and regressionsNamed already use on a list of names, and for
// the same reason — this is a line in a stream beside ▶ and ✓, and seventeen
// behaviours printed one per line is seventeen lines nobody reads. The whole
// list is on the gate event for whoever opens it.
func unexercisedWords(points []string) string {
	named := make([]string, 0, len(points))
	for _, point := range points {
		if point = strings.TrimSpace(point); point != "" {
			named = append(named, point)
		}
	}
	if len(named) == 0 {
		return ""
	}
	words := firstLine(named[0])
	if len(named) > 1 {
		words += fmt.Sprintf(" — and %d more", len(named)-1)
	}
	return words
}

// narrateRepair says what the structured-answer seam had to do to get an answer.
//
// The words are the seam's own, kept verbatim off the journal rather than
// rebuilt here, so a person watching and a person reading the record afterwards
// are looking at one sentence: "plan: answer cut at the ceiling — continued".
// The mark is ↻ because that is what this stream already means by it — something
// was tried again — and it is the same register every other retry line uses.
func (w *settlementWatch) narrateRepair(event store.Event) bool {
	var repair store.StructuredRepair
	if json.Unmarshal(event.Payload, &repair) != nil {
		return false
	}
	line := strings.TrimSpace(repair.Line)
	if line == "" {
		return false
	}
	fmt.Fprintf(w.progress, "  ↻ %s  %s\n", line, time.Since(w.started).Round(time.Second))
	return true
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

// workerChangeWords says which worker took the work over, for a journal written
// when one could. Nothing writes the event any more — this build has one worker
// — but every graph.db that carries it still opens here, and a line that named
// both ends is the fact that explains why the next thing that run did looked
// nothing like the last. A node given a worker it did not previously have was
// not escalated from anything, and saying it was would invent a failure.
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
	// A GATE A REPAIR CLOSED IS A PASS, AND SAYING "fail" OF IT WAS THE LINE
	// THAT DISAGREED WITH THE EXIT CODE. Pass is the FIRST reading of the work;
	// a delivery that failed it, was repaired and was re-judged carries the
	// second reading in PolishClosed, and this said only the first. Three of the
	// five s5 runs ended on "gate: fail — …" and left with exit 0 over it. The
	// settled verdict has one reading — store.DeliveryGate.Whole — and both this
	// and deliveredWhole spend it (SETTLEMENT.md §7). The gap is still named,
	// because what was wrong and then fixed is worth one clause.
	if gate.Whole() {
		if gap == "" {
			return "pass", ""
		}
		return "pass", gap + " — closed by the repair"
	}
	return "fail", gap
}

// gateStanding is the finding a settled run is still short of, and why nothing
// closed it, for the one line a person reads at the end.
//
// It answers nothing for a gate that settled whole: a delivery that passed, that
// a repair closed, or whose finding was weighed against the world and lost owes
// the person no reservation. What it names otherwise is the gap first and the
// reason second, in the gate's own words off the journal, for the reason
// gateWords does it in that order — THE FINDING IS THE NEWS.
//
// The reason is the refusal sentence when there is one, and otherwise the
// structural fact that nothing further ran. A run that failed its gate and had
// no round left says so; a run refused on where its review got its words says
// that; and either way the person is told what the run itself believes it did
// not do (FAILSAFE clause 3).
func gateStanding(gate store.DeliveryGate) (finding, reason string, ok bool) {
	if gate.Whole() {
		return "", "", false
	}
	// THE COVERAGE SET IS THE FINDING WHERE IT IS THE ONE NOTHING ADDRESSED. A
	// verdict that passed, that a repair closed, or whose refusal was weighed
	// against the world and lost has settled everything it was about — and the
	// behaviours nothing exercises are not among them. Leading with the gate's
	// own prose there would name the finding that was ACQUITTED as the reason
	// the run is short, which is the opposite of what happened.
	if open := len(gate.Unexercised) + len(gate.Unasserted); open > 0 &&
		(gate.Pass || gate.PolishClosed || gate.Overturned) {
		return uncheckedWords(open), "", true
	}
	finding = firstLine(strings.TrimSpace(gate.Gap))
	if finding == "" && gate.Unreadable {
		// A gate that PASSED over a suite nobody could read names no gap,
		// because the judge found none. What the run is short of is the
		// measurement itself, and that sentence is the finding.
		return firstLine(strings.TrimSpace(gate.Unmeasured)), "", true
	}
	// A GATE THAT WAS DECLINED RATHER THAN HELD NAMES NO GAP, AND ITS SENTENCE
	// IS THE FINDING. The harness stops asking for a judgement once nothing is
	// changing and journals the refusal in its place, unclosed; reading that row
	// for a gap it does not have returned "nothing standing", so the one line at
	// the end of a run that was never judged said nothing at all. There is no
	// second clause to add — the refusal already says why nothing further ran.
	if finding == "" && gate.Unclosed {
		if refused := firstLine(strings.TrimSpace(gate.Refused)); refused != "" {
			return refused, "", true
		}
	}
	if finding == "" {
		return "", "", false
	}
	reason = firstLine(strings.TrimSpace(gate.Refused))
	if reason == "" && gate.Unmoved {
		// The one reason worth saying over "nothing further was started",
		// because it is the reason a person would otherwise never guess: a
		// repair DID run and it rewrote the account without touching the tree,
		// so the finding it was aimed at is exactly where it was.
		reason = "the repair rewrote the account and changed nothing on disk"
	}
	if reason == "" {
		reason = "nothing further was started"
	}
	return finding, reason, true
}

// uncheckedWords is the coverage shortfall as a count, for the last line of a
// run that is short of nothing else.
//
// A count rather than the behaviours themselves, because unexercisedWords
// already spells one of them out where the finding is the news, and this line is
// the run's whole reservation in one clause. The list is on the gate event for
// whoever opens it.
func uncheckedWords(groups int) string {
	if groups == 1 {
		return "1 behaviour the request states has no check"
	}
	return fmt.Sprintf("%d behaviours the request states have no check", groups)
}

// partialWords is that reservation as the stream's last line.
//
// The exit code is the contract a pipeline reads and it is invisible to a person
// watching a terminal, so a run that ends short says it in the register every
// other line here uses. It is one line and it is last, after the ✓ rows, so the
// thing a person carries away from a ninety-minute run is the thing the run
// itself says it did not do.
func partialWords(finding, reason string) string {
	if strings.TrimSpace(reason) == "" {
		// The shortfall is the measurement rather than a finding a repair could
		// have closed, so there is nothing to say about why nothing was
		// repaired. "gate:" comes off with it: no gate said this.
		return "partial — " + finding
	}
	return fmt.Sprintf("partial — gate: %s (not repaired: %s)", finding, reason)
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
// THE FRESHEST ACCOUNT IS THE PROCESS'S OWN. The journal's usage row is written
// when a node's work is booked, and a bare leaf books its usage when it
// FINISHES — so a leaf ten minutes into its work read as "last call … 10m ago"
// while calls were landing every second. The call log (internal/calllog) hears
// every answer the moment it arrives, in this process, whether or not its file
// is on; the journal is the fallback for a run whose calls happen elsewhere.
// Only a call since this errand started counts, which is the same rule the
// journal read applies with its sequence number.
//
// A run with no call recorded yet says nothing about calls at all, rather than
// "0s ago" or "none". A model that has not been reached and a model that
// answered a moment ago are different situations, and a zero invented for the
// first is how they stop being told apart. A read that fails says nothing for
// the same reason and never ends the run: this line is an account of the work,
// never a part of it.
func (w *settlementWatch) lastCallWords() string {
	// The log stamps to the millisecond and the watch started on the
	// nanosecond, so the comparison is made at the log's own precision — a
	// call answered in the same millisecond the errand began is this errand's.
	if heard, found := calllog.Last(); found && !heard.At.Before(w.started.Truncate(time.Millisecond)) {
		return fmt.Sprintf(" · last call %s %s ago", heard.Model, time.Since(heard.At).Round(time.Second))
	}
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
		// DONE UNTIL SOMETHING BELOW SAYS OTHERWISE. Every ending this function
		// can reach that is not a clean one names itself, so the default is the
		// one it cannot name: the work settled and the deliverable stands.
		stop: stopDone,
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
			outcome.stop = stopIncomplete
			outcome.Deliverable = "It did not finish."
			if reason := plainWords(strings.TrimSpace(final.Error)); reason != "" {
				outcome.Deliverable += "\n\n" + reason
			} else {
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
				outcome.stop = stopIncomplete
				// AND A DELIVERY NOTHING JUDGED IS NEITHER A PASS NOR A FAIL,
				// SO IT IS NOT SAID AS EITHER. The work ships — the gate is
				// fail-open and stays so — and what changes is that nobody can
				// read the ending as a check that held. The number is the same
				// 2 a run that fell short leaves with, because how much is
				// wrong is the same; the WORD is its own, because a script
				// branching on `stop` is entitled to tell "it was checked and
				// came up short" from "nobody checked it".
				if reason := w.unjudgedReason(*final); reason != "" {
					outcome.stop, outcome.Unjudged = stopUnchecked, reason
				}
				w.sayStanding(*final)
			}
		}
		// Every ending above is still an answer to the same question about the
		// same settled root, so ask it once after the roads join rather than let
		// one ending silently miss the name.
		outcome.JudgedBy = w.judgedBy(*final)
		// A FAILED COLLECTION IS A FINDING ABOUT THE TREE, WHATEVER THE NODE'S
		// OWN ENDING SAID. It belongs after the switch so both a failed leaf and a
		// leaf that said Done carry it out. If the gate was also unreachable, the
		// tree wins the stop word: `incomplete` says a check ran and found a tree
		// it could not collect, while `unchecked` says no finding arrived at all.
		if reason := w.uncollectedReason(nodes); reason != "" {
			outcome.unfinishedTree = reason
			outcome.stop = stopIncomplete
			if strings.TrimSpace(outcome.Deliverable) == "" {
				outcome.Deliverable = reason
			} else {
				outcome.Deliverable = strings.TrimSpace(outcome.Deliverable) + "\n\n" + reason
			}
		}
		// WHAT LEFT THE WORK WHERE IT IS, in the record's own word. It is read
		// here, after the roads have joined and `stop` has had its last word, so
		// the verdict and the stop cannot disagree: a node the store settled
		// failed or cancelled is `failed`, and any other ending that ran and did
		// not settle whole is `unverified` — nobody could say the work holds.
		//
		// A run that settled whole takes NEITHER word: its work is on the branch
		// its caller already reads and nothing left it anywhere else. A run that
		// never started (error), did nothing (question, price) says nothing
		// either, because there is no work to account for. And the word is
		// `TaskFailed`/`TaskUnverified` and not a string typed here: these are the
		// same words the task record carries, and a reader comparing the envelope
		// against `tasks.json` must read one vocabulary, not two.
		switch {
		case final.Status == store.Failed || final.Status == store.Cancelled:
			outcome.Verdict = string(session.TaskFailed)
		case outcome.stop == stopIncomplete, outcome.stop == stopUnchecked,
			outcome.stop == stopBudget, outcome.stop == stopTurnCap, outcome.stop == stopDeadline:
			outcome.Verdict = string(session.TaskUnverified)
		}
		outcome.Deliverable = groundedInArtifacts(outcome.Deliverable, outcome.Artifacts)
		// One list, once. Grounding has had its look at the narration as the
		// worker wrote it, so the worker's own file list has done its job and
		// comes back off before anything is printed.
		outcome.Deliverable = withoutSummaryFileList(outcome.Deliverable, outcome.Artifacts)
		// AND WHAT BECAME OF EACH THING THIS RUN WAS ASKED FOR. The rows go out
		// whole for the machine; the person gets the bounded account only where
		// the gate's own mapping did not already answer the list, and never a
		// second time — a failed node's error carries it here already, and one
		// list said twice reads as two findings.
		var gateAnswered bool
		outcome.Checklist, gateAnswered = w.checklistFor(final.ID, outcome.Artifacts)
		if !gateAnswered && !strings.Contains(outcome.Deliverable, revision.ChecklistHeading) {
			if account := revision.ChecklistAccount(outcome.Checklist); account != "" {
				if strings.TrimSpace(outcome.Deliverable) != "" {
					outcome.Deliverable += "\n\n"
				}
				outcome.Deliverable += account
			}
		}
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

// checklistFor reads what became of each thing this run was asked for, off rows
// the run had already written down: the acceptance checklist journaled before
// any work began, and the delivery gate's own mapping of points onto checks
// where a gate ran. Nothing here spends anything — no model call, no second
// reading of the tree — which is what makes it affordable on EVERY ending,
// including the early ones that never reached a gate and used to hand back a
// stop reason and a file list with no account of the list at all (#551).
//
// gateAnswered says whether the gate settled these points itself. It is the
// question the caller has, and it is returned rather than re-derived from the
// rows, because a reading of a reading drifts from the fact it is about.
//
// The rows come off THE NODE THE CHECKLIST GOVERNS, which is the node that was
// handed the request — the ordinary errand's one settled root. A job the planner
// broke into several leaves journals a checklist against each leaf that carries
// one and none against the root that settles them, so this reports nothing there
// and the account still reaches the person the other way: it rides the failing
// leaf's own recorded error (humanFailure in chat.go), which is what every
// reader downstream of a stopped part opens.
//
// Every read is best-effort in the sense every other journal read on this path
// is: a store that will not answer costs the account, never the ending. AND AN
// UNREADABLE GATE IS NOT A GATE THAT ANSWERED — the list is still the person's
// and is still accounted for, with nothing claimed answered.
func (w *settlementWatch) checklistFor(nodeID string, wrote []string) (rows []revision.PointOutcome, gateAnswered bool) {
	if w.graph == nil {
		return nil, false
	}
	acceptance, found, err := w.graph.AcceptanceFor(nodeID)
	if err != nil || !found {
		return nil, false
	}
	gate, gateRead, err := w.graph.DeliveryGateFor(nodeID)
	if err != nil {
		gate, gateRead = store.DeliveryGate{}, false
	}
	gateAnswered = gateRead && len(gate.Exercises) > 0
	return revision.AnswerChecklist(acceptance.Points, gate, gateRead, wrote), gateAnswered
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
// THE READING IS store.DeliveryGate.Whole AND IT IS NOT REPEATED HERE. This
// combined the three fields inline for a while and gateWords, forty lines up,
// built the line a person watching reads out of two of them — so ink s5 and
// ofetch s5 printed "gate: fail" as the last thing anybody saw and left with
// exit 0, the exit code and the stream disagreeing about the same event
// (2026-08-29, bench/deepswe; SETTLEMENT.md §7).
//
// An unreadable store answers whole. This decides an exit code, not the work,
// and a failed read is not evidence of a shortfall.
func (w *settlementWatch) deliveredWhole(node store.Node) bool {
	if gate, ok, err := w.graph.DeliveryGateFor(node.ID); err == nil && ok && !gate.Whole() {
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

// unjudgedReason is why nothing checked this delivery, read off the gate's own
// row, and empty where something did.
//
// It asks the row rather than the judgement because the judgement is gone by the
// time the settlement runs — the gate was made in another process's turn loop,
// and the journal is the only thing that crosses that seam. An unreadable store
// answers empty, on the same terms deliveredWhole reads one: this decides how a
// run is described, and a failed read is not evidence about the run.
func (w *settlementWatch) unjudgedReason(node store.Node) string {
	gate, ok, err := w.graph.DeliveryGateFor(node.ID)
	if err != nil || !ok || !gate.Unjudged {
		return ""
	}
	return firstLine(strings.TrimSpace(gate.Refused))
}

// judgedBy is the name of what read this delivery, off the gate's own row, and
// empty where nothing read it.
//
// It asks the row for the same reason unjudgedReason above it does: the
// judgement was made in another process's turn loop and the journal is the only
// thing that crosses that seam. A ROW THAT IS NOT Unjudged IS A GATE THAT
// ANSWERED — including one whose answer no reader could parse, which is
// journaled as an unclosed gap and which the exit code already spends. A gate
// that was never asked writes no row at all and is absent here, which is the
// correct answer rather than a missing one.
//
// It reads the settled root, as both readers beside it do, and shares that
// road's one limitation: a job the planner broke into several leaves journals
// its gate against the leaf that delivered rather than against the root that
// settles them, so this reports nothing there.
//
// An unreadable store answers empty, on the same terms: this decides how a run
// is described, and a failed read is not evidence about the run.
func (w *settlementWatch) judgedBy(node store.Node) string {
	gate, ok, err := w.graph.DeliveryGateFor(node.ID)
	if err != nil || !ok || gate.Unjudged {
		return ""
	}
	return revision.GateName
}

// uncollectedReason is why this run's last finished-tree reading could not
// collect, and empty where its last word made no such finding.
//
// A SECOND READING REPLACES THE FIRST; IT DOES NOT ADD TO IT. A finding the
// first reading raised and the second does not must stop being a finding, and
// across a run the reading with the latest journal sequence has the last word.
// A node's later status update cannot reorder observations of the tree. It asks the rows
// because the readings were taken in another process's turn loop, and the
// journal is the only thing that crosses that seam. An unreadable store answers
// empty, on the same terms unjudgedReason does: this decides how a run is
// described, and a failed read is not evidence about the run.
func (w *settlementWatch) uncollectedReason(nodes []store.Node) string {
	var last store.VerificationReading
	var lastSeq int64
	for _, node := range nodes {
		reading, seq, err := w.graph.LatestFinishedVerification(node.ID)
		if err == nil && seq > lastSeq {
			last, lastSeq = reading, seq
		}
	}
	if lastSeq > 0 && last.Uncollected {
		return strings.TrimSpace(last.Why)
	}
	return ""
}

// unjudgedWords is the last line a person reads when the delivery went out and
// nothing read it.
//
// IT IS NOT partialWords, AND THAT IS THE POINT OF IT. "partial — gate: …" says
// a gate found something and nothing closed it, which is a sentence about the
// work; this run's gate found nothing, because it was never reached. The finding
// is that there is no finding, and a person who reads "delivered without a
// check" knows both that the answer above is theirs to keep and that nothing has
// vouched for it. The reason follows the colon in the gate's own words
// (revision.GateUnreached), and a row that somehow carries none says the four
// words alone rather than a dangling colon.
func unjudgedWords(gate store.DeliveryGate) string {
	said := "delivered without a check"
	if reason := firstLine(strings.TrimSpace(gate.Refused)); reason != "" {
		said += ": " + reason
	}
	return said
}

// unreachedStreamWords is the same event as it happens, in the register the rest
// of this stream reports gate rows in.
//
// It is a different line from unjudgedWords above because the two answer
// different questions at different moments. This one says WHAT JUST HAPPENED —
// the gate gave up on this node — while the run is still going and nothing about
// what will be handed over is settled. unjudgedWords says WHAT WAS DELIVERED,
// once there is a delivery to say it about.
//
// The record's note opens with the gate as its subject, because the closing line
// needs a whole sentence to stand on its own; this line's subject is already
// `gate:`, so the same words are folded into it rather than said twice. The
// reason clauses after the ` · ` — how it was asked, the provider's own sentence
// — are kept whole: they are the whole of what a person watching can act on.
func unreachedStreamWords(gate store.DeliveryGate) string {
	reason := firstLine(strings.TrimSpace(gate.Refused))
	if rest := strings.TrimPrefix(reason, revision.GateUnreached); rest != reason {
		return "gate: could not be reached" + rest
	}
	// The other note a gate can leave with no verdict behind it — it answered
	// and named no gap — keeps its own words, with the subject folded out the
	// same way so that the line does not name the gate twice.
	return "gate: " + strings.TrimPrefix(reason, "the gate ")
}

// sayStanding writes the one line that tells a person watching WHY the run is
// short, at the end, where the answer is.
//
// The exit code is the contract every pipeline reads and it is the one thing a
// person at a terminal cannot see. Five headless runs of ninety minutes ended
// with their last visible line being a ✓ on a node, and what the run itself
// believed it had not done was in the journal and nowhere a person could read it
// (2026-08-29, bench/deepswe; FAILSAFE clause 3, and SETTLEMENT.md §7).
//
// It says nothing when the store cannot be read, when there is no gate, or when
// the gate settled whole — a run that is short for a structural reason instead,
// a part that failed or was cancelled, already carries that in the deliverable's
// own words.
func (w *settlementWatch) sayStanding(node store.Node) {
	if w.saidStanding || w.progress == nil {
		return
	}
	// THE GOVERNOR SPEAKS FIRST WHEN IT SPOKE AT ALL. A run the growth
	// governor stopped is a run that discovered it had stopped working, and
	// that is a more particular fact than any gate verdict standing beside it:
	// the gate says what is missing, this says why nothing more was bought to
	// get it. Two 5400-second runs ended with a standstill refusal sitting in
	// the journal, on a node nobody opens, and a last line that said nothing
	// about it (FAILSAFE clause 3).
	if finding, standing := resident.GovernorStanding(w.graph, node.ID); standing {
		w.saidStanding = true
		w.note(partialWords(finding, ""), "")
		return
	}
	gate, ok, err := w.graph.DeliveryGateFor(node.ID)
	if err != nil || !ok {
		return
	}
	// A DELIVERY NOTHING JUDGED HAS ITS OWN SENTENCE, AHEAD OF EVERY VERDICT
	// WORD, because there is no verdict to word. Reading this row through
	// gateStanding would print "partial — the gate could not be reached", which
	// tells a person the gate said something; it said nothing, and what they
	// need to know is that the answer above them is unchecked.
	//
	// AND THIS IS THE ONE PLACE IT IS SAID, because this is the only one of the
	// two that may be the last thing a person reads. The stream reported the
	// event as it happened, in its own register (unreachedStreamWords), and a
	// run that ended after it would otherwise close on a ✓ over a delivery
	// nothing checked — five headless runs of ninety minutes ended exactly that
	// way, with what the run believed it had not done sitting in the journal
	// (FAILSAFE clause 3). The exit code is the contract every pipeline reads
	// and it is the one thing a person at a terminal cannot see, so the
	// reservation is stated here, last, whether or not the stream said anything
	// earlier.
	if gate.Unjudged {
		w.saidStanding = true
		w.note(unjudgedWords(gate), "")
		return
	}
	finding, reason, standing := gateStanding(gate)
	if !standing {
		return
	}
	w.saidStanding = true
	w.note(partialWords(finding, reason), "")
}

// sayWallStanding is the closing line for a run the clock ended: what a
// governor had already found, if one had found anything, over every job this
// errand owns.
//
// It walks the roots rather than being handed one because there is no
// deliverable at a wall — the run was killed mid-round, so nothing composed a
// final node — and the fact worth saying belongs to whichever job stopped
// moving.
// stoppedByHand says whether the person ended this run. A watcher nobody told
// how to answer says no, which is the reading every caller had before signals
// were routed through the context at all.
func (w *settlementWatch) stoppedByHand() bool {
	return w.stopped != nil && w.stopped()
}

func (w *settlementWatch) sayWallStanding() {
	if w.saidStanding || w.progress == nil {
		return
	}
	nodes, err := w.sessionNodes()
	if err != nil {
		return
	}
	for _, node := range nodes {
		if node.Parent != store.RootID {
			continue
		}
		w.sayStanding(node)
		if w.saidStanding {
			return
		}
	}
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

// keptNothing is the closing line a run that produced NOTHING owes the person,
// and it is the other half of producedWords' job.
//
// It says only what the run can prove. The evidence is the workspace's own
// before-and-after read of the tree plus what the write tools recorded, and
// that evidence deliberately excludes deletions (exec.Workspace.Artifacts drops
// ArtifactDeleted, because a path that is gone is not a path to open). So the
// claim concerns the recorded files, never that the directory is as it was
// found or that no write occurred. The bounded workspace sweep can miss files.
const keptNothing = "No created or changed files were recorded: this run worked in %s, editing it in place."

// groundedAfterShutdown includes files registered while the watcher was
// returning. The record is bounded and omits deleted paths, so an empty record
// is reported as an empty record rather than proof the tree never changed.
func groundedAfterShutdown(outcome headlessOutcome, produced *errandRegistry) headlessOutcome {
	if paths := produced.list(); len(paths) > 0 {
		// The watcher already grounded and removed its duplicate file list.
		// Only newly registered files need another account after shutdown.
		var late []string
		for _, path := range paths {
			if !slices.Contains(outcome.Artifacts, path) {
				late = append(late, path)
			}
		}
		outcome.Artifacts = paths
		outcome.Deliverable = groundedInArtifacts(outcome.Deliverable, late)
	}
	outcome.Deliverable = groundedInTheTree(outcome)
	return outcome
}

// groundedInTheTree holds a refused run's closing line answerable to the
// working directory, the way groundedInArtifacts holds it answerable to the
// file record.
//
// The defect it repairs: a run spent 45 minutes and $1.98 in somebody's
// project, wrote not one byte, and closed with `artifacts: []` and "The time
// limit was reached before anything finished" — a sentence about the clock,
// under which the reader had to run `git status` themselves to learn the only
// fact that mattered. The directory was honoured all the way through and never
// named once it was over.
//
// Four endings are left exactly as they were. A clean run has nothing to say
// about a tree; a run holding files has already said where they are, in
// absolute paths; a run stopped by a question keeps an empty deliverable on
// purpose (see sayBlocked and headlessOutcome.BlockedOn); and a run with no
// nodes never did anything a tree could show.
func groundedInTheTree(outcome headlessOutcome) string {
	if outcome.resolvedStop() == stopDone || outcome.resolvedStop() == stopPrice || len(outcome.Artifacts) > 0 ||
		strings.TrimSpace(outcome.BlockedOn) != "" || outcome.Nodes == 0 ||
		strings.TrimSpace(outcome.workspace) == "" {
		return outcome.Deliverable
	}
	words := fmt.Sprintf(keptNothing, outcome.workspace)
	body := strings.TrimSpace(outcome.Deliverable)
	if body == "" {
		return words
	}
	return body + "\n\n" + words
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
// A RUN'S OWN ACCOUNT OF ITSELF MAY NOT CONTRADICT WHAT ENDED IT. The two
// endings that reach here look identical from inside the watcher — the context
// is done either way — and they are not the same news: one is a clock the
// person set, the other is the person themselves, and telling somebody who
// pressed Ctrl+C that they ran out of time is a sentence they know to be false.
func wallWords(artifacts []string, stopped bool) string {
	reason := "The time limit was reached"
	if stopped {
		reason = "The run was stopped"
	}
	if len(artifacts) == 0 {
		return reason + " before anything finished."
	}
	return reason + " before the work was summarised. " + producedWords(artifacts)
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
	if outcome.recordKept != "" && request.stderr != nil {
		fmt.Fprintf(request.stderr, "record kept at %s\n", outcome.recordKept)
	}
	if request.asJSON {
		encoded, err := json.MarshalIndent(errandEnvelope(outcome), "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(request.stdout, string(encoded))
		return errandStatus(outcome)
	}
	if body := strings.TrimSpace(outcome.Deliverable); body != "" {
		fmt.Fprintln(request.stdout, body)
	}
	footer := errandFooter(outcome)
	// The blank line is a SEPARATOR, and a separator with nothing under it is
	// one more thing the emptiness law does not allow: a run that spent nothing,
	// touched no files and learned nothing ends at its last real line.
	if len(outcome.Artifacts) > 0 || len(outcome.Learned) > 0 || footer != "" {
		fmt.Fprintln(request.stdout)
	}
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
	if footer != "" {
		fmt.Fprintln(request.stdout, footer)
	}
	return errandStatus(outcome)
}

// errandEnvelope turns what `codeaf do` knows into the one machine contract
// every headless verb returns (envelope.go). It is the ONLY mapping between
// this file's private shape and what a caller reads, which is what keeps `do`,
// `exec` and `run` from publishing three different objects again.
func errandEnvelope(outcome headlessOutcome) resultEnvelope {
	return buildResultEnvelope(runResult{
		Stop:            outcome.resolvedStop(),
		Answer:          outcome.Deliverable,
		Files:           outcome.Artifacts,
		Error:           outcome.Error,
		SpendUSD:        outcome.Spend,
		TokensIn:        outcome.tokensIn,
		TokensOut:       outcome.tokensOut,
		Seconds:         outcome.Seconds,
		CoreDoneSeconds: outcome.coreDoneSeconds,
		Model:           outcome.Model,
		Steps:           outcome.Nodes,
		Run:             outcome.run,
		Calls:           outcome.calls,
		Rounds:          outcome.rounds,
		Redispatches:    outcome.redispatches,
		KeptBranch:      outcome.KeptBranch,
		Verdict:         outcome.Verdict,
		Extra:           legacyErrandFields(outcome),
	})
}

// errandFooter is the last line of a headless run, and it draws only what is
// true.
//
// THE EMPTINESS LAW. A run that never got started ended `0s · 0 nodes ·
// $0.0000` — three claims nobody earned, on the one line a person reads to find
// out what happened, directly under the sentence saying it did not run. Zero
// time, zero nodes and zero spend are each simply absent, and a run that really
// was that cheap draws the parts of it that are true. The spend is written by
// the one helper that owns how a spend is written ([config.SpentFigure]), which
// is also where the law for a zero one lives.
func errandFooter(outcome headlessOutcome) string {
	var parts []string
	if elapsed := time.Duration(outcome.Seconds * float64(time.Second)).Round(time.Second); elapsed > 0 {
		parts = append(parts, elapsed.String())
	}
	if outcome.Nodes > 0 {
		parts = append(parts, plural(outcome.Nodes, "node"))
	}
	if spent := config.SpentFigure(outcome.Spend); spent != "" {
		parts = append(parts, spent)
	}
	return strings.Join(parts, " · ")
}

// sayBlocked is the loud half. A run that ended on a question wrote nothing to
// stdout on purpose, and a pipeline reading only stdout would see a fast, cheap,
// empty success — which is precisely how a run that did zero work for three
// seconds and five thousandths of a cent went unnoticed. stderr carries the
// question verbatim and the one line that says nobody here could answer it.
func sayBlocked(stderr io.Writer, outcome headlessOutcome) {
	question := strings.TrimSpace(outcome.BlockedOn)
	if stderr == nil || question == "" || outcome.machineHeld {
		return
	}
	fmt.Fprintln(stderr, "it stopped to ask:")
	for _, line := range strings.Split(question, "\n") {
		fmt.Fprintln(stderr, "  "+line)
	}
	fmt.Fprintln(stderr,
		"headless mode cannot answer that — `codeaf do` runs with nobody at the keyboard, so nothing was done.")
	fmt.Fprintln(stderr,
		"say the answer in the ask itself and run it again, or bring it to `codeaf` where it can be answered.")
}

// errandStatus is the contract a script reads, and it reads it off the one exit
// ladder in envelope.go — 0 done, 1 it could not be run at all, 2 it ran and
// part of it does not stand, 3 a limit you set stopped it, 4 it needed an
// answer and nobody was there. Which of the five it is was decided when the
// outcome was composed, and this only spends it.
func errandStatus(outcome headlessOutcome) error {
	if outcome.status() == exitDone {
		return nil
	}
	return outcome.status()
}

// ── the run road ────────────────────────────────────────────────────────────

// slotsFor answers how many run-engine workers this errand may run at once,
// where 0 is no bound. A caller who named a figure gets it; anyone else gets
// the profile's `task.parallel`, which is the ONE row that answers this
// question for the chat door too (internal/session's task_run_belt.go reads
// the same setting) and is no limit out of the box. This door used to carry
// a constant of its own, four, beside a setting that promised no limit — two
// answers to one question, and the person who had set the row found `codeaf
// do` ignoring it.
func (r doRequest) slotsFor(profileDir string) int {
	if r.slots != nil {
		return *r.slots
	}
	return config.TaskParallelAt(profileDir)
}

// parseSlots reads the `--slots` flag. Blank is the flag unset, which leaves
// the answer to the profile; anything else is a whole number of workers, and
// 0 is no bound. The flag is a string rather than an int so that an unset
// flag and a named 0 are two different things, which an int's zero value
// cannot say.
func parseSlots(raw string) (*int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return nil, fmt.Errorf("--slots wants a whole number of workers, 0 for no limit; got %q", raw)
	}
	return &n, nil
}

// runErrand is `codeaf do` on the run engine: the same errand as the road above
// — the same worker, the same exit ladder and the same JSON envelope —
// dispatched by [internal/run]'s supervisor over a private plan store instead
// of by the resident's reconciler. It is taken whenever the
// bash belt is on ([session.BashBeltAsked]), which it is unless the person set
// CODEAF_TASK_BELT to one of the words that turn it off, because the worker it
// dispatches is the belt's.
//
// IT KEEPS THE OLDER ROAD'S CONTRACT WITH THE DIRECTORY: the run edits it in
// place and commits nothing. A landing here once staged the directory's whole
// `git status` and committed it on the checked-out branch — the person's own
// uncommitted edits and untracked files with it — which no `--dir` help line
// ever promised. The files the envelope names are the ones this run changed.
//
// THE STORE'S OWN ROOT IS THE RUN. Its description is the ask, verbatim, and
// its result is the answer: [runengine.Start] puts the brief on it and the root
// worker's report comes back as [runengine.Summary.Result], which is what the
// envelope calls the deliverable. Nothing here compiles or plans — the ask the
// door was handed is the whole assignment, which is the same verbatim contract
// the resident road keeps.
func runErrand(request doRequest, seats config.Seats) (outcome headlessOutcome, runErr error) {
	// A FLAG THIS ROAD CANNOT HONOUR IS REFUSED IN WORDS, NEVER DROPPED. `--db`
	// names a store the older engine works in; a run keeps its plan in its own
	// private folder instead, and a run that quietly worked somewhere
	// other than the store it was pointed at would leave the person reading an
	// untouched file for the answer.
	if strings.TrimSpace(request.database) != "" {
		return headlessOutcome{}, errors.New(runRoadRefusesStore)
	}
	settings, err := config.Load()
	if err != nil {
		return headlessOutcome{}, err
	}
	applySeats(&settings, seats)
	// THE SPENDING CONTRACT IS DECIDED BEFORE ANYTHING IS OPENED. A run nobody is
	// watching is bounded unless the person said otherwise, the way the older
	// road asked its plan-price question before it bought a step.
	bound, err := runSpendBound(request, settings.ProfileDir, time.Now())
	if err != nil {
		return headlessOutcome{}, err
	}
	if bound.refused {
		// A CEILING OF NOTHING IS A RUN THAT MAY SPEND NOTHING. Refused here,
		// before anything is opened or built, because a limit of zero is not a
		// limit that a worker crosses — it is a run that was stopped before one
		// began, and the promise of exit 3 is that raising the limit and running
		// it again is the remedy.
		return headlessOutcome{
			Artifacts: []string{},
			Settled:   true,
			stop:      bound.stop,
			BlockedOn: bound.words,
		}, nil
	}
	workspace, err := errandWorkspace(request.workspace)
	if err != nil {
		return headlessOutcome{}, err
	}
	// THE DIRECTORY IS ADMITTED BEFORE THE PRIVATE RECORD. The older road
	// creates a missing workspace while building its brain; this road must do
	// so here, and refuse a file here, before opening a record or a worker.
	if info, statErr := os.Stat(workspace); statErr == nil {
		if !info.IsDir() {
			return headlessOutcome{}, fmt.Errorf("%s is not a directory", workspace)
		}
	} else if os.IsNotExist(statErr) {
		if err := os.MkdirAll(workspace, 0o700); err != nil {
			return headlessOutcome{}, fmt.Errorf("create directory %s: %w", workspace, err)
		}
	} else {
		return headlessOutcome{}, fmt.Errorf("inspect directory %s: %w", workspace, statErr)
	}
	// THE RUN WORKS IN PLACE AND COMMITS NOTHING, which is what `--dir` has
	// always promised: "the directory to work in, edited in place". The copy is
	// read before the run starts so that, afterwards, the files this run names
	// are the ones IT changed — the person's own uncommitted edits and untracked
	// files were there first, still hold what they held, and are none of the
	// run's business ([session.RunTreeSnapshot]).
	before := session.SnapshotRunTree(workspace)
	title := topicTitle(request.task)
	_, recordDir, _, err := headlessStore("")
	if err != nil {
		return headlessOutcome{}, err
	}
	admitted := false
	// The record is private and unique per invocation. The existing headless
	// home owns both its location and the rule for when it survives a run.
	defer func() {
		if !admitted {
			_ = os.RemoveAll(recordDir)
			return
		}
		if !keepPrivateStore(request.keep, trace.Enabled(), errandSucceeded(outcome, runErr)) {
			_ = os.RemoveAll(recordDir)
			return
		}
		outcome.recordKept = recordDir
	}()
	store, err := session.OpenRunPlanAt(filepath.Join(recordDir, "plandb.db"), title, request.task)
	if err != nil {
		return headlessOutcome{}, err
	}
	admitted = true
	defer store.Close()

	completerFor := request.newBeltCompleter
	if completerFor == nil {
		newClient := request.newClient
		if newClient == nil {
			newClient = newLiveClient
		}
		completerFor = crewCompleters(settings, newClient)
	}
	// EVERY SEAT CALL IS PRICED BEFORE IT IS MADE (internal/session's
	// spendguard.go): the day's cap — the daily spending limit too unless
	// -yes-spend lifted it — the per-task limit, and the checker's own
	// ceiling on this run.
	guard := doSpendGuard(config.ProfileDir(), seats.Crew, spendPreauthorized(request.yesSpend, env.Value))
	unguarded := completerFor
	completerFor = func(model string) session.Completer { return guard.Wrap(model, unguarded(model)) }
	// THE REVIEW ROUND IS ON for every `do` run: a leaf that lands done is
	// checked against its acceptance, and a check that does not hold becomes a
	// fix task under the leaf's parent the run waits on.
	limits := runengine.Limits{ReviewRound: true, CostUSD: bound.usd}
	// AN INTERRUPT MUST LAND THE RUN, NOT VANISH IT, the same way it must on the
	// resident road: routed through the context, the supervisor stops launching,
	// drains what is in flight, and what it reached is composed and printed.
	signalled, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancel := context.WithTimeout(signalled, request.timeout)
	defer cancel()

	// THE MACHINE'S HOLD IS SAID, because nothing else here would say it: the
	// chat draws `waiting · machine busy` on the run's rail, and a headless run
	// held by task.max_load or task.min_free_mb used to wait in silence until
	// its --timeout and end with nothing started and nothing to say why.
	maxLoad, minFreeMB := config.TaskMaxLoadAt(settings.ProfileDir), config.TaskMinFreeMBAt(settings.ProfileDir)
	hold := watchMachineHold(session.NewRunAdmission(maxLoad, minFreeMB, session.NewTaskLanes()),
		request.stderr, maxLoad, minFreeMB)

	_, summary := runengine.Start(ctx, runengine.Spec{
		Store:     store,
		Workspace: workspace,
		Title:     title,
		Brief:     request.task,
		Slots:     request.slotsFor(settings.ProfileDir),
		Limits:    limits,
		Gate:      hold.runGate(),
		Factory: runengine.CrewFactory(store, workspace, settings.ProfileDir, runengine.Seats{
			Work:  seats.Work.Model,
			Plan:  seats.Plan.Model,
			Check: seats.Check.Model,
		}, completerFor),
	})
	errand := headlessOutcome{
		Artifacts: []string{},
		Nodes:     summary.Nodes,
		Seconds:   summary.Seconds,
		Spend:     summary.USD,
	}
	switch summary.Outcome {
	case runengine.OutcomeDone:
		errand.stop, errand.Settled, errand.Deliverable = stopDone, true, strings.TrimSpace(summary.Result)
	case runengine.OutcomeLimit:
		// The only limit this road sets is the spending bound, so a run the
		// engine stopped on a limit is one that reached it, and the sentence
		// is the bound's own: the figure, and what to pass to go past it.
		errand.stop, errand.Settled = bound.stop, true
		errand.BlockedOn = bound.words
	case runengine.OutcomeCannotRun:
		errand.stop = stopError
		errand.Error = "the run could not be started"
	default:
		// ran and did not finish: a leaf failed, or the clock arrived.
		errand.stop, errand.Settled = stopIncomplete, true
	}
	// THE RUN'S OWN CLOCK, read off the context because the summary's word is
	// the same one a failed leaf leaves and a caller raising a timeout has to be
	// able to tell them apart. It is 124 rather than the ladder's rung for a
	// deadline, and [headlessOutcome.status] says why.
	if ctx.Err() == context.DeadlineExceeded {
		errand.stop, errand.wall, errand.Settled = stopDeadline, true, false
		// A WALL THAT ARRIVED WHILE THE MACHINE HELD EVERY START is not a run
		// that was slow: nothing began. `stop` keeps the wall's word, which is
		// the one vocabulary every headless verb speaks, and `blocked_on` says
		// what it was blocked on, so a script can tell the two walls apart.
		if held := hold.heldLine(); held != "" && summary.Nodes == 0 {
			errand.BlockedOn, errand.machineHeld = held+" · nothing started before --timeout", true
		}
	}
	// WHAT THE RUN CHANGED IS WHERE IT STANDS: in the directory it was handed,
	// uncommitted, on whatever branch was checked out there. The envelope's
	// files are those paths and no others, on every ending — a run stopped short
	// still left its edits on disk, and a caller has to be able to find them.
	errand.Artifacts = landedPaths(workspace, before.Changed())
	return errand, nil
}

// machineHold is `codeaf do`'s voice for the machine gate on the run road. The
// chat has a rail to draw `waiting · machine busy` on; a headless run has only
// stderr, so the first start the gate refuses is said there in the rail's own
// words, with the settings row whose ceiling held it, and the first start
// after that says the machine has room again. ONE LINE EACH: the gate is
// re-asked every supervisor pass, and a line per refusal would be a line every
// 300 milliseconds for as long as the machine stays busy.
type machineHold struct {
	session.RunAdmission
	stderr    io.Writer
	maxLoad   float64
	minFreeMB int

	mu sync.Mutex
	// said is the held line once it has been printed, and empty before.
	said    string
	resumed bool
}

// machineResumedLine is what stderr says when a start the machine held begins.
const machineResumedLine = "starting · the machine has room again"

// watchMachineHold wraps the run's machine gate. A nil gate — both rows at 0 —
// can hold nothing, and nil comes back.
func watchMachineHold(gate session.RunAdmission, stderr io.Writer, maxLoad float64, minFreeMB int) *machineHold {
	if gate == nil {
		return nil
	}
	return &machineHold{RunAdmission: gate, stderr: stderr, maxLoad: maxLoad, minFreeMB: minFreeMB}
}

// MayStart asks the machine, and says the first refusal.
func (h *machineHold) MayStart() bool {
	if h.RunAdmission.MayStart() {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.said == "" {
		h.said = h.heldWords(h.RunAdmission.HeldBy())
		if h.stderr != nil {
			fmt.Fprintln(h.stderr, h.said)
		}
	}
	return false
}

// Started counts the worker, and says the first start after a hold.
func (h *machineHold) Started() {
	h.RunAdmission.Started()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.said != "" && !h.resumed {
		h.resumed = true
		if h.stderr != nil {
			fmt.Fprintln(h.stderr, machineResumedLine)
		}
	}
}

// heldWords is the held line: the rail's words, then the limit that held it
// when the governor named one.
func (h *machineHold) heldWords(heldBy string) string {
	line := "waiting · " + session.MachineBusy
	switch heldBy {
	case config.KeyTaskMaxLoad:
		return line + fmt.Sprintf(" · load per core at or above %s %g", config.KeyTaskMaxLoad, h.maxLoad)
	case config.KeyTaskMinFreeMB:
		return line + fmt.Sprintf(" · available memory under %s %d MiB", config.KeyTaskMinFreeMB, h.minFreeMB)
	}
	return line
}

// heldLine is the held line if one was said, and empty when the machine never
// held a start.
func (h *machineHold) heldLine() string {
	if h == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.said
}

// runGate is the wrapper as the engine's gate. A nil wrapper is a nil gate,
// never a typed nil inside the interface that the supervisor would call.
func (h *machineHold) runGate() runengine.AdmissionGate {
	if h == nil {
		return nil
	}
	return h
}

// runRoadRefusesStore is the sentence `codeaf do --db` answers on the run
// engine. The flag names a store the older engine works in, and a run keeps its
// plan in a private folder, so there is nothing for the flag to point
// at; the sentence says where the plan is instead and how to reach the engine
// that takes the flag.
const runRoadRefusesStore = "--db names a store only the older engine works in; " +
	"a run keeps its plan in a private folder under the state root's runs directory. " +
	"Drop --db, or set CODEAF_TASK_BELT=node to run this on the older engine"

// runSpend is the spending bound a run on this road is held to: the dollars
// it may spend (0 is no bound), which rung of the exit ladder reaching it is,
// the sentence `blocked_on` carries when it is reached, and whether the run
// may not start at all.
type runSpend struct {
	usd     float64
	stop    stopReason
	words   string
	refused bool
}

// doSpendGuard is the guard a `do` run's calls are held to. preauthorized
// (-yes-spend) lifts the day's spending limit and the crew's daily cap from it
// and nothing else: the per-task limit holds either way, with or without a
// routed crew.
func doSpendGuard(profileDir string, crew *crewroute.Decision, preauthorized bool) *session.SpendGuard {
	if crew == nil {
		return session.TaskSpendGuard(profileDir)
	}
	guard := session.CrewSpendGuard(profileDir, *crew, !preauthorized)
	if preauthorized {
		// THE CREW'S DAILY CAP TOO: the door let the run start past it on this
		// flag (doErrand's refusal names it), so its first call must not stop there.
		guard.Cap, guard.CapAction = 0, ""
	}
	return guard
}

// runSpendBound is THE SPENDING CONTRACT `--yes-spend` promises
// ([yesSpendFlagHelp]): without it, a run stops at the plan-price question's
// figure and at what is left of today's limit, whichever is nearer; with it,
// or with CODEAF_PREAUTHORIZE_SPEND=1, neither stops it.
//
// THE OLDER ROAD ASKED BEFORE IT BOUGHT, and this one cannot: a run has no
// estimate before its workers start, because nothing plans the whole of it up
// front. So the question becomes a ceiling. The run spends up to the figure
// the person set as the point where codeaf asks first (CODEAF_PLAN_CONSENT, or
// the profile's row for it), stops there with exit 3 and `stop` `price`, and
// says what to pass to go further. A figure of 0 is "never ask", and that rung
// does not bound the run.
//
// TODAY'S LIMIT IS THE SECOND RUNG, measured against the usage ledger the
// workers write, so a run started late in an expensive day stops where the day
// does. A day already spent starts nothing. A limit of 0 is no daily limit.
//
// A CAP HANDED IN BY A CALLER (request.costCap) is its own contract and wins
// over both, which is how a test holds a run to a price.
func runSpendBound(request doRequest, profileDir string, now time.Time) (runSpend, error) {
	if request.costCap != nil {
		limit := *request.costCap
		if limit <= 0 {
			return runSpend{refused: true, stop: stopBudget,
				words: fmt.Sprintf("this run's cost cap is $%.2f, so nothing was started", limit)}, nil
		}
		return runSpend{usd: limit, stop: stopBudget,
			words: fmt.Sprintf("the run reached the cost cap of $%.2f", limit)}, nil
	}
	if spendPreauthorized(request.yesSpend, env.Value) {
		return runSpend{}, nil
	}
	consent, err := config.PlanConsentUSDAt(profileDir)
	if err != nil {
		return runSpend{}, err
	}
	daily, err := config.DailyBudgetUSDAt(profileDir)
	if err != nil {
		return runSpend{}, err
	}
	bound := runSpend{}
	if consent > 0 {
		bound = runSpend{usd: consent, stop: stopPrice, words: fmt.Sprintf(
			"the run reached $%.2f, the price above which codeaf asks before it spends more; "+
				"rerun with --yes-spend to let it go past that", consent)}
	}
	if daily > 0 {
		left := daily - spentToday(now)
		if left <= 0 {
			return runSpend{refused: true, stop: stopBudget, words: fmt.Sprintf(
				"today's spending limit of $%.2f is spent, so nothing was started; "+
					"rerun with --yes-spend to spend past it", daily)}, nil
		}
		if bound.usd == 0 || left < bound.usd {
			bound = runSpend{usd: left, stop: stopBudget, words: fmt.Sprintf(
				"the run reached what was left of today's spending limit of $%.2f; "+
					"rerun with --yes-spend to spend past it", daily)}
		}
	}
	return bound, nil
}

// spentToday is what today has cost on this machine, read off the usage ledger
// every conversation and every run worker writes ([session.SpendToday]). A
// ledger that cannot be read is a day that has spent nothing as far as this
// door can tell; the plan-price rung still bounds the run.
func spentToday(now time.Time) float64 {
	lines, err := session.ReadUsage(session.UsageLedgerPath(), now.Add(-48*time.Hour))
	if err != nil {
		return 0
	}
	return session.SpendToday(lines, now)
}

// crewCompleters turns the run road's provider seam into the per-model
// completer [runengine.CrewFactory] asks for. The factory reads the crew at every
// launch, so a task's seat model is not known until the task is handed over;
// this builds one client per model on first ask and hands the same one back
// after, so two tasks in one seat share a handle rather than opening a second.
//
// A MODEL THE DOOR CANNOT REACH IS A SEAT THAT CANNOT RUN. A builder error is
// kept as a completer that refuses every call in the builder's own words, so a
// task seated on an unbuildable model fails with that reason rather than
// reaching a provider the door never built.
func crewCompleters(settings config.Config, build func(config.Config, string) (*liveClient, error)) func(string) session.Completer {
	var mu sync.Mutex
	made := make(map[string]session.Completer, 4)
	return func(model string) session.Completer {
		mu.Lock()
		defer mu.Unlock()
		if c, ok := made[model]; ok {
			return c
		}
		client, err := build(settings, model)
		completer := session.Completer(client)
		if err != nil || client == nil {
			completer = seatlessCompleter{err: err}
		}
		made[model] = completer
		return completer
	}
}

// seatlessCompleter is the seat a task gets when the door cannot build a
// provider client for its model. Every call refuses in the builder's own words,
// so the task fails on the reason rather than on a nil it would have had to
// guard against. A builder that answers no client and no error is the same
// refusal, named as the empty seat it is rather than a nil the worker would
// dereference.
type seatlessCompleter struct{ err error }

func (c seatlessCompleter) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	if c.err == nil {
		return nil, errors.New("no provider client could be built for this seat")
	}
	return nil, c.err
}

// topicTitle is the run's own name for the thing it was asked for: the first
// line of the ask, bounded, because the root task's title is the commit message
// the landing writes. It is deliberately not a model call — the ask the door
// was handed is the whole assignment, and a naming round-trip in front of it
// would buy a label nothing downstream waits on.
func topicTitle(task string) string {
	title := firstLine(strings.TrimSpace(task))
	if title == "" {
		return "codeaf do"
	}
	return clip(title, 72)
}

// landedPaths is what a landing carried, as absolute paths under the working
// copy: the commit reads its paths relative to the copy, and every other file
// list the product prints is absolute, so a caller can open one.
func landedPaths(workspace string, changed []string) []string {
	paths := make([]string, 0, len(changed))
	for _, path := range changed {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if filepath.IsAbs(path) {
			paths = append(paths, path)
			continue
		}
		paths = append(paths, filepath.Join(workspace, path))
	}
	sort.Strings(paths)
	return paths
}

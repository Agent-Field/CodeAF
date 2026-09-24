package main

// `codeaf <name> …` for a program this build carries (internal/delegate): the
// verb every one of them answers, from a person's shell and from the chat's
// own run alike.
//
// TWO CALLERS, ONE LINE. The chat's run starts `codeaf senior-dev run --json
// --dir … -- <brief>` as its child, with the model API's address and token in
// the child's environment; a person types the same verb at a shell with
// neither. The environment is how the two are told apart: a child of a host
// runs the program's body here and writes its records on stdout; a shell run
// becomes the host itself — it serves the model API and starts the same child.
//
// ── A SHELL RUN IS THE SAME TWO PROCESSES A CHAT'S RUN IS ───────────────────
//
// The host reaches models the way every headless verb does — the person's own
// profile, its services and its keys (config.Load) — and serves them to the
// program through a model API of its own (internal/provider/modelapi), exactly
// as the chat's run does: the program is started as a child of this very
// executable with the API's address and token and no key, every call it makes
// is metered, held to the ceiling the person set, written to this machine's
// spending ledger, and kept as one turn of a conversation log in the run's own
// record folder. What the chat draws on a task page, the host prints as lines.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/delegate/builtin"
	"github.com/Agent-Field/codeaf/internal/home"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/reltime"
)

// carriedStdout is where a carried verb writes what a person reads: its help,
// and a shell run's lines or records. A variable so a test can read it back;
// a child of a host writes its records to the real stdout whatever this says,
// because that pipe is its host's.
var carriedStdout io.Writer = os.Stdout

// carriedStderr is where a shell run says what it is doing about its own
// ending — the stop, and the wait for a last call's price — beside the lines
// or records on stdout. A variable so a test can read it back.
var carriedStderr io.Writer = os.Stderr

// carriedGrace overrides the launch's SIGTERM grace for a shell run, for a
// test that must not wait fifteen seconds; zero is delegate.DefaultGrace.
var carriedGrace time.Duration

// runCarried runs one line of a carried program's verb and leaves on the exit
// ladder (envelope.go).
func runCarried(program delegate.Delegate, args []string) error {
	inv, err := delegate.Parse(program, args, carriedStdout)
	if errors.Is(err, delegate.ErrHelp) {
		return exitDone
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return exitCannotRun
	}
	// SIGTERM IS THE HOST'S STOP (internal/delegate's launch): the body's
	// context ends, and the program writes its terminal on the way out.
	ctx, stop := carriedSignals()
	defer stop()
	if _, child := delegate.ModelAPIFromEnv(); child {
		return carriedExit(delegate.RunChild(ctx, inv, os.Stdout))
	}
	return runCarriedHost(ctx, inv)
}

// carriedSignals is a shell run's context: it ends on the first ctrl-c or
// SIGTERM, and that first signal hands the rest back to the terminal.
//
// A SECOND CTRL-C LEAVES AT ONCE. After the first one the run still waits for
// the program's grace, its last calls to finish and the price of a call the
// stop cut short — up to about a minute and a half, said on stderr as it
// happens. Holding the signals for all of that swallowed a second ctrl-c, and
// a person who means "now" is owed a way out that does not wait for money to
// be counted. What leaving costs is said in the manual: a price still being
// waited for is then not in the run's line.
func carriedSignals() (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	context.AfterFunc(ctx, stop)
	return ctx, stop
}

// carriedRoad is how one shell run reaches models: the funnel a call on a
// model goes out through, whether this person's services can take a call on a
// model, and the work seat a call nothing here can serve is answered on.
type carriedRoad struct {
	completerFor func(model string) modelapi.Completer
	serves       func(model string) bool
	seat         string
	// sign is the person's `attribution` row, which puts the trailer on the
	// one commit codeaf writes when the run ends (internal/session's
	// ProgramFolder.Finish).
	sign bool
}

// carriedModels resolves a shell run's road. It is the person's own profile,
// read the way `codeaf exec` reads it; a variable so a test can hand a
// scripted road instead of a profile and a key.
var carriedModels = profileRoad

// profileRoad is the road through the person's profile: config.Load's
// services and keys — so a machine with no key at all is answered with the
// one sentence every command gives it — the crew's work seat, and one adapter
// per model, built the way every client outside internal/config is built
// ([config.Config.ClientConfig]).
func profileRoad() (carriedRoad, error) {
	settings, err := config.Load()
	if err != nil {
		return carriedRoad{}, err
	}
	useAutoSeats(settings)
	seats := config.ResolveSeats(settings.ProfileDir, "", "")
	settings.Models = sharedCatalog(settings)
	adapters := &carriedAdapters{settings: settings, built: map[string]modelapi.Completer{}}
	sources := settings.Sources.OrDefault(settings.APIKey, settings.BaseURL)
	return carriedRoad{
		completerFor: adapters.forModel,
		serves:       func(model string) bool { return session.ServesModel(sources, model) },
		seat:         seats.Work.Model,
		sign:         settings.Attribution,
	}, nil
}

// carriedAdapters is one adapter per model a shell run's program asks for,
// built once and kept for the run.
type carriedAdapters struct {
	settings config.Config
	mu       sync.Mutex
	built    map[string]modelapi.Completer
}

// forModel is the adapter for one model. THE WIRE SLUG IS THE LAST OPTION: a
// program names a model in the person's own spelling — `openrouter/…`, a
// connection's own prefix — and that spelling decides the account; what goes
// on the wire is the service's own id for it, which config resolved beside
// the account, exactly as the conversation's own door appends it
// (internal/session's completeWithNamedModel).
func (a *carriedAdapters) forModel(model string) modelapi.Completer {
	a.mu.Lock()
	defer a.mu.Unlock()
	if built, ok := a.built[model]; ok {
		return built
	}
	configured := a.settings.ClientConfig(model)
	client, err := provider.NewClient(configured)
	if err != nil {
		return refusingCompleter{err: err}
	}
	built := wireCompleter{client: client, wire: configured.Model}
	a.built[model] = built
	return built
}

// wireCompleter is one adapter with the model's wire slug appended to every
// call.
type wireCompleter struct {
	client *provider.Client
	wire   string
}

func (c wireCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	return c.client.CompleteWithMessages(ctx, messages, append(options, ai.WithModel(c.wire))...)
}

// refusingCompleter is a model whose adapter could not be built: every call is
// answered with why.
type refusingCompleter struct{ err error }

func (c refusingCompleter) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	return nil, c.err
}

// runCarriedHost is a person's shell run: this process serves the model API
// with the person's own services and starts the program as its own child.
func runCarriedHost(ctx context.Context, inv *delegate.Invocation) error {
	// A RUN WITH NOTHING TO DO IS NOT STARTED. The default command is a whole
	// task, and a task with no brief is a program sent off to guess; the line
	// that says what it wanted is the answer, and nothing is spent on the way.
	if inv.Command.Name == inv.Program.Default && inv.Brief() == "" {
		fmt.Fprintf(os.Stderr, "no brief given\n\n%s\n\nrun `codeaf %s --help` for its commands and flags.\n",
			strings.Join(foldSynopsis("codeaf "+inv.Program.Name+" "+carriedSynopsis), "\n"), inv.Program.Name)
		return exitCannotRun
	}
	road, err := carriedModels()
	if err != nil {
		return err
	}
	// A PERSON TYPED THIS AND IS WATCHING ITS LINES, which is the fact the
	// lane layer reads for the calls that ride no context of the door's own
	// (exec.go's typedDoorContext says the whole of why).
	provider.SetPersonAtTheDoor(true)
	record := carriedRecordDir(inv.Program.Name)
	view := newCarriedView(carriedStdout, inv, record)
	// THE FOLDER IS READIED BEFORE ANYTHING STARTS, the one way a conversation's
	// run readies it (internal/session's programfolder.go): the folder itself,
	// on a branch of its own in a repository, and a refusal — changes that are
	// not committed, another program's run in it — before a cent is spent. A
	// plain folder is no longer the program's first-line failure: codeaf says
	// so on the program's line.
	folder, err := carriedFolder(inv, record, road.sign)
	if err != nil {
		fmt.Fprintln(carriedStderr, "error:", err)
		return exitCannotRun
	}
	view.inFolder(folder)
	// AND THE FOLDER IS FINISHED ON EVERY ROAD OUT: the run's ending below, or
	// a door that failed before the program ever ran, which leaves nothing.
	finished := false
	finish := func(result string) {
		if folder != nil && !finished {
			finished = true
			view.left(folder.Finish(result).Sentence())
		}
	}
	defer finish("")

	runCtx, cut := context.WithCancel(ctx)
	defer cut()
	// A LIMIT THE PERSON SET ENDS THE PROGRAM FROM OUTSIDE, whatever it does
	// with the same figure on its own command line: the hours by a clock here,
	// the dollars the moment a metered call reaches them. The API refuses the
	// next call as well, so a program that ignores its SIGTERM cannot spend on.
	var limited atomic.Bool
	if wall := inv.Ceilings.Elapsed(); wall > 0 {
		clock := time.AfterFunc(wall, func() {
			limited.Store(true)
			cut()
		})
		defer clock.Stop()
	}
	ledger := session.UsageLedgerPath()
	// A SHELL RUN'S ROWS NAME THE RUN. There is no conversation and no task
	// behind them, and a row that named nothing was money the spending page
	// could not say anything about; the run's own record folder is the one
	// name it has, so its rows are filed as one piece of work under it.
	subject := filepath.Base(record)
	api, err := modelapi.Open(modelapi.Config{
		TaskDir:      record,
		CompleterFor: road.completerFor,
		Serves:       road.serves,
		Seat:         road.seat,
		Ceiling:      inv.Ceilings.CostUSD,
		Bank: func(charge modelapi.Charge) {
			// THE MACHINE'S SPENDING LEDGER, one row per call, written here and
			// nowhere else: nothing else in this process meters these calls.
			line := session.UsageLine{
				Model: charge.Model, Calls: 1, Input: charge.TokensIn, Output: charge.TokensOut, USD: charge.CostUSD,
				Reconciled: charge.Late, Workspace: inv.Workspace, Task: subject,
			}
			session.RecordUsage(ledger, session.TagUsage(line, roles.RoleWorker, session.SeatWorker))
			view.call(charge)
			if ceiling := inv.Ceilings.CostUSD; ceiling > 0 && charge.Spent >= ceiling {
				limited.Store(true)
				cut()
			}
		},
		Unbilled: func(model string) {
			session.RecordUnbilledCall(ledger, session.TagUsage(session.UsageLine{Model: model, Workspace: inv.Workspace, Task: subject}, roles.RoleWorker, session.SeatWorker))
		},
		// THE LAST CALL'S PRICE IS WAITED FOR, AND THE PERSON IS TOLD WHY. A
		// run stopped by ctrl-c or its own ceiling is usually in the middle of
		// a call, priced by a receipt about twenty seconds later; this process
		// used to exit first, and that call never reached the ledger.
		Settling: func(owed int) { fmt.Fprintln(carriedStderr, carriedSettlingLine(owed)) },
		Role:     lanes.RoleLeafAttached,
		Node:     inv.Program.Name,
	})
	if err != nil {
		return err
	}
	defer func() { _ = api.Close() }()
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find codeaf's own executable to run %s: %w", inv.Program.Name, err)
	}
	here, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("read the folder %s was started in: %w", inv.Program.Name, err)
	}
	grace := carriedGrace
	if grace <= 0 {
		grace = delegate.DefaultGrace
	}
	// CTRL-C IS A STOP, SAID AS ONE. The program is sent SIGTERM and given the
	// grace to write how it ended; the person is told that much at once rather
	// than left watching a terminal that has gone quiet for fifteen seconds.
	untell := context.AfterFunc(ctx, func() {
		fmt.Fprintf(carriedStderr, "stopping %s: it has %s to say how it ended\n", inv.Program.Name, grace)
	})
	view.begin()
	// THE PROGRAM'S OWN CLOCK is written in its record folder, as a chat's
	// run writes it in the task's: the instant its process was started and
	// the instant it was gone (delegate.ProgramRecord).
	started := time.Now()
	view.opened(started)
	result, runErr := delegate.Run(runCtx, delegate.Launch{
		Name: inv.Program.Name,
		Bin:  exe,
		Args: carriedInFolder(carriedChildLine(inv), inv, folder),
		// NO KEY REACHES THE PROGRAM (delegate.ChildEnv): the API's address and
		// token are the whole of what it is given.
		Env:        delegate.ChildEnv(api.API()),
		Dir:        here,
		StderrPath: filepath.Join(record, carriedStderrName),
		Grace:      grace,
	}, view)
	// THE INSTANT THE PROCESS WAS GONE, not the instant its stdout drained, for
	// both the last line and the record's end: a helper the program left holding
	// stdout kept the launch open for up to the grace after the exit, and the
	// same program read that much longer here than in a conversation, whose
	// worker reads it this way ([delegate.Result.ExitedAt]).
	ended := result.ExitedAt(started, time.Now())
	untell()
	// The program has exited: its API goes with it, so nothing it left behind
	// can spend, and every row it cost is on disk before this process leaves —
	// the close waits for the price of a call the stop cut in the middle.
	_ = api.Close()
	view.closed(ended)
	session.CloseUsage()
	finish(view.endingWords())
	return view.end(result, runErr, limited.Load(), api.Spent(), ended.Sub(started))
}

// carriedFolder readies the folder a shell run's program works in
// (internal/session's PrepareProgramFolder); nil for a program that edits no
// files, which reads the folder where it is.
func carriedFolder(inv *delegate.Invocation, record string, sign bool) (*session.ProgramFolder, error) {
	if !inv.Program.LandsTree() {
		return nil, nil
	}
	return session.PrepareProgramFolder(session.ProgramFolderOrder{
		Program: inv.Program, Dir: inv.Workspace, Brief: inv.Brief(),
		Holder: "a run started at a shell", Keep: record, Sign: sign,
		Instead: "run it in the project's folder, or name that folder with --dir",
	})
}

// carriedInFolder puts on a shell run's child line what codeaf decided about
// its folder, after --json and before the person's own words: the folder
// itself when it is not the one the line names (a folder inside a repository
// is worked in at the repository's root, and the person's own --dir is taken
// off so it cannot win), and the program's own flags for a folder worked in
// without git ([delegate.Delegate.PlainFolder]).
func carriedInFolder(child []string, inv *delegate.Invocation, folder *session.ProgramFolder) []string {
	if folder == nil {
		return child
	}
	at := slices.Index(child, "--json")
	if at < 0 {
		return child
	}
	head, rest := append([]string(nil), child[:at+1]...), child[at+1:]
	if folder.Dir != inv.Workspace {
		flags, words := rest[:len(rest)-len(inv.Args)], rest[len(rest)-len(inv.Args):]
		kept := make([]string, 0, len(flags))
		for i := 0; i < len(flags); i++ {
			switch flag := flags[i]; {
			case flag == "--dir" || flag == "-dir":
				i++
			case strings.HasPrefix(flag, "--dir=") || strings.HasPrefix(flag, "-dir="):
			default:
				kept = append(kept, flag)
			}
		}
		head = append(head, "--dir", folder.Dir)
		rest = append(kept, words...)
	}
	if folder.Plain() {
		head = append(head, inv.Program.PlainFolder...)
	}
	return append(head, rest...)
}

// carriedSettlingLine is what a shell run says while it waits for the
// receipts still owed on the calls its ending cut short, bounded by the
// provider's own schedule (provider.ReceiptWait).
func carriedSettlingLine(owed int) string {
	calls := "1 call that was"
	if owed != 1 {
		calls = strconv.Itoa(owed) + " calls that were"
	}
	return "waiting up to " + reltime.Elapsed(provider.ReceiptWait) + " for the price of " + calls + " cut short"
}

// carriedStderrName is the file a shell run keeps its program's stderr in,
// beside the conversation log — the name the chat's run keeps it under too.
const carriedStderrName = "delegate-stderr.log"

// carriedRecordDir is a shell run's record folder: the conversation log, the
// action log, the program record and the program's stderr, under this machine's state root
// where a person can open them after the lines have scrolled away. It has no
// task page to live beside, so it has a folder of its own, one per run.
func carriedRecordDir(name string) string {
	return filepath.Join(carriedRecordRoot(name), time.Now().Format("20060102-150405.000000"))
}

// carriedRecordRoot is the folder every shell run of one program keeps its
// record under, one folder per run.
func carriedRecordRoot(name string) string {
	return home.Join("v3", "carried", name)
}

// carriedChildLine is the line a shell run starts its child with: THE
// PERSON'S OWN LINE, with --json added after the command word. It is not
// delegate.ChildArgs, which is the chat's line — the default command and the
// shared flags only — because a person at a shell may name another command or
// give the command a flag of its own, and a line rebuilt from the parsed
// invocation would silently drop both. The child reads this line with the same
// parser the host just read it with, from the same folder ([runCarriedHost]
// starts it where it was started), so it arrives at the same invocation.
func carriedChildLine(inv *delegate.Invocation) []string {
	line := append([]string(nil), inv.Line...)
	head := []string{inv.Program.Name}
	if len(line) > 0 {
		if _, named := inv.Program.Command(line[0]); named {
			head, line = append(head, line[0]), line[1:]
		}
	}
	head = append(head, "--json")
	return append(head, line...)
}

// carriedExit is an ending on the exit ladder: the work stands, a limit you
// set stopped it, or it ran and did not finish.
func carriedExit(status string) error {
	switch status {
	case delegate.StatusPass:
		return exitDone
	case delegate.StatusBudget:
		return exitLimit
	default:
		return exitIncomplete
	}
}

// ── the front page ──────────────────────────────────────────────────────────

// carriedHeading heads the group `codeaf --help` lists the carried programs
// under, in the table's own register: what the group is, a dash, the one thing
// a reader needs to know about all of it. It names no machinery: a person
// reads the program's own name, never the word the code calls it by.
const carriedHeading = "Hand it a whole task — a program codeaf carries does it on its own"

// carriedSynopsis is the shape every carried program's line takes: the brief,
// and the folder and the two ceilings codeaf puts on every one of them
// (delegate.Parse). It is ONE LINE ON PURPOSE: the front page was cut to fit a
// screen and a bit, and a program costs it two lines — this and its summary.
// `--json`, the program's own commands and their flags are its `--help`.
const carriedSynopsis = `"<brief>" [--dir dir] [--max-cost usd] [--max-hours h]`

// carriedGroup is the group `codeaf --help` gives the programs a build
// carries: one line per program in the table's shape, its summary under it.
// A build that carries none gets no group at all — not a heading over nothing
// — which is every Windows build.
func carriedGroup(programs []delegate.Delegate) string {
	if len(programs) == 0 {
		return ""
	}
	lines := []string{carriedHeading}
	for _, program := range programs {
		lines = append(lines, foldSynopsis("codeaf "+program.Name+" "+carriedSynopsis)...)
		indent := strings.Repeat(" ", helpTextColumn)
		for _, line := range wrapAt(program.Summary, helpWidth-helpTextColumn) {
			lines = append(lines, indent+line)
		}
	}
	// A program's own commands and flags are its `--help`, which the page's
	// last line already names for every command; saying it again here would be
	// a line of the capped page spent on a sentence the reader has.
	return strings.Join(lines, "\n")
}

// synopsisFold is the column a folded synopsis continues at: under the verb,
// so the flags stay one column, which is where the table folds every other
// command's (main.go's layout law).
const synopsisFold = 14

// foldSynopsis writes one command's synopsis at column 2 and folds it, when it
// must, at [synopsisFold], so no line draws wider than [helpWidth].
func foldSynopsis(synopsis string) []string {
	words := strings.Fields(synopsis)
	if len(words) == 0 {
		return nil
	}
	lines := []string{"  " + words[0]}
	for _, word := range words[1:] {
		last := len(lines) - 1
		if len(lines[last])+1+len(word) > helpWidth {
			lines = append(lines, strings.Repeat(" ", synopsisFold)+word)
			continue
		}
		lines[last] += " " + word
	}
	return lines
}

// frontPage is `codeaf --help` as it is printed: the one table, with the
// programs this build carries listed as a group of their own right after the
// work you hand it — they are work you hand it, the whole of a task. The table
// itself stays one constant ([usageText]) so every per-command page is still a
// reading of it; the group is read from the build's list at the moment of
// printing, because that list is what the build carries.
func frontPage() string {
	group := carriedGroup(builtin.All())
	if group == "" {
		return usageText
	}
	const after = "\nLook at what happened"
	at := strings.Index(usageText, after)
	if at < 0 {
		return usageText + "\n\n" + group
	}
	return usageText[:at] + "\n" + group + "\n" + usageText[at:]
}

// ── what a person at the shell sees ─────────────────────────────────────────

// carriedView is a shell run's delegate.Sink and its call line: the stage as
// it changes, each step, each model call, and the ending, as lines a person
// reads — or, with --json, the program's records passed through as records.
// The reader's goroutine, the API's calls and the host itself all write here,
// so every write is taken under one lock.
type carriedView struct {
	mu       sync.Mutex
	out      io.Writer
	inv      *delegate.Invocation
	record   string
	records  *delegate.Emitter
	stage    string
	status   string
	calls    int
	terminal *delegate.Terminal
	// program is the run's program record as it stands, rewritten whole in
	// the record folder each time it learns something: its start, its hello,
	// its end.
	program delegate.ProgramRecord
	// folder is the folder the run was readied in, and leftFolder is how the
	// run left it (internal/session's ProgramFolderEnd.Sentence); nil and
	// empty for a program that edits no files.
	folder     *session.ProgramFolder
	leftFolder string
}

func newCarriedView(out io.Writer, inv *delegate.Invocation, record string) *carriedView {
	view := &carriedView{out: out, inv: inv, record: record}
	if inv.JSON {
		view.records = delegate.NewEmitter(out)
	}
	return view
}

// begin says what is starting, where.
func (v *carriedView) begin() {
	if v.records != nil {
		return
	}
	v.say("%s · working in %s", v.inv.Program.Name, v.where())
}

// inFolder keeps the folder the run was readied in, for the line that says
// where it works.
func (v *carriedView) inFolder(folder *session.ProgramFolder) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.folder = folder
}

// where is the folder the program works in, as the first line says it: on its
// own branch when codeaf cut one.
func (v *carriedView) where() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.folder == nil {
		return v.inv.Workspace
	}
	if v.folder.Plain() {
		return v.folder.Dir
	}
	return v.folder.Dir + ", on its own branch " + v.folder.Branch
}

// left keeps how the run left its folder, for the lines that end the run.
func (v *carriedView) left(sentence string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.leftFolder = sentence
}

// endingWords is the program's ending in the one sentence a person reads
// ([carriedEnding]), the body of the commit that holds what it left.
func (v *carriedView) endingWords() string {
	terminal, _ := v.ending()
	if terminal == nil {
		return ""
	}
	return carriedEnding(v.inv.Program.Name, *terminal)
}

func (v *carriedView) Hello(h delegate.Hello) {
	v.remember(func(record *delegate.ProgramRecord) { record.Stages = h.Stages })
	if v.records != nil {
		_ = v.records.Hello(v.inv.Program.Name, h.Stages)
	}
}

// opened writes the program record the moment the program's process is
// started: whose run it is, its ceiling, and when it began.
func (v *carriedView) opened(at time.Time) {
	v.remember(func(record *delegate.ProgramRecord) {
		record.Name, record.CeilingUSD, record.StartedAt = v.inv.Program.Name, v.inv.Ceilings.CostUSD, at
	})
}

// closed writes the instant the program's process was gone.
func (v *carriedView) closed(at time.Time) {
	v.remember(func(record *delegate.ProgramRecord) { record.EndedAt = at })
}

// remember changes the program record and writes it whole. It is a record, so
// a disk that refuses it costs the record and never the run.
func (v *carriedView) remember(change func(record *delegate.ProgramRecord)) {
	v.mu.Lock()
	defer v.mu.Unlock()
	change(&v.program)
	if v.program.Name == "" {
		v.program.Name = v.inv.Program.Name
	}
	_ = delegate.WriteProgram(v.record, v.program)
}

// kept writes one received record to the run's action log in its record
// folder, stamped with the moment it arrived — the same log a chat's run keeps
// beside its task (delegate.ActionsFile), so the two roads leave one record. It
// is a record, so a disk that refuses it costs the record and never the run.
func (v *carriedView) kept(action delegate.Action) {
	if strings.TrimSpace(v.record) == "" {
		return
	}
	_ = delegate.AppendAction(v.record, action)
}

func (v *carriedView) Stage(record delegate.StageRecord) {
	v.kept(delegate.StageAction(time.Now(), record))
	if v.records != nil {
		_ = v.records.Stage(record)
		return
	}
	stage, status := record.Stage, record.Status
	if !v.moved(stage, status) {
		return
	}
	if status == "" {
		v.say("%s", stage)
		return
	}
	v.say("%s · %s", stage, status)
}

// moved takes the program's new phase and answers whether it is a change: a
// stage said twice is one line, not two.
func (v *carriedView) moved(stage, status string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	changed := stage != v.stage || status != v.status
	v.stage, v.status = stage, status
	return changed
}

func (v *carriedView) Step(record delegate.StepRecord) {
	v.kept(delegate.StepAction(time.Now(), record))
	if v.records != nil {
		_ = v.records.Step(record)
		return
	}
	if head := firstLineOf(record.Observation); head != "" {
		v.say("  %s · %s", record.Command, head)
		return
	}
	v.say("  %s", record.Command)
}

func (v *carriedView) Terminal(t delegate.Terminal) {
	v.keep(t)
	v.kept(delegate.EndAction(time.Now(), t))
	if v.records == nil {
		return
	}
	// THE RECORD PASSES THROUGH AS THE PROGRAM WROTE IT: its data travels whole,
	// every key the program put there, in the one terminal this stdout carries.
	extra := make(map[string]any, len(t.Data))
	for key, value := range t.Data {
		extra[key] = value
	}
	_ = v.records.Terminal(delegate.Ending{Status: t.Status, Message: t.Message, Extra: extra})
}

// keep holds the program's ending for the run's last lines.
func (v *carriedView) keep(t delegate.Terminal) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.terminal = &t
}

// counted counts one metered call.
func (v *carriedView) counted() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.calls++
}

// ending is the program's ending and how many calls it made.
func (v *carriedView) ending() (*delegate.Terminal, int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.terminal, v.calls
}

// call is one metered model call: `model · N in · N out · $X`, with whatever
// nobody measured left off rather than written as a zero.
func (v *carriedView) call(charge modelapi.Charge) {
	v.counted()
	if v.records != nil {
		return
	}
	parts := []string{charge.Model}
	if charge.Model == "" {
		parts[0] = "model"
	}
	if charge.TokensIn > 0 {
		parts = append(parts, strconv.Itoa(charge.TokensIn)+" in")
	}
	if charge.TokensOut > 0 {
		parts = append(parts, strconv.Itoa(charge.TokensOut)+" out")
	}
	if charge.CostUSD > 0 {
		parts = append(parts, carriedDollars(charge.CostUSD))
	}
	v.say("  %s", strings.Join(parts, " · "))
}

// end says how the run ended and answers its rung on the exit ladder. took is
// how long the program's process ran.
func (v *carriedView) end(result delegate.Result, runErr error, limited bool, spent float64, took time.Duration) error {
	terminal, calls := v.ending()
	name := v.inv.Program.Name
	if terminal == nil && result.ExitCode < 0 && !result.Stopped && runErr != nil && !errors.Is(runErr, delegate.ErrNoTerminal) {
		// IT NEVER RAN: the process could not be started at all, which is the
		// first rung of the ladder rather than work that did not finish.
		fmt.Fprintln(os.Stderr, "error:", runErr)
		return exitCannotRun
	}
	status := delegate.StatusCrashed
	if terminal != nil {
		status = terminal.Status
		if !delegate.KnownStatus(status) {
			status = delegate.StatusCrashed
		}
	}
	if limited {
		status = delegate.StatusBudget
	}
	if v.records != nil {
		// THE RECORDS ARE THE PROGRAM'S, so where its folder was left goes to
		// stderr beside them rather than into them.
		if said := v.folderLine(); said != "" {
			fmt.Fprintln(carriedStderr, said)
		}
		return carriedExit(status)
	}
	switch {
	case limited:
		line := name + " was stopped at a limit you set"
		if terminal != nil && terminal.Message != "" {
			line += ": " + terminal.Message
		}
		v.say("%s", line)
	case terminal == nil:
		line := fmt.Sprintf("%s exited %d without saying how it ended", name, result.ExitCode)
		if result.Reading.LastStage != "" {
			line += "; its last stage was " + result.Reading.LastStage
		}
		v.say("%s", line)
	default:
		v.say("%s", carriedEnding(name, *terminal))
		if claim := terminal.Claim(); claim != "" {
			v.say("  %s's model said: %s", name, claim)
		}
		if observed := terminal.Observed(); observed != "" {
			v.say("  %s observed: %s", name, observed)
		}
	}
	// WHERE THE WORK IS comes after how the run ended: its branch, checked out
	// in the folder, and how to go back — the sentence a conversation's run
	// says on its page.
	if said := v.folderLine(); said != "" {
		v.say("  %s", said)
	}
	// The folder the run's record is in comes before the last line, so that
	// line is always what the run came to.
	if _, err := os.Stat(v.record); err == nil {
		v.say("  the run's record is in %s", v.record)
	}
	// THE LAST LINE IS WHAT THE RUN CAME TO: its calls, its dollars and how
	// long the program ran, each left off rather than written as a zero. It is
	// last because the manual says so and a person reading `tail -1` is told
	// so; the record folder's line, which every real run has, used to follow it.
	var summary []string
	if calls > 0 {
		word := "calls"
		if calls == 1 {
			word = "call"
		}
		summary = append(summary, fmt.Sprintf("%d model %s", calls, word))
		if spent > 0 {
			summary = append(summary, carriedDollars(spent))
		}
	}
	if took >= time.Second {
		summary = append(summary, reltime.Elapsed(took))
	}
	if len(summary) > 0 {
		v.say("  %s", strings.Join(summary, " · "))
	}
	return carriedExit(status)
}

// folderLine is how the run left its folder, "" for a program that edits no
// files.
func (v *carriedView) folderLine() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.leftFolder
}

// carriedEnding is the ending in one sentence, in the program's own words
// after the one that says which of the four it was.
func carriedEnding(name string, terminal delegate.Terminal) string {
	message := strings.TrimSpace(terminal.Message)
	var said string
	switch terminal.Status {
	case delegate.StatusPass:
		said = name + " finished"
	case delegate.StatusBudget:
		said = name + " stopped at its ceiling"
	case delegate.StatusFail:
		said = name + " did not finish"
	default:
		said = name + " crashed"
	}
	if message == "" {
		return said
	}
	return said + ": " + message
}

func (v *carriedView) say(format string, args ...any) {
	v.mu.Lock()
	defer v.mu.Unlock()
	fmt.Fprintf(v.out, format+"\n", args...)
}

// carriedDollars writes an amount in cents, and to four places under a cent
// so one cheap call is not written as nothing.
func carriedDollars(amount float64) string {
	if amount < 0.01 {
		return fmt.Sprintf("$%.4f", amount)
	}
	return fmt.Sprintf("$%.2f", amount)
}

// firstLineOf is the first line of a text, trimmed and cut to a row's width.
func firstLineOf(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	line = strings.TrimSpace(line)
	if runes := []rune(line); len(runes) > 100 {
		line = string(runes[:100]) + "…"
	}
	return line
}

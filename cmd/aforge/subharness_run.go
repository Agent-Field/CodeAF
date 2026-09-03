package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/substore"
)

// `aforge run <program> --input <file.json|->` is one program, run once, with
// nobody watching. It was `aforge run subharness <name>` until `run` stopped
// meaning two things, and that spelling still works for one release
// (rename.go).
//
// It is the third of the three doors docs/SUBHARNESS-PRD.md §9 names, and it is
// the one with NO TASK SURFACE AT ALL — no roster row, no room, no intake card,
// no ✕. The PRD says why in one sentence: the task surface is a PRESENTATION of
// a run, not its definition, so a headless door that reached for it would make
// the presentation part of the definition and there would stop being a way to
// run a program without a conversation around it. Nothing in this file or in
// subharness_env.go beside it touches internal/session.
//
// A subharness is a FUNCTION (PRD §4), so this command is shaped like one: typed
// input in, typed output on stdout, and an ending that a script can read off the
// exit code without parsing a word of prose.
//
//	exit 0   it finished, and the output above is the shape it promised
//	exit 1   it could not be made to happen at all
//	exit 2   it ran and did not finish, and stderr says what ran out
//
// THOSE THREE NUMBERS ARE NOT THIS FILE'S TO CHOOSE. They are three rungs of
// the one exit ladder every headless verb in this binary leaves on, and the
// table is in envelope.go — this command says WHY it ended, as a [stopReason],
// and the ladder says what that costs. It used to write its own numbers here,
// and `do` wrote different ones for the same two facts.
//
// THE ENDINGS ARE STILL DECIDED IN EXACTLY ONE PLACE, [reportSubharnessRun]. A
// second reading of "done" anywhere in this file would be a second program
// disagreeing with this one about what happened.
//
// The vocabulary law reaches every line either stream carries: work is running,
// finishing, done, incomplete, or needs your look. A run that did not finish is
// INCOMPLETE and is never called a failure — not on stderr, not in the usage
// text, not in the flag help.

// runSubharnessCommand is the thin outer half: read the flags, build the clients
// and the registry, and hand the whole of the decision to [runSubharness].
//
// The split is deliberate and is what makes the endings testable. Everything
// above the seam needs a provider key, a workspace and a network; everything
// below it needs a registry, some bytes, and two writers.
func runSubharnessCommand(args []string) error {
	flags := commandFlags("run")
	input := flags.String("input", "",
		`the typed input, as a JSON file — "-" reads it from what is piped in`)
	workspace := flags.String("dir", "", "the directory to work in, edited in place (default: the current directory)")
	shorthandFlag(flags, "w", "dir")
	model := flags.String("model", "", modelFlagHelp)
	journalPath := flags.String("journal", "",
		"keep an account of every call this run makes in this file, one JSON object per line")
	asJSON := flags.Bool("json", false, jsonFlagHelp)
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	noteRenamedFlags(flags)
	rest := flags.Args()
	if len(rest) < 1 {
		return fmt.Errorf("usage: aforge run <program> --input <file.json|->")
	}
	name := strings.TrimSpace(rest[0])
	// The input is read before anything is built, because a run with no input is
	// a program handed nothing and there is no sense in opening a provider
	// connection to discover it.
	material, err := readSubharnessInput(*input, os.Stdin)
	if err != nil {
		return err
	}

	settings, err := config.Load()
	if err != nil {
		return err
	}
	// One seat here — a saved program executes and never plans — climbed on the
	// same ladder every other headless door climbs, so a profile's crew reaches
	// this one too (config.ResolveSeats).
	seats := config.ResolveSeats(settings.ProfileDir, *model, "")
	applySeats(&settings, seats)
	fmt.Fprintln(os.Stderr, seats.Work.Report())
	// The measured ruler is seated for the same reason `run` seats it: this is a
	// surface that will record what a worker cost, and a history keyed on
	// anything but the model is two histories for one executor.
	installMeasuredRulers(settings, settings.Model)
	modelCatalog := sharedCatalog(settings)
	settings.Models = modelCatalog
	client, err := settings.Client()
	if err != nil {
		return err
	}
	defer closeRouter(client)

	// The directory this program works in, resolved exactly the way `aforge do`
	// resolves its own: the place it was pointed at is the work, edited in place,
	// and saying nothing means the current directory. A one-shot that filed its
	// results into a freshly created subdirectory would write where nobody looks.
	root, err := errandWorkspace(*workspace)
	if err != nil {
		return err
	}
	space, err := exec.NewWorkspace(root)
	if err != nil {
		return err
	}
	web := exec.NewWeb()
	// The toolbox is opened HERE, above the registry, rather than beside the env
	// below: it is what a bundle's tool guard is checked against, and the guards
	// belong to runners the registry is about to build. It is the same object
	// [newHeadlessEnv] arms the whitelist on a moment later, which is what keeps
	// the guard and the call agreeing about what this build has.
	tools := exec.NewToolbox(space, name, web)
	// The turn, token and deadline ceilings are left at zero on purpose, which is
	// how this file states them without restating them: internal/exec owns every
	// one of those numbers and applies its own when it is handed nothing. A
	// second spelling of a budget here would be a number that drifts.
	linear := exec.NewLinear(client, space, web, 0, 0, 0).
		WithAttribution(settings.Attribution).
		WithContextLength(modelCatalog.ContextLength(settings.Model))
	registry := exec.NewRegistry(linear)
	registerLeafExecutors(registry, leafBuild{
		settings: settings, client: client, workspace: space, web: web,
		model: settings.Model, models: modelCatalog,
	})
	// THE SEAM, FILLED. The bundles a person wrote and the bundles a repository
	// carries are found here and nowhere else in this command. The lookup ORDER
	// is decided in the [exec.Layer] constants and not here, which is why this is
	// two registrations and not an edit to any lookup — including the packed
	// trailer's, whenever that phase lands.
	//
	// The look every bundle's guards are checked through is this command's own:
	// the workspace it was pointed at, and the bare toolbox above, which is the
	// only belt a run with no session has. It is filled immediately, because
	// unlike the conversation's belt this one already exists.
	//
	// A store that cannot be read registers a source that lists nothing, so
	// nothing here fails a launch over a directory.
	headlessBelt := &beltWatch{}
	headlessBelt.watch(toolboxBelt(tools))
	build := subharnessBuild(bundleLook{workspace: space.Root(), belt: headlessBelt.on})
	store := substore.Home()
	registry.UseBundles(exec.LayerHome, store.Source(build))
	// And the repository's own, when the directory this run works in really is
	// one. [substore.ProjectDir] is a name and nothing else — what is in it is
	// whatever a `git pull` left there — so whether there is a project at all is
	// asked with the one answer this binary already gives ([v3GitRoot]).
	if root, ok := v3GitRoot(space.Root()); ok {
		registry.UseBundles(exec.LayerProject, substore.At(substore.ProjectDir(root)).Source(build))
	}

	journal := &runJournal{}
	if path := strings.TrimSpace(*journalPath); path != "" {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		defer file.Close()
		journal.sink = file
	}

	policy, err := v3Policy(space.Root(), settings.ProfileDir, false)
	if err != nil {
		return err
	}
	// The run's own reasoning economy, layered the way every executing surface
	// layers it: the planning level is wrong for work, and a model with reasoning
	// suppressed stops writing anything down.
	ctx := settings.ExecContext(settings.Context(context.Background(), name))
	return runSubharness(ctx, subharnessRun{
		registry: registry, name: name, input: material, journal: journal,
		stdout: os.Stdout, stderr: os.Stderr,
		asJSON: *asJSON, model: settings.Model, started: time.Now(),
		env: func(manifest exec.Manifest) exec.Env {
			return newHeadlessEnv(client, tools, *policy, manifest, journal, os.Stderr)
		},
		// WHAT HAPPENED HERE IS WRITTEN DOWN WHERE THE CONVERSATION WILL READ IT.
		// The note goes beside the bundle in the same store the chat surface
		// records into (chatv3_subharness.go), because "when did this last run"
		// is a question about the MACHINE and not about which door was used —
		// and a headless run that left no note would make `/subharness` say a
		// program has never run when it ran this morning.
		record: subharnessRunRecorder(store),
	})
}

// subharnessRun is one headless invocation, with its streams named so a test
// drives the whole of the decision rather than a piece of it.
type subharnessRun struct {
	registry *exec.Registry
	name     string
	input    json.RawMessage
	// env is built once the manifest is known, because the whitelist a tool call
	// is filtered against is that manifest's and there is no manifest before the
	// name has been resolved.
	env func(exec.Manifest) exec.Env
	// journal is this run's account of itself and its ledger both. Nil is a run
	// nobody is keeping an account of, which is a real case rather than an error.
	journal *runJournal
	stdout  io.Writer
	stderr  io.Writer
	// asJSON prints the one result envelope every headless verb returns
	// (envelope.go) instead of the report and the typed output, and prints it
	// EVEN WHEN THE RUN FAILED — a machine contract that only holds on success
	// is not one a script can be written against.
	asJSON bool
	// model is the seat this run sat in, and started is when it began. Both are
	// facts the envelope carries and neither is otherwise this file's business;
	// a test driving the endings leaves them empty and the envelope then says
	// nothing about them rather than guessing.
	model   string
	started time.Time
	// record is told how the run went, once, the moment it lands. Nil is a build
	// that keeps no history — a test driving the endings, a store that could not
	// be opened — and a run then simply leaves no note, which is not an error and
	// draws nothing anywhere.
	record func(name string, note substore.RunNote)
}

// runSubharness resolves the name, runs the program, and lands the run on one of
// the three endings.
//
// THE DEOPT IS AN ENDING TOO, and it is reached from two different facts that
// mean the same thing to the person: a run that COULD NOT BE MADE TO HAPPEN
// (internal/exec/runner.go: an error is a broken bundle, a dead context) and a
// run that ASKED to be handled the long way (a guard that did not pass). Both
// hand the ORIGINAL INPUT, unchanged, to the generalist — the worker the person
// would otherwise have had — because they did not ask for a program, they asked
// for the work.
func runSubharness(ctx context.Context, run subharnessRun) error {
	runner, err := run.registry.Subharness(run.name)
	if err != nil {
		if errors.Is(err, exec.ErrNoSubharness) {
			// A PERSON TYPED THIS NAME, so a typo is told rather than served.
			// [exec.Registry.For] degrades an unknown worker to the generalist and
			// is right to — a leaf still has work to get done — but somebody who
			// spelled a name and silently received something else is worse served
			// than somebody who was told.
			return run.sayFailedEnvelope(noSuchSubharness(run.registry, run.name))
		}
		return run.sayFailedEnvelope(err)
	}
	manifest := runner.Manifest()
	env := exec.Env(exec.UnwiredEnv{})
	if run.env != nil {
		env = run.env(manifest)
	}

	result, runErr := runner.Run(ctx, run.input, env)
	if runErr != nil || exec.FellBack(result) {
		because := strings.TrimSpace(result.FellBack)
		if runErr != nil {
			because = runErr.Error()
		}
		// The one sentence, written once, in the one place both surfaces read it
		// from. Nothing here calls this a failure or a fallback where a person can
		// see it: the step needed a closer look and it was handled the long way —
		// or, where the long way would reach past the ceiling this program was
		// approved under, it says that instead and the run stops there
		// ([exec.DeoptHeld] carries the argument). Asked before anything is
		// announced, so stderr and the journal say what actually happens.
		line := exec.DeoptLineFor(manifest, because)
		fmt.Fprintln(run.stderr, line)
		_ = exec.Record(run.journal, exec.JournalEntry{At: time.Now(), Call: exec.CallLog, Note: line})
		result, err = exec.Deopt(ctx, run.registry, manifest, run.input, env, because)
		if err != nil {
			// There is no fourth worker under the generalist. This is the "could
			// not be made to happen" ending, and it leaves through main's own
			// default with the sentence attached.
			//
			// A RUN THAT COULD NOT BE MADE TO HAPPEN IS STILL A RUN THAT WAS
			// TRIED, and it is written down as unfinished with the reason it
			// carried. A note that stayed silent about it would send somebody
			// back to try the same broken program again tomorrow.
			run.note(result, err)
			return run.sayFailedEnvelope(err)
		}
	}
	run.note(result, nil)
	return reportSubharnessRun(run, result)
}

// note tells the store how this run went, in the facts and never in a sentence:
// [substore.RunNote] renders nothing, and how a "when", a "cost" and a run that
// did not finish are drawn belongs to whichever surface is drawing them.
//
// WHAT IT COST IS ASKED OF THE ONE THING THAT CAN SAY, and the order is the same
// one the conversation's side takes: a runner that journals its own host calls
// sets [exec.RunResult.Spend] to the sum of them, so adding this command's
// journal to that would be counting one run twice. A runner that reports nothing
// — the fronted leaf workers, which spend through their own clients — is measured
// by the journal instead, which for those is honestly empty.
func (run subharnessRun) note(result exec.RunResult, runErr error) {
	if run.record == nil {
		return
	}
	spend := result.Spend
	if !spend.Reported() {
		spend = run.journal.Ledger()
	}
	note := substore.RunNote{
		At: time.Now(), Finished: result.Finished(),
		Why: result.Incomplete, CostUSD: spend.CostUSD,
	}
	if runErr != nil {
		note.Finished, note.Why = false, runErr.Error()
	}
	run.record(run.name, note)
}

// reportSubharnessRun writes what happened and decides what the process leaves
// with. It is the WHOLE of the endings table and the only reader of
// [exec.RunResult.Finished] in this command.
//
// stdout carries the account and then the typed output, and nothing else, so a
// caller can read the answer off the last line. Everything a person watches — the
// progress, the files, the ledger, the reason a run did not finish — goes to
// stderr, which is where a run's own words have gone since `aforge do`.
func reportSubharnessRun(run subharnessRun, result exec.RunResult) error {
	sayArtifacts(run.stderr, result.Artifacts)
	sayLedger(run.stderr, run.journal.Ledger())
	if result.Finished() {
		if run.asJSON {
			return run.sayEnvelope(stopDone, result, "")
		}
		if report := strings.TrimSpace(result.Report); report != "" {
			fmt.Fprintln(run.stdout, report)
		}
		fmt.Fprintln(run.stdout, string(compactJSON(result.Output)))
		return nil
	}
	// IT RAN AND IT DID NOT FINISH. "incomplete" is one of the five sanctioned
	// words for the state of work and nothing may reach a person calling this a
	// failure — out of budget, out of time, a question nobody was here to answer
	// and a step that could not produce its shape all land here, and all of them
	// say what ran out.
	reason := strings.TrimSpace(result.Incomplete)
	if reason == "" {
		// Finished() is false and nothing said why: the run produced no output and
		// left no reason. Saying that plainly is better than a silent exit 2.
		reason = "it stopped without producing what it promised, and without saying why"
	}
	fmt.Fprintln(run.stderr, reason)
	if run.asJSON {
		return run.sayEnvelope(stopIncomplete, result, reason)
	}
	// What the run DID manage still goes to stdout where there is any of it. A
	// partial answer is the thing the incomplete rung exists to describe.
	if report := strings.TrimSpace(result.Report); report != "" {
		fmt.Fprintln(run.stdout, report)
	}
	if len(bytes.TrimSpace(result.Output)) > 0 && string(result.Output) != "null" {
		fmt.Fprintln(run.stdout, string(compactJSON(result.Output)))
	}
	return exitFor(stopIncomplete)
}

// sayEnvelope writes the one machine contract (envelope.go) and returns what
// the process leaves with. It is this command's ONLY mapping between a saved
// program's result and what a caller reads, which is what stops `run` from
// growing a third `--json` shape.
//
// `answer` is the run's own account of what it did, because that is what
// `answer` is on the other two verbs; a program that wrote no account carries
// its typed output there instead, so `jq -r .answer` is never empty on a run
// that produced something. The typed output is ALWAYS in `output`, whole and
// unflattened, which is what a script actually wants from a function.
func (run subharnessRun) sayEnvelope(stop stopReason, result exec.RunResult, incomplete string) error {
	files := make([]string, 0, len(result.Artifacts))
	for _, artifact := range result.Artifacts {
		files = append(files, artifact.Path)
	}
	// WHAT IT COST IS ASKED OF THE ONE THING THAT CAN SAY, in the same order
	// [subharnessRun.note] asks it: a runner that journals its own calls has
	// already summed them, and adding this command's journal to that would be
	// counting one run twice.
	spend := result.Spend
	if !spend.Reported() {
		spend = run.journal.Ledger()
	}
	answer := strings.TrimSpace(result.Report)
	output := bytes.TrimSpace(result.Output)
	if len(output) > 0 && string(output) != "null" {
		if answer == "" {
			answer = string(compactJSON(result.Output))
		}
	} else {
		output = nil
	}
	extra := map[string]any{}
	if len(output) > 0 {
		extra["output"] = json.RawMessage(compactJSON(result.Output))
	}
	if report := strings.TrimSpace(result.Report); report != "" {
		extra["report"] = report
	}
	// The reason it did not finish, in the same words stderr just carried. It is
	// NOT `error`: `error` means the run never produced an answer at all, and a
	// run that got part of the way did.
	if incomplete != "" {
		extra[envelopeIncomplete] = incomplete
	}
	var seconds float64
	if !run.started.IsZero() {
		seconds = time.Since(run.started).Seconds()
	}
	envelope := buildResultEnvelope(runResult{
		Stop: stop, Answer: answer, Files: files,
		SpendUSD: spend.CostUSD, TokensIn: spend.Input, TokensOut: spend.Output,
		Seconds: seconds, Model: run.model,
		// A saved program is one call to one function and does not count steps
		// the way `do` counts nodes or `exec` counts turns. Nothing renders as
		// nothing everywhere a person reads; this is a machine contract, where
		// an absent key is indistinguishable from an older binary, so the field
		// is present and honest at 0.
		Steps: 0,
		Extra: extra,
	})
	encoded, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(run.stdout, string(encoded))
	if code := exitFor(stop); code != exitDone {
		return code
	}
	return nil
}

// sayFailedEnvelope is the same contract for a run that COULD NOT BE MADE TO
// HAPPEN — a name that is not a program, a bundle that would not load, a
// generalist that could not be reached. Under --json the object is printed
// anyway and the sentence goes in `error`, because a caller reading stdout must
// never have to tell a crashed process apart from a failed run by the emptiness
// of the stream.
func (run subharnessRun) sayFailedEnvelope(err error) error {
	if !run.asJSON || err == nil {
		return err
	}
	var seconds float64
	if !run.started.IsZero() {
		seconds = time.Since(run.started).Seconds()
	}
	spend := run.journal.Ledger()
	envelope := buildResultEnvelope(runResult{
		Stop: stopError, Error: plainWords(err.Error()),
		SpendUSD: spend.CostUSD, TokensIn: spend.Input, TokensOut: spend.Output,
		Seconds: seconds, Model: run.model,
	})
	encoded, marshalErr := json.MarshalIndent(envelope, "", "  ")
	if marshalErr != nil {
		return err
	}
	fmt.Fprintln(run.stdout, string(encoded))
	// The sentence still reaches stderr through main's own door, so a person
	// watching and a script parsing are told the same thing.
	fmt.Fprintln(run.stderr, "error:", plainWords(err.Error()))
	return exitFor(stopError)
}

// noSuchSubharness is what somebody who mistyped a name is told: the name they
// typed, and — where this build has any to offer — what there is.
//
// `linear` is left off the list on purpose and by the same law the `/subharness`
// picker follows: the generalist is what you get when you pick nothing, not
// something you pick. It still RESOLVES by name, which is why the lookup above
// reaches it and this list does not.
func noSuchSubharness(registry *exec.Registry, name string) error {
	var names []string
	for _, manifest := range registry.Manifests() {
		if manifest.Name == exec.LinearSubharness {
			continue
		}
		names = append(names, manifest.Name)
	}
	if len(names) == 0 {
		return fmt.Errorf("there is no subharness called %q, and this build has none to offer", name)
	}
	return fmt.Errorf("there is no subharness called %q — this build has: %s", name, strings.Join(names, ", "))
}

// sayArtifacts lists the files a run left, where it left any. Nothing renders as
// nothing: a run that wrote no files says nothing about files.
func sayArtifacts(stderr io.Writer, artifacts []exec.Artifact) {
	if stderr == nil || len(artifacts) == 0 {
		return
	}
	fmt.Fprintln(stderr, "files:")
	for _, artifact := range artifacts {
		line := "  " + artifact.Path
		if note := strings.TrimSpace(artifact.Note); note != "" {
			line += "  " + note
		}
		fmt.Fprintln(stderr, line)
	}
}

// sayLedger prints what the run cost, and prints nothing at all when nobody
// said.
//
// THE EMPTINESS LAW REACHES EVERY FIGURE ON THIS LINE. Zero calls is not a line
// worth drawing, and a provider that published no usage left zero dollars behind
// — which is "nobody said", not "free" — so an unreported run draws nothing
// rather than $0.00. The figures are the journal's own sum and there is no
// counter beside it.
func sayLedger(stderr io.Writer, spend exec.Spend) {
	if stderr == nil || !spend.Reported() {
		return
	}
	var parts []string
	if spend.Calls > 0 {
		parts = append(parts, plural(spend.Calls, "call"))
	}
	if spend.Input > 0 || spend.Output > 0 {
		tokens := fmt.Sprintf("%d in / %d out", spend.Input, spend.Output)
		if spend.CacheRead > 0 {
			tokens = fmt.Sprintf("%d in (%d cached) / %d out", spend.Input, spend.CacheRead, spend.Output)
		}
		parts = append(parts, tokens)
	}
	if spend.CostUSD > 0 {
		parts = append(parts, fmt.Sprintf("$%.4f", spend.CostUSD))
	}
	if model := strings.TrimSpace(spend.Model); model != "" {
		parts = append(parts, model)
	}
	if len(parts) == 0 {
		return
	}
	fmt.Fprintln(stderr, strings.Join(parts, " · "))
}

// readSubharnessInput reads the typed input a run was handed.
//
// IT IS REQUIRED, and that is the honest shape rather than a strictness: a
// subharness is a function, and a function called with nothing has been handed
// nothing to do. `-` reads what was piped in, which is what `-` means everywhere
// else a command in this tree takes a file.
func readSubharnessInput(path string, stdin io.Reader) (json.RawMessage, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf(
			"this run needs its input: --input <file.json>, or --input - to read it from what is piped in")
	}
	var raw []byte
	var err error
	if path == "-" {
		raw, err = io.ReadAll(stdin)
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("the input is empty — there is nothing here for the run to do")
	}
	var material json.RawMessage
	if err := json.Unmarshal(raw, &material); err != nil {
		return nil, fmt.Errorf("the input is not JSON: %w", err)
	}
	return material, nil
}

// compactJSON puts the typed output on one line, so that a caller reading the
// last line of stdout has the whole answer. Bytes that will not compact are
// written exactly as the run produced them: this is a presentation choice, and a
// presentation choice may not change what a run said.
func compactJSON(raw json.RawMessage) []byte {
	var flat bytes.Buffer
	if err := json.Compact(&flat, raw); err != nil {
		return raw
	}
	return flat.Bytes()
}

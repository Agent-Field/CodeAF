package session

// automation_run.go is what one run of an automation does, as
// [automation.Runner]: a watch's look and its judgment, and a piece of work run
// unattended. docs/design/automations/DESIGN.md, "A run".
//
// A RUN IS THE PERSON'S OWN CONVERSATION, DONE LATER. Its configuration is built
// by the same assembly a conversation's is (the door hands it in), so it has
// their models, keys, connected accounts, memory and approval rules. The
// standing firing this replaces built a posture of its own, with no accounts
// and an allow-everything policy, and every difference between the two was a
// way for "it worked when I tried it" to fail at three in the morning.
//
// NOBODY IS THERE TO ASK. A call the person's rules would ask about is refused,
// and the FIRST such refusal stops the run as `your call`, naming the call — a
// run that carried on past it would be guessing what the person would say.
//
// AN OUTCOME IS REPORTED, NEVER INFERRED. Work ends by calling
// `automation_report` with `done` or `incomplete`. A run that ends without a
// report is incomplete, whatever it said on its way out; a run whose context
// ended is recorded by the clock from the context's cause.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/processgroup"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// The bounds of one look. A look is a check, not work: it gets a minute, and
// what it saw is clipped from the tail — where a command puts what went wrong.
const (
	automationLookWindow = 60 * time.Second
	automationSightClip  = 8 * 1024
)

// The bounds of one files look, which are its whole point: an unbounded scan of
// a home folder is a hang, and a reading that varies with how much it happened
// to read is worse than none.
const (
	automationFilesMax     = 2000
	automationFileHashCap  = 256 << 10
	automationFilesBudget  = 3 * time.Second
	automationChangesShown = 40
)

// AutomationRun is what a session running one run of an automation's work
// carries ([Config.AutomationRun]): the door its report goes through.
type AutomationRun struct {
	mu       sync.Mutex
	reported bool
	status   string
	summary  string
}

// report records the run's own account of how it went. The last report wins.
func (r *AutomationRun) report(status, summary string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reported, r.status, r.summary = true, status, summary
}

// result is what the run reported, and whether it did.
func (r *AutomationRun) result() (status, summary string, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status, r.summary, r.reported
}

// automationReportTools is `automation_report`, on the belt of a run and of
// nothing else.
func (a *Agent) automationReportTools() []bare.Tool {
	run := a.config.AutomationRun
	if run == nil {
		return nil
	}
	return []bare.Tool{{
		Name:        "automation_report",
		Description: "Say how this automation's run went. Call it ONCE, as your last act: status `done` when the work is finished, `incomplete` when it is not and cannot be, with a short summary of what you did and found — the person reads it as the run's result.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"status":{"type":"string","enum":["done","incomplete"]},"summary":{"type":"string","description":"What you did and what you found, in a few sentences."}},"required":["status","summary"]}`),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Status  string `json:"status"`
				Summary string `json:"summary"`
			}
			if err := decodeToolArguments(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			status := strings.ToLower(strings.TrimSpace(parsed.Status))
			if status != "done" && status != "incomplete" {
				return "Invalid arguments: status is done or incomplete", true, nil
			}
			run.report(status, strings.TrimSpace(parsed.Summary))
			return "recorded. End your turn now, with no further calls.", false, nil
		},
	}}
}

// NewAutomationRunner is the [automation.Runner] the clock runs with. base
// builds the person's own configuration for a workspace — the same assembly a
// conversation there gets — and root is the automations store's folder, under
// which each run of work keeps its session folder.
func NewAutomationRunner(base func(workspace string) (Config, error), root string) automation.Runner {
	return &automationRunner{base: base, root: root, clients: map[string]Completer{}}
}

type automationRunner struct {
	base    func(workspace string) (Config, error)
	root    string
	mu      sync.Mutex
	clients map[string]Completer
	// child builds the session one run of work runs in. It is [New] in every
	// real build and a field so a test can script the model behind it.
	child func(Config) (*Agent, error)
}

// newChild is the runner's one way to make a run's session.
func (r *automationRunner) newChild(cfg Config) (*Agent, error) {
	if r.child != nil {
		return r.child(cfg)
	}
	return New(cfg)
}

// ── the look ────────────────────────────────────────────────────────────────

func (r *automationRunner) Look(ctx context.Context, item automation.Automation) (automation.Sight, error) {
	look := item.Look
	if look == nil {
		return automation.Sight{}, errors.New("this automation has nothing to look at")
	}
	ctx, cancel := context.WithTimeout(ctx, automationLookWindow)
	defer cancel()
	switch {
	case strings.TrimSpace(look.Command) != "":
		return automationLookCommand(ctx, item.Workspace, look.Command)
	case strings.TrimSpace(look.Files) != "":
		return automationLookFiles(item.Workspace, look.Files, item.Memo)
	}
	return r.lookTool(ctx, item)
}

// automationLookCommand runs the command the way the bash tool does — the same
// shell, its own process group killed on the deadline — with the provider keys
// stripped from its environment (bare.StreamingEnv), which the standing probe
// it replaces did not do. A failing command is evidence, not an error: "is CI
// red" is often answered by an exit status.
func automationLookCommand(ctx context.Context, workspace, command string) (automation.Sight, error) {
	shell, args := jobShell()
	process := exec.CommandContext(ctx, shell, append(args, command)...)
	process.Dir = workspace
	process.Env = bare.StreamingEnv()
	processgroup.ConfigureDetached(process)
	process.Cancel = func() error {
		_ = processgroup.Kill(process.Process.Pid)
		return nil
	}
	process.WaitDelay = 2 * time.Second
	output, err := process.CombinedOutput()
	seen := string(output)
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		seen += fmt.Sprintf("\n(the look timed out after %s)", automationLookWindow)
	case err != nil:
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			seen += fmt.Sprintf("\n(exit status %d)", exit.ExitCode())
		} else {
			return automation.Sight{}, fmt.Errorf("could not run the command: %w", err)
		}
	}
	return automation.Sight{Text: automationTail(seen, automationSightClip)}, nil
}

// automationLookFiles reads the glob's files — names, sizes and a digest of
// their contents — and says what changed since the last decided look. The
// listing it keeps is the memo; with none, this is the first look, and it says
// so rather than reporting every file as new.
func automationLookFiles(workspace, glob, memo string) (automation.Sight, error) {
	now, truncated, err := automationListing(workspace, glob)
	if err != nil {
		return automation.Sight{}, err
	}
	before := automationParseListing(memo)
	var text strings.Builder
	fmt.Fprintf(&text, "%d file(s) match %s", len(now), glob)
	if truncated {
		text.WriteString(" (more than this look reads; the rest were not compared)")
	}
	text.WriteByte('\n')
	if memo == "" {
		text.WriteString("This is the first look: there is nothing earlier to compare with.\n")
	} else {
		var changes []string
		for name, digest := range now {
			was, ok := before[name]
			switch {
			case !ok:
				changes = append(changes, "+ "+name+" (new)")
			case was != digest:
				changes = append(changes, "~ "+name+" (changed)")
			}
		}
		for name := range before {
			if _, ok := now[name]; !ok {
				changes = append(changes, "- "+name+" (gone)")
			}
		}
		sort.Strings(changes)
		if len(changes) == 0 {
			text.WriteString("Nothing changed since the last look.\n")
		} else {
			fmt.Fprintf(&text, "%d change(s) since the last look:\n", len(changes))
			for i, change := range changes {
				if i == automationChangesShown {
					fmt.Fprintf(&text, "… and %d more\n", len(changes)-i)
					break
				}
				text.WriteString(change + "\n")
			}
		}
	}
	return automation.Sight{Text: automationTail(text.String(), automationSightClip), Memo: automationFormatListing(now)}, nil
}

// automationListing walks the workspace for paths matching glob (where ** spans
// folders), bounded in count, size read and time.
func automationListing(workspace, glob string) (map[string]string, bool, error) {
	glob = filepath.ToSlash(strings.TrimSpace(glob))
	if glob == "" {
		return nil, false, errors.New("an empty glob")
	}
	if strings.HasPrefix(glob, "/") || strings.Contains(glob, "../") {
		return nil, false, errors.New("the glob must stay inside the project")
	}
	listing := map[string]string{}
	deadline := time.Now().Add(automationFilesBudget)
	truncated := false
	err := filepath.WalkDir(workspace, func(full string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(workspace, full)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !automationGlobMatch(glob, rel) {
			return nil
		}
		if len(listing) >= automationFilesMax || time.Now().After(deadline) {
			truncated = true
			return filepath.SkipAll
		}
		listing[rel] = automationFileDigest(full)
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return listing, truncated, nil
}

// automationGlobMatch matches a slash-separated path against a glob whose **
// spans any number of folders, and whose other parts are path.Match patterns.
func automationGlobMatch(glob, name string) bool {
	return globParts(strings.Split(glob, "/"), strings.Split(name, "/"))
}

func globParts(pattern, parts []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			for skip := 0; skip <= len(parts); skip++ {
				if globParts(pattern[1:], parts[skip:]) {
					return true
				}
			}
			return false
		}
		if len(parts) == 0 {
			return false
		}
		if ok, err := path.Match(pattern[0], parts[0]); err != nil || !ok {
			return false
		}
		pattern, parts = pattern[1:], parts[1:]
	}
	return len(parts) == 0
}

// automationFileDigest is a file's size and a digest of its first bytes.
func automationFileDigest(full string) string {
	file, err := os.Open(full)
	if err != nil {
		return "unreadable"
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "unreadable"
	}
	sum := sha256.New()
	_, _ = io.Copy(sum, io.LimitReader(file, automationFileHashCap))
	return strconv.FormatInt(info.Size(), 10) + ":" + hex.EncodeToString(sum.Sum(nil))[:16]
}

func automationFormatListing(listing map[string]string) string {
	names := make([]string, 0, len(listing))
	for name := range listing {
		names = append(names, name)
	}
	sort.Strings(names)
	var out strings.Builder
	for _, name := range names {
		out.WriteString(name + "\t" + listing[name] + "\n")
	}
	return out.String()
}

func automationParseListing(memo string) map[string]string {
	listing := map[string]string{}
	for _, line := range strings.Split(memo, "\n") {
		name, digest, ok := strings.Cut(line, "\t")
		if ok && name != "" {
			listing[name] = digest
		}
	}
	return listing
}

// lookTool calls one tool through the same chokepoint a turn's call goes
// through — the person's approval rules included, with nobody to ask. A
// connected account's tools are only on the belt once the account is put to
// use, so the look puts it to use first.
func (r *automationRunner) lookTool(ctx context.Context, item automation.Automation) (automation.Sight, error) {
	cfg, err := r.base(item.Workspace)
	if err != nil {
		return automation.Sight{}, err
	}
	cfg.Workspace = item.Workspace
	cfg.Place = Place{}
	cfg.SessionFile = ""
	cfg.AskConsent = false
	cfg.InTask = true
	cfg.Automations = nil
	cfg.Memory = nil
	cfg.automationID = item.ID
	agent := &Agent{config: cfg, model: cfg.Model, id: NewSessionID()}
	agent.jobs = newJobRegistry(cfg.Workspace, cfg.droppingsPlace(), agent.enqueueJobNote, agent.enqueueWatchNote)
	agent.connect = newConnectHub(cfg)
	agent.tools = agent.belt()
	defer agent.jobs.shutdown(jobShutdownGrace)
	call := func(name string, args json.RawMessage) toolResult {
		tc := ai.ToolCall{ID: "automation-look", Type: "function", Function: ai.ToolCallFunction{Name: name, Arguments: string(args)}}
		return agent.executeTool(ctx, agent.newEpisode(), nil, tc, argsText(tc))
	}
	if service := strings.TrimSpace(item.Look.Service); service != "" {
		armArgs, _ := json.Marshal(map[string]string{"service": service, "tools": item.Look.Tool})
		armed := call("use_service", armArgs)
		if armed.isError {
			return automation.Sight{}, fmt.Errorf("could not use %s: %s", service, firstLineOf(armed.text))
		}
		agent.tools = agent.belt()
	}
	args := item.Look.Args
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	result := call(strings.TrimSpace(item.Look.Tool), args)
	if result.isError && automationNeedsPerson(result.text) {
		return automation.Sight{}, errors.New("needed your ok to use " + item.Look.Tool)
	}
	return automation.Sight{Text: automationTail(result.text, automationSightClip)}, nil
}

// ── the judgment ────────────────────────────────────────────────────────────

const automationJudgePrompt = `You check one condition a person asked to be told about, against what one look just saw. You decide ONE thing: does the condition hold now?

Answer with "yes", "no" or "unknown" as the first word, then ONE plain sentence saying what you saw — the sentence a person reads, so write it as you would say it: "the last run on main failed", "no new invoice since the last look".

The condition is the only criterion. What the look ran is part of the evidence: a look that printed only a status code or a ping shows that something answered, not that the thing named is true. When the evidence does not settle it — empty, unreadable, weaker than the condition — answer "unknown" and say what stopped you. Never answer "no" for something you could not decide, and never "yes" for something the output does not carry.`

func (r *automationRunner) Judge(ctx context.Context, item automation.Automation, sight automation.Sight) (automation.Judgment, error) {
	cfg, err := r.base(item.Workspace)
	if err != nil {
		return automation.Judgment{}, err
	}
	model, err := automationJudgeModel(cfg)
	if err != nil {
		return automation.Judgment{}, err
	}
	client, model, err := r.judgeClient(cfg, model)
	if err != nil {
		return automation.Judgment{}, err
	}
	callCtx := provider.WithRole(
		provider.WithRoutingIntent(
			provider.WithoutStream(withPurpose(ctx, purposeAutomationCheck)),
			provider.IntentBackground),
		lane.RoleStanding)
	response, err := client.CompleteWithMessages(callCtx, []ai.Message{
		textMessage("system", automationJudgePrompt),
		textMessage("user", automationJudgeQuestion(item, sight)),
	})
	if err != nil {
		return automation.Judgment{}, err
	}
	if response == nil {
		return automation.Judgment{}, errors.New("the model answered nothing")
	}
	usd := 0.0
	in, out := 0, 0
	if response.Usage != nil {
		if response.Usage.Cost != nil {
			usd = *response.Usage.Cost
		}
		in, out = response.Usage.PromptTokens, response.Usage.CompletionTokens
	}
	// THE JUDGMENT IS BILLED WHERE EVERY OTHER CALL IS, against its automation —
	// the standing sentinel it replaces never reached the usage ledger at all.
	RecordUsage(UsageLedgerPath(), UsageLine{
		Model: model, Role: string(roles.RoleSentinel), Calls: 1, Input: in, Output: out, USD: usd,
		Automation: item.ID, Workspace: item.Workspace,
	})
	word, line := automationVerdictWords(response.Text())
	switch word {
	case "yes":
		return automation.Judgment{Met: true, Sure: true, Line: line, USD: usd}, nil
	case "no":
		return automation.Judgment{Met: false, Sure: true, Line: line, USD: usd}, nil
	}
	if line == "" {
		line = "there was no clear answer"
	}
	return automation.Judgment{Sure: false, Line: line, USD: usd}, nil
}

// judgeClient is the one client per seat a runner keeps.
func (r *automationRunner) judgeClient(cfg Config, model string) (Completer, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	settings := cfg.clientConfig(model, providerTimeout)
	settings.Routing = provider.StaticRouting(cfg.Routing)
	settings.Fallbacks = cfg.ModelFallbacks
	settings.NearestModels = cfg.NearestModels
	if client, ok := r.clients[settings.Model]; ok {
		return client, settings.Model, nil
	}
	client, err := provider.NewClient(settings)
	if err != nil {
		return nil, settings.Model, err
	}
	r.clients[settings.Model] = client
	return client, settings.Model, nil
}

// automationJudgeModel is the model a watch's judgment runs on: the low-tier
// sentinel seat, resolved the way every role is.
func automationJudgeModel(cfg Config) (string, error) {
	return roles.Resolve(roles.Source(cfg.RolesSource), roles.RoleSentinel, cfg.Model)
}

// automationJudgeQuestion is the judgment as the model reads it.
func automationJudgeQuestion(item automation.Automation, sight automation.Sight) string {
	var out strings.Builder
	out.WriteString("THE CONDITION:\n" + strings.TrimSpace(item.Look.Condition))
	if words := strings.TrimSpace(item.Words); words != "" {
		out.WriteString("\n\nWHAT THE PERSON SAID WHEN THEY SET IT UP:\n" + words)
	}
	out.WriteString("\n\nWHAT THE LOOK RAN:\n" + automationLookWords(item.Look))
	text := strings.TrimSpace(sight.Text)
	if text == "" {
		text = "(nothing)"
	}
	out.WriteString("\n\nWHAT IT SAW:\n" + text)
	switch item.Seen {
	case "yes":
		out.WriteString("\n\nLAST TIME IT WAS DECIDED, THE CONDITION HELD.")
	case "no":
		out.WriteString("\n\nLAST TIME IT WAS DECIDED, THE CONDITION DID NOT HOLD.")
	}
	out.WriteString("\n\nDoes the condition hold now? Answer yes, no or unknown as the first word, then one plain sentence.")
	return out.String()
}

// automationLookWords is the look as one line.
func automationLookWords(look *automation.Look) string {
	switch {
	case strings.TrimSpace(look.Command) != "":
		return look.Command
	case strings.TrimSpace(look.Files) != "":
		return "the files matching " + look.Files
	}
	words := look.Tool
	if args := strings.TrimSpace(string(look.Args)); args != "" && args != "{}" {
		words += " " + args
	}
	return words
}

// automationVerdictWords reads a reply's verdict as its FIRST WHOLE WORD and
// the rest as the line. It is not a prefix match: "yesterday the run failed"
// and "nothing changed" are not verdicts.
func automationVerdictWords(reply string) (word, line string) {
	fields := strings.Fields(strings.TrimSpace(reply))
	if len(fields) == 0 {
		return "", ""
	}
	word = strings.ToLower(strings.Trim(fields[0], " \t.,:;!?\"'()[]{}—–-"))
	if len(fields) > 1 {
		line = strings.TrimSpace(strings.TrimLeft(strings.Join(fields[1:], " "), "—:-, "))
	}
	return word, line
}

// ── the work ────────────────────────────────────────────────────────────────

// automationBinding is the memory posture a run works under. THE PERSON'S RULES
// HOLD OVER A RUN, AND NOTHING A RUN DOES IS REMEMBERED: the brain is lent
// read-only ([Config.bindingOnlyMemory]), so the rules marked always are read
// before the first action and nothing work nobody watched turned up is
// extracted, promoted or kept. A run with memory off has nothing to lend.
func automationBinding(cfg Config) Config {
	cfg.bindingOnlyMemory = cfg.Memory != nil
	return cfg
}

// Work carries out one run of an automation's brief as unattended work, in
// three phases with one function each: the run's own configuration, the turn
// watched for the two reasons to stop it, and what it came to.
func (r *automationRunner) Work(ctx context.Context, item automation.Automation, run automation.Run, evidence string) (automation.Report, error) {
	setup, err := r.setUpWork(item, run)
	if err != nil {
		return automation.Report{Transcript: setup.dir}, err
	}
	if setup.refused != "" {
		return automation.Report{Outcome: automation.OutcomeYourCall, Line: setup.refused}, nil
	}
	agent, err := r.newChild(setup.cfg)
	if err != nil {
		return automation.Report{Transcript: setup.dir}, err
	}
	defer func() { _ = agent.Close() }()
	events, err := agent.Submit(ctx, automationBrief(item, evidence))
	if err != nil {
		return automation.Report{Transcript: setup.dir}, err
	}
	limit := item.Limits.Effective().USD
	return workReport(agent, setup, watchWork(agent, events, limit), limit)
}

// workSetup is one run's configuration and what its report needs from it: the
// run's folder (empty until a failure has a folder to point at), the door the
// run reports through, and where kept work went — or, when the run may not
// start at all, the line that says why.
type workSetup struct {
	cfg        Config
	dir        string
	report     *AutomationRun
	branchNote string
	refused    string
}

// setUpWork builds the run's configuration from the person's own assembly.
func (r *automationRunner) setUpWork(item automation.Automation, run automation.Run) (workSetup, error) {
	runDir := filepath.Join(r.root, "runs", item.ID, strconv.FormatInt(run.ID, 10))
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return workSetup{}, err
	}
	cfg, err := r.base(item.Workspace)
	if err != nil {
		return workSetup{}, err
	}
	place := Place{Dir: runDir, Workspace: item.Workspace}
	cfg.Workspace = item.Workspace
	cfg.Place = place
	cfg.SessionFile = place.Transcript()
	cfg.WorktreeRoot = place.Trees()
	// NOBODY IS THERE TO ASK, so a call the person's own rules would ask about
	// is refused and the run stops on it ([automationNeedsPerson]). The policy
	// itself is theirs: the door built it, and nothing here widens it.
	cfg.AskConsent = false
	cfg.InTask = true
	cfg.Interactive = false
	cfg.Unattended = false
	// NOTHING AN AUTOMATION DOES MAY ARM ANOTHER.
	cfg.Automations = nil
	cfg = automationBinding(cfg)
	report := &AutomationRun{}
	cfg.AutomationRun = report
	cfg.automationID = item.ID
	setup := workSetup{cfg: cfg, dir: runDir, report: report}
	// THE PERSON'S DAILY LIMIT HOLDS OVER A RUN OF WORK, as it holds over a
	// conversation's turn ([Agent.railBlockLocked]). A run is shaped like a
	// task, which that check passes over because the conversation that started
	// the task asked first; nothing asked before an automation's run, so it is
	// asked here, before anything — a worktree included — is made for it.
	//
	// A WATCH'S JUDGMENT IS NOT HELD TO IT. A look costs a fraction of a cent,
	// and a refused look is news, so holding looks to the limit would raise a
	// notification every few minutes until midnight to save pennies.
	if line, reached := automationDailyLimitReached(cfg.ProfileDir); reached {
		setup.refused = line
		return setup, nil
	}
	if item.Worktree {
		tree, err := automationWorktree(cfg, item)
		if err != nil {
			return setup, err
		}
		setup.cfg.Workspace = tree.dir
		setup.cfg.Place.Workspace = tree.dir
		setup.branchNote = "work kept on " + tree.branch + " in " + tree.dir
	}
	_ = SaveMeta(runDir, Meta{
		ID: place.ID(), Title: item.Title, Workspace: item.Workspace, Model: setup.cfg.Model, Created: time.Now(),
	})
	return setup, nil
}

// automationSpentToday is what the machine has spent today, read off the usage
// ledger. It is a variable so a test can stand in a figure without writing to a
// ledger every other test in the package reads.
var automationSpentToday = spentTodayOnLedger

// automationDailyLimitReached answers the line a run of work stops on when the
// person's daily spending limit is reached, the same reading a conversation's
// turn makes. No limit, or one that cannot be read, is no refusal.
func automationDailyLimitReached(profileDir string) (string, bool) {
	daily, err := config.DailyBudgetUSDAt(profileDir)
	if err != nil || daily <= 0 {
		return "", false
	}
	spent := automationSpentToday()
	if spent < daily {
		return "", false
	}
	return fmt.Sprintf("today's spending limit is reached · %s spent of %s · /budget day changes it",
		railMoney(spent), railMoney(daily)), true
}

// workEnd is how a run's turn ended, read off its events.
type workEnd struct {
	fault  error
	needs  string
	capped bool
}

// watchWork drains a run's turn, stopping it the moment it needs the person or
// reaches its money cap.
func watchWork(agent *Agent, events <-chan Event, limit float64) workEnd {
	var end workEnd
	for event := range events {
		switch event.Kind {
		case EventToolFailed:
			if end.needs == "" && automationNeedsPerson(event.Hint+" "+event.Output) {
				end.needs = automationNeedsLine(event)
				agent.InterruptFor(StopByWorkStopped)
			}
		case EventToolFinished:
			if !end.capped && limit > 0 && agent.Usage().CostUSD >= limit {
				end.capped = true
				agent.InterruptFor(StopByWorkStopped)
			}
		case EventError:
			if event.Err != nil {
				end.fault = event.Err
			}
		}
	}
	return end
}

// workReport is what a run came to: the person's ok it stopped on, the cap it
// reached, a fault, or its own report.
func workReport(agent *Agent, setup workSetup, end workEnd, limit float64) (automation.Report, error) {
	// THE LAST THING IT SAID is read from the run's own transcript, the one
	// account of the turn that does not depend on how its text was streamed.
	lastWords := strings.TrimSpace(lastSaid(agent))
	out := automation.Report{USD: agent.Usage().CostUSD, Transcript: setup.dir}
	status, summary, reported := setup.report.result()
	switch {
	case end.needs != "":
		out.Outcome, out.Line = automation.OutcomeYourCall, end.needs
	case end.capped:
		out.Outcome, out.Line = automation.OutcomeIncomplete, fmt.Sprintf("reached its $%.2f cap", limit)
	case end.fault != nil && !reported:
		return out, end.fault
	case reported && status == "done":
		out.Outcome, out.Line, out.Detail = automation.OutcomeDone, firstLineOf(summary), summary
	case reported:
		out.Outcome, out.Line, out.Detail = automation.OutcomeIncomplete, firstLineOf(summary), summary
	default:
		// NO REPORT, NO OUTCOME: the clock reads this as incomplete, and the
		// last thing the run said is kept as the detail, never as the result.
		out.Detail = lastWords
	}
	if setup.branchNote != "" {
		out.Detail = strings.TrimSpace(out.Detail + "\n\n" + setup.branchNote)
	}
	return out, nil
}

// automationBrief is what one run of work is told.
func automationBrief(item automation.Automation, evidence string) string {
	var out strings.Builder
	out.WriteString(strings.TrimSpace(item.Action.Do))
	if evidence = strings.TrimSpace(evidence); evidence != "" {
		out.WriteString("\n\nWHAT THE WATCH SAW, which is why this is running now:\n" + evidence)
	}
	out.WriteString("\n\nThis is an automation (" + item.Title + ") running on its own schedule; nobody is watching it run. ")
	out.WriteString("If something you need is refused for want of the person's permission, stop there — it will be asked of them. ")
	out.WriteString("When the work is finished, or you cannot finish it, call automation_report once with status done or incomplete and a short summary: that summary is the result the person reads.")
	return out.String()
}

// automationWorktree cuts the separate worktree one run of work runs in, and
// records it in the run's folder so its branch can always be found.
func automationWorktree(cfg Config, item automation.Automation) (taskTree, error) {
	root, ok := repositoryRoot(item.Workspace)
	if !ok || !hasCommit(root) {
		return taskTree{}, errors.New("a separate worktree needs a git repository with a commit")
	}
	name := "automation-" + slugify(item.Title) + "-" + shortID()
	dir := canonicalPath(filepath.Join(cfg.Place.Trees(), name))
	tree, err := cutWorktreeAt(cfg.Place, root, dir, "automation/"+name, 0o700)
	if err != nil {
		return taskTree{}, fmt.Errorf("cut the worktree: %w", err)
	}
	err = withMetaLock(cfg.Place.Dir, func() error {
		meta, err := LoadMeta(cfg.Place.Dir)
		if err != nil {
			return err
		}
		if meta.ID == "" {
			meta.ID = cfg.Place.ID()
		}
		meta.Workspace = item.Workspace
		meta.Trees = append(meta.Trees, StandingTree{
			Folder: item.Workspace, Dir: tree.dir, Branch: tree.branch, Root: tree.root,
			Home: tree.home, HomeSha: tree.homeSha, Mode: TaskModeWorktree, Cut: time.Now(),
		})
		return SaveMeta(cfg.Place.Dir, meta)
	})
	if err != nil {
		return taskTree{}, fmt.Errorf("record the worktree %s on %s: %w", tree.dir, tree.branch, err)
	}
	return tree, nil
}

// automationNeedsPerson reports whether a refusal is one only the person could
// have lifted — a call that needed their ok with nobody there to give it —
// rather than a refusal the run can read and work around. The doors that say
// so do not share a spelling, so it is read by what each states.
func automationNeedsPerson(text string) bool {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "no resolver is attached"):
		return true
	case strings.Contains(lower, "nobody") && (strings.Contains(lower, "to ask") || strings.Contains(lower, "is watching")):
		return true
	case strings.Contains(lower, "needs the person to say yes"):
		return true
	}
	return false
}

// automationNeedsLine is the one line a run stopped on a permission leaves:
// which call it needed the person's ok for.
func automationNeedsLine(event Event) string {
	what := strings.TrimSpace(event.Tool)
	var args map[string]any
	if json.Unmarshal([]byte(event.Args), &args) == nil {
		for _, key := range []string{"command", "cmd", "path", "url", "service"} {
			if value, ok := args[key].(string); ok && strings.TrimSpace(value) != "" {
				what += " " + clip(strings.TrimSpace(value), 80)
				break
			}
		}
	}
	return "needed your ok to run " + what
}

// automationTail keeps the last n bytes of text, on a line boundary where it
// can, and says it cut.
func automationTail(text string, n int) string {
	text = strings.TrimSpace(text)
	if len(text) <= n {
		return text
	}
	cut := text[len(text)-n:]
	if at := strings.IndexByte(cut, '\n'); at >= 0 && at < len(cut)-1 {
		cut = cut[at+1:]
	}
	return "…\n" + strings.TrimSpace(cut)
}

func firstLineOf(text string) string {
	text = strings.TrimSpace(text)
	if line, _, ok := strings.Cut(text, "\n"); ok {
		return strings.TrimSpace(line)
	}
	return text
}

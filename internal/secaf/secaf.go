// Package secaf is security-audit: sec-af, the security auditor, as a program
// codeaf carries and runs, and nothing else can. It takes one brief — the
// whole repository, or the changes on this branch — reads the folder it is
// handed without changing it, hunts for vulnerabilities, sets each against the
// code to show it exploitable or rule it out, suggests fixes for the ones that
// stand, and ends with one report.
//
// IT WAS A SEPARATE PROGRAM. sec-af (github.com/Agent-Field/sec-af) was an
// AgentField node: a control plane carried its calls, a router key it held
// paid for its models, and a coding-agent binary ran each of its agent
// sessions. It was copied into codeaf once, at the tag codeaf-absorb (47d57d7),
// and lives on only here (docs/design/security-audit/ABSORB.md). Its algorithm
// — the phases, the twelve hunters, the four-agent proof chain, the prompts —
// is in the packages below this one and is its own; what it runs on is
// codeaf's (internal/secaf/backing).
//
// IT HAS NO ENTRY POINT OF ITS OWN. What codeaf needs of it is a
// delegate.Delegate value, and its command's body takes a delegate.Host, which
// only codeaf makes: `/security-audit` in the chat, `codeaf security-audit` at
// a shell, and `propose_task` with `via: "security-audit"`.
package secaf

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/programguide"
	"github.com/Agent-Field/codeaf/internal/secaf/audit"
	"github.com/Agent-Field/codeaf/internal/secaf/backing"
)

// Name is the program's name: its chat command and its shell verb.
const Name = "security-audit"

// The stages the audit reports, in order. They are sec-af's own phases, and
// `starting` and `report` around them.
const (
	stageStarting    = "starting"
	stageRecon       = "recon"
	stageHunt        = "hunt"
	stageProve       = "prove"
	stageRemediation = "remediation"
	stageReport      = "report"
)

// Stages is every stage the audit reports, in the order it reaches them.
var Stages = []string{stageStarting, stageRecon, stageHunt, stageProve, stageRemediation, stageReport}

// stageWords is the stages in a person's words, for the task's row.
var stageWords = map[string]string{
	stageStarting:    "starting",
	stageRecon:       "mapping the code",
	stageHunt:        "hunting",
	stageProve:       "testing what it found",
	stageRemediation: "writing fixes",
	stageReport:      "writing the report",
}

// Unattended is the audit's own ceilings: $5 and two hours, which a standard
// audit of a mid-sized repository fits in with room, and which stop a run
// nobody is watching.
var Unattended = delegate.Ceilings{CostUSD: 5, Hours: 2}

// Program is security-audit as codeaf carries it.
var Program = delegate.Delegate{
	Name:    Name,
	Summary: "a security audit of the repository or of its changes",
	Guide:   programguide.SecurityAudit,
	Lands:   delegate.LandsText,
	// A BARE `/security-audit` AUDITS THE REPOSITORY IT IS STARTED IN, which
	// is what the name already says.
	DefaultBrief: "whole repository",
	Args:         "[changes [since <ref>]] [quick | thorough]",
	Unattended:   Unattended,
	// THE OFFER IS A FIX, AND THE PERSON SAYS WHETHER. A report that found
	// something wants mending; mending is another program's work and more of
	// the person's money, so the conversation offers it and waits.
	FollowUp: "If it found confirmed or likely problems, offer to hand them to senior-dev as one task that fixes them, " +
		"naming which; propose that task only after the person says yes.",
	CrewFlags:  crewFlags,
	StageWords: stageWords,
	Present:    presentActions,
	Default:    "run",
	Page:       Name,
	Commands:   []delegate.Command{runCommand},
}

// crewFlags is the conversation's crew as the audit's own flags: the working
// seat (or the models the person asked for) answers the agent sessions that
// read the code, and the light seat the single structured calls.
func crewFlags(crew delegate.Crew) []string {
	var flags []string
	model := crew.Hands
	if len(crew.Asked) > 0 {
		model = crew.Asked[0]
	}
	if model = strings.TrimSpace(model); model != "" {
		flags = append(flags, "--model", model)
	}
	if light := strings.TrimSpace(crew.Light); light != "" {
		flags = append(flags, "--light", light)
	}
	return flags
}

// options is one run's flags and brief.
type options struct {
	brief       string
	changes     bool
	base        string
	depth       string
	model       string
	light       string
	severity    string
	maxProvers  int
	compliance  string
	sessions    int
	maxTurns    int
	sessionWall time.Duration
}

var runCommand = delegate.Command{
	Name:    "run",
	Usage:   "[flags] [-- <brief>]",
	Summary: "audits the repository or its changes and reports what stands",
	Bind:    bindRun,
}

func bindRun(fs *flag.FlagSet) delegate.Body {
	var o options
	fs.BoolVar(&o.changes, "changes", false, "audit only the changes since the branch's base")
	fs.StringVar(&o.base, "base", "", "the commit or branch the changes are measured from")
	fs.StringVar(&o.depth, "depth", "", "quick, standard or thorough (default: standard)")
	fs.StringVar(&o.model, "model", "", "the model that reads the code (default: the run's work seat)")
	fs.StringVar(&o.light, "light", "", "the model for single structured calls (default: --model)")
	fs.StringVar(&o.severity, "severity", "low", "the least severe finding to report: info, low, medium, high")
	fs.IntVar(&o.maxProvers, "max-provers", 0, "the most findings to test (default: the depth's)")
	fs.StringVar(&o.compliance, "compliance", "", "frameworks to map findings to, such as owasp,pci-dss")
	fs.IntVar(&o.sessions, "sessions", 8, "how many agent sessions run at once")
	fs.IntVar(&o.maxTurns, "max-turns", 50, "the most turns one agent session takes")
	fs.DurationVar(&o.sessionWall, "session-wall", 30*time.Minute, "the longest one agent session runs")
	return func(ctx context.Context, host delegate.Host, args []string) error {
		o.brief = strings.Join(args, " ")
		run(ctx, host, o, os.Stderr)
		return nil
	}
}

// run is the body: hello first, the audit, and exactly one terminal.
func run(ctx context.Context, host delegate.Host, o options, notes io.Writer) {
	host.Hello(Stages)
	defer func() {
		if recovered := recover(); recovered != nil {
			_, _ = fmt.Fprintf(notes, "[security-audit] panic: %v\n%s", recovered, debug.Stack())
			host.Terminal(delegate.Ending{Status: delegate.StatusCrashed,
				Message: fmt.Sprintf("security-audit broke: %v", recovered)})
		}
	}()
	host.Terminal(runAudit(ctx, host, o, notes))
}

// reportReserve is the time kept back from the run's wall for writing the
// report, so an audit cut by its hours still says what it found.
const reportReserve = 3 * time.Minute

// runAudit is the audit from the brief to its ending.
func runAudit(ctx context.Context, host delegate.Host, o options, notes io.Writer) delegate.Ending {
	started := time.Now()
	scope := ReadScope(o.brief, o.changes, o.base, o.depth)
	switch scope.Depth {
	case "quick", "standard", "thorough":
	default:
		return delegate.Ending{Status: delegate.StatusFail,
			Message: fmt.Sprintf("security-audit did not start: the depth %q is not quick, standard or thorough", scope.Depth)}
	}
	root, err := filepath.EvalSymlinks(host.Workspace())
	if err != nil {
		return delegate.Ending{Status: delegate.StatusFail, Message: "security-audit did not start: " + err.Error()}
	}
	host.Stage(delegate.StageRecord{Stage: stageStarting, Status: "running", Data: stageData(map[string]any{
		"scope": scope.Describe(), "depth": scope.Depth})})
	var changes *Changes
	if scope.Changes {
		read, err := ReadChanges(ctx, root, scope.Base)
		if err != nil {
			if errors.Is(err, errNoChanges) {
				return delegate.Ending{Status: delegate.StatusPass, Message: "security-audit had nothing to audit: " + err.Error(),
					Deliverable: "Security audit of " + scope.Describe() + ": " + err.Error() + ", so there was nothing to audit.",
					Extra:       map[string]any{"status": "pass"}}
			}
			return delegate.Ending{Status: delegate.StatusFail, Message: "security-audit did not start: " + err.Error()}
		}
		changes = &read
		host.Stage(delegate.StageRecord{Stage: stageStarting, Status: "changes", Data: stageData(map[string]any{
			"changed": len(read.Changed), "nearby": len(read.Nearby), "base": read.BaseName})})
	}
	client, err := backing.NewClient(host.Models())
	if err != nil {
		return delegate.Ending{Status: delegate.StatusFail, Message: "security-audit did not start: " + err.Error()}
	}
	records := ""
	if recorder, ok := host.(delegate.Recorder); ok {
		records = recorder.Records()
	}
	light := o.light
	if light == "" {
		light = o.model
	}
	app := backing.New(client, backing.Config{
		Root: root, SessionModel: o.model, AIModel: light,
		Sessions: o.sessions, MaxTurns: o.maxTurns, SessionWall: o.sessionWall,
		Watch: newWatch(host),
	})
	request := auditRequest(root, scope, changes, o, records)
	// THE CHECKPOINTS GO WITH THE RECORDS, never into the person's folder. A
	// run with no record folder keeps them in a scratch folder of its own and
	// removes it when it ends.
	if records != "" {
		request.CheckpointDir = filepath.Join(records, "checkpoints")
	} else if scratch, err := os.MkdirTemp("", "security-audit-"); err == nil {
		defer os.RemoveAll(scratch)
		request.CheckpointDir = scratch
	}
	// THE RUN'S HOURS ARE THE AUDIT'S, LESS THE TIME TO WRITE IT DOWN. codeaf
	// stops a program at its wall whatever it is doing; an audit that stops
	// itself a little before has the minutes to say what it found.
	auditCtx := ctx
	if wall := host.Ceilings().Elapsed(); wall > 2*reportReserve {
		var cancel context.CancelFunc
		auditCtx, cancel = context.WithTimeout(ctx, wall-reportReserve-time.Since(started))
		defer cancel()
	}
	if changes != nil {
		auditCtx = withFocus(auditCtx, *changes)
	}
	result, err := audit.Run(auditCtx, app, request)
	spent, sessions, calls := app.Spent()
	if err != nil {
		_, _ = fmt.Fprintf(notes, "[security-audit] the audit ended without a result: %v\n", err)
		ending := delegate.Ending{CostUSD: spent, Reason: err.Error(),
			Extra: map[string]any{"sessions": sessions, "calls": calls}}
		switch {
		case errors.Is(err, backing.ErrCeiling):
			ending.Status = delegate.StatusBudget
			ending.Message = "security-audit reached the run's dollar ceiling before it finished, so it has no report"
		case errors.Is(auditCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil:
			ending.Status = delegate.StatusBudget
			ending.Message = "security-audit reached the run's time ceiling before it finished, so it has no report"
		case ctx.Err() != nil:
			ending.Status = delegate.StatusFail
			ending.Message = "security-audit was stopped before it finished"
		default:
			ending.Status = delegate.StatusFail
			ending.Message = "security-audit did not finish: " + firstSentence(err.Error())
		}
		return ending
	}
	host.Stage(delegate.StageRecord{Stage: stageReport, Status: "running"})
	files, err := reportFiles(records, result, len(request.ComplianceFrameworks) > 0)
	if err != nil {
		_, _ = fmt.Fprintf(notes, "[security-audit] could not write the full report: %v\n", err)
	}
	text := summary(scope, changes, result, files)
	return delegate.Ending{
		Status:      delegate.StatusPass,
		Message:     firstLine(text),
		CostUSD:     spent,
		Deliverable: text,
		Extra: map[string]any{
			"status": "pass", "reports": files, "sessions": sessions, "calls": calls,
			"confirmed": result.Confirmed, "likely": result.Likely,
			"inconclusive": result.Inconclusive, "ruled_out": result.NotExploitable,
		},
	}
}

// auditRequest is sec-af's own request for this run.
func auditRequest(root string, scope Scope, changes *Changes, o options, records string) audit.AuditRequest {
	request := audit.NewAuditRequest()
	request.RepoURL = root
	request.Depth = scope.Depth
	if severity := strings.TrimSpace(o.severity); severity != "" {
		request.SeverityThreshold = severity
	}
	if o.maxProvers > 0 {
		provers := o.maxProvers
		request.MaxProvers = &provers
	}
	if frameworks := splitList(o.compliance); len(frameworks) > 0 {
		request.ComplianceFrameworks = frameworks
	}
	if changes != nil {
		// AN AUDIT OF THE CHANGES KEEPS WHAT IT FOUND IN THEM AND NEAR THEM
		// (sec-af's own include filter, applied after the hunt), and tests
		// fewer findings than a whole repository's, unless told otherwise.
		request.IncludePaths = append([]string(nil), changes.Relevant...)
		if request.MaxProvers == nil {
			provers := changesProvers
			request.MaxProvers = &provers
		}
	}
	return request
}

// changesProvers is how many findings an audit of the changes tests by
// default: half a standard audit's thirty.
const changesProvers = 15

func splitList(text string) []string {
	var out []string
	for _, part := range strings.Split(text, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func firstSentence(text string) string {
	text = firstLine(strings.TrimSpace(text))
	if runes := []rune(text); len(runes) > 300 {
		text = string(runes[:300]) + "…"
	}
	return text
}

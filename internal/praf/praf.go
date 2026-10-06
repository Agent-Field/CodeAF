// Package praf is pr: pr-af, the pull-request reviewer, as a program codeaf
// carries and runs, and nothing else can. It takes one brief — a GitHub pull
// request, or the current branch's — checks the pull request out in a folder
// of its own, plans which parts of the change to review, sends a reviewer at
// each, challenges what they found against the code, looks for gaps, and ends
// with one review. It posts nothing; posting a review it wrote is a second,
// separate run (`/pr post <report>`) that the person says yes to.
//
// IT WAS A SEPARATE PROGRAM. pr-af (github.com/Agent-Field/pr-af) was an
// AgentField node: a control plane carried its calls, a router key it held
// paid for its models, and a coding-agent binary ran each of its reviewers.
// Its Go port was copied into codeaf once, at the tag codeaf-absorb
// (docs/design/pr-review/ABSORB.md); its algorithm — the intake, the review
// plan, the dimension reviewers, the evidence, challenge, cross-reference and
// coverage passes, the prompts — is in the packages below this one and is its
// own. What it runs on is codeaf's: the agent sessions sec's audit runs on
// (internal/agentsession), over the run's model API.
//
// IT HAS NO ENTRY POINT OF ITS OWN. What codeaf needs of it is a
// delegate.Delegate value, and its command's body takes a delegate.Host, which
// only codeaf makes: `/pr` in the chat, `codeaf pr` at a shell, and
// `propose_task` with `via: "pr"`.
package praf

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/programguide"
)

// Name is the program's name: its chat command and its shell verb.
const Name = "pr"

// Unattended is a review's own ceilings, which stop a run nobody is watching.
// pr-af's served node capped a review at $2 and an hour, and real reviews of
// mid-sized pull requests measured 35 to 70 minutes; these leave headroom for
// a stronger model and a large change until reviews through codeaf are
// measured.
var Unattended = delegate.Ceilings{CostUSD: 5, Hours: 2}

// Program is pr as codeaf carries it.
var Program = delegate.Delegate{
	Name:    Name,
	Summary: "a code review of a GitHub pull request",
	Guide:   programguide.PR,
	Lands:   delegate.LandsText,
	// A BARE `/pr` REVIEWS THE CURRENT BRANCH'S PULL REQUEST, which is what a
	// person in a checkout of their own work means by it.
	DefaultBrief: "current branch",
	// ITS BRIEF IS A PULL REQUEST AND A FOCUS, however the run was asked for: a
	// composed brief's quoted conversation could name some other pull request.
	Words: true,
	Args:  "[<pull request>] [what to focus on] | post <report>",
	// A TYPED RUN IS TITLED BY WHAT IT REVIEWS, because its brief is a link or
	// a few words that say little as a title.
	Title:      title,
	Unattended: Unattended,
	// THE OFFERS ARE THE PERSON'S TO TAKE. Posting is an outward-facing act on
	// their pull request and mending is another program's work and more of
	// their money, so the conversation offers both and waits.
	FollowUp: "If it found anything, offer the person two next steps and wait for their answer: posting this review " +
		"to the pull request as it stands (only after they say yes to posting, propose pr with the brief " +
		"`post <the pr-report.json path from the account>`), or handing the blocking findings to senior-dev as " +
		"one task that fixes them in this folder when it is the pull request's checkout, naming which. " +
		"Propose neither before they say yes to it.",
	ModelFlag:  "model",
	CrewFlags:  crewFlags,
	StageWords: stageWords,
	Present:    presentActions,
	Default:    "run",
	Page:       Name,
	Commands:   []delegate.Command{runCommand},
}

// title is a typed run's title.
func title(brief string) string {
	request := ReadBrief(brief, nil)
	switch {
	case request.Post != "":
		return "Posting a code review to GitHub"
	case request.Target.Number != 0:
		return "Code review of " + request.Target.String()
	}
	return "Code review of this branch's pull request"
}

// crewFlags is the conversation's crew as the review's own flags: the working
// seat (or the first model the person asked for) answers the reviewers' agent
// sessions, and the light seat the single structured calls.
//
// THE IDS GO ON AS THE CREW SPELLS THEM, which is how the run's model API
// reads them: a first segment naming a connected service is that service, and
// anything else is the default service's.
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

// defaultMaxTurns is the most turns one of a review's agent sessions takes.
//
// IT IS 20, NOT THE 50 sec's AGENTS TAKE. A review's prompts already carry the
// diff and the touched files, so a reviewer reads to confirm, not to find its
// way; yet told only to stop when it was sure, nine of twelve sessions in a
// live review of a two-file pull request read until the loop made them answer
// at their fiftieth turn, at about twenty seconds a turn, and the review had
// reached its challenge pass after an hour (2026-10-06). A session at its
// limit is still made to answer, so a lower limit costs depth, not an answer.
const defaultMaxTurns = 20

// options is one run's flags and brief.
type options struct {
	brief       string
	model       string
	light       string
	sessions    int
	maxTurns    int
	sessionWall time.Duration
}

var runCommand = delegate.Command{
	Name:    "run",
	Usage:   "[flags] [-- <brief>]",
	Summary: "reviews a pull request, or posts a review it wrote",
	Bind:    bindRun,
}

func bindRun(fs *flag.FlagSet) delegate.Body {
	var o options
	fs.StringVar(&o.model, "model", "", "the model that reads the code (default: the run's work seat)")
	fs.StringVar(&o.light, "light", "", "the model for single structured calls (default: --model)")
	fs.IntVar(&o.sessions, "sessions", 8, "how many agent sessions run at once")
	fs.IntVar(&o.maxTurns, "max-turns", defaultMaxTurns, "the most turns one agent session takes")
	fs.DurationVar(&o.sessionWall, "session-wall", 30*time.Minute, "the longest one agent session runs")
	return func(ctx context.Context, host delegate.Host, args []string) error {
		o.brief = strings.Join(args, " ")
		run(ctx, host, o, os.Stderr)
		return nil
	}
}

// run is the body: hello first, the review or the post, and exactly one
// terminal — a panic in either included, written as the crash it is.
func run(ctx context.Context, host delegate.Host, o options, notes io.Writer) {
	request := ReadBrief(o.brief, func() (string, string, bool) { return workspaceOrigin(ctx, host.Workspace()) })
	if request.Post != "" {
		host.Hello([]string{stagePost})
	} else {
		host.Hello(Stages)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			_, _ = fmt.Fprintf(notes, "[pr] panic: %v\n%s", recovered, debug.Stack())
			host.Terminal(delegate.Ending{Status: delegate.StatusCrashed, Message: fmt.Sprintf("the review panicked: %v", recovered)})
		}
	}()
	if request.Post != "" {
		host.Terminal(runPost(ctx, host, request.Post, defaultGitHub()))
		return
	}
	host.Terminal(runReview(ctx, host, request, o, notes, defaultReviewer()))
}

// errNoBranchPR wraps why a brief that named no pull request found none on
// the current branch either.
var errNoBranchPR = errors.New("no pull request to review")

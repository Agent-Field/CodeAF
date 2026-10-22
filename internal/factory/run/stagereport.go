package run

// A STEP THAT STOPS WITHOUT REPORTING STILL ENDS WITH A REPORT.
//
// The factory reads exactly one thing from a chat step: its `stage_result`.
// A step that did all its work and ended its turn in words counted as having
// done nothing, and on codeaf#1765 a plan that had run twenty-five commands was
// marked `the stage ended without reporting` after one reminder. A model that
// forgot the closing call has almost always done the work, so this file is
// every way the round has of getting the report out of it, in order, each
// tried only when the one before came back empty:
//
//  1. A TURN THAT FAILED IS TRIED AGAIN ONCE, on the same conversation, with a
//     note saying what stopped it ([stageRetry]); a provider's refusal, a
//     timeout or a refused turn is not a forgotten report. A turn a spending
//     limit refused is the exception: it is a stop ([SpendStop]), because a
//     turn tried again would be refused again.
//  2. THREE REMINDERS, each more specific than the last ([stageAsker]): the
//     plain one, then the step's ask again with `call stage_result now`, then
//     a turn that must call the verb (internal/session's FollowUpRequiring,
//     the provider's required-tool setting), which a capable model cannot
//     answer in prose.
//  3. A REPORT THE FACTORY MAKES ([unreported]): one short call apart from the
//     conversation, reading the step's brief, its last message and the change
//     in its checkout, that must call `stage_result`. The report is marked
//     Inferred, shown as `reported for the step`, and judged like any other.
//
// `the stage ended without reporting` is written only when all of that was
// tried and nothing came back, and `ran out of time without reporting` only
// when [StageWall] ended the round first.

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// requirer is what a conversation may also do: start a turn whose first
// answer must call one tool, through the provider's required-tool setting.
// cmd/codeaf's stage conversation does; one that cannot is reminded in words.
type requirer interface {
	NoteRequiring(ctx context.Context, words, tool string) error
}

// inferrer is what a conversation may also do: say the last thing its model
// said, and make the step's report itself in one short call apart from its
// turns (internal/session's InferStageResult). One that cannot leaves a round
// that never reported as [stageNoReport].
type inferrer interface {
	LastWords() string
	Infer(ctx context.Context, ask string) (factory.StageResult, error)
}

// stageResultTool is the verb a stage reports with, as the conversation's belt
// names it (internal/session's tools_stage.go).
const stageResultTool = "stage_result"

// stageReminders is how many times a round asks for a report it never got.
const stageReminders = 3

// The sentences this file writes: what the conversation is told, what the
// item's log keeps of it, and how a round a spending limit stopped reads.
const (
	stageReminder = "You stopped without calling stage_result, so the factory has nothing from this step yet. " +
		"If the work is finished, call stage_result now with what you found. If it is not, carry on and call stage_result when it is, " +
		"or with done false and why, if it cannot be finished."
	stageReminderLast = "Call stage_result now with this step's result: whether its ask is carried out, what you did and found, and what is still open."
	stageReminded     = "stopped without reporting · reminded, "
	stageRequired     = ", on a turn that must report"
	stageRetried      = "the turn stopped on an error · trying it again: "
	stageInferred     = factory.ReportedForTheStep + " · from its brief, its last message and its change"
	// stageSpendDaily is the output of a round today's spending limit stopped,
	// and stageSpendStopped leads one any other spending limit stopped.
	stageSpendDaily   = "today's spend limit stopped this step"
	stageSpendStopped = "a spend limit stopped this step"
)

// stageReminderAgain is the second reminder: the step's ask again, and the call.
func stageReminderAgain(job Job) string {
	return "You stopped again without calling stage_result. This step is " + stageAsk(job) + ". " +
		"Call stage_result now: done true with what you did and found if that is carried out, or done false and why if it is not."
}

// stageRetry is what a conversation whose turn failed is told, once.
func stageRetry(why string) string {
	return "Your last turn stopped on an error before this step reported: " + why + ". " +
		"Carry on with this step from where you stopped. " + briefClosing
}

// SpendStop is a turn a spending limit refused, as the conversation hands it
// to the round (cmd/codeaf's stage conversation builds it from the session's
// refusal): Daily when it was today's limit over the whole machine, Said in the
// limit's own words. A round that ends on one is a stop, not a step that never
// reported, and the loop stops the job on it ([spendStopped]).
type SpendStop struct {
	Job   bool
	Daily bool
	Said  string
}

func (s SpendStop) Error() string {
	if s.Daily {
		return stageSpendDaily
	}
	return stageSpendStopped + ": " + oneLine(s.Said)
}

// spendStopOf is the spending limit that refused a turn, if one did.
func spendStopOf(err error) (SpendStop, bool) {
	var stop SpendStop
	return stop, errors.As(err, &stop)
}

// spendStopped says whether a round ended on a spending limit with nothing
// reported: its output is the stop's own sentence ([SpendStop.Error]).
func spendStopped(res factory.StageResult) bool {
	if res.Done || res.Reports > 0 || res.Inferred {
		return false
	}
	return res.Output == stageSpendDaily || strings.HasPrefix(res.Output, stageSpendStopped+": ")
}

// stageAsker is one round's account of what it has asked for a report it has
// not got: whether a failed turn was tried again, and how many reminders went.
type stageAsker struct {
	job      Job
	retried  bool
	reminded int
}

// next says the next thing to a conversation that went idle with nothing
// reported, and answers the line the item's log keeps of it. ok is false when
// there is nothing left to say, or the conversation would not take it.
func (s *stageAsker) next(ctx context.Context, conv Conversation, waitErr error) (line string, ok bool) {
	if waitErr != nil && !s.retried {
		s.retried = true
		why := oneLine(waitErr.Error())
		if conv.Note(ctx, stageRetry(why)) != nil {
			return "", false
		}
		return stageRetried + why, true
	}
	if s.reminded >= stageReminders {
		return "", false
	}
	s.reminded++
	line = stageReminded + strconv.Itoa(s.reminded) + " of " + strconv.Itoa(stageReminders)
	var err error
	switch s.reminded {
	case 1:
		err = conv.Note(ctx, stageReminder)
	case 2:
		err = conv.Note(ctx, stageReminderAgain(s.job))
	default:
		if r, can := conv.(requirer); can {
			err = r.NoteRequiring(ctx, stageReminderLast, stageResultTool)
			line += stageRequired
		} else {
			err = conv.Note(ctx, stageReminderLast)
		}
	}
	return line, err == nil
}

// The caps on what the factory's own report reads, in characters.
const (
	inferBriefMost  = 8000
	inferWordsMost  = 3000
	inferChangeMost = 4000
)

// unreported is the result of a round whose step never reported, and the line
// the item's log keeps of how it was made, "" for none: the wall's or the
// spending limit's own words when one of them ended the round, else the report
// the factory made for the step, else [stageNoReport] and why.
func unreported(ctx context.Context, conv Conversation, job Job, brief string, waitErr error, walled bool) (factory.StageResult, string) {
	if walled {
		return factory.StageResult{Output: stageWalled}, ""
	}
	if stop, ok := spendStopOf(waitErr); ok {
		return factory.StageResult{Output: stop.Error()}, ""
	}
	why := ""
	if waitErr != nil {
		why = oneLine(waitErr.Error())
	}
	if inf, ok := conv.(inferrer); ok {
		made, err := inf.Infer(ctx, inferAsk(brief, inf.LastWords(), checkoutChange(job.Dir, job.Item.ChangeBase)))
		if err == nil {
			made.Inferred, made.Reports = true, 0
			return made, stageInferred
		}
		why = "no report could be made for it: " + oneLine(err.Error())
	}
	if why == "" {
		return factory.StageResult{Output: stageNoReport}, ""
	}
	return factory.StageResult{Output: stageNoReport + ": " + why}, ""
}

// inferAsk is the whole of what the factory's own report reads: what it is
// for, then the step's brief, its last message and its change, each cut to its
// most and said when there was none.
func inferAsk(brief, words, change string) string {
	if words = strings.TrimSpace(words); words == "" {
		words = "(it said nothing)"
	}
	return "A step of a factory job stopped without calling stage_result, and its report is made here from what it left. " +
		"Read the step's brief, its last message and the change in its checkout, then call stage_result once with an honest account of that step: " +
		"done only when the brief's ask is carried out, findings for what is still wrong, and claims only for what the change or the message shows, each with its evidence.\n\n" +
		"The step's brief:\n" + cutAt(brief, inferBriefMost) + "\n\n" +
		"The step's last message:\n" + cutAt(words, inferWordsMost) + "\n\n" +
		"The change in its checkout:\n" + change
}

// checkoutChange is the step's change as git tells it, against the job's
// change base (HEAD when it has none): the stat, the new files git does not
// track yet, and the diff, cut together to [inferChangeMost].
func checkoutChange(dir, base string) string {
	if strings.TrimSpace(dir) == "" {
		return "(the checkout is not known)"
	}
	if base = strings.TrimSpace(base); base == "" {
		base = "HEAD"
	}
	git := changeGit{}
	var parts []string
	if stat, err := git.Run(dir, "diff", "--no-ext-diff", "--stat", base, "--"); err == nil && strings.TrimSpace(stat) != "" {
		parts = append(parts, strings.TrimRight(stat, "\n"))
	}
	if added, err := git.Run(dir, "ls-files", "--others", "--exclude-standard"); err == nil {
		var names []string
		for _, name := range strings.Split(added, "\n") {
			if name = strings.TrimSpace(name); name != "" {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			parts = append(parts, "new files: "+strings.Join(names, ", "))
		}
	}
	if patch, err := git.Run(dir, "diff", "--no-ext-diff", base, "--"); err == nil && strings.TrimSpace(patch) != "" {
		parts = append(parts, strings.TrimRight(patch, "\n"))
	}
	if len(parts) == 0 {
		return "(nothing changed in the checkout)"
	}
	return cutAt(strings.Join(parts, "\n\n"), inferChangeMost)
}

// A drafted Plan whose completion handoff failed resumes its conversation on
// the next bounded round or explicit retry. It does not rediscover the plan.
func planReportPending(name string, res *factory.StageResult) bool {
	return name == planStep && res != nil && !res.Done && res.Reports == 0 && !res.Inferred && (strings.HasPrefix(res.Output, stageNoReport) || strings.HasPrefix(res.Output, "Plan report recovery timed out"))
}

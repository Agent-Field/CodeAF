package session

// THE THREE OR FOUR WORDS A BACKGROUND JOB IS CALLED.
//
// A job's row is what a person reads to know what is running: the section on
// the right, the job's own page, the sentence a model is handed in `jobs list`.
// Until this file existed, a plain background command put its RAW INVOCATION
// there, cut to three words — so
//
//	bash -lc "pytest -q tests/ --maxfail=1"
//
// drew as `bash -lc "pytest`. That is not a name. It is whatever the command
// happened to open with, and a column of them names every job after its first
// noise.
//
// So a command is NAMED, by one small call, and the design is four properties
// copied from the task namer (taskname.go) because they are the same job:
//
//   - ONE CALL, ONCE, PER JOB, AND ONLY WHERE NOTHING NAMED IT. A watch, a
//     render, a hand and a task node are each given a label the moment they
//     start ([jobNameOf]), and those labels are what `jobs list` already puts
//     in front of the model. A second call to rename any of those would be the
//     harness disagreeing with itself and billing for it. A command that is
//     already short and has no shell metacharacters in it is left alone too
//     ([jobNameNeeded]), which is what stops `npm run dev` from paying for a
//     call that changes nothing.
//
//   - THE CHEAP MODEL. It resolves through internal/roles as RoleJobName, on
//     the low tier, beside the session's own namer and the task namer: naming
//     in a few words is the archetypal cheap call, and it is one of the calls
//     that must NOT think — the resolved level is deliberately not put on the
//     request.
//
//   - IT NEVER BLOCKS THE WORK. THE JOB NEVER WAITS TO BE NAMED. The process
//     is forked, registered and announced before this call is made; it runs on
//     a goroutine of its own with its own deadline, and the surfaces draw the
//     command they draw today until the answer lands. A name that never arrives
//     costs a good name and nothing else — there is no state for "a small thing
//     did not work" and inventing one would report a fault about work nobody
//     asked for.
//
//   - THE NAME IS THE JOB'S NAME FIELD, NOT A SECOND STRING BESIDE THE
//     COMMAND. It is written through [job.setName], which is under the
//     registry's lock because the name arrives LATE, and one update is
//     published so a row that is already on screen learns it. The command is
//     left exactly as it is: the name is what the job is called, the command
//     is what is running.
//
// IT IS CALLED FROM [Agent.announceJobRow], which is the registry's announce
// callback — the one door every start, adopt, watch, render and hand already
// walks through to reach the agent. A new way of starting a job inherits the
// name without knowing this file exists, and the gate makes the labelled
// kinds a no-op.

import (
	"context"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The namer is a ROLE, registered from the file that makes the call, as
// internal/roles asks. LOW, for the session title's reason rather than a
// judge's: a wrong name costs a glance at a column and is not a decision
// anything downstream is made from. A person who disagrees pins it
// (`roles.jobname: <model>`).
func init() { roles.Register(roles.RoleJobName, roles.TierLow) }

// JobNameWords is how long a background job's name is allowed to be.
//
// FOUR IS THE LENGTH A PERSON READS AS A LABEL rather than as a sentence, and
// it is a cap and not a target — a two-word name is left at two. It is exported
// because the surface that draws the name cuts to the same figure, and a namer
// asked for more words than the column can show would be paying for words that
// are thrown away on the way to the screen.
const JobNameWords = 4

// jobNameSystem is all the system message says, for the session namer's reason
// (title.go): the cheap model reads the system message as character and the end
// of the user message as the thing to do, so the instruction is not put here.
const jobNameSystem = "You name background jobs."

// jobNamePrompt is the whole instruction, and the shape of the answer IS the
// requirement: a label, in the lowercase every other label on this surface is
// drawn in. IT GOES LAST IN THE USER MESSAGE, after the command it is about —
// this call lands on the same small models the session's namer does, and one of
// them handed that instruction straight back as a session's name.
//
// IT IS ON [namesTheInstruction]'S OWN LIST in title.go, beside the session
// namer's and the task namer's, which is how an answer that hands the
// instruction back is refused. Relying instead on this prompt merely SHARING
// enough words with the task namer's would be a guard that held by coincidence
// and broke the first time either sentence was reworded.
const jobNamePrompt = "Name this piece of work in three or four words — a label for a running command, not a sentence. Lowercase, no quotes, no full stop, no file paths, no ids. Answer with the name and nothing else."

const (
	// jobNameCommandClip bounds the command the namer is shown. Past a page of
	// it everything is a heredoc or a flag list, and sending four thousand
	// characters to produce four words would pay for a whole context per job.
	jobNameCommandClip = 800
	// jobNameDirClip bounds the working directory, which is a path and not a
	// novel; the namer only needs to know where, not a nested volume prefix.
	jobNameDirClip = 400

	// jobNameTokens is the ceiling on the answer. Four words is a handful of
	// tokens; this is that with room for a model that says "Title: …" first,
	// which [cleanTitle] strips.
	jobNameTokens = 32

	// jobNameWindow is how long the call is given. Nobody is waiting for it —
	// the job is already running — so this is not a person's patience but a
	// bound on a goroutine holding a provider slot for work that has stopped
	// mattering: the row has been drawn under its command for twenty seconds by
	// then and a name arriving after that is a column changing under somebody's
	// eyes for no reason they can see.
	jobNameWindow = 20 * time.Second
)

// jobNameShellMeta is the characters that make a command a SCRIPT rather than
// a name. A pipe, a quote, a dollar — any of these and the command is not
// already what a person would call the job.
const jobNameShellMeta = "|&;<>()$\"'`\\*?[]{}!~#\n"

// auxRoleJobName is the journal tag for this errand. It lives here rather than
// beside auxRoleTaskName in loop.go because this file owns the call, and the
// two strings are the same word the role is registered under.
const auxRoleJobName = "jobname"

// nameJob gives one freshly announced job a name, if it needs one.
//
// It is called from [Agent.announceJobRow] — the ONE door every job that is
// published walks through, whoever started it — so a new way of starting a job
// inherits the name without knowing this file exists. THE JOB NEVER WAITS: the
// process is already running, and this is fire-and-forget.
func (a *Agent) nameJob(info jobInfo) {
	if a == nil || a.jobs == nil || !jobNameNeeded(info) {
		return
	}
	one := a.jobs.find(info.id)
	if one == nil {
		return
	}
	subject := jobNameSubject(info, a.config.Workspace)
	if subject == "" {
		return
	}
	// IT DOES NOT RIDE THE TURN'S CONTEXT. The turn that started this job ends
	// in a moment and the job outlives it by minutes; a namer cancelled with the
	// turn would only ever land for work started at the very end of one.
	go func() {
		name := a.jobName(context.Background(), subject)
		if name == "" {
			return
		}
		if !one.setName(name) {
			return
		}
		// A NAME ARRIVING IS NEWS even when nothing else moved: the first
		// notice went out under the command, and this is the row learning what
		// it is called.
		a.emitJobUpdate(noticeOf(one.info()))
	}()
}

// jobNameNeeded reports whether a job still wants a name made for it.
//
// A JOB THAT ALREADY HAS A LABEL IS LEFT ALONE, which is what keeps this from
// being a call on every watch, render, hand and task node: the registry minted
// those names the moment they started. A command that is already short and has
// no shell metacharacters in it is left alone too — `npm run dev` is what a
// person would call that job, and paying to rewrite it reads worse.
func jobNameNeeded(info jobInfo) bool {
	if strings.TrimSpace(info.label) != "" || strings.TrimSpace(info.name) != "" {
		return false
	}
	command := strings.TrimSpace(info.command)
	if command == "" {
		return false
	}
	if strings.ContainsAny(command, jobNameShellMeta) {
		return true
	}
	return len(strings.Fields(command)) > JobNameWords
}

// jobNameSubject is what the namer reads: the command, and the working
// directory if the registry has one. Both, because a `make test` in two
// different trees is two different jobs, and a giant heredoc is clipped so it
// does not become a giant prompt.
func jobNameSubject(info jobInfo, workspace string) string {
	command := strings.TrimSpace(clip(info.command, jobNameCommandClip))
	if command == "" {
		return ""
	}
	if dir := strings.TrimSpace(workspace); dir != "" {
		return command + "\n\nworking directory: " + clip(dir, jobNameDirClip)
	}
	return command
}

// jobName asks the cheap model for the name. Every failure answers "", and the
// caller's only response to that is to leave the job named as it was.
func (a *Agent) jobName(ctx context.Context, subject string) string {
	a.mu.Lock()
	model, closed, client := a.model, a.closed, a.client
	a.mu.Unlock()
	if closed || client == nil {
		return ""
	}
	// IT CARRIES ITS OWN DEADLINE for the shaper's reason: the provider's client
	// is built with no timeout, so a stalled namer would be a goroutine and a
	// provider slot held for the life of the session. Twenty seconds is a fact
	// about THIS call — three or four words off a command — and it is tighter
	// than the low tier's own bound, so it is the one in force (auxiliary.go).
	ctx, cancel := context.WithTimeout(ctx, jobNameWindow)
	defer cancel()

	// NO EFFORT IS PUT ON THE REQUEST, and that is the reflex law rather than an
	// omission: the calls that are told not to think are the ones that sort and
	// name in a few words, and this is one of them.
	response, named, callErr := a.callRole(ctx, roles.RoleJobName, model,
		[]ai.Message{
			textMessage("system", jobNameSystem),
			textMessage("user", subject+"\n\n"+jobNamePrompt),
		},
		ai.WithMaxTokens(jobNameTokens))
	if callErr != nil || response == nil {
		return ""
	}
	// The person pays for it out of the same pocket the session's own title and
	// the task namer come out of, and no turn asked for it — against the model
	// that ANSWERED, which is not always the rung the ladder resolved first.
	a.addAuxiliaryUsageAs(response, named, 1, auxRoleJobName)
	return cleanJobName(response.Text())
}

// cleanJobName reads the answer back through the same repair the session's own
// namer uses ([cleanTitle], title.go) — a model asked for a short lowercase name
// answers "Title: …", or quotes it, or welds it into a slug, at the same rates
// whichever prompt asked — and then holds it to the cap.
//
// AN ANSWER THAT IS STILL NOT A NAME IS NO ANSWER. The test is the same one that
// decided to make the call ([jobNameNeeded]): a namer that echoed a command has
// handed back exactly the thing the call was made to get rid of, and taking it
// would be paying to make the row no better. The other way an answer is not a
// name — the INSTRUCTION handed back — is refused by [cleanTitle] before the cut
// to four words ever happens, because a shared hand is the only place a rule
// like that can be true of both namers at once.
func cleanJobName(raw string) string {
	name := firstWordsOf(cleanTitle(raw), JobNameWords)
	if name == "" || jobNameNeeded(jobInfo{command: name}) {
		return ""
	}
	return name
}

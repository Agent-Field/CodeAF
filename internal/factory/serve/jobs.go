package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/agentfield"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// jobVerbs keeps canvas actions additive to the existing /v1 doors.
func (h *Handler) jobVerbs() {
	stored := h.config.Store != nil
	h.postJob("publish_anyway", stored && h.seam.Answer != nil, h.publishAnyway)
	h.postJob("start_now", h.seam.Launch != nil, h.guardedStart())
	h.postJob("run-as-new", stored && h.seam.NewJob != nil && h.seam.Launch != nil, h.onlyOwner(h.runAsNew))
	h.postJob("run_again", stored && h.seam.Launch != nil, h.guarded("run_again", h.onlyOwner(h.runAgain)))
	h.postJob("close", stored && h.workers != nil, h.closeJob)
	// factory.job_reject on a job whose AgentField run paused is the run's
	// Reject: the runner that owns the run carries it, then the job ends
	// rejected (agentFieldAnswer, lease.Doors.AnswerAgentField).
	h.postJob("reject", true, h.agentFieldAnswer(agentfield.DecisionRejected, h.rejectJob, stored && h.workers != nil))
	h.postJob("reopen", stored, h.guarded("reopen", h.onlyOwner(h.reopenJob)))
	h.postJob("restore", stored, h.guarded("restore", h.onlyOwner(h.restoreJob)))
	h.postJob("dismiss", stored, h.dismissJob)
	h.postJob("brief_seen", stored, h.briefSeen)
	h.postJob("assign_to_me", stored, h.assignToMe)
	h.postJob("change_assignee", stored, h.changeAssignee)
	h.postJob("unassign", stored, h.onlyOwner(h.unassignJob))
	h.postJob("nudge", stored, h.nudgeJob)
	h.postJob("follow", stored, h.followJob)
	h.postJob("priority", stored, h.setPriority)
	h.postJob(SetRepoVerb, stored, h.setRepo)
	h.postJob("stop-after-step", stored, h.onlyOwner(h.stopAfterStep))
	h.postJob("raise_budget", stored, h.onlyOwner(h.raiseBudget))
	h.postJob("model", stored, h.onlyOwner(h.setModel))
	h.postJob("control", stored, h.onlyOwner(h.controlJob))
	h.postJob("undo", stored, h.undoJob)
}

// startJob is start_now and run: every refusal, then one write, then the
// launch.
//
// EVERY REFUSAL COMES BEFORE ANY WRITE. A start that is refused leaves the job
// exactly as the person read it, its version included, so the app shows the
// refusal's own sentence instead of reading the job again, finding it moved,
// and saying the job changed (the owner's run of 2026-10-20, where every Start
// of a private repository's job wrote its mark and its budget, was then
// refused for having no checkout, and answered as a crash). The repository
// being off is refused by postJob before this; then the question the job
// waits on, GitHub's own word that it ended ([Handler.endedOnGitHub], whose
// refusal records that ending on purpose), the checkout ([Handler.needsCheckout])
// and the recipe the budget comes from. Only then is the mark and the budget
// written, in one write, and only when either is missing. A launch that still
// refuses takes the mark back, so the queue does not keep a start that never
// happened at its top.
func (h *Handler) startJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	if h.startRefused(w, r, it) {
		return
	}
	h.launchJob(w, r, it, "", nil)
}

// guardedStart is start_now's guard: the version read, as for every guarded
// verb, and the pipeline the person picked, "" for the steps the job has.
//
// A PICKED PIPELINE IS ONE THE REPOSITORY'S FILE LISTS, read from the file the
// start decides on the job ([Handler.jobPipeline]), and it replaces the steps
// of a job that never ran in the start's one write. pr and ci are not an
// issue job's to run, as at creation. A JOB QUEUED AND NOT YET TAKEN HAS NOT
// RUN ([run.Ran]): its pick replaces its steps and lays its run out again on
// them, and Start now lets it take a slot as it always does. Only a job that
// ran keeps its steps. Every refusal comes before any write.
func (h *Handler) guardedStart() jobAction {
	return func(w http.ResponseWriter, r *http.Request, it factory.Item) {
		var body struct {
			Expect   *api.Expect `json:"expect,omitempty"`
			Pipeline string      `json:"pipeline,omitempty"`
		}
		if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "start_now", body.Expect) {
			return
		}
		h.onlyOwner(func(w http.ResponseWriter, r *http.Request, it factory.Item) {
			if h.pickRefused(w, it, body.Pipeline) || h.startRefused(w, r, it) {
				return
			}
			h.launchJob(w, r, it, body.Pipeline, nil)
		})(w, r, it)
	}
}

// pickRefused answers every refusal of a picked pipeline that the file is not
// needed for, and true when it refused.
func (h *Handler) pickRefused(w http.ResponseWriter, it factory.Item, pick string) bool {
	switch {
	case pick == "":
		return false
	case !pipeline.KindNamed(pick):
		h.fail(w, 422, "pipeline must be a pipeline name of the repository's file")
	case pick == string(factory.KindPR) || pick == string(factory.KindCI):
		h.fail(w, 422, "factory jobs support the issue pipeline only")
	case run.Ran(it):
		h.refuse(w, 409, "This job has already run, so it keeps its steps.", "conflict", "", nil, factory.JobState(it))
	default:
		return !h.requireVerb(w, h.config.Pipeline != nil && h.config.Store != nil)
	}
	return true
}

// startRefused answers every refusal a start makes before it writes, and true
// when it refused.
func (h *Handler) startRefused(w http.ResponseWriter, r *http.Request, it factory.Item) bool {
	// A JOB WITH NO REPOSITORY IS REFUSED IN SO MANY WORDS, before anything
	// reads a checkout for it: a definite conflict, which the app shows as
	// the factory said it, never a failure that may have half-happened.
	if factory.NamesNoRepo(it) {
		h.refuse(w, 409, factory.NoRepoRefusal, "conflict", "", nil, factory.JobState(it))
		return true
	}
	if it.QKind == factory.QKindAskFirst {
		h.refuse(w, 409, "Approval needed: start", "conflict", "", nil, factory.JobState(it))
		return true
	}
	if strings.HasSuffix(r.URL.Path, "/start_now") && h.pinnedStartRefused(w, r, it) {
		return true
	}
	if h.startModelRefused(w, it) {
		return true
	}
	if h.endedOnGitHub(w, r, it) {
		return true
	}
	// A JOB THAT RUNS ELSEWHERE NEEDS NO CHECKOUT HERE (factory.RunsElsewhere).
	return !factory.RunsElsewhere(it) && h.needsCheckout(w, r, it.Repo, factory.JobState(it))
}

// launchJob is the start's one write and its launch, for a job startRefused
// has passed. pick is the pipeline the start runs instead of the job's own
// steps, "" for none ([Handler.guardedStart]). also is what else the start
// changes on the job in that same write (run_again's clearing of the run
// before it), nil for nothing.
// The result says whether launch accepted, even if recording its receipt failed;
// run-as-new must retain an accepted job when the receipt could not be written.
//
// START NOW ON A JOB ALREADY IN THE QUEUE IS NOT A SECOND LAUNCH: it lets the
// job take one slot above its computer's Jobs at once ([factory.Item.OverLimit]),
// lifts a hold in the same write, and launches nothing. A start from Backlog
// queues the job at the top and waits for a slot like any other.
func (h *Handler) launchJob(w http.ResponseWriter, r *http.Request, it factory.Item, pick string, also func(*factory.Item)) bool {
	mark, queued := false, false
	if st := h.config.Store; st != nil {
		cur, err := st.Get(it.ID)
		if h.failed(w, err) {
			return false
		}
		who, err := h.callerHandle(r)
		if h.failed(w, err) {
			return false
		}
		now := strings.HasSuffix(r.URL.Path, "/start_now")
		mark = now && !cur.Marked
		queued = now && cur.State == factory.StateQueued
		// THE RULES A JOB STARTS UNDER GO WITH IT: the repository's ask-first
		// and never lines with the factory's own, so the approval after its
		// plan is checked against them on whichever machine runs it. A
		// repository file that cannot be read does not stop a person's start:
		// the job takes the defaults with the factory's rules, is marked as
		// started with its rules unread, and its plan then waits for a person.
		// Only the factory's own rules unread refuse it.
		//
		// THEY ARE READ FROM THE JOB'S OWN FILE ([Handler.jobPipeline]): the
		// one it was first started under, or, for a job never started, the
		// factory's read now, which this start decides on the job for its life
		// ([factory.Item.DecidePipeline]) in the same write as its rules and
		// budget, so its steps, rules and budget come from one file.
		var rules *factory.StartRules
		var file *pipeline.File
		var fileErr error
		if h.config.Pipeline != nil && (!queued || pick != "") {
			f, err := h.jobPipeline(r.Context(), cur, "")
			if errors.Is(err, pipeline.ErrHouseRulesUnread) {
				h.fail(w, 503, "The factory's own rules could not be read, so this job cannot start yet. Try again in a moment.")
				return false
			}
			rules = pipeline.StartRulesOf(f)
			if err != nil {
				pipeline.MarkUnread(rules, err)
			}
			file, fileErr = &f, err
		}
		var picked []factory.Stage
		if pick != "" {
			// A PICK IS READ FROM THE FILE OR NOT AT ALL: a read that failed
			// refuses it, where a plain start would run on the defaults. The
			// name resolves the one way ([pipeline.File.Resolve]): the
			// repository's file, then the factory's library, then the
			// built-in steps.
			if file == nil || fileErr != nil {
				h.fail(w, 503, "The repository's pipeline file could not be read, so this job cannot start on a picked pipeline yet.")
				return false
			}
			if picked = file.Named(pick); len(picked) == 0 {
				h.fail(w, 422, "pipeline not found")
				return false
			}
		}
		var budget float64
		if file != nil && cur.Cap <= 0 {
			// An unread repository takes a bounded default. A clean read
			// must still supply its own positive budget.
			if fileErr != nil {
				budget = factory.DefaultJobBudget
			} else {
				budget, err = h.budgetOf(file.Recipe())
			}
		} else {
			budget, err = h.startBudget(r, cur)
		}
		if h.failed(w, err) {
			return false
		}
		if factory.StoppedByPerson(cur) || mark || queued || budget > 0 || also != nil || rules != nil || picked != nil || (cur.Starter == "" && who != "") {
			err := st.Update(it.ID, func(cur *factory.Item) error {
				factory.ClearPersonStop(cur)
				if also != nil {
					also(cur)
				}
				if picked != nil {
					if run.Ran(*cur) {
						return fmt.Errorf("%s has already run, so it keeps its steps", cur.Ref())
					}
					cur.Stages = factory.CopyStages(picked)
					// A queued job's run was laid out on its old steps.
					run.RelayTail(cur)
				}
				if file != nil {
					cur.DecidePipeline(file.Copy, h.clock())
				}
				if rules != nil && !queued {
					// A person's start judges nothing: what the factory's own
					// start found under older rules no longer speaks for it.
					cur.StartRules, cur.RuleHold = rules, ""
					if cur.RuleChecks != nil {
						cur.RuleChecks.Start = ""
					}
					if rules.Unread && !rules.Pending {
						cur.Note(h.clock(), pipeline.UnreadNote)
					}
				}
				if queued {
					if cur.State != factory.StateQueued {
						return fmt.Errorf("%s is no longer queued", cur.Ref())
					}
					cur.OverLimit = true
					if cur.Stream != nil {
						cur.Stream.Paused = false
					}
				}
				cur.Marked = cur.Marked || mark
				if cur.Starter == "" {
					cur.Starter = factory.NormalizeHandle(who)
				}
				if cur.Cap <= 0 {
					cur.Cap = budget
				}
				return nil
			})
			if h.failed(w, err) {
				return false
			}
		}
	}
	if queued {
		h.finish(w, h.recordAction(r, it, record.Started, startedOverLimit, func() error { return nil }))
		return true
	}
	started := false
	h.finish(w, h.recordAction(r, it, record.Started, "", func() error {
		err := h.seam.Launch(it.ID)
		started = err == nil
		if err != nil && mark {
			_ = h.config.Store.Update(it.ID, func(cur *factory.Item) error { cur.Marked = false; return nil })
		}
		return err
	}))
	return started
}

// startedOverLimit is the detail of a Start now that let a queued job take a
// slot above Jobs at once.
const startedOverLimit = "above Jobs at once"

// needsCheckout answers 409 needs_checkout and true when repo has no
// checkout on this factory's machine and cannot be cloned there
// ([factory.Seam.CheckoutOf]). A repository that is checked out, or that the
// runner will clone on the first step, passes; so does every repository on a
// factory whose seam does not know its machine's checkouts. state is the
// job's state for now_state, "" when there is no job yet.
func (h *Handler) needsCheckout(w http.ResponseWriter, r *http.Request, repo, state string) bool {
	if h.seam.CheckoutOf == nil {
		return false
	}
	if _, err := h.seam.CheckoutOf(r.Context(), repo); err != nil {
		status, reason := classify(err)
		h.refuse(w, status, err.Error(), reason, "", nil, state)
		return true
	}
	return false
}

// recheckWait bounds the one GitHub read a person's start makes. It is short
// because the person is waiting on the button, and a read that runs out
// starts the job anyway.
const recheckWait = 5 * time.Second

// endedOnGitHub is the check a person's start makes on the ONE item they chose
// (start_now, run, run_again), supplementing the poll's closure reconciliation.
// When GitHub says the
// issue or pull request was closed or merged, the job is finished the way a
// pull request that ended finishes one (done, DoneHow "closed" or "merged"),
// the factory records it, and the start is refused in one sentence.
//
// STARTING NEVER DEPENDS ON GITHUB BEING UP. A read that fails, times out, or
// has no token to read with answers nothing, and the job starts on what the
// floor already knows; the poll is still the source of every other fact.
func (h *Handler) endedOnGitHub(w http.ResponseWriter, r *http.Request, it factory.Item) bool {
	if h.config.Recheck == nil || h.config.Store == nil || it.Origin != factory.OriginForge || it.Num <= 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), recheckWait)
	answer, err := h.config.Recheck(ctx, it)
	cancel()
	if errors.Is(err, factory.ErrSourceIdentityChanged) || (err == nil && !answer.Matches(it)) {
		h.refuse(w, http.StatusConflict, fmt.Sprintf("#%d now identifies a different GitHub issue, so it wasn't started. The saved job is unchanged.", it.Num), "conflict", "", nil, factory.JobState(it))
		return true
	}
	how := answer.How
	if err != nil {
		return false
	}
	now := h.clock().UTC()
	state := factory.JobState(it)
	finished := false
	err = h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		state = factory.JobState(*cur)
		if !answer.Matches(*cur) {
			return factory.ErrSourceIdentityChanged
		}
		cur.GitHubIssueID = answer.IssueID
		if (how != factorygithub.StandingClosed && how != factorygithub.StandingMerged) || !factory.UntouchedIssue(*cur) {
			return nil
		}
		factory.TouchIssue(cur)
		finished = true
		state = api.StateDone
		cur.State = factory.StateShipped
		cur.DoneHow = how
		cur.DoneAt = now
		cur.Marked = false
		cur.Question = ""
		cur.QKind = ""
		cur.StopUndo = nil
		return nil
	})
	if errors.Is(err, factory.ErrSourceIdentityChanged) {
		h.refuse(w, http.StatusConflict, fmt.Sprintf("#%d now identifies a different GitHub issue, so it wasn't started. The saved job is unchanged.", it.Num), "conflict", "", nil, state)
		return true
	}
	if h.failed(w, err) {
		return true
	}
	if how != factorygithub.StandingClosed && how != factorygithub.StandingMerged {
		return false
	}
	if finished && h.config.Record != nil {
		if _, err := h.config.Record.Append(r.Context(), record.Event{Kind: record.SourceClosed, Verb: how, Job: it.ID, Detail: "on GitHub", Cost: jobCost(it)}); h.failed(w, err) {
			return true
		}
	}
	h.refuse(w, http.StatusConflict, fmt.Sprintf("#%d was %s on GitHub, so it wasn't started.", it.Num, how), "conflict", "", nil, state)
	return true
}

// ensureBudget takes the pipeline's own default, not a global task spending cap.
func (h *Handler) ensureBudget(r *http.Request, it factory.Item) error {
	budget, err := h.startBudget(r, it)
	if err != nil || budget <= 0 {
		return err
	}
	return h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		if cur.Cap <= 0 {
			cur.Cap = budget
		}
		return nil
	})
}

// startBudget is the budget a start gives a job that has none, read from its
// repository's recipe without writing it; 0 when the job already has one.
func (h *Handler) startBudget(r *http.Request, it factory.Item) (float64, error) {
	if it.Cap > 0 {
		return 0, nil
	}
	recipe, err := h.recipeOf(r, it)
	if err != nil {
		return 0, err
	}
	return h.budgetOf(recipe)
}

// budgetOf is the budget recipe gives a job that has none.
func (h *Handler) budgetOf(recipe factory.Recipe) (float64, error) {
	// A PIPELINE FILE'S BUDGET IS TAKEN AS IT SAYS, and one that says none is
	// refused below; a recipe read any other way falls back to the default.
	budget := recipe.DefaultBudget
	if h.config.Pipeline == nil && budget <= 0 {
		budget = factory.DefaultJobBudget
	}
	if budget <= 0 {
		return 0, errors.New("pipeline default_budget must be positive")
	}
	return budget, nil
}

// recipeOf is the job's recipe: the host's read of the job's own file when it
// reads pipelines ([Handler.jobPipeline]: the file its first start decided, or
// its repository's as it reads now before that), else the floor's, else the
// built-in recipe.
func (h *Handler) recipeOf(r *http.Request, it factory.Item) (factory.Recipe, error) {
	if h.config.Pipeline != nil {
		f, err := h.jobPipeline(r.Context(), it, "")
		if err != nil {
			return factory.Recipe{}, err
		}
		return f.Recipe(), nil
	}
	if h.seam.RecipeAt != nil {
		recipe, _, _, err := h.seam.RecipeAt(itemRepo(it))
		return recipe, err
	}
	return factory.DefaultRecipe(), nil
}

func (h *Handler) runAgain(w http.ResponseWriter, r *http.Request, it factory.Item) {
	if !factory.StoppedByPerson(it) && factory.JobDoneHow(it) != "closed" && it.Failure == nil {
		h.refuse(w, 409, "Run again requires failed work or a PR closed without merge", "conflict", "", nil, factory.JobState(it))
		return
	}
	// The start's refusals come before the run is cleared too, and the
	// clearing is the start's one write, so a run again that cannot start
	// leaves the failed job as the person read it.
	if h.startRefused(w, r, it) {
		return
	}
	h.launchJob(w, r, it, "", func(cur *factory.Item) {
		cur.State = factory.StateNew
		cur.DoneHow = ""
		cur.DoneAt = time.Time{}
		cur.PullRequest = nil
		cur.Approval = nil
		cur.Failure = nil
		// The last run's change check spoke for that change: the new run's
		// change is checked when it lands.
		if cur.RuleChecks != nil {
			cur.RuleChecks.Publish, cur.RuleChecks.PublishHead = "", ""
		}
	})
}

// restoreJob is POST /v1/jobs/{id}/restore, Bring back: the one way out of a
// drop. The job goes back to Backlog AS IF IT HAD NEVER RUN, and nothing
// starts; starting it is the person's own start. The drop threw its work on
// this machine away (its local branch and folder, its conversations and its
// crew), so its next run begins on a fresh branch, from the first step, with
// its lifetime money kept, and makes an agent and a crew of its own.
//
// FRESH MEANS FRESH (the owner's decision of 2026-10-19): no earlier
// conversation reaches the job again. Its agent's conversation, what the
// person said there and what the runner took from it, the person's notes to
// the steps, and every change made to its steps go, and the steps are the
// repository's recipe again. What the job is stays: its identity, the issue's
// own words, who it is assigned to, its priority, budget and model, and its
// activity, the history of what happened to it.
//
// WHAT IS STILL HERE GOES FIRST, WHILE THE JOB IS STILL DROPPED
// ([Handler.throwAway]): a folder or branch something re-created after the
// drop (an agent's write, a shell), and the conversations of a job dropped
// before a drop deleted them. Only then is the job reset, so a start that
// races the Bring back is refused as a dropped job's until nothing of the old
// run is left, and never launches a fresh run whose new branch, folder or
// conversation the delete would then take. THE JOB COMES BACK EVEN WHEN A
// DELETE FAILS: it no longer names anything of the old run, so nothing of it
// leaks into the fresh one, and what was kept is said on its activity.
//
// THE EARLIER PULL REQUEST IS FORGOTTEN, NOT CLOSED: nothing on GitHub is
// touched. Kept, the poll would go on writing that pull request's state onto
// a Backlog job and the job would be judged by a pull request its fresh run
// never made, while that run's branch no longer holds what the pull request
// shows. Forgotten, a fresh run cannot open a second pull request beside it by
// accident either: its push to the same branch name is refused while the old
// branch is still on GitHub, because a push never forces. The receipt names
// the earlier pull request, so it is not lost from the record.
func (h *Handler) restoreJob(w http.ResponseWriter, r *http.Request, it factory.Item) {

	if factory.JobDoneHow(it) != "dropped" {
		h.refuse(w, http.StatusConflict, "only a dropped job can be brought back", "conflict", "", nil, factory.JobState(it))
		return
	}
	// The recipe is read before anything changes, so a pipeline file that
	// cannot be read refuses the whole Bring back and the job stays dropped.
	// It is read as for a job never started: Bring back forgets the file the
	// job was decided on, and its next start decides afresh.
	fresh := it
	fresh.ForgetPipeline()
	recipe, err := h.recipeOf(r, fresh)
	if h.failed(w, err) {
		return
	}
	detail := ""
	if pr := it.PullRequest; pr != nil && pr.URL != "" {
		detail = "earlier pr · " + pr.URL
	}
	h.throwAway(it, "brought back")
	h.finish(w, h.recordAction(r, it, record.Restored, detail, func() error {
		return h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			forgetRun(cur, recipe)
			cur.Note(h.clock(), "Brought back to Backlog; earlier run cleared")
			return nil
		})
	}))
}

// forgetRun takes every trace of a run off the job: its state and ending, its
// steps, its question, failure, approval and stop, its
// proof, its change and the commits and machine it was made on, and what it
// published; and every trace of what was said about it: its conversations
// ([forgetChats]), the notes for its steps, and the changes made to its
// steps, which are the recipe's for its kind again, with the pipeline file
// it was decided on. Everything else on the job is the job's own and stays.
func forgetRun(it *factory.Item, recipe factory.Recipe) {
	forgetChats(it)
	it.State = factory.StateNew
	it.DoneHow, it.DoneAt = "", time.Time{}
	factory.ClearJobRun(it)
	it.Failure = nil
	it.Approval, it.StopUndo = nil, nil
	it.Question, it.QKind = "", ""
	it.Asking, it.AnsweredQuestions = nil, nil
	it.Proof, it.Policy = nil, nil
	it.PlannedFiles = nil
	it.Change, it.ChangeBase, it.HeadCommit = nil, "", ""
	it.WorktreeMachine = ""
	it.Publish, it.Publishing, it.PublishStatus, it.PublishError = "", false, nil, ""
	it.PullRequest, it.CommentURL, it.FollowUp = nil, "", nil
	it.MergePolicy, it.Merging = nil, nil
	it.Notes = nil
	it.Stages, it.Adapted = factory.CopyStages(recipe.For(it.Kind)), nil
	it.StartRules, it.RuleHold, it.RuleChecks = nil, "", nil
	// The pipeline file it was decided on goes too: its next start decides
	// afresh ([factory.Item.DecidePipeline]).
	it.ForgetPipeline()
}

// forgetChats takes every link to a conversation off the job: its agent's
// conversation, the thread replicated from it and what the runner heard
// there, its crew, and each step's and each round's chat with the step's
// address in that crew. The rest of its run stays as it was: a dropped job
// still shows which steps ran, what they spent and what they said.
//
// THE STREAM IS COPIED, NEVER CHANGED IN PLACE, so a copy of the job taken
// before (the one its conversations are deleted by) still names them.
func forgetChats(it *factory.Item) {
	it.Talk, it.Thread, it.Heard = "", nil, time.Time{}
	if it.Stream == nil {
		return
	}
	s := *it.Stream
	s.Room = ""
	s.Phases = slices.Clone(s.Phases)
	for i := range s.Phases {
		ph := &s.Phases[i]
		ph.Chat, ph.Handle = "", ""
		ph.Rounds = slices.Clone(ph.Rounds)
		for j := range ph.Rounds {
			ph.Rounds[j].Chat = ""
		}
	}
	it.Stream = &s
}

func (h *Handler) dismissJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "dismiss", body.Expect) {
		return
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	if who == "" {
		h.fail(w, 422, "Dismiss requires a GitHub login")
		return
	}
	h.finish(w, h.recordAction(r, it, record.Dismissed, "until GitHub changes", func() error {
		return h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			if !factory.IsDismissed(*cur, who) {
				cur.DismissedBy = append(cur.DismissedBy, who)
			}
			return nil
		})
	}))
}

func (h *Handler) assignToMe(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "assign_to_me", body.Expect) {
		return
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	if it.Person() != "" {
		h.taken(w, r, it)
		return
	}
	h.assignJob(w, r, it, who)
}

func (h *Handler) changeAssignee(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Assignee string      `json:"assignee"`
		Expect   *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, false) || !h.fresh(w, r, it, "change_assignee", body.Expect) || !h.assigneeOnly(w, r, it) {
		return
	}
	h.assignJob(w, r, it, factory.NormalizeHandle(body.Assignee))
}

// Reassignment belongs to the current assignee even while a job is in backlog.
func (h *Handler) assigneeOnly(w http.ResponseWriter, r *http.Request, it factory.Item) bool {
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return false
	}
	if it.Person() != "" && factory.NormalizeHandle(who) == it.Person() {
		return true
	}
	h.notJobPerson(w, it, it.Person())
	return false
}

func (h *Handler) assignJob(w http.ResponseWriter, r *http.Request, it factory.Item, who string) {
	if who == "" {
		h.fail(w, 422, "assignment requires a GitHub login")
		return
	}
	if h.config.ChangeAssignee != nil {
		if h.failed(w, h.config.ChangeAssignee(r.Context(), it, who)) {
			return
		}
	}
	if factory.JobState(it) == "running" || factory.JobState(it) == "queued" || factory.JobState(it) == "needs_you" {
		if h.failed(w, h.stopForChange(it.ID)) {
			return
		}
	}
	h.finish(w, h.recordAction(r, it, record.Assigned, "@"+who, func() error {
		return h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			cur.Assignee = who
			cur.Taker = ""
			cur.Starter = ""
			cur.StopUndo = nil
			cur.Approval = nil
			return nil
		})
	}))
}

func (h *Handler) unassignJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "unassign", body.Expect) || !h.assigneeOnly(w, r, it) {
		return
	}
	if h.config.ChangeAssignee != nil {
		if h.failed(w, h.config.ChangeAssignee(r.Context(), it, "")) {
			return
		}
	}
	if factory.JobState(it) == "running" || factory.JobState(it) == "queued" || factory.JobState(it) == "needs_you" {
		if h.failed(w, h.stopForChange(it.ID)) {
			return
		}
	}
	h.finish(w, h.recordAction(r, it, record.Unassigned, "", func() error {
		return h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			cur.Assignee = ""
			cur.Taker = ""
			cur.Starter = ""
			cur.State = factory.StateNew
			cur.Failure = nil
			cur.PublishError = ""
			cur.Approval = nil
			cur.StopUndo = nil
			return nil
		})
	}))
}

func (h *Handler) stopForChange(id int) error {
	if h.workers != nil && h.workers.desk != nil {
		return lease.NewDoors(h.workers.desk).Reset(id)
	}
	return h.seam.Stop(id)
}

// Nudge addresses the assignee in Activity; it never steers their AI run.
func (h *Handler) nudgeJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Words  string      `json:"words"`
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "nudge", body.Expect) {
		return
	}
	if it.Person() == "" {
		h.fail(w, 422, "Nudge requires an assignee")
		return
	}
	h.finish(w, h.recordAction(r, it, record.Nudged, "@"+it.Person()+" "+strings.TrimSpace(body.Words), func() error { return nil }))
}

func (h *Handler) setPriority(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Priority string      `json:"priority"`
		Expect   *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, false) || !h.fresh(w, r, it, "priority", body.Expect) {
		return
	}
	if !factory.ValidPriority(body.Priority) {
		h.fail(w, 422, "priority must be urgent, high, medium or low")
		return
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	if who == "" {
		h.fail(w, 422, "priority requires a GitHub login")
		return
	}
	h.finish(w, h.recordAction(r, it, record.PriorityChanged, body.Priority, func() error {
		return h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			cur.Priority = &factory.JobPriority{Value: body.Priority, SetBy: who}
			return nil
		})
	}))
}

func (h *Handler) raiseBudget(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Budget float64     `json:"budget"`
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, false) {
		return
	}
	if !slices.Contains(factory.JobBudgetSteps(), body.Budget) && body.Budget != factory.NextJobCap(it) {
		h.fail(w, 422, "choose a budget: 5, 10, 20, 30 USD or the job's next budget")
		return
	}
	var revision *int
	if body.Expect != nil {
		revision = body.Expect.Version
	}
	var budgetRefused error
	var atCap *factory.Item // the job as its cap question stood, when the raise resumes it
	err := h.recordAction(r, it, record.BudgetChanged, fmt.Sprintf("$%.2f", body.Budget), func() error {
		return h.config.Store.UpdateBudgetExpected(it.ID, revision, func(cur *factory.Item) error {
			if factory.JobState(*cur) == api.StateDone || cur.State == factory.StateDismissed {
				return factory.EditRefusal{Cause: errors.New("finished or dropped jobs cannot change budget")}
			}
			if body.Budget <= cur.Cap && body.Budget < factory.JobLifetimeSpent(*cur) {
				budgetRefused = fmt.Errorf("job total $%.2f has reached budget $%.2f; choose a budget above the job total", factory.JobLifetimeSpent(*cur), cur.Cap)
				return budgetRefused
			}
			cur.Cap = body.Budget
			if cur.QKind == "cap" && !cur.Publishing && body.Budget <= factory.JobLifetimeSpent(*cur) {
				cur.Question, _ = (*run.Money)(nil).CapQuestion(*cur)
				if cur.Asking != nil {
					cur.Asking.Question = cur.Question
				}
			}
			if capResumes(*cur) {
				asked := *cur
				atCap = &asked
			}
			return nil
		})
	})
	if budgetRefused != nil {
		h.fail(w, 422, budgetRefused.Error())
		return
	}
	// A competing budget edit needs a new decision, not the desktop's stale retry.
	var stale *store.StaleError
	if errors.As(err, &stale) {
		h.refuse(w, 409, "budget changed while you looked; read the job again", "conflict", "", nil, factory.JobState(it))
		return
	}
	if h.failed(w, err) {
		return
	}
	// A budget edit does not queue a published PR from stale observations.
	// Wake the read-only poll; its fresh snapshot releases spending-owned work.
	_ = h.config.Store.NudgePoll()
	// The runner hears the new cap before the continue, so it carries on
	// under the cap the person chose and never doubles the old one.
	if h.workers != nil && h.workers.desk != nil {
		if _, err = h.workers.desk.SignalIfHeld(it.ID, lease.Signal{Verb: lease.SignalBudget, Budget: body.Budget}); h.failed(w, err) {
			return
		}
	}
	if atCap != nil {
		err = h.resumeAtCap(r, *atCap)
	}
	h.finish(w, err)
}

// resumeAtCap answers the cap question the raise cleared with continue, as a
// person's answer does, and only then says on the timeline that the job
// carries on under its new budget.
func (h *Handler) resumeAtCap(r *http.Request, it factory.Item) error {
	if err := h.continueCap(r, it, factory.ChoiceContinue); err != nil {
		return err
	}
	return h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		run.NoteBudgetRaised(cur, h.now())
		return nil
	})
}

// capResumes is a raise that answers the open cap question on Auto or Manual
// when the new cap clears spend. Take over, publishing and other questions
// keep their own decisions; a cap at or under spend keeps the cap question.
// A person's Stop requires their explicit Start even after a raise.
func capResumes(it factory.Item) bool {
	asksCap := it.QKind == "cap" && (it.Asking == nil || it.Asking.Question == it.Question)
	return asksCap && it.Cap > factory.JobLifetimeSpent(it) && !it.Manual && !it.Publishing && !factory.StoppedByPerson(it)
}

// jobModelMost is the longest model id a job keeps.
const jobModelMost = 200

// setModel keeps the model a person chose for one job's steps ("" for the
// factory's default) and tells the worker running it, whose next step opens on
// it and whose running step switches at its next turn. A finished job has no
// step left to run on it.
//
// A MODEL PICK IS NEVER STALE. The body's expect is accepted and not checked:
// a model is a preference, not a decision on content the person reviewed, and
// it commutes with every other write on the job. Checking it failed the first
// pick on every job whose chat had just been opened, because opening the chat
// keeps the conversation on the item and moves its version under the page that
// had just read it. The one guard that stays is the job's state: Done refuses.
func (h *Handler) setModel(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Automatic *bool       `json:"automatic,omitempty"`
		Model     string      `json:"model"`
		Expect    *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, false) {
		return
	}
	model := strings.TrimSpace(body.Model)
	if len(model) > jobModelMost || strings.ContainsAny(model, " \t\r\n") {
		h.fail(w, http.StatusUnprocessableEntity, "model is a model id of at most 200 characters, with no spaces")
		return
	}
	if factory.JobState(it) == api.StateDone {
		h.refuse(w, http.StatusConflict, "job is already Done", "conflict", "", nil, api.StateDone)
		return
	}
	if err := h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		cur.Model = model
		if body.Automatic != nil {
			cur.AutomaticModels = body.Automatic
		}
		return nil
	}); h.failed(w, err) {
		return
	}
	var err error
	if h.workers != nil && h.workers.desk != nil {
		_, err = h.workers.desk.SignalIfHeld(it.ID, lease.Signal{Verb: lease.SignalModel, Words: model, Automatic: body.Automatic})
	}
	h.finish(w, err)
}

// Undo withdraws only this caller's pending decision, never another person's work.
func (h *Handler) undoJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "undo", body.Expect) {
		return
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	now := h.clock()
	err = h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		if a := cur.Approval; a != nil && a.State == "approving" && now.Before(a.UndoUntil) && a.DecidedBy == who {
			cur.Approval = nil
			return nil
		}
		if m := cur.Merging; m != nil && m.State == factory.MergeAsked && now.Before(m.UndoUntil) && m.By == who {
			cur.Merging = nil
			return nil
		}
		if u := cur.StopUndo; u != nil && now.Before(u.Until) && u.By == who && cur.State == factory.StateNew {
			// Undo restores the work snapshot, but receipts may have arrived
			// since Stop. Keep those dollars on the same run.
			restored := u.Stream
			if cur.Stream != nil {
				if restored == nil {
					cur.ChatSpent += cur.Stream.Spent
				} else {
					stream := *restored
					stream.Spent = max(stream.Spent, cur.Stream.Spent)
					restored = &stream
				}
			}
			factory.ClearPersonStop(cur)
			cur.Stream = restored
			cur.Failure = u.Failure
			cur.Question = u.Question
			cur.QKind = u.QKind
			cur.State = u.State
			if cur.State == factory.StateRunning {
				cur.State = factory.StateQueued
			}
			cur.StopUndo = nil
			return nil
		}
		return errors.New("job is not waiting for Undo")
	})
	if err == nil && h.config.Record != nil {
		_, err = h.config.Record.Append(r.Context(), record.Event{Kind: record.Undone, Job: it.ID, Handle: who})
	}
	h.finish(w, err)
}

// approve persists a five-second decision before any runner or publish door opens.
func (h *Handler) approve(w http.ResponseWriter, r *http.Request, it factory.Item) {
	if h.config.Store == nil {
		h.commitApproval(w, r, it)
		return
	}
	var body api.Approve
	if !h.decode(w, r, &body, true) || !h.commitFresh(w, it, body.Commit) || !h.fresh(w, r, it, "approve", body.Expect) || !h.approves(w, r, it) {
		return
	}
	a := factory.JobApproval(it)
	if a == nil && factory.NeedsKind(it) == factory.NeedsPublishFailed {
		data, _ := json.Marshal(body)
		r.Body = io.NopCloser(strings.NewReader(string(data)))
		h.commitApproval(w, r, it)
		return
	}
	if a == nil || a.State != "waiting" {
		h.notWaiting(w, r, it)
		return
	}
	if a.Kind == "proof" {
		if err := run.ApprovalRefused(&it, body.Edited); err != nil {
			h.refuse(w, 409, err.Error(), "conflict", "", nil, factory.JobState(it))
			return
		}
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	if who == "" && h.mode() == api.FactorySolo {
		h.fail(w, 422, "Sign in to GitHub before approving this job")
		return
	}
	if who == "" || (h.mode() != api.FactorySolo && it.Person() == "") {
		h.fail(w, 422, "approval requires an assignee")
		return
	}
	a.Automatic = false
	a.Error = ""
	a.State = "approving"
	a.DecidedBy = who
	a.At = h.clock().UTC()
	a.UndoUntil = a.At.Add(factory.ApprovalUndo)
	a.Commit = it.HeadCommit
	a.Edited = body.Edited
	a.Words = body.Words
	var pinned *factory.MergePolicy
	if a.Kind == "proof" {
		pinned = h.pinnedMerge(r, it)
	}
	if h.failed(w, h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		cur.Approval = a
		if a.Kind == "proof" {
			// APPROVING THE PROOF PUBLISHES, and the pull request it opens may
			// merge on its own only if the pipeline said so at this moment.
			cur.MergePolicy = pinned
		}
		return nil
	})) {
		return
	}
	h.scheduleJobTimer()
	h.write(w, 200, struct {
		Factory   api.Factory       `json:"factory"`
		Approval  *factory.Approval `json:"approval"`
		UndoUntil time.Time         `json:"undo_until"`
	}{h.factory, a, a.UndoUntil})
}

// settles counts the settles run: each one lists and decodes every job.
var settles atomic.Int64

// listFloor reads every job. A test may count those reads; the handler
// itself always uses the store.
func (h *Handler) listFloor() ([]factory.Item, error) {
	if h.config.Store == nil {
		return nil, nil
	}
	if h.floorList != nil {
		return h.floorList(h.config.Store)
	}
	return h.config.Store.ListShared()
}

// floorLook is one observation that nothing needed settling. sig names the
// documents that observation read; next is the earliest deadline that can
// come due without those documents changing. A zero next means there is none.
type floorLook struct {
	sig  string
	next time.Time
}

// settleJobs is run at deadlines, before reads and after a restart. Consent is
// locked across processes so two servers cannot open the same decision twice.
// An unchanged floor with nothing due is not decoded again: every factory
// request used to read every job first, and an idle runner asks often.
func (h *Handler) settleJobs(approvalsOnly ...bool) {
	if h.config.Store == nil {
		return
	}
	if h.floorUnchanged() {
		return
	}
	// One listing serves every scan for due work. Each scan only picks
	// candidates from it and reads every candidate again under its lock
	// before acting, so sharing it changes nothing but the number of times
	// every document on the floor is read before each request.
	// The signature is taken on both sides of the read. A job that changes
	// while the read is in progress must not be remembered as idle: the
	// signature would name the new files while the deadlines came from
	// the old ones.
	settles.Add(1)
	h.rememberFloor(nil, "")
	before := h.floorSignature()
	pending, err := h.listFloor()
	if err != nil {
		return
	}
	pending, err = h.prepareStepRecoveries(pending)
	if err != nil {
		return
	}
	pending, err = h.prepareAutomaticApprovals(pending)
	if err != nil {
		return
	}
	sweep := len(approvalsOnly) == 0 || !approvalsOnly[0]
	if err := h.settleMerges(sweep, pending); err != nil {
		return
	}
	due := false
	for _, it := range pending {
		if a := it.Approval; a != nil && a.State == "approving" && !h.clock().Before(a.UndoUntil) {
			due = true
			break
		}
	}
	if !due {
		// Nothing to settle, so the next request can skip the decode until
		// a document this look depends on changes or a deadline arrives.
		// A signature that moved during the read is left unremembered.
		if after := h.floorSignature(); sweep && after != "" && after == before {
			h.rememberFloor(pending, after)
		}
		return
	}
	unlock, err := h.lockConsent()
	if err != nil {
		return
	}
	defer unlock()
	items, err := h.listFloor()
	if err != nil {
		return
	}
	for _, it := range items {
		a := it.Approval
		if a == nil || a.State != "approving" || h.clock().Before(a.UndoUntil) {
			continue
		}
		if a.Automatic && !h.automaticApprovalAllowed(it) {
			// Mode, budget or the repository switch changed after the approval
			// was prepared. Hand it to the person instead of leaving it approving
			// past its time, which re-arms the job timer every millisecond.
			why := "Auto approval stopped: this repository is switched off"
			if err := factory.CheckAutomaticApproval(it); err != nil {
				why = err.Error()
			}
			_ = h.config.Store.Update(it.ID, func(cur *factory.Item) error {
				if cur.Approval == nil || !cur.Approval.At.Equal(a.At) || cur.Approval.State != "approving" {
					return nil
				}
				cp := *cur.Approval
				cp.State, cp.Error = "waiting", why
				cur.Approval = &cp
				return nil
			})
			continue
		}
		// A PERSON'S YES TO A HELD START IS THEIR START, as pressing Start is: a
		// job nobody is on becomes theirs before it launches, so Auto has an
		// owner at its first approval. Only a start approval does this; a yes to
		// a plan or a proof never makes anyone the job's person.
		if a.Kind == "start" && !a.Automatic && it.Person() == "" && a.DecidedBy != "" {
			starter := factory.NormalizeHandle(a.DecidedBy)
			if err := h.config.Store.Update(it.ID, func(cur *factory.Item) error {
				if cur.Person() == "" {
					cur.Starter = starter
				}
				return nil
			}); err == nil {
				it.Starter = starter
			}
		}
		// Ownership authorizes this replay; it is not the automatic decision's actor.
		handle := a.DecidedBy
		if a.Automatic {
			handle = it.Person()
		}
		// WHO IS PASSING IT goes on the job before the answer does, so the
		// write that passes the approve step keeps it on the step: the
		// manager for Auto's approval (with its reason when it judged a
		// condition), else the person who pressed.
		passing := &factory.Passed{By: factory.PassedByPerson, Handle: factory.NormalizeHandle(a.DecidedBy)}
		if a.Automatic {
			passing = &factory.Passed{By: factory.PassedByManager}
			if j, ok := factory.PassJudged(it); ok && j.Yes {
				passing.Why = j.Why
			}
		}
		_ = h.config.Store.Update(it.ID, func(cur *factory.Item) error { cur.Passing = passing; return nil })
		raw, _ := json.Marshal(api.Approve{Commit: a.Commit, Edited: a.Edited, Words: a.Words})
		r, _ := http.NewRequestWithContext(context.WithValue(context.Background(), workerKey{}, lease.Worker{Handle: handle}), http.MethodPost, "/v1/jobs/approve", strings.NewReader(string(raw)))
		r.SetPathValue("id", strconv.Itoa(it.ID))
		w := &replayWriter{header: make(http.Header)}
		h.commitApproval(w, r, it)
		_ = h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			if cur.Approval == nil || !cur.Approval.At.Equal(a.At) {
				return nil
			}
			cp := *cur.Approval
			if w.status() < 300 {
				cp.State = "approved"
			} else {
				cur.Passing = nil
				cp.State = "waiting"
				var refusal refusalBody
				_ = json.Unmarshal(w.body.Bytes(), &refusal)
				cp.Error = refusal.Error
			}
			cur.Approval = &cp
			return nil
		})
	}
}

// floorUnchanged reports that the previous idle look is still true.
// A runner heartbeat rewrites runners.json and must not count: that file
// is how presence is saved, and it changes while the jobs do not.
func (h *Handler) floorUnchanged() bool {
	h.jobsMu.Lock()
	kept := h.floorKept
	h.jobsMu.Unlock()
	if kept.sig == "" {
		return false
	}
	if !kept.next.IsZero() && !h.clock().Before(kept.next) {
		return false
	}
	return h.floorSignature() == kept.sig
}

func (h *Handler) rememberFloor(items []factory.Item, sig string) {
	h.jobsMu.Lock()
	defer h.jobsMu.Unlock()
	if sig == "" {
		h.floorKept = floorLook{}
		return
	}
	h.floorKept = floorLook{sig: sig, next: floorNext(items)}
}

// floorSignature is the identity of every JSON document settle consults.
// runners.json is omitted on purpose. Presence writes it on each request,
// and treating that as a job change would decode the floor for every heartbeat.
func (h *Handler) floorSignature() string {
	root := h.config.Store.Root()
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == "runners.json" || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".lock") || !strings.HasSuffix(name, ".json") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			return ""
		}
		fmt.Fprintf(&b, "%s %s\n", name, store.FileStamp(info))
	}
	return b.String()
}

// floorNext is the earliest moment a saved deadline can come due on this
// set of jobs. It is zero when only a document change can make work due.
func floorNext(items []factory.Item) time.Time {
	var next time.Time
	consider := func(at time.Time) {
		if at.IsZero() {
			return
		}
		if next.IsZero() || at.Before(next) {
			next = at
		}
	}
	for _, it := range items {
		if rec := it.Recovery; rec != nil && (rec.State == "waiting" || rec.State == "queued") {
			at := rec.Deadline
			if rec.State == "waiting" {
				at = rec.NextAt
			}
			consider(at)
		}
		if a := it.Approval; a != nil && a.State == "approving" {
			consider(a.UndoUntil)
		}
		if m := it.Merging; m != nil && m.State == factory.MergeAsked {
			consider(m.UndoUntil)
		}
	}
	return next
}

func (h *Handler) scheduleJobTimer() {
	if h.config.Store == nil {
		return
	}
	// The timer only reads deadlines, so the shared listing serves it.
	items, err := h.config.Store.ListShared()
	if err != nil {
		return
	}
	var next time.Time
	for _, it := range items {
		if rec := it.Recovery; rec != nil && (rec.State == "waiting" || rec.State == "queued") {
			at := rec.Deadline
			if rec.State == "waiting" {
				at = rec.NextAt
			}
			if !at.After(h.clock()) {
				at = h.clock().Add(2 * time.Second)
			}
			if next.IsZero() || at.Before(next) {
				next = at
			}
		}
		if a := it.Approval; a != nil && a.State == "approving" && (next.IsZero() || a.UndoUntil.Before(next)) {
			next = a.UndoUntil
		}
		if m := it.Merging; m != nil && m.State == factory.MergeAsked && (next.IsZero() || m.UndoUntil.Before(next)) {
			next = m.UndoUntil
		}
	}
	h.jobsMu.Lock()
	defer h.jobsMu.Unlock()
	if h.jobsClosed {
		return
	}
	if h.jobsTimer != nil {
		h.jobsTimer.Stop()
	}
	if !next.IsZero() {
		h.jobsTimer = time.AfterFunc(max(next.Sub(h.clock()), time.Millisecond), h.runJobTimer)
	}
}

func approvalWords(it factory.Item) string {
	if factory.ReadinessPending(it) {
		return "Mark the existing draft PR ready for review; continue monitoring. Merge remains my decision."
	}
	if it.Approval != nil && it.Approval.Words != "" {
		return it.Approval.Words
	}
	return "yes"
}

// A plain question is answered once. The second writer receives the exact
// collision copy, even when they did not include a document revision.
func (h *Handler) questionCollision(w http.ResponseWriter, r *http.Request, it factory.Item, id string, exp *api.Expect) bool {
	if len(it.AnsweredQuestions) == 0 {
		return false
	}
	q := it.AnsweredQuestions[len(it.AnsweredQuestions)-1]
	if q.Answer == nil || q.Kind == factory.QKindApprove || q.Kind == factory.QKindAskFirst {
		return false
	}
	if it.Question != "" && (id == "" || id != fmt.Sprintf("q%d", it.ID)) && (exp == nil || exp.Version == nil || *exp.Version == h.withVersion(api.FromItem(it)).Version) {
		return false
	}
	elapsed := max(0, int(h.clock().Sub(q.Answer.At).Seconds()))
	h.refuse(w, 409, fmt.Sprintf("@%s answered this %ds ago. Your answer wasn't sent.", q.Answer.By, elapsed), "already_answered", "@"+q.Answer.By, &q.Answer.At, factory.JobState(it))
	return true
}

// Closing waits for an already fired deadline to finish. A stopped timer alone
// cannot prevent its callback from writing after the service has shut down.
func (h *Handler) runJobTimer() {
	h.jobsMu.Lock()
	if h.jobsClosed {
		h.jobsMu.Unlock()
		return
	}
	h.jobsBusy.Add(1)
	h.jobsMu.Unlock()
	defer h.jobsBusy.Done()
	h.settleJobs()
	h.scheduleJobTimer()
}

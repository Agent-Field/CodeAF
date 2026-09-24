//go:build !windows

package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/seniordev/baked"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/project"
	"github.com/Agent-Field/codeaf/internal/seniordev/tool"
)

// The solo pipeline is one continuous coding context surrounded by small
// deterministic stages that do not think:
//
//	intake (deterministic) -> ONE model context: explore, pin, implement,
//	conform, submit -> freeze (inside the submit tool) -> bounded verification
//	-> ship.
//
// The stages here own only what a model should not have to remember: writing
// the spec down verbatim, capturing the candidate the instant it is submitted,
// refusing to ship something worse than what was captured, and emitting one
// terminal event that says why the run ended.

// soloMaxNudges bounds the continuations offered to a run that stops without
// submitting. Two is enough for "you forgot" and "here is what is actually
// wrong"; a third is the model arguing with the runner.
const (
	soloMaxNudges              = 2
	soloMaxToolLeakCorrections = 2
)

// soloMaxRecoveryRetries bounds fresh turns offered after a transient
// provider or transport failure. This is the only model-call retry in senior-dev:
// re-entering the persisted session preserves completed work without silently
// replaying a partial streamed response. Without it one dropped stream is a
// lost run.
const soloMaxRecoveryRetries = 3

var errSoloLanding = errors.New("solo landing window reached")

const (
	soloLandingReserveCap  = 12 * time.Minute
	soloCheckTimeout       = 2 * time.Minute
	soloLandingTurnTimeout = 5 * time.Minute
	soloFinalCheckTimeout  = 3 * time.Minute
)

// soloOutcome is what the run decided, separated into what the model claimed
// and what senior-dev independently observed. Keeping the two apart is the
// point: a run that says "all tests pass" and did not run them must leave both
// facts in the event stream rather than one reconciled story.
type soloOutcome struct {
	Status           string
	SubmissionReason string
	Frozen           *frozenCandidate
	Verification     *projectVerificationResult
	Nudges           int
	LandingTurns     int
	TerminalTrigger  string
	RestoreSource    string
	LiveTree         string
	FinalTree        string
	SuiteDead        bool

	// TerminalData and TerminalReason are what the run has to say about how it
	// ended. They travel to the CLI layer rather than being emitted here so the
	// run emits exactly one terminal event; see soloTerminal.
	TerminalData   map[string]any
	TerminalReason string
}

// frozenCandidate is the artifact of record. It is captured inside the submit
// tool call, so by the time the model's next step runs this already exists and
// nothing it does can reach what ships.
type frozenCandidate struct {
	// CommitSHA is a real commit object holding the whole tree (tracked and
	// untracked, ignored files excluded), written through a temporary index so
	// the working tree and the real index are never touched.
	CommitSHA string
	TreeSHA   string
	// PatchBytes and PatchFiles describe the diff against the run's base, and
	// exist so a later restore decision can be logged in terms a human reads.
	PatchBytes int
	PatchFiles int
	PatchSHA   string
	At         time.Time

	// The model's own claim, recorded verbatim and never reconciled with what
	// senior-dev later observes.
	Reason             string
	Evidence           string
	ChecklistSatisfied bool
}

func (candidate *frozenCandidate) describe() string {
	if candidate == nil {
		return "nothing frozen"
	}
	return fmt.Sprintf(
		"%d bytes across %d file(s), tree %s",
		candidate.PatchBytes, candidate.PatchFiles, shortSHA(candidate.TreeSHA),
	)
}

func shortSHA(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

// soloState carries the run's mutable freeze across the tool boundary. The
// mutex exists because the submit tool executes on the step loop's goroutine
// while the stage machine reads the result on its own.
type soloState struct {
	mu       sync.Mutex
	frozen   *frozenCandidate
	start    *soloCheckpoint
	coherent *soloCheckpoint
	baseSHA  string
}

type soloCheckpoint struct {
	CommitSHA string
	TreeSHA   string
	Source    string
}

func (state *soloState) candidate() *frozenCandidate {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.frozen
}

func (state *soloState) freeze(candidate frozenCandidate) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.frozen = &candidate
}

func (state *soloState) checkpoints() (*soloCheckpoint, *soloCheckpoint) {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.start, state.coherent
}

func (state *soloState) setStart(checkpoint soloCheckpoint) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.start = &checkpoint
}

func (state *soloState) setCoherent(checkpoint soloCheckpoint) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.coherent = &checkpoint
}

// runSolo executes the whole simplified pipeline for one request.
func (runner *pipeline) runSolo(
	ctx context.Context, goal, baseSHA string,
) (soloOutcome, error) {
	state := &soloState{baseSHA: baseSHA}
	outcome := soloOutcome{Status: "fail"}

	if err := runner.soloIntake(goal); err != nil {
		return outcome, err
	}
	if err := runner.soloCaptureStart(state); err != nil {
		runner.note("[senior-dev] landing: could not capture the exact starting tree: " + err.Error() + "\n")
	}
	// Installed before the first turn so the tool is advertised, and left
	// installed afterwards so a late submit during a nudge still freezes.
	runner.runtime.registry.SetSubmitFreezer(
		func(submitCtx context.Context, submission tool.Submission) (string, error) {
			return runner.soloFreezeWithContext(submitCtx, state, submission)
		},
	)

	// Ship runs on EVERY ending, the wall-clock kill included. Returning early
	// on a converse error would skip both halves of stage 4 on the common
	// ending of a full-budget run: no restore, so a run that submitted and then
	// kept editing would ship the post-submission tree; and no terminal, so the
	// run could not say whether it had submitted at all.
	converseErr := runner.soloConverse(ctx, goal, state, &outcome)
	runner.soloShip(ctx, state, &outcome, converseErr)
	return outcome, converseErr
}

// soloIntake is stage 0. It writes the request down verbatim and nothing else.
// The spec travels to every later stage as a file rather than as a paraphrase:
// a restated request loses the exact identifiers the original names.
func (runner *pipeline) soloIntake(goal string) error {
	directory := filepath.Join(runner.workspace, ".senior-dev")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("solo intake: %w", err)
	}
	specPath := filepath.Join(directory, "spec.md")
	if err := os.WriteFile(specPath, []byte(goal), 0o644); err != nil {
		return fmt.Errorf("solo intake: %w", err)
	}
	runner.events.stage("intake", "captured", map[string]any{
		"spec_path": ".senior-dev/spec.md", "spec_bytes": len(goal),
	})
	runner.note("[senior-dev] intake: request captured verbatim at .senior-dev/spec.md\n")
	return nil
}

// soloConverse runs the single coding context, plus bounded corrections for a
// real stop or provider markup that failed to execute as a tool call.
func (runner *pipeline) soloConverse(
	ctx context.Context, goal string, state *soloState, outcome *soloOutcome,
) error {
	workCtx, cancel := runner.soloWorkContext(ctx)
	defer cancel()
	prompt, err := adaptSoloPrompt(
		runner.recorder,
		buildSoloPrompt(goal, runner.readPinnedCommand(), ".senior-dev/checklist.md"),
	)
	if err != nil {
		return err
	}
	leakCorrections := 0
	recoveryRetries := 0
	for attempt := 0; ; {
		runner.events.stage("implement", "running", map[string]any{"attempt": attempt})
		response, err := runner.soloTurn(workCtx, goal, prompt)
		if err != nil {
			// A turn that errored may still have submitted before it died; the
			// freeze is what decides, not the error.
			if state.candidate() == nil {
				if errors.Is(context.Cause(workCtx), errSoloLanding) {
					outcome.TerminalTrigger = "landing-window"
					return runner.soloLandingTurn(ctx, goal, state, outcome)
				}
				if retryInfo, transient := transientTurnError(err); transient &&
					recoveryRetries < soloMaxRecoveryRetries && workCtx.Err() == nil {
					recoveryRetries++
					delay := soloRecoveryDelay(recoveryRetries)
					data := map[string]any{
						"attempt": attempt, "retry": recoveryRetries,
						"max_retries": soloMaxRecoveryRetries,
						"delay_ms":    delay.Milliseconds(),
						"class":       retryInfo.Class,
						"error":       err.Error(),
					}
					if retryInfo.StatusCode != nil {
						data["http_status"] = *retryInfo.StatusCode
					}
					if retryInfo.ProviderCode != "" {
						data["provider_code"] = retryInfo.ProviderCode
					}
					runner.events.stage("implement", "transport-retry", data)
					runner.note(fmt.Sprintf(
						"[senior-dev] implement: turn died on a transient provider failure (%s); retry %d/%d\n",
						retryInfo.Class, recoveryRetries, soloMaxRecoveryRetries,
					))
					// A refused sleep means the work window closed during the
					// wait; the next turn fails fast and lands above.
					_ = runner.sleep(workCtx, delay)
					prompt = soloRecoveryPrompt()
					continue
				}
				// A dead turn is not a dead run: offer the landing turn so the
				// tree that exists still gets independent verification and one
				// bounded chance to submit. If nothing lands, the original
				// error stands -- the trigger and the exit stay honest.
				runner.events.stage("implement", "turn-error", map[string]any{
					"attempt": attempt, "transport_retries": recoveryRetries,
					"error": err.Error(),
				})
				if landErr := runner.soloLandingTurn(ctx, goal, state, outcome); landErr != nil {
					runner.note("[senior-dev] implement: landing after a turn error also failed: " +
						landErr.Error() + "\n")
				}
				if state.candidate() != nil {
					return nil
				}
				outcome.TerminalTrigger = "turn-error"
				return err
			}
			runner.note("[senior-dev] implement: turn ended with an error after submitting: " +
				err.Error() + "\n")
		}
		if candidate := state.candidate(); candidate != nil {
			outcome.TerminalTrigger = "submitted"
			outcome.SubmissionReason = candidate.Reason
			runner.events.stage("implement", "submitted", map[string]any{
				"attempt": attempt, "reason": candidate.Reason,
				"checklist_satisfied": candidate.ChecklistSatisfied,
			})
			return nil
		}
		if leakedToolCall(response) && leakCorrections < soloMaxToolLeakCorrections {
			leakCorrections++
			runner.events.stage("implement", "tool-call-leak", map[string]any{
				"attempt": attempt, "correction": leakCorrections,
			})
			prompt = soloToolLeakPrompt()
			continue
		}
		findings := runner.soloUnsubmittedFindings(state.baseSHA)
		verification := runner.soloCheckUnsubmitted(
			workCtx, state, soloCheckTimeout, "nudge",
		)
		if verification != nil {
			outcome.Verification = verification
			findings = append(findings, soloVerificationFindings(*verification)...)
		}
		if attempt >= soloMaxNudges || runner.budgetIsExhausted() {
			if runner.budgetIsExhausted() {
				outcome.TerminalTrigger = "budget"
			} else {
				outcome.TerminalTrigger = "nudge-cap"
			}
			runner.events.stage("implement", "unsubmitted", map[string]any{
				"attempt": attempt, "budget_exhausted": runner.budgetIsExhausted(),
			})
			runner.note("[senior-dev] implement: run ended without a submission\n")
			return nil
		}
		outcome.Nudges = attempt + 1
		prompt = soloNudge(attempt+1, findings)
		attempt++
	}
}

func (runner *pipeline) soloTurn(ctx context.Context, goal, prompt string) (turnResult, error) {
	if runner.turnForTest != nil {
		return runner.turnForTest(ctx, goal, prompt)
	}
	markdown, ok := baked.GetBakedAgent("coder")
	if !ok {
		return turnResult{}, errors.New("solo: the coder agent is not available")
	}
	markdown, err := adaptCoderPrompt(runner.recorder, markdown)
	if err != nil {
		return turnResult{}, err
	}
	providerID, modelID := splitModelID(firstModel(runner.pool.high))
	ctx = project.WithContext(ctx, project.InstanceContext{
		Directory: runner.workspace, Worktree: runner.workspace,
		Project: project.Info{Worktree: runner.workspace},
	})
	configured, err := runner.runtime.configureTurn(turn{
		SessionID: runner.sessionID, SessionTitle: prefixUTF16(goal, 60),
		Agent: "coder", AgentMarkdown: markdown, Workspace: runner.workspace,
		ProviderID: providerID, ModelID: modelID, Prompt: prompt,
	})
	if err != nil {
		return turnResult{}, err
	}
	configured.Tools = runner.runtime.definitionsFor(
		configured.ProviderID, configured.ModelID, "coder", nil,
	)
	configured.Execute = runner.runtime.registry.Execute
	configured.SystemInstructions = runner.runtime.registry.SystemInstructions(ctx)
	configured.LoadInstructions = runner.runtime.registry.SystemInstructions
	configured.AfterAssistant = runner.runtime.registry.ClearInstructionClaims
	response, err := runner.runtime.runTurn(ctx, configured)
	runner.runtime.addCost(response.CostUSD)
	return response, err
}

type turnRetryInfo struct {
	Class        string
	StatusCode   *uint64
	ProviderCode string
}

// transientTurnError recognizes only failures for which a fresh model turn is
// useful. Structured provider data wins; the string table is a fallback for
// transports that expose only Error(). Context endings and permanent account,
// request, and configuration failures never retry.
func transientTurnError(err error) (turnRetryInfo, bool) {
	if err == nil || errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return turnRetryInfo{}, false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return turnRetryInfo{Class: "unexpected-eof"}, true
	}

	text := strings.ToLower(err.Error())
	var failure *modelTurnError
	if errors.As(err, &failure) {
		text += " " + strings.ToLower(failure.responseBody)
		if permanentProviderLimit(text) || failure.kind == msgmodel.ErrNameContextOverflow {
			return turnRetryInfo{}, false
		}
		providerCode := providerErrorType(failure.responseBody)
		info := turnRetryInfo{StatusCode: failure.statusCode, ProviderCode: providerCode}
		if failure.statusCode != nil {
			switch status := *failure.statusCode; {
			case status == 408:
				info.Class = "request-timeout"
			case status == 409:
				info.Class = "provider-conflict"
			case status == 429:
				info.Class = "rate-limit"
			case status >= 500:
				info.Class = "provider-5xx"
			default:
				return turnRetryInfo{}, false
			}
			return info, true
		}
		if providerCode == "provider_unavailable" {
			info.Class = "provider-unavailable"
			return info, true
		}
		if failure.retryable {
			info.Class = "provider-retryable"
			return info, true
		}
	}

	for _, transport := range []struct{ needle, class string }{
		{"sse read timed out", "sse-read-timeout"},
		{"the operation timed out", "request-timeout"},
		{"unexpected eof", "unexpected-eof"},
		{"connection reset", "connection-reset"},
		{"broken pipe", "broken-pipe"},
		{"connection refused", "connection-refused"},
		{"tls handshake timeout", "tls-handshake-timeout"},
		{"server closed idle connection", "idle-connection-closed"},
		{"http2: server sent goaway", "http2-goaway"},
		{"i/o timeout", "io-timeout"},
		{"network error", "network-error"},
		{"connection error", "connection-error"},
		{"connection lost", "connection-lost"},
		{"other side closed", "connection-closed"},
		{"fetch failed", "fetch-failed"},
		{"getaddrinfo", "dns-failure"},
		{"enotfound", "dns-failure"},
		{"eai_again", "dns-failure"},
		{"upstream connect", "upstream-connect"},
		{"reset before headers", "connection-reset"},
		{"socket hang up", "socket-hangup"},
		{"socket connection was closed", "socket-closed"},
		{"stream ended before", "stream-ended"},
		{"ended without", "stream-ended"},
		{"provider_unavailable", "provider-unavailable"},
		{"provider unavailable", "provider-unavailable"},
		{"service unavailable", "provider-unavailable"},
		{"overloaded", "provider-overloaded"},
		{"rate limit", "rate-limit"},
		{"too many requests", "rate-limit"},
		{"retry after", "provider-retry-requested"},
		{"you can retry your request", "provider-retry-requested"},
		{"please retry", "provider-retry-requested"},
		{"try your request again", "provider-retry-requested"},
	} {
		if strings.Contains(text, transport.needle) {
			return turnRetryInfo{Class: transport.class}, true
		}
	}
	return turnRetryInfo{}, false
}

func permanentProviderLimit(text string) bool {
	for _, phrase := range []string{
		"gousagelimiterror", "freeusagelimiterror", "monthly usage limit reached",
		"available balance", "insufficient_quota", "out of budget", "quota exceeded",
		"billing",
	} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func providerErrorType(body string) string {
	if body == "" {
		return ""
	}
	type metadata struct {
		ErrorType string `json:"error_type"`
	}
	var value struct {
		Metadata metadata `json:"metadata"`
		Error    *struct {
			Metadata metadata `json:"metadata"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(body), &value) != nil {
		return ""
	}
	if value.Metadata.ErrorType != "" {
		return strings.ToLower(value.Metadata.ErrorType)
	}
	if value.Error != nil {
		return strings.ToLower(value.Error.Metadata.ErrorType)
	}
	return ""
}

// soloRecoveryDelay escalates 5s, 15s, 45s: long enough for a proxy or
// provider blip to clear, short against a landing reserve measured in minutes.
func soloRecoveryDelay(retry int) time.Duration {
	delay := 5 * time.Second
	for i := 1; i < retry; i++ {
		delay *= 3
	}
	return delay
}

func soloRecoveryPrompt() string {
	return "The previous turn failed and is not in context. Its completed tool effects remain " +
		"in the workspace. Inspect the diff and continue the original request."
}

func leakedToolCall(result turnResult) bool {
	for _, part := range result.Parts {
		if part.Type == "tool" {
			return false
		}
	}
	text := strings.ToLower(result.Text)
	if !strings.Contains(text, "dsml") {
		return false
	}
	for _, toolName := range []string{"bash", "read", "write", "edit", "grep", "glob"} {
		if strings.Contains(text, toolName) {
			return true
		}
	}
	return false
}

// soloUnsubmittedFindings is what senior-dev can say about the tree without
// asking the model. A nudge carrying facts beats a nudge carrying
// encouragement: the usual cause of a missing submit is not sloth but a model
// that believes it is finished and is wrong about one mechanical thing.
func (runner *pipeline) soloUnsubmittedFindings(baseSHA string) []string {
	var findings []string
	// Whether the tree differs from the base is the first thing to say, and it
	// is not the same question as whether git status is clean: a run can have
	// committed everything and still have changed nothing that matters.
	if change, err := runner.soloTreeChange(baseSHA); err == nil {
		if !change.changed {
			findings = append(findings,
				"the tree is byte-identical to the starting commit — "+
					"nothing has been implemented, so there is nothing to submit")
		} else {
			findings = append(findings, fmt.Sprintf(
				"the tree differs from the starting commit in %d file(s)", change.files))
		}
	}
	// Only the git recorder has an index to be unclean, and only it can act on
	// the advice. Under --in-place nothing commits, so the finding would send
	// the model after a step it cannot take.
	if git, ok := runner.recorder.(*gitRecorder); ok {
		findings = append(findings, git.statusFindings()...)
	}
	if pinned := runner.readPinnedCommand(); pinned == "" {
		findings = append(findings,
			"no pinned command was recorded in .senior-dev/pinned.txt — "+
				"you have no reproducible way to show the work passes")
	} else {
		findings = append(findings, "your pinned command is: "+pinned)
	}
	if _, err := os.Stat(filepath.Join(runner.workspace, ".senior-dev", "checklist.md")); err != nil {
		findings = append(findings,
			"no .senior-dev/checklist.md exists — the request's own requirements were never enumerated")
	}
	return findings
}

func plural(count int, singular, many string) string {
	if count == 1 {
		return singular
	}
	return many
}

func (runner *pipeline) readPinnedCommand() string {
	data, err := os.ReadFile(filepath.Join(runner.workspace, ".senior-dev", "pinned.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(strings.TrimSpace(string(data)), "\n", 2)[0])
}

// soloFreezeWithContext captures the candidate. It runs inside the submit tool
// call, and its refusals are the cheapest place in the whole run to catch an
// empty or debris-laden patch: the same defects found after the run cost
// everything.
func (runner *pipeline) soloFreezeWithContext(
	ctx context.Context, state *soloState, submission tool.Submission,
) (string, error) {
	existing := state.candidate()
	if existing != nil {
		return "", runner.soloRefuseSubmit("already-submitted", fmt.Errorf(
			"this run already submitted at %s (%s); the frozen tree is the answer and cannot be replaced",
			existing.At.Format(time.RFC3339), existing.describe(),
		))
	}
	change, err := runner.soloTreeChange(state.baseSHA)
	if err != nil {
		return "", runner.soloRefuseSubmit("capture-error",
			fmt.Errorf("could not capture the tree: %w", err))
	}
	if !change.changed {
		return "", runner.soloRefuseSubmit("empty-tree", errors.New(
			"the working tree is identical to the base commit — there is nothing to submit"))
	}
	checklist := runner.soloChecklistState()
	if !checklist.present {
		return "", runner.soloRefuseSubmit("no-checklist", errors.New(
			"no .senior-dev/checklist.md exists — the request's own requirements were never "+
				"enumerated, so there is nothing to have checked the work against. "+
				"Write it, verify against it, then submit again"))
	}
	treeSHA, patch := change.treeSHA, change.patch
	commitSHA, err := runner.soloCommitTree(treeSHA, submission.Reason)
	if err != nil {
		return "", runner.soloRefuseSubmit("record-error",
			fmt.Errorf("could not record the tree: %w", err))
	}
	digest := sha256.Sum256([]byte(patch))
	candidate := frozenCandidate{
		CommitSHA: commitSHA, TreeSHA: treeSHA,
		PatchBytes: len(patch), PatchFiles: change.files,
		PatchSHA: fmt.Sprintf("%x", digest[:8]), At: runner.now(),
		Reason: submission.Reason, Evidence: submission.Evidence,
		ChecklistSatisfied: submission.ChecklistSatisfied,
	}
	state.freeze(candidate)
	// checklist_satisfied is the model's CLAIM; checklist_items/checklist_ticked
	// are what the file actually says. They are recorded side by side and never
	// reconciled: models routinely claim satisfaction without ticking a box, so
	// gating on the ticks would refuse most submissions. The one refusal with
	// evidence behind it is no checklist at all.
	runner.events.stage(frozenStage, frozenStatus, map[string]any{
		"reason": submission.Reason, "evidence": submission.Evidence,
		"checklist_satisfied": submission.ChecklistSatisfied,
		"checklist_items":     checklist.items,
		"checklist_ticked":    checklist.ticked,
		"patch_bytes":         candidate.PatchBytes, "patch_files": candidate.PatchFiles,
		"tree_sha": treeSHA, "commit_sha": commitSHA,
	})
	runner.note("[senior-dev] submit: candidate frozen — " + candidate.describe() + "\n")
	return candidate.describe(), nil
}

// soloRefuseSubmit makes a submit refusal countable. A refusal that travels
// only as tool-call error text is reconstructable from the message stream by
// callID and from nothing else; a refusal the event stream cannot count cannot
// be diagnosed.
func (runner *pipeline) soloRefuseSubmit(class string, err error) error {
	runner.events.stage("submit", "refused", map[string]any{
		"reason_class": class, "detail": err.Error(),
	})
	return err
}

// soloCommitTree writes a commit object for an already-written tree without
// moving HEAD, the index, or the working tree. The commit exists so the
// candidate can be restored later by a single git command even if the run dies
// between here and finalize.
func (runner *pipeline) soloCommitTree(treeSHA, reason string) (string, error) {
	message := "senior-dev: submitted candidate"
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		message += "\n\n" + trimmed
	}
	commitSHA, err := runner.soloRecordTree(treeSHA, message)
	if err != nil {
		return "", err
	}
	if err := runner.recorder.Publish(soloFrozenRef, commitSHA); err != nil {
		// The ref is a convenience for a restore from outside this process; losing it does not
		// invalidate the freeze, which is already a durable commit object.
		runner.note("[senior-dev] submit: could not update " + soloFrozenRef + ": " + err.Error() + "\n")
	}
	return commitSHA, nil
}

func (runner *pipeline) soloRecordTree(treeSHA, message string) (string, error) {
	return runner.recorder.Record(treeSHA, message)
}

// soloFrozenRef makes the frozen candidate reachable from outside this process,
// so a hard kill between submit and finalize still has something to restore.
const soloFrozenRef = "refs/senior-dev/submitted"

// soloTreeChange describes the whole working tree against the run's base.
type soloTreeChange struct {
	treeSHA string
	patch   string
	files   int
	changed bool
}

// seniorDevArtifactPathspecs exclude the run artifacts senior-dev itself writes into
// the workspace -- the session database, spec.md, the checklist, the pinned
// command -- from the answer. Without the exclusion, submit would accept a
// tree whose only change is senior-dev's own bookkeeping and the run would ship
// nothing while reporting success.
var seniorDevArtifactPathspecs = []string{
	":(exclude).senior-dev",
}

// soloChecklistState reports what .senior-dev/checklist.md actually contains, as
// distinct from what the model says about it. Both markdown task-list forms are
// counted ("- [ ] x" and "[ ] x"), because the prompt shows the bare form and
// models usually write the dashed one.
type soloChecklist struct {
	present bool
	items   int
	ticked  int
}

var soloChecklistItem = regexp.MustCompile(`^\s*(?:[-*]\s*)?\[([ xX])\]\s`)

func (runner *pipeline) soloChecklistState() soloChecklist {
	raw, err := os.ReadFile(filepath.Join(runner.workspace, ".senior-dev", "checklist.md"))
	if err != nil {
		return soloChecklist{}
	}
	state := soloChecklist{present: true}
	for _, line := range strings.Split(string(raw), "\n") {
		match := soloChecklistItem.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		state.items++
		if match[1] != " " {
			state.ticked++
		}
	}
	return state
}

// soloTreeChange compares the workspace against the base commit's tree,
// ignoring senior-dev's own artifacts.
//
// It deliberately does not use `git diff <base>` against the working copy,
// which reports only tracked changes. A run whose whole deliverable is a new
// file -- which is most of them -- produces an empty `git diff` while having
// changed everything that matters, so diffing that way would refuse exactly
// the submissions worth accepting. currentTreeSHA stages everything through a
// temporary index, so comparing against that tree sees new files the way a
// diff of the final tree will.
func (runner *pipeline) soloTreeChange(baseSHA string) (soloTreeChange, error) {
	return runner.recorder.Change(baseSHA)
}

func nonEmptyLines(value string) []string {
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

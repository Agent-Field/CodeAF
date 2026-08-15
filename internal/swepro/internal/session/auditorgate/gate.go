// This file ports the stateful post-merge auditor chain from
// src/session/auditor-gate.ts:1765-2744. Framework-bound services are narrow
// seams: agentjson owns auditor dispatch, ClauseJudger owns the out-of-bundle
// low-tier spec judgment, and optional Adjudicator/ExecutedCommands hooks own
// the frontier and session-message surfaces.
package auditorgate

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/baked"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/adaptiveflag"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/agentjson"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/auditconvergence"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/donecriteria"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/evidenceharvest"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/ledgers"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/observer"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/sizeband"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/specclauses"
)

type ClauseJudger interface {
	JudgeSpecClauses(ctx context.Context, spec, workspace string) (specclauses.SpecClauseJudgment, error)
}

type ClauseJudgerFunc func(
	ctx context.Context, spec, workspace string,
) (specclauses.SpecClauseJudgment, error)

func (f ClauseJudgerFunc) JudgeSpecClauses(
	ctx context.Context, spec, workspace string,
) (specclauses.SpecClauseJudgment, error) {
	return f(ctx, spec, workspace)
}

type LowLanguageProvider interface {
	GetModel(ctx context.Context, providerID, modelID string) (any, error)
	GetLanguage(ctx context.Context, model any) (any, error)
}

// ResolveLowLanguageWith ports the exported provider-resolution seam. Invalid
// candidate IDs and provider failures are skipped; nil is the fail-open
// fallback. The whole search carries the source's ten-second ceiling.
func ResolveLowLanguageWith(
	ctx context.Context, candidates []string, provider LowLanguageProvider,
) any {
	if provider == nil {
		return nil
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for _, candidate := range candidates {
		slash := strings.Index(candidate, "/")
		if slash <= 0 {
			continue
		}
		model, err := provider.GetModel(
			timeoutCtx, candidate[:slash], candidate[slash+1:],
		)
		if err != nil || model == nil {
			continue
		}
		language, err := provider.GetLanguage(timeoutCtx, model)
		if err == nil && language != nil {
			return language
		}
	}
	return nil
}

type AdjudicatorInput struct {
	Workspace       string
	ParentSessionID string
	Disputed        AuditorVerdict
	EvidencePack    string
	TimeoutMS       int64
	AuditCycle      float64
}

type Adjudicator interface {
	Adjudicate(ctx context.Context, input AdjudicatorInput) (*AuditorVerdict, error)
}

type AdjudicatorFunc func(context.Context, AdjudicatorInput) (*AuditorVerdict, error)

func (f AdjudicatorFunc) Adjudicate(
	ctx context.Context, input AdjudicatorInput,
) (*AuditorVerdict, error) {
	return f(ctx, input)
}

type GateInput struct {
	Workspace        string
	UserPrompt       string
	ParentSessionID  string
	BaseSHA          *string
	AuditCycle       *float64
	TimeoutMS        *int64
	Light            *bool
	SizeBand         *string
	ContractEvidence *string
	// ContractPassed reports that the registered acceptance contract was run by
	// the harness this cycle and exited 0. It is independent, fresh, machine
	// evidence — and unlike VerifiedTestsPassed it does not depend on the
	// command being spelled like an ecosystem test runner. See the
	// done-criteria evidence check below.
	ContractPassed bool
	// VerificationEvidence is harness-executed build/test evidence. It is
	// prompt context only: the auditor must still run and cite its own fresh
	// commands before a pass is admissible.
	VerificationEvidence *string
	History              []*auditconvergence.ConvergenceCycle
	FixedPoint           *auditconvergence.FixedPointEvidence
	MaxCleanupCycles     *float64
}

type GateStatus string

const (
	StatusPass      GateStatus = "pass"
	StatusFail      GateStatus = "fail"
	StatusSkipped   GateStatus = "skipped"
	StatusEscalated GateStatus = "escalated"
)

type GateResult struct {
	Status      GateStatus                              `json:"status"`
	Verdict     *AuditorVerdict                         `json:"verdict,omitempty"`
	Reason      *string                                 `json:"reason,omitempty"`
	Evidence    *string                                 `json:"-"`
	Convergence *auditconvergence.ConvergenceAssessment `json:"-"`
}

type GateDependencies struct {
	AgentJSON   agentjson.Dependencies
	Git         ProcessRunner
	Clauses     ClauseJudger
	Adjudicator Adjudicator
	Observer    observer.Tracker

	Now              func() float64
	ImpactedTests    func(workspace string, changedFiles []string) []string
	ExecutedCommands func(attempt int) []string
	EvidenceBlocks   func(attempt int, verdict AuditorVerdict) []string
}

func GateSession(
	ctx context.Context, input GateInput, deps GateDependencies,
) (GateResult, error) {
	if input.Workspace == "" {
		return GateResult{}, errors.New("auditorgate: workspace is required")
	}
	git := deps.Git
	if git == nil {
		git = ExecProcessRunner{}
	}
	now := deps.Now
	if now == nil {
		now = func() float64 { return float64(time.Now().UnixMilli()) }
	}
	adaptiveCuts := adaptiveflag.AdaptiveCutsEnabled()
	auditCycle := 1.0
	if input.AuditCycle != nil {
		auditCycle = *input.AuditCycle
	}

	UnlinkVerdictFile(input.Workspace)

	judgment := specclauses.SpecClauseJudgment{
		Clauses: []string{}, Count: 0, Source: "fallback",
	}
	if adaptiveCuts && deps.Clauses != nil {
		if judged, err := deps.Clauses.JudgeSpecClauses(
			ctx, input.UserPrompt, input.Workspace,
		); err == nil {
			judgment = judged
		}
	}
	matrix := []string{}
	if judgment.Matrix != nil {
		matrix = *judgment.Matrix
	}

	currentHead := (*string)(nil)
	var previous *AuditProvenance
	changedSince := []string{}
	var carryForward *string
	if adaptiveCuts {
		currentHead = readHeadSHA(git, input.Workspace)
		if auditCycle > 1 {
			previous = ReadAuditProvenance(input.Workspace)
			var priorSHA *string
			if previous != nil {
				priorSHA = &previous.AuditSHA
			}
			changedSince = changedFilesSinceAudit(
				git, input.Workspace, priorSHA, currentHead,
			)
			args := AuditCarryForwardArgs{
				PriorSHA: priorSHA, CurrentSHA: currentHead,
				ChangedFiles: changedSince,
			}
			if previous != nil {
				args.PreviousVerdict = &previous.Verdict
			}
			block := BuildAuditCarryForwardBlock(args)
			carryForward = &block
		}
	}

	if !hasChanges(git, input.Workspace, input.BaseSHA) {
		openBlockers := len(ledgers.LoadOpenBlockers(input.Workspace))
		if previous == nil {
			previous = ReadAuditProvenance(input.Workspace)
		}
		priorRejected := previous != nil && previous.Verdict.Verdict == VerdictFail
		// An empty diff terminates as PASS only when the run's obligations are
		// already met. Two things can meet them: a registered acceptance
		// contract that passes (the no-behavioral-change deliverable the
		// contract-cannot-fail path produces), or an audit that already ran and
		// accepted this tree. With neither, the run delivered nothing at all —
		// no diff, no contract, no accepted audit — and "no changes to audit"
		// reported success for a cycle that produced no work. A leaf whose
		// coder wrote nothing, failed cycle 1 on the missing contract, and then
		// resumed, passed on exactly this path.
		//
		// Returning a fail WITH a verdict lets the audit-fix loop re-dispatch
		// (bounded by maxCycles and the budget) rather than ending the run, so
		// a coder that simply produced nothing on its first attempt gets
		// another one instead of a false success.
		if adaptiveCuts && previous == nil && !input.ContractPassed {
			verdict := synthesizeNothingDeliveredVerdict()
			reason := "nothing delivered: the working tree is unchanged, no acceptance " +
				"contract is registered, and no audit has accepted this tree"
			return GateResult{
				Status: StatusFail, Verdict: &verdict, Reason: &reason,
			}, nil
		}
		if openBlockers == 0 && !priorRejected {
			reason := "no changes to audit"
			return GateResult{Status: StatusSkipped, Reason: &reason}, nil
		}
	}

	fullDiff := captureSessionDiff(git, input.Workspace, input.BaseSHA)
	changedFiles := []string{}
	criteria := []donecriteria.DoneCriterion{}
	if adaptiveCuts {
		changedFiles = changedFilesForAudit(git, input.Workspace, input.BaseSHA)
		criteria = donecriteria.ParseDoneCriteria(input.UserPrompt)
	}

	deltaScoped := false
	if adaptiveCuts && os.Getenv("CODEAF_DELTA_AUDIT") != "0" &&
		previous != nil && previous.AuditSHA != "" {
		deltaScoped = ShouldDeltaScope(ShouldDeltaScopeArgs{
			AuditCycle:   auditCycle,
			ChangedSince: float64(len(changedSince)),
			TotalChanged: float64(len(changedFiles)),
		})
	}
	auditDiff := fullDiff
	promptBaseSHA := input.BaseSHA
	deltaScopeNote := ""
	if deltaScoped {
		prior := previous.AuditSHA
		promptBaseSHA = &prior
		auditDiff = captureSessionDiff(git, input.Workspace, &prior)
		shortSHA := prior
		if len(shortSHA) > 12 {
			shortSHA = shortSHA[:12]
		}
		lines := []string{
			"",
			"# DELTA AUDIT SCOPE",
			"The previous cycle audited the base tree; the diff shown above is ONLY the",
			"delta since the prior audit (SHA " + shortSHA + "). Audit the",
			"delta plus the carried conclusions from the previous cycle (see 'Verified",
			"evidence from previous audit cycle'). Re-verify only what changed; carry",
			"forward prior verified evidence for clauses whose files did NOT change.",
			"Full changed-file list (base → now):",
		}
		for _, file := range changedFiles {
			lines = append(lines, "- "+file)
		}
		deltaScopeNote = strings.Join(lines, "\n")
	}

	band := string(sizeband.EstimateSizeBand(sizeband.EstimateSizeBandInput{
		Description: input.UserPrompt, Tags: []string{},
	}))
	if input.SizeBand != nil {
		band = *input.SizeBand
	}
	mode := ResolveAuditMode(ResolveAuditModeArgs{
		Light: input.Light, AdaptiveCutsEnabled: adaptiveCuts,
		SizeBand: &band, SpecClauseCount: judgment.Count,
	})
	WriteVerdictDraft(input.Workspace, AuditorVerdictDraft{
		StartedAt: now(), Mode: mode, ClauseCount: judgment.Count,
		MatrixCellCount: float64(len(matrix)),
	})

	configuredAttempts := numberEnv("CODEAF_AUDITOR_MAX_ATTEMPTS", 2)
	maxAttempts := configuredAttempts
	if adaptiveCuts {
		maxAttempts = math.Max(3, configuredAttempts)
	}
	timeoutMS := int64(numberEnv("CODEAF_AUDITOR_TIMEOUT_MS", 30*60_000))
	if input.TimeoutMS != nil {
		timeoutMS = *input.TimeoutMS
	}
	if maxAttempts < 0 {
		maxAttempts = 0
	}
	impacted := []string(nil)
	if deps.ImpactedTests != nil && len(changedFiles) > 0 {
		impacted = deps.ImpactedTests(input.Workspace, changedFiles)
		if len(impacted) == 0 || len(impacted) > 20 {
			impacted = nil
		}
	}

	var verdict *AuditorVerdict
	var lastInadmissible *AuditorVerdict
	var lastInadmissibleReason *string
	lastReason := ""
	fullRetries := 0
	forceFull := false
	producedRealVerdict := false
	var harvested *string

	for attempt := 1; float64(attempt) <= maxAttempts; attempt++ {
		attemptMode := mode
		if forceFull {
			attemptMode = "full"
		}
		agent := "auditor"
		if attemptMode == "light" {
			agent = "auditor-light"
		}
		var taskPrompt string
		if attemptMode == "light" {
			taskPrompt = BuildLightAuditPrompt(LightAuditPromptArgs{
				UserPrompt: input.UserPrompt, Diff: auditDiff,
				Workspace: input.Workspace, TurnCap: LightAuditTurnCap,
				CarryForward: carryForward, ImpactedTests: impacted,
				ClauseInventory: judgment.Clauses, ClauseMatrix: matrix,
				BaseSHA: promptBaseSHA,
			})
		} else {
			taskPrompt = BuildAuditPrompt(AuditPromptArgs{
				UserPrompt: input.UserPrompt, Diff: auditDiff,
				Workspace: input.Workspace, CarryForward: carryForward,
				ImpactedTests: impacted, ClauseInventory: judgment.Clauses,
				ClauseMatrix: matrix, BaseSHA: promptBaseSHA,
			})
		}
		if deltaScoped {
			taskPrompt += "\n" + deltaScopeNote
		}
		if adaptiveCuts {
			taskPrompt += "\n\n" + donecriteria.BuildDoneCriteriaPrompt(criteria)
		}
		if input.ContractEvidence != nil && *input.ContractEvidence != "" {
			taskPrompt += "\n\n" + *input.ContractEvidence
		}
		if input.VerificationEvidence != nil && *input.VerificationEvidence != "" {
			taskPrompt += "\n\n" + *input.VerificationEvidence
		}
		if attempt > 1 {
			taskPrompt += "\n\n" + buildRetryTail(
				attempt, int(maxAttempts), input.Workspace,
				judgment.Clauses, lastInadmissible, lastInadmissibleReason,
			)
		}

		maxRetries := 0
		label := agent
		dispatchInput := agentjson.Input[AuditorVerdict]{
			Agent: agent, ParentSessionID: input.ParentSessionID,
			Workspace: input.Workspace, TaskPrompt: taskPrompt,
			OutputPath: filepath.Join(input.Workspace, verdictRelative),
			Schema:     AuditorSchema{}, MaxRetries: &maxRetries,
			TimeoutMS: &timeoutMS, Label: &label,
			Tools: []agentjson.ToolSetting{
				{Name: "read", Enabled: true},
				{Name: "grep", Enabled: true},
				{Name: "glob", Enabled: true},
				{Name: "bash", Enabled: true},
				{Name: "write", Enabled: true},
			},
			PreserveOnSuccess: true,
		}
		observedSessionID := ""
		if deps.Observer != nil {
			dispatchInput.SessionStarted = func(request agentjson.Request) {
				// TS auditor-gate.ts:2147-2165 tracks each fresh auditor
				// session immediately before its prompt and :2288-2294 untracks
				// after the attempt, before interpreting the verdict artifact.
				deps.Observer.Track(observer.ObservedSession{
					SessionID: request.SessionID, AgentRole: "auditor",
					TaskSummary:  "Audit completion of: " + sliceUTF16(input.UserPrompt, 0, 160),
					StartedAt:    now(),
					ArtifactPath: filepath.Join(input.Workspace, verdictRelative),
					Workspace:    input.Workspace, ParentSessionID: input.ParentSessionID,
				})
				observedSessionID = request.SessionID
			}
		}
		result, err := agentjson.DispatchJSON(ctx, dispatchInput, deps.AgentJSON)
		if observedSessionID != "" {
			// TS auditor-gate.ts:2288-2294 reads the verdict artifact, then
			// untracks before the gate interprets it. DispatchJSON owns that
			// artifact read in Go, so its return is the equivalent boundary.
			deps.Observer.Untrack(observedSessionID)
		}
		if err != nil {
			lastReason = "attempt " + strconv.Itoa(attempt) +
				" produced no parseable verdict file"
			continue
		}
		fileVerdict := result.Data
		RemoveVerdictDraft(input.Workspace)
		blocks := verdictEvidenceBlocks(fileVerdict)
		if deps.EvidenceBlocks != nil {
			blocks = append(blocks, deps.EvidenceBlocks(attempt, fileVerdict)...)
		}
		harvested = evidenceharvest.HarvestEvidence(blocks)

		underfilled := IsUnderfilledTemplateVerdict(fileVerdict)
		admissibility := AdmissibilityResult{true, "admissibility gate disabled"}
		if underfilled {
			admissibility = AdmissibilityResult{
				false, "verdict notes indicate a draft or incomplete audit",
			}
		} else if AdmissibilityEnabled() {
			admissibility = IsAdmissibleVerdict(fileVerdict, &AdmissibilityOptions{
				SpecClauseCount:     judgment.Count,
				SpecClauseInventory: judgment.Clauses,
				SpecMatrix:          matrix,
			})
			if admissibility.Admissible && adaptiveCuts {
				executed := []string{}
				if deps.ExecutedCommands != nil {
					executed = deps.ExecutedCommands(attempt)
				}
				cross := CrossCheckExecutedCommands(fileVerdict, executed)
				if !cross.OK {
					admissibility = AdmissibilityResult{false, cross.Reason}
				}
			}
		}
		if !admissibility.Admissible {
			retryFull := fullRetries < 2 && adaptiveCuts
			// auditor-gate.ts:2071-2076 already bounds attempts independently of
			// adaptive cuts. An underfilled response consumes that existing budget;
			// it is never promoted merely because optional admissibility is off.
			retryUnderfilled := underfilled && float64(attempt) < maxAttempts
			denseForced := fileVerdict.Verdict == VerdictPass &&
				judgment.Count >= DenseSpecClauseThreshold && retryFull
			if denseForced && deps.Adjudicator != nil &&
				readFrontierCallCount(input.Workspace) < FrontierMaxCallsPerRun {
				if applied := runAdjudication(
					ctx, input, deps, git, fileVerdict, fullDiff,
					carryForward, timeoutMS, auditCycle,
				); applied != nil {
					bumpFrontierCallCount(input.Workspace)
					return finishGate(
						input, judgment, currentHead, auditCycle,
						applied.Verdict, applied.Note, harvested, true,
					)
				}
			}
			if retryFull || retryUnderfilled {
				if adaptiveCuts {
					forceFull = true
					fullRetries++
				}
				copyVerdict := fileVerdict
				lastInadmissible = &copyVerdict
				reason := admissibility.Reason
				lastInadmissibleReason = &reason
				continue
			}
			if fileVerdict.Verdict == VerdictPass {
				actionable := ConvertInadmissiblePassToFail(
					fileVerdict, admissibility.Reason,
				)
				_ = writeVerdictAtomic(input.Workspace, actionable)
				reason := "inadmissible pass converted to actionable fail: " +
					admissibility.Reason
				return finishGate(
					input, judgment, currentHead, auditCycle,
					actionable, reason, harvested, true,
				)
			}
			if adaptiveCuts && currentHead != nil {
				_ = WriteAuditProvenance(input.Workspace, AuditProvenance{
					AuditSHA: *currentHead, AuditCycle: auditCycle,
					Verdict: fileVerdict,
				})
			}
			reason := "audit inconclusive after inadmissible verdict: " +
				admissibility.Reason
			return GateResult{
				Status: StatusEscalated, Verdict: &fileVerdict,
				Reason: &reason, Evidence: harvested,
			}, nil
		}

		verdict = &fileVerdict
		producedRealVerdict = true
		if adaptiveCuts && fileVerdict.Verdict == VerdictPass {
			testsOK := VerifiedTestsPassed(fileVerdict)
			// VerifiedTestsPassed only recognizes ecosystem-standard runners
			// (`go test`, `pytest`, `npm test`, …). A task in a repo with no
			// test framework gets a bespoke acceptance script instead — the
			// registered contract, e.g. `bash test-hello.sh` — which matches
			// none of them, so the harness saw "no test evidence" no matter
			// what the coder did and every behavior criterion stayed
			// unsatisfiable however correct the work was. The contract is the
			// better signal: the harness ran it itself in a fresh subprocess
			// this cycle, and the base-contract check requires it to have
			// FAILED at the base commit, so a pass is proof behavior changed.
			//
			// It feeds the criteria check ONLY. evidenceBacked below keeps the
			// stricter VerifiedTestsPassed, so a passing contract can satisfy a
			// behavior criterion but can never wave past a criterion the
			// evidence genuinely does not meet — a file criterion naming a path
			// absent from the diff still fails the audit.
			testEvidence := testsOK || input.ContractPassed
			done := donecriteria.CheckDone(criteria, &donecriteria.DoneEvidence{
				TestsPassed: &testEvidence, ChangedFiles: changedFiles,
				TestOutput: signalNotes(fileVerdict),
			})
			evidenceBacked := fileVerdict.Step2Signal != nil &&
				fileVerdict.Step2Signal.Reproduced != nil &&
				*fileVerdict.Step2Signal.Reproduced && testsOK
			if !done.Done && !evidenceBacked {
				blockers := append([]Blocker{}, fileVerdict.Blockers...)
				blockers = append(blockers, newBlockerOrdered(
					field("step", 2),
					field("detail", "done-criteria evidence incomplete: unsatisfied "+
						strings.Join(done.Unsatisfied, ", ")),
				))
				hints := append([]string{}, fileVerdict.RepairHints...)
				hints = append(hints,
					"Satisfy every parsed completion criterion with changed-file and fresh test evidence.",
				)
				updated := fileVerdict.cloneSet(
					field("verdict", VerdictFail),
					field("blockers", blockers),
					field("repair_hints", hints),
				)
				verdict = &updated
			}
		}
		break
	}

	if verdict == nil {
		fallback := synthesizeFailVerdict(
			"auditor did not produce a parseable verdict at .codeaf/auditor-verdict.json after " +
				jscompat.FormatNumber(maxAttempts) + " attempts (" + lastReason + ")",
		)
		verdict = &fallback
	}
	normalized := SynthesizeScopeBlockers(*verdict, judgment.Clauses, matrix)
	verdict = &normalized

	if producedRealVerdict && adaptiveCuts && verdict.Verdict == VerdictFail &&
		len(verdict.Blockers) > 0 && ShouldAdjudicate(ShouldAdjudicateArgs{
		AuditCycle: auditCycle, VerdictFlipped: true,
	}) && deps.Adjudicator != nil &&
		readFrontierCallCount(input.Workspace) < FrontierMaxCallsPerRun {
		if applied := runAdjudication(
			ctx, input, deps, git, *verdict, fullDiff,
			carryForward, timeoutMS, auditCycle,
		); applied != nil {
			bumpFrontierCallCount(input.Workspace)
			return finishGate(
				input, judgment, currentHead, auditCycle,
				applied.Verdict, applied.Note, harvested, false,
			)
		}
	}

	ledgers.ReconcileVerdictBlockers(
		input.Workspace, auditCycle, blockerDetails(verdict.Blockers),
	)
	if adaptiveCuts && currentHead != nil {
		_ = WriteAuditProvenance(input.Workspace, AuditProvenance{
			AuditSHA: *currentHead, AuditCycle: auditCycle, Verdict: *verdict,
		})
	}
	final := AdmissibilityResult{true, "admissibility gate disabled"}
	if AdmissibilityEnabled() {
		final = IsAdmissibleVerdict(*verdict, &AdmissibilityOptions{
			SpecClauseCount:     judgment.Count,
			SpecClauseInventory: judgment.Clauses,
			SpecMatrix:          matrix,
		})
	}
	convergence := AssessVerdictConvergence(
		*verdict, input.History, input.FixedPoint, input.MaxCleanupCycles,
	)
	if !final.Admissible {
		reason := "audit inconclusive after retry: " + final.Reason
		return GateResult{
			Status: StatusEscalated, Verdict: verdict, Reason: &reason,
			Evidence: harvested, Convergence: &convergence,
		}, nil
	}
	return GateResult{
		Status: GateStatus(verdict.Verdict), Verdict: verdict,
		Evidence: harvested, Convergence: &convergence,
	}, nil
}

func numberEnv(name string, fallback float64) float64 {
	raw, exists := os.LookupEnv(name)
	if !exists {
		return fallback
	}
	return jscompat.ToNumber(raw)
}

func buildRetryTail(
	attempt, maxAttempts int, workspace string, inventory []string,
	last *AuditorVerdict, reason *string,
) string {
	if last != nil {
		return strings.Join(BuildInadmissibleRetryReminder(
			inventory, *last, reason,
		), "\n")
	}
	return strings.Join([]string{
		"<system-reminder>",
		"RETRY ATTEMPT " + strconv.Itoa(attempt) + "/" + strconv.Itoa(maxAttempts) + ". Your previous attempt did not",
		"produce a parseable verdict file at " + workspace + "/.codeaf/auditor-verdict.json.",
		"",
		"Write a DRAFT fail verdict immediately, then rewrite it after every probe batch.",
		"Do not save the file write for the end.",
		"</system-reminder>",
	}, "\n")
}

type AuditorSchema struct{}

func (AuditorSchema) SafeParse(raw json.RawMessage) agentjson.Validation[AuditorVerdict] {
	var verdict AuditorVerdict
	if err := json.Unmarshal(raw, &verdict); err != nil {
		return agentjson.Validation[AuditorVerdict]{
			Issues: []agentjson.Issue{{Message: "Expected auditor verdict object"}},
		}
	}
	if err := validateAuditorVerdict(verdict); err != nil {
		return agentjson.Validation[AuditorVerdict]{
			Issues: []agentjson.Issue{{Message: err.Error()}},
		}
	}
	return agentjson.Validation[AuditorVerdict]{Data: normalizeVerdict(verdict)}
}

// synthesizeNothingDeliveredVerdict is the verdict for a cycle that produced
// no work at all. The repair hint names the two ways out so the fix generator
// has somewhere to go, rather than re-deriving an empty tree.
func synthesizeNothingDeliveredVerdict() AuditorVerdict {
	return newVerdictOrdered(
		field("verdict", VerdictFail),
		field("blockers", []Blocker{newBlockerOrdered(
			field("step", 0),
			field("detail", "nothing was delivered: the working tree is unchanged, "+
				"no acceptance contract is registered, and no audit has accepted "+
				"this tree — the task produced no work"),
		)}),
		field("repair_hints", []string{
			"Register the acceptance contract at `.codeaf/contract.json` and implement the requested change; an unchanged tree with no contract cannot complete the task.",
		}),
	)
}

func synthesizeFailVerdict(reason string) AuditorVerdict {
	return newVerdictOrdered(
		field("verdict", VerdictFail),
		field("blockers", []Blocker{newBlockerOrdered(
			field("step", 0),
			field("detail", "auditor: "+reason),
		)}),
		field("repair_hints", []string{
			"Auditor must end with a structured verdict at `.codeaf/auditor-verdict.json`. If the auditor crashed or produced no parseable output, the gate defaults to fail per the auditor's own anti-pattern #4 (default-pass forbidden).",
		}),
	)
}

func writeVerdictAtomic(workspace string, verdict AuditorVerdict) error {
	target := filepath.Join(workspace, verdictRelative)
	if err := os.MkdirAll(filepath.Dir(target), 0o777); err != nil {
		return err
	}
	body, err := jscompat.StringifyIndent(verdict)
	if err != nil {
		return err
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, body, 0o666); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

func signalNotes(verdict AuditorVerdict) *string {
	if verdict.Step2Signal == nil {
		return nil
	}
	return verdict.Step2Signal.Notes
}

func blockerDetails(blockers []Blocker) []string {
	out := make([]string, len(blockers))
	for i, blocker := range blockers {
		out[i] = blocker.Detail
	}
	return out
}

func verdictEvidenceBlocks(verdict AuditorVerdict) []string {
	blocks := []string{}
	for _, command := range verdictCommands(verdict) {
		line := CommandName(command) + " exit=" + CommandExit(command)
		if tail := CommandOutputTail(command); tail != "" {
			line += "\n" + tail
		}
		blocks = append(blocks, line)
	}
	for _, blocker := range verdict.Blockers {
		blocks = append(blocks, blocker.Detail)
	}
	return blocks
}

func runAdjudication(
	ctx context.Context,
	input GateInput,
	deps GateDependencies,
	git ProcessRunner,
	disputed AuditorVerdict,
	fullDiff string,
	carryForward *string,
	timeoutMS int64,
	auditCycle float64,
) *ApplyAdjudicationResult {
	if deps.Adjudicator == nil {
		return nil
	}
	pack := BuildAdjudicationEvidencePack(AdjudicationEvidencePackArgs{
		Disputed:         disputed,
		DiffStat:         captureDiffStat(git, input.Workspace, input.BaseSHA),
		TopHunks:         TopDiffHunks(fullDiff),
		ContractEvidence: input.ContractEvidence,
		CarryForward:     carryForward,
	})
	verdict, err := deps.Adjudicator.Adjudicate(ctx, AdjudicatorInput{
		Workspace:       input.Workspace,
		ParentSessionID: input.ParentSessionID,
		Disputed:        disputed, EvidencePack: pack,
		TimeoutMS: timeoutMS, AuditCycle: auditCycle,
	})
	if err != nil || verdict == nil {
		return nil
	}
	contractPassed := ContractEvidenceIndicatesPass(input.ContractEvidence)
	return pointerApply(ApplyAdjudication(ApplyAdjudicationArgs{
		Disputed: disputed, Adjudicator: *verdict,
		ContractPassed: &contractPassed,
	}))
}

func pointerApply(result ApplyAdjudicationResult) *ApplyAdjudicationResult { return &result }

func finishGate(
	input GateInput,
	judgment specclauses.SpecClauseJudgment,
	currentHead *string,
	auditCycle float64,
	verdict AuditorVerdict,
	reason string,
	evidence *string,
	persistVerdict bool,
) (GateResult, error) {
	if persistVerdict {
		_ = writeVerdictAtomic(input.Workspace, verdict)
	}
	ledgers.ReconcileVerdictBlockers(
		input.Workspace, auditCycle, blockerDetails(verdict.Blockers),
	)
	if adaptiveflag.AdaptiveCutsEnabled() && currentHead != nil {
		_ = WriteAuditProvenance(input.Workspace, AuditProvenance{
			AuditSHA: *currentHead, AuditCycle: auditCycle, Verdict: verdict,
		})
	}
	RemoveVerdictDraft(input.Workspace)
	convergence := AssessVerdictConvergence(
		verdict, input.History, input.FixedPoint, input.MaxCleanupCycles,
	)
	return GateResult{
		Status: GateStatus(verdict.Verdict), Verdict: &verdict,
		Reason: &reason, Evidence: evidence, Convergence: &convergence,
	}, nil
}

func AssessVerdictConvergence(
	verdict AuditorVerdict,
	history []*auditconvergence.ConvergenceCycle,
	fixedPoint *auditconvergence.FixedPointEvidence,
	maxCleanup *float64,
) auditconvergence.ConvergenceAssessment {
	severityBlockers := make([]*auditconvergence.SeverityBlocker, 0, len(verdict.Blockers))
	for _, blocker := range verdict.Blockers {
		severityBlockers = append(severityBlockers, &auditconvergence.SeverityBlocker{
			File: blocker.File, Line: blocker.Line, Step: blocker.Step,
			Detail: blocker.Detail, Severity: blocker.Severity,
		})
	}
	partition := auditconvergence.PartitionBlockers(severityBlockers)
	keys := make([]string, 0, len(severityBlockers))
	for _, blocker := range severityBlockers {
		keys = append(keys, auditconvergence.BlockerKey(blocker))
	}
	cycles := append(append([]*auditconvergence.ConvergenceCycle{}, history...),
		&auditconvergence.ConvergenceCycle{
			CorrectnessCount: float64(len(partition.Correctness)),
			HygieneCount:     float64(len(partition.Hygiene)),
			PolishCount:      float64(len(partition.Polish)),
			BlockerKeys:      keys,
		},
	)
	limit := auditconvergence.AUDIT_CLEANUP_MAX_CYCLES_DEFAULT
	if maxCleanup != nil {
		limit = *maxCleanup
	}
	return auditconvergence.AssessConvergence(auditconvergence.AssessConvergenceInput{
		Cycles: cycles, MaxCleanupCycles: limit, FixedPoint: fixedPoint,
	})
}

// AgentJSONAdjudicator provides the live frontier seam through the same strict
// JSON dispatcher used by the main auditor. It is optional because callers
// may deliberately leave frontier routing unconfigured.
type AgentJSONAdjudicator struct {
	Dependencies agentjson.Dependencies
}

func (a AgentJSONAdjudicator) Adjudicate(
	ctx context.Context, input AdjudicatorInput,
) (*AuditorVerdict, error) {
	output := filepath.Join(
		input.Workspace, ".codeaf", "agents", "adjudicator",
		"cycle-"+jscompat.FormatNumber(input.AuditCycle)+".json",
	)
	prompt := strings.Join([]string{
		"You are the final adjudicator at a single disagreement branch point in an",
		"autonomous coding run. A completion audit produced a verdict that is now",
		`contested. The verdict under dispute is "` + string(input.Disputed.Verdict) + `".`,
		"",
		"Rule CONFIRM (emit the SAME verdict) or OVERTURN (emit the OPPOSITE), per your",
		"role: strict on correctness, dismiss cosmetic objections, and never wave past a",
		"failing or absent machine check. Give a one-line reason per blocker.",
		"",
		"## Distilled evidence",
		input.EvidencePack,
	}, "\n")
	maxRetries := 0
	label := "adjudicator"
	tier := baked.TierFrontier
	result, err := agentjson.DispatchJSON(ctx, agentjson.Input[AuditorVerdict]{
		Agent: "adjudicator", ParentSessionID: input.ParentSessionID,
		Workspace: input.Workspace, TaskPrompt: prompt, OutputPath: output,
		Schema: AuditorSchema{}, MaxRetries: &maxRetries,
		TimeoutMS: &input.TimeoutMS, Tier: &tier, Label: &label,
		Tools: []agentjson.ToolSetting{
			{Name: "read", Enabled: true},
			{Name: "grep", Enabled: true},
			{Name: "glob", Enabled: true},
			{Name: "bash", Enabled: false},
			{Name: "write", Enabled: true},
		},
	}, a.Dependencies)
	if err != nil {
		return nil, err
	}
	return &result.Data, nil
}

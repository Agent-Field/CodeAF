package serve

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/verification"
)

// Reading the explicit retry grant never renews a lease, clears a decision or
// starts work. Only the machine and person already owning this checkout see it.
func (h *Handler) workerRecoveryStatus(w http.ResponseWriter, r *http.Request) {
	if !h.workerReady(w) {
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 1 {
		h.fail(w, http.StatusBadRequest, "retained results require a numeric job id")
		return
	}
	it, err := h.workers.st.Get(id)
	if h.workerError(w, err) {
		return
	}
	worker := requestWorker(r)
	if worker.Machine == "" || worker.Machine != it.WorktreeMachine || it.Person() == "" || it.Person() != factory.NormalizeHandle(worker.Handle) {
		h.fail(w, http.StatusForbidden, "retained results belong to another worktree owner")
		return
	}
	h.write(w, http.StatusOK, api.WorkerRecovered{Factory: h.factory, RetryRequestedAt: it.ReportRetryAt})
}

func (h *Handler) recoverWorkerReport(w http.ResponseWriter, r *http.Request) {
	if !h.workerReady(w) {
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 1 {
		h.fail(w, http.StatusBadRequest, "retained results require a numeric job id")
		return
	}
	var body api.WorkerRecovery
	if !h.decode(w, r, &body, false) {
		return
	}
	report, digest, err := retainedWorkerReport(body)
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	saved, already, err := h.workers.desk.ReconcileReport(id, requestWorker(r), body.OriginalLease, digest, body.AdditionalCost, func(it *factory.Item, receipt *factory.WorkerReportReceipt) error {
		if receipt != nil {
			if it.Stream == nil {
				return fmt.Errorf("the acknowledged report no longer has a spending ledger")
			}
			it.Stream.Spent += body.AdditionalCost - receipt.AdditionalCost
			receipt.AdditionalCost = body.AdditionalCost
			return nil
		}
		if err := validateRetainedReport(*it, report); err != nil {
			return err
		}
		// Only this typed internal block is cleared. Any newer decision, stop,
		// pause, checkout identity or phase attempt refuses the entire write.
		it.Question, it.QKind, it.Asking = "", "", nil
		if err := run.ApplyReport(it, report, h.workers.now()); err != nil {
			return err
		}
		it.Stream.Spent += body.AdditionalCost
		it.Failure, it.Recovery = nil, nil
		if report.Stages[0].Name == factory.FollowPRStep && it.FollowUp != nil && (it.FollowUp.State == "watching" || it.FollowUp.State == "healthy") {
			it.State = factory.StateShipped
			it.Stream.Ended = h.workers.now()
			it.Stream.Phases[it.Stream.Cur].Note = it.FollowUp.Reason
		} else {
			// The completed result is now the baseline. The normal pull door can
			// continue remaining phases under a new lease, never this old one.
			it.State = factory.StateQueued
			it.Stream.Ended = time.Time{}
		}
		it.ReportReceipts = append(it.ReportReceipts, factory.WorkerReportReceipt{Digest: digest, At: h.workers.now(), AdditionalCost: body.AdditionalCost})
		if len(it.ReportReceipts) > factory.WorkerReportReceiptsMost {
			it.ReportReceipts = append([]factory.WorkerReportReceipt(nil), it.ReportReceipts[len(it.ReportReceipts)-factory.WorkerReportReceiptsMost:]...)
		}
		it.Note(h.workers.now(), "Retained step results saved; the pipeline recovered without repeating completed work.")
		return nil
	})
	if errors.Is(err, lease.ErrLeased) || errors.Is(err, lease.ErrPerson) {
		h.fail(w, http.StatusConflict, "retained results no longer belong to the current job owner or lease")
		return
	}
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	job := h.withVersion(api.WorkerJob(saved))
	job.Can = api.CanFor(saved, requestWorker(r).Handle)
	h.presentWorkerJob(r, &job)
	h.write(w, http.StatusOK, api.WorkerRecovered{Factory: h.factory, Job: job, AlreadyApplied: already, RetryRequestedAt: saved.ReportRetryAt})
}

// A receipt binds the original lease, immutable body and every compatibility
// header. The store/job containing it supplies the factory identity. A worker
// cannot substitute new result bytes behind a previously acknowledged digest.
func retainedWorkerReport(body api.WorkerRecovery) (factory.RunReport, string, error) {
	if math.IsNaN(body.AdditionalCost) || math.IsInf(body.AdditionalCost, 0) || body.AdditionalCost < 0 {
		return factory.RunReport{}, "", fmt.Errorf("additional retained usage must be finite and nonnegative")
	}
	if !strings.HasPrefix(body.OriginalLease, "lease_") || len(body.OriginalLease) > 128 || len(body.Headers.Get("Idempotency-Key")) == 0 || len(body.Headers.Get("Idempotency-Key")) > 128 {
		return factory.RunReport{}, "", fmt.Errorf("retained results require their original lease and request key")
	}
	for name, values := range body.Headers {
		if http.CanonicalHeaderKey(name) != name {
			return factory.RunReport{}, "", fmt.Errorf("retained result header names must be canonical")
		}
		switch http.CanonicalHeaderKey(name) {
		case "Idempotency-Key", api.StepModelsHeader, api.StepChatsHeader, api.StepRuntimeHeader:
		default:
			return factory.RunReport{}, "", fmt.Errorf("retained results contain an unsupported header")
		}
		if len(values) != 1 || len(values[0]) > 64<<10 {
			return factory.RunReport{}, "", fmt.Errorf("retained result headers must contain one bounded value")
		}
	}
	var wire api.WorkerReport
	d := json.NewDecoder(bytes.NewReader(body.Body))
	d.DisallowUnknownFields()
	if err := d.Decode(&wire); err != nil {
		return factory.RunReport{}, "", fmt.Errorf("invalid retained report: %w", err)
	}
	if d.Decode(new(any)) != io.EOF {
		return factory.RunReport{}, "", fmt.Errorf("retained results require one report")
	}
	if err := workerReportHeaders(&wire, body.Headers); err != nil {
		return factory.RunReport{}, "", err
	}
	identity := body
	identity.AdditionalCost = 0
	raw, err := json.Marshal(identity)
	if err != nil {
		return factory.RunReport{}, "", err
	}
	sum := sha256.Sum256(raw)
	return api.ToWorkerReport(wire), hex.EncodeToString(sum[:]), nil
}

// Recovery saves completed work, rather than granting an old worker permission
// to publish, merge, answer a human decision or replace a newer phase attempt.
func validateRetainedReport(it factory.Item, report factory.RunReport) error {
	if it.State != factory.StateNeedsYou || it.QKind != "results_not_saved" || it.Manual || it.DoneHow != "" || it.Publishing || it.Stream == nil || it.Stream.Paused || it.Merging != nil || it.Approval != nil && it.Approval.State == "approving" {
		return fmt.Errorf("only the current internal result-save block can be reconciled")
	}
	if it.Asking != nil && (it.Asking.Stage != "" || it.Asking.Question != it.Question) {
		return fmt.Errorf("a newer decision owns the job")
	}
	if it.PullRequest != nil && it.PullRequest.State != factory.PROpen {
		return fmt.Errorf("the PR has already closed")
	}
	if report.Question != nil || report.Pipeline != nil || report.Publishing != nil || report.Merge != nil || report.Publish != "" || report.CommentURL != "" || report.HeadCommit != "" && report.HeadCommit != it.HeadCommit || report.ChangeBase != "" && report.ChangeBase != it.ChangeBase || report.Change != nil && !reflect.DeepEqual(report.Change, it.Change) {
		return fmt.Errorf("retained result recovery cannot change publication, pipeline or decisions")
	}
	if report.PR != nil && (it.PullRequest == nil || report.PR.Number != it.PullRequest.Number || report.PR.URL != it.PullRequest.URL || report.PR.HeadSHA != it.PullRequest.HeadSHA) {
		return fmt.Errorf("retained PR observation is no longer current")
	}
	if it.Stream.Cur < 0 || it.Stream.Cur >= len(it.Stream.Phases) || len(report.Stages) != 1 {
		return fmt.Errorf("retained results must name the current completed step")
	}
	ph, step := it.Stream.Phases[it.Stream.Cur], report.Stages[0]
	if ph.Kind == factory.StageGate || step.Name != ph.Name || step.Round < 1 || step.Round != ph.Round || !ph.Started.Equal(step.Started) || step.Chat != "" && ph.Chat != "" && step.Chat != ph.Chat || step.Result == nil || !step.Result.Done || step.Result.Checkpoint || step.Result.ExecutionBlocked != "" || step.Result.JobBudgetStopped || step.Result.Findings != 0 || step.Result.Exit != 0 || step.Failure != nil {
		return fmt.Errorf("retained results do not match the current completed step attempt")
	}
	for _, note := range step.Result.Notes {
		if strings.HasPrefix(note, verification.FailurePrefix) {
			return fmt.Errorf("retained results still contain an unresolved verification failure")
		}
	}
	if ph.State != factory.PhaseRunning {
		return fmt.Errorf("the step no longer owns its running attempt")
	}
	if step.State == factory.PhaseDone && report.FollowUp == nil && ph.Name != factory.FollowPRStep {
		return nil
	}
	if ph.Name != factory.FollowPRStep || step.State != factory.PhaseWaiting || report.FollowUp == nil || it.FollowUp == nil || it.FollowUp.Kind != "pr" || report.FollowUp.Kind != "pr" || it.FollowUp.State != "running" || report.FollowUp.HeadSHA != it.FollowUp.HeadSHA || it.PullRequest == nil || it.PullRequest.HeadSHA != it.FollowUp.HeadSHA || report.FollowUp.EventKey != it.FollowUp.EventKey || report.FollowUp.ReviewKey != it.FollowUp.ReviewKey || step.Result.Repair == nil || step.Result.Repair.Decision != "no_change" || report.FollowUp.State != "watching" && report.FollowUp.State != "healthy" {
		return fmt.Errorf("retained Follow PR completion no longer matches the captured work")
	}
	if why := factory.ReadinessReportFault(it.FollowUp.HeadSHA, *step.Result); why != "" {
		return fmt.Errorf("retained completion is incomplete: %s", why)
	}
	return nil
}

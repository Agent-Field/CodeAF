package session

// share_publish.go is one share_deliverable operation from its arguments to a
// settled receipt: admission, the inspected plan, and the appends.
//
// ── THE ORDER IS THE GUARANTEE ──
//
//  1. Admission writes a pending receipt holding the planned ids.
//  2. Every file is read once — hashed, and copied when it needs an owned copy —
//     and the FULL rows are persisted as the plan.
//  3. Only then is any row appended, and only rows of that plan, read back from
//     the claim's own durable bytes ([ShareClaim.PlannedRows]) and never from
//     the Receipt view or from the rows that were handed to Plan.
//  4. Every appended row is found again by its exact id before it is called
//     recorded, so "accepted" is never said of a row a listing cannot find.
//
// A retry of a planned or unknown operation never reads a file again: it asks
// the index for each planned id EXACTLY, under the same ten-second budget, and
// appends only an id the whole bounded read proved absent. An answer the read
// could not finish is unknown, the receipt is kept, and nothing is appended —
// one row too few is a retry, one row too many is a lie nobody can take back.
//
// ── WHAT A SHARED ROW SAYS ──
//
// shared_by_agent: the agent handed the file over. It is never Authored, its
// state is unknown, and nothing about the file being new is inferred, because
// sharing is not making. An existing created row of the same file is never
// touched: a share is a row of its own. Its media fact, for a picture, a sound
// or a video, is origin shared — never generated, never a declared shell output.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// shareTarget is where one operation records, decided by the caller from the
// actual dispatch authority — the conversation itself, or the sink its owning
// conversation handed a task worker — and never from a path or a clock.
type shareTarget struct {
	index   string // the deliverables index the rows are appended to
	session string // the conversation the rows belong to
	base    string // what a relative path is relative to
	// roots are the owner's own folders: a file inside one is recorded where
	// it is. reach are folders a file may only be read FROM, into an owned
	// copy under copies, which is inside the owner's own artifacts folder (a
	// task worker's tree, which may be removed once its work lands).
	roots  []string
	reach  []string
	copies string
	prov   ShareProvenance
	task   string
	// emit is told of every row once it has been found again on the index.
	emit func(Artifact)
}

// ShareResultFile is one file of a settled receipt as the tool reports it.
type ShareResultFile struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Kind    string `json:"kind,omitempty"`
	MIME    string `json:"mime,omitempty"`
	Bytes   int64  `json:"bytes,omitempty"`
	Version string `json:"version,omitempty"`
	// Media is the media source id of a shared picture, sound or video.
	Media string `json:"media,omitempty"`
	// Original is the file an owned copy was taken from; Path is the copy.
	Original string `json:"original,omitempty"`
}

// ShareResult is the structured receipt one call answers.
type ShareResult struct {
	OperationID string            `json:"operation_id"`
	Status      string            `json:"status"`
	Replayed    bool              `json:"replayed"`
	RecordedIDs []string          `json:"recorded_ids,omitempty"`
	Files       []ShareResultFile `json:"files,omitempty"`
	Reason      string            `json:"reason,omitempty"`
}

// Status words a result may carry beyond the receipt's own.
const (
	shareResultBusy     = "busy"
	shareResultConflict = "conflict"
	shareResultInvalid  = "invalid"
)

// The reasons a retry is the right next step, said once.
const (
	shareRetryTime   = "the share ran out of time; retry with the same operation_id"
	shareRetryCheck  = "the record could not be checked in time; retry with the same operation_id"
	shareRetryRecord = "a row could not be recorded; retry with the same operation_id"
	shareRetryPlan   = "the plan could not be written; nothing was recorded, retry with the same operation_id"
	shareRetrySettle = "the receipt could not be settled in time; retry with the same operation_id"
)

// publishShare runs one operation to its settlement.
func publishShare(ctx context.Context, target shareTarget, operationID string, paths []string) ShareResult {
	result := ShareResult{OperationID: operationID}
	asked, err := target.absolute(paths)
	if err == nil {
		err = ValidateShareOperationID(operationID)
	}
	if err == nil {
		err = validateShareProvenance(target.prov)
	}
	if err != nil || target.index == "" || target.session == "" {
		result.Status, result.Reason = shareResultInvalid, shareReason(err, "the arguments are not a share this conversation can record")
		return result
	}
	claim, err := AdmitShareOperation(filepath.Dir(target.index), target.session, operationID, asked, target.prov, time.Time{})
	if err != nil {
		return shareRefusedClaim(result, claim, err)
	}
	ctx, cancel := context.WithDeadline(ctx, claim.deadline)
	defer cancel()
	result.Replayed = claim.Outcome == ShareClaimReplay
	return target.settle(ctx, claim, result)
}

// shareRefusedClaim reports an admission that took nothing.
func shareRefusedClaim(result ShareResult, claim *ShareClaim, err error) ShareResult {
	switch {
	case errors.Is(err, ErrShareOpConflict):
		result.Status, result.Reason = shareResultConflict, "this operation_id was already used for different paths; choose a new operation_id for a new share"
		result.Replayed = true
		result.Files = shareResultFiles(claim.Receipt)
	case claim != nil && claim.Outcome == ShareClaimInvalid:
		result.Status, result.Reason = shareResultInvalid, shareReason(err, "the arguments are not valid")
	default:
		result.Status, result.Reason = shareResultBusy, "another attempt of this operation, or too many shares at once, is still running; retry with the same operation_id"
	}
	return result
}

// settle takes a held claim from whatever its DURABLE receipt says to as far
// as it can honestly go in this call. Every decision reads the claim's own
// bytes; the Receipt view is only ever described, never consulted.
func (t shareTarget) settle(ctx context.Context, claim *ShareClaim, result ShareResult) ShareResult {
	defer claim.Release()
	receipt, err := claim.frozen()
	if err != nil {
		result.Status, result.Reason = shareResultBusy, shareRetryCheck
		return result
	}
	fresh := false
	switch receipt.Status {
	case ShareOpAccepted, ShareOpRefused:
		return shareResultOf(result, receipt)
	case ShareOpPending:
		if reason, ok := t.plan(ctx, claim, receipt); !ok {
			return shareDescribed(result, claim, reason)
		}
		fresh = true
	}
	rows, err := claim.PlannedRows()
	if err != nil {
		return shareDescribed(result, claim, shareRetryCheck)
	}
	recorded, reason := t.record(ctx, receipt.Session, rows, fresh)
	status := ShareOpUnknown
	if len(recorded) == len(rows) {
		status, reason = ShareOpAccepted, ""
	}
	if err := claim.Settle(status, recorded); err != nil {
		// The rows that were appended are on the index either way; the receipt
		// stays as it was, and the next retry reconciles them exactly.
		result = shareDescribed(result, claim, shareRetrySettle)
		result.Status, result.RecordedIDs = ShareOpUnknown, recorded
		return result
	}
	return shareDescribed(result, claim, reason)
}

// shareDescribed is the result as the claim's durable receipt now states it.
func shareDescribed(result ShareResult, claim *ShareClaim, reason string) ShareResult {
	if receipt, err := claim.frozen(); err == nil {
		result = shareResultOf(result, receipt)
	}
	result.Reason = reason
	return result
}

// plan reads every planned file and persists the full plan. A refusal before
// the plan is settled refused and its owned copies are removed. A plan whose
// write did not finish leaves the receipt as it is on disk — pending, or
// planned if the rename landed — for the next retry to find; nothing is
// appended either way, and a planned receipt's copies are never removed.
func (t shareTarget) plan(ctx context.Context, claim *ShareClaim, receipt ShareReceipt) (string, bool) {
	t.removeCopies(receipt)
	rows, reason := t.inspect(ctx, receipt)
	if rows == nil && ctx.Err() != nil {
		// Running out of time, or being cancelled, says nothing about the
		// files: the receipt stays pending, so the same operation_id can be
		// tried again, and the copies taken so far go now.
		t.removeCopies(receipt)
		return shareRetryTime, false
	}
	if rows == nil {
		t.refuse(claim, receipt)
		return reason, false
	}
	if err := claim.Plan(rows); err != nil {
		if errors.Is(err, ErrShareOpInvalid) {
			t.refuse(claim, receipt)
			return "a file's record would be larger than one deliverable row may be", false
		}
		return shareRetryPlan, false
	}
	return "", true
}

// refuse settles a receipt that was never planned as refused, and only then
// removes the copies it took: no row can name them.
func (t shareTarget) refuse(claim *ShareClaim, receipt ShareReceipt) {
	if claim.Settle(ShareOpRefused, nil) == nil {
		receipt.Status = ShareOpRefused
		t.removeCopies(receipt)
	}
}

// inspect builds one row per planned path, under one byte budget for the
// whole operation. Any refusal refuses the operation: a share is all or none.
func (t shareTarget) inspect(ctx context.Context, receipt ShareReceipt) ([]Artifact, string) {
	remaining := shareFileBytes
	rows := make([]Artifact, 0, len(receipt.Planned))
	for _, planned := range receipt.Planned {
		row, reason := t.inspectOne(ctx, receipt, planned, &remaining)
		if reason != "" {
			return nil, filepath.Base(planned.Path) + ": " + reason
		}
		rows = append(rows, row)
	}
	return rows, ""
}

// inspectOne reads EXACTLY planned path i — never a path spelled again from
// the arguments — where it is, or into an owned copy when it lives only in a
// folder the owner can read from but not keep. A link at the file itself is
// refused by the no-follow open; its folders were resolved once, at admission.
func (t shareTarget) inspectOne(ctx context.Context, receipt ShareReceipt, planned SharePlannedRow, remaining *int64) (Artifact, string) {
	path := planned.Path
	roots, reach := canonicalRoots(t.roots), canonicalRoots(t.reach)
	var file, source sharedFile
	var err error
	switch {
	case withinAny(roots, path):
		file, err = inspectSharedFile(ctx, roots, path, remaining)
	case t.copies != "" && withinAny(reach, path):
		file, source, err = t.snapshot(ctx, reach, planned, remaining)
	default:
		return Artifact{}, "it is outside this conversation's folders"
	}
	if err != nil {
		return Artifact{}, shareInspectReason(ctx, err)
	}
	return t.row(receipt, planned, file, source), ""
}

// snapshot copies a reach-only file into the owner's artifacts folder, under a
// folder named for the planned id, in the same single pass that hashes it.
func (t shareTarget) snapshot(ctx context.Context, reach []string, planned SharePlannedRow, remaining *int64) (copied, source sharedFile, err error) {
	if err := os.MkdirAll(t.copies, 0o700); err != nil {
		return sharedFile{}, sharedFile{}, err
	}
	copied, source, err = copySharedFile(ctx, reach, planned.Path, t.copyDir(planned.ID), remaining)
	if err == nil {
		// The copy's own folder was synced with its name; this makes that
		// folder's entry durable too, so a plan never names a copy a crash lost.
		err = syncShareOpDir(t.copies)
	}
	return copied, source, err
}

// row is the exact index row one planned id is appended as.
func (t shareTarget) row(receipt ShareReceipt, planned SharePlannedRow, file, source sharedFile) Artifact {
	prov := receipt.Provenance
	identity := file.File
	now := time.Now().UTC()
	fact := DeliverableFact{
		ID: planned.ID, At: now, Turn: prov.Turn, Run: prov.Run, Call: prov.Call, Task: t.task,
		Origin: DeliverOriginSharedByAgent, File: &identity, Version: "sha256:" + identity.SHA256,
	}
	if source.Path != "" {
		original := source.File
		fact.Source = &original
	}
	row := Artifact{Path: file.Path, Session: receipt.Session, Title: filepath.Base(file.Path), Kind: file.Kind, Created: now, Deliver: &fact}
	if mediaKindOf(file.Media.MIME) == file.Kind {
		// The source id is minted ONCE, here, into the plan; a retry appends
		// the plan's row and never mints another.
		media := file.Media
		media.ID, media.Run, media.Tool, media.Message = NewMediaID(), prov.Run, prov.Call, prov.Turn
		row.Media = &media
	}
	return row
}

// record appends every planned row the index does not already hold, then finds
// each appended row again. fresh says the plan was written in this very call,
// so no append of it can exist yet and the index need not be searched first.
func (t shareTarget) record(ctx context.Context, session string, rows []Artifact, fresh bool) ([]string, string) {
	// ONE DEADLINE FOR EVERY SEARCH. A zero one would read the index without
	// bound, so a call that has none searches nothing and appends nothing.
	deadline, bounded := ctx.Deadline()
	if !bounded {
		return nil, shareRetryCheck
	}
	recorded := make([]string, 0, len(rows))
	for _, row := range rows {
		if ctx.Err() != nil {
			return recorded, shareRetryTime
		}
		if !fresh {
			present, known := shareRowPresent(t.index, session, row, deadline)
			if !known {
				return recorded, shareRetryCheck
			}
			if present {
				recorded = append(recorded, row.Deliver.ID)
				continue
			}
		}
		if err := appendShareRow(t.index, row); err != nil {
			return recorded, shareRetryRecord
		}
		if present, _ := shareRowPresent(t.index, session, row, deadline); !present {
			return recorded, shareRetryCheck
		}
		recorded = append(recorded, row.Deliver.ID)
		if t.emit != nil {
			t.emit(row)
		}
	}
	return recorded, ""
}

// shareRowPresent asks the index for one planned row exactly, by its
// deliverable id and, for media, its source id too. Absent is answered only
// after the whole bounded read; anything else is not known.
func shareRowPresent(index, session string, row Artifact, deadline time.Time) (present, known bool) {
	found, err := FindDeliverable(index, session, row.Deliver.ID, deadline)
	switch {
	case errors.Is(err, ErrMediaNotFound):
		return false, true
	case err != nil:
		return false, false
	}
	// The id is ours and random; a row under it that is not this plan's is not
	// something to append beside or to call recorded.
	if found.Fact.Origin != DeliverOriginSharedByAgent || found.Artifact.Path != row.Path || found.Fact.Version != row.Deliver.Version {
		return false, false
	}
	if row.Media == nil {
		return true, true
	}
	source, err := FindMedia(index, session, row.Media.ID, deadline)
	if err != nil || source.Fact.Origin != MediaShared || source.Artifact.Path != row.Path {
		return false, false
	}
	return true, true
}

// absolute spells every asked path from the conversation's captured base, its
// folders resolved once so the receipt names the file the plan will read; the
// file itself is not followed, so a link there is refused rather than chased.
func (t shareTarget) absolute(paths []string) ([]string, error) {
	if _, _, err := NormalizeSharePayload(paths); err != nil {
		return nil, err
	}
	out := make([]string, len(paths))
	for i, path := range paths {
		if !filepath.IsAbs(path) {
			if t.base == "" {
				return nil, ErrShareOpInvalid
			}
			path = filepath.Join(t.base, path)
		}
		path = filepath.Clean(path)
		out[i] = filepath.Join(canonicalFile(filepath.Dir(path)), filepath.Base(path))
	}
	return out, nil
}

func (t shareTarget) copyDir(id string) string {
	return filepath.Join(t.copies, id)
}

// removeCopies removes the owned copies of a receipt that is pending or was
// just refused: nothing references them, because no row of that receipt can
// have been appended. Ids are checked before they name a folder. A planned,
// unknown or accepted receipt's copies are what its rows name, and stay.
func (t shareTarget) removeCopies(receipt ShareReceipt) {
	if t.copies == "" || (receipt.Status != ShareOpPending && receipt.Status != ShareOpRefused) {
		return
	}
	for _, planned := range receipt.Planned {
		if ValidDeliverableID(planned.ID) {
			_ = os.RemoveAll(t.copyDir(planned.ID))
		}
	}
}

func canonicalRoots(roots []string) []string {
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if root != "" {
			out = append(out, canonicalFile(root))
		}
	}
	return out
}

// shareResultOf states a receipt as the tool reports it.
func shareResultOf(result ShareResult, receipt ShareReceipt) ShareResult {
	result.Status = receipt.Status
	result.RecordedIDs = append([]string(nil), receipt.RecordedIDs...)
	result.Files = shareResultFiles(receipt)
	return result
}

func shareResultFiles(receipt ShareReceipt) []ShareResultFile {
	var files []ShareResultFile
	for _, planned := range receipt.Planned {
		file := ShareResultFile{ID: planned.ID, Path: planned.Path}
		if row := planned.Row; row != nil {
			file.Path, file.Kind, file.Version = row.Path, row.Kind, row.Deliver.Version
			file.MIME, file.Bytes = row.Deliver.File.MIME, row.Deliver.File.Bytes
			if row.Media != nil {
				file.Media = row.Media.ID
			}
			if row.Deliver.Source != nil {
				file.Original = row.Deliver.Source.Canonical
			}
		}
		files = append(files, file)
	}
	return files
}

// shareInspectReason is a refusal in words a person reads, never an errno.
func shareInspectReason(ctx context.Context, err error) string {
	switch {
	case ctx.Err() != nil:
		return "the share ran out of time"
	case errors.Is(err, os.ErrNotExist):
		return "there is no file there"
	case errors.Is(err, errNotRegular):
		return "it is not a regular file, or it is reached through a link"
	case errors.Is(err, ErrShareOpInvalid):
		return "it is larger than the " + byteLimit(int(shareFileBytes)) + " a share may read"
	}
	return "it changed or could not be read while it was being shared"
}

func shareReason(err error, fallback string) string {
	if err != nil && errors.Is(err, ErrShareOpInvalid) {
		if text := err.Error(); len(text) <= 256 {
			return text
		}
	}
	return fallback
}

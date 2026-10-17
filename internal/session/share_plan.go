package session

// share_plan.go is the second durable step of one share_deliverable operation:
// the FULL inspected plan.
//
// THE PLAN IS WRITTEN BEFORE THE FIRST INDEX APPEND AND NEVER AGAIN. Admission
// fixes the ids; Plan fixes, for every id, the exact row that id is appended as
// — path, kind, the bytes' hash and identity, the original a copy was taken
// from, and who handed the file over. A replay of a planned or unknown receipt
// appends from these rows and never inspects the file a second time, so a file
// edited between a crash and its retry cannot change what the first attempt
// promised. A pending receipt carries no rows at all, which is the proof that
// no append for it can have happened yet.

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"
)

// Plan persists rows as the immutable plan of a pending claim, in planned
// order, while the lock is held. It never releases the claim: a failure leaves
// the caller to settle refused (nothing was appended) or simply release.
//
// THE ROWS ARE FROZEN ON THE WAY IN. Each is marshalled and decoded into fresh
// memory before it is validated, so no pointer or slice the caller still holds
// — Deliver, Media, File, Source, From, References — reaches the plan. What
// the plan starts from is the claim's durable bytes, never Receipt.
func (c *ShareClaim) Plan(rows []Artifact) error {
	if c == nil || c.lock == nil {
		return ErrShareOpInvalid
	}
	receipt, err := c.frozen()
	if err != nil {
		return err
	}
	if receipt.Status != ShareOpPending || len(rows) != len(receipt.Planned) {
		return ErrShareOpInvalid
	}
	if time.Now().After(c.deadline) {
		return ErrShareOpBusy
	}
	for i := range receipt.Planned {
		frozen, err := freezeShareRow(rows[i])
		if err != nil {
			return err
		}
		receipt.Planned[i].Row = frozen
	}
	receipt.Status = ShareOpPlanned
	receipt.WrittenAt = time.Now().UTC()
	raw, err := writeShareReceipt(c.dir, receipt)
	if err != nil {
		return err
	}
	return c.adopt(raw)
}

// PlannedRows is the plan as the ONLY thing an append may write: a fresh decode
// of the durable receipt, every row its own memory, in planned order. A
// receipt without a written plan has nothing to append.
func (c *ShareClaim) PlannedRows() ([]Artifact, error) {
	receipt, err := c.frozen()
	if err != nil {
		return nil, err
	}
	if !receipt.PlanWritten() {
		return nil, ErrShareOpInvalid
	}
	rows := make([]Artifact, len(receipt.Planned))
	for i, planned := range receipt.Planned {
		rows[i] = *planned.Row
	}
	return rows, nil
}

// freezeShareRow is row in memory nobody else holds.
func freezeShareRow(row Artifact) (*Artifact, error) {
	raw, err := json.Marshal(row)
	if err != nil {
		return nil, ErrShareOpInvalid
	}
	var frozen Artifact
	if err := json.Unmarshal(raw, &frozen); err != nil {
		return nil, ErrShareOpInvalid
	}
	return &frozen, nil
}

// PlanWritten says the receipt carries its full plan, so its rows are the ONLY
// thing a retry may append.
func (r ShareReceipt) PlanWritten() bool {
	return len(r.Planned) > 0 && r.Planned[0].Row != nil
}

// validateSharePlan is the all-or-none law on rows: a pending receipt has none,
// a planned, accepted or unknown one has every one, and a refused one may have
// either (refused before or after it was planned, never after an append).
func validateSharePlan(r ShareReceipt) error {
	rows := 0
	for _, planned := range r.Planned {
		if planned.Row == nil {
			continue
		}
		rows++
		if err := validateSharePlannedRow(r, planned); err != nil {
			return err
		}
	}
	switch {
	case rows != 0 && rows != len(r.Planned):
		return ErrShareOpInvalid
	case r.Status == ShareOpPending && rows != 0:
		return ErrShareOpInvalid
	case r.Status != ShareOpPending && r.Status != ShareOpRefused && rows == 0:
		return ErrShareOpInvalid
	}
	return nil
}

// validateSharePlannedRow refuses a planned row that is not exactly what this
// engine writes for a shared file: its own id, this session, the shared origin
// with nothing claimed about authorship or state, attributed to the ORIGINAL
// receipt's run, message and call, and a row that fits.
func validateSharePlannedRow(r ShareReceipt, planned SharePlannedRow) error {
	row := planned.Row
	fact := row.Deliver
	if fact == nil || fact.ID != planned.ID || row.Session != r.Session || strings.TrimSpace(row.Title) == "" {
		return ErrShareOpInvalid
	}
	if !shareFactOnlyShares(fact) || !shareRowAttributedTo(row, r.Provenance) {
		return ErrShareOpInvalid
	}
	if err := validateSharePlannedFiles(row); err != nil {
		return err
	}
	line, err := json.Marshal(row)
	if err != nil || !deliverRowFits(line) {
		return ErrShareOpInvalid
	}
	return nil
}

// shareRowAttributedTo says the row names exactly the run, message and call
// the operation was first handed over under — in its deliverable fact and in
// its media fact alike — so a plan can never attribute to a later caller.
func shareRowAttributedTo(row *Artifact, prov ShareProvenance) bool {
	fact := row.Deliver
	if fact.Run != prov.Run || fact.Turn != prov.Turn || fact.Call != prov.Call {
		return false
	}
	media := row.Media
	return media == nil || media.Run == prov.Run && media.Message == prov.Turn && media.Tool == prov.Call
}

// shareFactOnlyShares says the fact claims sharing and nothing else: no
// authorship, no state, no update, no branch a landing could settle it by.
func shareFactOnlyShares(fact *DeliverableFact) bool {
	if fact.Origin != DeliverOriginSharedByAgent || fact.Authored || fact.Update || fact.Removed {
		return false
	}
	return fact.State == "" && fact.Kept == nil && fact.Branch == "" && fact.Rel == ""
}

// validateSharePlannedFiles ties the row's path, its file identity, an owned
// copy's original and a media fact to the one hash every byte was read for.
func validateSharePlannedFiles(row *Artifact) error {
	file := row.Deliver.File
	if file == nil || len(file.SHA256) != 64 || !isHex(file.SHA256) || row.Deliver.Version != "sha256:"+file.SHA256 {
		return ErrShareOpInvalid
	}
	if !shareAbsolute(row.Path) || file.Canonical != row.Path {
		return ErrShareOpInvalid
	}
	if !shareSourceMatches(row.Deliver.Source, file) || !shareMediaMatches(row.Media, row.Path, file) {
		return ErrShareOpInvalid
	}
	return nil
}

// shareSourceMatches says an owned copy's original holds the copy's bytes.
func shareSourceMatches(source, file *DeliverableFile) bool {
	return source == nil || shareAbsolute(source.Canonical) && source.SHA256 == file.SHA256 && source.Bytes == file.Bytes
}

// shareMediaMatches says a media fact is the published one for these bytes.
func shareMediaMatches(media *MediaFact, path string, file *DeliverableFile) bool {
	if media == nil {
		return true
	}
	return media.Origin == MediaPublished && validMediaID(media.ID) && media.Canonical == path && media.SHA256 == file.SHA256
}

func shareAbsolute(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path
}

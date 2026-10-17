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
	"reflect"
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
// receipt's run, message and call, built from exactly the planned path, and a
// row that fits.
func validateSharePlannedRow(r ShareReceipt, planned SharePlannedRow) error {
	row := planned.Row
	fact := row.Deliver
	if fact == nil || fact.ID != planned.ID || row.Session != r.Session || row.Created.IsZero() {
		return ErrShareOpInvalid
	}
	if !shareFactOnlyShares(fact) || !shareRowAttributedTo(row, r.Provenance) || !shareRowBindsPath(row, planned) {
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

// shareRowBindsPath says row i was built from planned path i and from nothing
// else. In place, the row IS that file. A snapshot names that file as its
// original, and sits in a folder of its own named for the planned id, under the
// same file name — so a plan for b.png can never record /elsewhere/b2.png.
//
// WHAT THIS DOES NOT CHECK: which folder the copies live under. The receipt
// does not record the copies root, so a hand-forged receipt could name a copy
// under some other <root>/<id>/<name>. The producer never builds one — every
// copy is made under the owner's artifacts/shared ([shareTarget.snapshot]) and
// opened only under the owner's roots by the existing source authority — and a
// writer of ~/.codeaf is outside what a receipt defends against.
func shareRowBindsPath(row *Artifact, planned SharePlannedRow) bool {
	name := filepath.Base(planned.Path)
	if row.Title != name || filepath.Base(row.Path) != name {
		return false
	}
	if source := row.Deliver.Source; source != nil {
		return source.Canonical == planned.Path && row.Path != planned.Path && filepath.Base(filepath.Dir(row.Path)) == planned.ID
	}
	return row.Path == planned.Path
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

// shareFactOnlyShares says the fact claims sharing and nothing else.
//
// IT IS A LIST OF WHAT MAY BE SAID, not of what may not: the fact is compared
// with a copy holding only the fields a share records, so a field added to
// DeliverableFact later is refused here until somebody decides a share may
// claim it. No authorship, state, inputs, profile, update, branch or keep.
func shareFactOnlyShares(fact *DeliverableFact) bool {
	if fact.Origin != DeliverOriginSharedByAgent || fact.At.IsZero() {
		return false
	}
	allowed := DeliverableFact{
		ID: fact.ID, At: fact.At, Turn: fact.Turn, Run: fact.Run, Task: fact.Task, Origin: fact.Origin,
		Call: fact.Call, Version: fact.Version, File: fact.File, Source: fact.Source,
	}
	return reflect.DeepEqual(*fact, allowed)
}

// validateSharePlannedFiles ties the row's path, its kind, its file identity,
// an owned copy's original and a media fact to the one hash every byte was read
// for.
func validateSharePlannedFiles(row *Artifact) error {
	file := row.Deliver.File
	if file == nil || len(file.SHA256) != 64 || !isHex(file.SHA256) || row.Deliver.Version != "sha256:"+file.SHA256 {
		return ErrShareOpInvalid
	}
	if !shareAbsolute(row.Path) || file.Canonical != row.Path || file.Bytes < 0 || file.MIME == "" {
		return ErrShareOpInvalid
	}
	if !shareKindMatches(row) || !shareSourceMatches(row.Deliver.Source, file) || !shareMediaMatches(row.Media, row.Path, file) {
		return ErrShareOpInvalid
	}
	return nil
}

// shareKindMatches says the kind is one a share records, and that a media fact
// is present exactly when the kind is a media kind the header confirmed.
func shareKindMatches(row *Artifact) bool {
	switch row.Kind {
	case "image", "audio", "video":
		return row.Media != nil && mediaKindOf(row.Media.MIME) == row.Kind
	case DeliverableMarkdown, DeliverableMermaid, shareKindFile:
		return row.Media == nil
	}
	return false
}

// shareSourceMatches says an owned copy's original holds the copy's bytes.
func shareSourceMatches(source, file *DeliverableFile) bool {
	if source == nil {
		return true
	}
	return shareAbsolute(source.Canonical) && source.SHA256 == file.SHA256 && source.Bytes == file.Bytes && source.MIME == file.MIME
}

// shareMediaMatches says a media fact is the shared one for exactly these
// bytes, and claims nothing beyond what the bytes' own header said.
//
// LIKE THE FACT, IT IS A LIST OF WHAT MAY BE SAID. Sharing is not generating:
// no model, no request count, no batch, no price, no charge, no declared output,
// no observed change, no references and nothing an evidence row says. A field
// added to MediaFact later is refused until a share is allowed to claim it.
func shareMediaMatches(media *MediaFact, path string, file *DeliverableFile) bool {
	if media == nil {
		return true
	}
	allowed := MediaFact{
		ID: media.ID, Origin: media.Origin, Canonical: media.Canonical, MIME: media.MIME,
		Bytes: media.Bytes, ModNS: media.ModNS, CtimeNS: media.CtimeNS, Dev: media.Dev, Ino: media.Ino, SHA256: media.SHA256,
		Width: media.Width, Height: media.Height, DurationMS: media.DurationMS,
		Run: media.Run, Message: media.Message, Tool: media.Tool,
	}
	if !reflect.DeepEqual(*media, allowed) || media.Origin != MediaShared || !validMediaID(media.ID) {
		return false
	}
	same := media.Canonical == path && media.SHA256 == file.SHA256 && media.Bytes == file.Bytes && media.MIME == file.MIME
	return same && media.ModNS == file.ModNS && media.CtimeNS == file.CtimeNS && media.Dev == file.Dev && media.Ino == file.Ino
}

func shareAbsolute(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path
}

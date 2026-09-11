package direction

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// writeTx is one fenced write. EVERY DIRECTION WRITE GOES THROUGH Store.write
// AND ENDS IN writeTx.put: one BEGIN IMMEDIATE, the pointer read and compared,
// the revision and its rows written, the live index maintained, the pointer
// moved, one commit. No model or network call happens inside; a writer drafts
// first and the write carries the result (L1, L4).
type writeTx struct {
	ctx context.Context
	tx  *sql.Tx
	at  time.Time
}

func (s *Store) write(ctx context.Context, fn func(*writeTx) error) error {
	return workspace.WriteImmediate(ctx, s.ws, func(tx *sql.Tx) error {
		return fn(&writeTx{ctx: ctx, tx: tx, at: s.now().UTC()})
	})
}

// fenced reads the record's current revision and refuses a writer who read an
// older one.
func (w *writeTx) fenced(f Fence) (Revision, error) {
	cur, err := current(w.ctx, w.tx, f.ID)
	if err != nil {
		return Revision{}, err
	}
	if cur.Revision != f.Revision {
		return Revision{}, fmt.Errorf("%w: %s: expected revision %d, current revision is %d", ErrConflict, f.ID, f.Revision, cur.Revision)
	}
	return cur, nil
}

// personReceipt turns a person's receipt into the stored one, if the state
// carries one.
func (w *writeTx) personReceipt(r PersonReceipt, state State) Receipt {
	if !receiptRequired(state, AuthorPerson) {
		return Receipt{}
	}
	return Receipt{Actor: ActorPerson, Door: r.door, Ref: r.ref, At: w.at}
}

// put writes one revision and moves the record to it. A record's first
// revision creates its identity; prev is the revision it follows, if any.
func (w *writeTx) put(r Revision, prev *Revision) error {
	if err := permit(prev, r); err != nil {
		return err
	}
	if err := w.checkReferences(r, prev); err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(r.Text))
	r.TextSHA256 = hex.EncodeToString(sum[:])
	if r.WrittenAt.IsZero() {
		// Only an import brings a time of its own: when the old store wrote it.
		r.WrittenAt = w.at
	}
	if r.Revision == 1 {
		if _, err := w.tx.ExecContext(w.ctx, "INSERT INTO direction_records(id,revision,created_at) VALUES (?,?,?)",
			r.ID, r.Revision, stamp(w.at)); err != nil {
			return err
		}
	}
	if _, err := w.tx.ExecContext(w.ctx, `INSERT INTO direction_revisions(record_id,revision,kind,state,state_reason,
 title,text,text_sha256,quote,quote_origin,source_class,source_id,source_session,source_hint,source_sha256,
 author_class,author_ref,receipt_actor,receipt_door,receipt_ref,receipt_at,written_at)
 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Revision, r.Kind, r.State, r.StateReason, r.Title, r.Text, r.TextSHA256, r.Quote, r.QuoteOrigin,
		r.Source.Class, r.Source.ID, r.Source.Session, r.Source.Hint, r.Source.SHA256, r.Author.Class, r.Author.Ref,
		r.Receipt.Actor, r.Receipt.Door, r.Receipt.Ref, stamp(r.Receipt.At), stamp(r.WrittenAt)); err != nil {
		return err
	}
	for position, t := range r.Targets {
		if _, err := w.tx.ExecContext(w.ctx, `INSERT INTO direction_targets(record_id,revision,position,target_kind,ref_id,session_id,reach)
 VALUES (?,?,?,?,?,?,?)`, r.ID, r.Revision, position, t.Kind, t.Ref, t.Session, t.Reach); err != nil {
			return err
		}
	}
	for _, e := range r.Exclusions {
		if _, err := w.tx.ExecContext(w.ctx, `INSERT INTO direction_exclusions(record_id,revision,target_kind,ref_id,session_id,at)
 VALUES (?,?,?,?,?,?)`, r.ID, r.Revision, e.Kind, e.Ref, e.Session, stamp(e.At)); err != nil {
			return err
		}
	}
	for _, l := range r.Links {
		if _, err := w.tx.ExecContext(w.ctx, `INSERT INTO direction_links(record_id,revision,link_kind,to_ref,to_revision)
 VALUES (?,?,?,?,?)`, r.ID, r.Revision, l.Kind, l.To, l.ToRevision); err != nil {
			return err
		}
	}
	if r.Revision > 1 {
		result, err := w.tx.ExecContext(w.ctx, "UPDATE direction_records SET revision=? WHERE id=? AND revision=?", r.Revision, r.ID, r.Revision-1)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil {
			return err
		} else if n != 1 {
			return fmt.Errorf("%w: %s did not stand at revision %d", ErrConflict, r.ID, r.Revision-1)
		}
	}
	return w.refreshLive(r)
}

// checkReferences asks this store about what it owns: a record a link names
// must exist and must not be the record itself, and a folder a new writer
// names must exist. An import keeps a dangling folder id — it is reported by
// the import, not invented away — and a later revision may keep what the one
// before it carried.
func (w *writeTx) checkReferences(r Revision, prev *Revision) error {
	for _, l := range r.Links {
		if l.Kind == DerivedFrom && !isRecordID(l.To) {
			continue
		}
		if l.To == r.ID {
			return invalid("a record does not link to itself")
		}
		if err := w.checkLinkTarget(r, l); err != nil {
			return err
		}
	}
	if r.Author.Class == AuthorMigration {
		return nil
	}
	carried := map[placeKey]bool{}
	if prev != nil {
		for _, place := range placesOf(*prev) {
			carried[place] = true
		}
	}
	for _, place := range placesOf(r) {
		if place.kind != TargetCollection || carried[place] {
			continue
		}
		var found int
		err := w.tx.QueryRowContext(w.ctx, "SELECT 1 FROM collections WHERE id=?", place.ref).Scan(&found)
		if err == sql.ErrNoRows {
			return fmt.Errorf("%w: %v", workspace.ErrNotFound, place.ref)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// checkLinkTarget reads the record a link names. PRECEDENCE BELONGS TO THE
// PERSON (R4): an import copies an overrides or conflicts_with link only onto
// a record whose current revision an import wrote — and since an import never
// writes after another writer, one no one else has written. A record the
// person has written is theirs to rank.
func (w *writeTx) checkLinkTarget(r Revision, l Link) error {
	revision, err := pointer(w.ctx, w.tx, l.To)
	if err != nil || r.Author.Class != AuthorMigration || (l.Kind != Overrides && l.Kind != ConflictsWith) {
		return err
	}
	var by AuthorClass
	if err := w.tx.QueryRowContext(w.ctx, "SELECT author_class FROM direction_revisions WHERE record_id=? AND revision=?",
		l.To, revision).Scan(&by); err != nil {
		return err
	}
	if by != AuthorMigration {
		return fmt.Errorf("%w: %s revision %d was written by %s; an import does not rank a record it did not write (%s)",
			ErrTransition, l.To, revision, by, l.Kind)
	}
	return nil
}

func placesOf(r Revision) []placeKey {
	places := make([]placeKey, 0, len(r.Targets)+len(r.Exclusions))
	for _, t := range r.Targets {
		places = append(places, placeKey{t.Kind, t.Ref, t.Session})
	}
	for _, e := range r.Exclusions {
		places = append(places, placeKey{e.Kind, e.Ref, e.Session})
	}
	return places
}

// rejectedText asks the partial index of rejected wording.
var rejectedText = named("rejected-text", "SELECT EXISTS(SELECT 1 FROM direction_revisions WHERE text_sha256=? AND state=?)")

// rejectedBefore reports whether the person rejected this exact wording.
func (w *writeTx) rejectedBefore(text string) (bool, error) {
	sum := sha256.Sum256([]byte(text))
	var rejected bool
	err := w.tx.QueryRowContext(w.ctx, rejectedText, hex.EncodeToString(sum[:]), Rejected).Scan(&rejected)
	return rejected, err
}

// compose builds the next revision of cur from a draft.
func (w *writeTx) compose(d Draft, id string, revision int, state State, author Author, prev *Revision) (Revision, error) {
	d, err := d.normalize(author.Class == AuthorMigration, inheritedFrom(prev), w.at)
	if err != nil {
		return Revision{}, err
	}
	return Revision{ID: id, Revision: revision, Kind: d.Kind, State: state, Title: d.Title, Text: d.Text,
		Quote: d.Quote, QuoteOrigin: d.QuoteOrigin, Source: d.Source, Author: author,
		Targets: d.Targets, Exclusions: d.Exclusions, Links: d.Links}, nil
}

// create writes a new record's first revision on behalf of an actor.
func (s *Store) create(ctx context.Context, d Draft, by Actor, state State) (Revision, error) {
	if err := by.check(); err != nil {
		return Revision{}, err
	}
	id, err := workspace.NewID()
	if err != nil {
		return Revision{}, err
	}
	var written Revision
	err = s.write(ctx, func(w *writeTx) error {
		r, err := w.compose(d, id, 1, state, by.author, nil)
		if err != nil {
			return err
		}
		if err := w.refuseRejected(r, by); err != nil {
			return err
		}
		if err := w.put(r, nil); err != nil {
			return err
		}
		written, err = current(ctx, w.tx, id)
		return err
	})
	return written, err
}

// refuseRejected is non-restoration (C16): wording the person rejected is not
// proposed again by anyone but the person.
func (w *writeTx) refuseRejected(r Revision, by Actor) error {
	if by.person() || !r.Kind.directive() {
		return nil
	}
	rejected, err := w.rejectedBefore(r.Text)
	if err != nil {
		return err
	}
	if rejected {
		return ErrRejectedText
	}
	return nil
}

// Propose writes a new rule or decision that does not govern until the person
// accepts it.
func (s *Store) Propose(ctx context.Context, d Draft, by Actor) (Revision, error) {
	if !d.Kind.directive() {
		return Revision{}, invalid("a finding is written with Note")
	}
	return s.create(ctx, d, by, Proposed)
}

// Note writes a finding: sourced information that never governs.
func (s *Store) Note(ctx context.Context, d Draft, by Actor) (Revision, error) {
	if d.Kind != Finding {
		return Revision{}, invalid("a rule or a decision is written with Propose")
	}
	return s.create(ctx, d, by, Informational)
}

// Revise changes a record's wording, places or links and keeps its identity
// and state. The person revises any live record, and a revision of an
// accepted one carries their receipt; the model revises only its own
// proposal (§4.1). permit decides; this door only names the missing receipt.
func (s *Store) Revise(ctx context.Context, f Fence, d Draft, by Actor) (Revision, error) {
	if err := by.check(); err != nil {
		return Revision{}, err
	}
	return s.transition(ctx, f, func(w *writeTx, cur Revision) (Revision, error) {
		if cur.State == Accepted && !by.person() {
			return Revision{}, fmt.Errorf("%w: only the person revises an accepted record", ErrNoReceipt)
		}
		next, err := w.compose(d, cur.ID, cur.Revision+1, cur.State, by.author, &cur)
		if err != nil {
			return Revision{}, err
		}
		if err := w.refuseRejected(next, by); err != nil {
			return Revision{}, err
		}
		next.Receipt = w.personReceipt(by.receipt, next.State)
		if by.person() {
			return next, w.checkStatement(next, by.receipt)
		}
		return next, nil
	}, nil)
}

// Accept gives a proposal — or a withdrawn record the person takes back —
// authority. A proposal that supersedes other records replaces them in the
// same transaction, each fenced at the revision the proposal named.
func (s *Store) Accept(ctx context.Context, f Fence, r PersonReceipt) (Revision, error) {
	var fromProposal bool
	return s.personChange(ctx, f, r, func(w *writeTx, cur Revision) (Revision, error) {
		fromProposal = cur.State == Proposed
		return w.byPerson(cur, Accepted, "", r)
	}, func(w *writeTx, accepted Revision) error {
		// A record taken back from withdrawn replaced what it replaces when it
		// was first accepted; only a proposal's replacements happen now.
		if !fromProposal {
			return nil
		}
		return w.supersedeLinked(accepted, r)
	})
}

// Reject closes a proposal. Its wording is kept, and the same wording is not
// proposed again by anyone but the person.
func (s *Store) Reject(ctx context.Context, f Fence, r PersonReceipt) (Revision, error) {
	return s.personChange(ctx, f, r, func(w *writeTx, cur Revision) (Revision, error) {
		return w.byPerson(cur, Rejected, "", r)
	}, nil)
}

// Withdraw stops an accepted record from applying, or retires a finding, with
// the person's reason, and keeps its history. A withdrawn rule or decision
// comes back only through Accept; a withdrawn finding stays withdrawn.
func (s *Store) Withdraw(ctx context.Context, f Fence, reason string, r PersonReceipt) (Revision, error) {
	if !workspace.ValidLine(reason, maxReason) {
		return Revision{}, invalid("a withdrawal says why in at most %d bytes", maxReason)
	}
	return s.personChange(ctx, f, r, func(w *writeTx, cur Revision) (Revision, error) {
		return w.byPerson(cur, Withdrawn, reason, r)
	}, nil)
}

// Link adds or replaces one link on a live record. Overrides and conflicts
// are precedence the person states, so every link change is theirs.
func (s *Store) Link(ctx context.Context, f Fence, l Link, r PersonReceipt) (Revision, error) {
	return s.personChange(ctx, f, r, func(w *writeTx, cur Revision) (Revision, error) {
		links := make([]Link, 0, len(cur.Links)+1)
		for _, existing := range cur.Links {
			if existing.Kind != l.Kind || existing.To != l.To {
				links = append(links, existing)
			}
		}
		return w.relink(cur, append(links, l), r)
	}, nil)
}

// Unlink removes one link from a live record.
func (s *Store) Unlink(ctx context.Context, f Fence, kind LinkKind, to string, r PersonReceipt) (Revision, error) {
	return s.personChange(ctx, f, r, func(w *writeTx, cur Revision) (Revision, error) {
		links := make([]Link, 0, len(cur.Links))
		for _, existing := range cur.Links {
			if existing.Kind != kind || existing.To != to {
				links = append(links, existing)
			}
		}
		if len(links) == len(cur.Links) {
			return Revision{}, fmt.Errorf("%w: %s has no %s link to %s", ErrNotFound, cur.ID, kind, to)
		}
		return w.relink(cur, links, r)
	}, nil)
}

func (w *writeTx) relink(cur Revision, links []Link, r PersonReceipt) (Revision, error) {
	d := cur.draft()
	d.Links = links
	next, err := w.compose(d, cur.ID, cur.Revision+1, cur.State, Author{Class: AuthorPerson, Ref: r.ref}, &cur)
	if err != nil {
		return Revision{}, err
	}
	next.Receipt = w.personReceipt(r, next.State)
	return next, nil
}

// Supersede writes a new accepted rule or decision that replaces one or more
// accepted ones, and moves every old one to superseded in the same
// transaction, each fenced. It is all or nothing: one old record that moved,
// or that is not accepted, refuses the whole replacement.
func (s *Store) Supersede(ctx context.Context, d Draft, olds []Fence, r PersonReceipt) (Revision, error) {
	if !r.valid() {
		return Revision{}, ErrNoReceipt
	}
	if len(olds) == 0 {
		return Revision{}, invalid("a replacement names what it replaces")
	}
	id, err := workspace.NewID()
	if err != nil {
		return Revision{}, err
	}
	links := append(make([]Link, 0, len(d.Links)+len(olds)), d.Links...)
	named := make(map[string]bool, len(olds))
	for _, old := range olds {
		if named[old.ID] {
			return Revision{}, invalid("a replacement names each record it replaces once")
		}
		named[old.ID] = true
		links = append(links, Link{Kind: Supersedes, To: old.ID, ToRevision: old.Revision})
	}
	d.Links = links
	var written Revision
	err = s.write(ctx, func(w *writeTx) error {
		next, err := w.compose(d, id, 1, Accepted, Author{Class: AuthorPerson, Ref: r.ref}, nil)
		if err != nil {
			return err
		}
		next.Receipt = w.personReceipt(r, next.State)
		if err := w.checkStatement(next, r); err != nil {
			return err
		}
		if err := w.put(next, nil); err != nil {
			return err
		}
		if err := w.supersedeLinked(next, r); err != nil {
			return err
		}
		written, err = current(ctx, w.tx, id)
		return err
	})
	return written, err
}

// supersedeLinked moves every record the revision supersedes to superseded,
// fenced at the revision the link names.
func (w *writeTx) supersedeLinked(by Revision, r PersonReceipt) error {
	for _, l := range by.Links {
		if l.Kind != Supersedes {
			continue
		}
		old, err := w.fenced(Fence{ID: l.To, Revision: l.ToRevision})
		if err != nil {
			return err
		}
		next, err := w.byPerson(old, Superseded, "", r)
		if err != nil {
			return err
		}
		if err := w.put(next, &old); err != nil {
			return err
		}
	}
	return nil
}

// byPerson is a state change the person makes: the same content, a new state,
// their receipt. It is a change of state, never a revision in place.
func (w *writeTx) byPerson(cur Revision, state State, reason string, r PersonReceipt) (Revision, error) {
	if cur.State == state {
		return Revision{}, fmt.Errorf("%w: %s is already %s", ErrTransition, cur.ID, state)
	}
	next, err := w.compose(cur.draft(), cur.ID, cur.Revision+1, state, Author{Class: AuthorPerson, Ref: r.ref}, &cur)
	if err != nil {
		return Revision{}, err
	}
	next.StateReason = reason
	next.Receipt = w.personReceipt(r, state)
	return next, nil
}

// checkStatement holds a statement receipt to what it verified: the revision
// it authorizes quotes the person's words and cites the hash of their line.
func (w *writeTx) checkStatement(next Revision, r PersonReceipt) error {
	if r.door != DoorStatement || next.Receipt == (Receipt{}) {
		return nil
	}
	if next.Quote != r.quote || next.QuoteOrigin != PersonSaid || next.Source.SHA256 != r.ref {
		return invalid("a statement authorizes only the words it verified, cited by the hash of their line")
	}
	return nil
}

// personChange runs a person-only transition.
func (s *Store) personChange(ctx context.Context, f Fence, r PersonReceipt, next func(*writeTx, Revision) (Revision, error), after func(*writeTx, Revision) error) (Revision, error) {
	if !r.valid() {
		return Revision{}, ErrNoReceipt
	}
	return s.transition(ctx, f, func(w *writeTx, cur Revision) (Revision, error) {
		rev, err := next(w, cur)
		if err != nil {
			return Revision{}, err
		}
		return rev, w.checkStatement(rev, r)
	}, after)
}

// transition is a fenced change of one existing record. after, when given,
// runs in the same transaction once the new revision is written.
func (s *Store) transition(ctx context.Context, f Fence, next func(*writeTx, Revision) (Revision, error), after func(*writeTx, Revision) error) (Revision, error) {
	var written Revision
	err := s.write(ctx, func(w *writeTx) error {
		cur, err := w.fenced(f)
		if err != nil {
			return err
		}
		rev, err := next(w, cur)
		if err != nil {
			return err
		}
		if err := refuseNewSupersedes(cur, rev); err != nil {
			return err
		}
		if err := w.put(rev, &cur); err != nil {
			return err
		}
		if after != nil {
			if err := after(w, rev); err != nil {
				return err
			}
		}
		written, err = current(ctx, w.tx, f.ID)
		return err
	})
	return written, err
}

// refuseNewSupersedes keeps replacement in its two doors: a proposal that
// names what it replaces, applied when it is accepted, and Supersede. A
// supersedes link added to a record that is no longer a proposal would claim
// a replacement nothing performed.
func refuseNewSupersedes(cur, next Revision) error {
	if next.State == Proposed {
		return nil
	}
	had := make(map[linkKey]bool, len(cur.Links))
	for _, l := range cur.Links {
		had[linkKey{l.Kind, l.To}] = true
	}
	for _, l := range next.Links {
		if l.Kind == Supersedes && !had[linkKey{l.Kind, l.To}] {
			return invalid("a replacement is proposed and accepted, or made with Supersede")
		}
	}
	return nil
}

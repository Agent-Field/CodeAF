package direction

import (
	"fmt"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// ── WHO MAY WRITE WHAT (design §4.1) ────────────────────────────────────────
//
// THIS TABLE IS §4.1, AND THE WRITE PATH CONSULTS NOTHING ELSE. Every revision
// any door builds — a proposal, a person's answer, a revision, an import — is
// held to it by permit, inside the transaction that writes it. The doors may
// refuse earlier with a kinder message; none of them decides.
//
// Cells the design leaves unwritten are refused, not guessed. Two readings
// are needed where §4.1 is silent and another section speaks:
//   - the person revises a live record in place (§1.1 "revise"), which is how
//     exclusions and links are added or removed;
//   - an import (§3.4) is the only writer that moves a finding out of the
//     informational state; §4.1 gives no other writer that transition.

// move is one step of a record's state, from its current revision to the next.
// A move to the same state is a revision in place.
type move struct{ from, to State }

// createCell is one thing a writer may create: a record of one of these kinds
// in this state, citing a required source class and quote origin when set, and
// naming no place when untargeted.
type createCell struct {
	kinds      []Kind
	state      State
	source     SourceClass
	origin     QuoteOrigin
	untargeted bool
}

func (c createCell) admits(r Revision) bool {
	kindOK := false
	for _, k := range c.kinds {
		kindOK = kindOK || k == r.Kind
	}
	return kindOK && r.State == c.state &&
		(c.source == "" || r.Source.Class == c.source) &&
		(c.origin == "" || r.QuoteOrigin == c.origin) &&
		(!c.untargeted || len(r.Targets) == 0)
}

// rights is one row of §4.1.
type rights struct {
	creates []createCell
	moves   map[move]bool
	// ownOnly holds a writer to records whose current revision it wrote,
	// class and reference both.
	ownOnly bool
	// links are the link kinds its revisions may carry. Overrides and
	// conflicts_with are precedence, and precedence is the person's (R4).
	links map[LinkKind]bool
	// excludes is whether its revisions may carry exclusions: "not here"
	// places are made only by the person (§1.2), or copied by an import.
	excludes bool
	// followsOthers is whether it may write a revision after another
	// writer's. An import never does: a record a person has touched since
	// it was imported is theirs (L1).
	followsOthers bool
}

var (
	directive  = []Kind{Rule, Decision}
	provenance = map[LinkKind]bool{DerivedFrom: true}
	allLinks   = map[LinkKind]bool{Supersedes: true, Overrides: true, ConflictsWith: true, DerivedFrom: true}
)

// section41 is the design's table, row by row. The quoted words are §4.1's.
var section41 = map[AuthorClass]rights{
	// "Person: any kind, any state it asserts. proposed → accepted / rejected;
	// accepted → withdrawn / superseded; withdrawn → accepted; add or remove
	// exclusions; write overrides / conflicts_with." It creates an accepted
	// record only as a replacement it makes (Supersede).
	AuthorPerson: {
		creates: []createCell{{kinds: directive, state: Proposed}, {kinds: []Kind{Finding}, state: Informational},
			{kinds: directive, state: Accepted}},
		moves: map[move]bool{
			{Proposed, Accepted}: true, {Proposed, Rejected}: true,
			{Accepted, Withdrawn}: true, {Accepted, Superseded}: true,
			{Withdrawn, Accepted}: true,
			{Proposed, Proposed}:  true, {Accepted, Accepted}: true, {Informational, Informational}: true,
		},
		links: allLinks, excludes: true, followsOthers: true,
	},
	// "Model in a live conversation: proposed rule/decision; informational
	// finding. Revise its own still-proposed record." Its propose_change is a
	// proposal naming what it replaces, applied only if the person accepts.
	AuthorModel: {
		creates: []createCell{{kinds: directive, state: Proposed}, {kinds: []Kind{Finding}, state: Informational}},
		moves:   map[move]bool{{Proposed, Proposed}: true}, ownOnly: true, followsOthers: true,
		links: map[LinkKind]bool{Supersedes: true, DerivedFrom: true},
	},
	// "Memory extractor and tidy: proposed decision only. Zero targets,
	// quote_origin=model_extracted."
	AuthorExtractor: {
		creates: []createCell{{kinds: []Kind{Decision}, state: Proposed, origin: ModelExtracted, untargeted: true}},
		links:   provenance,
	},
	// "Unattended run / InTask / T06 sourced finding: informational finding;
	// proposed record with source_class=occurrence."
	AuthorRun: {
		creates: []createCell{{kinds: []Kind{Finding}, state: Informational},
			{kinds: directive, state: Proposed, source: SourceOccurrence}},
		links: provenance,
	},
	// "Voice / inter-folder outcome [DEPENDS ON Q7]": under Q7 yes, a closing
	// decision-proposal is a proposed decision with
	// source_class=conversation_outcome, and findings are informational. If
	// Q7 is no, only this row changes.
	AuthorVoice: {
		creates: []createCell{{kinds: []Kind{Decision}, state: Proposed, source: SourceConversationOutcome},
			{kinds: []Kind{Finding}, state: Informational}},
		links: provenance,
	},
	// "Steward (delegated principal): proposed." Never accept.
	AuthorSteward: {
		creates: []createCell{{kinds: directive, state: Proposed}},
		links:   provenance,
	},
	// "Migration: any state, only with a legacy-copied receipt. Import
	// revisions before P3. Never mint receipt_actor=person."
	AuthorMigration: {
		creates: migrationCreates(), moves: migrationMoves(),
		links: allLinks, excludes: true,
	},
}

func migrationCreates() []createCell {
	var cells []createCell
	for _, s := range []State{Proposed, Accepted, Rejected, Withdrawn, Superseded, Informational} {
		cells = append(cells, createCell{kinds: []Kind{Rule, Decision, Finding}, state: s})
	}
	return cells
}

func migrationMoves() map[move]bool {
	states := []State{Proposed, Accepted, Rejected, Withdrawn, Superseded, Informational}
	moves := map[move]bool{}
	for _, from := range states {
		for _, to := range states {
			moves[move{from, to}] = true
		}
	}
	return moves
}

// permit holds one revision to §4.1: may this writer create it, or make this
// move from the revision before it, with these places and links, and does it
// carry exactly the receipt its state needs. prev is nil for a new record.
func permit(prev *Revision, next Revision) error {
	if !next.State.fits(next.Kind) {
		return fmt.Errorf("%w: a %s cannot be %s", ErrTransition, next.Kind, next.State)
	}
	row, ok := section41[next.Author.Class]
	if !ok {
		return fmt.Errorf("%w: %q is not a writer", ErrTransition, next.Author.Class)
	}
	if err := row.admits(prev, next); err != nil {
		return err
	}
	if len(next.Exclusions) > 0 && !row.excludes {
		return fmt.Errorf("%w: only the person says where a record does not apply", ErrTransition)
	}
	for _, l := range next.Links {
		if !row.links[l.Kind] {
			return fmt.Errorf("%w: a %s writer does not write a %s link", ErrTransition, next.Author.Class, l.Kind)
		}
	}
	return checkReceipt(next)
}

func (row rights) admits(prev *Revision, next Revision) error {
	if prev == nil {
		for _, cell := range row.creates {
			if cell.admits(next) {
				return nil
			}
		}
		return fmt.Errorf("%w: a %s writer does not create this %s %s", ErrTransition, next.Author.Class, next.State, next.Kind)
	}
	if next.Kind != prev.Kind {
		return invalid("a revision keeps the record's kind")
	}
	if !row.moves[move{prev.State, next.State}] {
		return fmt.Errorf("%w: a %s writer does not move a %s %s to %s", ErrTransition, next.Author.Class, prev.State, prev.Kind, next.State)
	}
	if row.ownOnly && prev.Author != next.Author {
		return fmt.Errorf("%w: a %s writer changes only its own %s record", ErrTransition, next.Author.Class, prev.State)
	}
	if !row.followsOthers && prev.Author.Class != next.Author.Class {
		return fmt.Errorf("%w: %s revision %d was written by %s; a %s writer does not write over it", ErrTransition,
			prev.ID, prev.Revision, prev.Author.Class, next.Author.Class)
	}
	return nil
}

// receiptRequired is the one rule for where authority is recorded: on every
// revision entering accepted, rejected or withdrawn, and on a superseded
// revision a person made. Nowhere else.
func receiptRequired(state State, author AuthorClass) bool {
	switch state {
	case Accepted, Rejected, Withdrawn:
		return true
	case Superseded:
		return author == AuthorPerson
	}
	return false
}

// checkReceipt holds a revision's receipt to its state and its writer. A
// PERSON RECEIPT IS STORED ONLY ON A REVISION THE PERSON WROTE, and the person
// writes only through a PersonReceipt; an import carries the old store's
// evidence under a legacy actor, never the person.
func checkReceipt(r Revision) error {
	has := r.Receipt != (Receipt{})
	if need := receiptRequired(r.State, r.Author.Class); need != has {
		if need {
			return fmt.Errorf("%w: a %s revision records who gave it authority", ErrNoReceipt, r.State)
		}
		return invalid("a %s revision carries no receipt", r.State)
	}
	if !has {
		return nil
	}
	if r.Receipt.At.IsZero() || !workspace.ValidLine(r.Receipt.Ref, maxRef) {
		return invalid("a receipt names its act and its time")
	}
	switch r.Author.Class {
	case AuthorPerson:
		if r.Receipt.Actor != ActorPerson {
			return invalid("a person's revision carries a person receipt")
		}
		switch r.Receipt.Door {
		case DoorCard, DoorTerminal, DoorPage, DoorStatement:
			return nil
		}
		return invalid("unknown receipt door %q", r.Receipt.Door)
	case AuthorMigration:
		if !r.Receipt.Actor.legacy() {
			return invalid("an import copies the old store's evidence as a legacy actor, never %q", r.Receipt.Actor)
		}
		switch r.Receipt.Door {
		case DoorCard, DoorTerminal, DoorPage, DoorMigration:
			return nil
		}
		return invalid("an imported receipt keeps its own door or says migration, not %q", r.Receipt.Door)
	}
	return fmt.Errorf("%w: only a person or an import records authority", ErrTransition)
}

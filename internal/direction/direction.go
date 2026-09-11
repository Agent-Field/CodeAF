// Package direction keeps the one record for direction: a rule, a decision or
// a finding, revisioned and sourced, with the places it reaches and the person
// receipt that gave it authority (design-t03b). It lives in the collections
// database beside the placements it is resolved against.
//
// A RECORD GOVERNS IF AND ONLY IF IT IS A RULE OR A DECISION IN THE ACCEPTED
// STATE, and it reaches accepted only through a PersonReceipt the runtime
// constructs. Everything else a model, an extractor, an unattended run, a
// voice or a delegated principal writes is a proposal or a finding.
//
// This package holds execution state for nothing: no wake condition, no
// grant, no attention policy, no payload. Those have their own owners.
package direction

import (
	"errors"
	"fmt"
)

// Kind is what a record is. Rule and decision differ in display and staleness,
// never in authority; a finding is sourced information and never governs.
type Kind string

const (
	Rule     Kind = "rule"
	Decision Kind = "decision"
	Finding  Kind = "finding"
)

func (k Kind) valid() bool { return k == Rule || k == Decision || k == Finding }

// directive reports whether a kind can ever govern.
func (k Kind) directive() bool { return k == Rule || k == Decision }

// State is where a revision stands. Every transition writes a new revision, so
// the history of a record is also its audit log.
type State string

const (
	Proposed      State = "proposed"
	Accepted      State = "accepted"
	Rejected      State = "rejected"
	Withdrawn     State = "withdrawn"
	Superseded    State = "superseded"
	Informational State = "informational"
)

// fits reports whether a state is legal for a kind: a finding is informational
// until it is withdrawn or superseded; a rule or a decision is never
// informational.
func (s State) fits(k Kind) bool {
	switch s {
	case Proposed, Accepted, Rejected:
		return k.directive()
	case Informational:
		return k == Finding
	case Withdrawn, Superseded:
		return k.valid()
	}
	return false
}

// Lane is which part of a resolve a current revision is delivered in.
type Lane string

const (
	LaneGoverning     Lane = "governing"
	LanePending       Lane = "pending"
	LaneInformational Lane = "informational"
	laneNone          Lane = ""
)

// lane IS THE GOVERNING PREDICATE, AND THE ONLY PLACE IT IS COMPUTED. The write
// path stores its answer in direction_live; the resolver reads that answer and
// never re-derives it; Rebuild calls this same function.
func lane(k Kind, s State) Lane {
	switch {
	case k.directive() && s == Accepted:
		return LaneGoverning
	case k.directive() && s == Proposed:
		return LanePending
	case k == Finding && s == Informational:
		return LaneInformational
	}
	return laneNone
}

// SourceClass is where a revision's words came from.
type SourceClass string

const (
	SourceConversation        SourceClass = "conversation"
	SourceTask                SourceClass = "task"
	SourceArtifact            SourceClass = "artifact"
	SourceExchange            SourceClass = "exchange"
	SourceTerminal            SourceClass = "terminal"
	SourceConversationOutcome SourceClass = "conversation_outcome"
	SourceOccurrence          SourceClass = "occurrence"
	SourceLegacyUnknown       SourceClass = "legacy_unknown"
)

func (c SourceClass) valid() bool {
	switch c {
	case SourceConversation, SourceTask, SourceArtifact, SourceExchange, SourceTerminal,
		SourceConversationOutcome, SourceOccurrence, SourceLegacyUnknown:
		return true
	}
	return false
}

// QuoteOrigin says honestly whose words the quote is: a card's wording is
// adopted by the person, not said by them.
type QuoteOrigin string

const (
	PersonSaid     QuoteOrigin = "person_said"
	AdoptedWording QuoteOrigin = "adopted_wording"
	ModelExtracted QuoteOrigin = "model_extracted"
	PeerQuote      QuoteOrigin = "peer"
	ArtifactQuote  QuoteOrigin = "artifact"
	UnknownQuote   QuoteOrigin = "unknown"
)

func (o QuoteOrigin) valid() bool {
	switch o {
	case PersonSaid, AdoptedWording, ModelExtracted, PeerQuote, ArtifactQuote, UnknownQuote:
		return true
	}
	return false
}

// AuthorClass is who wrote a revision. It is not authority: authority is the
// receipt.
type AuthorClass string

const (
	AuthorPerson    AuthorClass = "person"
	AuthorModel     AuthorClass = "model"
	AuthorExtractor AuthorClass = "extractor"
	AuthorRun       AuthorClass = "run"
	AuthorVoice     AuthorClass = "voice"
	AuthorSteward   AuthorClass = "steward"
	AuthorMigration AuthorClass = "migration"
)

// writer reports the classes that write through As: every class except the
// person, who writes only with a receipt, and migration, which writes only
// through Import.
func (c AuthorClass) writer() bool {
	switch c {
	case AuthorModel, AuthorExtractor, AuthorRun, AuthorVoice, AuthorSteward:
		return true
	}
	return false
}

// ReceiptActor is who gave a revision its authority. Only a person receipt is
// minted now, and only from a PersonReceipt; the legacy actors exist only on
// imported revisions, so an old rule keeps governing and stays labelled
// rather than being dressed up (design §3.2, F9).
type ReceiptActor string

const (
	ActorPerson ReceiptActor = "person"
	// ActorLegacyPerson is an old store's record that a person adopted it
	// through that store's own door. It is evidence copied, not an act this
	// runtime saw, so it is never the person.
	ActorLegacyPerson    ReceiptActor = "legacy_person"
	ActorLegacyDelegated ReceiptActor = "legacy_delegated"
	ActorLegacyUnknown   ReceiptActor = "legacy_unknown"
)

// legacy reports the actors an import may copy.
func (a ReceiptActor) legacy() bool {
	return a == ActorLegacyPerson || a == ActorLegacyDelegated || a == ActorLegacyUnknown
}

// Door is how a receipt reached the runtime.
type Door string

const (
	DoorCard      Door = "card"
	DoorTerminal  Door = "terminal"
	DoorPage      Door = "page"
	DoorStatement Door = "statement"
	DoorMigration Door = "migration"
)

// TargetKind is what a target or an exclusion names.
type TargetKind string

const (
	TargetCollection      TargetKind = "collection"
	TargetConversation    TargetKind = "conversation"
	TargetTask            TargetKind = "task"
	TargetStanding        TargetKind = "standing"
	TargetArtifact        TargetKind = "artifact"
	TargetEverywhere      TargetKind = "everywhere"
	TargetLegacyWorkspace TargetKind = "legacy_workspace"
)

// Everywhere is the one ref an everywhere target carries.
const Everywhere = "*"

// Reach says whether a folder's direction reaches its descendants. It is
// direct unless the person chose otherwise (C23).
type Reach string

const (
	Direct  Reach = "direct"
	Subtree Reach = "subtree"
)

// reaches is every reach, for a read that takes each place's rows one reach at
// a time.
var reaches = []Reach{Direct, Subtree}

// LinkKind is a typed relation from one revision to another record.
type LinkKind string

const (
	Supersedes    LinkKind = "supersedes"
	Overrides     LinkKind = "overrides"
	ConflictsWith LinkKind = "conflicts_with"
	DerivedFrom   LinkKind = "derived_from"
)

func (k LinkKind) valid() bool {
	return k == Supersedes || k == Overrides || k == ConflictsWith || k == DerivedFrom
}

// resolved reports the link kinds the resolver reads. has_links in the live
// index counts only these, so an imported record's provenance link does not
// send the resolver to read links it will ignore.
func (k LinkKind) resolved() bool { return k == Overrides || k == ConflictsWith }

// THE BOUNDS ARE HERE AND NOWHERE ELSE. The title and text bounds are the ones
// shared context already keeps, so every finding imported from it fits.
const (
	MaxQuote      = 2048
	MaxTargets    = 64
	MaxExclusions = 64
	MaxLinks      = 64
	maxReason     = 256
	maxRef        = 4096
)

var (
	// ErrInvalid is a malformed request, refused whole before anything is written.
	ErrInvalid = errors.New("invalid direction record")
	// ErrNotFound is an identity this store does not hold.
	ErrNotFound = errors.New("direction record not found")
	// ErrConflict is a fence that failed: the record moved under a writer
	// holding an older revision. Nothing is merged or overwritten.
	ErrConflict = errors.New("direction record was revised by someone else")
	// ErrTransition is a state change the record's state or the writer does not allow.
	ErrTransition = errors.New("direction record cannot make that change")
	// ErrNoReceipt is a person-only change asked for without a person receipt.
	ErrNoReceipt = errors.New("that change needs a person receipt")
	// ErrRejectedText refuses re-proposing wording the person rejected (C16).
	ErrRejectedText = errors.New("the person rejected this wording before")
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalid}, args...)...)
}

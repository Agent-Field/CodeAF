package workspace

import "fmt"

// Provenance is who filed a membership and why. It is stored with the event,
// never inferred later from retrieved prose.
type Provenance struct {
	Origin         string
	Reason         string
	Actor          string
	Evidence       string
	IdempotencyKey string
	// ExpectedRevision is optional. Zero means no precondition (old CLI).
	// Non-zero must match the collection's revision inside the write txn.
	ExpectedRevision int
	// ExpectedFrom / ExpectedTo apply to Move only; zero means no check.
	ExpectedFrom, ExpectedTo int
}

const (
	OriginPerson         = "person"
	OriginSystemFallback = "system_fallback"
	OriginOrganizer      = "organizer"
	// OriginAgent is a representative contribution (Wave 3). Membership events
	// still refuse it; participants and deliveries accept it so a model cannot
	// stamp from_person by writing the body.
	OriginAgent     = "agent"
	ActionAdd       = "add"
	ActionRemove    = "remove"
	LifecycleActive = "active"
)

// MembershipEvent is one add or remove that actually happened. Active
// memberships stay unique; this table is the history that why-here reads.
type MembershipEvent struct {
	CollectionID   string
	Kind           Kind
	RefID          string
	SessionID      string
	Action         string
	Origin         string
	Reason         string
	Actor          string
	Evidence       string
	At             string
	IdempotencyKey string
}

func (p Provenance) normalized() Provenance {
	if p.Origin == "" {
		p.Origin = OriginPerson
	}
	return p
}

func validOrigin(origin string) bool {
	switch origin {
	case OriginPerson, OriginSystemFallback, OriginOrganizer:
		return true
	}
	return false
}

func validCollabOrigin(origin string) bool {
	return validOrigin(origin) || origin == OriginAgent
}

func prepareProvenance(p Provenance) (Provenance, error) {
	p = p.normalized()
	if !validOrigin(p.Origin) {
		return Provenance{}, fmt.Errorf("%w: unknown membership origin %q", ErrInvalid, p.Origin)
	}
	return p, nil
}

func prepareCollabProvenance(p Provenance) (Provenance, error) {
	p = p.normalized()
	if !validCollabOrigin(p.Origin) {
		return Provenance{}, fmt.Errorf("%w: unknown collaboration origin %q", ErrInvalid, p.Origin)
	}
	return p, nil
}

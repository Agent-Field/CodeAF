package direction

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// Source is where a revision's words came from. Its identity is SHA256, the
// hash of the anchored content; Hint is only a locator, because compaction
// re-journals windows and a position does not survive that.
type Source struct {
	Class   SourceClass `json:"class"`
	ID      string      `json:"id,omitempty"`
	Session string      `json:"session,omitempty"`
	Hint    string      `json:"hint,omitempty"`
	SHA256  string      `json:"sha256,omitempty"`
}

// Author is who wrote one revision. It is never authority.
type Author struct {
	Class AuthorClass `json:"class"`
	Ref   string      `json:"ref,omitempty"`
}

// Receipt is the authority a revision carries, as stored. It is present
// exactly on revisions entering accepted, rejected or withdrawn, and on a
// superseded revision a person made; it is the zero value everywhere else.
type Receipt struct {
	Actor ReceiptActor `json:"actor,omitempty"`
	Door  Door         `json:"door,omitempty"`
	Ref   string       `json:"ref,omitempty"`
	At    time.Time    `json:"at,omitzero"`
}

// Target is one place a revision applies. Reach is meaningful only for a
// collection; every other kind matches by equality.
type Target struct {
	Kind    TargetKind `json:"kind"`
	Ref     string     `json:"ref"`
	Session string     `json:"session,omitempty"`
	Reach   Reach      `json:"reach"`
}

// Exclusion is a place the person said the revision does not apply.
type Exclusion struct {
	Kind    TargetKind `json:"kind"`
	Ref     string     `json:"ref"`
	Session string     `json:"session,omitempty"`
	At      time.Time  `json:"at"`
}

// Link is a typed relation from a revision to another record. ToRevision is
// the revision the writer saw; for supersedes it is a fence.
type Link struct {
	Kind       LinkKind `json:"kind"`
	To         string   `json:"to"`
	ToRevision int      `json:"to_revision,omitempty"`
}

// Draft is the content of a revision a writer asks for: everything except its
// identity, state, author and receipt, which the store decides.
type Draft struct {
	Kind        Kind
	Title       string
	Text        string
	Quote       string
	QuoteOrigin QuoteOrigin
	Source      Source
	Targets     []Target
	Exclusions  []Exclusion
	Links       []Link
}

// Revision is one immutable revision as stored.
type Revision struct {
	ID          string      `json:"id"`
	Revision    int         `json:"revision"`
	Seq         int64       `json:"seq"`
	Kind        Kind        `json:"kind"`
	State       State       `json:"state"`
	StateReason string      `json:"state_reason,omitempty"`
	Title       string      `json:"title"`
	Text        string      `json:"text"`
	TextSHA256  string      `json:"text_sha256"`
	Quote       string      `json:"quote,omitempty"`
	QuoteOrigin QuoteOrigin `json:"quote_origin"`
	Source      Source      `json:"source"`
	Author      Author      `json:"author"`
	Receipt     Receipt     `json:"receipt,omitzero"`
	Targets     []Target    `json:"targets"`
	Exclusions  []Exclusion `json:"exclusions"`
	Links       []Link      `json:"links"`
	WrittenAt   time.Time   `json:"written_at"`
}

// Fence names the revision a writer read. A change is applied only while the
// record's pointer still equals it (L1).
type Fence struct {
	ID       string
	Revision int
}

// Fence returns the fence for this revision.
func (r Revision) Fence() Fence { return Fence{ID: r.ID, Revision: r.Revision} }

// Lane is where this revision is delivered while it is current.
func (r Revision) Lane() Lane { return lane(r.Kind, r.State) }

func (r Revision) draft() Draft {
	return Draft{Kind: r.Kind, Title: r.Title, Text: r.Text, Quote: r.Quote, QuoteOrigin: r.QuoteOrigin,
		Source: r.Source, Targets: r.Targets, Exclusions: r.Exclusions, Links: r.Links}
}

type placeKey struct {
	kind         TargetKind
	ref, session string
}

type linkKey struct {
	kind LinkKind
	to   string
}

// inherited is what a new revision may keep from the one before it without
// being allowed to create it: a legacy workspace target or exclusion.
type inherited map[placeKey]bool

func inheritedFrom(prev *Revision) inherited {
	kept := inherited{}
	if prev == nil {
		return kept
	}
	for _, t := range prev.Targets {
		if t.Kind == TargetLegacyWorkspace {
			kept[placeKey{t.Kind, t.Ref, t.Session}] = true
		}
	}
	for _, e := range prev.Exclusions {
		if e.Kind == TargetLegacyWorkspace {
			kept[placeKey{e.Kind, e.Ref, e.Session}] = true
		}
	}
	return kept
}

// normalize validates a draft and returns it with defaults filled and repeats
// collapsed. LEGACY WORKSPACE PLACES ARE CREATED ONLY BY AN IMPORT (R11): any
// other writer may keep one a previous revision carried, never add one.
func (d Draft) normalize(migration bool, kept inherited, at time.Time) (Draft, error) {
	if !d.Kind.valid() {
		return Draft{}, invalid("unknown kind %q", d.Kind)
	}
	if !workspace.ValidLine(d.Title, workspace.MaxContextTitle) {
		return Draft{}, invalid("a title is 1–%d bytes without surrounding whitespace or control characters", workspace.MaxContextTitle)
	}
	if !workspace.ValidProse(d.Text, workspace.MaxContextText) {
		return Draft{}, invalid("text is 1–%d bytes of prose", workspace.MaxContextText)
	}
	if d.Quote != "" && !workspace.ValidProse(d.Quote, MaxQuote) {
		return Draft{}, invalid("a quote is at most %d bytes of prose", MaxQuote)
	}
	if !d.QuoteOrigin.valid() {
		return Draft{}, invalid("unknown quote origin %q", d.QuoteOrigin)
	}
	if err := d.Source.validate(); err != nil {
		return Draft{}, err
	}
	legacyOK := func(k placeKey) bool { return migration || kept[k] }
	targets, err := normalizeTargets(d.Targets, legacyOK)
	if err != nil {
		return Draft{}, err
	}
	exclusions, err := normalizeExclusions(d.Exclusions, legacyOK, at)
	if err != nil {
		return Draft{}, err
	}
	links, err := normalizeLinks(d.Links, d.Kind)
	if err != nil {
		return Draft{}, err
	}
	d.Targets, d.Exclusions, d.Links = targets, exclusions, links
	return d, nil
}

func (s Source) validate() error {
	if !s.Class.valid() {
		return invalid("unknown source class %q", s.Class)
	}
	for _, field := range []string{s.ID, s.Session, s.Hint} {
		if field != "" && !workspace.ValidProse(field, maxRef) {
			return invalid("a source field is at most %d bytes without control characters", maxRef)
		}
	}
	if s.SHA256 != "" && !isHex(s.SHA256, 64) {
		return invalid("a source hash is 64 lowercase hex characters")
	}
	// An artifact changes under its path, so a citation of one names the
	// version it read.
	if s.Class == SourceArtifact && s.SHA256 == "" {
		return invalid("an artifact source names the version it read")
	}
	return nil
}

func (k placeKey) validate(legacyOK func(placeKey) bool) error {
	switch k.kind {
	case TargetCollection, TargetConversation, TargetTask, TargetStanding, TargetArtifact:
		ref := workspace.Ref{Kind: workspace.Kind(k.kind), ID: k.ref, SessionID: k.session}
		if err := ref.Validate(); err != nil {
			return invalid("%v", err)
		}
	case TargetEverywhere:
		if k.ref != Everywhere || k.session != "" {
			return invalid("an everywhere place is spelled %q", Everywhere)
		}
	case TargetLegacyWorkspace:
		if !legacyOK(k) {
			return invalid("only an import may name a legacy workspace")
		}
		if !filepath.IsAbs(k.ref) || filepath.Clean(k.ref) != k.ref || k.session != "" {
			return invalid("a legacy workspace is a clean absolute path")
		}
	default:
		return invalid("unknown place kind %q", k.kind)
	}
	return nil
}

func normalizeTargets(targets []Target, legacyOK func(placeKey) bool) ([]Target, error) {
	kept := make([]Target, 0, len(targets))
	seen := make(map[placeKey]Reach, len(targets))
	for _, t := range targets {
		key := placeKey{t.Kind, t.Ref, t.Session}
		if err := key.validate(legacyOK); err != nil {
			return nil, err
		}
		if t.Reach == "" {
			t.Reach = Direct
		}
		if t.Reach != Direct && t.Reach != Subtree {
			return nil, invalid("unknown reach %q", t.Reach)
		}
		if t.Reach == Subtree && t.Kind != TargetCollection {
			return nil, invalid("only a folder reaches a subtree")
		}
		if reach, repeated := seen[key]; repeated {
			if reach != t.Reach {
				return nil, invalid("one place is named with two reaches")
			}
			continue
		}
		seen[key] = t.Reach
		kept = append(kept, t)
	}
	if len(kept) > MaxTargets {
		return nil, invalid("a record reaches at most %d places", MaxTargets)
	}
	return kept, nil
}

func normalizeExclusions(exclusions []Exclusion, legacyOK func(placeKey) bool, at time.Time) ([]Exclusion, error) {
	kept := make([]Exclusion, 0, len(exclusions))
	seen := make(map[placeKey]bool, len(exclusions))
	for _, e := range exclusions {
		key := placeKey{e.Kind, e.Ref, e.Session}
		if e.Kind == TargetEverywhere {
			return nil, invalid("everywhere cannot be excluded; withdraw the record instead")
		}
		if err := key.validate(legacyOK); err != nil {
			return nil, err
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		if e.At.IsZero() {
			e.At = at
		}
		kept = append(kept, e)
	}
	if len(kept) > MaxExclusions {
		return nil, invalid("a record excludes at most %d places", MaxExclusions)
	}
	return kept, nil
}

// legacyLinkPrefixes are the stores a derived_from link may cite by their own
// identity, so an imported record keeps the id its old receipts name.
var legacyLinkPrefixes = []string{"memory:", "standing:", "context:"}

func normalizeLinks(links []Link, kind Kind) ([]Link, error) {
	kept := make([]Link, 0, len(links))
	seen := make(map[linkKey]bool, len(links))
	for _, l := range links {
		if !l.Kind.valid() {
			return nil, invalid("unknown link kind %q", l.Kind)
		}
		if l.ToRevision < 0 {
			return nil, invalid("a linked revision is positive, or 0 for none named")
		}
		switch {
		case isRecordID(l.To):
		case l.Kind == DerivedFrom && legacyLink(l.To):
		default:
			return nil, invalid("a %s link names a record id", l.Kind)
		}
		if l.Kind.resolved() && !kind.directive() {
			return nil, invalid("only a rule or a decision overrides or conflicts")
		}
		if l.Kind == Supersedes && l.ToRevision == 0 {
			return nil, invalid("a supersedes link names the revision it replaces")
		}
		key := linkKey{l.Kind, l.To}
		if seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, l)
	}
	if len(kept) > MaxLinks {
		return nil, invalid("a record carries at most %d links", MaxLinks)
	}
	return kept, nil
}

func legacyLink(to string) bool {
	for _, prefix := range legacyLinkPrefixes {
		if rest, ok := strings.CutPrefix(to, prefix); ok && workspace.ValidLine(rest, maxRef) {
			return true
		}
	}
	return false
}

func isRecordID(s string) bool { return isHex(s, 32) }

func isHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

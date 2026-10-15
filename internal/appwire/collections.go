package appwire

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// WAVE 9: COLLECTIONS (PROTOCOL.md, V75). A collection is a hand-picked list
// of references: conversations, tasks, files, crews, processes and other
// collections. It is a list, not a saved query and not memory: nothing is
// copied, nothing is learned, and removing a reference never touches the
// thing. What a member is NOW (there, gone, moved) is found when it is
// opened and never stored.

// EventCollectionChanged says a collection changed through this door.
const EventCollectionChanged = "collection.changed"

// FeatureCollections is the hello feature an engine serving collections
// carries.
const FeatureCollections = "collections"

// The kinds of a reference. RefStanding is legacy: an older build's standing
// item, found and removed, never added.
const (
	RefCollection   = "collection"
	RefConversation = "conversation"
	RefTask         = "task"
	RefArtifact     = "artifact"
	RefCrew         = "crew"
	RefProcess      = "process"
	RefStanding     = "standing"
)

// The states of a target.
const (
	TargetLive    = "live"
	TargetDeleted = "deleted"
	TargetMissing = "missing"
	TargetUnknown = "unknown"
)

// The bounds of this wave's methods.
const (
	CollectionRefsMax       = 200  // refs in one add or remove
	CollectionsListMax      = 1000 // collections one listing answers
	CollectionOpenDepth     = 2    // levels below the top open opens
	CollectionOpenLimit     = 500  // members per level, and expand's most refs
	CollectionExpandMax     = 3    // levels expand reads
	CollectionExpandDefault = 200  // refs expand answers when not told
	CollectionNodesMax      = 1500 // nodes one open answers in all
	collectionNameMax       = 256
	collectionIDMax         = 4096
)

// Ref is one reference: a task and a process carry their agent as SessionID,
// and an artifact is a clean absolute path.
type Ref struct {
	Kind      string `json:"kind" enum:"collection,conversation,task,artifact,crew,process,standing"`
	ID        string `json:"id"`
	SessionID string `json:"session_id,omitempty"`
}

// Member is a reference as a collection holds it, with when it was added;
// AddedAt is absent for a row written by a store older than schema 2. In
// collections.add, an AddedAt puts a removed reference back in place.
type Member struct {
	Ref
	AddedAt *time.Time `json:"added_at,omitempty"`
}

// Collection is one collection: its direct member count, when it was made and
// when its newest member was added, and, when collections.list was asked
// about a reference, whether it directly holds it.
type Collection struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
	Count       int        `json:"count"`
	LastAddedAt *time.Time `json:"last_added_at,omitempty"`
	Holds       *bool      `json:"holds,omitempty"`
}

// Target is what a member was found to be now, never stored. In is where it
// lives, outer first; Count is a crew's members or a collection's direct
// members; Repo, Branch and Path place a file inside a git work tree.
type Target struct {
	State  string   `json:"state" enum:"live,deleted,missing,unknown"`
	Title  string   `json:"title,omitempty"`
	In     []string `json:"in,omitempty"`
	Status string   `json:"status,omitempty"`
	Repo   string   `json:"repo,omitempty"`
	Branch string   `json:"branch,omitempty"`
	Path   string   `json:"path,omitempty"`
	Count  *int     `json:"count,omitempty"`
}

// Node is a member of an opened collection with its target, and for a
// collection opened within the depth asked, its own members.
type Node struct {
	Member
	Target   Target    `json:"target"`
	Children *NodeList `json:"children,omitempty"`
}

// NodeList is one collection's members as collections.open answers them,
// newest added first, and whether there were more.
type NodeList struct {
	Members []Node `json:"members"`
	More    bool   `json:"more"`
}

// ExpandedRef is one reference collections.expand answers, with its target.
type ExpandedRef struct {
	Member
	Target Target `json:"target"`
}

// The answers of this wave's methods.
type (
	CollectionsResult struct {
		Collections []Collection `json:"collections"`
	}
	CollectionResult struct {
		Collection Collection `json:"collection"`
	}
	CollectionAddResult struct {
		Added   []Member `json:"added"`
		Already []Ref    `json:"already"`
	}
	CollectionRemoveResult struct {
		Removed []Member `json:"removed"`
	}
	CollectionOpenResult struct {
		Collection Collection `json:"collection"`
		Members    []Node     `json:"members"`
		More       bool       `json:"more"`
	}
	CollectionExpandResult struct {
		Collection  Collection    `json:"collection"`
		Refs        []ExpandedRef `json:"refs"`
		Missing     int           `json:"missing"`
		Collections int           `json:"collections"`
		More        bool          `json:"more"`
	}
	// CollectionChanged is said after any change made through this door, once
	// per collection changed; Deleted says it is gone.
	CollectionChanged struct {
		ID      string `json:"id"`
		Deleted bool   `json:"deleted,omitempty"`
	}
)

// Collector is the optional engine half of collections. One that is not a
// Collector answers every collections method unknown_method and carries no
// "collections" feature. Bounds and shapes are checked here before it is
// asked; depth and limit arrive already defaulted.
type Collector interface {
	ListCollections(holds *Ref) ([]Collection, error)
	CreateCollection(name string) (Collection, error)
	RenameCollection(id, name string) (Collection, error)
	DuplicateCollection(id, name string) (Collection, error)
	DeleteCollection(id string) error
	AddToCollection(id string, refs []Member) (CollectionAddResult, error)
	RemoveFromCollection(id string, refs []Ref) (CollectionRemoveResult, error)
	OpenCollection(id string, depth, limit int) (CollectionOpenResult, error)
	ExpandCollection(id string, depth, limit int) (CollectionExpandResult, error)
}

func collector(e Engine, name string) (Collector, error) {
	c, ok := e.(Collector)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return c, nil
}

// collectCall decodes a method's params into P and answers what call answers.
func collectCall[P, R any](name string, call func(Collector, P) (*R, error)) method {
	return typed(func(e Engine, p P) (*R, error) {
		c, err := collector(e, name)
		if err != nil {
			return nil, err
		}
		return call(c, p)
	})
}

// withCollectionsFeature is hello carrying "collections" when the engine
// serves it.
func withCollectionsFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Collector); !ok {
		return hello
	}
	for _, f := range hello.Features {
		if f == FeatureCollections {
			return hello
		}
	}
	hello.Features = append(append([]string(nil), hello.Features...), FeatureCollections)
	return hello
}

// plainText is non-empty text of at most limit bytes with no control
// characters and no surrounding space.
func plainText(s string, limit int) bool {
	return s != "" && len(s) <= limit && strings.TrimSpace(s) == s && strings.IndexFunc(s, unicode.IsControl) < 0
}

// CheckRef refuses a malformed reference as bad_params. adding refuses the
// legacy standing kind, which can be found and removed and never added.
func CheckRef(r Ref, adding bool) error {
	if !plainText(r.ID, collectionIDMax) {
		return Errorf(CodeBadParams, "a reference needs an id without surrounding space or control characters")
	}
	needsSession := r.Kind == RefTask || r.Kind == RefProcess
	if needsSession && !plainText(r.SessionID, 1024) {
		return Errorf(CodeBadParams, "a %s reference needs the session_id of its agent", r.Kind)
	}
	if !needsSession && r.SessionID != "" {
		return Errorf(CodeBadParams, "only a task or a process reference has a session_id")
	}
	switch r.Kind {
	case RefCollection, RefConversation, RefCrew, RefProcess:
	case RefTask:
		n, err := strconv.ParseUint(r.ID, 10, 64)
		if err != nil || n == 0 || strconv.FormatUint(n, 10) != r.ID {
			return Errorf(CodeBadParams, "a task reference's id is its positive number")
		}
	case RefArtifact:
		if !filepath.IsAbs(r.ID) || filepath.Clean(r.ID) != r.ID {
			return Errorf(CodeBadParams, "an artifact reference needs a clean absolute path")
		}
	case RefStanding:
		if adding {
			return Errorf(CodeBadParams, "standing items are gone, so a collection cannot gain one")
		}
	default:
		return Errorf(CodeBadParams, "unknown reference kind %q", r.Kind)
	}
	return nil
}

// collectionName is a name trimmed, or bad_params.
func collectionName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !plainText(name, collectionNameMax) {
		return "", Errorf(CodeBadParams, "a collection needs a name of 1 to %d bytes without control characters", collectionNameMax)
	}
	return name, nil
}

// collectionRefs checks a list of references against its bounds.
func collectionRefs(name string, n int, check func(int) error) error {
	if n < 1 || n > CollectionRefsMax {
		return Errorf(CodeBadParams, "%s takes 1 to %d refs", name, CollectionRefsMax)
	}
	for i := 0; i < n; i++ {
		if err := check(i); err != nil {
			return err
		}
	}
	return nil
}

// bounded is an optional bound: def when absent, bad_params outside lo–hi.
func bounded(field string, v *int, lo, hi, def int) (int, error) {
	if v == nil {
		return def, nil
	}
	if *v < lo || *v > hi {
		return 0, Errorf(CodeBadParams, "%s is %d to %d", field, lo, hi)
	}
	return *v, nil
}

// collectionOf answers one collection or the error.
func collectionOf(c Collection, err error) (*CollectionResult, error) {
	if err != nil {
		return nil, err
	}
	return &CollectionResult{c}, nil
}

type (
	collectionNameParams struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	collectionBoundParams struct {
		ID    string `json:"id"`
		Depth *int   `json:"depth"`
		Limit *int   `json:"limit"`
	}
)

func init() {
	for name, m := range collectionMethods {
		methods[name] = m
	}
}

var collectionMethods = map[string]method{
	"collections.list": collectCall("collections.list", func(c Collector, p struct {
		Holds *Ref `json:"holds"`
	}) (*CollectionsResult, error) {
		if p.Holds != nil {
			if err := CheckRef(*p.Holds, false); err != nil {
				return nil, err
			}
		}
		list, err := c.ListCollections(p.Holds)
		if err != nil {
			return nil, err
		}
		if len(list) > CollectionsListMax {
			list = list[:CollectionsListMax]
		}
		return &CollectionsResult{nonNil(list)}, nil
	}),
	"collections.create": collectCall("collections.create", func(c Collector, p struct {
		Name string `json:"name"`
	}) (*CollectionResult, error) {
		name, err := collectionName(p.Name)
		if err != nil {
			return nil, err
		}
		return collectionOf(c.CreateCollection(name))
	}),
	"collections.rename": collectCall("collections.rename", func(c Collector, p collectionNameParams) (*CollectionResult, error) {
		if err := needID("collections.rename", p.ID); err != nil {
			return nil, err
		}
		name, err := collectionName(p.Name)
		if err != nil {
			return nil, err
		}
		return collectionOf(c.RenameCollection(p.ID, name))
	}),
	"collections.duplicate": collectCall("collections.duplicate", func(c Collector, p collectionNameParams) (*CollectionResult, error) {
		if err := needID("collections.duplicate", p.ID); err != nil {
			return nil, err
		}
		name, err := collectionName(p.Name)
		if err != nil {
			return nil, err
		}
		return collectionOf(c.DuplicateCollection(p.ID, name))
	}),
	"collections.delete": collectCall("collections.delete", func(c Collector, p IDParams) (*Empty, error) {
		if err := needID("collections.delete", p.ID); err != nil {
			return nil, err
		}
		return done(c.DeleteCollection(p.ID))
	}),
	"collections.add": collectCall("collections.add", func(c Collector, p struct {
		ID   string   `json:"id"`
		Refs []Member `json:"refs"`
	}) (*CollectionAddResult, error) {
		if err := needID("collections.add", p.ID); err != nil {
			return nil, err
		}
		err := collectionRefs("collections.add", len(p.Refs), func(i int) error {
			if at := p.Refs[i].AddedAt; at != nil && at.UnixMilli() <= 0 {
				return Errorf(CodeBadParams, "added_at is a moment after 1970")
			}
			return CheckRef(p.Refs[i].Ref, true)
		})
		if err != nil {
			return nil, err
		}
		r, err := c.AddToCollection(p.ID, p.Refs)
		if err != nil {
			return nil, err
		}
		r.Added, r.Already = nonNil(r.Added), nonNil(r.Already)
		return &r, nil
	}),
	"collections.remove": collectCall("collections.remove", func(c Collector, p struct {
		ID   string `json:"id"`
		Refs []Ref  `json:"refs"`
	}) (*CollectionRemoveResult, error) {
		if err := needID("collections.remove", p.ID); err != nil {
			return nil, err
		}
		if err := collectionRefs("collections.remove", len(p.Refs), func(i int) error { return CheckRef(p.Refs[i], false) }); err != nil {
			return nil, err
		}
		r, err := c.RemoveFromCollection(p.ID, p.Refs)
		if err != nil {
			return nil, err
		}
		r.Removed = nonNil(r.Removed)
		return &r, nil
	}),
	"collections.open": collectCall("collections.open", func(c Collector, p collectionBoundParams) (*CollectionOpenResult, error) {
		if err := needID("collections.open", p.ID); err != nil {
			return nil, err
		}
		depth, err := bounded("depth", p.Depth, 0, CollectionOpenDepth, 1)
		if err != nil {
			return nil, err
		}
		limit, err := bounded("limit", p.Limit, 1, CollectionOpenLimit, CollectionOpenLimit)
		if err != nil {
			return nil, err
		}
		r, err := c.OpenCollection(p.ID, depth, limit)
		if err != nil {
			return nil, err
		}
		r.Members = nonNil(r.Members)
		return &r, nil
	}),
	"collections.expand": collectCall("collections.expand", func(c Collector, p collectionBoundParams) (*CollectionExpandResult, error) {
		if err := needID("collections.expand", p.ID); err != nil {
			return nil, err
		}
		depth, err := bounded("depth", p.Depth, 1, CollectionExpandMax, CollectionExpandMax)
		if err != nil {
			return nil, err
		}
		limit, err := bounded("limit", p.Limit, 1, CollectionOpenLimit, CollectionExpandDefault)
		if err != nil {
			return nil, err
		}
		r, err := c.ExpandCollection(p.ID, depth, limit)
		if err != nil {
			return nil, err
		}
		r.Refs = nonNil(r.Refs)
		return &r, nil
	}),
}

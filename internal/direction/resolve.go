package direction

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// THE RESOLVER'S BOUNDS. Governing input is admitted whole or not at all: more
// than MaxGoverning records, or more than MaxGoverningBytes of their wording,
// stops the work with the target that contributes most named, the same law the
// standing governing block keeps. The closure bound is the governing ancestry a
// subject may have before it is told to narrow its scope.
const (
	MaxGoverning      = 64
	MaxGoverningBytes = 64 * 1024
	MaxClosureEdges   = 256
	MaxPending        = 20
	MaxInformational  = 50
	MaxSubjectRefs    = workspace.MaxContextTargets
	// PendingWindow is how long a proposal is shown to runs as stance. An older
	// one stays proposed and in the review list; it just stops riding along.
	PendingWindow = 30 * 24 * time.Hour
)

var (
	// ErrGoverningTooLarge stops work whose complete governing input does not fit.
	ErrGoverningTooLarge = errors.New("governing direction exceeds its bound; narrow the governing scope before continuing")
	// ErrClosureTooLarge stops work whose governing ancestry is too deep or wide.
	ErrClosureTooLarge = errors.New("governing ancestry exceeds its bound; narrow the governing scope before continuing")
)

// GoverningTooLargeError names the bound that was crossed and the target
// that contributes most, so the person knows where to narrow.
//
// "Contributes" counts each record once per target that reaches the subject,
// however many of the subject's refs it reaches through. Over the record bound
// the heaviest target is the one with the most records, and Bytes and
// HeaviestBytes are zero: the resolve stops before reading more than the bound
// of bodies. Over the byte bound the heaviest target is the one whose records'
// title and text are largest, and both counts are given.
type GoverningTooLargeError struct {
	Records, Bytes  int
	Heaviest        Target
	HeaviestRecords int
	HeaviestBytes   int
}

func (e *GoverningTooLargeError) Error() string {
	if e.Bytes == 0 {
		return fmt.Sprintf("%v: %d records; %s %s contributes %d of them", ErrGoverningTooLarge, e.Records,
			e.Heaviest.Kind, e.Heaviest.Ref, e.HeaviestRecords)
	}
	return fmt.Sprintf("%v: %d records, %d bytes; %s %s contributes %d records, %d bytes", ErrGoverningTooLarge,
		e.Records, e.Bytes, e.Heaviest.Kind, e.Heaviest.Ref, e.HeaviestRecords, e.HeaviestBytes)
}

func (e *GoverningTooLargeError) Unwrap() error { return ErrGoverningTooLarge }

// Phase is where a resolve is asked from. It is recorded, never a filter.
type Phase string

const (
	PhaseChat       Phase = "chat"
	PhaseTask       Phase = "task"
	PhaseUnattended Phase = "unattended"
	PhaseEgress     Phase = "egress"
)

// Subject is the work direction is resolved for.
type Subject struct {
	// Refs is the work itself: its conversation, task, standing item or
	// artifact, and the owners the governing configuration names.
	Refs []workspace.Ref
	// Workspace is the physical root, used ONLY to match legacy workspace
	// targets imported from project-altitude holds.
	Workspace string
	Phase     Phase
}

// Path is one way a record reaches the subject: the target that matched, the
// subject ref the walk started from, and the folders from the subject's own
// placement up to the target folder. A target naming the subject itself, a
// legacy workspace or everywhere has no chain.
type Path struct {
	Target Target        `json:"target"`
	From   workspace.Ref `json:"from"`
	Chain  []string      `json:"chain,omitempty"`
}

// LegacyRef names the old record an imported one came from, so an old receipt
// that cites it still resolves.
type LegacyRef struct {
	Store   LegacyStore `json:"store"`
	ID      string      `json:"id"`
	Version string      `json:"version"`
}

// Applied is one record delivered to the subject, with its provenance. Rev is
// the revision's body. Via holds REPRESENTATIVE paths, not every path: for a
// folder target, one shortest surviving chain per subject ref, and for any
// other target the target itself. Blocked holds one chain per excluded folder
// that lies on a way to the target — shown, never silently dropped. Whether a
// record applies is decided exactly; only the listing is bounded, because the
// number of paths in a multi-parent graph grows exponentially.
type Applied struct {
	Rev          Revision   `json:"rev"`
	Via          []Path     `json:"via"`
	Blocked      []Path     `json:"blocked,omitempty"`
	Overrides    []string   `json:"overrides,omitempty"`
	OverriddenBy []string   `json:"overridden_by,omitempty"`
	Legacy       *LegacyRef `json:"legacy,omitempty"`
}

// Conflict is an unresolved conflicts_with pair that both apply to the subject.
// The resolver never picks a winner; the work asks one question.
type Conflict struct {
	A, B string
}

// Page is one page of a lane and whether the store had more.
type Page struct {
	Items []Applied `json:"items"`
	More  bool      `json:"more"`
}

// Effective is everything that reaches one subject, read in one snapshot.
type Effective struct {
	// Snapshot identifies the read: the governing ancestry and the newest
	// revision the store held.
	Snapshot      string
	Phase         Phase
	Governing     []Applied // complete, or ErrGoverningTooLarge; never truncated
	Pending       []Applied // labelled proposals, at most MaxPending
	PendingMore   bool      // more proposals reach here than the page carries
	Informational Page
	Conflicts     []Conflict
	// Placements is the governing ancestry: collection → nearest depth, for
	// the exposure receipt.
	Placements map[string]int
}

// Resolve returns the direction that reaches a subject. Every step reads one
// snapshot, so placements and direction are seen at the same instant. The
// cost does not depend on how many records the store has ever held: two index
// seeks per probed place, bounded work in Go, and bodies for what is returned.
func (s *Store) Resolve(ctx context.Context, subj Subject) (Effective, error) {
	subj, err := subj.normalize()
	if err != nil {
		return Effective{}, err
	}
	var eff Effective
	err = s.ws.ReadSnapshot(ctx, func(tx *sql.Tx) error {
		var err error
		eff, err = resolveIn(ctx, tx, subj, s.now().UTC())
		return err
	})
	return eff, err
}

// Informational returns one page of the findings that reach a subject: the
// subject's own places and the folders it is a member of, one hop. Membership
// supplies relevance here and nowhere else (C15, C23).
func (s *Store) Informational(ctx context.Context, subj Subject, offset, limit int) (Page, error) {
	subj, err := subj.normalize()
	if err != nil {
		return Page{}, err
	}
	if offset < 0 || limit < 0 {
		return Page{}, invalid("a page starts at a non-negative offset")
	}
	if limit == 0 || limit > MaxInformational {
		limit = MaxInformational
	}
	var page Page
	err = s.ws.ReadSnapshot(ctx, func(tx *sql.Tx) error {
		var err error
		page, err = informational(ctx, tx, subj, offset, limit)
		return err
	})
	return page, err
}

func (subj Subject) normalize() (Subject, error) {
	refs := make([]workspace.Ref, 0, len(subj.Refs))
	seen := make(map[workspace.Ref]bool, len(subj.Refs))
	for _, ref := range subj.Refs {
		if err := ref.Validate(); err != nil {
			return Subject{}, invalid("%v", err)
		}
		if !seen[ref] {
			seen[ref] = true
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 || len(refs) > MaxSubjectRefs {
		return Subject{}, invalid("a subject is 1–%d refs", MaxSubjectRefs)
	}
	subj.Refs = refs
	if subj.Workspace != "" {
		if !filepath.IsAbs(subj.Workspace) {
			return Subject{}, invalid("a workspace is an absolute path")
		}
		subj.Workspace = filepath.Clean(subj.Workspace)
	}
	switch subj.Phase {
	case PhaseChat, PhaseTask, PhaseUnattended, PhaseEgress:
	default:
		return Subject{}, invalid("unknown phase %q", subj.Phase)
	}
	return subj, nil
}

// ── the queries ─────────────────────────────────────────────────────────────
//
// EVERY QUERY HERE DRIVES FROM A SHORT LIST OF KEYS INTO A PRIMARY KEY OR AN
// INDEX, with CROSS JOIN pinning the order so the planner cannot choose to scan
// the big table and probe the list (L10). Their plans are checked in under
// testdata/explain and a law refuses a SCAN of any table that grows.

// named prefixes a statement with its name, so a statement the resolver sent
// can be matched to its checked-in plan by what it says it is.
func named(name, query string) string { return "/* " + name + " */ " + query }

// snapshotQuery is the newest revision the store holds, for the snapshot id.
var snapshotQuery = named("snapshot", "SELECT max(seq) FROM direction_revisions")

func valuesCTE(name string, columns string, rows, width int) string {
	row := "(" + strings.TrimSuffix(strings.Repeat("?,", width), ",") + ")"
	return name + "(" + columns + ") AS (VALUES " + strings.TrimSuffix(strings.Repeat(row+",", rows), ",") + ")"
}

// closureQuery walks placements upward from the subject's refs, edge by edge.
// UNION over edges terminates on any DAG and never enumerates diamond paths;
// the LIMIT stops the walk one edge past the bound.
func closureQuery(refs int) string {
	return named("closure", "WITH RECURSIVE "+valuesCTE("q", "kind,ref,sess", refs, 3)+`,
up(child_kind,child,child_sess,parent) AS (
 SELECT q.kind,q.ref,q.sess,p.collection_id FROM q CROSS JOIN placements p
  ON p.kind=q.kind AND p.ref_id=q.ref AND p.session_id=q.sess
 UNION
 SELECT 'collection',u.parent,'',p.collection_id FROM up u CROSS JOIN placements p
  ON p.kind='collection' AND p.ref_id=u.parent AND p.session_id=''
 LIMIT `+strconv.Itoa(MaxClosureEdges+1)+`
) SELECT child_kind,child,child_sess,parent FROM up`)
}

// candidateQuery finds the governing and pending live rows at the probed places.
func candidateQuery(keys int) string {
	return named("candidates", "WITH "+valuesCTE("q", "k,r,s", keys, 3)+`
SELECT l.target_kind,l.ref_id,l.session_id,l.lane,l.record_id,l.revision,l.reach,l.has_exclusions,l.has_links,l.written_at
 FROM q CROSS JOIN direction_live l ON l.target_kind=q.k AND l.ref_id=q.r AND l.session_id=q.s
 WHERE l.lane IN ('governing','pending') AND (l.lane='governing' OR l.written_at>=?)`)
}

// revisionsCTE is the list of (record, revision) pairs the follow-up reads key on.
func revisionsCTE(pairs int) string { return valuesCTE("c", "id,rev", pairs, 2) }

func exclusionQuery(pairs int) string {
	return named("exclusions", "WITH "+revisionsCTE(pairs)+`
SELECT e.record_id,e.target_kind,e.ref_id,e.session_id FROM c CROSS JOIN direction_exclusions e
 ON e.record_id=c.id AND e.revision=c.rev`)
}

func linkQuery(pairs int) string {
	return named("links", "WITH "+revisionsCTE(pairs)+`
SELECT k.record_id,k.link_kind,k.to_ref FROM c CROSS JOIN direction_links k
 ON k.record_id=c.id AND k.revision=c.rev WHERE k.link_kind IN (`+resolvedLinkKinds()+`)`)
}

// bodyQuery reads the bodies of the revisions the live index named, and only
// while each is still its record's current revision: a live row the records
// no longer say returns no body, which the resolver reports as drift.
func bodyQuery(pairs int) string {
	return named("bodies", "WITH "+revisionsCTE(pairs)+`
SELECT `+revisionColumns("r")+` FROM c CROSS JOIN direction_records d ON d.id=c.id AND d.revision=c.rev
 CROSS JOIN direction_revisions r ON r.record_id=c.id AND r.revision=c.rev`)
}

// legacyQuery reads each record's newest legacy name: a backwards seek of
// (record_id, revision) for the newest mapping's rowid, then that row by
// rowid. Two seeks per record, however often it was re-imported.
func legacyQuery(records int) string {
	return named("legacy", "WITH "+valuesCTE("c", "id", records, 1)+`
SELECT c.id,g.source_store,g.source_id,g.source_version FROM c CROSS JOIN direction_legacy g
 ON g.rowid=(SELECT n.rowid FROM direction_legacy n WHERE n.record_id=c.id ORDER BY n.revision DESC LIMIT 1)`)
}

// informationalQuery pages the findings at the subject's own places, at the
// folders it is a member of (one hop) and at everywhere or its legacy
// workspace. The page is cut in SQL — newest first, then by id — so only the
// page crosses into Go, each finding once with the places that matched it.
func informationalQuery(refs, extra int) string {
	cte := "WITH " + valuesCTE("q", "k,r,s", refs, 3)
	keys := `, keys(k,r,s) AS (
 SELECT k,r,s FROM q
 UNION SELECT 'collection',m.collection_id,'' FROM q CROSS JOIN memberships m
  ON m.kind=q.k AND m.ref_id=q.r AND m.session_id=q.s`
	if extra > 0 {
		cte += ", " + valuesCTE("x", "k,r,s", extra, 3)
		keys += `
 UNION SELECT k,r,s FROM x`
	}
	return named("informational", cte+keys+`)
SELECT l.record_id,l.revision,l.written_at,json_group_array(json_array(l.target_kind,l.ref_id,l.session_id))
 FROM keys CROSS JOIN direction_live l ON l.target_kind=keys.k AND l.ref_id=keys.r AND l.session_id=keys.s AND l.lane='informational'
 GROUP BY l.record_id ORDER BY l.written_at DESC, l.record_id LIMIT ? OFFSET ?`)
}

// ── the walk ────────────────────────────────────────────────────────────────

// node is a place in the governing graph: a subject ref, or a folder.
type node struct {
	kind         workspace.Kind
	ref, session string
}

func folderNode(id string) node { return node{kind: workspace.CollectionKind, ref: id} }

func refNode(r workspace.Ref) node { return node{kind: r.Kind, ref: r.ID, session: r.SessionID} }

// closure is the subject's governing ancestry: every placement edge above it.
type closure struct {
	roots   []node            // the subject's refs
	parents map[node][]string // child → the folders it is placed in
	depth   map[string]int    // folder → nearest depth (0 = the subject is placed in it)
	edges   []string          // for the snapshot identity
}

func readClosure(ctx context.Context, q querier, refs []workspace.Ref) (closure, error) {
	args := make([]any, 0, len(refs)*3)
	c := closure{parents: map[node][]string{}, depth: map[string]int{}}
	for _, r := range refs {
		args = append(args, string(r.Kind), r.ID, r.SessionID)
		c.roots = append(c.roots, refNode(r))
	}
	type edge struct {
		child  node
		parent string
	}
	edges, err := collect(ctx, q, closureQuery(len(refs)), args, func(row scanner) (edge, error) {
		var e edge
		return e, row.Scan(&e.child.kind, &e.child.ref, &e.child.session, &e.parent)
	})
	if err != nil {
		return closure{}, err
	}
	if len(edges) > MaxClosureEdges {
		return closure{}, fmt.Errorf("%w: more than %d placement edges above this work", ErrClosureTooLarge, MaxClosureEdges)
	}
	for _, e := range edges {
		c.parents[e.child] = append(c.parents[e.child], e.parent)
		c.edges = append(c.edges, fmt.Sprint(e.child, "→", e.parent))
	}
	sort.Strings(c.edges)
	for _, list := range c.parents {
		sort.Strings(list)
	}
	// Nearest depth by breadth-first search from the subject.
	frontier := c.roots
	for depth := 0; len(frontier) > 0; depth++ {
		var next []node
		for _, n := range frontier {
			for _, p := range c.parents[n] {
				if _, seen := c.depth[p]; !seen {
					c.depth[p] = depth
					next = append(next, folderNode(p))
				}
			}
		}
		frontier = next
	}
	return c, nil
}

// path is the shortest chain of folders from `from` up to `to`, never passing
// through a folder in avoid; nil when there is none. The walk is over at most
// MaxClosureEdges edges.
func (c closure) path(from node, to string, avoid map[string]bool) []string {
	type step struct {
		at   node
		prev int
	}
	queue := []step{{at: from, prev: -1}}
	seen := map[node]bool{from: true}
	for i := 0; i < len(queue); i++ {
		for _, p := range c.parents[queue[i].at] {
			n := folderNode(p)
			if seen[n] || avoid[p] {
				continue
			}
			seen[n] = true
			queue = append(queue, step{at: n, prev: i})
			if p == to {
				var chain []string
				for j := len(queue) - 1; j >= 0; j = queue[j].prev {
					if queue[j].at.kind == workspace.CollectionKind && queue[j].prev >= 0 {
						chain = append([]string{queue[j].at.ref}, chain...)
					}
				}
				return chain
			}
		}
	}
	return nil
}

// directPlacement reports whether the subject ref is placed in the folder
// itself, which is all a direct-reach folder rule reaches (C23).
func (c closure) directPlacement(from node, folder string) bool {
	for _, p := range c.parents[from] {
		if p == folder {
			return true
		}
	}
	return false
}

// ── resolving ───────────────────────────────────────────────────────────────

type candidate struct {
	id            string
	revision      int
	lane          Lane
	hasExclusions bool
	hasLinks      bool
	writtenAt     string
	targets       []Target
}

// applied is a candidate that survived, with its provenance and display rank.
type applied struct {
	candidate
	via, blocked []Path
	rank         int
}

// Display groups (R3). They order the list a reader sees and carry no
// precedence: compatible direction is a union.
const (
	rankSubject    = 0
	rankFolder     = 1 // + depth
	rankLegacy     = 1 << 20
	rankEverywhere = rankLegacy + 1
)

func resolveIn(ctx context.Context, q querier, subj Subject, now time.Time) (Effective, error) {
	c, err := readClosure(ctx, q, subj.Refs)
	if err != nil {
		return Effective{}, err
	}
	var maxSeq sql.NullInt64
	if err := q.QueryRowContext(ctx, snapshotQuery).Scan(&maxSeq); err != nil {
		return Effective{}, err
	}
	sum := sha256.Sum256([]byte(strings.Join(c.edges, "\n") + "\n" + strconv.FormatInt(maxSeq.Int64, 10)))
	eff := Effective{Snapshot: hex.EncodeToString(sum[:16]), Phase: subj.Phase, Placements: c.depth}

	cands, err := readCandidates(ctx, q, subj, c, now)
	if err != nil {
		return Effective{}, err
	}
	excluded, err := readExclusions(ctx, q, cands)
	if err != nil {
		return Effective{}, err
	}
	var governing, pending []applied
	for _, cand := range cands {
		a, ok := reach(cand, subj, c, excluded[cand.id])
		if !ok {
			continue
		}
		if cand.lane == LaneGoverning {
			governing = append(governing, a)
		} else {
			pending = append(pending, a)
		}
	}
	if len(governing) > MaxGoverning {
		return Effective{}, tooLarge(governing, nil)
	}
	links, err := readLinks(ctx, q, governing)
	if err != nil {
		return Effective{}, err
	}
	sortApplied(pending)
	if len(pending) > MaxPending {
		eff.PendingMore = true
		pending = pending[:MaxPending]
	}
	bodies, err := readBodies(ctx, q, append(append([]applied{}, governing...), pending...))
	if err != nil {
		return Effective{}, err
	}
	bytes := 0
	for _, a := range governing {
		body := bodies[a.id]
		bytes += len(body.Title) + len(body.Text)
	}
	if bytes > MaxGoverningBytes {
		return Effective{}, tooLarge(governing, bodies)
	}
	legacy, err := readLegacy(ctx, q, bodies)
	if err != nil {
		return Effective{}, err
	}
	eff.Governing = deliver(governing, bodies, legacy, true)
	eff.Pending = deliver(pending, bodies, legacy, false)
	eff.Conflicts = annotate(eff.Governing, links)
	if eff.Informational, err = informational(ctx, q, subj, 0, MaxInformational); err != nil {
		return Effective{}, err
	}
	return eff, nil
}

// probes is the set of places a record can name to reach the subject.
func probes(subj Subject, c closure) []placeKey {
	keys := make([]placeKey, 0, len(subj.Refs)+len(c.depth)+2)
	for _, r := range subj.Refs {
		keys = append(keys, placeKey{TargetKind(r.Kind), r.ID, r.SessionID})
	}
	folders := make([]string, 0, len(c.depth))
	for f := range c.depth {
		folders = append(folders, f)
	}
	sort.Strings(folders)
	for _, f := range folders {
		keys = append(keys, placeKey{TargetCollection, f, ""})
	}
	keys = append(keys, placeKey{TargetEverywhere, Everywhere, ""})
	if subj.Workspace != "" {
		keys = append(keys, placeKey{TargetLegacyWorkspace, subj.Workspace, ""})
	}
	return dedupKeys(keys)
}

func dedupKeys(keys []placeKey) []placeKey {
	seen := make(map[placeKey]bool, len(keys))
	out := keys[:0]
	for _, k := range keys {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

func keyArgs(keys []placeKey) []any {
	args := make([]any, 0, len(keys)*3)
	for _, k := range keys {
		args = append(args, string(k.kind), k.ref, k.session)
	}
	return args
}

func readCandidates(ctx context.Context, q querier, subj Subject, c closure, now time.Time) ([]candidate, error) {
	keys := probes(subj, c)
	type row struct {
		t Target
		candidate
	}
	rows, err := collect(ctx, q, candidateQuery(len(keys)), append(keyArgs(keys), stamp(now.Add(-PendingWindow))),
		func(r scanner) (row, error) {
			var x row
			return x, r.Scan(&x.t.Kind, &x.t.Ref, &x.t.Session, &x.lane, &x.id, &x.revision, &x.t.Reach,
				&x.hasExclusions, &x.hasLinks, &x.writtenAt)
		})
	if err != nil {
		return nil, err
	}
	byID := map[string]int{}
	var cands []candidate
	for _, r := range rows {
		i, ok := byID[r.id]
		if !ok {
			i = len(cands)
			byID[r.id] = i
			cands = append(cands, r.candidate)
		}
		cands[i].targets = append(cands[i].targets, r.t)
	}
	return cands, nil
}

func pairArgs[T any](items []T, pair func(T) (string, int)) []any {
	args := make([]any, 0, len(items)*2)
	for _, item := range items {
		id, rev := pair(item)
		args = append(args, id, rev)
	}
	return args
}

// readExclusions loads the exclusions of the candidates that have any.
func readExclusions(ctx context.Context, q querier, cands []candidate) (map[string][]placeKey, error) {
	var with []candidate
	for _, c := range cands {
		if c.hasExclusions {
			with = append(with, c)
		}
	}
	out := map[string][]placeKey{}
	if len(with) == 0 {
		return out, nil
	}
	type row struct {
		id string
		k  placeKey
	}
	rows, err := collect(ctx, q, exclusionQuery(len(with)), pairArgs(with, func(c candidate) (string, int) { return c.id, c.revision }),
		func(r scanner) (row, error) {
			var x row
			return x, r.Scan(&x.id, &x.k.kind, &x.k.ref, &x.k.session)
		})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.id] = append(out[r.id], r.k)
	}
	return out, nil
}

// reach decides whether a candidate applies to the subject and by which paths.
//
//   - A folder target reaches the subject through placements only (R1): a
//     direct-reach folder only where the subject is placed in it, a subtree
//     folder at any depth (R2, C23). Every other target matches by equality.
//   - An exclusion naming the subject — one of its refs, or its legacy
//     workspace — removes the record (R5).
//   - An exclusion naming a folder removes only the paths through that folder;
//     the record still applies if some path avoids every excluded folder, and
//     the removed paths are kept as Blocked (R6).
func reach(cand candidate, subj Subject, c closure, exclusions []placeKey) (applied, bool) {
	avoid := map[string]bool{}
	for _, e := range exclusions {
		for _, r := range subj.Refs {
			if e == (placeKey{TargetKind(r.Kind), r.ID, r.SessionID}) {
				return applied{}, false
			}
		}
		if e.kind == TargetLegacyWorkspace && e.ref == subj.Workspace {
			return applied{}, false
		}
		if e.kind == TargetCollection {
			avoid[e.ref] = true
		}
	}
	a := applied{candidate: cand, rank: rankEverywhere + 1}
	for _, t := range cand.targets {
		rank := targetRank(t, c)
		if t.Kind != TargetCollection {
			a.via = append(a.via, Path{Target: t, From: fromFor(t, subj)})
			a.rank = min(a.rank, rank)
			continue
		}
		via, blocked := folderPaths(t, subj, c, avoid)
		a.via = append(a.via, via...)
		a.blocked = append(a.blocked, blocked...)
		if len(via) > 0 {
			a.rank = min(a.rank, rank)
		}
	}
	return a, len(a.via) > 0
}

// fromFor is the subject ref a non-folder target matched.
func fromFor(t Target, subj Subject) workspace.Ref {
	for _, r := range subj.Refs {
		if TargetKind(r.Kind) == t.Kind && r.ID == t.Ref && r.SessionID == t.Session {
			return r
		}
	}
	return workspace.Ref{}
}

func targetRank(t Target, c closure) int {
	switch t.Kind {
	case TargetEverywhere:
		return rankEverywhere
	case TargetLegacyWorkspace:
		return rankLegacy
	case TargetCollection:
		if d, ok := c.depth[t.Ref]; ok {
			return rankFolder + d
		}
	}
	return rankSubject
}

// folderPaths is how a folder target reaches the subject. For each subject ref
// it keeps one shortest surviving chain; for each excluded folder that lies on
// a way to the target it keeps one blocked chain through it. That bounds the
// provenance by the subject's refs and the excluded folders rather than by the
// number of paths in the graph, which grows exponentially with diamonds.
func folderPaths(t Target, subj Subject, c closure, avoid map[string]bool) (via, blocked []Path) {
	for _, r := range subj.Refs {
		from := refNode(r)
		// The subject is itself the folder the record names.
		if r.Kind == workspace.CollectionKind && r.ID == t.Ref {
			via = append(via, Path{Target: t, From: r})
			continue
		}
		if t.Reach == Direct {
			if !c.directPlacement(from, t.Ref) {
				continue
			}
			if avoid[t.Ref] {
				blocked = append(blocked, Path{Target: t, From: r, Chain: []string{t.Ref}})
			} else {
				via = append(via, Path{Target: t, From: r, Chain: []string{t.Ref}})
			}
			continue
		}
		if chain := c.path(from, t.Ref, avoid); chain != nil && !avoid[t.Ref] {
			via = append(via, Path{Target: t, From: r, Chain: chain})
		}
		for _, e := range sortedKeys(avoid) {
			if _, inClosure := c.depth[e]; !inClosure {
				continue
			}
			toE := c.path(from, e, nil)
			if toE == nil {
				continue
			}
			if e == t.Ref {
				blocked = append(blocked, Path{Target: t, From: r, Chain: toE})
				continue
			}
			if onward := c.path(folderNode(e), t.Ref, nil); onward != nil {
				blocked = append(blocked, Path{Target: t, From: r, Chain: append(toE, onward...)})
			}
		}
	}
	return via, blocked
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// tooLarge names the target that contributes most (see
// GoverningTooLargeError): by records when bodies were not read, by bytes
// when they were. A record counts once per target, never once per path.
func tooLarge(governing []applied, bodies map[string]Revision) error {
	type weight struct {
		target         Target
		records, bytes int
	}
	per := map[placeKey]*weight{}
	e := &GoverningTooLargeError{Records: len(governing)}
	for _, a := range governing {
		size := 0
		if bodies != nil {
			size = len(bodies[a.id].Title) + len(bodies[a.id].Text)
			e.Bytes += size
		}
		counted := map[placeKey]bool{}
		for _, p := range a.via {
			k := placeKey{p.Target.Kind, p.Target.Ref, p.Target.Session}
			if counted[k] {
				continue
			}
			counted[k] = true
			if per[k] == nil {
				per[k] = &weight{target: p.Target}
			}
			per[k].records++
			per[k].bytes += size
		}
	}
	keys := make([]placeKey, 0, len(per))
	for k := range per {
		keys = append(keys, k)
	}
	measure := func(w *weight) int {
		if bodies != nil {
			return w.bytes
		}
		return w.records
	}
	sort.Slice(keys, func(i, j int) bool {
		if a, b := measure(per[keys[i]]), measure(per[keys[j]]); a != b {
			return a > b
		}
		return fmt.Sprint(keys[i]) < fmt.Sprint(keys[j])
	})
	if len(keys) > 0 {
		w := per[keys[0]]
		e.Heaviest, e.HeaviestRecords, e.HeaviestBytes = w.target, w.records, w.bytes
	}
	return e
}

func readLinks(ctx context.Context, q querier, governing []applied) ([]struct{ from, kind, to string }, error) {
	var with []applied
	for _, a := range governing {
		if a.hasLinks {
			with = append(with, a)
		}
	}
	if len(with) == 0 {
		return nil, nil
	}
	return collect(ctx, q, linkQuery(len(with)), pairArgs(with, func(a applied) (string, int) { return a.id, a.revision }),
		func(r scanner) (struct{ from, kind, to string }, error) {
			var l struct{ from, kind, to string }
			return l, r.Scan(&l.from, &l.kind, &l.to)
		})
}

// readBodies loads the bodies of what will be delivered. THE LIVE INDEX IS
// CHECKED AGAINST THE RECORDS FOR EVERYTHING IT DELIVERS: a row naming a
// revision that is not its record's current one, or a lane that revision is
// not in, is drift, and the resolve stops rather than hand out authority the
// records do not grant. The check is the body read itself, so it costs one
// seek per delivered record and nothing per record the store holds.
func readBodies(ctx context.Context, q querier, all []applied) (map[string]Revision, error) {
	out := make(map[string]Revision, len(all))
	if len(all) == 0 {
		return out, nil
	}
	revs, err := collect(ctx, q, bodyQuery(len(all)), pairArgs(all, func(a applied) (string, int) { return a.id, a.revision }), scanRevision)
	if err != nil {
		return nil, err
	}
	for _, r := range revs {
		out[r.ID] = r
	}
	for _, a := range all {
		r, ok := out[a.id]
		if !ok {
			return nil, fmt.Errorf("%w: the live index names %s revision %d, which is not its current revision; rebuild it", ErrDrift, a.id, a.revision)
		}
		if r.Lane() != a.lane {
			return nil, fmt.Errorf("%w: the live index puts %s in the %s lane; its revision %d is %s", ErrDrift, a.id, a.lane, a.revision, r.Lane())
		}
	}
	return out, nil
}

func readLegacy(ctx context.Context, q querier, bodies map[string]Revision) (map[string]*LegacyRef, error) {
	out := map[string]*LegacyRef{}
	if len(bodies) == 0 {
		return out, nil
	}
	ids := make([]any, 0, len(bodies))
	for id := range bodies {
		ids = append(ids, id)
	}
	type row struct {
		id  string
		ref LegacyRef
	}
	rows, err := collect(ctx, q, legacyQuery(len(ids)), ids, func(r scanner) (row, error) {
		var x row
		return x, r.Scan(&x.id, &x.ref.Store, &x.ref.ID, &x.ref.Version)
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		ref := r.ref
		out[r.id] = &ref
	}
	return out, nil
}

// sortApplied puts records in display order (R3): subject-direct, then folders
// by depth, then the legacy workspace, then everywhere; within a group the
// newest acceptance (or, for proposals, the newest proposal) first.
func sortApplied(list []applied) {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].rank != list[j].rank {
			return list[i].rank < list[j].rank
		}
		if list[i].writtenAt != list[j].writtenAt {
			return list[i].writtenAt > list[j].writtenAt
		}
		return list[i].id < list[j].id
	})
}

func deliver(list []applied, bodies map[string]Revision, legacy map[string]*LegacyRef, governing bool) []Applied {
	if governing {
		// Newest acceptance is the receipt's time, known only once bodies are read.
		for i := range list {
			list[i].writtenAt = stamp(bodies[list[i].id].Receipt.At)
		}
		sortApplied(list)
	}
	out := make([]Applied, 0, len(list))
	for _, a := range list {
		out = append(out, Applied{Rev: bodies[a.id], Via: a.via, Blocked: a.blocked, Legacy: legacy[a.id]})
	}
	return out
}

// annotate applies the links between governing records that both apply here.
// An overrides link annotates both records and both are delivered (R4); a
// conflicts_with pair is a Conflict unless the person also said which one
// governs here with an overrides link between them (R8).
//
// MUTUAL OVERRIDES RESOLVE NOTHING. A overrides B and B overrides A says
// nothing about which governs, so neither is annotated, the pair is an
// unresolved Conflict, and it never hides a conflicts_with the person wrote.
// Overrides are pairwise annotations and the delivery is a union, so a longer
// cycle (A over B over C over A) leaves every pair with one direction and
// needs no rule of its own.
func annotate(governing []Applied, links []struct{ from, kind, to string }) []Conflict {
	index := make(map[string]int, len(governing))
	for i, a := range governing {
		index[a.Rev.ID] = i
	}
	both := func(l struct{ from, kind, to string }) bool {
		_, ok := index[l.from]
		_, ok2 := index[l.to]
		return ok && ok2 && l.from != l.to
	}
	directed := map[[2]string]bool{}
	for _, l := range links {
		if both(l) && LinkKind(l.kind) == Overrides {
			directed[[2]string{l.from, l.to}] = true
		}
	}
	resolved, seen := map[[2]string]bool{}, map[[2]string]bool{}
	var conflicts []Conflict
	unresolved := func(p [2]string) {
		if !seen[p] {
			seen[p] = true
			conflicts = append(conflicts, Conflict{A: p[0], B: p[1]})
		}
	}
	for _, l := range links {
		if !both(l) || LinkKind(l.kind) != Overrides {
			continue
		}
		if directed[[2]string{l.to, l.from}] {
			unresolved(pairOf(l.from, l.to))
			continue
		}
		governing[index[l.from]].Overrides = append(governing[index[l.from]].Overrides, l.to)
		governing[index[l.to]].OverriddenBy = append(governing[index[l.to]].OverriddenBy, l.from)
		resolved[pairOf(l.from, l.to)] = true
	}
	for _, l := range links {
		if both(l) && LinkKind(l.kind) == ConflictsWith && !resolved[pairOf(l.from, l.to)] {
			unresolved(pairOf(l.from, l.to))
		}
	}
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].A+conflicts[i].B < conflicts[j].A+conflicts[j].B })
	return conflicts
}

func pairOf(a, b string) [2]string {
	if a > b {
		a, b = b, a
	}
	return [2]string{a, b}
}

// informational reads one page of the findings lane. The store cuts the page;
// Go reads only its bodies.
func informational(ctx context.Context, q querier, subj Subject, offset, limit int) (Page, error) {
	args := make([]any, 0, len(subj.Refs)*3+8)
	for _, r := range subj.Refs {
		args = append(args, string(r.Kind), r.ID, r.SessionID)
	}
	extra := []placeKey{{TargetEverywhere, Everywhere, ""}}
	if subj.Workspace != "" {
		extra = append(extra, placeKey{TargetLegacyWorkspace, subj.Workspace, ""})
	}
	args = append(append(args, keyArgs(extra)...), limit+1, offset)
	list, err := collect(ctx, q, informationalQuery(len(subj.Refs), len(extra)), args, func(r scanner) (applied, error) {
		a := applied{candidate: candidate{lane: LaneInformational}}
		var matched string
		if err := r.Scan(&a.id, &a.revision, &a.writtenAt, &matched); err != nil {
			return a, err
		}
		var places [][3]string
		if err := json.Unmarshal([]byte(matched), &places); err != nil {
			return a, err
		}
		for _, p := range places {
			t := Target{Kind: TargetKind(p[0]), Ref: p[1], Session: p[2]}
			a.via = append(a.via, Path{Target: t, From: fromFor(t, subj)})
		}
		return a, nil
	})
	if err != nil {
		return Page{}, err
	}
	page := Page{Items: make([]Applied, 0, len(list))}
	if len(list) > limit {
		page.More = true
		list = list[:limit]
	}
	bodies, err := readBodies(ctx, q, list)
	if err != nil {
		return Page{}, err
	}
	legacy, err := readLegacy(ctx, q, bodies)
	if err != nil {
		return Page{}, err
	}
	page.Items = deliver(list, bodies, legacy, false)
	return page, nil
}

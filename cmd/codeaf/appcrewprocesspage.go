package main

// processes.page for a crew and for all crews (internal/appwire's
// processpage.go has the wire; the process-history contract is
// docs/design/app-v3/PROTOCOL.md). The crew layer decides only WHICH
// conversations are read, in WHAT deterministic order, and says honestly which
// it could not read; every row is an owner's own, read from that owner's own
// authority exactly as the agent form reads it.
//
// THE ROSTER IS THE RECORDED ONE. It is the team file's membership at the first
// page: the selected team and every team below it by recorded parent (closed and
// archived ones included), their members as recorded, and nothing inferred from
// former members, removals, traffic or packets. A conversation in no team is
// never part of it.
//
// THERE IS NO CROSS-OWNER ORDER. Owners are visited in rounds by id, five rows
// of one owner's own page per visit; live-first holds inside one owner only. A
// reply says how many owners are read to their end, so a surface never claims
// a whole day before the walk has ended.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// The bounds of a crew walk.
const (
	// crewQuantum is how many rows one visit asks of one owner. It is a constant
	// of the walk and no parameter, so every client sees the same fairness.
	crewQuantum = 5
	// crewVisitsMax bounds the owners one page visits, so a roster of owners
	// with nothing in them cannot hold a page.
	crewVisitsMax = 256
	// crewOwnerCall is the longest one owner is given within the page's deadline.
	crewOwnerCall = 250 * time.Millisecond
	// crewCursorMax bounds an owner's own cursor held by a walk.
	crewCursorMax = appwire.ProcessCursorMax
	// crewKeyMax bounds a stored member key held for naming an unnamed owner;
	// a longer one is never cut into a different identity, the owner is only
	// counted in more.
	crewKeyMax = 256
	// crewNameMax bounds a conversation id named in a reply.
	crewNameMax = 128
	// crewPartialBytesMax bounds the ENCODED bytes of the owners.partial names
	// of one page, JSON escaping included; names past it are counted in more.
	crewPartialBytesMax = 32 << 10
	// crewReplyReserve is what a reply keeps for its scalars: revision, roster,
	// counts and next. Together with the partial names it must fit in
	// appProcessPageEnvelope, which a test pins.
	crewReplyReserve = 8 << 10

	crewWalkBytesMax  = 8 << 20
	crewRegistryBytes = 64 << 20
	crewWalksMax      = 16
	crewWalksPerScope = 2
	crewIdleTTL       = 5 * time.Minute
	crewAgeMax        = 15 * time.Minute

	crewTokenPrefix = "c1."
	crewTokenLen    = len(crewTokenPrefix) + 22
)

// An owner's standing in a walk.
const (
	crewOwnerActive = iota
	crewOwnerExhausted
	crewOwnerPartial
)

// crewOwner is one conversation of the roster and where its walk stands.
type crewOwner struct {
	// id is the conversation's id, the folder its transcript is in; "" for a
	// member that did not verify, which is partial (missing) from the start and
	// never stands in for any conversation. key is that member's stored key,
	// kept only when it can be named whole.
	id, key string
	// bucket and file are the project folder and the exact stored path of a
	// named owner: the stored transcript path is exactly
	// <places>/<bucket>/<id>/transcript.jsonl.
	bucket, file string
	// cursor is the owner's own opaque page cursor, empty before its first
	// visit.
	cursor string
	state  int
	reason string
	// told says a page has already named this owner as partial.
	told bool
}

// crewWalk is one walk's state, held by the registry.
type crewWalk struct {
	scope    string
	revision string
	owners   []crewOwner
	// at is the next owner to visit; rounds are cycles of the list.
	at         int
	total      int
	done       int
	unread     int
	incomplete bool
	finished   bool

	cur, prev string
	// last is the encoded reply that answered the request carrying prev.
	last []byte

	busy      bool
	born      time.Time
	touched   time.Time
	accounted int
}

// bytes is what this walk holds, counted from what it holds: every stored
// string and byte slice plus the compiled size of each record. The encoded reply
// is counted at its length.
func (w *crewWalk) bytes() int {
	n := int(unsafe.Sizeof(*w)) + len(w.scope) + len(w.revision) + len(w.cur) + len(w.prev) + len(w.last)
	for i := range w.owners {
		o := &w.owners[i]
		n += int(unsafe.Sizeof(*o)) + len(o.id) + len(o.key) + len(o.bucket) + len(o.file) + len(o.cursor) + len(o.reason)
	}
	return n
}

func (w *crewWalk) active() bool {
	for i := range w.owners {
		if w.owners[i].state == crewOwnerActive {
			return true
		}
	}
	return false
}

// crewRegistry holds the walks, in memory only: nothing is written, and a
// restart ends every walk.
type crewRegistry struct {
	mu     sync.Mutex
	now    func() time.Time
	walks  map[string]*crewWalk // by current AND previous token
	live   []*crewWalk
	bytes  int
	limits crewLimits
}

type crewLimits struct{ walk, total, walks, perScope int }

func newCrewRegistry() *crewRegistry {
	return &crewRegistry{
		now: time.Now, walks: map[string]*crewWalk{},
		limits: crewLimits{walk: crewWalkBytesMax, total: crewRegistryBytes, walks: crewWalksMax, perScope: crewWalksPerScope},
	}
}

func crewStale() error {
	return appwire.Errorf(appwire.CodeStaleCursor, "the crew history walk ended; read it again from the first page")
}

func crewBusy(what string) error {
	return appwire.Errorf(appwire.CodeBusy, "%s; try again", what)
}

func crewToken() string {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	return crewTokenPrefix + base64.RawURLEncoding.EncodeToString(raw[:])
}

// removeLocked forgets a walk and what it held.
func (r *crewRegistry) removeLocked(w *crewWalk) {
	delete(r.walks, w.cur)
	delete(r.walks, w.prev)
	for i, x := range r.live {
		if x == w {
			r.live = append(r.live[:i], r.live[i+1:]...)
			break
		}
	}
	r.bytes -= w.accounted
	w.accounted = 0
}

// expireLocked ends every idle walk past its idle time or age.
func (r *crewRegistry) expireLocked() {
	now := r.now()
	for _, w := range append([]*crewWalk(nil), r.live...) {
		if !w.busy && (now.Sub(w.touched) > crewIdleTTL || now.Sub(w.born) > crewAgeMax) {
			r.removeLocked(w)
		}
	}
}

// evictLocked ends the least recently used idle walk of scope ("" for any),
// and says whether there was one. A walk with a page in flight is never one.
func (r *crewRegistry) evictLocked(scope string) bool {
	var victim *crewWalk
	for _, w := range r.live {
		if w.busy || (scope != "" && w.scope != scope) {
			continue
		}
		if victim == nil || w.touched.Before(victim.touched) {
			victim = w
		}
	}
	if victim == nil {
		return false
	}
	r.removeLocked(victim)
	return true
}

func (r *crewRegistry) scopeCountLocked(scope string) int {
	n := 0
	for _, w := range r.live {
		if w.scope == scope {
			n++
		}
	}
	return n
}

// admit registers a new walk, pinned, evicting idle ones in the order the
// limits ask: the scope's own first, then any. A walk too large for one walk is
// too_large; one that cannot be fitted beside pinned walks is busy.
func (r *crewRegistry) admit(w *crewWalk) error {
	size := w.bytes()
	if size > r.limits.walk {
		return appwire.Errorf(appwire.CodeTooLarge, "this crew's history walk is larger than one walk may hold")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked()
	for r.scopeCountLocked(w.scope) >= r.limits.perScope {
		if !r.evictLocked(w.scope) {
			return crewBusy("too many walks of this crew are being read")
		}
	}
	for len(r.live) >= r.limits.walks || r.bytes+size > r.limits.total {
		if !r.evictLocked("") {
			return crewBusy("too many crew histories are being read")
		}
	}
	now := r.now()
	w.born, w.touched, w.busy, w.accounted = now, now, true, size
	r.live = append(r.live, w)
	r.bytes += size
	return nil
}

// crewReplay is what a token meant: a page to run, or the stored reply to give
// again.
type crewReplay struct {
	walk  *crewWalk
	reply []byte
}

// take finds the walk a cursor belongs to. The current token pins the walk for
// the next page; the previous token answers the stored reply; anything else
// is stale.
func (r *crewRegistry) take(token, scope string) (crewReplay, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked()
	w := r.walks[token]
	if w == nil {
		return crewReplay{}, crewStale()
	}
	if w.scope != scope {
		return crewReplay{}, appwire.Errorf(appwire.CodeBadParams, "the cursor belongs to another crew's history")
	}
	if w.busy {
		return crewReplay{}, crewBusy("another page of this walk is being read")
	}
	if token == w.prev {
		if w.last == nil {
			return crewReplay{}, crewStale()
		}
		w.touched = r.now()
		return crewReplay{reply: w.last}, nil
	}
	w.busy, w.touched = true, r.now()
	return crewReplay{walk: w}, nil
}

// settle unpins a walk and accounts what it now holds. When the walk with its
// reply is over its share, the reply is dropped (the previous token then goes
// stale) rather than the walk; when its state alone is, the walk ends.
func (r *crewRegistry) settle(w *crewWalk, end bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	w.busy = false
	w.touched = r.now()
	if end {
		r.removeLocked(w)
		return nil
	}
	if len(w.last) > appwire.ProcessPageReplyMax || w.bytes() > r.limits.walk {
		w.last = nil
	}
	size := w.bytes()
	if size > r.limits.walk {
		r.removeLocked(w)
		return appwire.Errorf(appwire.CodeTooLarge, "this crew's history walk is larger than one walk may hold")
	}
	for r.bytes-w.accounted+size > r.limits.total {
		w.busy = true // the walk being settled is never its own victim
		ok := r.evictLocked("")
		w.busy = false
		if !ok {
			w.last = nil
			size = w.bytes()
			break
		}
	}
	r.bytes += size - w.accounted
	w.accounted = size
	return nil
}

// release unpins a walk a page did not advance, keeping it and its cursor for
// a retry within its idle time.
func (r *crewRegistry) release(w *crewWalk) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w.busy = false
	w.touched = r.now()
}

// unreachable reports whether no token can reach the walk any more.
func (r *crewRegistry) unreachable(w *crewWalk) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return w.cur == "" && w.prev == ""
}

// storeLast keeps raw as the reply the previous token replays.
func (r *crewRegistry) storeLast(w *crewWalk, raw []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w.last = raw
}

// rotate gives the walk next as its new current token ("" when the walk is
// over), the old one becoming the previous, which replays the reply stored
// with it.
func (r *crewRegistry) rotate(w *crewWalk, next string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.walks, w.prev)
	w.prev, w.last = w.cur, nil
	if w.prev != "" {
		r.walks[w.prev] = w
	}
	w.cur = next
	if next != "" {
		r.walks[next] = w
	}
}

// ── the roster ──────────────────────────────────────────────────────────────

// crewSelection is the target of a request: one team's subtree, or every team.
type crewSelection struct {
	crew string
	all  bool
}

func (s crewSelection) scope() string {
	if s.all {
		return "all"
	}
	return "crew:" + s.crew
}

// crewRoster is the recorded membership of a selection.
type crewRoster struct {
	revision string
	owners   []crewOwner
}

// crewPlaceOf is the bucket and id a stored transcript path names, and true
// only when it is exactly <places>/<bucket>/<id>/transcript.jsonl under one of
// roots, clean, absolute, with plain single-component bucket and id.
func crewPlaceOf(file string, roots []string) (bucket, id string, ok bool) {
	if file == "" || !filepath.IsAbs(file) || filepath.Clean(file) != file || filepath.Base(file) != "transcript.jsonl" {
		return "", "", false
	}
	for _, root := range roots {
		rel, err := filepath.Rel(root, file)
		if err != nil {
			continue
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) != 3 || !crewPlain(parts[0]) || !crewPlain(parts[1]) {
			continue
		}
		return parts[0], parts[1], true
	}
	return "", "", false
}

// crewPlain is a single plain path component that is not hidden.
func crewPlain(name string) bool {
	return name != "" && !strings.HasPrefix(name, ".") && !strings.ContainsAny(name, `/\`+"\x00") && filepath.Base(name) == name
}

// crewName is a conversation id fit to be named in a reply whole.
func crewName(id string) bool {
	return crewPlain(id) && len(id) <= crewNameMax && utf8.ValidString(id)
}

// crewPlacesRoots are the spellings of the places root a stored path may use:
// the configured one, and where it resolves.
func crewPlacesRoots() []string {
	root := filepath.Clean(session.PlacesRoot())
	roots := []string{root}
	if resolved, err := filepath.EvalSymlinks(root); err == nil && filepath.Clean(resolved) != root {
		roots = append(roots, filepath.Clean(resolved))
	}
	return roots
}

// crewRosterOf reads the selection's recorded membership from a stored list.
// Nothing here is guessed: a member's owner is the folder of its stored
// transcript path, and a member that names none is kept as an owner that could
// not be read.
func crewRosterOf(ctx context.Context, teams []teamstore.Team, sel crewSelection, roots []string) (*crewRoster, error) {
	steps := 0
	tick := func() error {
		if steps++; steps%appTeamsPollEvery == 0 && ctx.Err() != nil {
			return appTeamsBusy()
		}
		return nil
	}
	shape, err := appTeamsStructure(teams, tick)
	if err != nil {
		return nil, err
	}
	var picked []int
	if sel.all {
		for i := range teams {
			picked = append(picked, i)
		}
	} else {
		at := shape.byID[sel.crew]
		if at == 0 {
			return nil, appwire.Errorf(appwire.CodeNotFound, "there is no crew %s", sel.crew)
		}
		picked = []int{at - 1}
		for next := 0; next < len(picked); next++ {
			if err := tick(); err != nil {
				return nil, err
			}
			picked = append(picked, shape.children[teams[picked[next]].ID]...)
		}
	}
	var lines []string
	record := func(kind string, fields ...string) {
		var line strings.Builder
		line.WriteString(kind)
		for _, f := range fields {
			line.WriteString(strconv.Itoa(len(f)))
			line.WriteByte(':')
			line.WriteString(f)
		}
		lines = append(lines, line.String())
	}
	owners := map[string]*crewOwner{}
	pairs := 0
	for _, i := range picked {
		t := teams[i]
		flags := ""
		if t.Closed() {
			flags += "c"
		}
		if !t.ArchivedAt.IsZero() {
			flags += "a"
		}
		record("t", t.ID, t.Parent, flags)
		held := map[string]bool{}
		for _, m := range t.Members {
			if err := tick(); err != nil {
				return nil, err
			}
			// ONLY A STORED PATH THAT IS EXACTLY <places>/<bucket>/<id>/
			// transcript.jsonl NAMES AN OWNER. Anything else — another machine's
			// path, a key with no file — is an unnamed owner that could not be
			// read; its basename or key is never looked up, so it can neither
			// stand in for a conversation held here nor merge with one.
			bucket, id, placed := crewPlaceOf(m.File, roots)
			named := placed && crewName(id)
			key := "\x00" + t.ID + "\x00" + m.Key + "\x00" + m.File
			if named {
				key = bucket + "\x00" + id
			}
			if held[key] {
				continue
			}
			held[key] = true
			if pairs++; pairs > appTeamsPairsMax {
				return nil, appwire.Errorf(appwire.CodeTooLarge, "the crew holds more members than can be listed as one history")
			}
			o := owners[key]
			if o == nil {
				if len(owners) >= appTeamsOwnersMax {
					return nil, appwire.Errorf(appwire.CodeTooLarge, "the crew holds more conversations than can be listed as one history")
				}
				o = &crewOwner{}
				if named {
					o.id, o.bucket, o.file = id, bucket, m.File
				} else {
					o.state, o.reason = crewOwnerPartial, appwire.ProcessOwnerMissing
					stored := m.Key
					if stored == "" {
						stored = m.File
					}
					if len(stored) <= crewKeyMax && utf8.ValidString(stored) {
						o.key = stored
					}
				}
				owners[key] = o
			} else if named && m.File < o.file {
				o.file = m.File
			}
			record("m", t.ID, key)
		}
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	out := &crewRoster{revision: hex.EncodeToString(sum[:16])}
	for _, o := range owners {
		out.owners = append(out.owners, *o)
	}
	sort.Slice(out.owners, func(i, j int) bool {
		a, b := out.owners[i], out.owners[j]
		if (a.id == "") != (b.id == "") {
			return a.id != ""
		}
		if a.id != b.id {
			return a.id < b.id
		}
		if a.bucket != b.bucket {
			return a.bucket < b.bucket
		}
		return a.key < b.key
	})
	return out, nil
}

// roster reads the stored teams once and the selection's roster from them.
func (b *appBackend) crewRoster(ctx context.Context, sel crewSelection) (*crewRoster, error) {
	file, err := b.teamsSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return crewRosterOf(ctx, file.Teams, sel, crewPlacesRoots())
}

// ── the walk ────────────────────────────────────────────────────────────────

// crewHistory is the backend's crew walks and the doors they read through. The
// pager is a field so the rule is proved without an engine or a disk.
type crewHistory struct {
	once sync.Once
	reg  *crewRegistry
	// visit reads one owner's next page: rows at most n, its own cursor in and
	// out. ok is false when the owner could not be read at all, with why.
	visit func(ctx context.Context, b *appBackend, o crewOwner, n int) (*appwire.ProcessPageResult, error)
}

func (b *appBackend) crewWalks() *crewRegistry {
	b.crewHistory.once.Do(func() {
		if b.crewHistory.reg == nil {
			b.crewHistory.reg = newCrewRegistry()
		}
	})
	return b.crewHistory.reg
}

// PageCrewProcesses answers one page of a crew's, or every crew's, recorded
// process history.
func (b *appBackend) PageCrewProcesses(ctx context.Context, ask appwire.ProcessPageAsk) (*appwire.ProcessPageResult, error) {
	limit, err := appwire.CheckProcessCrewAsk(ask)
	if err != nil {
		return nil, err
	}
	sel := crewSelection{crew: strings.TrimSpace(ask.Crew), all: ask.Crew == ""}
	reg := b.crewWalks()
	var walk *crewWalk
	if ask.Cursor == "" {
		roster, err := b.crewRoster(ctx, sel)
		if err != nil {
			return nil, err
		}
		walk = &crewWalk{scope: sel.scope(), revision: roster.revision, owners: roster.owners, total: len(roster.owners)}
		// Members no conversation could be named for are partial from the start.
		for _, o := range roster.owners {
			if o.state == crewOwnerPartial {
				walk.unread++
				walk.done++
			}
		}
		if err := reg.admit(walk); err != nil {
			return nil, err
		}
	} else {
		if len(ask.Cursor) != crewTokenLen || !strings.HasPrefix(ask.Cursor, crewTokenPrefix) {
			return nil, appwire.Errorf(appwire.CodeBadParams, "cursor is one a previous page gave")
		}
		taken, err := reg.take(ask.Cursor, sel.scope())
		if err != nil {
			return nil, err
		}
		if taken.reply != nil {
			var again appwire.ProcessPageResult
			if json.Unmarshal(taken.reply, &again) != nil {
				return nil, crewStale()
			}
			return &again, nil
		}
		walk = taken.walk
		// A CHANGED MEMBERSHIP ENDS THE WALK; a rename, a recolour or spend does
		// not, because the revision is of the recorded tree and the verified
		// (team, conversation) pairs only. A roster that merely could not be read
		// in time ends nothing: the cursor stays for a retry.
		roster, err := b.crewRoster(ctx, sel)
		switch {
		case err == nil && roster.revision != walk.revision:
			_ = reg.settle(walk, true)
			return nil, crewStale()
		case err != nil:
			reg.release(walk)
			return nil, err
		}
	}
	return b.crewCommit(ctx, reg, walk, limit)
}

// crewCommit runs the page and commits it, or changes nothing: the walk's
// progress is saved before the step and put back if the page cannot be
// committed, so a failed page never skips what it read.
func (b *appBackend) crewCommit(ctx context.Context, reg *crewRegistry, walk *crewWalk, limit int) (res *appwire.ProcessPageResult, err error) {
	// ONLY PROGRESS IS SAVED AND PUT BACK. The registry reads and writes the
	// walk's other fields (pin, times, tokens, stored reply, accounted size)
	// under its lock from other requests, so a restore of the whole walk would
	// race with them; the progress fields are the page's alone while it holds
	// the pin.
	type progress struct {
		owners               []crewOwner
		at, done, unread     int
		incomplete, finished bool
	}
	snap := progress{append([]crewOwner(nil), walk.owners...), walk.at, walk.done, walk.unread, walk.incomplete, walk.finished}
	committed := false
	// EVERY WAY OUT OF A PAGE THAT DID NOT COMMIT, a panic included, puts the
	// progress back and lets go of the pin: a walk a token can reach stays for a
	// retry; one no token can reach (a first page) is removed, as is one no retry
	// can help (a page too large for a reply). A pin is never released by time —
	// a page still reading is the only writer — so it ends only when the page does.
	defer func() {
		if committed {
			return
		}
		walk.owners, walk.at, walk.done, walk.unread, walk.incomplete, walk.finished =
			snap.owners, snap.at, snap.done, snap.unread, snap.incomplete, snap.finished
		if reg.unreachable(walk) || (err != nil && isWireCode(err, appwire.CodeTooLarge)) {
			_ = reg.settle(walk, true)
			return
		}
		reg.release(walk)
	}()
	out, reported, err := b.crewStep(ctx, walk, limit)
	if err != nil {
		return nil, err
	}
	more := walk.active()
	next := ""
	if more {
		next = crewToken()
	}
	out.Next = next
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, appwire.Errorf(appwire.CodeEngine, "the crew history could not be encoded")
	}
	if len(raw) > appwire.ProcessPageReplyMax {
		return nil, appwire.Errorf(appwire.CodeTooLarge, "a page of the crew history is larger than a reply may be")
	}
	committed = true
	for _, i := range reported {
		walk.owners[i].told = true
	}
	if !more {
		walk.finished, walk.owners = true, nil
	}
	reg.rotate(walk, next)
	if !more && walk.prev == "" {
		// A walk that began and ended in one page has no token to replay.
		_ = reg.settle(walk, true)
		return out, nil
	}
	reg.storeLast(walk, raw)
	if err := reg.settle(walk, false); err != nil {
		return nil, err
	}
	return out, nil
}

// crewStep runs one page of visits against the walk. A visit is atomic: its
// rows join the reply whole and its owner's state moves, or neither happens,
// so a reply never splits an owner's page and never skips a row. It answers the
// owners to mark as told once the page is committed; the caller restores the
// walk when it is not.
func (b *appBackend) crewStep(ctx context.Context, w *crewWalk, limit int) (*appwire.ProcessPageResult, []int, error) {
	out := &appwire.ProcessPageResult{Processes: []appwire.Process{}, Revision: w.revision, Roster: appwire.ProcessRosterRecorded}
	used := appProcessPageEnvelope
	var named []appwire.ProcessOwnerPartial
	var reported []int
	partialBytes, more := 0, 0
	// mark names an owner that became partial, within the page's partial bytes.
	// A name that does not fit whole, or that cannot be named whole, is counted
	// in more: identity is never cut into a different one.
	mark := func(i int) {
		o := &w.owners[i]
		if o.told || o.state != crewOwnerPartial {
			return
		}
		reported = append(reported, i)
		p := appwire.ProcessOwnerPartial{Reason: o.reason}
		if o.id != "" {
			p.Agent = o.id
		} else {
			p.Key = o.key
		}
		raw, _ := json.Marshal(p)
		if (p.Agent == "" && p.Key == "") || len(named) == appwire.ProcessPartialMax || partialBytes+len(raw)+1 > crewPartialBytesMax {
			more++
			return
		}
		partialBytes += len(raw) + 1
		named = append(named, p)
	}
	for i := range w.owners {
		mark(i)
	}
	visits := 0
	for visits < crewVisitsMax && ctx.Err() == nil && w.active() {
		// A VISIT ASKS min(5, ROOM LEFT): the page fills to its limit whatever the
		// limit is, and the owner's own cursor says exactly where it stopped, so
		// no limit from 1 to 100 repeats or skips a row.
		n := min(crewQuantum, limit-len(out.Processes))
		if n <= 0 {
			break
		}
		if appwire.ProcessPageReplyMax-used < n*(appProcessRowMax+1) {
			break
		}
		for w.owners[w.at%len(w.owners)].state != crewOwnerActive {
			w.at++
		}
		w.at %= len(w.owners)
		at := w.at
		o := &w.owners[at]
		res, err := b.crewVisit(ctx, *o, n)
		if ctx.Err() != nil {
			// The page's own time or request is gone: this visit does not count.
			break
		}
		visits++
		w.at++
		if err != nil {
			o.state, o.reason = crewOwnerPartial, crewReason(err)
			w.unread++
			w.done++
			mark(at)
			continue
		}
		for _, p := range res.Processes {
			raw, _ := json.Marshal(p)
			used += len(raw) + 1
			out.Processes = append(out.Processes, p)
		}
		w.incomplete = w.incomplete || res.Incomplete
		switch {
		case res.Next == "":
			o.state = crewOwnerExhausted
			w.done++
		case res.Next == o.cursor || len(res.Next) > crewCursorMax:
			// An owner whose cursor does not move would be asked the same
			// question for ever; its rows are kept and it is named.
			o.state, o.reason = crewOwnerPartial, appwire.ProcessOwnerUnreadable
			w.unread++
			w.done++
			mark(at)
		default:
			o.cursor = res.Next
		}
	}
	if visits == 0 && w.active() {
		return nil, nil, crewBusy("the crew history could not advance in time")
	}
	out.Incomplete = w.incomplete || w.unread > 0
	out.Owners = &appwire.ProcessOwners{Total: w.total, Done: w.done, Unread: w.unread, Partial: named, More: more}
	return out, reported, nil
}

// crewReason is why an owner could not be read.
func crewReason(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return appwire.ProcessOwnerTimeout
	case errors.Is(err, session.ErrProcessStale), isWireCode(err, appwire.CodeStaleCursor):
		return appwire.ProcessOwnerStale
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, session.ErrWithinRoot), errors.Is(err, errCrewOwnerMissing):
		return appwire.ProcessOwnerMissing
	}
	return appwire.ProcessOwnerUnreadable
}

func isWireCode(err error, code string) bool {
	var wire *appwire.Error
	return errors.As(err, &wire) && wire.Code == code
}

var errCrewOwnerMissing = errors.New("that conversation is not held and its stored location cannot be read")

// crewVisit reads one owner's next n rows within its share of the page's time.
func (b *appBackend) crewVisit(ctx context.Context, o crewOwner, n int) (*appwire.ProcessPageResult, error) {
	call := crewOwnerCall
	if deadline, ok := ctx.Deadline(); ok {
		call = min(call, time.Until(deadline))
	}
	vctx, cancel := context.WithTimeout(ctx, max(call, 0))
	defer cancel()
	if b.crewHistory.visit != nil {
		return b.crewHistory.visit(vctx, b, o, n)
	}
	return b.crewOwnerPage(vctx, o, n)
}

// crewOwnerPage is the real visit: the engine that holds the conversation is
// asked when one does; otherwise the conversation is read from the exact stored
// transcript location, through a descriptor pinned below the places root.
// Open crews restore their owner before asking it; other stored conversations
// stay closed. No directory is listed.
func (b *appBackend) crewOwnerPage(ctx context.Context, o crewOwner, n int) (*appwire.ProcessPageResult, error) {
	if o.bucket == "" || o.id == "" {
		return nil, errCrewOwnerMissing
	}
	places := session.PlacesRoot()
	// THE STORED LOCATION IS VERIFIED THROUGH A PINNED DESCRIPTOR BEFORE
	// ANYTHING ABOUT IT IS USED, a held conversation's cache included.
	conv, err := session.OpenConversationWithin(places, o.bucket, o.id)
	if err != nil {
		return nil, err
	}
	conv.Close()
	// A HELD CONVERSATION IS USABLE ONLY AS THE EXACT ONE THE ROSTER RECORDS:
	// its live engine's own transcript path is the stored file, byte for byte.
	// A same-named conversation at another path is not this member, and is not
	// asked; this member is then read as saved, which says unknown, not running.
	var a *appAgent
	b.locked(func() {
		if candidate := b.agents[o.id]; candidate != nil && candidate.transcript == o.file {
			a = candidate
		}
	})
	if a != nil && b.crewKeeps(o.id) {
		if _, _, err := b.heldLive(o.id); err != nil {
			return nil, err
		}
	}
	var live *appLive
	convDir := ""
	b.locked(func() {
		if a := b.agents[o.id]; a != nil && a.live != nil && a.transcript != "" && a.transcript == o.file {
			live = a.live
			convDir = session.DirOf(a.transcript)
		}
	})
	src := appProcessOwner{agent: o.id, live: live, convDir: convDir}
	if live == nil {
		bucket, id := o.bucket, o.id
		src.read = func(ctx context.Context, ask session.ProcessPageAsk) (session.ProcessPage, error) {
			return session.ReadProcessPageWithinRoot(ctx, places, bucket, id, ask)
		}
	}
	return b.pageOwner(ctx, src, o.cursor, n, appProcessPageEnvelope+n*(appProcessRowMax+1))
}

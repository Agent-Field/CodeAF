package teams

import (
	"sort"
	"strings"
)

// ── MOVING TEAMS: WHERE ONE MAY GO, AND WHAT A MOVE CHANGES ─────────────────
//
// The person moves a team by putting it inside another (ruling c-12, amended
// by c-13): `Move into…` on the teams page, a drag in its rail, or `Inside:` on
// the team's card. The write itself is [File.SetParent], which refuses only
// what would break the tree (a loop, the root under something). Two questions
// come before it, and both are here rather than in the interface because the
// session's own restructuring tools will ask the same ones:
//
// WHERE MAY IT GO ([File.MoveCheck]). Not into itself or anything under it,
// not into a closed team, and not so deep that the deepest team it carries
// would stand past the depth limit of the team it lands in. The answer is a
// [MoveBlock] with the facts a sentence needs (`harbor is 3 levels deep ·
// limit 3 · Settings`), and the interface draws every target, dimming the ones
// that are blocked and saying why, so nothing is hidden.
//
// WHAT WOULD CHANGE ([File.MoveEffects]). A move is only a line in a tree
// until it moves authority, money or judgement, and those are what a person
// should see before it happens: whose manager each conversation carried along
// reports to ([File.Home]), whose capped pool the moved team's spend now counts
// toward (a cap is a pool over the subtree, spend.go), and which manager
// decides a conflict among the team's own conversations ([File.LCA]). Each is
// computed on a copy of the file as the move would leave it, tidied exactly as
// the store's write would tidy it, so the answer is the one the next read will
// show. A move that changes none of them is applied at once.

// The kinds of [MoveBlock].
const (
	// MoveBlockSelf is a team asked to go inside itself.
	MoveBlockSelf = "self"
	// MoveBlockInside is a team asked to go inside a team under it.
	MoveBlockInside = "inside"
	// MoveBlockClosed is a closed target.
	MoveBlockClosed = "closed"
	// MoveBlockDepth is a move that would stand a team past the target's
	// depth limit.
	MoveBlockDepth = "depth"
	// MoveBlockHere is a target every moved team is already directly under.
	MoveBlockHere = "here"
	// MoveBlockRoot is the root team asked to move (it holds every team).
	MoveBlockRoot = "root"
	// MoveBlockGone is a team or a target that is not in the file.
	MoveBlockGone = "gone"
)

// MoveBlock is why teams cannot go inside a target. Team names the team the
// reason is about: the moved team for self, inside and root, the target for
// closed, depth and here. For depth, Depth is how many levels deep the target
// stands, Need how many levels the moved teams take up (a team with one level
// of sub-teams under it needs two), Limit the target's effective depth limit
// and LimitFrom where that limit came from.
type MoveBlock struct {
	Kind      string
	Team      string
	Name      string
	Depth     int
	Need      int
	Limit     int
	LimitFrom Origin
}

// MoveTarget is the id a move into parent really writes: "" is the top level,
// which is the root team when there is one (root.go keeps every other
// top-level team under it).
func (f *File) MoveTarget(parent string) string {
	if parent != "" {
		return parent
	}
	if r, ok := f.Root(); ok {
		return r.ID
	}
	return ""
}

// MoveRoots is ids with every id that sits under another of them left out, in
// the order given: a team moved together with its parent goes along inside
// it and is not moved on its own.
func (f *File) MoveRoots(ids []string) []string {
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		carried := false
		for _, a := range f.Ancestors(id) {
			if set[a.ID] {
				carried = true
				break
			}
		}
		if !carried {
			out = append(out, id)
		}
	}
	return out
}

// levels is how many levels team id and its open sub-teams take up: 1 for a
// team with none under it. The walk is bounded by the list, so a loop in a
// file not yet tidied cannot hang it.
func (f *File) levels(id string) int { return f.levelsWithin(id, len(f.Teams)) }

func (f *File) levelsWithin(id string, budget int) int {
	most := 1
	if budget <= 0 {
		return most
	}
	for _, c := range f.Children(id) {
		if c.Closed() {
			continue
		}
		if n := 1 + f.levelsWithin(c.ID, budget-1); n > most {
			most = n
		}
	}
	return most
}

// MoveCheck reports whether the teams ids may go inside parent ("" the top
// level), and when not, why. Among several teams the first that cannot go is
// the answer; a team already under parent is not moved and does not block,
// unless every one of them is (MoveBlockHere).
func (f *File) MoveCheck(ids []string, parent string, d Defaults) (MoveBlock, bool) {
	ids = f.MoveRoots(ids)
	if len(ids) == 0 {
		return MoveBlock{Kind: MoveBlockGone}, false
	}
	target := f.MoveTarget(parent)
	var p Team
	if target != "" {
		var ok bool
		if p, ok = f.Team(target); !ok {
			return MoveBlock{Kind: MoveBlockGone, Team: target}, false
		}
		if p.Closed() {
			return MoveBlock{Kind: MoveBlockClosed, Team: p.ID, Name: p.Name}, false
		}
	}
	limit, from := d.DepthLimit, Origin{Kind: OriginSettings}
	if target != "" {
		e := f.Effective(target, d)
		limit, from = e.DepthLimit, e.DepthFrom
	}
	moving := 0
	for _, id := range ids {
		t, ok := f.Team(id)
		if !ok {
			return MoveBlock{Kind: MoveBlockGone, Team: id}, false
		}
		if t.Root {
			return MoveBlock{Kind: MoveBlockRoot, Team: t.ID, Name: t.Name}, false
		}
		if id == target {
			return MoveBlock{Kind: MoveBlockSelf, Team: t.ID, Name: t.Name}, false
		}
		if target != "" && ParentLoops(f.Teams, id, target) {
			return MoveBlock{Kind: MoveBlockInside, Team: t.ID, Name: t.Name}, false
		}
		if t.Parent == target {
			continue
		}
		moving++
		need := f.levels(id)
		if depth := f.Depth(target); limit > 0 && depth+need > limit {
			return MoveBlock{Kind: MoveBlockDepth, Team: p.ID, Name: p.Name, Depth: depth, Need: need,
				Limit: limit, LimitFrom: from}, false
		}
	}
	if moving == 0 {
		return MoveBlock{Kind: MoveBlockHere, Team: p.ID, Name: p.Name}, false
	}
	return MoveBlock{}, true
}

// Move puts every team in ids inside parent ("" the top level), each team
// carried along inside another moved one left where it is. It checks nothing
// [File.SetParent] does not; ask [File.MoveCheck] first.
func (f *File) Move(ids []string, parent string) error {
	target := f.MoveTarget(parent)
	for _, id := range f.MoveRoots(ids) {
		if t, ok := f.Team(id); ok && t.Parent == target {
			continue
		}
		if err := f.SetParent(id, target); err != nil {
			return err
		}
	}
	return nil
}

// ReportMove is one conversation whose manager a move changes: the manager it
// reports to before and after, each with whether there is one.
type ReportMove struct {
	Key           string
	Before, After Report
	Had, Has      bool
}

// PoolMove is one moved team whose spend counts toward another capped pool:
// Before and After are the teams that own the pool above it ("" none), and
// BeforeCap and AfterCap those pools' daily caps.
type PoolMove struct {
	Team                string
	Before, After       string
	BeforeCap, AfterCap float64
}

// JudgeMove is one moved team whose conflicts another manager decides: Before
// and After are the deciding teams ([File.LCA]), "" for the person.
type JudgeMove struct {
	Team          string
	Before, After string
}

// MoveEffect is what a move changes.
type MoveEffect struct {
	Reports []ReportMove
	Pools   []PoolMove
	Judges  []JudgeMove
}

// Changes reports whether the move changes anything a person is asked about.
func (e MoveEffect) Changes() bool {
	return len(e.Reports) > 0 || len(e.Pools) > 0 || len(e.Judges) > 0
}

// MoveEffects is what moving ids inside parent would change, computed on
// copies of the file tidied as the store's write tidies them. The file is not
// changed. An error is a move [File.SetParent] refuses.
func (f *File) MoveEffects(ids []string, parent string, d Defaults) (MoveEffect, error) {
	before := f.tidyCopy()
	after := f.tidyCopy()
	if err := after.Move(ids, parent); err != nil {
		return MoveEffect{}, err
	}
	tidy(after.Teams)
	var out MoveEffect
	for _, id := range before.MoveRoots(ids) {
		t, ok := before.Team(id)
		if !ok {
			continue
		}
		// Every conversation carried: the team's and every open team's under it.
		keys := before.subtreeKeys(id)
		for _, k := range keys {
			was, had := before.Home(k)
			now, has := after.Home(k)
			if had != has || was.Manager != now.Manager || was.Team != now.Team {
				out.Reports = append(out.Reports, ReportMove{Key: k, Before: was, After: now, Had: had, Has: has})
			}
		}
		was, wasCap := before.poolAbove(t.Parent, d)
		nt, _ := after.Team(id)
		now, nowCap := after.poolAbove(nt.Parent, d)
		if was != now {
			out.Pools = append(out.Pools, PoolMove{Team: id, Before: was, After: now, BeforeCap: wasCap, AfterCap: nowCap})
		}
		if len(keys) > 0 {
			jb, okb := before.LCA(keys...)
			ja, oka := after.LCA(keys...)
			if okb != oka || jb.ID != ja.ID {
				out.Judges = append(out.Judges, JudgeMove{Team: id, Before: jb.ID, After: ja.ID})
			}
		}
	}
	return out, nil
}

// tidyCopy is a copy of f that shares nothing with it, tidied.
func (f *File) tidyCopy() *File {
	g := &File{Version: f.Version, Teams: make([]Team, len(f.Teams))}
	for i, t := range f.Teams {
		g.Teams[i] = t.Clone()
	}
	tidy(g.Teams)
	return g
}

// subtreeKeys is every member key of team id and of the open teams under it,
// each once, in file order.
func (f *File) subtreeKeys(id string) []string {
	in := map[string]bool{id: true}
	for _, t := range f.Descendants(id) {
		if !t.Closed() {
			in[t.ID] = true
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, t := range f.Teams {
		if !in[t.ID] {
			continue
		}
		for _, m := range t.Members {
			if !seen[m.Key] {
				seen[m.Key] = true
				out = append(out, m.Key)
			}
		}
	}
	return out
}

// poolAbove is the team owning the capped pool a team directly under parent
// counts toward, and that pool's cap: "" and 0 for a team at the top or under
// no cap. An inherited cap is its owner's one pool; the profile's default cap
// is measured over the whole chain, so its pool is the top of it.
func (f *File) poolAbove(parent string, d Defaults) (string, float64) {
	if parent == "" {
		return "", 0
	}
	e := f.Effective(parent, d)
	if e.CapUSDDay <= 0 {
		return "", 0
	}
	switch e.CapFrom.Kind {
	case OriginTeam, OriginAncestor:
		return e.CapFrom.Team, e.CapUSDDay
	case OriginSettings:
		top := parent
		for _, a := range f.Ancestors(parent) {
			if !a.Closed() {
				top = a.ID
			}
		}
		return top, e.CapUSDDay
	}
	return "", 0
}

// MoveNotice is one Traffic line a committed move writes: Team is whose log,
// Entry the line. The store appends it; a frame never does.
type MoveNotice struct {
	Team  string
	Entry Entry
}

// MoveNotices is the Traffic written when ids have moved, read off the file
// before the move and the file after it. A team whose parent did not change
// contributes nothing, so a refused move (the file left as it was) writes
// nothing, and asking twice about the same pair does not invent a second move.
//
// EACH AFFECTED TEAM GETS ONE [KindEvent] PER MEMBER THAT MOVED WITH THE TEAM.
// The team that was left, and the moved team, say `@handle moved to harbor`.
// The team that was joined says `@handle joined from ops`. A manager reads
// those on its next turn through the ordinary Traffic read. The lines are
// from codeaf to everyone and carry no member state, so they do not start a
// wake of their own. A member with no handle is named by the team's name,
// once, so a team of unnamed conversations is still told.
func MoveNotices(before, after *File, ids []string) []MoveNotice {
	if before == nil || after == nil {
		return nil
	}
	var out []MoveNotice
	for _, id := range before.MoveRoots(ids) {
		was, ok := before.Team(id)
		if !ok {
			continue
		}
		now, ok := after.Team(id)
		if !ok || now.Parent == was.Parent {
			continue
		}
		to := placeName(after, now.Parent)
		from := placeName(before, was.Parent)
		for _, h := range moveHandles(was) {
			moved := "@" + h + " moved to " + to
			joined := "@" + h + " joined from " + from
			// The moved team, and the team it left, both lost the line it had.
			out = append(out, moveEvent(id, moved))
			if was.Parent != "" {
				out = append(out, moveEvent(was.Parent, moved))
			}
			// The team it joined gained it.
			if now.Parent != "" {
				out = append(out, moveEvent(now.Parent, joined))
			}
		}
	}
	return out
}

// moveHandles is who a move names: each member's handle, or the team's name
// once when nobody has one.
func moveHandles(t Team) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range t.Members {
		h := strings.TrimPrefix(strings.TrimSpace(m.Handle), "@")
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	if len(out) == 0 && strings.TrimSpace(t.Name) != "" {
		out = append(out, t.Name)
	}
	return out
}

// placeName is team id's name, or "the top" when id is the top level.
func placeName(f *File, id string) string {
	if id == "" {
		return "the top"
	}
	if t, ok := f.Team(id); ok && t.Name != "" {
		return t.Name
	}
	return id
}

// moveEvent is one move line, from codeaf to everyone, with no member state
// so the wake watch does not start a turn for it.
func moveEvent(team, text string) MoveNotice {
	return MoveNotice{Team: team, Entry: Entry{
		Kind: KindEvent, From: FromSystem, To: ToEveryone, Text: text,
	}}
}

// MemberMoveNotices is the Traffic for conversations that left one team and
// joined another between before and after. A team move that only changes a
// parent is not one of these (that is [MoveNotices]). A file that did not
// change membership writes nothing, so a refused transfer writes nothing.
// Each team left says `@handle moved to harbor`. Each team joined says
// `@handle joined from ops`.
func MemberMoveNotices(before, after *File) []MoveNotice {
	if before == nil || after == nil {
		return nil
	}
	type seat struct {
		team Team
		mem  Member
	}
	was := map[string][]seat{}
	for _, t := range before.Teams {
		for _, m := range t.Members {
			if m.Key == "" {
				continue
			}
			was[m.Key] = append(was[m.Key], seat{t, m})
		}
	}
	now := map[string][]seat{}
	for _, t := range after.Teams {
		for _, m := range t.Members {
			if m.Key == "" {
				continue
			}
			now[m.Key] = append(now[m.Key], seat{t, m})
		}
	}
	var out []MoveNotice
	seen := map[string]bool{}
	for key, froms := range was {
		tos := now[key]
		for _, from := range froms {
			still := false
			for _, to := range tos {
				if to.team.ID == from.team.ID {
					still = true
					break
				}
			}
			if still {
				continue
			}
			h := moveHandle(from.mem)
			if h == "" {
				continue
			}
			for _, to := range tos {
				if to.team.ID == from.team.ID {
					continue
				}
				held := false
				for _, back := range froms {
					if back.team.ID == to.team.ID {
						held = true
						break
					}
				}
				if held {
					continue
				}
				mark := from.team.ID + "->" + to.team.ID + ":" + h
				if seen[mark] {
					continue
				}
				seen[mark] = true
				out = append(out, moveEvent(from.team.ID, "@"+h+" moved to "+placeName(after, to.team.ID)))
				out = append(out, moveEvent(to.team.ID, "@"+h+" joined from "+placeName(before, from.team.ID)))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Team != out[j].Team {
			return out[i].Team < out[j].Team
		}
		return out[i].Entry.Text < out[j].Entry.Text
	})
	return out
}

// moveHandle is the handle a move line uses, or "" when the member has none.
func moveHandle(m Member) string {
	return strings.TrimPrefix(strings.TrimSpace(m.Handle), "@")
}

// WriteMoveNotices appends each notice to that team's Traffic, once per
// notice. A refused move hands none, and this writes none.
func WriteMoveNotices(profileDir string, notes []MoveNotice) error {
	var err error
	for _, n := range notes {
		if n.Team == "" || strings.TrimSpace(n.Entry.Text) == "" {
			continue
		}
		if e := AppendTraffic(profileDir, n.Team, n.Entry); e != nil && err == nil {
			err = e
		}
	}
	return err
}

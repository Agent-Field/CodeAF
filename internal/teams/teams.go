package teams

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"
)

// Version is the file format this build writes.
const Version = 2

// Member is one conversation a team holds, with enough to reopen it when no
// window has it open.
type Member struct {
	Key   string `json:"key"`
	File  string `json:"file"`
	Where string `json:"where"`
	Word  string `json:"word"`
	// Handle is the member's short name inside this team (handle.go). It is
	// empty only while the member has no title to derive one from.
	Handle string `json:"handle,omitempty"`
	// Home marks the one membership, among all of this conversation's, that
	// names the manager it reports to (home.go): the nearest manager up this
	// team's chain. At most one membership of a key carries it, and only one
	// with a manager somewhere up its chain. [File.SetHome] moves it; tidy
	// picks it when there is none and never moves a valid one.
	Home bool `json:"home,omitempty"`
	// Started says this membership was made by the team manager's team_start
	// (a [KindStart] the interface carried out), which is the second rule a
	// home is picked by.
	Started bool `json:"started,omitempty"`
}

// Team is one named set of conversations.
type Team struct {
	// ID is random and minted once ([NewID]).
	ID   string
	Name string
	// Parent is the id of the team this one sits under, "" at the top level.
	Parent string
	// Members are the conversations, in the order the person stored them.
	Members []Member
	// Manager is the conversation key of the member that manages the team, ""
	// for none. It is always one of Members.
	Manager string
	// Hue and Tier are the team's colour (hue.go).
	Hue  float64
	Tier int
	Made time.Time
	// State is [TeamOpen] or [TeamClosed] (lifecycle.go); the empty string a
	// file from before the lifecycle wrote reads as open. ClosedAt is when it
	// closed, ClosedWith the id of the team whose close closed it (itself, or
	// the ancestor a cascade came from), and Report the id of its closing
	// report packet, "" for a team closed without one.
	State      string
	ClosedAt   time.Time
	ClosedWith string
	Report     string
	// Root marks the one team that holds every other (root.go): the `All
	// teams` row, made when the person gives it a manager.
	Root bool
	// Settings are the team's own delegation overrides (teamsettings.go),
	// each unset field inheriting from the parent chain and then the
	// profile's `teams.` defaults. They are stored flat on the team.
	Settings Settings

	// hued says the team has a colour: the file gave it one or [Team.SetHue]
	// did. A hue of 0 is a real hue, so absence is kept apart from the value.
	// extra is every field a later build wrote that this one does not know.
	hued  bool
	extra map[string]json.RawMessage
}

// File is the whole of teams.json.
type File struct {
	Version int
	Teams   []Team
}

// knownFields is every key [Team] reads itself.
var knownFields = map[string]bool{
	"id": true, "name": true, "parent": true, "members": true, "manager": true,
	"hue": true, "tier": true, "made": true,
	"state": true, "closed_at": true, "closed_with": true, "report": true, "root": true,
	"questions_up": true, "cap_usd_day": true, "depth_limit": true, "sub_share": true, "wake": true,
}

// wireTeam is the stored shape. Hue and Tier are pointers so a team with no
// colour is written without one, and the next reader with a palette colours
// it rather than reading 0 as a choice.
type wireTeam struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Parent  string    `json:"parent"`
	Members []Member  `json:"members"`
	Manager string    `json:"manager"`
	Hue     *float64  `json:"hue,omitempty"`
	Tier    *int      `json:"tier,omitempty"`
	Made    time.Time `json:"made"`
	// The lifecycle, written only for a closed team.
	State      string     `json:"state,omitempty"`
	ClosedAt   *time.Time `json:"closed_at,omitempty"`
	ClosedWith string     `json:"closed_with,omitempty"`
	Report     string     `json:"report,omitempty"`
	Root       bool       `json:"root,omitempty"`
	// The overrides are written flat beside the fields above, each only when
	// set, so a team with none is written exactly as before.
	Settings
}

// UnmarshalJSON reads a team, keeping every field it does not know.
func (t *Team) UnmarshalJSON(raw []byte) error {
	var w wireTeam
	if err := json.Unmarshal(raw, &w); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return err
	}
	*t = Team{ID: w.ID, Name: w.Name, Parent: w.Parent, Members: w.Members, Manager: w.Manager, Made: w.Made,
		Settings: w.Settings, State: w.State, ClosedWith: w.ClosedWith, Report: w.Report, Root: w.Root}
	if w.ClosedAt != nil {
		t.ClosedAt = *w.ClosedAt
	}
	if w.Hue != nil {
		t.Hue, t.hued = *w.Hue, true
	}
	if w.Tier != nil {
		t.Tier = *w.Tier
	}
	for k, v := range all {
		if knownFields[k] {
			continue
		}
		if t.extra == nil {
			t.extra = map[string]json.RawMessage{}
		}
		t.extra[k] = v
	}
	return nil
}

// MarshalJSON writes the known fields in their order, then any field a later
// build wrote, sorted, exactly as it was read.
func (t Team) MarshalJSON() ([]byte, error) {
	w := wireTeam{ID: t.ID, Name: t.Name, Parent: t.Parent, Members: t.Members, Manager: t.Manager, Made: t.Made,
		Settings: t.Settings, ClosedWith: t.ClosedWith, Report: t.Report, Root: t.Root}
	// A state this build does not know is written back as it was read, so a
	// later build's word survives; open is written as nothing.
	if t.State != "" && t.State != TeamOpen {
		w.State = t.State
	}
	if !t.ClosedAt.IsZero() {
		at := t.ClosedAt
		w.ClosedAt = &at
	}
	if t.Hued() {
		hue, tier := t.Hue, t.Tier
		w.Hue, w.Tier = &hue, &tier
	}
	raw, err := json.Marshal(w)
	if err != nil || len(t.extra) == 0 {
		return raw, err
	}
	keys := make([]string, 0, len(t.extra))
	for k := range t.extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b bytes.Buffer
	b.Write(raw[:len(raw)-1])
	for _, k := range keys {
		name, _ := json.Marshal(k)
		b.WriteByte(',')
		b.Write(name)
		b.WriteByte(':')
		b.Write(t.extra[k])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Wakes reports whether the team's OWN setting leaves waking on: true unless
// the team itself says "wake": false. It does not walk the chain; whether team
// traffic really wakes a conversation is [Effective].Wake, which inherits.
func (t Team) Wakes() bool { return t.Settings.Wake == nil || *t.Settings.Wake }

// Hued reports whether the team has a colour. A team built in code with a
// non-zero hue or tier counts as coloured.
func (t Team) Hued() bool { return t.hued || t.Hue != 0 || t.Tier != 0 }

// HueSpec is the team's colour.
func (t Team) HueSpec() HueSpec { return HueSpec{Hue: t.Hue, Tier: t.Tier} }

// SetHue gives the team a colour.
func (t *Team) SetHue(h HueSpec) { t.Hue, t.Tier, t.hued = h.Hue, h.Tier, true }

// Holds reports whether key is one of the team's members.
func (t Team) Holds(key string) bool { return t.member(key) >= 0 }

// Member is the member with key.
func (t Team) Member(key string) (Member, bool) {
	if i := t.member(key); i >= 0 {
		return t.Members[i], true
	}
	return Member{}, false
}

// ByHandle is the member with handle.
func (t Team) ByHandle(handle string) (Member, bool) {
	for _, m := range t.Members {
		if handle != "" && m.Handle == handle {
			return m, true
		}
	}
	return Member{}, false
}

func (t Team) member(key string) int {
	if key == "" {
		return -1
	}
	for i, m := range t.Members {
		if m.Key == key {
			return i
		}
	}
	return -1
}

// Clone is a copy of t that shares nothing with it.
func (t Team) Clone() Team {
	t.Members = append([]Member(nil), t.Members...)
	t.Settings = t.Settings.clone()
	if t.extra != nil {
		extra := make(map[string]json.RawMessage, len(t.extra))
		for k, v := range t.extra {
			extra[k] = v
		}
		t.extra = extra
	}
	return t
}

// NewID is a fresh random team id: twelve hex digits, never derived from the
// name, so a rename is a rename and two teams once called the same are two.
func NewID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano()&0xffffffffffff, 16)
	}
	return hex.EncodeToString(b[:])
}

// ── THE TREE ────────────────────────────────────────────────────────────────

// Index is where the team with id sits in teams, -1 when it is not there.
func Index(teams []Team, id string) int {
	if id == "" {
		return -1
	}
	for i, t := range teams {
		if t.ID == id {
			return i
		}
	}
	return -1
}

// ParentLoops reports whether making parent the parent of id would close a
// loop: whether id is parent itself or one of parent's ancestors. The walk is
// bounded by the list, so a loop already in the list cannot hang it.
func ParentLoops(teams []Team, id, parent string) bool {
	for at, steps := parent, 0; at != "" && steps <= len(teams); steps++ {
		if at == id {
			return true
		}
		i := Index(teams, at)
		if i < 0 {
			return false
		}
		at = teams[i].Parent
	}
	return false
}

// Team is the team with id.
func (f *File) Team(id string) (Team, bool) {
	if i := Index(f.Teams, id); i >= 0 {
		return f.Teams[i], true
	}
	return Team{}, false
}

// Children is every team whose parent is id, in stored order; "" is the top
// level.
func (f *File) Children(id string) []Team {
	var out []Team
	for _, t := range f.Teams {
		if t.Parent == id {
			out = append(out, t)
		}
	}
	return out
}

// Ancestors is id's parent, its parent's parent, and so on to the top,
// nearest first.
func (f *File) Ancestors(id string) []Team {
	var out []Team
	t, ok := f.Team(id)
	for ok && t.Parent != "" && len(out) < len(f.Teams) {
		t, ok = f.Team(t.Parent)
		if ok {
			out = append(out, t)
		}
	}
	return out
}

// SetParent puts team id under parent, or at the top level for "". The parent
// must exist and may not be the team or anything under it.
func (f *File) SetParent(id, parent string) error {
	i, err := f.at(id)
	if err != nil {
		return err
	}
	if f.Teams[i].Root && parent != "" {
		return ErrRoot
	}
	if parent != "" {
		if Index(f.Teams, parent) < 0 {
			return fmt.Errorf("no team %s", parent)
		}
		if ParentLoops(f.Teams, id, parent) {
			return errors.New("a team cannot sit under itself")
		}
	}
	f.Teams[i].Parent = parent
	return nil
}

func (f *File) at(id string) (int, error) {
	if i := Index(f.Teams, id); i >= 0 {
		return i, nil
	}
	return -1, fmt.Errorf("no team %s", id)
}

// ── MEMBERS AND THE MANAGER ─────────────────────────────────────────────────

// AddMember puts m into team id after the members it has, and gives it a
// handle if it has a title. A key already there is left as it is.
func (f *File) AddMember(id string, m Member) error {
	i, err := f.at(id)
	if err != nil {
		return err
	}
	if m.Key == "" {
		return errors.New("a member needs a conversation key")
	}
	t := &f.Teams[i]
	if t.Holds(m.Key) {
		return nil
	}
	if m.Handle != "" && handleProblem(*t, m.Key, m.Handle) != nil {
		m.Handle = ""
	}
	t.Members = append(t.Members, m)
	assignHandles(t)
	return nil
}

// RemoveMember takes the conversation with key out of team id. The team is
// kept even when it is left empty, and a manager removed is no longer one.
func (f *File) RemoveMember(id, key string) error {
	i, err := f.at(id)
	if err != nil {
		return err
	}
	t := &f.Teams[i]
	if j := t.member(key); j >= 0 {
		t.Members = append(t.Members[:j:j], t.Members[j+1:]...)
	}
	if t.Manager == key {
		t.Manager = ""
	}
	return nil
}

// SetManager makes the conversation with key team id's manager. A team has
// one manager, so this replaces any other. A conversation that is not a member
// is added first, with only its key; call [File.AddMember] before this to add
// it with its title and file.
func (f *File) SetManager(id, key string) error {
	if key == "" {
		return errors.New("a manager needs a conversation key")
	}
	if err := f.AddMember(id, Member{Key: key}); err != nil {
		return err
	}
	f.Teams[Index(f.Teams, id)].Manager = key
	return nil
}

// ClearManager leaves team id without a manager. The conversation stays a
// member.
func (f *File) ClearManager(id string) error {
	i, err := f.at(id)
	if err != nil {
		return err
	}
	f.Teams[i].Manager = ""
	return nil
}

// SetHandle gives the member with key in team id the handle h. It must be a
// valid handle ([ValidHandle]) that no other member of the team has.
func (f *File) SetHandle(id, key, h string) error {
	i, err := f.at(id)
	if err != nil {
		return err
	}
	t := &f.Teams[i]
	j := t.member(key)
	if j < 0 {
		return fmt.Errorf("%s is not in team %s", key, t.Name)
	}
	if err := handleProblem(*t, key, h); err != nil {
		return err
	}
	t.Members[j].Handle = h
	return nil
}

// ── REPAIRS ─────────────────────────────────────────────────────────────────

// tidy puts a list in order, in place, and reports whether it changed
// anything: an id for every team (and a new one for an id used twice), a
// parent that exists and does not lead back, handles for members with titles
// and no valid unique handle, a manager that is a member, overrides inside
// their bands, and one home for every conversation that has a manager to
// report to (home.go). Colour is not its business ([File.Colour] is).
func tidy(teams []Team) bool {
	changed := false
	seen := map[string]bool{}
	for i := range teams {
		if teams[i].ID == "" || seen[teams[i].ID] {
			teams[i].ID = NewID()
			changed = true
		}
		seen[teams[i].ID] = true
	}
	for i := range teams {
		if p := teams[i].Parent; p != "" && (!seen[p] || p == teams[i].ID) {
			teams[i].Parent = ""
			changed = true
		}
	}
	// A loop in the file is cut where it is first met.
	for i := range teams {
		if teams[i].Parent != "" && ParentLoops(teams, teams[i].ID, teams[i].Parent) {
			teams[i].Parent = ""
			changed = true
		}
	}
	for i := range teams {
		if assignHandles(&teams[i]) {
			changed = true
		}
		if m := teams[i].Manager; m != "" && !teams[i].Holds(m) {
			teams[i].Manager = ""
			changed = true
		}
		if teams[i].Settings.tidy() {
			changed = true
		}
	}
	// The root holds every other top-level team (root.go).
	if tidyRoot(teams) {
		changed = true
	}
	// Homes last: they depend on the managers and parents settled above.
	if assignHomes(teams) {
		changed = true
	}
	return changed
}

// Colour gives every team without a colour one from the generator, in file
// order, around the ones that have theirs, and reports whether it gave any.
// It depends on nothing but the list and reserved, so the same file is
// coloured the same way on every load.
func (f *File) Colour(reserved []float64) bool {
	var used []HueSpec
	for _, t := range f.Teams {
		if t.Hued() {
			used = append(used, t.HueSpec())
		}
	}
	changed := false
	for i := range f.Teams {
		if f.Teams[i].Hued() {
			continue
		}
		next := NextHue(used, reserved)
		next.Tier = TierFor(i)
		f.Teams[i].SetHue(next)
		used = append(used, next)
		changed = true
	}
	return changed
}

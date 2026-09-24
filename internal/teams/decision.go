package teams

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// ── DECISION PACKETS ────────────────────────────────────────────────────────
//
// Every decision that has to be made above the conversation that met it (a
// conflict between parties, a question a manager escalates, a judgement call,
// a cap reached, a closing report) travels as a SELF-CONTAINED PACKET (ruling
// c-7): the question, the parties and what each of them said, the options each
// with what happens if it is chosen, and a recommendation with its reason. The
// same packet is answered by a manager or by the person, so either can decide
// it without reading a transcript.
//
// WHERE IT LIVES. A packet is written in the file of the team it was raised
// from (Origin), <profile>/teams/<origin>/decisions.jsonl, and never moves:
// escalating it changes who decides (Team) and adds a hop to its trail, and
// the line that says so is appended to the same file. So a packet has one home
// and one id for its life, and a sub-team's closed view can list everything
// its members raised.
//
// THE FILE IS EVENTS, FOLDED BY ID. One JSON object per line, only ever
// appended to: a `raise` carrying the whole packet, then `decide` or
// `escalate` lines naming it by id. Reading folds them in file order into the
// packet as it stands now. Nothing is rewritten, so a reader never sees a
// half-written packet and a writer never loses another's line.
//
// EVERY WRITE IS UNDER THE FILE'S LOCK and re-reads the fold under it, so two
// deciders cannot both decide one packet: the second is [ErrDecided].
//
// READS ARE STAT-FIRST AND INCREMENTAL, for traffic.go's reason: the teams
// page asks about open packets on its clock. A file whose stamp has not moved
// is answered from memory, and one that grew is read from the byte where the
// last read stopped, never from the top. The scan across teams is one
// directory listing and a stat per team.
//
// THE FILE ROTATES LIKE TRAFFIC, and keeps every packet still waiting. Past
// [decisionsRotateBytes] the file is renamed to decisions.1.jsonl (replacing
// the one before) and the new file opens with one `carry` line per packet
// still waiting, the packet whole as it stands (its trail, its escalated
// state). A reader folds the rotated file and then the current one, and a
// carry replaces what the rotated file said of that id, so a waiting packet
// is never lost to a rotation and a decided one stays readable for one
// rotation more, which is long enough for its raiser to be handed the answer.
//
// EVERY CHANGE IS ALSO A LINE OF TRAFFIC, a [KindPacket] entry in the log of
// the team that raised it and of every team that was asked to decide it, so
// the interface tailing a team's Traffic learns of a packet without polling
// this file, and a session delivering Traffic hands it to the manager.

// Packet kinds.
const (
	PacketQuestion  = "question"  // a question nobody below could answer
	PacketConflict  = "conflict"  // parties disagree; raised by team_raise
	PacketCap       = "cap"       // a team reached its daily cap
	PacketJudgement = "judgement" // a call a manager will not make alone
	PacketClosing   = "closing"   // a manager's closing report before a close
)

var packetKinds = map[string]bool{
	PacketQuestion: true, PacketConflict: true, PacketCap: true, PacketJudgement: true, PacketClosing: true,
}

// Packet states. Escalated is still waiting: it is waiting at Team, which is
// no longer where it was raised.
const (
	PacketOpen      = "open"
	PacketDecided   = "decided"
	PacketEscalated = "escalated"
)

// Person is the decider that is not a team: the person. It is the Team of a
// packet waiting on them, the by of a decision they made, and a scope.
const Person = "you"

// ScopeAll is every packet still waiting, whoever decides it.
const ScopeAll = ""

// The option ids of a closing packet and a cap packet, which the interface
// and the session both act on.
const (
	OptionClose     = "close"      // close the team
	OptionCloseNow  = "close-now"  // close although the wrap-up did not finish
	OptionKeepGoing = "keep-going" // do not close
	OptionRaiseCap  = "raise"      // raise the cap (the option says to what)
	OptionStopToday = "stop"       // stop the team until tomorrow
)

// Party is one side of a packet: a conversation, its handle and team, and the
// context it gave in its own words.
type Party struct {
	Key     string `json:"key"`
	Handle  string `json:"handle,omitempty"`
	Team    string `json:"team,omitempty"`
	Context string `json:"context,omitempty"`
}

// Option is one answer and what happens if it is chosen.
type Option struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Consequence string `json:"consequence"`
}

// Recommendation is the option the raiser would choose, and why.
type Recommendation struct {
	Option string `json:"option"`
	Reason string `json:"reason"`
}

// Hop is one escalation: from the decider it left to the one it went to.
type Hop struct {
	From   string    `json:"from"`
	To     string    `json:"to"`
	By     string    `json:"by"`
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at"`
}

// CapFacts is what a [PacketCap] packet says about the pool: whose cap it is
// (Team, the pool's owner), the local day, the cap reached, what the pool had
// spent when it was raised, and the figure the `raise` option raises it to
// for the rest of that day. The session reads RaiseTo back when the person
// picks `raise`, so the amount on the button is the amount that holds.
type CapFacts struct {
	Team     string  `json:"team"`
	Day      string  `json:"day"`
	CapUSD   float64 `json:"cap_usd"`
	SpentUSD float64 `json:"spent_usd"`
	RaiseTo  float64 `json:"raise_to"`
}

// ClosingReport is what a [PacketClosing] packet says about the team: what
// was done, what is left, where the files are and what it spent. Incomplete
// says the wrap-up ran out of time or money before it finished.
type ClosingReport struct {
	Done       string   `json:"done"`
	Left       string   `json:"left,omitempty"`
	Files      []string `json:"files,omitempty"`
	SpendUSD   float64  `json:"spend_usd,omitempty"`
	Incomplete bool     `json:"incomplete,omitempty"`
}

// Packet is one decision, as it stands.
type Packet struct {
	// ID is minted by [Raise].
	ID string `json:"id"`
	// Team is who decides it now: a team id, whose manager decides, or
	// [Person].
	Team string `json:"team"`
	// Origin is the team it was raised from, whose file holds it.
	Origin string `json:"origin"`
	// Kind is one of the Packet kinds.
	Kind string `json:"kind"`
	// RaisedBy is the raiser's handle, or [FromManager] or [Person].
	RaisedBy string `json:"raised_by"`
	// Parties are the sides, each with its own context. A question or a
	// judgement may have one.
	Parties  []Party  `json:"parties,omitempty"`
	Question string   `json:"question"`
	Options  []Option `json:"options,omitempty"`
	// Recommendation is optional; when there is one it names an option.
	Recommendation *Recommendation `json:"recommendation,omitempty"`
	// Report is a closing packet's report, and nil on every other kind.
	Report *ClosingReport `json:"report,omitempty"`
	// Cap is a cap packet's facts, and nil on every other kind.
	Cap *CapFacts `json:"cap,omitempty"`
	// State is one of the Packet states.
	State string `json:"state"`
	// DecidedBy is the deciding manager's handle, or [Person].
	DecidedBy string `json:"decided_by,omitempty"`
	// Decision is the chosen option's id, or the person's own words when none
	// fitted.
	Decision string `json:"decision,omitempty"`
	// Reason is the decider's reason, or the last escalation's.
	Reason string `json:"reason,omitempty"`
	// Trail is every escalation, oldest first.
	Trail []Hop `json:"trail,omitempty"`
	// Raised is when it was raised, At when it last changed.
	Raised time.Time `json:"raised"`
	At     time.Time `json:"at"`
}

// Waiting reports whether the packet still needs a decision.
func (p Packet) Waiting() bool { return p.State == PacketOpen || p.State == PacketEscalated }

// Option is the option with id.
func (p Packet) Option(id string) (Option, bool) {
	for _, o := range p.Options {
		if o.ID == id {
			return o, true
		}
	}
	return Option{}, false
}

// Errors a packet write can answer.
var (
	ErrDecided    = errors.New("teams: that packet was already decided")
	ErrNoPacket   = errors.New("teams: no such packet")
	ErrNotDecider = errors.New("teams: only the manager it waits on, or you, can decide or escalate it")
	ErrSideways   = errors.New("teams: a packet goes up the tree or to you, never sideways or down")
)

// decisionEvent is one line of a packet file.
type decisionEvent struct {
	Op       string    `json:"op"`
	At       time.Time `json:"at"`
	Packet   *Packet   `json:"packet,omitempty"`
	ID       string    `json:"id,omitempty"`
	By       string    `json:"by,omitempty"`
	Decision string    `json:"decision,omitempty"`
	To       string    `json:"to,omitempty"`
	Reason   string    `json:"reason,omitempty"`
}

const (
	opRaise    = "raise"
	opDecide   = "decide"
	opEscalate = "escalate"
	// opCarry is a waiting packet written again, whole, at the head of a
	// rotated file. It replaces what an older line said of that id.
	opCarry = "carry"
)

// decisionsRotateBytes is the size past which a packet file starts a new one.
// A packet is a few hundred bytes to a few kilobytes, so a megabyte is
// hundreds of decisions; a var so a test can rotate in a few lines.
var decisionsRotateBytes int64 = 1 << 20

func decisionsRotated(path string) string {
	return strings.TrimSuffix(path, ".jsonl") + ".1.jsonl"
}

// DecisionsPath is team teamID's packet file.
func DecisionsPath(profileDir, teamID string) string {
	return config.ProfilePath(profileDir, filepath.Join("teams", teamID, "decisions.jsonl"))
}

// ── WRITING ─────────────────────────────────────────────────────────────────

// Raise records p as a new packet and answers it as written: with an id, open,
// raised now. p.Team is who decides (a team id or [Person]; the caller finds
// it, with [File.LCA] for parties or [File.Home] for a question), and
// p.Origin the team it is raised from, which must be p.Team or a team under
// it; an empty Origin is p.Team. An option with no id is given its place
// ("1", "2", ...). The kind, the question, the raiser, and a label and a
// consequence on every option are required; a question may have no options
// (the answer is words), every other kind must have one.
func Raise(profileDir string, p Packet) (Packet, error) {
	if !packetKinds[p.Kind] {
		return Packet{}, fmt.Errorf("teams: %q is not a packet kind", p.Kind)
	}
	if strings.TrimSpace(p.Question) == "" || strings.TrimSpace(p.RaisedBy) == "" {
		return Packet{}, errors.New("teams: a packet needs a question and who raised it")
	}
	if len(p.Options) == 0 && p.Kind != PacketQuestion {
		return Packet{}, errors.New("teams: a packet needs its options, each with what happens")
	}
	p.Options = append([]Option(nil), p.Options...)
	seen := map[string]bool{}
	for i := range p.Options {
		o := &p.Options[i]
		if o.ID == "" {
			o.ID = strconv.Itoa(i + 1)
		}
		if seen[o.ID] || strings.TrimSpace(o.Label) == "" || strings.TrimSpace(o.Consequence) == "" {
			return Packet{}, errors.New("teams: every option needs its own id, a label and what happens if it is chosen")
		}
		seen[o.ID] = true
	}
	if r := p.Recommendation; r != nil && (!seen[r.Option] || strings.TrimSpace(r.Reason) == "") {
		return Packet{}, errors.New("teams: a recommendation names one of the options and says why")
	}
	if p.Origin == "" {
		p.Origin = p.Team
	}
	if err := safeTeamID(p.Origin); err != nil || p.Origin == Person {
		return Packet{}, errors.New("teams: a packet is raised from a team")
	}
	f, err := Load(profileDir)
	if err != nil {
		return Packet{}, err
	}
	if _, ok := f.Team(p.Origin); !ok {
		return Packet{}, fmt.Errorf("no team %s", p.Origin)
	}
	if p.Team != Person {
		decider, ok := f.Team(p.Team)
		if !ok || decider.Closed() {
			return Packet{}, fmt.Errorf("teams: %q is not an open team to decide it", p.Team)
		}
		if p.Team != p.Origin && !isAncestor(f, p.Team, p.Origin) {
			return Packet{}, ErrSideways
		}
	}
	now := time.Now()
	p.ID, p.State, p.Raised, p.At = "p"+NewID(), PacketOpen, now, now
	p.DecidedBy, p.Decision, p.Reason, p.Trail = "", "", "", nil
	if err := appendDecision(profileDir, p.Origin, &decisionEvent{Op: opRaise, At: now, Packet: &p}, nil); err != nil {
		return Packet{}, err
	}
	logPacket(profileDir, f, p, []string{p.Origin, p.Team},
		fmt.Sprintf("%s raised a %s: %s", p.RaisedBy, p.Kind, p.Question))
	return p, nil
}

// Decide records by's decision on packet id: an option's id, or the person's
// own words. by is the handle of the manager of the team the packet waits on,
// or [Person], who may decide any packet. A packet already decided is
// [ErrDecided], and nothing is written.
func Decide(profileDir, id, by, decision, reason string) (Packet, error) {
	if strings.TrimSpace(decision) == "" {
		return Packet{}, errors.New("teams: a decision needs an answer")
	}
	var out Packet
	f, err := Load(profileDir)
	if err != nil {
		return Packet{}, err
	}
	origin, err := packetOrigin(profileDir, id)
	if err != nil {
		return Packet{}, err
	}
	now := time.Now()
	e := decisionEvent{Op: opDecide, At: now, ID: id, Decision: decision, Reason: reason}
	err = appendDecision(profileDir, origin, &e, func(p Packet) error {
		if !p.Waiting() {
			return ErrDecided
		}
		who, ok := mayDecide(f, p, by)
		if !ok {
			return ErrNotDecider
		}
		out, e.By = p, who
		return nil
	})
	if err != nil {
		return Packet{}, err
	}
	out = foldDecide(out, e)
	word := decision
	if o, ok := out.Option(decision); ok {
		word = o.Label
	}
	// A QUESTION'S ANSWER READS AS ONE on the rail: `answered @web: JSON`,
	// which the interface draws after the decider's mark.
	said := fmt.Sprintf("%s decided: %s", e.By, word)
	if out.Kind == PacketQuestion {
		said = fmt.Sprintf("answered %s: %s", raiserWord(out.RaisedBy), word)
	}
	logPacket(profileDir, f, out, []string{out.Origin, out.Team}, said)
	return out, nil
}

// Escalate sends packet id up: to an open team above the one it waits on, or
// to [Person]. by is as for [Decide]. Down, sideways or to a closed team is
// [ErrSideways]; a decided packet is [ErrDecided].
func Escalate(profileDir, id, by, to, reason string) (Packet, error) {
	var out Packet
	f, err := Load(profileDir)
	if err != nil {
		return Packet{}, err
	}
	origin, err := packetOrigin(profileDir, id)
	if err != nil {
		return Packet{}, err
	}
	now := time.Now()
	from := ""
	e := decisionEvent{Op: opEscalate, At: now, ID: id, To: to, Reason: reason}
	err = appendDecision(profileDir, origin, &e, func(p Packet) error {
		if !p.Waiting() {
			return ErrDecided
		}
		who, ok := mayDecide(f, p, by)
		if !ok {
			return ErrNotDecider
		}
		e.By = who
		if p.Team == Person {
			return ErrSideways
		}
		if to != Person {
			t, ok := f.Team(to)
			if !ok || t.Closed() || !isAncestor(f, to, p.Team) {
				return ErrSideways
			}
		}
		out, from = p, p.Team
		return nil
	})
	if err != nil {
		return Packet{}, err
	}
	out = foldEscalate(out, e)
	where := "you"
	if t, ok := f.Team(to); ok {
		where = "◆ " + t.Name
	}
	logPacket(profileDir, f, out, []string{out.Origin, from, to}, fmt.Sprintf("%s sent it up to %s: %s", e.By, where, reason))
	return out, nil
}

// raiserWord is how a raiser is named in a Traffic line: @handle for a
// member, the word itself for manager, you or system.
func raiserWord(by string) string {
	switch by {
	case FromManager, FromSystem, Person:
		return by
	}
	return "@" + strings.TrimPrefix(by, "@")
}

// mayDecide reports whether by may decide or escalate p, and the name it is
// recorded under: [Person], or the handle of the manager p waits on, which
// by may give as its handle or as [FromManager].
func mayDecide(f *File, p Packet, by string) (string, bool) {
	if by == Person {
		return Person, true
	}
	t, ok := f.Team(p.Team)
	if !ok || t.Closed() || t.Manager == "" || by == "" {
		return "", false
	}
	m, ok := t.Member(t.Manager)
	if !ok || (m.Handle != by && by != FromManager) {
		return "", false
	}
	if m.Handle != "" {
		return m.Handle, true
	}
	return FromManager, true
}

// isAncestor reports whether team above is an ancestor of team id.
func isAncestor(f *File, above, id string) bool {
	for _, a := range f.Ancestors(id) {
		if a.ID == above {
			return true
		}
	}
	return false
}

// packetOrigin is the team whose file holds packet id.
func packetOrigin(profileDir, id string) (string, error) {
	all, err := allPackets(profileDir)
	if err != nil {
		return "", err
	}
	for _, p := range all {
		if p.ID == id {
			return p.Origin, nil
		}
	}
	return "", ErrNoPacket
}

// appendDecision appends e to team teamID's packet file under its lock. check,
// when given, is handed the packet e names as it stands under the lock, may
// fill in e, and an error from it writes nothing.
func appendDecision(profileDir, teamID string, e *decisionEvent, check func(Packet) error) error {
	if err := safeTeamID(teamID); err != nil {
		return err
	}
	path := DecisionsPath(profileDir, teamID)
	return lockedAt(strings.TrimSuffix(path, ".jsonl")+".lock", lockWait, func() error {
		if check != nil {
			packets, err := packetCache.read(path)
			if err != nil {
				return err
			}
			p, ok := packets.byID[e.ID]
			if !ok {
				return ErrNoPacket
			}
			if err := check(p); err != nil {
				return err
			}
		}
		line, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if info, err := os.Stat(path); err == nil && info.Size() > 0 && info.Size()+int64(len(line))+1 > decisionsRotateBytes {
			if err := rotateDecisions(path); err != nil {
				return err
			}
		}
		before := modTime(path)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		if _, err := file.Write(append(line, '\n')); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		advance(path, before)
		return nil
	})
}

// rotateDecisions starts a new packet file at path, under its lock: the fold
// as it stands is taken, the file is renamed over the rotated one, and the new
// file is written (temporary file and rename) with a carry line for every
// packet still waiting, so nothing waiting lives only in the rotated file.
func rotateDecisions(path string) error {
	now, err := packetCache.read(path)
	if err != nil {
		return err
	}
	var carry bytes.Buffer
	for _, p := range now.list() {
		if !p.Waiting() {
			continue
		}
		p := p
		line, err := json.Marshal(decisionEvent{Op: opCarry, At: p.At, Packet: &p})
		if err != nil {
			return err
		}
		carry.Write(line)
		carry.WriteByte('\n')
	}
	if err := os.Rename(path, decisionsRotated(path)); err != nil {
		return err
	}
	if carry.Len() == 0 {
		return nil
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".decisions-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	if _, err := temp.Write(carry.Bytes()); err != nil {
		_ = temp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

// logPacket appends a [KindPacket] line to the Traffic of each distinct team
// in teams that is a team (not [Person], not ""). A log that cannot be written
// costs the line and never the packet, which is written already.
func logPacket(profileDir string, f *File, p Packet, teams []string, text string) {
	seen := map[string]bool{}
	for _, id := range teams {
		if id == "" || id == Person || seen[id] {
			continue
		}
		seen[id] = true
		if _, ok := f.Team(id); !ok {
			continue
		}
		_ = AppendTraffic(profileDir, id, Entry{Kind: KindPacket, From: FromSystem, To: ToManager,
			Text: text, Packet: p.ID, State: p.State})
	}
}

// ── READING ─────────────────────────────────────────────────────────────────

// OpenPackets is every packet waiting on scope (a team id, [Person], or
// [ScopeAll] for every waiting packet), oldest first, and the stamp of the
// packet files it was read from: equal stamps are the same answer, so a
// reader can hand the stamp back and be told nothing moved ([PacketsStamp]).
func OpenPackets(profileDir, scope string) ([]Packet, string, error) {
	stamp := PacketsStamp(profileDir)
	all, err := allPackets(profileDir)
	if err != nil {
		return nil, stamp, err
	}
	var out []Packet
	for _, p := range all {
		if p.Waiting() && (scope == ScopeAll || p.Team == scope) {
			out = append(out, p)
		}
	}
	return out, stamp, nil
}

// Packets is every packet raised from team teamID, decided or not, oldest
// first: a team's history, and its closing reports.
func Packets(profileDir, teamID string) ([]Packet, error) {
	if err := safeTeamID(teamID); err != nil {
		return nil, err
	}
	got, err := packetCache.read(DecisionsPath(profileDir, teamID))
	if err != nil {
		return nil, err
	}
	return got.list(), nil
}

// PacketByID is packet id as it stands.
func PacketByID(profileDir, id string) (Packet, error) {
	all, err := allPackets(profileDir)
	if err != nil {
		return Packet{}, err
	}
	for _, p := range all {
		if p.ID == id {
			return p, nil
		}
	}
	return Packet{}, ErrNoPacket
}

// PacketsStamp is one stamp over every team's packet file: one directory
// listing and a stat per team. It moves when any packet file is written,
// made or removed.
func PacketsStamp(profileDir string) string {
	files := packetFiles(profileDir)
	if len(files) == 0 {
		return MissingStamp
	}
	h := fnv.New64a()
	for _, path := range files {
		_, _ = io.WriteString(h, path)
		_, _ = io.WriteString(h, "=")
		_, _ = io.WriteString(h, stampOf(path))
		_, _ = io.WriteString(h, "\n")
	}
	return strconv.FormatUint(h.Sum64(), 36) + "." + strconv.Itoa(len(files))
}

// packetFiles is every team directory's packet file that exists, sorted.
func packetFiles(profileDir string) []string {
	root := config.ProfilePath(profileDir, "teams")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || safeTeamID(e.Name()) != nil {
			continue
		}
		path := filepath.Join(root, e.Name(), "decisions.jsonl")
		if _, err := os.Stat(path); err == nil {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

// allPackets is every packet in every team's file, oldest raised first.
func allPackets(profileDir string) ([]Packet, error) {
	var out []Packet
	for _, path := range packetFiles(profileDir) {
		got, err := packetCache.read(path)
		if err != nil {
			return nil, err
		}
		out = append(out, got.list()...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Raised.Before(out[j].Raised) })
	return out, nil
}

// ── THE FOLD, AND ITS MEMORY ────────────────────────────────────────────────

// folded is one packet file as read so far: the rotated file whole (as it
// was at rotated), then the current file up to offset.
type folded struct {
	stamp   string
	rotated string
	offset  int64
	order   []string
	byID    map[string]Packet
}

func (f *folded) list() []Packet {
	out := make([]Packet, 0, len(f.order))
	for _, id := range f.order {
		out = append(out, clonePacket(f.byID[id]))
	}
	return out
}

// apply folds one event in.
func (f *folded) apply(e decisionEvent) {
	switch e.Op {
	case opRaise:
		if e.Packet == nil || e.Packet.ID == "" {
			return
		}
		if _, dup := f.byID[e.Packet.ID]; dup {
			return
		}
		f.order = append(f.order, e.Packet.ID)
		f.byID[e.Packet.ID] = clonePacket(*e.Packet)
	case opCarry:
		if e.Packet == nil || e.Packet.ID == "" {
			return
		}
		if _, known := f.byID[e.Packet.ID]; !known {
			f.order = append(f.order, e.Packet.ID)
		}
		f.byID[e.Packet.ID] = clonePacket(*e.Packet)
	case opDecide:
		if p, ok := f.byID[e.ID]; ok && p.Waiting() {
			f.byID[e.ID] = foldDecide(p, e)
		}
	case opEscalate:
		if p, ok := f.byID[e.ID]; ok && p.Waiting() {
			f.byID[e.ID] = foldEscalate(p, e)
		}
	}
}

func foldDecide(p Packet, e decisionEvent) Packet {
	p.State, p.DecidedBy, p.Decision, p.Reason, p.At = PacketDecided, e.By, e.Decision, e.Reason, e.At
	return p
}

func foldEscalate(p Packet, e decisionEvent) Packet {
	p.Trail = append(append([]Hop(nil), p.Trail...), Hop{From: p.Team, To: e.To, By: e.By, Reason: e.Reason, At: e.At})
	p.State, p.Team, p.Reason, p.At = PacketEscalated, e.To, e.Reason, e.At
	return p
}

func clonePacket(p Packet) Packet {
	p.Parties = append([]Party(nil), p.Parties...)
	p.Options = append([]Option(nil), p.Options...)
	p.Trail = append([]Hop(nil), p.Trail...)
	if p.Recommendation != nil {
		r := *p.Recommendation
		p.Recommendation = &r
	}
	if p.Report != nil {
		r := *p.Report
		r.Files = append([]string(nil), r.Files...)
		p.Report = &r
	}
	if p.Cap != nil {
		c := *p.Cap
		p.Cap = &c
	}
	return p
}

// packetMemory is every packet file read so far, by path, shared by every
// caller in the process (the engine answers several windows from it).
type packetMemory struct {
	mu    sync.Mutex
	files map[string]*folded
	// reads counts the bytes read, for a test to see a quiet file costs none.
	reads int64
}

var packetCache packetMemory

// forgetPackets drops what is remembered of profileDir's files, after a
// delete removed some.
func forgetPackets(profileDir string) {
	prefix := config.ProfilePath(profileDir, "teams") + string(filepath.Separator)
	packetCache.mu.Lock()
	defer packetCache.mu.Unlock()
	for path := range packetCache.files {
		if strings.HasPrefix(path, prefix) {
			delete(packetCache.files, path)
		}
	}
}

// read is the file at path folded: from memory when its stamp has not moved,
// from where the last read stopped when it grew, and from the top when it is
// new to this process or shrank. The answer is a copy.
func (m *packetMemory) read(path string) (*folded, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.files == nil {
		m.files = map[string]*folded{}
	}
	stamp := stampOf(path)
	rotated := stampOf(decisionsRotated(path))
	f := m.files[path]
	if f != nil && f.stamp == stamp && f.rotated == rotated {
		return f.copy(), nil
	}
	if stamp == MissingStamp && rotated == MissingStamp {
		delete(m.files, path)
		return &folded{byID: map[string]Packet{}}, nil
	}
	size := int64(0)
	if stamp != MissingStamp {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		size = info.Size()
	}
	// FROM THE TOP when this process has not read it, when it shrank, or when
	// the rotated file moved (a rotation happened): the rotated file whole,
	// then the current one.
	if f == nil || size < f.offset || f.rotated != rotated {
		f = &folded{byID: map[string]Packet{}}
		if rotated != MissingStamp {
			if _, err := m.foldFrom(decisionsRotated(path), 0, f); err != nil && !os.IsNotExist(err) {
				return nil, err
			}
		}
		f.offset = 0
	}
	if stamp != MissingStamp {
		end, err := m.foldFrom(path, f.offset, f)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		f.offset = end
	}
	// A line still being written has no newline yet; it is read next time,
	// from the offset that stops before it.
	f.stamp, f.rotated = stamp, rotated
	m.files[path] = f
	return f.copy(), nil
}

// foldFrom folds the complete lines of the file at path from offset into f,
// and answers the offset after the last complete line.
func (m *packetMemory) foldFrom(path string, offset int64, f *folded) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return offset, err
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return offset, err
	}
	r := bufio.NewReader(file)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			m.reads += int64(len(line))
			offset += int64(len(line))
			var e decisionEvent
			if json.Unmarshal(bytes.TrimSpace(line), &e) == nil {
				f.apply(e)
			}
		}
		if err == io.EOF {
			return offset, nil
		}
		if err != nil {
			return offset, err
		}
	}
}

func (f *folded) copy() *folded {
	out := &folded{stamp: f.stamp, rotated: f.rotated, offset: f.offset, order: append([]string(nil), f.order...),
		byID: make(map[string]Packet, len(f.byID))}
	for id, p := range f.byID {
		out.byID[id] = p
	}
	return out
}

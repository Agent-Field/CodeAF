package session

// A CONVERSATION IN A TEAM: who it is there, what it is told, and what it is
// shown.
//
// internal/teams is the one store for teams (teams.json) and for the Traffic a
// team's members and manager write to each other (teams/<id>/traffic.jsonl).
// The conversations view writes it from the interface; this file is the other
// reader and writer, the session's half, and the two meet ONLY through that
// store. Nothing here calls into the interface and nothing there calls in here
// for a team feature, which is what lets either side restart, run on another
// build, or not be running at all without the other losing a message.
//
// THREE THINGS LIVE HERE, and each has one door:
//
//   - IDENTITY ([Agent.teamRoles]). A conversation finds its teams by its own
//     conversation key, which is the key the interface stores a member under:
//     the transcript's path, cleaned and with its symlinks resolved (tui3's
//     convKey, detach.go). Both spellings are kept, cleaned and resolved, because
//     a hosted surface keys an engine's path by its cleaned spelling alone and
//     /tmp against /private/tmp is one file with two names on a Mac. The manager
//     is the member whose key is Team.Manager.
//   - DELIVERY ([Agent.teamBoundary]). At every step boundary, the one legal place a
//     user-role line may join a turn (agent.go's [Agent.drainSteering]), what was
//     addressed to this conversation since its cursor is put in front of the
//     model as ONE marked note: "◆ from manager: …", "from @web: …". It is the
//     session's line and never the person's, it starts no turn (v1 does not
//     wake on team events), and it is journaled as a note so a reopened page
//     draws it in the harness's lane.
//   - THE DIGEST ([Agent.refreshTeamDigest]). A manager's turn carries
//     [teams.Digest] for its team, with the manager's brief above it, in a note
//     of its own at the tail of the transcript ([teamNoteOpening]). It is read
//     beside the work at the start of a turn, the way the other windows' block
//     is (taskdelta.go), and never carries a member's transcript.
//
// WHO IS NEVER IN A TEAM. A task node, a worker and an auditor are not
// conversations anybody put in a team: their keys are not in the file, and a
// node's brief is its whole world by contract. They are answered no before any
// disk is read ([Config.teamProfile]). So is a session with no profile
// directory, on the absence law [Config.ProfileDir] states for the settings
// pair: every door that is a conversation resolves the profile before it builds
// a session, and a package that resolved ~/.codeaf itself would read the
// person's real teams from a test.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// teamRole is one team this conversation is in, as teams.json says right now.
type teamRole struct {
	id   string
	name string
	// handle is this conversation's short name in the team, empty while it has
	// no title to derive one from.
	handle string
	// manager says this conversation is the team's manager.
	manager bool
	// managed says the team has a manager at all. A member's `team_post` is
	// offered only in a team that has one, because the manager is the one the
	// room is run by (docs/design/conversations-and-teams/DESIGN.md, section 5).
	managed bool
	// key is the conversation key this conversation is stored under in the
	// team, the one spelling of [Agent.teamKeysLocked] the file holds. An event
	// this conversation writes names it ([teams.Entry.Member]).
	key string
}

// fileStamp is what a stat says about a file, and the whole of how this file
// decides a read is worth doing: a teams file or a Traffic log whose size and
// modification time have not moved has nothing new in it. present is false for
// a file that is not there.
type fileStamp struct {
	size    int64
	mod     time.Time
	present bool
	read    bool
}

func stampOf(path string) fileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{read: true}
	}
	return fileStamp{size: info.Size(), mod: info.ModTime(), present: true, read: true}
}

// teamSeat is this session's own account of its teams: its keys, the teams it
// was last found in, and how far into each team's Traffic it has read.
//
// IT HAS ITS OWN LOCK and that lock is never held with the agent's: every road
// through here reads the disk, and a disk read under a.mu is a stall for every
// surface asking this agent anything. Callers take this one, read, let go, and
// only then take a.mu to hand over what they found.
type teamSeat struct {
	mu sync.Mutex
	// keys are the conversation key's spellings, resolved once the transcript
	// exists ([Agent.teamKeysLocked]).
	keys     []string
	resolved bool
	// teamsAt is the teams file as it was when roles was read from it.
	teamsAt fileStamp
	roles   []teamRole
	// cursors is the id of the last Traffic entry read, per team, and
	// trafficAt is each log as it was when this conversation last caught up on
	// it. cursorsRead says cursors has been loaded from the session folder.
	cursors     map[string]string
	cursorsRead bool
	trafficAt   map[string]fileStamp
	// events is the lane this conversation's own events leave by (teamevent.go).
	events teamEventLane
}

// teamLogStart is the cursor that reads a Traffic log from its first entry:
// [teams.ReadTraffic] pages forward from an id and reads the tail from "".
const teamLogStart = "000000000000"

// teamPageLimit is how many entries one boundary reads from one team. A log
// that has more waiting is read again at the next boundary rather than all at
// once, so a conversation away for a week is caught up a page at a time.
const teamPageLimit = 100

// teamFirstLook is how far back a conversation looks the first time it meets a
// team, for the one entry it is owed from before it was born: its own start
// ([Agent.firstTeamCursor]).
const teamFirstLook = 200

// teamStartGrace is how long before this process began a start addressed to
// this conversation may be and still be the start that made it. The interface
// opens the conversation on seeing the start, which is seconds; ten minutes is
// generous and still keeps a week-old start from replaying a week of Traffic.
const teamStartGrace = 10 * time.Minute

// teamEntryText is how many characters of one entry's text are delivered. A
// member writing a report should write it into a file and post the path.
const teamEntryText = 4000

// teamProfile is the profile directory this session's teams are read from, and
// "" for a session that is never in a team (see the file comment).
func (c Config) teamProfile() string {
	if c.InTask || c.taskID != 0 {
		return ""
	}
	if strings.TrimSpace(c.transcriptPath()) == "" {
		return ""
	}
	return strings.TrimSpace(c.ProfileDir)
}

// transcriptPath is the journal this conversation is keyed by.
func (c Config) transcriptPath() string {
	if path := strings.TrimSpace(c.SessionFile); path != "" {
		return path
	}
	return c.Place.Transcript()
}

// teamKeysLocked is this conversation's key, in both spellings. The resolved
// one needs the file to exist, so until it does the cleaned one is answered and
// the resolution is tried again next time.
func (a *Agent) teamKeysLocked() []string {
	if a.team.resolved {
		return a.team.keys
	}
	path := strings.TrimSpace(a.config.transcriptPath())
	if path == "" {
		return nil
	}
	clean := filepath.Clean(path)
	keys := []string{clean}
	real, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return keys
	}
	if real = filepath.Clean(real); real != clean {
		keys = append(keys, real)
	}
	a.team.keys, a.team.resolved = keys, true
	return keys
}

// teamRoles is every team this conversation is in, read again only when the
// teams file has moved.
func (a *Agent) teamRoles() []teamRole {
	profile := a.config.teamProfile()
	if profile == "" {
		return nil
	}
	a.team.mu.Lock()
	defer a.team.mu.Unlock()
	return a.teamRolesLocked(profile)
}

func (a *Agent) teamRolesLocked(profile string) []teamRole {
	keys := a.teamKeysLocked()
	if len(keys) == 0 {
		return nil
	}
	now := stampOf(teams.Path(profile))
	if now == a.team.teamsAt {
		return a.team.roles
	}
	if !now.present {
		a.team.teamsAt, a.team.roles = now, nil
		a.team.events.member.Store(false)
		return nil
	}
	file, err := teams.Load(profile)
	if err != nil {
		// AN UNREADABLE FILE IS THE INTERFACE'S TO SET ASIDE ([teams.SetAside]),
		// never this reader's. What was known a moment ago stays known, and the
		// stamp is not taken, so the next boundary tries again.
		return a.team.roles
	}
	a.team.teamsAt = now
	a.team.roles = rolesFor(file.Teams, keys)
	a.team.events.member.Store(eventfulRoles(a.team.roles))
	return a.team.roles
}

// rolesFor is the teams whose members include one of keys, in file order.
func rolesFor(list []teams.Team, keys []string) []teamRole {
	var roles []teamRole
	for _, team := range list {
		member, ok := memberOf(team, keys)
		if !ok {
			continue
		}
		roles = append(roles, teamRole{
			id:      team.ID,
			name:    team.Name,
			handle:  member.Handle,
			manager: team.Manager != "" && team.Manager == member.Key,
			managed: team.Manager != "",
			key:     member.Key,
		})
	}
	return roles
}

// memberOf is the member of team that is this conversation.
func memberOf(team teams.Team, keys []string) (teams.Member, bool) {
	for _, key := range keys {
		if member, ok := team.Member(key); ok {
			return member, true
		}
	}
	return teams.Member{}, false
}

// ── delivery ────────────────────────────────────────────────────────────────

// teamBoundary is the step boundary's whole business with teams: the verbs this
// conversation's roles give it go onto the belt, and what was addressed to it is
// handed back as one note, "" for nothing. It is called by [Agent.drainSteering]
// before that drain takes the agent's lock.
//
// THE VERBS ARRIVE BY THE ARMING DOOR AND NEVER LEAVE. A conversation made a
// manager halfway through its life is given the manager's verbs at its next
// boundary through [Agent.armFamily], the door a connected account and a loaded
// group arrive through, on the same append law: at the tail, nothing already
// there moves. A conversation that stops being a manager keeps the verbs and
// each one refuses, because it asks the file again when it is called
// ([Agent.teamTarget]); taking a tool out of the block would re-price the whole
// conversation for a verb nobody will call.
func (a *Agent) teamBoundary() string {
	profile := a.config.teamProfile()
	if profile == "" {
		return ""
	}
	a.team.mu.Lock()
	roles := a.teamRolesLocked(profile)
	news := a.teamNewsLocked(profile, roles)
	a.team.mu.Unlock()
	a.armTeamTools(roles)
	return news
}

// armTeamTools puts the verbs these roles give onto the belt.
func (a *Agent) armTeamTools(roles []teamRole) {
	var arriving []bare.Tool
	manager, member := false, false
	for _, role := range roles {
		manager = manager || role.manager
		member = member || (!role.manager && role.managed)
	}
	if manager {
		arriving = append(arriving, a.managerTools()...)
	}
	if member {
		arriving = append(arriving, a.memberTools()...)
	}
	if len(arriving) == 0 {
		return
	}
	// A schema that will not parse is a bug in this build, and every tool here
	// is a literal; the error has nowhere useful to go at a boundary.
	_, _ = a.armFamily(arriving)
}

// teamNewsLocked reads each team's Traffic past this conversation's cursor and
// composes what is addressed to it. The caller holds a.team.mu.
func (a *Agent) teamNewsLocked(profile string, roles []teamRole) string {
	if len(roles) == 0 {
		return ""
	}
	a.readTeamCursorsLocked()
	moved := false
	var groups []string
	for _, role := range roles {
		path := teams.TrafficPath(profile, role.id)
		stamp := stampOf(path)
		cursor, known := a.team.cursors[role.id]
		if known && stamp == a.team.trafficAt[role.id] {
			continue
		}
		if !known {
			cursor = a.firstTeamCursor(profile, role)
			a.team.cursors[role.id] = cursor
			moved = true
		}
		entries, err := teams.ReadTraffic(profile, role.id, cursor, teamPageLimit)
		if err != nil {
			continue
		}
		var lines []string
		for _, entry := range entries {
			cursor = entry.ID
			if line := teamLine(role, entry); line != "" {
				lines = append(lines, line)
			}
		}
		if cursor != a.team.cursors[role.id] {
			a.team.cursors[role.id] = cursor
			moved = true
		}
		// CAUGHT UP IS WHAT THE STAMP MEANS. A page that came back full has more
		// behind it, so the stamp is left unmatched and the next boundary reads on.
		if len(entries) < teamPageLimit {
			a.team.trafficAt[role.id] = stamp
		}
		if len(lines) > 0 {
			groups = append(groups, teamNewsGroup(role, lines))
		}
	}
	if moved {
		a.saveTeamCursorsLocked()
	}
	return strings.Join(groups, "\n\n")
}

// firstTeamCursor is where a conversation starts reading a team it has no
// cursor for.
//
// A FRESH CONVERSATION DOES NOT REPLAY THE TEAM'S HISTORY. It starts at the last
// entry written before this process began, so what it is handed is what was said
// while it was listening, and a member added to a team with a month of Traffic
// is not handed the month.
//
// THE ONE EXCEPTION IS ITS OWN START. A member the manager started with
// `team_start` is opened by the interface after the start is written, and the
// manager may have said something to it in between; so a start addressed to
// this conversation's handle, written shortly before this process began, is
// where it starts reading instead. It starts reading AT the start and not after
// it, because the start carries the brief, and the brief is handed to the new
// member here, marked as the manager's, on its first request ([teamBriefLine]).
// The person never typed it, so it must not arrive as the person's message.
func (a *Agent) firstTeamCursor(profile string, role teamRole) string {
	tail, err := teams.ReadTraffic(profile, role.id, "", teamFirstLook)
	if err != nil {
		return teamLogStart
	}
	born := a.startedAt
	cursor := teamLogStart
	started := ""
	for _, entry := range tail {
		if !entry.At.After(born) {
			cursor = entry.ID
		}
		if role.handle != "" && entry.Kind == teams.KindStart && entry.To == role.handle &&
			!entry.At.Before(born.Add(-teamStartGrace)) {
			started = entry.ID
		}
	}
	if started != "" && started <= cursor {
		return teamCursorBefore(started)
	}
	return cursor
}

// teamLine is one entry as this conversation is told it, or "" for an entry
// that is not addressed to it.
//
// WHAT A MEMBER IS TOLD is what is addressed to it: a line to its own handle, to
// everyone, or to the room. WHAT A MANAGER IS TOLD is every member's post,
// wherever it was aimed, because the room is the manager's to run; and never its
// own lines, the person's (which reach it in its own chat), a start or a stop
// (which the interface performs) or an event (which the digest carries).
func teamLine(role teamRole, entry teams.Entry) string {
	if entry.Kind == teams.KindStart {
		return teamBriefLine(role, entry)
	}
	if entry.Kind != teams.KindNote && entry.Kind != teams.KindDirective {
		return ""
	}
	text := strings.TrimSpace(entry.Text)
	if text == "" {
		return ""
	}
	text = indentAfterFirst(cutRunesTeam(text, teamEntryText))
	if role.manager {
		switch entry.From {
		case teams.FromManager, teams.FromYou, teams.FromSystem:
			return ""
		}
		return teamSpeaker(entry.From) + teamAimed(entry.To, true) + ": " + text
	}
	if entry.From == role.handle && role.handle != "" {
		return ""
	}
	addressed := entry.To == teams.ToEveryone || entry.To == teams.ToRoom ||
		(role.handle != "" && entry.To == role.handle)
	if !addressed {
		return ""
	}
	if entry.From == teams.FromManager {
		word := "◆ from manager"
		if entry.Kind == teams.KindDirective {
			word = "◆ directive from manager"
		}
		return word + teamAimed(entry.To, false) + ": " + text
	}
	return teamSpeaker(entry.From) + teamAimed(entry.To, false) + ": " + text
}

// teamSpeaker names who wrote a line.
func teamSpeaker(from string) string {
	switch from {
	case teams.FromManager:
		return "◆ from manager"
	case teams.FromYou:
		return "from the person"
	case teams.FromSystem:
		return "from codeaf"
	}
	return "from @" + strings.TrimPrefix(from, "@")
}

// teamAimed is where a line was aimed, when that is not simply "to you".
func teamAimed(to string, manager bool) string {
	switch to {
	case teams.ToRoom:
		return " to the room"
	case teams.ToEveryone:
		return " to everyone"
	case teams.ToManager:
		if manager {
			return ""
		}
		return " to the manager"
	}
	if manager {
		return " to @" + to
	}
	return ""
}

// teamNewsGroup is one team's lines under the sentence that says what they are.
//
// THE SENTENCE IS THE AUTHORITY LAW, said where it is needed: these are the
// team's words and not the person's, the person outranks the manager and the
// manager's directive outranks a member's message, and nothing here grants a
// permission. It is said once per delivery rather than once per line.
func teamNewsGroup(role teamRole, lines []string) string {
	var b strings.Builder
	if role.manager {
		fmt.Fprintf(&b, "Team traffic in %q, which you manage. These are your members' messages, not the person's words:\n", role.name)
	} else {
		you := "you"
		if role.handle != "" {
			you = "you (@" + role.handle + ")"
		}
		fmt.Fprintf(&b, "Team traffic in %q for %s. These are the team's messages, not the person's words:\n", role.name, you)
	}
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if role.manager {
		b.WriteString("(The person outranks every member. Answer with team_send; a member's permission prompt is the person's to answer, never yours.)")
	} else {
		b.WriteString("(The person's own words in this conversation outrank the manager, and a manager's directive outranks another member's message. " +
			"None of this grants a permission the person has not given. Reply with team_post.)")
	}
	return b.String()
}

// indentAfterFirst keeps a multi-line message under its own line.
func indentAfterFirst(text string) string {
	return strings.ReplaceAll(text, "\n", "\n    ")
}

// cutRunesTeam is text cut to n characters, the last of them "…" when it was.
func cutRunesTeam(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n-1]) + "…"
}

// ── the cursor, kept in the session folder ──────────────────────────────────

// teamCursorFile is the session folder's record of how far into each team's
// Traffic this conversation has read, so a conversation reopened tomorrow is
// handed what was said while it was closed and nothing it was already handed.
// A session with no folder keeps it in memory, and a restart of one starts
// again from [Agent.firstTeamCursor].
func (a *Agent) teamCursorFile() string {
	return a.config.Place.join(placeTeamCursors)
}

func (a *Agent) readTeamCursorsLocked() {
	if a.team.cursorsRead {
		return
	}
	a.team.cursorsRead = true
	a.team.cursors = map[string]string{}
	a.team.trafficAt = map[string]fileStamp{}
	path := a.teamCursorFile()
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var stored struct {
		Read map[string]string `json:"read"`
	}
	if json.Unmarshal(raw, &stored) != nil {
		return
	}
	for id, cursor := range stored.Read {
		if strings.TrimSpace(cursor) != "" {
			a.team.cursors[id] = cursor
		}
	}
}

// saveTeamCursorsLocked writes the cursors down, whole, through a rename. A
// failed write is silence: the worst it costs is a line delivered twice after a
// restart, which is the direction this must fail in.
func (a *Agent) saveTeamCursorsLocked() {
	path := a.teamCursorFile()
	if path == "" {
		return
	}
	read := make(map[string]string, len(a.team.cursors))
	for id, cursor := range a.team.cursors {
		read[id] = cursor
	}
	raw, err := json.Marshal(struct {
		Read map[string]string `json:"read"`
	}{read})
	if err != nil {
		return
	}
	_ = writeTeamFile(path, raw)
}

func writeTeamFile(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".team-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// ── the digest ──────────────────────────────────────────────────────────────

// teamNoteOpening is the first line of the note a manager's team rides in. It is
// its own note rather than a paragraph of [volatileNoteOpening] because the two
// move on different beats: the card moves when work lands, and a team moves
// whenever a member does.
const teamNoteOpening = "A note from the session, not from the person: the team you manage, as it stands right now. Facts, not requests, and the last such note is the one that holds."

// teamManagerBrief is what a manager is told about being one, on every turn it
// is one. It is three laws and nothing else, because the verbs' own
// descriptions carry how.
const teamManagerBrief = "You are this team's manager. Hand real work to members with team_send (or team_start for a new one) rather than doing it yourself, and keep track with team_status and team_read. " +
	"The person outranks you: what they say in a member's own conversation stands over your directive, and a conflict goes to them. " +
	"You cannot answer a member's permission prompt; tell the person it is waiting."

// teamDigestBudget is how many characters of digest ride each managed team.
const teamDigestBudget = 1600

// teamRecent is how many Traffic entries a digest is offered.
const teamRecent = 20

// refreshTeamDigest reads, for every team this conversation manages, what each
// member is doing and the last few lines of Traffic, and leaves the composed
// block where the next request's note will carry it. It is started beside the
// work at the top of a turn (loop.go), so a slow disk costs the block a step
// and never costs the person a wait.
func (a *Agent) refreshTeamDigest(ctx context.Context) {
	profile := a.config.teamProfile()
	if profile == "" || ctx.Err() != nil {
		return
	}
	block := a.teamDigest(profile)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	a.teamDigestText = block
}

// teamDigest is the brief and one digest per managed team, "" for a
// conversation that manages none.
func (a *Agent) teamDigest(profile string) string {
	a.team.mu.Lock()
	roles := a.teamRolesLocked(profile)
	keys := append([]string(nil), a.teamKeysLocked()...)
	a.team.mu.Unlock()
	var managed []teamRole
	for _, role := range roles {
		if role.manager {
			managed = append(managed, role)
		}
	}
	if len(managed) == 0 {
		return ""
	}
	file, err := teams.Load(profile)
	if err != nil {
		return ""
	}
	now := time.Now()
	parts := []string{teamManagerBrief}
	for _, role := range managed {
		team, ok := file.Team(role.id)
		if !ok {
			continue
		}
		log, _ := teams.ReadTraffic(profile, role.id, "", teamStateLook)
		parts = append(parts, teams.Digest(team, memberStates(team, keys, now, log), recentOf(log), teamDigestBudget))
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

// teamBlockLocked is what the team note carries.
func (a *Agent) teamBlockLocked() string { return strings.TrimSpace(a.teamDigestText) }

// ── what a member is doing, read off its journal ────────────────────────────

// memberStates is each member's state as its journal says it, keyed by
// conversation key, corrected by the events in log (the team's Traffic, oldest
// first) for the one state a journal cannot hold: a member held on a permission
// prompt ([askingFromEvents]). This conversation's own row is running: it is
// the one reading the file.
func memberStates(team teams.Team, self []string, now time.Time, log []teams.Entry) map[string]teams.MemberState {
	states := make(map[string]teams.MemberState, len(team.Members))
	for _, member := range team.Members {
		if teamHoldsKey(self, member.Key) {
			states[member.Key] = teams.MemberState{State: teams.StateRunning}
			continue
		}
		if state, last, ok := journalStateAt(member.File, now); ok {
			states[member.Key] = askingFromEvents(state, last, member, log)
		}
	}
	return states
}

func teamHoldsKey(list []string, want string) bool {
	for _, have := range list {
		if have == want {
			return true
		}
	}
	return false
}

// journalTail is how much of the end of a member's journal is read to learn
// what it is doing. A turn's last lines are what say so, and a quarter of a
// megabyte holds dozens of turns of ordinary work.
const journalTail = 256 << 10

// journalStale is how long a turn with no ending may go quiet before it is
// read as over. A process that died mid-turn writes no ending, and a member
// shown running forever would be a digest that lies.
const journalStale = 20 * time.Minute

// journalFiles is how many touched files a state carries.
const journalFiles = 8

// journalState reads one member's journal WITHOUT opening it the way a session
// does ([Peek]'s discipline: no lock, no repair, no write) and says what the
// member is doing. The boolean is false for a file that cannot be read.
//
// WHAT THE FILE CAN SAY, and nothing more:
//
//   - asking, when the last `ask` it called has no answer after it;
//   - failed, when a turn's last word is an error or a failure line;
//   - running, when the person (or the harness) spoke after the last turn's
//     pace line and the file has moved within [journalStale];
//   - idle otherwise: a turn ended and nobody has said anything since.
//
// A permission prompt waiting on the person is not written to the journal, so a
// member held on one reads as running here; the member's own asking event in
// the Traffic log is what says otherwise ([askingFromEvents]).
func journalState(path string, now time.Time) (teams.MemberState, bool) {
	state, _, ok := journalStateAt(path, now)
	return state, ok
}

// journalStateAt is [journalState] and the instant of the journal's last line,
// which is what an asking event is weighed against.
func journalStateAt(path string, now time.Time) (teams.MemberState, time.Time, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return teams.MemberState{}, time.Time{}, false
	}
	lines, mod, err := journalTailLines(path, journalTail)
	if err != nil {
		return teams.MemberState{}, time.Time{}, false
	}
	var (
		last       time.Time
		spokeAt    = -1
		endedAt    = -1
		failedAt   = -1
		pendingAsk = map[string]string{}
		files      []string
	)
	for index, line := range lines {
		var entry sessionEntry
		if json.Unmarshal(line, &entry) != nil {
			continue
		}
		if at, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err == nil {
			last = at
		}
		switch entry.Type {
		case "pace":
			endedAt = index
		case "error", "failure":
			failedAt = index
		case "message":
			switch entry.Role {
			case "user":
				spokeAt = index
			case "assistant":
				for _, call := range entry.ToolCalls {
					switch call.Function.Name {
					case "ask":
						var args struct {
							Head string `json:"head"`
						}
						_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
						pendingAsk[call.ID] = strings.TrimSpace(args.Head)
					case "edit", "write":
						var args struct {
							Path string `json:"path"`
						}
						if json.Unmarshal([]byte(call.Function.Arguments), &args) == nil && strings.TrimSpace(args.Path) != "" {
							files = appendFresh(files, strings.TrimSpace(args.Path))
						}
					}
				}
			case "tool":
				delete(pendingAsk, entry.ToolCallID)
			}
		}
	}
	if last.IsZero() {
		last = mod
	}
	state := teams.MemberState{SinceActive: now.Sub(last), Files: newestFirst(files, journalFiles)}
	if state.SinceActive <= 0 {
		state.SinceActive = time.Second
	}
	switch {
	case len(pendingAsk) > 0:
		state.State = teams.StateAsking
		for _, head := range pendingAsk {
			if head != "" {
				state.Question = head
			}
		}
	case failedAt > endedAt && failedAt > spokeAt:
		state.State = teams.StateFailed
	case spokeAt > endedAt && now.Sub(last) < journalStale:
		state.State = teams.StateRunning
	default:
		state.State = teams.StateIdle
	}
	return state, last, true
}

// appendFresh moves path to the end of files, so the list is in the order the
// files were last touched.
func appendFresh(files []string, path string) []string {
	for index, have := range files {
		if have == path {
			files = append(files[:index:index], files[index+1:]...)
			break
		}
	}
	return append(files, path)
}

// newestFirst is the last n of files, newest first.
func newestFirst(files []string, n int) []string {
	out := make([]string, 0, min(len(files), n))
	for index := len(files) - 1; index >= 0 && len(out) < n; index-- {
		out = append(out, files[index])
	}
	return out
}

// journalTailLines is the complete lines in the last window bytes of a file, and the
// file's modification time. A first line cut by the window is dropped.
func journalTailLines(path string, window int64) ([][]byte, time.Time, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, time.Time{}, err
	}
	if info.IsDir() {
		return nil, time.Time{}, errors.New("session: a journal is a file")
	}
	start := max(info.Size()-window, 0)
	buf := make([]byte, info.Size()-start)
	if _, err := file.ReadAt(buf, start); err != nil && len(buf) > 0 && !errors.Is(err, io.EOF) {
		return nil, time.Time{}, err
	}
	parts := strings.Split(string(buf), "\n")
	if start > 0 && len(parts) > 0 {
		parts = parts[1:]
	}
	lines := make([][]byte, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			lines = append(lines, []byte(part))
		}
	}
	return lines, info.ModTime(), nil
}

// sortedTeamNames is the names of roles, for a refusal that lists them.
func sortedTeamNames(roles []teamRole) []string {
	names := make([]string, 0, len(roles))
	for _, role := range roles {
		names = append(names, fmt.Sprintf("%q", role.name))
	}
	sort.Strings(names)
	return names
}

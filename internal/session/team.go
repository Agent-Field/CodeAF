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
//     session's line and never the person's, and it is journaled as a note so
//     a reopened page draws it in the harness's lane. The boundary itself
//     starts no turn; the lines that ask for an answer (a directive to a
//     member, a reply to the manager) wake an idle conversation through the
//     traffic watch (team_wakewatch.go), which hands them over by this door.
//   - THE ROLE ([teamRoleBlock]). What this conversation IS in each team, the
//     manager of it with its members named and its three laws, or a member
//     under a manager with its handle and its verb, rides a note of its own
//     ([teamRoleNoteOpening]) worded as codeaf's instruction. It is composed
//     from the same read the boundary makes before the request, so it is in
//     front of the model from the first request a conversation sends as a
//     manager, and never waits on a read beside the work.
//   - THE DIGEST ([Agent.refreshTeamDigest]). A manager's turn carries
//     [teams.Digest] for its team in a note of its own at the tail of the
//     transcript ([teamNoteOpening]). It is read beside the work at the start
//     of a turn, the way the other windows' block is (taskdelta.go), and never
//     carries a member's transcript.
//
// WHO IS NEVER IN A TEAM. A task node, a worker and an auditor are not
// conversations anybody put in a team: their keys are not in the file, and a
// node's brief is its whole world by contract. They are answered no before any
// disk is read ([Config.teamProfile]).
//
// AN EMPTY PROFILE DIRECTORY IS NOT ONE OF THEM. It is the ordinary launch:
// CODEAF_PROFILE_DIR is what nearly nobody exports, so the engine daemon that
// builds a conversation hands it "" and means the profile where it always is
// ([config.ProfilePath] is the one place that says so). This file once read
// "" as "no team", and that made every team feature, the verbs, the brief,
// delivery, events and the wake, dead for everybody while every test here
// passed, because every test set the directory. A test keeps off the person's
// real teams the way this package's tests keep off every other file under the
// state root: hermetic_test.go moves HOME for the run, and internal/home
// refuses a test binary the root it inherited.

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

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/guard"
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
	// managed says the team has a manager at all, its own or, for a sub-team
	// with none, the nearest one above it (boss). A member's `team_post` is
	// offered only in a team that has one, because the manager is the one the
	// room is run by (docs/design/conversations-and-teams/DESIGN.md, section 5).
	managed bool
	// boss is the managed team whose manager this membership answers to: the
	// team itself when it has a manager, and otherwise the nearest open
	// ancestor with one (team_nest.go's [bossOf]). A member of an unmanaged
	// sub-team asks that manager its questions and posts to it; it is not
	// that manager's to direct, because it is not a member of its team.
	boss     string
	bossName string
	// root says the team is the root, `All teams` (teams' root.go): its
	// manager is the global manager, and its other members are the top-level
	// teams' managers.
	root bool
	// key is the conversation key this conversation is stored under in the
	// team, the one spelling of [Agent.teamKeysLocked] the file holds. An event
	// this conversation writes names it ([teams.Entry.Member]).
	key string
	// wakes says the team's traffic starts this conversation's turn when it is
	// idle: the team's effective `wake` ([teams.Effective], inherited from
	// its parents and the profile's `teams.wake`; team_wakewatch.go).
	wakes bool
	// questionsUp is the effective `questions_up` of the team this
	// conversation reports to, which is what decides where its clarifying
	// questions go (team_questions.go).
	questionsUp bool
	// shared says this conversation is a member (not the manager) of this
	// managed team but reports to a manager elsewhere (teams.File.Home): this
	// team's manager is a LINK, who may read it and send it notes, and whose
	// directives and stops do not reach it as its manager's. reportsTo names
	// the team it reports to, for the sentence that says so.
	shared    bool
	reportsTo string
	// derived says handle is the word list's guess, which the title model
	// chooses again once (handlepick.go).
	derived bool
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
	// teamsAt is the teams file as it was when roles was read from it, and
	// configAt the profile's config.json, whose `teams.` rows the roles'
	// inherited settings fall back to ([teams.DefaultsAt]); defaults is what
	// was read from it.
	teamsAt  fileStamp
	configAt fileStamp
	defaults teams.Defaults
	roles    []teamRole
	// cursors is the id of the last Traffic entry read, per team, and
	// trafficAt is each log as it was when this conversation last caught up on
	// it. cursorsRead says cursors has been loaded from the session folder.
	cursors     map[string]string
	cursorsRead bool
	trafficAt   map[string]fileStamp
	// events is the lane this conversation's own events leave by (teamevent.go).
	events teamEventLane
	// file is the teams file roles was read from, kept so a manager's digest
	// reads the members off it rather than loading the file a second time.
	file *teams.File
	// journals is what each member's journal last said (teamcache.go).
	journals journalCache
	// watch is the wake's own reading of the Traffic, and person counts the
	// turns the person started, which the wake's loop breaker is reset by
	// (team_wakewatch.go).
	watch  teamWatch
	person personTurns
	// spends is each cap pool's spend as last read, against its stamp
	// (team_cap.go), and wraps each managed team's wrap-up in progress
	// (team_wrapup.go).
	spends map[string]spendMemo
	wraps  map[string]*wrapUp
	// claims are the sub-teams this conversation's start named it to manage,
	// found by the delivery that handed it the brief and made good after it
	// (team_nest.go's [Agent.claimSubTeams]).
	claims []subTeamClaim
	// told is the packet answers just handed over. Their durable marks are
	// written after the seat lock is released and away from the turn's path.
	told []string
	// handleTried says this process has asked for its handle once
	// (handlepick.go), so it is never asked for twice.
	handleTried bool
	// answering is, per team, the id of the last message the manager sent
	// this conversation that it was handed: a note, a directive or its brief.
	// It is what a member's reply to the manager answers when it names nothing
	// else, and what the events its turn raises answer (internal/teams'
	// thread.go). Memory only: a conversation reopened answers nothing until
	// the manager next speaks to it, which reads as a thread of its own.
	answering map[string]string
}

// teamAnsweringLocked is the id this conversation's replies in team id answer
// by default, "" for none. The caller holds a.team.mu.
func (a *Agent) teamAnsweringLocked(id string) string { return a.team.answering[id] }

// teamAnswering is [Agent.teamAnsweringLocked] under the seat's lock.
func (a *Agent) teamAnswering(id string) string {
	a.team.mu.Lock()
	defer a.team.mu.Unlock()
	return a.teamAnsweringLocked(id)
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
//
// THE ANSWER IS RESOLVED, NEVER THE RAW FIELD. "" is this function's word for
// "never in a team", and an empty [Config.ProfileDir] is the ordinary launch's
// word for "the usual profile", so handing the field through made the two one
// word. [config.ProfilePath] with no name is the profile directory itself,
// resolved the way every other reader of the profile resolves it.
func (c Config) teamProfile() string {
	if c.InTask || c.taskID != 0 {
		return ""
	}
	if strings.TrimSpace(c.transcriptPath()) == "" {
		return ""
	}
	return config.ProfilePath(c.ProfileDir, "")
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
	settings := a.teamDefaultsMovedLocked(profile)
	if now == a.team.teamsAt && !settings {
		return a.team.roles
	}
	if !now.present {
		a.team.teamsAt, a.team.roles, a.team.file = now, nil, nil
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
	a.team.roles = rolesFor(file, keys, a.team.defaults)
	a.team.file = file
	a.team.events.member.Store(eventfulRoles(a.team.roles))
	return a.team.roles
}

// teamDefaultsMovedLocked reads the profile's `teams.` rows again when its
// config.json has moved since the last read, and reports whether it did. A
// config that has not moved costs one stat. The caller holds a.team.mu.
func (a *Agent) teamDefaultsMovedLocked(profile string) bool {
	now := stampOf(config.BudgetConfigPath(profile))
	if now == a.team.configAt {
		return false
	}
	a.team.configAt = now
	a.team.defaults = teams.DefaultsAt(profile)
	return true
}

// teamDefaults is the profile's `teams.` rows, read again only when they moved.
func (a *Agent) teamDefaults(profile string) teams.Defaults {
	a.team.mu.Lock()
	defer a.team.mu.Unlock()
	a.teamDefaultsMovedLocked(profile)
	return a.team.defaults
}

// rolesFor is the open teams whose members include one of keys, in file
// order, each with its settings resolved against d.
//
// A CLOSED TEAM IS NO PART AT ALL (teams' lifecycle.go): it spends nothing,
// so a conversation whose only teams closed has no verb, no role and no
// delivery, and takes no team turn.
func rolesFor(file *teams.File, keys []string, d teams.Defaults) []teamRole {
	if file == nil {
		return nil
	}
	home, hasHome := teams.Report{}, false
	for _, key := range keys {
		if report, ok := file.Home(key); ok {
			home, hasHome = report, true
			break
		}
	}
	var roles []teamRole
	for _, team := range file.Teams {
		if team.Closed() {
			continue
		}
		member, ok := memberOf(team, keys)
		if !ok {
			continue
		}
		effective := file.Effective(team.ID, d)
		boss, bossName := bossOf(file, team, member.Key)
		role := teamRole{
			id:          team.ID,
			name:        team.Name,
			handle:      member.Handle,
			manager:     team.Manager != "" && team.Manager == member.Key,
			managed:     boss != "",
			boss:        boss,
			bossName:    bossName,
			root:        team.Root,
			key:         member.Key,
			wakes:       effective.Wake,
			questionsUp: effective.QuestionsUp,
			derived:     member.HandleDerived(),
		}
		if hasHome {
			role.questionsUp = file.Effective(home.Team, d).QuestionsUp
		}
		if role.managed && !role.manager && hasHome && home.Team != boss {
			role.shared = true
			if at, ok := file.Team(home.Team); ok {
				role.reportsTo = at.Name
			}
		}
		roles = append(roles, role)
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
	claims := a.team.claims
	a.team.claims = nil
	if len(claims) > 0 {
		// A START THAT NAMED A TEAM FOR THIS CONVERSATION TO RUN is made good
		// before the role is composed, so the request that carries the brief
		// also says this conversation is that team's manager.
		a.team.mu.Unlock()
		a.claimSubTeams(profile, claims)
		a.team.mu.Lock()
		roles = a.teamRolesLocked(profile)
	}
	role := teamRoleBlock(roles, a.team.file)
	told := append([]string(nil), a.team.told...)
	a.team.told = nil
	a.team.mu.Unlock()
	if len(told) > 0 {
		guard.Go("team answers handed over", func() {
			for _, id := range told {
				_ = teams.Told(profile, id)
			}
		})
	}
	// A wrap-up the delivery just started is written down here, after the
	// seat's lock, so a restart keeps the clock (team_wrapup.go).
	a.teamWrapUpNote(profile)
	a.setTeamRole(role)
	a.armTeamTools(roles)
	// A handle that is still the word list's guess is chosen once, beside the
	// turn (handlepick.go).
	a.teamHandlePass(roles)
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
		arriving = append(arriving, a.packetTools()...)
		arriving = append(arriving, a.wrapUpTools()...)
	}
	if member {
		arriving = append(arriving, a.memberTools()...)
	}
	if manager || member {
		arriving = append(arriving, a.raiseTools()...)
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
	// THE CURSOR IS WRITTEN DOWN ONLY WHEN IT MATTERS AFTER A RESTART: when a
	// team's first cursor is taken, because a restart would take a later one and
	// skip what was said while this was closed, and when something was
	// delivered, because a restart must not deliver it twice. A cursor that
	// moved over lines addressed to somebody else is kept in memory; a restart
	// reads those lines again and hands on none of them, which costs a read and
	// never a message, where a write per step cost a file per step.
	save := false
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
			save = true
		}
		entries, err := teams.ReadTraffic(profile, role.id, cursor, teamPageLimit)
		if err != nil {
			continue
		}
		var lines []string
		for _, entry := range entries {
			cursor = entry.ID
			if line := a.teamEntryLineLocked(profile, role, entry); line != "" {
				lines = append(lines, line)
				// WHAT THE MANAGER LAST SAID TO IT IS WHAT IT ANSWERS, until the
				// manager says something else: its next reply to the manager, and
				// the events its turn raises, name it.
				if !role.manager && entry.From == teams.FromManager {
					if a.team.answering == nil {
						a.team.answering = map[string]string{}
					}
					a.team.answering[role.id] = entry.ID
				}
			}
		}
		a.team.cursors[role.id] = cursor
		// CAUGHT UP IS WHAT THE STAMP MEANS. A page that came back full has more
		// behind it, so the stamp is left unmatched and the next boundary reads on.
		if len(entries) < teamPageLimit {
			a.team.trafficAt[role.id] = stamp
		}
		if len(lines) > 0 {
			groups = append(groups, teamNewsGroup(role, lines))
			save = true
		}
	}
	if save {
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
		// AND THE DIRECTIVE IT WAS OPENED FOR. A member nobody had open is
		// opened by the engine because a directive was just sent to it
		// (team_wakewatch.go's [Agent.teamRouse]), and a member that never read
		// this team before would otherwise start after that very directive. So
		// the earliest directive to it inside the same grace is where it starts,
		// for a member only: a manager is never opened for a line it sent.
		if started == "" && !role.manager && role.wakes && teamWakes(role, entry) &&
			!entry.At.Before(born.Add(-teamStartGrace)) {
			started = entry.ID
		}
	}
	if started != "" && started <= cursor {
		return teamCursorBefore(started)
	}
	return cursor
}

// teamEntryLineLocked is one entry as this conversation is told it: a packet
// line by its packet ([teamPacketLine]), the person's wrap-up request to a
// manager as the instruction it is (and the wrap-up's clock started,
// team_wrapup.go), and everything else by [teamLine]. The caller holds
// a.team.mu.
func (a *Agent) teamEntryLineLocked(profile string, role teamRole, entry teams.Entry) string {
	switch {
	case entry.Kind == teams.KindPacket:
		line, told := teamPacketLine(profile, role, entry)
		if told != "" {
			a.team.told = append(a.team.told, told)
		}
		return line
	case entry.Kind == teams.KindStart && entry.Team != "":
		line := teamBriefLine(role, entry)
		if line == "" {
			return ""
		}
		a.team.claims = append(a.team.claims, subTeamClaim{team: entry.Team, parent: role.id, key: role.key})
		return subTeamBriefLine(a.team.file, entry, line)
	case role.manager && teams.IsWrapUp(entry):
		a.teamWrapUpBeginLocked(role, entry.At)
		return wrapUpLine(role, entry)
	}
	return teamLine(role, entry)
}

// teamLine is one entry as this conversation is told it, or "" for an entry
// that is not addressed to it.
//
// WHAT A MEMBER IS TOLD is what is addressed to it: a line to its own handle, to
// everyone, or to the room. WHAT A MANAGER IS TOLD is every member's post,
// wherever it was aimed, because the room is the manager's to run; and never its
// own lines, the person's (which reach it in its own chat), a start or a stop
// (which the interface performs) or an event (which the digest carries). The
// one event everyone is told is codeaf's, that a handle changed.
func teamLine(role teamRole, entry teams.Entry) string {
	// A CONFLICT'S RULING reaches the party it names whoever wrote it and
	// whatever this conversation is in the team (team_nest.go).
	if teams.IsRuling(entry) {
		if rulingFor(role, entry) {
			return rulingLine(entry)
		}
		return ""
	}
	if entry.Kind == teams.KindStart {
		return teamBriefLine(role, entry)
	}
	// A HANDLE THAT CHANGED IS TOLD TO EVERYONE, manager included: it is how a
	// member is addressed, and a line to the old one would reach nobody
	// (handlepick.go's [handleRenameEntry]).
	if entry.Kind == teams.KindEvent && entry.From == teams.FromSystem && entry.To == teams.ToEveryone {
		if text := strings.TrimSpace(entry.Text); text != "" {
			return teamSpeaker(entry.From) + ": " + cutRunesTeam(text, teamEntryText)
		}
		return ""
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
	if entry.To != teams.ToRoom && !entry.Addressed(role.handle) {
		return ""
	}
	// A MEMBER IS TOLD EACH LINE'S NUMBER, "#42", so a reply can name the
	// line it answers (team_post's thread); one to the manager names the
	// manager's last line by itself.
	number := teamNumber(entry)
	if entry.From == teams.FromManager {
		word := "◆ from manager"
		if entry.Kind == teams.KindDirective {
			word = "◆ directive from manager"
		}
		// A LINK'S WORD IS AN FYI. A member shared into this team reports to
		// another manager, and this team's manager may send it notes and
		// nothing more (the verb refuses the rest); a directive that reached
		// it anyway, from an older build, is information.
		if role.shared {
			word = fmt.Sprintf("◆ fyi from the manager of %q (you report to %q, whose word directs you)", role.name, role.reportsTo)
		}
		return word + teamAimed(entry.To, false) + number + ": " + text
	}
	return teamSpeaker(entry.From) + teamAimed(entry.To, false) + number + ": " + text
}

// teamNumber is a delivered line's number with the space before it, " #42",
// and "" for an entry that has no id yet.
func teamNumber(entry teams.Entry) string {
	if entry.ID == "" {
		return ""
	}
	return " " + teams.ThreadNumber(entry.ID)
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
	case teams.ToSeveral:
		// A member named among several is told it as its own line; a manager
		// never reads its own messages back.
		return ""
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

// teamDigest is one digest per managed team, "" for a conversation that
// manages none. Who the manager is and what it does is not here: that is the
// role note's, which cannot arrive late ([teamRoleBlock]).
func (a *Agent) teamDigest(profile string) string {
	a.team.mu.Lock()
	roles := a.teamRolesLocked(profile)
	keys := append([]string(nil), a.teamKeysLocked()...)
	file := a.team.file
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
	// THE FILE THE ROLES CAME OFF, not a second load: the role read above
	// stats the teams file and reads it only when it moved.
	if file == nil {
		return ""
	}
	now := time.Now()
	var parts []string
	for _, role := range managed {
		team, ok := file.Team(role.id)
		if !ok {
			continue
		}
		team = managedView(file, team)
		log, _ := teams.ReadTraffic(profile, role.id, "", teamStateLook)
		states := memberStates(team, keys, now, log, &a.team.journals)
		markShared(file, team, states)
		parts = append(parts, teams.Digest(team, states, recentOf(log), teamDigestBudget))
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
//
// A MEMBER'S JOURNAL IS READ ONLY WHEN IT MOVED: journals keeps what each said
// against its stamp, and a nil one reads every time.
func memberStates(team teams.Team, self []string, now time.Time, log []teams.Entry, journals *journalCache) map[string]teams.MemberState {
	states := make(map[string]teams.MemberState, len(team.Members))
	for _, member := range team.Members {
		if teamHoldsKey(self, member.Key) {
			states[member.Key] = teams.MemberState{State: teams.StateRunning}
			continue
		}
		if state, last, ok := journals.stateOf(member.File, now); ok {
			states[member.Key] = askingFromEvents(state, last, member, log, now)
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
	facts, ok := readJournalFacts(path)
	if !ok {
		return teams.MemberState{}, time.Time{}, false
	}
	return facts.state(now), facts.last, true
}

// journalFacts is what a member's journal tail says, before the clock is
// asked: everything [journalState] needs that does not move while the file
// does not. It is what a cache keeps per file (teamcache.go), so a manager's
// turn that finds a member's journal unmoved reads nothing.
type journalFacts struct {
	// last is the newest line's instant, the file's modification time when no
	// line carried one.
	last time.Time
	// asking is a question the member called `ask` for and has no answer to,
	// with its head, "" when it gave none.
	asking   bool
	question string
	// failed says a turn's last word was an error or a failure line; spoke says
	// somebody spoke after the last turn's pace line.
	failed bool
	spoke  bool
	// files is the newest [journalFiles] files touched, newest first.
	files []string
}

// memberJournalLine is the few fields of a journal line a state is read from. The
// rest of each line (the message's content, its reasoning) is skipped rather
// than decoded.
type memberJournalLine struct {
	Type       string        `json:"type"`
	Role       string        `json:"role"`
	Timestamp  string        `json:"timestamp"`
	ToolCalls  []ai.ToolCall `json:"toolCalls"`
	ToolCallID string        `json:"toolCallId"`
}

// readJournalFacts reads the last [journalTail] bytes of a member's journal.
// The boolean is false for a file that cannot be read.
func readJournalFacts(path string) (journalFacts, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return journalFacts{}, false
	}
	lines, mod, err := journalTailLines(path, journalTail)
	if err != nil {
		return journalFacts{}, false
	}
	var (
		last     time.Time
		spokeAt  = -1
		endedAt  = -1
		failedAt = -1
		pending  []string // the ids of asks with no answer yet, in order
		heads    = map[string]string{}
		files    []string
	)
	for index, raw := range lines {
		var entry memberJournalLine
		if json.Unmarshal(raw, &entry) != nil {
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
						pending = append(pending, call.ID)
						heads[call.ID] = strings.TrimSpace(args.Head)
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
				delete(heads, entry.ToolCallID)
			}
		}
	}
	if last.IsZero() {
		last = mod
	}
	facts := journalFacts{
		last:   last,
		failed: failedAt > endedAt && failedAt > spokeAt,
		spoke:  spokeAt > endedAt,
		files:  newestFirst(files, journalFiles),
	}
	for _, id := range pending {
		head, open := heads[id]
		if !open {
			continue
		}
		facts.asking = true
		if head != "" {
			facts.question = head
		}
	}
	return facts, true
}

// state is the facts read against the clock: a turn with no ending that has
// gone quiet for [journalStale] is over.
func (f journalFacts) state(now time.Time) teams.MemberState {
	state := teams.MemberState{SinceActive: now.Sub(f.last), Files: f.files}
	if state.SinceActive <= 0 {
		state.SinceActive = time.Second
	}
	switch {
	case f.asking:
		state.State = teams.StateAsking
		state.Question = f.question
	case f.failed:
		state.State = teams.StateFailed
	case f.spoke && now.Sub(f.last) < journalStale:
		state.State = teams.StateRunning
	default:
		state.State = teams.StateIdle
	}
	return state
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

// ── the role ────────────────────────────────────────────────────────────────

// teamRoleNoteOpening is the first line of the note that says what this
// conversation is in its teams.
//
// IT IS AN INSTRUCTION AND SAYS SO. The digest's opening calls its note facts
// and not requests, which is right for a list of who is doing what and wrong
// for "you are this team's manager": a model told that its role is a fact
// beside the work, and not a request, weighs it like one, and the manager found
// on the ordinary launch answered "what is happening here" as an ordinary chat
// about its folder. So the role rides its own opening, in codeaf's voice.
const teamRoleNoteOpening = "Instructions from codeaf, not from the person: your part in a team. They hold until a later note that opens this way replaces them."

// teamRoleWithdrawn is the role note for a conversation that had a part in a
// team and has none now. An earlier role note is never taken out of the
// transcript (the append law), so the change is said at the tail instead.
const teamRoleWithdrawn = "You are no longer the manager or a member of any team. What earlier notes that opened this way said no longer holds, and a team verb you still carry will refuse."

// teamManagerLaws is how a manager works, stated once in the role. The verbs'
// own descriptions carry how each is called.
const teamManagerLaws = "Seven laws:\n" +
	"1. Hand real work to members with team_send, or team_start for a new member, rather than doing it yourself, and keep track with team_status and team_read. Asked what is happening, answer from the team.\n" +
	"2. The person outranks you: what they say in a member's own conversation stands over your directive, and a conflict goes to them.\n" +
	"3. You cannot answer a member's permission prompt; tell the person it is waiting.\n" +
	"4. You direct only the members who report to you, one level down: a team under yours is its manager's to run, so you direct that manager, never its members. A member team_status marks `reports to` another team is shared: read it and send it notes, never a directive or a stop.\n" +
	"5. Members' questions come to you as decision packets: answer with team_decide, or send one up with team_escalate when it is not yours to decide. Your own questions go up the same way.\n" +
	"6. A team's daily cap is the person's: you cannot raise it, and at the cap nothing new starts until they answer.\n" +
	"7. A conflict between conversations is declared with team_raise and goes to the lowest manager above every party; when it waits on you, your team_decide reaches every party as a directive."

// teamRosterMax is how many members the role names before it sends the model
// to team_status for the rest, and teamRosterWord how much of each member's
// title it quotes.
const (
	teamRosterMax  = 12
	teamRosterWord = 60
)

// teamRoleBlock is what this conversation is in its teams, one paragraph per
// team it manages or is a managed member of, "" for none. A member of a team
// with no manager has no part to be told: it has no verb and nobody to report
// to.
//
// It is composed off the teams file the boundary has just read, under the
// seat's lock, so it costs no disk of its own.
func teamRoleBlock(roles []teamRole, file *teams.File) string {
	var parts []string
	for _, role := range roles {
		switch {
		case role.manager:
			parts = append(parts, teamManagerRole(role, file))
		case role.managed:
			parts = append(parts, teamMemberRole(role))
		}
	}
	return strings.Join(parts, "\n\n")
}

func teamManagerRole(role teamRole, file *teams.File) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are the manager of the team %q. The person talks to you and you run the team for them: you decide who does what, hand the work out, and tell the person where it stands.\n", role.name)
	b.WriteString(teamRoster(role, file))
	b.WriteString("\n")
	if scope := teamManagerScope(role, file); scope != "" {
		b.WriteString(scope)
		b.WriteString("\n")
	}
	b.WriteString(teamManagerDelivery(role))
	b.WriteString("\n")
	b.WriteString(teamManagerLaws)
	return b.String()
}

// teamManagerScope is where the manager's team sits in the tree, "" for a
// team alone at the top with nothing under it: whom it reports to (a
// sub-team's manager reports one level up; a top-level team's to the global
// manager when there is one), what runs under it, and, for the global manager,
// what its members are.
func teamManagerScope(role teamRole, file *teams.File) string {
	if file == nil {
		return ""
	}
	var parts []string
	if role.root {
		parts = append(parts, fmt.Sprintf("You are the global manager: %q holds every team, and your members are the managers of the top-level teams, never their members. "+
			"Direct those managers and they pass it on. Their questions and conflicts between teams come to you before the person. team_start with kind team starts a new top-level team.", role.name))
	} else if home, ok := file.Home(role.key); ok && home.Team != role.id {
		boss := "its manager"
		if t, ok := file.Team(home.Team); ok {
			if m, ok := t.Member(t.Manager); ok && m.Handle != "" {
				boss = "its manager, @" + m.Handle
			}
			what := "a team under"
			if t.Root {
				what = "a top-level team, under the global manager of"
			}
			parts = append(parts, fmt.Sprintf("Your team is %s %q: you report to %s. Post to it with team_post; your questions go to it before the person.", what, t.Name, boss))
		}
	}
	var under []string
	for _, child := range file.Children(role.id) {
		if child.Closed() || role.root {
			continue
		}
		who := "no manager yet"
		if m, ok := child.Member(child.Manager); ok && m.Handle != "" {
			who = "run by @" + m.Handle
		}
		under = append(under, fmt.Sprintf("%q (%s)", child.Name, who))
	}
	if len(under) > 0 {
		parts = append(parts, "Teams under yours: "+strings.Join(under, ", ")+". Direct their managers, never their members.")
	}
	return strings.Join(parts, " ")
}

// teamManagerDelivery is what happens to what the manager sends and what comes
// back, said as it is on this team: with the auto-wake on, a directive starts
// an idle member and replies start the manager (team_wakewatch.go); with it
// off, everything waits for the next turn each conversation takes.
func teamManagerDelivery(role teamRole) string {
	if !role.wakes {
		return "This team's auto-wake is off: what you send, and your members' replies, wait for each conversation's next turn, so an idle member does not start on a directive until it next runs."
	}
	return "A directive (team_send kind directive) starts an idle member's turn; a note waits for its next turn. Members' replies to you, and their finishing, failing or asking, come back to you and start your turn when you are idle, so do not wait or poll for them."
}

// teamRoster names the manager's members by handle, with the start of each
// one's title.
func teamRoster(role teamRole, file *teams.File) string {
	if file == nil {
		return "team_status lists its members."
	}
	team, ok := file.Team(role.id)
	if !ok {
		return "team_status lists its members."
	}
	team = managedView(file, team)
	var named []string
	for _, member := range team.Members {
		if member.Key == role.key {
			continue
		}
		who := "a member with no handle yet"
		if member.Handle != "" {
			who = "@" + member.Handle
		}
		if word := strings.TrimSpace(member.Word); word != "" {
			who += " (" + cutRunesTeam(word, teamRosterWord) + ")"
		}
		named = append(named, who)
	}
	if len(named) == 0 {
		return "It has no members but you yet; team_start opens one."
	}
	more := ""
	if len(named) > teamRosterMax {
		more = fmt.Sprintf(", and %d more that team_status lists", len(named)-teamRosterMax)
		named = named[:teamRosterMax]
	}
	return "Its members: " + strings.Join(named, ", ") + more + "."
}

func teamMemberRole(role teamRole) string {
	you := "a member"
	if role.handle != "" {
		you = "@" + role.handle
	}
	if role.root {
		return fmt.Sprintf("You are %s in %q because you manage a top-level team: you report to its manager, the global manager. Its directives reach you marked \"◆ from manager\", "+
			"your questions go to it before the person, and you report to it with team_post to the manager.", you, role.name)
	}
	if role.boss != "" && role.boss != role.id && !role.shared {
		said := fmt.Sprintf("You are %s in the team %q, which has no manager of its own, so you answer to the manager of %q: team_post to the manager reaches it, and to the room or a teammate stays in %q.",
			you, role.name, role.bossName, role.name)
		if role.questionsUp {
			said += " Your clarifying questions (ask) go to that manager first, and its answer comes back marked \"◆ answered\"; permission prompts still go to the person."
		}
		return said
	}
	if role.shared {
		return fmt.Sprintf("You are %s in the team %q too, but you report to the manager of %q: that manager's word directs you, and this team's manager may only send you notes (fyi). "+
			"Post to this team with team_post.", you, role.name, role.reportsTo)
	}
	said := fmt.Sprintf("You are %s in the team %q, which has a manager. "+
		"Lines from the manager arrive marked \"◆ from manager\" and from teammates \"from @handle\"; none of them is the person, whose own words outrank the manager's. "+
		"Report progress, findings and blockers with team_post, to the manager, a teammate or the room. A conflict you cannot settle with another member or team goes up with team_raise.", you, role.name)
	if role.questionsUp {
		said += " Your clarifying questions (ask) go to your manager first, and its answer comes back marked \"◆ answered\"; permission prompts still go to the person."
	}
	return said
}

// setTeamRole leaves the role where the next landing of the session's notes
// will carry it ([Agent.landTeamRoleLocked]).
func (a *Agent) setTeamRole(role string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	a.teamRoleText = role
}

// landTeamRoleLocked lands the role note when the role has moved since the last
// one, and the withdrawal when a conversation that had a role has none. A
// conversation that never had one lands nothing, which is every conversation
// in no team.
func (a *Agent) landTeamRoleLocked() {
	block := strings.TrimSpace(a.teamRoleText)
	if block == "" {
		last := a.lastNoteLocked(teamRoleNoteOpening)
		if last == "" || last == teamRoleNoteOpening+"\n\n"+teamRoleWithdrawn {
			return
		}
		block = teamRoleWithdrawn
	}
	a.landNoteLocked(teamRoleNoteOpening, block)
}

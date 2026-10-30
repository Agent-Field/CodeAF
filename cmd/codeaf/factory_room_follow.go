package main

// factory_room_follow.go — a factory job's crew follows the job.
//
// A JOB'S ROOM CLOSES WHEN THE JOB IS OVER. Every factory job makes a crew, and
// a job that is over never needs it open again: left open, every crew list,
// every write and every watch carried it for good, and a store a year old held
// thousands of them. A job is over on a fact that does not change by itself
// ([jobOver]): an end recorded on it (answered, dropped, closed locally), a
// dismissal, or its pull request merged or closed. An approved job still
// publishing is not over. Its room is then closed quietly, without a report,
// a traffic line or any change to who reports to whom
// ([teams.File.CloseForJob]). A closed room stays: the job's page, its crew
// tab and its history read it by id; the job's steps come from the job's own
// record and are drawn as before.
//
// THE JOB COMING BACK REOPENS IT. When the job stops being over, whether a
// person brought it back or it came back by itself (a pull request reopened
// on the forge), its room opens again with its reporting as it was, so its
// next round joins its crew. Only a room its job's end closed is reopened: a
// crew a person closed or archived by hand stays as they left it.
//
// THE ROOM FOLLOWS THE JOB'S STATE, NOT ITS LAST WRITE. The store tells of a
// write after the item's lock is let go, so two writes may be told in either
// order, and the crews store may be busy: each telling reads the job again and
// makes the room match it, and one the crews store refused is tried again. A
// round that starts also reopens a room its job's end closed
// ([factoryChatStage]), so a live job never runs teamless for a reopen that
// was lost.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/teams"
)

func init() { store.WhenWritten(factoryRoomHook) }

// factoryRoomHook is what the store tells of every item write: a write that
// takes a job across its end makes its room follow.
func factoryRoomHook(st *store.Store, before, after factory.Item) {
	if jobOver(before) != jobOver(after) {
		factoryRoomFollow(factoryProfile(config.ProfileDir()), st, after)
	}
}

// jobOver reports whether job it is over for its room: an end recorded on it,
// a dismissal, or its pull request merged or closed. [factory.JobState] also
// calls a shipped job with no pull request done, which an approved job is
// until its publish records one; that is not an end.
func jobOver(it factory.Item) bool {
	if it.DoneHow != "" || it.State == factory.StateDismissed {
		return true
	}
	pr := it.PullRequest
	return pr != nil && (pr.State == factory.PRMerged || pr.State == factory.PRClosed)
}

// roomRetries is how often, and how far apart, a room the crews store
// refused to follow its job is tried again.
var roomRetries = []time.Duration{time.Second, 5 * time.Second, 30 * time.Second, 2 * time.Minute}

// factoryRoomFollow makes job it's room match the job as the store now holds
// it, and tries again later when the crews store refused.
func factoryRoomFollow(profile string, st *store.Store, it factory.Item) {
	follow := func() error {
		if now, err := st.Get(it.ID); err == nil {
			it = now
		}
		if jobOver(it) {
			return closeJobRooms(profile, nil, it)
		}
		return reopenJobRoom(profile, it)
	}
	if err := follow(); err == nil || !errors.Is(err, teams.ErrBusy) {
		return
	}
	guard.Go("factory/room-follow", func() {
		for _, wait := range roomRetries {
			time.Sleep(wait)
			if err := follow(); err == nil || !errors.Is(err, teams.ErrBusy) {
				return
			}
		}
	})
}

// jobRoomRecorded is the room job it recorded, "" for none.
func jobRoomRecorded(it factory.Item) string {
	if it.Stream == nil {
		return ""
	}
	return strings.TrimSpace(it.Stream.Room)
}

// jobTalkKey is the conversation key of job it's own conversation, "" for none.
func jobTalkKey(it factory.Item) string {
	key := strings.TrimSpace(it.Talk)
	if key == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(key); err == nil {
		key = real
	}
	return filepath.Clean(key)
}

// closeJobRooms closes the open room of each job in one write, and leaves
// alone any room keep holds back, asked with the open crews in hand (nil holds
// none back). ONLY WHAT THE FACTORY
// RECORDED IS CLOSED: the job's own Stream.Room, else a factory-made item
// team holding the job's conversation. An ordinary crew that holds the same
// conversation, the root and the `factory` parent are the person's, and stay
// open. A recorded room that is here but not open (closed already, or a crew
// that is not the factory's) is not looked for any other way.
func closeJobRooms(profile string, keep func(f *teams.File, room teams.Team) bool, jobs ...factory.Item) error {
	var recorded []string
	for _, it := range jobs {
		if room := jobRoomRecorded(it); room != "" {
			recorded = append(recorded, room)
		}
	}
	here := map[string]bool{}
	if len(recorded) > 0 {
		found, err := teams.LoadTeams(profile, recorded...)
		if err != nil {
			return err
		}
		for _, t := range found.Teams {
			here[t.ID] = true
		}
	}
	return teams.UpdateOpen(profile, func(f *teams.File) error {
		closable := func(t teams.Team) bool {
			return !t.Closed() && !t.Root && !factoryIsParent(f, t.ID) && (keep == nil || !keep(f, t))
		}
		now := time.Now()
		for _, it := range jobs {
			room := jobRoomRecorded(it)
			if t, ok := f.Team(room); ok && room != "" {
				if closable(t) {
					if err := f.CloseForJob(t.ID, now); err != nil {
						return err
					}
				}
				continue
			}
			if room != "" && here[room] {
				continue
			}
			key := jobTalkKey(it)
			if key == "" {
				continue
			}
			for _, t := range f.Teams {
				if t.Holds(key) && t.FactoryManaged && t.Parent != "" && closable(t) {
					if err := f.CloseForJob(t.ID, now); err != nil {
						return err
					}
					break
				}
			}
		}
		return nil
	})
}

// reopenJobRoom opens again the room job it's end closed: its recorded room,
// else the factory-made item team holding its conversation that its end
// closed. A room closed or archived by a person, or closed for any other
// reason, is left ([teams.File.ReopenForJob]). The rooms are looked at by id
// first, and the store is written, with every team in hand as a reopen needs,
// only when one is to be reopened.
func reopenJobRoom(profile string, it factory.Item) error {
	room, key := jobRoomRecorded(it), jobTalkKey(it)
	var candidates *teams.File
	var err error
	switch {
	case room != "":
		candidates, err = teams.LoadTeams(profile, room)
	case key != "":
		candidates, err = teams.LoadTeamsOf(profile, teams.ConversationName(key), false)
	default:
		return nil
	}
	if err != nil {
		return err
	}
	due := ""
	for _, t := range candidates.Teams {
		if (room != "" && t.ID == room || room == "" && t.Holds(key)) && t.FactoryManaged && t.Closed() && t.JobEnded && !t.Archived() {
			due = t.ID
			break
		}
	}
	if due == "" {
		return nil
	}
	return teams.Update(profile, func(f *teams.File) error {
		if t, ok := f.Team(due); !ok || t.Root || factoryIsParent(f, due) {
			return nil
		}
		return f.ReopenForJob(due)
	})
}

// sweptMarker is the profile file that says [factorySweepEndedRooms] ran.
const sweptMarker = "factory-rooms-swept"

// factorySweepEndedRooms closes, once per profile, the rooms of the jobs that
// were already over before rooms followed their jobs, and answers how many
// jobs it looked at (0 when it had run already). A room a person may have
// reopened is left open ([personMayHaveReopened]). It marks the profile once
// it has run, and never runs again there.
//
// TODO(crews-store): a one-time fix for stores from before 2026-10-29, when an
// ended job's room stayed open. Remove once every developer's store has been
// swept; nothing else calls it.
func factorySweepEndedRooms(profile, root string) int {
	marker := config.ProfilePath(profile, sweptMarker)
	if _, err := os.Stat(marker); err == nil {
		return 0
	}
	st, err := store.Open(root)
	if err != nil {
		return 0
	}
	items, err := st.List()
	if err != nil {
		return 0
	}
	var over []factory.Item
	for _, it := range items {
		if jobOver(it) {
			over = append(over, it)
		}
	}
	reopened := personMayHaveReopened(profile)
	if len(over) > 0 {
		if err := closeJobRooms(profile, reopened, over...); err != nil {
			return len(over)
		}
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0o700); err == nil {
		_ = os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
	}
	return len(over)
}

// firstTrafficID is the id of the first line any crew's log holds.
var firstTrafficID = fmt.Sprintf("%012d", 1)

// personMayHaveReopened answers, for the sweep, whether a person may have
// reopened a room: a reopen line, written after the room was made, in its own
// log or in the log of any crew above it (a crew's reopen opens again what its
// close closed, and says so only in its own log); or such a log whose first
// lines rotated out after the room was made, which may have held one; or a
// crew above it that is not open, or a log that does not read. WHEN UNSURE THE
// ROOM IS LEFT OPEN: the sweep is only a tidy-up. Each crew's log is read once.
func personMayHaveReopened(profile string) func(f *teams.File, room teams.Team) bool {
	type look struct {
		reopens []time.Time
		oldest  time.Time
		lost    bool
		err     error
	}
	logs := map[string]look{}
	read := func(id string) look {
		if l, ok := logs[id]; ok {
			return l
		}
		var l look
		lines, err := teams.ReadTraffic(profile, id, "", 0)
		l.err = err
		if len(lines) > 0 {
			l.oldest, l.lost = lines[0].At, lines[0].ID != firstTrafficID
		}
		for _, e := range lines {
			if e.Kind == teams.KindReopen {
				l.reopens = append(l.reopens, e.At)
			}
		}
		logs[id] = l
		return l
	}
	return func(f *teams.File, room teams.Team) bool {
		chain := append([]teams.Team{room}, f.Ancestors(room.ID)...)
		if top := chain[len(chain)-1]; top.Parent != "" {
			return true
		}
		for _, t := range chain {
			l := read(t.ID)
			if l.err != nil || l.lost && !l.oldest.Before(room.Made) {
				return true
			}
			for _, at := range l.reopens {
				if !at.Before(room.Made) {
					return true
				}
			}
		}
		return false
	}
}

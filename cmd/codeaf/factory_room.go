package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// factoryRoomOwned requires local origin, not a server-supplied id or a crew name.
// A legacy local room can corroborate that origin through its actual conversations.
func factoryRoomOwned(f *teams.File, it factory.Item, room string) bool {
	t, ok := f.Team(room)
	if !ok || t.Root || t.Closed() || factoryIsParent(f, room) {
		return false
	}
	if t.FactoryManaged {
		return true
	}
	if it.Stream == nil {
		return false
	}
	s := *it.Stream
	s.Room = ""
	it.Stream = &s
	c, proved := legacyRoomClaimFor(f, it)
	return proved && c.Room == room
}

// factoryEnsureRoom recreates the logical job room on a new worker machine.
// An ordinary local crew with the same id is never adopted. A collision leaves
// this round teamless; it never clears or replaces the factory's logical id.
func factoryEnsureRoom(profile string, it factory.Item, room string) (string, error) {
	room = strings.TrimSpace(room)
	if !factoryrun.ValidRoom(room) {
		return "", nil
	}
	owned := ""
	err := teams.Update(profile, func(f *teams.File) error {
		if _, exists := f.Team(room); exists {
			if factoryRoomOwned(f, it, room) {
				for i := range f.Teams {
					if f.Teams[i].ID == room {
						f.Teams[i].FactoryManaged = true
					}
				}
				owned = room
			}
			return nil
		}
		now := time.Now()
		parent := factoryParentTeam(f, now)
		f.Teams = append(f.Teams, teams.Team{ID: room, Name: talkTeamName(it), Parent: parent, Made: now, FactoryManaged: true})
		owned = room
		return nil
	})
	return owned, err
}

// ── A DROP DELETES THE JOB'S CONVERSATIONS ─────────────────────────────────
//
// A drop throws the job's work away (internal/factory/serve's drop), and what
// was said about it goes with it: the crew the factory made for it and the
// conversations the factory recorded for it, on this machine. Bring back then
// brings the job back fresh, and deletes again whatever a job dropped before
// this still names. EXACTLY THOSE AND NOTHING FOUND BY A NAME OR A PATTERN:
//
//   - the crew is the job's recorded Stream.Room when it is the factory's own
//     here ([factoryRoomOwned], or stamped before), else the factory-made item
//     team that holds the job's recorded conversation ([talkPutAway]'s rule);
//     the root, the one `factory` team and a person's crew are never it;
//   - the conversations are the job's agent (Item.Talk), each step's latest
//     chat (Phase.Chat) and each of its finished rounds' (RoundLine.Chat),
//     and every member of that crew the factory stamped as a step when it
//     joined (Member.FactoryStage), which is every round's.
//
// A conversation a person put in that crew themselves is not the job's: it
// loses the crew and keeps everything else. Another job's crew and
// conversations are never reached, because nothing here is read from anything
// but this job's own record.

// factoryDeleteJobChats is serve's DeleteChats on this machine: the teams of
// profileDir, the conversations under this machine's places root, and an owner
// this process holds stopped first.
func factoryDeleteJobChats(profileDir string) func(factory.Item) error {
	profileDir = factoryProfile(profileDir)
	return func(it factory.Item) error {
		return deleteJobChats(profileDir, session.PlacesRoot(), it, stopHeldConversation)
	}
}

// stopHeldConversation stops a conversation this process holds open before it
// is deleted: the engine's own, which is where the app holds a job's chats. A
// conversation another process holds is asked to let go by the delete itself
// (session's DeleteConversationUnder), and one nobody holds needs nothing.
func stopHeldConversation(file string) error {
	if proc := openedEngineProcess(); proc != nil {
		return proc.stopConversation(file)
	}
	return nil
}

// deleteJobChats deletes job it's crew, then its conversations, through the
// engine's own doors for each ([teams.Delete], [session.DeleteConversationUnder]).
// THE CREW GOES FIRST, so the job's agent leads nothing when it is deleted,
// and a conversation that cannot be deleted leaves no crew a fresh run could
// find. Every failure is joined into the one error, and the rest still go.
func deleteJobChats(profileDir, root string, it factory.Item, stop func(string) error) error {
	f, err := teams.Snapshot(profileDir)
	if err != nil {
		return err
	}
	room := jobRoomIn(f, it)
	chats := jobChatsIn(f, it, room)
	var failures []error
	if room != "" {
		if err := deleteJobRoom(profileDir, f, room); err != nil {
			failures = append(failures, err)
		}
	}
	for _, chat := range chats {
		err := session.DeleteConversationUnder(root, profileDir, chat, nil, stop)
		// A conversation already gone is what was asked for.
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, fmt.Errorf("%s: %w", filepath.Base(filepath.Dir(chat)), err))
		}
	}
	return errors.Join(failures...)
}

// jobRoomIn is job it's crew in f, by the job's own record, or "".
func jobRoomIn(f *teams.File, it factory.Item) string {
	ours := func(t teams.Team) bool {
		return !t.Root && !factoryIsParent(f, t.ID) && (t.FactoryManaged || factoryRoomOwned(f, it, t.ID))
	}
	if it.Stream != nil {
		if room := strings.TrimSpace(it.Stream.Room); room != "" {
			// A RECORDED ROOM THAT IS NOT THE FACTORY'S HERE IS NOT LOOKED FOR
			// ANY OTHER WAY: its crew is on the machine that ran it.
			if t, ok := f.Team(room); ok && ours(t) {
				return t.ID
			}
			return ""
		}
	}
	key := chatKey(it.Talk)
	if key == "" {
		return ""
	}
	for _, t := range f.Teams {
		if t.Holds(key) && t.FactoryManaged && t.Parent != "" && ours(t) {
			return t.ID
		}
	}
	return ""
}

// jobChatsIn is every conversation the factory recorded for job it, each once:
// its agent, its steps' latest chats and their rounds', and the steps stamped
// in its crew.
func jobChatsIn(f *teams.File, it factory.Item, room string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(file string) {
		if key := chatKey(file); key != "" && !seen[key] {
			seen[key] = true
			out = append(out, strings.TrimSpace(file))
		}
	}
	add(it.Talk)
	if it.Stream != nil {
		for _, ph := range it.Stream.Phases {
			add(ph.Chat)
			for _, round := range ph.Rounds {
				add(round.Chat)
			}
		}
	}
	if t, ok := f.Team(room); ok && room != "" {
		for _, m := range t.Members {
			if m.FactoryStage {
				if m.File != "" {
					add(m.File)
				} else {
					add(m.Key)
				}
			}
		}
	}
	return out
}

// chatKey is a conversation's identity as the teams file keys it: its
// transcript's path, resolved and cleaned; "" for none.
func chatKey(file string) string {
	file = strings.TrimSpace(file)
	if file == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(file); err == nil {
		file = real
	}
	return filepath.Clean(file)
}

// deleteJobRoom deletes the job's crew, and only it. A crew with another crew
// inside it, or one the teams file would not let go, is CLOSED INSTEAD, which
// keeps everything and can be undone, and which is enough: neither a fresh run
// nor the job's new agent ever joins a closed crew (itemTeamIn adopts only an
// open one by its name). The error says which happened.
func deleteJobRoom(profileDir string, f *teams.File, room string) error {
	why := errors.New("another crew is inside it")
	if len(f.Descendants(room)) == 0 {
		gone, err := teams.Delete(profileDir, room, []string{room})
		if err == nil {
			return nil
		}
		if len(gone) > 0 {
			return fmt.Errorf("the job's crew was deleted, but not its history: %w", err)
		}
		why = err
	}
	err := teams.Update(profileDir, func(f *teams.File) error {
		if t, ok := f.Team(room); !ok || t.Closed() {
			return nil
		}
		return f.Close(room, time.Now(), "")
	})
	if err != nil {
		return fmt.Errorf("the job's crew could not be deleted (%v) or closed: %w", why, err)
	}
	return fmt.Errorf("the job's crew was closed, not deleted: %w", why)
}

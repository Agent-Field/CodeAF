package main

// factory_room_follow.go — a factory job's crew follows the job.
//
// A JOB'S ROOM CLOSES WHEN THE JOB ENDS. Every factory job makes a crew, and a
// job that is over never needs it open again: left open, every crew list, every
// write and every watch carried it for good, and a store a year old held
// thousands of them. When a write takes a job to its end ([factory.JobState]
// "done": merged, closed, answered, dropped, closed locally), its room is
// closed quietly, the way a dismissed job's always was ([talkPutAway]): no
// closing report and no traffic line. A closed room stays. The job's page, its
// crew tab and its history read it by id; the job's steps come from the job's
// own record and are drawn as before.
//
// THE JOB COMING BACK REOPENS IT. When a write takes the job out of its end,
// whether a person brought it back or it came back by itself (a pull request
// reopened on the forge), its room opens again, so its next round joins its
// crew as before. Only a room its job's end closed is reopened
// ([teams.File.CloseForJob]): a crew a person closed or archived by hand stays
// as they left it, and no keeper or recovery path reopens a room.

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/teams"
)

func init() {
	store.WhenWritten(func(_ string, before, after factory.Item) {
		factoryRoomFollowsJob(factoryProfile(config.ProfileDir()), before, after)
	})
}

// factoryRoomFollowsJob closes job after's room when the write before→after
// ended the job, and reopens it when a person brought the job back.
func factoryRoomFollowsJob(profile string, before, after factory.Item) {
	switch {
	case factory.JobState(after) == "done" && factory.JobState(before) != "done":
		_ = closeJobRooms(profile, after)
	case factory.JobState(before) == "done" && factory.JobState(after) != "done":
		_ = reopenJobRoom(profile, after)
	}
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

// closeJobRooms closes the open room of each job in one write. ONLY WHAT THE
// FACTORY RECORDED IS CLOSED: the job's own Stream.Room, else a factory-made
// item team holding the job's conversation. An ordinary crew that holds the
// same conversation, the root and the `factory` parent are the person's, and
// stay open. A recorded room that is here but not open (closed already, or a
// crew that is not the factory's) is not looked for any other way.
func closeJobRooms(profile string, jobs ...factory.Item) error {
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
			return !t.Closed() && !t.Root && !factoryIsParent(f, t.ID)
		}
		now := time.Now()
		for _, it := range jobs {
			room := jobRoomRecorded(it)
			if t, ok := f.Team(room); ok && room != "" && closable(t) {
				if err := f.CloseForJob(t.ID, now); err != nil {
					return err
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
// closed. It reads every team, closed ones included, as a write that reopens
// one must; a room closed or archived by a person is left
// ([teams.File.ReopenForJob]).
func reopenJobRoom(profile string, it factory.Item) error {
	room, key := jobRoomRecorded(it), jobTalkKey(it)
	if room == "" && key == "" {
		return nil
	}
	return teams.Update(profile, func(f *teams.File) error {
		if t, ok := f.Team(room); ok && room != "" {
			if t.Root || !t.FactoryManaged || factoryIsParent(f, room) {
				return nil
			}
			return f.ReopenForJob(room)
		}
		for _, t := range f.Teams {
			if key != "" && t.Holds(key) && t.FactoryManaged && t.JobEnded && t.Parent != "" && !factoryIsParent(f, t.ID) {
				return f.ReopenForJob(t.ID)
			}
		}
		return nil
	})
}

// factorySweepEndedRooms closes the rooms of the jobs that had already ended
// before rooms followed their jobs, in one write, and answers how many jobs it
// looked at. It reads the floor's items once; a room already closed is left.
//
// TODO(crews-store): a one-time fix for stores from before 2026-10-29, when an
// ended job's room stayed open. Remove once every developer's store has been
// swept; nothing else calls it.
func factorySweepEndedRooms(profile, root string) int {
	st, err := store.Open(root)
	if err != nil {
		return 0
	}
	items, err := st.List()
	if err != nil {
		return 0
	}
	var ended []factory.Item
	for _, it := range items {
		if factory.JobState(it) == "done" {
			ended = append(ended, it)
		}
	}
	if len(ended) > 0 {
		_ = closeJobRooms(profile, ended...)
	}
	return len(ended)
}

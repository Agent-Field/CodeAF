package identity

import (
	"errors"
	"io/fs"
)

// Pristine says whether the home holds nothing but what starting codeaf makes:
// no chats, no workspaces, no synced cells and no secrets. Such a computer can
// join another identity without losing anything. When in doubt it answers no:
// an entry that [started] does not name counts as use.
func Pristine(home string) (bool, error) {
	used, err := started(home)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	return !used && err == nil, err
}

// started is the whole of what starting codeaf leaves in a home, read off a real
// run: it is the one place that says which files are made again on demand and
// which are the person's. Each name is a glob on one entry, and each entry
// carries its own judge, so a file that is sometimes made by a start and
// sometimes by use (a chat's transcript, the memory graph) is judged by what it
// holds rather than by its name.
var started = within(rules{
	File: never, DeviceFile: never, legacyKeyFile: never, soloFile: never,
	"config.json": never, "config.json.lock": never, "notices.json": never,
	"catalog.json": never, "model-catalog.json": never, "model-quirks.json": never,
	"credits.json": never, "logs": never, "bin": never, "cache": never,
	"chat.log": empty, "cas": empty,
	"graph.db": bareGraph, "graph.db-wal": never, "graph.db-shm": never,
	"pool": within(rules{
		"doc.json": never, "doc.sig": never, "install": never,
		"meta.json": never, "sweep-last.json": never, "outbox.jsonl": empty,
	}),
	"telemetry": within(rules{"first_run": never, "install_id": never, "notice_seen": never}),
	"v3": within(rules{
		"models.json": never, "lanes.json": never, "lanes.json.lock": never,
		"draft-*.json": untypedDraft, "hosts": never, "standing": never,
		"stores": within(rules{
			"sweep.stamp": never,
			"*":           within(rules{"budget.json": never, "budget.lock": never}),
		}),
		"projects": within(rules{"*": within(rules{"*": withinSession})}),
	}),
})

// withinSession is one chat folder: its bookkeeping is made again on demand and
// its transcript and memories are the chat itself.
var withinSession = within(rules{
	"meta.json": never, "presence.json": never,
	".cell": within(rules{
		"meta.json": never, "session.json": never, "env": empty,
		"memories.jsonl": empty, "transcript.jsonl": noTurns,
	}),
})

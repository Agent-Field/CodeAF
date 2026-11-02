package identity

import (
	"errors"
	"io/fs"
	"os"
)

// Pristine says whether the home holds nothing but what starting codeaf makes:
// no chats, no workspaces, no synced cells and no secrets. Such a computer can
// join another identity without losing anything. When in doubt it answers no:
// an entry that [started] does not name counts as use.
func Pristine(home string) (bool, error) {
	// K3: only a missing HOME is pristine. On Windows a file where a folder
	// belongs can also report path-not-found; that nested error must keep data.
	if _, err := os.Stat(home); errors.Is(err, fs.ErrNotExist) {
		return true, nil
	} else if err != nil {
		return false, err
	}
	used, err := started(home)
	return !used && err == nil, err
}

// started is the whole of what starting codeaf leaves in a home, read off a real
// run: it is the one place that says which files are made again on demand and
// which are the person's. Each name is a glob on one entry, and each entry
// carries its own judge, so a file that is sometimes made by a start and
// sometimes by use (a chat's transcript, the memory graph) is judged by what it
// holds rather than by its name.
var startupRules = rules{
	File: never, DeviceFile: never, legacyKeyFile: never, soloFile: never,
	"vault.enc.lock": never, "pair-sharing.pending": never,
	"config.json": never, "config.json.lock": never, "notices.json": never,
	// The pairing name prompt writes this before checking whether the home can join.
	"device-name":          never,
	"teams.json.lock":      never,
	"model-catalog-*.json": catalogCache, // Provider catalog caches are fetched again on demand.
	"knowledge": within(rules{
		".worker.lock":         never,
		"knowledge.sqlite":     knowledgeStartup,
		"knowledge.sqlite-wal": never, "knowledge.sqlite-shm": never,
	}),
	"catalog.json": never, "model-catalog.json": never, "model-quirks.json": never,
	"credits.json": never, "logs": never, "bin": never, "cache": never,
	"chat.log": never, "cas": empty,
	"graph.db": bareGraph, "graph.db-wal": never, "graph.db-shm": never,
	"pool": within(rules{
		"doc.json": never, "doc.sig": never, "install": never,
		"meta.json": never, "sweep-last.json": never, "outbox.jsonl": empty,
	}),
	"telemetry": within(rules{"first_run": never, "install_id": never, "notice_seen": never}),
	"v3": within(rules{
		"automations": within(rules{
			"automations.db":     automationStartup,
			"automations.db-wal": never, "automations.db-shm": never,
			"clock.lock": never, "clock.log": never, "environment.salt": never, "windows": never,
		}),
		"history.jsonl": onlyCommands, "models*.json": pickerCache, "lanes.json": never, "lanes.json.lock": never,
		"lanes.log": never, "lanes": never, // provider facts fetched from the model catalogue, fetched again on demand
		"draft-*.json": untypedDraft, "hosts": never, "standing": never,
		"stores": within(rules{
			"sweep.stamp": never,
			"*":           within(rules{"budget.json": never, "budget.lock": never}),
		}),
		"projects": within(rules{"*": within(rules{"*": withinSession})}),
	}),
}

var started = within(startupRules)

// withinSession is one chat folder: its bookkeeping is made again on demand and
// its transcript and memories are the chat itself.
var withinSession = within(rules{
	"meta.json": never, "meta.lock": never, "presence.json": never,
	".cell": within(rules{
		"meta.json": never, "session.json": never, ".furrowpolicy": never, "env": empty,
		"memories.jsonl": empty, "transcript.jsonl": noTurns,
	}),
})

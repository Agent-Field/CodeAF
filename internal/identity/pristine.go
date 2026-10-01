package identity

import (
	"errors"
	"io/fs"
	"os"
)

// incidental is what a computer holds after it has only been started: its own
// keys, settings and caches that are made again on demand. Chats, workspaces,
// synced cells, secrets and rotation history are never in it, and neither is
// any name this list does not know, so an unknown file counts as use.
var incidental = map[string]bool{
	File: true, DeviceFile: true, legacyKeyFile: true,
	"config.json": true, "config.json.lock": true, "notices.json": true,
	"catalog.json": true, "model-catalog.json": true, "model-quirks.json": true,
	"logs": true, "bin": true, "cache": true,
}

// Pristine says whether the home holds nothing but what starting codeaf makes:
// no chats, no workspaces, no synced cells and no secrets. Such a computer can
// join another identity without losing anything. When in doubt it answers no.
func Pristine(home string) (bool, error) {
	entries, err := os.ReadDir(home)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if !incidental[e.Name()] {
			return false, nil
		}
	}
	return true, nil
}

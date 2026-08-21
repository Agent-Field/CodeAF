package standing

// inbox.go is how news reaches a conversation whose window is not open. It is
// deliberately the simplest thing that works: one JSONL file inside the session
// folder, appended by whoever has news, drained whole the next time the person
// opens that conversation and shown under one "while you were away" fold.
//
// THE DRAIN RENAMES BEFORE IT READS. A note delivered while the fold is being
// built would otherwise be read and then deleted unseen; moving the file aside
// first means a racing delivery starts a fresh inbox that the next open finds.

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"time"
)

// Deliver appends a note to a session's inbox.
func Deliver(sessionDir string, note Note) error {
	if note.At.IsZero() {
		note.At = time.Now()
	}
	line, err := json.Marshal(note)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(InboxPath(sessionDir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(line); err != nil {
		return err
	}
	return file.Close()
}

// Drain reads and removes a session's inbox, oldest first. An absent inbox is
// an empty slice and no error.
func Drain(sessionDir string) ([]Note, error) {
	path := InboxPath(sessionDir)
	staged := path + "." + newID() + ".draining"
	if err := os.Rename(path, staged); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer os.Remove(staged)
	file, err := os.Open(staged)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var notes []Note
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var note Note
		if err := json.Unmarshal(raw, &note); err != nil {
			continue
		}
		notes = append(notes, note)
	}
	if err := scanner.Err(); err != nil {
		return notes, err
	}
	// The file is already in the order it was written; the sort only matters
	// when two writers interleaved, and a stable sort keeps that order for the
	// notes that share a moment.
	sort.SliceStable(notes, func(a, b int) bool { return notes[a].At.Before(notes[b].At) })
	return notes, file.Close()
}

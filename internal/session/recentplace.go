// recentplace.go lists the conversations a directory holds when that directory
// may hold them in EITHER shape.
//
// A v3 session is a folder (place.go, and docs/CHAT-V3.md's Decision 26), and
// every session written before that decision is a flat `<stem>.jsonl` beside
// its siblings. Both are real on the same disk for as long as the migration
// takes, and a picker that could read only one of them would be a picker that
// loses a person's work on the day they upgrade — so this reads both and says
// nothing about which was which.
//
// It is [Recent] with two differences and no third:
//
//   - A FOLDER IS A CANDIDATE WHEN IT HOLDS A TRANSCRIPT, and the path answered
//     for it is that transcript, because a transcript is what a caller opens.
//   - THE FOLDER'S OWN RECORD OUTRANKS THE JOURNAL. meta.json is written for
//     exactly this reading (place.go's [Meta]): its title is the name the
//     session settled on, and its last-user stamp is the ordering law — a
//     background write touching a file is not a person returning to a
//     conversation, which is why neither the file's modification time nor the
//     newest line in it is allowed to decide the order.
//
// Recent is left standing rather than taught the second shape, because it is
// the FLAT layout's reader and dies with it: one function that grew a branch
// for each layout would be one function two waves have to agree about.
package session

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RecentSessions is the directory's conversations, newest first, in both
// layouts. It is bounded exactly as [Recent] is: limit is what the caller
// wants, and peekBudget is how many transcripts will be read to find them.
func RecentSessions(dir string, limit int) []Summary {
	if limit <= 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type candidate struct {
		file string
		meta Meta
		at   time.Time
	}
	files := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() {
			if !strings.HasSuffix(name, ".jsonl") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			files = append(files, candidate{file: filepath.Join(dir, name), at: info.ModTime()})
			continue
		}
		// A DIRECTORY IS A SESSION WHEN IT HOLDS A TRANSCRIPT, asked through
		// [Place] so that where a transcript lives inside a folder is decided in
		// one file. Everything else under the v3 home — locks, the index, a
		// directory somebody made by hand — simply has no transcript in it.
		place := Place{Dir: filepath.Join(dir, name)}
		info, err := os.Stat(place.Transcript())
		if err != nil || info.IsDir() {
			continue
		}
		meta, _ := LoadMeta(place.Dir)
		at := meta.LastUserAt
		if at.IsZero() {
			at = info.ModTime()
		}
		files = append(files, candidate{file: place.Transcript(), meta: meta, at: at})
	}
	// The cheap approximation of "newest" decides which files are worth opening,
	// and what the files themselves said decides the answer's order.
	sort.Slice(files, func(i, j int) bool { return files[i].at.After(files[j].at) })

	found := make([]Summary, 0, limit)
	for read, file := range files {
		if len(found) >= limit || read >= peekBudget {
			break
		}
		summary, ok := Peek(file.file)
		if !ok {
			continue
		}
		// Peek records the path it opened. The listing sets it again so the
		// path a mention resolves is the transcript this walk opened.
		summary.File = file.file
		if title := strings.TrimSpace(file.meta.Title); title != "" {
			summary.Title = title
		}
		if !file.meta.LastUserAt.IsZero() {
			summary.At = file.meta.LastUserAt
		}
		if summary.At.IsZero() {
			summary.At = file.at
		}
		found = append(found, summary)
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].At.After(found[j].At) })
	return found
}

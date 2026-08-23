package substore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// Memory is one subharness's accumulated domain notes — the backing for the
// runtime's remember() and recall(), which are exec.Env.Remember and
// exec.Env.Recall.
//
// IT IS SCOPED TO THE NAME AND NOT TO THE VERSION, and that is a decision worth
// being explicit about because PRD §6 draws memory.md inside the version
// directory. Both halves of that are kept and neither is bent: the version's
// memory.md is the SEED the bundle was minted with, immutable like everything
// else in a version page, and the file a run actually appends to lives one level
// up, beside the versions. Anything else would make a version mutable — which is
// the rule the whole store is built on — or would throw a subharness's learning
// away every time somebody minted a new version of it, which is exactly the
// thing accumulated domain notes exist not to do.
//
// The file is markdown because a person reads it. It is the same file whether it
// was written by a run, by an editor, or by a `git pull` over a project store.
//
// [Memory.Remember] and [Memory.Recall] TAKE A CONTEXT THEY BARELY USE, and
// that is deliberate: they are exec.Env.Remember and exec.Env.Recall, spelled
// the way the contract spells them, so the runtime hands this value straight to
// a bundle instead of wrapping it in an adapter whose only job is to drop an
// argument. An interface a store satisfies by having the methods is one nobody
// has to keep in sync.
type Memory struct {
	path string
	// seed is the head version's memory.md, copied in the first time this file is
	// touched and never again. A subharness shipped with domain notes starts with
	// them; one that was not starts empty.
	seed func() []byte
}

// Memory opens one subharness's memory door.
//
// It is safe to open for a name with no versions and no directory — the file is
// created on the first note, not here, so recalling from a subharness nobody has
// run is an empty answer rather than a mutation.
func (s *Store) Memory(name string) *Memory {
	return &Memory{
		path: s.memoryPath(name),
		seed: func() []byte {
			head, err := s.Head(name)
			if err != nil {
				return nil
			}
			data, err := os.ReadFile(filepath.Join(s.VersionDir(name, head), MemoryFile))
			if err != nil {
				return nil
			}
			return data
		},
	}
}

// Path names the file, for a surface that wants to open it in an editor.
func (m *Memory) Path() string { return m.path }

// Remember appends one note, stamped.
//
// APPEND, NEVER REWRITE. A subharness's memory is a log of what it learnt and
// when, and the only operation on it is adding to the end — there is no door here
// that edits or forgets, because a program that could quietly rewrite its own
// history is a program whose recall nobody can trust. A person editing the file
// is a different matter: it is theirs, it is markdown, and nothing here objects.
func (m *Memory) Remember(ctx context.Context, note string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return nil
	}
	if err := m.plant(); err != nil {
		return err
	}
	// A subharness may remember something before it has a directory here at all —
	// a Go-native one has no bundle on disk and never will, and its notes belong
	// beside everybody else's.
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(m.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	// One note is a heading with the time in it and the note under it. The
	// heading is what [Memory.Recall] reads the stamp back out of, and it is a
	// markdown heading rather than a machine's delimiter because the file is one
	// a person opens.
	_, err = fmt.Fprintf(file, "\n## %s\n\n%s\n", time.Now().UTC().Format(time.RFC3339), note)
	return err
}

// Recall answers the notes that match, most recent first.
//
// THE SEARCH IS A SUBSTRING AND THAT IS THE WHOLE OF IT. A subharness's memory is
// a handful of domain notes, not a corpus, and the model on the other end of this
// call is perfectly able to read fifteen lines and decide which two matter. An
// empty query is everything, which is what a program asking "what do I know about
// this domain" means.
func (m *Memory) Recall(ctx context.Context, query string) ([]exec.Note, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	notes, err := m.Notes()
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return notes, nil
	}
	matched := make([]exec.Note, 0, len(notes))
	for _, note := range notes {
		if strings.Contains(strings.ToLower(note.Text), query) {
			matched = append(matched, note)
		}
	}
	return matched, nil
}

// Notes is every note in the file, most recent first.
func (m *Memory) Notes() ([]exec.Note, error) {
	text, err := m.Read()
	if err != nil {
		return nil, err
	}
	notes := parseNotes(text)
	// Newest first, which is the order a recall wants: what a subharness learnt
	// last week about a domain is more likely to be what it needs than what it
	// learnt the first time it ran.
	for left, right := 0, len(notes)-1; left < right; left, right = left+1, right-1 {
		notes[left], notes[right] = notes[right], notes[left]
	}
	return notes, nil
}

// Read is the whole file, for a surface that wants to show it. A subharness that
// has never remembered anything reads as empty and is not an error.
func (m *Memory) Read() (string, error) {
	if err := m.plant(); err != nil {
		return "", err
	}
	data, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// plant copies the head version's seed memory into the live file, once, the
// first time anything touches it.
//
// It is the only moment the two files are related. After it, the version's
// memory.md is history — what this subharness was shipped knowing — and the live
// file is what it knows now.
func (m *Memory) plant() error {
	if _, err := os.Stat(m.path); err == nil || !errors.Is(err, os.ErrNotExist) {
		return nil
	}
	seed := []byte(nil)
	if m.seed != nil {
		seed = m.seed()
	}
	if len(strings.TrimSpace(string(seed))) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return err
	}
	// writeNew and not replace: planting a seed over a memory that appeared while
	// we were looking would throw notes away, and the exclusive create is what
	// makes that impossible rather than unlikely.
	if err := writeNew(m.path, seed); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return nil
}

// parseNotes reads the file back into notes, oldest first.
//
// Anything before the first heading is ONE note with no time on it, which is
// what a seed memory somebody wrote by hand looks like. It is carried rather than
// dropped: a bundle shipped with domain notes meant them to be recallable, and
// the fact that nobody stamped them is not a reason to hide them.
func parseNotes(text string) []exec.Note {
	var notes []exec.Note
	var at, body string
	flush := func() {
		if strings.TrimSpace(body) != "" {
			notes = append(notes, exec.Note{Text: strings.TrimSpace(body), At: at})
		}
		at, body = "", ""
	}
	for _, line := range strings.Split(text, "\n") {
		if stamp, ok := strings.CutPrefix(line, "## "); ok {
			flush()
			at = strings.TrimSpace(stamp)
			continue
		}
		body += line + "\n"
	}
	flush()
	return notes
}

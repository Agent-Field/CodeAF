package session

// Reading a transcript WITHOUT opening it.
//
// A picker of past conversations needs three things about each of them — what
// it was called, what was last happening in it, and when that was — and it
// needs them about every session in a directory at once. Opening each one is
// the wrong way to learn that: [openSessionFile] takes an exclusive flock,
// rebuilds every image part and repairs the transcript, so a list of twenty
// sessions would be twenty locks a running window is holding, twenty replays,
// and a picker that cannot show you the conversation you are currently in.
//
// So this is a read and nothing else: open, scan forward, close. It never
// locks, never writes, never creates the file, and it costs one pass over
// lines it mostly discards.
//
// It reads what the file SAYS rather than the transcript a resume rebuilds,
// which is the one place its answer and a resume's can differ: a rewind with
// nothing typed after it leaves the message it took back as the last thing the
// file heard. That is a stale sentence in a picker row, and the alternative is
// paying for a replay per row to be right about the rarest line in the journal.

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Summary is one transcript as a picker needs it.
type Summary struct {
	// File is the transcript, and what [Config.SessionFile] is set to to resume
	// it.
	File string
	// Title is the name the session gave itself (title.go), empty for one that
	// was never named — a session whose first turn never completed, or one that
	// was had before the namer existed. A surface derives a name from Opening
	// rather than showing an empty row, which is why that field is here.
	Title string
	// Opening is the first thing the person said, one line.
	Opening string
	// Last is the last thing they said — the "where was I" of the row. It falls
	// back to what the agent last answered, for the session whose final message
	// was a picture with no words in it.
	Last string
	// LastUser and LastAssistant keep the two sides of the last exchange apart
	// for surfaces that show where a conversation left off. Last retains its
	// picker-compatible fallback above.
	LastUser      string
	LastAssistant string
	// At is the newest timestamp in the file. Zero for a file whose lines carry
	// none, which a caller fills from the file's own modification time.
	At time.Time
	// Asked is how many of the person's messages the FILE holds. It counts
	// lines rather than turns — a compaction re-journals the messages it kept,
	// so a long session counts some of them twice — because the only question
	// asked of it is whether anything was ever said here at all.
	Asked int
}

// summaryClip bounds each excerpt. A picker draws sixty or eighty columns of
// one of these; the rest is carried so a narrow frame and a wide one clip from
// the same sentence rather than from two different ones.
const summaryClip = 200

// Peek reads one transcript and reports what a picker can show of it. The
// boolean is false for a file that is not a conversation — missing, unreadable,
// a header with nothing under it, or a session nobody ever spoke in. A picker
// row for one of those is a row with no words on it and nothing behind it.
func Peek(path string) (Summary, bool) {
	file, err := os.Open(path)
	if err != nil {
		return Summary{}, false
	}
	defer file.Close()

	summary := Summary{File: path}
	answered := ""
	scanner := bufio.NewScanner(file)
	// The same buffer the replay takes, for the same reason: one tool result of
	// any size would otherwise end the scan at the line before it, and the title
	// and the last message both sit at the END of the file.
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry sessionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if at, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err == nil {
			summary.At = at
		}
		switch entry.Type {
		case "title":
			// LAST one wins, exactly as the replay reads it: a name written twice
			// is a name that was changed — and a name that is the namer's own
			// instruction is read as no name at all ([healedTitle], title.go),
			// so the row falls back to Opening like any unnamed session's.
			if named := healedTitle(entry.Title); named != "" {
				summary.Title = named
			}
		case "message":
			text := summaryLine(entry.Content)
			switch entry.Role {
			case "user":
				// A MESSAGE WITH NO WORDS IN IT IS STILL A PERSON SPEAKING. A
				// picture and nothing else is the ordinary case (the surface's
				// attach.go writes exactly that), and a file whose only turn was
				// one is a conversation — so it counts toward [Summary.Asked],
				// which is the whole test for whether this file is one at all.
				// What it cannot be is the OPENING or the LAST line: those are
				// sentences, and this message has none. The answer below is what
				// the last line falls back to, for precisely this case.
				summary.Asked++
				if text == "" {
					continue
				}
				if summary.Opening == "" {
					summary.Opening = text
				}
				summary.Last = text
				summary.LastUser = text
			case "assistant":
				if text != "" {
					answered = text
					summary.LastAssistant = text
				}
			}
		}
	}
	// A scan that ended early is not a failure here. Whatever it read is still
	// true — a name, an opening line, the messages up to the line it choked on —
	// and a picker row built from the first half of a file says more than a row
	// that is missing because the second half was unreadable.
	if summary.Asked == 0 {
		return Summary{}, false
	}
	if summary.Last == "" {
		summary.Last = answered
	}
	return summary, true
}

// reportPeekMax bounds what one report is read back as. A node's final message
// is three lines in the project's record and a page or two in the journal it
// was cut from; eight kilobytes is far past any report a person reads down and
// far short of a journal line somebody pasted a file into.
const reportPeekMax = 8 << 10

// PeekReport is the LAST THING THE AGENT SAID in a journal, read off the file
// and whole.
//
// IT IS THE SENTENCE THE PROJECT'S RECORD ALREADY QUOTES THE FIRST LINE OF. A
// node's report is its final assistant message (task_run.go's lastSaid), and
// [TaskIndexEntry.Outcome] is that message's first sentence, cut to
// [taskOutcomeLimit]. So a surface holding the row and wanting the rest of it
// follows the row's own TranscriptURI through here rather than keeping a second
// copy — the index carries citations, and this is what following one costs.
//
// It is [Peek]'s discipline and not [openSessionFile]'s: open, scan forward,
// close. No lock, no replay, no repair, nothing created. A journal that is not
// there, cannot be read, or never had the agent say anything answers false,
// which is a surface drawing nothing rather than a surface drawing an empty
// quotation.
func PeekReport(path string) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()

	said := ""
	scanner := bufio.NewScanner(file)
	// The same buffer [Peek] takes, for the same reason: one tool result of any
	// size would otherwise end the scan at the line before it, and the message
	// this function is looking for is the LAST one in the file.
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry sessionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Type != "message" || entry.Role != "assistant" {
			continue
		}
		if text := strings.TrimSpace(entry.Content); text != "" {
			// LAST ONE WINS, which is the whole rule. A node says a great many
			// things on its way to a report and only the final one is the report.
			said = text
		}
	}
	if said == "" {
		return "", false
	}
	if len(said) > reportPeekMax {
		said = strings.TrimSpace(said[:reportPeekMax]) + "…"
	}
	return said, true
}

// Recent is [Peek] over a directory of FLAT transcripts, newest first.
//
// It is the old layout's reader and dies with it: a directory that may hold
// folder sessions beside the flat files (a machine mid-migration holds both)
// is read by [RecentSessions], which also takes the folder's own meta.json as
// the name and the ordering. One function that grew a branch for each layout
// would be one function two waves have to agree about, so this one keeps
// exactly its old law.
//
// It is bounded twice. limit is what the caller wants; peekBudget is how many
// files it will read to find them, so a directory holding a year of sessions
// costs a fixed number of scans rather than one per file. The candidates are
// ordered by modification time before any of them is opened — the cheap
// approximation of "newest" — and the answer is re-sorted by what the files
// themselves said, which is the fact a person recognizes.
func Recent(dir string, limit int) []Summary {
	if limit <= 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type candidate struct {
		path string
		at   time.Time
	}
	files := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		files = append(files, candidate{path: filepath.Join(dir, entry.Name()), at: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at.After(files[j].at) })

	found := make([]Summary, 0, limit)
	for read, file := range files {
		if len(found) >= limit || read >= peekBudget {
			break
		}
		summary, ok := Peek(file.path)
		if !ok {
			continue
		}
		if summary.At.IsZero() {
			summary.At = file.at
		}
		found = append(found, summary)
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].At.After(found[j].At) })
	return found
}

// peekBudget is how many transcripts one listing will read. Sixty-four is far
// past any list a person reads down and far short of a directory that has been
// accumulating for a year.
const peekBudget = 64

// summaryLine is one message as a row of a list: its first line, whitespace
// folded out, bounded.
func summaryLine(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return clip(strings.Join(strings.Fields(firstLine(text)), " "), summaryClip)
}

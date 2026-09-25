package teams

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// THE TRAFFIC LOG is what passes between a team's members, its manager and the
// person, and the events the team's conversations raise, in the order they
// happened. It is one file per team, <profile>/teams/<id>/traffic.jsonl, one
// JSON object per line, only ever appended to. Past trafficRotateBytes the
// file is renamed to traffic.1.jsonl (replacing the one before) and a new one
// begins, so a team that talks for weeks costs a few megabytes at most.
//
// EVERY ENTRY HAS AN ID THAT SORTS. The id is a sequence number, zero-padded
// to twelve digits, taken under the log's lock, so string order is the order
// of appending and a reader can ask for everything after the last id it saw.

// Entry kinds.
const (
	KindEvent     = "event"     // something a conversation did: finished, failed, asked
	KindNote      = "note"      // a message from one party to another
	KindDirective = "directive" // an instruction from the manager or the person
	KindStop      = "stop"      // a member was stopped
	KindStart     = "start"     // a member was started
	KindYou       = "you"       // the person spoke to the team
)

// Addresses that are not a member's handle.
const (
	FromManager = "manager"
	FromYou     = "you"
	FromSystem  = "system"
	ToEveryone  = "everyone"
	ToManager   = "manager"
	ToRoom      = "room"
	// ToSeveral is a message to more than one member and fewer than all of
	// them, by name: [Entry.Handles] lists who (thread.go).
	ToSeveral = "several"
)

var kinds = map[string]bool{
	KindEvent: true, KindNote: true, KindDirective: true, KindStop: true, KindStart: true, KindYou: true,
}

// Entry is one line of the Traffic log. This shape is the contract between
// the interface and the team tools.
type Entry struct {
	// ID is assigned by [AppendTraffic]; any ID given is replaced.
	ID string `json:"id"`
	// At is when it happened; [AppendTraffic] fills a zero one with now.
	At time.Time `json:"at"`
	// Kind is one of the Kind constants.
	Kind string `json:"kind"`
	// From is a member's handle, or manager, you or system.
	From string `json:"from"`
	// To is a member's handle, or everyone, manager or room, or several with
	// the handles in Handles.
	To   string `json:"to"`
	Text string `json:"text"`
	// Handles is who a message to several members is for, in the order the
	// sender named them; empty on every other entry (thread.go).
	Handles []string `json:"handles,omitempty"`
	// Answers is the id of the entry this one answers, which is what threads
	// the log: a member's reply names the manager's message it replies to, and
	// the events its turn raises name the same one. Empty on an entry that
	// answers nothing, and on every entry written before threads (thread.go).
	Answers string `json:"answers,omitempty"`
	// Files are paths the entry is about, when it is about any.
	Files []string `json:"files,omitempty"`
	// Member is the conversation key the entry concerns, when there is one.
	Member string `json:"member,omitempty"`
	// State is what a [KindEvent] says the member it concerns is now: one of
	// the State constants ([StateFinished], [StateFailed], [StateAsking],
	// [StateIdle] for a turn that was stopped, [StateRunning] for one that
	// carried on after its question was answered). It is empty on every other
	// kind. A reader colours by it rather than by reading Text, which is the
	// words a person reads.
	State string `json:"state,omitempty"`
}

// trafficRotateBytes is the size past which the log starts a new file.
var trafficRotateBytes int64 = 4 << 20

// trafficIDWidth is how many digits an entry id has.
const trafficIDWidth = 12

// TrafficPath is the log of team id: <profile>/teams/<id>/traffic.jsonl.
func TrafficPath(profileDir, teamID string) string {
	return config.ProfilePath(profileDir, filepath.Join("teams", teamID, "traffic.jsonl"))
}

func trafficRotated(path string) string {
	return strings.TrimSuffix(path, ".jsonl") + ".1.jsonl"
}

// safeTeamID reports whether id can name a directory: letters, digits, _ and -.
func safeTeamID(id string) error {
	if id == "" || len(id) > 64 {
		return fmt.Errorf("teams: %q is not a team id", id)
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return fmt.Errorf("teams: %q is not a team id", id)
		}
	}
	return nil
}

// AppendTraffic adds e to the end of team teamID's log, under the log's lock,
// giving it the next id and, when it has none, the time now.
func AppendTraffic(profileDir, teamID string, e Entry) error {
	_, err := AppendTrafficID(profileDir, teamID, e)
	return err
}

// AppendTrafficID is [AppendTraffic], and the id the entry was given, which a
// writer that will be answered keeps so the answer can name it.
func AppendTrafficID(profileDir, teamID string, e Entry) (string, error) {
	if err := safeTeamID(teamID); err != nil {
		return "", err
	}
	if !kinds[e.Kind] {
		return "", fmt.Errorf("teams: %q is not a traffic kind", e.Kind)
	}
	if strings.TrimSpace(e.From) == "" || strings.TrimSpace(e.To) == "" {
		return "", errors.New("teams: a traffic entry needs a from and a to")
	}
	if e.To == ToSeveral && len(e.Handles) == 0 {
		return "", errors.New("teams: a message to several members needs their handles")
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	path := TrafficPath(profileDir, teamID)
	err := lockedAt(strings.TrimSuffix(path, ".jsonl")+".lock", lockWait, func() error {
		last, err := lastTrafficID(path)
		if err != nil {
			return err
		}
		e.ID = fmt.Sprintf("%0*d", trafficIDWidth, last+1)
		line, err := json.Marshal(e)
		if err != nil {
			return err
		}
		line = append(line, '\n')
		if info, err := os.Stat(path); err == nil && info.Size() > 0 && info.Size()+int64(len(line)) > trafficRotateBytes {
			if err := os.Rename(path, trafficRotated(path)); err != nil {
				return err
			}
		}
		before := modTime(path)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		if _, err := file.Write(line); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		// The log's stamp moves with every line (stamp.go), under the log's
		// lock, so a reader that stats before it reads never misses one.
		advance(path, before)
		return nil
	})
	if err != nil {
		return "", err
	}
	return e.ID, nil
}

// ReadTraffic is team teamID's log after the entry with id after, oldest
// first, from both the current file and the rotated one before it.
//
// It is made for two readers. With after "" it is the TAIL: the last limit
// entries, for a digest or a first look. With an id it PAGES FORWARD: the first
// limit entries after that id, so a reader that keeps the last id it saw never
// skips one. A limit of 0 or less is every entry. A log that does not exist is
// no entries and no error. It takes no lock; a line still being written is
// skipped and read next time.
//
// IT READS WHAT IT RETURNS AND LITTLE ELSE. A log is up to two files of
// [trafficRotateBytes], and a reader asking at every step of a turn must not
// parse megabytes to learn that nothing is new. So a tail is read backwards
// from the end in windows until it holds limit entries ([tailTraffic]), and a
// page forward first asks each file for its last id (one window at its end,
// [lastIDIn]): a file with nothing past the cursor is not read at all, and one
// with something is entered where the cursor is, found by a binary search over
// byte offsets on the ids, which are in file order ([forwardTraffic]). Only a
// limit of 0 or less reads the files whole.
func ReadTraffic(profileDir, teamID string, after string, limit int) ([]Entry, error) {
	if err := safeTeamID(teamID); err != nil {
		return nil, err
	}
	path := TrafficPath(profileDir, teamID)
	files := []string{trafficRotated(path), path}
	var (
		all []Entry
		err error
	)
	switch {
	case limit <= 0:
		for _, p := range files {
			got, err := readTrafficFile(p)
			if err != nil {
				return nil, err
			}
			all = append(all, got...)
		}
	case after == "":
		all, err = tailTraffic(files, limit)
	default:
		all, err = forwardTraffic(files, after, limit)
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	seen := make(map[string]bool, len(all))
	for _, e := range all {
		if seen[e.ID] || (after != "" && e.ID <= after) {
			continue
		}
		seen[e.ID] = true
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if limit > 0 && len(out) > limit {
		if after == "" {
			out = out[len(out)-limit:]
		} else {
			out = out[:limit]
		}
	}
	return out, nil
}

// trafficWindow is how much of a file one backwards step reads.
var trafficWindow int64 = 64 << 10

// tailTraffic is at least the last limit entries of files (oldest file first),
// read backwards from the end of the newest in [trafficWindow] steps.
func tailTraffic(files []string, limit int) ([]Entry, error) {
	var out []Entry
	for index := len(files) - 1; index >= 0 && len(out) < limit; index-- {
		got, err := tailTrafficFile(files[index], limit-len(out))
		if err != nil {
			return nil, err
		}
		out = append(got, out...)
	}
	return out, nil
}

// tailTrafficFile is at least the last want entries of one file, or all of it.
// Only complete lines count; a line still being written has no newline yet.
func tailTrafficFile(path string, want int) ([]Entry, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	end := info.Size()
	var carry []byte // the head of a line cut by the window, joined next step
	var out []Entry
	for end > 0 && len(out) < want {
		start := max(end-trafficWindow, 0)
		buf := make([]byte, end-start, end-start+int64(len(carry)))
		if _, err := file.ReadAt(buf, start); err != nil && err != io.EOF {
			return nil, err
		}
		buf = append(buf, carry...)
		cut := 0
		if start > 0 {
			// The first line may begin before this window; keep it for the next.
			nl := bytes.IndexByte(buf, '\n')
			if nl < 0 {
				carry, end = buf, start
				continue
			}
			cut = nl + 1
		}
		carry = append([]byte(nil), buf[:cut]...)
		var got []Entry
		for _, line := range completeLines(buf[cut:]) {
			if e, ok := parseTrafficLine(line); ok {
				got = append(got, e)
			}
		}
		out = append(got, out...)
		end = start
	}
	return out, nil
}

// forwardTraffic is the first limit entries after the id after, from files in
// order (the rotated file holds only ids below the current file's).
func forwardTraffic(files []string, after string, limit int) ([]Entry, error) {
	var out []Entry
	for _, path := range files {
		if len(out) >= limit {
			break
		}
		got, err := forwardTrafficFile(path, after, limit-len(out))
		if err != nil {
			return nil, err
		}
		out = append(out, got...)
	}
	return out, nil
}

// forwardTrafficFile is up to want entries of one file with ids past after.
func forwardTrafficFile(path, after string, want int) ([]Entry, error) {
	last, found, err := lastIDIn(path)
	if err != nil || !found {
		return nil, err
	}
	if fmt.Sprintf("%0*d", trafficIDWidth, last) <= after {
		return nil, nil
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	start, err := trafficOffsetAfter(file, info.Size(), after)
	if err != nil {
		return nil, err
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	var out []Entry
	r := bufio.NewReader(file)
	for len(out) < want {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			if e, ok := parseTrafficLine(line); ok && e.ID > after {
				out = append(out, e)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// trafficOffsetAfter is the byte offset of the first line whose id is past
// after, found by a binary search over offsets: the id of the first whole line
// at or after an offset only grows with the offset. A line that does not parse
// is read as past, which can only move the answer earlier; the caller filters
// by id, so earlier costs a few lines read and never an entry skipped.
func trafficOffsetAfter(file *os.File, size int64, after string) (int64, error) {
	lo, hi := int64(0), size
	for lo < hi {
		mid := lo + (hi-lo)/2
		_, id, ok, err := trafficLineAt(file, size, mid)
		if err != nil {
			return 0, err
		}
		if !ok || id > after {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	start, _, _, err := trafficLineAt(file, size, lo)
	return start, err
}

// trafficLineAt is the first whole line starting at or after offset: where it
// starts and its id. ok is false when there is none or it does not parse.
func trafficLineAt(file *os.File, size, offset int64) (int64, string, bool, error) {
	start := offset
	if offset > 0 {
		// A line starts after a newline; find the first one at or after offset-1.
		at, err := nextNewline(file, size, offset-1)
		if err != nil || at < 0 {
			return size, "", false, err
		}
		start = at + 1
	}
	if start >= size {
		return size, "", false, nil
	}
	end, err := nextNewline(file, size, start)
	if err != nil || end < 0 {
		return start, "", false, err
	}
	buf := make([]byte, end-start)
	if _, err := file.ReadAt(buf, start); err != nil && err != io.EOF {
		return start, "", false, err
	}
	var e struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(buf, &e) != nil || e.ID == "" {
		return start, "", false, nil
	}
	return start, e.ID, true, nil
}

// nextNewline is the offset of the first newline at or after from, -1 for none.
func nextNewline(file *os.File, size, from int64) (int64, error) {
	const step = 4 << 10
	buf := make([]byte, step)
	for at := from; at < size; at += step {
		n, err := file.ReadAt(buf[:min(step, size-at)], at)
		if i := bytes.IndexByte(buf[:n], '\n'); i >= 0 {
			return at + int64(i), nil
		}
		if err != nil && err != io.EOF {
			return -1, err
		}
	}
	return -1, nil
}

// completeLines is the newline-ended lines of buf, without their newlines.
func completeLines(buf []byte) [][]byte {
	var lines [][]byte
	for {
		nl := bytes.IndexByte(buf, '\n')
		if nl < 0 {
			return lines
		}
		lines = append(lines, buf[:nl])
		buf = buf[nl+1:]
	}
}

// parseTrafficLine is one line as an entry, false for one that is not.
func parseTrafficLine(line []byte) (Entry, bool) {
	var e Entry
	if json.Unmarshal(line, &e) != nil || e.ID == "" {
		return Entry{}, false
	}
	return e, true
}

// readTrafficFile is every entry in one file that parses; a missing file is
// none.
func readTrafficFile(path string) ([]Entry, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var out []Entry
	r := bufio.NewReader(file)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			if e, ok := parseTrafficLine(line); ok {
				out = append(out, e)
			}
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// lastTrafficID is the highest id in the log, 0 for an empty one. It reads the
// end of the current file, or the rotated file when the current one is empty.
func lastTrafficID(path string) (int64, error) {
	for _, p := range []string{path, trafficRotated(path)} {
		n, found, err := lastIDIn(p)
		if err != nil || found {
			return n, err
		}
	}
	return 0, nil
}

// lastIDIn is the highest id among the complete lines at the end of one file:
// the last 64 KB, or the whole file when no whole entry fits in that.
func lastIDIn(path string) (int64, bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, false, err
	}
	for _, window := range []int64{64 << 10, info.Size()} {
		start := max(info.Size()-window, 0)
		buf := make([]byte, info.Size()-start)
		if _, err := file.ReadAt(buf, start); err != nil && err != io.EOF {
			return 0, false, err
		}
		// FROM THE END BACKWARDS, stopping at the first line with an id: ids
		// only grow down a file, so the last one that parses is the highest, and
		// a reader asking "is there anything new" decodes one line, not a window.
		lines := bytes.Split(buf, []byte{'\n'})
		for index := len(lines) - 1; index >= 0; index-- {
			if start > 0 && index == 0 {
				break // cut by the window
			}
			var e struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(lines[index], &e) != nil {
				continue
			}
			if n, err := strconv.ParseInt(e.ID, 10, 64); err == nil {
				return n, true, nil
			}
		}
		if start == 0 {
			return 0, false, nil
		}
	}
	return 0, false, nil
}

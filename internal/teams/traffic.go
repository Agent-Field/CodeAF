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
	// To is a member's handle, or everyone, manager or room.
	To   string `json:"to"`
	Text string `json:"text"`
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
	if err := safeTeamID(teamID); err != nil {
		return err
	}
	if !kinds[e.Kind] {
		return fmt.Errorf("teams: %q is not a traffic kind", e.Kind)
	}
	if strings.TrimSpace(e.From) == "" || strings.TrimSpace(e.To) == "" {
		return errors.New("teams: a traffic entry needs a from and a to")
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	path := TrafficPath(profileDir, teamID)
	return lockedAt(strings.TrimSuffix(path, ".jsonl")+".lock", lockWait, func() error {
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
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		if _, err := file.Write(line); err != nil {
			_ = file.Close()
			return err
		}
		return file.Close()
	})
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
func ReadTraffic(profileDir, teamID string, after string, limit int) ([]Entry, error) {
	if err := safeTeamID(teamID); err != nil {
		return nil, err
	}
	path := TrafficPath(profileDir, teamID)
	var all []Entry
	seen := map[string]bool{}
	for _, p := range []string{trafficRotated(path), path} {
		got, err := readTrafficFile(p)
		if err != nil {
			return nil, err
		}
		for _, e := range got {
			if seen[e.ID] || (after != "" && e.ID <= after) {
				continue
			}
			seen[e.ID] = true
			all = append(all, e)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	if limit > 0 && len(all) > limit {
		if after == "" {
			all = all[len(all)-limit:]
		} else {
			all = all[:limit]
		}
	}
	return all, nil
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
			var e Entry
			if json.Unmarshal(line, &e) == nil && e.ID != "" {
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
		best, found := int64(0), false
		for _, line := range bytes.Split(buf, []byte{'\n'}) {
			var e struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(line, &e) != nil {
				continue
			}
			if n, err := strconv.ParseInt(e.ID, 10, 64); err == nil && (!found || n > best) {
				best, found = n, true
			}
		}
		if found || start == 0 {
			return best, found, nil
		}
	}
	return 0, false, nil
}

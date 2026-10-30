package teams

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

// These bounds keep recent context useful without allowing Traffic to crowd
// credentials and preferences out of the sealed object shared by the computers.
const (
	TrafficSyncLines       = 32
	TrafficSyncBytes       = 16 << 10
	TrafficSyncTextBytes   = 2 << 10
	TrafficSyncDeviceBytes = 64 << 10
	TrafficSyncEvery       = 30 * time.Second
)

// TrafficForSync reads this computer's bounded tail across rotation. Received
// windows live in a separate file and can never be republished here.
func TrafficForSync(profile, team, device string) ([]Entry, error) {
	if err := safeTeamID(team); err != nil {
		return nil, err
	}
	all, err := tailTraffic([]string{trafficRotated(TrafficPath(profile, team)), TrafficPath(profile, team)}, TrafficSyncLines)
	if err != nil {
		return nil, err
	}
	if len(all) > TrafficSyncLines {
		all = all[len(all)-TrafficSyncLines:]
	}
	out := make([]Entry, 0, len(all))
	for _, e := range all {
		if e.Origin != "" {
			continue
		}
		e.ID = device + "/" + e.ID
		if e.Answers != "" && !strings.Contains(e.Answers, "/") {
			e.Answers = device + "/" + e.Answers
		}
		if e.Reply != "" && !strings.Contains(e.Reply, "/") {
			e.Reply = device + "/" + e.Reply
		}
		if len(e.Text) > TrafficSyncTextBytes {
			end := TrafficSyncTextBytes
			for end > 0 && !utf8.RuneStart(e.Text[end]) {
				end--
			}
			e.Text = e.Text[:end] + " …"
		}
		// Optional metadata must not exclude the words of an oversized line.
		raw, _ := json.Marshal(e)
		if len(raw) > TrafficSyncBytes/2 {
			e = Entry{ID: e.ID, At: e.At, Kind: e.Kind, From: e.From, To: e.To, Text: e.Text, Answers: e.Answers, Reply: e.Reply}
			if len(e.Answers) > TrafficSyncTextBytes {
				e.Answers = ""
			}
			if len(e.Reply) > TrafficSyncTextBytes {
				e.Reply = ""
			}
			if len(e.From) > TrafficSyncTextBytes {
				e.From = ""
			}
			if len(e.To) > TrafficSyncTextBytes {
				e.To = ""
			}
		}
		// A short optional field can still expand sixfold when JSON escapes
		// it. Preserve the text even when those encoded fields do not fit.
		if raw, _ := json.Marshal(e); len(raw)+2 > TrafficSyncBytes {
			e.From, e.To, e.Answers, e.Reply = "", "", "", ""
		}
		out = append(out, e)
	}
	for len(out) > 0 {
		raw, _ := json.Marshal(out)
		if len(raw) <= TrafficSyncBytes {
			break
		}
		out = out[1:]
	}
	return out, nil
}

// TrafficMirrorPath is deliberately outside both names the local log reader
// knows. Older builds ignore it, including when they tidy a team's directory.
func TrafficMirrorPath(profile, team string) string {
	return filepath.Join(TeamDir(profile, team), "elsewhere.jsonl")
}

// WriteTrafficMirror replaces the other computers' current windows as one
// atomic file. Portable ids keep threads intact without consuming local ids.
func WriteTrafficMirror(profile, team string, incoming []Entry) error {
	if err := safeTeamID(team); err != nil {
		return err
	}
	// Serialize with deletion so a late network response cannot recreate a
	// removed team's folder. Reading here performs no store repairs.
	return withLock(profile, lockWait, func() error {
		f, _, _, err := read(profile)
		if err != nil {
			return err
		}
		if f != nil {
			if _, ok := f.Team(team); !ok {
				return nil
			}
		}
		return writeTrafficMirror(profile, team, incoming)
	})
}

func writeTrafficMirror(profile, team string, incoming []Entry) error {
	byID := map[string]Entry{}
	for _, e := range incoming {
		if e.ID == "" {
			continue
		}
		e.Origin = e.ID
		if old, ok := byID[e.ID]; !ok || string(mustTrafficJSON(e)) > string(mustTrafficJSON(old)) {
			byID[e.ID] = e
		}
	}
	lines := make([]Entry, 0, len(byID))
	for _, e := range byID {
		lines = append(lines, e)
	}
	sortHistory(lines)
	var raw []byte
	for _, e := range lines {
		raw = append(raw, mustTrafficJSON(e)...)
		raw = append(raw, '\n')
	}
	path := TrafficMirrorPath(profile, team)
	have, err := os.ReadFile(path)
	if err == nil && bytes.Equal(have, raw) {
		return nil
	}
	if os.IsNotExist(err) && len(raw) == 0 {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".elsewhere-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	before := modTime(path)
	if err := fsreplace.Rename(tmp.Name(), path); err != nil {
		return err
	}
	advance(path, before)
	return nil
}

func mustTrafficJSON(e Entry) []byte { raw, _ := json.Marshal(e); return raw }

func sortHistory(lines []Entry) {
	sort.Slice(lines, func(i, j int) bool {
		if !lines[i].At.Equal(lines[j].At) {
			return lines[i].At.Before(lines[j].At)
		}
		if lines[i].Origin != lines[j].Origin {
			return lines[i].Origin < lines[j].Origin
		}
		return lines[i].ID < lines[j].ID
	})
}

// ReadHistory merges a local tail with the display mirror. Delivery always
// uses ReadTraffic instead; a mirror id is never a delivery position.
func ReadHistory(profile, team string, limit int) ([]Entry, error) {
	local, err := ReadTraffic(profile, team, "", limit)
	if err != nil {
		return nil, err
	}
	remote, err := readTrafficFile(TrafficMirrorPath(profile, team))
	if err != nil {
		SharingError(profile, err)
		remote = nil
	}
	all := append(local, remote...)
	sortHistory(all)
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all, nil
}

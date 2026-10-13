package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/filelock"
)

// AppPermissionRule keeps the scope a person picked, rather than a tool-wide
// memo. Removal retains the record so Undo restores the same identity.
type AppPermissionRule struct {
	ID             string    `json:"id"`
	Match          string    `json:"match"`
	Pattern        string    `json:"pattern"`
	ClassLabel     string    `json:"class_label"`
	Who            string    `json:"who"`
	AgentID        string    `json:"agent_id,omitempty"`
	MadeAt         time.Time `json:"made_at"`
	Tool           string    `json:"tool"`
	Removed        bool      `json:"removed,omitempty"`
	MatchedID      string    `json:"matched_id,omitempty"`
	MatchedCommand string    `json:"matched_command,omitempty"`
}

type appPermissionDisk struct {
	Rules map[string]AppPermissionRule `json:"rules"`
}

// The profile lock covers reads and private atomic replacement across engines.
// A malformed document is reported and never replaced by an empty rule set.
func appPermissionUpdate(profile string, save bool, fn func(*appPermissionDisk) error) error {
	profile = ProfilePath(profile, "")
	if err := os.MkdirAll(profile, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(profile, "app-permissions.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		return err
	}
	defer func() { _ = filelock.Unlock(lock) }()
	path := filepath.Join(profile, "app-permissions.json")
	disk := appPermissionDisk{Rules: map[string]AppPermissionRule{}}
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if err := json.Unmarshal(raw, &disk); err != nil {
			return err
		}
	}
	if disk.Rules == nil {
		disk.Rules = map[string]AppPermissionRule{}
	}
	before, _ := json.Marshal(disk)
	if err := fn(&disk); err != nil {
		return err
	}
	if !save {
		return nil
	}
	raw, err = json.Marshal(disk)
	if err != nil {
		return err
	}
	if bytes.Equal(before, raw) {
		return nil
	}
	f, err := os.CreateTemp(profile, ".app-permissions-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func AppPermissionRules(profile string, removed bool) ([]AppPermissionRule, error) {
	out := []AppPermissionRule{}
	err := appPermissionUpdate(profile, false, func(d *appPermissionDisk) error {
		for _, r := range d.Rules {
			if removed || !r.Removed {
				out = append(out, r)
			}
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].MadeAt.Equal(out[j].MadeAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].MadeAt.Before(out[j].MadeAt)
	})
	return out, err
}

// AppPermissionCommand uses a literal equality for exact rules. A '*' in a
// command is shell text here and never silently becomes a policy wildcard.
func AppPermissionCommand(tool string, args json.RawMessage) string {
	if tool == approval.ToolBash {
		var v struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(args, &v) == nil {
			return v.Command
		}
	}
	return tool + " " + string(args)
}

// AppPermissionClass is the bounded class shown by the binary before saving.
// Compound shell commands have no class because a rule cannot vouch for them.
func AppPermissionClass(tool string, args json.RawMessage) string {
	if tool != approval.ToolBash {
		return ""
	}
	shapes := BashShapes(AppPermissionCommand(tool, args))
	if len(shapes) > 1 {
		return shapes[0]
	}
	return ""
}

func SaveAppPermissionRule(profile, agent, tool string, args json.RawMessage, match, who, askID string, at time.Time) (AppPermissionRule, error) {
	r, _, err := EnsureAppPermissionRule(profile, agent, tool, args, match, who, askID, at)
	return r, err
}

// EnsureAppPermissionRule reports creation under the same lock as deduplication.
// A refused answer may roll back its new rule, but must keep an earlier saved answer.
func EnsureAppPermissionRule(profile, agent, tool string, args json.RawMessage, match, who, askID string, at time.Time) (AppPermissionRule, bool, error) {
	r := AppPermissionRule{}
	if approval.AlwaysAsks(tool, args) {
		return r, false, errors.New("irreversible actions always ask")
	}
	if match != "exact" && match != "class" || who != "agent" && who != "all_agents" {
		return r, false, errors.New("choose exact or class, and agent or all_agents")
	}
	pattern := AppPermissionCommand(tool, args)
	label := "exactly this command"
	if match == "class" {
		pattern = AppPermissionClass(tool, args)
		label = pattern
		if pattern == "" {
			return r, false, errors.New("this command has no always-allow class")
		}
	}
	if strings.TrimSpace(pattern) == "" || agent == "" {
		return r, false, errors.New("no command or agent to remember")
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return r, false, err
	}
	r = AppPermissionRule{ID: "rule_" + hex.EncodeToString(id), Match: match, Pattern: pattern, ClassLabel: label, Who: who, MadeAt: at.UTC(), Tool: tool, MatchedID: askID, MatchedCommand: AppPermissionCommand(tool, args)}
	if who == "agent" {
		r.AgentID = agent
	}
	created := false
	err := appPermissionUpdate(profile, true, func(d *appPermissionDisk) error {
		for _, old := range d.Rules {
			if !old.Removed && old.Match == r.Match && old.Pattern == r.Pattern && old.Tool == r.Tool && old.Who == r.Who && old.AgentID == r.AgentID {
				r = old
				return nil
			}
		}
		d.Rules[r.ID] = r
		created = true
		return nil
	})
	return r, created && err == nil, err
}

func ChangeAppPermissionRule(profile, id string, removed bool, expected ...bool) (AppPermissionRule, error) {
	var r AppPermissionRule
	err := appPermissionUpdate(profile, true, func(d *appPermissionDisk) error {
		var ok bool
		r, ok = d.Rules[id]
		if !ok {
			return os.ErrNotExist
		}
		if len(expected) > 0 && r.Removed != expected[0] {
			return ErrAppStale
		}
		r.Removed = removed
		d.Rules[id] = r
		return nil
	})
	return r, err
}

// The mandatory floors cannot be lifted by a mode, an account setting or a
// saved rule. Extra always-ask rows name a tool, exact command or shown class.
func AppPermissionAlwaysAsk(profile, tool string, args json.RawMessage, inTask bool) (bool, error) {
	if approval.AlwaysAsks(tool, args) {
		return true, nil
	}
	values, _, err := AppSettingsAt(profile)
	if err != nil {
		return true, err
	}
	command := AppPermissionCommand(tool, args)
	var rows []string
	raw, _ := json.Marshal(values["permissions.always_ask"])
	if err := json.Unmarshal(raw, &rows); err != nil {
		return true, err
	}
	for _, row := range rows {
		if row == tool || row == command || row == AppPermissionClass(tool, args) {
			return true, nil
		}
		if row == "payments" && (strings.HasPrefix(tool, "stripe_") || strings.HasPrefix(tool, "payment")) && !approval.ReadOnly(tool, args) {
			return true, nil
		}
		if row == "deleting outside a task" && !inTask && appDeletes(tool, command) {
			return true, nil
		}
	}
	return false, nil
}

func appDeletes(tool, command string) bool {
	if tool == "delete" || tool == "remove" {
		return true
	}
	if tool != approval.ToolBash {
		return false
	}
	for _, word := range strings.Fields(command) {
		if word == "rm" || word == "rmdir" || word == "unlink" || word == "-delete" {
			return true
		}
	}
	return false
}

func MatchAppPermissionRule(profile, agent, tool string, args json.RawMessage, inTask bool, callID string) (*AppPermissionRule, error) {
	floor, err := AppPermissionAlwaysAsk(profile, tool, args, inTask)
	if err != nil || floor {
		return nil, err
	}
	var matched *AppPermissionRule
	err = appPermissionUpdate(profile, true, func(d *appPermissionDisk) error {
		command := AppPermissionCommand(tool, args)
		ids := make([]string, 0, len(d.Rules))
		for id := range d.Rules {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			r := d.Rules[id]
			if r.Removed || r.Tool != tool || r.Who == "agent" && r.AgentID != agent {
				continue
			}
			hit := r.Match == "exact" && r.Pattern == command
			if r.Match == "class" {
				p := approval.Policy{BashPatterns: []approval.Rule{{Match: r.Pattern, Action: approval.ActionAllow}}}
				_, hit = p.MatchRule(command)
			}
			if hit {
				r.MatchedID, r.MatchedCommand = callID, command
				d.Rules[id] = r
				matched = &r
				break
			}
		}
		return nil
	})
	return matched, err
}

// AppQuestionClock resolves the saved duration without giving silence an answer.
func AppQuestionClock(profile string) (time.Duration, error) {
	values, _, err := AppSettingsAt(profile)
	if err != nil {
		return 0, err
	}
	raw, _ := json.Marshal(values["permissions.question_clock_min"])
	var n int
	if json.Unmarshal(raw, &n) != nil || n < 1 {
		return 0, fmt.Errorf("invalid question clock")
	}
	return time.Duration(n) * time.Minute, nil
}

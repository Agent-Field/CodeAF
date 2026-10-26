package main

// ── codeaf app: THE CREWS' APP LAYER ────────────────────────────────────────
//
// A crew is a codeaf team, and everything a team keeps is read from the teams
// store. The design asks for a few things teams do not keep — a goal and an end
// date, linked items, roles, a paused member, what a member was, the agent's
// tasks before it became a crew, an agent's proposal to become one — and those
// live HERE, in one small file beside the app's own state, keyed by team id and
// member agent id. Nothing here is ever written into the teams file, and the
// terminal never reads it: a crew made in the terminal simply has none of it.
//
// A member's budget is the one app field that is not in this file: it is the
// agent's own budget the app already keeps (appstate.go's budgets), bound to the
// member's conversation as its spending limit, so the engine stops it at the
// cap with a budget ask exactly as it stops any agent.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// appCrewLayerName is the layer's file, beside the app's state file.
const appCrewLayerName = "crews.json"

// appCrewExtra is what the layer keeps for one crew.
type appCrewExtra struct {
	// GoalAgent and GoalClientID bind the goal to its submission, never its text.
	GoalAgent    string     `json:"goal_agent,omitempty"`
	GoalClientID string     `json:"goal_client_id,omitempty"`
	Goal         string     `json:"goal,omitempty"`
	EndsAt       *time.Time `json:"ends_at,omitempty"`
	History      []string   `json:"history,omitempty"`
	// HistoryOwner is the agent that held History's tasks when the crew was made.
	HistoryOwner string             `json:"history_owner,omitempty"`
	Items        []appwire.CrewItem `json:"items,omitempty"`
	// Start is the request that made this crew with its own manager
	// (appteamstart.go): what a repeated request is answered from.
	Start *appStartRecord `json:"start,omitempty"`
	// Proposal freezes the accepted edits so interrupted setup can resume.
	Proposal     *appwire.Proposal `json:"proposal,omitempty"`
	ProposalDone bool              `json:"proposal_done,omitempty"`
}

// appMemberExtra is what the layer keeps for one member of one crew.
type appMemberExtra struct {
	Role   string             `json:"role,omitempty"`
	Paused bool               `json:"paused,omitempty"`
	Was    *appwire.MemberWas `json:"was,omitempty"`
}

// appPendingStart is what a member started from a proposal is to be given
// once it joins: its role and model.
type appPendingStart struct {
	ReportsTo string `json:"reports_to,omitempty"`
	Role      string `json:"role,omitempty"`
	Model     string `json:"model,omitempty"`
}

// appCrewLayerFile is the whole layer.
type appCrewLayerFile struct {
	Crews     map[string]*appCrewExtra     `json:"crews,omitempty"`
	Members   map[string]*appMemberExtra   `json:"members,omitempty"`
	Roles     map[string]*appwire.Role     `json:"roles,omitempty"`
	Proposals map[string]*appwire.Proposal `json:"proposals,omitempty"`
	Pending   map[string]appPendingStart   `json:"pending,omitempty"`
	Seq       int                          `json:"seq,omitempty"`
}

// appCrewLayer is the file, read once and written whole.
type appCrewLayer struct {
	mu     sync.Mutex
	path   string
	loaded bool
	stamp  string
	file   appCrewLayerFile
}

// appMemberKey is a member's key in the layer: its crew and its agent.
func appMemberKey(crew, agent string) string { return crew + "/" + agent }

// appPendingKey is a pending start's key: its crew and the handle it was
// started under.
func appPendingKey(crew, handle string) string { return crew + "/@" + handle }

func (l *appCrewLayer) loadLocked() error {
	stamp := appInputStamp(l.path)
	if l.loaded && l.stamp == stamp {
		return nil
	}
	l.file = appCrewLayerFile{}
	raw, err := os.ReadFile(l.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	default:
		if err := json.Unmarshal(raw, &l.file); err != nil {
			return err
		}
	}
	l.loaded, l.stamp = true, stamp
	return nil
}

// read hands fn the layer as it stands.
func (l *appCrewLayer) read(fn func(*appCrewLayerFile)) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.loadLocked(); err != nil {
		return err
	}
	fn(&l.file)
	return nil
}

// write changes the layer and saves it whole: a new file renamed over the old.
func (l *appCrewLayer) write(fn func(*appCrewLayerFile) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.loadLocked(); err != nil {
		return err
	}
	// The change is made on a copy, so a refusal leaves the layer as it was.
	raw, err := json.Marshal(l.file)
	if err != nil {
		return err
	}
	var next appCrewLayerFile
	if err := json.Unmarshal(raw, &next); err != nil {
		return err
	}
	if err := fn(&next); err != nil {
		return err
	}
	out, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), appStateFolderMode); err != nil {
		return err
	}
	temp := l.path + ".tmp"
	if err := os.WriteFile(temp, out, appStateFileMode); err != nil {
		return err
	}
	if err := os.Rename(temp, l.path); err != nil {
		return err
	}
	// The next read rechecks disk, including a replacement immediately after
	// our rename; an external writer must never be cached as our own copy.
	l.file, l.loaded = next, false
	return nil
}

// crew is the layer's record for a crew, made when missing.
func (f *appCrewLayerFile) crew(id string) *appCrewExtra {
	if f.Crews == nil {
		f.Crews = map[string]*appCrewExtra{}
	}
	if f.Crews[id] == nil {
		f.Crews[id] = &appCrewExtra{}
	}
	return f.Crews[id]
}

// member is the layer's record for a member, made when missing.
func (f *appCrewLayerFile) member(crew, agent string) *appMemberExtra {
	if f.Members == nil {
		f.Members = map[string]*appMemberExtra{}
	}
	key := appMemberKey(crew, agent)
	if f.Members[key] == nil {
		f.Members[key] = &appMemberExtra{}
	}
	return f.Members[key]
}

// next is a new id with a prefix, unique in the layer.
func (f *appCrewLayerFile) next(prefix string) string {
	f.Seq++
	return prefix + strconv.Itoa(f.Seq)
}

// crewLayer is the backend's layer, beside its state file.
func (b *appBackend) crewLayer() *appCrewLayer {
	b.crewOnce.Do(func() {
		b.layer = &appCrewLayer{path: filepath.Join(filepath.Dir(b.state.where()), appCrewLayerName)}
	})
	return b.layer
}

// crewPaused says the person paused this agent in some open crew.
func (b *appBackend) crewPaused(agent string) bool {
	paused := false
	_ = b.crewLayer().read(func(f *appCrewLayerFile) {
		for key, m := range f.Members {
			if m != nil && m.Paused && filepath.Base(key) == agent {
				paused = true
			}
		}
	})
	return paused
}

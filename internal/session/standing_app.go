package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/Agent-Field/codeaf/internal/standing"
)

// App delivery always keeps an Inbox copy. Chat scope selects delivery targets
// from the binary's registry; it never changes the workspace an order examines.
func (r *standingRunner) deliverApp(item standing.Item, note standing.Note) error {
	if r.root == "" {
		return errors.New("this standing order has no engine home")
	}
	inbox := standing.AppInboxDir(r.root, item.ID)
	if err := standing.Deliver(inbox, note); err != nil && !errors.Is(err, standing.ErrAlreadyDrained) {
		return err
	}
	if item.App.Scope == "not_here" {
		return nil
	}
	targets, err := appStandingTargets(r.root, item)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if err := standing.Deliver(filepath.Dir(target.Transcript), note); err != nil {
			if errors.Is(err, standing.ErrAlreadyDrained) {
				continue
			}
			return err
		}
		if agent := liveSession(target.ID); agent != nil {
			agent.emitStandingNews(standingUpdateWord(note.Kind), item, note.Text)
		}
	}
	return nil
}

type appStandingTarget struct{ ID, Transcript string }

// The registry is an engine document, read here without importing the HTTP
// package. Crew membership is read at delivery time so moves cannot leak news.
func appStandingTargets(root string, item standing.Item) ([]appStandingTarget, error) {
	if item.App.Scope == "this_chat" {
		if item.Origin.Transcript == "" {
			return nil, errors.New("this standing order has no chat address")
		}
		return []appStandingTarget{{item.Origin.SessionID, item.Origin.Transcript}}, nil
	}
	var disk struct {
		Agents map[string]struct {
			Agent struct {
				ID     string `json:"id"`
				CrewID string `json:"crew_id"`
				State  string `json:"state"`
			} `json:"agent"`
			Transcript string `json:"session_file"`
		} `json:"agents"`
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(root), "app", "agents.json"))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &disk); err != nil {
		return nil, err
	}
	targets := []appStandingTarget{}
	for _, record := range disk.Agents {
		if record.Agent.State == "archived" || record.Transcript == "" {
			continue
		}
		if item.App.Scope == "this_crew" && record.Agent.CrewID != item.App.CrewID {
			continue
		}
		targets = append(targets, appStandingTarget{record.Agent.ID, record.Transcript})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	return targets, nil
}

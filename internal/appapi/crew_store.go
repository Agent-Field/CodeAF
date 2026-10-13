package appapi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/teams"
)

const defaultCrewBudgetUSD = 20.0

func defaultCrewBudget() Money { return Money{fmt.Sprintf("%.2f", defaultCrewBudgetUSD)} }

// App fields live inside the native document's preserved extension, so TUI
// membership and manager edits remain the source of truth across launches.
type crewMeta struct {
	BudgetAsk    *Ask   `json:"budget_ask,omitempty"`
	RetiredSpent *Money `json:"retired_spent,omitempty"`
	Crew
	Members    map[string]crewMemberMeta `json:"members"`
	Version    uint64                    `json:"version"`
	ProposalID string                    `json:"proposal_id,omitempty"`
}
type crewMemberMeta struct {
	MessageKeys []string `json:"message_keys,omitempty"`
	EndSpent    *Money   `json:"end_spent,omitempty"`
	StartSpent  *Money   `json:"start_spent,omitempty"`
	BudgetAsk   *Ask     `json:"budget_ask,omitempty"`
	RoleID      string   `json:"role_id"`
	Role        NewRole  `json:"role"`
	Cap         *Money   `json:"cap,omitempty"`
	ReportsTo   string   `json:"reports_to"`
	State       string   `json:"state"`
}

func readCrewMeta(t teams.Team) (crewMeta, error) {
	raw, err := json.Marshal(t)
	if err != nil {
		return crewMeta{}, err
	}
	var envelope struct {
		App json.RawMessage `json:"app_v2"`
	}
	if err = json.Unmarshal(raw, &envelope); err != nil {
		return crewMeta{}, err
	}
	m := crewMeta{Crew: Crew{Tint: crewTints[((int(t.Hue/60)%len(crewTints))+len(crewTints))%len(crewTints)], Budget: defaultCrewBudget(), State: "active"}, Version: 1, Members: map[string]crewMemberMeta{}}
	if len(envelope.App) > 0 {
		if err = json.Unmarshal(envelope.App, &m); err != nil {
			return crewMeta{}, err
		}
	}
	if m.Members == nil {
		m.Members = map[string]crewMemberMeta{}
	}
	return m, nil
}
func writeCrewMeta(t *teams.Team, m crewMeta) error {
	raw, err := json.Marshal(t)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	fields["app_v2"], err = json.Marshal(m)
	if err != nil {
		return err
	}
	raw, err = json.Marshal(fields)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, t)
}
func crewAgentID(d *agentDisk, key string) string {
	for id, r := range d.Records {
		if r.SessionFile == key || id == key {
			return id
		}
	}
	return key
}
func crewView(f *teams.File, t teams.Team, d *agentDisk, a *AgentRegistry) (Crew, []Member, error) {
	meta, err := readCrewMeta(t)
	if err != nil {
		return Crew{}, nil, err
	}
	c := meta.Crew
	c.ID = t.ID
	c.Name = t.Name
	c.ManagerID = crewAgentID(d, t.Manager)
	c.ParentID = t.Parent
	c.Depth = len(f.Ancestors(t.ID)) + 1
	if t.Closed() {
		if c.State != "archived" {
			c.State = "closed"
		}
		at := t.ClosedAt
		c.ClosedAt = &at
	}
	members := []Member{}
	running, needs, total := 0, 0, int64(0)
	for _, native := range t.Members {
		id := crewAgentID(d, native.Key)
		m := meta.Members[id]
		member := Member{CrewID: t.ID, AgentID: id, RoleID: m.RoleID, IsManager: native.Key == t.Manager, ReportsTo: m.ReportsTo, Cap: m.Cap, State: m.State, AlsoIn: []string{}}
		if member.ReportsTo == "" && !member.IsManager {
			member.ReportsTo = c.ManagerID
		}
		if record, ok := d.Records[id]; ok {
			agent := a.view(record)
			if member.State != "paused" && member.State != "stopped" {
				member.State = agent.State
				if member.State == "closed" || member.State == "archived" || member.State == "failed" {
					member.State = "stopped"
				}
			}
			member.Spent = agent.Spent
			if m.EndSpent != nil {
				member.Spent = m.EndSpent
			}
			if member.Spent != nil && m.StartSpent != nil {
				n, _ := crewCents(*member.Spent)
				start, _ := crewCents(*m.StartSpent)
				n -= start
				member.Spent = nil
				if n > 0 {
					member.Spent = &Money{fmt.Sprintf("%d.%02d", n/100, n%100)}
				}
			}
			if member.Spent != nil {
				n, _ := crewCents(*member.Spent)
				total += n
			}
		}
		if member.State == "" {
			member.State = "idle"
		}
		if member.State == "running" {
			running++
		}
		if member.State == "needs_you" {
			needs++
		}
		for _, other := range f.Teams {
			if other.ID == t.ID || other.Closed() {
				continue
			}
			om, err := readCrewMeta(other)
			if err != nil {
				return Crew{}, nil, err
			}
			for _, nm := range other.Members {
				oid := crewAgentID(d, nm.Key)
				if oid == id || m.RoleID != "" && om.Members[oid].RoleID == m.RoleID {
					member.AlsoIn = append(member.AlsoIn, other.ID)
					break
				}
			}
		}
		sort.Strings(member.AlsoIn)
		members = append(members, member)
	}
	sort.SliceStable(members, func(i, j int) bool {
		if members[i].IsManager != members[j].IsManager {
			return members[i].IsManager
		}
		return members[i].AgentID < members[j].AgentID
	})
	c.StateLine = ""
	if running > 0 {
		c.StateLine = fmt.Sprintf("%d running", running)
	}
	if needs > 0 {
		if c.StateLine != "" {
			c.StateLine += " "
		}
		c.StateLine += fmt.Sprintf("%d needs you", needs)
	}
	total, err = crewPoolSpent(f, t, d, a)
	if err != nil {
		return Crew{}, nil, err
	}
	c.Spent = nil
	if total > 0 {
		c.Spent = &Money{fmt.Sprintf("%d.%02d", total/100, total%100)}
	}
	return c, members, nil
}

type crewProposalRecord struct {
	ParentID      string         `json:"parent_id,omitempty"`
	ParentVersion uint64         `json:"parent_version,omitempty"`
	Proposal      HelperProposal `json:"proposal"`
	AgentID       string         `json:"agent_id"`
	Goal          string         `json:"goal"`
	Version       uint64         `json:"version"`
	Made          time.Time      `json:"made"`
}

// Proposals are saved independently because proposing creates no team or member.
func crewProposals(home string, fn func(map[string]crewProposalRecord) error, save bool) error {
	dir := filepath.Join(home, "v3", "app")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "crew-proposals.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = filelock.Lock(lock, true, false); err != nil {
		return err
	}
	defer filelock.Unlock(lock)
	path := filepath.Join(dir, "crew-proposals.json")
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	records := map[string]crewProposalRecord{}
	if err == nil {
		if err = json.Unmarshal(raw, &records); err != nil {
			return err
		}
	}
	if err = fn(records); err != nil || !save {
		return err
	}
	raw, err = json.Marshal(records)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".crew-proposals-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(raw)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), path)
}
func loadCrewProposal(home, id string) (crewProposalRecord, error) {
	var p crewProposalRecord
	err := crewProposals(home, func(records map[string]crewProposalRecord) error {
		var ok bool
		p, ok = records[id]
		if !ok {
			return agentError(404, "not_found", "no such helper proposal")
		}
		return nil
	}, false)
	return p, err
}

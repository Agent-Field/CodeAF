package data

import (
	"database/sql"
	"errors"
	"strings"
)

// AgentPrincipal identifies the trusted agent bound to a content session.
// The writer combines this identity with the app and declared action in Scope.
type AgentPrincipal struct {
	ID string `json:"id"`
}

const agentGrantMigration = `
	CREATE TABLE agent_grants (
		app TEXT NOT NULL,
		agent TEXT NOT NULL,
		grant_group TEXT NOT NULL,
		grant_id TEXT NOT NULL,
		PRIMARY KEY(app, agent, grant_group, grant_id)
	) WITHOUT ROWID;
`

func validAgentGrant(app string, agent AgentPrincipal, group, id string) bool {
	valid := func(value string) bool {
		return value != "" && len(value) <= 200 && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\r\n\x00")
	}
	return valid(app) && valid(agent.ID) && valid(id) && (group == "actions" || group == "data")
}

// SetAgentGrant records a person's grant or revocation in the data writer.
func (s *Store) SetAgentGrant(app string, agent AgentPrincipal, group, id string, allowed bool) error {
	if !validAgentGrant(app, agent, group, id) {
		return errorf(CodeInvalid, "an agent grant needs an app, agent and declared scope")
	}
	return s.write(func(w *writeTx) error {
		statement := `DELETE FROM agent_grants WHERE app=? AND agent=? AND grant_group=? AND grant_id=?`
		if allowed {
			statement = `INSERT INTO agent_grants (app, agent, grant_group, grant_id) VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING`
		}
		result, err := w.tx.Exec(statement, app, agent.ID, group, id)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed == 0 {
			return err
		}
		return w.invalidate(EventPolicyChanged, "", "")
	})
}

// AgentAllowed checks the named grant for an app principal.
func (s *Store) AgentAllowed(app string, agent AgentPrincipal, group, id string) (bool, error) {
	return agentAllowed(s.rdb, app, agent, group, id)
}

func agentAllowed(q querier, app string, agent AgentPrincipal, group, id string) (bool, error) {
	if !validAgentGrant(app, agent, group, id) {
		return false, nil
	}
	var found int
	err := q.QueryRow(`SELECT 1 FROM agent_grants WHERE app=? AND agent=? AND grant_group=? AND grant_id=?`, app, agent.ID, group, id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return found == 1, err
}

// writeAs checks the grant in the same transaction as the mutation, so a
// revoke and a write have one order even across different store handles.
func (s *Store) writeAs(sc Scope, write func(*writeTx) error) error {
	legacyAgent := s.requireAgentPrincipal && (sc.isAgent() || sc.AI && !sc.Flow)
	if sc.Principal.ID == "" && !legacyAgent {
		return s.write(write)
	}
	return s.write(func(w *writeTx) error {
		allowed, err := agentAllowed(w.tx, sc.App, sc.Principal, "actions", sc.Action)
		if err != nil {
			return err
		}
		if !allowed {
			return &Error{Code: CodeNotAllowed, Need: sc.Action, Message: "the agent does not have permission to write through this action"}
		}
		return write(w)
	})
}

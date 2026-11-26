package data

import "strings"

// MCPAgent is display metadata for an external principal registered by the
// local app MCP launcher. Grants and writes still use AgentPrincipal.
type MCPAgent struct {
	AgentPrincipal
	Name    string
	Harness string
}

func (s *Store) RegisterMCPAgent(app string, agent MCPAgent) error {
	switch agent.Harness {
	case "codeaf", "claude_code", "codex", "agentfield":
	default:
		return errorf(CodeInvalid, "choose a named agent harness")
	}
	if !validAgentGrant(app, agent.AgentPrincipal, "actions", "mcp") ||
		len(agent.Name) > 200 || strings.TrimSpace(agent.Name) == "" || strings.ContainsAny(agent.Name, "\r\n\x00") {
		return errorf(CodeInvalid, "choose a valid app agent name")
	}
	return s.write(func(w *writeTx) error {
		_, err := w.tx.Exec("INSERT INTO app_mcp_agents VALUES(?,?,?,?) ON CONFLICT DO NOTHING", app, agent.ID, agent.Name, agent.Harness)
		return err
	})
}

func (s *Store) MCPAgents(app string) ([]MCPAgent, error) {
	rows, err := s.rdb.Query("SELECT agent, name, harness FROM app_mcp_agents WHERE app=? ORDER BY agent", app)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var agents []MCPAgent
	for rows.Next() {
		var agent MCPAgent
		if err := rows.Scan(&agent.ID, &agent.Name, &agent.Harness); err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return agents, rows.Err()
}

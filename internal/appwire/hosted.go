package appwire

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

// FeatureAgentsHosted tells the app it can work with chats on its organization's server.
const FeatureAgentsHosted = "agents.hosted"

// HostedChats is the optional engine capability for organization chats.
type HostedChats interface {
	HostedList(org string) (HostedListResult, error)
}

// HostedListResult carries the organization's chats and its server's hosting policy.
type HostedListResult struct {
	Agents    []Agent                    `json:"agents"`
	Available bool                       `json:"available"`
	Hosting   map[string]json.RawMessage `json:"hosting,omitzero"`
}

// HostedFacts describes the caller's access to a chat on their organization's server.
type HostedFacts struct {
	Org     string   `json:"org"`
	Session string   `json:"session"`
	Owner   string   `json:"owner,omitempty"`
	Role    string   `json:"role,omitempty"`
	Verbs   []string `json:"verbs"`
	Note    string   `json:"note,omitempty"`
}

const (
	hostedSessionPrefix = "hs_"
	hostedSessionIDSize = len(hostedSessionPrefix) + 24
)

var hostedConnectionID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ValidHostedSessionID checks a server's chat name before using it in a path.
func ValidHostedSessionID(session string) bool {
	if len(session) != hostedSessionIDSize || !strings.HasPrefix(session, hostedSessionPrefix) {
		return false
	}
	for _, c := range session[len(hostedSessionPrefix):] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// HostedAgentID joins a server's chat name and the connection through which it is reached.
func HostedAgentID(session, connection string) string { return session + "_" + connection }

// ParseHostedAgentID reads the fixed-length session prefix because both names may contain underscores.
func ParseHostedAgentID(id string) (session, connection string, ok bool) {
	if len(id) <= hostedSessionIDSize || id[hostedSessionIDSize] != '_' {
		return "", "", false
	}
	session, connection = id[:hostedSessionIDSize], id[hostedSessionIDSize+1:]
	if !ValidHostedSessionID(session) || !hostedConnectionID.MatchString(connection) {
		return "", "", false
	}
	return session, connection, true
}

func withHostedFeatures(hello Hello, e Engine) Hello {
	if _, ok := e.(HostedChats); ok {
		hello = withFeature(hello, FeatureAgentsHosted)
	}
	return hello
}

func init() {
	methods["agents.hosted_list"] = typed(func(e Engine, p struct {
		Org string `json:"org"`
	}) (*HostedListResult, error) {
		hosted, ok := e.(HostedChats)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve agents.hosted_list")
		}
		if _, err := required("org", p.Org); err != nil {
			return nil, err
		}
		result, err := hosted.HostedList(p.Org)
		result.Agents = nonNil(result.Agents)
		return &result, err
	})
}

// hostedActionRefuser keeps local-only methods from interpreting a hosted id as a
// local folder, process, or inventory reference. The backend names the host.
type hostedActionRefuser interface {
	RefuseHostedAction(org string) error
}

func hostedActionRefusal(e Engine, name string, raw json.RawMessage) error {
	switch name {
	case "agents.open", "agents.entries", "agents.send", "agents.stop", "agents.close",
		"asks.list", "asks.answer", MethodAnswerSecret:
		return nil
	}
	door, ok := e.(hostedActionRefuser)
	if !ok || (!bytes.Contains(raw, []byte(hostedSessionPrefix)) && !bytes.Contains(raw, []byte(`\u`))) {
		return nil
	}
	var params map[string]any
	if json.Unmarshal(raw, &params) != nil {
		return nil
	}
	if org := hostedActionOrg(params); org != "" {
		return door.RefuseHostedAction(org)
	}
	return nil
}

func hostedActionOrg(params map[string]any) string {
	for name, value := range params {
		switch strings.ToLower(name) {
		case "id", "ids", "agent", "agents", "manager", "member", "members", "owner", "chat", "chats",
			"cell", "session", "session_id", "local_agent", "run_agent", "agent_id", "from_chat":
			if org := hostedActionReference(value); org != "" {
				return org
			}
		}
		// Batches and resource references can carry the agent inside another object.
		switch nested := value.(type) {
		case map[string]any:
			if org := hostedActionOrg(nested); org != "" {
				return org
			}
		case []any:
			for _, item := range nested {
				if object, ok := item.(map[string]any); ok {
					if org := hostedActionOrg(object); org != "" {
						return org
					}
				}
			}
		}
	}
	return ""
}

func hostedActionReference(value any) string {
	switch refs := value.(type) {
	case string:
		_, org, _ := ParseHostedAgentID(refs)
		return org
	case []any:
		for _, ref := range refs {
			if org := hostedActionReference(ref); org != "" {
				return org
			}
		}
	}
	return ""
}

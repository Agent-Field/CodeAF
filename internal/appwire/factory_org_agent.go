package appwire

import (
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

// ── AN ORGANIZATION WORKSTREAM'S AGENT ON THE ORGANIZATION'S SERVER ────────
//
// While the operator runs the organization's workstream agents on its server
// (agent_runner "server"), every workstream has one agent there, with one
// conversation everyone who can see the workstream reads, and those who can
// edit it talk to. The server runs it with the organization's model key,
// which only the operator sets and which no desk ever reads back.
//
//	factory.org_keys                    OrgConn                     -> OrgKeys            GET    /v1/org/keys (operator)
//	factory.org_key_set                 OrgKeySetParams             -> OrgKey             PUT    /v1/org/keys/{provider} (operator)
//	factory.org_key_delete              OrgKeyParams                -> {}                 DELETE /v1/org/keys/{provider} (operator)
//	factory.org_workstream_agent        OrgWorkstreamAgentParams    -> OrgWorkstreamAgent GET    /v1/workstreams/{id}/agent?since=
//	factory.org_workstream_agent_send   OrgWorkstreamAgentSendParams -> OrgWorkstreamAgent POST  /v1/workstreams/{id}/agent/messages
//	factory.org_workstream_agent_stop   OrgWorkstreamParams         -> OrgWorkstreamAgent POST   /v1/workstreams/{id}/agent/stop
//
// The server's `agent.entry` and `agent.state` are relayed as
// factory.org_workstream_agent, already filtered to those who can see the
// workstream. A message sent while the agent answers someone else is
// conflict with detail {"reason":"busy","by":"@handle"}.

// FeatureOrgAgentServer is the hello word for the methods above.
const FeatureOrgAgentServer = "org_agent_server"

// EventFactoryOrgWorkstreamAgent is one line of a workstream agent's
// conversation, or a change to what it is doing; its data is
// [FactoryOrgWorkstreamAgent].
const EventFactoryOrgWorkstreamAgent = "factory.org_workstream_agent"

// OrgKey is one of the organization's model keys as a screen may show it.
type OrgKey struct {
	Provider string `json:"provider"`
	Last4    string `json:"last4"`
	SetAt    string `json:"set_at"`
	SetBy    string `json:"set_by"`
}

// OrgKeys is every key kept, and the providers a key may be kept for.
type OrgKeys struct {
	Keys      []OrgKey `json:"keys"`
	Providers []string `json:"providers"`
}

func (k *OrgKeys) fill() {
	if k.Keys == nil {
		k.Keys = []OrgKey{}
	}
	if k.Providers == nil {
		k.Providers = []string{}
	}
}

// OrgKeyParams names one provider.
type OrgKeyParams struct {
	OrgConn
	Provider string `json:"provider"`
}

// OrgKeySetParams is the key the operator hands the server.
type OrgKeySetParams struct {
	OrgConn
	Provider string `json:"provider"`
	Key      string `json:"key"`
}

// OrgAgentEntry is one line of the conversation: what someone asked
// ("person", By is them), the agent's answer ("agent"), one thing it did on
// the way ("step", Tool, Failed), or the server saying what happened ("note").
type OrgAgentEntry struct {
	Seq    int    `json:"seq"`
	At     string `json:"at"`
	Kind   string `json:"kind"`
	By     string `json:"by,omitempty"`
	Text   string `json:"text"`
	Tool   string `json:"tool,omitempty"`
	Failed bool   `json:"failed,omitempty"`
	Turn   string `json:"turn,omitempty"`
}

// OrgAgentState is what the agent is doing: "idle", "working" (For is whose
// message it answers) or "stopping" (For is who stopped it).
type OrgAgentState struct {
	State string `json:"state"`
	Turn  string `json:"turn,omitempty"`
	For   string `json:"for,omitempty"`
	Since string `json:"since,omitempty"`
}

// OrgWorkstreamAgent is the agent as the caller reads it: its state, the
// lines after since, the last seq, and whether the caller may talk to it now.
// Cannot is the one sentence saying why not; it is empty for a reader who
// could never talk to it.
type OrgWorkstreamAgent struct {
	ID string `json:"id"`
	OrgAgentState
	Entries []OrgAgentEntry `json:"entries"`
	Seq     int             `json:"seq"`
	CanTalk bool            `json:"can_talk"`
	Cannot  string          `json:"cannot,omitempty"`
}

func (a *OrgWorkstreamAgent) fill() {
	if a.Entries == nil {
		a.Entries = []OrgAgentEntry{}
	}
	if a.State == "" {
		a.State = "idle"
	}
}

// OrgWorkstreamAgentParams reads one workstream's agent after Since.
type OrgWorkstreamAgentParams struct {
	OrgConn
	ID    string `json:"id"`
	Since int    `json:"since,omitempty"`
}

// OrgWorkstreamAgentSendParams is one message to the agent.
type OrgWorkstreamAgentSendParams struct {
	OrgConn
	ID   string `json:"id"`
	Text string `json:"text"`
}

// FactoryOrgWorkstreamAgent is one agent event on the connection it came
// from: a new line (Entry) or what the agent is doing now (State).
type FactoryOrgWorkstreamAgent struct {
	Connection string         `json:"connection"`
	ID         string         `json:"id"`
	Entry      *OrgAgentEntry `json:"entry,omitempty"`
	State      *OrgAgentState `json:"state,omitempty"`
}

// orgAgentMessageMost is the longest message, as the server takes it.
const orgAgentMessageMost = 16 << 10

func orgKeyPath(provider string) (string, error) {
	p, err := orgSegment("provider", provider)
	if err != nil {
		return "", err
	}
	return "/v1/org/keys/" + url.PathEscape(p), nil
}

func init() {
	eventShapes[EventFactoryOrgWorkstreamAgent] = reflect.TypeFor[FactoryOrgWorkstreamAgent]()
	agent := func(a *OrgWorkstreamAgent) { a.fill() }
	methods["factory.org_keys"] = filled(orgMethod[OrgConn, OrgKeys]("factory.org_keys", func(OrgConn) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodGet, Path: "/v1/org/keys"}, nil
	}), func(k *OrgKeys) { k.fill() })
	methods["factory.org_key_set"] = orgMethod[OrgKeySetParams, OrgKey]("factory.org_key_set", func(p OrgKeySetParams) (OrgRequest, error) {
		path, err := orgKeyPath(p.Provider)
		if err != nil {
			return OrgRequest{}, err
		}
		key := strings.TrimSpace(p.Key)
		if key == "" || len(key) > 512 || strings.ContainsAny(key, " \t\r\n") {
			return OrgRequest{}, Errorf(CodeBadParams, "key is one word of at most 512 characters")
		}
		return OrgRequest{Method: http.MethodPut, Path: path, Body: map[string]string{"key": key}}, nil
	})
	methods["factory.org_key_delete"] = orgMethod[OrgKeyParams, Empty]("factory.org_key_delete", func(p OrgKeyParams) (OrgRequest, error) {
		path, err := orgKeyPath(p.Provider)
		return OrgRequest{Method: http.MethodDelete, Path: path}, err
	})
	methods["factory.org_workstream_agent"] = filled(orgMethod[OrgWorkstreamAgentParams, OrgWorkstreamAgent]("factory.org_workstream_agent", func(p OrgWorkstreamAgentParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		if p.Since < 0 {
			return OrgRequest{}, Errorf(CodeBadParams, "since is the last seq you have")
		}
		path += "/agent"
		if p.Since > 0 {
			path += "?since=" + strconv.Itoa(p.Since)
		}
		return OrgRequest{Method: http.MethodGet, Path: path}, nil
	}), agent)
	methods["factory.org_workstream_agent_send"] = filled(orgMethod[OrgWorkstreamAgentSendParams, OrgWorkstreamAgent]("factory.org_workstream_agent_send", func(p OrgWorkstreamAgentSendParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		text := strings.TrimSpace(p.Text)
		if text == "" || len(text) > orgAgentMessageMost {
			return OrgRequest{}, Errorf(CodeBadParams, "text is 1 to 16384 bytes of words")
		}
		return OrgRequest{Method: http.MethodPost, Path: path + "/agent/messages", Body: map[string]string{"text": text}}, nil
	}), agent)
	methods["factory.org_workstream_agent_stop"] = filled(orgMethod[OrgWorkstreamParams, OrgWorkstreamAgent]("factory.org_workstream_agent_stop", func(p OrgWorkstreamParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		return OrgRequest{Method: http.MethodPost, Path: path + "/agent/stop"}, err
	}), agent)
}

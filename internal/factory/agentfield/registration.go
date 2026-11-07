package agentfield

import (
	"encoding/json"
	"strings"
)

// The three reasoners the factory registers. Their names are what a trigger
// targets (`codeaf-factory.intake`) and what the node's routes answer.
const (
	ReasonerIntake  = "intake"
	ReasonerMessage = "message"
	ReasonerStatus  = "status"
)

// NodeVersion is the version the node registers as. IT IS THE CONTRACT'S, NOT
// THE BUILD'S: the control plane keys a registration by (id, version), so a
// version that changed with every build would leave one row per build behind
// and a trigger aimed at the node id would not know which is live. It changes
// only when the reasoners' contract does; the build is in the metadata.
const NodeVersion = "1"

// SecretPrefix is the path segment the node's address secret follows.
const SecretPrefix = "/n/"

// RegisteredBase is the base_url the control plane is given: the base_url a
// person typed with the address secret after it.
func RegisteredBase(baseURL, secret string) string {
	return strings.TrimRight(baseURL, "/") + SecretPrefix + secret
}

// entrySchema is an inbox.Entry as JSON Schema, the input of a direct call to
// intake and the shape a generic trigger's event takes.
const entrySchema = `{
  "type": "object",
  "properties": {
    "title": {"type": "string", "maxLength": 200, "description": "one line: the work, in the asker's words"},
    "body": {"type": "string", "maxLength": 32000},
    "external_id": {"type": "string", "maxLength": 200, "description": "the work's id on the sending system; with the source, its identity"},
    "url": {"type": "string", "format": "uri"},
    "author": {"type": "string", "maxLength": 200},
    "labels": {"type": "array", "items": {"type": "string", "maxLength": 100}, "maxItems": 30},
    "priority": {"type": "string", "enum": ["p0", "p1", "p2", "p3"]},
    "repo": {"type": "string", "description": "owner/name, when the sender knows the repository"},
    "trust": {"type": "string", "enum": ["owner", "collaborator", "stranger"], "description": "held to the control plane's ceiling"}
  },
  "required": ["title"]
}`

// intakeInput is the intake reasoner's input: a trigger's {event, _meta}, or
// an entry (directly, or as {entry}).
var intakeInput = `{
  "description": "A trigger's dispatch {event, _meta}, or an inbox entry from any caller.",
  "oneOf": [
    {"type": "object", "properties": {"event": {}, "_meta": {"type": "object"}}, "required": ["event", "_meta"]},
    {"type": "object", "properties": {"entry": ` + entrySchema + `}, "required": ["entry"]},
    ` + entrySchema + `
  ]
}`

const intakeOutput = `{
  "type": "object",
  "properties": {
    "job": {"type": "integer"},
    "created": {"type": "boolean"},
    "state": {"type": "string"},
    "pr_url": {"type": "string"},
    "steps_done": {"type": "integer"},
    "steps_total": {"type": "integer"},
    "filed": {"type": "boolean"},
    "reason": {"type": "string"}
  }
}`

const messageInput = `{
  "type": "object",
  "description": "A message on a job: by its source and id, or by its number.",
  "properties": {
    "source": {"type": "string", "description": "the job's source, such as agentfield:linear"},
    "id": {"type": "string", "description": "the job's id on that source"},
    "job": {"type": "integer"},
    "text": {"type": "string", "maxLength": 32000}
  },
  "required": ["text"]
}`

const messageOutput = `{
  "type": "object",
  "properties": {
    "job": {"type": "integer"},
    "delivered": {"type": "string", "enum": ["manager", "context"]}
  }
}`

const statusInput = `{
  "type": "object",
  "description": "Which job: by its source and id, or by its number.",
  "properties": {
    "source": {"type": "string"},
    "id": {"type": "string"},
    "job": {"type": "integer"}
  }
}`

const statusOutput = `{
  "type": "object",
  "properties": {
    "job": {"type": "integer"},
    "title": {"type": "string"},
    "state": {"type": "string"},
    "repo": {"type": "string"},
    "route": {"type": "string"},
    "pr_url": {"type": "string"},
    "steps_done": {"type": "integer"},
    "steps_total": {"type": "integer"},
    "question": {"type": "string"}
  }
}`

// Reasoners is the node's reasoner list, each with its JSON Schemas. Only
// intake takes webhook triggers; message and status are for direct calls.
func Reasoners() []Reasoner {
	yes, no := "true", "false"
	return []Reasoner{
		{ID: ReasonerIntake, Description: "File work from a trigger or any caller as a codeaf factory job; reports the job's progress and its pull request as this execution's result.", InputSchema: json.RawMessage(intakeInput), OutputSchema: json.RawMessage(intakeOutput), Tags: []string{"codeaf", "factory", "intake"}, AcceptsWebhook: &yes},
		{ID: ReasonerMessage, Description: "Send a message to a factory job's manager, held to the job's trust.", InputSchema: json.RawMessage(messageInput), OutputSchema: json.RawMessage(messageOutput), Tags: []string{"codeaf", "factory"}, AcceptsWebhook: &no},
		{ID: ReasonerStatus, Description: "Read a factory job's state, steps and pull request.", InputSchema: json.RawMessage(statusInput), OutputSchema: json.RawMessage(statusOutput), Tags: []string{"codeaf", "factory"}, AcceptsWebhook: &no},
	}
}

// RegistrationFor is the node's registration for one start.
//
// callback_discovery is "manual": the control plane takes the base_url as
// given and probes no other address, because the address a person typed is
// the one their network routes.
func RegistrationFor(s Settings, secret, instance, version, build string) Registration {
	r := Registration{
		ID:             s.NodeID,
		BaseURL:        RegisteredBase(s.BaseURL, secret),
		Version:        version,
		Reasoners:      Reasoners(),
		Skills:         []json.RawMessage{},
		DeploymentType: "long_running",
		InstanceID:     instance,
		CallbackDiscovery: map[string]any{
			"mode": "manual",
		},
		Metadata: map[string]any{"codeaf": map[string]any{"kind": "factory", "build": build, "trust_ceiling": string(s.Trust)}},
	}
	r.CommunicationConfig.Protocols = []string{"http"}
	r.CommunicationConfig.HeartbeatInterval = "60s"
	return r
}

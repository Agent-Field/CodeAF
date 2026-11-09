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

// SecretPrefix is the path segment a LEGACY node secret follows. A fresh
// node registers a plain base_url and is authenticated by the internal
// token; the prefix is only answered, for one release, when an old secret is
// still kept (see connection.go).
const SecretPrefix = "/n/"

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
    "text": {"type": "string", "minLength": 1, "maxLength": 8000},
    "author": {"type": "string", "description": "who said it on the source's side; the source itself when left out"}
  },
  "required": ["text"]
}`

const messageOutput = `{
  "type": "object",
  "properties": {
    "job": {"type": "integer"},
    "accepted": {"type": "boolean"},
    "outcome": {"type": "string"},
    "line": {"type": "string", "description": "what the job's manager made of the message, as the job's comments show it"}
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
    "state": {"type": "string"},
    "steps_done": {"type": "integer"},
    "steps_total": {"type": "integer"},
    "route": {"type": "string"},
    "pr_url": {"type": "string"},
    "question": {"type": "string"},
    "updated": {"type": "string", "format": "date-time"}
  }
}`

// Reasoners is the node's reasoner list, each with its JSON Schemas. Only
// intake takes webhook triggers; message and status are for direct calls.
func Reasoners() []Reasoner {
	yes, no := "true", "false"
	return []Reasoner{
		{ID: ReasonerIntake, Description: "File work from a trigger or any caller as a codeaf factory job; reports the job's progress and its pull request as this execution's result.", InputSchema: json.RawMessage(intakeInput), OutputSchema: json.RawMessage(intakeOutput), Tags: []string{"codeaf", "factory", "intake"}, AcceptsWebhook: &yes},
		{ID: ReasonerMessage, Description: "Say more about a factory job, read as a comment on it and held to the job's trust.", InputSchema: json.RawMessage(messageInput), OutputSchema: json.RawMessage(messageOutput), Tags: []string{"codeaf", "factory"}, AcceptsWebhook: &no},
		{ID: ReasonerStatus, Description: "Read a factory job's state, steps and pull request.", InputSchema: json.RawMessage(statusInput), OutputSchema: json.RawMessage(statusOutput), Tags: []string{"codeaf", "factory"}, AcceptsWebhook: &no},
	}
}

// RegistrationFor is the node's registration for one start.
//
// callback_discovery is "manual": the control plane takes the base_url as
// given and probes no other address, because the address a person typed is
// the one their network routes.
//
// AN EMPTY base_url IS A LINK-ONLY NODE: the control plane has no address to
// dial or probe, and reaches it only down the link it opens.
func RegistrationFor(s Settings, instance, version, build string) Registration {
	r := Registration{
		ID:             s.NodeID,
		BaseURL:        strings.TrimRight(s.BaseURL, "/"),
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
	r.CommunicationConfig.Protocols = []string{"link"}
	if s.BaseURL != "" {
		r.CommunicationConfig.Protocols = []string{"link", "http"}
	}
	r.CommunicationConfig.HeartbeatInterval = "60s"
	return r
}

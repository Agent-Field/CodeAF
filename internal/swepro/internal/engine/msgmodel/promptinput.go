// Prompt-input shapes port src/session/message-v2.ts:482-544. Stored parts
// carry ambient sessionID/messageID fields; these drafts intentionally do not,
// and ID is optional so the prompt layer may allocate it.
package msgmodel

type TextPartInput struct {
	ID        *string       `json:"id,omitempty"`
	Type      string        `json:"type"`
	Text      string        `json:"text"`
	Synthetic *bool         `json:"synthetic,omitempty"`
	Ignored   *bool         `json:"ignored,omitempty"`
	Time      *TimeStartEnd `json:"time,omitempty"`
	Metadata  RawObject     `json:"metadata,omitempty"`
}

type FilePartInput struct {
	ID       *string         `json:"id,omitempty"`
	Type     string          `json:"type"`
	Mime     string          `json:"mime"`
	Filename *string         `json:"filename,omitempty"`
	URL      string          `json:"url"`
	Source   *FilePartSource `json:"source,omitempty"`
}

type AgentPartInput struct {
	ID     *string      `json:"id,omitempty"`
	Type   string       `json:"type"`
	Name   string       `json:"name"`
	Source *AgentSource `json:"source,omitempty"`
}

type SubtaskPartInput struct {
	ID          *string       `json:"id,omitempty"`
	Type        string        `json:"type"`
	Prompt      string        `json:"prompt"`
	Description string        `json:"description"`
	Agent       string        `json:"agent"`
	Model       *SubtaskModel `json:"model,omitempty"`
	Command     *string       `json:"command,omitempty"`
}

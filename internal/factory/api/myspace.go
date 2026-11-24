package api

// ── MY SPACE ────────────────────────────────────────────────────────────────
//
// Inside an organization each person has their own space (serve/myspace.go):
// their pages, canvases and chats about the organization's work, kept and run
// on the organization's server with its key and charged to the organization
// under their name. Only they see it. The operator sees totals: what each
// person's space spent this month and how many things it holds, never a name,
// an owner's item or a word of what it says.

// MySpace is GET /v1/myspace's answer: the caller's own space, made on first
// read. Workstream is the space as a workstream (space true), so its files,
// canvases and chats are read with the workstream methods.
type MySpace struct {
	Workstream Workstream `json:"workstream"`
	// LimitUSD is what one person's space may spend in a calendar month (UTC);
	// SpentUSD is what this one has spent this month. AtLimit is true once
	// SpentUSD has reached LimitUSD: its chats then take no more messages.
	LimitUSD float64 `json:"limit_usd"`
	SpentUSD float64 `json:"spent_usd"`
	AtLimit  bool    `json:"at_limit,omitempty"`
}

// MySpaceTotals is GET /v1/myspace/totals's answer, the operator's view of
// every person's space: totals only.
type MySpaceTotals struct {
	// Month is the calendar month the spend is for, "2026-11".
	Month    string          `json:"month"`
	LimitUSD float64         `json:"limit_usd"`
	People   []MySpacePerson `json:"people"`
}

// MySpacePerson is one person's space as the operator sees it: who, what it
// spent this month, and how many things it holds (documents, files, canvases
// and chats together). Nothing else about it is answered.
type MySpacePerson struct {
	Person   string  `json:"person"`
	Name     string  `json:"name,omitempty"`
	SpentUSD float64 `json:"spent_usd"`
	Items    int     `json:"items"`
	AtLimit  bool    `json:"at_limit,omitempty"`
}

// MySpaceLimitPut is PUT /v1/myspace/limit's body: the operator's
// per-person monthly limit, in US dollars. Zero stops every space's chats.
type MySpaceLimitPut struct {
	LimitUSD float64 `json:"limit_usd"`
}

// MySpaceLimitDefaultUSD is a person's monthly limit until the operator sets
// one.
const MySpaceLimitDefaultUSD = 20.0

// MySpaceLimitMostUSD is the highest monthly limit the operator may set.
const MySpaceLimitMostUSD = 10000.0

// AgentChat is one of a person's own chats with a workstream's agent on the
// server, beside the one Afi conversation every workstream has. What is said
// in it is private to the person, like Afi's conversation: nobody else reads
// it. In a shared workstream, others see its OtherChat row only when the chat
// is shared with them or the organization's policy makes new chats visible.
type AgentChat struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// State is the agent's state in this chat (AgentIdle, AgentWorking,
	// AgentStopping).
	State     string `json:"state"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	// Agent is the named agent this chat talks to (org_agents.go); absent is
	// Afi. Its name is read fresh, so a renamed agent reads renamed.
	Agent *AgentRef `json:"agent,omitempty"`
}

// AgentChatList is GET /v1/workstreams/{id}/chats's answer: the caller's own
// chats there, newest change first.
type AgentChatList struct {
	Chats []AgentChat `json:"chats"`
	// Others are the chats of other people in this workstream that the
	// caller may see, metadata only. A chat is private to its owner and the
	// people it is shared with unless the organization's policy says new chats
	// start otherwise, so a private chat is not here. Always empty in a space.
	Others []OtherChat `json:"others"`
}

// OtherChat is another person's chat as someone it is shared with sees it
// ("Lena's chat: Onboarding emails · Working"). It carries its title, its
// owner, its state and its last activity, and never a line of what is said.
// ID matches the chat_id on the tasks and files it made, so a row can open
// them.
type OtherChat struct {
	ID         string `json:"id"`
	Workstream string `json:"workstream"`
	Title      string `json:"title"`
	Owner      string `json:"owner"`
	OwnerName  string `json:"owner_name,omitempty"`
	// State is AgentIdle, AgentWorking or AgentStopping.
	State        string `json:"state"`
	LastActivity string `json:"last_activity"`
}

// AgentChatNew is POST /v1/workstreams/{id}/chats's body. An empty title is
// filled from the chat's first message.
type AgentChatNew struct {
	Title string `json:"title,omitempty"`
	// Agent is the id of a named agent to talk to instead of Afi: one of the
	// caller's own in their space, or one the workstream lists.
	Agent string `json:"agent,omitempty"`
}

// AgentChatsMost is how many chats one person keeps in one workstream.
const AgentChatsMost = 500

// AgentChatTitleMost is the longest title a chat may have, in bytes.
const AgentChatTitleMost = 120

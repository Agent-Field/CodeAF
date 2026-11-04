package appwire

import "time"

const (
	FeatureChatsTake = "chats.take"
	// FeatureChatsMove says chat rows carry move and move_ended, and
	// agents.take_cancel exists: a client learns how every move ends from the
	// rows and needs its own timer only as a fallback.
	FeatureChatsMove = "chats.move"
	FeatureSyncRelay = "sync.relay"
	EventChatChanged = "chat.changed"
	EventChatRemoved = "chat.removed"
)

type Seen struct {
	Device string `json:"device"`
	Fence  uint64 `json:"fence"`
	Head   string `json:"head,omitempty"`
}
type ChatRow struct {
	Cell         string          `json:"cell"`
	Title        string          `json:"title"`
	LocalAgent   string          `json:"local_agent,omitempty"`
	Here         bool            `json:"here"`
	Holder       *LocationHolder `json:"holder,omitempty"`
	Online       *bool           `json:"online,omitempty"`
	SavedAt      time.Time       `json:"saved_at,omitzero"`
	Status       string          `json:"status" enum:"here,running,idle,off"`
	Crew         string          `json:"crew,omitempty"`
	Seen         Seen            `json:"seen"`
	CanTake      bool            `json:"can_take"`
	Taking       bool            `json:"taking"`
	Sharing      *ChatSharing    `json:"sharing,omitempty"`
	Move         *ChatMove       `json:"move,omitempty"`
	MoveEnded    *ChatMoveEnded  `json:"move_ended,omitempty"`
	Line         string          `json:"line,omitempty"`
	TakeoverLine string          `json:"takeover_line,omitempty"`
}

// ChatSharing says the computer holding the chat has sealed turns it has not
// finished sharing yet; can_take stays false until it has, while that
// computer is a known one. Sent and Total are how far its upload had got at
// its last beat, absent when it did not say (an older engine or relay).
type ChatSharing struct {
	Pending uint32 `json:"pending"`
	Sent    int64  `json:"sent,omitempty"`
	Total   int64  `json:"total,omitempty"`
}

// ChatMove is how far agents.take has got moving this chat here. A queued
// move waits for the move ahead of it; moves run one at a time.
type ChatMove struct {
	State string `json:"state" enum:"queued,moving"`
	Step  string `json:"step,omitempty" enum:"preparing,copying,opening"`
	// Done is the bytes copied to this computer so far.
	Done int64 `json:"done,omitempty"`
	// Total is the bytes the copy is expected to take, absent when unknown.
	Total int64 `json:"total,omitempty"`
}

// ChatMoveEnded is how this computer's last move of the chat ended when it did
// not move it. It stays on the row until the chat is here or another move of
// it starts, so a client that stopped waiting still learns the outcome.
type ChatMoveEnded struct {
	Outcome string `json:"outcome" enum:"stopped,failed,cancelled"`
	// Line is the person-facing sentence; Detail is what went wrong, for
	// "Copy details".
	Line   string `json:"line"`
	Detail string `json:"detail,omitempty"`
}
type ChatsResult struct {
	Chats   []ChatRow `json:"chats"`
	Version uint64    `json:"version"`
}
type ChatChanged struct {
	Chat    ChatRow `json:"chat"`
	Version uint64  `json:"version"`
}
type ChatRemoved struct {
	Cell    string `json:"cell"`
	Version uint64 `json:"version"`
}
type TakeParams struct {
	Cell string `json:"cell"`
	Seen Seen   `json:"seen"`
}
type TakeResult struct {
	Agent Agent `json:"agent"`
}
type TakeCancelParams struct {
	Cell string `json:"cell"`
}
type TakeCancelResult struct {
	// Cancelled is false when no move of the chat was running or queued.
	Cancelled bool `json:"cancelled"`
}
type HolderChanged struct {
	Kind string `json:"kind" enum:"holder_changed"`
}
type ChatsTaker interface {
	ListChats() (ChatsResult, error)
	TakeAgent(TakeParams) (Agent, error)
}

// ChatsMover cancels a move agents.take is running or has queued.
type ChatsMover interface {
	CancelTake(TakeCancelParams) (TakeCancelResult, error)
}
type Relay struct {
	URL    string `json:"url,omitempty"`
	Source string `json:"source" enum:"environment,paired,default,none"`
	Off    bool   `json:"off"`
}
type RelayReporter interface{ RelayKnown() bool }

func withChatsFeatures(h Hello, e Engine) Hello {
	if _, ok := e.(ChatsTaker); ok {
		h = withFeature(h, FeatureChatsTake)
	}
	if _, ok := e.(ChatsMover); ok {
		h = withFeature(h, FeatureChatsMove)
	}
	if d, ok := e.(RelayReporter); ok && d.RelayKnown() {
		h = withFeature(h, FeatureSyncRelay)
	}
	return h
}
func init() {
	methods["chats.list"] = typed(func(e Engine, _ struct{}) (*ChatsResult, error) {
		d, ok := e.(ChatsTaker)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not list chats on other computers")
		}
		got, err := d.ListChats()
		got.Chats = nonNil(got.Chats)
		return &got, err
	})
	methods["agents.take"] = typed(func(e Engine, p TakeParams) (*TakeResult, error) {
		d, ok := e.(ChatsTaker)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not continue chats here")
		}
		if _, err := required("cell", p.Cell); err != nil {
			return nil, err
		}
		if p.Seen.Device == "" {
			return nil, Errorf(CodeBadParams, "seen is required")
		}
		got, err := d.TakeAgent(p)
		return &TakeResult{Agent: got}, err
	})
	methods["agents.take_cancel"] = typed(func(e Engine, p TakeCancelParams) (*TakeCancelResult, error) {
		d, ok := e.(ChatsMover)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not cancel moves")
		}
		if _, err := required("cell", p.Cell); err != nil {
			return nil, err
		}
		got, err := d.CancelTake(p)
		return &got, err
	})
}

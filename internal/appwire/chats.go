package appwire

import "time"

const (
	FeatureChatsTake = "chats.take"
	FeatureSyncRelay = "sync.relay"
	EventChatChanged = "chat.changed"
	EventChatRemoved = "chat.removed"
)

type Seen struct {
	Device string `json:"device"`
	Fence  uint64 `json:"fence"`
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
	Line         string          `json:"line,omitempty"`
	TakeoverLine string          `json:"takeover_line,omitempty"`
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
type HolderChanged struct {
	Kind string `json:"kind" enum:"holder_changed"`
}
type ChatsTaker interface {
	ListChats() (ChatsResult, error)
	TakeAgent(TakeParams) (Agent, error)
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
}

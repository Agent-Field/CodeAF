package appwire

import (
	"encoding/json"
	"time"
)

const (
	FeatureChatsTake = "chats.take"
	// FeatureChatsRead says agents.entries follows saved remote heads without taking the chat.
	FeatureChatsRead = "chats.read"
	// FeatureChatReadSource identifies engines that tag remote and fallback pages.
	FeatureChatReadSource = "chats.read_source"
	// FeatureChatsMove says chat rows carry move and move_ended, and
	// agents.take_cancel exists: a client learns how every move ends from the
	// rows and needs its own timer only as a fallback.
	FeatureChatsMove    = "chats.move"
	FeatureChatsPreview = "chats.preview"
	// FeatureChatsWorking says chat rows carry working: the holder said at its
	// last beat that work is still running or settling there. A relay that does not keep the
	// flag leaves it false, so a client says "open there", never "idle there".
	FeatureChatsWorking = "chats.working"
	// FeatureChatsSend says chats.send exists and chat rows carry notes: a
	// message written here to a chat another computer holds, which that
	// computer puts in the chat's queue, so it runs when the turn there ends.
	// It is absent while the account's hosted sync storage is off ([ChatsSendOffer]).
	FeatureChatsSend = "chats.send"
	FeatureSyncRelay = "sync.relay"
	// FeatureChatsLive says chats.open_live and chats.live_keyboard exist and
	// chat rows carry can_live: a chat another computer holds can be opened as a
	// live view and typed into there, with nothing moved. It is absent unless
	// the engine can dial that computer and remote control is on.
	FeatureChatsLive = "chats.live"
	EventChatChanged = "chat.changed"
	EventChatRemoved = "chat.removed"
)

type Seen struct {
	Device string `json:"device"`
	Fence  uint64 `json:"fence"`
	Head   string `json:"head,omitempty"`
}
type ChatRow struct {
	CanPreview bool            `json:"can_preview,omitempty"`
	CanOpen    bool            `json:"can_open,omitempty"`
	Cell       string          `json:"cell"`
	Title      string          `json:"title"`
	LocalAgent string          `json:"local_agent,omitempty"`
	Here       bool            `json:"here"`
	Holder     *LocationHolder `json:"holder,omitempty"`
	Online     *bool           `json:"online,omitempty"`
	SavedAt    time.Time       `json:"saved_at,omitzero"`
	Status     string          `json:"status" enum:"here,running,idle,off"`
	Crew       string          `json:"crew,omitempty"`
	Seen       Seen            `json:"seen"`
	CanTake    bool            `json:"can_take"`
	Taking     bool            `json:"taking"`
	// Folder is the name of the folder the chat works in on its holder, when
	// the holder said; Size is the bytes moving it here copies, absent when
	// unknown or when this computer already has a copy (a move then copies
	// only what changed).
	Folder       string         `json:"folder,omitempty"`
	Size         int64          `json:"size,omitempty"`
	Sharing      *ChatSharing   `json:"sharing,omitempty"`
	Move         *ChatMove      `json:"move,omitempty"`
	MoveEnded    *ChatMoveEnded `json:"move_ended,omitempty"`
	Line         string         `json:"line,omitempty"`
	TakeoverLine string         `json:"takeover_line,omitempty"`
	// Working says the holder has a turn or background work still to settle
	// and save (status running only). Absent while it is merely open there.
	Working bool `json:"working,omitempty"`
	// CanLive says chats.open_live may be asked for this chat now: the computer
	// holding it is online and running it, and this engine can dial it.
	CanLive bool `json:"can_live,omitempty"`
	// Notes are the messages this computer wrote to the chat while another
	// computer held it, oldest first: waiting until that computer takes one into
	// the chat's queue, then delivered. Delivered ones are kept while this engine
	// runs and the chat is elsewhere; they leave once it is here, where the
	// transcript has them.
	Notes []ChatNote `json:"notes,omitempty"`
}

// ChatNote is one message on its way to the computer holding a chat.
type ChatNote struct {
	ID    string    `json:"id"`
	Text  string    `json:"text"`
	At    time.Time `json:"at"`
	State string    `json:"state" enum:"waiting,delivered"`
}

// ChatNote states.
const (
	ChatNoteWaiting   = "waiting"
	ChatNoteDelivered = "delivered"
)

// ChatSendParams is chats.send: text for whichever computer holds cell.
type ChatSendParams struct {
	Cell     string `json:"cell"`
	Text     string `json:"text"`
	ClientID string `json:"client_id,omitempty"`
}

// ChatSendResult names the message, so a client can follow it in the row's
// notes until the holder takes it.
type ChatSendResult struct {
	Note ChatNote `json:"note"`
}

// ChatsSender leaves a message for the computer holding a chat.
type ChatsSender interface {
	SendToChat(ChatSendParams) (ChatNote, error)
}

// ChatsSendOffer is an engine's answer to whether a message to a chat another
// computer holds can be sent now. A message waits on the sync service as sealed
// text until the holder takes it, which an account whose hosted storage is off
// may not keep there, and a capability that cannot work is absent, not broken:
// an engine that answers false does not advertise chats.send. An engine that
// does not implement it keeps advertising chats.send, as before.
type ChatsSendOffer interface{ ChatsSendOffered() bool }

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

// ChatOpenLiveParams is chats.open_live: the chat another computer holds, by
// its cell id.
type ChatOpenLiveParams struct {
	Cell string `json:"cell"`
}

// ChatOpenLiveResult is the chat as a local-looking agent. Its location carries
// live; a send into it is agents.send, never chats.send.
type ChatOpenLiveResult struct {
	Agent Agent `json:"agent"`
}

// ChatLiveKeyboardParams is chats.live_keyboard: take, or only ask who holds,
// the keyboard of an agent opened live.
type ChatLiveKeyboardParams struct {
	Agent string `json:"agent"`
	Take  bool   `json:"take,omitempty"`
}

// ChatLiveKeyboardResult says who holds the keyboard now.
type ChatLiveKeyboardResult struct {
	Driver LiveDriver `json:"driver"`
}

// ChatsLiveOpener opens a chat another computer holds as a live view.
type ChatsLiveOpener interface {
	OpenLive(ChatOpenLiveParams) (Agent, error)
	LiveKeyboard(ChatLiveKeyboardParams) (ChatLiveKeyboardResult, error)
}

// CallNoter is an optional engine door that hears of each agents.* call as it
// arrives, before it is run. It must not block: it only notes that the person
// touched the agent the params name.
type CallNoter interface {
	NoteCall(method string, params json.RawMessage)
}

// LiveRefused is the detail of a refused chats.open_live: the closed reason a
// live door gives (not-running, not-allowed, holder-old, removed, ...), beside
// the sentence the person reads.
type LiveRefused struct {
	Kind   string `json:"kind" enum:"live_refused"`
	Reason string `json:"reason"`
}

// ChatsLiveOffer is an engine's answer to whether live views are on now. A
// capability that cannot work is absent, not broken: an engine that answers
// false does not advertise chats.live, so no client shows the verb.
type ChatsLiveOffer interface{ ChatsLiveOffered() bool }
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

type ChatReadFollower interface{ ChatReadsFollow() bool }

type ChatEntryPageReader interface {
	EntriesWithFallback(id, before string, limit int) (EntriesResult, error)
}

// ChatPreview is a read-only excerpt of recently saved conversation text.
type ChatPreview struct {
	LastUser      string `json:"last_user,omitempty"`
	LastAssistant string `json:"last_assistant,omitempty"`
}

type ChatPreviewParams struct {
	Cell string `json:"cell"`
}

type ChatPreviewReader interface {
	PreviewChat(cell string) (ChatPreview, error)
}

func withChatsFeatures(h Hello, e Engine) Hello {
	if _, ok := e.(ChatPreviewReader); ok {
		h = withFeature(h, FeatureChatsPreview)
	}
	if d, ok := e.(ChatReadFollower); ok && d.ChatReadsFollow() {
		h = withFeature(h, FeatureChatsRead)
		if _, ok := e.(ChatEntryPageReader); ok {
			h = withFeature(h, FeatureChatReadSource)
		}
	}
	if _, ok := e.(ChatsTaker); ok {
		h = withFeature(h, FeatureChatsTake)
	}
	if _, ok := e.(ChatsMover); ok {
		h = withFeature(h, FeatureChatsMove)
	}
	if _, ok := e.(ChatsTaker); ok {
		h = withFeature(h, FeatureChatsWorking)
	}
	if _, ok := e.(ChatsSender); ok {
		if offer, asked := e.(ChatsSendOffer); !asked || offer.ChatsSendOffered() {
			h = withFeature(h, FeatureChatsSend)
		}
	}
	if _, ok := e.(ChatsLiveOpener); ok {
		if offer, asked := e.(ChatsLiveOffer); asked && offer.ChatsLiveOffered() {
			h = withFeature(h, FeatureChatsLive)
		}
	}
	if d, ok := e.(RelayReporter); ok && d.RelayKnown() {
		h = withFeature(h, FeatureSyncRelay)
	}
	return h
}
func init() {
	methods["chats.preview"] = typed(func(e Engine, p ChatPreviewParams) (*ChatPreview, error) {
		d, ok := e.(ChatPreviewReader)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not preview chats on other computers")
		}
		if _, err := required("cell", p.Cell); err != nil {
			return nil, err
		}
		got, err := d.PreviewChat(p.Cell)
		return &got, err
	})
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
			return nil, Errorf(CodeUnknownMethod, "this engine does not move chats here")
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
	methods["chats.send"] = typed(func(e Engine, p ChatSendParams) (*ChatSendResult, error) {
		d, ok := e.(ChatsSender)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not send messages to other computers")
		}
		if _, err := required("cell", p.Cell); err != nil {
			return nil, err
		}
		if _, err := required("text", p.Text); err != nil {
			return nil, err
		}
		got, err := d.SendToChat(p)
		return &ChatSendResult{Note: got}, err
	})
	methods["chats.open_live"] = typed(func(e Engine, p ChatOpenLiveParams) (*ChatOpenLiveResult, error) {
		d, ok := e.(ChatsLiveOpener)
		if offer, asked := e.(ChatsLiveOffer); !ok || !asked || !offer.ChatsLiveOffered() {
			return nil, Errorf(CodeUnknownMethod, "this engine does not open chats on other computers live")
		}
		if _, err := required("cell", p.Cell); err != nil {
			return nil, err
		}
		got, err := d.OpenLive(p)
		return &ChatOpenLiveResult{Agent: got}, err
	})
	methods["chats.live_keyboard"] = typed(func(e Engine, p ChatLiveKeyboardParams) (*ChatLiveKeyboardResult, error) {
		d, ok := e.(ChatsLiveOpener)
		if offer, asked := e.(ChatsLiveOffer); !ok || !asked || !offer.ChatsLiveOffered() {
			return nil, Errorf(CodeUnknownMethod, "this engine does not open chats on other computers live")
		}
		if _, err := required("agent", p.Agent); err != nil {
			return nil, err
		}
		got, err := d.LiveKeyboard(p)
		return &got, err
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

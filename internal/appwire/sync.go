package appwire

const (
	FeatureSyncStatus    = "sync.status"
	FeatureChatsLocation = "chats.location"
	EventSyncChanged     = "sync.changed"

	SyncQuiet       = "quiet"
	SyncOn          = "on"
	SyncOff         = "off"
	SyncUnavailable = "unavailable"

	NoticeSync        = "sync"
	NoticeMoved       = "moved"
	NoticeInterrupted = "interrupted"
)

// Sync is this computer's sync setting. Disclosure is present only after a
// hosted drive starts and until a surface acknowledges the sentence.
type Sync struct {
	Relay      *Relay          `json:"relay,omitempty"`
	Mode       string          `json:"mode" enum:"quiet,on,off,unavailable"`
	Reason     string          `json:"reason,omitempty"`
	Disclosure *SyncDisclosure `json:"disclosure,omitempty"`
	Storage    *SyncStorage    `json:"storage,omitempty"`
	// Unshared counts this computer's chats the other computers cannot list
	// yet. Absent while sync is not on here, or before the first count.
	Unshared *SyncUnshared `json:"unshared,omitempty"`
}

// SyncStorage is a local permission reading, absent before pairing or with sync
// off. Only hosted storage-off accounts have a public request URL.
type SyncStorage struct {
	State       string `json:"state" enum:"on,off"`
	RequestCode string `json:"request_code,omitempty"`
	RequestURL  string `json:"request_url,omitempty"`
	// LiveTransfer says chats can move directly between paired computers while both are running.
	LiveTransfer bool `json:"live_transfer,omitempty"`
}

// SyncUnshared is how many of this computer's chats have no directory record,
// so the other computers do not list them. Total is the sum of the rest.
type SyncUnshared struct {
	Total int `json:"total"`
	// Waiting are still to be uploaded: queued, uploading, or older chats
	// still to be copied. They are shared without anything the person does.
	Waiting int `json:"waiting"`
	// Failing are chats whose last upload failed; the engine tries again.
	Failing int `json:"failing"`
	// KeptHere are older chats a rule keeps here: last used over 30 days ago,
	// which opening one on this computer shares, or working in the home
	// folder, which stays on this computer.
	KeptHere int `json:"kept_here"`
}

type SyncDisclosure struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type SyncResult struct {
	Sync Sync `json:"sync"`
}

type SyncChanged struct {
	Sync Sync `json:"sync"`
}

// Location names the computer that can continue this chat. Its absence leaves
// the ordinary admission rules in force; chats.take also reports local ownership.
type Location struct {
	Cell         string          `json:"cell,omitempty"`
	Seen         *Seen           `json:"seen,omitempty"`
	CanTake      bool            `json:"can_take,omitempty"`
	TakeoverLine string          `json:"takeover_line,omitempty"`
	Here         bool            `json:"here"`
	Holder       *LocationHolder `json:"holder,omitempty"`
	Line         string          `json:"line,omitempty"`
	// Live says the chat is open as a live view of the computer that holds it
	// (chats.open_live), and how that view stands. It rides on the existing
	// agent.changed; a client that does not know the feature never receives an
	// agent that carries it. Absent for every chat that is held here or only read.
	Live *LocationLive `json:"live,omitempty"`
	// Keyboard says who holds the keyboard of a chat that is held and run on
	// THIS computer when another window than the one drawing it does: another
	// of the person's computers that took it, or another window here. It is the
	// same three words a live view's driver says, and it rides on the existing
	// agent.changed with remote control on and nowhere else. Absent says the
	// window drawing the chat holds it, which is every chat with nobody else in
	// it. chats.live_keyboard {agent, take:true} takes it back for this window.
	Keyboard *LiveDriver `json:"keyboard,omitempty"`
}

// The states of a live view, as the app reads them. An unknown state is read as
// live, and the permit then decides what the person may do in it.
const (
	LiveStateLive         = "live"
	LiveStateReconnecting = "reconnecting"
	LiveStateOffline      = "offline"
	LiveStateIdle         = "idle"
	// LiveStateWatching is the holder's own verdict that this computer may read
	// the chat and not type in it: the permit it holds, or the day's allowance used.
	LiveStateWatching = "watching"
)

// LocationLive is how a live view of a chat on another computer stands.
type LocationLive struct {
	State string `json:"state" enum:"live,reconnecting,offline,idle,watching"`
	// Driver names who holds the keyboard when it is not this window. Absent
	// says nobody else does.
	Driver *LiveDriver `json:"driver,omitempty"`
	// Road is how the bytes travel: "relay", or "direct" once the two computers
	// have connected to each other. It changes while a view is open and the engine
	// says so with an agent.changed of its own; it is a word of the strip and
	// nothing a call may depend on.
	Road string `json:"road,omitempty"`
	// SeenAt is when the other computer was last heard from, RFC 3339.
	SeenAt string `json:"seen_at,omitempty"`
	// Permit is what the other computer lets this one do in the chat.
	Permit *LivePermit `json:"permit,omitempty"`
}

// LiveDriver is who holds the keyboard of a live chat, as the engine's own
// driver fact says it. Yours is this window; Machine names another computer;
// Here is another window on this computer.
type LiveDriver struct {
	Yours   bool   `json:"yours,omitempty"`
	Machine string `json:"machine,omitempty"`
	Here    bool   `json:"here,omitempty"`
}

// LivePermit is what the computer holding the chat lets this one do in it.
// Reason is a finished sentence for why typing is off, shown unchanged.
type LivePermit struct {
	Watch  bool   `json:"watch"`
	Drive  bool   `json:"drive"`
	Reason string `json:"reason,omitempty"`
}

type LocationHolder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type HeldElsewhere struct {
	Kind   string          `json:"kind" enum:"held_elsewhere"`
	Holder *LocationHolder `json:"holder,omitempty"`
}

// Syncer is optional: older engines keep their hello and answer unknown_method.
type Syncer interface {
	SyncStatus() (Sync, error)
	AcknowledgeSync(id string) error
}

type ChatLocator interface {
	ChatLocationsKnown() bool
}

func withSyncFeatures(hello Hello, e Engine) Hello {
	if _, ok := e.(Syncer); ok {
		hello = withFeature(hello, FeatureSyncStatus)
	}
	if door, ok := e.(ChatLocator); ok && door.ChatLocationsKnown() {
		hello = withFeature(hello, FeatureChatsLocation)
	}
	return hello
}

func init() {
	methods["sync.get"] = typed(func(e Engine, _ struct{}) (*SyncResult, error) {
		door, ok := e.(Syncer)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve sync.get")
		}
		state, err := door.SyncStatus()
		return &SyncResult{Sync: state}, err
	})
	methods["sync.acknowledge"] = typed(func(e Engine, p IDParams) (*Empty, error) {
		door, ok := e.(Syncer)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve sync.acknowledge")
		}
		id, err := required("id", p.ID)
		if err != nil {
			return nil, err
		}
		return done(door.AcknowledgeSync(id))
	})
}

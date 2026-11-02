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

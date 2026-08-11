package palette

// The result vocabulary — the whole contract between a chosen row and the
// wiring that interprets it.
//
// It is a closed sum: the unexported marker method means no package outside
// this one can add a fourth shape, so a wiring type-switch over the three
// below is exhaustive today and stays exhaustive. That matters more than it
// looks: the alternative — a struct with a Kind field and three optional
// string members — lets a caller read RoomID off a settings row and get "",
// which is a bug that compiles. Here it does not compile.
//
// Every member is a value type carrying one identifier and nothing else. The
// palette knows ids; it does not know what they mean, which is what keeps it
// unable to execute anything.

// Result is what one row yields when the user chooses it. Implemented by
// exactly [JumpToRoom], [RunEntry] and [OpenSetting].
type Result interface {
	// Target is the one identifier the result carries, for a caller that
	// wants to log or compare a result without switching on its type.
	Target() string
	isResult()
}

// JumpToRoom asks the wiring to move the conversation to a room — a rail
// scope id, with the empty string meaning home ([rail.HomeScopeID]). It is a
// navigation, never a send: 5.18's law that a dispatch must not teleport the
// composer is about sending, and choosing a room in the palette IS the user
// asking to be moved.
type JumpToRoom struct {
	// ID is the rail scope id of the room. Empty is home.
	ID string
}

// RunEntry asks the wiring to perform a registry entry, named by its
// [registry.Entry.ID]. The palette resolves nothing about it — not the key,
// not the slash alias, not the journal mapping — because the surface that
// already routes that entry's key is the surface that must route this.
type RunEntry struct {
	// ID is the registry entry id, unique across the whole catalog.
	ID string
}

// OpenSetting asks the wiring to open one setting row for editing, named by
// its config key ("model.work", "budget.daily"). Opening, not applying: a
// palette that could write a setting would be a settings surface, and the
// value it wrote would bypass the validation and the announce hook that
// internal/config's own Apply carries.
type OpenSetting struct {
	// Key is the config setting key.
	Key string
}

// Target implements [Result].
func (r JumpToRoom) Target() string { return r.ID }

// Target implements [Result].
func (r RunEntry) Target() string { return r.ID }

// Target implements [Result].
func (r OpenSetting) Target() string { return r.Key }

func (JumpToRoom) isResult()  {}
func (RunEntry) isResult()    {}
func (OpenSetting) isResult() {}

// String names the result for a log line or a test failure.
func (r JumpToRoom) String() string { return "jump-to-room " + quoteEmpty(r.ID) }

// String names the result for a log line or a test failure.
func (r RunEntry) String() string { return "run-entry " + quoteEmpty(r.ID) }

// String names the result for a log line or a test failure.
func (r OpenSetting) String() string { return "open-setting " + quoteEmpty(r.Key) }

// quoteEmpty renders the home scope's empty id as a word rather than as
// nothing at all, so a log line saying "jump-to-room" with a blank tail cannot
// be mistaken for a truncated one.
func quoteEmpty(s string) string {
	if s == "" {
		return "(home)"
	}
	return s
}

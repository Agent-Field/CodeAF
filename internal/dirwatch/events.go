package dirwatch

import (
	"slices"
	"sort"
)

// The event frames of a socket opened with events (docs/ux-pairing-contract.md,
// section 5). They carry a string key "t" and no key "v".
const (
	EventJoined   = "joined"
	EventPresence = "presence"
	EventRevoked  = "revoked"
	EventWatching = "watching"
)

// MaxJoined is how many joined events State keeps. A consumer that reads State
// at least once per few seconds never misses one; older ones fall off.
const MaxJoined = 8

// MaxWatching is how many scoped-client watching events State keeps.
const MaxWatching = 32

// MaxRevoked is how many device-revoked events State keeps.
const MaxRevoked = 16

// Event is one event frame as it is on the wire. Fields a kind does not use
// are absent. Name is sealed under the metadata key; this package never opens it.
type Event struct {
	T        string `json:"t"`
	Device   string `json:"device"`
	Name     string `json:"name,omitempty"`
	Platform string `json:"platform,omitempty"`
	Online   *bool  `json:"online,omitempty"`
	On       *bool  `json:"on,omitempty"`
	At       int64  `json:"at"`
}

// PresenceEvent is the frame that says device went online or offline at at.
func PresenceEvent(device string, online bool, at int64) Event {
	return Event{T: EventPresence, Device: device, Online: &online, At: at}
}

// JoinedEvent is the frame that says device joined the fleet at at.
func JoinedEvent(device, name, platform string, at int64) Event {
	return Event{T: EventJoined, Device: device, Name: name, Platform: platform, At: at}
}

// RevokedEvent is the frame that says device was revoked at at.
func RevokedEvent(device string, at int64) Event {
	return Event{T: EventRevoked, Device: device, At: at}
}

// WatchingEvent is the frame that says a scoped client started or stopped watching at at.
func WatchingEvent(device string, on bool, at int64) Event {
	return Event{T: EventWatching, Device: device, On: &on, At: at}
}

// Joined is a device-joined event as State keeps it. Seq counts joined events
// seen by this feed from 1, so a consumer remembers the newest Seq it showed
// and shows each higher one once.
type Joined struct {
	Seq      uint64
	Device   string
	Name     string // sealed, as sent
	Platform string
	At       int64
}

// Watching is a scoped-client watching event as State keeps it. Seq counts
// watching events seen by this feed from 1, so a runner remembers the newest
// Seq it acted on and applies each higher transition once.
type Watching struct {
	Seq    uint64
	Device string
	On     bool
	At     int64
}

// Revoked is a device-revoked event as State keeps it. Seq counts revoked
// events seen by this feed from 1.
type Revoked struct {
	Seq    uint64
	Device string
	At     int64
}

// view is what events have told so far: the online set, the watching set, and
// the recent join, watching, and revocation transitions.
// Its slices are replaced, never edited, so a State handed to a surface stays valid.
type view struct {
	online     []string // sorted
	watching   []string // sorted
	joined     []Joined
	watched    []Watching
	revoked    []Revoked
	seq        uint64
	watchSeq   uint64
	revokedSeq uint64
}

// eventKinds is how each kind of event changes a view. An event of a kind not
// listed changes nothing, as the contract asks of every client.
var eventKinds = map[string]func(view, Event) view{
	EventPresence: applyPresence,
	EventJoined:   applyJoined,
	EventRevoked:  applyRevoked,
	EventWatching: applyWatching,
}

func (v view) withEvent(e Event) view {
	if apply, ok := eventKinds[e.T]; ok {
		return apply(v, e)
	}
	return v
}

func applyPresence(v view, e Event) view {
	if e.Online != nil && *e.Online {
		return v.withOnline(e.Device)
	}
	return v.withoutOnline(e.Device)
}

func applyJoined(v view, e Event) view {
	v.seq++
	j := Joined{Seq: v.seq, Device: e.Device, Name: e.Name, Platform: e.Platform, At: e.At}
	keep := v.joined[max(0, len(v.joined)-MaxJoined+1):]
	v.joined = append(slices.Clone(keep), j)
	return v
}

func applyRevoked(v view, e Event) view {
	v = v.withoutOnline(e.Device).withoutWatching(e.Device)
	v.revokedSeq++
	r := Revoked{Seq: v.revokedSeq, Device: e.Device, At: e.At}
	keep := v.revoked[max(0, len(v.revoked)-MaxRevoked+1):]
	v.revoked = append(slices.Clone(keep), r)
	return v
}

func applyWatching(v view, e Event) view {
	if e.On == nil {
		return v
	}
	if *e.On {
		v = v.withWatching(e.Device)
	} else {
		v = v.withoutWatching(e.Device)
	}
	v.watchSeq++
	w := Watching{Seq: v.watchSeq, Device: e.Device, On: *e.On, At: e.At}
	keep := v.watched[max(0, len(v.watched)-MaxWatching+1):]
	v.watched = append(slices.Clone(keep), w)
	return v
}

func (v view) withOnline(device string) view {
	if slices.Contains(v.online, device) {
		return v
	}
	v.online = append(slices.Clone(v.online), device)
	sort.Strings(v.online)
	return v
}

func (v view) withoutOnline(device string) view {
	v.online = slices.DeleteFunc(slices.Clone(v.online), func(d string) bool { return d == device })
	return v
}

func (v view) withWatching(device string) view {
	if slices.Contains(v.watching, device) {
		return v
	}
	v.watching = append(slices.Clone(v.watching), device)
	sort.Strings(v.watching)
	return v
}

func (v view) withoutWatching(device string) view {
	v.watching = slices.DeleteFunc(slices.Clone(v.watching), func(d string) bool { return d == device })
	return v
}

// forgotten is the view after the socket went down: nobody is known online,
// because the relay says who is only when a socket opens. Joins are kept.
func (v view) forgotten() view {
	v.online = nil
	v.watching = nil
	return v
}

// apply files a frame into the state. A pong changes nothing, an event goes
// through the view, and any other frame is a version.
func (fr Frame) apply(st *State, v *view) {
	switch {
	case fr.Pong, fr.Holds != nil:
	case fr.Event != nil:
		*v = v.withEvent(*fr.Event)
		st.Online, st.Watching = v.online, v.watching
		st.Joined, st.Watched, st.Revoked = v.joined, v.watched, v.revoked
	default:
		st.Version = fr.Version
	}
}

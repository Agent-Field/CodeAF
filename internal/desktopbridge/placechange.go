package desktopbridge

// "NOW ALSO USING <PLACE>: <SOURCE> · UNDO", DRAWN THE MOMENT A CHAT IS FILED.
//
// When a place starts or stops reaching an open conversation, that conversation
// (and no other) gets one `placeChange` event on its own ring, so the line is on
// screen immediately rather than at the next turn. The copy that survives a
// reload is the engine's own journaled note (session.NoteKindPlaces); the
// renderer de-duplicates the two by the undo token, which is the receipt id of
// the membership write and so the same on both.
//
// ONLY A CONVERSATION THIS BRIDGE HOLDS OPEN IS TOLD. A chat nobody has open has
// no screen to draw on; the journaled note is its record. A source is NAMED only
// when this place is what newly brings it: one another place already gave the
// chat is not news, and a removal names no sources at all, because "no longer
// using Release" is the whole sentence.

import (
	"path/filepath"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// placeChangeNamed is how many source labels the line spells out before it
// says "N more".
const placeChangeNamed = 3

// PlaceChange is the payload of a `placeChange` event (Event.PlaceChange).
type PlaceChange struct {
	PlaceID   string    `json:"placeId"`
	PlaceName string    `json:"placeName"`
	Tint      string    `json:"tint"`
	Sources   []string  `json:"sources"`
	Added     bool      `json:"added"`
	Undo      string    `json:"undo"`
	At        time.Time `json:"at"`
}

// changeWatch remembers, for each open conversation among the chats about to
// be filed or unfiled, which sources it was given before the write.
type changeWatch struct {
	p      *Places
	before map[string]map[string]bool
}

// watchChats reads the sources each OPEN chat uses now. A bridge with nothing to
// tell (no delivery hook, or no chat among these is open) returns nil, and a
// nil watch announces nothing.
func (p *Places) watchChats(chats []string) *changeWatch {
	if p.tell == nil || p.live == nil {
		return nil
	}
	open := map[string]bool{}
	for _, id := range p.live() {
		open[id] = true
	}
	w := &changeWatch{p: p, before: map[string]map[string]bool{}}
	snap, err := p.Store.Snapshot()
	if err != nil {
		return nil
	}
	for _, c := range chats {
		if open[c] {
			w.before[c] = p.sourceKeys(snap, c)
		}
	}
	if len(w.before) == 0 {
		return nil
	}
	return w
}

// sourceKeys is the set of source keys a chat is given, from the same
// resolver, picks and policy the engine and the Using list use.
func (p *Places) sourceKeys(snap *placegraph.Snapshot, chat string) map[string]bool {
	keys := map[string]bool{}
	for _, s := range p.bundle(snap, chat).Sources {
		keys[s.Key] = true
	}
	return keys
}

func (p *Places) bundle(snap *placegraph.Snapshot, chat string) *placegraph.Bundle {
	var picks []placegraph.Choice
	if p.choices != nil {
		picks, _ = p.choices.For(chat)
	}
	return snap.Resolve(chat, placegraph.ResolveOptions{Choices: picks, Sources: p.sourcePolicy()})
}

// announce tells each watched chat what placeID now does for it. undo maps a
// chat to the receipt id of the write that changed it; a chat the write did not
// change (already filed, already out) has no entry and hears nothing.
func (w *changeWatch) announce(placeID string, added bool, undo map[string]string) {
	if w == nil {
		return
	}
	snap, err := w.p.Store.Snapshot()
	if err != nil {
		return
	}
	pl, found := snap.Place(placeID)
	if !found {
		return
	}
	for chat, token := range undo {
		before, watched := w.before[chat]
		if !watched {
			continue
		}
		change := PlaceChange{PlaceID: pl.ID, PlaceName: pl.Name, Tint: string(pl.Tint), Sources: []string{}, Added: added, Undo: token, At: w.p.now().UTC()}
		if added {
			change.Sources = newLabels(w.p.bundle(snap, chat).Sources, placeID, before)
		}
		w.p.tell(chat, change)
	}
}

// newLabels names the sources placeID lists that the chat was not given
// before, three at most and then "N more".
func newLabels(sources []placegraph.UsedSource, placeID string, before map[string]bool) []string {
	var labels []string
	for _, s := range sources {
		if before[s.Key] || !listedBy(s, placeID) {
			continue
		}
		label := s.Label
		if label == "" {
			label = filepath.Base(s.Ref)
		}
		labels = append(labels, label)
	}
	if len(labels) <= placeChangeNamed {
		if labels == nil {
			return []string{}
		}
		return labels
	}
	more := len(labels) - placeChangeNamed
	return append(labels[:placeChangeNamed:placeChangeNamed], moreWord(more))
}

func listedBy(s placegraph.UsedSource, placeID string) bool {
	for _, from := range s.From {
		if from.PlaceID == placeID {
			return true
		}
	}
	return false
}

func moreWord(n int) string { return strconv.Itoa(n) + " more" }

// tellChat puts a placeChange event on the ring of the open conversation filed
// under chat, and on no other.
func (b *Bridge) tellChat(chat string, change PlaceChange) {
	b.mu.Lock()
	var target *conversation
	for _, s := range b.sessions {
		if ChatIDFromSessionFile(s.conn.Welcome.SessionFile) == chat {
			target = s
			break
		}
	}
	b.mu.Unlock()
	if target == nil {
		return
	}
	text := "No longer using " + change.PlaceName
	if change.Added {
		text = "Now also using " + change.PlaceName
	}
	target.publish(Record{Type: "event", Event: &Event{Kind: "placeChange", Text: text, PlaceChange: &change}})
}

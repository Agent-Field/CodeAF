package placegraph

// Memberships: which chats are filed under which places, and the queries that
// answer "what is in this place", "where is this chat" and "what is in no place".
//
// A chat may be in no place, one, or several; unplaced is a valid steady state and
// has no rows. The chat id is whatever canonical id the caller uses; this package
// never checks the chat exists and never deletes one. A caller that learns a chat
// is gone calls ForgetChat.

import (
	"fmt"
	"time"
)

// ---- Snapshot queries ------------------------------------------------------

// activeIn reports whether a membership counts: its place exists and is not archived.
func (s *Snapshot) active(placeID string) bool {
	i, ok := s.byID[placeID]
	return ok && !s.Places[i].Archived
}

// ChatsIn lists the chat ids filed in a place, each once, in the order they were
// filed. With includeDescendants, chats in any active place below it are included
// too; a chat in a diamond's shared child is still listed once. RootID with
// includeDescendants lists every chat that is in any active place. Asking for an
// archived place by id still answers (that is how an archived Home is read).
func (s *Snapshot) ChatsIn(placeID string, includeDescendants bool) []string {
	want := map[string]bool{}
	if placeID != RootID {
		want[placeID] = true
	}
	if includeDescendants {
		for _, p := range s.Descendants(placeID, false) {
			want[p.ID] = true
		}
	}
	seen := map[string]bool{}
	out := []string{}
	for _, m := range s.Memberships {
		if !want[m.PlaceID] || seen[m.ChatID] {
			continue
		}
		if m.PlaceID != placeID && !s.active(m.PlaceID) {
			continue
		}
		seen[m.ChatID] = true
		out = append(out, m.ChatID)
	}
	return out
}

// PlacesOf lists a chat's memberships in the order they were filed, archived
// places included (the rows are real; callers filter for display).
func (s *Snapshot) PlacesOf(chatID string) []Membership {
	var out []Membership
	for _, m := range s.Memberships {
		if m.ChatID == chatID {
			out = append(out, m)
		}
	}
	return out
}

// ContextPlace is one place whose context reaches a chat, and how: Level 0 is a
// place the chat is filed in, 1 and 2 are ancestors of those.
type ContextPlace struct {
	PlaceID string `json:"placeId"`
	Level   int    `json:"level"`
}

// ContextPlaces is the set of places a chat draws context from: its active
// memberships plus each one's ancestors up to ContextAncestorLevels, de-duplicated
// at the NEAREST level (design 6e). It depends only on membership, never on which
// strip a tab sits in. Order: level 0 in filing order, then 1, then 2. Resolving
// the context itself (conflicts, budget) is a later layer that starts from this.
func (s *Snapshot) ContextPlaces(chatID string) []ContextPlace {
	best := map[string]int{}
	var order []string
	note := func(id string, level int) {
		if lv, ok := best[id]; !ok {
			best[id] = level
			order = append(order, id)
		} else if level < lv {
			best[id] = level
		}
	}
	for _, m := range s.PlacesOf(chatID) {
		if !s.active(m.PlaceID) {
			continue
		}
		note(m.PlaceID, 0)
		for _, a := range s.ContextAncestors(m.PlaceID) {
			note(a.PlaceID, a.Level)
		}
	}
	out := make([]ContextPlace, 0, len(order))
	for lvl := 0; lvl <= ContextAncestorLevels; lvl++ {
		for _, id := range order {
			if best[id] == lvl {
				out = append(out, ContextPlace{PlaceID: id, Level: lvl})
			}
		}
	}
	return out
}

// Unplaced filters the CALLER'S canonical chat ids down to those in no active
// place: the contents of Now. The store has no list of chats of its own, so the
// caller supplies them (the engine's session list). Order is kept, repeats and
// invalid ids are dropped. A chat whose only places are archived counts as
// unplaced, so archiving a place never hides its chats from Now.
func (s *Snapshot) Unplaced(chatIDs []string) []string {
	placed := map[string]bool{}
	for _, m := range s.Memberships {
		if s.active(m.PlaceID) {
			placed[m.ChatID] = true
		}
	}
	seen := map[string]bool{}
	out := []string{}
	for _, id := range chatIDs {
		if id == "" || seen[id] || placed[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// ---- Store mutations -------------------------------------------------------

func (st *State) hasMembership(chatID, placeID string) int {
	for i, m := range st.Memberships {
		if m.ChatID == chatID && m.PlaceID == placeID {
			return i
		}
	}
	return -1
}

func (st *State) chatPlaceCount(chatID string) int {
	n := 0
	for _, m := range st.Memberships {
		if m.ChatID == chatID {
			n++
		}
	}
	return n
}

func (st *State) addMembership(chatID, placeID string, by AddedBy, now time.Time) error {
	p, err := st.needPlace(placeID)
	if err != nil {
		return err
	}
	if p.Archived {
		return fmt.Errorf("%w: %s", ErrArchived, placeID)
	}
	if len(st.Memberships) >= MaxMemberships {
		return fmt.Errorf("%w: %d memberships", ErrTooLarge, MaxMemberships)
	}
	if st.chatPlaceCount(chatID) >= MaxChatPlaces {
		return fmt.Errorf("%w: a chat can be in at most %d places", ErrTooLarge, MaxChatPlaces)
	}
	st.Memberships = append(st.Memberships, Membership{ChatID: chatID, PlaceID: placeID, AddedBy: by, At: now})
	return nil
}

func (st *State) removeMembership(chatID, placeID string) bool {
	i := st.hasMembership(chatID, placeID)
	if i < 0 {
		return false
	}
	st.Memberships = append(st.Memberships[:i], st.Memberships[i+1:]...)
	return true
}

// AddChat files a chat under a place. Already filed there: the existing row is
// returned and nothing changes (no revision, no receipt). NowID is refused: Now is
// "in no place", so to put a chat there use RemoveChat or MoveChat.
func (s *Store) AddChat(chatID, placeID string, by AddedBy) (Membership, Receipt, error) {
	var out Membership
	rc, err := s.mutate(func(st *State, now time.Time) (*change, error) {
		if err := validChatID(chatID); err != nil {
			return nil, err
		}
		if !by.Valid() {
			return nil, fmt.Errorf("%w: addedBy %q", ErrInvalid, by)
		}
		if i := st.hasMembership(chatID, placeID); i >= 0 {
			out = st.Memberships[i]
			return nil, nil
		}
		if err := st.addMembership(chatID, placeID, by, now); err != nil {
			return nil, err
		}
		out = st.Memberships[len(st.Memberships)-1]
		return &change{ActionFile, chatID}, nil
	})
	if err != nil {
		return Membership{}, Receipt{}, err
	}
	return out, rc, nil
}

// RemoveChat unfiles a chat from one place. Not filed there: nothing happens.
func (s *Store) RemoveChat(chatID, placeID string) (Receipt, error) {
	return s.mutate(func(st *State, _ time.Time) (*change, error) {
		if !st.removeMembership(chatID, placeID) {
			return nil, nil
		}
		return &change{ActionUnfile, chatID}, nil
	})
}

// MoveChat takes a chat out of `from` and files it under `to` in one commit.
// `from` may be NowID (the chat was unplaced: only the filing happens) and `to` may
// be NowID (unfile: only the removal happens). If the chat is already under `to`,
// the move just removes it from `from`. A `from` the chat is not in is ErrNotFound.
func (s *Store) MoveChat(chatID, from, to string, by AddedBy) (Receipt, error) {
	return s.mutate(func(st *State, now time.Time) (*change, error) {
		if err := validChatID(chatID); err != nil {
			return nil, err
		}
		if !by.Valid() {
			return nil, fmt.Errorf("%w: addedBy %q", ErrInvalid, by)
		}
		if from == to {
			return nil, nil
		}
		if from != NowID {
			if st.hasMembership(chatID, from) < 0 {
				return nil, fmt.Errorf("%w: chat %s is not in %s", ErrNotFound, chatID, from)
			}
		}
		if to != NowID && st.hasMembership(chatID, to) < 0 {
			// Check the destination before touching the source so a refusal leaves both.
			if _, err := st.needPlace(to); err != nil {
				return nil, err
			}
			if st.find(to).Archived {
				return nil, fmt.Errorf("%w: %s", ErrArchived, to)
			}
		}
		if from != NowID {
			st.removeMembership(chatID, from)
		}
		if to != NowID && st.hasMembership(chatID, to) < 0 {
			if err := st.addMembership(chatID, to, by, now); err != nil {
				return nil, err
			}
		}
		return &change{ActionMove, chatID}, nil
	})
}

// ForgetChat drops every membership of a chat, for when the engine no longer has
// it. No chat data is touched; this only removes our rows.
func (s *Store) ForgetChat(chatID string) (Receipt, error) {
	return s.mutate(func(st *State, _ time.Time) (*change, error) {
		kept := st.Memberships[:0:0]
		for _, m := range st.Memberships {
			if m.ChatID != chatID {
				kept = append(kept, m)
			}
		}
		if len(kept) == len(st.Memberships) {
			return nil, nil
		}
		st.Memberships = kept
		return &change{ActionForgetChat, chatID}, nil
	})
}

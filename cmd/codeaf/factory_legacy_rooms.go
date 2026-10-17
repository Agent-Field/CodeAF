package main

// A job the factory ran before Stream.Room was persisted carries no Room, but
// its stages left a trace of their own: every stage joined the item's team
// under its transcript path (stageJoinTeam), so a team that holds ALL of the
// item's recorded Phase.Chat paths and its Talk, and is led by that Talk, is
// the team the factory used. This file proves that and nothing weaker. It reads
// no name, handle, title or first prompt, and any doubt recovers nothing.

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// legacyRoomClaim is what the evidence pass found for one item: the team, and
// the exact record it rests on, kept so the compare-and-set can refuse an item
// that changed since.
type legacyRoomClaim struct {
	ID    int
	Room  string
	Talk  string
	Chats []string
}

// legacyRoomClaims is the read-only evidence pass: item id to the one team that
// provably is its room. An item missing from the answer is unknown. ALL
// CONDITIONS ARE REQUIRED, AND NOTHING HERE WRITES.
func legacyRoomClaims(f *teams.File, items []factory.Item) map[int]legacyRoomClaim {
	claims := map[int]legacyRoomClaim{}
	taken := map[string]bool{}
	for _, it := range items {
		if it.Stream != nil {
			if room := strings.TrimSpace(it.Stream.Room); room != "" {
				taken[room] = true
			}
		}
	}
	for _, it := range items {
		if c, ok := legacyRoomClaimFor(f, it); ok {
			claims[it.ID] = c
		}
	}
	// One team per item: two items resolving to one team, or to a team another
	// item already records, is ambiguity and both abstain.
	count := map[string]int{}
	for _, c := range claims {
		count[c.Room]++
	}
	for id, c := range claims {
		if count[c.Room] > 1 || taken[c.Room] {
			delete(claims, id)
		}
	}
	return claims
}

func legacyRoomClaimFor(f *teams.File, it factory.Item) (legacyRoomClaim, bool) {
	none := legacyRoomClaim{}
	if it.Stream == nil || strings.TrimSpace(it.Stream.Room) != "" {
		return none, false
	}
	talk := strings.TrimSpace(it.Talk)
	chats := legacyPhaseChats(it)
	if talk == "" || len(chats) == 0 {
		return none, false
	}
	// Closed and archived teams count as holders, so a closed duplicate forces
	// an abstain instead of being skipped.
	var holders []int
	for i := range f.Teams {
		all := legacyHolds(f.Teams[i], talk) >= 0
		for _, c := range chats {
			all = all && legacyHolds(f.Teams[i], c) >= 0
		}
		if all {
			holders = append(holders, i)
		}
	}
	if len(holders) != 1 {
		return none, false
	}
	t := f.Teams[holders[0]]
	if t.Root || t.Closed() || factoryIsParent(f, t.ID) {
		return none, false
	}
	// The lead is the Talk: memberMove refuses to move a manager, so a person
	// who moved the stages out cannot have taken the Talk-as-lead with them.
	lead := t.Members[legacyHolds(t, talk)]
	if t.Manager == "" || t.Manager != lead.Key {
		return none, false
	}
	// A stage the manager started is a crew member, not a factory stage.
	for _, c := range chats {
		if m := t.Members[legacyHolds(t, c)]; m.Started || m.StartEntry != "" {
			return none, false
		}
	}
	return legacyRoomClaim{ID: it.ID, Room: t.ID, Talk: talk, Chats: chats}, true
}

// legacyPhaseChats is the item's distinct non-empty Phase.Chat values.
func legacyPhaseChats(it factory.Item) []string {
	var out []string
	seen := map[string]bool{}
	for _, ph := range it.Stream.Phases {
		chat := strings.TrimSpace(ph.Chat)
		if chat != "" && !seen[chat] {
			seen[chat] = true
			out = append(out, chat)
		}
	}
	return out
}

// legacyHolds is the index of the CURRENT member of t that holds path, or -1.
// The member's File is the very string stageJoinTeam stored, and its Key is that
// path resolved (including a path through the adopted legacy home link).
func legacyHolds(t teams.Team, path string) int {
	want := filepath.Clean(path)
	real := legacyReal(path)
	for i, m := range t.Members {
		if m.File == "" || m.Key == "" || filepath.Clean(m.File) != want {
			continue
		}
		if m.Key == want || m.Key == real || legacyReal(m.Key) == real {
			return i
		}
	}
	return -1
}

func legacyReal(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return filepath.Clean(p)
}

var errLegacyRoomChanged = errors.New("the item changed since its room was proved")

// factoryRecoverLegacyRooms recovers the Room of items the factory ran before it
// was persisted and answers the rooms and chats [factoryStampStore] should
// stamp. The evidence is derived twice: once on an unlocked snapshot, and again
// under the teams lock (so a concurrent edit cannot invalidate it). The store
// writes happen AFTER that lock is released, never inside it, so the teams lock
// and an item lock are never nested. Each write is a compare-and-set that only
// fills an empty Room and only while the item's Talk and chats are what the
// proof read. Whatever the teams do afterwards, the stamp stays a historical
// fact of the job.
func factoryRecoverLegacyRooms(profileDir string, st *store.Store, items []factory.Item) (rooms []string, chats map[string][]string) {
	chats = map[string][]string{}
	snap, err := teams.Snapshot(profileDir)
	if err != nil || len(legacyRoomClaims(snap, items)) == 0 {
		return nil, chats
	}
	var proved map[int]legacyRoomClaim
	if err := teams.Inspect(profileDir, func(f *teams.File) error {
		proved = legacyRoomClaims(f, items)
		if len(proved) == 0 {
			return errLegacyRoomChanged
		}
		return nil
	}); err != nil {
		return nil, chats
	}
	for _, c := range proved {
		err := st.Annotate(c.ID, func(it *factory.Item) error {
			if it.Stream == nil || strings.TrimSpace(it.Stream.Room) != "" ||
				strings.TrimSpace(it.Talk) != c.Talk || !sameStrings(legacyPhaseChats(*it), c.Chats) {
				return errLegacyRoomChanged
			}
			it.Stream.Room = c.Room
			return nil
		})
		if err != nil {
			continue
		}
		rooms = append(rooms, c.Room)
		chats[c.Room] = append(chats[c.Room], c.Chats...)
	}
	return rooms, chats
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

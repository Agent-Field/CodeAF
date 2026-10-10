package placegraph

// Status roll-up: what a place's dot, count and hover sentence say, computed from
// the chats filed under it and under everything below it.
//
// The graph never knows a chat's state; the engine does. The caller passes one
// ChatState per chat it knows about and gets back one Status per active place. A
// parent's dot comes from its children (a place with a failed chat three levels
// down shows failed), and a chat filed in two places under one parent (a diamond)
// is counted ONCE in that parent, so the badge never promises more than there is.

import "sort"

// ChatState is the slice of a chat's state the roll-up needs.
type ChatState struct {
	NeedsYou int  // questions or approvals waiting on the person
	Failed   bool // the chat's last run failed
	Running  bool // the chat is working now
}

// Origin names one place holding chats that need the person, for the hover
// sentence ("2 in Billing, 1 in Docs"). A chat that needs the person is credited to
// the nearest place under the rolled-up one that holds it, never to two.
type Origin struct {
	PlaceID  string `json:"placeId"`
	NeedsYou int    `json:"needsYou"`
}

// Status is one place's roll-up.
type Status struct {
	NeedsYou int  `json:"needsYou"` // sum over ChatsAll, each chat once
	Failed   bool `json:"failed"`
	Running  bool `json:"running"`
	// Chats are filed directly here; ChatsAll adds every active descendant's, each
	// chat once.
	Chats    int `json:"chats"`
	ChatsAll int `json:"chatsAll"`
	// Children and Descendants count active places only.
	Children    int      `json:"children"`
	Descendants int      `json:"descendants"`
	Origins     []Origin `json:"origins,omitempty"`
}

// Totals are the numbers the All places header and the Now row show.
type Totals struct {
	TopLevel int `json:"topLevel"` // active top-level places
	All      int `json:"all"`      // every active place
	Unplaced int `json:"unplaced"` // known chats in no active place
}

// RollupResult is the roll-up of the whole graph. Places holds an entry for every
// active place and for RootID (which rolls up everything and has no direct chats).
type RollupResult struct {
	Places map[string]Status `json:"places"`
	Totals Totals            `json:"totals"`
}

// Rollup computes every place's Status from the chat states the caller knows. It is
// pure. Archived places get no entry and contribute no chats, but the walk passes
// through them so their children stay reachable, as Descendants does. Cost is one
// stamped walk per place, so a tree or a shallow graph is near-linear in places
// plus memberships; only a deep chain pays the subtree size at every level, which
// is why the bound test uses 200 places.
func Rollup(s *Snapshot, states map[string]ChatState) RollupResult {
	kids := s.kids()
	direct := map[string][]string{}
	placed := map[string]bool{}
	for _, m := range s.Memberships {
		if s.active(m.PlaceID) {
			direct[m.PlaceID] = append(direct[m.PlaceID], m.ChatID)
			placed[m.ChatID] = true
		}
	}
	order := make(map[string]int, len(s.Places)+1)
	order[RootID] = -1
	res := RollupResult{Places: map[string]Status{}}
	for i, p := range s.Places {
		order[p.ID] = i
		if p.Archived {
			continue
		}
		res.Totals.All++
		if len(p.Parents) == 0 {
			res.Totals.TopLevel++
		}
	}
	for id := range states {
		if id != "" && !placed[id] {
			res.Totals.Unplaced++
		}
	}

	nodeStamp := map[string]string{}
	chatStamp := map[string]string{}
	roll := func(root string) Status {
		var st Status
		needs := map[string]int{}
		var visit []string
		stack := []string{root}
		nodeStamp[root] = root
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if n != root && s.active(n) {
				st.Descendants++
			}
			if s.active(n) {
				for _, c := range direct[n] {
					if chatStamp[c] == root {
						continue
					}
					chatStamp[c] = root
					st.ChatsAll++
					cs := states[c]
					st.NeedsYou += cs.NeedsYou
					st.Failed = st.Failed || cs.Failed
					st.Running = st.Running || cs.Running
					if cs.NeedsYou > 0 {
						if needs[n] == 0 {
							visit = append(visit, n)
						}
						needs[n] += cs.NeedsYou
					}
				}
			}
			for _, k := range kids[n] {
				if nodeStamp[k] != root {
					nodeStamp[k] = root
					stack = append(stack, k)
				}
			}
		}
		sort.Slice(visit, func(i, j int) bool { return order[visit[i]] < order[visit[j]] })
		for _, n := range visit {
			st.Origins = append(st.Origins, Origin{PlaceID: n, NeedsYou: needs[n]})
		}
		return st
	}

	for _, p := range s.Places {
		if p.Archived {
			continue
		}
		st := roll(p.ID)
		st.Chats = len(direct[p.ID])
		for _, c := range kids[p.ID] {
			if s.active(c) {
				st.Children++
			}
		}
		res.Places[p.ID] = st
	}
	rs := roll(RootID)
	for _, c := range kids[RootID] {
		if s.active(c) {
			rs.Children++
		}
	}
	res.Places[RootID] = rs
	return res
}

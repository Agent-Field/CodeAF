package stream

import (
	"context"
	"sort"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// Births offers one new place for each group of chats in Now that belong
// together. Belonging is a link in the kNN graph: each chat's nearest
// neighbours, kept only when they are at least NeighbourFloor apart, joined
// in either direction. A group smaller than Quorum is not a place, and a
// probe or a duplicate is not a member and cannot glue two groups together.
// Arrivals never reach this; File does not call it.
func (s *Stream) Births(ctx context.Context, t *placeorg.Tree, inbox []placeorg.Arrival) ([]placeorg.Op, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var chats []placeorg.Arrival
	for _, a := range inbox {
		if !s.countsTowardBirth(t, a) {
			continue
		}
		chats = append(chats, a)
	}
	if len(chats) < placeorg.Quorum {
		return nil, nil
	}
	parent := make([]int, len(chats))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	join := func(i, j int) {
		ri, rj := find(i), find(j)
		if ri != rj {
			parent[rj] = ri
		}
	}
	for i := range chats {
		for _, j := range neighbours(chats, i) {
			join(i, j)
		}
	}
	groups := map[int][]string{}
	for i, a := range chats {
		groups[find(i)] = append(groups[find(i)], a.Chat.ID)
	}
	var ops []placeorg.Op
	for _, members := range groups {
		if len(members) < placeorg.Quorum {
			continue
		}
		sort.Strings(members)
		ops = append(ops, placeorg.Op{
			Kind:     placeorg.OpBirth,
			Stage:    stage,
			Members:  members,
			Proposed: true,
			Why:      "These chats in Now belong together",
		})
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].Members[0] < ops[j].Members[0] })
	return ops, nil
}

// countsTowardBirth keeps a chat that is in Now, is not a probe, and is not
// a duplicate of another chat. A chat the tree already homed is not in Now.
func (s *Stream) countsTowardBirth(t *placeorg.Tree, a placeorg.Arrival) bool {
	id := a.Chat.ID
	if id == "" || len(a.Vector) == 0 {
		return false
	}
	if t != nil {
		if _, placed := t.Home(id); placed {
			return false
		}
	}
	if s == nil {
		return true
	}
	switch s.Kinds[id] {
	case placeorg.KindProbe, placeorg.KindDuplicate:
		return false
	}
	if rep := s.Representative[id]; rep != "" && rep != id {
		return false
	}
	return true
}

// neighbours lists the indexes of the chats within NeighbourK and at or
// above NeighbourFloor of i. The edge is recorded from either side, so a
// chat that is close to another joins it even when the other has nearer
// neighbours of its own.
func neighbours(chats []placeorg.Arrival, i int) []int {
	type pair struct {
		j int
		s float32
	}
	var ps []pair
	vi := chats[i].Vector
	for j := range chats {
		if j == i || len(chats[j].Vector) != len(vi) {
			continue
		}
		s := dot(unit(vi), unit(chats[j].Vector))
		if s >= NeighbourFloor {
			ps = append(ps, pair{j, s})
		}
	}
	sort.Slice(ps, func(a, b int) bool {
		if ps[a].s != ps[b].s {
			return ps[a].s > ps[b].s
		}
		return ps[a].j < ps[b].j
	})
	if len(ps) > NeighbourK {
		ps = ps[:NeighbourK]
	}
	out := make([]int, len(ps))
	for k, p := range ps {
		out[k] = p.j
	}
	return out
}

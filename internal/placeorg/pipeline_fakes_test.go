package placeorg

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// The fake world for the Runner's tests: three groups of six chats, stages
// that behave the way the real ones are documented to, and a log of the order
// they were called in. Nothing here needs a model, a network or a disk.

const (
	fakeGroups   = 3
	fakeGroupLen = 6
)

type memJournal struct {
	mu      sync.Mutex
	entries []Entry
}

func (j *memJournal) Write(e Entry) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.entries = append(j.entries, e)
	return nil
}

func (j *memJournal) kinds() []EntryKind {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]EntryKind, len(j.entries))
	for i, e := range j.entries {
		out[i] = e.Kind
	}
	return out
}

func (j *memJournal) has(k EntryKind) bool {
	for _, got := range j.kinds() {
		if got == k {
			return true
		}
	}
	return false
}

type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(s string) {
	l.mu.Lock()
	l.calls = append(l.calls, s)
	l.mu.Unlock()
}

func fakeCorpus() *Corpus {
	c := &Corpus{}
	for g := 0; g < fakeGroups; g++ {
		for i := 0; i < fakeGroupLen; i++ {
			c.Chats = append(c.Chats, Chat{
				ID: fmt.Sprintf("chat-%d-%d", g, i), Title: fmt.Sprintf("group %d chat %d", g, i),
				Created: time.Date(2026, 1, 1+g*10+i, 0, 0, 0, 0, time.UTC), Replies: 3,
			})
		}
	}
	return c
}

func groupOf(chatID string) int {
	var g, i int
	fmt.Sscanf(chatID, "chat-%d-%d", &g, &i)
	return g
}

type fakeDedup struct{ log *callLog }

func (f fakeDedup) Dedup(context.Context, []Chat) (map[string]string, error) {
	f.log.add("dedup")
	return map[string]string{}, nil
}

type fakeKind struct{ log *callLog }

func (f fakeKind) Kinds(_ context.Context, chats []Chat, _ map[string]string) (map[string]Kind, error) {
	f.log.add("kind")
	out := map[string]Kind{}
	for _, c := range chats {
		out[c.ID] = KindConversation
	}
	return out, nil
}

type fakeAnchors struct{ log *callLog }

func (f fakeAnchors) Anchors(context.Context, []Chat, []string) (map[string]AnchorSet, error) {
	f.log.add("anchors")
	return map[string]AnchorSet{}, nil
}

type fakeVectors struct {
	log  *callLog
	fell bool
}

func (f fakeVectors) Vectorize(_ context.Context, c *Corpus, _, _ Embedder) (SpaceID, [][]float32, bool, error) {
	f.log.add("vectors")
	out := make([][]float32, len(c.Chats))
	for i, ch := range c.Chats {
		v := make([]float32, fakeGroups)
		v[groupOf(ch.ID)] = 1
		out[i] = v
	}
	return SpacePrimary, out, f.fell, nil
}

type fakeKNN struct{ log *callLog }

func (f fakeKNN) KNN(_ context.Context, v [][]float32, _ map[string]AnchorSet, _ []string) (Graph, error) {
	f.log.add("neighbours")
	return Graph{N: len(v)}, nil
}

type fakeCommunities struct {
	log    *callLog
	onCall func(n int)
	calls  int
}

func (f *fakeCommunities) Partition(_ context.Context, g Graph, _ float64, _ uint64) ([]int, error) {
	f.log.add("communities")
	f.calls++
	if f.onCall != nil {
		f.onCall(f.calls)
	}
	// The chats arrive in group order, so the label is the group.
	out := make([]int, g.N)
	for i := range out {
		out[i] = i / fakeGroupLen
	}
	return out, nil
}

type fakeNest struct{ log *callLog }

func (f fakeNest) Nest(_ context.Context, c *Corpus, fine, mid, coarse []int) (*Tree, error) {
	f.log.add("nest")
	var nodes []BuiltNode
	homes := map[string]string{}
	seen := map[int]bool{}
	for i, ch := range c.Chats {
		g := fine[i]
		leaf := fmt.Sprintf("f/%d", g)
		if !seen[g] {
			seen[g] = true
			nodes = append(nodes,
				BuiltNode{ID: fmt.Sprintf("c/%d", g), Depth: 1},
				BuiltNode{ID: fmt.Sprintf("m/%d", g), Parent: fmt.Sprintf("c/%d", g), Depth: 2},
				BuiltNode{ID: leaf, Parent: fmt.Sprintf("m/%d", g), Depth: 3},
			)
		}
		homes[ch.ID] = leaf
	}
	for i := range nodes {
		for chat, h := range homes {
			if h == nodes[i].ID {
				nodes[i].Members = append(nodes[i].Members, chat)
			}
		}
		sort.Strings(nodes[i].Members)
	}
	return Assemble(nodes, homes, nil)
}

type fakeAudit struct{ log *callLog }

func (f fakeAudit) Fallbacks() []Fallback {
	return []Fallback{{When: "judging a place", Does: "keep it"}}
}

func (f fakeAudit) Audit(context.Context, *Tree, *Corpus, [][]float32, Judge) ([]Op, error) {
	f.log.add("audit")
	return nil, nil
}

type fakePark struct{ log *callLog }

func (f fakePark) Park(_ context.Context, t *Tree, c *Corpus, _ [][]float32, _ SpaceID) ([]Op, error) {
	f.log.add("park")
	var ops []Op
	for _, ch := range c.Chats {
		home, ok := t.Home(ch.ID)
		if !ok {
			ops = append(ops, Op{Kind: OpInbox, Stage: StagePark, Chat: ch.ID, Why: "nothing is close"})
			continue
		}
		ops = append(ops, Op{Kind: OpPark, Stage: StagePark, Chat: ch.ID, Place: home, Why: "it fits"})
	}
	return ops, nil
}

type fakeName struct {
	log *callLog
	// asks makes the namer put a question to the Judge for each place.
	asks bool
}

func (f fakeName) Fallbacks() []Fallback {
	return []Fallback{{When: "naming", Does: "the first candidate"}}
}

func (f fakeName) Name(ctx context.Context, t *Tree, _ *Corpus, judge Judge) ([]Op, error) {
	f.log.add("name")
	var ids []string
	var walk func(string)
	walk = func(id string) {
		ids = append(ids, id)
		n, _ := t.Node(id)
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, r := range t.Roots() {
		walk(r)
	}
	var ops []Op
	for i, id := range ids {
		name := fmt.Sprintf("place%s", strings.Repeat("x", i+1))
		if f.asks {
			valid := func(raw string) (string, bool) { raw = strings.TrimSpace(raw); return raw, raw != "" }
			pick := judge.Write(ctx, Question{Role: RolePlaceName, Purpose: "Name this place."}, valid, name)
			name = pick.Written
		}
		n, _ := t.Node(id)
		ops = append(ops, Op{Kind: OpName, Stage: StageName, Place: id, Name: name, Why: "named", Proposed: n.Person})
	}
	return ops, nil
}

type fakeReview struct{ log *callLog }

func (f fakeReview) Fallbacks() []Fallback { return []Fallback{{When: "reviewing", Does: "no change"}} }
func (f fakeReview) Review(context.Context, *Tree, *Corpus, Judge) ([]Op, error) {
	f.log.add("review")
	return nil, nil
}

type fakeLink struct{ log *callLog }

func (f fakeLink) Link(context.Context, *Tree, *Corpus, [][]float32) ([]Op, error) {
	f.log.add("link")
	return nil, nil
}

type fakeStream struct{ log *callLog }

func (f fakeStream) Fallbacks() []Fallback {
	return []Fallback{{When: "a tie", Does: "the shallower place"}}
}

// File puts an arrival at the leaf of its own group, which the fake tree
// names by position.
func (f fakeStream) File(_ context.Context, t *Tree, a Arrival, _ Judge) (Filing, error) {
	f.log.add("file")
	for _, id := range t.Roots() {
		n, _ := t.Node(id)
		if n.Name == fmt.Sprintf("group%d", groupOf(a.Chat.ID)) {
			return Filing{Op: Op{Kind: OpFile, Stage: "stream", Chat: a.Chat.ID, Place: id, Why: "fits"}}, nil
		}
	}
	return Filing{Op: Op{Kind: OpInbox, Stage: "stream", Chat: a.Chat.ID, Why: "no fit"}}, nil
}

func (f fakeStream) Births(context.Context, *Tree, []Arrival) ([]Op, error) {
	f.log.add("births")
	return nil, nil
}

func fakeStages(log *callLog) Stages {
	return Stages{
		Dedup: fakeDedup{log}, Kind: fakeKind{log}, Anchors: fakeAnchors{log}, Vectors: fakeVectors{log: log},
		Neighbours: fakeKNN{log}, Communities: &fakeCommunities{log: log}, Nest: fakeNest{log},
		Audit: fakeAudit{log}, Park: fakePark{log}, Name: fakeName{log: log}, Review: fakeReview{log},
		Link: fakeLink{log}, Stream: fakeStream{log},
	}
}

func fixedClock() func() time.Time {
	t := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	return func() time.Time { return t }
}

// askCounter is an Asker that answers every question the same and counts.
type askCounter struct {
	mu    sync.Mutex
	calls int
}

func (a *askCounter) ask(_ context.Context, _ placegraph.ModelRequest) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	return "name" + strings.Repeat("y", a.calls), nil
}

func newTestRunner(t *testing.T, stages Stages) (*Runner, *memJournal) {
	t.Helper()
	j := &memJournal{}
	return &Runner{Stages: stages, Journal: j}, j
}

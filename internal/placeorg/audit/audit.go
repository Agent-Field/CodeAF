package audit

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// Auditor is the op algebra. The zero value is ready to use.
type Auditor struct{}

// Compile-time checks: the stage is an Auditor, and because it asks a Judge
// it states what it does when the Judge does not answer.
var (
	_ placeorg.Auditor = Auditor{}
	_ placeorg.Judged  = Auditor{}
)

// Fallbacks is the one degraded path. An unanswered facet question keeps the
// place; it does not turn the place into a view.
func (Auditor) Fallbacks() []placeorg.Fallback {
	return []placeorg.Fallback{{
		When: "deciding whether a place is about one thing",
		Does: "keep the place",
	}}
}

// Audit proposes collapse, dissolve, interpose, lift, demote and split, in
// that order. Later ops see the shadow left by earlier ones. Nothing here
// writes the Tree.
func (a Auditor) Audit(ctx context.Context, t *placeorg.Tree, corpus *placeorg.Corpus, vectors [][]float32, judge placeorg.Judge) ([]placeorg.Op, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if t == nil || corpus == nil {
		return nil, fmt.Errorf("placeorg audit: a tree and a corpus are required")
	}
	if vectors != nil && len(vectors) != len(corpus.Chats) {
		return nil, fmt.Errorf("placeorg audit: %d vectors for %d chats", len(vectors), len(corpus.Chats))
	}
	f := loadForest(t)
	ix := indexCorpus(corpus, vectors)
	var ops []placeorg.Op
	ops = append(ops, a.collapse(f)...)
	ops = append(ops, a.dissolve(f)...)
	ops = append(ops, a.interpose(f, ix)...)
	ops = append(ops, a.lift(f, ix)...)
	demoted, err := a.demote(ctx, f, ix, judge)
	if err != nil {
		return nil, err
	}
	ops = append(ops, demoted...)
	split, err := a.split(ctx, f, ix, judge)
	if err != nil {
		return nil, err
	}
	ops = append(ops, split...)
	return ops, nil
}

// corpusIndex is the corpus in chat-id form, so a shadow that stores ids
// can find a title, a vector and an anchor without scanning the slice.
type corpusIndex struct {
	order  map[string]int
	title  map[string]string
	vector map[string][]float32
	anchor map[string]placeorg.AnchorSet
}

func indexCorpus(c *placeorg.Corpus, vectors [][]float32) *corpusIndex {
	ix := &corpusIndex{
		order:  map[string]int{},
		title:  map[string]string{},
		vector: map[string][]float32{},
		anchor: c.Anchors,
	}
	if ix.anchor == nil {
		ix.anchor = map[string]placeorg.AnchorSet{}
	}
	for i, ch := range c.Chats {
		ix.order[ch.ID] = i
		ix.title[ch.ID] = oneLine(ch.Title, ch.Text)
		if vectors != nil && i < len(vectors) {
			ix.vector[ch.ID] = vectors[i]
		}
	}
	return ix
}

func oneLine(title, text string) string {
	line := strings.TrimSpace(title)
	if line == "" {
		line = strings.TrimSpace(text)
	}
	line = strings.ReplaceAll(line, "\n", " ")
	r := []rune(line)
	if len(r) > 50 {
		r = r[:50]
	}
	return string(r)
}

// proposed builds an op the person still has to accept. Every structural
// change waits for a yes; filing is the park stage's, not this one's.
func proposed(kind placeorg.OpKind, parent, place, into string, members []string, why string) placeorg.Op {
	members = append([]string(nil), members...)
	sort.Slice(members, func(i, j int) bool { return members[i] < members[j] })
	return placeorg.Op{
		Kind:     kind,
		Stage:    placeorg.StageAudit,
		Place:    place,
		Parent:   parent,
		Into:     into,
		Members:  members,
		Why:      why,
		Proposed: true,
	}
}

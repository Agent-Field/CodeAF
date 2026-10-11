// Package review proposes validated changes after one bounded outline judgment.
package review

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/placeorg"
	"github.com/Agent-Field/codeaf/internal/placeorg/name"
)

const MaxTokens = 400
const maxOutlineRunes = 24000

// Stage reviews names and suggests merges without changing the tree.
type Stage struct{}

var _ placeorg.Reviewer = (*Stage)(nil)
var _ placeorg.Judged = (*Stage)(nil)

func (*Stage) Fallbacks() []placeorg.Fallback {
	return []placeorg.Fallback{{When: "reviewing the place outline", Does: "no change"}}
}

type answer struct {
	Renames map[string]string `json:"renames"`
	Merges  [][]string        `json:"merge_suggestions"`
}

func decode(raw string) (answer, bool) {
	var a answer
	err := json.Unmarshal([]byte(raw), &a)
	return a, err == nil && strings.HasPrefix(strings.TrimSpace(raw), "{")
}

func (s *Stage) Review(ctx context.Context, t *placeorg.Tree, corpus *placeorg.Corpus, judge placeorg.Judge) ([]placeorg.Op, error) {
	return s.reviewNodes(ctx, name.Nodes(t), corpus, judge)
}

func (*Stage) reviewNodes(ctx context.Context, nodes []placeorg.Node, corpus *placeorg.Corpus, judge placeorg.Judge) ([]placeorg.Op, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if judge == nil || len(nodes) == 0 {
		return nil, nil
	}
	var outline strings.Builder
	outlineRunes := 0
	byName := map[string]placeorg.Node{}
	taken := map[string]bool{}
	shown := map[string]bool{}
	for _, n := range nodes {
		if n.Name == "" {
			continue
		}
		taken[strings.ToLower(n.Name)] = true
		line := fmt.Sprintf("%s- %s: %s\n", strings.Repeat("  ", max(0, min(6, n.Depth)-1)), n.Name, name.Titles(corpus, name.Members(nodes, n.ID), 3))
		if outlineRunes+len([]rune(line)) > maxOutlineRunes {
			continue
		}
		outline.WriteString(line)
		outlineRunes += len([]rune(line))
		byName[n.Name] = n
		shown[n.ID] = true
	}
	if len(shown) == 0 {
		return nil, nil
	}
	valid := func(raw string) (string, bool) { _, ok := decode(raw); return strings.TrimSpace(raw), ok }
	pick := judge.Write(ctx, placeorg.Question{Role: placeorg.RolePlaceReview, Purpose: `Review this outline once. Return JSON {"renames":{"current name":"better name"},"merge_suggestions":[["a","b"]]}. Names must be one or two specific member-spelled words. Suggest merges only between shown places.`, Context: outline.String(), MaxTokens: MaxTokens}, valid, `{"renames":{},"merge_suggestions":[]}`)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a, ok := decode(pick.Written)
	if !ok || pick.FellBack {
		return nil, nil
	}
	idx := name.NewIndex(corpus)
	var ops []placeorg.Op
	for _, n := range nodes {
		raw, exists := a.Renames[n.Name]
		if !exists || !shown[n.ID] {
			continue
		}
		clean, ok := idx.Validator(name.Members(nodes, n.ID), taken)(raw)
		if !ok || clean == n.Name {
			continue
		}
		delete(taken, strings.ToLower(n.Name))
		taken[strings.ToLower(clean)] = true
		ops = append(ops, placeorg.Op{Kind: placeorg.OpRename, Stage: "review", Place: n.ID, Name: clean, Proposed: n.Person, Why: "A more specific name from these chats."})
	}
	seen := map[string]bool{}
	for _, pair := range a.Merges {
		if len(pair) != 2 {
			continue
		}
		from, aok := byName[pair[0]]
		into, bok := byName[pair[1]]
		if !aok || !bok || from.ID == into.ID {
			continue
		}
		key := from.ID + "\x00" + into.ID
		reverse := into.ID + "\x00" + from.ID
		if seen[key] || seen[reverse] {
			continue
		}
		seen[key] = true
		ops = append(ops, placeorg.Op{Kind: placeorg.OpMerge, Stage: "review", Place: from.ID, Into: into.ID, Proposed: true, Why: "These places may belong together."})
	}
	return ops, nil
}

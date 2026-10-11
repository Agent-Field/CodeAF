package name

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// Stage returns naming operations; the Runner retains the only tree-writing door.
type Stage struct{}

var _ placeorg.Namer = (*Stage)(nil)
var _ placeorg.Judged = (*Stage)(nil)

func (*Stage) Fallbacks() []placeorg.Fallback {
	return []placeorg.Fallback{
		{When: "summarising a top-level place", Does: "the member titles in corpus order"},
		{When: "naming a place", Does: "the first untaken c-TF-IDF candidate; no operation if none is usable"},
	}
}

// Nodes reads a forest in preorder, without retaining mutable tree slices.
func Nodes(t *placeorg.Tree) []placeorg.Node {
	var out []placeorg.Node
	seen := map[string]bool{}
	var visit func(string)
	visit = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		n, ok := t.Node(id)
		if !ok {
			return
		}
		out = append(out, n)
		for _, child := range n.Children {
			visit(child)
		}
	}
	for _, root := range t.Roots() {
		visit(root)
	}
	return out
}

// Members includes descendants because a root is named for its whole branch, including parked citizens.
func Members(nodes []placeorg.Node, id string) []string {
	children := map[string][]string{}
	byID := map[string]placeorg.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
		children[n.Parent] = append(children[n.Parent], n.ID)
	}
	seen, chats := map[string]bool{}, map[string]bool{}
	var out []string
	var visit func(string)
	visit = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		for _, chat := range byID[id].Members {
			if !chats[chat] {
				out = append(out, chat)
				chats[chat] = true
			}
		}
		for _, child := range children[id] {
			visit(child)
		}
	}
	visit(id)
	return out
}

// Titles bounds model context independently of pasted message length.
func Titles(corpus *placeorg.Corpus, members []string, limit int) string {
	ids := map[string]bool{}
	for _, id := range members {
		ids[id] = true
	}
	var out []string
	for _, c := range corpus.Chats {
		if !ids[c.ID] || corpus.Kinds[c.ID] == placeorg.KindProbe || corpus.Kinds[c.ID] == placeorg.KindDuplicate {
			continue
		}
		title := c.Title
		if title == "" {
			title = c.Text
		}
		r := []rune(strings.Join(strings.Fields(title), " "))
		if len(r) > 160 {
			r = r[:160]
		}
		if len(r) > 0 {
			out = append(out, string(r))
		}
		if len(out) == limit {
			break
		}
	}
	return strings.Join(out, "\n")
}

func (s *Stage) Name(ctx context.Context, t *placeorg.Tree, corpus *placeorg.Corpus, judge placeorg.Judge) ([]placeorg.Op, error) {
	return s.nameNodes(ctx, Nodes(t), corpus, judge)
}

func (*Stage) nameNodes(ctx context.Context, nodes []placeorg.Node, corpus *placeorg.Corpus, judge placeorg.Judge) ([]placeorg.Op, error) {
	idx := NewIndex(corpus)
	taken := map[string]bool{}
	for _, n := range nodes {
		if n.Name != "" {
			taken[strings.ToLower(n.Name)] = true
		}
	}
	var ops []placeorg.Op
	for _, n := range nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if n.Name != "" || n.Person {
			continue
		}
		members := Members(nodes, n.ID)
		valid := idx.Validator(members, taken)
		candidates := idx.Candidates(members)
		var fallback string
		for _, candidate := range candidates {
			if generic[candidate] {
				continue
			}
			if clean, ok := valid(candidate); ok {
				fallback = clean
				break
			}
		}
		// A missing candidate remains a no-op on silence, while a semantic pick may still name it.
		const noName = "No supported name."
		if fallback == "" {
			fallback = noName
		}
		contextText := Titles(corpus, members, 10)
		if n.Parent == "" && judge != nil {
			charterValid := func(raw string) (string, bool) {
				line := strings.TrimSpace(raw)
				return line, line != "" && !strings.ContainsAny(line, "\r\n") && len(strings.Fields(line)) <= 10
			}
			charterFallback := strings.Join(strings.Fields(contextText)[:min(10, len(strings.Fields(contextText)))], " ")
			if charterFallback == "" {
				charterFallback = fallback
			}
			charter := judge.Write(ctx, placeorg.Question{Role: placeorg.RolePlaceCharter, Purpose: "Say what these chats are about in one line of at most ten words.", Context: contextText, MaxTokens: 40}, charterValid, charterFallback)
			line, ok := charterValid(charter.Written)
			if !ok || charter.FellBack {
				line = charterFallback
			}
			contextText = "These chats are about: " + line + "\n" + contextText
		}
		var used []string
		for text := range taken {
			used = append(used, text)
		}
		sort.Strings(used)
		contextText += fmt.Sprintf("\nHint words: %s\nTaken names: %s", strings.Join(candidates, ", "), strings.Join(used, ", "))
		result := fallback
		if judge != nil {
			pick := judge.Write(ctx, placeorg.Question{Role: placeorg.RolePlaceName, Purpose: "Name the thing these chats are about in one or two specific member-spelled words; avoid generic names and taken names.", Context: contextText, MaxTokens: 30}, valid, fallback)
			if clean, ok := valid(pick.Written); ok && !pick.FellBack {
				result = clean
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if result == noName {
			continue
		}
		taken[strings.ToLower(result)] = true
		ops = append(ops, placeorg.Op{Kind: placeorg.OpName, Stage: "name", Place: n.ID, Name: result, Why: "Named from the words in these chats.", Proposed: n.Person})
	}
	return ops, nil
}

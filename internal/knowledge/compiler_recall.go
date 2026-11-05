package knowledge

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

type Citation struct {
	Claim string `json:"claim"`
	Evidence
}
type Answer struct {
	Abstain   bool       `json:"abstain"`
	Answer    string     `json:"answer"`
	Citations []Citation `json:"citations"`
	Claims    []string   `json:"claims"`
}

// Recall answers from the current publication, including explicit human edits.
func (v *Vault) Recall(ctx context.Context, key, question, scope string) (Answer, error) {
	var answer Answer
	if !v.Enabled() {
		return answer, ErrDisabled
	}
	if err := ctx.Err(); err != nil {
		return answer, err
	}
	db, err := v.database()
	if err != nil {
		return answer, err
	}
	defer db.Close()
	vault, err := stateDocument(db, "published")
	if err != nil {
		return answer, err
	}
	model := compilerModel{ctx: ctx, root: v.root, key: key, model: knowledgeModel(v.profile), db: db}
	result, err := recallCompilation(vault, question, scope, 16, "lexical", model)
	if err != nil {
		return answer, err
	}
	raw, err := json.Marshal(result)
	if err == nil {
		err = json.Unmarshal(raw, &answer)
	}
	return answer, err
}
func recallCompilation(vault document, question, scope string, limit int, retrieval string, model compilerModel) (document, error) {
	if limit < 1 || limit > 128 {
		return nil, errors.New("recall limit must be between 1 and 128")
	}
	if !contains([]string{"lexical", "graph", "coverage"}, retrieval) {
		return nil, errors.New("unknown knowledge retrieval strategy")
	}
	terms := words(question)
	aliases := map[string]string{}
	for _, a := range items(vault["scope_aliases"]) {
		aliases[foldText(textValue(a["from"]))] = foldText(textValue(a["to"]))
	}
	canonical := func(label string) string {
		label = foldText(label)
		seen := map[string]bool{}
		for aliases[label] != "" && !seen[label] {
			seen[label] = true
			label = aliases[label]
		}
		return label
	}
	visible := func(label string) bool {
		return scope == "" || canonical(label) == canonical(scope) || canonical(label) == "personal"
	}
	notes := []document{}
	for _, n := range items(vault["notes"]) {
		if visible(textValue(n["scope"])) {
			notes = append(notes, n)
		}
	}
	selectedNotes := map[string]bool{}
	if retrieval != "lexical" {
		catalog := []document{}
		for _, n := range notes {
			catalog = append(catalog, choose(n, "id", "title", "scope"))
		}
		plan, err := model.call("query", document{"question": question, "notes": catalog})
		if err != nil {
			return nil, err
		}
		extra := stringsValue(plan["terms"])
		for t := range words(strings.Join(extra[:min(12, len(extra))], " ")) {
			terms[t] = true
		}
		eligible := index(notes)
		for _, id := range stringsValue(plan["notes"]) {
			if eligible[id] != nil {
				selectedNotes[id] = true
			}
		}
	}
	sources := merge(object(vault["sources"]), nil)
	candidates := []document{}
	active := activeClaims(vault)
	for _, c := range items(vault["claims"]) {
		if c["status"] == "active" && visible(textValue(c["scope"])) {
			candidates = append(candidates, c)
		}
	}
	derived := map[string][]string{}
	for _, n := range notes {
		if flag(n["human_edited"]) && textValue(n["manual_markdown"]) != "" {
			text := textValue(n["manual_markdown"])
			id := "human-edit:" + identity([]any{n["id"], text})
			ts := n["human_edited_at"]
			if ts == nil {
				ts = "1970-01-01T00:00:00Z"
			}
			sources[id] = document{"id": id, "role": "user", "text": text, "ts": ts, "provenance": document{"kind": "human_edit"}}
			candidates = append(candidates, document{"id": id, "text": text, "scope": n["scope"], "kind": "reported", "status": "active", "human_edited": true, "evidence": []document{{"source": id, "quote": text, "origin": "human_edit"}}})
		}
		for _, s := range items(n["sections"]) {
			for _, p := range items(s["paragraphs"]) {
				ev := items(p["evidence"])
				dependencies := append(append([]string{}, stringsValue(p["depends_on"])...), stringsValue(p["claims"])...)
				if !contains([]string{"gap_answer", "subject_guide"}, textValue(p["derivation"])) || p["review"] == nil || p["review"] == "" || len(ev) == 0 || !subset(dependencies, active) || !validSourceEvidence(ev, sources, true) {
					continue
				}
				id := "derived:" + identity([]any{n["id"], p["text"], ev})
				candidates = append(candidates, document{"id": id, "text": p["text"], "topic": s["heading"], "scope": n["scope"], "status": "active", "kind": "reported", "partial": flag(p["partial"]), "evidence_basis": p["evidence_basis"], "evidence": ev})
				nid := textValue(n["id"])
				derived[nid] = append(derived[nid], id)
			}
		}
	}
	frequency := map[string]int{}
	for t := range terms {
		for _, c := range candidates {
			if words(textValue(c["text"]))[t] {
				frequency[t]++
			}
		}
	}
	boosts := map[string]float64{}
	byNote := index(notes)
	for id := range selectedNotes {
		n := byNote[id]
		for _, cid := range append(append([]string{}, stringsValue(n["claims"])...), derived[id]...) {
			boosts[cid] += 4
		}
		for _, link := range items(n["links"]) {
			lid := textValue(link["id"])
			if linked := byNote[lid]; linked != nil {
				for _, cid := range append(append([]string{}, stringsValue(linked["claims"])...), derived[lid]...) {
					boosts[cid]++
				}
			}
		}
	}
	type rankedClaim struct {
		claim document
		score float64
	}
	ranked := []rankedClaim{}
	termOrder := []string{}
	for t := range terms {
		termOrder = append(termOrder, t)
	}
	sort.Strings(termOrder)
	for _, c := range candidates {
		tokens := words(textValue(c["text"]) + " " + textValue(c["topic"]) + " " + textValue(c["scope"]))
		score := boosts[textValue(c["id"])]
		for _, t := range termOrder {
			if tokens[t] {
				score += math.Log(1 + float64(len(candidates)+1)/float64(frequency[t]+1))
			}
		}
		ranked = append(ranked, rankedClaim{c, score})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return textValue(ranked[i].claim["id"]) < textValue(ranked[j].claim["id"])
	})
	selected := []document{}
	for _, r := range ranked[:min(limit, len(ranked))] {
		if r.score > 0 {
			selected = append(selected, r.claim)
		}
	}
	if retrieval == "coverage" {
		remaining := []rankedClaim{}
		for _, r := range ranked {
			if r.score > 0 {
				remaining = append(remaining, r)
			}
		}
		selected = []document{}
		covered := map[string]bool{}
		for len(remaining) > 0 && len(selected) < limit {
			best, bestGain := 0, math.Inf(-1)
			for i, r := range remaining {
				tokens := words(textValue(r.claim["text"]))
				novelty := 0
				for t := range tokens {
					if terms[t] && !covered[t] {
						novelty++
					}
				}
				overlap := float64(0)
				for _, c := range selected {
					ct := words(textValue(c["text"]))
					intersection, union := 0, len(tokens)
					for t := range ct {
						if tokens[t] {
							intersection++
						} else {
							union++
						}
					}
					overlap = math.Max(overlap, float64(intersection)/float64(max(1, union)))
				}
				gain := r.score*(1-0.6*overlap) + float64(novelty)
				if gain > bestGain {
					best, bestGain = i, gain
				}
			}
			c := remaining[best].claim
			selected = append(selected, c)
			for t := range words(textValue(c["text"])) {
				if terms[t] {
					covered[t] = true
				}
			}
			remaining = append(remaining[:best], remaining[best+1:]...)
		}
	}
	if len(selected) == 0 {
		return document{"answer": "I could not find supported knowledge for that question.", "citations": []document{}, "claims": []string{}, "abstain": true}, nil
	}
	reviewContext := []document{}
	for _, c := range selected {
		reviewContext = append(reviewContext, merge(c, document{"evidence_basis": merge(object(c["evidence_basis"]), evidenceBasis(sources, items(c["evidence"])))}))
	}
	response, err := model.call("recall", document{"question": question, "claims": reviewContext})
	if err != nil {
		return nil, err
	}
	known := index(selected)
	refs := stringsValue(response["citations"])
	if !referencesAllowed(response["citations"], known) || (textValue(response["answer"]) != "" && len(refs) == 0 && !flag(response["abstain"])) {
		return nil, errors.New("answer contains an unsupported citation or lacks citations")
	}
	if flag(response["abstain"]) {
		refs = []string{}
	}
	citations := []document{}
	for _, id := range unique(refs) {
		for _, ev := range items(known[id]["evidence"]) {
			source := object(sources[textValue(ev["source"])])
			citations = append(citations, merge(ev, document{"claim": id, "role": source["role"], "ts": source["ts"]}))
		}
	}
	ids := []string{}
	for _, c := range selected {
		ids = append(ids, textValue(c["id"]))
	}
	return document{"answer": textValue(response["answer"]), "citations": citations, "claims": ids, "abstain": flag(response["abstain"])}, nil
}

//go:embed testdata/support_contracts.json
var frozenSupportContracts []byte

// runFrozenSuite replays the frozen capture batches and checks source contracts.
func runFrozenSuite(model compilerModel) (document, error) {
	var cases []document
	if err := json.Unmarshal(frozenSupportContracts, &cases); err != nil {
		return nil, err
	}
	outputs := []document{}
	for _, c := range cases {
		vault := emptyCompilation()
		batches, ok := c["batches"].([]any)
		if !ok {
			return nil, fmt.Errorf("invalid batches for frozen case %s", textValue(c["id"]))
		}
		for _, batch := range batches {
			messages := []document{}
			for _, original := range items(batch) {
				source := clone(original)
				kind := "agent_synthesis"
				if source["role"] == "user" {
					kind = "user_statement"
				}
				source["provenance"] = document{"kind": kind, "recorded_at": source["ts"], "partition": document{"owner": "benchmark", "project": c["id"]}, "content_hash": identity(source["text"])}
				messages = append(messages, source)
			}
			var err error
			vault, err = updateCompilation(messages, vault, nil, model)
			if err != nil {
				return nil, fmt.Errorf("frozen case %s: %w", textValue(c["id"]), err)
			}
		}
		answer, err := recallCompilation(vault, textValue(c["question"]), textValue(c["scope"]), 16, "lexical", model)
		if err != nil {
			return nil, fmt.Errorf("frozen recall %s: %w", textValue(c["id"]), err)
		}
		outputs = append(outputs, document{"id": c["id"], "vault": vault, "answer": answer})
	}
	return evaluateFrozenContracts(cases, outputs)
}

// RunSupportContracts replays the frozen evaluation manifest in an isolated profile.
// The returned report remains available when an explicit support contract fails.
func (v *Vault) RunSupportContracts(ctx context.Context, key string) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db, err := v.database()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	model := compilerModel{ctx: ctx, root: v.root, key: key, model: knowledgeModel(v.profile), db: db}
	result, runErr := runFrozenSuite(model)
	if result == nil {
		return nil, runErr
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return raw, runErr
}

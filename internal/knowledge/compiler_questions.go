package knowledge

import (
	"fmt"
	"sort"
	"strings"
)

func resolveLearningQuestions(learning, previous document, subjects []document, claims map[string]document, model compilerModel) error {
	claimIDs := []string{}
	for id := range claims {
		claimIDs = append(claimIDs, id)
	}
	sort.Strings(claimIDs)
	corpus := []any{}
	postings := map[string][]string{}
	for _, id := range claimIDs {
		c := claims[id]
		corpus = append(corpus, []any{id, c["text"], c["kind"], c["scope"]})
		for term := range words(textValue(c["text"])) {
			postings[term] = append(postings[term], id)
		}
	}
	digest := identity(corpus)
	type candidate struct {
		question document
		refs     []string
		refresh  bool
	}
	pending := map[string]candidate{}
	order := []string{}
	occurrences := map[string]int{}
	for _, s := range subjects {
		owned := map[string]bool{}
		for _, id := range stringsValue(s["claims"]) {
			owned[id] = true
		}
		for _, q := range items(s["questions"]) {
			id := resolutionQuestionID(s, q, occurrences)
			if textValue(object(q["search"])["corpus_hash"]) == digest {
				if refs := stringsValue(object(q["search"])["candidate_claims"]); len(refs) > 0 {
					pending[id] = candidate{q, refs, false}
					order = append(order, id)
				}
				continue
			}
			terms := words(strings.Join(stringsValue(q["search_terms"]), " "))
			for _, stop := range strings.Fields("the and how are what does for with this that from can") {
				delete(terms, stop)
			}
			type scored struct {
				id    string
				score int
			}
			ranked := []scored{}
			scores := map[string]int{}
			for term := range terms {
				for _, id := range postings[term] {
					if !owned[id] {
						scores[id]++
					}
				}
			}
			for id, score := range scores {
				if score >= 2 {
					ranked = append(ranked, scored{id, score})
				}
			}
			sort.Slice(ranked, func(i, j int) bool {
				if ranked[i].score != ranked[j].score {
					return ranked[i].score > ranked[j].score
				}
				return ranked[i].id < ranked[j].id
			})
			refs := []string{}
			for _, r := range ranked[:min(6, len(ranked))] {
				refs = append(refs, r.id)
			}
			q["search"] = document{"corpus_hash": digest, "candidate_claims": refs, "method": "bounded lexical search of active extracted claims; raw sources not exhaustively searched"}
			q["state"] = "unresolved_in_inspected_evidence"
			delete(q, "related_claims")
			if len(refs) > 0 {
				pending[id] = candidate{q, refs, true}
				order = append(order, id)
			}
		}
	}
	request := func(ids []string) document {
		questions := []document{}
		refs := []string{}
		for i, id := range ids {
			c := pending[id]
			questions = append(questions, document{"id": fmt.Sprintf("Q%d", i), "question": c.question["question"], "candidates": c.refs})
			refs = append(refs, c.refs...)
		}
		selected := []document{}
		for _, id := range unique(refs) {
			selected = append(selected, choose(claims[id], "id", "text", "scope", "kind"))
		}
		return document{"questions": questions, "claims": selected}
	}
	apply := func(ids []string, response document) {
		for _, match := range items(response["matches"]) {
			for i, id := range ids {
				c := pending[id]
				if !c.refresh || textValue(match["id"]) != fmt.Sprintf("Q%d", i) || textValue(match["relation"]) != "answers" {
					continue
				}
				refs := stringsValue(match["claims"])
				valid := len(refs) > 0
				for _, id := range refs {
					if !contains(c.refs, id) {
						valid = false
					}
				}
				if valid {
					c.question["state"] = "answer_candidate_found"
					c.question["related_claims"] = unique(refs)
				}
			}
		}
	}
	batches := []document{}
	changed := map[string]bool{}
	for _, group := range stableQuestionBatches(previous, order) {
		ids := stringsValue(group["questions"])
		refresh := false
		for _, id := range ids {
			refresh = refresh || pending[id].refresh
		}
		if !refresh {
			batches = append(batches, group)
			continue
		}
		response, found, err := model.cached("resolve_questions", request(ids))
		if err != nil {
			return err
		}
		if found {
			apply(ids, response)
			batches = append(batches, group)
		} else {
			for _, id := range ids {
				changed[id] = true
			}
		}
	}
	// Repack only cache misses, so sparse historical groups cannot multiply
	// provider calls when new evidence affects many questions at once.
	fresh := []string{}
	for _, id := range order {
		if changed[id] {
			fresh = append(fresh, id)
			delete(changed, id)
		}
	}
	for start := 0; start < len(fresh); start += questionBatchLimit {
		ids := fresh[start:min(len(fresh), start+questionBatchLimit)]
		response, err := model.call("resolve_questions", request(ids))
		if err != nil {
			return err
		}
		apply(ids, response)
		batches = append(batches, document{"questions": ids})
	}
	learning["resolution_batches"] = batches
	return nil
}

const questionBatchLimit = 16

func resolutionQuestionID(subject, question document, occurrences map[string]int) string {
	id := questionID(subject, question)
	ordinal := occurrences[id]
	occurrences[id]++
	return fmt.Sprintf("%s:%d", id, ordinal)
}

// Keep the complete context of an unchanged request stable when other questions
// become searchable. The existing receipt cache still validates the whole input.
func stableQuestionBatches(previous document, order []string) []document {
	prior := items(previous["resolution_batches"])
	if previous["resolution_batches"] == nil {
		legacy := []string{}
		occurrences := map[string]int{}
		for _, subject := range items(previous["subjects"]) {
			for _, q := range items(subject["questions"]) {
				id := resolutionQuestionID(subject, q, occurrences)
				if len(stringsValue(object(q["search"])["candidate_claims"])) > 0 {
					legacy = append(legacy, id)
				}
			}
		}
		for start := 0; start < len(legacy); start += questionBatchLimit {
			prior = append(prior, document{"questions": legacy[start:min(len(legacy), start+questionBatchLimit)]})
		}
	}
	remaining := map[string]bool{}
	for _, id := range order {
		remaining[id] = true
	}
	batches := []document{}
	for _, group := range prior {
		members := []string{}
		for _, id := range stringsValue(group["questions"]) {
			if remaining[id] {
				members = append(members, id)
				delete(remaining, id)
			}
		}
		for start := 0; start < len(members); start += questionBatchLimit {
			batches = append(batches, document{"questions": members[start:min(len(members), start+questionBatchLimit)]})
		}
	}
	added := []string{}
	for _, id := range order {
		if remaining[id] {
			added = append(added, id)
			delete(remaining, id)
		}
	}
	for start := 0; start < len(added); start += questionBatchLimit {
		batches = append(batches, document{"questions": added[start:min(len(added), start+questionBatchLimit)]})
	}
	return batches
}

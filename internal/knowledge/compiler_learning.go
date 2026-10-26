package knowledge

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const learningVersion = "learning-1"

var conversationNarration = regexp.MustCompile(`(?i)\b(the user|the agent|the owner|user|agent|owner) (said|reported|asked|requested|wants|instructed|corrected|specified)\b`)

func activeClaims(vault document) map[string]document {
	result := map[string]document{}
	for _, c := range items(vault["claims"]) {
		if textValue(c["status"]) == "active" {
			result[textValue(c["id"])] = c
		}
	}
	return result
}
func memberClaims(notes []string, byNote, claims map[string]document) []string {
	ids := []string{}
	for _, id := range notes {
		for _, cid := range stringsValue(byNote[id]["claims"]) {
			if claims[cid] != nil {
				ids = append(ids, cid)
			}
		}
	}
	return unique(ids)
}
func extendLearning(vault, previous document, model compilerModel) (document, error) {
	result := clone(previous)
	subjects := items(result["subjects"])
	active := activeClaims(vault)
	byNote := index(items(vault["notes"]))
	assigned := map[string]bool{}
	dirty := []string{}
	for _, s := range subjects {
		for _, id := range stringsValue(s["notes"]) {
			assigned[id] = true
		}
		members := memberClaims(stringsValue(s["notes"]), byNote, active)
		added := []string{}
		for _, id := range members {
			if !contains(stringsValue(s["claims"]), id) {
				added = append(added, id)
			}
		}
		if len(added) > 0 {
			s["claims"] = sortedUnique(append(stringsValue(s["claims"]), added...))
			s["needs_editorial_update"] = true
			s["editorial_pending_claims"] = sortedUnique(append(stringsValue(s["editorial_pending_claims"]), added...))
			dirty = append(dirty, textValue(s["id"]))
		}
	}
	notes := []document{}
	discoveryInputs := object(result["discovery_inputs"])
	for _, n := range items(vault["notes"]) {
		if assigned[textValue(n["id"])] {
			continue
		}
		members := memberClaims([]string{textValue(n["id"])}, byNote, active)
		if len(members) > 0 {
			inputs := []document{}
			for _, id := range members {
				inputs = append(inputs, active[id])
			}
			digest := identity(inputs)
			if textValue(discoveryInputs[textValue(n["id"])]) != digest {
				notes = append(notes, n)
			}
		}
	}
	for start := 0; start < len(notes); start += 256 {
		batch := notes[start:min(len(notes), start+256)]
		ids := map[string]bool{}
		for _, n := range batch {
			inputs := []document{}
			for _, id := range memberClaims([]string{textValue(n["id"])}, byNote, active) {
				ids[id] = true
				inputs = append(inputs, active[id])
			}
			discoveryInputs[textValue(n["id"])] = identity(inputs)
		}
		selected := []document{}
		for _, c := range items(vault["claims"]) {
			if ids[textValue(c["id"])] {
				selected = append(selected, c)
			}
		}
		discovered, err := learnSubjects(merge(vault, document{"notes": batch, "claims": selected}), document{}, model)
		if err != nil {
			return nil, err
		}
		old := index(subjects)
		for _, s := range items(discovered["subjects"]) {
			if prior := old[textValue(s["id"])]; prior != nil {
				prior["notes"] = sortedUnique(append(stringsValue(prior["notes"]), stringsValue(s["notes"])...))
				added := []string{}
				for _, id := range stringsValue(s["claims"]) {
					if !contains(stringsValue(prior["claims"]), id) {
						added = append(added, id)
					}
				}
				prior["editorial_pending_claims"] = sortedUnique(append(stringsValue(prior["editorial_pending_claims"]), added...))
				prior["claims"] = sortedUnique(append(stringsValue(prior["claims"]), added...))
				prior["sections"] = append(items(prior["sections"]), items(s["sections"])...)
				prior["needs_editorial_update"] = true
			} else {
				subjects = append(subjects, s)
				old[textValue(s["id"])] = s
			}
		}
	}
	result["discovery_inputs"] = discoveryInputs
	result["subjects"] = subjects
	result["incremental_discovery"] = document{"new_notes": len(notes), "dirty_existing_subjects": dirty, "limitation": "Changed known subjects keep sourced prose and are marked for bounded later editing; not rewritten automatically."}
	return result, nil
}
func learnSubjects(vault, previous document, model compilerModel) (document, error) {
	claims := activeClaims(vault)
	byNote := index(items(vault["notes"]))
	notes := []document{}
	for _, n := range items(vault["notes"]) {
		if len(memberClaims([]string{textValue(n["id"])}, byNote, claims)) > 0 {
			notes = append(notes, n)
		}
	}
	sort.Slice(notes, func(i, j int) bool { return textValue(notes[i]["id"]) < textValue(notes[j]["id"]) })
	catalog := []document{}
	for _, n := range notes {
		gist := []string{}
		count := 0
		for _, id := range stringsValue(n["claims"]) {
			if c := claims[id]; c != nil {
				count++
				if len(gist) < 2 {
					runes := []rune(textValue(c["text"]))
					gist = append(gist, string(runes[:min(len(runes), 240)]))
				}
			}
		}
		catalog = append(catalog, document{"id": n["id"], "title": n["title"], "scope": n["scope"], "claim_count": count, "gist": gist})
	}
	if len(catalog) > 256 {
		return nil, errors.New("knowledge discovery exceeds 256 notes; partition the collection before learning")
	}
	digest := identity([]any{learningVersion, catalog})
	discovery := items(previous["discovery"])
	if len(catalog) > 0 && textValue(previous["catalog_hash"]) != digest {
		response, err := model.call("discover", document{"catalog": catalog})
		if err != nil {
			return nil, err
		}
		discovery = items(response["subjects"])
	}
	refined := []document{}
	for _, raw := range discovery {
		refs := []string{}
		for _, id := range stringsValue(raw["notes"]) {
			if byNote[id] != nil {
				refs = append(refs, id)
			}
		}
		if len(memberClaims(refs, byNote, claims)) > 160 && len(refs) > 1 {
			selected := []document{}
			for _, n := range catalog {
				if contains(refs, textValue(n["id"])) {
					selected = append(selected, n)
				}
			}
			response, err := model.call("discover", document{"catalog": selected, "partition_parent": raw["title"], "constraint": "Split this oversized group into coherent subjects with at most 160 claims each."})
			if err != nil {
				return nil, err
			}
			partition := items(response["subjects"])
			all := []string{}
			valid := len(partition) > 0
			for _, child := range partition {
				for _, id := range stringsValue(child["notes"]) {
					if !contains(refs, id) {
						valid = false
					}
					all = append(all, id)
				}
			}
			if valid && identity(sortedUnique(all)) == identity(sortedUnique(refs)) {
				refined = append(refined, partition...)
				continue
			}
		}
		refined = append(refined, raw)
	}
	old := index(items(previous["subjects"]))
	seen := map[string]bool{}
	subjects := []document{}
	for _, raw := range refined {
		title, scope := textValue(raw["title"]), textValue(raw["scope"])
		refs := []string{}
		for _, id := range unique(stringsValue(raw["notes"])) {
			if byNote[id] != nil {
				refs = append(refs, id)
			}
		}
		if strings.TrimSpace(title) == "" || scope == "" || len(refs) == 0 {
			continue
		}
		id := identity([]any{"subject", strings.ToLower(scope), strings.ToLower(title)})
		aliases := stringsValue(raw["aliases"])
		if old[id] == nil {
			matches := []document{}
			for _, prior := range items(previous["subjects"]) {
				if !seen[textValue(prior["id"])] && strings.EqualFold(textValue(prior["scope"]), scope) && identity(sortedUnique(stringsValue(prior["notes"]))) == identity(sortedUnique(refs)) {
					matches = append(matches, prior)
				}
			}
			if len(matches) == 1 {
				id = textValue(matches[0]["id"])
				aliases = unique(append(append(aliases, stringsValue(matches[0]["aliases"])...), textValue(matches[0]["title"])))
			}
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		ids := memberClaims(refs, byNote, claims)
		subject := document{"id": id, "title": title, "scope": scope, "notes": refs, "claims": ids, "aliases": aliases, "sections": []document{}, "questions": []document{}, "conflicts": []document{}}
		if len(ids) > 160 {
			subject["state"] = "needs_partition"
			subject["reason"] = "More than 160 claims; split this subject before synthesis"
			subjects = append(subjects, subject)
			continue
		}
		evidence := []document{}
		sources := object(vault["sources"])
		for _, cid := range ids {
			c := claims[cid]
			contexts := []document{}
			for _, e := range items(c["evidence"]) {
				if s, ok := sources[textValue(e["source"])]; ok {
					contexts = append(contexts, choose(object(s), "role", "ts"))
				}
			}
			evidence = append(evidence, merge(c, document{"source_context": contexts}))
		}
		inputHash := identity([]any{learningVersion, title, scope, refs, evidence})
		subject["input_hash"] = inputHash
		if prior := old[id]; prior != nil && textValue(prior["input_hash"]) == inputHash {
			subjects = append(subjects, clone(prior))
			continue
		}
		response, err := model.call("understand", document{"subject": choose(subject, "id", "title", "scope", "notes", "claims", "aliases"), "claims": evidence})
		if err != nil {
			return nil, err
		}
		allowed := map[string]document{}
		for _, cid := range ids {
			allowed[cid] = claims[cid]
		}
		sections := []document{}
		paragraphs := []document{}
		for _, s := range items(response["sections"]) {
			kept := []document{}
			for _, p := range items(s["paragraphs"]) {
				text := textValue(p["text"])
				refs := stringsValue(p["claims"])
				if text != "" && !conversationNarration.MatchString(text) && len(refs) > 0 && subset(refs, allowed) {
					p = document{"text": text, "claims": unique(refs)}
					kept = append(kept, p)
					paragraphs = append(paragraphs, merge(p, document{"id": fmt.Sprintf("P%d", len(paragraphs))}))
				}
			}
			if len(kept) > 0 {
				sections = append(sections, document{"heading": textValue(s["heading"]), "paragraphs": kept})
			}
		}
		questions := []document{}
		for _, q := range items(response["questions"]) {
			if len(questions) >= 5 {
				break
			}
			refs := stringsValue(q["claims"])
			if textValue(q["question"]) != "" && len(refs) > 0 && subset(refs, allowed) {
				terms := stringsValue(q["search_terms"])
				q = merge(choose(q, "question", "why"), document{"claims": unique(refs), "search_terms": terms[:min(8, len(terms))], "state": "unresolved_in_inspected_evidence"})
				questions = append(questions, q)
			}
		}
		conflicts := []document{}
		for _, c := range items(response["conflicts"]) {
			refs := unique(stringsValue(c["claims"]))
			if len(refs) == 2 && subset(refs, allowed) {
				conflicts = append(conflicts, c)
			}
		}
		review := document{}
		if len(paragraphs)+len(questions)+len(conflicts) > 0 {
			qIDs, cIDs := []document{}, []document{}
			for i, q := range questions {
				qIDs = append(qIDs, merge(q, document{"id": fmt.Sprintf("Q%d", i)}))
			}
			for i, c := range conflicts {
				cIDs = append(cIDs, merge(c, document{"id": fmt.Sprintf("X%d", i)}))
			}
			review, err = model.call("review_understanding", document{"claims": evidence, "paragraphs": paragraphs, "questions": qIDs, "conflicts": cIDs})
			if err != nil {
				return nil, err
			}
		}
		checks := index(items(review["checks"]))
		finalSections := []document{}
		ordinal := 0
		for _, s := range sections {
			kept := []document{}
			for _, p := range items(s["paragraphs"]) {
				if flag(checks[fmt.Sprintf("P%d", ordinal)]["supported"]) {
					kept = append(kept, p)
				}
				ordinal++
			}
			if len(kept) > 0 {
				finalSections = append(finalSections, merge(s, document{"paragraphs": kept}))
			}
		}
		qChecks := index(items(review["questions"]))
		keptQuestions := []document{}
		for i, q := range questions {
			if flag(qChecks[fmt.Sprintf("Q%d", i)]["grounded"]) {
				keptQuestions = append(keptQuestions, q)
			}
		}
		cChecks := index(items(review["conflicts"]))
		keptConflicts := []document{}
		for i, c := range conflicts {
			if flag(cChecks[fmt.Sprintf("X%d", i)]["supported"]) {
				keptConflicts = append(keptConflicts, merge(c, document{"state": "unresolved"}))
			}
		}
		subject["sections"] = finalSections
		subject["questions"] = keptQuestions
		subject["conflicts"] = keptConflicts
		subject["review"] = review
		subject["state"] = "needs_evidence"
		if len(finalSections) > 0 {
			subject["state"] = "explained"
		}
		subjects = append(subjects, subject)
	}
	if err := resolveLearningQuestions(subjects, claims, model); err != nil {
		return nil, err
	}
	assigned := []string{}
	for _, s := range subjects {
		assigned = append(assigned, stringsValue(s["notes"])...)
	}
	unassigned := []string{}
	for _, n := range notes {
		if !contains(assigned, textValue(n["id"])) {
			unassigned = append(unassigned, textValue(n["id"]))
		}
	}
	return document{"version": learningVersion, "catalog_hash": digest, "discovery": refined, "subjects": subjects, "coverage": document{"eligible_notes": len(notes), "assigned_notes": len(unique(assigned)), "notes_without_subject": unassigned}}, nil
}
func resolveLearningQuestions(subjects []document, claims map[string]document, model compilerModel) error {
	claimIDs := []string{}
	for id := range claims {
		claimIDs = append(claimIDs, id)
	}
	sort.Strings(claimIDs)
	corpus := []any{}
	for _, id := range claimIDs {
		c := claims[id]
		corpus = append(corpus, []any{id, c["text"], c["kind"], c["scope"]})
	}
	digest := identity(corpus)
	type candidate struct {
		question document
		refs     []string
	}
	pending := []candidate{}
	for _, s := range subjects {
		for _, q := range items(s["questions"]) {
			if textValue(object(q["search"])["corpus_hash"]) == digest {
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
			for _, id := range claimIDs {
				if contains(stringsValue(s["claims"]), id) {
					continue
				}
				score := 0
				for term := range words(textValue(claims[id]["text"])) {
					if terms[term] {
						score++
					}
				}
				if score >= 2 {
					ranked = append(ranked, scored{id, score})
				}
			}
			sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
			refs := []string{}
			for _, r := range ranked[:min(6, len(ranked))] {
				refs = append(refs, r.id)
			}
			q["search"] = document{"corpus_hash": digest, "candidate_claims": refs, "method": "bounded lexical search of active extracted claims; raw sources not exhaustively searched"}
			q["state"] = "unresolved_in_inspected_evidence"
			delete(q, "related_claims")
			if len(refs) > 0 {
				pending = append(pending, candidate{q, refs})
			}
		}
	}
	for start := 0; start < len(pending); start += 16 {
		batch := pending[start:min(len(pending), start+16)]
		questions := []document{}
		ids := []string{}
		for i, c := range batch {
			questions = append(questions, document{"id": fmt.Sprintf("Q%d", i), "question": c.question["question"], "candidates": c.refs})
			ids = append(ids, c.refs...)
		}
		selected := []document{}
		for _, id := range unique(ids) {
			selected = append(selected, choose(claims[id], "id", "text", "scope", "kind"))
		}
		response, err := model.call("resolve_questions", document{"questions": questions, "claims": selected})
		if err != nil {
			return err
		}
		for _, match := range items(response["matches"]) {
			for i, c := range batch {
				if textValue(match["id"]) != fmt.Sprintf("Q%d", i) || textValue(match["relation"]) != "answers" {
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
	return nil
}

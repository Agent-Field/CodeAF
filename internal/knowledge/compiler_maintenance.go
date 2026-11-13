package knowledge

import (
	"fmt"
	"sort"
	"strings"
)

func maintainKnowledge(vault, learning, previous document, model compilerModel, budget int, context string, advance bool) (document, error) {
	if budget < 0 || budget > 32 {
		return nil, fmt.Errorf("question budget must be between zero and 32")
	}
	result := clone(learning)
	active := activeClaims(vault)
	sources := object(vault["sources"])
	oldQuestions := map[string]document{}
	for _, s := range items(previous["subjects"]) {
		for _, q := range items(s["questions"]) {
			if id := textValue(q["id"]); id != "" {
				oldQuestions[id] = q
			}
		}
	}
	candidates := []document{}
	questionMap := map[string]document{}
	for _, s := range items(result["subjects"]) {
		for _, q := range items(s["questions"]) {
			id := questionID(s, q)
			q["id"] = id
			old := oldQuestions[id]
			for _, key := range []string{"answer", "attempt_hash", "history", "state", "search", "priority_reason", "processed_wakes"} {
				if v, ok := old[key]; ok {
					q[key] = clone(document{"v": v})["v"]
				}
			}
			if q["state"] == nil {
				q["state"] = "backlog"
			}
			refs := stringsValue(q["claims"])
			if len(refs) == 0 || !subset(refs, active) {
				delete(q, "answer")
				q["state"] = "retired_evidence"
				continue
			}
			a := object(q["answer"])
			if len(a) > 0 && (!subset(stringsValue(a["claims"]), active) || !validSourceEvidence(items(a["evidence"]), sources, true)) {
				q["history"] = append(items(q["history"]), document{"answer": a, "reason": "Supporting evidence changed or retired"})
				delete(q, "answer")
				delete(q, "attempt_hash")
				q["state"] = "needs_review"
			}
			questionMap[id] = q
			if object(q["curiosity"])["state"] == "parked" && len(stringsValue(q["wake_sources"])) == 0 {
				continue
			}
			kinds := []string{}
			for _, cid := range refs {
				kinds = append(kinds, textValue(active[cid]["kind"]))
			}
			wakes := stringsValue(q["wake_sources"])
			candidates = append(candidates, document{"id": id, "subject_id": s["id"], "subject": s["title"], "question": q["question"], "why": q["why"], "wake_sources": wakes[:min(2, len(wakes))], "waking_source_count": len(wakes), "kinds": sortedUnique(kinds)})
		}
	}
	catalogHash := identity([]any{"maintenance-3", candidates, context, budget})
	priorPlan := object(previous["maintenance"])
	calls := 0
	selection := []document{}
	if budget > 0 && len(candidates) > 0 {
		if !advance && priorPlan["plan_hash"] == catalogHash {
			selection = items(priorPlan["selection"])
		} else {
			sort.SliceStable(candidates, func(i, j int) bool {
				a, b := candidates[i], candidates[j]
				aw, bw := len(stringsValue(a["wake_sources"])) > 0, len(stringsValue(b["wake_sources"])) > 0
				if aw != bw {
					return aw
				}
				as, bs := textValue(a["subject_id"]), textValue(b["subject_id"])
				if as != bs {
					return as < bs
				}
				return textValue(a["id"]) < textValue(b["id"])
			})
			groups := map[string][]document{}
			order := []string{}
			for _, c := range candidates {
				sid := textValue(c["subject_id"])
				if _, ok := groups[sid]; !ok {
					order = append(order, sid)
				}
				groups[sid] = append(groups[sid], c)
			}
			short := []document{}
			for level := 0; level < 5; level++ {
				for _, sid := range order {
					if len(short) < 64 && level < len(groups[sid]) {
						c := clone(groups[sid][level])
						c["state"] = questionMap[textValue(c["id"])]["state"]
						short = append(short, c)
					}
				}
			}
			response, e := model.call("frontier", document{"context": context, "budget": budget, "questions": short})
			if e != nil {
				return nil, e
			}
			calls++
			allowed := index(short)
			seen := map[string]bool{}
			counts := map[string]int{}
			for _, item := range items(response["selected"]) {
				id := textValue(item["id"])
				sid := textValue(allowed[id]["subject_id"])
				if allowed[id] == nil || seen[id] || len(selection) >= budget || counts[sid] >= 2 {
					continue
				}
				seen[id] = true
				counts[sid]++
				selection = append(selection, document{"id": id, "reason": textValue(item["reason"])})
			}
		}
	}
	selected := index(selection)
	for id, q := range questionMap {
		if selected[id] == nil && !contains([]string{"answered", "partially_answered", "retired_evidence"}, textValue(q["state"])) {
			q["state"] = "backlog"
		}
	}
	idx, e := newEvidenceIndex(active, sources)
	if e != nil {
		return nil, e
	}
	defer idx.db.Close()
	investigations := []document{}
	evidenceClaims, evidenceSources := map[string]document{}, map[string]document{}
	questionSpans := map[string][]document{}
	reused := 0
	for _, chosen := range selection {
		id := textValue(chosen["id"])
		q := questionMap[id]
		if q == nil {
			continue
		}
		q["priority_reason"] = chosen["reason"]
		query := strings.Join(stringsValue(q["search_terms"]), " ")
		if query == "" {
			query = textValue(q["question"])
		}
		terms := boundedQuestionTerms(query)
		if len(terms) == 0 {
			q["state"] = "blocked_by_evidence"
			continue
		}
		hits, err := idx.search(terms, 48)
		if err != nil {
			return nil, err
		}
		cc, sc := []document{}, []document{}
		for _, h := range hits {
			key := textValue(h["id"])
			body := textValue(h["text"])
			if h["kind"] == "claim" && len(cc) < 4 && runeLength(body) <= 1200 {
				cc = append(cc, choose(active[key], "id", "text", "scope", "kind"))
			}
			if h["kind"] == "source" && len(sc) < 3 {
				sc = append(sc, evidenceSnippet(key, object(sources[key]), terms, 300, 1200, false))
			}
		}
		waking := []document{}
		wakeIDs := stringsValue(q["wake_sources"])
		for _, sid := range wakeIDs[:min(2, len(wakeIDs))] {
			if sources[sid] != nil {
				waking = append(waking, evidenceSnippet(sid, object(sources[sid]), terms, 300, 1200, true))
			}
		}
		processed := []string{}
		for _, span := range waking {
			processed = append(processed, textValue(span["source"]))
		}
		if len(waking) > 0 {
			merged := append([]document{}, waking...)
			for _, span := range sc {
				if !contains(processed, textValue(span["source"])) {
					merged = append(merged, span)
				}
			}
			sc = merged[:min(3, len(merged))]
		}
		attempt := identity([]any{"maintenance-3", q["question"], cc, sc})
		consume := func() {
			q["processed_wakes"] = sortedUnique(append(stringsValue(q["processed_wakes"]), processed...))
			delete(q, "wake_sources")
		}
		if q["attempt_hash"] == attempt {
			consume()
			reused++
			q["state"] = "blocked_by_evidence"
			if len(object(q["answer"])) > 0 {
				q["state"] = "partially_answered"
				if flag(object(q["answer"])["complete"]) {
					q["state"] = "answered"
				}
			}
			continue
		}
		if a := object(q["answer"]); len(a) > 0 {
			q["history"] = append(items(q["history"]), document{"answer": a, "reason": "Relevant search evidence changed; review required"})
			delete(q, "answer")
		}
		if len(cc)+len(sc) == 0 {
			q["attempt_hash"] = attempt
			q["state"] = "blocked_by_evidence"
			continue
		}
		claimIDs, sourceIDs := []string{}, []string{}
		for _, c := range cc {
			claimIDs = append(claimIDs, textValue(c["id"]))
		}
		for _, s := range sc {
			sourceIDs = append(sourceIDs, textValue(s["source"]))
		}
		item := document{"id": id, "question": q["question"], "claims": claimIDs, "sources": sourceIDs}
		pc, ps := map[string]document{}, map[string]document{}
		for k, v := range evidenceClaims {
			pc[k] = v
		}
		for k, v := range evidenceSources {
			ps[k] = v
		}
		for _, c := range cc {
			pc[textValue(c["id"])] = c
		}
		for _, s := range sc {
			ps[textValue(s["id"])] = s
		}
		if jsonCharacterLength(document{"questions": append(investigations, item), "claims": sortedDocuments(pc), "sources": sortedDocuments(ps)}) > 32000 {
			q["state"] = "budget_deferred"
			continue
		}
		q["attempt_hash"] = attempt
		q["state"] = "blocked_by_evidence"
		q["search"] = document{"claims": claimIDs, "sources": sourceIDs, "method": "bounded FTS search over active claims and original messages"}
		consume()
		questionSpans[id] = sc
		investigations = append(investigations, item)
		evidenceClaims, evidenceSources = pc, ps
	}
	if len(investigations) > 0 {
		response, err := model.call("investigate", document{"questions": investigations, "claims": sortedDocuments(evidenceClaims), "sources": sortedDocuments(evidenceSources)})
		if err != nil {
			return nil, err
		}
		calls++
		permitted := index(investigations)
		seen := map[string]bool{}
		accepted := []document{}
		for _, raw := range items(response["answers"]) {
			id := textValue(raw["question"])
			allowed := permitted[id]
			text := textValue(raw["text"])
			if allowed == nil || seen[id] || strings.TrimSpace(text) == "" {
				continue
			}
			refs := stringsValue(raw["claims"])
			evs := items(raw["evidence"])
			valid := len(refs)+len(evs) > 0 && referencesAllowed(raw["claims"], evidenceClaims) && isDocumentArray(raw["evidence"])
			for _, ref := range refs {
				valid = valid && contains(stringsValue(allowed["claims"]), ref)
			}
			evidence := []document{}
			for _, ev := range evs {
				sid, quote := textValue(ev["source"]), textValue(ev["quote"])
				matched := false
				for _, span := range questionSpans[id] {
					if span["source"] == sid && quote != "" && strings.Contains(textValue(span["quote"]), quote) {
						matched = true
					}
				}
				if !matched {
					valid = false
					break
				}
				source := object(sources[sid])
				evidence = append(evidence, merge(ev, document{"source_hash": identity(source["text"]), "role": source["role"], "ts": source["ts"]}))
			}
			if !valid {
				continue
			}
			heading := textValue(raw["heading"])
			if heading == "" {
				heading = "Further understanding"
			}
			accepted = append(accepted, document{"question": id, "heading": heading, "text": text, "claims": unique(refs), "evidence": evidence})
			seen[id] = true
		}
		if len(accepted) > 0 {
			qs := []document{}
			for _, a := range accepted {
				id := textValue(a["question"])
				qs = append(qs, document{"id": id, "question": questionMap[id]["question"]})
			}
			review, err := model.call("review_answers", document{"answers": accepted, "questions": qs, "claims": sortedDocuments(evidenceClaims), "sources": sortedDocuments(evidenceSources)})
			if err != nil {
				return nil, err
			}
			calls++
			checks := map[string]document{}
			for _, check := range items(review["checks"]) {
				checks[textValue(check["question"])] = check
			}
			for _, a := range accepted {
				id := textValue(a["question"])
				check := checks[id]
				if flag(check["supported"]) {
					a["complete"] = flag(check["complete"])
					a["review"] = "separate same-model entailment check"
					questionMap[id]["answer"] = a
					questionMap[id]["state"] = "partially_answered"
					if flag(check["complete"]) {
						questionMap[id]["state"] = "answered"
					}
				}
			}
		}
	}
	current := map[string]bool{}
	for _, s := range items(result["subjects"]) {
		for _, q := range items(s["questions"]) {
			current[textValue(q["id"])] = true
		}
	}
	retired := index(items(previous["retired_questions"]))
	for id, q := range oldQuestions {
		if !current[id] {
			retired[id] = merge(clone(q), document{"retirement_reason": "Question removed or rephrased; semantic equivalence not assumed"})
		}
	}
	result["retired_questions"] = sortedDocuments(retired)
	states := document{}
	for _, q := range questionMap {
		state := textValue(q["state"])
		states[state] = int(number(states[state])) + 1
	}
	result["maintenance"] = document{"policy": "maintenance-3", "plan_hash": catalogHash, "selection": selection, "selected": len(selected), "backlog": int(number(states["backlog"])), "answered": int(number(states["answered"])), "states": states, "provider_calls": calls, "reused_attempts": reused, "context_limit_characters": 32000, "limitation": "Bounded source search and same-model review; top-k candidate changes trigger repair, but lower-ranked unseen evidence can be missed. Blocked means no accepted answer in this attempt, including review rejection. No external research or new source facts."}
	return result, nil
}

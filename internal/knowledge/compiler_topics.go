package knowledge

import (
	"errors"
	"sort"
	"strings"
	"time"
)

func reconcileTopics(learning, previous document, decisions []document) (document, error) {
	state := clone(previous)
	if state["concepts"] == nil {
		state = document{"concepts": document{}, "redirects": document{}, "history": []document{}, "applied": []string{}}
	}
	concepts := object(state["concepts"])
	prior := map[string]bool{}
	for id := range concepts {
		prior[id] = true
	}
	matched := map[string]bool{}
	for _, subject := range items(learning["subjects"]) {
		id := textValue(subject["id"])
		matches := []document{}
		for _, raw := range concepts {
			c := object(raw)
			if textValue(c["state"]) == "active" && contains(stringsValue(c["source_subjects"]), id) {
				matches = append(matches, c)
			}
		}
		if len(matches) == 0 {
			for _, raw := range concepts {
				c := object(raw)
				cid := textValue(c["id"])
				if prior[cid] && !matched[cid] && textValue(c["state"]) == "active" && strings.EqualFold(textValue(c["scope"]), textValue(subject["scope"])) && identity(sortedUnique(stringsValue(c["notes"]))) == identity(sortedUnique(stringsValue(subject["notes"]))) {
					matches = append(matches, c)
				}
			}
		}
		if len(matches) == 1 && !flag(matches[0]["membership_locked"]) {
			c := matches[0]
			matched[textValue(c["id"])] = true
			c["source_subjects"] = sortedUnique(append(stringsValue(c["source_subjects"]), id))
			c["notes"] = sortedUnique(append(stringsValue(c["notes"]), stringsValue(subject["notes"])...))
		} else if len(matches) == 0 && concepts[id] == nil {
			concepts[id] = document{"id": id, "title": subject["title"], "scope": subject["scope"], "notes": stringsValue(subject["notes"]), "source_subjects": []string{id}, "aliases": stringsValue(subject["aliases"]), "state": "active"}
		}
	}
	for _, decision := range decisions {
		did := textValue(decision["id"])
		if did == "" || textValue(decision["reviewed_by"]) == "" {
			return nil, errors.New("authenticated topic decision ID and reviewer required")
		}
		if contains(stringsValue(state["applied"]), did) {
			continue
		}
		before := clone(choose(state, "concepts", "redirects"))
		op := textValue(decision["op"])
		if op == "undo" {
			history := items(state["history"])
			if len(history) == 0 || history[len(history)-1]["id"] != decision["target"] {
				return nil, errors.New("topic undo must name the latest structural decision")
			}
			last := object(history[len(history)-1]["before"])
			state["concepts"] = clone(object(last["concepts"]))
			state["redirects"] = clone(object(last["redirects"]))
			state["history"] = history[:len(history)-1]
			state["applied"] = append(stringsValue(state["applied"]), did)
			concepts = object(state["concepts"])
			continue
		}
		target := textValue(decision["target"])
		if target == "" {
			target = textValue(decision["into"])
		}
		current := object(concepts[target])
		if textValue(current["state"]) != "active" {
			return nil, errors.New("topic decision target must be active")
		}
		switch op {
		case "rename":
			title := strings.TrimSpace(textValue(decision["title"]))
			if title == "" {
				return nil, errors.New("topic title cannot be empty")
			}
			current["aliases"] = sortedUnique(append(stringsValue(current["aliases"]), textValue(current["title"])))
			current["title"] = title
		case "merge":
			ids := stringsValue(decision["from"])
			if len(ids) == 0 || contains(ids, target) || len(unique(ids)) != len(ids) {
				return nil, errors.New("distinct topic merge sources required")
			}
			for _, id := range ids {
				c := object(concepts[id])
				if textValue(c["state"]) != "active" || !strings.EqualFold(textValue(c["scope"]), textValue(current["scope"])) {
					return nil, errors.New("topic merge sources must be active in the same scope")
				}
			}
			for _, id := range ids {
				c := object(concepts[id])
				for _, key := range []string{"notes", "source_subjects", "aliases"} {
					current[key] = sortedUnique(append(stringsValue(current[key]), stringsValue(c[key])...))
				}
				current["aliases"] = sortedUnique(append(stringsValue(current["aliases"]), textValue(c["title"])))
				c["state"] = "retired"
				object(state["redirects"])[id] = []string{target}
			}
		case "split":
			children := items(decision["children"])
			members := []string{}
			for _, child := range children {
				members = append(members, stringsValue(child["notes"])...)
			}
			if len(children) < 2 || len(unique(members)) != len(members) || identity(sortedUnique(members)) != identity(sortedUnique(stringsValue(current["notes"]))) {
				return nil, errors.New("topic split must partition all member notes without overlap")
			}
			childIDs := []string{}
			for _, child := range children {
				title := strings.TrimSpace(textValue(child["title"]))
				notes := stringsValue(child["notes"])
				if title == "" || len(notes) == 0 {
					return nil, errors.New("topic split child requires title and member notes")
				}
				cid := identity([]any{"concept-child", target, sortedUnique(notes)})
				if concepts[cid] != nil {
					return nil, errors.New("topic split identity already exists")
				}
				concepts[cid] = merge(clone(current), document{"id": cid, "title": title, "notes": sortedUnique(notes), "aliases": []string{}, "membership_locked": true})
				childIDs = append(childIDs, cid)
			}
			current["state"] = "retired"
			object(state["redirects"])[target] = childIDs
		default:
			return nil, errors.New("unknown topic structural decision")
		}
		state["history"] = append(items(state["history"]), document{"id": did, "op": op, "reviewed_by": decision["reviewed_by"], "before": before})
		state["applied"] = append(stringsValue(state["applied"]), did)
	}
	subjects := index(items(learning["subjects"]))
	for _, raw := range concepts {
		c := object(raw)
		if textValue(c["state"]) != "active" || flag(c["membership_locked"]) {
			continue
		}
		notes := stringsValue(c["notes"])
		for _, sid := range stringsValue(c["source_subjects"]) {
			notes = append(notes, stringsValue(subjects[sid]["notes"])...)
		}
		c["notes"] = sortedUnique(notes)
	}
	return state, nil
}
func publishTopics(vault, learning, registry document) (document, error) {
	result := clone(vault)
	subjects := index(items(learning["subjects"]))
	original := index(items(vault["notes"]))
	active := activeClaims(vault)
	projected := []document{}
	concepts := object(registry["concepts"])
	ids := []string{}
	for id := range concepts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		c := object(concepts[id])
		if textValue(c["state"]) != "active" {
			continue
		}
		parts := []document{}
		for _, sid := range stringsValue(c["source_subjects"]) {
			if s := subjects[sid]; s != nil {
				parts = append(parts, s)
			}
		}
		if len(parts) == 0 {
			continue
		}
		notes := []string{}
		for _, nid := range stringsValue(c["notes"]) {
			if original[nid] != nil {
				notes = append(notes, nid)
			}
		}
		claims := []string{}
		allowed := map[string]document{}
		for _, nid := range notes {
			for _, cid := range stringsValue(original[nid]["claims"]) {
				claims = append(claims, cid)
				allowed[cid] = active[cid]
			}
		}
		sections := []document{}
		questions, conflicts := []document{}, []document{}
		seen := map[string]bool{}
		pending := []string{}
		for _, part := range parts {
			for _, s := range items(part["sections"]) {
				paragraphs := []document{}
				for _, p := range items(s["paragraphs"]) {
					key := identity([]any{p["text"], stringsValue(p["claims"]), items(p["evidence"])})
					if !seen[key] && (!flag(c["membership_locked"]) || (len(stringsValue(p["claims"])) > 0 && subset(stringsValue(p["claims"]), allowed))) {
						seen[key] = true
						paragraphs = append(paragraphs, clone(p))
					}
				}
				if len(paragraphs) > 0 {
					sections = append(sections, merge(s, document{"paragraphs": paragraphs}))
				}
			}
			for _, q := range items(part["questions"]) {
				if !flag(c["membership_locked"]) || subset(stringsValue(q["claims"]), allowed) {
					questions = append(questions, clone(q))
				}
			}
			for _, conflict := range items(part["conflicts"]) {
				if subset(stringsValue(conflict["claims"]), allowed) {
					conflicts = append(conflicts, clone(conflict))
				}
			}
			for _, cid := range stringsValue(part["editorial_pending_claims"]) {
				if _, ok := allowed[cid]; ok {
					pending = append(pending, cid)
				}
			}
		}
		state := parts[0]["state"]
		if len(sections) == 0 && state != "needs_partition" {
			state = "needs_evidence"
		}
		projected = append(projected, merge(clone(parts[0]), document{"id": id, "title": c["title"], "scope": c["scope"], "aliases": stringsValue(c["aliases"]), "notes": notes, "claims": unique(claims), "sections": sections, "questions": questions, "conflicts": conflicts, "state": state, "editorial_pending_claims": sortedUnique(pending)}))
	}
	result, err := projectSubjects(result, document{"subjects": projected})
	if err != nil {
		return nil, err
	}
	groups := map[string][]document{}
	for _, n := range items(result["notes"]) {
		if textValue(n["kind"]) == "subject" {
			key := strings.ToLower(textValue(n["scope"]))
			groups[key] = append(groups[key], n)
		}
	}
	keys := []string{}
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	notes := items(result["notes"])
	existing := index(notes)
	for _, key := range keys {
		members := groups[key]
		if len(members) < 2 {
			continue
		}
		scopes := []string{}
		for _, n := range members {
			scopes = append(scopes, textValue(n["scope"]))
		}
		sort.Strings(scopes)
		scope := scopes[0]
		id := identity([]any{"scope-map", strings.ToLower(scope)})
		if existing[id] != nil {
			return nil, errors.New("topic map identity collides with existing knowledge")
		}
		sort.Slice(members, func(i, j int) bool {
			return strings.ToLower(textValue(members[i]["title"])) < strings.ToLower(textValue(members[j]["title"]))
		})
		links := []document{}
		for _, member := range members {
			links = append(links, document{"id": member["id"], "kind": "contains", "reason": "Subject in this reading collection; not a factual relationship"})
			member["links"] = append(items(member["links"]), document{"id": id, "kind": "index", "reason": "Browse the wider topic collection"})
		}
		folder := append([]string{}, stringsValue(members[0]["folder"])...)
		for _, member := range members[1:] {
			path := stringsValue(member["folder"])
			common := 0
			for common < len(folder) && common < len(path) && folder[common] == path[common] {
				common++
			}
			folder = folder[:common]
		}
		if len(folder) == 0 {
			folder = []string{scope}
		}
		notes = append(notes, document{"id": id, "kind": "map", "title": scope + " — topic map", "scope": scope, "folder": folder, "claims": []string{}, "sections": []document{}, "links": links})
	}
	result["notes"] = notes
	result["topic_registry"] = clone(registry)
	return result, nil
}
func validSourceEvidence(evidence []document, sources document, requireHash bool) bool {
	for _, e := range evidence {
		source, ok := sources[textValue(e["source"])]
		quote := textValue(e["quote"])
		if !ok || quote == "" || !strings.Contains(textValue(object(source)["text"]), quote) {
			return false
		}
		if requireHash || e["source_hash"] != nil {
			if textValue(e["source_hash"]) != identity(object(source)["text"]) {
				return false
			}
		}
	}
	return true
}
func evidenceBasis(sources document, evidence []document) document {
	basis, _ := evidenceBasisWithFreshness(sources, evidence, nil, nil)
	return basis
}

// Freshness is an optional host policy, never a truth score or a reason to
// rewrite claims. Supplying only one policy value leaves age unclassified.
func evidenceBasisWithFreshness(sources document, evidence []document, asOf *time.Time, staleAfter *time.Duration) (document, error) {
	if asOf != nil && staleAfter != nil && *staleAfter < 0 {
		return nil, errors.New("nonnegative freshness horizon required")
	}
	ids := []string{}
	for _, e := range evidence {
		if sources[textValue(e["source"])] != nil {
			ids = append(ids, textValue(e["source"]))
		}
	}
	kinds, primary, observed, roles := []string{}, []string{}, []string{}, []string{}
	for _, id := range sortedUnique(ids) {
		s := object(sources[id])
		p := object(s["provenance"])
		kind := textValue(p["kind"])
		if kind == "" {
			kind = "legacy_unknown"
			if textValue(s["role"]) == "user" {
				kind = "user_statement"
			}
		}
		kinds = append(kinds, kind)
		roles = append(roles, textValue(s["role"]))
		if contains([]string{"repository_snapshot", "document_fetch", "command_result"}, kind) {
			primary = append(primary, id)
		}
		ts := textValue(p["valid_at"])
		if ts == "" {
			ts = textValue(s["ts"])
		}
		observed = append(observed, ts)
	}
	// Admission validates source timestamps. Compare their instants while
	// retaining the exact recorded spellings in the resulting metadata.
	stamps := make(map[string]time.Time, len(observed))
	for _, ts := range observed {
		stamp, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return nil, errors.New("knowledge evidence has an invalid observation timestamp")
		}
		stamps[ts] = stamp
	}
	sort.SliceStable(observed, func(i, j int) bool { return stamps[observed[i]].Before(stamps[observed[j]]) })
	var first, last, stale any
	if len(observed) > 0 {
		first = observed[0]
		last = observed[len(observed)-1]
	}
	if asOf != nil && staleAfter != nil {
		stale = false
		for _, ts := range observed {
			if asOf.After(stamps[ts].Add(*staleAfter)) {
				stale = true
				break
			}
		}
	}
	return document{"source_kinds": sortedUnique(kinds), "source_roles": sortedUnique(roles), "primary_artifact_sources": primary, "observed_from": first, "observed_to": last, "age_exceeds_policy": stale, "independently_verified": false}, nil
}
func projectSubjects(vault, learning document) (document, error) {
	result := clone(vault)
	original := index(items(result["notes"]))
	active := activeClaims(vault)
	sources := object(vault["sources"])
	memberships := map[string][]string{}
	for _, s := range items(learning["subjects"]) {
		id := textValue(s["id"])
		if original[id] != nil {
			return nil, errors.New("subject identity collides with evidence note")
		}
		for _, nid := range stringsValue(s["notes"]) {
			memberships[nid] = append(memberships[nid], id)
		}
	}
	notes := items(result["notes"])
	for _, subject := range items(learning["subjects"]) {
		sections := []document{}
		stale := false
		for _, s := range items(subject["sections"]) {
			paragraphs := []document{}
			for _, p := range items(s["paragraphs"]) {
				refs := append(append([]string{}, stringsValue(p["claims"])...), stringsValue(p["depends_on"])...)
				evidence := items(p["evidence"])
				supported := len(stringsValue(p["claims"])) > 0 || (textValue(p["derivation"]) == "subject_guide" && p["review"] != nil && len(evidence) > 0)
				if supported && subset(refs, active) && validSourceEvidence(evidence, sources, true) {
					paragraphs = append(paragraphs, clone(p))
				} else {
					stale = true
				}
			}
			if len(paragraphs) > 0 {
				sections = append(sections, merge(s, document{"paragraphs": paragraphs}))
			}
		}
		questions := items(clone(document{"questions": subject["questions"]})["questions"])
		answerClaims := []string{}
		for _, q := range questions {
			answer := object(q["answer"])
			if len(answer) == 0 {
				continue
			}
			if !subset(stringsValue(q["claims"]), active) || !subset(stringsValue(answer["claims"]), active) || !validSourceEvidence(items(answer["evidence"]), sources, true) {
				delete(q, "answer")
				q["state"] = "needs_review"
				stale = true
				continue
			}
			if contains([]string{"answered", "partially_answered"}, textValue(q["state"])) {
				sections = append(sections, document{"heading": answer["heading"], "paragraphs": []document{{"text": answer["text"], "claims": answer["claims"], "evidence": answer["evidence"], "question": q["id"], "partial": textValue(q["state"]) == "partially_answered", "derivation": "gap_answer", "review": answer["review"], "depends_on": q["claims"]}}})
				answerClaims = append(answerClaims, stringsValue(answer["claims"])...)
			}
		}
		pending := []string{}
		for _, id := range sortedUnique(stringsValue(subject["editorial_pending_claims"])) {
			if active[id] != nil {
				pending = append(pending, id)
			}
		}
		conflictIDs := map[string]bool{}
		for _, c := range items(subject["conflicts"]) {
			if textValue(c["state"]) == "unresolved" && subset(stringsValue(c["claims"]), active) {
				for _, id := range stringsValue(c["claims"]) {
					conflictIDs[id] = true
				}
			}
		}
		for _, s := range sections {
			for _, p := range items(s["paragraphs"]) {
				evidence := append([]document{}, items(p["evidence"])...)
				kinds := []string{}
				conflict := false
				for _, id := range stringsValue(p["claims"]) {
					if c := active[id]; c != nil {
						kinds = append(kinds, textValue(c["kind"]))
						evidence = append(evidence, items(c["evidence"])...)
					}
					conflict = conflict || conflictIDs[id]
				}
				p["evidence_basis"] = merge(evidenceBasis(sources, evidence), document{"claim_kinds": sortedUnique(kinds), "unresolved_conflict": conflict})
			}
		}
		roots := map[string]int{}
		links := []document{}
		related := map[string]bool{}
		for _, nid := range stringsValue(subject["notes"]) {
			n := original[nid]
			if n == nil {
				continue
			}
			folder := stringsValue(n["folder"])
			if len(folder) > 0 {
				roots[folder[0]]++
			}
			links = append(links, document{"id": nid, "kind": "explains", "reason": "Detailed evidence for this subject"})
		}
		for _, nid := range stringsValue(subject["notes"]) {
			for _, edge := range items(original[nid]["links"]) {
				for _, target := range memberships[textValue(edge["id"])] {
					if target != textValue(subject["id"]) && textValue(edge["reason"]) != "" && !related[target] && len(related) < 3 {
						related[target] = true
						links = append(links, document{"id": target, "kind": "related", "reason": edge["reason"], "via": []string{nid, textValue(edge["id"])}})
					}
				}
			}
		}
		root := textValue(subject["scope"])
		count := 0
		for key, n := range roots {
			if n > count || (n == count && key < root) {
				count = n
				root = key
			}
		}
		state := subject["state"]
		if stale {
			state = "needs_review"
		}
		notes = append(notes, document{"id": subject["id"], "title": subject["title"], "scope": subject["scope"], "folder": subjectFolder(subject, root), "claims": unique(append(stringsValue(subject["claims"]), answerClaims...)), "sections": sections, "questions": questions, "aliases": stringsValue(subject["aliases"]), "conflicts": items(subject["conflicts"]), "learning_state": state, "kind": "subject", "links": links, "needs_editorial_update": len(pending) > 0})
	}
	result["notes"] = notes
	return result, nil
}

func subjectFolder(subject document, fallback string) []string {
	if folder := stringsValue(subject["folder"]); len(folder) > 0 {
		return folder
	}
	return []string{fallback}
}

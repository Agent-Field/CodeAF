package knowledge

import (
	"fmt"
	"sort"
	"strings"
)

type quoteSpan struct{ start, end int }

type sourceClaimSpan struct {
	claim string
	quoteSpan
}

type sourceClaimSpans struct {
	text   string
	claims []sourceClaimSpan
}

// Every exact occurrence is retained because a repeated quote does not prove
// which occurrence originally supported a claim.
func exactQuoteSpans(body, quote string) []quoteSpan {
	spans := []quoteSpan{}
	if quote == "" {
		return spans
	}
	for offset := 0; offset <= len(body)-len(quote); {
		found := strings.Index(body[offset:], quote)
		if found < 0 {
			break
		}
		position := offset + found
		start := runeLength(body[:position])
		spans = append(spans, quoteSpan{start: start, end: start + runeLength(quote)})
		offset = position + 1
	}
	return spans
}

// Dependencies describe overlapping evidence, not extra facts asserted by prose.
func sourceClaimDependencies(vault document) map[string]sourceClaimSpans {
	result := map[string]sourceClaimSpans{}
	sources := object(vault["sources"])
	for _, claim := range items(vault["claims"]) {
		for _, evidence := range items(claim["evidence"]) {
			source := textValue(evidence["source"])
			entry := result[source]
			entry.text = textValue(object(sources[source])["text"])
			for _, span := range exactQuoteSpans(entry.text, textValue(evidence["quote"])) {
				entry.claims = append(entry.claims, sourceClaimSpan{claim: textValue(claim["id"]), quoteSpan: span})
			}
			result[source] = entry
		}
	}
	return result
}

func paragraphDependencies(paragraph document, sourceClaims map[string]sourceClaimSpans) []string {
	refs := append([]string{}, stringsValue(paragraph["claims"])...)
	if textValue(paragraph["derivation"]) == "subject_guide" {
		// Recompute derived dependencies so older source-wide metadata cannot
		// invalidate a disjoint cited span.
		for _, evidence := range items(paragraph["evidence"]) {
			source := sourceClaims[textValue(evidence["source"])]
			quote := textValue(evidence["quote"])
			spans := exactQuoteSpans(source.text, quote)
			start, hasStart := evidence["span_start"]
			end, hasEnd := evidence["span_end"]
			if hasStart && hasEnd {
				span := quoteSpan{start: int(number(start)), end: int(number(end))}
				if span.start >= 0 && span.end > span.start && textSpan(source.text, span.start, span.end-span.start) == quote {
					spans = []quoteSpan{span}
				}
			}
			for _, claim := range source.claims {
				for _, span := range spans {
					if claim.start < span.end && span.start < claim.end {
						refs = append(refs, claim.claim)
						break
					}
				}
			}
		}
	} else {
		refs = append(refs, stringsValue(paragraph["depends_on"])...)
	}
	return sortedUnique(refs)
}

func learnerSeedClaim(claim, sources document) document {
	item := choose(claim, "id", "text", "kind", "scope")
	roles := []string{}
	for _, evidence := range items(claim["evidence"]) {
		source := object(sources[textValue(evidence["source"])])
		if len(source) > 0 {
			roles = append(roles, textValue(source["role"]))
		}
	}
	item["source_roles"] = sortedUnique(roles)
	return item
}

func learnSubject(vault, subject, previous document, model compilerModel, goal string, maxSteps, readBudget int) (document, error) {
	if maxSteps < 1 || maxSteps > 8 || readBudget < 1000 || readBudget > 40000 {
		return nil, fmt.Errorf("learner bounds: 1–8 steps and 1,000–40,000 source characters per evidence part")
	}
	subject = clone(subject)
	subject["conflicts"] = activeSubjectConflicts(vault, subject)
	sources := object(vault["sources"])
	active := activeClaims(vault)
	previousParts := object(previous["parts"])
	groups := [][]string{}
	covered := map[string]bool{}
	previousOrder := stringsValue(previous["part_order"])
	for i, key := range previousOrder {
		ids := stringsValue(object(previousParts[key])["claim_ids"])
		// Every nonfinal part was closed by the claim or character limit. Keep
		// those memberships stable when a correction invalidates an earlier part.
		if len(ids) == 0 || len(ids) > 80 || (len(ids) < 80 && i == len(previousOrder)-1) {
			continue
		}
		valid := true
		for _, id := range ids {
			if active[id] == nil || covered[id] || !contains(stringsValue(subject["claims"]), id) {
				valid = false
			}
		}
		if valid {
			groups = append(groups, ids)
			for _, id := range ids {
				covered[id] = true
			}
		}
	}
	current := []string{}
	size := 2
	for _, claim := range items(vault["claims"]) {
		id := textValue(claim["id"])
		if textValue(claim["status"]) != "active" || covered[id] || !contains(stringsValue(subject["claims"]), id) {
			continue
		}
		length := asciiJSONLength(learnerSeedClaim(claim, sources)) + 1
		if len(current) > 0 && (len(current) == 80 || size+length > 14000) {
			groups = append(groups, current)
			current, size = []string{}, 2
		}
		current = append(current, id)
		size += length
	}
	if len(current) > 0 || len(groups) == 0 {
		groups = append(groups, current)
	}
	parts := document{}
	considered := []string{}
	partOrder := []string{}
	sections, blocks, trace := []document{}, []document{}, []document{}
	gaps := []string{}
	fingerprints := []any{}
	steps, characters := 0, 0
	for _, ids := range groups {
		considered = append(considered, ids...)
		part := clone(subject)
		part["claims"] = ids
		part["conflicts"] = conflictsForClaims(items(subject["conflicts"]), ids)
		part["section_context"] = "This is one evidence segment of the same canonical guide. Write only useful sections supported by this segment; omit repeated introductions."
		partGoal := goal + " Write only sections supported by this evidence segment; other segments of this guide are processed separately."
		key := identity(ids)
		prior := object(previousParts[key])
		if len(previousParts) == 0 && len(groups) == 1 {
			prior = previous
		}
		guide, err := learnSubjectPart(vault, part, prior, model, partGoal, maxSteps, readBudget)
		if err != nil {
			return nil, err
		}
		guide["claim_ids"] = ids
		parts[key] = guide
		partOrder = append(partOrder, key)
		fingerprints = append(fingerprints, []any{key, guide["input_hash"]})
		sections = append(sections, items(guide["sections"])...)
		blocks = unionDocuments(blocks, items(guide["blocks"]))
		for _, entry := range items(guide["trace"]) {
			trace = append(trace, merge(entry, document{"part": key}))
		}
		gaps = unique(append(gaps, stringsValue(guide["gaps"])...))
		steps += int(number(guide["steps"]))
		characters += int(number(guide["source_characters"]))
	}
	sections = mergeSubjectSections(sections)
	state := "insufficient_evidence"
	if len(sections) > 0 {
		state = "reviewed"
	}
	return document{"subject": subject["id"], "input_hash": identity([]any{"learner-parts-1", fingerprints}), "state": state, "sections": sections, "considered_claims": sortedUnique(considered), "blocks": blocks, "trace": trace, "gaps": gaps[:min(5, len(gaps))], "source_characters": characters, "steps": steps, "parts": parts, "part_order": partOrder, "part_count": len(groups), "max_steps_per_part": maxSteps, "source_character_budget_per_part": readBudget, "limitation": "Read-only evidence parts; each part has its own step and source-read budget. Aggregate work scales with parts; same-model support review is not independent verification."}, nil
}

func learnSubjectPart(vault, subject, previous document, model compilerModel, goal string, maxSteps, readBudget int) (document, error) {
	if maxSteps < 1 || maxSteps > 8 || readBudget < 1000 || readBudget > 40000 {
		return nil, fmt.Errorf("learner bounds: 1–8 steps and 1,000–40,000 source characters")
	}
	sources := object(vault["sources"])
	active := activeClaims(vault)
	sourceClaims := sourceClaimDependencies(vault)
	claims := map[string]document{}
	orderedClaims := []document{}
	seedIDs := []string{}
	for _, c := range items(vault["claims"]) {
		id := textValue(c["id"])
		if c["status"] == "active" && contains(stringsValue(subject["claims"]), id) {
			claims[id] = c
			orderedClaims = append(orderedClaims, c)
			for _, ev := range items(c["evidence"]) {
				sid := textValue(ev["source"])
				if sources[sid] != nil {
					seedIDs = append(seedIDs, sid)
				}
			}
		}
	}
	seedIDs = sortedUnique(seedIDs)
	fingerprints := []any{}
	for _, id := range seedIDs {
		s := object(sources[id])
		fingerprints = append(fingerprints, []any{id, identity(s["text"]), s["role"], s["ts"]})
	}
	digest := identity([]any{"learner-5", subject["id"], subject["title"], orderedClaims, goal, maxSteps, readBudget, fingerprints, items(subject["conflicts"])})
	cacheValid := previous["input_hash"] == digest
	for _, b := range items(previous["blocks"]) {
		s := object(sources[textValue(b["source"])])
		if len(s) == 0 || b["source_hash"] != identity(s["text"]) || b["role"] != s["role"] || b["ts"] != s["ts"] {
			cacheValid = false
		}
	}
	for _, section := range items(previous["sections"]) {
		for _, paragraph := range items(section["paragraphs"]) {
			if !subset(paragraphDependencies(paragraph, sourceClaims), active) {
				cacheValid = false
			}
		}
	}
	if cacheValid {
		return clone(previous), nil
	}
	seed := []document{}
	for _, c := range orderedClaims {
		seed = append(seed, learnerSeedClaim(c, sources))
	}
	shown := index(seed)
	idx, e := newEvidenceIndex(nil, sources)
	if e != nil {
		return nil, e
	}
	defer idx.db.Close()
	blocks := map[string]document{}
	blockOrder := []document{}
	trace := []document{}
	used := 0
	var draft document
	block := func(id string, start, length int) document {
		s := object(sources[id])
		if len(s) == 0 || used >= readBudget {
			return document{"error": "Unknown permitted source or source-read budget exhausted"}
		}
		body := textValue(s["text"])
		start = max(0, min(start, runeLength(body)))
		length = min(max(1, length), 4000, readBudget-used)
		quote := textSpan(body, start, length)
		if quote == "" {
			return document{"error": "No text at that position"}
		}
		key := identity([]any{id, start, quote})
		if blocks[key] == nil {
			used += runeLength(quote)
			blocks[key] = document{"id": key, "source": id, "quote": quote, "span_start": start, "span_end": start + runeLength(quote), "source_hash": identity(body), "role": s["role"], "ts": s["ts"]}
			blockOrder = append(blockOrder, blocks[key])
		}
		return blocks[key]
	}
	catalog := []document{}
	for _, id := range seedIDs[:min(80, len(seedIDs))] {
		s := object(sources[id])
		catalog = append(catalog, document{"id": id, "role": s["role"], "ts": s["ts"], "characters": runeLength(textValue(s["text"]))})
	}
	questions := []string{}
	for _, q := range items(subject["questions"]) {
		if len(questions) < 5 {
			questions = append(questions, textValue(q["question"]))
		}
	}
	notes := []document{}
	for _, n := range items(vault["notes"]) {
		if contains(stringsValue(subject["notes"]), textValue(n["id"])) {
			notes = append(notes, choose(n, "id", "title"))
		}
	}
	for step := 0; step < maxSteps; step++ {
		response, err := model.call("learner_step", document{"subject": document{"id": subject["id"], "title": subject["title"], "scope": subject["scope"], "questions": questions, "conflicts": items(subject["conflicts"]), "section_context": subject["section_context"]}, "goal": goal, "claims": seed, "omitted_claims": len(claims) - len(seed), "source_catalog": catalog, "notes": notes, "blocks": blockOrder, "trace": trace, "remaining_steps": maxSteps - step, "remaining_source_characters": readBudget - used})
		if err != nil {
			return nil, err
		}
		action := textValue(response["action"])
		if action == "write" {
			draft = response
			break
		}
		obs := document{}
		switch action {
		case "read":
			id, ok := response["source"].(string)
			start, length := 0, 3000
			valid := ok
			for _, key := range []string{"start", "length"} {
				if value, exists := response[key]; exists {
					n := number(value)
					switch value.(type) {
					case int, float64:
						if n != float64(int(n)) {
							valid = false
						}
					default:
						valid = false
					}
					if key == "start" {
						start = int(n)
					} else {
						length = int(n)
					}
				}
			}
			if !valid {
				obs["error"] = "Read requires a permitted source ID and integer span"
			} else {
				b := block(id, start, length)
				obs = document{"block": b["id"], "error": b["error"]}
			}
		case "search":
			query := textValue(response["query"])
			terms := boundedQuestionTerms(query)
			hits, err := idx.search(terms, 5)
			if err != nil {
				return nil, err
			}
			found := []document{}
			for _, h := range hits {
				id := textValue(h["id"])
				span := evidenceSnippet(id, object(sources[id]), terms, 100, 500, false)
				body := textValue(object(sources[id])["text"])
				pos := strings.Index(body, textValue(span["quote"]))
				start := 0
				if pos >= 0 {
					start = runeLength(body[:pos])
				}
				b := block(id, start, 500)
				if b["id"] != nil {
					found = append(found, document{"source": id, "block": b["id"]})
				}
			}
			obs = document{"query": query, "hits": found}
		default:
			obs["error"] = "Only search, read and write are supported"
		}
		trace = append(trace, document{"step": step + 1, "action": action, "observation": obs})
	}
	sections := []document{}
	rawSections := items(draft["sections"])
	for _, s := range rawSections[:min(8, len(rawSections))] {
		paragraphs := []document{}
		rawPars := items(s["paragraphs"])
		for _, p := range rawPars[:min(3, len(rawPars))] {
			text := textValue(p["text"])
			refs, bids := stringsValue(p["claims"]), stringsValue(p["blocks"])
			if strings.TrimSpace(text) == "" || len(refs)+len(bids) == 0 || !referencesAllowed(p["claims"], shown) || !referencesAllowed(p["blocks"], blocks) || articleNarration.MatchString(text) {
				continue
			}
			ev := []document{}
			for _, id := range unique(bids) {
				ev = append(ev, blocks[id])
			}
			paragraphs = append(paragraphs, document{"text": text, "claims": unique(refs), "evidence": ev})
		}
		if len(paragraphs) > 0 {
			sections = append(sections, document{"heading": textValue(s["heading"]), "paragraphs": paragraphs})
		}
	}
	pars := []document{}
	citedClaims, citedBlocks := map[string]document{}, map[string]document{}
	for _, s := range sections {
		for _, p := range items(s["paragraphs"]) {
			id := fmt.Sprintf("P%d", len(pars))
			bids := []string{}
			for _, ev := range items(p["evidence"]) {
				bid := textValue(ev["id"])
				bids = append(bids, bid)
				citedBlocks[bid] = blocks[bid]
			}
			for _, cid := range stringsValue(p["claims"]) {
				citedClaims[cid] = shown[cid]
			}
			p["_review_id"] = id
			pars = append(pars, document{"id": id, "text": p["text"], "claims": p["claims"], "blocks": bids})
		}
	}
	if len(pars) > 0 {
		review, err := model.call("review_guide", document{"subject": subject["title"], "conflicts": items(subject["conflicts"]), "claims": orderedReferences(seed, citedClaims), "blocks": orderedReferences(blockOrder, citedBlocks), "paragraphs": pars})
		if err != nil {
			return nil, err
		}
		checks := index(items(review["checks"]))
		keptSections := []document{}
		for _, s := range sections {
			kept := []document{}
			for _, p := range items(s["paragraphs"]) {
				check := checks[textValue(p["_review_id"])]
				delete(p, "_review_id")
				if flag(check["supported"]) && flag(check["useful"]) {
					p["derivation"] = "subject_guide"
					p["review"] = "separate same-model support and reference-usefulness checks"
					p["depends_on"] = paragraphDependencies(p, sourceClaims)
					if !subset(stringsValue(p["depends_on"]), active) {
						continue
					}
					kept = append(kept, p)
				}
			}
			if len(kept) > 0 {
				s["paragraphs"] = kept
				keptSections = append(keptSections, s)
			}
		}
		sections = keptSections
	}
	gaps := stringsValue(draft["gaps"])
	gaps = gaps[:min(5, len(gaps))]
	state := "insufficient_evidence"
	if len(sections) > 0 {
		state = "reviewed"
	}
	steps := len(trace)
	if draft != nil {
		steps++
	}
	return document{"subject": subject["id"], "input_hash": digest, "state": state, "sections": sections, "blocks": blockOrder, "trace": trace, "gaps": gaps, "source_characters": used, "steps": steps, "limitation": "Read-only bounded learner; same-model review and partial source coverage, no new canonical claims."}, nil
}

func auditCapture(vault, previous document, model compilerModel, sampleSize int, seed string) (document, error) {
	if sampleSize < 1 || sampleSize > 16 {
		return nil, fmt.Errorf("audit samples one to sixteen sources")
	}
	src := object(vault["sources"])
	ids := []string{}
	for id := range src {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return identity([]any{seed, ids[i]}) < identity([]any{seed, ids[j]}) })
	ids = ids[:min(sampleSize, len(ids))]
	sources := []document{}
	truncated := []string{}
	for _, id := range ids {
		s := object(src[id])
		item := choose(s, "id", "role", "ts")
		item["text"] = textSpan(textValue(s["text"]), 0, 1500)
		sources = append(sources, item)
		if runeLength(textValue(s["text"])) > 1500 {
			truncated = append(truncated, id)
		}
	}
	claims := []document{}
	eligible := 0
	for _, c := range items(vault["claims"]) {
		if c["status"] != "active" {
			continue
		}
		sourceIDs := []string{}
		relevant := false
		for _, ev := range items(c["evidence"]) {
			id := textValue(ev["source"])
			sourceIDs = append(sourceIDs, id)
			relevant = relevant || contains(ids, id)
		}
		if !relevant {
			continue
		}
		eligible++
		item := choose(c, "id", "text", "kind", "scope")
		item["source_ids"] = sortedUnique(sourceIDs)
		if len(claims) < 48 && asciiJSONLength(document{"sources": sources, "claims": append(claims, item)}) <= 28000 {
			claims = append(claims, item)
		}
	}
	digest := identity([]any{"omission-audit-1", sources, claims})
	if previous["input_hash"] == digest {
		r := clone(previous)
		r["model_calls"] = 0
		return r, nil
	}
	response := document{}
	calls := 0
	if len(sources) > 0 {
		var e error
		response, e = model.call("audit_omissions", document{"sources": sources, "captured_claims": claims, "max_candidates": 8})
		if e != nil {
			return nil, e
		}
		calls = 1
	}
	supplied := index(sources)
	candidates := []document{}
	seen := map[string]bool{}
	raw := items(response["omissions"])
	for _, item := range raw[:min(8, len(raw))] {
		sid, quote, text := textValue(item["source"]), textValue(item["quote"]), textValue(item["text"])
		if supplied[sid] == nil || quote == "" || !strings.Contains(textValue(supplied[sid]["text"]), quote) || strings.TrimSpace(text) == "" {
			continue
		}
		id := identity([]any{sid, quote, text})
		if seen[id] {
			continue
		}
		seen[id] = true
		candidates = append(candidates, document{"id": id, "source": sid, "text": textSpan(text, 0, 1000), "quote": quote, "reason": textSpan(textValue(item["reason"]), 0, 400), "state": "pending_review"})
	}
	return document{"input_hash": digest, "sampled_sources": ids, "candidates": candidates, "model_calls": calls, "captured_claims_omitted": eligible - len(claims), "truncated_sources": truncated, "limitation": "Sampled same-model omission candidates, not measured capture recall or accepted facts. Only initial source spans and at most 48 claims were inspected."}, nil
}

func orderedReferences(values []document, selected map[string]document) []document {
	result := []document{}
	for _, value := range values {
		if selected[textValue(value["id"])] != nil {
			result = append(result, value)
		}
	}
	return result
}

package knowledge

import (
	"fmt"
	"strings"
)

func composeArticles(vault document, model compilerModel) (document, error) {
	r := clone(vault)
	byID := index(items(r["claims"]))
	batches := [][]document{}
	batch := []document{}
	count := 0
	for _, note := range items(r["notes"]) {
		note["sections"] = []document{}
		size := 0
		for _, id := range stringsValue(note["claims"]) {
			if byID[id]["status"] == "active" {
				size++
			}
		}
		if len(batch) > 0 && (count+size > 80 || len(batch) >= 8) {
			batches = append(batches, batch)
			batch = []document{}
			count = 0
		}
		batch = append(batch, note)
		count += size
	}
	if len(batch) > 0 {
		batches = append(batches, batch)
	}
	for _, batch := range batches {
		notes := map[string]document{}
		claimIDs := []string{}
		for i, n := range batch {
			notes[fmt.Sprintf("N%d", i)] = n
			for _, id := range stringsValue(n["claims"]) {
				if byID[id]["status"] == "active" {
					claimIDs = append(claimIDs, id)
				}
			}
		}
		claimIDs = unique(claimIDs)
		if len(claimIDs) == 0 {
			continue
		}
		names, reverse := map[string]string{}, map[string]string{}
		for i, id := range claimIDs {
			local := fmt.Sprintf("C%d", i)
			names[id] = local
			reverse[local] = id
		}
		pn, pc := []document{}, []document{}
		for i, n := range batch {
			refs := []string{}
			for _, id := range stringsValue(n["claims"]) {
				if local := names[id]; local != "" {
					refs = append(refs, local)
				}
			}
			pn = append(pn, document{"id": fmt.Sprintf("N%d", i), "title": n["title"], "scope": n["scope"], "claims": refs, "related_notes": n["links"]})
		}
		for _, id := range claimIDs {
			c := byID[id]
			pc = append(pc, document{"id": names[id], "text": c["text"], "kind": c["kind"], "unsure": flag(c["unsure"])})
		}
		response, err := model.call("compose", document{"notes": pn, "claims": pc})
		if err != nil {
			return nil, err
		}
		for _, article := range items(response["notes"]) {
			note := notes[textValue(article["id"])]
			if note == nil {
				continue
			}
			allowed := stringsValue(note["claims"])
			sections := []document{}
			used := []string{}
			rejected := 0
			for _, s := range items(article["sections"]) {
				paragraphs := []document{}
				for _, p := range items(s["paragraphs"]) {
					text := textValue(p["text"])
					refs := stringsValue(p["claims"])
					ids := []string{}
					valid := strings.TrimSpace(text) != "" && !articleNarration.MatchString(text) && len(refs) > 0 && referencesAllowed(p["claims"], index(pc))
					for _, ref := range refs {
						id := reverse[ref]
						if id == "" || !contains(allowed, id) {
							valid = false
						}
						ids = append(ids, id)
					}
					if !valid {
						rejected++
						continue
					}
					ids = unique(ids)
					paragraphs = append(paragraphs, document{"text": text, "claims": ids})
					used = append(used, ids...)
				}
				if len(paragraphs) > 0 {
					sections = append(sections, document{"heading": textValue(s["heading"]), "paragraphs": paragraphs})
				}
			}
			available := 0
			for _, id := range claimIDs {
				if contains(allowed, id) {
					available++
				}
			}
			note["sections"] = sections
			note["composition"] = document{"cited_claims": len(unique(used)), "available_claims": available, "rejected_paragraphs": rejected, "limitation": "References validated; generated prose still needs semantic review."}
		}
	}
	return r, nil
}
func reviewArticles(vault document, model compilerModel) (document, error) {
	r := clone(vault)
	claims := index(items(r["claims"]))
	type paragraphRef struct{ note, section, paragraph document }
	all := []paragraphRef{}
	for _, n := range items(r["notes"]) {
		for _, s := range items(n["sections"]) {
			for _, p := range items(s["paragraphs"]) {
				all = append(all, paragraphRef{n, s, p})
			}
		}
	}
	for start := 0; start < len(all); start += 32 {
		batch := all[start:min(start+32, len(all))]
		payload := []document{}
		for i, item := range batch {
			refs := []document{}
			for _, id := range stringsValue(item.paragraph["claims"]) {
				refs = append(refs, choose(claims[id], "text", "kind"))
			}
			payload = append(payload, document{"id": fmt.Sprintf("P%d", i), "title": item.note["title"], "heading": item.section["heading"], "text": item.paragraph["text"], "claims": refs})
		}
		response, err := model.call("review_articles", document{"paragraphs": payload})
		if err != nil {
			return nil, err
		}
		checks := index(items(response["checks"]))
		for i, item := range batch {
			id := fmt.Sprintf("P%d", i)
			check := checks[id]
			if check == nil {
				check = document{"supported": false, "reason": "No paragraph review returned"}
			}
			item.paragraph["review"] = check
		}
	}
	for _, n := range items(r["notes"]) {
		sections, rejected := []document{}, []document{}
		for _, s := range items(n["sections"]) {
			kept := []document{}
			for _, p := range items(s["paragraphs"]) {
				check := object(p["review"])
				if flag(check["supported"]) {
					kept = append(kept, p)
				} else {
					reason := textValue(check["reason"])
					if reason == "" {
						reason = "Unreviewed"
					}
					rejected = append(rejected, merge(p, document{"reason": reason}))
				}
			}
			if len(kept) > 0 {
				sections = append(sections, merge(s, document{"paragraphs": kept}))
			}
		}
		n["sections"] = sections
		n["article_rejections"] = rejected
	}
	return r, nil
}

// checkFrozenContract evaluates replay outputs without assigning a model self-grade.
func checkFrozenContract(c, vault, answer document) document {
	cited := []string{}
	for _, e := range items(answer["citations"]) {
		cited = append(cited, textValue(e["source"]))
	}
	allIn := func(ids []string) bool {
		for _, id := range ids {
			if !contains(cited, id) {
				return false
			}
		}
		return true
	}
	anyIn := func(ids []string) bool {
		for _, id := range ids {
			if contains(cited, id) {
				return true
			}
		}
		return false
	}
	signals := stringsValue(c["answer_must_include_any"])
	signalOK := len(signals) == 0
	for _, s := range signals {
		signalOK = signalOK || strings.Contains(foldText(textValue(answer["answer"])), foldText(s))
	}
	kindOK := textValue(c["required_kind"]) == ""
	reported, retired := true, true
	for _, claim := range items(vault["claims"]) {
		if claim["status"] == "active" {
			kindOK = kindOK || claim["kind"] == c["required_kind"]
			reported = reported && claim["kind"] == "reported"
			for _, e := range items(claim["evidence"]) {
				if textValue(c["retire_source"]) != "" && e["source"] == c["retire_source"] {
					retired = false
				}
			}
		}
	}
	checks := document{"required_sources": allIn(stringsValue(c["required_sources"])), "any_required_source": len(stringsValue(c["any_required_sources"])) == 0 || anyIn(stringsValue(c["any_required_sources"])), "answer_signal": signalOK, "forbidden_sources": !anyIn(stringsValue(c["forbidden_sources"])), "required_kind": kindOK, "reported_only": !flag(c["reported_only"]) || reported, "abstention": !flag(c["abstain"]) || len(cited) == 0, "retired_source": retired}
	passed := true
	for _, v := range checks {
		passed = passed && flag(v)
	}
	return document{"id": c["id"], "passed": passed, "checks": checks, "answer": answer}
}

func evaluateFrozenContracts(cases, outputs []document) (document, error) {
	byID := index(outputs)
	results := []document{}
	passed := 0
	for _, c := range cases {
		out := byID[textValue(c["id"])]
		if out == nil {
			return nil, fmt.Errorf("missing replay output for frozen case %s", textValue(c["id"]))
		}
		r := checkFrozenContract(c, object(out["vault"]), object(out["answer"]))
		results = append(results, r)
		if flag(r["passed"]) {
			passed++
		}
	}
	result := document{"manifest_hash": identity(cases), "suite": "frozen-support-contracts-1", "cases": results, "passed": passed, "total": len(cases), "limitation": "Deterministic checks of source support, modality, retirement and scoping. This does not establish general semantic accuracy or production recall."}
	if passed != len(cases) {
		return result, fmt.Errorf("frozen support contracts failed: %d/%d passed", passed, len(cases))
	}
	return result, nil
}

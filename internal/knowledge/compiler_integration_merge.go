package knowledge

import (
	"errors"
	"fmt"
	"strings"
)

func validateIntegrationMerge(payload, response document) error {
	allowed := index(items(payload["paragraphs"]))
	headings := map[string]bool{}
	for _, p := range allowed {
		headings[textValue(p["heading"])] = true
	}
	seen := map[string]bool{}
	usedHeadings := map[string]bool{}
	sections := items(response["sections"])
	for _, section := range sections {
		heading := textValue(section["heading"])
		key := foldText(strings.TrimSpace(heading))
		if !headings[heading] || usedHeadings[key] || len(items(section["paragraphs"])) == 0 {
			return errors.New("knowledge integration merge requires an existing heading and paragraphs")
		}
		usedHeadings[key] = true
		for _, p := range items(section["paragraphs"]) {
			refs := stringsValue(p["inputs"])
			if strings.TrimSpace(textValue(p["text"])) == "" || len(refs) == 0 || !referencesAllowed(p["inputs"], allowed) {
				return errors.New("knowledge integration merge must cite inspected input paragraphs")
			}
			for _, id := range refs {
				seen[id] = true
			}
		}
	}
	if len(seen) != len(allowed) {
		return errors.New("knowledge integration merge must cover every inspected input")
	}
	return nil
}

func validateIntegrationReview(payload, response document) error {
	for _, pair := range []struct{ input, output string }{{"paragraphs", "coverage"}, {"proposed", "checks"}} {
		allowed := index(items(payload[pair.input]))
		seen := map[string]bool{}
		for _, check := range items(response[pair.output]) {
			id := textValue(check["id"])
			if allowed[id] == nil || seen[id] {
				return errors.New("knowledge integration review must inspect each paragraph exactly once")
			}
			seen[id] = true
		}
		if len(seen) != len(allowed) {
			return errors.New("knowledge integration review omitted a paragraph")
		}
	}
	return nil
}

// Merge only when placing useful additions exceeds the guide budget. Every
// input must remain represented, and a separate call checks support and meaning
// coverage before the host transfers its original citations to the new prose.
func mergeIntegratedGuide(note document, sections []document, vault document, model compilerModel) ([]document, error) {
	changed, unchanged := []document{}, []document{}
	for _, section := range sections {
		if hasGuideAnswers([]document{section}) {
			changed = append(changed, section)
		} else {
			unchanged = append(unchanged, section)
		}
	}
	words, paragraphs := 0, 0
	for _, section := range unchanged {
		words += len(strings.Fields(textValue(section["heading"])))
		for _, p := range items(section["paragraphs"]) {
			words += len(strings.Fields(textValue(p["text"])))
			paragraphs++
		}
	}
	if len(changed) > 0 && len(unchanged) > 0 && words < guideMaxWords-100 && paragraphs < guideMaxParagraphs && len(unchanged) < guideMaxSections {
		merged, err := mergeGuideWithinBudget(note, changed, vault, model, guideMaxWords-words, guideMaxParagraphs-paragraphs, guideMaxSections-len(unchanged))
		if err != nil {
			return nil, err
		}
		result, inserted := []document{}, false
		for _, section := range sections {
			if hasGuideAnswers([]document{section}) {
				if !inserted {
					result = append(result, merged...)
					inserted = true
				}
			} else {
				result = append(result, section)
			}
		}
		return result, nil
	}
	return mergeGuideWithinBudget(note, sections, vault, model, guideMaxWords, guideMaxParagraphs, guideMaxSections)
}

func mergeGuideWithinBudget(note document, sections []document, vault document, model compilerModel, maxWords, maxParagraphs, maxSections int) ([]document, error) {
	inputs := []document{}
	originals := map[string]document{}
	for _, section := range sections {
		for _, p := range items(section["paragraphs"]) {
			id := fmt.Sprintf("P%d", len(inputs))
			inputs = append(inputs, merge(choose(p, "text", "partial", "evidence_basis"), document{"id": id, "heading": section["heading"]}))
			originals[id] = p
		}
	}
	payload := document{"merge_policy": "integration-merge-1", "subject": choose(note, "id", "title", "scope"), "paragraphs": inputs, "max_sections": maxSections, "max_paragraphs": maxParagraphs, "max_words": maxWords, "target_words": maxWords * 4 / 5}
	inputWords := 0
	for _, p := range inputs {
		inputWords += len(strings.Fields(textValue(p["text"])))
	}
	for _, section := range sections {
		inputWords += len(strings.Fields(textValue(section["heading"])))
	}
	var proposal document
	accepted := false
	failure := "knowledge integration merge did not preserve supported meaning; prior publication retained"
	for attempt := 0; attempt < 2; attempt++ {
		var err error
		proposal, err = model.call("merge_integration", payload)
		if err != nil {
			return nil, err
		}
		withinTarget := integrationWithinBudget(items(proposal["sections"]), maxWords, maxParagraphs, maxSections)
		// After one repair, prefer reviewed meaning over an arbitrary word target.
		// Never expand the input, or relax structural limits.
		withinPreservationBudget := attempt > 0 && integrationWithinBudget(items(proposal["sections"]), max(maxWords, inputWords), maxParagraphs, maxSections)
		if !withinTarget && !withinPreservationBudget {
			failure = "knowledge integration merge exceeded the guide budget; prior publication retained"
			words, paragraphs := 0, 0
			for _, section := range items(proposal["sections"]) {
				words += len(strings.Fields(textValue(section["heading"])))
				for _, p := range items(section["paragraphs"]) {
					words += len(strings.Fields(textValue(p["text"])))
					paragraphs++
				}
			}
			payload["previous_draft"] = proposal
			payload["review_feedback"] = document{"error": "Exceeds the guide budget; condense repetition without losing input meaning", "observed_words": words, "observed_paragraphs": paragraphs, "observed_sections": len(items(proposal["sections"]))}
			continue
		}
		proposed := []document{}
		for _, section := range items(proposal["sections"]) {
			for _, p := range items(section["paragraphs"]) {
				proposed = append(proposed, merge(p, document{"id": fmt.Sprintf("R%d", len(proposed)), "heading": section["heading"]}))
			}
		}
		review, err := model.call("review_integration", document{"coverage_policy": "integration-coverage-1", "paragraphs": inputs, "proposed": proposed})
		if err != nil {
			return nil, err
		}
		accepted = true
		failure = "knowledge integration merge did not preserve supported meaning; prior publication retained"
		for _, check := range items(review["checks"]) {
			accepted = accepted && flag(check["supported"]) && flag(check["useful"])
		}
		for _, check := range items(review["coverage"]) {
			accepted = accepted && flag(check["preserved"])
		}
		if accepted {
			break
		}
		payload["previous_draft"] = proposal
		payload["review_feedback"] = review
	}
	if !accepted {
		return nil, errors.New(failure)
	}
	claims := activeClaims(vault)
	sources := object(vault["sources"])
	result := []document{}
	for _, section := range items(proposal["sections"]) {
		paragraphs := []document{}
		for _, raw := range items(section["paragraphs"]) {
			refs, dependencies, kinds := []string{}, []string{}, []string{}
			evidence := []document{}
			partial, conflict := false, false
			for _, id := range stringsValue(raw["inputs"]) {
				p := originals[id]
				refs = append(refs, stringsValue(p["claims"])...)
				dependencies = append(dependencies, stringsValue(p["claims"])...)
				dependencies = append(dependencies, stringsValue(p["depends_on"])...)
				evidence = unionEvidence(evidence, items(p["evidence"]))
				partial = partial || flag(p["partial"])
				conflict = conflict || flag(object(p["evidence_basis"])["unresolved_conflict"])
				kinds = append(kinds, stringsValue(object(p["evidence_basis"])["claim_kinds"])...)
			}
			for _, id := range sortedUnique(refs) {
				evidence = unionEvidence(evidence, items(claims[id]["evidence"]))
				kinds = append(kinds, textValue(claims[id]["kind"]))
			}
			for i, ev := range evidence {
				source := object(sources[textValue(ev["source"])])
				evidence[i] = merge(ev, document{"source_hash": identity(source["text"]), "role": source["role"], "ts": source["ts"]})
			}
			basis := merge(evidenceBasis(sources, evidence), document{"claim_kinds": sortedUnique(kinds), "unresolved_conflict": conflict})
			paragraphs = append(paragraphs, document{"text": raw["text"], "claims": sortedUnique(refs), "depends_on": sortedUnique(dependencies), "evidence": evidence, "evidence_basis": basis, "partial": partial, "derivation": "subject_guide", "review": "separate same-model support, usefulness and per-input coverage checks"})
		}
		result = append(result, document{"heading": section["heading"], "paragraphs": paragraphs})
	}
	return result, nil
}

func integrationWithinBudget(sections []document, maxWords, maxParagraphs, maxSections int) bool {
	words, paragraphs := 0, 0
	for _, section := range sections {
		words += len(strings.Fields(textValue(section["heading"])))
		for _, p := range items(section["paragraphs"]) {
			words += len(strings.Fields(textValue(p["text"])))
			paragraphs++
		}
	}
	return words <= maxWords && paragraphs <= maxParagraphs && len(sections) <= maxSections
}

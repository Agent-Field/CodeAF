package knowledge

import (
	"errors"
	"fmt"
	"strings"
)

const answerIntegrationPolicy = "answer-integration-1"

func hasGuideAnswers(sections []document) bool {
	for _, section := range sections {
		for _, p := range items(section["paragraphs"]) {
			if textValue(p["derivation"]) == "gap_answer" {
				return true
			}
		}
	}
	return false
}

func validateAnswerIntegration(payload, response document) error {
	answers := index(items(payload["answers"]))
	sections := index(items(payload["existing_sections"]))
	seen := map[string]bool{}
	for _, decision := range items(response["decisions"]) {
		id, action := textValue(decision["id"]), textValue(decision["action"])
		if answers[id] == nil || seen[id] {
			return errors.New("knowledge answer integration must decide each inspected answer exactly once")
		}
		seen[id] = true
		section := textValue(decision["section"])
		heading := strings.TrimSpace(textValue(decision["heading"]))
		switch action {
		case "covered", "integrate":
			if sections[section] == nil || heading != "" {
				return errors.New("knowledge answer integration must reference an inspected existing section")
			}
		case "new_section":
			if section != "" || heading == "" || strings.ContainsAny(heading, "\r\n") {
				return errors.New("knowledge answer integration requires a short new section heading")
			}
		case "evidence_only":
			if section != "" || heading != "" {
				return errors.New("knowledge evidence-only answers cannot introduce a section")
			}
		default:
			return errors.New("knowledge answer integration returned an unknown decision")
		}
	}
	if len(seen) != len(answers) {
		return errors.New("knowledge answer integration omitted an inspected answer")
	}
	return nil
}

// Placement keeps reviewed prose and support intact. Overflow uses a separate
// meaning-preserving merge and review instead of selecting away useful inputs.
func integrateGuideAnswers(note, previous, vault document, model compilerModel) ([]document, error) {
	base := []document{}
	answers := []document{}
	for _, section := range items(note["sections"]) {
		kept := []document{}
		for _, p := range items(section["paragraphs"]) {
			if textValue(p["derivation"]) == "gap_answer" {
				answers = append(answers, merge(clone(p), document{"heading": section["heading"]}))
			} else {
				kept = append(kept, clone(p))
			}
		}
		if len(kept) > 0 {
			base = append(base, merge(clone(section), document{"paragraphs": kept}))
		}
	}
	if len(answers) == 0 {
		return base, nil
	}
	// Selection may summarize raw detail, but it cannot silently discard a
	// still-supported paragraph the reader already had. Include those paragraphs
	// in the merge coverage inputs after checking current claim/source validity.
	active, sources := activeClaims(vault), object(vault["sources"])
	preserved := []document{}
	for _, section := range coreGuideSections(items(previous["sections"])) {
		paragraphs := []document{}
		for _, p := range items(section["paragraphs"]) {
			refs := append(append([]string{}, stringsValue(p["claims"])...), stringsValue(p["depends_on"])...)
			evidence := items(p["evidence"])
			supported := len(stringsValue(p["claims"])) > 0 || (textValue(p["derivation"]) == "subject_guide" && p["review"] != nil && len(evidence) > 0)
			if supported && subset(refs, active) && validSourceEvidence(evidence, sources, true) {
				paragraphs = append(paragraphs, p)
			}
		}
		if len(paragraphs) > 0 {
			preserved = append(preserved, merge(section, document{"paragraphs": paragraphs}))
		}
	}
	priorCore := coreGuideSections(items(previous["sections"]))
	if textValue(previous["editorial_core_hash"]) == guideCoreFingerprint(base) && len(preserved) > 0 && guideParagraphCount(preserved) == guideParagraphCount(priorCore) {
		base = []document{}
	} else if !guideWithinLimits(base) {
		compact, err := curateGuideSections(note, items(note["sections"]), model)
		if err != nil {
			return nil, err
		}
		base = coreGuideSections(compact)
	}
	base = mergeSubjectSections(append(base, preserved...))
	refreshGuideEvidence(base, items(note["conflicts"]), active, sources)
	for len(answers) > 0 {
		count := 1
		for count < len(answers) && count < guideCandidateLimit {
			if jsonCharacterLength(answerIntegrationPayload(note, base, answers[:count+1])) > guideCandidateCharacters {
				break
			}
			count++
		}
		var err error
		base, err = placeGuideAnswers(note, base, answers[:count], vault, model)
		if err != nil {
			return nil, err
		}
		answers = answers[count:]
	}
	return base, nil
}

func answerIntegrationPayload(note document, base, answers []document) document {
	sections := []document{}
	for i, section := range base {
		paragraphs := []document{}
		for _, p := range items(section["paragraphs"]) {
			paragraphs = append(paragraphs, choose(p, "text", "partial", "evidence_basis"))
		}
		sections = append(sections, document{"id": fmt.Sprintf("S%d", i), "heading": section["heading"], "paragraphs": paragraphs})
	}
	proposed := []document{}
	for i, answer := range answers {
		proposed = append(proposed, merge(choose(answer, "heading", "text", "partial", "evidence_basis"), document{"id": fmt.Sprintf("A%d", i)}))
	}
	return document{"integration_policy": answerIntegrationPolicy, "subject": choose(note, "id", "title", "scope", "folder"), "existing_sections": sections, "answers": proposed}
}

func placeGuideAnswers(note document, base, answers []document, vault document, model compilerModel) ([]document, error) {
	payload := answerIntegrationPayload(note, base, answers)
	paragraphs := len(answers)
	for _, section := range base {
		paragraphs += len(items(section["paragraphs"]))
	}
	if paragraphs > guideCandidateLimit || jsonCharacterLength(payload) > guideCandidateCharacters {
		return nil, errors.New("knowledge integration input exceeds safe review bounds; prior publication retained")
	}
	proposed := items(payload["answers"])
	response, err := model.call("integrate_answers", payload)
	if err != nil {
		return nil, err
	}
	bySection := map[string]int{}
	byHeading := map[string]int{}
	for i, section := range base {
		bySection[fmt.Sprintf("S%d", i)] = i
		byHeading[foldText(strings.TrimSpace(textValue(section["heading"])))] = i
	}
	byAnswer := map[string]document{}
	for i, answer := range proposed {
		byAnswer[textValue(answer["id"])] = answers[i]
	}
	needsMerge := false
	for _, decision := range items(response["decisions"]) {
		action := textValue(decision["action"])
		if action == "covered" || action == "evidence_only" {
			continue
		}
		needsMerge = needsMerge || action == "integrate"
		id := textValue(decision["id"])
		paragraph := clone(byAnswer[id])
		delete(paragraph, "heading")
		target := bySection[textValue(decision["section"])]
		if action == "new_section" {
			heading := strings.TrimSpace(textValue(decision["heading"]))
			key := foldText(heading)
			var found bool
			target, found = byHeading[key]
			if !found {
				target = len(base)
				base = append(base, document{"heading": heading, "paragraphs": []document{}})
				byHeading[key] = target
			}
		}
		base[target]["paragraphs"] = append(items(base[target]["paragraphs"]), paragraph)
	}
	if needsMerge || !guideWithinLimits(base) {
		return mergeIntegratedGuide(note, base, vault, model)
	}
	return base, nil
}

func coreGuideSections(sections []document) []document {
	result := []document{}
	for _, section := range sections {
		paragraphs := []document{}
		for _, p := range items(section["paragraphs"]) {
			if textValue(p["derivation"]) != "gap_answer" {
				paragraphs = append(paragraphs, p)
			}
		}
		if len(paragraphs) > 0 {
			result = append(result, merge(clone(section), document{"paragraphs": paragraphs}))
		}
	}
	return result
}

func guideCoreFingerprint(sections []document) string {
	return identity(answerIntegrationPayload(document{}, coreGuideSections(sections), nil)["existing_sections"])
}

func guideParagraphCount(sections []document) int {
	count := 0
	for _, section := range sections {
		count += len(items(section["paragraphs"]))
	}
	return count
}

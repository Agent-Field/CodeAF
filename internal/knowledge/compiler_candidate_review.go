package knowledge

import "errors"

func validateAdditionReview(payload, response document) error {
	allowed, seen := index(items(payload["answers"])), map[string]bool{}
	for _, check := range items(response["checks"]) {
		if _, ok := check["useful"].(bool); !ok {
			return errors.New("knowledge addition review requires an explicit usefulness judgement")
		}
		id := textValue(check["id"])
		if allowed[id] == nil || seen[id] {
			return errors.New("knowledge addition review must inspect every candidate exactly once")
		}
		seen[id] = true
	}
	if len(seen) != len(allowed) {
		return errors.New("knowledge addition review omitted a candidate")
	}
	return nil
}

// Candidate admission judges the specific concept, not merely its project.
// Rejected draft material remains in canonical source evidence.
func reviewGuideAdditions(note document, base, answers []document, model compilerModel) ([]document, error) {
	payload := answerIntegrationPayload(note, base, answers)
	delete(payload, "integration_policy")
	payload["admission_policy"] = "concept-additions-1"
	response, err := model.call("review_additions", payload)
	if err != nil {
		return nil, err
	}
	checks := index(items(response["checks"]))
	result := []document{}
	for i, raw := range items(payload["answers"]) {
		if flag(checks[textValue(raw["id"])]["useful"]) {
			result = append(result, answers[i])
		}
	}
	return result, nil
}

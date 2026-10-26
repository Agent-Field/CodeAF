package knowledge

import "errors"

const discoveryCatalogLimit = 128

// Discovery searches every existing concept before proposing new pages. Large
// catalogs are inspected in bounded reuse-only windows rather than truncated.
func discoverSubjects(catalog, subjects []document, model compilerModel) (document, error) {
	overrides, err := readOverrides(model.db)
	if err != nil {
		return nil, err
	}
	existing := []document{}
	for _, subject := range subjects {
		entry := choose(subject, "id", "title", "scope", "aliases", "folder", "purpose")
		outline := []string{}
		for _, section := range items(subject["sections"]) {
			outline = append(outline, textValue(section["heading"]))
		}
		entry["headings"] = outline
		entry["claim_count"] = len(stringsValue(subject["claims"]))
		if override, ok := overrides[textValue(subject["id"])]; ok {
			entry["human_edited"] = !override.Deleted
			entry["deleted"] = override.Deleted
		}
		existing = append(existing, entry)
	}
	if len(existing) <= discoveryCatalogLimit {
		return reviewSubjectPlan(catalog, existing, false, model)
	}
	remaining := append([]document{}, catalog...)
	assignments := []document{}
	for start := 0; start < len(existing) && len(remaining) > 0; start += discoveryCatalogLimit {
		batch := existing[start:min(start+discoveryCatalogLimit, len(existing))]
		response, err := model.call("discover", document{"catalog": remaining, "existing_subjects": batch, "reuse_only": true})
		if err != nil {
			return nil, err
		}
		normalizeSubjectReferences(response)
		allowed, notes := index(batch), index(remaining)
		assigned := map[string]bool{}
		for _, raw := range items(response["subjects"]) {
			if allowed[textValue(raw["id"])] == nil {
				return nil, errors.New("knowledge reuse-only discovery must reference an inspected existing subject")
			}
			for _, id := range stringsValue(raw["notes"]) {
				if notes[id] == nil {
					return nil, errors.New("knowledge discovery must reference an inspected note")
				}
				assigned[id] = true
			}
			assignments = append(assignments, raw)
		}
		unmatched := []document{}
		for _, n := range remaining {
			if !assigned[textValue(n["id"])] {
				unmatched = append(unmatched, n)
			}
		}
		remaining = unmatched
	}
	if len(remaining) > 0 {
		response, err := reviewSubjectPlan(remaining, nil, true, model)
		if err != nil {
			return nil, err
		}
		assignments = append(assignments, items(response["subjects"])...)
	}
	return document{"subjects": assignments}, nil
}

func reviewSubjectPlan(catalog, existing []document, searched bool, model compilerModel) (document, error) {
	payload := document{"catalog": catalog, "existing_subjects": existing, "existing_catalog_searched": searched}
	proposal, err := model.call("discover", payload)
	if err != nil {
		return nil, err
	}
	normalizeSubjectReferences(proposal)
	newPages := 0
	for _, subject := range items(proposal["subjects"]) {
		if textValue(subject["id"]) == "" {
			newPages++
		}
	}
	if newPages < 2 {
		return proposal, nil
	}
	payload["proposed_subjects"] = items(proposal["subjects"])
	reviewed, err := model.call("organize_subjects", payload)
	if err != nil {
		return nil, err
	}
	normalizeSubjectReferences(reviewed)
	before, after := []string{}, []string{}
	allowed := index(catalog)
	for _, subject := range items(proposal["subjects"]) {
		before = append(before, stringsValue(subject["notes"])...)
	}
	for _, subject := range items(reviewed["subjects"]) {
		for _, id := range stringsValue(subject["notes"]) {
			if allowed[id] == nil {
				return nil, errors.New("knowledge organization plan referenced an uninspected note")
			}
			after = append(after, id)
		}
	}
	if identity(sortedUnique(before)) != identity(sortedUnique(after)) {
		return nil, errors.New("knowledge organization review must preserve planned evidence coverage")
	}
	return reviewed, nil
}

func normalizeSubjectReferences(response document) {
	for _, subject := range items(response["subjects"]) {
		subject["id"] = textValue(subject["existing_subject_id"])
	}
}

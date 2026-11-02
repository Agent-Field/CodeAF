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
	assignments := []document{}
	bySubject := map[string]document{}
	assigned := map[string]bool{}
	for start := 0; start < len(existing); start += discoveryCatalogLimit {
		batch := existing[start:min(start+discoveryCatalogLimit, len(existing))]
		response, err := model.call("discover", document{"catalog": catalog, "existing_subjects": batch, "reuse_only": true})
		if err != nil {
			return nil, err
		}
		normalizeSubjectReferences(response)
		allowed, notes := index(batch), index(catalog)
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
			id := textValue(raw["id"])
			if prior := bySubject[id]; prior != nil {
				prior["notes"] = sortedUnique(append(stringsValue(prior["notes"]), stringsValue(raw["notes"])...))
			} else {
				entry := clone(raw)
				entry["notes"] = sortedUnique(stringsValue(raw["notes"]))
				bySubject[id] = entry
				assignments = append(assignments, entry)
			}
		}
	}
	remaining := []document{}
	for _, note := range catalog {
		if !assigned[textValue(note["id"])] {
			remaining = append(remaining, note)
		}
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
	return reviewed, nil
}

func normalizeSubjectReferences(response document) {
	for _, subject := range items(response["subjects"]) {
		subject["id"] = textValue(subject["existing_subject_id"])
	}
}

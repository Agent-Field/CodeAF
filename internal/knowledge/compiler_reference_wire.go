package knowledge

import (
	"errors"
	"fmt"
)

type wireReferences struct {
	toWire, toCanonical map[string]string
}

func aliasReferences(entries []document, prefix string) wireReferences {
	refs := wireReferences{toWire: map[string]string{}, toCanonical: map[string]string{}}
	for i, entry := range entries {
		canonical := textValue(entry["id"])
		alias := fmt.Sprintf("%s%d", prefix, i)
		refs.toWire[canonical] = alias
		refs.toCanonical[alias] = canonical
		entry["id"] = alias
	}
	return refs
}

type compilerReferenceWire struct {
	payload      document
	notes, pages wireReferences
	organization bool
}

// Request-local reference types keep evidence notes distinct from existing pages.
// Canonical references remain the authority for cache keys and stored receipts.
func newCompilerReferenceWire(role string, payload document) (compilerReferenceWire, error) {
	wire := compilerReferenceWire{payload: payload}
	if role != "discover" && role != "organize_subjects" {
		return wire, nil
	}
	wire.organization = true
	wire.payload = clone(payload)
	wire.notes = aliasReferences(items(wire.payload["catalog"]), "N")
	wire.pages = aliasReferences(items(wire.payload["existing_subjects"]), "P")
	wire.payload["reference_types"] = document{"notes": "N references from catalog.id", "existing_subject_id": "P references from existing_subjects.id, or empty for a new page"}
	for _, subject := range items(wire.payload["proposed_subjects"]) {
		delete(subject, "id")
		if target := textValue(subject["existing_subject_id"]); target != "" {
			alias, ok := wire.pages.toWire[target]
			if !ok {
				return wire, errors.New("knowledge proposal page reference is outside the inspected catalog")
			}
			subject["existing_subject_id"] = alias
		}
		notes := []string{}
		for _, id := range stringsValue(subject["notes"]) {
			alias, ok := wire.notes.toWire[id]
			if !ok {
				return wire, errors.New("knowledge proposal note reference is outside the inspected catalog")
			}
			notes = append(notes, alias)
		}
		subject["notes"] = notes
	}
	return wire, nil
}

func (wire compilerReferenceWire) decode(result document) error {
	if !wire.organization {
		return nil
	}
	for _, subject := range items(result["subjects"]) {
		delete(subject, "id")
		if target := textValue(subject["existing_subject_id"]); target != "" {
			canonical, ok := wire.pages.toCanonical[target]
			if !ok {
				return errors.New("knowledge organization referenced an uninspected existing page; expected a P reference")
			}
			subject["existing_subject_id"] = canonical
		}
		notes := []string{}
		for _, id := range stringsValue(subject["notes"]) {
			canonical, ok := wire.notes.toCanonical[id]
			if !ok {
				return errors.New("knowledge organization referenced an uninspected evidence note; expected an N reference")
			}
			notes = append(notes, canonical)
		}
		subject["notes"] = notes
	}
	return nil
}

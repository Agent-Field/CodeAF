package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

func curatePublishedGuides(draft document, model compilerModel) (document, error) {
	overrides, err := readOverrides(model.db)
	if err != nil {
		return nil, err
	}
	notes := append([]document{}, items(draft["notes"])...)
	indices := []int{}
	for i, note := range notes {
		if _, overridden := overrides[textValue(note["id"])]; overridden {
			continue
		}
		if textValue(note["kind"]) == "subject" && (!guideWithinLimits(items(note["sections"])) || hasGuideAnswers(items(note["sections"]))) {
			indices = append(indices, i)
		}
	}
	ctx, cancel := context.WithCancel(model.ctx)
	defer cancel()
	type selectionResult struct {
		note document
		err  error
	}
	results := make([]selectionResult, len(indices))
	jobs := make(chan int, len(indices))
	for i := range indices {
		jobs <- i
	}
	close(jobs)
	var workers sync.WaitGroup
	for worker := 0; worker < min(subjectConcurrency, len(indices)); worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			workerModel := model
			workerModel.ctx = ctx
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				note := clone(notes[indices[i]])
				sections, err := integrateGuideAnswers(note, draft, workerModel)
				if err == nil {
					sections, err = curateGuideSections(note, sections, workerModel)
				}
				if err != nil {
					results[i].err = err
					cancel()
					return
				}
				note["sections"] = sections
				if len(sections) == 0 {
					note["learning_state"] = "needs_evidence"
				}
				results[i].note = note
			}
		}()
	}
	workers.Wait()
	for _, result := range results {
		if result.err != nil && !errors.Is(result.err, context.Canceled) {
			return nil, result.err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i, result := range results {
		notes[indices[i]] = result.note
	}
	return merge(draft, document{"notes": notes}), nil
}

const (
	guideMaxSections         = 8
	guideMaxParagraphs       = 12
	guideMaxWords            = 900
	guideCandidateLimit      = 48
	guideCandidateCharacters = 24000
)

type guideCandidate struct {
	id, heading string
	paragraph   document
}

func validateGuideSelection(payload, response document) error {
	allowed := index(items(payload["paragraphs"]))
	sections := items(response["sections"])
	seen := map[string]bool{}
	for _, section := range sections {
		heading := strings.TrimSpace(textValue(section["heading"]))
		ids := stringsValue(section["paragraphs"])
		if heading == "" || len(ids) == 0 {
			return errors.New("knowledge guide selection requires a heading and selected paragraphs")
		}
		for _, id := range ids {
			if allowed[id] == nil || seen[id] {
				return errors.New("knowledge guide selection must use distinct inspected paragraph IDs")
			}
			seen[id] = true
		}
	}
	return nil
}

func guideWithinLimits(sections []document) bool {
	paragraphs, words := 0, 0
	for _, section := range sections {
		words += len(strings.Fields(textValue(section["heading"])))
		for _, paragraph := range items(section["paragraphs"]) {
			paragraphs++
			words += len(strings.Fields(textValue(paragraph["text"])))
		}
	}
	return len(sections) <= guideMaxSections && paragraphs <= guideMaxParagraphs && words <= guideMaxWords
}

func guidePayload(subject document, candidates []guideCandidate) document {
	paragraphs := []document{}
	for _, candidate := range candidates {
		paragraphs = append(paragraphs, document{"id": candidate.id, "heading": candidate.heading, "text": candidate.paragraph["text"], "word_count": len(strings.Fields(textValue(candidate.paragraph["text"])))})
	}
	return document{"subject": choose(subject, "id", "title", "scope", "purpose"), "paragraphs": paragraphs, "max_paragraphs": guideMaxParagraphs}
}

func guideSections(candidates []guideCandidate) []document {
	sections := []document{}
	for _, candidate := range candidates {
		sections = append(sections, document{"heading": candidate.heading, "paragraphs": []document{clone(candidate.paragraph)}})
	}
	return mergeSubjectSections(sections)
}

func selectGuideCandidates(subject document, candidates []guideCandidate, model compilerModel, maxParagraphs int) ([]guideCandidate, error) {
	payload := guidePayload(subject, candidates)
	payload["max_paragraphs"] = maxParagraphs
	response, err := model.call("curate_guide", payload)
	if err != nil {
		return nil, err
	}
	byID := map[string]guideCandidate{}
	for _, candidate := range candidates {
		byID[candidate.id] = candidate
	}
	selected := []guideCandidate{}
	limit := max(0, min(guideMaxParagraphs, maxParagraphs))
	headings := map[string]bool{}
	words := 0
	for _, section := range items(response["sections"]) {
		heading := strings.TrimSpace(textValue(section["heading"]))
		key := strings.ToLower(heading)
		for _, id := range stringsValue(section["paragraphs"]) {
			candidate := byID[id]
			addedWords := len(strings.Fields(textValue(candidate.paragraph["text"])))
			if !headings[key] {
				addedWords += len(strings.Fields(heading))
			}
			if len(selected) >= limit || (!headings[key] && len(headings) >= guideMaxSections) || words+addedWords > guideMaxWords {
				continue
			}
			candidate.heading = heading
			selected = append(selected, candidate)
			headings[key] = true
			words += addedWords
		}
	}
	return selected, nil
}

func curateGuideSections(subject document, sections []document, model compilerModel) ([]document, error) {
	if guideWithinLimits(sections) {
		return sections, nil
	}
	candidates := []guideCandidate{}
	for _, section := range sections {
		for _, paragraph := range items(section["paragraphs"]) {
			candidates = append(candidates, guideCandidate{id: fmt.Sprintf("P%d", len(candidates)), heading: textValue(section["heading"]), paragraph: paragraph})
		}
	}
	for {
		batches := [][]guideCandidate{}
		current := []guideCandidate{}
		for _, candidate := range candidates {
			next := append(append([]guideCandidate{}, current...), candidate)
			if len(current) > 0 && (len(next) > guideCandidateLimit || jsonCharacterLength(guidePayload(subject, next)) > guideCandidateCharacters) {
				batches = append(batches, current)
				current = []guideCandidate{}
			}
			current = append(current, candidate)
		}
		if len(current) > 0 {
			batches = append(batches, current)
		}
		if len(batches) == 1 {
			selected, err := selectGuideCandidates(subject, batches[0], model, guideMaxParagraphs)
			if err != nil {
				return nil, err
			}
			return guideSections(selected), nil
		}
		reduced := []guideCandidate{}
		for _, batch := range batches {
			selected, err := selectGuideCandidates(subject, batch, model, min(guideMaxParagraphs, len(batch)-1))
			if err != nil {
				return nil, err
			}
			reduced = append(reduced, selected...)
		}
		if len(reduced) == 0 {
			return []document{}, nil
		}
		candidates = reduced
	}
}

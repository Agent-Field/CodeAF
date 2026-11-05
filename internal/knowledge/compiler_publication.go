package knowledge

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

func applyNativeOverrides(vault document, overrides map[string]pageOverride) document {
	if len(overrides) == 0 {
		return vault
	}
	claims := index(items(vault["claims"]))
	kept := []document{}
	suppressed := map[string]bool{}
	for _, note := range items(vault["notes"]) {
		if change, ok := overrides[textValue(note["id"])]; ok {
			original := stringsValue(note["original_claims"])
			if note["original_claims"] == nil {
				original = stringsValue(note["claims"])
			}
			for _, id := range original {
				suppressed[id] = true
			}
			if change.Deleted {
				continue
			}
			evidence := items(note["original_evidence"])
			for _, id := range stringsValue(note["claims"]) {
				evidence = unionEvidence(evidence, items(claims[id]["evidence"]))
			}
			for _, s := range items(note["sections"]) {
				for _, p := range items(s["paragraphs"]) {
					evidence = unionEvidence(evidence, items(p["evidence"]))
				}
			}
			note["original_evidence"] = evidence
			note["original_claims"] = original
			note["human_edited"] = true
			note["human_edited_at"] = change.UpdatedAt
			note["manual_markdown"] = change.Markdown
			note["claims"] = []string{}
			note["links"] = []document{}
			note["backlinks"] = []document{}
			note["needs_editorial_update"] = false
			note["learning_state"] = "human_edited"
			note["sections"] = []document{{"heading": "", "paragraphs": []document{{"text": change.Markdown, "claims": []string{}, "evidence": []document{}, "derivation": "human_edit"}}}}
			for _, key := range []string{"questions", "conflicts", "composition", "article_rejections"} {
				delete(note, key)
			}
		}
		kept = append(kept, note)
	}
	visible := index(kept)
	refs := map[string]bool{}
	for _, note := range kept {
		for _, id := range stringsValue(note["claims"]) {
			refs[id] = true
		}
		for _, section := range items(note["sections"]) {
			for _, p := range items(section["paragraphs"]) {
				for _, id := range stringsValue(p["claims"]) {
					refs[id] = true
				}
				p["inline_links"] = visibleDocumentLinks(items(p["inline_links"]), visible)
			}
		}
		note["links"] = visibleDocumentLinks(items(note["links"]), visible)
		note["backlinks"] = visibleDocumentLinks(items(note["backlinks"]), visible)
	}
	keptClaims := []document{}
	for _, c := range items(vault["claims"]) {
		id := textValue(c["id"])
		if !suppressed[id] || refs[id] {
			keptClaims = append(keptClaims, c)
		}
	}
	remaining := index(keptClaims)
	relations := []document{}
	for _, r := range items(vault["relations"]) {
		if remaining[textValue(r["from"])] != nil && remaining[textValue(r["to"])] != nil {
			relations = append(relations, r)
		}
	}
	vault["notes"] = kept
	vault["claims"] = keptClaims
	vault["relations"] = relations
	return vault
}
func unionEvidence(left, right []document) []document {
	seen := map[string]bool{}
	result := []document{}
	for _, e := range append(append([]document{}, left...), right...) {
		key := textValue(e["source"]) + "\x00" + textValue(e["quote"])
		if !seen[key] {
			seen[key] = true
			result = append(result, e)
		}
	}
	return result
}
func visibleDocumentLinks(links []document, visible map[string]document) []document {
	kept := []document{}
	for _, l := range links {
		if visible[textValue(l["id"])] != nil {
			kept = append(kept, l)
		}
	}
	return kept
}

var protectedMarkup = regexp.MustCompile("(?s)```.*?```|~~~.*?~~~|`+[^`]*`+|\\[\\[[^\\]]*\\]\\]|!?\\[[^\\]]*\\]\\([^)]*\\)|<[^>]*>|https?://\\S+")

func linkCompilation(vault document) {
	notes := items(vault["notes"])
	byID := index(notes)
	labels := map[string]string{}
	ambiguous := map[string]bool{}
	for _, n := range notes {
		for _, label := range append([]string{textValue(n["title"])}, stringsValue(n["aliases"])...) {
			label = strings.TrimSpace(label)
			if label == "" {
				continue
			}
			key := strings.ToLower(label)
			id := textValue(n["id"])
			if prior := labels[key]; prior != "" && prior != id {
				ambiguous[key] = true
			}
			labels[key] = id
		}
	}
	spellings := []string{}
	for label := range labels {
		if !ambiguous[label] {
			spellings = append(spellings, label)
		}
	}
	sort.Slice(spellings, func(i, j int) bool {
		if len(spellings[i]) != len(spellings[j]) {
			return len(spellings[i]) > len(spellings[j])
		}
		return spellings[i] < spellings[j]
	})
	incoming := map[string][]string{}
	for _, note := range notes {
		id := textValue(note["id"])
		edges := []document{}
		for _, edge := range items(note["links"]) {
			if textValue(edge["kind"]) != "mention" {
				edges = append(edges, edge)
			}
		}
		targets := []string{}
		for _, s := range items(note["sections"]) {
			for _, p := range items(s["paragraphs"]) {
				text := textValue(p["text"])
				protected := protectedMarkup.FindAllStringIndex(text, -1)
				spans := []document{}
				for _, span := range protected {
					token := text[span[0]:span[1]]
					if !strings.HasPrefix(token, "[[") || !strings.HasSuffix(token, "]]") {
						continue
					}
					target, display, hasDisplay := strings.Cut(token[2:len(token)-2], "|")
					target = strings.TrimSpace(target)
					label := target
					if hasDisplay {
						label = display
					}
					if byID[target] == nil {
						key := strings.ToLower(target)
						if ambiguous[key] {
							continue
						}
						target = labels[key]
					}
					if target != "" && target != id {
						spans = append(spans, document{"start": utf8.RuneCountInString(text[:span[0]]), "end": utf8.RuneCountInString(text[:span[1]]), "id": target, "label": label})
						targets = append(targets, target)
					}
				}
				// Rune scanning avoids byte-offset errors on non-ASCII source text. Longest
				// matching unambiguous title wins; existing markup stays opaque.
				runes := []rune(text)
				byteOffset := 0
				for pos := 0; pos < len(runes); {
					protectedEnd := 0
					for _, span := range protected {
						if byteOffset >= span[0] && byteOffset < span[1] {
							protectedEnd = span[1]
							break
						}
					}
					if protectedEnd > 0 {
						advance := utf8.RuneCountInString(text[byteOffset:protectedEnd])
						pos += advance
						byteOffset = protectedEnd
						continue
					}
					matched := 0
					for _, label := range spellings {
						labelRunes := []rune(label)
						end := pos + len(labelRunes)
						if end > len(runes) || labels[label] == id {
							continue
						}
						if pos > 0 && isWord(runes[pos-1]) || end < len(runes) && isWord(runes[end]) {
							continue
						}
						if !strings.EqualFold(string(runes[pos:end]), label) {
							continue
						}
						overlap := false
						byteEnd := byteOffset + len(string(runes[pos:end]))
						for _, span := range protected {
							if byteOffset < span[1] && byteEnd > span[0] {
								overlap = true
								break
							}
						}
						if overlap {
							continue
						}
						spans = append(spans, document{"start": pos, "end": end, "id": labels[label]})
						targets = append(targets, labels[label])
						matched = len(labelRunes)
						break
					}
					if matched == 0 {
						matched = 1
					}
					byteOffset += len(string(runes[pos : pos+matched]))
					pos += matched
				}
				sort.Slice(spans, func(i, j int) bool { return number(spans[i]["start"]) < number(spans[j]["start"]) })
				p["inline_links"] = spans
			}
		}
		for _, target := range sortedUnique(targets) {
			incoming[target] = append(incoming[target], id)
			exists := false
			for _, edge := range edges {
				if textValue(edge["id"]) == target {
					exists = true
					break
				}
			}
			if !exists {
				edges = append(edges, document{"id": target, "kind": "mention", "reason": "Mentioned in the article; navigation only"})
			}
		}
		note["links"] = edges
	}
	for _, n := range notes {
		backlinks := []document{}
		for _, id := range sortedUnique(incoming[textValue(n["id"])]) {
			backlinks = append(backlinks, document{"id": id, "kind": "mention"})
		}
		n["backlinks"] = backlinks
	}
}
func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' }

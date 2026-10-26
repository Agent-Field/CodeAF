package knowledge

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var compilerWords = regexp.MustCompile(`[\pL\pN_.-]+`)
var operationalCall = regexp.MustCompile(`(?s)^\[tool call: (read_file|read|use_skill|ls|view_image)\((.*)\)\]$`)

func words(text string) map[string]bool {
	result := map[string]bool{}
	for _, s := range compilerWords.FindAllString(strings.ToLower(text), -1) {
		result[s] = true
	}
	return result
}
func meaningfulWords(text string) map[string]bool {
	result := words(text)
	for _, word := range strings.Fields("the and for with that this from are was were has have not only into then than its you your our use uses used agent reported") {
		delete(result, word)
	}
	return result
}
func publicSource(source document) document {
	public := merge(source, nil)
	public["provenance"] = choose(object(source["provenance"]), "kind", "content_hash", "recorded_at", "valid_at", "locator", "revision", "independently_verified")
	return public
}
func operationalSource(source document) bool {
	if textValue(source["role"]) != "agent" {
		return false
	}
	match := operationalCall.FindStringSubmatch(textValue(source["text"]))
	if match == nil {
		return false
	}
	var args document
	if decodeDocument([]byte(match[2]), &args) != nil {
		return false
	}
	allowed := map[string][]string{"read_file": {"path", "offset", "limit"}, "read": {"path", "offset", "limit"}, "use_skill": {"mode", "name"}, "ls": {"path"}, "view_image": {"path"}}
	for key := range args {
		if !contains(allowed[match[1]], key) {
			return false
		}
	}
	return true
}
func compactContext(previous document, messages []document) ([]document, []document, []document) {
	known := object(previous["sources"])
	sessions := map[string]bool{}
	var combined strings.Builder
	for _, s := range messages {
		sessions[textValue(s["session"])] = true
		combined.WriteString(textValue(s["text"]))
		combined.WriteByte(' ')
	}
	terms := meaningfulWords(combined.String())
	claims := items(previous["claims"])
	frequency := map[string]int{}
	for _, c := range claims {
		for term := range meaningfulWords(textValue(c["text"])) {
			frequency[term]++
		}
	}
	type candidate struct {
		claim document
		same  bool
		score float64
	}
	ranked := []candidate{}
	for _, c := range claims {
		if textValue(c["status"]) != "active" {
			continue
		}
		same := false
		for _, e := range items(c["evidence"]) {
			if sessions[textValue(object(known[textValue(e["source"])])["session"])] {
				same = true
			}
		}
		score := float64(0)
		for term := range meaningfulWords(textValue(c["text"])) {
			if terms[term] {
				score += math.Log(1 + float64(len(claims)+1)/float64(frequency[term]+1))
			}
		}
		if same || score > 0 {
			ranked = append(ranked, candidate{c, same, score})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].same != ranked[j].same {
			return ranked[i].same
		}
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return textValue(ranked[i].claim["id"]) < textValue(ranked[j].claim["id"])
	})
	compact := []document{}
	selected := map[string]bool{}
	scopes := map[string]bool{}
	for _, candidate := range ranked {
		c := candidate.claim
		item := choose(c, "id", "text", "scope", "key", "kind", "status")
		contexts := map[string][]document{}
		for _, e := range items(c["evidence"]) {
			if source, ok := known[textValue(e["source"])]; ok {
				s := object(source)
				role := textValue(s["role"])
				contexts[role] = append(contexts[role], choose(s, "role", "ts"))
			}
		}
		summary := []document{}
		roles := []string{}
		for role := range contexts {
			roles = append(roles, role)
		}
		sort.Strings(roles)
		for _, role := range roles {
			group := contexts[role]
			sort.Slice(group, func(i, j int) bool { return textValue(group[i]["ts"]) < textValue(group[j]["ts"]) })
			summary = append(summary, group[0])
			if len(group) > 1 {
				summary = append(summary, group[len(group)-1])
			}
		}
		item["source_context"] = summary
		if len(compact) < 24 && len(mustJSON(append(append([]document{}, compact...), item))) <= 8000 {
			compact = append(compact, item)
			selected[textValue(c["id"])] = true
			scopes[textValue(c["scope"])] = true
		}
	}
	notes := []document{}
	for _, note := range items(previous["notes"]) {
		refs := []string{}
		for _, id := range stringsValue(note["claims"]) {
			if selected[id] {
				refs = append(refs, id)
			}
		}
		if len(refs) == 0 {
			continue
		}
		item := choose(note, "id", "title", "scope", "folder")
		item["claims"] = refs
		item["claim_count"] = len(stringsValue(note["claims"]))
		item["membership_patch"] = "append; host retains all existing members"
		if len(notes) < 8 && len(mustJSON(append(append([]document{}, notes...), item))) <= 4000 {
			notes = append(notes, item)
		}
	}
	aliases := []document{}
	for _, a := range items(previous["scope_aliases"]) {
		if !scopes[textValue(a["from"])] && !scopes[textValue(a["to"])] {
			continue
		}
		item := choose(a, "from", "to")
		if len(mustJSON(append(append([]document{}, aliases...), item))) <= 1500 {
			aliases = append(aliases, item)
		}
	}
	return compact, notes, aliases
}
func updateCompilation(messages []document, previous document, revisited []string, model compilerModel) (document, error) {
	vault := clone(previous)
	sources := object(vault["sources"])
	supplied := index(messages)
	for _, id := range revisited {
		if supplied[id] == nil {
			return nil, errors.New("revisited IDs must be supplied as original immutable sources")
		}
	}
	windows, err := compilerSourceWindows(model, revisited)
	if err != nil {
		return nil, err
	}
	fresh := []document{}
	shown := map[string]string{}
	newCount := 0
	size := 0
	for _, source := range messages {
		id := textValue(source["id"])
		if old, ok := sources[id]; ok && identity(old) != identity(source) {
			return nil, errors.New("immutable knowledge source changed")
		}
		if _, ok := sources[id]; !ok {
			newCount++
		} else if !contains(revisited, id) {
			continue
		}
		sources[id] = source
		if operationalSource(source) {
			continue
		}
		public := publicSource(source)
		if window := windows[id]; window != nil {
			start, length := int(number(window["start"])), int(number(window["length"]))
			if start < 0 || length < 1 || length > 24000 || start+length > runeLength(textValue(source["text"])) {
				return nil, errors.New("invalid exact source window")
			}
			public["text"] = textSpan(textValue(source["text"]), start, length)
			public["source_window"] = document{"start": start, "length": length, "original_characters": runeLength(textValue(source["text"]))}
		}
		fresh = append(fresh, public)
		shown[id] = textValue(public["text"])
		size += utf8.RuneCountInString(textValue(public["text"]))
	}
	vault["sources"] = sources
	if len(fresh) == 0 {
		return vault, nil
	}
	if size > 24000 {
		return nil, errors.New("knowledge compiler context exceeds 24,000 characters")
	}
	compact, notes, aliases := compactContext(previous, fresh)
	hints, hintReceipt, err := compilerProposalHints(model, fresh)
	if err != nil {
		return nil, err
	}
	result, err := model.call("update", document{"messages": fresh, "existing_claims": compact, "existing_notes": notes, "aliases": aliases, "proposals": hints})
	if err != nil {
		return nil, err
	}
	claims := items(vault["claims"])
	rejected := items(vault["rejected"])
	references := map[string]string{}
	for _, raw := range items(result["claims"]) {
		valid := true
		for _, key := range []string{"text", "scope", "topic", "kind"} {
			if strings.TrimSpace(textValue(raw[key])) == "" {
				valid = false
			}
		}
		if !valid || !contains([]string{"fact", "preference", "decision", "proposal", "reported"}, textValue(raw["kind"])) {
			rejected = append(rejected, document{"claim": raw, "reason": "invalid claim schema or kind"})
			continue
		}
		evidence := items(raw["evidence"])
		if len(evidence) == 0 {
			valid = false
		}
		for _, e := range evidence {
			quote := textValue(e["quote"])
			if quote == "" || !strings.Contains(shown[textValue(e["source"])], quote) {
				valid = false
			}
		}
		if !valid {
			rejected = append(rejected, document{"claim": raw, "reason": "Quote outside supplied compiler source context"})
			continue
		}
		claim := clone(raw)
		id := identity([]any{raw["text"], raw["evidence"]})
		references["new:"+textValue(raw["local_id"])] = id
		human := false
		enriched := []document{}
		for _, e := range evidence {
			source := object(sources[textValue(e["source"])])
			text := textValue(source["text"])
			quote := textValue(e["quote"])
			start := strings.Index(text, quote)
			item := merge(e, document{"span_start": utf8.RuneCountInString(text[:start]), "span_end": utf8.RuneCountInString(text[:start]) + utf8.RuneCountInString(quote)})
			enriched = append(enriched, item)
			if textValue(source["role"]) == "user" {
				human = true
			}
		}
		if !human {
			claim["kind"] = "reported"
			if !strings.HasPrefix(textValue(claim["text"]), "The agent reported") {
				claim["text"] = "The agent reported: " + textValue(claim["text"])
			}
		}
		duplicate := false
		for _, old := range claims {
			if old["scope"] == claim["scope"] && old["kind"] == claim["kind"] && strings.EqualFold(textValue(old["text"]), textValue(claim["text"])) {
				if textValue(old["status"]) == "active" {
					old["evidence"] = unionDocuments(items(old["evidence"]), enriched)
					references["new:"+textValue(raw["local_id"])] = textValue(old["id"])
					duplicate = true
					break
				}
				covered := true
				for _, e := range evidence {
					found := false
					for _, prior := range items(old["evidence"]) {
						if e["source"] == prior["source"] && e["quote"] == prior["quote"] {
							found = true
							break
						}
					}
					if !found {
						covered = false
					}
				}
				if covered {
					duplicate = true
					break
				}
			}
		}
		if duplicate {
			continue
		}
		claim["id"] = id
		claim["status"] = "active"
		claim["evidence"] = enriched
		claims = append(claims, claim)
	}
	aliasTargets := map[string]bool{}
	labels := map[string]bool{}
	for _, c := range claims {
		labels[textValue(c["scope"])] = true
	}
	for _, a := range items(result["aliases"]) {
		aliasTargets[textValue(a["to"])] = true
	}
	savedAliases := items(vault["scope_aliases"])
	mapping := map[string]string{}
	for _, a := range items(result["aliases"]) {
		from, to := textValue(a["from"]), textValue(a["to"])
		if labels[from] && labels[to] && from != to && !aliasTargets[from] && from != "Personal" && to != "Personal" {
			mapping[from] = to
			savedAliases = unionDocuments(savedAliases, []document{a})
		}
	}
	for _, c := range claims {
		if to, ok := mapping[textValue(c["scope"])]; ok {
			if _, ok = c["original_scope"]; !ok {
				c["original_scope"] = c["scope"]
			}
			c["scope"] = to
		}
	}
	byID := index(claims)
	relations := items(vault["relations"])
	resolve := func(ref any) string {
		s := textValue(ref)
		if id, ok := references[s]; ok {
			return id
		}
		return s
	}
	for _, r := range items(result["relations"]) {
		relation := merge(r, document{"from": resolve(r["from"]), "to": resolve(r["to"])})
		new, old := byID[textValue(relation["from"])], byID[textValue(relation["to"])]
		if new == nil || old == nil || new["id"] == old["id"] || new["scope"] != old["scope"] {
			continue
		}
		switch textValue(relation["kind"]) {
		case "supersedes":
			if textValue(new["key"]) != "" && textValue(old["key"]) != "" && !strings.EqualFold(textValue(new["key"]), textValue(old["key"])) {
				continue
			}
			newMin, _, human := claimTimes(new, sources)
			_, oldMax, _ := claimTimes(old, sources)
			if !human || !newMin.After(oldMax) || textValue(new["kind"]) == "proposal" {
				continue
			}
			old["status"] = "superseded"
		case "duplicate":
			if new["kind"] != old["kind"] || textValue(old["status"]) != "active" {
				continue
			}
			newMin, _, _ := claimTimes(new, sources)
			oldMin, _, _ := claimTimes(old, sources)
			if newMin.Before(oldMin) || (newMin.Equal(oldMin) && textValue(new["id"]) <= textValue(old["id"])) {
				continue
			}
			new["status"] = "duplicate"
		case "contradicts":
			new["unsure"] = true
			old["unsure"] = true
		default:
			continue
		}
		relations = unionDocuments(relations, []document{relation})
	}
	proposed := []document{}
	patched := map[string]bool{}
	for _, n := range items(result["notes"]) {
		scope, title := textValue(n["scope"]), textValue(n["title"])
		if title == "" || scope == "" {
			continue
		}
		refs := []string{}
		for _, old := range items(previous["notes"]) {
			if old["scope"] == scope && old["title"] == title {
				refs = append(refs, stringsValue(old["claims"])...)
			}
		}
		for _, ref := range stringsValue(n["claims"]) {
			refs = append(refs, resolve(ref))
		}
		proposed = append(proposed, merge(n, document{"claims": unique(refs)}))
		patched[scope+"\x00"+title] = true
	}
	for _, n := range items(previous["notes"]) {
		if !patched[textValue(n["scope"])+"\x00"+textValue(n["title"])] {
			proposed = append(proposed, n)
		}
	}
	finalNotes := []document{}
	assigned := map[string]bool{}
	for _, n := range proposed {
		refs := []string{}
		for _, id := range unique(stringsValue(n["claims"])) {
			if c := byID[id]; c != nil && c["scope"] == n["scope"] && !assigned[id] {
				refs = append(refs, id)
				assigned[id] = true
			}
		}
		if len(refs) == 0 {
			continue
		}
		folder := stringsValue(n["folder"])
		if len(folder) == 0 {
			folder = []string{textValue(n["scope"])}
		}
		folder = folder[:min(2, len(folder))]
		finalNotes = append(finalNotes, document{"id": identity([]any{n["scope"], n["title"]}), "title": n["title"], "scope": n["scope"], "folder": folder, "claims": refs, "links": []document{}, "suggested_links": n["links"]})
	}
	groups := map[string]document{}
	order := []string{}
	for _, c := range claims {
		id := textValue(c["id"])
		if assigned[id] {
			continue
		}
		key := identity([]any{c["scope"], c["topic"]})
		group := groups[key]
		if group == nil {
			group = document{"id": key, "title": c["topic"], "scope": c["scope"], "folder": []string{textValue(c["scope"])}, "claims": []string{}, "links": []document{}}
			groups[key] = group
			order = append(order, key)
		}
		group["claims"] = append(stringsValue(group["claims"]), id)
	}
	for _, key := range order {
		finalNotes = append(finalNotes, groups[key])
	}
	merged := []document{}
	noteIndex := map[string]document{}
	for _, n := range finalNotes {
		id := textValue(n["id"])
		if old := noteIndex[id]; old != nil {
			old["claims"] = unique(append(stringsValue(old["claims"]), stringsValue(n["claims"])...))
			old["suggested_links"] = append(items(old["suggested_links"]), items(n["suggested_links"])...)
		} else {
			noteIndex[id] = n
			merged = append(merged, n)
		}
	}
	lookup := map[string]document{}
	for _, n := range merged {
		lookup[textValue(n["scope"])+"\x00"+textValue(n["title"])] = n
	}
	for _, n := range merged {
		for _, l := range items(n["suggested_links"]) {
			target := lookup[textValue(l["scope"])+"\x00"+textValue(l["title"])]
			if target != nil && target["id"] != n["id"] {
				n["links"] = append(items(n["links"]), document{"id": target["id"], "reason": l["reason"]})
			}
		}
		delete(n, "suggested_links")
	}
	// Keep the previous identity when membership still overlaps, then apply the
	// whole rename permutation once so swaps cannot redirect links twice.
	used := map[string]bool{}
	remapped := map[string]string{}
	oldNotes := items(previous["notes"])
	for _, n := range merged {
		var best document
		bestOverlap := 0
		for _, old := range oldNotes {
			if old["scope"] != n["scope"] || used[textValue(old["id"])] {
				continue
			}
			overlap := 0
			for _, id := range stringsValue(old["claims"]) {
				if contains(stringsValue(n["claims"]), id) {
					overlap++
				}
			}
			if overlap > bestOverlap {
				best = old
				bestOverlap = overlap
			}
		}
		if best != nil {
			remapped[textValue(n["id"])] = textValue(best["id"])
			n["id"] = best["id"]
			used[textValue(best["id"])] = true
		}
	}
	priorClaims := index(items(previous["claims"]))
	priorNotes := index(oldNotes)
	for _, n := range merged {
		for _, l := range items(n["links"]) {
			if id, ok := remapped[textValue(l["id"])]; ok {
				l["id"] = id
			}
		}
		old := priorNotes[textValue(n["id"])]
		if old == nil {
			continue
		}
		if _, ok := old["sections"]; !ok {
			continue
		}
		unchanged := identity(sortedUnique(stringsValue(n["claims"]))) == identity(sortedUnique(stringsValue(old["claims"])))
		for _, id := range stringsValue(n["claims"]) {
			if identity(byID[id]) != identity(priorClaims[id]) {
				unchanged = false
			}
		}
		sections := []document{}
		for _, s := range items(old["sections"]) {
			paragraphs := []document{}
			for _, p := range items(s["paragraphs"]) {
				valid := true
				for _, id := range stringsValue(p["claims"]) {
					c := byID[id]
					if !contains(stringsValue(n["claims"]), id) || textValue(c["status"]) != "active" || c["text"] != priorClaims[id]["text"] {
						valid = false
					}
				}
				if valid {
					paragraphs = append(paragraphs, p)
				}
			}
			if len(paragraphs) > 0 {
				sections = append(sections, merge(s, document{"paragraphs": paragraphs}))
			}
		}
		n["sections"] = sections
		n["needs_editorial_update"] = !unchanged
		for _, field := range []string{"composition", "article_rejections"} {
			if value, ok := old[field]; ok {
				n[field] = value
			}
		}
	}
	vault["claims"] = claims
	vault["notes"] = merged
	vault["relations"] = relations
	vault["scope_aliases"] = savedAliases
	vault["rejected"] = rejected
	vault["update_receipt"] = document{"proposal_hints": hintReceipt, "new_sources": newCount, "revisited_sources": len(revisited), "candidate_claims": len(compact), "candidate_notes": len(notes), "provider_calls": 1, "semantic_audit": "not run; new claims have only structural/quote validation"}
	return vault, nil
}
func unionDocuments(left, right []document) []document {
	seen := map[string]bool{}
	result := []document{}
	for _, d := range append(append([]document{}, left...), right...) {
		key := identity(d)
		if !seen[key] {
			seen[key] = true
			result = append(result, d)
		}
	}
	return result
}
func claimTimes(claim, sources document) (time.Time, time.Time, bool) {
	var first, last time.Time
	human := false
	for _, e := range items(claim["evidence"]) {
		source := object(sources[textValue(e["source"])])
		stamp, _ := time.Parse(time.RFC3339Nano, textValue(source["ts"]))
		if first.IsZero() || stamp.Before(first) {
			first = stamp
		}
		if stamp.After(last) {
			last = stamp
		}
		if textValue(source["role"]) == "user" {
			human = true
		}
	}
	return first, last, human
}

func compilerSourceWindows(model compilerModel, revisited []string) (map[string]document, error) {
	result := map[string]document{}
	for _, id := range revisited {
		var start, length int
		err := model.db.QueryRow("SELECT start,length FROM source_windows WHERE partition='profile' AND id=?", id).Scan(&start, &length)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		result[id] = document{"start": start, "length": length}
	}
	return result, nil
}
func compilerProposalHints(model compilerModel, messages []document) ([]document, document, error) {
	selected := map[string]bool{}
	marks := []string{}
	args := []any{"profile"}
	for _, s := range messages {
		id := textValue(s["id"])
		selected[id] = true
		marks = append(marks, "?")
		args = append(args, id)
	}
	hints := []document{}
	receipt := document{"supplied": 0, "omitted": 0, "lookup_capped": false}
	if len(marks) == 0 {
		return hints, receipt, nil
	}
	rows, err := model.db.Query("SELECT DISTINCT p.id,p.payload FROM proposals p JOIN proposal_sources s ON p.partition=s.partition AND p.id=s.id WHERE p.partition=? AND s.source IN ("+strings.Join(marks, ",")+") ORDER BY p.id LIMIT 128", args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id, payload string
		if err = rows.Scan(&id, &payload); err != nil {
			return nil, nil, err
		}
		var p document
		if err = json.Unmarshal([]byte(payload), &p); err != nil {
			return nil, nil, fmt.Errorf("invalid staged proposal %s: %w", id, err)
		}
		count++
		evidence := []document{}
		for _, e := range items(p["evidence"]) {
			if selected[textValue(e["source"])] {
				evidence = append(evidence, choose(e, "source", "span_start", "span_end"))
			}
		}
		if len(evidence) == 0 {
			continue
		}
		hint := choose(p, "op", "text", "subject_hint", "target")
		hint["evidence"] = evidence
		if len(hints) < 32 && asciiJSONLength(append(hints, hint)) <= 12000 {
			hints = append(hints, hint)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	receipt["supplied"] = len(hints)
	receipt["omitted"] = count - len(hints)
	receipt["lookup_capped"] = count == 128
	return hints, receipt, nil
}

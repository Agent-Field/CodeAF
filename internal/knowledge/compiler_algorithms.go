package knowledge

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"golang.org/x/text/cases"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
)

func textSpan(s string, start, n int) string {
	r := []rune(s)
	start = max(0, min(start, len(r)))
	return string(r[start:min(len(r), start+max(0, n))])
}
func runeLength(s string) int { return len([]rune(s)) }
func sortedDocuments(m map[string]document) []document {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []document{}
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}
func questionID(s, q document) string {
	return identity([]any{"question", s["id"], foldText(strings.TrimSpace(textValue(q["question"])))})
}

var questionWordPattern = regexp.MustCompile(`[\pL\pN_]{3,}`)

func questionTerms(s string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, w := range questionWordPattern.FindAllString(foldText(s), -1) {
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}
func boundedQuestionTerms(s string) []string { w := questionTerms(s); return w[:min(12, len(w))] }
func canonicalText(v any) string {
	raw, _ := json.Marshal(v)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var plain any
	_ = decoder.Decode(&plain)
	var out strings.Builder
	canonicalJSON(&out, plain)
	return out.String()
}
func jsonCharacterLength(v any) int { return runeLength(canonicalText(v)) }
func asciiJSONLength(v any) int {
	size := 0
	for _, r := range canonicalText(v) {
		switch {
		case r <= 127:
			size++
		case r <= 0xffff:
			size += 6
		default:
			hi, lo := utf16.EncodeRune(r)
			if hi >= 0 && lo >= 0 {
				size += 12
			}
		}
	}
	return size
}
func isArray(v any) bool {
	switch v.(type) {
	case []any, []document, []string:
		return true
	}
	return false
}
func foldText(s string) string { return cases.Fold().String(s) }
func isDocumentArray(v any) bool {
	if v == nil {
		return true
	}
	if !isArray(v) {
		return false
	}
	switch a := v.(type) {
	case []any:
		return len(a) == len(items(v))
	case []document:
		return true
	}
	return false
}
func referencesAllowed(v any, allowed map[string]document) bool {
	if v == nil {
		return true
	}
	if !isArray(v) {
		return false
	}
	ids := stringsValue(v)
	switch a := v.(type) {
	case []any:
		if len(a) != len(ids) {
			return false
		}
	}
	return subset(ids, allowed)
}

var articleNarration = regexp.MustCompile(`(?i)\b(?:the owner|the user|the agent|user|agent) (?:asked|said|reported|requested|instructed|wants)\b`)

func curateQuestions(vault, learning, previous document, model compilerModel, context string, perSubject, budget int) (document, error) {
	if perSubject < 0 || perSubject > 5 || budget < 0 || budget > 128 {
		return nil, fmt.Errorf("invalid curation bounds")
	}
	r := clone(learning)
	active := activeClaims(vault)
	catalog := []document{}
	questions := map[string]document{}
	eligible := map[string]bool{}
	hashes := document{}
	for _, s := range items(r["subjects"]) {
		for _, q := range items(s["questions"]) {
			id := questionID(s, q)
			q["id"] = id
			questions[id] = q
			c := document{"id": id, "subject_id": s["id"], "subject": s["title"], "question": q["question"], "why": textValue(q["why"]), "claims": q["claims"]}
			catalog = append(catalog, c)
			hashes[id] = identity(c)
			if q["state"] != "answered" && len(stringsValue(q["claims"])) > 0 && subset(stringsValue(q["claims"]), active) {
				eligible[id] = true
			}
		}
	}
	settings := identity([]any{"curiosity-2", context, perSubject, budget})
	old := object(previous["curation"])
	same := old["context_hash"] == settings
	retained := []document{}
	changed := []document{}
	byID := index(catalog)
	if same {
		for _, q := range items(old["selection"]) {
			id := textValue(q["id"])
			if eligible[id] && object(old["question_hashes"])[id] == hashes[id] {
				retained = append(retained, q)
			}
		}
	}
	for _, q := range catalog {
		id := textValue(q["id"])
		if eligible[id] && object(old["question_hashes"])[id] != hashes[id] {
			changed = append(changed, q)
		}
	}
	pool := []document{}
	if same {
		for _, q := range retained {
			pool = append(pool, byID[textValue(q["id"])])
		}
		pool = append(pool, changed...)
	} else {
		for _, q := range catalog {
			if eligible[textValue(q["id"])] {
				pool = append(pool, q)
			}
		}
	}
	pool = unionDocuments(nil, pool)
	inspected := map[string]bool{}
	if same {
		for _, id := range stringsValue(old["inspected_question_ids"]) {
			if eligible[id] {
				inspected[id] = true
			}
		}
	}
	chosen := retained
	calls := 0
	if budget == 0 || perSubject == 0 {
		chosen = []document{}
	} else if !(same && len(changed) == 0) && len(pool) > 0 {
		groups := map[string][]document{}
		for _, q := range pool {
			sid := textValue(q["subject_id"])
			groups[sid] = append(groups[sid], q)
		}
		keys := []string{}
		levels := 0
		for sid, g := range groups {
			keys = append(keys, sid)
			levels = max(levels, len(g))
		}
		sort.Slice(keys, func(i, j int) bool { return identity([]any{context, keys[i]}) < identity([]any{context, keys[j]}) })
		short := []document{}
		size := 2
		for level := 0; level < levels; level++ {
			for _, sid := range keys {
				g := groups[sid]
				if level >= len(g) || len(short) >= 256 {
					continue
				}
				q := g[level]
				n := asciiJSONLength(q) + 2
				if size+n <= 80000 {
					short = append(short, q)
					size += n
					inspected[textValue(q["id"])] = true
				}
			}
		}
		response, e := model.call("curate_questions", document{"context": context, "per_subject": perSubject, "budget": budget, "questions": short})
		if e != nil {
			return nil, e
		}
		calls = 1
		allowed := index(short)
		counts := map[string]int{}
		seen := map[string]bool{}
		chosen = []document{}
		for _, item := range items(response["keep"]) {
			id := textValue(item["id"])
			sid := textValue(allowed[id]["subject_id"])
			reason := textValue(item["reason"])
			if allowed[id] == nil || seen[id] || len(chosen) >= budget || counts[sid] >= perSubject || strings.TrimSpace(reason) == "" {
				continue
			}
			chosen = append(chosen, document{"id": id, "reason": textSpan(reason, 0, 400)})
			seen[id] = true
			counts[sid]++
		}
	}
	selected := index(chosen)
	parked := 0
	for id, q := range questions {
		if q["state"] == "answered" {
			q["curiosity"] = document{"state": "closed", "reason": "Reviewed complete answer exists."}
		} else {
			state, reason := "parked", "Parked in bounded curation; no claim of unimportance."
			if selected[id] != nil {
				state = "active"
				reason = textValue(selected[id]["reason"])
			} else {
				parked++
			}
			q["curiosity"] = document{"state": state, "reason": reason, "inspected": inspected[id], "wake_condition": "New matching evidence, relevant task context, or explicit request"}
		}
	}
	inspectedIDs := []string{}
	missing := 0
	for id := range inspected {
		inspectedIDs = append(inspectedIDs, id)
	}
	sort.Strings(inspectedIDs)
	for id := range eligible {
		if !inspected[id] {
			missing++
		}
	}
	r["curation"] = document{"input_hash": identity([]any{settings, hashes}), "context_hash": settings, "question_hashes": hashes, "inspected_question_ids": inspectedIDs, "selection": chosen, "model_calls": calls, "active": len(chosen), "parked": parked, "not_inspected": missing, "limitation": "Incremental model usefulness selection; parked or uninspected questions are not proven unimportant."}
	return r, nil
}
func wakeQuestions(learning, wakeups, vault document) document {
	r := clone(learning)
	sources := object(vault["sources"])
	for _, s := range items(r["subjects"]) {
		for _, q := range items(s["questions"]) {
			id := questionID(s, q)
			newIDs := []string{}
			for _, w := range items(wakeups[id]) {
				sid := textValue(w["source"])
				if sources[sid] != nil && !contains(stringsValue(q["processed_wakes"]), sid) {
					newIDs = append(newIDs, sid)
				}
			}
			if len(newIDs) > 0 {
				q["wake_sources"] = sortedUnique(append(stringsValue(q["wake_sources"]), newIDs...))
				q["curiosity"] = document{"state": "woken", "was_active": object(q["curiosity"])["state"] == "active", "reason": "New source matches indexed question vocabulary", "wake_condition": "Review these sources once; time alone does not retry"}
			}
		}
	}
	return r
}

type evidenceIndex struct{ db *sql.DB }

func newEvidenceIndex(claims map[string]document, sources document) (*evidenceIndex, error) {
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	if _, e = db.Exec(`CREATE VIRTUAL TABLE documents USING fts5(kind UNINDEXED, key UNINDEXED, body)`); e != nil {
		db.Close()
		return nil, e
	}
	for _, c := range sortedDocuments(claims) {
		if _, e = db.Exec("INSERT INTO documents VALUES(?,?,?)", "claim", c["id"], c["text"]); e != nil {
			db.Close()
			return nil, e
		}
	}
	keys := []string{}
	for id := range sources {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		if _, e = db.Exec("INSERT INTO documents VALUES(?,?,?)", "source", id, object(sources[id])["text"]); e != nil {
			db.Close()
			return nil, e
		}
	}
	return &evidenceIndex{db}, nil
}
func (i *evidenceIndex) search(terms []string, limit int) ([]document, error) {
	out := []document{}
	if len(terms) == 0 {
		return out, nil
	}
	quoted := []string{}
	for _, t := range terms {
		quoted = append(quoted, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
	}
	rows, e := i.db.Query("SELECT kind,key,body FROM documents WHERE documents MATCH ? ORDER BY bm25(documents),key LIMIT ?", strings.Join(quoted, " OR "), limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var kind, id, body string
		if e = rows.Scan(&kind, &id, &body); e != nil {
			return nil, e
		}
		out = append(out, document{"kind": kind, "id": id, "text": body})
	}
	return out, rows.Err()
}
func evidenceSnippet(id string, source document, terms []string, lead, size int, earliest bool) document {
	body := textValue(source["text"])

	pos := -1
	if !earliest {
		terms = append([]string{}, terms...)
		sort.SliceStable(terms, func(i, j int) bool { return len(terms[i]) > len(terms[j]) })
	}
	for _, t := range terms {
		p := foldedRuneIndex(body, t)
		if p >= 0 && (pos < 0 || earliest && p < pos) {
			pos = p
			if !earliest {
				break
			}
		}
	}
	start := 0
	if pos >= 0 {
		start = max(0, pos-lead)
	}
	quote := textSpan(body, start, size)
	return document{"id": identity([]any{id, start, quote}), "source": id, "quote": quote, "role": source["role"], "ts": source["ts"], "source_hash": identity(body)}
}

func collectProposals(envelope, committed, sources, partition, previous document, claimIDs []string) (document, error) {
	if len(partition) != 2 || strings.TrimSpace(textValue(partition["owner"])) == "" || strings.TrimSpace(textValue(partition["project"])) == "" {
		return nil, fmt.Errorf("host must supply proven owner/project partition keys")
	}
	proposals := items(clone(previous)["proposals"])
	for _, p := range proposals {
		if identity(p["partition"]) != identity(partition) {
			return nil, fmt.Errorf("do not mix proposal outboxes from different host partitions")
		}
	}
	sid := textValue(committed["id"])
	if sid == "" {
		return nil, fmt.Errorf("committed response requires immutable source ID")
	}
	if sources[sid] != nil && identity(sources[sid]) != identity(committed) {
		return nil, fmt.Errorf("committed response changed under immutable source ID")
	}
	permitted := merge(sources, document{sid: committed})
	rejected := []document{}
	newCount, duplicates := 0, 0
	delta := envelope["knowledge_delta"]
	if delta != nil && !isArray(delta) {
		rejected = append(rejected, document{"reason": "Optional knowledge_delta must be a list"})
		delta = nil
	}
	byID := index(proposals)
	raws := []any{}
	switch a := delta.(type) {
	case []any:
		raws = a
	case []document:
		for _, d := range a {
			raws = append(raws, d)
		}
	}
	for n, v := range raws {
		raw := object(v)
		reason := ""
		op := textValue(raw["op"])
		text, subject := strings.TrimSpace(textValue(raw["text"])), strings.TrimSpace(textValue(raw["subject"]))
		target := any(nil)
		evidence := []document{}
		switch {
		case n >= 4:
			reason = "At most four proposals per checkpoint"
		case !contains([]string{"learn", "correct", "procedure", "question"}, op):
			reason = "Unknown proposal operation"
		case runeLength(text) < 1 || runeLength(text) > 1000 || runeLength(subject) < 1 || runeLength(subject) > 120:
			reason = "Missing or oversized text/subject"
		case op == "correct" && !contains(claimIDs, textValue(raw["target"])):
			reason = "Correction must name a known claim; it is still only a proposal"
		case !isDocumentArray(raw["evidence"]) || len(items(raw["evidence"])) < 1 || len(items(raw["evidence"])) > 4:
			reason = "Proposal needs one to four evidence spans"
		}
		if op == "correct" {
			target = raw["target"]
		}
		if reason == "" {
			for _, ev := range items(raw["evidence"]) {
				id := textValue(ev["source"])
				if id == "self" {
					id = sid
				}
				s := object(permitted[id])
				quote := textValue(ev["quote"])
				body := textValue(s["text"])
				start := strings.Index(body, quote)
				if id == "" || len(s) == 0 || quote == "" || start < 0 {
					reason = "Evidence absent from permitted committed sources"
					break
				}
				offset := runeLength(body[:start])
				evidence = append(evidence, document{"source": id, "quote": quote, "span_start": offset, "span_end": offset + runeLength(quote), "role": s["role"], "source_hash": identity(body)})
			}
		}
		if reason != "" {
			rejected = append(rejected, document{"index": n, "reason": reason})
			continue
		}
		normalized := foldText(strings.Join(strings.Fields(text), " "))
		id := identity([]any{partition, op, normalized, target})
		if p := byID[id]; p != nil {
			p["evidence"] = unionEvidence(items(p["evidence"]), evidence)
			duplicates++
			continue
		}
		p := document{"id": id, "partition": clone(partition), "op": op, "text": text, "subject_hint": subject, "evidence": evidence, "target": target, "state": "pending_semantic_review"}
		proposals = append(proposals, p)
		byID[id] = p
		newCount++
	}
	return document{"proposals": proposals, "rejected": rejected, "receipt": document{"new": newCount, "duplicates": duplicates, "rejected": len(rejected), "provider_calls": 0, "limitation": "Staged only. Exact quotes and schema checks do not prove truth."}}, nil
}

func simulateSchedule(messages []document, quiet, maxWait, maxChars int) (document, error) {
	if quiet < 0 || maxWait < 0 || maxChars < 1 {
		return nil, fmt.Errorf("invalid schedule bounds")
	}
	messages = append([]document{}, messages...)
	sort.SliceStable(messages, func(i, j int) bool {
		a, b := textValue(messages[i]["ts"]), textValue(messages[j]["ts"])
		if a == b {
			return textValue(messages[i]["id"]) < textValue(messages[j]["id"])
		}
		return a < b
	})
	pending := map[string]document{}
	order := []string{}
	batches := []document{}
	maxObserved := float64(0)
	flush := func(session string, at float64) {
		p := pending[session]
		wait := at - number(p["first"])
		maxObserved = math.Max(maxObserved, wait)
		batches = append(batches, document{"session": session, "at": at, "messages": p["messages"], "characters": p["characters"], "oldest_wait_seconds": wait})
		delete(pending, session)
		for j, s := range order {
			if s == session {
				order = append(order[:j], order[j+1:]...)
				break
			}
		}
	}
	for _, m := range messages {
		stamp, e := time.Parse(time.RFC3339Nano, textValue(m["ts"]))
		if e != nil {
			return nil, e
		}
		at := float64(stamp.UnixNano()) / 1e9
		for _, session := range append([]string{}, order...) {
			p := pending[session]
			due := math.Min(number(p["last"])+float64(quiet), number(p["first"])+float64(maxWait))
			if due <= at {
				flush(session, due)
			}
		}
		session := textValue(m["session"])
		p := pending[session]
		if p == nil {
			p = document{"first": at, "messages": []string{}, "characters": 0}
			pending[session] = p
			order = append(order, session)
		}
		p["last"] = at
		p["characters"] = int(number(p["characters"])) + runeLength(textValue(m["text"]))
		p["messages"] = append(stringsValue(p["messages"]), textValue(m["id"]))
		if number(p["characters"]) >= float64(maxChars) {
			flush(session, at)
		}
	}
	for _, session := range append([]string{}, order...) {
		p := pending[session]
		flush(session, math.Min(number(p["last"])+float64(quiet), number(p["first"])+float64(maxWait)))
	}
	return document{"quiet_seconds": quiet, "max_wait_seconds": maxWait, "capture_events": len(messages), "extraction_batches": len(batches), "max_observed_wait_seconds": maxObserved, "mean_messages_per_batch": math.Round(float64(len(messages))/float64(max(1, len(batches)))*100) / 100, "batches": batches}, nil
}

// foldedRuneIndex maps a folded match back to original rune coordinates.
// Unicode case folding can expand a rune, so transformed byte offsets cannot
// be used to slice the original source text.
func foldedRuneIndex(text, term string) int {
	target := strings.Index(foldText(text), foldText(term))
	if target < 0 {
		return -1
	}
	offset := 0
	for i, r := range []rune(text) {
		size := len(foldText(string(r)))
		if target < offset+size {
			return i
		}
		offset += size
	}
	return -1
}

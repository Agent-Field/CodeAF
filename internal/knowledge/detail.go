package knowledge

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
)

// ClaimDetail carries the claim's recorded origin, never an inference from its
// author's role. An agent's synthesis and report are different kinds of evidence.
type ClaimDetail struct {
	ID          string     `json:"id"`
	Text        string     `json:"text"`
	Kind        string     `json:"kind"`
	SourceKinds []string   `json:"source_kinds"`
	Status      string     `json:"status"`
	Evidence    []Evidence `json:"evidence"`
}
type SourceLocation struct {
	Session string `json:"session"`
	ID      string `json:"id"`
	Role    string `json:"role"`
	TS      string `json:"ts"`
}
type ChangeDetail struct {
	Before string `json:"before"`
	After  string `json:"after"`
	Reason string `json:"reason"`
}
type PageDetail struct {
	Changes          []ChangeDetail `json:"changes"`
	EditableMarkdown string         `json:"editable_markdown"`
	Page
	ClaimDetails []ClaimDetail             `json:"claim_details"`
	Sources      map[string]SourceLocation `json:"sources"`
}

// ReadDetail pins the page and its provenance to the same publication. Source
// bodies stay in the vault; the desk receives only locators and cited quotes.
func (v *Vault) ReadDetail(id string) (PageDetail, error) {
	db, err := v.database()
	if err != nil {
		return PageDetail{}, err
	}
	defer db.Close()
	if _, err = db.Exec("BEGIN DEFERRED"); err != nil {
		return PageDetail{}, err
	}
	defer db.Exec("ROLLBACK")
	var raw string
	if err = db.QueryRow("SELECT payload FROM pages WHERE id=?", id).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PageDetail{}, os.ErrNotExist
		}
		return PageDetail{}, err
	}
	var result PageDetail
	if err = json.Unmarshal([]byte(raw), &result.Page); err != nil {
		return result, err
	}
	result.EditableMarkdown = PageMarkdown(result.Page)
	published, err := stateDocument(db, "published")
	if err != nil {
		return result, err
	}
	for i := range result.Conflicts {
		result.Conflicts[i].ID = ConflictID(result.Conflicts[i])
	}
	wanted := map[string]bool{}
	for _, c := range result.Claims {
		wanted[c] = true
	}
	for _, s := range result.Sections {
		for _, p := range s.Paragraphs {
			for _, c := range p.Claims {
				wanted[c] = true
			}
		}
	}
	for _, c := range result.Conflicts {
		for _, id := range c.Claims {
			wanted[id] = true
		}
	}
	for _, choice := range result.Resolutions {
		for _, id := range choice.Claims {
			wanted[id] = true
		}
	}
	result.Changes = []ChangeDetail{}
	relations := items(published["relations"])
	adjacent := map[string][]string{}
	for _, r := range relations {
		if textValue(r["kind"]) == "supersedes" {
			before, after := textValue(r["to"]), textValue(r["from"])
			adjacent[before] = append(adjacent[before], after)
			adjacent[after] = append(adjacent[after], before)
		}
	}
	queue := []string{}
	for id := range wanted {
		queue = append(queue, id)
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, next := range adjacent[id] {
			if !wanted[next] {
				wanted[next] = true
				queue = append(queue, next)
			}
		}
	}
	for _, r := range relations {
		before, after := textValue(r["to"]), textValue(r["from"])
		if textValue(r["kind"]) == "supersedes" && wanted[before] && wanted[after] {
			result.Changes = append(result.Changes, ChangeDetail{Before: before, After: after, Reason: textValue(r["reason"])})
		}
	}
	result.ClaimDetails = []ClaimDetail{}
	for _, c := range items(published["claims"]) {
		if wanted[textValue(c["id"])] {
			data, _ := json.Marshal(c)
			var detail ClaimDetail
			if err = json.Unmarshal(data, &detail); err != nil {
				return result, err
			}
			detail.SourceKinds = append([]string{}, stringsValue(evidenceBasis(object(published["sources"]), items(c["evidence"]))["source_kinds"])...)
			result.ClaimDetails = append(result.ClaimDetails, detail)
		}
	}
	result.Sources = map[string]SourceLocation{}
	sources := object(published["sources"])
	evidence := append([]Evidence{}, result.Evidence...)
	for _, claim := range result.ClaimDetails {
		evidence = append(evidence, claim.Evidence...)
	}
	for _, e := range evidence {
		if source, ok := sources[e.Source]; ok {
			data, _ := json.Marshal(source)
			var location SourceLocation
			if err = json.Unmarshal(data, &location); err != nil {
				return result, err
			}
			result.Sources[e.Source] = location
		}
	}
	restorePageQuoteFormatting(&result, sources)
	return result, nil
}

// Quote validation folds whitespace. Reading restores only the cited substring
// from its original source, so Markdown keeps its lines without exposing the
// surrounding conversation or changing the stored evidence.
func quoteMarkdown(quote, source string) string {
	if quote == "" || source == "" || strings.Contains(source, quote) {
		return quote
	}
	words := strings.Fields(quote)
	for i := range words {
		words[i] = regexp.QuoteMeta(words[i])
	}
	pattern, err := regexp.Compile(strings.Join(words, `[[:space:]\p{Z}\x{85}]+`))
	if err != nil {
		return quote
	}
	if found := pattern.FindString(source); found != "" {
		return found
	}
	return quote
}

func restorePageQuoteFormatting(page *PageDetail, sources document) {
	cache := map[[2]string]string{}
	restore := func(evidence []Evidence) {
		for i := range evidence {
			e := &evidence[i]
			key := [2]string{e.Source, e.Quote}
			formatted, found := cache[key]
			if !found {
				formatted = quoteMarkdown(e.Quote, textValue(object(sources[e.Source])["text"]))
				cache[key] = formatted
			}
			e.Quote = formatted
		}
	}
	restore(page.Evidence)
	for i := range page.ClaimDetails {
		restore(page.ClaimDetails[i].Evidence)
	}
	for i := range page.Sections {
		for j := range page.Sections[i].Paragraphs {
			restore(page.Sections[i].Paragraphs[j].Evidence)
		}
	}
}

// ConflictID survives reordering of the two accounts between publications.
func ConflictID(c Conflict) string { return identity(sortedUnique(c.Claims)) }

package knowledge

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"time"
)

var ErrPageChanged = errors.New("knowledge page changed; reopen it before saving or deleting")

// Human overrides are separate database records. Publishing generated pages
// always overlays the latest committed edits and tombstones in its transaction.
type pageOverride struct {
	ID                   string `json:"id"`
	Deleted              bool   `json:"deleted,omitempty"`
	Markdown             string `json:"markdown,omitempty"`
	UpdatedAt            string `json:"updated_at"`
	EvidenceBaselineHash string `json:"evidence_baseline_hash,omitempty"`
}

func evidenceFingerprint(evidence []Evidence) string {
	keys := []string{}
	for _, item := range evidence {
		keys = append(keys, identity([]any{item.Source, item.Quote, item.TS, item.Role}))
	}
	return identity(sortedUnique(keys))
}

// PageMarkdown returns the editable article body; source citations stay outside
// the editor because editing a sentence does not verify it against old evidence.
func PageMarkdown(page Page) string {
	if page.HumanEdited {
		return page.Markdown
	}
	var sections []string
	for _, section := range page.Sections {
		var parts []string
		if section.Heading != "" {
			parts = append(parts, "## "+section.Heading)
		}
		for _, paragraph := range section.Paragraphs {
			text := []rune(paragraph.Text)
			for i := len(paragraph.InlineLinks) - 1; i >= 0; i-- {
				link := paragraph.InlineLinks[i]
				if link.Start < 0 || link.End > len(text) || link.End <= link.Start {
					continue
				}
				label := link.Label
				if label == "" {
					label = string(text[link.Start:link.End])
				}
				text = append(append(append([]rune{}, text[:link.Start]...), []rune("[["+link.ID+"|"+label+"]]")...), text[link.End:]...)
			}
			parts = append(parts, string(text))
		}
		sections = append(sections, strings.Join(parts, "\n\n"))
	}
	return strings.Join(sections, "\n\n")
}

func (v *Vault) EditPage(id, expectedRevision, markdown string) error {
	if strings.TrimSpace(markdown) == "" {
		return errors.New("knowledge page body cannot be empty; use delete to remove the page")
	}
	if len(markdown) > 128*1024 {
		return errors.New("knowledge page body exceeds 128 KiB")
	}
	return v.changePage(id, expectedRevision, pageOverride{ID: id, Markdown: markdown})
}
func (v *Vault) DeletePage(id, expectedRevision string) error {
	return v.changePage(id, expectedRevision, pageOverride{ID: id, Deleted: true})
}
func (v *Vault) changePage(id, revision string, override pageOverride) error {
	if !v.Enabled() {
		return ErrDisabled
	}
	db, err := v.database()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	if err = tx.QueryRow("SELECT payload FROM pages WHERE id=?", id).Scan(&raw); err == sql.ErrNoRows {
		return os.ErrNotExist
	} else if err != nil {
		return err
	}
	var page Page
	if err = json.Unmarshal([]byte(raw), &page); err != nil {
		return err
	}
	if revision == "" || page.Revision != revision {
		return ErrPageChanged
	}
	if override.Deleted {
		override.Markdown = PageMarkdown(page)
	}
	override.EvidenceBaselineHash = evidenceFingerprint(page.Evidence)
	override.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	payload, err := json.Marshal(override)
	if err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO overrides(id,payload) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", id, string(payload)); err != nil {
		return err
	}
	generated, err := stateDocument(tx, "generated")
	if err != nil {
		return err
	}
	overrides, err := readOverrides(tx)
	if err != nil {
		return err
	}
	effective := applyNativeOverrides(generated, overrides)
	linkCompilation(effective)
	pages, err := materializePages(effective)
	if err != nil {
		return err
	}
	pages, err = applyPageOverrides(pages, overrides)
	if err != nil {
		return err
	}
	if err = storePages(tx, pages); err != nil {
		return err
	}
	if err = saveState(tx, "revision", document{"value": identity(effective)}); err != nil {
		return err
	}
	if err = saveState(tx, "published", effective); err != nil {
		return err
	}
	return tx.Commit()
}
func applyPageOverrides(pages []Page, overrides map[string]pageOverride) ([]Page, error) {
	kept := make([]Page, 0, len(pages))
	visible := map[string]bool{}
	for _, page := range pages {
		if override, ok := overrides[page.ID]; ok {
			if override.Deleted {
				continue
			}
			page.HumanEdited = true
			page.HumanEditedAt = override.UpdatedAt
			page.Links = nil
			page.Backlinks = nil
			page.Markdown = override.Markdown
			page.Sections = manualSections(override.Markdown)
			page.Claims = nil
			page.NeedsEditorialUpdate = override.EvidenceBaselineHash == "" || override.EvidenceBaselineHash != evidenceFingerprint(page.Evidence)
			page.LearningState = "human_edited"
		}
		visible[page.ID] = true
		kept = append(kept, page)
	}
	labels := map[string]string{}
	ambiguous := map[string]bool{}
	for _, p := range kept {
		for _, label := range append([]string{p.Title}, p.Aliases...) {
			key := strings.ToLower(label)
			if prior, ok := labels[key]; ok && prior != p.ID {
				ambiguous[key] = true
			}
			labels[key] = p.ID
		}
	}
	for i := range kept {
		page := &kept[i]
		page.Links = visibleLinks(page.Links, visible)
		page.Backlinks = visibleLinks(page.Backlinks, visible)
		for si := range page.Sections {
			for pi := range page.Sections[si].Paragraphs {
				para := &page.Sections[si].Paragraphs[pi]
				var links []InlineLink
				for _, link := range para.InlineLinks {
					if visible[link.ID] {
						links = append(links, link)
					}
				}
				if page.HumanEdited {
					links = nil
					literals := wikiLiteral.FindAllStringIndex(para.Text, -1)
					for _, span := range wikiReference.FindAllStringSubmatchIndex(para.Text, -1) {
						protected := false
						for _, literal := range literals {
							if span[0] < literal[1] && span[1] > literal[0] {
								protected = true
								break
							}
						}
						if protected {
							continue
						}
						target := para.Text[span[2]:span[3]]
						label := target
						if span[4] >= 0 {
							label = para.Text[span[4]:span[5]]
						}
						if !visible[target] {
							key := strings.ToLower(target)
							if ambiguous[key] {
								continue
							}
							target = labels[key]
						}
						if !visible[target] || target == page.ID {
							continue
						}
						links = append(links, InlineLink{Start: len([]rune(para.Text[:span[0]])), End: len([]rune(para.Text[:span[1]])), ID: target, Label: label})
						found := false
						for _, l := range page.Links {
							if l.ID == target {
								found = true
								break
							}
						}
						if !found {
							page.Links = append(page.Links, Link{ID: target, Kind: "mention", Reason: "Linked in human-edited article"})
						}
					}
				}
				para.InlineLinks = links
			}
		}
	}
	incoming := map[string][]Link{}
	for _, page := range kept {
		seen := map[string]bool{}
		for _, section := range page.Sections {
			for _, para := range section.Paragraphs {
				for _, link := range para.InlineLinks {
					if !seen[link.ID] {
						incoming[link.ID] = append(incoming[link.ID], Link{ID: page.ID, Kind: "mention"})
						seen[link.ID] = true
					}
				}
			}
		}
	}
	for i := range kept {
		page := &kept[i]
		var backlinks []Link
		for _, link := range page.Backlinks {
			if link.Kind != "mention" {
				backlinks = append(backlinks, link)
			}
		}
		page.Backlinks = append(backlinks, incoming[page.ID]...)
		page.Revision = ""
		raw, err := json.Marshal(page)
		if err != nil {
			return nil, err
		}
		page.Revision = hash(string(raw))
	}

	return kept, nil
}

var wikiLiteral = regexp.MustCompile("(?s)```.*?```|~~~.*?~~~|`+[^`]*`+|https?://\\S+|!?\\[[^\\]]*\\]\\([^)]*\\)")

var wikiReference = regexp.MustCompile(`\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)

func visibleLinks(links []Link, visible map[string]bool) []Link {
	var kept []Link
	for _, link := range links {
		if visible[link.ID] {
			kept = append(kept, link)
		}
	}
	return kept
}
func manualSections(markdown string) []Section {
	var sections []Section
	current := Section{}
	var lines []string
	flush := func() {
		text := strings.Trim(strings.Join(lines, "\n"), "\n")
		if text != "" {
			current.Paragraphs = append(current.Paragraphs, Paragraph{Text: text})
		}
		lines = nil
	}
	fenced := false
	for _, line := range strings.Split(markdown, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			fenced = !fenced
		}
		if !fenced && strings.HasPrefix(line, "## ") {
			flush()
			if current.Heading != "" || len(current.Paragraphs) > 0 {
				sections = append(sections, current)
			}
			current = Section{Heading: strings.TrimPrefix(line, "## ")}
			continue
		}
		lines = append(lines, line)
	}
	flush()
	if current.Heading != "" || len(current.Paragraphs) > 0 {
		sections = append(sections, current)
	}
	return sections
}

type overrideReader interface {
	Query(string, ...any) (*sql.Rows, error)
}

func readOverrides(db overrideReader) (map[string]pageOverride, error) {
	rows, err := db.Query("SELECT payload FROM overrides ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]pageOverride{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var override pageOverride
		if err = json.Unmarshal([]byte(raw), &override); err != nil {
			return nil, err
		}
		result[override.ID] = override
	}
	return result, rows.Err()
}

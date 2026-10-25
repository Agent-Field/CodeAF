package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

var ErrPageChanged = errors.New("knowledge page changed; reopen it before saving or deleting")

// ErrPageChangeApplied means the durable human override was committed, but its
// disposable exports could not be refreshed. Callers must not retry the write
// with its old revision; they should reload the already changed page instead.
var ErrPageChangeApplied = errors.New("knowledge page change saved; export refresh failed")

func pageChangeApplied(err error) error { return fmt.Errorf("%w: %w", ErrPageChangeApplied, err) }

// Human changes are separate from disposable compiler output. The compiler and
// editor share the publication lock, while atomic overlay files make reads safe.
type pageOverride struct {
	ID        string `json:"id"`
	Deleted   bool   `json:"deleted,omitempty"`
	Markdown  string `json:"markdown,omitempty"`
	UpdatedAt string `json:"updated_at"`
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
	if err := v.persistPageOverride(id, revision, override); err != nil {
		return err
	}
	// A generated vault has all compiler projections. Refresh them without a
	// provider call; a seed not yet compiled has only the Go reader/export.
	if _, err := os.Stat(filepath.Join(v.root, "knowledge", "vault.sqlite")); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return pageChangeApplied(err)
	}
	root, err := runtime(v.profile)
	if err != nil {
		return pageChangeApplied(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", filepath.Join(root, "bridge.py"), "--root", v.root, "--project", v.project, "--publish-overrides")
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		return pageChangeApplied(fmt.Errorf("knowledge exports could not be refreshed: %w: %s", err, strings.TrimSpace(string(out))))
	}
	return nil
}
func (v *Vault) persistPageOverride(id, revision string, override pageOverride) error {
	if !v.Enabled() {
		return ErrDisabled
	}
	directory := filepath.Join(v.root, "knowledge")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(directory, ".writer.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = filelock.Lock(lock, true, false); err != nil {
		return err
	}
	defer filelock.Unlock(lock)
	page, err := v.Read(id)
	if err != nil {
		return err
	}
	if revision == "" || page.Revision != revision {
		return ErrPageChanged
	}
	overlays := filepath.Join(v.root, "overrides")
	if err = os.MkdirAll(overlays, 0700); err != nil {
		return err
	}
	if override.Deleted {
		override.Markdown = PageMarkdown(page)
	}
	override.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err = writeJSON(filepath.Join(overlays, hash(id)+".json"), override); err != nil {
		return err
	}
	// Export is disposable. Its lock is shared with Python's publisher so a
	// compilation already in flight cannot put stale Markdown back afterwards.
	if err := v.exportChangedPage(id, override); err != nil {
		return pageChangeApplied(err)
	}
	return nil
}
func (v *Vault) exportChangedPage(id string, override pageOverride) error {
	if filepath.Base(id) != id || id == "." || id == ".." {
		return nil
	}
	dir := filepath.Join(v.root, "knowledge", "markdown")
	if override.Deleted {
		err := os.Remove(filepath.Join(dir, id+".md"))
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	page, err := v.Read(id)
	if err != nil {
		return err
	}
	body := "# " + page.Title + "\n\n> Edited by you. This text has not been verified against the original sources.\n\n" + override.Markdown + "\n"
	f, err := os.CreateTemp(dir, ".knowledge-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.WriteString(body); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, filepath.Join(dir, id+".md"))
}
func (v *Vault) applyOverrides(pages []Page) ([]Page, error) {
	overrides, err := v.pageOverrides()
	if err != nil {
		return nil, err
	}
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
			page.NeedsEditorialUpdate = false
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

func (v *Vault) pageOverrides() (map[string]pageOverride, error) {
	result := map[string]pageOverride{}
	dir := filepath.Join(v.root, "overrides")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var override pageOverride
		if err = json.Unmarshal(raw, &override); err != nil {
			return nil, fmt.Errorf("knowledge page override: %w", err)
		}
		if override.ID == "" || entry.Name() != hash(override.ID)+".json" {
			return nil, errors.New("knowledge page override identity mismatch")
		}
		result[override.ID] = override
	}
	return result, nil
}

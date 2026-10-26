// Package knowledge owns opt-in, profile-wide knowledge capture and browsing.
// Capture is local and model-free. Flush runs the native compiler.
package knowledge

import (
	"context"
	"crypto/sha256"

	"encoding/hex"
	"encoding/json"
	"errors"
	_ "modernc.org/sqlite"

	"github.com/Agent-Field/codeaf/internal/config"

	"path/filepath"
)

const Model = config.DefaultKnowledgeModel

var ErrDisabled = errors.New("knowledge is not enabled for this profile")

type Source struct {
	Project string `json:"project,omitempty"`
	ID      string `json:"id"`
	Session string `json:"session"`
	Role    string `json:"role"`
	TS      string `json:"ts"`
	Text    string `json:"text"`
	Kind    string `json:"kind"`
}
type Link struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}
type InlineLink struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
}
type Evidence struct {
	Source string `json:"source"`
	Quote  string `json:"quote"`
	TS     string `json:"ts,omitempty"`
	Role   string `json:"role,omitempty"`
}
type Paragraph struct {
	Text        string       `json:"text"`
	Claims      []string     `json:"claims"`
	Evidence    []Evidence   `json:"evidence"`
	InlineLinks []InlineLink `json:"inline_links"`
}
type Section struct {
	Heading    string      `json:"heading"`
	Paragraphs []Paragraph `json:"paragraphs"`
}
type Page struct {
	OriginalEvidence     []Evidence `json:"original_evidence,omitempty"`
	Revision             string     `json:"revision,omitempty"`
	HumanEditedAt        string     `json:"human_edited_at,omitempty"`
	HumanEdited          bool       `json:"human_edited,omitempty"`
	Markdown             string     `json:"manual_markdown,omitempty"`
	ID                   string     `json:"id"`
	Title                string     `json:"title"`
	Scope                string     `json:"scope"`
	Kind                 string     `json:"kind"`
	Folder               []string   `json:"folder"`
	Sections             []Section  `json:"sections"`
	Links                []Link     `json:"links"`
	Backlinks            []Link     `json:"backlinks"`
	Aliases              []string   `json:"aliases"`
	Claims               []string   `json:"claims"`
	Evidence             []Evidence `json:"evidence,omitempty"`
	NeedsEditorialUpdate bool       `json:"needs_editorial_update"`
	LearningState        string     `json:"learning_state"`
}
type Snapshot struct {
	Revision    string
	Build       BuildStatus
	Enabled     bool
	Pages       []Page
	Pending     int
	NeedsUpdate bool
	LastError   string
	CostUSD     float64
	Model       string
}
type Result struct {
	Page    Page
	Snippet string
}
type Vault struct{ root, profile string }

func hash(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
func Open(profile string) *Vault {
	return &Vault{root: filepath.Join(profile, "knowledge"), profile: profile}
}
func (v *Vault) Directory() string { return v.root }
func materializePages(vault document) ([]Page, error) {
	b, err := json.Marshal(vault)
	if err != nil {
		return nil, err
	}
	var pages []Page
	var data struct {
		Notes  []Page `json:"notes"`
		Claims []struct {
			ID       string     `json:"id"`
			Text     string     `json:"text"`
			Kind     string     `json:"kind"`
			Status   string     `json:"status"`
			Evidence []Evidence `json:"evidence"`
		} `json:"claims"`
		Sources map[string]struct {
			Role string `json:"role"`
			TS   string `json:"ts"`
		} `json:"sources"`
	}
	if e := json.Unmarshal(b, &data); e != nil {
		return nil, e
	}
	pages = data.Notes
	claimIndex := make(map[string]int, len(data.Claims))
	for i, c := range data.Claims {
		claimIndex[c.ID] = i
	}
	for i := range pages {
		p := &pages[i]
		seen := map[string]bool{}
		add := func(e Evidence) {
			key := e.Source + "\x00" + e.Quote
			if seen[key] {
				return
			}
			seen[key] = true
			if src, ok := data.Sources[e.Source]; ok {
				e.Role = src.Role
				e.TS = src.TS
			}
			p.Evidence = append(p.Evidence, e)
		}
		for _, e := range p.OriginalEvidence {
			add(e)
		}
		for si := range p.Sections {
			for pi := range p.Sections[si].Paragraphs {
				para := &p.Sections[si].Paragraphs[pi]
				cited := map[string]bool{}
				var evidence []Evidence
				cite := func(e Evidence) {
					key := e.Source + "\x00" + e.Quote
					if cited[key] {
						return
					}
					cited[key] = true
					if source, ok := data.Sources[e.Source]; ok {
						e.Role = source.Role
						e.TS = source.TS
					}
					evidence = append(evidence, e)
					add(e)
				}
				for _, e := range para.Evidence {
					cite(e)
				}
				for _, id := range para.Claims {
					if index, ok := claimIndex[id]; ok && data.Claims[index].Status == "active" {
						for _, e := range data.Claims[index].Evidence {
							cite(e)
						}
					}
				}
				para.Evidence = evidence
			}
		}
		var paragraphs []Paragraph
		for _, id := range p.Claims {
			index, ok := claimIndex[id]
			if !ok {
				continue
			}
			c := data.Claims[index]
			if c.Status != "active" {
				continue
			}
			for _, e := range c.Evidence {
				add(e)
			}
			paragraphs = append(paragraphs, Paragraph{Text: "[" + c.Kind + "] " + c.Text, Claims: []string{c.ID}, Evidence: c.Evidence})
		}
		if p.Kind != "subject" && len(p.Sections) == 0 && len(paragraphs) > 0 {
			p.Sections = []Section{{Heading: "Recorded knowledge", Paragraphs: paragraphs}}
		}
	}

	return pages, nil
}
func (v *Vault) Flush(ctx context.Context, key string, force bool) error {
	return v.compile(ctx, key, force)
}

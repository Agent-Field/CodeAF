package workstream

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/workspace"
	"github.com/Agent-Field/codeaf/internal/workstream/pagedoc"
)

const WakeItemsMax = 10

// WakeProposal contains only model suggestions; result references are engine owned.
type WakeProposal struct {
	Kind      string   `json:"kind"`
	Factory   string   `json:"factory,omitempty"`
	Job       int      `json:"job,omitempty"`
	DraftID   string   `json:"draft_id,omitempty"`
	ToWave    int      `json:"to_wave,omitempty"`
	Wave      int      `json:"wave,omitempty"`
	Title     string   `json:"title,omitempty"`
	Repo      string   `json:"repo,omitempty"`
	Text      string   `json:"text,omitempty"`
	Size      string   `json:"size,omitempty"`
	BudgetUSD *float64 `json:"budget_usd,omitempty"`
	PageID    string   `json:"page_id,omitempty"`
	Markdown  string   `json:"markdown,omitempty"`
	Why       string   `json:"why,omitempty"`
}

type WakeAnswer struct {
	Summary string
	Items   []WakeProposal
}

// ParseWakeAnswer reads the first object, including an object inside a JSON fence.
// Invalid items are dropped independently so one bad suggestion cannot hide good ones.
func ParseWakeAnswer(text string) (WakeAnswer, error) {
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return WakeAnswer{}, errors.New("no answer object")
	}
	var raw struct {
		Summary *string           `json:"summary"`
		Items   []json.RawMessage `json:"items"`
	}
	if err := json.NewDecoder(strings.NewReader(text[start:])).Decode(&raw); err != nil {
		return WakeAnswer{}, err
	}
	if raw.Summary == nil || raw.Items == nil {
		return WakeAnswer{}, errors.New("answer needs summary and items")
	}
	out := WakeAnswer{Summary: workstreamWakeClip(*raw.Summary, 2000), Items: []WakeProposal{}}
	for _, item := range raw.Items {
		var p WakeProposal
		if json.Unmarshal(item, &p) != nil {
			continue
		}
		p = workstreamWakeProposal(p)
		if !workstreamWakeValid(p) {
			continue
		}
		p.Why = workstreamWakeClip(strings.Join(strings.Fields(p.Why), " "), 200)
		out.Items = append(out.Items, p)
		if len(out.Items) == WakeItemsMax {
			break
		}
	}
	return out, nil
}

func workstreamWakeClip(text string, limit int) string {
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "")
	}
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}

func workstreamWakeValid(p WakeProposal) bool {
	switch p.Kind {
	case "move":
		job := p.Factory != "" && p.Job > 0 && p.DraftID == "" && lineWithin(p.Factory, 4096)
		draft := p.DraftID != "" && p.Factory == "" && p.Job == 0 && lineWithin(p.DraftID, 4096)
		return p.ToWave > 0 && (job || draft)
	case "draft":
		return workstreamWakeDraftValid(p)
	case "page":
		return (p.PageID != "" || validTitle(p.Title) == nil) && (p.Title == "" || validTitle(p.Title) == nil) && lineWithin(p.PageID, 4096) && textWithin(p.Markdown, DocMax) && len(pagedoc.FromMarkdown(p.Markdown)) <= DocMax
	case "decision", "question":
		return recordText(p.Kind, p.Text) == nil
	}
	return false
}

// Fields belonging to another kind are ignored, never result identities or undo targets.
func workstreamWakeProposal(p WakeProposal) WakeProposal {
	base := WakeProposal{Kind: p.Kind, Why: p.Why}
	switch p.Kind {
	case "move":
		base.Factory, base.Job, base.DraftID, base.ToWave = p.Factory, p.Job, p.DraftID, p.ToWave
	case "draft":
		base.Title, base.Repo, base.Text, base.Size, base.BudgetUSD, base.Wave = p.Title, p.Repo, p.Text, p.Size, p.BudgetUSD, p.Wave
	case "page":
		base.PageID, base.Title, base.Markdown = p.PageID, p.Title, p.Markdown
	case "decision", "question":
		base.Text = p.Text
	}
	return base
}

func workstreamWakeDraftValid(p WakeProposal) bool {
	if validTitle(p.Title) != nil || !lineWithin(p.Repo, 200) || !textWithin(p.Text, PlanDraftTextMax) || p.Wave < 0 {
		return false
	}
	if p.Size != "" && p.Size != "S" && p.Size != "M" && p.Size != "L" {
		return false
	}
	if p.BudgetUSD == nil {
		return true
	}
	b := *p.BudgetUSD
	return !math.IsNaN(b) && !math.IsInf(b, 0) && b > 0 && b <= workspace.WorkstreamGoAheadCapMax
}

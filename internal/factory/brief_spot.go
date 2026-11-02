package factory

import (
	"strconv"
	"strings"
)

// SpotHunk is one hunk of a spot as the peek draws it.
type SpotHunk struct {
	Start   int    `json:"start"`
	Symbol  string `json:"symbol,omitempty"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Text    string `json:"text"`
}

// SpotNote is one of the writer's notes, drawn as a row after the hunk it is
// on (an index into Hunks).
type SpotNote struct {
	AfterHunk int    `json:"after_hunk"`
	Text      string `json:"text"`
}

// SpotHunks is one spot of the job's newest brief with the change's hunks
// that are that spot: the hunks of its file, only those of its symbol when
// any hunk names it, in the change's order.
type SpotHunks struct {
	Spot        string     `json:"spot"`
	File        string     `json:"file"`
	Symbol      string     `json:"symbol,omitempty"`
	Why         string     `json:"why"`
	Lines       int        `json:"lines"`
	Hunks       []SpotHunk `json:"hunks"`
	Annotations []SpotNote `json:"annotations"`
	// Truncated says the job's patch was cut before its end, so a hunk of
	// this file may be missing.
	Truncated bool `json:"truncated,omitempty"`
}

// BriefSpotOf is the spot named id in the job's newest kept brief, with its
// hunks read from the job's change, and false when the brief has no such
// spot.
func BriefSpotOf(it Item, id string) (SpotHunks, bool) {
	spot, ok := latestSpot(it, id)
	if !ok {
		return SpotHunks{}, false
	}
	out := SpotHunks{Spot: id, File: spot.File, Symbol: spot.Symbol, Why: spot.Why, Lines: spot.Lines, Hunks: []SpotHunk{}, Annotations: []SpotNote{}}
	if it.Change == nil {
		return out, true
	}
	out.Truncated = it.Change.PatchTruncated
	hunks := spotHunks(PatchHunks(it.Change.Patch), spot)
	at := map[string]int{}
	for i, h := range hunks {
		out.Hunks = append(out.Hunks, SpotHunk{Start: h.Start, Symbol: h.Symbol, Added: h.Added, Removed: h.Removed, Text: h.Text})
		at[hunkRef(h)] = i
	}
	for _, n := range spot.Notes {
		if i, ok := at[n.Hunk]; ok {
			out.Annotations = append(out.Annotations, SpotNote{AfterHunk: i, Text: n.Text})
		}
	}
	return out, true
}

func latestSpot(it Item, id string) (BriefSpot, bool) {
	if n := len(it.Briefs); n > 0 {
		for _, b := range it.Briefs[n-1].Blocks {
			for _, s := range b.Spots {
				if s.ID == id {
					return s, true
				}
			}
		}
	}
	return BriefSpot{}, false
}

// spotHunks is the hunks of the spot's file; those naming its symbol when
// any does, since a spot is one place, not the whole file.
func spotHunks(all []PatchHunk, spot BriefSpot) []PatchHunk {
	var file, symbol []PatchHunk
	for _, h := range all {
		if h.File != spot.File {
			continue
		}
		file = append(file, h)
		if spot.Symbol != "" && strings.Contains(h.Symbol, spot.Symbol) {
			symbol = append(symbol, h)
		}
	}
	if len(symbol) > 0 {
		return symbol
	}
	return file
}

func hunkRef(h PatchHunk) string { return h.File + ":" + strconv.Itoa(h.Start) }

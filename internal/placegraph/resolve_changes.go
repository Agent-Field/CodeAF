package placegraph

// THE LINE A CHAT POSTS WHEN WHAT IT USES MOVES (design 6e "Always visible":
// "Adding or removing a place posts a line in the chat ('Now also using
// Marketing: brand-voice.md'), because it changes how the chat behaves").
//
// It is computed from two Bundles rather than from the mutation that caused it,
// so every cause — filing the chat, giving a parent place a parent, archiving,
// restoring, deleting — says the same kind of line, and nothing that did not
// change what the chat uses says anything.

import (
	"fmt"
	"strings"
)

// ChangeKind says which way a place moved.
type ChangeKind string

const (
	ChangeAdded   ChangeKind = "added"
	ChangeRemoved ChangeKind = "removed"
)

// Change is one place that started or stopped reaching a chat.
type Change struct {
	Kind    ChangeKind `json:"kind"`
	PlaceID string     `json:"placeId"`
	Name    string     `json:"name"`
	// Sources are the labels of the sources this place gives the chat now
	// (added only).
	Sources []string `json:"sources,omitempty"`
	// Text is the person-facing line.
	Text string `json:"text"`
}

// changeSourceNames is how many source names one line spells before it counts.
const changeSourceNames = 3

// Changes lists the places added to and removed from a chat's context between
// two resolutions: added in after's order, then removed in before's. A nil
// before is "nothing to compare with" (a conversation opening), which is not a
// change and answers nil.
func Changes(before, after *Bundle) []Change {
	if before == nil || after == nil {
		return nil
	}
	had := map[string]bool{}
	for _, p := range before.Places {
		had[p.ID] = true
	}
	has := map[string]UsedPlace{}
	for _, p := range after.Places {
		has[p.ID] = p
	}
	var out []Change
	for _, p := range after.Places {
		if had[p.ID] {
			continue
		}
		names := sourceNamesFrom(after, p.ID)
		text := "Now also using " + p.Name
		if len(p.Through) > 0 {
			if via, ok := has[p.Through[0]]; ok {
				text += " (through " + via.Name + ")"
			}
		}
		if len(names) > 0 {
			shown := names
			if len(shown) > changeSourceNames {
				shown = shown[:changeSourceNames]
			}
			text += ": " + strings.Join(shown, ", ")
			if more := len(names) - len(shown); more > 0 {
				text += fmt.Sprintf(" and %d more", more)
			}
		}
		out = append(out, Change{Kind: ChangeAdded, PlaceID: p.ID, Name: p.Name, Sources: names, Text: text})
	}
	for _, p := range before.Places {
		if _, ok := has[p.ID]; ok {
			continue
		}
		out = append(out, Change{Kind: ChangeRemoved, PlaceID: p.ID, Name: p.Name, Text: "No longer using " + p.Name})
	}
	return out
}

// sourceNamesFrom is the label (or base of the ref) of each source the bundle
// gives that the place offers.
func sourceNamesFrom(b *Bundle, placeID string) []string {
	var names []string
	for _, s := range b.Sources {
		for _, o := range s.From {
			if o.PlaceID == placeID {
				names = append(names, SourceName(s))
				break
			}
		}
	}
	return names
}

// SourceName is the short name a person reads for a source: its label, else the
// last element of its ref.
func SourceName(s UsedSource) string {
	if s.Label != "" {
		return s.Label
	}
	ref := strings.TrimRight(s.Ref, "/")
	if i := strings.LastIndexAny(ref, `/\`); i >= 0 && i < len(ref)-1 {
		return ref[i+1:]
	}
	return s.Ref
}

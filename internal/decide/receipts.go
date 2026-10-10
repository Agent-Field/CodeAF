package decide

import (
	"fmt"
	"strings"
)

// AsideKind names the transcript aside that tells a person the app answered
// for them. It is persisted bytes (a journal written today is read by builds
// that do not exist yet), so it is never respelled.
const AsideKind = "decision-receipt"

// permissionAsk is the ask kind that reads "Allowed" rather than "Decided".
// It is spelled here, not imported, because session depends on this package.
const permissionAsk = "permission"

// Why is the payload behind a receipt's "Why?" link. By is the name of the
// place whose rule decided, never its id, because a person reads it.
type Why struct {
	By         string `json:"by"`
	Because    string `json:"because,omitempty"`
	Percent    int    `json:"percent,omitempty"`
	Reversible bool   `json:"reversible"`
}

// Receipt is one decision as the transcript tells it. Question anchors the
// aside under the call or question it answered.
type Receipt struct {
	DecisionID string      `json:"decisionId"`
	Text       string      `json:"text"`
	Question   QuestionRef `json:"question"`
	Why        Why         `json:"why"`
}

// Aside is what the transcript draws: one receipt, or a group whose Text names
// how many things were done and whose Children are the single receipts.
type Aside struct {
	Kind     string    `json:"kind"`
	Text     string    `json:"text"`
	Why      *Why      `json:"why,omitempty"`
	Children []Receipt `json:"children,omitempty"`
}

// Sentence is the receipt line: "Allowed automatically by Marketing · reason ·
// Why?". An empty reason leaves its segment out rather than drawing a blank.
func Sentence(askKind, place, because string) string {
	verb := "Decided"
	if askKind == permissionAsk {
		verb = "Allowed"
	}
	if place = strings.TrimSpace(place); place == "" {
		place = "this place"
	}
	line := verb + " automatically by " + place
	if because = strings.TrimSpace(because); because != "" {
		line += " · " + because
	}
	return line + " · Why?"
}

// ReceiptOf turns a ledger row into the receipt that tells it. placeName is the
// display name of the place that decided; the row only holds its id.
func ReceiptOf(d Decision, placeName string) Receipt {
	by := strings.TrimSpace(placeName)
	if by == "" {
		by = d.By
	}
	return Receipt{
		DecisionID: d.ID,
		Text:       Sentence(d.AskKind, by, d.Because),
		Question:   d.QuestionRef,
		Why:        Why{By: by, Because: strings.TrimSpace(d.Because), Percent: d.Percent, Reversible: d.Reversible},
	}
}

// Compose draws the receipts of one turn batch as one aside: a lone receipt as
// itself, two or more as "Did 3 things" over their children. No receipts
// compose to nothing, by the emptiness law.
func Compose(rs []Receipt) (Aside, bool) {
	switch len(rs) {
	case 0:
		return Aside{}, false
	case 1:
		why := rs[0].Why
		return Aside{Kind: AsideKind, Text: rs[0].Text, Why: &why, Children: nil}, true
	}
	return Aside{
		Kind:     AsideKind,
		Text:     fmt.Sprintf("Did %d things", len(rs)),
		Children: append([]Receipt(nil), rs...),
	}, true
}

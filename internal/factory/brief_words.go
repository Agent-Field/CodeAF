package factory

import (
	"regexp"
	"strings"
)

// brief_words.go keeps machinery vocabulary out of the Brief a person reads.
// The writer is told not to use it ([BriefPlainWordsRule]), but a model told
// is not a model that obeys: the seventh live run's Brief carried a section
// headed "What the verification found". So every word the writer drafts goes
// through [BriefPlainWords] after the check and before the version is kept.
//
// THE BANNED WORDS NEVER REACH A PERSON: auditor, verdict, verified,
// verification and refuted (and unverified, the old spelling of "your call").
// The draft's JSON key "verdict" is a field name the writer answers in and is
// never shown, so it stays. A literal a person must type exactly (a proof's
// command, a file path, a step a question's action names, an evidence ref) is
// left as written: changing it would make it wrong, not plain.

// BriefPlainWordsRule is the sentence both of the writer's prompts carry.
const BriefPlainWordsRule = `Write every heading and every text in a person's plain words: NEVER use auditor, verdict, verified, verification, unverified or refuted. Say "What the checks found", "checked", "the outcome". The JSON key "verdict" is only a field name; never write that word.`

// briefBannedWord finds a banned word on its own, in any case.
var briefBannedWord = regexp.MustCompile(`(?i)\b(?:the\s+verification|unverified|verified|verifications|verification|verdicts|verdict|auditors|auditor|refuted)\b`)

// briefPlain is each banned word's plain replacement, by its lower case.
// "the verification" reads as "the checks" because what a verification found
// is what the checks found; on its own a verification is one check.
var briefPlain = map[string]string{
	"the verification": "the checks",
	"unverified":       "unchecked",
	"verified":         "checked",
	"verifications":    "checks",
	"verification":     "check",
	"verdicts":         "outcomes",
	"verdict":          "outcome",
	"auditors":         "reviewers",
	"auditor":          "reviewer",
	"refuted":          "disproved",
}

// BriefPlainWords is s with every banned word replaced by its plain word,
// keeping the case it was written in: Title stays Title, ALL CAPS stays caps.
func BriefPlainWords(s string) string {
	return briefBannedWord.ReplaceAllStringFunc(s, func(m string) string {
		key := strings.Join(strings.Fields(strings.ToLower(m)), " ")
		plain, ok := briefPlain[key]
		if !ok {
			return m
		}
		switch {
		case m == strings.ToUpper(m):
			return strings.ToUpper(plain)
		case m[:1] == strings.ToUpper(m[:1]):
			return strings.ToUpper(plain[:1]) + plain[1:]
		}
		return plain
	})
}

// briefPlainBlock puts a block's person-read words through [BriefPlainWords].
// A diagram's mermaid and its node labels are left alone: a label is matched
// against the graph's own text, and a node id that happened to be a banned
// word would stop the graph drawing.
func briefPlainBlock(b *BriefBlock) {
	b.Heading = BriefPlainWords(b.Heading)
	b.Rest = BriefPlainWords(b.Rest)
	for i := range b.Bullets {
		b.Bullets[i].Text = BriefPlainWords(b.Bullets[i].Text)
	}
	for i := range b.Flow {
		b.Flow[i].Label = BriefPlainWords(b.Flow[i].Label)
		for j := range b.Flow[i].Nodes {
			b.Flow[i].Nodes[j].Text = BriefPlainWords(b.Flow[i].Nodes[j].Text)
		}
	}
	if b.Table != nil {
		for i := range b.Table.Columns {
			b.Table.Columns[i] = BriefPlainWords(b.Table.Columns[i])
		}
		for _, row := range b.Table.Rows {
			for j := range row {
				row[j] = BriefPlainWords(row[j])
			}
		}
	}
	for i := range b.Spots {
		b.Spots[i].Why = BriefPlainWords(b.Spots[i].Why)
		for j := range b.Spots[i].Notes {
			b.Spots[i].Notes[j].Text = BriefPlainWords(b.Spots[i].Notes[j].Text)
		}
	}
	for i := range b.Questions {
		b.Questions[i].Text = BriefPlainWords(b.Questions[i].Text)
		if a := b.Questions[i].Action; a != nil {
			a.Label = BriefPlainWords(a.Label)
		}
	}
	for i := range b.Options {
		b.Options[i] = BriefPlainWords(b.Options[i])
	}
	for i := range b.Shots {
		b.Shots[i].Label = BriefPlainWords(b.Shots[i].Label)
	}
}

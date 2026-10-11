package localembed

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// maxWordChars is the WordPiece ceiling the model's tokenizer.json states
// (max_input_chars_per_word): a longer word is one unknown token.
const maxWordChars = 100

// continuation is the prefix of every word-piece that is not the first.
const continuation = "##"

// specials are the added tokens the tokenizer matches in the RAW text before
// it normalises anything, so a title that literally contains "[CLS]" keeps
// the id Python gives it. [UNK] is among them and is dropped by the caller.
var specials = []string{"[PAD]", "[UNK]", "[CLS]", "[SEP]", "[MASK]"}

// wordPiece is a BERT tokeniser built from the vocabulary alone: clean,
// space out CJK, strip accents, lowercase, split on whitespace and
// punctuation, then match each word greedily longest-first. It reproduces the
// model's tokenizer.json (BertNormalizer + BertPreTokenizer + WordPiece), and
// testdata/tokens.json pins that against the Python tokenizer.
type wordPiece struct {
	ids map[string]int32
	unk int32
}

// newWordPiece indexes the vocabulary, given in id order.
func newWordPiece(tokens []string) *wordPiece {
	w := &wordPiece{ids: make(map[string]int32, len(tokens)), unk: -1}
	for i, t := range tokens {
		w.ids[t] = int32(i)
	}
	if u, ok := w.ids["[UNK]"]; ok {
		w.unk = u
	}
	return w
}

// Encode returns the token ids of text with unknown words left out, which is
// what model2vec does before pooling.
func (w *wordPiece) Encode(text string) []int32 {
	var out []int32
	for len(text) > 0 {
		at, tok := nextSpecial(text)
		if at < 0 {
			return w.encodePlain(text, out)
		}
		out = w.encodePlain(text[:at], out)
		if id := w.ids[tok]; id != w.unk {
			out = append(out, id)
		}
		text = text[at+len(tok):]
	}
	return out
}

// nextSpecial finds the earliest literal special token in text.
func nextSpecial(text string) (int, string) {
	best, tok := -1, ""
	for _, s := range specials {
		if i := strings.Index(text, s); i >= 0 && (best < 0 || i < best) {
			best, tok = i, s
		}
	}
	return best, tok
}

func (w *wordPiece) encodePlain(text string, out []int32) []int32 {
	for _, word := range preTokenize(normalize(text)) {
		out = w.piece(word, out)
	}
	return out
}

// piece appends the greedy longest-match pieces of one word, or nothing when
// any part of the word cannot be matched.
func (w *wordPiece) piece(word string, out []int32) []int32 {
	runes := []rune(word)
	if len(runes) > maxWordChars {
		return out
	}
	start, mark := 0, len(out)
	for start < len(runes) {
		end, found := len(runes), int32(-1)
		for end > start {
			sub := string(runes[start:end])
			if start > 0 {
				sub = continuation + sub
			}
			if id, ok := w.ids[sub]; ok {
				found = id
				break
			}
			end--
		}
		if found < 0 {
			return out[:mark]
		}
		out = append(out, found)
		start = end
	}
	return out
}

// normalize is BertNormalizer with lowercase on: drop control characters,
// turn every whitespace into a space, pad CJK ideographs with spaces,
// decompose and drop combining marks, then lowercase.
func normalize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == 0 || r == 0xFFFD || isControl(r):
			continue
		case unicode.IsSpace(r):
			b.WriteByte(' ')
		case isCJK(r):
			b.WriteByte(' ')
			b.WriteRune(r)
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	var out strings.Builder
	for _, r := range norm.NFD.String(b.String()) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		out.WriteRune(unicode.ToLower(r))
	}
	return out.String()
}

// isControl matches the characters BERT cleaning removes: controls and
// format characters, except the three that are whitespace.
func isControl(r rune) bool {
	if r == '\t' || r == '\n' || r == '\r' {
		return false
	}
	return unicode.In(r, unicode.Cc, unicode.Cf)
}

// isCJK is the ideograph block list BERT spaces out.
func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x20000 && r <= 0x2A6DF) || (r >= 0x2A700 && r <= 0x2B73F) ||
		(r >= 0x2B740 && r <= 0x2B81F) || (r >= 0x2B820 && r <= 0x2CEAF) ||
		(r >= 0xF900 && r <= 0xFAFF) || (r >= 0x2F800 && r <= 0x2FA1F)
}

// preTokenize splits on whitespace and makes every punctuation character a
// word of its own (BertPreTokenizer).
func preTokenize(s string) []string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = cur[:0]
		}
	}
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			flush()
		case isPunct(r):
			flush()
			words = append(words, string(r))
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return words
}

// isPunct is BERT punctuation: every ASCII symbol that is not alphanumeric,
// plus Unicode punctuation.
func isPunct(r rune) bool {
	if (r >= 33 && r <= 47) || (r >= 58 && r <= 64) || (r >= 91 && r <= 96) || (r >= 123 && r <= 126) {
		return true
	}
	return unicode.IsPunct(r)
}

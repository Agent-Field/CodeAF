package heft

// localeCompare reproduces V8's String.prototype.localeCompare under the
// default collator (ICU/CLDR root data, locale en-US, sensitivity "variant",
// alternate=non-ignorable) for the repertoire that HEFT task and model IDs
// realistically use: printable ASCII plus a handful of spaces and format
// controls.
//
// Byte order is NOT a usable stand-in. The CLDR root collation places "_"
// BEFORE "-", places every punctuation mark before the digits, and sorts
// lowercase before uppercase at the tertiary level, so strings.Compare
// disagrees with localeCompare on exactly the adjacencies model IDs hit
// ("gpt-4.1" vs "gpt_4.1", "Model" vs "model"). heft.ts calls localeCompare in
// two places -- the rank-tie task-priority sort and the EFT-tie model choice --
// so a wrong comparator silently reorders whole schedules.
//
// Model: a "completely ignorable" code point contributes nothing at any level;
// every other code point in the repertoire contributes one primary and one
// tertiary weight. (Every secondary weight in this repertoire is equal, so the
// UCA secondary level is a no-op here and is omitted.) Two strings compare by
// primary weight sequence, then by tertiary weight sequence, with the shorter
// sequence sorting first when one is a prefix of the other.
//
// The weights were extracted from bun 1.2.23's Intl.Collator using sensitivity
// base/accent/variant, then validated against 66,000 randomly generated
// localeCompare pairs with zero mismatches.
//
// DIVERGENCE (documented; the fixtures stay inside the repertoire): code points
// outside the repertoire fall back to collationFallbackBase + code point at the
// primary level, which sorts them after every modelled character. Real ICU
// interleaves them (emoji, for instance, sort before "z") and applies
// contractions/expansions such as "ss" for the sharp s that no per-character
// table can express.

const collationFallbackBase = 1000

type collationWeight struct {
	primary  int32
	tertiary int32
}

// collationIgnorableRunes are "completely ignorable" in the root collation:
// they contribute no weight at any level, so a NUL sandwiched inside an ID
// makes that ID collate EQUAL to the same ID without it.
var collationIgnorableRunes = []rune{
	0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
	0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
	0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
	0x7f, 0xad, 0x200b, 0x200c, 0x200d, 0x2060, 0xfeff,
}

// collationPrimaryGroups lists the repertoire in primary-weight order. Runes
// inside one group share a primary weight (the ASCII letter pairs "aA".."zZ",
// and the space group U+0020 / U+3000 / U+2009 / U+00A0); a rune's position
// inside its group is its tertiary weight.
var collationPrimaryGroups = [][]rune{
	{'\t'}, {'\n'}, {'\v'}, {'\f'}, {'\r'},
	{'\u0020', '\u3000', '\u2009', '\u00a0'},
	{'_'}, {'-'}, {','}, {';'}, {':'}, {'!'}, {'?'}, {'.'}, {'\''}, {'"'},
	{'('}, {')'}, {'['}, {']'}, {'{'}, {'}'}, {'@'}, {'*'}, {'/'}, {'\\'},
	{'&'}, {'#'}, {'%'}, {'`'}, {'^'},
	{'+'}, {'<'}, {'='}, {'>'}, {'|'}, {'~'}, {'$'},
	{'0'}, {'1'}, {'2'}, {'3'}, {'4'}, {'5'}, {'6'}, {'7'}, {'8'}, {'9'},
	{'a', 'A'}, {'b', 'B'}, {'c', 'C'}, {'d', 'D'}, {'e', 'E'}, {'f', 'F'},
	{'g', 'G'}, {'h', 'H'}, {'i', 'I'}, {'j', 'J'}, {'k', 'K'}, {'l', 'L'},
	{'m', 'M'}, {'n', 'N'}, {'o', 'O'}, {'p', 'P'}, {'q', 'Q'}, {'r', 'R'},
	{'s', 'S'}, {'t', 'T'}, {'u', 'U'}, {'v', 'V'}, {'w', 'W'}, {'x', 'X'},
	{'y', 'Y'}, {'z', 'Z'},
}

var (
	collationIgnorable = map[rune]bool{}
	collationTable     = map[rune]collationWeight{}
)

func init() {
	for _, r := range collationIgnorableRunes {
		collationIgnorable[r] = true
	}
	for primary, group := range collationPrimaryGroups {
		for tertiary, r := range group {
			collationTable[r] = collationWeight{primary: int32(primary), tertiary: int32(tertiary)}
		}
	}
}

// collationKey returns the primary and tertiary weight sequences of s.
func collationKey(s string) (primary []int32, tertiary []int32) {
	for _, r := range s {
		if collationIgnorable[r] {
			continue
		}
		w, ok := collationTable[r]
		if !ok {
			w = collationWeight{primary: collationFallbackBase + int32(r), tertiary: 0}
		}
		primary = append(primary, w.primary)
		tertiary = append(tertiary, w.tertiary)
	}
	return primary, tertiary
}

func compareWeightSeq(a, b []int32) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	if len(a) == len(b) {
		return 0
	}
	if len(a) < len(b) {
		return -1
	}
	return 1
}

// localeCompare is `a.localeCompare(b)`: -1, 0 or 1.
func localeCompare(a, b string) int {
	if a == b {
		return 0
	}
	pa, ta := collationKey(a)
	pb, tb := collationKey(b)
	if c := compareWeightSeq(pa, pb); c != 0 {
		return c
	}
	return compareWeightSeq(ta, tb)
}

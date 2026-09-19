package approval

import "testing"

func TestFirstCompositionOutsideQuotesShellProperties(t *testing.T) {
	const measured = `grep -n "dialTimeout\|waitFor\"Host\|func Dial" notes/a-folder-with-a-long-name/and-another-one-under-it/and-a-third-beneath-that/the-fourth-and-the-last/walls-and-the-notes-kept-beside-them-and-the-n.md`
	tests := []struct {
		name     string
		line     string
		want     byte
		composed bool
	}{
		{"double quoted backslash bar pair is text", measured, 0, false},
		{"escaped double quote does not close", `"a\" ; b"`, 0, false},
		{"double quoted backslash pair", `"a\\b"`, 0, false},
		{"single quotes make every byte text", `'a\|$()'`, 0, false},
		{"ordinary quoted separators", `echo "a;|&<>"`, 0, false},
		{"literal backslash then closed quote exposes semicolon", `"a\\" ; b`, ';', true},
		{"expansion in double quotes", `"a $(b) c"`, '$', true},
		{"backtick in double quotes", "\"a `b` c\"", '`', true},
		{"trailing backslash uncertain", `echo a\`, '\\', true},
		{"backslash newline in double quotes uncertain", "\"a\\\nb\"", '\\', true},
		{"backslash newline outside uncertain", "a\\\nb", '\\', true},
		{"single quote closes after literal backslash", `'a\' ; b`, ';', true},
		{"unclosed single quote", `'abc`, '\'', true},
		{"unclosed double quote", `"abc`, '"', true},
		{"escaped dollar is text", `"a\$b"`, 0, false},
		{"escaped backtick is text", "\"a\\`b\"", 0, false},
		{"outside backslash remains conservative", `echo a\ b`, '\\', true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, composed := FirstCompositionOutsideQuotes(tt.line)
			if got != tt.want || composed != tt.composed {
				t.Fatalf("FirstCompositionOutsideQuotes(%q) = (%q, %v), want (%q, %v)", tt.line, got, composed, tt.want, tt.composed)
			}
		})
	}
}

func TestFirstBarOutsideQuotesMirrorsScanner(t *testing.T) {
	const measured = `grep -n "dialTimeout\|waitFor\"Host\|func Dial" notes/a-folder-with-a-long-name/and-another-one-under-it/and-a-third-beneath-that/the-fourth-and-the-last/walls-and-the-notes-kept-beside-them-and-the-n.md`
	tests := []struct {
		name string
		line string
		want int
	}{
		{"quoted backslash bar pair", measured, -1},
		{"escaped quote keeps bar quoted", `"a\" | b"`, -1},
		{"single quoted backslash then quoted bar", `'a\'" | b"`, -1},
		{"literal backslash closes before real bar", `"a\\" | b`, len(`"a\\" `)},
		{"ordinary real bar", `echo a | b`, len(`echo a `)},
		{"outside backslash uncertainty fails before bar", `echo a\ b | c`, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FirstBarOutsideQuotes(tt.line); got != tt.want {
				t.Fatalf("FirstBarOutsideQuotes(%q) = %d, want %d", tt.line, got, tt.want)
			}
		})
	}
}

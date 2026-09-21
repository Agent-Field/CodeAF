package prose

import (
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
)

// Go and markdown, built rather than parsed.
//
// Every other language in the curated set is XML, so it can be embedded and
// parsed lazily like the dependency parses its own. These two are not: the
// dependency ships Go and markdown as Go source, and there is no XML of them to
// copy. They are reconstructed here out of the same rule tables, and built on
// first use with the rest of the set, so they cost nothing at init and the most
// common fence in this repository keeps its colour.
//
// The rule tables are copies of the dependency's, unchanged, and are marked here
// as such rather than quietly edited: a hand-widened rule is a claim about
// somebody's source, and the whole point of copying is that the claim is the one
// already made everywhere else.

// goTemplate is the one part of Go highlighting that is not a rule of the Go
// lexer: the inside of a raw string, which is Go template when it is a template.
// Its definition IS XML — `embedded/go_template.xml` — so it is parsed lazily
// with everything else.
var goTemplate = sync.OnceValue(func() chroma.Lexer {
	lexer, err := chroma.NewXMLLexer(curatedFS, "embedded/go_template.xml")
	if err != nil {
		return nil
	}
	return lexer.SetConfig(&chroma.Config{
		Name:    "Go Text Template",
		Aliases: []string{"go-text-template"},
	})
})

// newGoLexer builds the Go lexer for the curated set.
func newGoLexer() chroma.Lexer {
	return chroma.MustNewLexer(&chroma.Config{
		Name:      "Go",
		Aliases:   []string{"go", "golang"},
		Filenames: []string{"*.go"},
		MimeTypes: []string{"text/x-gosrc"},
	}, goRules)
}

// goRules is the dependency's Go rule table, unchanged.
func goRules() chroma.Rules {
	// A raw string's inside is Go template when it is one and a plain string when
	// it is not: the template lexer is remapped so everything it does not name
	// still reads as a string, leaving an ordinary raw string untouched.
	var rawInside chroma.Emitter = chroma.LiteralString
	if template := goTemplate(); template != nil {
		rawInside = chroma.UsingLexer(chroma.TypeRemappingLexer(
			template,
			chroma.TypeMapping{{From: chroma.Other, To: chroma.LiteralString}},
		))
	}
	return chroma.Rules{
		"root": {
			{Pattern: `\n`, Type: chroma.TextWhitespace},
			{Pattern: `\s+`, Type: chroma.TextWhitespace},
			{Pattern: `//[^\s\n\r][^\n\r]*`, Type: chroma.CommentPreproc},
			{Pattern: `//[^\n\r]*`, Type: chroma.CommentSingle},
			{Pattern: `/(\\\n)?[*](.|\n)*?[*](\\\n)?/`, Type: chroma.CommentMultiline},
			{Pattern: `(import|package)\b`, Type: chroma.KeywordNamespace},
			{Pattern: chroma.Words(``, `\b`, `break`, `default`, `select`, `case`, `defer`, `go`, `else`, `goto`, `switch`, `fallthrough`, `if`, `range`, `continue`, `for`, `return`), Type: chroma.Keyword},
			{Pattern: `(true|false|iota|nil)\b`, Type: chroma.KeywordConstant},
			{Pattern: chroma.Words(``, `\b(\()`, `uint`, `uint8`, `uint16`, `uint32`, `uint64`, `int`, `int8`, `int16`, `int32`, `int64`, `float`, `float32`, `float64`, `complex64`, `complex128`, `byte`, `rune`, `string`, `bool`, `error`, `uintptr`, `print`, `println`, `panic`, `recover`, `close`, `complex`, `real`, `imag`, `len`, `cap`, `append`, `copy`, `delete`, `new`, `make`, `clear`, `min`, `max`), Type: chroma.ByGroups(chroma.NameBuiltin, chroma.Punctuation)},
			{Pattern: chroma.Words(``, `\b`, `uint`, `uint8`, `uint16`, `uint32`, `uint64`, `int`, `int8`, `int16`, `int32`, `int64`, `float`, `float32`, `float64`, `complex64`, `complex128`, `byte`, `rune`, `string`, `bool`, `error`, `uintptr`, `any`), Type: chroma.KeywordType},
			{Pattern: `\d+i`, Type: chroma.LiteralNumber},
			{Pattern: `\d+\.\d*([Ee][-+]\d+)?i`, Type: chroma.LiteralNumber},
			{Pattern: `\.\d+([Ee][-+]\d+)?i`, Type: chroma.LiteralNumber},
			{Pattern: `\d+[Ee][-+]\d+i`, Type: chroma.LiteralNumber},
			{Pattern: `\d+(\.\d+[eE][+\-]?\d+|\.\d*|[eE][+\-]?\d+)`, Type: chroma.LiteralNumberFloat},
			{Pattern: `\.\d+([eE][+\-]?\d+)?`, Type: chroma.LiteralNumberFloat},
			{Pattern: `0[0-7]+`, Type: chroma.LiteralNumberOct},
			{Pattern: `0[xX][0-9a-fA-F_]+`, Type: chroma.LiteralNumberHex},
			{Pattern: `0b[01_]+`, Type: chroma.LiteralNumberBin},
			{Pattern: `(0|[1-9][0-9_]*)`, Type: chroma.LiteralNumberInteger},
			{Pattern: `'(\\['"\\abfnrtv]|\\x[0-9a-fA-F]{2}|\\[0-7]{1,3}|\\u[0-9a-fA-F]{4}|\\U[0-9a-fA-F]{8}|[^\\])'`, Type: chroma.LiteralStringChar},
			{Pattern: "(`)([^`]*)(`)", Type: chroma.ByGroups(chroma.LiteralString, rawInside, chroma.LiteralString)},
			{Pattern: `"(\\\\|\\"|[^"])*"`, Type: chroma.LiteralString},
			{Pattern: `(<<=|>>=|<<|>>|<=|>=|&\^=|&\^|\+=|-=|\*=|/=|%=|&=|\|=|&&|\|\||<-|\+\+|--|==|!=|:=|\.\.\.|[+\-*/%&])`, Type: chroma.Operator},
			{Pattern: `([a-zA-Z_]\w*)(\s*)(\()`, Type: chroma.ByGroups(chroma.NameFunction, chroma.UsingSelf("root"), chroma.Punctuation)},
			{Pattern: `[|^<>=!()\[\]{}.,;:~]`, Type: chroma.Punctuation},
			{Pattern: `[^\W\d]\w*`, Type: chroma.NameOther},
		},
	}
}

// newMarkdownLexer builds the markdown lexer for the curated set.
func newMarkdownLexer() chroma.Lexer {
	return &markdownLexer{Lexer: chroma.MustNewLexer(&chroma.Config{
		Name:      "markdown",
		Aliases:   []string{"md", "mkd"},
		Filenames: []string{"*.md", "*.mkd", "*.markdown"},
		MimeTypes: []string{"text/x-markdown"},
	}, markdownRules)}
}

// markdownLexer is the dependency's markdown lexer: the base rules, with a
// leading YAML frontmatter block handed to the YAML lexer first.
type markdownLexer struct {
	chroma.Lexer
}

// Tokenise highlights a leading YAML frontmatter block with the YAML lexer
// before delegating the rest of the document to the markdown rules.
func (m *markdownLexer) Tokenise(options *chroma.TokeniseOptions, text string) (chroma.Iterator, error) {
	frontmatter, rest, ok := splitFrontmatter(text)
	if !ok {
		return m.Lexer.Tokenise(options, text)
	}
	yaml := curatedGet("yaml")
	if yaml == nil {
		return m.Lexer.Tokenise(options, text)
	}
	yamlTokens, err := yaml.Tokenise(options, frontmatter)
	if err != nil {
		return nil, err
	}
	markdownTokens, err := m.Lexer.Tokenise(options, rest)
	if err != nil {
		return nil, err
	}
	return chroma.Concaterator(yamlTokens, markdownTokens), nil
}

// splitFrontmatter extracts a leading YAML frontmatter block, if the document
// starts with one. It is the dependency's, unchanged.
func splitFrontmatter(text string) (frontmatter string, rest string, ok bool) {
	if !strings.HasPrefix(text, "---\n") && !strings.HasPrefix(text, "---\r\n") {
		return "", text, false
	}
	lineEnd := strings.IndexByte(text, '\n')
	if lineEnd < 0 {
		return "", text, false
	}
	if strings.TrimSuffix(text[:lineEnd], "\r") != "---" {
		return "", text, false
	}
	for pos := lineEnd + 1; pos < len(text); {
		next := strings.IndexByte(text[pos:], '\n')
		if next < 0 {
			break
		}
		lineEnd = pos + next
		line := strings.TrimSuffix(text[pos:lineEnd], "\r")
		if line == "---" {
			return text[:lineEnd+1], text[lineEnd+1:], true
		}
		pos = lineEnd + 1
	}
	return "", text, false
}

// markdownRules is the dependency's markdown rule table, unchanged.
func markdownRules() chroma.Rules {
	return chroma.Rules{
		"root": {
			{Pattern: `<!--[\w\W]*?-->`, Type: chroma.CommentMultiline},
			{Pattern: `^(#[^#].+\n)`, Type: chroma.ByGroups(chroma.GenericHeading)},
			{Pattern: `^(#{2,6}.+\n)`, Type: chroma.ByGroups(chroma.GenericSubheading)},
			{Pattern: `^(\s*)([*-] )(\[[ xX]\])( .+\n)`, Type: chroma.ByGroups(chroma.Text, chroma.Keyword, chroma.Keyword, chroma.UsingSelf("inline"))},
			{Pattern: `^(\s*)([*-])(\s)(.+\n)`, Type: chroma.ByGroups(chroma.Text, chroma.Keyword, chroma.Text, chroma.UsingSelf("inline"))},
			{Pattern: `^(\s*)([0-9]+\.)( .+\n)`, Type: chroma.ByGroups(chroma.Text, chroma.Keyword, chroma.UsingSelf("inline"))},
			{Pattern: `^(\s*>\s)(.+\n)`, Type: chroma.ByGroups(chroma.Keyword, chroma.GenericEmph)},
			{Pattern: "^(```\\n)([\\w\\W]*?)(^```$)", Type: chroma.ByGroups(chroma.String, chroma.Text, chroma.String)},
			{Pattern: "^(```)(\\w+)(\\n)([\\w\\W]*?)(^```$)", Type: chroma.UsingByGroup(2, 4, chroma.String, chroma.String, chroma.String, chroma.Text, chroma.String)},
			chroma.Include("inline"),
		},
		"inline": {
			{Pattern: `<!--[\w\W]*?-->`, Type: chroma.CommentMultiline},
			{Pattern: `\\.`, Type: chroma.Text},
			{Pattern: `(\s)(\*|_)((?:(?!\2).)*)(\2)((?=\W|\n))`, Type: chroma.ByGroups(chroma.Text, chroma.GenericEmph, chroma.GenericEmph, chroma.GenericEmph, chroma.Text)},
			{Pattern: `(\s)((\*\*|__).*?)\3((?=\W|\n))`, Type: chroma.ByGroups(chroma.Text, chroma.GenericStrong, chroma.GenericStrong, chroma.Text)},
			{Pattern: `(\s)(~~[^~]+~~)((?=\W|\n))`, Type: chroma.ByGroups(chroma.Text, chroma.GenericDeleted, chroma.Text)},
			{Pattern: "`[^`]+`", Type: chroma.LiteralStringBacktick},
			{Pattern: `[@#][\w/:]+`, Type: chroma.NameEntity},
			{Pattern: `(!?\[)([^]]+)(\])(\()([^)]+)(\))`, Type: chroma.ByGroups(chroma.Text, chroma.NameTag, chroma.Text, chroma.Text, chroma.NameAttribute, chroma.Text)},
			{Pattern: `.|\n`, Type: chroma.Text},
		},
	}
}

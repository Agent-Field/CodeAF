package prose

import (
	"embed"
	"sync"
	"sync/atomic"

	"github.com/alecthomas/chroma/v2"
)

// The curated lexer set.
//
// The highlighter hangs off one dependency, and that dependency's own lexer
// package parses every one of its 279 embedded definitions from XML in a package
// initializer — before main, and whether or not a single block is ever drawn. On
// the cold-start benchmark that parse is about seven of the twelve milliseconds
// `codeaf --version` costs, a toll every run of every command pays and none of
// them repays.
//
// So codeaf owns a small set instead: the languages a fenced block or a file
// preview actually reaches, embedded here as the same XML the dependency keeps,
// and parsed LAZILY. Nothing is read at package init; the set is built once, on
// the first block or filename that asks for it. A language outside the set is a
// MISS — it draws as plain text and raises nothing, which is the degradation the
// callers already make for a nil lexer.
//
// Two curated languages, Go and markdown, are not XML in that dependency at all;
// it ships them as Go code. They keep their behaviour by being built here, lazily
// and out of the same rule tables, in [lexers_builtin.go]. Without them the most
// common fence in this repository would lose its colour, which is the one thing
// the set must not do.
//
// The whole set is built before any member of it runs, because a definition may
// delegate to another BY NAME: `html.xml` leans on CSS and Javascript,
// `docker.xml` on Bash and JSON, `makefile.xml` on Bash. A definition parsed
// alone whose delegate was never registered would tokenise as text, so the first
// use loads every file — each once — rather than being finer-grained than the
// delegation allows.

// curatedFS carries the set's XML where the loader can reach it. The pattern is
// the manner the dependency embeds the same files, so a file moved here from
// there keeps its path byte for byte.
//
//go:embed embedded/*.xml
var curatedFS embed.FS

// curatedEntry is one language in the set.
//
// What reaches an entry — its name, its aliases, and the file globs Match
// answers for — is the Config inside its own definition, read when the entry
// loads. There is deliberately no second table of names here: a copy of names
// that already exist is a copy that drifts, and the lookup drives the aliases
// each definition declares.
type curatedEntry struct {
	// path is the embedded XML, empty when build is set.
	path string
	// build constructs a lexer the dependency ships as Go source rather than XML.
	build func() chroma.Lexer
}

// curatedEntries is the set, in registration order.
//
// The order matters in exactly one place. `hcl.xml` and `terraform.xml` both
// claim the alias `hcl`, and registration leaves the later of the two holding it,
// so Terraform is registered after HCL and a ```hcl fence reaches what it reaches
// in the dependency the set replaces.
var curatedEntries = []curatedEntry{
	{path: "embedded/bash.xml"},
	{path: "embedded/c.xml"},
	{path: "embedded/c++.xml"},
	{path: "embedded/c#.xml"},
	{path: "embedded/css.xml"},
	{path: "embedded/dart.xml"},
	{path: "embedded/diff.xml"},
	{path: "embedded/docker.xml"},
	{path: "embedded/elixir.xml"},
	{path: "embedded/graphql.xml"},
	{path: "embedded/haskell.xml"},
	{path: "embedded/hcl.xml"},
	{path: "embedded/html.xml"},
	{path: "embedded/ini.xml"},
	{path: "embedded/java.xml"},
	{path: "embedded/javascript.xml"},
	{path: "embedded/json.xml"},
	{path: "embedded/kotlin.xml"},
	{path: "embedded/lua.xml"},
	{path: "embedded/makefile.xml"},
	{path: "embedded/nix.xml"},
	{path: "embedded/objective-c.xml"},
	{path: "embedded/perl.xml"},
	{path: "embedded/php.xml"},
	{path: "embedded/plaintext.xml"},
	{path: "embedded/protocol_buffer.xml"},
	{path: "embedded/python.xml"},
	{path: "embedded/r.xml"},
	{path: "embedded/ruby.xml"},
	{path: "embedded/rust.xml"},
	{path: "embedded/scala.xml"},
	{path: "embedded/sql.xml"},
	{path: "embedded/swift.xml"},
	{path: "embedded/terraform.xml"},
	{path: "embedded/toml.xml"},
	{path: "embedded/typescript.xml"},
	{path: "embedded/xml.xml"},
	{path: "embedded/yaml.xml"},
	{path: "embedded/zig.xml"},
	{build: newGoLexer},
	{build: newMarkdownLexer},
}

// curatedParses counts the definitions parsed from XML. It exists for the
// laziness test, which fails if it is not zero before the first use — the probe
// that catches an eager parse somebody re-adds at init.
var curatedParses atomic.Int64

// loadCuratedXML parses one embedded definition. A file that will not parse is a
// MISS like any language outside the set: nil, and the caller draws it plain.
func loadCuratedXML(path string) chroma.Lexer {
	curatedParses.Add(1)
	lexer, err := chroma.NewXMLLexer(curatedFS, path)
	if err != nil {
		return nil
	}
	return lexer
}

// curatedRegistry is the built set, behind the one sync.Once that makes it
// lazy: first use builds it, later uses find it, and no use at all pays nothing.
var curatedRegistry struct {
	once sync.Once
	reg  *chroma.LexerRegistry
}

// curated is the set, built once on first use.
func curated() *chroma.LexerRegistry {
	curatedRegistry.once.Do(func() {
		reg := chroma.NewLexerRegistry()
		for _, entry := range curatedEntries {
			var lexer chroma.Lexer
			if entry.build != nil {
				lexer = entry.build()
			} else {
				lexer = loadCuratedXML(entry.path)
			}
			if lexer != nil {
				reg.Register(lexer)
			}
		}
		curatedRegistry.reg = reg
	})
	return curatedRegistry.reg
}

// curatedGet resolves the word a fence's info string carries — a language name,
// an alias, or a bare file extension such as `yml` — to a lexer, or nil when the
// set has nothing for it. It is the dependency's own lookup, over the curated
// set: name, then alias, then the extension match that makes ```yml reach YAML.
func curatedGet(name string) chroma.Lexer {
	return curated().Get(name)
}

// curatedMatch resolves a FILENAME to the curated language it is written in, or
// nil. It is the dependency's own Match, over the curated set: the same globs,
// the same priority ordering, the same ignored editor suffixes.
func curatedMatch(filename string) chroma.Lexer {
	return curated().Match(filename)
}

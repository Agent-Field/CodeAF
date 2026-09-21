---
kind: changed
title: the curated lexer set loads on first use, not all 279 at init
pr: 1193
surface: [chat]
invalidates:
  - "Highlighting reached chroma's whole lexer registry, whose package initializer parsed all 279 of its embedded XML definitions — about seven of the twelve milliseconds a cold start cost — before main and whether or not a block was ever drawn. codeaf now owns a set of about forty languages, embedded as the same XML and parsed lazily on first use, so nothing is parsed at package init."
  - "A fenced block in a language outside the set, and an unlabelled one, were highlighted by chroma's fallback lexer or guessed from the source by text analysis (`lexers.Analyse`). Both are now plain text with no error, which is exactly what a nil lexer already drew."
  - "`internal/tui2/prose` imported `github.com/alecthomas/chroma/v2/lexers`; it no longer does. The four call sites there — `lexers.Get`, `lexers.Analyse`, `lexers.Fallback` in `highlightPieces`, and `lexers.Match` in `LexerName` — now go through the curated set, which is the only thing chroma's XML is reached by."
---

**Cold start, measured on Spark the benchmark's way** — `docs/benchmarks/measure-cli.sh`
startup phase, best-of-7 `codeaf --version` after 2 warmups, both binaries built from
this tree (the parent `f4079d716` and the change on top of it, so the pair differs
only by this work), measured BACK TO BACK in a quiet window — **1-min load 0.50
throughout, well under the load-2 ceiling; no figure here without its load** — three
rounds each:

| round | before best | before median | after best | after median |
| --- | --- | --- | --- | --- |
| 1 | 11.2 ms | 12.6 ms | **7.8 ms** | 8.6 ms |
| 2 | 10.7 ms | 17.7 ms | **8.3 ms** | 8.9 ms |
| 3 | 11.7 ms | 18.1 ms | **7.9 ms** | 11.3 ms |

Headline: **before ~11 ms, after ~8 ms** (best-of-7, the middle of each side's three
rounds) — about 3 ms of startup gone. The whole 279-lexer init parse is ~7 ms, but a
`--version` start pays only the part the process actually reaches before exit; the
binary-size drop below shows the rest of the removed cost. Every row above was taken
at 1-min load 0.50.

**Size.** Both binaries are `make build` of this tree — the parent commit `f4079d716`
without the change, and the change on top of it — so the pair differs only by this
work: **53,739,785 → 51,511,561 bytes**, 2,228,224 bytes (2.2 MB) the binary no longer
carries. Of that, 1,657,714 bytes is embedded XML: the dependency's 279 definitions
weigh 1,975,839 bytes and the curated 40 weigh 318,125. The remaining 570,510 bytes is
the dependency's compiled lexer package — its registry initializer and the lexers it
ships as Go source — which nothing in this tree imports any more.

**Why it is lazy and not per-language.** Every file in the set is parsed the first
time any block or filename needs one, and never again. It is not loaded one
language at a time because a definition can delegate to another BY NAME —
`html.xml` leans on CSS and Javascript, `docker.xml` on Bash and JSON,
`makefile.xml` on Bash — and a definition parsed alone whose delegate was not
registered would tokenise the delegated region as text. So first use builds the
whole set, each file once; nothing is parsed at init.

**The set** (names and aliases come from each definition's own config, so there is
no second table to drift): bash, c, c++, c#, css, dart, diff, docker/dockerfile,
elixir, go, graphql, haskell, hcl, html, ini, java, javascript, json, kotlin, lua,
makefile, markdown, nix, objective-c, perl, php, plaintext, protobuf, python, r,
ruby, rust, scala, sql, swift, terraform, toml, typescript, xml, yaml, zig. Go and
markdown are the two chroma ships as Go source rather than XML, so they keep their
highlighting from a local lazy definition built out of the same rule tables;
without them the most common fence in this repository would lose its colour.

A language outside the set is a MISS: plain text, no error, which the callers
already degraded to. `LexerName` still answers "" for an uncurated filename, so a
file preview falls back to what it drew before rather than being painted by a
fallback lexer as if it were code.

The laziness is pinned: `internal/tui2/prose/lexers_test.go` fails before any test
runs if the registry is not empty at init, so a re-added eager parse — a package
variable that calls the loader — is caught rather than silently restoring the
cost this removes. That is not a claim about the test, it is a run of it: adding
`var _ = curatedGet("go")` to the package makes `TestMain` exit non-zero before a
single test reports, printing `the curated lexer set parsed 39 definition(s) from
XML before any use; the registry is eager`; deleting that one line returns the
package to green.

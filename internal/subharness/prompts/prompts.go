// Package prompts is where the designer's brief LIVES, as prose, so that the
// hardest writing in this system is edited as writing rather than as a builder
// full of Fprintf calls.
//
// WHY AN ASSET AND NOT A STRING BUILDER: the brief is a meta-guide — it teaches a
// derivation procedure, and a procedure is argued in paragraphs. Held in Go it was
// held in fragments, every line wearing quotes and an escape, and the shape of the
// argument was invisible to the person changing it. Held here it is a document,
// diffable as a document, readable by a person who is not going to open a compiler.
//
// WHAT IS STILL DERIVED: every cap, both ladders and the tool belt. They are
// «placeholders», filled by [Render] from the package that owns the numbers, and a
// placeholder with nobody to fill it is an ERROR rather than a stray brace in a
// prompt — so a guide that has drifted from the machinery it describes fails at
// load, in the first second of a run, instead of being noticed in a bad design.
//
// The guillemets are the point of the delimiter choice: the guide has to quote the
// runtime's own `{{input}}` verbatim, so the two vocabularies must not collide.
package prompts

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"
)

// Designer is the meta-guide: the machinery, the derivation procedure, the output
// contract. Unrendered — see [Render].
//
//go:embed designer.md
var Designer string

// Reviewer is stage 1.5's addendum. It is APPENDED to the rendered guide rather
// than standing alone, because a critic that cannot see the law it is judging
// against would be reviewing its own recollection of it — and the checklists in it
// are the guide's own steps, turned into questions.
//
//go:embed reviewer.md
var Reviewer string

// placeholder is what Render fills and what it refuses to leave behind.
var placeholder = regexp.MustCompile(`«[a-z_]+»`)

// Render fills a guide's placeholders. An unknown placeholder left in the text is
// an error and so is a value nothing asked for: the first means the guide is
// describing machinery the caller did not supply, the second means the caller is
// supplying machinery the guide stopped describing. Both are drift, and drift in
// this file is the failure mode the whole arrangement exists to prevent.
func Render(guide string, values map[string]string) (string, error) {
	used := map[string]bool{}
	out := placeholder.ReplaceAllStringFunc(guide, func(found string) string {
		name := strings.Trim(found, "«»")
		value, ok := values[name]
		if !ok {
			return found
		}
		used[name] = true
		return value
	})
	if left := placeholder.FindAllString(out, -1); len(left) > 0 {
		return "", fmt.Errorf("the guide asks for %s and nothing supplied %s", strings.Join(dedupe(left), ", "), plural(len(dedupe(left)), "it", "them"))
	}
	var unused []string
	for name := range values {
		if !used[name] {
			unused = append(unused, "«"+name+"»")
		}
	}
	if len(unused) > 0 {
		return "", fmt.Errorf("nothing in the guide reads %s", strings.Join(sorted(unused), ", "))
	}
	return out, nil
}

func dedupe(words []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, word := range words {
		if !seen[word] {
			seen[word] = true
			out = append(out, word)
		}
	}
	return out
}

func sorted(words []string) []string {
	out := append([]string(nil), words...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

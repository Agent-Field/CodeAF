package approval

import "strings"

// ── the read-only look table ────────────────────────────────────────────────
//
// A CLI signed in to the person's account asks every time (accounts.go). A
// look — `gh pr view 7`, `gh issue list` — changes nothing at the far end, so
// a person can say yes to that family's looks for one task at once
// (internal/session's ConsentTask). Everything this file does serves that one
// answer, and it is written to FAIL CLOSED:
//
//   - IT IS AN ALLOWLIST AND NOTHING ELSE. A line is a look only when every
//     character, every word and every flag is one this file names. There is no
//     list of dangerous things here; whatever is not recognised asks again.
//   - THE LINE IS READ AS THE SHELL WOULD SPLIT IT ONLY BECAUSE NOTHING THE
//     SHELL REWRITES IS ALLOWED IN IT. No quote, backslash, glob, expansion,
//     redirect, separator, tab or environment prefix survives [lookCharset], so
//     splitting on spaces is the argv the program receives.
//   - ONLY gh, tonight. aws and gcloud have verbs that read at the far end and
//     write here, or print secrets; they are not in the table.

// lookCharset is every byte a look may contain. Letters, digits, single
// spaces and the punctuation of a flag, a repository, a path and a number.
const lookCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 -_./:=@,+"

// lookSpec is one subcommand that only reads: the flags it may carry (true
// when the flag takes a value) and how many positional words it accepts.
type lookSpec struct {
	flags      map[string]bool
	minArgs    int
	maxArgs    int
	positional func(word string) bool
}

// flagSet reads a table row: space-separated flags, a trailing ':' marking
// one that takes a value.
func flagSet(rows ...string) map[string]bool {
	set := map[string]bool{}
	for _, row := range rows {
		for _, word := range strings.Fields(row) {
			set[strings.TrimSuffix(word, ":")] = strings.HasSuffix(word, ":")
		}
	}
	return set
}

const (
	ghRepoFlags = "-R: --repo:"
	ghJSONFlags = "--json: -q: --jq: -t: --template:"
)

// ghLooks is every gh invocation that is a look, keyed by its subcommand
// words. A subcommand, flag or flag spelling that is not here asks again.
var ghLooks = map[string]lookSpec{
	"issue view":    {flags: flagSet(ghRepoFlags, ghJSONFlags, "-c --comments"), minArgs: 1, maxArgs: 1},
	"issue list":    {flags: flagSet(ghRepoFlags, ghJSONFlags, "-a: --assignee: -A: --author: -l: --label: -L: --limit: --mention: -m: --milestone: -S: --search: -s: --state: --app:")},
	"issue status":  {flags: flagSet(ghRepoFlags, ghJSONFlags)},
	"pr view":       {flags: flagSet(ghRepoFlags, ghJSONFlags, "-c --comments"), maxArgs: 1},
	"pr list":       {flags: flagSet(ghRepoFlags, ghJSONFlags, "-a: --assignee: -A: --author: -B: --base: -d --draft -H: --head: -l: --label: -L: --limit: -S: --search: -s: --state: --app:")},
	"pr status":     {flags: flagSet(ghRepoFlags, ghJSONFlags, "-c --conflict-status")},
	"pr diff":       {flags: flagSet(ghRepoFlags, "--color: --name-only --patch"), maxArgs: 1},
	"pr checks":     {flags: flagSet(ghRepoFlags, ghJSONFlags, "--required"), maxArgs: 1},
	"repo view":     {flags: flagSet(ghJSONFlags, "-b: --branch:"), maxArgs: 1},
	"repo list":     {flags: flagSet(ghJSONFlags, "-L: --limit: -l: --language: --visibility: --source --fork --archived --no-archived --topic:"), maxArgs: 1},
	"release view":  {flags: flagSet(ghRepoFlags, ghJSONFlags), maxArgs: 1},
	"release list":  {flags: flagSet(ghRepoFlags, ghJSONFlags, "-L: --limit: --exclude-drafts --exclude-pre-releases -O: --order:")},
	"run view":      {flags: flagSet(ghRepoFlags, ghJSONFlags, "--log --log-failed -j: --job: -a: --attempt: -v --verbose --exit-status"), maxArgs: 1},
	"run list":      {flags: flagSet(ghRepoFlags, ghJSONFlags, "-L: --limit: -b: --branch: -e: --event: -s: --status: -u: --user: -w: --workflow: -c: --commit: --created: -a --all")},
	"workflow view": {flags: flagSet(ghRepoFlags, "-y --yaml -r: --ref:"), maxArgs: 1},
	"workflow list": {flags: flagSet(ghRepoFlags, ghJSONFlags, "-L: --limit: -a --all")},
	"label list":    {flags: flagSet(ghRepoFlags, ghJSONFlags, "-L: --limit: -S: --search: --order: --sort:")},
	// `gh api` is a GET of one REST path. A body flag, a header, any method
	// spelling but the three below, and the graphql endpoint are simply not in
	// the table.
	"api": {flags: flagSet("-q: --jq: -t: --template: --paginate --slurp -i --include --silent --verbose --cache: -p: --preview: -X: --method:"), minArgs: 1, maxArgs: 1, positional: restPath},
}

// lookFlagValues pins the value of a flag that is only a look with one value.
var lookFlagValues = map[string]string{"-X": "GET", "--method": "GET"}

// restPath is a REST path on the signed-in host: it never names the graphql
// endpoint, which can carry a mutation, and never a URL of its own.
func restPath(word string) bool {
	return !strings.Contains(strings.ToLower(word), "graphql") && !strings.Contains(word, ":")
}

// ReadOnlyLook reports the family ("gh") of a command line that is a look by
// the table above. Any line the table does not recognise whole is not one.
func ReadOnlyLook(command string) (family string, ok bool) {
	command, ok = withoutStderrJoin(command)
	if !ok {
		return "", false
	}
	if strings.ContainsFunc(command, func(r rune) bool { return !strings.ContainsRune(lookCharset, r) }) {
		return "", false
	}
	words := strings.Fields(command)
	if len(words) < 2 || words[0] != "gh" {
		return "", false
	}
	spec, rest, found := lookSpecFor(words[1:])
	if !found || !spec.accepts(rest) {
		return "", false
	}
	return "gh", true
}

// lookSpecFor finds the spec named by the leading one or two words.
func lookSpecFor(words []string) (lookSpec, []string, bool) {
	for n := min(2, len(words)); n >= 1; n-- {
		if spec, ok := ghLooks[strings.Join(words[:n], " ")]; ok {
			return spec, words[n:], true
		}
	}
	return lookSpec{}, nil, false
}

// accepts walks the argv after the subcommand: every dash word is a flag in
// the table in its exact spelling, every value is present and is not itself a
// dash word, and the positionals fit.
func (s lookSpec) accepts(words []string) bool {
	args := 0
	for i := 0; i < len(words); i++ {
		word := words[i]
		if !strings.HasPrefix(word, "-") {
			if s.positional != nil && !s.positional(word) {
				return false
			}
			args++
			continue
		}
		name, value, joined := strings.Cut(word, "=")
		takesValue, known := s.flags[name]
		if !known || joined && !(takesValue && strings.HasPrefix(name, "--")) {
			return false
		}
		if takesValue && !joined {
			i++
			if i >= len(words) {
				return false
			}
			value = words[i]
		}
		if !lookValueOK(name, value, takesValue) {
			return false
		}
	}
	return args >= s.minArgs && args <= s.maxArgs
}

// lookValueOK refuses an empty or dash-led value and a pinned flag carrying
// anything but its one value.
func lookValueOK(name, value string, takesValue bool) bool {
	if !takesValue {
		return true
	}
	if value == "" || strings.HasPrefix(value, "-") {
		return false
	}
	pinned, ok := lookFlagValues[name]
	return !ok || value == pinned
}

// stderrJoin is the one wrapper a look may carry: a single trailing ` 2>&1`,
// which only folds the program's own error stream into its output. It is cut
// once, as exact bytes; the rest must pass the charset and the table as it
// stands, so a second one, another redirect or anything after it still asks.
const stderrJoin = " 2>&1"

// withoutStderrJoin cuts one trailing [stderrJoin]. The text before it may
// not end in white space: `gh pr list  2>&1` is not the shape that was ruled on.
func withoutStderrJoin(command string) (string, bool) {
	rest, cut := strings.CutSuffix(command, stderrJoin)
	if !cut {
		return command, true
	}
	return rest, rest != "" && !strings.HasSuffix(rest, " ")
}

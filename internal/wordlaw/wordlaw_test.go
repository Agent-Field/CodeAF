// Package wordlaw is THE WORDS LAW, and it is a package of nothing but the law.
//
// Three spellings were retired on 2026-11-23, and a string a person reads may
// not bring any of them back:
//
//   - "Continue here" was the label for carrying a chat from another computer to
//     this one. The act has one verb, "Move here", wherever a person meets it.
//   - "sync service" was the TUI's name for the server. People call it the relay.
//   - "sync storage" was the name of the switch that lets the relay keep sealed
//     copies. It is "relay storage".
//
// And one word is fenced: "relay" names the server and its storage switch, and
// NOTHING ELSE. It is never the name of a feature or something a person does (a
// person does not "relay a chat"; they move it, or open it live). Because a
// count of strings cannot tell a server from a feature, the second half of the
// law is a ledger: every string in the program that says "relay" is listed in
// relay_words.golden with a class a person gave it, and a new one fails until a
// person classifies it. The chat manual's own count of the word is a ratchet
// that may only fall.
//
// The law reads every string literal in the program's own sources, because a
// person reads the string and not the constant it hangs from, and it fails
// naming the file, the line and the spelling. Comments are not strings and are
// not read here. Test files are not read either: a test that asserts a person-
// facing string asserts the NEW spelling. The two manual corpora are read as
// text, wrapped lines and all, because a sentence the manual breaks across two
// lines is still one sentence.
//
// IT LIVES IN ITS OWN PACKAGE FOR THE SAME REASON iconlaw DOES: a word is not one
// surface's law, and a list of roots here is how a new surface joins it. It
// imports go/ast, so `make test-laws` finds it and runs it on every pull request.
//
// To classify a new string, or to lower the manual's baseline after a page lost
// some mentions:
//
//	go test ./internal/wordlaw -run Relay -update
//
// and read the diff of the two .golden files before committing it.
package wordlaw

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite relay_words.golden and relay_manual.golden from the tree")

// goRoots are the trees whose string literals a person can read: the commands
// and every package under internal. third_party is vendored code and never ours.
var goRoots = []string{"../../cmd", ".."}

// corpora are the two Markdown manuals a person (or the model, on a person's
// behalf) reads. The resident's pages are a separate corpus and say none of this,
// and they are held to the same law so that stays true.
var corpora = []string{"../manual/chat", "../manual/pages"}

// workerRoot is the hosted relay's own source, which is JavaScript.
const workerRoot = "../../relay/hosted/src"

// legacyHeading is THE ONE PLACE "continue here" may be written in the manual.
// Everything a person might ask in the old words reaches the Move page through
// this heading, and nowhere else spells the label: a section that offered it
// would send people looking for a button that is gone.
const (
	legacyHeadingPage = "continuing-a-chat-on-another-computer.md"
	legacyHeading     = `## Move here: also called "continue here", "take over" or "continue on this machine"`
)

// kept are the few Go literals that may still carry a retired spelling, each
// with the words it may carry. A search alias is typed by a person and never
// drawn, so it is how somebody who remembers the old label still finds the row
// it named; config.LegacyWords is the one slice that holds them.
var kept = map[string][]string{
	"internal/config/settings_presentation.go": {"continue here", "sync storage"},
}

// continueHere matches the retired label and the sentences that wrapped it:
// `Continue here`, `continue it here`, `Continue this chat here?`, `continue the
// chat here`, `continuing here…` and `Continue %s's latest chat here?`. It does
// not match "a task that cannot be continued here", which is the task verb and a
// different feature.
var continueHere = spaced(`\bcontinu(?:e|ing) (?:(?:this|the) chat |it )?here\b|\bcontinue \S{1,30}'s latest chat here\b`)

// syncWords matches the two retired names for the server and its storage.
var syncWords = spaced(`\bsync service\b|\bsync storage\b`)

// relayAsAFeature matches "relay" used as a verb or as the name of a feature. A
// server plural ("both relays") and the server's own nouns are fine; a person
// never "relays a chat", and there is no "relay mode".
var relayAsAFeature = spaced(`\b(?:relayed|relaying|relay mode|relay feature|relays? (?:a|the|your) (?:chat|chats|message|messages|answer|conversation)|relay (?:a|your) (?:chat|chats|message|messages))\b`)

// spaced compiles a case-insensitive pattern whose spaces match any run of
// whitespace, so a phrase the manual wraps across two lines is still found.
func spaced(pattern string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)` + strings.ReplaceAll(pattern, " ", `\s+`))
}

// goString is one string literal in the program and where it stands.
type goString struct {
	path string // repository-relative, forward slashes
	line int
	text string
}

// programStrings reads every non-test Go file under goRoots and returns each
// string literal except an import path, which is a name for the compiler and not
// a sentence for a person.
func programStrings(t *testing.T) []goString {
	t.Helper()
	var out []goString
	files := 0
	for _, root := range goRoots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case "testdata", "third_party", "wordlaw":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			files++
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Errorf("%s: %v", path, err)
				return nil
			}
			rel := repoRelative(path)
			ast.Inspect(file, func(n ast.Node) bool {
				switch lit := n.(type) {
				case *ast.ImportSpec:
					return false
				case *ast.BasicLit:
					if lit.Kind != token.STRING {
						return true
					}
					text, err := strconv.Unquote(lit.Value)
					if err != nil {
						return true
					}
					out = append(out, goString{path: rel, line: fset.Position(lit.Pos()).Line, text: text})
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if files < 500 {
		t.Fatalf("the law read only %d Go files; its roots no longer reach the program", files)
	}
	return out
}

// page is one Markdown file of a manual corpus.
type page struct {
	corpus string // "chat" or "pages"
	name   string // file name, e.g. "home.md"
	text   string
}

func manualPages(t *testing.T) []page {
	t.Helper()
	var out []page
	for _, root := range corpora {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(root, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, page{corpus: filepath.Base(root), name: entry.Name(), text: string(raw)})
		}
	}
	if len(out) < 90 {
		t.Fatalf("the law read only %d manual pages; its roots no longer reach the corpora", len(out))
	}
	return out
}

// lineOf is the 1-based line of a byte offset.
func lineOf(text string, offset int) int { return strings.Count(text[:offset], "\n") + 1 }

// lineAt is the text of the line holding a byte offset.
func lineAt(text string, offset int) string {
	start := strings.LastIndex(text[:offset], "\n") + 1
	end := strings.Index(text[offset:], "\n")
	if end < 0 {
		return text[start:]
	}
	return text[start : offset+end]
}

func allowed(rel, word string) bool {
	for _, w := range kept[rel] {
		if w == word {
			return true
		}
	}
	return false
}

// TestSyncServiceIsGone is the law for the two names of the server and its
// storage that are retired: Go strings, both manuals and the hosted relay's own
// JavaScript. The manual's three old-name headings are the one exception, and
// they are an exception because a question in the old words reaches a page by its
// heading: a heading naming both "relay storage" and "sync storage" is how
// somebody who remembers the old switch still finds the page that answers.
func TestSyncServiceIsGone(t *testing.T) {
	for _, s := range programStrings(t) {
		lower := strings.ToLower(s.text)
		for _, word := range []string{"sync service", "sync storage"} {
			if strings.Contains(lower, word) && !allowed(s.path, word) {
				t.Errorf("%s:%d spells %q in a string a person can read; the server is the relay and its switch is relay storage", s.path, s.line, word)
			}
		}
	}
	for _, p := range manualPages(t) {
		for _, at := range syncWords.FindAllStringIndex(p.text, -1) {
			line := lineAt(p.text, at[0])
			word := strings.ToLower(strings.Join(strings.Fields(p.text[at[0]:at[1]]), " "))
			if word == "sync storage" && strings.HasPrefix(line, "## ") && strings.Contains(strings.ToLower(line), "relay storage") {
				continue
			}
			t.Errorf("manual/%s/%s:%d says %q; the server is the relay and its switch is relay storage", p.corpus, p.name, lineOf(p.text, at[0]), word)
		}
	}
	err := filepath.WalkDir(workerRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".js") {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, at := range syncWords.FindAllStringIndex(string(raw), -1) {
			t.Errorf("%s:%d says %q; the server is the relay", repoRelative(path), lineOf(string(raw), at[0]), strings.ToLower(string(raw[at[0]:at[1]])))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestContinueHereSurvivesOnlyInOneManualHeading is the law for the old label.
// Go strings may carry it only in the Settings search alias list; the manual may
// carry it only in the one heading that answers a question asked in the old
// words, and that heading must still stand.
func TestContinueHereSurvivesOnlyInOneManualHeading(t *testing.T) {
	for _, s := range programStrings(t) {
		if found := continueHere.FindString(s.text); found != "" && !allowed(s.path, strings.ToLower(found)) {
			t.Errorf("%s:%d spells %q in a string a person can read; say \"Move here\" instead", s.path, s.line, found)
		}
	}
	headings := 0
	for _, p := range manualPages(t) {
		text := p.text
		if p.corpus == "chat" && p.name == legacyHeadingPage {
			headings += strings.Count(text, "\n"+legacyHeading+"\n")
			// BLANK THE HEADING RATHER THAN CUT IT, so every later line number is
			// still the line number in the file.
			text = strings.ReplaceAll(text, "\n"+legacyHeading+"\n", "\n"+strings.Repeat(" ", len(legacyHeading))+"\n")
		}
		for _, at := range continueHere.FindAllStringIndex(text, -1) {
			t.Errorf("manual/%s/%s:%d offers %q; the act is \"Move here\", and the old words live only in the heading %q",
				p.corpus, p.name, lineOf(text, at[0]), strings.Join(strings.Fields(text[at[0]:at[1]]), " "), legacyHeading)
		}
	}
	if headings != 1 {
		t.Errorf("%s must hold the heading %s exactly once, and holds it %d times: a question in the old words reaches the Move page through it", legacyHeadingPage, legacyHeading, headings)
	}
}

// The relay classes. A string that says "relay" is one of these.
const (
	classServer   = "server"        // the server a person runs or reaches: its address, its refusals, its commands
	classStorage  = "storage"       // the switch that lets it keep sealed copies
	classFeature  = "other-feature" // a different feature that happens to say the word: today, only the model pool
	classPrompt   = "model-prompt"  // an instruction to the model that uses the plain verb ("relay it"); not a name for anything
	relayGolden   = "relay_words.golden"
	manualGolden  = "relay_manual.golden"
	goldenComment = "#"
)

// modelPoolPaths are the only places "relay" may name something other than the
// server: the model pool has a relay of its own (a different feature, with its
// own address and its own key), and it is out of this rename's scope.
var modelPoolPaths = []string{
	"cmd/codeaf/pool.go",
	"internal/pool/",
	"internal/config/settings.go",
	"internal/tui3/settings.go",
}

// modelPromptPaths are the only places the plain verb may stand in an
// instruction to the model. These sentences are read by the model, not by a
// person, and the session package pins their text in its law registry, so they
// are reworded in a prompt wave of their own rather than inside a words pass;
// they sit in the ledger so that a fourth one fails the build.
var modelPromptPaths = []string{"internal/session/"}

func isModelPromptPath(path string) bool {
	for _, p := range modelPromptPaths {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func isModelPoolPath(path string) bool {
	for _, p := range modelPoolPaths {
		if path == p || (strings.HasSuffix(p, "/") && strings.HasPrefix(path, p)) {
			return true
		}
	}
	return false
}

// relayKey is the identity of one ledger line: the file and the text, never the
// line number, so an edit above a string does not move it.
func relayKey(path, text string) string { return path + "\t" + strconv.Quote(text) }

// readGolden parses a .golden file into its lines, skipping comments.
func readGolden(t *testing.T, name string) []string {
	t.Helper()
	f, err := os.Open(name)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		if line := scanner.Text(); line != "" && !strings.HasPrefix(line, goldenComment) {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

func writeGolden(t *testing.T, name, header string, lines []string) {
	t.Helper()
	sort.Strings(lines)
	body := header + "\n" + strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestNoPersonFacingStringNamesRelayAsAFeature holds the fence round the word
// "relay". Contract: "relay" names the server and its storage switch.
//
//   - Every Go string that says it is in relay_words.golden with a class. A new one
//     fails until a person has said which: server, storage, or (only in the model
//     pool's own files) other-feature, or (only in the session package's prompts)
//     model-prompt, the plain verb in an instruction the model reads.
//   - A line in the ledger whose string is gone fails too, so the ledger is a
//     picture of the program and not a pile.
//   - No chat manual page uses "relay" as a verb or the name of a feature.
func TestNoPersonFacingStringNamesRelayAsAFeature(t *testing.T) {
	type found struct {
		path string
		line int
		text string
	}
	current := map[string]found{}
	for _, s := range programStrings(t) {
		if strings.Contains(strings.ToLower(s.text), "relay") {
			key := relayKey(s.path, s.text)
			if _, seen := current[key]; !seen {
				current[key] = found{s.path, s.line, s.text}
			}
		}
	}
	classes := map[string]string{} // key -> class
	for _, line := range readGolden(t, relayGolden) {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			t.Fatalf("%s: a ledger line is `class<TAB>path<TAB>quoted text`, not %q", relayGolden, line)
		}
		classes[parts[1]] = parts[0]
	}

	if *update {
		var lines []string
		for key, f := range current {
			class := classes[key]
			if class == "" {
				class = classServer
				if strings.Contains(strings.ToLower(f.text), "storage") {
					class = classStorage
				}
			}
			lines = append(lines, class+"\t"+key)
		}
		writeGolden(t, relayGolden, `# Every non-test Go string in cmd and internal that says "relay", with the class a person gave it.
# server = the server (its address, commands, refusals); storage = the switch that lets it keep sealed copies;
# other-feature = a different feature that says the word (only the model pool's own files).
# model-prompt = an instruction to the model using the plain verb ("relay it"), only in internal/session; to be reworded.
# Regenerate with: go test ./internal/wordlaw -run Relay -update   Then read the diff.
# Format: class<TAB>path<TAB>quoted text`, lines)
		return
	}

	var problems []string
	for key, f := range current {
		class, ok := classes[key]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("%s:%d says \"relay\" and nobody has classified it: %s\n\t\tRun `go test ./internal/wordlaw -run Relay -update`, then read the diff and set its class (server, storage or other-feature).", f.path, f.line, strconv.Quote(f.text)))
		case class != classServer && class != classStorage && class != classFeature && class != classPrompt:
			problems = append(problems, fmt.Sprintf("%s:%d is classed %q; the classes are %s, %s, %s and %s", f.path, f.line, class, classServer, classStorage, classFeature, classPrompt))
		case class == classFeature && !isModelPoolPath(f.path):
			problems = append(problems, fmt.Sprintf("%s:%d is classed %s, but \"relay\" names a feature only in the model pool's files (%s); it names the server and its storage switch", f.path, f.line, classFeature, strings.Join(modelPoolPaths, ", ")))
		case class == classPrompt && !isModelPromptPath(f.path):
			problems = append(problems, fmt.Sprintf("%s:%d is classed %s, but only the session package's prompts (%s) may use \"relay\" as a plain verb; say \"pass it on\" or \"tell the person\"", f.path, f.line, classPrompt, strings.Join(modelPromptPaths, ", ")))
		}
	}
	for key := range classes {
		if _, ok := current[key]; !ok {
			problems = append(problems, fmt.Sprintf("%s lists a string that is gone: %s\n\t\tRun `go test ./internal/wordlaw -run Relay -update` to drop it.", relayGolden, key))
		}
	}
	sort.Strings(problems)
	for _, p := range problems {
		t.Error(p)
	}

	for _, p := range manualPages(t) {
		for _, at := range relayAsAFeature.FindAllStringIndex(p.text, -1) {
			t.Errorf("manual/%s/%s:%d uses %q; relay names only the server and its storage switch, never a feature or something a person does",
				p.corpus, p.name, lineOf(p.text, at[0]), strings.Join(strings.Fields(p.text[at[0]:at[1]]), " "))
		}
	}
}

// newPages are the chat manual pages that may appear without a baseline: they
// are written after the ratchet was set, and say what the word means as they go.
var newPages = map[string]bool{
	"remote-control.md": true,
}

// TestTheRelayWordInTheChatManualOnlyFalls is the ratchet. Contract: the chat
// manual says "relay" for the server in 424 places, and a person reading it
// should meet fewer of them over time, not more. A page's count may fall and may
// not rise; a page with no baseline must be a new page.
//
// `-update` writes the lower of the two for every page that already had a
// baseline, so it can tighten the ratchet and never loosen it, and it adds a page
// that is new.
func TestTheRelayWordInTheChatManualOnlyFalls(t *testing.T) {
	counts := map[string]int{}
	for _, p := range manualPages(t) {
		if p.corpus == "chat" {
			counts[p.name] = strings.Count(strings.ToLower(p.text), "relay")
		}
	}
	baseline := map[string]int{}
	for _, line := range readGolden(t, manualGolden) {
		parts := strings.Split(line, "\t")
		n, err := strconv.Atoi(parts[len(parts)-1])
		if len(parts) != 2 || err != nil {
			t.Fatalf("%s: a baseline line is `page<TAB>count`, not %q", manualGolden, line)
		}
		baseline[parts[0]] = n
	}

	if *update {
		var lines []string
		for name, n := range counts {
			if old, ok := baseline[name]; ok && old < n {
				n = old
			}
			lines = append(lines, fmt.Sprintf("%s\t%d", name, n))
		}
		writeGolden(t, manualGolden, `# How many times each chat manual page says "relay" (any case, any form: relay, relays, relayed).
# A page's count may fall and may not rise. Regenerate with: go test ./internal/wordlaw -run Relay -update
# Format: page<TAB>count`, lines)
		return
	}

	var names []string
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		old, ok := baseline[name]
		switch {
		case !ok && newPages[name]:
		case !ok:
			t.Errorf("manual/chat/%s says \"relay\" %d times and has no baseline. A page that is not new must be in %s: run `go test ./internal/wordlaw -run Relay -update` and read the diff", name, counts[name], manualGolden)
		case counts[name] > old:
			t.Errorf("manual/chat/%s says \"relay\" %d times, up from %d. The word names the server; say \"your other computers\", \"live\" or \"relay storage\" where the sentence is about what a person does", name, counts[name], old)
		}
	}
	for name := range baseline {
		if _, ok := counts[name]; !ok {
			t.Errorf("%s lists manual/chat/%s, which is gone; run `go test ./internal/wordlaw -run Relay -update` to drop it", manualGolden, name)
		}
	}
}

// TestTheRetiredPatternsMatchWhatTheyShouldAndNothingElse gives the other tests
// their teeth. A law whose pattern silently stopped matching would pass forever,
// so each spelling the law fences is written down here with a sentence it must
// catch (including one the manual wraps over two lines) and a sentence it must
// leave alone.
func TestTheRetiredPatternsMatchWhatTheyShouldAndNothingElse(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pattern *regexp.Regexp
		hits    []string
		misses  []string
	}{
		{"the old label", continueHere,
			[]string{"Continue here", "press continue here to resume", "Continue this chat here?", "continue the chat here first", "continue it here", "continuing here…", "Continue tower's latest chat here?", "beside every Continue\nhere"},
			[]string{"Move here", "so it cannot be continued here", "a kept run that continues here later", "continue in the working chat", "Continue the plan's remaining steps"}},
		{"the old names for the server and its storage", syncWords,
			[]string{"the sync service", "Sync storage is off", "hosted sync\nstorage", "SYNC SERVICE"},
			[]string{"relay storage", "the relay", "sync space", "your chats sync", "a sync in progress"}},
		{"relay as a feature or an act", relayAsAFeature,
			[]string{"the node relays the answer", "it relayed your message", "relaying other people's words", "relay mode", "relay your chat", "the Factory manager relays\nthe answer"},
			[]string{"both relays", "the relay at tower", "your relay stores sealed text", "a self-hosted relay", "relay storage"}},
	} {
		for _, text := range tc.hits {
			if !tc.pattern.MatchString(text) {
				t.Errorf("%s: the law would miss %q", tc.name, text)
			}
		}
		for _, text := range tc.misses {
			if tc.pattern.MatchString(text) {
				t.Errorf("%s: the law would wrongly flag %q", tc.name, text)
			}
		}
	}
}

// repoRelative turns a path found under ../.. or .. into the repository-relative
// spelling the kept and class tables use.
func repoRelative(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	clean = strings.TrimPrefix(clean, "../../")
	if strings.HasPrefix(clean, "../") {
		clean = "internal/" + strings.TrimPrefix(clean, "../")
	}
	return clean
}

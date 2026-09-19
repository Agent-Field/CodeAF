package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/calllog"
	"github.com/Agent-Field/codeaf/internal/callrows"
	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// ── THE INSTRUMENT'S OWN LAW ────────────────────────────────────────────────
//
// A replay is an argument about a counterfactual, and an argument about a
// counterfactual cannot be checked against the world. So it is checked against a
// log somebody WROTE, where what the best machine is, what the served policy
// cost, and what a deliberately wrong policy costs are all known before the
// program runs.
//
// `testdata/calls.jsonl` is fifty requests on one model over an hour, served
// round-robin by three machines whose behaviour is planted:
//
//	Steady    200 ms to the first token, 100 tokens a second, always
//	Bimodal   the same, alternating with 20 s and 2 tokens a second — the shape
//	          a median cannot see and a tail policy exists for
//	Slow      4 s and 5 tokens a second, always
//
// with five arms of a hedge that was cut off, one paced pool that names the
// machine that refused inside its own sentence and carries no `served` column,
// and one machine seen exactly once.

const fixture = "testdata/calls.jsonl"

func fixtureWorld(t *testing.T) (*world, []asked) {
	t.Helper()
	rows, err := callrows.Read(fixture)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	requests, _ := requestsOf(rows)
	return newWorld(rows, defaultWindow), requests
}

// talkShape is what the fixture's requests ask for: a hundred tokens somebody
// reads, which is what every expectation below is computed over.
var talkShape = shape{visible: 100}

func TestTheOracleIsTheMachineThatWasPlantedFastest(t *testing.T) {
	measured, requests := fixtureWorld(t)
	picked := map[string]int{}
	for _, one := range requests {
		best, felt := measured.best(one.model, one.at, talkShape, true, one.id)
		if math.IsInf(felt, 0) {
			continue
		}
		picked[best]++
	}
	if picked["Steady"] == 0 {
		t.Fatalf("the oracle never picked the machine planted fastest: %v", picked)
	}
	for machine, times := range picked {
		if machine != "Steady" {
			t.Errorf("the oracle picked %s %d times; only Steady is ever the best machine here", machine, times)
		}
	}
}

func TestATriviallyWrongPolicyPaysTheRegretItWasBuiltToPay(t *testing.T) {
	measured, requests := fixtureWorld(t)
	// The planted difference, from lane.PerceivedSeconds over a hundred read
	// tokens: Steady is 0.2 s + nothing (it writes faster than anybody reads),
	// Slow is 4 s + 100·(1/5 − 1/18) = 18.4 s. A policy that demands Slow every
	// time is therefore owed about eighteen seconds a request and nothing else.
	var wrong, served []float64
	for _, one := range requests {
		best, bestFelt := measured.best(one.model, one.at, talkShape, true, one.id)
		if best == "" || math.IsInf(bestFelt, 0) {
			continue
		}
		if felt := measured.felt(lane.ID{Model: one.model, Lane: "Slow"}, one.at, talkShape, true, one.id); !math.IsInf(felt, 0) {
			wrong = append(wrong, felt-bestFelt)
		}
		if one.machine == "" {
			continue
		}
		if felt := measured.felt(lane.ID{Model: one.model, Lane: one.machine}, one.at, talkShape, true, one.id); !math.IsInf(felt, 0) {
			served = append(served, felt-bestFelt)
		}
	}
	// It is a band and not a point because the fixture's one paced pool is
	// credited to Slow, and the windows that contain it divide Slow's wait by the
	// share of requests it answered — the availability fold, which is part of the
	// quantity and not noise in it. The floor is the planted 18.2 s and the
	// ceiling is that figure over five answers in six.
	if got := mean(wrong); got < 18 || got > 23 {
		t.Errorf("a policy that always demands Slow: mean regret %.2fs, want between 18.2s and 22.2s", got)
	}
	if mean(served) >= mean(wrong) {
		t.Errorf("what actually served (%.2fs) is not cheaper than the worst machine (%.2fs)", mean(served), mean(wrong))
	}
	if mean(served) <= 0 {
		t.Errorf("what actually served paid no regret at all (%.2fs), and the fixture serves Slow a third of the time", mean(served))
	}
}

func TestAWatchedRoleIsPricedOnTheTailAndAnUnwatchedOneOnTheMedian(t *testing.T) {
	measured, requests := fixtureWorld(t)
	// The bimodal machine is the whole point of the fixture: its median is as
	// fast as Steady and half its answers take a minute. A role somebody reads
	// must be priced above a role nobody does, and by more than the difference
	// between the two plants.
	tail, middle := 0, 0
	for _, one := range requests {
		bimodal := lane.ID{Model: one.model, Lane: "Bimodal"}
		read := measured.felt(bimodal, one.at, talkShape, true, one.id)
		unread := measured.felt(bimodal, one.at, talkShape, false, one.id)
		if math.IsInf(read, 0) || math.IsInf(unread, 0) {
			continue
		}
		switch {
		case read > unread:
			tail++
		case read == unread:
			middle++
		default:
			t.Fatalf("a watched request was priced BELOW an unwatched one on the same machine: %.2f < %.2f", read, unread)
		}
	}
	if tail == 0 {
		t.Fatalf("the bimodal machine was never priced above its own median for a watched role")
	}
}

func TestAMachineIsNeverPricedFromTheAnswerBeingScored(t *testing.T) {
	measured, requests := fixtureWorld(t)
	// `Lonely` answered exactly once, and that answer IS the request below. With
	// its own row left out there is nothing to price it from, which is the whole
	// of the leave-one-out law: a machine priced on the very answer being scored
	// is a machine scored against itself.
	found := false
	for _, one := range requests {
		if one.machine != "Lonely" {
			continue
		}
		found = true
		id := lane.ID{Model: one.model, Lane: "Lonely"}
		if felt := measured.felt(id, one.at, talkShape, true, one.id); !math.IsInf(felt, 0) {
			t.Errorf("a machine with only the scored answer was priced at %.2fs; it must be unpriceable", felt)
		}
		if felt := measured.felt(id, one.at, talkShape, true, ""); math.IsInf(felt, 0) {
			t.Errorf("with nothing left out the same machine is unpriceable, so the exclusion is not what made it so")
		}
	}
	if !found {
		t.Fatal("the fixture no longer contains the machine seen exactly once")
	}
}

func TestACutStreamIsCountedAndNeverDropped(t *testing.T) {
	measured, _ := fixtureWorld(t)
	if measured.censored != 5 {
		t.Errorf("cut streams: got %d, want the fixture's 5", measured.censored)
	}
	if measured.refusals != 1 {
		t.Errorf("refusals: got %d, want the fixture's 1", measured.refusals)
	}
}

func TestARefusalIsCreditedToTheMachineItsOwnSentenceNames(t *testing.T) {
	rows, err := callrows.Read(fixture)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	// The paced row carries no `served` column at all and says `via Slow` in its
	// sentence. Filing it under `Steady` — the machine the preference asked for —
	// would credit a refusal to a machine that was never reached.
	for _, row := range rows {
		if row.Status != 429 {
			continue
		}
		if machine := answered(row); machine != "Slow" {
			t.Errorf("a refusal naming `via Slow` was credited to %q", machine)
		}
	}
}

func TestEveryTagTheBuildWritesResolvesToARoleSomebodyDeclared(t *testing.T) {
	// THE ONE JOIN THIS TOOL MAKES FOR ITSELF, AND THE ONLY ONE OF ITS LAWS THAT
	// HAS TO READ THE TREE. A tag missing from `callSiteRoles` does not fail
	// anything at run time: it reads as [lane.RoleUnknown], which is a patient,
	// unwatched background errand, so a word added to the build tomorrow would
	// quietly move every number in the watched half of the report and produce a
	// table that still looks right. So this walks the sources for the tags the
	// build can actually write and fails on one nothing here names.
	//
	// It is the stopgap until `calllog.Record` carries the role itself (#928);
	// until then, this law is what keeps the stopgap true.
	for tag, want := range tagsTheBuildWrites(t) {
		got := roleOf(tag)
		if got == lane.RoleUnknown {
			t.Errorf("the build writes the tag %q (%s) and nothing here names it, so every call "+
				"made under it is priced as an unwatched background errand", tag, want)
			continue
		}
		if !got.Known() {
			t.Errorf("tag %q resolved to %q, which is not in internal/lane's role table", tag, got)
		}
	}
	// And the rule the table stands beside: an errand tagged with its own role
	// word needs no row at all.
	for _, role := range lane.Roles() {
		if role == lane.RoleUnknown {
			continue
		}
		if got := roleOf(string(role)); got != role {
			t.Errorf("a call tagged with the role word %q resolved to %q", role, got)
		}
	}
	if got := roleOf("a tag nobody has written down"); got != lane.RoleUnknown {
		t.Errorf("an unrecognised tag resolved to %q; it must read as a background errand", got)
	}
}

// derivedTags are the tag arguments in the tree that are not a string literal,
// spelled exactly as the source spells them, with what each one can produce.
//
// A CALL SITE WHOSE TAG THIS WALK CANNOT READ IS A FAILURE AND NOT A SKIP. The
// alternative — collecting the literals and ignoring everything else — is a law
// that passes forever the moment somebody builds a tag out of a variable, which
// is exactly how the log came to carry 2,309 untagged rows in the first place.
var derivedTags = map[string]func(yield func(tag, where string)){
	// internal/session/toolask.go: a tool's own call carries the tool's name
	// after the role word, and no walk can enumerate the belt.
	"toolCallTag(question.tool)": func(yield func(tag, where string)) {
		yield("tool", "internal/session/toolask.go, a call with no tool named")
		yield("tool:a_tool_nobody_has_added_yet", "internal/session/toolask.go, qualified by the tool")
	},
	// cmd/codeaf/chat.go's errandContext passes the errand's own name straight
	// through; its call sites are walked for the literals they hand it.
	"task": func(yield func(tag, where string)) {},
	// internal/session/clientdoor.go is the ONE place that package writes a tag
	// now, and what it writes is whatever purpose its caller handed it. It
	// enumerates nothing on its own: the words come from the door's own call
	// sites, which the walk below reads through the same rules.
	"string(purpose)": func(yield func(tag, where string)) {},
	// internal/session/auxiliary.go hands the door the errand's own role word, so
	// the whole of internal/roles' vocabulary reaches the log. The spelling is
	// what is left after `callPurpose(...)` comes off the argument.
	"role": func(yield func(tag, where string)) {
		for _, word := range roleWords() {
			yield(word, "an internal/roles constant, written by internal/session/auxiliary.go")
		}
	},
	// RoleEmbed is a PIN, not a text-tier tenant, so it never reaches the
	// session door. The media client and the adapter stamp the role word
	// themselves; the join is callSiteRoles["embed"] → lane.RoleAuxiliary.
	"string(roles.RoleEmbed)": func(yield func(tag, where string)) {
		yield("embed", "internal/embed and internal/provider, the RoleEmbed pin")
	},
}

// purposeWords and roleWords are the two vocabularies this law joins, each read
// from the ONE FILE that declares it and each read ONCE per run.
//
// They are [constWordsOfType] twice. The purposes that are a role's own name
// need no entry in either table — they arrive through the role vocabulary — and
// what is left in the door's own block is the handful that are not any role.
var purposeWords = sync.OnceValue(func() []string {
	return constWordsOfType(filepath.Join("internal", "session", "clientdoor.go"), "callPurpose")
})

var roleWords = sync.OnceValue(func() []string {
	return constWordsOfType(filepath.Join("internal", "roles", "roles.go"), "Role")
})

// constWordsOfType reads one file for the string constants declared with one
// named type, and PANICS ON AN EMPTY ANSWER.
//
// The panic is the point. This law joins two vocabularies by parsing the files
// that declare them, and a parse that silently found nothing would make the join
// trivially complete — every tag resolved, no row missing, the report quietly
// wrong. A file renamed out from under it has to be loud.
func constWordsOfType(relative, typeName string) []string {
	path := filepath.Join(moduleRoot, relative)
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		panic("reading " + relative + ": " + err.Error())
	}
	var words []string
	ast.Inspect(file, func(node ast.Node) bool {
		spec, isValue := node.(*ast.ValueSpec)
		if !isValue || len(spec.Values) != 1 {
			return true
		}
		if kind, isIdent := spec.Type.(*ast.Ident); !isIdent || kind.Name != typeName {
			return true
		}
		if word, literal := stringLiteral(spec.Values[0]); literal && word != "" {
			words = append(words, word)
		}
		return true
	})
	if len(words) == 0 {
		panic(relative + " declares no " + typeName + " constants; this law is reading the wrong tree")
	}
	return words
}

// tagsTheBuildWrites reads the module's sources for every tag that can reach
// `calllog.Record.Tag`, and says where each came from.
func tagsTheBuildWrites(t *testing.T) map[string]string {
	t.Helper()
	found := map[string]string{}
	walkBuildSources(t, func(path string, file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return true
			}
			switch methodName(call.Fun) {
			case "WithCallTag":
				if len(call.Args) < 2 {
					return true
				}
				if word, literal := stringLiteral(call.Args[1]); literal {
					found[word] = path
					return true
				}
				spelling := source(t, call.Args[1])
				derived, known := derivedTags[spelling]
				if !known {
					t.Errorf("%s writes a call tag spelled %q, which this law cannot read; "+
						"add it to derivedTags with what it can produce", path, spelling)
					return true
				}
				derived(func(tag, where string) { found[tag] = where })
			case "completeWithModel", "completeWithNamedModel":
				// THE DOOR IS WHERE internal/session'S TAGS ARE STATED NOW. Its
				// second argument is a [session.callPurpose], and it reaches
				// `calllog.Record.Tag` verbatim (clientdoor.go). Reading it here is
				// what keeps this law pointed at the same thing the build is: the
				// `WithCallTag` case above sees only the one relay line.
				if len(call.Args) < 2 {
					return true
				}
				purpose := call.Args[1]
				// A PURPOSE MAY WEAR ITS TYPE. `callPurpose("...")` and a bare
				// constant are the same statement, so the conversion comes off
				// before the argument is read.
				if conversion, isCall := purpose.(*ast.CallExpr); isCall &&
					methodName(conversion.Fun) == "callPurpose" && len(conversion.Args) == 1 {
					if _, literal := stringLiteral(conversion.Args[0]); !literal {
						// A ROLE WORN AS A PURPOSE IS STILL A ROLE, and the
						// vocabulary it can produce is internal/roles' own. It is
						// over-approximated deliberately: this law asks whether
						// every tag the build CAN write is named, so answering
						// with more of them than one site writes is safe, where
						// answering with fewer is the hole it exists to close.
						if strings.Contains(source(t, conversion.Args[0]), "Role") {
							for _, word := range roleWords() {
								found[word] = path + ", a role worn as a purpose"
							}
							return true
						}
					}
					purpose = conversion.Args[0]
				}
				if word, literal := stringLiteral(purpose); literal {
					if word != "" {
						found[word] = path
					}
					return true
				}
				// A NAMED CONSTANT IS THE ORDINARY CASE and the door declares them
				// all in one block, so they are read from the declaration rather
				// than listed here.
				if ident, isIdent := purpose.(*ast.Ident); isIdent && strings.HasPrefix(ident.Name, "purpose") {
					for _, word := range purposeWords() {
						found[word] = "an internal/session/clientdoor.go callPurpose constant"
					}
					return true
				}
				spelling := source(t, purpose)
				derived, known := derivedTags[spelling]
				if !known {
					t.Errorf("%s hands the model door a purpose spelled %q, which this law cannot "+
						"read; add it to derivedTags with what it can produce", path, spelling)
					return true
				}
				derived(func(tag, where string) { found[tag] = where })
			case "errandContext":
				// func errandContext(ctx, settings, task string, role lane.Role)
				if len(call.Args) < 3 {
					return true
				}
				if word, literal := stringLiteral(call.Args[2]); literal {
					found[word] = path
				}
			}
			return true
		})
	})
	if len(found) < len(callSiteRoles) {
		t.Fatalf("the walk found only %d tags for a table of %d rows; it is reading the wrong tree", len(found), len(callSiteRoles))
	}
	return found
}

// TestEveryRoleWordIsDeclaredWhereTheVocabularyIs fails a role named anywhere
// but internal/roles, in either spelling.
//
// [roleWords] parses ONE FILE, because that is where this build says the role
// vocabulary lives and because the tool that reads it cannot import the engine
// to ask the registry at run time — a replay reader would pull in the whole
// session package for a list of words. A role constant declared at its own call
// site is therefore invisible to it, and to every other reader that asks what
// the roles ARE.
//
// THE MEASURED FAILURE. `const spellOutRole roles.Role = "spellout"` lived in
// internal/session/spellout.go. It registered, resolved and tagged its call
// correctly — and reached the cost report as a word nobody had written down,
// which [roleOf] prices as a background errand with nobody waiting. The
// spell-out is the one auxiliary a person sits and watches, so the single call
// whose latency matters most was the one counted as if it did not. Nothing
// failed; a number was quietly wrong. `const standingSentinelRole roles.Role =
// "sentinel"` was the second, caught by this law on the day it was written.
//
// BOTH SPELLINGS, because there are two. A declared type
// (`x roles.Role = "w"`) and a conversion (`x = roles.Role("w")`) produce the
// same constant and only the first has a Type for a reader to match on;
// `roleRepair = roles.Role("repair")` was invisible to the first version of
// this law. The second half — every word handed to [roles.Register] — is the
// one that cannot be spelled around at all, because a role nothing registers
// resolves under no tier.
func TestEveryRoleWordIsDeclaredWhereTheVocabularyIs(t *testing.T) {
	const home = "internal/roles/roles.go"
	declared := map[string]bool{}
	for _, word := range roleWords() {
		declared[word] = true
	}
	walkBuildSources(t, func(path string, file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			if call, isCall := node.(*ast.CallExpr); isCall {
				if methodName(call.Fun) == "Register" && len(call.Args) > 0 {
					if word, spelled := roleWordOf(call.Args[0]); spelled && !declared[word] {
						t.Errorf("%s registers the role %q, which %s does not declare. A role's "+
							"WORD belongs there beside the others — spelled out, not conjured at "+
							"the call — because everything that reads the vocabulary reads that "+
							"file and a word it cannot see is priced as an errand nobody is "+
							"waiting on.", path, word, home)
					}
				}
				return true
			}
			spec, isValue := node.(*ast.ValueSpec)
			if !isValue || path == home {
				return true
			}
			for index, name := range spec.Names {
				spelled := isRoleType(spec.Type)
				if !spelled && index < len(spec.Values) {
					_, spelled = roleWordOf(spec.Values[index])
				}
				if spelled {
					t.Errorf("%s declares the role %s. Every role's WORD belongs in %s, beside "+
						"the others: a role declared at its call site resolves and tags correctly "+
						"and is still invisible to everything that reads the vocabulary, which "+
						"prices its calls as errands nobody is waiting on. Declare it there and "+
						"keep the roles.Register call here.", path, name.Name, home)
				}
			}
			return true
		})
	})
}

// TestTheVocabularyListsEveryRoleTheFileDeclares keeps [roles.Vocabulary] whole.
//
// THE REGISTRY IS NOT THE VOCABULARY, and that difference cost a real setting.
// A role gets into the registry by registering, which is how it gets a TIER; a
// role with no tier never registers, and `imagegen` is one — a painter is chosen
// by a pin or not at all. Reading the registry as "what are the roles" threw a
// person's `imagegen:` pin away on the way in.
//
// So internal/roles answers both questions, and the vocabulary is a hand-written
// list, which is a thing that falls behind. This is why it cannot: the same
// parse that reads the file for [roleWords] is checked against what the package
// says it has.
func TestTheVocabularyListsEveryRoleTheFileDeclares(t *testing.T) {
	listed := map[string]bool{}
	for _, role := range roles.Vocabulary() {
		listed[string(role)] = true
	}
	for _, word := range roleWords() {
		if !listed[word] {
			t.Errorf("internal/roles declares the role %q and roles.Vocabulary does not list it. "+
				"Every reader that asks what the roles ARE reads that list, and a word missing "+
				"from it is a pin thrown away on the way in.", word)
		}
	}
	if len(listed) != len(roleWords()) {
		t.Errorf("roles.Vocabulary lists %d roles and the file declares %d; a word in the list "+
			"that is not a constant is a word nothing can ever write",
			len(listed), len(roleWords()))
	}
}

// isRoleType reports the `roles.Role` type spelled on a declaration.
func isRoleType(expr ast.Expr) bool {
	named, isSelector := expr.(*ast.SelectorExpr)
	if !isSelector || named.Sel.Name != "Role" {
		return false
	}
	pkg, isIdent := named.X.(*ast.Ident)
	return isIdent && pkg.Name == "roles"
}

// roleWordOf reads a role written as a conversion — `roles.Role("repair")` —
// and answers the word inside it.
func roleWordOf(expr ast.Expr) (string, bool) {
	call, isCall := expr.(*ast.CallExpr)
	if !isCall || !isRoleType(call.Fun) || len(call.Args) != 1 {
		return "", false
	}
	return stringLiteral(call.Args[0])
}

// moduleRoot is this package's place in the tree, which is two directories up.
const moduleRoot = "../.."

// walkBuildSources hands every Go file the build compiles to the visitor. Test
// files are skipped for the reason every other law in this tree skips them: a
// fixture is allowed to name things the product does not.
func walkBuildSources(t *testing.T, visit func(path string, file *ast.File)) {
	t.Helper()
	set := token.NewFileSet()
	err := filepath.WalkDir(moduleRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "testdata", "node_modules", "vendor", "bin":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(set, path, nil, 0)
		if err != nil {
			return nil
		}
		visit(filepath.ToSlash(strings.TrimPrefix(path, moduleRoot+"/")), file)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
}

// methodName is the LAST WORD of a call expression — the function or method
// being called, whatever it was reached through.
//
// THE RECEIVER IS NOT PART OF THE QUESTION THIS LAW ASKS. "Does this call go
// through the door" has the same answer for `completeWithModel`,
// `a.completeWithModel`, `p.agent.completeWithModel` and
// `agentFor(x).completeWithModel`, and for `WithCallTag` whether or not the
// package is spelled in front of it.
//
// It used to be a list of the spellings somebody had seen, rendered by a reader
// that flattened ONE selector level — so `p.agent.completeWithModel` and
// `e.agent.completeWithModel` rendered as "" and three of the six rows in that
// list could never match anything. The ladder looked complete and was half
// dead, which is what a ladder of names always eventually is, and the only
// symptom would have been a tag this law quietly stopped knowing about.
func methodName(fun ast.Expr) string {
	switch shape := fun.(type) {
	case *ast.Ident:
		return shape.Name
	case *ast.SelectorExpr:
		return shape.Sel.Name
	}
	return ""
}

func stringLiteral(expr ast.Expr) (string, bool) {
	lit, isLit := expr.(*ast.BasicLit)
	if !isLit || lit.Kind != token.STRING {
		return "", false
	}
	word, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return word, true
}

func source(t *testing.T, expr ast.Expr) string {
	t.Helper()
	var out bytes.Buffer
	if err := printer.Fprint(&out, token.NewFileSet(), expr); err != nil {
		t.Fatalf("printing an expression: %v", err)
	}
	return out.String()
}

func TestTheClassesAreExactlyWhatTheRoleTableCanProduce(t *testing.T) {
	// The report's two tables are keyed on this list, and the quantile candidate
	// keeps one picture per entry of it. A role added to internal/lane whose class
	// is not in the list would drop every call in it out of both silently.
	can := map[string]bool{}
	for _, role := range append(lane.Roles(), lane.RoleUnknown) {
		can[classOf(role)] = true
	}
	for _, class := range classes {
		if !can[class] {
			t.Errorf("the report prints a %q class no role can produce", class)
		}
		delete(can, class)
	}
	for class := range can {
		t.Errorf("the role table produces a %q class the report never prints", class)
	}
}

func TestTheHalfLifeFallsBackToTheBeliefsOwnWhenNothingMoves(t *testing.T) {
	measured, _ := fixtureWorld(t)
	// Nothing in the fixture drifts, so the variogram never reaches halfway and
	// the honest answer is the forgetting internal/lane already uses rather than
	// a figure invented to fill the hole.
	if got := changeHalfLife(measured); got != lane.HalfLife {
		t.Errorf("the measured half-life is %v; with nothing drifting it must be internal/lane's own %v", got, lane.HalfLife)
	}
	if got := processNoise(measured); got != 0 {
		t.Errorf("the fixture spans one day, so no day-to-day variance can be measured; got %.4f", got)
	}
}

func TestEveryCandidateAnswersTheSameQuestionAndTheTableSaysSo(t *testing.T) {
	rows, err := callrows.Read(fixture)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	var out bytes.Buffer
	report(&out, settings{window: defaultWindow, min: 2, incidents: 2, log: fixture}, rows, nil)
	printed := out.String()
	for _, name := range order {
		if !strings.Contains(printed, "`"+name+"`") {
			t.Errorf("the report never names the %q candidate", name)
		}
	}
	for _, want := range []string{"## Regret", "## The moments that hurt", "## How good the estimate is", "Steady"} {
		if !strings.Contains(printed, want) {
			t.Errorf("the report is missing %q", want)
		}
	}
}

func TestTheCommonSetIsTheSameSizeForEveryCandidate(t *testing.T) {
	rows, err := callrows.Read(fixture)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	measured := newWorld(rows, defaultWindow)
	requests, _ := requestsOf(rows)
	stream := momentsOf(rows, requests, nil)
	look := settings{window: defaultWindow, min: 2}
	said := map[string]answers{}
	for _, build := range candidates(lane.HalfLife, 0) {
		policy := build()
		said[policy.Name()] = pass(policy, stream, len(requests))
	}
	bills := bill(requests, said, measured, look, nil)
	// FOUR CANDIDATES SCORED ON FOUR DIFFERENT SETS CANNOT BE COMPARED, which is
	// the fault this law exists to keep out: a policy that declines the hard half
	// of the log would otherwise win every table by declining it.
	for _, class := range classes {
		want := -1
		for name, held := range bills {
			one := held[class]
			if one == nil {
				continue
			}
			if want < 0 {
				want = one.scored
				continue
			}
			if one.scored != want {
				t.Errorf("%s scored %d requests in the %s class; another candidate scored %d", name, one.scored, class, want)
			}
		}
	}
}

func TestAPolicyIsNeverTaughtAnAnswerItCouldNotHaveHadYet(t *testing.T) {
	rows, err := callrows.Read(fixture)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	requests, _ := requestsOf(rows)
	// THE ORDER IS THE WHOLE OF AN OFFLINE EVALUATION'S HONESTY. A stream that
	// handed a candidate an answer before the request that produced it would
	// flatter every policy that is good at hindsight, which is all of them.
	//
	// So the property is read FROM WHERE A POLICY STANDS — what it had been
	// taught by the time it was asked, through pass() itself — and never off the
	// sorted slice, whose order is its own answer to the question.
	if complaint := hindsight(momentsOf(rows, requests, nil), requests); complaint != "" {
		t.Errorf("over the fixture, %s", complaint)
	}

	// And the case the sort alone decides: an answer landing in the same
	// millisecond a request goes out. The request is what the chooser was asked
	// and the answer is somebody else's call finishing, so the ask is walked
	// first — which today holds only because momentsOf appends the requests
	// before the rows and the sort is stable. That is a fact about one line.
	at := time.Date(2026, 9, 11, 14, 27, 50, 0, time.UTC)
	tie := []callrows.Row{{
		Record: calllog.Record{
			Model: "a/model", Served: "Steady", Tag: "turn",
			TTFTms: 200, Millis: 1200, CompletionTokens: 100,
		},
		At: at,
	}}
	asks := []asked{{
		index: 0, id: "the request being made", at: at,
		model: "a/model", role: lane.RoleTalk, want: talkShape,
	}}
	stream := momentsOf(tie, asks, nil)
	if len(stream) < 2 || stream[0].ask == nil {
		t.Fatalf("at one stamp the stream does not put the request first: %d moments, first ask %v", len(stream), stream[0].ask)
	}
	if complaint := hindsight(stream, asks); complaint != "" {
		t.Errorf("at an equal stamp, %s", complaint)
	}
	// THE SAME CHECK OVER THE ORDER THIS BUILD IS CAREFUL NOT TO PRODUCE MUST
	// FAIL, or nothing above proves anything: a check that cannot go red is a
	// comment with a test's name on it.
	wrong := append([]moment(nil), stream...)
	sort.SliceStable(wrong, func(i, j int) bool { return wrong[i].ask == nil && wrong[j].ask != nil })
	if complaint := hindsight(wrong, asks); complaint == "" {
		t.Error("a stream that teaches the answer before the request it answers passes this law, so the law checks nothing")
	}
}

// taughtWhen is a candidate that demands nothing and only remembers WHEN: for
// every request it is asked about it records the stamp of the most recent thing
// it had been taught by the time the question arrived.
type taughtWhen struct {
	latest time.Time
	when   map[int]time.Time
}

func (t *taughtWhen) Name() string { return "when" }

func (t *taughtWhen) learn(at time.Time) {
	if at.After(t.latest) {
		t.latest = at
	}
}

func (t *taughtWhen) Saw(one lane.Sighting, _ string) { t.learn(one.At) }
func (t *taughtWhen) Judged(one lane.Outcome)         { t.learn(one.At) }
func (t *taughtWhen) Published(lane.Row, float64)     {}

func (t *taughtWhen) Demand(one asked) string {
	t.when[one.index] = t.latest
	return ""
}

// hindsight walks a stream the way a real candidate does and names the first
// request that was answered already knowing something it could not have known.
func hindsight(stream []moment, requests []asked) string {
	recorder := &taughtWhen{when: map[int]time.Time{}}
	pass(recorder, stream, len(requests))
	for _, one := range requests {
		taught, asked := recorder.when[one.index], one.at
		if taught.IsZero() || taught.Before(asked) {
			continue
		}
		return fmt.Sprintf("the request at %s was answered already taught an answer stamped %s",
			asked.Format(callrows.TimeLayout), taught.Format(callrows.TimeLayout))
	}
	return ""
}

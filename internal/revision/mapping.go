package revision

// The door the acceptance mapping goes through, which is the same door a
// citation goes through.
//
// A JUDGE'S SAY-SO IS VOCABULARY. The mapping asks one model call which check
// exercises which behaviour, and until this existed the only thing weighed about
// the answer was that the check NAME EXISTED in the roster — a real check, said
// to cover a real behaviour, with nothing between the two but the model's
// confidence. ofetch's runs kept naming the same family of behaviours as
// exercised by nothing (`circuitBreaker: true, defaults are
// halfOpenMaxRequests = 1` and its four siblings) while the suite grew checks
// about the breaker in general, and a mapping weighed on vocabulary alone will
// eventually pair those two: the words are about the same subject, and the
// check is not about that behaviour.
//
// So the pairing is GROUNDED BY STRUCTURE, exactly as an admitted citation is
// (grounding.go): a check may satisfy a point only where the names the point
// spells distinctively are names the check itself spells — in its own identity,
// or in the body of the file that identity comes out of. That is a fact about
// two strings and a file on disk, and no model has a say in it.

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// mappingBodyBudget bounds what this reads off disk to ground one settlement.
//
// It is verify's own scopeReadBudget at the same size and for the same reason: a
// bounded sweep of test files is cheap and a repository full of fixtures is not,
// and the door degrades in the safe direction when it runs out — a body it could
// not read grounds nothing, so the point stays unexercised.
const mappingBodyBudget = 2 << 20

// mappingBodyBytes bounds one file, so a single generated fixture cannot spend
// the whole budget on its own.
const mappingBodyBytes = 512 << 10

// GroundMapping keeps only the pairings the world supports, and empties the rest.
//
// root is the tree the delivery stands in and record is what the run left behind;
// between them they are how a check identity becomes a file whose text can be
// read. Nothing here invents a pairing — it only ever removes one — so a run with
// no workspace, no record and no readable file is left exactly as the judge
// answered it.
//
// A POINT THAT SPELLS NO NAME IS NOT JUDGED HERE, and that asymmetry is
// deliberate. "Normal scrolling must still update the visible viewport" names
// nothing distinctive; there is no structural question to ask of it, and a door
// that answered "unexercised" to every such behaviour would fail every prose
// request this program is given — a floor that refuses everything is not a
// floor. The door speaks where the request spelled something, which is where a
// mapping can actually be wrong in a way that is checkable.
func GroundMapping(root string, record []string,
	points []plan.Point, mapping []store.ExercisedPoint,
) []store.ExercisedPoint {
	if len(mapping) == 0 {
		return mapping
	}
	bodies := &checkBodies{root: root, budget: mappingBodyBudget, held: map[string]string{}}
	bodies.record = verify.OwnChecks(root, record)
	grounded := make([]store.ExercisedPoint, len(mapping))
	copy(grounded, mapping)
	for index := range grounded {
		check := strings.TrimSpace(grounded[index].Check)
		if check == "" || index >= len(points) {
			continue
		}
		wanted := symbolsIn(points[index].Behaviour)
		if len(wanted) == 0 {
			continue
		}
		if !checkNames(check, wanted, bodies) {
			grounded[index].Check = ""
		}
	}
	return grounded
}

// checkNames is the door itself: every name the behaviour spells distinctively
// is a name this check spells, in its own identity or in the text it came from.
//
// EVERY name and not merely one, which is symbolsGrounded's rule and is what
// makes it discriminating. `circuitBreaker` alone is named by every check about
// the breaker; `circuitBreaker` AND `halfOpenMaxRequests` is named by the check
// about that default and by nothing else.
func checkNames(check string, wanted []string, bodies *checkBodies) bool {
	haystack := strings.ToLower(check) + "\n" + bodies.forCheck(check)
	for _, symbol := range wanted {
		if !namesSymbol(haystack, symbol) {
			return false
		}
	}
	return true
}

// namesSymbol is a WHOLE-NAME match and never a substring one, which is the rule
// scope.go's adjacency is held to and for the identical reason: `Log` inside
// `Logger` is not a mention of Log, and a door that read it as one would admit
// exactly the loose pairings it exists to catch. The symbol's own separators —
// the dots, dashes and slashes that made it a symbol — are part of it, so what
// bounds it is a letter, a digit or an underscore on either side.
func namesSymbol(haystack, symbol string) bool {
	if symbol == "" {
		return false
	}
	for offset := 0; ; {
		at := strings.Index(haystack[offset:], symbol)
		if at < 0 {
			return false
		}
		at += offset
		before := at == 0 || !nameByte(haystack[at-1])
		end := at + len(symbol)
		after := end == len(haystack) || !nameByte(haystack[end])
		if before && after {
			return true
		}
		offset = at + 1
	}
}

// nameByte says this byte can be part of a name, which is what makes a match
// inside one rather than beside it.
func nameByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// checkBodies reads the text a check identity comes out of, once per file and
// inside one budget.
type checkBodies struct {
	root   string
	record []string
	budget int
	held   map[string]string
}

// forCheck is the text this check's identity points at, lowercased, or the empty
// string when there is none to read.
//
// Every runner this program reads names the file the check is in, at the head of
// the identity: pytest's `tests/test_log.py::test_follow`, go test's package
// path, vitest's `test/circuit-breaker.test.ts > defaults > threshold`. So the
// file is read off the identity first. Where the identity names none — a runner
// that prints bare check names — the files the run itself left behind stand in,
// because those are the checks a round most often wrote and the ones a mapping is
// most often asked about.
func (b *checkBodies) forCheck(check string) string {
	var bodies strings.Builder
	if named := checkFile(check); named != "" {
		bodies.WriteString(b.read(named))
	}
	if bodies.Len() > 0 {
		return bodies.String()
	}
	for _, path := range b.record {
		bodies.WriteString(b.read(path))
	}
	return bodies.String()
}

// checkFile is the file a check identity names, if it names one.
func checkFile(check string) string {
	head := strings.TrimSpace(check)
	for _, cut := range []string{"::", " > ", " › ", " | "} {
		if before, _, found := strings.Cut(head, cut); found {
			head = strings.TrimSpace(before)
		}
	}
	if head = strings.Trim(filepath.ToSlash(head), "/"); head == "" {
		return ""
	}
	if strings.Contains(head, " ") || !strings.Contains(filepath.Base(head), ".") {
		return ""
	}
	return head
}

// read is the file's text, lowercased, cached, and bounded twice — once per file
// and once for the whole settlement.
func (b *checkBodies) read(path string) string {
	if b.root == "" || path == "" {
		return ""
	}
	if held, seen := b.held[path]; seen {
		return held
	}
	b.held[path] = ""
	if b.budget <= 0 {
		return ""
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return ""
	}
	info, err := os.Stat(filepath.Join(b.root, clean))
	if err != nil || info.IsDir() || info.Size() > mappingBodyBytes {
		return ""
	}
	body, err := os.ReadFile(filepath.Join(b.root, clean))
	if err != nil {
		return ""
	}
	b.budget -= len(body)
	text := strings.ToLower(string(body))
	b.held[path] = text
	return text
}

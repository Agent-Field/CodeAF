package session

// A LANDING'S CLAIMS ARE ITS CHECKLIST.
//
// ── WHAT WENT WRONG WITHOUT THIS ──
//
// A run on 2026-08-31 produced a diff whose own documents said a string had been
// taken out of the surface, while the function that returns that string was
// still there and still being called. Three artifacts — a change note, a page of
// prose and the comments around the code — asserted a state of the world the
// code did not have, and every gate the build owns waved them through, because
// every gate the build owns asks whether a NAME is mentioned and never whether
// the sentence around it is true. The check that followed judged the work
// against its acceptance, which the work met; nobody had asked whether what the
// landing SAID about the world was so.
//
// So the law: every completion report is a list of claims, and checking is
// hunting each claim in the ground. What a landing asserts is exactly what a
// reader will believe without looking, which makes it the most expensive thing
// in the change to get wrong and the cheapest thing in the change to test.
//
// ── DOMAIN-NEUTRAL, BECAUSE THE WORK IS ──
//
// Nothing here knows what a repository is, and nothing here is about code. "The
// page no longer says X", "the three follow-ups were sent", "the table has forty
// rows" are all claims, and all of them are settled the same way: read what the
// claim asserts, then go and look. Two shapes are settled by this file on its
// own because they are settled by LOOKING —
//
//   - a claim that a string is gone is a search of what would ship;
//   - a claim that something under a named place was updated is a question about
//     which files the landing wrote.
//
// A claim that a BEHAVIOUR changed cannot be settled by looking; it has to be
// run, and running things is what the checker with a shell is for. So this file
// does not guess at those: it hands them to the checker as a written checklist
// and asks for any it could not settle by name.
//
// ── AND A CLAIM NOBODY SETTLED IS SAID OUT LOUD ──
//
// The failure this file exists for is a false claim passing SILENTLY, so the one
// thing it may never do is drop a claim on the floor. A claim the ground
// contradicts is a finding. A claim nothing settled is named in the report as
// one nothing checked. Only a claim that was actually hunted and held goes quiet.

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// claimLimit is how many claims one landing's checklist carries.
	//
	// A landing with two hundred asserted clauses is a landing whose claims
	// nobody is going to read either, and the cost of hunting is paid per claim
	// in a walk of the tree. Two dozen is more than any change note this build
	// has ever carried and still cheap enough to hunt on every check.
	claimLimit = 24

	// claimNoteLimit bounds one landing note as it is read. A note is a document
	// somebody wrote by hand; anything past sixty-four kilobytes of it is not a
	// note, and the frontmatter this reads is in the first few lines regardless.
	claimNoteLimit = 64 << 10

	// claimTextLimit keeps one claim readable where it is quoted back — in the
	// checker's packet, and in the report a person reads off the card.
	claimTextLimit = 300

	// claimScanFiles and claimScanBytes bound the hunt through what would ship.
	//
	// The search is the whole cost of this machinery and it is paid on every
	// check, so it is bounded the way [auditRestoreEntries] is bounded and for
	// the same reason: a tree with a built `node_modules` in it is not a tree
	// anybody is claiming anything about, and a hunt that spent four of the
	// check's five minutes walking one would have turned a gate into a timeout.
	// Past either bound the hunt stops and the claims it did not reach are
	// reported as claims nothing checked, which is the honest answer.
	claimScanFiles = 8000
	claimScanBytes = 16 << 20

	// claimFileLimit is the largest single file the hunt reads. A minified
	// bundle or a fixture corpus is not prose anybody wrote a claim about.
	claimFileLimit = 2 << 20

	// claimLiteralFloor is the shortest quoted span the hunt will chase. Two
	// characters is a word like "of" or an operator, and a landing saying such a
	// thing is gone means something no search can settle.
	claimLiteralFloor = 3

	// claimUncheckedNamed is how many unsettled claims the report names. One
	// line, at most two claims: the person is owed the news that something went
	// unchecked, and a card carrying six of them is a card nobody finishes.
	claimUncheckedNamed = 2
)

// claim is one thing a landing asserts about the world.
type claim struct {
	// text is the sentence as its author wrote it. It is quoted back verbatim,
	// in the checker's packet and in the finding, because a claim restated is a
	// claim somebody can argue was never made.
	text string
	// source says where it was said, in plain words: the note's own path, or the
	// work's own account.
	source string
	// declared is true for a claim a landing WROTE DOWN as an obligation — a
	// clause under a note's `invalidates:`. It is false for a sentence read out
	// of the work's own account of itself.
	//
	// IT IS ONLY EVER USED TO DECIDE WHAT IS SAID ALOUD. Both kinds are hunted
	// and both kinds ride the checker's checklist; only a declared clause is
	// named in the report when nothing settled it, because a written obligation
	// going unchecked is news and a sentence of prose going unchecked is every
	// task there has ever been.
	declared bool
}

// claimStanding is what the ground had to say about one claim.
type claimStanding int

const (
	// claimUnchecked is the honest answer and the default: nothing here could
	// settle this one either way.
	claimUnchecked claimStanding = iota
	// claimHolds means the ground was searched and agrees.
	claimHolds
	// claimBroken means the ground CONTRADICTS the claim. It is the only
	// standing that ends a check on its own.
	claimBroken
)

// claimFinding is one claim and what became of it.
type claimFinding struct {
	claim    claim
	standing claimStanding
	// evidence is what was found, in a person's words, and it is empty for a
	// claim nothing settled.
	evidence string
}

// line is the finding as a person reads it: what the landing says, and what is
// actually there.
//
// IT LEADS WITH THE CLAIM AND NOT WITH THE MACHINERY. The news is that something
// asserted is not so, and the sentence that was asserted is the only part of it
// the person already recognises.
func (f claimFinding) line() string {
	return clip("it says \""+f.claim.text+"\" — but "+f.evidence, taskReportLineLimit)
}

// claimGround is what a claim is hunted in: the tree that would ship, and the
// files the landing wrote.
type claimGround struct {
	// dir is the tree as it would land — for a check, the clean restore.
	dir string
	// wrote are the paths the landing wrote, relative to dir.
	wrote []string
	// notes are the landing's own notes, which are never counted as the ground
	// disagreeing with itself. A note saying `$0.00` is gone contains `$0.00`,
	// and a hunt that read its own source would refute every claim ever written.
	notes map[string]bool
}

// ── the hunters ─────────────────────────────────────────────────────────────

// claimHunter recognises ONE SHAPE of claim and settles it against the ground.
//
// IT IS A REGISTRY AND NOT A SWITCH, for the reason every registry in this
// package is one: a shape of claim is a thing somebody adds — "the mail was
// sent", "the row count is forty" — and a switch is a place they have to be let
// into. A hunter that does not recognise the claim answers false and costs
// nothing, and the claim goes to the checker instead.
type claimHunter struct {
	// name says what this hunter recognises, for the log and for the test that
	// holds the registry to having one of each.
	name string
	// hunt settles the claim, and answers false when it does not recognise it.
	hunt func(claim, claimGround) (claimFinding, bool)
}

// claimHunters is every shape this build can settle by looking. Order is the
// order a claim is offered to them, and the first to recognise it answers.
var claimHunters = []claimHunter{
	{name: "a string that is gone", hunt: huntStringIsGone},
	{name: "work under a named place", hunt: huntWorkUnderPlace},
}

// huntClaims puts every claim to the registry and returns the whole checklist,
// in the order the claims were made.
func huntClaims(claims []claim, ground claimGround) []claimFinding {
	checklist := make([]claimFinding, 0, len(claims))
	for _, made := range claims {
		checklist = append(checklist, huntOne(made, ground))
	}
	return checklist
}

// huntOne offers one claim to each hunter in turn. A claim nobody recognises is
// unchecked, which is a standing and not a failure.
func huntOne(made claim, ground claimGround) claimFinding {
	for _, hunter := range claimHunters {
		if finding, known := hunter.hunt(made, ground); known {
			return finding
		}
	}
	return claimFinding{claim: made}
}

// brokenClaims is the claims the ground contradicts, and it is what ends a check.
func brokenClaims(checklist []claimFinding) []claimFinding {
	var broken []claimFinding
	for _, finding := range checklist {
		if finding.standing == claimBroken {
			broken = append(broken, finding)
		}
	}
	return broken
}

// uncheckedClaims is the claims nothing settled — the ones the checker is asked
// about, and the ones a passing check has to admit it never reached.
func uncheckedClaims(checklist []claimFinding) []claimFinding {
	var open []claimFinding
	for _, finding := range checklist {
		if finding.standing == claimUnchecked {
			open = append(open, finding)
		}
	}
	return open
}

// ── a claim that a string is gone ───────────────────────────────────────────

// goneWords are the ways a landing says a thing is not there any more. They are
// PHRASES rather than stems because the whole safety of this hunter is that it
// only fires on a sentence whose subject really is an absence.
var goneWords = []string{
	"no longer",
	"is gone",
	"are gone",
	"was removed",
	"were removed",
	"is removed",
	"are removed",
	"removed",
	"deleted",
	"rendered nowhere",
	"appears nowhere",
	"nowhere",
	"nothing says",
	"does not say",
	"does not appear",
	"never appears",
	"says nothing",
	"is not there",
	"stopped saying",
}

// huntStringIsGone settles "this exact text is not in the work any more" by
// going and looking for it.
//
// IT IS SCOPED TO ONE CLAUSE, AND THAT IS THE WHOLE OF ITS SAFETY. The form a
// good claim is written in is two halves — what it WAS, and what it IS NOW —
// and the old thing is quoted in the first half: "The trunk was `chat-v3-task`.
// It no longer exists on origin." A hunter reading the whole sentence would find
// `chat-v3-task` beside "no longer", go looking, find the very document it read
// the claim out of, and refute a landing for saying something true. So a literal
// is only chased when it stands in the SAME clause as the absence, which is the
// only place a writer puts the thing they are saying is gone.
//
// AND IT NEEDS A QUOTED LITERAL. "The refusal is gone" is a claim about a
// behaviour and no search settles it; "`8 open is as many as aforge holds` is
// gone" names the exact text and a search settles it outright. A claim with no
// literal in its absent clause is not recognised, and goes to the checker.
func huntStringIsGone(made claim, ground claimGround) (claimFinding, bool) {
	if strings.TrimSpace(ground.dir) == "" {
		return claimFinding{}, false
	}
	var literals []string
	for _, clause := range claimClauses(made.text) {
		if !saysGone(clause) {
			continue
		}
		literals = append(literals, quotedSpans(clause)...)
	}
	if len(literals) == 0 {
		return claimFinding{}, false
	}
	for _, literal := range literals {
		if where, found := findInGround(ground, literal); found {
			return claimFinding{
				claim:    made,
				standing: claimBroken,
				evidence: literal + " is still in " + where,
			}, true
		}
	}
	return claimFinding{
		claim:    made,
		standing: claimHolds,
		evidence: strings.Join(literals, ", ") + " is nowhere in what would ship",
	}, true
}

// saysGone reports whether one clause asserts an absence.
func saysGone(clause string) bool {
	lower := strings.ToLower(clause)
	for _, word := range goneWords {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// ── a claim that work landed under a named place ────────────────────────────

// updateWords are the ways a landing says something under a place was worked on.
var updateWords = []string{
	"updated",
	"update",
	"rewritten",
	"rewrote",
	"edited",
	"changed",
	"say what is true",
	"says what is true",
	"now say",
	"now says",
	"added to",
	"written",
}

// huntWorkUnderPlace settles "the things under here were updated" by asking the
// only question that settles it: did this landing write anything under there?
//
// A LANDING THAT CLAIMS IT UPDATED SOMEWHERE AND WROTE NOTHING THERE IS THE
// CHEAPEST FALSE CLAIM THERE IS TO CATCH, and it is the exact shape the manual
// law is broken in — a change that says the pages were brought up to date and
// touched no page.
//
// AND IT READS THE WHOLE CLAIM RATHER THAN ONE CLAUSE OF IT, which is where it
// parts company with the absence hunter above. The two halves of an invalidation
// carry different things: the absence is asserted in one of them, so a literal
// has to stand in the half that asserts it — but a PLACE is named once, in
// whichever half reads better, and it is the same place in both. "The pages under
// `docs/guide` said the key was unbound; they now say what is true instead" names
// the place in the first half and makes its claim in the second, and it is one
// claim about one directory.
func huntWorkUnderPlace(made claim, ground claimGround) (claimFinding, bool) {
	if strings.TrimSpace(ground.dir) == "" || !saysUpdated(made.text) {
		return claimFinding{}, false
	}
	for _, span := range quotedSpans(made.text) {
		place := strings.Trim(strings.TrimSpace(span), "/")
		if place == "" || !strings.Contains(place, "/") {
			continue
		}
		if !existsUnder(ground.dir, place) {
			continue
		}
		if wroteUnder(ground.wrote, place) {
			return claimFinding{
				claim:    made,
				standing: claimHolds,
				evidence: "this landing wrote under " + place,
			}, true
		}
		return claimFinding{
			claim:    made,
			standing: claimBroken,
			evidence: "nothing under " + place + " was written",
		}, true
	}
	return claimFinding{}, false
}

// saysUpdated reports whether one clause asserts that something was worked on.
func saysUpdated(clause string) bool {
	lower := strings.ToLower(clause)
	for _, word := range updateWords {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// existsUnder reports whether a named place is really in the tree. A claim about
// somewhere that does not exist is not a claim this hunter can settle — it may
// be about a place somewhere else entirely — so it goes to the checker.
func existsUnder(dir, place string) bool {
	_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(place)))
	return err == nil
}

// wroteUnder reports whether the landing wrote anything at or under a place.
func wroteUnder(wrote []string, place string) bool {
	prefix := place + "/"
	for _, path := range wrote {
		clean := strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(path)), "./")
		if clean == place || strings.HasPrefix(clean, prefix) {
			return true
		}
	}
	return false
}

// ── reading the claims out of a landing ─────────────────────────────────────

// landingClaims is every claim a landing makes: the clauses its notes declare,
// and the sentences of its own account of what it did.
//
// THE NOTES COME FIRST because they are the written obligations — a document the
// landing wrote saying, in as many words, what stopped being true. The account
// follows because "I did X" is a claim too, and the run this file exists for
// made several of them.
func landingClaims(dir string, wrote []string, account string) []claim {
	claims := landingNoteClaims(dir, wrote)
	for _, said := range accountClaims(account) {
		if len(claims) >= claimLimit {
			break
		}
		claims = append(claims, said)
	}
	if len(claims) > claimLimit {
		claims = claims[:claimLimit]
	}
	return claims
}

// landingNoteClaims reads the clauses a landing DECLARED.
//
// A LANDING NOTE IS A DOCUMENT WITH FRONTMATTER THAT LISTS WHAT STOPPED BEING
// TRUE. That is a convention, not a file format anybody owns, and it is spelled
// generically here on purpose: a repository's change entry is one instance of it,
// and a project that keeps a release note, a handover or a status document in the
// same shape gets the same checking for free. What makes a file a landing note is
// that THIS landing wrote it and that it declares an `invalidates:` list; nothing
// about its name or where it lives is consulted.
func landingNoteClaims(dir string, wrote []string) []claim {
	var claims []claim
	for _, path := range wrote {
		if len(claims) >= claimLimit {
			break
		}
		relative := filepath.ToSlash(strings.TrimSpace(path))
		if relative == "" {
			continue
		}
		body, err := readCapped(filepath.Join(dir, filepath.FromSlash(relative)), claimNoteLimit)
		if err != nil {
			continue
		}
		for _, clause := range declaredInvalidations(body) {
			if len(claims) >= claimLimit {
				break
			}
			claims = append(claims, claim{
				text:     clip(clause, claimTextLimit),
				source:   relative,
				declared: true,
			})
		}
	}
	return claims
}

// noteClaimSources is the set of paths a landing's own notes occupy, so the hunt
// never reads a claim's own source as the ground disagreeing with it.
func noteClaimSources(claims []claim) map[string]bool {
	sources := map[string]bool{}
	for _, made := range claims {
		if made.declared {
			sources[made.source] = true
		}
	}
	return sources
}

// declaredInvalidations pulls the `invalidates:` clauses out of a document's
// frontmatter.
//
// IT IS READ BY HAND AND NOT BY A YAML LIBRARY, which is a size decision and
// also an honesty one. The binary carries no YAML parser and this is not a
// reason to make it carry one; and what is wanted here is not a document model
// but one list of sentences, tolerantly read — a scanner that gives up quietly
// on a shape it does not know is exactly right, because a landing note nobody
// can parse is a landing that simply declares nothing.
//
// The three spellings a writer actually uses are all taken: a quoted one-liner,
// a bare one-liner, and a folded block opened with `>-` or `|`.
func declaredInvalidations(body string) []string {
	lines := strings.Split(body, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil
	}
	var (
		clauses []string
		inList  bool
		folded  []string
	)
	flush := func() {
		if len(folded) == 0 {
			return
		}
		if clause := strings.TrimSpace(strings.Join(folded, " ")); clause != "" {
			clauses = append(clauses, clause)
		}
		folded = nil
	}
	for _, raw := range lines[1:] {
		if strings.TrimSpace(raw) == "---" {
			break
		}
		trimmed := strings.TrimSpace(raw)
		indented := raw != trimmed
		switch {
		case !indented && strings.HasPrefix(trimmed, "invalidates:"):
			flush()
			inList = true
			continue
		case !indented && trimmed != "":
			// Another key at the top level ends the list.
			flush()
			inList = false
			continue
		}
		if !inList {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || trimmed == "-" {
			flush()
			item := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
			if item == ">-" || item == ">" || item == "|" || item == "|-" || item == "" {
				// A folded clause: its text is on the indented lines below.
				continue
			}
			if clause := unquote(item); clause != "" {
				clauses = append(clauses, clause)
			}
			continue
		}
		if trimmed != "" {
			folded = append(folded, trimmed)
		}
	}
	flush()
	return clauses
}

// unquote takes the quoting off a one-line clause without pretending to be a
// parser: a clause a writer wrapped in quotes is the same claim without them.
func unquote(text string) string {
	text = strings.TrimSpace(text)
	for _, pair := range [][2]string{{`"`, `"`}, {"'", "'"}} {
		if len(text) >= 2 && strings.HasPrefix(text, pair[0]) && strings.HasSuffix(text, pair[1]) {
			return strings.TrimSpace(text[1 : len(text)-1])
		}
	}
	return text
}

// accountClaims reads the work's OWN account of itself as a list of claims.
//
// Every non-empty line is one, and no attempt is made to tell an assertion from
// a hedge: a hunter that recognises nothing in a line costs nothing, and a
// reader that tried to decide in advance which sentences were claims would be
// the same guess this whole file exists to replace with a look. The account is
// bounded because it is prose somebody wrote at the end of a long run.
func accountClaims(account string) []claim {
	var claims []claim
	for _, raw := range strings.Split(account, "\n") {
		line := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(raw), "-*·• "))
		if line == "" || len(claims) >= claimLimit {
			continue
		}
		claims = append(claims, claim{text: clip(line, claimTextLimit), source: "the work's own account"})
	}
	return claims
}

// ── clauses and literals ────────────────────────────────────────────────────

// claimSeparators are what divides one clause of a claim from the next. A full
// stop is only a separator when a space follows it, so a version number and a
// money figure stay whole.
var claimSeparators = []string{". ", "; ", " — ", " – ", ": ", "\n"}

// claimClauses splits a claim into the parts a writer means separately.
func claimClauses(text string) []string {
	parts := []string{strings.TrimSpace(text)}
	for _, separator := range claimSeparators {
		var next []string
		for _, part := range parts {
			for _, piece := range strings.Split(part, separator) {
				if piece = strings.TrimSpace(piece); piece != "" {
					next = append(next, piece)
				}
			}
		}
		parts = next
	}
	// A trailing full stop belongs to the last clause and not to a clause of its
	// own, and it is taken off so a literal at the end of a sentence is found.
	for i, part := range parts {
		parts[i] = strings.TrimSpace(strings.TrimSuffix(part, "."))
	}
	return parts
}

// quotedSpans is every literal a clause quotes — in backticks, in double quotes
// or in single quotes.
//
// ONLY A QUOTED SPAN COUNTS. A writer marks the exact text they are talking
// about, and everything outside the marks is prose about it; a hunter that
// searched for unquoted words would go looking for "the" and "status" and find
// them everywhere.
func quotedSpans(clause string) []string {
	var spans []string
	for _, mark := range []byte{'`', '"', '\''} {
		rest := clause
		for {
			open := strings.IndexByte(rest, mark)
			if open < 0 {
				break
			}
			shut := strings.IndexByte(rest[open+1:], mark)
			if shut < 0 {
				break
			}
			span := rest[open+1 : open+1+shut]
			rest = rest[open+1+shut+1:]
			if span = strings.TrimSpace(span); len([]rune(span)) >= claimLiteralFloor {
				spans = append(spans, span)
			}
		}
	}
	return spans
}

// ── the search through what would ship ──────────────────────────────────────

// findInGround looks for one literal in the tree that would land, and answers
// where it found it — the path and the line, which is what makes a finding
// something somebody can go and check for themselves.
//
// THE LANDING'S OWN NOTES ARE NOT THE GROUND. A note asserting that `$0.00` is
// gone contains `$0.00` in the act of saying so, and so does every page that
// quotes the claim; reading them back would refute every true claim ever
// written. What settles a claim is the rest of the tree.
func findInGround(ground claimGround, literal string) (string, bool) {
	root := ground.dir
	var (
		files int
		bytes int
		where string
		found bool
	)
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if found {
			return fs.SkipAll
		}
		if err != nil {
			// An unreadable corner of a tree is not evidence about a claim.
			return nil
		}
		if entry.IsDir() {
			if skipDuringHunt(entry.Name()) && path != root {
				return fs.SkipDir
			}
			return nil
		}
		if files >= claimScanFiles || bytes >= claimScanBytes {
			return fs.SkipAll
		}
		relative := filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(path, root), string(filepath.Separator)))
		if ground.notes[relative] {
			return nil
		}
		info, statErr := entry.Info()
		if statErr != nil || info.Size() > claimFileLimit {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		files++
		bytes += len(body)
		if strings.IndexByte(string(body), 0) >= 0 {
			// Not text, so nothing anybody wrote a claim about.
			return nil
		}
		if at := lineHolding(string(body), literal); at > 0 {
			where, found = relative+":"+strconv.Itoa(at), true
			return fs.SkipAll
		}
		return nil
	})
	return where, found
}

// skipDuringHunt names the directories a hunt never walks: the repository's own
// bookkeeping, and the places a build puts things it made. Everything in them
// either is not text or is not anybody's claim.
func skipDuringHunt(name string) bool {
	switch name {
	case ".git", "node_modules", "target", "vendor", ".venv", "__pycache__", ".next", "dist", "build":
		return true
	}
	return false
}

// lineHolding answers which line of a document holds the literal, and zero when
// none does. The occurrence has to STAND ON ITS OWN — a literal that runs
// straight into a letter or a digit either side is part of a longer thing, and
// `$0` inside `$0.00` is a different string from the one somebody claimed was
// gone.
func lineHolding(body, literal string) int {
	for number, line := range strings.Split(body, "\n") {
		for at := 0; at < len(line); {
			found := strings.Index(line[at:], literal)
			if found < 0 {
				break
			}
			found += at
			if standsAlone(line, found, len(literal)) {
				return number + 1
			}
			at = found + 1
		}
	}
	return 0
}

// standsAlone reports whether an occurrence is the whole literal rather than the
// middle of a longer one.
func standsAlone(line string, at, width int) bool {
	if at > 0 && continuesLiteral(line[at-1]) {
		return false
	}
	after := at + width
	if after < len(line) && continuesLiteral(line[after]) {
		return false
	}
	// And a figure that runs on into a longer figure — `$0` inside `$0.00` — is
	// caught by the dot that a digit follows, which no boundary rule about
	// letters would see.
	if after+1 < len(line) && line[after] == '.' && line[after+1] >= '0' && line[after+1] <= '9' {
		return false
	}
	return true
}

// continuesLiteral reports whether a byte would make the text either side of an
// occurrence part of the same word or number.
func continuesLiteral(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9', b == '_':
		return true
	}
	return false
}

// readCapped reads at most n bytes of a file.
func readCapped(path string, n int) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	buffer := make([]byte, n)
	read, err := file.Read(buffer)
	if read == 0 && err != nil {
		return "", err
	}
	return string(buffer[:read]), nil
}

// ── what the check does with a checklist ────────────────────────────────────

// claimsChecklist is the whole of one landing's checklist, hunted.
type claimsChecklist struct {
	findings []claimFinding
}

// checklistFor reads a landing's claims and hunts every one of them in the tree
// that would ship.
func checklistFor(dir string, wrote []string, account string) claimsChecklist {
	claims := landingClaims(dir, wrote, account)
	ground := claimGround{dir: dir, wrote: wrote, notes: noteClaimSources(claims)}
	return claimsChecklist{findings: huntClaims(claims, ground)}
}

// broken is the claims the ground contradicts.
func (c claimsChecklist) broken() []claimFinding { return brokenClaims(c.findings) }

// open is the claims nothing settled.
func (c claimsChecklist) open() []claimFinding { return uncheckedClaims(c.findings) }

// brokenClaimAnswer is the check's answer when the ground contradicts what the
// landing says.
//
// IT IS AN ANSWER AND NOT AN ABSENCE OF ONE. Somebody looked — the search is the
// looking — and what they found is that a sentence the landing wrote is not so.
// That is a finding about the work, it goes back to the worker with the other
// findings, and the worker's job is the one it always is: make the claim true,
// or stop making it.
func brokenClaimAnswer(broken []claimFinding) auditVerdict {
	answer := auditVerdict{answered: true, word: auditRefuted}
	for _, finding := range broken {
		if len(answer.evidence) >= auditEvidenceLines {
			break
		}
		answer.evidence = append(answer.evidence, finding.line())
	}
	return answer
}

// claimsBlock is the checklist as the checker reads it.
//
// IT ASKS FOR THE ONES IT COULD NOT SETTLE BY NAME, which is the half that stops
// a claim being passed over quietly. A checker told only "check these" answers
// about the ones it happened to reach; a checker told to name what it could not
// settle produces the list of open questions the person actually needs.
func claimsBlock(open []claimFinding) string {
	if len(open) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("WHAT THIS LANDING CLAIMS — each line is an assertion about the world, and every one of them is " +
		"part of what you are checking. Hunt them: a claim about what something says is a search, a claim about what " +
		"something now does is a thing to run. Name any you could not settle rather than passing over it.\n")
	for _, finding := range open {
		out.WriteString("- " + finding.claim.text + " (" + finding.claim.source + ")\n")
	}
	out.WriteString("\n")
	return out.String()
}

// withOpenClaims adds, to a check that came back holding, the claims nothing
// ever settled.
//
// THE SILENT PASS IS THE FAILURE THIS FILE EXISTS FOR, so a landing whose
// written obligations went unexamined may not read as one whose obligations were
// met. Only DECLARED clauses are named — a written obligation nobody checked is
// news, while a line of the work's own prose nobody could settle is every task
// there has ever been, and a card that said so every time would teach a person
// to stop reading the line.
//
// AND A CLAIM THE CHECKER ITSELF ANSWERED IS NOT NAMED AGAIN. It is looked for
// in the checker's own evidence by the literal the claim quotes, which is the
// only fragment of a claim a checker writing in its own words would reproduce.
func withOpenClaims(answer auditVerdict, open []claimFinding) auditVerdict {
	if !answer.answered || !answer.verified {
		return answer
	}
	said := strings.ToLower(strings.Join(answer.evidence, "\n"))
	var named []string
	for _, finding := range open {
		if !finding.claim.declared || len(named) >= claimUncheckedNamed {
			continue
		}
		if claimSaidAlready(said, finding.claim) {
			continue
		}
		named = append(named, finding.claim.text)
	}
	if len(named) == 0 {
		return answer
	}
	answer.evidence = append(answer.evidence,
		clip("nothing checked this claim: "+strings.Join(named, " · "), taskReportLineLimit))
	return answer
}

// claimSaidAlready reports whether the checker's own evidence already speaks to
// a claim.
func claimSaidAlready(said string, made claim) bool {
	for _, clause := range claimClauses(made.text) {
		for _, literal := range quotedSpans(clause) {
			if strings.Contains(said, strings.ToLower(literal)) {
				return true
			}
		}
	}
	return false
}

// ── the divider is a landing too ────────────────────────────────────────────

// landingFiles is what a check stands on: what this node's own worker wrote, and
// what the parts it handed out wrote and merged into its tree.
//
// ── WHY THE SECOND HALF EXISTS ──
//
// A node that divides its work is a node whose tree, at the moment it merges
// upward, holds far more than its own worker ever wrote: every part came home
// into it. Before this, the check was handed the parent's OWN list — the files
// one worker touched — so the parts' work was laid over the ground as though it
// had always been there, and the biggest artifact of a divided run was the one
// thing nothing looked at. The parts were each checked, correctly, and then the
// tree they were assembled into went home unexamined.
//
// A DIVIDER IS A PART LIKE ANY OTHER. Its deliverable is the assembled tree, so
// the assembled tree is what its check stands on: staged the same way, restored
// the same way, and named in the checker's packet as what it is.
type landingFiles struct {
	// own is what this node's worker wrote.
	own []string
	// parts is what the parts it handed out wrote and brought home, in the order
	// the parts finished. The two halves are disjoint, and a path they share is
	// filed here — see [landingFilesFor].
	parts []string
}

// all is everything the landing carries — what the check stages, restores and
// hunts claims through.
func (f landingFiles) all() []string { return alsoChanged(f.own, f.parts) }

// divided reports whether any part contributed to this landing.
func (f landingFiles) divided() bool { return len(f.parts) > 0 }

// landingFilesFor reads what a node's landing carries, split into the two halves
// the checker's packet needs: the paths this node's own worker wrote, and the
// paths its parts wrote and brought home into the same tree.
//
// ONLY A PART THAT LANDED COUNTS. A part that was refused kept its branch and
// never merged, so its files are not in the parent's tree and claiming them would
// be the check standing on work that is not there.
//
// IT IS READ FROM THE PARTS RATHER THAN INTO THEM, and that is what makes it
// safe to call on a ledger that has ALREADY absorbed them (task_ledger.go). The
// parts are whatever the graph says landed; own is the rest of the list. So a
// re-audit handed the complete family ledger still sees its own half as its own
// and its parts' half as its parts', and `Files it wrote:` stays a true claim
// about this node however many times the list has been folded.
//
// A PATH BOTH WROTE IS FILED UNDER THE PARTS, and that is a decision rather
// than a detail. It used to be filed under the node, which cannot survive being
// asked twice: the second reading has no way to tell a path the node wrote from
// one it absorbed, so the split would drift with every re-audit. Filing it with
// the writer the graph can still name keeps one answer at every age — and it
// costs the packet nothing, because the path is named in the packet either way,
// staged either way, restored either way, and on [all] exactly once either way.
// The only thing that changes is which of the two true sentences carries it.
func landingFilesFor(node *TaskNode, changed []string) landingFiles {
	if node == nil || node.graph == nil {
		return landingFiles{own: changed}
	}
	// ONE SET, ONE PASS. The parts are gathered against the same `seen` the
	// split below reads, so a family of five parts costs one map and one walk of
	// each list rather than a fresh map per part and a linear scan of the node's
	// own list per path.
	seen := make(map[string]bool, len(changed))
	files := landingFiles{}
	for _, child := range node.graph.children(node.id) {
		if child.stateNow() != TaskDone {
			continue
		}
		_, wrote, _, _ := child.leavings()
		for _, path := range wrote {
			if seen[path] {
				continue
			}
			seen[path] = true
			files.parts = append(files.parts, path)
		}
	}
	if len(files.parts) == 0 {
		files.own = changed
		return files
	}
	for _, path := range changed {
		if seen[path] {
			continue
		}
		seen[path] = true
		files.own = append(files.own, path)
	}
	return files
}

// containsPath reports whether a list already names a path.
func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}

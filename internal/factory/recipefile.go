package factory

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The recipe file is a repository's recipe written the way the floor itself
// spells one, so a person edits it by hand and the floor writes it back:
//
//	# factory recipe · codeaf
//
//	## issue · ask
//	1. plan · chat · read the issue and say how · gate plan when large
//	2. write · chat · fanout 3
//
//	## policy
//	- tests pass before anything posts
//
//	## habits
//	- factory PRs from your own issues self-ship when the proof is green
//
// A stage line is `N. name · kind · ask · knob · knob …`. The kind is optional
// and chat when absent; the ask is the first segment that is neither a kind nor
// a knob; every other segment is a knob. A line with no ask is the default
// recipe's stage of that name for that kind, copied, with the line's own knobs
// laid over it, which is how `4. proof` stays short. A STAGE IS ON UNLESS ITS
// LINE SAYS `off`, copied or not, because a stage a person wrote into the file
// and found not running would be a line that silently means nothing.
//
// A kind's heading may end in one word for how much the plan stage may change
// that kind's stages: `## issue · adapt`, `## issue · ask` or `## issue ·
// fixed` ([AdaptMode]). No word is adapt, and [Format] writes the word only
// when it is not adapt.
//
// A STAGE LINE MAY END `· fixed`, and then the file binds that stage for
// everyone ([Stage.Fixed]): the manager, plan and the person may not change
// its ask, thinking or loop, or switch it off. A `## issue · fixed` heading
// marks every stage of its section fixed as well as keeping plan's hands off
// the list. The law changes only by changing the file, and the file is read
// from the repository's main branch ([RecipeSource]).
//
// THE FILE NEVER FAILS TO LOAD. A line the reader cannot make sense of is a
// [Problem] that names it, and everything else loads; a missing section is
// the default recipe's for that kind, and a missing file is the default recipe.

// RecipeFile is where a repository keeps its recipe, relative to its root.
const RecipeFile = ".codeaf/factory.md"

// recipeTitle is the file's first line.
const recipeTitle = "# factory recipe · codeaf"

// Problem is one line of a recipe file that did not load, and why. Line is
// 1-based; Text is the line as written.
type Problem struct {
	Line int
	Text string
	Why  string
}

func (p Problem) Error() string {
	return fmt.Sprintf("%s line %d: %s (%q)", RecipeFile, p.Line, p.Why, p.Text)
}

// sectionPolicy and sectionHabits are the two sections that are sentences
// rather than stages.
const (
	sectionPolicy = "policy"
	sectionHabits = "habits"
)

// fileKinds are the kinds a `## ` section may name, in the order [Format]
// writes them.
var fileKinds = []Kind{KindIssue, KindPR, KindCI, KindChore}

var reStageLine = regexp.MustCompile(`^(\d+)\.\s*(.*)$`)

// Parse reads a recipe file. It never panics and never refuses the
// whole file: each line it cannot read is a [Problem] and the rest loads.
func Parse(text string) (Recipe, []Problem) {
	var probs []Problem
	got := map[Kind][]Stage{}
	said := map[string]bool{}
	var adapt map[Kind]AdaptMode
	var policy, habits []string
	section := ""
	skip := false
	sectionFixed := false
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		n := i + 1
		bad := func(why string) { probs = append(probs, Problem{Line: n, Text: line, Why: why}) }
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "## "):
			name, word := sectionWord(line)
			skip = true
			sectionFixed = false
			switch {
			case said[name]:
				bad("this section is already in the file; the first one is read")
			case name == sectionPolicy || name == sectionHabits || knownKind(name):
				skip = false
			default:
				bad("not a section codeaf knows: issue, pr, ci, chore, policy or habits")
			}
			if !skip && word != "" {
				switch {
				case !knownKind(name):
					bad(name + " takes no word after it")
				case !oneOf(word, AdaptWords):
					bad("after the kind comes " + strings.Join(AdaptWords, ", ") + "; " + string(AdaptFree) + " when there is none")
				case AdaptMode(word) != AdaptFree:
					if adapt == nil {
						adapt = map[Kind]AdaptMode{}
					}
					adapt[Kind(name)] = AdaptMode(word)
					sectionFixed = AdaptMode(word) == AdaptFixed
				}
			}
			said[name] = true
			section = name
			continue
		case strings.HasPrefix(line, "# "):
			continue
		}
		if skip {
			continue
		}
		switch section {
		case "":
			bad("a line before any section")
		case sectionPolicy, sectionHabits:
			s, ok := bullet(line)
			if !ok {
				bad("a policy or habit is one line starting with -")
				continue
			}
			if section == sectionPolicy {
				policy = append(policy, s)
			} else {
				habits = append(habits, s)
			}
		default:
			st, whys, ok := parseStageLine(Kind(section), line)
			for _, why := range whys {
				bad(why)
			}
			switch {
			case ok && len(got[Kind(section)]) >= StageMost:
				bad(ErrNineStages.Error())
			case ok:
				if sectionFixed {
					st.Fixed = true
				}
				got[Kind(section)] = append(got[Kind(section)], st)
			}
		}
	}
	r := DefaultRecipe()
	if s := got[KindIssue]; len(s) > 0 {
		r.Stages = s
	}
	for k, s := range got {
		if k != KindIssue && len(s) > 0 {
			r.ByKind[k] = s
		}
	}
	r.Policy, r.Habits, r.Adapt = policy, habits, adapt
	// A FIXED SECTION WITH NO LINES OF ITS OWN still binds the stages its
	// kind falls back to, copied so the default recipe stays as it was.
	for k, m := range adapt {
		if m != AdaptFixed || len(got[k]) > 0 {
			continue
		}
		ss := CopyStages(r.For(k))
		for i := range ss {
			ss[i].Fixed = true
		}
		if k == KindIssue {
			r.Stages = ss
		} else {
			r.ByKind[k] = ss
		}
	}
	// A section that fell back to the default says its kind out loud too, so
	// a recipe read from a file is the same value it is once written back.
	chat := func(ss []Stage) {
		for i := range ss {
			if ss[i].Kind == "" {
				ss[i].Kind = StageChat
			}
		}
	}
	chat(r.Stages)
	for _, ss := range r.ByKind {
		chat(ss)
	}
	return r, probs
}

func sectionName(line string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimPrefix(line, "##")), " "))
}

// sectionWord is a heading's section and the word after its `·`, if any:
// `## issue · ask` is issue and ask.
func sectionWord(line string) (name, word string) {
	name, word, _ = strings.Cut(sectionName(line), "·")
	return strings.TrimSpace(name), strings.TrimSpace(word)
}

func knownKind(name string) bool {
	for _, k := range fileKinds {
		if string(k) == name {
			return true
		}
	}
	return false
}

func bullet(line string) (string, bool) {
	for _, p := range []string{"- ", "* "} {
		if s, ok := strings.CutPrefix(line, p); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s), true
		}
	}
	return "", false
}

// parseStageLine reads one `N. name · …` line. ok is false only when there is
// no stage to keep at all; a bad knob is a reason in whys and the stage loads
// without it.
func parseStageLine(k Kind, line string) (st Stage, whys []string, ok bool) {
	m := reStageLine.FindStringSubmatch(line)
	if m == nil {
		return Stage{}, []string{"a stage is one line: N. name · kind · ask · knobs"}, false
	}
	segs := strings.Split(m[2], "·")
	name := strings.TrimSpace(segs[0])
	if name == "" {
		return Stage{}, []string{"a stage needs a name"}, false
	}
	name, err := StageWord(name)
	if err != nil {
		return Stage{}, []string{err.Error()}, false
	}
	var kind StageKind
	var ask string
	var knobs []func(*Stage)
	for _, seg := range segs[1:] {
		seg = strings.Join(strings.Fields(seg), " ")
		if seg == "" {
			continue
		}
		if sk := StageKind(strings.ToLower(seg)); oneOfKind(sk) {
			if kind != "" {
				whys = append(whys, fmt.Sprintf("%q is a second kind; a stage has one", seg))
				continue
			}
			kind = sk
			continue
		}
		if knob, isKnob, why := parseKnob(seg); isKnob {
			if why != "" {
				whys = append(whys, why)
				continue
			}
			knobs = append(knobs, knob)
			continue
		}
		if ask != "" {
			whys = append(whys, fmt.Sprintf("%q is not a knob, and the ask was already %q", seg, ask))
			continue
		}
		ask = seg
	}
	st = Stage{Name: name, Ask: ask}
	if ask == "" {
		if i := StageIndex(DefaultRecipe().For(k), name); i >= 0 {
			st = CopyStages(DefaultRecipe().For(k)[i : i+1])[0]
		}
	}
	if kind != "" {
		st.Kind = kind
	}
	if st.Kind == "" {
		st.Kind = StageChat
	}
	st.On = true
	for _, f := range knobs {
		f(&st)
	}
	return st, whys, true
}

func oneOfKind(k StageKind) bool {
	for _, s := range StageKinds {
		if k == s {
			return true
		}
	}
	return false
}

// parseKnob reads one segment as a knob. isKnob says the segment starts with a
// knob's word; why, when not empty, says what is wrong with the rest of it. A
// segment that starts with a knob's word is never an ask, so `until clen` is
// a typo the file names rather than a stage whose ask is `until clen`.
func parseKnob(seg string) (knob func(*Stage), isKnob bool, why string) {
	low := strings.ToLower(seg)
	word, rest, _ := strings.Cut(low, " ")
	rest = strings.TrimSpace(rest)
	switch word {
	case "fixed":
		// `fixed` alone is the knob; `fixed bugs get a test` is an ask.
		if rest != "" {
			return nil, false, ""
		}
		return func(s *Stage) { s.Fixed = true }, true, ""
	case "off":
		if rest != "" {
			return nil, true, "off takes no words"
		}
		return func(s *Stage) { s.On = false }, true, ""
	case "on":
		if rest != "" {
			return nil, true, "on takes no words"
		}
		return func(s *Stage) { s.On = true }, true, ""
	case "when":
		if !oneOf(rest, WhenWords) {
			return nil, true, "when is one of " + strings.Join(WhenWords, ", ")
		}
		return func(s *Stage) { s.When = rest }, true, ""
	case "until":
		if !oneOf(rest, untilWords) {
			return nil, true, "until is one of " + strings.Join(untilWords, ", ")
		}
		return func(s *Stage) { s.Until = rest }, true, ""
	case "max", "rounds":
		n, err := strconv.Atoi(rest)
		if err != nil || n < 1 {
			return nil, true, word + " is a number of rounds, 1 or more"
		}
		return func(s *Stage) { s.Max = n }, true, ""
	case "fanout":
		f, ok := fanoutWord(rest)
		if !ok {
			return nil, true, "fanout is a number, one, or per finding, per file, per claim"
		}
		return func(s *Stage) { s.Fanout = f }, true, ""
	case "gate":
		g, cond, _ := strings.Cut(rest, " when ")
		g, cond = strings.TrimSpace(g), strings.TrimSpace(cond)
		switch Gate(g) {
		case GatePlan, GateShip, GateNone:
		default:
			return nil, true, "gate is plan, ship or none, and may end `when <condition>`"
		}
		if cond != "" && !oneOf(cond, WhenWords) {
			return nil, true, "a gate's when is one of " + strings.Join(WhenWords, ", ")
		}
		// THE CONDITION IS THE GATE'S, NEVER THE STAGE'S: `gate plan when
		// large` runs plan on every item and stops for the person only on a
		// large one ([GateApplies]).
		return func(s *Stage) {
			s.Gate = Gate(g)
			s.GateWhen = cond
		}, true, ""
	case "effort":
		if !oneOf(rest, EffortWords) {
			return nil, true, "effort is cheap or strong"
		}
		return func(s *Stage) { s.Effort = rest }, true, ""
	case "proof":
		// The proof's own words keep the case they were written in.
		_, words, _ := strings.Cut(seg, " ")
		var list []string
		for _, p := range strings.Split(words, ",") {
			if p = strings.TrimSpace(p); p != "" {
				list = append(list, p)
			}
		}
		if len(list) == 0 {
			return nil, true, "proof names what the stage must show, separated by commas"
		}
		return func(s *Stage) { s.Proof = list }, true, ""
	}
	return nil, false, ""
}

// fanoutWord reads a fanout knob into the stage's spelling: `per finding` is
// kept as per-finding, the way [DefaultRecipe] spells it.
func fanoutWord(w string) (string, bool) {
	if w == "one" {
		return w, true
	}
	if n, err := strconv.Atoi(w); err == nil && n > 0 {
		return w, true
	}
	if each, ok := strings.CutPrefix(strings.ReplaceAll(w, "-", " "), "per "); ok && each != "" && !strings.Contains(each, " ") {
		return "per-" + each, true
	}
	return "", false
}

// Format writes r in the recipe file's shape: the title, a section per
// kind (issue first, then pr, ci, chore and any other kind by name), then the
// policy and the habits. Every stage is written whole, its knobs in one fixed
// order, even one equal to the default, so the file reads whole without the
// default beside it. An empty policy or habits section is not written. A
// kind's adapt word is written after its heading when it is not adapt, and a
// kind with a word and no stages of its own writes the stages it falls back
// to, so the word is never dropped for want of a section to carry it.
func Format(r Recipe) string {
	var b strings.Builder
	b.WriteString(recipeTitle + "\n")
	section := func(name string, lines []string) {
		if len(lines) == 0 {
			return
		}
		b.WriteString("\n## " + name + "\n")
		for _, l := range lines {
			b.WriteString(l + "\n")
		}
	}
	kindSection := func(k Kind, stages []Stage) {
		head := string(k)
		m := r.AdaptFor(k)
		if m != AdaptFree {
			head += " · " + string(m)
			if len(stages) == 0 {
				stages = r.For(k)
			}
		}
		if m == AdaptFixed {
			// The heading says fixed for every line; a line need not.
			stages = unfixed(stages)
		}
		section(head, StageLines(stages))
	}
	kindSection(KindIssue, r.For(KindIssue))
	var rest []Kind
	for k := range r.ByKind {
		if k != KindIssue && !knownKind(string(k)) {
			rest = append(rest, k)
		}
	}
	for k := range r.Adapt {
		if _, ok := r.ByKind[k]; !ok && k != KindIssue && !knownKind(string(k)) {
			rest = append(rest, k)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i] < rest[j] })
	for _, k := range append(append([]Kind{}, fileKinds[1:]...), rest...) {
		kindSection(k, r.ByKind[k])
	}
	section(sectionPolicy, bullets(r.Policy))
	section(sectionHabits, bullets(r.Habits))
	return b.String()
}

// unfixed is a copy of stages with no stage marked fixed, for a section whose
// heading already says it.
func unfixed(stages []Stage) []Stage {
	out := CopyStages(stages)
	for i := range out {
		out[i].Fixed = false
	}
	return out
}

func bullets(in []string) []string {
	var out []string
	for _, s := range in {
		if s = oneLine(s); s != "" {
			out = append(out, "- "+s)
		}
	}
	return out
}

// oneLine is a sentence fit for one line of the file.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// StageLines are stages as the file's numbered lines.
func StageLines(stages []Stage) []string {
	out := make([]string, 0, len(stages))
	for i, s := range stages {
		out = append(out, strconv.Itoa(i+1)+". "+stageLine(s))
	}
	return out
}

// stageLine is one stage after its number. The knobs come in one order: when,
// until, max, fanout, gate, effort, proof, off, fixed. A gate with a condition of its
// own ([Stage.GateWhen]) writes it as `gate plan when large`, the way the owner
// spells it, and the stage's own when stays its own knob.
func stageLine(s Stage) string {
	kind := s.Kind
	if kind == "" {
		kind = StageChat
	}
	parts := []string{oneLine(s.Name), string(kind)}
	if a := oneLine(s.Ask); a != "" {
		parts = append(parts, a)
	}
	if s.When != "" {
		parts = append(parts, "when "+s.When)
	}
	if s.Until != "" {
		parts = append(parts, "until "+s.Until)
	}
	if s.Max > 0 {
		parts = append(parts, "max "+strconv.Itoa(s.Max))
	}
	if s.Fanout != "" {
		parts = append(parts, "fanout "+strings.ReplaceAll(s.Fanout, "-", " "))
	}
	if s.Gate != "" {
		g := "gate " + string(s.Gate)
		if s.GateWhen != "" {
			g += " when " + s.GateWhen
		}
		parts = append(parts, g)
	}
	if s.Effort != "" {
		parts = append(parts, "effort "+s.Effort)
	}
	if len(s.Proof) > 0 {
		parts = append(parts, "proof "+strings.Join(s.Proof, ", "))
	}
	if !s.On {
		parts = append(parts, "off")
	}
	if s.Fixed {
		parts = append(parts, "fixed")
	}
	return strings.Join(parts, " · ")
}

// Load reads the recipe of the repository checked out at repoDir, through
// [RecipeSource]: the main branch's copy of .codeaf/factory.md, the working
// file only when there is no git or no main branch to read, and the default
// recipe when neither has one. A missing file is the default recipe and no
// error; a file that cannot be read is the default recipe and the error, so a
// caller drawing a floor can still draw it.
//
// THE RECIPE IS READ FROM THE MAIN BRANCH, NEVER FROM AN ITEM'S BRANCH, so a
// pull request cannot change the law for its own review. A caller must never
// hand in a worktree an item's run made.
func Load(repoDir string) (Recipe, []Problem, error) {
	text, _, err := RecipeSource(repoDir)
	if err != nil {
		return DefaultRecipe(), nil, err
	}
	if text == "" {
		return DefaultRecipe(), nil, nil
	}
	r, probs := Parse(text)
	return r, probs, nil
}

// Save writes r to <repoDir>/.codeaf/factory.md, making .codeaf when it
// is not there. The write is whole or not at all: a temporary file beside it,
// renamed over.
func Save(repoDir string, r Recipe) error {
	return writeRecipeText(repoDir, Format(r))
}

func writeRecipeText(repoDir, text string) error {
	path := filepath.Join(repoDir, RecipeFile)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".factory-*.md")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// recipeText is the file as it stands, or the default recipe written out when
// there is none, so a first bank writes a file that reads whole.
func recipeText(repoDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(repoDir, RecipeFile))
	if errors.Is(err, fs.ErrNotExist) {
		return Format(DefaultRecipe()), nil
	}
	return string(data), err
}

// BankRecipeStages writes stages into the file's section for kind and leaves
// every other section as it was written, problems and all: A BANK CHANGES ONE
// SECTION, so a line the person mistyped elsewhere is still theirs to fix, not
// something the floor quietly dropped on the way through.
func BankRecipeStages(repoDir string, kind Kind, stages []Stage) error {
	if kind == "" {
		kind = KindIssue
	}
	if len(stages) == 0 {
		return errors.New("there are no stages to bank")
	}
	text, err := recipeText(repoDir)
	if err != nil {
		return err
	}
	f := splitSections(text)
	if i := f.find(string(kind)); i >= 0 {
		if _, word := sectionWord(f.secs[i].head); AdaptMode(word) == AdaptFixed {
			stages = unfixed(stages)
		}
	}
	f.replace(string(kind), StageLines(stages))
	return writeRecipeText(repoDir, f.join())
}

// BankRecipeHabit appends a sentence to the file's habits, once: a sentence
// already there is not written twice.
func BankRecipeHabit(repoDir, sentence string) error {
	sentence = oneLine(sentence)
	if sentence == "" {
		return errors.New("say the habit")
	}
	text, err := recipeText(repoDir)
	if err != nil {
		return err
	}
	f := splitSections(text)
	i := f.find(sectionHabits)
	if i < 0 {
		f.secs = append(f.secs, recipeSection{head: "## " + sectionHabits})
		i = len(f.secs) - 1
	}
	for _, l := range f.secs[i].body {
		if s, ok := bullet(strings.TrimSpace(l)); ok && s == sentence {
			return nil
		}
	}
	f.secs[i].body = append(trimBlank(f.secs[i].body), "- "+sentence)
	return writeRecipeText(repoDir, f.join())
}

// BankRecipePolicy appends a sentence to the file's policy, once, exactly as
// [BankRecipeHabit] appends a habit: a sentence already there is not written
// twice, and every other section is left as it was written.
func BankRecipePolicy(repoDir, sentence string) error {
	sentence = oneLine(sentence)
	if sentence == "" {
		return errors.New("say the policy")
	}
	text, err := recipeText(repoDir)
	if err != nil {
		return err
	}
	f := splitSections(text)
	i := f.find(sectionPolicy)
	if i < 0 {
		// THE POLICY GOES BEFORE THE HABITS when the file has none yet, which
		// is the order [Format] writes them in.
		at := len(f.secs)
		if h := f.find(sectionHabits); h >= 0 {
			at = h
		}
		f.secs = append(f.secs[:at], append([]recipeSection{{head: "## " + sectionPolicy}}, f.secs[at:]...)...)
		i = at
	}
	for _, l := range f.secs[i].body {
		if s, ok := bullet(strings.TrimSpace(l)); ok && s == sentence {
			return nil
		}
	}
	f.secs[i].body = append(trimBlank(f.secs[i].body), "- "+sentence)
	return writeRecipeText(repoDir, f.join())
}

// recipeFile is a recipe file cut at its `## ` headings, each section's lines
// kept exactly as written.
type recipeFile struct {
	pre  []string
	secs []recipeSection
}

type recipeSection struct {
	head string
	body []string
}

func splitSections(text string) recipeFile {
	var f recipeFile
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "## ") {
			f.secs = append(f.secs, recipeSection{head: strings.TrimSpace(l)})
			continue
		}
		if len(f.secs) == 0 {
			f.pre = append(f.pre, l)
		} else {
			f.secs[len(f.secs)-1].body = append(f.secs[len(f.secs)-1].body, l)
		}
	}
	return f
}

func (f recipeFile) find(name string) int {
	for i, s := range f.secs {
		if n, _ := sectionWord(s.head); n == name {
			return i
		}
	}
	return -1
}

// replace sets a stage section's lines. A kind the file has no section for
// gains one before the policy and the habits, which close the file.
func (f *recipeFile) replace(name string, lines []string) {
	if i := f.find(name); i >= 0 {
		f.secs[i].body = lines
		return
	}
	at := len(f.secs)
	for i, s := range f.secs {
		if n, _ := sectionWord(s.head); n == sectionPolicy || n == sectionHabits {
			at = i
			break
		}
	}
	sec := recipeSection{head: "## " + name, body: lines}
	f.secs = append(f.secs[:at], append([]recipeSection{sec}, f.secs[at:]...)...)
}

// join writes the file back with one blank line between sections.
func (f recipeFile) join() string {
	out := trimBlank(f.pre)
	for _, s := range f.secs {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, s.head)
		out = append(out, trimBlank(s.body)...)
	}
	return strings.Join(out, "\n") + "\n"
}

func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

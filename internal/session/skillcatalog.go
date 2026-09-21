package session

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	store "github.com/Agent-Field/codeaf/internal/store"
)

// THE SKILL CATALOG is the one place the conversation is shown the shelf it
// already owns: the skills the distiller has promoted and the resident has
// installed, each named by the command a worker would run and described by the
// line the notebook holds. It is DYNAMIC CONTENT rendered as a section, not a
// session fact — beltfacts.go's facts are sentences about which TOOLS a belt
// carries, and a skill is neither a tool nor a property of the shape: which
// ones exist depends on the notebook, not on the config.
//
// IT IS WINDOWED ON PURPOSE. This section is prepended to every request, so its
// bytes are paid on every round of every turn (prefixbudget_test.go). A person
// whose shelf has grown past a handful of entries must not pay for the tail on
// every call, so the catalog scores the skills against the little context the
// prompt has — the workspace, the project's name, the date — and keeps only the
// few most likely to matter.
const (
	// skillCatalogMaxLines bounds how many skill bullets the catalog may carry.
	//
	// IT IS SMALL BECAUSE IT IS A PROMPT PREFIX BUDGET. The section rides in
	// front of every request, so every line is bought again on each round of
	// each turn; eight lines is about the size of the belt-fact sections it sits
	// beside, and anything past it is a roll call rather than a menu. The
	// overflow is not hidden — it is summarised as "- … and N more skills", and
	// the whole shelf is always one `recall` away.
	skillCatalogMaxLines = 8

	// skillCatalogScanLimit is how far into the active shelf the catalog reads
	// before it scores. It is deliberately larger than [skillCatalogMaxLines] so
	// the scorer has something to choose BETWEEN rather than merely the newest
	// handful, and small enough that a machine with a thousand skills still
	// reads a bounded slice per render.
	skillCatalogScanLimit = 50

	// skillCatalogHeader is the section heading and the one sentence saying how
	// a skill is reached. It uses `## ` and not `# ` because a top-level heading
	// is the unit the lean profile drops (promptprofile.go's [leanPageSections]),
	// and the shelf is not a law to be traded against window size.
	skillCatalogHeader = "## Available skills\n\n" +
		"Skills are prepended to task work — use them through task nodes or by name with `use_skill`.\n"
)

// renderSkillCatalog composes the skill catalog for one config, or the empty
// string when there is nothing to show.
//
// AN EMPTY SHELF IS ZERO BYTES, which is the whole reason the section is
// conditional: a person with no skills must not pay a heading that names
// nothing, and a store-less shape (a standing check's probe, a fork's hand) must
// render byte-for-byte what it rendered before this file existed.
func renderSkillCatalog(config Config) string {
	if config.Memory == nil {
		return ""
	}
	active, err := config.Memory.SkillFacts(store.FactActive, skillCatalogScanLimit)
	if err != nil || len(active) == 0 {
		return ""
	}

	scored := scoreSkills(active, config.Workspace)
	sort.SliceStable(scored, func(first, second int) bool {
		return scored[first].score > scored[second].score
	})

	shown := scored
	hidden := 0
	if len(shown) > skillCatalogMaxLines {
		hidden = len(shown) - skillCatalogMaxLines
		shown = shown[:skillCatalogMaxLines]
	}

	var out strings.Builder
	out.WriteString(skillCatalogHeader)
	out.WriteByte('\n')
	for _, skill := range shown {
		name := filepath.Base(strings.TrimSpace(skill.fact.Artifact))
		if name == "" || name == "." {
			name = strings.TrimSpace(skill.fact.Scope)
		}
		out.WriteString("- ")
		out.WriteString(name)
		out.WriteString(": ")
		out.WriteString(strings.TrimSpace(skill.fact.Body))
		out.WriteByte('\n')
	}
	if hidden > 0 {
		out.WriteString("- … and ")
		out.WriteString(strconv.Itoa(hidden))
		out.WriteString(" more skills\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

// scoredSkill is one skill and the weight this prompt gave it.
type scoredSkill struct {
	fact  store.Fact
	score int
}

// scoreSkills ranks the active shelf against what the prompt knows about this
// moment. It is a HEURISTIC and deliberately not a model call: a weighted sum of
// three cheap signals, in the order they matter.
//
//   - SCOPE MATCH — a skill whose scope names something in front of the model
//     (the workspace, the folder under it, the project's name) is almost
//     certainly about the work at hand, so it outweighs anything else.
//   - WORD OVERLAP — a doc line sharing words with that same context is a
//     weaker, fuzzier signal of relevance, so it is a smaller bonus per word.
//   - RECENCY — a skill used more recently is likelier to be the one wanted, so
//     fresh use is a small tie-breaker. Most skills have never been used, and
//     [Fact.LastUsed] is zero for them, which is exactly the stable default the
//     sort keeps in journal order (newest first, as [Store.SkillFacts] returns
//     them).
func scoreSkills(facts []store.Fact, workspace string) []scoredSkill {
	context := contextWords(workspace)
	now := time.Now()
	scored := make([]scoredSkill, 0, len(facts))
	for _, fact := range facts {
		score := 0
		if scopeMatchesContext(fact.Scope, context) {
			score += 100
		}
		for _, word := range docWords(fact.Body) {
			if context[word] {
				score += 5
			}
		}
		if !fact.LastUsed.IsZero() {
			// A recency bonus that stays under the scope and word weights: used
			// within the last week scores highest, and anything older than a
			// month is a tie-breaker at most.
			switch age := now.Sub(fact.LastUsed); {
			case age < 7*24*time.Hour:
				score += 3
			case age < 30*24*time.Hour:
				score += 1
			}
		}
		scored = append(scored, scoredSkill{fact: fact, score: score})
	}
	return scored
}

// contextWords is the set of lowercase words the prompt already knows: the
// workspace path's own components. It is the whole of the "prompt context"
// available to a function handed only a [Config], and it is enough — a skill
// scoped to `repo:/…/codeaf` or describing a path under the working directory
// shares a word with it.
func contextWords(workspace string) map[string]bool {
	words := make(map[string]bool)
	for _, part := range strings.FieldsFunc(workspace, func(r rune) bool {
		return r == '/' || r == '\\' || r == ':' || r == '.' || r == '-' || r == '_' || r == ' '
	}) {
		if part = strings.ToLower(strings.TrimSpace(part)); part != "" {
			words[part] = true
		}
	}
	return words
}

// docWords is the comparable words of one doc line.
func docWords(doc string) []string {
	fields := strings.FieldsFunc(strings.ToLower(doc), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	words := fields[:0]
	for _, field := range fields {
		if len(field) >= 3 {
			words = append(words, field)
		}
	}
	return words
}

// scopeMatchesContext says whether a skill's scope names something the prompt
// carries. A scope is `kind:value` ("repo:/path", "tool:git", "domain:x"), so
// each side is compared against the context words: a scope of `repo:/…/codeaf`
// matches a workspace that ends in `codeaf`, and `tool:git` matches nothing
// unless the path happens to say `git`.
func scopeMatchesContext(scope string, context map[string]bool) bool {
	for _, part := range strings.FieldsFunc(strings.ToLower(scope), func(r rune) bool {
		return r == ':' || r == '/' || r == '\\' || r == '.' || r == '-' || r == '_' || r == ' '
	}) {
		if part != "" && context[part] {
			return true
		}
	}
	return false
}

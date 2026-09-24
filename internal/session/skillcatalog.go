package session

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	store "github.com/Agent-Field/codeaf/internal/store"
)

// THE SKILL CATALOG is the one place the conversation is shown the whole shelf
// it can use: every skill a person installed for another harness and every one
// the distiller promoted, each by its name and the line that says what it is
// for. It is DYNAMIC CONTENT rendered as a section, not a session fact —
// beltfacts.go's facts are sentences about which TOOLS a belt carries, and a
// skill is neither a tool nor a property of the shape: which ones exist
// depends on the shelf, not on the config.
//
// IT IS HOW A SKILL IS CHOSEN BY MEANING. The skills a message carries
// (skillturn.go) are picked by the words the message shares with a skill's
// description, which is cheap and literal: "make me a deck for the board"
// shares no word with "create and edit presentation slides". A model reading
// the whole catalog makes that connection itself, the way Claude Code's own
// skill list works — the description is in front of it on every request, it
// decides a skill applies, and it fetches the body with `use_skill`. So the
// catalog names EVERY skill, rather than the few a path-based score guessed at,
// and the per-message pass stays as the cheap first guess beside it.
//
// IT IS BOUNDED BY BYTES AND IT IS STABLE. The section rides in front of every
// request (prefixbudget_test.go), so its size is a cost paid on every round of
// every turn — which a prompt cache discounts only while the bytes stay the
// same. So each skill costs at most one line of [skillCatalogDocRunes] runes of
// description plus its name, the lines stop at [skillCatalogBudget] bytes, the
// names of the skills past that are listed alone up to [skillCatalogNamesBudget]
// more, and anything past that is counted. The order is fixed for a given
// shelf and workspace — the skills scoped to the work first, then by name — so
// two requests over one shelf render the same bytes.
const (
	// skillCatalogDocRunes bounds one skill's description in the catalog.
	// A description a skill's author wrote for Claude Code's own list can run to
	// a thousand characters; the first line or so of it is what decides whether
	// the skill applies, and the rest is one `use_skill` away.
	skillCatalogDocRunes = 160

	// skillCatalogBudget bounds the described lines, in bytes. At the most a
	// line can cost — a 64-rune name, the separators and a full description,
	// about 230 bytes — it holds fifty skills; at the 120 to 170 bytes a real
	// shelf's lines measure, seventy to a hundred. It is about a fifth of the
	// fixed prefix the page and the belt already cost (prefixbudget_test.go), and
	// it is paid only by a person who has that many skills.
	skillCatalogBudget = 12 * 1024

	// skillCatalogNamesBudget bounds the names-only line for the skills whose
	// described lines did not fit: a name alone is still something a model can
	// fetch, at a tenth of the cost of its description.
	skillCatalogNamesBudget = 2 * 1024

	// skillCatalogScanLimit is how far into the active shelf the catalog reads,
	// from the one bound every shelf reader shares.
	skillCatalogScanLimit = store.SkillShelfLimit

	// skillCatalogHeader is the section heading and the sentence saying how a
	// skill is used. It uses `## ` and not `# ` because a top-level heading is
	// the unit the lean profile drops (promptprofile.go's [leanPageSections]),
	// and the shelf is not a law to be traded against window size.
	skillCatalogHeader = "## Available skills\n\n" +
		"Procedures installed for this project and this machine, each with what it is for. " +
		"When a request's work fits one, even in none of its words, fetch it with `use_skill` (mode get) and follow it before starting: " +
		"before any other tool and before answering. Skills suited to a message's words are also attached to that message.\n"

	// skillCatalogNamesLead opens the line of skills listed by name alone.
	skillCatalogNamesLead = "- also on the shelf (fetch by name): "
)

// skillShelf is the store the skill shelf is read from: [Config.Skills] when a
// door named one, and otherwise the store this session remembers into. Every
// reader of the shelf — this catalog, the skills a message carries,
// `use_skill` and the page's own row for it — asks here, so the belt, the
// prompt and the message can never be reading two different shelves.
//
// MEMORY OFF IS NO LONGER SKILLS OFF. The live door hands a shelf of its own
// when it hands no memory (Config.Skills says why), so the sentence this
// catalog once rendered for that case — skills exist, memory is off, turn it
// on — would now be false, and is gone rather than kept for a door that
// still names no shelf: such a door has no skills to explain.
func (c Config) skillShelf() *store.Store {
	if c.Skills != nil {
		return c.Skills
	}
	return c.Memory
}

// renderSkillCatalog composes the skill catalog for one config, or the empty
// string when there is nothing to show.
//
// AN EMPTY SHELF IS ZERO BYTES, which is the whole reason the section is
// conditional: a person with no skills must not pay a heading that names
// nothing, and a store-less shape (a standing check's probe, a fork's hand) must
// render byte-for-byte what it rendered before this file existed.
//
// AND A BELT WITHOUT `use_skill` GETS NO CATALOG. The section's whole use is
// choosing a skill to fetch, and it names the verb that fetches one; a worker
// on the floor of its tree has no such verb (tools_skill.go), and a menu it can
// only read is a menu that promises a call it cannot make. The skills its
// brief carried still reach it with the brief.
func renderSkillCatalog(config Config) string {
	shelf := config.skillShelf()
	if shelf == nil || !config.mayProposeTask() {
		return ""
	}
	active, err := shelf.SkillFacts(store.FactActive, skillCatalogScanLimit)
	if err != nil || len(active) == 0 {
		return ""
	}

	scored := scoreSkills(active, config.Workspace)
	sort.SliceStable(scored, func(first, second int) bool {
		if scored[first].score != scored[second].score {
			return scored[first].score > scored[second].score
		}
		return catalogName(scored[first].fact) < catalogName(scored[second].fact)
	})

	var out strings.Builder
	out.WriteString(skillCatalogHeader)
	out.WriteByte('\n')
	spent := 0
	named := make([]string, 0)
	for _, skill := range scored {
		name := catalogName(skill.fact)
		line := "- " + name + ": " + clipRunes(oneCatalogLine(skill.fact.Body), skillCatalogDocRunes) + "\n"
		if len(named) == 0 && spent+len(line) <= skillCatalogBudget {
			out.WriteString(line)
			spent += len(line)
			continue
		}
		named = append(named, name)
	}
	hidden := 0
	if len(named) > 0 {
		out.WriteString(skillCatalogNamesLead)
		written := 0
		for index, name := range named {
			cost := len(name) + 2
			if written+cost > skillCatalogNamesBudget {
				hidden = len(named) - index
				break
			}
			if index > 0 {
				out.WriteString(", ")
			}
			out.WriteString(name)
			written += cost
		}
		out.WriteByte('\n')
	}
	if hidden > 0 {
		out.WriteString("- … and ")
		out.WriteString(strconv.Itoa(hidden))
		out.WriteString(" more skills — `use_skill` list shows them\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

// catalogName is the name a skill is fetched by: its folder's name, which is
// the one identity every shelf reader keys on ([store.Fact.SkillName]).
func catalogName(fact store.Fact) string {
	name := filepath.Base(strings.TrimSpace(fact.Artifact))
	if name == "" || name == "." || name == "/" {
		name = strings.TrimSpace(fact.Scope)
	}
	return name
}

// oneCatalogLine is a description on one line, whatever its author's
// line breaks were.
func oneCatalogLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// clipRunes cuts a description to at most limit runes, on a word boundary when
// one is near, and marks the cut so a reader knows there is more.
func clipRunes(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	cut := string(runes[:limit-1])
	if space := strings.LastIndexByte(cut, ' '); space > len(cut)*3/4 {
		cut = cut[:space]
	}
	return strings.TrimRight(cut, " ,;:") + "…"
}

// scoredSkill is one skill and the weight this prompt gave it.
type scoredSkill struct {
	fact  store.Fact
	score int
}

// scoreSkills ranks the active shelf against what the prompt knows about this
// moment. It is a HEURISTIC and deliberately not a model call: a weighted sum of
// two cheap signals, in the order they matter.
//
//   - SCOPE MATCH — a skill whose scope names something in front of the model
//     (the workspace, the folder under it, the project's name) is almost
//     certainly about the work at hand, so it outweighs anything else.
//   - WORD OVERLAP — a doc line sharing words with that same context is a
//     weaker, fuzzier signal of relevance, so it is a smaller bonus per word.
//
// NOTHING THAT MOVES BETWEEN TWO REQUESTS IS A SIGNAL HERE. It once gave a
// recently used skill a small bonus, and a use in the middle of a conversation
// then reordered the section and cost the whole cached prefix behind it. The
// order only decides which lines fit the budget, and a shelf small enough to
// fit whole is not ranked by it at all.
func scoreSkills(facts []store.Fact, workspace string) []scoredSkill {
	context := contextWords(workspace)
	scored := make([]scoredSkill, 0, len(facts))
	for _, fact := range facts {
		score := 0
		if scopeMatchesContext(fact.Scope, workspace, context) {
			score += 100
		}
		for _, word := range docWords(fact.Body) {
			if context[word] {
				score += 5
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
// carries. For a repository scope (`repo:/path`), it matches only if the workspace
// path is within the repo, or if the repository base name matches the workspace base
// name or appears in the context words. Path segments like "users", "home", or "work"
// do not cause a spurious match. Other scopes (`tool:git`, `domain:x`) are compared
// against context words.
func scopeMatchesContext(scope, workspace string, context map[string]bool) bool {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return false
	}
	if strings.HasPrefix(strings.ToLower(scope), "repo:") {
		repoPath := strings.TrimSpace(scope[len("repo:"):])
		repoClean := filepath.Clean(repoPath)
		if workspace != "" {
			wsClean := filepath.Clean(workspace)
			if wsClean == repoClean ||
				strings.HasPrefix(wsClean+string(filepath.Separator), repoClean+string(filepath.Separator)) ||
				strings.HasPrefix(repoClean+string(filepath.Separator), wsClean+string(filepath.Separator)) {
				return true
			}
			repoBase := strings.ToLower(filepath.Base(repoClean))
			if repoBase != "." && repoBase != "/" && repoBase != "\\" {
				if strings.EqualFold(filepath.Base(wsClean), repoBase) {
					return true
				}
			}
		}
		repoBase := strings.ToLower(filepath.Base(repoClean))
		if repoBase != "." && repoBase != "/" && repoBase != "\\" && len(repoBase) >= 3 {
			if context[repoBase] {
				return true
			}
		}
		return false
	}

	for _, part := range strings.FieldsFunc(strings.ToLower(scope), func(r rune) bool {
		return r == ':' || r == '/' || r == '\\' || r == '.' || r == '-' || r == '_' || r == ' '
	}) {
		if part != "" && context[part] {
			return true
		}
	}
	return false
}

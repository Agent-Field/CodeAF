package exec

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

// A subharness is an alternative way to turn the same Task into the same
// Outcome. This file is everything the rest of the system needs to know about
// one before it runs: what it is for, how much it can take, and how long it may
// have. The executor that actually does the work is registered separately, on a
// Registry, because a surface builds its workers with its own clients and
// workspaces while this description is a fact about the process.
//
// The whole file is inert with only linear registered, and that is the law
// rather than a convenience: MenuText returns nothing, the sizing prompt keeps
// its pre-subharness bytes, and every lookup answers linear.

// LinearSubharness is the baseline and the fallback. The empty string means it.
const LinearSubharness = plan.LinearSubharness

// SubharnessInfo is one registration.
//
// Purpose and PriorAnchors are the two halves of teaching a model to choose:
// the purpose says what the work has to be about, the anchors say how much of
// it fits. Both are prompt text, and both are priors — measurement replaces the
// anchors through the profile store, and the measured history is rendered
// beside the purpose through the knowledge hook below.
type SubharnessInfo struct {
	Name    string
	Purpose string
	// PriorAnchors is the initial capacity ruler in the style of plan/size.go's
	// three worked examples: comfortably atomic, borderline, oversized. It is
	// the initial setting of this subharness's hardness and nothing more; the
	// first eight measured leaves start replacing it.
	PriorAnchors string

	// DeadlineFloor and the scaling pair are the budget shape. A leaf's hang
	// backstop is not a policy about patience, it is a claim about how long
	// this kind of work legitimately takes, and the claim differs per worker: a
	// linear leaf's fifteen minutes would kill a coding pipeline in its first
	// merge. Zero values fall back to linear's shape, so a registration that
	// says nothing about time is served rather than refused.
	DeadlineFloor     time.Duration
	DeadlineStep      time.Duration
	DeadlinePerTokens int
}

// linearInfo is the shape the whole system ran on before there was a second
// one: fifteen minutes floor, one minute per fifty thousand tokens above it
// (cmd/aforge leafDeadline, and the headless runner it was copied from). Its
// Purpose and PriorAnchors are deliberately empty — linear is the baseline
// every node is already judged against, not an entry on a menu.
var linearInfo = SubharnessInfo{
	Name:              LinearSubharness,
	DeadlineFloor:     15 * time.Minute,
	DeadlineStep:      time.Minute,
	DeadlinePerTokens: 50_000,
}

// Deadline is the budget shape applied to one leaf's token grant.
func (s SubharnessInfo) Deadline(budgetTokens int) time.Duration {
	floor, step, per := s.DeadlineFloor, s.DeadlineStep, s.DeadlinePerTokens
	if floor <= 0 {
		floor = linearInfo.DeadlineFloor
	}
	if step <= 0 || per <= 0 {
		step, per = linearInfo.DeadlineStep, linearInfo.DeadlinePerTokens
	}
	if scaled := time.Duration(budgetTokens/per) * step; scaled > floor {
		return scaled
	}
	return floor
}

var (
	subharnessMutex sync.RWMutex
	subharnessBy    = map[string]SubharnessInfo{LinearSubharness: linearInfo}
	subharnessOrder []string
	// measured is the hook onto self-knowledge: one line per subharness of what
	// its leaves have actually cost. It lives here as a function rather than as
	// data because the profile store belongs to the surface, and the menu must
	// never be the reason a package imports one.
	measured func(subharness string) string
)

// RegisterSubharness makes one available to the whole process: to the menu the
// compiler chooses from, to the sizing pass's rulers, and to the budget shape a
// leaf is granted. It is process-global for the same reason the anchors are —
// one process is one set of workers — and it is deliberately the only door:
// registering here is what puts a subharness in front of every model that could
// choose it, so a new one is never half-installed.
func RegisterSubharness(info SubharnessInfo) {
	name := strings.TrimSpace(info.Name)
	if name == "" || name == LinearSubharness {
		return
	}
	info.Name = name
	subharnessMutex.Lock()
	if _, known := subharnessBy[name]; !known {
		subharnessOrder = append(subharnessOrder, name)
		sort.Strings(subharnessOrder)
	}
	subharnessBy[name] = info
	subharnessMutex.Unlock()
	// The sizing pass reads its rulers out of plan, which cannot import this
	// package. One registration, both readers.
	plan.UseSubharness(plan.Subharness{Name: name, Purpose: info.Purpose}, info.PriorAnchors)
}

// Subharnesses returns the registered specialists in a stable order. Linear is
// never among them: a menu with one entry is no menu.
func Subharnesses() []SubharnessInfo {
	subharnessMutex.RLock()
	defer subharnessMutex.RUnlock()
	list := make([]SubharnessInfo, 0, len(subharnessOrder))
	for _, name := range subharnessOrder {
		list = append(list, subharnessBy[name])
	}
	return list
}

// SubharnessFor resolves a name to what will actually run it. An unknown or
// empty name is linear rather than an error — the same degradation Registry.For
// promises, said one layer up so a budget shape can be read before dispatch.
func SubharnessFor(name string) SubharnessInfo {
	subharnessMutex.RLock()
	defer subharnessMutex.RUnlock()
	if info, ok := subharnessBy[strings.TrimSpace(name)]; ok {
		return info
	}
	return linearInfo
}

// KnownSubharness reports whether a name reaches a registered specialist. It is
// the validator every surface uses on a name that came from a model or a flag,
// and it answers no for "linear" and for empty: those are the baseline, and
// nothing needs to say so out loud.
func KnownSubharness(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || name == LinearSubharness {
		return false
	}
	subharnessMutex.RLock()
	defer subharnessMutex.RUnlock()
	_, ok := subharnessBy[name]
	return ok
}

// UseSubharnessKnowledge installs the measured-history hook the menu renders
// under each purpose. Nil, and a hook that returns nothing, leave the menu
// exactly as the registrations wrote it — which is what every process has
// before its first specialist leaf has ever run.
func UseSubharnessKnowledge(knowledge func(subharness string) string) {
	subharnessMutex.Lock()
	defer subharnessMutex.Unlock()
	measured = knowledge
}

// MenuText is the choice context, and it is empty until there is a choice.
//
// It is built from the registrations rather than written anywhere, so adding a
// subharness adds it to every prompt that chooses one; and it states the rule
// in the same breath as the options, because a menu without a rule for reading
// it is how a model talks itself into the interesting answer.
func MenuText() string {
	specialists := Subharnesses()
	if len(specialists) == 0 {
		return ""
	}
	subharnessMutex.RLock()
	knowledge := measured
	subharnessMutex.RUnlock()

	var menu strings.Builder
	menu.WriteString("Subharnesses. A job is normally taken by the default worker: one agent, " +
		"alone and in order, with tools. These specialists sit beside it, each a whole " +
		"different way of doing one job:\n")
	for _, info := range specialists {
		fmt.Fprintf(&menu, "\n- %s — %s\n", info.Name, strings.TrimSpace(info.Purpose))
		if knowledge == nil {
			continue
		}
		if line := strings.TrimSpace(knowledge(info.Name)); line != "" {
			fmt.Fprintf(&menu, "  measured here so far: %s\n", line)
		}
	}
	menu.WriteString("\nChoose a specialist subharness only when the job's essence matches its " +
		"purpose. When in doubt, or for mixed or non-matching work, leave it unset " +
		"(the default worker).")
	return menu.String()
}

// ForgetSubharnesses restores the process to its linear-only state. Tests own
// it: registration is global by design, and a test that adds one must be able
// to put the process back for every test that asserts the baseline.
func ForgetSubharnesses() {
	subharnessMutex.Lock()
	subharnessOrder = nil
	subharnessBy = map[string]SubharnessInfo{LinearSubharness: linearInfo}
	measured = nil
	subharnessMutex.Unlock()
	plan.ForgetSubharnesses()
}

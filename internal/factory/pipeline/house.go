package pipeline

import "github.com/Agent-Field/codeaf/internal/factory"

// WithHouseRules adds the factory's own rules ([factory.HouseRules]) to one
// repository's read, the way a personal file tightens it: its rules join the
// repository's, its ask-first lines join `start.ask_first` and its never paths
// join `start.never`. NOTHING THE REPOSITORY SAYS IS DROPPED OR LOOSENED, so a
// house rule can only make a job ask sooner or a step follow one more sentence.
//
// The repository's Text is left as it was read; House records what was added.
// The file is copied first, so a cached read is never changed in place.
func WithHouseRules(f File, house factory.HouseRules) File {
	out := cloneFile(f)
	if house.Empty() {
		return out
	}
	out.Rules = unionFold(out.Rules, house.Rules)
	out.Start.AskFirst = unionFold(out.Start.AskFirst, house.AskFirst)
	out.Start.Never = unionExact(out.Start.Never, house.Never)
	out.House = factory.HouseRules{
		Rules:    append([]string{}, house.Rules...),
		AskFirst: append([]string{}, house.AskFirst...),
		Never:    append([]string{}, house.Never...),
	}
	return out
}

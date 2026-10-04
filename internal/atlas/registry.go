package atlas

// Maps is the ordered registry of every atlas map. Every entry point —
// `codeaf atlas`, the /atlas slash command and their pickers — reads this list
// and nothing else; adding a map here is all it takes to publish one.
var Maps = []*Map{
	Pairing,
}

// ByName answers the map a person named, in the order it was registered.
func ByName(name string) (*Map, bool) {
	for _, mp := range Maps {
		if mp.Name == name {
			return mp, true
		}
	}
	return nil, false
}

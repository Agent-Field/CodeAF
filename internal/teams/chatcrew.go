package teams

// ChatCrew is the one open crew a chat is shown under, and whether the chat is
// that crew's manager. A conversation can hold several memberships (it may
// manage one crew and report into the crew above it), but a chat list shows a
// chat once, so exactly one is chosen, in this order:
//
//  1. the crew it manages, so every crew keeps its manager row (the row a
//     person sends to);
//  2. its home membership, the crew it reports into;
//  3. the first open crew that lists it.
//
// The root `All teams` crew is never chosen: it holds every crew, so naming it
// would put every chat in one group. Closed crews are not looked at, which is
// why the caller passes the open ones.
//
// is says whether a member is the chat. Both the holder (sealing the chat's
// directory record, syncsetup) and the app's chat list (cmd/codeaf) use this
// one rule, so a chat shows under the same crew on every computer.
func ChatCrew(open []Team, is func(Member) bool) (team Team, manager, ok bool) {
	pick := -1
	rank := 4
	for i, t := range open {
		if t.Root || t.Closed() {
			continue
		}
		for _, m := range t.Members {
			if !is(m) {
				continue
			}
			r := 3
			switch {
			case t.Manager != "" && t.Manager == m.Key:
				r = 1
			case m.Home:
				r = 2
			}
			if r < rank {
				pick, rank = i, r
			}
			break
		}
	}
	if pick < 0 {
		return Team{}, false, false
	}
	return open[pick], rank == 1, true
}

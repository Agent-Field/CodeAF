package teams

// startclaim.go is who carries out a manager's start line.
//
// A START IS CARRIED OUT ONCE, by whichever window claims it first. Every
// window that holds a team's manager reads its Traffic and answers a start by
// opening a new conversation, and a terminal and the desktop app can both
// hold one manager (the app joins the terminal's conversation). Without a
// claim each of them started a member for the same line. The claim is made in
// the teams file under its own lock, so it is one answer on one machine, and
// over --host it crosses the wire with every other edit of the file.

// keepStarts is how many claimed start lines a team remembers. A start is
// answered within seconds of being written, so only the newest matter.
const keepStarts = 64

// ClaimStart claims start line entry of team for the caller, and reports
// whether the caller is the one to carry it out: false when another window
// claimed it first, or the team is not in the file.
func (f *File) ClaimStart(team, entry string) bool {
	for i := range f.Teams {
		t := &f.Teams[i]
		if t.ID != team {
			continue
		}
		for _, claimed := range t.Starts {
			if claimed == entry {
				return false
			}
		}
		t.Starts = append(t.Starts, entry)
		if over := len(t.Starts) - keepStarts; over > 0 {
			t.Starts = append([]string(nil), t.Starts[over:]...)
		}
		return true
	}
	return false
}

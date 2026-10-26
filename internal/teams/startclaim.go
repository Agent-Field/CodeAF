package teams

import "errors"

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

// AppendClaimedStart records a manager's start for the caller to carry out.
// Holding the lifecycle lock through the claim keeps other windows from
// carrying out the line before the caller records its member. On error, the
// traffic line may already have been written.
func AppendClaimedStart(profileDir, teamID string, e Entry) (string, error) {
	if e.Kind != KindStart || e.From != FromManager {
		return "", errors.New("teams: a claimed start must be a manager's start")
	}
	if err := ValidHandle(e.To); err != nil {
		return "", err
	}
	var id string
	err := withLock(profileDir, lockWait, func() error {
		var err error
		id, err = appendTrafficIDLocked(profileDir, teamID, e)
		if err != nil {
			return err
		}
		_, err = updateLocked(profileDir, func(f *File) error {
			if !f.ClaimStart(teamID, id) {
				return errors.New("teams: start could not be claimed")
			}
			return nil
		})
		return err
	})
	return id, err
}

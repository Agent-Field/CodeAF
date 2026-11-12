package teams

import (
	"fmt"
	"strconv"
)

// ReadAround is entry id of a team's log with up to around entries on each
// side, oldest first, and false when id is not in the retained log. It seeks
// to just before them and reads forward ([forwardTraffic]); the log is never
// read whole.
func ReadAround(profileDir, teamID, id string, around int) ([]Entry, bool, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 || around < 0 {
		return nil, false, nil
	}
	if err := safeTeamID(teamID); err != nil {
		return nil, false, err
	}
	from := n - 1 - int64(around)
	if from < 0 {
		from = 0
	}
	path := TrafficPath(profileDir, teamID)
	got, err := forwardTraffic([]string{trafficRotated(path), path}, fmt.Sprintf("%0*d", trafficIDWidth, from), 2*around+1)
	if err != nil {
		return nil, false, err
	}
	last := fmt.Sprintf("%0*d", trafficIDWidth, n+int64(around))
	window, found := got[:0], false
	for _, e := range got {
		if e.ID > last {
			break
		}
		found = found || e.ID == id
		window = append(window, e)
	}
	if !found {
		return nil, false, nil
	}
	return window, true, nil
}

// Involves says whether entry e was written by or to the member m: its sender,
// its addressee, the member it is about, or one of the room it went to.
func Involves(e Entry, m Member) bool {
	if m.Key != "" && (e.FromKey == m.Key || e.ToKey == m.Key || e.Member == m.Key) {
		return true
	}
	switch e.To {
	case ToRoom, ToEveryone:
		return true
	case ToSeveral:
		for _, h := range e.Handles {
			if h == m.Handle {
				return true
			}
		}
	}
	return false
}

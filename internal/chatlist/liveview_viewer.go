package chatlist

import "time"

// LiveNotSentOffline explains why a message typed into an offline live view was
// held back in the composer rather than sent.
func LiveNotSentOffline(host string) string {
	return "not sent. " + hostWord(host) + " is offline; your words are still in the box"
}

// LiveSeenAt formats the moment a viewer's local copy of a chat was last
// brought up to date, using clock time on the same local calendar day and
// adding the day and month once the day has turned. A zero timestamp renders as
// nothing under the emptiness law.
func LiveSeenAt(seen, now time.Time) string {
	if seen.IsZero() {
		return ""
	}
	local := seen.Local()
	if !now.IsZero() {
		today := now.Local()
		if local.Year() == today.Year() && local.YearDay() == today.YearDay() {
			return local.Format("15:04")
		}
	}
	return local.Format("2 Jan 15:04")
}

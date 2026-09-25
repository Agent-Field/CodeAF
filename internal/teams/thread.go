package teams

import (
	"slices"
	"sort"
	"strconv"
	"strings"
)

// THREADS: WHAT ANSWERS WHAT.
//
// A manager asks three members one question and the log used to hold twelve
// lines for it: three copies of the question, three wakes, three replies and
// the finishings, newest at the bottom, with nothing saying which reply was to
// which question. So the log is threaded, by one field:
//
//   - A MESSAGE TO SEVERAL MEMBERS IS ONE ENTRY. [ToSeveral] with the handles
//     in [Entry.Handles], or [ToEveryone]; delivery asks [Entry.Addressed],
//     which is true for each of them, so every member is still told once.
//   - AN ANSWER NAMES WHAT IT ANSWERS. [Entry.Answers] is the id of the entry a
//     reply is to. A member's team_post to its manager names the last message
//     the manager sent it, unless it named another, and the events its turn
//     raises (finished, failed, asking, and the wake that started it) name the
//     same one, so a reader can draw the question with its answers under it
//     and fold the events into the answer's line.
//   - AN ENTRY WRITTEN BEFORE THREADS answers nothing, and reads as a thread of
//     its own, which is exactly what it was drawn as before.
//
// The field travels in the entry's JSON, so a log read over --host is threaded
// the same way, and a build that does not know the field keeps reading the log.

// Addressed reports whether e is for the member with handle: to that handle,
// to everyone, or to several of which it is one.
func (e Entry) Addressed(handle string) bool {
	if handle == "" {
		return false
	}
	switch e.To {
	case ToEveryone:
		return true
	case ToSeveral:
		return slices.Contains(e.Handles, handle)
	}
	return e.To == handle
}

// Recipients is the members e is addressed to by handle: its one handle, or
// its several. It is nil for an address that is not a member's (everyone, the
// room, the manager); a reader that wants everyone's names has the team.
func (e Entry) Recipients() []string {
	switch e.To {
	case ToSeveral:
		return e.Handles
	case "", ToEveryone, ToRoom, ToManager:
		return nil
	}
	return []string{e.To}
}

// Wake reports whether e says a turn was started by team traffic: `woke @web`,
// `woke ◆`. It is the cause of a state and never news of its own, so a reader
// draws the state (a member working) rather than the line.
func (e Entry) Wake() bool {
	return e.Kind == KindEvent && e.State == StateRunning && strings.HasPrefix(strings.TrimSpace(e.Text), "woke ")
}

// ThreadNumber is an entry id as a person and a model write it: "#42".
func ThreadNumber(id string) string {
	trimmed := strings.TrimLeft(id, "0")
	if trimmed == "" {
		trimmed = "0"
	}
	return "#" + trimmed
}

// ThreadID reads what a model wrote for a thread ("#42", "42", or a whole id)
// back into an entry id, and false when it is not one.
func ThreadID(s string) (string, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if s == "" || len(s) > trafficIDWidth {
		return "", false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return "", false
	}
	return padID(n), true
}

// padID is a sequence number as an entry id.
func padID(n int64) string {
	id := strconv.FormatInt(n, 10)
	if len(id) < trafficIDWidth {
		id = strings.Repeat("0", trafficIDWidth-len(id)) + id
	}
	return id
}

// Thread is one message and everything that answered it, directly or through
// another answer.
type Thread struct {
	// Root is the message the thread began with.
	Root Entry
	// Replies is everything that answers it, oldest first.
	Replies []Entry
	// Latest is the id of the newest entry in the thread, which is what orders
	// threads by activity.
	Latest string
}

// threadHops is the longest chain of answers followed to a root: a reply to a
// reply to a reply. Past it an entry is a root of its own, which can only
// happen to a log somebody wrote by hand.
const threadHops = 16

// Threads is entries (oldest first, as [ReadTraffic] gives them) grouped into
// threads, the thread with the newest activity FIRST and the entries inside
// each in the order they were written. An entry answering an id that is not
// among entries (older than the window, or never written) begins a thread of
// its own, as does every entry that answers nothing.
func Threads(entries []Entry) []Thread {
	if len(entries) == 0 {
		return nil
	}
	at := make(map[string]int, len(entries))
	for i, e := range entries {
		at[e.ID] = i
	}
	rootOf := func(i int) int {
		for hop := 0; hop < threadHops; hop++ {
			up, ok := at[entries[i].Answers]
			if entries[i].Answers == "" || !ok || up == i || entries[up].ID >= entries[i].ID {
				return i
			}
			i = up
		}
		return i
	}
	index := map[int]int{}
	var out []Thread
	for i, e := range entries {
		root := rootOf(i)
		n, ok := index[root]
		if !ok {
			n = len(out)
			index[root] = n
			out = append(out, Thread{Root: entries[root], Latest: entries[root].ID})
		}
		if root != i {
			out[n].Replies = append(out[n].Replies, e)
		}
		if e.ID > out[n].Latest {
			out[n].Latest = e.ID
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Latest > out[j].Latest })
	return out
}

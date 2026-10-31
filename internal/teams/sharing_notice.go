package teams

import (
	"log"
	"sync"

	"github.com/Agent-Field/codeaf/internal/config"
)

var sharingNotices = struct {
	sync.Mutex
	profiles map[string]*sharingNotice
}{profiles: map[string]*sharingNotice{}}

type sharingNotice struct {
	say     func(string)
	pending []string
	seen    map[string]bool
}

// WatchSharingErrors connects optional sharing failures to the existing sync
// notice desk. Early failures wait for the surface, and retries say each failure
// once per process rather than flooding the person or consuming another error.
func WatchSharingErrors(profile string, say func(string)) func() {
	profile = config.ProfilePath(profile, "")
	sharingNotices.Lock()
	n := sharingNotices.profiles[profile]
	if n == nil {
		n = &sharingNotice{seen: map[string]bool{}}
		sharingNotices.profiles[profile] = n
	}
	n.say = say
	pending := n.pending
	n.pending = nil
	sharingNotices.Unlock()
	for _, line := range pending {
		say(line)
	}
	return func() {
		sharingNotices.Lock()
		n.say = nil
		sharingNotices.Unlock()
	}
}

// SharingError keeps retry reporting on the same notice path for every team
// carrier. A failed backup therefore cannot disappear into the chat log.
func SharingError(profile string, err error) {
	if err == nil {
		return
	}
	profile = config.ProfilePath(profile, "")
	line := "teams: sharing will retry: " + err.Error()
	sharingNotices.Lock()
	n := sharingNotices.profiles[profile]
	if n == nil {
		n = &sharingNotice{seen: map[string]bool{}}
		sharingNotices.profiles[profile] = n
	}
	if n.seen[line] {
		sharingNotices.Unlock()
		return
	}
	n.seen[line] = true
	say := n.say
	if say == nil {
		n.pending = append(n.pending, line)
	}
	sharingNotices.Unlock()
	log.Print(line)
	if say != nil {
		say(line)
	}
}

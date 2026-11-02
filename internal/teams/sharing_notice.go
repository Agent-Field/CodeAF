package teams

import (
	"log"
	"sync"
)

var sharingNotices sync.Map

// SharingError uses the application's existing log destination, once per
// profile and process. Retrying optional sharing must not flood that log.
func SharingError(profile string, err error) {
	if err == nil {
		return
	}
	if _, seen := sharingNotices.LoadOrStore(profile, true); !seen {
		log.Printf("teams: sharing will retry: %v", err)
	}
}

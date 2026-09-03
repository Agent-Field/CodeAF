package session

import (
	"net/http"
	"strings"
)

// answersChatOnly is the guard every recording chat stub in this package runs
// first. A stub here stands in for one route, the chat completions POST, and
// since #373 a session's lane beat asks EVERY base for its endpoints page on
// the first refresh (agent.go's [Agent.startLaneBeat]) — so the stub also
// receives `GET /models/<model>/endpoints`, and a stub that files that GET as
// a chat body records an empty request and hands the real turn the wrong
// script line. It answers as a bare base does, with a plain 404, which the
// sheet reads as "no page here" and leaves alone (lanes.ErrNoSheetHere).
//
// It reports whether the request was the chat POST; the caller returns at once
// when it was not.
func answersChatOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/chat/completions") {
		return true
	}
	http.NotFound(w, r)
	return false
}

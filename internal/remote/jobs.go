package remote

import (
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/session"
)

// JobNotices is the conversation's background jobs over the wire: every live
// job and every settled one the engine still holds, newest first.
//
// IT RETURNS AN ERROR because a surface that attaches late draws its job shelf
// from this one read, and a link that could not answer must not look like a
// conversation with no jobs. An engine older than this door answers "no such
// method"; that arrives here as the error too.
func (a *Agent) JobNotices() ([]session.JobNotice, error) {
	payload, err := a.c.call(nil, MethodJobsList, nil)
	if err != nil {
		return nil, err
	}
	var rows []session.JobNotice
	if err := json.Unmarshal(payload, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

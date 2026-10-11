package factory

import "time"

// Publishing records the outcome of carrying one commit's approval to GitHub.
// The older Item.Publishing flag remains the execution and affinity seam.
type Publishing struct {
	State   string       `json:"state"`
	Code    string       `json:"code,omitempty"`
	Detail  string       `json:"detail,omitempty"`
	PR      *PublishedPR `json:"pr,omitempty"`
	Attempt int          `json:"attempt"`
	At      time.Time    `json:"at"`
}

// PublishedPR is the small receipt clients need while publishing changes state.
type PublishedPR struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

// MarkPublishing replaces the snapshot so worker diffs retain the old value.
// A queued transition starts an attempt; pushing and outcomes keep its number.
func (it *Item) MarkPublishing(state, code, detail string, now time.Time) {
	attempt := 0
	if it.PublishStatus != nil {
		attempt = it.PublishStatus.Attempt
	}
	if state == "queued" || attempt == 0 {
		attempt++
	}
	it.PublishStatus = &Publishing{State: state, Code: code, Detail: detail, Attempt: attempt, At: now.UTC()}
	if pr := it.PullRequest; pr != nil {
		it.PublishStatus.PR = &PublishedPR{Number: pr.Number, URL: pr.URL}
	}
}

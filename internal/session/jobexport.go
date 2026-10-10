package session

import "sort"

// JobNotices is every job this conversation's registry still holds, newest
// first: the ones running now and the ones that have settled but not yet been
// swept by retention.
//
// IT IS THE SNAPSHOT HALF OF THE JOB FEED. [Agent.emitJobUpdate] pushes one
// notice per change; a surface that attaches late, or across a wire, has missed
// those pushes and needs the whole shelf once. Each row is built by [noticeOf],
// the same projection the pushes use, so a row read here and a row pushed later
// cannot disagree about a field. Nothing is added: stopping a job stays
// Agent.Cancel("job:N").
//
// Task-kind workers are left out for the reason [Agent.liveJobNotices] gives —
// they already have a roster row of their own. An agent with no registry answers
// nothing, never an empty row.
func (a *Agent) JobNotices() []JobNotice {
	live := a.liveJobNotices()
	if len(live) == 0 {
		return nil
	}
	out := make([]JobNotice, 0, len(live))
	for _, notice := range live {
		out = append(out, notice)
	}
	// Ids are minted in order, so the larger id is the newer job.
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

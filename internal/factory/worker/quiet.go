package worker

import (
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/store"
	factorythread "github.com/Agent-Field/codeaf/internal/factory/thread"
)

// ── a held job whose mirror has not changed is not read again ─────────────
//
// EVERY HELD JOB RE-READ ITS WHOLE JOB ON EVERY REPORT TICK. The flush runs
// twice a second for each job a runner holds, and each one read the mirror's
// document (a deep copy, twice, through the hydrated read), compared it with
// what was last sent (which decodes every step's recorded output again) and
// looked at the manager's journal, to learn that nothing had moved. With two
// held jobs that was a tenth of the eighth idle measurement's samples on
// macOS after #2517 (PERF.md, "Factory floor reads").
//
// So a flush that found nothing to send remembers what it looked at: the
// mirror document's file ([store.Stamp]) and the manager's journal
// ([factorythread.Version]). The next flush looks at both, two stats, and
// while both are the files it saw it has nothing to send either, because
// everything it compares is what was sent (which only a send changes, and a
// send forgets the quiet) against what those two files hold. A change by
// anything, the runner, another process or a person's editor, is another
// file, and the next flush reads the job in full. Deliveries a person's reply
// is owed are asked afresh on every flush, since they are not in either file.
//
// A QUIET IS TRUSTED FOR quietTrust, NO LONGER. What a read of the job answers
// also depends on the clock (the output retention drops lines as they age),
// so a held job is still read in full once a minute however still it is.

// quietTrust is how long a flush that found nothing is trusted unread.
const quietTrust = time.Minute

// quietFlush is the last flush that found nothing to send. Its zero value is
// no quiet: the next flush reads the job.
type quietFlush struct {
	mirror store.Stamp
	talk   string
	thread factorythread.Version
	at     time.Time
}

// fullFlushes counts the flushes that read the job, so a test can say a held
// job whose mirror is unchanged was not read again.
var fullFlushes atomic.Int64

// holds says the job is still as the quiet flush saw it at now.
func (q quietFlush) holds(mirror store.Stamp, now time.Time) bool {
	if q.at.IsZero() || now.Before(q.at) || now.Sub(q.at) >= quietTrust || !q.mirror.Same(mirror) {
		return false
	}
	return q.thread.Same(factorythread.VersionOf(q.talk))
}

// owesDelivery says a reply the factory accepted is not yet acknowledged.
func (j *job) owesDelivery() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	for id := range j.accepted {
		if !j.acknowledged[id] {
			return true
		}
	}
	return false
}

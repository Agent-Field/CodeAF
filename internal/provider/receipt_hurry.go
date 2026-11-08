package provider

// ── a quitting process asks for its receipts once more, and then stops ──────
//
// A RECEIPT'S SCHEDULE IS GENEROUS BECAUSE IT IS IN THE BACKGROUND, and a quit
// makes it the foreground. [receiptRetrySchedule] waits up to forty seconds
// between asks and [ReceiptWait] is seventy, because a router prices a cut
// generation about twenty seconds after the cut and nobody is waiting for it.
// A process that is leaving is waited for: the ninth live WorkAF round's desk
// quit with chats mid-reply, every one of them cut a stream, and the engine sat
// in [Client.reconcile]'s pauses until the app's thirty-second kill ended it
// (exit 143), with one conversation's close measured at 27.8 seconds.
//
// So a process on its way out HURRIES its receipts ([HurryReceipts]): a pause
// under way ends at once, and every receipt still owed is asked for one last
// time within [receiptHurryAsk] and then answered with what is known. A receipt
// the router has not priced yet is recorded the way an exhausted schedule
// records it, as a call whose price is missing, and never dropped: the row,
// its tokens if the stream said them, and its generation id all reach the
// ledger before the process ends.

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// receiptHurryAsk bounds each request a hurried receipt makes: the last ask a
// pause gave up for, and a request already in flight when the hurry came.
const receiptHurryAsk = 2 * time.Second

// receiptHurry is the process's hurry, cancelled by [HurryReceipts]. It is a
// pointer so that calming it ([HurryReceipts]' answer) installs a fresh one
// for whatever runs next in the same process, as a test binary does.
var receiptHurry atomic.Pointer[hurrySignal]

type hurrySignal struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func init() { calmReceipts() }

func calmReceipts() {
	ctx, cancel := context.WithCancel(context.Background())
	receiptHurry.Store(&hurrySignal{ctx: ctx, cancel: cancel})
}

// HurryReceipts makes every receipt still owed in this process finish within
// about [receiptHurryAsk], and every receipt owed from now on ask once. The
// answer calms the hurry again; a process that is really leaving never needs to
// call it.
func HurryReceipts() (calm func()) {
	signal := receiptHurry.Load()
	signal.cancel()
	var once sync.Once
	return func() {
		once.Do(func() {
			if receiptHurry.Load() == signal {
				calmReceipts()
			}
		})
	}
}

// receiptHurried is the hurry a receipt reads: done once the process hurries.
func receiptHurried() context.Context { return receiptHurry.Load().ctx }

// pauseReceipt is one pause of the schedule, cut short by a hurry. hurried
// says the hurry, and not the pause, ended it.
func (c *Client) pauseReceipt(ctx context.Context, hurry context.Context, delay time.Duration) (hurried bool, err error) {
	if hurry.Err() != nil {
		return true, nil
	}
	pause, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(hurry, cancel)
	defer stop()
	err = c.wait(pause, delay)
	if hurry.Err() != nil && ctx.Err() == nil {
		return true, nil
	}
	return false, err
}

// receiptAskContext is one receipt request's context: the schedule's own, and
// once the process hurries, at most [receiptHurryAsk] more.
func receiptAskContext(ctx context.Context, hurry context.Context) (context.Context, context.CancelFunc) {
	if hurry.Err() != nil {
		return context.WithTimeout(ctx, receiptHurryAsk)
	}
	ask, cancel := context.WithCancel(ctx)
	var mu sync.Mutex
	var timer *time.Timer
	stop := context.AfterFunc(hurry, func() {
		mu.Lock()
		defer mu.Unlock()
		timer = time.AfterFunc(receiptHurryAsk, cancel)
	})
	stopTimer := func() {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
	}
	return ask, func() {
		stop()
		stopTimer()
		cancel()
	}
}

package main

// runWake is `codeaf wake`, and it does nothing.
//
// It used to run one bounded pass of the v1 resident — charters woken and
// judged, practice, tenure — against the resident's store, and an operating
// system timer an older build installed ran it every five minutes. That
// scheduler is gone (docs/design/automations/DESIGN.md): nothing in this
// build schedules anything on the machine, and automations run only while a
// codeaf window is open.
//
// THE VERB STAYS, HIDDEN, BECAUSE THE TIMERS OUTLIVE THE BINARY THAT MADE
// THEM. A launchd or systemd unit already installed keeps running
// `<binary> wake` until this build's first start removes it, and an unknown
// verb exits 1 — which every one of those timers would report as a failure,
// every five minutes, to whoever reads the system log. So the word is still
// dispatched, answers 0, and says nothing; it is off the help page
// (beltdoors_test.go's doorsOffThePage) because nobody should ever type it.
// The arguments are ignored on purpose: the timers passed --db and --timeout,
// and refusing a flag would be the same failure by another route.
func runWake([]string) error {
	return nil
}

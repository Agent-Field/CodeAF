package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/manual"
)

// THE APP'S PAGES REACH THE MANUAL THIS PROCESS'S CONVERSATIONS READ, under
// their prefix, and a set that breaks a bound is bad_params and changes nothing.
func TestAppManualPagesReachTheManual(t *testing.T) {
	t.Cleanup(func() { _, _ = manual.SetAppPages(nil) })
	b := newFakeBackend(newAppRecorder(), nil)
	defer b.Close()
	names, err := b.SetManualPages([]appwire.ManualPage{{Name: "inbox", Markdown: "# Inbox\n\nSwipe to snooze."}})
	if err != nil || len(names) != 1 || names[0] != "workaf/inbox" {
		t.Fatalf("the set answered %v %v", names, err)
	}
	if _, ok := manual.WithApp().Page("workaf/inbox"); !ok {
		t.Fatal("the page did not reach the manual")
	}
	if _, err := b.SetManualPages([]appwire.ManualPage{{Name: "Not A Slug"}}); wireCode(err) != appwire.CodeBadParams {
		t.Fatalf("a bad name answered %v", err)
	}
	if _, ok := manual.WithApp().Page("workaf/inbox"); !ok {
		t.Fatal("a refused set changed the pages")
	}
}

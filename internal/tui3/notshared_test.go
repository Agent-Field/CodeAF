package tui3

import (
	"strings"
	"testing"
	"time"
)

func notSharedOf(n NotShared, known bool) func() (NotShared, bool) {
	return func() (NotShared, bool) { return n, known }
}

// TestTheDevicesRowSaysHowManyChatsAreNotSharedYet: another computer lists only
// the chats this one has uploaded, so the row says how many are still here only.
func TestTheDevicesRowSaysHowManyChatsAreNotSharedYet(t *testing.T) {
	a, _ := devicesRig("dev_tower")
	a.notShared = notSharedOf(NotShared{Waiting: 3, KeptHere: 9}, true)
	want := "12 chats on " + thisComputer() + " aren't shared yet"
	if got := devicesFrame(a, 80); !strings.Contains(got, want) {
		t.Fatalf("row lacks %q:\n%s", want, got)
	}
	a.notShared = notSharedOf(NotShared{Failing: 1}, true)
	if got := devicesFrame(a, 80); !strings.Contains(got, "1 chat on "+thisComputer()+" isn't shared yet") {
		t.Fatalf("one chat not said in the singular:\n%s", got)
	}
	for _, quiet := range []func() (NotShared, bool){nil, notSharedOf(NotShared{}, true), notSharedOf(NotShared{Waiting: 4}, false)} {
		a.notShared = quiet
		if got := devicesFrame(a, 80); strings.Contains(got, "shared yet") {
			t.Fatalf("said something with nothing to say:\n%s", got)
		}
	}
}

// TestTheDevicesCardSaysWhyChatsAreNotSharedYet: /devices says the count and,
// for the chats that will not upload by themselves, why, in plain words.
func TestTheDevicesCardSaysWhyChatsAreNotSharedYet(t *testing.T) {
	a, _ := devicesRig("dev_tower")
	a.notShared = notSharedOf(NotShared{Waiting: 2, Failing: 1, KeptHere: 9}, true)
	c := a.newDeviceCard()
	c.loaded = true
	got := plain(strings.Join(c.rows(200, time.Now(), a.pal), "\n"))
	for _, want := range []string{
		"12 chats on " + thisComputer() + " aren't shared yet.",
		"2 are still uploading.",
		"1 could not be uploaded yet; codeaf keeps trying.",
		"9 older ones are kept on " + thisComputer() + ".",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("card lacks %q:\n%s", want, got)
		}
	}
	for count, want := range map[NotShared]string{
		{Waiting: 1, KeptHere: 2}: "1 is still uploading. 2 older ones are kept",
		{Waiting: 1}:              "1 chat on " + thisComputer() + " isn't shared yet. It is still uploading.",
		{Waiting: 2, KeptHere: 1}: "2 are still uploading. 1 older one is kept",
	} {
		if got := notSharedDetail(notSharedOf(count, true)); !strings.Contains(got, want) {
			t.Fatalf("%+v reads %q, want %q", count, got, want)
		}
	}
	if strings.Contains(notSharedDetail(a.notShared), " · ") || strings.Contains(notSharedDetail(a.notShared), "service") {
		t.Fatal("sentence breaks the wording rules")
	}
}

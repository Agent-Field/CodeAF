package remote

import "testing"

// TestTwoRemoteViewersOneHoldsTheKeyboardAndTheOtherIsToldWho verifies V6 (W2,
// W11): remote viewers arrive passively without displacing the current driver
// or sending moved frames, report the authenticated Remote.Label rather than
// Hello.Surface, refuse typing without the keyboard using the not-driving
// sentence naming the holder's label, move the keyboard only via MethodTake
// when Remote.Drive is true, and never move it for a watch-only remote viewer.
func TestTwoRemoteViewersOneHoldsTheKeyboardAndTheOtherIsToldWho(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	// A local window opens the conversation and holds the keyboard.
	desk := dialSession(t, sess)
	deskWelcome := decode[Welcome](t, desk.hello(Hello{
		Version: Version,
		Surface: "macbook",
	}).Payload)
	if !deskWelcome.Driver.Yours {
		t.Fatalf("local window did not start with the keyboard: %+v", deskWelcome.Driver)
	}

	// First remote-drive viewer ("laptop") arrives; it carries a spoofed Surface
	// and Back=false, yet must not displace the local window or fire moved.
	laptop := dialSession(t, sess)
	laptopWelcome := decode[Welcome](t, laptop.hello(Hello{
		Version: Version,
		Surface: "spoofed-desk",
		Session: sess.engine.SessionFile,
		Join:    true,
		Remote: &RemoteViewer{
			Device: "dev-laptop",
			Label:  "laptop",
			Watch:  true,
			Drive:  true,
			Road:   "relay",
		},
	}).Payload)
	if laptopWelcome.Driver.Yours {
		t.Fatalf("remote viewer took the keyboard from the local window on arrival: %+v", laptopWelcome.Driver)
	}
	if laptopWelcome.Driver.Machine != "macbook" {
		t.Fatalf("remote viewer was told the keyboard is on %q, want macbook", laptopWelcome.Driver.Machine)
	}
	if told := driverOf(desk); !told.Yours {
		t.Fatalf("local window lost the keyboard on remote arrival: %+v", told)
	}
	for _, frame := range drain(desk) {
		if frame.Kind == "moved" {
			t.Fatalf("remote viewer arrival sent a moved frame to the local window: %+v", frame)
		}
	}

	// Laptop takes the keyboard explicitly via MethodTake.
	took := decode[Driver](t, laptop.ok(1, MethodTake, nil).Payload)
	if !took.Yours {
		t.Fatalf("laptop did not receive the keyboard on MethodTake: %+v", took)
	}
	// The local window is told the keyboard moved to "laptop" (Remote.Label),
	// never "spoofed-desk" (Hello.Surface).
	deskTold := driverSaying(desk, "laptop")
	if deskTold.Yours {
		t.Fatalf("local window still believes it holds the keyboard: %+v", deskTold)
	}

	// Second remote-drive viewer ("phone") arrives; it does not move the
	// keyboard from "laptop" nor send a moved frame.
	phone := dialSession(t, sess)
	phoneWelcome := decode[Welcome](t, phone.hello(Hello{
		Version: Version,
		Surface: "spoofed-phone",
		Session: sess.engine.SessionFile,
		Join:    true,
		Remote: &RemoteViewer{
			Device: "dev-phone",
			Label:  "phone",
			Watch:  true,
			Drive:  true,
			Road:   "relay",
		},
	}).Payload)
	if phoneWelcome.Driver.Yours {
		t.Fatalf("second remote viewer took the keyboard on arrival: %+v", phoneWelcome.Driver)
	}
	if phoneWelcome.Driver.Machine != "laptop" {
		t.Fatalf("second remote viewer was told the keyboard is on %q, want laptop", phoneWelcome.Driver.Machine)
	}
	if told := driverOf(laptop); !told.Yours {
		t.Fatalf("first remote viewer lost the keyboard when the second arrived: %+v", told)
	}
	for _, frame := range append(drain(laptop), drain(desk)...) {
		if frame.Kind == "moved" {
			t.Fatalf("second remote viewer arrival sent a moved frame: %+v", frame)
		}
	}

	// Phone does not hold the keyboard, so MethodSubmit is refused with the
	// existing notDrivingWord sentence naming "laptop".
	refused := phone.call(1, MethodSubmit, SubmitArgs{Text: "from phone without keyboard"})
	wantRefusal := "the keyboard is on laptop right now — press enter here to move it here"
	if refused.Error != wantRefusal {
		t.Fatalf("phone Submit without keyboard answered %q, want %q", refused.Error, wantRefusal)
	}

	// Phone takes the keyboard with MethodTake; laptop is told "phone" now drives.
	phoneTook := decode[Driver](t, phone.ok(2, MethodTake, nil).Payload)
	if !phoneTook.Yours {
		t.Fatalf("phone did not receive the keyboard on MethodTake: %+v", phoneTook)
	}
	laptopTold := driverSaying(laptop, "phone")
	if laptopTold.Yours || laptopTold.Machine != "phone" {
		t.Fatalf("laptop was told %+v after phone took the keyboard, want Machine=phone", laptopTold)
	}
	laptopRefused := laptop.call(2, MethodSubmit, SubmitArgs{Text: "from laptop after losing keyboard"})
	wantLaptopRefusal := "the keyboard is on phone right now — press enter here to move it here"
	if laptopRefused.Error != wantLaptopRefusal {
		t.Fatalf("laptop Submit without keyboard answered %q, want %q", laptopRefused.Error, wantLaptopRefusal)
	}
	phone.ok(3, MethodSubmit, SubmitArgs{Text: "from phone with keyboard"})

	// A remote watch-only viewer arrives carrying Watch:false, Back:true,
	// Surface:"desk"; it still arrives watch-only and its MethodTake moves nothing.
	watcher := dialSession(t, sess)
	watcherWelcome := decode[Welcome](t, watcher.hello(Hello{
		Version: Version,
		Surface: "desk",
		Watch:   false,
		Back:    true,
		Session: sess.engine.SessionFile,
		Join:    true,
		Remote: &RemoteViewer{
			Device: "dev-watch",
			Label:  "tablet",
			Watch:  true,
			Drive:  false,
			Road:   "relay",
		},
	}).Payload)
	if watcherWelcome.Driver.Yours {
		t.Fatalf("watch-only remote viewer was given the keyboard on arrival: %+v", watcherWelcome.Driver)
	}
	if watcherWelcome.Driver.Machine != "phone" {
		t.Fatalf("watch-only remote viewer was told the keyboard is on %q, want phone", watcherWelcome.Driver.Machine)
	}
	watchTake := watcher.call(1, MethodTake, nil)
	if watchTake.Error == "" {
		t.Fatalf("watch-only remote viewer MethodTake succeeded over the wire: %+v", watchTake)
	}
	// Phone still holds the keyboard and can submit.
	phone.ok(4, MethodSubmit, SubmitArgs{Text: "still driving on phone"})
}

// TestARemoteViewerCannotSpoofItsGrantOrItsLabel verifies W2 and W11: a remote
// hello carrying Watch:false, Back:true, Surface:"desk" still arrives
// watch-only when the stamp says Drive:false, and a drive viewer's label is
// sanitised from Remote.Label rather than Hello.Surface.
func TestARemoteViewerCannotSpoofItsGrantOrItsLabel(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	// Watch-only viewer arrives into an empty room trying to claim Watch:false.
	watchOnly := dialSession(t, sess)
	watchWelcome := decode[Welcome](t, watchOnly.hello(Hello{
		Version: Version,
		Surface: "desk",
		Watch:   false,
		Back:    true,
		Session: sess.engine.SessionFile,
		Join:    true,
		Remote: &RemoteViewer{
			Device: "dev-1",
			Label:  "watch-pad",
			Watch:  true,
			Drive:  false,
			Road:   "relay",
		},
	}).Payload)
	if watchWelcome.Driver.Yours {
		t.Fatalf("watch-only viewer arrived with Yours=true in an empty room: %+v", watchWelcome.Driver)
	}

	// Drive viewer arrives with an escape sequence in Remote.Label and
	// Surface:"desk"; after it takes the keyboard, others see the sanitised
	// Remote.Label and never "desk".
	driver := dialSession(t, sess)
	driver.hello(Hello{
		Version: Version,
		Surface: "desk",
		Watch:   false,
		Back:    true,
		Session: sess.engine.SessionFile,
		Join:    true,
		Remote: &RemoteViewer{
			Device: "dev-2",
			Label:  "  la\x1b[2Jptop\n ",
			Watch:  true,
			Drive:  true,
			Road:   "relay",
		},
	})
	if arrivalNote := driverOf(watchOnly); arrivalNote.Yours || arrivalNote.Machine != "" {
		t.Fatalf("remote drive arrival took the keyboard before MethodTake: %+v", arrivalNote)
	}
	driver.ok(1, MethodTake, nil)

	told := driverOf(watchOnly)
	if told.Machine != "la[2Jptop" {
		t.Fatalf("watch-only viewer was told the driver is %q, want sanitised Remote.Label %q", told.Machine, "la[2Jptop")
	}
}

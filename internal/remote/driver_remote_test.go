package remote

import (
	"testing"
	"time"
)

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

// ── the keyboard across a road swap (phase 2, D13, W15) ─────────────────────

// remoteArrival is a viewer's hello as the door writes it: Back is ALWAYS true
// on a remote connection, the stamp names the authenticated device, and the
// surface's own client id is the only part the viewer chooses.
func remoteArrival(sess *Session, device, label, clientID string) Hello {
	return Hello{
		Version:  Version,
		Surface:  "ignored",
		Session:  sess.engine.SessionFile,
		Join:     true,
		Back:     true,
		ClientID: clientID,
		Remote:   &RemoteViewer{Device: device, Label: label, Watch: true, Drive: true, Road: "relay"},
	}
}

// TestARoadSwapKeepsTheKeyboardWithTheSameDeviceAndNeverGivesItToAnother pins
// W15 and D13: a remote viewer that holds the keyboard and comes back through a
// different link (a road swap closes the old link first, a silent path leaves it
// attached) still holds it, and that is true only for the SAME authenticated
// device and window. A first arrival still has to ask for the keyboard.
func TestARoadSwapKeepsTheKeyboardWithTheSameDeviceAndNeverGivesItToAnother(t *testing.T) {
	// holder sets up the room: the host's own window, then a remote viewer that
	// has taken the keyboard by asking for it.
	holder := func(t *testing.T) (*Session, *link, *link) {
		t.Helper()
		sess := heldSession(&fakeAgent{})
		desk := dialSession(t, sess)
		desk.hello(Hello{Version: Version, Surface: "macbook"})
		laptop := dialSession(t, sess)
		laptop.hello(remoteArrival(sess, "dev-laptop", "laptop", "window-1"))
		laptop.ok(1, MethodTake, nil)
		driverSaying(desk, "laptop")
		return sess, desk, laptop
	}
	waitDetached := func(t *testing.T, sess *Session, left int) {
		t.Helper()
		waitFor(t, "the host to see the link end", func() bool { return sess.Attached() == left })
	}

	t.Run("the old link ended first", func(t *testing.T) {
		sess, desk, laptop := holder(t)
		_ = laptop.end()
		waitDetached(t, sess, 1)
		// The detach handed the keyboard to the host's own window, as it always did.
		if told := driverOf(desk); !told.Yours {
			t.Fatalf("the host's window was not handed the keyboard when the link ended: %+v", told)
		}
		back := dialSession(t, sess)
		welcome := decode[Welcome](t, back.hello(remoteArrival(sess, "dev-laptop", "laptop", "window-1")).Payload)
		if !welcome.Driver.Yours {
			t.Fatalf("the same device and window came back without the keyboard: %+v", welcome.Driver)
		}
		if told := driverSaying(desk, "laptop"); told.Yours {
			t.Fatalf("the host's window still believes it types: %+v", told)
		}
		back.ok(1, MethodSubmit, SubmitArgs{Text: "after the swap"})
	})

	t.Run("the new link arrives before the old one has detached", func(t *testing.T) {
		sess, desk, laptop := holder(t)
		back := dialSession(t, sess)
		welcome := decode[Welcome](t, back.hello(remoteArrival(sess, "dev-laptop", "laptop", "window-1")).Payload)
		if !welcome.Driver.Yours {
			t.Fatalf("the replacing link did not take the keyboard from its own stale one: %+v", welcome.Driver)
		}
		// The stale link ending later must not hand the keyboard to anyone.
		_ = laptop.end()
		waitDetached(t, sess, 2)
		for _, frame := range drain(desk) {
			if frame.Kind == "driver" && decode[Driver](t, frame.Payload).Machine != "laptop" {
				t.Fatalf("the stale link's late end moved the keyboard: %+v", frame)
			}
		}
		back.ok(1, MethodSubmit, SubmitArgs{Text: "still the viewer's"})
	})

	t.Run("another device never gets it, whatever id it claims", func(t *testing.T) {
		sess, desk, laptop := holder(t)
		_ = laptop.end()
		waitDetached(t, sess, 1)
		_ = driverOf(desk)
		other := dialSession(t, sess)
		welcome := decode[Welcome](t, other.hello(remoteArrival(sess, "dev-phone", "phone", "window-1")).Payload)
		if welcome.Driver.Yours {
			t.Fatalf("another device took the keyboard by repeating a client id: %+v", welcome.Driver)
		}
	})

	t.Run("the same device in another window never gets it", func(t *testing.T) {
		sess, desk, laptop := holder(t)
		_ = laptop.end()
		waitDetached(t, sess, 1)
		_ = driverOf(desk)
		second := dialSession(t, sess)
		welcome := decode[Welcome](t, second.hello(remoteArrival(sess, "dev-laptop", "laptop", "window-2")).Payload)
		if welcome.Driver.Yours {
			t.Fatalf("a second window of the same device took the keyboard: %+v", welcome.Driver)
		}
	})

	t.Run("a viewer that arrives for the first time still has to ask", func(t *testing.T) {
		sess := heldSession(&fakeAgent{})
		desk := dialSession(t, sess)
		desk.hello(Hello{Version: Version, Surface: "macbook"})
		first := dialSession(t, sess)
		welcome := decode[Welcome](t, first.hello(remoteArrival(sess, "dev-laptop", "laptop", "window-1")).Payload)
		if welcome.Driver.Yours {
			t.Fatalf("a first arrival took the keyboard: %+v", welcome.Driver)
		}
	})

	t.Run("a viewer that left on purpose does not get it back", func(t *testing.T) {
		sess, desk, laptop := holder(t)
		laptop.ok(2, MethodDetach, nil)
		_ = laptop.end()
		waitDetached(t, sess, 1)
		_ = driverOf(desk)
		back := dialSession(t, sess)
		welcome := decode[Welcome](t, back.hello(remoteArrival(sess, "dev-laptop", "laptop", "window-1")).Payload)
		if welcome.Driver.Yours {
			t.Fatalf("a viewer that closed the view took the keyboard back: %+v", welcome.Driver)
		}
	})

	t.Run("a keyboard somebody used in the meantime stays with them", func(t *testing.T) {
		sess, desk, laptop := holder(t)
		_ = laptop.end()
		waitDetached(t, sess, 1)
		_ = driverOf(desk)
		desk.ok(1, MethodSubmit, SubmitArgs{Text: "typed on the host while the link was down"})
		back := dialSession(t, sess)
		welcome := decode[Welcome](t, back.hello(remoteArrival(sess, "dev-laptop", "laptop", "window-1")).Payload)
		if welcome.Driver.Yours {
			t.Fatalf("a returning viewer took the keyboard back from a window that had used it: %+v", welcome.Driver)
		}
	})

	t.Run("a keyboard kept too long is let go", func(t *testing.T) {
		sess, desk, laptop := holder(t)
		_ = laptop.end()
		waitDetached(t, sess, 1)
		_ = driverOf(desk)
		sess.mu.Lock()
		sess.kept.at = time.Now().Add(-2 * keyboardKeepFor)
		sess.mu.Unlock()
		back := dialSession(t, sess)
		welcome := decode[Welcome](t, back.hello(remoteArrival(sess, "dev-laptop", "laptop", "window-1")).Payload)
		if welcome.Driver.Yours {
			t.Fatalf("a viewer back after the keeping window took the keyboard: %+v", welcome.Driver)
		}
	})
}

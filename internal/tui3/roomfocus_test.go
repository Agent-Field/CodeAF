package tui3

import (
	"fmt"
	"testing"
)

func TestOpeningTaskFromKeyboardRosterLetsEnterSendItsNote(t *testing.T) {
	for _, width := range []int{80, 140} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a, fake, _ := roomApp(t)
			a.width, a.height = width, 42
			drive(t, a, altT())
			drive(t, a, key("enter"))
			if a.room == nil || a.room.id != 7 {
				t.Fatal("keyboard roster did not open the selected task")
			}
			for _, r := range "Cover invalid units too" {
				drive(t, a, key(string(r)))
			}
			drive(t, a, key("enter"))
			if len(fake.steered) != 1 || fake.steered[0].text != "Cover invalid units too" {
				t.Fatalf("Enter did not send the task note: %+v; draft %q", fake.steered, a.input.String())
			}
			opened := a.room
			a.input.setText("Preserve this next note")
			drive(t, a, altT())
			drive(t, a, key("enter"))
			if a.room != opened {
				t.Fatal("selecting the open task replaced its room")
			}
			drive(t, a, key("enter"))
			if len(fake.steered) != 2 || fake.steered[1].text != "Preserve this next note" {
				t.Fatalf("reselecting the room lost or blocked its existing note: %+v", fake.steered)
			}
		})
	}
}

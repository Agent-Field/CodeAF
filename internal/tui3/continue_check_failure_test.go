package tui3

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// TestAFailedLookAtThisComputersCopyIsNotBlamedOnTheFolder is the card for a
// check that fails before any move starts, which a launch used to show as "the
// chat could not be put in this folder" although nothing had been put anywhere.
// The sentence names what was being done, the cause goes to the engine log, and
// a lease that was taken meanwhile still reads as a lost race.
func TestAFailedLookAtThisComputersCopyIsNotBlamedOnTheFolder(t *testing.T) {
	for _, automatic := range []bool{true, false} {
		a := leftOffApp(&safeContinueTaker{clean: true})
		a.autoContinue = true
		row := leftOffRow("saved-chat", "laptop", chatlist.Idle, time.Minute)
		row.Released = true
		a.machineRead.rows = []chatlist.Row{row}
		cause := errors.New("open /project: No such file or directory (os error 2)")

		var out bytes.Buffer
		previous := log.Writer()
		log.SetOutput(&out)
		a.continuationReady(continuationChecked{row: row, automatic: automatic, generation: a.continueGeneration, err: cause})
		log.SetOutput(previous)

		screen := homeText(a)
		card := strings.Join(strings.Fields(strings.NewReplacer("│", " ").Replace(strings.Join(cardRows(a), " "))), " ")
		if !strings.Contains(card, moveCheckFails) {
			t.Fatalf("automatic=%v: the card does not say the check failed:\n%s", automatic, screen)
		}
		if strings.Contains(screen, "could not be put in this folder") || noted(a, "could not be put in this folder") {
			t.Fatalf("automatic=%v: the check's failure is blamed on the folder:\n%s", automatic, screen)
		}
		if !strings.Contains(out.String(), cause.Error()) || !strings.Contains(out.String(), "saved-chat") {
			t.Fatalf("automatic=%v: the cause is missing from the engine log: %q", automatic, out.String())
		}
	}

	a := leftOffApp(&safeContinueTaker{clean: true})
	row := leftOffRow("saved-chat", "laptop", chatlist.Idle, time.Minute)
	a.autoContinue = true
	a.machineRead.rows = []chatlist.Row{row}
	a.continuationReady(continuationChecked{row: row, automatic: true, generation: a.continueGeneration, err: directory.ErrLeaseHeld})
	if strings.Contains(homeText(a), moveCheckFails) {
		t.Fatal("a lease taken meanwhile was reported as a failed check")
	}
}

package syncsetup

import (
	"os"
	"path/filepath"
	"testing"
)

// C3: reading a disclosure leaves it owed to either surface until acknowledged.
func TestC3FirstRunPeekDoesNotTellAndAcknowledgementDoes(t *testing.T) {
	s := &Sync{Home: t.TempDir(), Hosted: true}
	for range 2 {
		if line, owed := s.PeekFirstRun(); !owed || line != FirstRunLine {
			t.Fatalf("peek = %q, %v", line, owed)
		}
	}
	if _, err := os.Stat(filepath.Join(s.Home, toldFile)); !os.IsNotExist(err) {
		t.Fatalf("peek wrote the marker: %v", err)
	}
	if told, err := s.AcknowledgeFirstRun(); err != nil || !told {
		t.Fatalf("acknowledge = %v, %v", told, err)
	}
	if _, owed := s.PeekFirstRun(); owed {
		t.Fatal("acknowledged disclosure is still owed")
	}
	if told, err := s.AcknowledgeFirstRun(); err != nil || told {
		t.Fatalf("repeated acknowledgement = %v, %v", told, err)
	}
	if line, first := s.FirstRun(); first || line != "" {
		t.Fatalf("terminal repeated an acknowledged disclosure: %q, %v", line, first)
	}
}

func TestC3UnacknowledgedPeekStillReachesTheTerminal(t *testing.T) {
	s := &Sync{Home: t.TempDir(), Hosted: true}
	s.PeekFirstRun()
	if line, first := s.FirstRun(); !first || line != FirstRunLine {
		t.Fatalf("terminal lost the disclosure: %q, %v", line, first)
	}
	if _, owed := s.PeekFirstRun(); owed {
		t.Fatal("terminal did not mark the disclosure told")
	}
}

func TestC3ChosenRelayHasNoDisclosureToAcknowledge(t *testing.T) {
	s := &Sync{Home: t.TempDir()}
	if _, owed := s.PeekFirstRun(); owed {
		t.Fatal("chosen relay owes a hosted disclosure")
	}
	if told, err := s.AcknowledgeFirstRun(); err != nil || told {
		t.Fatalf("chosen relay acknowledgement = %v, %v", told, err)
	}
}

func TestC3FailedAcknowledgementKeepsDisclosureOwed(t *testing.T) {
	s := &Sync{Home: filepath.Join(t.TempDir(), "missing"), Hosted: true}
	if told, err := s.AcknowledgeFirstRun(); told || err == nil {
		t.Fatalf("failed marker write = %v, %v", told, err)
	}
	if line, owed := s.PeekFirstRun(); !owed || line != FirstRunLine {
		t.Fatalf("failed acknowledgement lost the disclosure: %q, %v", line, owed)
	}
}

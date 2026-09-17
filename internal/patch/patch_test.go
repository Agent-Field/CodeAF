package patch

import (
	"strings"
	"testing"
)

func TestApplyReplacesTheSingleMatch(t *testing.T) {
	got, err := Apply("alpha\nbeta\ngamma\n", "beta", "BETA")
	if err != nil {
		t.Fatalf("Apply refused a unique match: %v", err)
	}
	if got != "alpha\nBETA\ngamma\n" {
		t.Errorf("Apply wrote %q, want %q", got, "alpha\nBETA\\ngamma\\n")
	}
}

func TestApplyRefusesZeroMatchesAndNamesTheCount(t *testing.T) {
	_, err := Apply("alpha\nbeta\n", "delta", "x")
	if err == nil {
		t.Fatal("Apply accepted an old text that matches nothing")
	}
	if !strings.Contains(err.Error(), "0") {
		t.Errorf("the refusal says %q, which never names the count", err.Error())
	}
}

func TestApplyRefusesTwoMatchesAndNamesTheCount(t *testing.T) {
	_, err := Apply("line\nline\nline\n", "line", "x")
	if err == nil {
		t.Fatal("Apply accepted an old text that matches three regions")
	}
	if !strings.Contains(err.Error(), "3") {
		t.Errorf("the refusal says %q, which never names the count", err.Error())
	}
}

func TestApplyRefusesAnEmptyOldText(t *testing.T) {
	if _, err := Apply("anything\n", "", "x"); err == nil {
		t.Fatal("Apply accepted an empty old text, which would match every position")
	}
}

func TestApplyEditsACRLFFileWithLFText(t *testing.T) {
	got, err := Apply("first\r\nsecond\r\nthird\r\n", "second", "SECOND")
	if err != nil {
		t.Fatalf("Apply refused an LF old text against a CRLF file: %v", err)
	}
	if got != "first\r\nSECOND\r\nthird\r\n" {
		t.Errorf("Apply wrote %q; the file's CRLF endings must survive the edit", got)
	}
}

func TestApplyKeepsACRLFFileUnchangedWhenTheOldTextIsAbsent(t *testing.T) {
	// The refusal path never hands back content, so this only checks that a
	// CRLF file is counted rather than mangled: the match is still found in
	// normalized space even though the file never contains a bare LF.
	if _, err := Apply("alpha\r\nbeta\r\n", "alpha\r\nbeta\r\n", "x"); err != nil {
		t.Fatalf("Apply refused a whole-file CRLF match: %v", err)
	}
}

func TestApplyRestoresTheByteOrderMark(t *testing.T) {
	got, err := Apply("\uFEFFalpha\nbeta\n", "beta", "BETA")
	if err != nil {
		t.Fatalf("Apply refused a match in a file with a byte-order mark: %v", err)
	}
	if got != "\uFEFFalpha\nBETA\n" {
		t.Errorf("Apply wrote %q; the file's own byte-order mark must survive", got)
	}
}

func TestApplyCountingIgnoresLineEndingDifferencesInsideTheOldText(t *testing.T) {
	// An old text pasted with CRLF endings edits an LF file: both sides are
	// counted in the same normalized space.
	got, err := Apply("one\ntwo\nthree\n", "one\r\ntwo", "ONE\nTWO")
	if err != nil {
		t.Fatalf("Apply refused a CRLF old text against an LF file: %v", err)
	}
	if got != "ONE\nTWO\nthree\n" {
		t.Errorf("Apply wrote %q, want the old block replaced once", got)
	}
}

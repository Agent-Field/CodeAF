//go:build linux

package executor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The contract these tests are written from, in the words of a person using a
// hosted chat and not of the code:
//
//	C4  a fence that is REQUIRED never lets a command run open: where this
//	    computer can apply none of it, the command is refused with one plain
//	    sentence and nothing it would have done has happened.
//	C8  a fence that is not required behaves exactly as it always did: where
//	    nothing can be applied the command still runs, and what is handed to
//	    the helper is unchanged.

// noFenceAvailable pretends this kernel can apply none of the fence.
func noFenceAvailable(t *testing.T) {
	t.Helper()
	fenceModeInTests = func() fenceMode { return fenceNone }
	t.Cleanup(func() { fenceModeInTests = nil })
}

// runMarkerCommand runs, through the fenced seat as a session's calls go, a
// command whose only effect is to create a marker file, and answers the
// result, whether the marker exists afterwards, and the error.
func runMarkerCommand(t *testing.T, w fenceWorld) (res ExecResult, ran bool, err error) {
	t.Helper()
	marker := filepath.Join(w.root, "ran-marker")
	runner := FencedSeat(w.fence).In(w.root)
	res, err = runner.Exec(context.Background(), ExecRequest{
		Argv: []string{"/bin/sh", "-c", `echo ran > "` + marker + `"`},
		Env:  append(os.Environ(), "HOME="+w.home),
	}, nil)
	_, statErr := os.Stat(marker)
	return res, statErr == nil, err
}

func TestC4_RequiredFenceRefusesWhenNothingCanBeApplied(t *testing.T) {
	noFenceAvailable(t)
	w := newFenceWorld(t, FenceAllow{})
	w.fence.Require = true

	_, ran, err := runMarkerCommand(t, w)
	if ran {
		t.Fatal("the command ran although the fence was required and nothing could be applied")
	}
	if !errors.Is(err, ErrFenceRequired) {
		t.Fatalf("the refusal was %v, want ErrFenceRequired", err)
	}
	const sentence = "This computer cannot keep commands inside this chat's folder, so no command was run."
	if err.Error() != sentence {
		t.Fatalf("the refusal reads %q, want %q", err.Error(), sentence)
	}
	for _, word := range []string{"uid", "sandbox", "sessionhost", "fence", "landlock", "daemon"} {
		if strings.Contains(strings.ToLower(err.Error()), word) {
			t.Fatalf("the refusal says %q, which a person reading a chat should never meet: %q", word, err.Error())
		}
	}
}

func TestC4_RequiredFenceStillRunsWhenItCanBeApplied(t *testing.T) {
	if fenceLimit() == FenceDegradedLine {
		t.Skip("this system cannot apply any of the fence, so a required one correctly refuses")
	}
	w := newFenceWorld(t, FenceAllow{})
	w.fence.Require = true

	res, ran, err := runMarkerCommand(t, w)
	if err != nil || res.Exit != 0 || !ran {
		t.Fatalf("a required fence that can be applied must let the job's own work run: err %v exit %d ran %v stderr %q", err, res.Exit, ran, res.Stderr)
	}
}

func TestC8_UnrequiredFenceStillRunsWhereNothingCanBeApplied(t *testing.T) {
	noFenceAvailable(t)
	w := newFenceWorld(t, FenceAllow{})

	res, ran, err := runMarkerCommand(t, w)
	if err != nil || res.Exit != 0 || !ran {
		t.Fatalf("a fence that is not required must run the command as it always did: err %v exit %d ran %v", err, res.Exit, ran)
	}
}

func TestC8_FenceSpecIsUnchangedUnlessRequired(t *testing.T) {
	w := newFenceWorld(t, FenceAllow{})
	plain, err := json.Marshal(fenceSpec{Fence: *w.fence, Mode: fenceLandlock})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "Require") {
		t.Fatalf("a fence that is not required changed what the helper is handed: %s", plain)
	}
	required := *w.fence
	required.Require = true
	body, err := json.Marshal(fenceSpec{Fence: required, Mode: fenceLandlock})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"Require":true`) {
		t.Fatalf("a required fence did not travel to the helper: %s", body)
	}
}

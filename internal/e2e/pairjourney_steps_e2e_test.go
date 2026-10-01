//go:build e2e

package e2e

import (
	"context"
	"os"
	"regexp"
	"strings"
	"time"
)

// journeyRun walks the steps in the order a person meets them.
func journeyRun(j *journey, env []string, homeA, homeB, wsA, wsB string) {
	t := j.t
	var a, b *rig
	var link string

	j.step("A has used sync before (identity exists)", false, func() {
		setup := startCommand(t, env, "a-setup", homeA, wsA, "pair", "--code")
		_, ok := waitPlain(setup, 15*time.Second, "codeaf pair")
		j.check(ok, "pair --code never showed a code")
		j.see(setup)
		setup.kill()
	})

	j.step("1 card: Add another machine on A's home", true, func() {
		a = startWithEnv(t, env, "a", homeA, wsA, 180, 45)
		a.skipSetup(t)
		a.keys("Space")
		a.keys("Space")
		screen, ok := waitPlain(a, 12*time.Second, "Add another machine")
		j.check(ok, "the home screen never offered another machine")
		j.check(strings.Contains(screen, "pick up your work anywhere"), "pitch missing")
		j.see(a)
	})

	j.step("2 B: codeaf pair prints link and waits", true, func() {
		b = startCommand(t, env, "b", homeB, wsB, "pair")
		screen, ok := waitPlain(b, 15*time.Second, "codeaf.link/p/", "Check number")
		j.require(ok, "B never printed a link and a check number")
		link = linkShape.FindString(screen)
		j.check(link != "", "no link on B's screen")
		j.check(strings.Contains(screen, "Waiting for approval"), "B does not say it is waiting")
		j.see(b)
	})

	j.step("3a A: paste the link into the card", true, func() {
		a.keys("M-d")
		_, ok := waitPlain(a, 5*time.Second, "Paste it here")
		j.check(ok, "the card did not open its steps")
		a.paste(link)
		screen, ok := waitPlain(a, 6*time.Second, "wants to join your fleet")
		j.check(ok, "pasting the link into the card showed no approve screen")
		j.see(a)
		if !ok {
			// A person gives up on the card and types the command the card's
			// own link opens; the card's miss is already written down.
			a.keys("Escape")
			a.lit("/pair " + link)
			a.keys("Enter")
		}
		_ = screen
	})

	var check string
	j.step("3b A: approve screen names the device", true, func() {
		screen, ok := waitPlain(a, 12*time.Second, "wants to join your fleet")
		j.require(ok, "no approve screen")
		j.see(a)
		m := regexp.MustCompile(`Check number (\d{4})`).FindStringSubmatch(screen)
		j.check(m != nil, "approve screen has no check number")
		if m != nil {
			check = m[1]
		}
		bScreen := plain(b)
		j.check(check != "" && strings.Contains(bScreen, "Check number: "+check), "the check number on A (%s) is not the one B shows", check)
		j.check(strings.Contains(screen, "Linux"), "no platform on the approve screen")
		j.check(regexp.MustCompile(`asked .*(ago|now)`).MatchString(screen), "no request time on the approve screen")
		j.check(strings.Contains(screen, "a approve"), "no approve key offered")
		j.check(!strings.Contains(screen, "now ago"), "the request time reads %q", "asked now ago")
	})

	j.step("3c A approves; B prints Paired", true, func() {
		a.keys("a")
		screen, ok := waitPlain(b, 20*time.Second, "Paired")
		j.require(ok, "B never printed Paired")
		j.check(regexp.MustCompile(`Paired - \d+ workspaces? available`).MatchString(screen), "B's line is not 'Paired - N workspaces available'")
		j.see(b)
		toast, ok := waitPlain(a, 6*time.Second, "joined your fleet")
		j.check(ok, "A showed no 'joined your fleet' news")
		t.Logf("A after approve:\n%s", toast)
	})

	if os.Getenv("UX_SEGMENT") == "A" {
		j.freeze()
		stepFresh(j, env)
		return
	}

	var b2 *rig
	j.step("4a B: opens a chat and does some work", true, func() {
		b2 = startWithEnv(t, env, "b2", homeB, wsB, 120, 40)
		b2.skipSetup(t)
		b2.lit("Run the shell command make test and tell me what it printed.")
		b2.keys("Enter")
		screen, ok := waitPlain(b2, 90*time.Second, "3 passed")
		j.require(ok, "B's chat never showed the test output")
		time.Sleep(4 * time.Second) // let the chat reach the sync service
		j.see(b2)
		_ = screen
	})

	j.step("4b A: devices row shows both online", true, func() {
		b2.keys("Space")
		b2.keys("Space")
		time.Sleep(3 * time.Second)
		a.keys("Space")
		a.keys("Space")
		a.keys("Up")
		time.Sleep(2 * time.Second)
		home := j.see(a)
		j.check(strings.Contains(home, "This") || strings.Count(home, "●") >= 2, "no devices row on A's home with a chat from the other device under the cursor")
		a.keys("Escape")
		a.lit("/devices")
		a.keys("Enter")
		list, ok := waitPlain(a, 8*time.Second, "esc close")
		j.check(ok, "/devices showed no list")
		j.check(strings.Count(list, "●") >= 2, "the device list does not show both devices online")
		sc := strings.Split(strings.TrimRight(list, "\n "), "\n")
		t.Logf("A /devices:\n%s", strings.Join(sc[len(sc)-6:], "\n"))
		a.keys("Escape")
	})

	j.step("5a stop B; reopen A; continue prompt", true, func() {
		b2.loseLid()
		time.Sleep(3 * time.Second)
		a.lit("/quit")
		a.keys("Enter")
		time.Sleep(3 * time.Second)
		a.kill()
		diag := guardedCommand(t, context.Background(), homeA, append(env, "CODEAF_HOME="+homeA), binary(t), "cell", "list", "--all")
		dout, _ := diag.CombinedOutput()
		t.Logf("diagnostic cell list:\n%s", dout)
		a = startWithEnv(t, env, "a2", homeA, wsA, 180, 45)
		a.skipSetup(t)
		a.keys("Space")
		a.keys("Space")
		screen, ok := waitPlain(a, 15*time.Second, "Continue where you left off on")
		j.require(ok, "A offered no way to continue where B left off")
		j.see(a)
		_ = screen
		t.Logf("A reopened:\n%s", screen)
	})

	moved := false
	j.step("6a A: Continue here on B's chat", false, func() {
		a.keys("Up")
		a.keys("Enter")
		screen, ok := waitPlain(a, 8*time.Second, "Continue this chat here?")
		j.require(ok, "no takeover question for B's chat")
		j.see(a)
		j.check(strings.Contains(screen, "continue here"), "the question does not offer 'continue here'")
		a.keys("1")
		a.keys("Enter")
		began := time.Now()
		after, ok := waitPlain(a, 40*time.Second, "Moved from")
		j.require(ok, "no 'Moved from' banner after continuing")
		j.check(regexp.MustCompile(`Moved from \S+ in [0-9.]+m?s`).MatchString(after), "the banner does not say how long the move took")
		j.see(a)
		t.Logf("banner after %s", time.Since(began))
		time.Sleep(3 * time.Second)
		moved = true
	})

	j.step("6b A: the moved chat opens with the resume card", false, func() {
		j.check(moved, "the move did not complete")
		screen := plain(a)
		j.see(a)
		j.check(strings.Contains(screen, "README.md"), "the resume card does not list the uncommitted file")
		j.check(strings.Contains(screen, "last tests"), "the resume card does not give the last test run")
		j.check(strings.Contains(screen, "3 passed") || strings.Contains(screen, "passed"), "the moved chat does not show the result B saw")
	})

	j.step("7 revoke B from A; B is rejected", false, func() {
		a.keys("Escape")
		a.lit("/devices")
		a.keys("Enter")
		list, ok := waitPlain(a, 8*time.Second, "r revoke")
		j.require(ok, "no device list on A")
		t.Logf("devices before revoke:\n%s", list)
		a.keys("Down")
		a.keys("r")
		time.Sleep(3 * time.Second)
		after := plain(a)
		j.check(strings.Contains(after, "was revoked"), "A did not confirm the revoke")
		j.see(a)
		bAgain := startCommand(t, env, "b3", homeB, wsB)
		time.Sleep(12 * time.Second)
		rejected := plain(bAgain)
		t.Logf("B after revoke:\n%s", rejected)
		j.check(regexp.MustCompile(`(?i)revoked|reach your chats|bring it back|removed|not (allowed|paired)`).MatchString(rejected), "B opened its chat normally after being revoked and said nothing")
		bAgain.kill()
		cmd := startCommand(t, env, "b4", homeB, wsB, "devices")
		time.Sleep(6 * time.Second)
		said := plain(cmd)
		t.Logf("B codeaf devices after revoke:\n%s", said)
		j.check(regexp.MustCompile(`(?i)revoked|reach your chats|bring it back|removed|not (allowed|paired)`).MatchString(said), "B's own device list does not say B was turned away")
		j.see(cmd)
	})

	j.freeze()
	stepFresh(j, env)
}

func stepFresh(j *journey, env []string) {
	t := j.t
	j.step("0 fresh install: the card shows with no setup", false, func() {
		fresh := startWithEnv(t, env, "fresh", newHome(t, nil), newWorkspace(t, "fresh", true), 180, 45)
		fresh.skipSetup(t)
		fresh.keys("Space")
		fresh.keys("Space")
		_, ok := waitPlain(fresh, 10*time.Second, "Add another machine")
		j.check(ok, "a machine that never ran pair --code shows no 'Add another machine' card")
		j.see(fresh)
	})
}

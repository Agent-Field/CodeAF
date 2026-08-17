package approval

import (
	"encoding/json"
	"testing"
)

// The floor, asked in advance. It is what internal/session's gate consults
// before it lets a remembered answer stand in for a person, so it has to say
// yes to exactly the two things a blanket allow cannot switch off and no to
// everything else.

func bashArgs(command string) json.RawMessage {
	raw, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		panic(err)
	}
	return raw
}

func TestAlwaysAsksCoversTheCriticalShapes(t *testing.T) {
	for _, command := range []string{
		"rm -rf /",
		"rm -rf / --no-preserve-root",
		"sudo rm -rf /var",
		"mkfs.ext4 /dev/sda1",
		"dd if=/dev/zero of=/dev/sda",
		"echo hi > /dev/sda",
		"shutdown -h now",
		"reboot",
		":(){ :|:& };:",
		"cd /tmp && rm -rf /",
	} {
		if !AlwaysAsks(ToolBash, bashArgs(command)) {
			t.Fatalf("%q is not on the floor", command)
		}
	}
}

func TestAlwaysAsksLeavesOrdinaryWorkAlone(t *testing.T) {
	for _, command := range []string{
		"git status --short",
		"npm test",
		"rm -rf build",
		"ls -la",
		"make -j8",
	} {
		if AlwaysAsks(ToolBash, bashArgs(command)) {
			t.Fatalf("%q was put on the floor", command)
		}
	}
}

// The other half of the floor: the calls that leave this machine as the person.
func TestAlwaysAsksCoversTheCallsThatActInThePersonsName(t *testing.T) {
	if !AlwaysAsks("gmail_send", nil) {
		t.Fatal("sending mail is not on the floor")
	}
	if !AlwaysAsks("stripe"+ServiceRequestSuffix, json.RawMessage(`{"method":"POST"}`)) {
		t.Fatal("a write against somebody's own account is not on the floor")
	}
	if AlwaysAsks("stripe"+ServiceRequestSuffix, json.RawMessage(`{"method":"GET"}`)) {
		t.Fatal("a read was put on the floor")
	}
	if AlwaysAsks("read", json.RawMessage(`{"path":"x"}`)) {
		t.Fatal("an ordinary tool was put on the floor")
	}
}

// A bash call whose command never arrived has nothing to match against the
// table, and inventing a floor for it would make every malformed call
// unanswerable rather than safe. The policy already turns it into a prompt.
func TestAlwaysAsksSaysNothingAboutAnUnreadableBashCall(t *testing.T) {
	if AlwaysAsks(ToolBash, nil) {
		t.Fatal("a bash call with no arguments was put on the floor")
	}
	if AlwaysAsks(ToolBash, json.RawMessage(`{`)) {
		t.Fatal("a bash call with broken arguments was put on the floor")
	}
}

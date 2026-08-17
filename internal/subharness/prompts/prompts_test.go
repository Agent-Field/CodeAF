package prompts

import (
	"strings"
	"testing"
)

func TestRenderFills(t *testing.T) {
	out, err := Render("a «one» and a «two», and «one» again", map[string]string{"one": "1", "two": "2"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "a 1 and a 2, and 1 again"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

// Drift in either direction is an error, because either direction means the guide
// and the package that fills it have stopped describing the same machine.
func TestRenderRefusesDrift(t *testing.T) {
	if _, err := Render("«one» and «missing»", map[string]string{"one": "1"}); err == nil {
		t.Error("a hole nobody filled rendered anyway")
	} else if !strings.Contains(err.Error(), "«missing»") {
		t.Errorf("the error does not name the hole: %v", err)
	}
	if _, err := Render("«one»", map[string]string{"one": "1", "unread": "2"}); err == nil {
		t.Error("a value nothing reads rendered anyway")
	} else if !strings.Contains(err.Error(), "«unread»") {
		t.Errorf("the error does not name the unread value: %v", err)
	}
}

// The guide has to quote the runtime's own {{input}} verbatim, which is the whole
// reason the delimiters are not braces.
func TestRenderLeavesTheRuntimesOwnPlaceholderAlone(t *testing.T) {
	out, err := Render("args: {{input}} at «max»", map[string]string{"max": "8"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "{{input}}") {
		t.Errorf("got %q", out)
	}
}

func TestTheAssetsArePresent(t *testing.T) {
	if len(Designer) < 4000 {
		t.Errorf("the designer guide is %d bytes, which is not a meta-guide", len(Designer))
	}
	if len(Reviewer) < 1000 {
		t.Errorf("the reviewer addendum is %d bytes", len(Reviewer))
	}
}

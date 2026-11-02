package remote

import (
	"errors"
	"testing"
)

// L4-H10a, L4-H10b: Refused-send words identify the link and use the serving computer's chosen name.
func TestL4B2RefusedSendWords(t *testing.T) {
	c := newClient("localhost", Hello{})
	if c.SendRefusal() != "" {
		t.Fatal("healthy link refused")
	}
	c.reconnecting = true
	if got := c.SendRefusal(); got != "not sent — still reconnecting to localhost; enter sends it once the link is back" {
		t.Fatal(got)
	}
	c.reconnecting = false
	c.host = "seagull-lichen-63"
	c.welcome.Machine = "desk-b"
	c.dead = errors.New("raw pipe error")
	if got := c.SendRefusal(); got != "not sent — the connection to desk-b is gone; your words are still in the box. Run the same command to pick the conversation back up" {
		t.Fatal(got)
	}
}

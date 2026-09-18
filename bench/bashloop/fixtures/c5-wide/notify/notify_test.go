package notify

import "testing"

func TestRecipient(t *testing.T) {
	cases := []struct {
		address, want string
	}{
		{"ops@example.com", "example.com"},
		{"a@b@c.example", "c.example"},
		{"no-at-here", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := Recipient(c.address); got != c.want {
			t.Errorf("Recipient(%q) = %q, want %q", c.address, got, c.want)
		}
	}
}

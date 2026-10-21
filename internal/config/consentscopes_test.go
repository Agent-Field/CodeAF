package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestApprovalScopes(t *testing.T) {
	for _, tc := range []struct {
		tool, command, reason string
		scopes                []string
	}{
		{"bash", "git status --short", "", []string{"git status*", "git *", "git status --short"}},
		{"write", "", "", []string{"write"}},
		{"bash", "git status && git push", "compound", nil},
		{"bash", "git status; git push", "compound", nil},
		{"bash", "git status | cat", "compound", nil},
		{"slack_send", "", "acts_as_you", nil},
		{"gmail_send", "", "acts_as_you", nil},
		{"calendar_create", "", "acts_as_you", nil},
		{"bash", "rm -rf /", "critical", nil},
		{"bash", "mkfs.ext4 /dev/sda", "critical", nil},
		{"bash", "dd if=x of=/dev/sda", "critical", nil},
		{"bash", "shutdown", "critical", nil},
	} {
		t.Run(tc.tool+tc.command, func(t *testing.T) {
			args, _ := json.Marshal(map[string]string{"command": tc.command})
			scopes, reason := ApprovalScopes(tc.tool, args)
			if !reflect.DeepEqual(scopes, tc.scopes) || reason != tc.reason {
				t.Fatalf("got %v, %q; want %v, %q", scopes, reason, tc.scopes, tc.reason)
			}
		})
	}
}

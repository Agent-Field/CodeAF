package approval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// Contract C1 and C2: Both policy entry points refuse protected deletion before
// any approval rule or saved-profile error can change the answer.
func TestProtectedDeletionOverridesEveryBashPolicy(t *testing.T) {
	home := t.TempDir()
	link := filepath.Join(t.TempDir(), "home alias")
	if err := os.Symlink(home, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", link)
	t.Setenv("USERPROFILE", link)

	commands := []string{
		"rm -r /", "rm -R /", "rm --recursive /", "rm --rec /",
		"rm -rf /", "rm -fr /", "rm -Rf /", "rm -fR /", "rm -r -f /",
		"rm /etc -r", "rm / -r -f", "rm -r -- /", "rm build / -r",
		"/bin/rm -rf /", "sudo rm -rf /", "doas rm -rf /", "env VAR=x rm -rf /",
		`rm -rf "/"`, `rm -r '/'`, "cd /tmp && rm -rf /", "true; rm -rf ~", "a || rm -rf /",
		`rm -r '>' /`, "2>log rm -r /", "rm -r &>log /",
		"rm -rf / --no-preserve-root", "rm --no-preserve-root build", "rm build --no-preserve-root",
		"find / -name x -delete", "sudo find ~ -delete", "find -L /etc -type f -delete", "find -- / -delete",
		"find /var/tmp/x /usr -delete",
		"rm -rf //", "rm -rf /./", "rm -rf /etc/", "rm -rf /etc//",
		"rm -rf /usr/../", "rm -rf /tmp/..", "rm -rf /./etc/*",
	}
	targets := []string{
		"~", "$HOME", "${HOME}", "/home", "/Users", "/root", "/etc", "/usr", "/var",
		"/bin", "/sbin", "/lib", "/lib64", "/opt", "/boot", "/System", "/Library",
		"/Applications", filepath.ToSlash(home), filepath.ToSlash(link),
	}
	for _, target := range targets {
		for _, tail := range []string{"", "/", "/*"} {
			commands = append(commands, `rm -rf "`+target+tail+`"`, `find "`+target+tail+`" -delete`)
		}
	}
	commands = append(commands, "rm -rf /*", "rm -rf ${HOME}/*", "find /* -delete")

	policies := map[string]Policy{
		"allow":          {Default: ActionAllow},
		"unset":          {},
		"prompt":         {Default: ActionPrompt},
		"deny":           {Default: ActionDeny},
		"tool allow":     {Tools: map[string]Action{ToolBash: ActionAllow}},
		"rm allow":       {BashPatterns: []Rule{{Match: "rm *", Action: ActionAllow}}},
		"any allow":      {BashPatterns: []Rule{{Match: "*", Action: ActionAllow}}},
		"unread profile": {ProfileReadError: "open profile: permission denied"},
	}
	for name, policy := range policies {
		t.Run(name, func(t *testing.T) {
			for _, command := range commands {
				preview := policy.CheckBash(command)
				call := policy.Check(ToolBash, bashArgs(command))
				if preview.Action != ActionDeny || !preview.Never || preview.Rule == "" ||
					preview.Scope != "" || preview.ProfileReadError != "" || call != preview {
					t.Errorf("%q: CheckBash = %+v, Check = %+v, want the same hard refusal", command, preview, call)
				}
			}
		})
	}
}

// Contract C3 and C6: The protected folder itself is refused, while its
// descendants and other critical shapes retain the existing approval question.
func TestProtectedDeletionKeepsOtherBashDecisions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	allow := Policy{Default: ActionAllow}
	prompt := Policy{Default: ActionPrompt}

	critical := []string{
		"rm -rf /var/tmp/x", "rm -rf /usr/local/foo", "rm -rf /home/abir/old",
		"rm -rf ~/projects/old", "rm -rf $HOME/.cache/x", "rm -rf /opt/foo/bar",
		"rm -rf build >/etc",
		"rm -rf *", "mkfs.ext4 /dev/sdb1", "dd if=/dev/zero of=/dev/sdb",
		"echo hi > /dev/sdb", "shutdown -h now", "reboot", "halt", "poweroff", ":(){ :|:& };:",
	}
	ordinary := []string{
		"rm -rf build", "rm -r ./tmp", "rm -rf node_modules", "rm /tmp/file", "rm -f ~/.bashrc",
		"ls /", "find / -name x", "find . -delete", "find /var/tmp/x -delete",
		"find /var/tmp/x -path /etc -delete", "rm /", "rm -f /etc", "rm -- --no-preserve-root",
	}
	for _, command := range critical {
		args := bashArgs(command)
		decision := allow.CheckBash(command)
		if decision.Action != ActionPrompt || allow.Check(ToolBash, args) != decision ||
			prompt.Check(ToolBash, args).Action != ActionPrompt || prompt.CheckBash(command).Action != ActionPrompt {
			t.Errorf("%q: want a prompt in both modes and entry points, got %+v under allow", command, decision)
		}
		if !AlwaysAsks(ToolBash, args) || !AlwaysAsksRule(decision.Rule) {
			t.Errorf("%q: want an always-ask reason, got %q", command, decision.Rule)
		}
	}
	for _, command := range ordinary {
		if d := allow.CheckBash(command); d.Action != ActionAllow || allow.Check(ToolBash, bashArgs(command)) != d {
			t.Errorf("%q: want allow in both entry points, got %+v in preview", command, d)
		}
	}
	if decision := allow.CheckBash("rm -rf /var"); decision.Action != ActionDeny {
		t.Errorf("the protected parent should be refused outright, got %+v", decision)
	}
}

// Contract C4: The refusal identifies the command, ends attempts to run it,
// and tells the person how to take over. Its command excerpt is bounded.
func TestProtectedDeletionRefusalWording(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	policy := Policy{Default: ActionAllow}
	decision := policy.CheckBash("  sudo rm -rf /  ")
	if decision.Action != ActionDeny {
		t.Fatalf("want deny, got %+v", decision)
	}
	for _, phrase := range []string{
		"sudo rm -rf /", "codeaf never runs", "any approval mode", "no Approve option",
		"Do not try another route", "script", "find -delete", "another spelling", "stop and tell the person", "their own terminal",
	} {
		if !strings.Contains(decision.Rule, phrase) {
			t.Errorf("refusal omits %q: %q", phrase, decision.Rule)
		}
	}
	for _, word := range []string{"auditor", "verdict", "verified", "refuted"} {
		if strings.Contains(strings.ToLower(decision.Rule), word) {
			t.Errorf("refusal contains %q: %q", word, decision.Rule)
		}
	}
	long := policy.CheckBash("rm -rf /; echo " + strings.Repeat("é", 10_000) + "marker at end")
	if long.Action != ActionDeny || strings.Contains(long.Rule, "marker at end") || len(long.Rule) > 800 || !utf8.ValidString(long.Rule) {
		t.Errorf("want a bounded UTF-8 refusal, got %s with %d bytes", long.Action, len(long.Rule))
	}
}

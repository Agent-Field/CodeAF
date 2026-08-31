package redact

import (
	"strings"
	"testing"
)

// THE TWO HALVES OF THIS PACKAGE ARE TESTED AS TWO HALVES, and the second one
// is the one that decides whether it can ship. A redactor that catches every
// secret and also eats a git sha, an inlined picture or a URL is a redactor
// people turn off, and then it catches nothing at all.

// The secrets below are SHAPES, assembled to look exactly like the real thing
// and belonging to nobody. Nothing in this file is a credential.
func TestEverySecretShapeIsTakenOut(t *testing.T) {
	for _, probe := range []struct {
		name string
		text string
		want string
	}{
		{
			name: "a GitHub OAuth token, the one that started this",
			text: "gho_16C7e42F292c6912E7710c838347Ae178B4a",
			want: "[redacted token · gho_…]",
		},
		{
			name: "a GitHub personal token in a line of output",
			text: "token: ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 (expires never)",
			want: "token: [redacted token · ghp_…] (expires never)",
		},
		{
			name: "the three server-side GitHub prefixes",
			text: "ghu_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 ghs_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 ghr_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
			want: "[redacted token · ghu_…] [redacted token · ghs_…] [redacted token · ghr_…]",
		},
		{
			name: "a fine-grained GitHub token",
			text: "github_pat_11ABCDEFG0abcdefghijkl_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abc",
			want: "[redacted token · github_pat_…]",
		},
		{
			name: "an OpenAI-style key",
			text: "OPENAI_API_KEY=sk-abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKL",
			want: "OPENAI_API_KEY=[redacted token · sk-…]",
		},
		{
			name: "the same family with a vendor infix",
			text: "sk-ant-api03-aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789",
			want: "[redacted token · sk-…]",
		},
		{
			name: "an AWS access key id",
			text: "aws_access_key_id = AKIAIOSFODNN7EXAMPLE",
			want: "aws_access_key_id = [redacted token · AKIA…]",
		},
		{
			name: "the temporary one an assumed role hands out",
			text: "ASIAY34FZKBOKMUTVV7A",
			want: "[redacted token · ASIA…]",
		},
		{
			name: "a Slack bot token",
			text: "xoxb-123456789012-1234567890123-AbCdEfGhIjKlMnOpQrStUvWx",
			want: "[redacted token · xoxb-…]",
		},
		{
			name: "a JWT, all three segments",
			text: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk",
			want: "[redacted token · eyJ…]",
		},
		{
			name: "a bearer credential of no recognisable family",
			text: `curl -H "Authorization: Bearer 8f75d6c1ffdeaaf50fedcba9876543210" https://api.example.com`,
			want: `curl -H "Authorization: Bearer [redacted token]" https://api.example.com`,
		},
		{
			name: "a bearer credential that IS a recognisable family keeps its name",
			text: "Authorization: Bearer gho_16C7e42F292c6912E7710c838347Ae178B4a",
			want: "Authorization: Bearer [redacted token · gho_…]",
		},
		{
			name: "a private key block, fences and all",
			text: "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEAx4fV3nQ9MZ0kLQ\nb2FtZW9uZQ==\n-----END RSA PRIVATE KEY-----",
			want: "[redacted private key]",
		},
		{
			name: "a modern unlabelled private key block",
			text: "before\n-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAA==\n-----END PRIVATE KEY-----\nafter",
			want: "before\n[redacted private key]\nafter",
		},
		{
			name: "two secrets in one result, and the text between them survives",
			text: "first ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 then AKIAIOSFODNN7EXAMPLE done",
			want: "first [redacted token · ghp_…] then [redacted token · AKIA…] done",
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			if got := Secrets(probe.text); got != probe.want {
				t.Fatalf("Secrets\n got  %q\n want %q", got, probe.want)
			}
		})
	}
}

// THE MUST-NOT LIST. Every string here is something a tool result carries all
// day long, and every one of them has a leading fragment in common with
// something in the table above. A single false positive here is a person's
// picture, build output or commit history quietly corrupted on the record.
func TestOrdinaryOutputIsLeftExactlyAsItCame(t *testing.T) {
	for _, probe := range []struct {
		name string
		text string
	}{
		{
			name: "a git sha, which is forty characters of nothing but entropy",
			text: "commit 54754565a1b2c3d4e5f60718293a4b5c6d7e8f90",
		},
		{
			name: "a short sha and a branch, the way git log prints them",
			text: "853b8317 Merge pull request #74 from Agent-Field/ux/phase-clock",
		},
		{
			name: "an inlined picture, which is a very long run of base64",
			text: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==",
		},
		{
			name: "base64url, where the prefixes CAN occur mid-run",
			text: "eyJhbGciOiJIUzI1NiJ9AKIAIOSFODNN7EXAMPLEghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789sk-abcdefghijklmnopqrstuvwxyz",
		},
		{
			name: "a URL with a long path",
			text: "https://github.com/Agent-Field/aforge-v2/blob/main/internal/session/loop.go#L1820-L1899",
		},
		{
			name: "a long file path with hyphenated segments",
			text: "/Users/someone/Library/Caches/go-build/a1/a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0-d",
		},
		{
			name: "the word bearer in a sentence",
			text: "the bearer of this note may collect one parcel",
		},
		{
			name: "a page that merely mentions a private key fence",
			text: "look for a line reading -----BEGIN RSA PRIVATE KEY----- at the top of the file",
		},
		{
			name: "a hex digest, which is what most tools print",
			text: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name: "the shell line that reads a token without printing one",
			text: `TOKEN=$(gh auth token) && gh api -H "Authorization: token $TOKEN" /user`,
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			if got := Secrets(probe.text); got != probe.text {
				t.Fatalf("ordinary output was rewritten\n got  %q\n want %q", got, probe.text)
			}
		})
	}
}

// A marker never carries a byte of the random part, which is the one property
// that makes it safe to write a marker into a file at all.
func TestNoMarkerCarriesARandomByte(t *testing.T) {
	const secret = "gho_16C7e42F292c6912E7710c838347Ae178B4a"
	got := Secrets(secret)
	if strings.Contains(got, "16C7") {
		t.Fatalf("the marker kept part of the token: %q", got)
	}
	if !strings.Contains(got, "gho_") {
		t.Fatalf("the marker lost the prefix that says which kind it was: %q", got)
	}
}

// Empty and unremarkable text comes back as the identical string, because this
// runs on every tool result and most of them have nothing in them.
func TestNothingToDoCostsNothing(t *testing.T) {
	for _, text := range []string{"", "ok", "2 files changed, 14 insertions(+), 3 deletions(-)"} {
		if got := Secrets(text); got != text {
			t.Fatalf("Secrets(%q) = %q", text, got)
		}
	}
}

func BenchmarkSecretsOnALargeCleanResult(b *testing.B) {
	text := strings.Repeat("internal/session/loop.go:1820: the chokepoint every execution passes through\n", 2000)
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Secrets(text)
	}
}

package exec

import "testing"

// Contract 4: a CONTRIBUTING requirement cannot be mistaken for a ban.
func TestContributingRefusesTrailersOnlyForAIBans(t *testing.T) {
	for _, row := range []struct {
		text string
		ban  bool
	}{
		{"Do not add AI co-author trailers to commits.", true},
		{"Please don't include \"Generated with Claude Code\" or Co-authored-by lines from AI tools.", true},
		{"No AI trailers.", true},
		{"No A.I. trailers.", true},
		{"Commits with AI attribution trailers will be rejected.", true},
		{"Never add Assisted-by lines.", true},
		{"Avoid **AI attribution** footers.", true},
		{"Commits that do not include a Signed-off-by trailer will not be merged.", false},
		{"AI-assisted contributions must include an Assisted-by: trailer.", false},
		{"Commits missing an Assisted-by trailer will be rejected.", false},
		{"Do not submit AI-generated code without an Assisted-by trailer.", false},
		{"Do not remove the Signed-off-by trailer added by the tooling.", false},
		{"Please run the tests before opening a pull request.", false},
		{"Avoid large files. AI attribution trailers are required.", false},
		{"Do not remove the AI co-author trailer.", false},
		{"Never strip Assisted-by lines from commits.", false},
		{"Please keep the Co-authored-by line your AI tool adds.", false},
		{"Preserve AI attribution trailers when squashing.", false},
		{"Remove any AI co-author trailers before submitting.", true},
		{"Strip \"Generated with Claude Code\" footers from pull request descriptions.", true},
		{"Avoid large commits. Use Co-authored-by for pair programming.", false},
		{"Our bot adds a co-author line; do not edit it.", false},
		{"Do not keep AI trailers in commits.", true},
		{"Never preserve Assisted-by lines.", true},
	} {
		if got := ContributingRefusesTrailers(row.text); got != row.ban {
			t.Errorf("ContributingRefusesTrailers(%q) = %v, want %v", row.text, got, row.ban)
		}
	}
}

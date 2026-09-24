package delegate

import "testing"

// The brief a program reads names the copy it works in wherever it named the
// folder the task was proposed on, in every spelling, and leaves every other
// path alone.
func TestRehomeBriefNamesTheCopyWhereverTheFolderWasNamed(t *testing.T) {
	from := []string{"~/Code/app", "/Users/p/Code/app", "/private/Users/p/Code/app/"}
	const to = "/Users/p/.codeaf/trees/3"
	for _, c := range []struct{ in, want string }{
		{"work in the checkout at /Users/p/Code/app (a git repo).", "work in the checkout at /Users/p/.codeaf/trees/3 (a git repo)."},
		{"cd /Users/p/Code/app/packages/x && npm test", "cd /Users/p/.codeaf/trees/3/packages/x && npm test"},
		{"the repo is ~/Code/app.", "the repo is /Users/p/.codeaf/trees/3."},
		{"resolved: /private/Users/p/Code/app", "resolved: /Users/p/.codeaf/trees/3"},
		{"`/Users/p/Code/app`", "`/Users/p/.codeaf/trees/3`"},
		// Not the folder: a sibling, a longer name, an extension, a deeper root.
		{"/Users/p/Code/app-two and /Users/p/Code/apps", "/Users/p/Code/app-two and /Users/p/Code/apps"},
		{"/Users/p/Code/app.tar", "/Users/p/Code/app.tar"},
		{"/mnt/Users/p/Code/app", "/mnt/Users/p/Code/app"},
		{"nothing to rewrite", "nothing to rewrite"},
	} {
		if got := RehomeBrief(c.in, from, to); got != c.want {
			t.Errorf("RehomeBrief(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := RehomeBrief("at /a/b", []string{"/a/b"}, "/a/b"); got != "at /a/b" {
		t.Errorf("a copy that is the folder itself rewrote the brief: %q", got)
	}
}

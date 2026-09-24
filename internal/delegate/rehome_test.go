package delegate

import "testing"

// moveAll maps every spelling to one folder, the shape a task proposed on a
// repository's own root takes.
func moveAll(to string, from ...string) []Rehome {
	moves := make([]Rehome, 0, len(from))
	for _, spelling := range from {
		moves = append(moves, Rehome{From: spelling, To: to})
	}
	return moves
}

// The brief a program reads names the copy it works in wherever it named the
// folder the task was proposed on, in every spelling, and leaves every other
// path alone.
func TestRehomeBriefNamesTheCopyWhereverTheFolderWasNamed(t *testing.T) {
	const to = "/Users/p/.codeaf/trees/3"
	moves := moveAll(to, "~/Code/app", "/Users/p/Code/app", "/private/Users/p/Code/app/")
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
		if got := RehomeBrief(c.in, moves); got != c.want {
			t.Errorf("RehomeBrief(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := RehomeBrief("at /a/b", moveAll("/a/b", "/a/b")); got != "at /a/b" {
		t.Errorf("a copy that is the folder itself rewrote the brief: %q", got)
	}
}

// A TASK PROPOSED ON A SUBFOLDER names that subfolder inside the copy, and the
// repository around it names the copy's root. The copy is cut at the
// repository's root, so a subfolder mapped to the copy's root sent every path
// in the brief to a file that does not exist, and the repository's own
// spelling was left pointing at the person's checkout.
func TestRehomeBriefMapsASubfolderIntoTheCopyAndTheRepositoryToItsRoot(t *testing.T) {
	moves := []Rehome{
		{From: "/Users/p/Code/app/packages/foo", To: "/c/trees/3/packages/foo"},
		{From: "~/Code/app/packages/foo", To: "/c/trees/3/packages/foo"},
		{From: "/Users/p/Code/app", To: "/c/trees/3"},
		{From: "~/Code/app", To: "/c/trees/3"},
	}
	for _, c := range []struct{ in, want string }{
		{"fix /Users/p/Code/app/packages/foo/src/a.ts, then run git -C /Users/p/Code/app status",
			"fix /c/trees/3/packages/foo/src/a.ts, then run git -C /c/trees/3 status"},
		{"cd ~/Code/app/packages/foo && make", "cd /c/trees/3/packages/foo && make"},
		{"see ~/Code/app/packages/bar/x.go", "see /c/trees/3/packages/bar/x.go"},
	} {
		if got := RehomeBrief(c.in, moves); got != c.want {
			t.Errorf("RehomeBrief(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A COPY INSIDE THE FOLDER IS NAMED ONCE. A repository at the home folder keeps
// its copies under ~/.codeaf, so the copy's path starts with the folder's own
// spelling; rewriting spelling by spelling found its own output again on every
// pass. The spellings are deduplicated, the text is read once, and a path the
// brief already spells inside the copy is left as it is.
func TestRehomeBriefNamesACopyInsideTheFolderOnce(t *testing.T) {
	const to = "/Users/p/.codeaf/v3/projects/x/s/trees/3"
	moves := moveAll(to, "/Users/p", "/Users/p", "/Users/p/")
	for _, c := range []struct{ in, want string }{
		{"work in /Users/p on the dotfiles", "work in " + to + " on the dotfiles"},
		{"read " + to + "/notes and /Users/p/.zshrc", "read " + to + "/notes and " + to + "/.zshrc"},
	} {
		if got := RehomeBrief(c.in, moves); got != c.want {
			t.Errorf("RehomeBrief(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

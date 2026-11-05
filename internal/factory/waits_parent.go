package factory

import "strings"

// WaitsParent keeps another repository's identity out of the child's text.
// Readers of the child may not be able to read the parent's repository.
func WaitsParent(child, parent Item) Item {
	if !waitsSameRepo(child, parent) {
		parent.Repo, parent.Product, parent.Num = "", "", 0
	}
	return parent
}

func waitsParentWords(child, parent Item, why string) (Item, string) {
	if !waitsSameRepo(child, parent) {
		if strings.HasPrefix(why, "failed") {
			why = "failed"
		} else {
			why = "closed without merging"
		}
	}
	return WaitsParent(child, parent), why
}

func waitsSameRepo(child, parent Item) bool {
	a, b := RepoOf(child), RepoOf(parent)
	if a == "" {
		a = strings.TrimSpace(child.Repo)
	}
	if b == "" {
		b = strings.TrimSpace(parent.Repo)
	}
	return strings.EqualFold(a, b)
}

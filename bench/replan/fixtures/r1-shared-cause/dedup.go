package contacts

// Dedup returns the names with duplicates removed, keeping the first
// spelling it saw of each.
func Dedup(names []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, name := range names {
		key := canonical(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out
}

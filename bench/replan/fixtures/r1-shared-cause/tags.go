package contacts

// CountTags counts how many times each tag was used.
func CountTags(tags []string) map[string]int {
	counts := map[string]int{}
	for _, tag := range tags {
		counts[canonical(tag)]++
	}
	return counts
}

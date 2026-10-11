package nest

import "sort"

// majorityParent maps each fine community to the coarse community that
// contains the most of its chats. A negative label on either side is not a
// vote. Ties go to the smaller coarse id: the labels are sorted before the
// count is read, and the first maximum wins, which is what the reference
// implementation does with a sorted unique and argmax. That tie break is
// the difference between two runs agreeing and a parent flipping.
func majorityParent(fine, coarse []int) map[int]int {
	counts := map[int]map[int]int{}
	for i, child := range fine {
		parent := coarse[i]
		if child < 0 || parent < 0 {
			continue
		}
		votes := counts[child]
		if votes == nil {
			votes = map[int]int{}
			counts[child] = votes
		}
		votes[parent]++
	}
	out := make(map[int]int, len(counts))
	for child, votes := range counts {
		out[child] = winner(votes)
	}
	return out
}

// winner returns the id with the highest count. On a tie it returns the
// smaller id, because the ids are visited in ascending order and a later
// id wins only by strictly exceeding the count.
func winner(votes map[int]int) int {
	ids := make([]int, 0, len(votes))
	for id := range votes {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	best, bestN := ids[0], -1
	for _, id := range ids {
		if votes[id] > bestN {
			best = id
			bestN = votes[id]
		}
	}
	return best
}

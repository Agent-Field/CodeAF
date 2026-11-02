package factory

import "slices"

// ── WHO FOLLOWS A JOB ───────────────────────────────────────────────────────
//
// A person follows a job to be told when it waits on someone and about its
// pull request. Following is each person's own switch, kept on the item by
// normalised GitHub handle ([NormalizeHandle]) and answered only to that
// person as `following`: a reader never learns who else follows a job.
//
// It is not the factory's own PR maintenance step ([FollowPRStep], the job's
// `follow_pr`), which keeps a published pull request healthy whoever watches.

// SetFollowing switches person's following of the job on or off. It answers
// whether the item changed; a blank person changes nothing.
func SetFollowing(it *Item, person string, on bool) bool {
	person = NormalizeHandle(person)
	if person == "" || IsFollowing(*it, person) == on {
		return false
	}
	if on {
		it.Followers = append(it.Followers, person)
		return true
	}
	it.Followers = slices.DeleteFunc(slices.Clone(it.Followers), func(h string) bool { return h == person })
	if len(it.Followers) == 0 {
		it.Followers = nil
	}
	return true
}

// IsFollowing is whether person follows the job.
func IsFollowing(it Item, person string) bool {
	person = NormalizeHandle(person)
	return person != "" && slices.Contains(it.Followers, person)
}

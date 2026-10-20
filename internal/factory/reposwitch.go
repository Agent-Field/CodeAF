package factory

import "strings"

// RepoOffRefusal ends the sentence every door that would work on a job of a
// repository that is off is refused with, after the repository's name
// ([RepoOffWords]). It is spelled once, here, so the start, the run, a step's
// rerun, the publish and the merge all say the same words.
const RepoOffRefusal = "is off · turn it on in Repos to run its jobs."

// RepoOffWords is the refusal for a job of repo, which is off.
func RepoOffWords(repo string) string { return repo + " " + RepoOffRefusal }

// RepoSwitch is the factory's power switch per repository, read off the
// watched list its store keeps: a repository on the list is ON, and one that is
// not is OFF. TURNING A REPOSITORY OFF STOPS THE FACTORY WORKING ON IT
// ENTIRELY (the owner's decision): its jobs leave the queue, intake and triage
// pass them by, its pull requests are not followed, and every door that would
// start, run, publish or merge one is refused in [RepoOffWords]. Nothing of the
// jobs is deleted, so turning it back on brings each one back as it was.
//
// A FLOOR THAT KEEPS NO LIST HAS NO SWITCH, so nothing on it is off: a floor
// nobody connected to GitHub runs the jobs typed into it on its own checkouts,
// as it always did. Once a list is kept (the first repositories picked), an
// empty one turns every repository off. The zero RepoSwitch is a floor with no
// list.
type RepoSwitch struct {
	kept bool
	on   map[string]bool
}

// NewRepoSwitch is the switch for a watched list; kept says the floor keeps
// one at all, even an empty one.
func NewRepoSwitch(watched []string, kept bool) RepoSwitch {
	s := RepoSwitch{kept: kept, on: make(map[string]bool, len(watched))}
	for _, name := range watched {
		s.on[repoKey(name)] = true
	}
	return s
}

// Watched says repo is on the watched list. A floor that keeps no list
// watches nothing.
func (s RepoSwitch) Watched(repo string) bool { return s.on[repoKey(repo)] }

// Off says repo is switched off. A name that is not `owner/name` is never off:
// it cannot be on the list, so the switch says nothing about it.
func (s RepoSwitch) Off(repo string) bool {
	key := repoKey(repo)
	return s.kept && strings.Count(key, "/") == 1 && !s.on[key]
}

// OffJob says the job's repository ([RepoOf]) is switched off.
func (s RepoSwitch) OffJob(it Item) bool { return s.Off(RepoOf(it)) }

// repoKey compares repositories without regard to case, GitHub's own rule.
func repoKey(repo string) string { return strings.ToLower(strings.TrimSpace(repo)) }

// RepoOf is the job's repository as `owner/name`: its repo as written when it
// says both, else its product (the owner) before a bare name. A job that names
// no owner answers "", which no list holds.
func RepoOf(it Item) string {
	repo := strings.TrimSpace(it.Repo)
	if repo == "" || strings.Count(repo, "/") == 1 {
		return repo
	}
	if strings.Contains(repo, "/") {
		return ""
	}
	product := strings.TrimSpace(it.Product)
	if product == "" || strings.Contains(product, "/") {
		return ""
	}
	return product + "/" + repo
}

// UnderWay says the job has work a person's Stop ends: it is running, queued
// or waiting on a person, in a run that has not already ended ([RunEnded]),
// or its pull request is open and waiting to merge. It is the job's Stop
// button (api.CanFor) and the work that keeps a repository from being turned
// off unasked, so it answers exactly the jobs a stop accepts
// (internal/factory/run's StopItem). A job whose pull request waits on a
// person (changes requested, a conflict, a merge that failed) is not under
// way: nothing of the factory's is running for it, and a stop would refuse it.
func UnderWay(it Item) bool {
	state := JobState(it)
	if state != "running" && state != "queued" && state != "needs_you" {
		return false
	}
	switch it.State {
	case StateQueued, StateRunning, StateNeedsYou, StateLanded:
		return true
	case StateShipped:
		return state == "running"
	}
	return false
}

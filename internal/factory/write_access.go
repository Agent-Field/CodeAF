package factory

// RefusesWrite reports whether the job lacks recorded permission for work that
// can write. A blank tier on a person's own job keeps its existing permission.
func (it Item) RefusesWrite() bool {
	return it.Tier == TierStranger || it.Tier == "" && (it.Origin == OriginForge || it.Num > 0 || it.URL != "")
}

// WriteAccessRefusalWords is the reason shared by job controls and workers.
const WriteAccessRefusalWords = "This job came from someone without write access to this repository, so it can't run a step that can change files or update the repository. No further steps were run. To do this work, open it yourself as a new job, or give the author write access."

// AuthorAccessUnknownPrefix names a transient permission lookup failure.
const AuthorAccessUnknownPrefix = "Author access unknown: "

package api

// Verb is one name the API answers and the [factory.Seam] doors it calls, by
// the Seam's field names. A verb with no door is the server's own (the lease
// desk's take).
type Verb struct {
	Name string
	Seam []string
}

// Verbs is ARCHITECTURE §4 as a table: the reads, then the verbs on one job
// (`POST /v1/jobs/{id}/<verb>`), then the floor's own. The client seam wires
// exactly these doors; the server answers exactly these names.
var Verbs = []Verb{
	{"queue", []string{"Load"}},
	{"job", []string{"Load"}},
	{"repos", []string{"Repos"}},
	{"github", []string{"GitHub"}},

	{"run", []string{"Launch"}},
	{"stop", []string{"Stop"}},
	{"pause", []string{"Hold"}},
	{"resume", []string{"Hold"}},
	{"drop", []string{"Dismiss"}},
	{"answer", []string{"Answer"}},
	{"steer", []string{"Steer"}},
	{"approve", []string{"SignOff", "Answer"}},
	{"retry_publish", nil},
	{"request_changes", []string{"SendBack"}},
	{"recheck", []string{"Reverify"}},
	{"shape", []string{"Shape"}},
	{"take", nil},
	// Release is un-take: the person who took the job lets it go. Like take,
	// it is the lease desk's door and has no Seam field.
	{"release", nil},
	{"prioritize", []string{"Mark"}},
	{"reply", []string{"Say"}},

	{"new_job", []string{"New"}},
	{"set_repos", []string{"SetRepos"}},
	{"connect_github", []string{"ConnectGitHub"}},
}

// TUIOnly are the Seam doors the wire does not carry in this wave, each with
// why. A client seam leaves them nil, so the desk draws no key for them when
// it is attached to a factory rather than serving one. EVERY SEAM FIELD IS IN
// [Verbs] OR HERE; the structural test fails on one that is in neither.
var TUIOnly = map[string]string{
	"ForJob": "selects the local doors for a job held by a worker on this machine",
	// Doors that answer a chat session file: a session lives on the machine
	// that made it, so its id means nothing to another machine. The thread
	// read (`/v1/jobs/{id}/thread`) reads replicated messages instead.
	"Talk":    "answers a local chat session file; the wire reads the thread instead",
	"Foreman": "answers a local chat session file for the floor's own conversation",

	// Doors on this machine's disk.
	"Clone":        "checks a repository out on this machine",
	"RecipeAt":     "reads the repository rules from the forge with an offline checkout fallback",
	"SaveRecipe":   "writes the recipe file in this machine's checkout",
	"RecipeOffer":  "asks whether this machine's checkout has a recipe file",
	"WriteRecipe":  "writes the recipe file into this machine's checkout",
	"RecipeNotNow": "puts away an offer about this machine's checkout",
	"Bank":         "writes a habit into this machine's recipe file",
	"BankNote":     "Bank answering what happened to this machine's recipe file",
	"BankStages":   "writes an item's stages into this machine's recipe file",
	"GHLogin":      "asks this machine's gh for its login",
	"Open":         "the URL is already on the job; a browser opens it where the person is",

	// Doors §4 names no verb for in this wave: the person's own edits to one
	// job's run, and the floor's knobs and reads from source.
	"Pause":      "the API's pause and resume say Hold with its intent; Pause is the desk's fallback",
	"Sync":       "opens a terminal-made item on the forge; no API verb in this wave",
	"AskAuthor":  "posts triage questions to the author; no API verb in this wave",
	"SetStage":   "switches one stage of one job; no API verb in this wave",
	"AddStage":   "adds a stage to one job from words; no API verb in this wave",
	"SetCap":     "sets one job's cap; no API verb in this wave",
	"SetEffort":  "sets one stage's effort; no API verb in this wave",
	"Edit":       "the manager's edit to a job's stages; no API verb in this wave",
	"SetRail":    "sets the day's rail; no API verb in this wave",
	"Refresh":    "re-reads one item from its source; the server polls on its own",
	"RefreshAll": "re-reads the whole floor from its sources; the server polls on its own",
}

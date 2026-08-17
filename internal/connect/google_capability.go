package connect

// What a person's Google account can be used for, in four sentences.
//
// It is declared HERE rather than beside the plug in google.go for one reason
// and it is not a good one: this file was added while that one was being
// rewritten elsewhere. The declaration belongs with the plug, and a later wave
// should fold this file into google.go — nothing outside it names anything
// declared here, so that fold is a move and not a change.
//
// ── WHY FOUR AND NOT TWO, AND NOT FIVE ──
//
// The line that matters is drawn in the same place internal/approval draws it,
// because it is the same line: two of these reach outside this machine in the
// person's name and three of the tools do not. So the four rows are the two
// halves of each account, split by that line — reading mail is not sending it,
// and reading a calendar is not putting a meeting on somebody else's.
//
// Not two, because "Gmail" and "Calendar" would put a search and a send behind
// one control, and the person who wants aforge to read their mail without ever
// writing from their address would have nothing to click. Not five, because
// gmail_search and gmail_read are one sentence — nobody wants a program that may
// list their mail but not open it, and a row nobody would ever set differently
// from its neighbour is a row that only costs attention.
//
// ── THE PHRASES ──
//
// They are what the person reads, so they are written the way the tools'
// descriptions are: second person, plain verbs, no machinery. "send mail as you"
// says the thing worth knowing about gmail_send in four words — that what
// arrives at the other end is FROM THEM — which is exactly the fact a control
// beside it is asking them to decide about.

func init() {
	RegisterCapabilities("google", googleCapabilities, googleTools)
}

var googleCapabilities = []Capability{
	{ID: "mail-read", Phrase: "read your mail", Acts: false},
	{ID: "mail-send", Phrase: "send mail as you", Acts: true},
	{ID: "calendar-read", Phrase: "read your calendar", Acts: false},
	{ID: "calendar-write", Phrase: "put things on your calendar and invite people", Acts: true},
}

// googleTools is every tool this build arms for the Google account, and EVERY
// ONE OF THEM IS HERE. A tool left out would answer "" from
// [Manager.ToolCapability] and go on being judged as it is today, which is a
// tool no control on the panel can reach — the exact failure this map exists to
// prevent, and the reason a capability_test.go case walks the armed family
// against it.
var googleTools = map[string]string{
	"gmail_search":    "mail-read",
	"gmail_read":      "mail-read",
	"gmail_send":      "mail-send",
	"calendar_list":   "calendar-read",
	"calendar_create": "calendar-write",
}

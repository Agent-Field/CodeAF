package connect

// What a person's Slack connection can be used for, in two sentences.
//
// ── WHY TWO AND NOT ONE ──
//
// The line is the same line internal/approval draws: reading Slack is not
// posting in somebody's name. A person who wants aforge to find and read a
// thread without ever speaking as them needs a control that can say exactly
// that. Search, thread reading and channel listing stay together because a
// program allowed to find a message but not open its thread is a distinction
// nobody would choose and a row nobody would set differently from its neighbour.
//
// The phrases are what the person reads, so they use second-person plain verbs.
// "send Slack messages as you" names the fact worth deciding about: what arrives
// at the other end is from them.

func init() { RegisterCapabilities("slack", slackCapabilities, slackTools) }

var slackCapabilities = []Capability{
	{ID: "messages-read", Phrase: "read your Slack", Acts: false},
	{ID: "messages-send", Phrase: "send Slack messages as you", Acts: true},
}

// slackTools is every tool this build arms for Slack. A test walks this map
// against the family, so a tool can never land outside the controls a person
// reads.
var slackTools = map[string]string{
	"slack_search":        "messages-read",
	"slack_read_thread":   "messages-read",
	"slack_list_channels": "messages-read",
	"slack_send":          "messages-send",
}

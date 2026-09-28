// Package modelapi is the model API codeaf serves each run of a program it
// carries (internal/delegate): an OpenAI-style chat-completions endpoint on
// this machine, opened by one token, whose every call goes through codeaf's own
// model funnel — refused at the run's ceiling, priced, logged, and written down
// as one turn of the program's conversation with codeaf.
//
// IT LIVES UNDER internal/provider BECAUSE THAT IS THE ONLY PLACE A MODEL
// ROUTE MAY BE SPELLED (funnel_law_test.go). A program in codeaf's own tree
// builds its request URL with [ChatURL] rather than appending the route
// itself, so the route is written once, here, and the law holds for the
// program's code as for everything else.
package modelapi

import "strings"

// chatRoute is the one route a program calls, relative to the API's base URL.
const chatRoute = "/chat/completions"

// ChatURL is the chat-completions endpoint of an API whose base URL is base,
// the way every OpenAI client joins them: one slash between.
func ChatURL(base string) string {
	return strings.TrimRight(strings.TrimSpace(base), "/") + chatRoute
}

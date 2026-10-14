package builder

// teachingPage is the worker's page about the `codeaf app` verbs. It rides in
// the task brief so the worker does not spend a turn reading it. The full page
// is prompt/appbuilder.md, which the draft lane owns; until that is embedded
// this short page carries the order and the finish rule, and `codeaf app help`
// prints the verb table.
func teachingPage() string { return fallbackPage }

const fallbackPage = `You build one app through the "codeaf app" command. There are no files to write.
Every verb checks the piece you give it at once. An answer that starts with "error:" changed
nothing: fix that one piece on the next call and never send the same text twice.

Order: name and purpose, then plan lines, then "codeaf app plan ready". After that: records and
fields, screens, runs, the agent, connect/trigger/command, "codeaf app check", "codeaf app try <run>"
for each run, and last "codeaf app done". Read the draft back with "codeaf app show". A screen, run
or agent text goes in on stdin as a heredoc, one piece per call.
Never run "plandb add" or "plandb split". Finish with "codeaf app done" and wait for "ok", then
run "plandb done".`

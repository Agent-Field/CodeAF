package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/store"
)

// priorOutcomeLimit is how many prior attempts a single turn may be shown. Two
// is a caution, not a wall of history, and it rides inside the same
// [memoryBlockRunes] ceiling as the rules and the impacts.
const priorOutcomeLimit = 2

// recordMemoryAttempt writes one independently observed outcome at the
// executeTool boundary, BEFORE any model has been asked what the turn meant. It
// is the reason a real failure outlives the turn that produced it: the evidence
// buffer is turn-scoped, but this row is in the canonical journal, so a single
// extraction candidate or a rollover cannot erase it.
//
// IT LEARNS FROM FAILURE AND BLOCKING ONLY. A successful call is not stored —
// saving every shell success is the noise contract 6 refuses — and the status
// comes from the tool boundary's own reading, never from model text: a refusal
// is blocked/unknown, never a demonstrated failure, and no overall exit code
// proves a sub-check passed.
//
// ONE SUCCESS IS CAUGHT, AND ONLY AS AN ALTERNATIVE. When the SAME turn and
// goal that recorded a demonstrated failure later succeeds at the boundary —
// the same tool class, an action sharing a meaningful token with the one that
// failed — that success is carried on the failure's evidence as its observed
// alternative ([Agent.recordMemoryAlternative]). That is the one success kept,
// and only when there is a failure for it to belong to; a turn of ordinary
// successes writes nothing.
func (a *Agent) recordMemoryAttempt(ctx context.Context, turn uint64, call ai.ToolCall, result toolResult, preSnapshot string) {
	if !a.remembers() {
		return
	}
	status := attemptOutcomeStatus(result)
	if status == "" {
		a.recordMemoryAlternative(ctx, turn, call, result, preSnapshot)
		return
	}
	if !attemptWorthStoring(call, result) {
		return
	}
	action := attemptAction(call)
	if action == "" {
		return
	}
	// The receipt bytes are the SAME clip the evidence path hashes, so the two
	// rows share a source key and an explicit forget retires both together. The
	// stored observation is redacted and untrusted text; the hash is identity.
	//
	// IT IS REDACTED BEFORE IT IS HASHED, AND THAT ORDER IS THE CONTRACT. The
	// evidence path stores the redacted clip and hashes the bytes it stored
	// ([Agent.recordMemoryTool] -> [Agent.recordContextualMemory]). Hashing the
	// raw clip here made the two rows disagree whenever a receipt carried a
	// secret-shaped span: the memory-forget provenance join matches a source by
	// key AND hash, so a claim forgotten through its evidence row no longer
	// retired the attempt learned from the same receipt. Redacting first makes
	// the stored observation and the hash the same bytes on both writers, and a
	// secret never reaches the journal on either.
	receipt := redact.Secrets(contextualClip(result.text, contextualReceiptRunes))
	if strings.TrimSpace(receipt) == "" {
		return
	}
	// THE CALL'S OWN CIRCUMSTANCES, BEFORE AND AFTER. A snapshot taken only
	// after the action could certify a state the failure was never earned under;
	// capturing the pre-action state and comparing lets a tree that moved while
	// the call ran be reported honestly as unknown rather than as the post state.
	post := a.captureSourceSnapshot(ctx).Identity
	snapshot := post
	if preSnapshot != post {
		snapshot = "unknown"
	}
	a.memory.mu.Lock()
	goal := a.memory.outcomeGoal
	turnID := a.memory.outcomeTurnID
	a.memory.mu.Unlock()
	if turnID == "" {
		turnID = a.memorySourceSession() + ":" + fmt.Sprint(turn)
	}
	owner := a.ownerForScope(store.MemoryScopeProject)
	conditions := map[string]string{}
	if key := strings.TrimSpace(a.config.MemoryProjectKey); key != "" {
		conditions["project"] = key
	}
	receipts := []string{}
	if status == store.AttemptFailed {
		receipts = append(receipts, call.ID)
	}
	e := store.ContextualAttempt{
		ID:          store.NewMemoryID(),
		Owner:       owner,
		SessionID:   a.memorySourceSession(),
		TurnID:      turnID,
		Tool:        call.Function.Name,
		Action:      redact.Secrets(contextualClip(action, 1024)),
		Goal:        redact.Secrets(contextualClip(goal, 1024)),
		Status:      status,
		ReceiptIDs:  receipts,
		Observation: receipt,
		Snapshot:    snapshot,
		Conditions:  conditions,
		SourceKey:   turnID + ":" + call.ID,
		SourceHash:  contextualHash(receipt),
		ValidFrom:   time.Now(),
	}
	if _, err := a.memory.store.AppendContextualAttempt(e); err != nil {
		a.journalMemoryFailure("attempt", err)
		return
	}
	// ONLY A DEMONSTRATED FAILURE CAN BEAR AN ALTERNATIVE. A block is an unknown
	// outcome, not a proven dead end, so it is not paired: there is nothing the
	// later success was an alternative to. The failure's own source key, tool
	// and action are kept until the turn ends or a strictly newer demonstrated
	// failure replaces them, and its one alternative is then allowed.
	if status == store.AttemptFailed {
		a.memory.mu.Lock()
		a.memory.outcomeFailedKey = e.SourceKey
		a.memory.outcomeFailedTool = e.Tool
		a.memory.outcomeFailedAction = e.Action
		a.memory.outcomeAlternativeDone = false
		a.memory.mu.Unlock()
	}
}

// recordMemoryAlternative carries the ONE success that follows a demonstrated
// failure of the same turn and goal. It is the only successful call this turn
// will ever store, and it is stored on the failure rather than in its own right:
// the row is an AttemptSucceeded whose AlternativeOf names the failed attempt's
// source key, so the read side can render the pair and an explicit forget of
// either source retires the pair by the ordinary suppression join.
//
// THE PAIRING IS NARROW AND LABELLED AS OBSERVED. It requires the same tool
// class and a shared meaningful token, never a claim that the second call ran
// BECAUSE the first failed; the read side calls it an observed alternative, not
// a cause. A true tool-boundary success only: a harness door or a refusal is not
// a success, and a summary is not consulted. A lookup or a metadata call is
// never an alternative to an action failure, so a later `ls` cannot be dressed
// up as the way the work got done.
func (a *Agent) recordMemoryAlternative(ctx context.Context, turn uint64, call ai.ToolCall, result toolResult, preSnapshot string) {
	if !a.remembers() {
		return
	}
	if result.harness || result.refusedBy != "" || result.isError {
		return
	}
	action := attemptAction(call)
	receipt := redact.Secrets(contextualClip(result.text, contextualReceiptRunes))
	if action == "" || strings.TrimSpace(receipt) == "" {
		return
	}
	// RESERVE THE ONE SLOT BEFORE THE WRITE. The decision and the reservation
	// run under ONE lock, so two eligible siblings of a concurrent tool batch
	// cannot both append: the second sees the slot already taken and returns.
	// The earlier check-then-append-then-mark admitted every sibling, because
	// the mark landed only after the journal write ([Agent.recordMemoryAttempt]
	// is called from each of the batch's goroutines).
	a.memory.mu.Lock()
	key := a.memory.outcomeFailedKey
	if key == "" || a.memory.outcomeAlternativeDone || !alternativeEligible(call, a.memory.outcomeFailedTool, a.memory.outcomeFailedAction, a.memory.outcomeGoal) {
		a.memory.mu.Unlock()
		return
	}
	a.memory.outcomeAlternativeDone = true
	goal := a.memory.outcomeGoal
	turnID := a.memory.outcomeTurnID
	a.memory.mu.Unlock()
	// THE ALTERNATIVE'S OWN CIRCUMSTANCES, taken before and after this call, the
	// same way a failure's are: a tree that moved while the call ran is unknown.
	post := a.captureSourceSnapshot(ctx).Identity
	snapshot := post
	if preSnapshot != post {
		snapshot = "unknown"
	}
	if turnID == "" {
		turnID = a.memorySourceSession() + ":" + fmt.Sprint(turn)
	}
	owner := a.ownerForScope(store.MemoryScopeProject)
	conditions := map[string]string{}
	if projectKey := strings.TrimSpace(a.config.MemoryProjectKey); projectKey != "" {
		conditions["project"] = projectKey
	}
	e := store.ContextualAttempt{
		ID:            store.NewMemoryID(),
		Owner:         owner,
		SessionID:     a.memorySourceSession(),
		TurnID:        turnID,
		Tool:          call.Function.Name,
		Action:        redact.Secrets(contextualClip(action, 1024)),
		Goal:          redact.Secrets(contextualClip(goal, 1024)),
		Status:        store.AttemptSucceeded,
		ReceiptIDs:    []string{call.ID},
		Observation:   receipt,
		Snapshot:      snapshot,
		Conditions:    conditions,
		AlternativeOf: key,
		SourceKey:     turnID + ":" + call.ID,
		SourceHash:    contextualHash(receipt),
		ValidFrom:     time.Now(),
	}
	if _, err := a.memory.store.AppendContextualAttempt(e); err != nil {
		// A FAILED WRITE REOPENS THE PAIRING, but only while the SAME failure is
		// still the turn's most recent one: a newer failure owns the slot, and
		// clearing it would let this dead row's sibling attach to the wrong
		// work. The failure itself is exposed through the existing lane, never
		// swallowed, and the reserved slot is given back so a retry can try.
		a.memory.mu.Lock()
		if a.memory.outcomeFailedKey == key {
			a.memory.outcomeAlternativeDone = false
		}
		a.memory.mu.Unlock()
		a.journalMemoryFailure("attempt-alternative", err)
		return
	}
}

// alternativeExcludedTools are the bare lookups and metadata calls that are
// never the observed alternative to an action failure. A successful `ls` after a
// failed import says nothing about how the work got done, so it is refused
// rather than rendered as the way through.
var alternativeExcludedTools = map[string]bool{
	"read": true, "ls": true, "glob": true, "grep": true, "find": true,
	"search": true, "list": true, "view": true, "open": true, "head": true,
	"tail": true, "cat": true, "remember": true, "stand": true, "forget": true,
	"ask": true, "manual": true, "tasks": true, "jobs": true, "settings": true,
	"skill": true, "workspace": true,
}

// alternativeEligible is the whole association rule, and it is deliberately
// lexical and narrow: the success must be the SAME tool class as the failure,
// must not be a bare lookup or metadata call, and its action must share a
// meaningful WHOLE token with the failed action. Anything less is not labelled
// an alternative; the model is left to judge relevance for itself.
func alternativeEligible(call ai.ToolCall, failedTool, failedAction, goal string) bool {
	name := strings.TrimSpace(call.Function.Name)
	if name == "" || name != strings.TrimSpace(failedTool) || alternativeExcludedTools[name] {
		return false
	}
	// THE TOOL NAME IS NOT A MEANINGFUL TOKEN. The stored action is "name: body",
	// so matching on the whole string let every bash action share a token
	// ("bash") with every other. The match runs on the two BODIES, and a call
	// with no body is not an alternative to an action.
	body := strings.TrimSpace(attemptActionBody(attemptAction(call)))
	if body == "" {
		return false
	}
	// A SHELL METADATA OR LOOKUP ACTION IS NEVER THE WAY THE WORK GOT DONE,
	// whether it is the `ls` tool or that same read wrapped in a pipeline:
	// `cd <cwd> && ls -la && cat week.csv`, or `git log`, is a lookup rather
	// than the action that answered a failure. It is judged by what each
	// command DOES, never by the tool's name.
	if shellToolName(name) && shellMetadataOnly(body) {
		return false
	}
	return sharedMeaningfulActionToken(attemptActionBody(failedAction), body, goal)
}

// sharedMeaningfulActionToken answers whether the two action bodies share a
// WHOLE, meaningful token. Matching is on tokens, never on a raw substring, so
// `ledger` does not match inside `ledgering`. A token the success carries only
// as part of a path (the shared cwd every command of one session runs in) never
// counts; a token the failure carries only as part of a path counts only when
// the turn's own GOAL named it, because a goal token is the purpose the work was
// done for and a cwd part like the project name is not. Without both halves the
// shared `cd /home/.../<project> &&` prefix alone would make any unrelated bash
// command the alternative.
func sharedMeaningfulActionToken(failedBody, successBody, goal string) bool {
	failed := actionTokens(failedBody)
	if len(failed) == 0 {
		return false
	}
	success := actionTokens(successBody)
	if len(success) == 0 {
		return false
	}
	failedPaths := shellPathTokens(failedBody)
	successPaths := shellPathTokens(successBody)
	purpose := outcomeTerms(goal)
	for token := range success {
		if !failed[token] || successPaths[token] {
			continue
		}
		if failedPaths[token] && !purpose[token] {
			continue
		}
		return true
	}
	return false
}

// actionTokens splits a lowercased action body into its distinct WHOLE words.
// A token is a maximal run of letters and digits; short fragments, the generic
// engineering vocabulary and the shell/interpreter words that join any two
// commands are dropped, so only a meaningful token can carry the association.
func actionTokens(body string) map[string]bool {
	tokens := map[string]bool{}
	for _, field := range strings.FieldsFunc(strings.ToLower(body), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len([]rune(field)) < 3 || outcomeStopwords[field] || actionGenericTokens[field] {
			continue
		}
		tokens[field] = true
	}
	return tokens
}

// shellPathTokens returns the words an action contributes ONLY through an
// ABSOLUTE path-like span: the shared cwd. A word the body ALSO spells outside
// a path (the `ledger` in `ledger.py`, beside the cwd's own `ledger`) is not
// path-only, because the action named the thing itself. A relative `./target`
// is the thing under test rather than a cwd part, so it is not collected.
func shellPathTokens(body string) map[string]bool {
	inPath := map[string]bool{}
	elsewhere := map[string]bool{}
	for _, field := range strings.Fields(body) {
		word := strings.Trim(field, "\"'`()[]{};,&|")
		parts := splitActionWord(word)
		absolute := strings.HasPrefix(word, "/") || strings.HasPrefix(word, "~") || strings.HasPrefix(word, "$HOME")
		for _, part := range parts {
			if absolute {
				inPath[part] = true
			} else {
				elsewhere[part] = true
			}
		}
	}
	for token := range elsewhere {
		delete(inPath, token)
	}
	return inPath
}

// splitActionWord is the one whole-token splitter the association uses: a word
// becomes its maximal runs of letters and digits.
func splitActionWord(word string) []string {
	return strings.FieldsFunc(strings.ToLower(word), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// actionGenericTokens are the shell, path and interpreter words an action
// shares with any other action: navigation, the interpreters' own names and the
// bare directory words a build sits in. They are never evidence of the same
// work.
var actionGenericTokens = map[string]bool{
	"cd": true, "chdir": true, "pushd": true, "popd": true, "sudo": true,
	"doas": true, "exec": true, "export": true, "source": true, "env": true,
	"set": true, "unset": true, "echo": true, "exit": true, "nohup": true,
	"then": true, "else": true, "done": true, "fi": true, "esac": true,
	"and": true, "not": true, "true": true, "false": true,
	"import": true, "from": true, "print": true, "return": true, "def": true,
	"class": true, "func": true, "main": true, "args": true, "argv": true,
	"python": true, "python3": true, "pip": true, "node": true, "ruby": true,
	"perl": true, "java": true, "bash": true, "zsh": true, "dash": true,
	"venv": true, "bin": true, "usr": true, "lib": true, "opt": true,
	"tmp": true, "var": true, "etc": true, "local": true, "share": true,
}

// shellToolName reports whether a tool runs a shell command, so its action body
// can be read as a pipeline.
func shellToolName(name string) bool {
	switch name {
	case "bash", "sh", "shell", "zsh", "dash":
		return true
	}
	return false
}

// shellMetadataOnly answers whether a shell action is nothing but metadata and
// lookups: reading a directory, printing a file, asking git for history. Such a
// command is never the way a failed piece of work got done, whether it is a
// bare `ls` or the same read wrapped in a pipeline. A body it cannot read is
// treated as ordinary work, never as metadata, so an unfamiliar command is
// never mislabelled.
func shellMetadataOnly(body string) bool {
	replacer := strings.NewReplacer("&&", "\n", "||", "\n")
	segments := strings.FieldsFunc(replacer.Replace(body), func(r rune) bool {
		return r == '\n' || r == ';' || r == '|'
	})
	read := false
	for _, segment := range segments {
		words := shellSegmentCommand(segment)
		if len(words) == 0 {
			continue
		}
		read = true
		if !shellCommandIsMetadata(words) {
			return false
		}
	}
	return read
}

// shellSegmentCommand returns the command word a shell segment runs, with
// navigation, assignments and wrappers skipped so the real program is read. A
// `git` segment also carries its subcommand. It is a small lexical reader, not
// a shell parser.
func shellSegmentCommand(segment string) []string {
	fields := strings.Fields(segment)
	for i := 0; i < len(fields); i++ {
		word := strings.Trim(fields[i], "\"'`")
		if word == "" || shellAssignment(word) {
			continue
		}
		command := shellWordBase(word)
		if shellCommandIgnored[command] {
			continue
		}
		words := []string{command}
		if command == "git" {
			for j := i + 1; j < len(fields); j++ {
				candidate := strings.Trim(fields[j], "\"'`")
				if strings.HasPrefix(candidate, "-") || shellAssignment(candidate) || strings.ContainsAny(candidate, "/\\") {
					continue
				}
				words = append(words, strings.ToLower(candidate))
				break
			}
		}
		return words
	}
	return nil
}

// shellCommandIsMetadata answers whether one command word is a metadata or
// lookup action. A bare git counts (it prints usage); a git read subcommand
// counts; anything else does not.
func shellCommandIsMetadata(words []string) bool {
	if len(words) == 0 {
		return true
	}
	if words[0] == "git" {
		return len(words) < 2 || shellGitReadSubcommands[words[1]]
	}
	return shellMetadataCommands[words[0]]
}

// shellWordBase is the program name at the end of a possibly relative or
// absolute path, lowercased.
func shellWordBase(word string) string {
	if i := strings.LastIndexAny(word, "/\\"); i >= 0 {
		word = word[i+1:]
	}
	return strings.ToLower(word)
}

// shellAssignment reports whether a word is a NAME=value binding rather than a
// command.
func shellAssignment(word string) bool {
	i := strings.Index(word, "=")
	if i <= 0 {
		return false
	}
	for _, r := range word[:i] {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

// shellCommandIgnored are the shell words skipped before the real program is
// read: navigation, wrappers and variable setting, never a lookup itself.
var shellCommandIgnored = map[string]bool{
	"cd": true, "chdir": true, "pushd": true, "popd": true, "sudo": true,
	"doas": true, "env": true, "exec": true, "nohup": true, "time": true,
	"then": true, "else": true, "done": true, "fi": true, "esac": true,
	"do": true, "for": true, "while": true, "if": true, "and": true, "or": true,
}

// shellMetadataCommands are the read-only lookup and metadata verbs that are
// never the way a failed action got done.
var shellMetadataCommands = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true, "find": true,
	"grep": true, "rg": true, "wc": true, "stat": true, "file": true,
	"du": true, "df": true, "tree": true, "pwd": true, "echo": true,
	"which": true, "whereis": true, "type": true,
	"less": true, "more": true, "column": true, "realpath": true,
	"readlink": true, "basename": true, "dirname": true, "whoami": true,
	"uname": true, "date": true, "hostname": true, "id": true, "sort": true,
	"uniq": true, "cut": true, "tr": true, "diff": true, "md5sum": true,
	"sha256sum": true, "help": true, "man": true, "history": true,
}

// shellGitReadSubcommands are the git subcommands that only read history or
// configuration; any other git command is treated as ordinary work.
var shellGitReadSubcommands = map[string]bool{
	"log": true, "status": true, "diff": true, "show": true, "branch": true,
	"remote": true, "config": true, "ls-files": true, "rev-parse": true,
	"describe": true, "blame": true, "tag": true, "shortlog": true,
	"reflog": true, "cat-file": true, "ls-remote": true, "show-ref": true,
	"symbolic-ref": true, "whatchanged": true,
}

// attemptActionBody strips the "tool: " prefix [attemptAction] adds and returns
// the bare action, or "" when the call carried none.
func attemptActionBody(action string) string {
	if i := strings.Index(action, ": "); i >= 0 {
		return strings.TrimSpace(action[i+2:])
	}
	return ""
}

// attemptWorthStoring refuses the noise contract 5 names. A failed LOOKUP — a
// read/ls/grep/glob of a path that simply is not there — is not a lesson about
// the world, and an argument the harness rejected before anything ran is not an
// outcome at all. A blocked door is KEPT: it is the harness's own refusal and
// the honest unknown contract 1 preserves.
func attemptWorthStoring(call ai.ToolCall, result toolResult) bool {
	if result.harness || result.refusedBy != "" {
		return true
	}
	if !result.isError {
		return false
	}
	text := strings.TrimSpace(result.text)
	if strings.HasPrefix(text, "Invalid arguments") || strings.HasPrefix(text, "Unknown tool:") {
		return false
	}
	switch call.Function.Name {
	case "read", "ls", "glob", "grep", "find", "search", "list", "view", "open", "head", "tail", "cat":
		return !lookupMiss(text)
	}
	return true
}

// lookupMiss answers whether an error is a provably-empty lookup rather than a
// failure with something to learn. The markers are the shapes a missing path or
// an empty result set answers with; a permission error or a real diagnostic
// carries none of them and is kept.
func lookupMiss(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{"no such file", "not found", "no matches", "did not match", "does not exist", "no files", "no entries", "cannot find", "no results", "empty directory"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// attemptOutcomeStatus reads how a call ended. A refusal or a harness failure
// is BLOCKED — its outcome is unknown, not proven — and only an error the world
// returned is a demonstrated failure. A success returns "" and is not stored.
func attemptOutcomeStatus(result toolResult) string {
	switch {
	case result.refusedBy != "" || result.harness:
		return store.AttemptUnknown
	case result.isError:
		return store.AttemptFailed
	default:
		return ""
	}
}

// attemptAction is the human-readable action a call tried: a shell command, a
// path, or the tool's own name when it carries neither. It is the ACTION, kept
// apart from any inferred cause.
func attemptAction(call ai.ToolCall) string {
	var args struct {
		Command string `json:"command"`
		Cmd     string `json:"cmd"`
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	}
	_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
	name := strings.TrimSpace(call.Function.Name)
	for _, candidate := range []string{args.Command, args.Cmd, args.Path, args.Pattern} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return name + ": " + contextualClip(trimmed, 240)
		}
	}
	return name
}

// priorOutcomeContext is the before-action half of contract 4. It runs inside
// prepareBindingContext, BEFORE the first provider request of the turn, so a
// prior verified failure sits in front of the model before it chooses a
// matching action. It shows only failures and blocks, only ones lexically
// relevant to the goal, and always as an ADVISORY observation with its
// circumstance label — never a prohibition and never a claimed cause.
func (a *Agent) priorOutcomeContext(cue, snapshot string) string {
	if !a.remembers() || memoryTrivialCue(cue) {
		return ""
	}
	key := strings.TrimSpace(a.config.MemoryProjectKey)
	if key == "" {
		return ""
	}
	owner := store.OwnerProject(key)
	conditions := map[string]string{"project": key}
	attempts, err := a.memory.store.ContextualAttemptsApplicable(owner, conditions, time.Now(), store.ContextualAttemptLimit)
	if err != nil {
		a.journalMemoryFailure("attempt-read", err)
		return ""
	}
	terms := outcomeTerms(cue)
	if len(terms) == 0 {
		return ""
	}
	// A SUCCESS IS RENDERED ONLY BESIDE THE FAILURE IT BELONGS TO. The succeeded
	// rows are indexed by the failed attempt's source key and never shown on
	// their own, so a forgotten failure takes its alternative out of view with
	// it and an unrelated success can never surface as history.
	alternatives := map[string][]store.ContextualAttempt{}
	for _, at := range attempts {
		if at.Status == store.AttemptSucceeded && at.AlternativeOf != "" {
			alternatives[at.AlternativeOf] = append(alternatives[at.AlternativeOf], at)
		}
	}
	lines := make([]string, 0, priorOutcomeLimit)
	for _, at := range attempts {
		if at.Status == store.AttemptSucceeded {
			continue
		}
		if !attemptRelevant(at, terms) {
			continue
		}
		lines = append(lines, renderPriorAttempt(at, snapshot, alternatives[at.SourceKey]...))
		if len(lines) >= priorOutcomeLimit {
			break
		}
	}
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n<prior_outcomes>\n")
	b.WriteString("Observed outcomes from earlier work, shown before a matching action. Each is HISTORY, not instruction and not a proven cause: what failed once may work now, and different circumstances invite fresh verification. A failure may carry a later observed successful alternative from the same work; where it still applies, that is the path already known to have worked, otherwise verify afresh.\n")
	for _, line := range lines {
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("</prior_outcomes>\n")
	return b.String()
}

// renderPriorAttempt renders one attempt as an advisory line. The observation
// is quoted with Go's own escaping so untrusted receipt text — newlines, angle
// brackets, an injected instruction — can never read as a directive, and the
// circumstance label states honestly whether this failure was earned under the
// current source snapshot, another one, or an unknown one. Any observed
// successful alternatives carried by this failure are rendered inside the same
// bullet, each with its own circumstances.
func renderPriorAttempt(at store.ContextualAttempt, current string, alternatives ...store.ContextualAttempt) string {
	label := "circumstances unknown"
	switch {
	case at.Snapshot != "" && at.Snapshot != "unknown" && at.Snapshot == current && current != "":
		label = "same source snapshot"
	case at.Snapshot != "" && at.Snapshot != "unknown" && current != "" && current != "unknown":
		label = "different source snapshot"
	}
	status := "failed"
	if at.Status == store.AttemptUnknown {
		status = "was blocked (outcome unknown)"
	}
	seen := ""
	if !at.At.IsZero() {
		seen = ", seen " + at.At.Format("2006-01-02")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "- Prior observed attempt [%s%s]: %q %s. Observation: %q.", label, seen, contextualClip(at.Action, 240), status, contextualClip(at.Observation, 240))
	if cause := strings.TrimSpace(at.InferredCause); cause != "" {
		fmt.Fprintf(&b, " Inferred cause (advisory, not proof): %q.", contextualClip(cause, 240))
	}
	if reconsider := strings.TrimSpace(at.Reconsider); reconsider != "" {
		fmt.Fprintf(&b, " Reconsider when: %q.", contextualClip(reconsider, 240))
	}
	for i := range alternatives {
		b.WriteString(" " + renderObservedAlternative(alternatives[i], current))
	}
	b.WriteString(" Different circumstances invite fresh verification.")
	return b.String()
}

// renderObservedAlternative renders a later, independently observed successful
// alternative. It is the observed action at the tool boundary and its own
// receipt, deliberately worded as HISTORY and as ONE OBSERVED PATH rather than a
// cause: it says a success was seen, never that it was why the failure stopped.
// Its circumstance label is the source snapshot it was earned under, and the
// line refuses to pretend a snapshot is the environment — an ignored virtual
// environment can change underneath an identical tree, so a matching snapshot
// narrows the check without ever being a permanent ban.
func renderObservedAlternative(at store.ContextualAttempt, current string) string {
	label := "circumstances unknown"
	switch {
	case at.Snapshot != "" && at.Snapshot != "unknown" && at.Snapshot == current && current != "":
		label = "same source snapshot"
	case at.Snapshot != "" && at.Snapshot != "unknown" && current != "" && current != "unknown":
		label = "different source snapshot"
	}
	seen := ""
	if !at.At.IsZero() {
		seen = ", seen " + at.At.Format("2006-01-02")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Observed successful alternative [%s%s]: %q succeeded at the tool boundary. Observation: %q.", label, seen, contextualClip(at.Action, 240), contextualClip(at.Observation, 240))
	// NO CAUSAL CLAIM AND NO HARD BAN: the alternative is preferred only while
	// its circumstances still hold, and a snapshot is explicitly not the
	// environment, because an ignored virtual environment can change under an
	// identical tree.
	b.WriteString(" This is one observed successful path from the same work, not proof of cause and not an instruction; a source snapshot is not the environment, so prefer it only while these circumstances still hold, and treat changed or unknown source or environment as an invitation to verification rather than a ban.")
	return b.String()
}

// attemptRelevant answers whether an attempt shares MEANINGFUL terms with the
// turn's goal — never one substring of a generic engineering word. Unrelated
// history stays quiet: a failure of a different task is not shown before this
// one. One shared term is enough only when it is a token of the action itself,
// which is where the work actually named the thing; otherwise two distinct
// terms are required.
func attemptRelevant(at store.ContextualAttempt, terms map[string]bool) bool {
	haystack := strings.ToLower(at.Goal + " " + at.Action)
	action := strings.ToLower(at.Action)
	matched := 0
	for term := range terms {
		if strings.Contains(haystack, term) {
			matched++
		}
	}
	if matched >= 2 {
		return true
	}
	if matched == 1 {
		for term := range terms {
			if strings.Contains(action, term) && strings.Contains(haystack, term) {
				return true
			}
		}
	}
	return false
}

// outcomeStopwords are the words a software task shares with every other one.
// Matching on them would surface a prior failure of an unrelated task the
// moment two goals both said "implement" or "tests".
var outcomeStopwords = map[string]bool{
	"implement": true, "feature": true, "script": true, "tests": true, "test": true,
	"code": true, "bug": true, "fix": true, "file": true, "files": true,
	"update": true, "change": true, "work": true, "task": true, "build": true,
	"error": true, "issue": true, "please": true, "make": true, "help": true,
	"need": true, "want": true, "using": true, "with": true, "this": true,
	"that": true, "from": true, "when": true, "then": true, "them": true,
	"they": true, "have": true, "should": true,
}

// outcomeTerms reduces a goal to the distinct words worth matching on. Short
// words and the generic engineering vocabulary are dropped so "the", "run",
// "implement" and "tests" do not drag every attempt into view.
func outcomeTerms(cue string) map[string]bool {
	terms := map[string]bool{}
	for _, field := range strings.FieldsFunc(cue, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		word := strings.ToLower(field)
		if len([]rune(word)) >= 4 && !outcomeStopwords[word] {
			terms[word] = true
		}
	}
	return terms
}

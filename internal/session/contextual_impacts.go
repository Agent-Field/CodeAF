package session

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/gitidentity"
	"github.com/Agent-Field/codeaf/internal/store"
)

const contextualFileBytes = 64 << 10

// contextualReadReceipt accepts only a successful full read the tool actually
// made. Partial output, refused reads and arbitrary shell programs prove no link.
func (a *Agent) contextualReadReceipt(ctx context.Context, call ai.ToolCall, result toolResult, r memoryToolReceipt) memoryToolReceipt {
	if result.isError || result.harness {
		return r
	}
	var path string
	if call.Function.Name == "read" {
		var args struct {
			Path   string `json:"path"`
			Offset *int   `json:"offset"`
			Limit  *int   `json:"limit"`
		}
		if json.Unmarshal([]byte(call.Function.Arguments), &args) != nil || args.Offset != nil || args.Limit != nil {
			return r
		}
		path = args.Path
	} else if call.Function.Name == "bash" {
		var args struct {
			Command string `json:"command"`
			Cmd     string `json:"cmd"`
		}
		if json.Unmarshal([]byte(call.Function.Arguments), &args) != nil {
			return r
		}
		command := args.Command
		if command == "" {
			command = args.Cmd
		}
		words := strings.Fields(command)
		if len(words) != 2 || words[0] != "cat" || strings.ContainsAny(words[1], ";&|><$`*?\n") {
			return r
		}
		path = strings.Trim(words[1], "\"'")
	} else {
		return r
	}
	if path == "" {
		return r
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(a.config.Workspace, path)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return r
	}
	body, err := contextualReadFile(canonical)
	if err != nil || body == "" || !strings.Contains(result.text, body) {
		return r
	}
	r.Path = canonical
	r.Body = body
	r.Hash = contextualHash(body)
	// The receipt's OWN snapshot: the evidence row is earned under the state
	// the read saw, not the turn's arbitrary start-of-turn identity.
	r.Snapshot = a.captureSourceSnapshot(ctx).Identity
	return r
}

// contextualDropping answers whether a canonical path is one of THIS agent's own
// session droppings — a stub or job log the session wrote for itself — which is
// never independent source evidence about anybody's project. Both layouts are
// covered: the session folder's logs/ and, for a caller with no Place, the
// workspace's legacy .codeaf/. A condenser's stub is a COPY of a real file, so
// byte identity cannot tell them apart; the folder is what refuses it.
func (a *Agent) contextualDropping(path string) bool {
	roots := []string{}
	if logs := a.config.droppingsPlace().Logs(); logs != "" {
		roots = append(roots, logs)
	}
	if workspace := strings.TrimSpace(a.config.Workspace); workspace != "" {
		roots = append(roots, filepath.Join(workspace, flatDroppingsDir))
	}
	for _, root := range roots {
		if canonical := canonicalPath(root); canonical != "" && contextualPathWithin(path, canonical) {
			return true
		}
	}
	return false
}

// contextualPathWithin answers whether path is root itself or a descendant.
func contextualPathWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// contextualForwardedReads keeps only the receipts that carry a trusted full
// read — the canonical Path, the content Hash and the Body the tool returned.
// It is the ONE filter a caller uses to forward receipts from an original
// execution boundary into a frozen origin BEFORE a condenser replaces the
// result with a stub. Worker prose, stub text and arbitrary successful shell
// output carry no Path/Hash/Body and are dropped. The helper is pure; the seam
// that calls it decides when a worker is created.
func contextualForwardedReads(rows []memoryToolReceipt) []memoryToolReceipt {
	out := make([]memoryToolReceipt, 0, len(rows))
	for _, r := range rows {
		if r.Path == "" || r.Hash == "" || r.Body == "" {
			continue
		}
		out = append(out, r)
	}
	return out
}

// contextualTurnReceipts makes the delegated and the root paths ONE evidence set
// for the frozen turn they share. A worker forwards its own full reads under the
// origin turn captured at its creation; the root session's OWN receipts for that
// same turn — a `read` it actually made at its execution boundary — are still
// live while the delegated call runs, so they are merged in BEFORE the condenser
// replaces that result with a stub. Only receipts that are a trusted full read
// are carried across, so an unrelated observation cannot ride the turn either.
//
// THE MERGE IS AUTHENTICATED, NOT MERELY KEYED ON A NUMBER. The live cache is
// mutable and is read at observation time, so three things must agree before a
// root receipt is allowed to join a frozen origin's set: the SESSION the turn
// spelling names, the OWNER the origin was admitted under, and — when the origin
// carries one — its admitted PROJECT. Without the session check a source at a
// colliding numeric turn could borrow another session's reads; without the owner
// check a root anchor that re-homed the conversation after the worker was
// admitted could let the worker's observation borrow a post-anchor read of a
// different repository. The source's OWN trusted boundary receipts are never
// filtered: they were earned under the origin when it was stamped.
func (a *Agent) contextualTurnReceipts(source memoryTurnEvidence) []memoryToolReceipt {
	rows := append([]memoryToolReceipt(nil), source.Receipts...)
	seq, ok := contextualTurnSeq(source.Turn)
	if !ok {
		return rows
	}
	// The turn spelling carries the session it belongs to. A source that names a
	// DIFFERENT session cannot borrow this agent's live cache, whatever its
	// numeric turn.
	if session := strings.TrimSpace(source.Session); session != "" && session != a.memorySourceSession() {
		return rows
	}
	a.memory.mu.Lock()
	own := append([]memoryToolReceipt(nil), a.memory.receipts[seq]...)
	a.memory.mu.Unlock()
	if len(own) == 0 {
		return rows
	}
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		seen[r.ID] = true
	}
	for _, r := range contextualForwardedReads(own) {
		if seen[r.ID] {
			continue
		}
		if !contextualReceiptAdmitted(source, r) {
			continue
		}
		seen[r.ID] = true
		rows = append(rows, r)
	}
	return rows
}

// contextualReceiptAdmitted answers whether one of the ROOT's own live read
// receipts may join a frozen source. A conversation's own turn carries no owner
// and admits every one of its own reads. A delegated origin's frozen owner admits
// only a receipt whose exact path resolves to that owner's repository, and when
// the origin also froze a project key, to that same project — so a read of a
// different repository, taken before or after a root anchor, can never be
// borrowed into an already-admitted worker's evidence.
func contextualReceiptAdmitted(source memoryTurnEvidence, r memoryToolReceipt) bool {
	owner := strings.TrimSpace(source.Owner)
	if owner == "" {
		return true
	}
	pathOwner := contextualPathOwner(r.Path)
	if pathOwner == "" || pathOwner != owner {
		return false
	}
	if project := strings.TrimSpace(source.Project); project != "" && projectKeyFromOwner(pathOwner) != project {
		return false
	}
	return true
}

// contextualTurnSeq reads the turn number from either spelling of a turn id:
// the root's own "seq:hash" and the frozen origin's "session:seq:hash".
func contextualTurnSeq(turn string) (uint64, bool) {
	parts := strings.Split(strings.TrimSpace(turn), ":")
	if len(parts) == 3 {
		parts = parts[1:]
	}
	if len(parts) != 2 {
		return 0, false
	}
	n, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// contextualPathLiteral is a quoted path-like literal: it must carry a directory
// separator, so an ordinary quoted word ("export.py", "amount") is never read as
// a path. It is only ever a CANDIDATE: it must appear in a construct that
// actually CONSUMES the file, and resolve to a real regular file inside the
// consumer's authorized source scope, before it means anything.
var contextualPathLiteral = regexp.MustCompile(`["']([^"'\n]*[/\\][^"'\n]*)["']`)

// contextualFileConsumer names the calls whose argument is an ACTUAL use of a
// file: the program opens, reads, executes, imports or loads it. A quoted path
// literal anywhere else — a comment, a print/log call, a bare assignment or a
// list of strings — is a MENTION, and a mention names no producer. The name is
// compared by its last dotted segment, so subprocess.check_output and io.open
// still count while logger.info does not.
var contextualFileConsumer = map[string]bool{
	"open": true, "read": true, "read_text": true, "read_bytes": true,
	"load": true, "exec": true, "compile": true,
	"check_output": true, "check_call": true, "popen": true, "call": true, "run": true,
	"sourcefileloader": true, "spec_from_file_location": true,
	"include": true, "require": true, "source": true, "cat": true,
}

// contextualCodeOnly drops the text that is NEVER evaluated — line comments and
// triple-quoted block strings — so a path merely mentioned in a comment, a
// docstring or a printed block is never read as a reference. It is deliberately
// conservative, and it always fails CLOSED: BOTH comment spellings the tree sees
// are dropped (`#` for Python, `//` for the JavaScript the walk also reads) and
// the triple-quoted forms (three single quotes, three double quotes) are
// removed whole, so an unsupported
// mention is lost rather than accepted. An ordinary quoted span is kept, because
// a genuine consuming call's literal argument is the candidate; a `#` or `//`
// inside a plain string is left alone there.
func contextualCodeOnly(body string) string {
	var b strings.Builder
	for i := 0; i < len(body); {
		c := body[i]
		if c == '#' {
			for i < len(body) && body[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(body) && body[i+1] == '/' {
			for i < len(body) && body[i] != '\n' {
				i++
			}
			continue
		}
		if (c == '\'' || c == '"') && i+2 < len(body) && body[i+1] == c && body[i+2] == c {
			i = contextualPastTripleQuoted(body, i, c)
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			end := contextualPastQuoted(body, i, c)
			b.WriteString(body[i:end])
			i = end
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// contextualPastQuoted returns the index just past one plain quoted span,
// honouring backslash escapes.
func contextualPastQuoted(s string, i int, quote byte) int {
	i++
	for i < len(s) {
		if s[i] == '\\' {
			i += 2
			continue
		}
		if s[i] == quote {
			return i + 1
		}
		i++
	}
	return len(s)
}

// contextualPastTripleQuoted returns the index just past a triple-quoted block
// string. An unterminated block is dropped to the end, which can only ever lose
// a candidate.
func contextualPastTripleQuoted(s string, i int, quote byte) int {
	i += 3
	for i < len(s) {
		if s[i] == '\\' {
			i += 2
			continue
		}
		if s[i] == quote && i+2 < len(s) && s[i+1] == quote && s[i+2] == quote {
			return i + 3
		}
		i++
	}
	return len(s)
}

// contextualInsideQuoted answers whether offset sits inside a quoted string of
// code. A pathlib chain or a quoted path that appears only inside a printed or
// debug string is text the program never evaluates.
func contextualInsideQuoted(code string, offset int) bool {
	inside := false
	var quote byte
	for i := 0; i < offset && i < len(code); i++ {
		c := code[i]
		if c == '\\' {
			i++
			continue
		}
		if inside {
			if c == quote {
				inside = false
			}
			continue
		}
		if c == '"' || c == '\'' {
			inside = true
			quote = c
		}
	}
	return inside
}

// contextualLiteralConsumed answers whether a quoted path literal whose opening
// quote ends prefix is actually USED: an argument of a file-consuming call, or a
// reading operand/redirection (a `cat`, a `<`). Everything else — a print, a
// log, a bare assignment, a literal list — is a mention and proves nothing.
func contextualLiteralConsumed(prefix string) bool {
	p := strings.TrimRight(prefix, " \t\r\n")
	if p == "" {
		return false
	}
	switch {
	case strings.HasSuffix(p, "<"), strings.HasSuffix(p, "-f"),
		strings.HasSuffix(p, "--file"), strings.HasSuffix(p, "--file="),
		strings.HasSuffix(p, "cat"), strings.HasSuffix(p, "source"):
		return true
	}
	return contextualBracketConsumed(p)
}

// contextualBracketConsumed walks outward from the innermost still-open bracket
// and answers whether the call that owns it is a file consumer. A bracket with no
// call name of its own — a literal list, a grouping — is skipped so the call
// around it (check_output([...])) is still found.
func contextualBracketConsumed(p string) bool {
	depth := 0
	for i := len(p) - 1; i >= 0; i-- {
		switch p[i] {
		case ')', ']', '}':
			depth++
		case '(', '[', '{':
			if depth == 0 {
				name := contextualCallNameBefore(p, i)
				if name != "" {
					return contextualFileConsumer[name]
				}
				return contextualBracketConsumed(p[:i])
			}
			depth--
		}
	}
	return false
}

// contextualCallNameBefore reads the (possibly dotted) call name immediately
// before an opening bracket, ignoring spaces, and returns its lowercased last
// dotted segment.
func contextualCallNameBefore(p string, bracket int) string {
	end := bracket
	for end > 0 && (p[end-1] == ' ' || p[end-1] == '\t') {
		end--
	}
	start := end
	for start > 0 {
		c := p[start-1]
		if c == '.' || c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			start--
			continue
		}
		break
	}
	if start == end {
		return ""
	}
	name := p[start:end]
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	return strings.ToLower(name)
}

// contextualConsumerNeighborhood is the widest directory a consumer's own source
// can legitimately reference a producer from: the PARENT of the consumer's own
// repository root, which holds its sibling repositories. The consumer's own
// repository is inside it too. A path outside it is an arbitrary path named in
// file text, not a reference this conversation's source scope DISCOVERS, so the
// framework neither reads it nor mints an owner for it.
//
// LOOKING UP A DIRECTORY AND HASHING WHATEVER OWNER IT HAPPENS TO CARRY IS NOT A
// FROZEN ACCESS GRANT, and neither is this neighbourhood. It is a DISCOVERY
// bound only: it says where a source text is allowed to LOOK for a producer. The
// authority to actually READ the resolved file comes from the EXISTING consent
// policy ([Agent.contextualProducerReadAllowedUnder], reusing the pure
// [Agent.decide] gate AND the origin's frozen admission ceiling), so a private
// sibling repository a policy denies is never opened just because it shares a
// parent directory with the consumer, and a later blanket allow cannot widen an
// admission that denied it.
func contextualConsumerNeighborhood(consumerPath string) string {
	base := filepath.Dir(consumerPath)
	if root, ok := repositoryRoot(base); ok {
		base = root
	}
	return filepath.Dir(base)
}

// contextualReferencedProducers resolves the producer paths a consumer's source
// ACTUALLY references. A reference is either the code-level pathlib chain — a
// real expression the program evaluates against its own location, read FIRST so
// a list of quoted strings cannot starve it — or a quoted path literal in an
// actual file-consuming construct. The exact resolved path must be a real
// regular file within the consumer's authorized source scope; a name, a prefix,
// a same-basename match, a comment and a bare quoted string prove nothing.
func contextualReferencedProducers(consumerPath, body string) []string {
	if strings.TrimSpace(consumerPath) == "" || strings.TrimSpace(body) == "" {
		return nil
	}
	code := contextualCodeOnly(body)
	directory := filepath.Dir(consumerPath)
	neighborhood := contextualConsumerNeighborhood(consumerPath)
	found := map[string]bool{}
	var out []string
	add := func(candidate string) {
		if strings.TrimSpace(candidate) == "" {
			return
		}
		path := candidate
		if !filepath.IsAbs(path) {
			path = filepath.Join(directory, path)
		}
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil || canonical == consumerPath {
			return
		}
		// AUTHORIZED SOURCE SCOPE, DECIDED BEFORE ANY READ. A path outside the
		// neighborhood the consumer's repository lives in is refused here, so the
		// framework never opens an arbitrary named file and never mints an owner
		// from a directory the conversation was not standing in.
		if !contextualPathWithin(canonical, neighborhood) {
			return
		}
		info, err := os.Stat(canonical)
		if err != nil || !info.Mode().IsRegular() {
			return
		}
		if !found[canonical] {
			found[canonical] = true
			out = append(out, canonical)
		}
	}
	// THE CODE-LEVEL CHAIN IS THE PRIMARY REFERENCE. It is read first and only
	// outside a comment or a quoted string, so a printed `Path(...)` example or a
	// debug string is never a reference.
	for _, loc := range contextualPathChain.FindAllStringSubmatchIndex(code, contextualContextLimit) {
		if loc[0] < 0 || loc[1] > len(code) || contextualInsideQuoted(code, loc[0]) {
			continue
		}
		chain := contextualPathChain.FindStringSubmatch(code[loc[0]:loc[1]])
		if chain == nil {
			continue
		}
		resolved := consumerPath
		for i := 0; i < strings.Count(chain[1], ".parent"); i++ {
			resolved = filepath.Dir(resolved)
		}
		for _, part := range contextualPathPart.FindAllStringSubmatch(chain[2], contextualContextLimit) {
			resolved = filepath.Join(resolved, part[1])
		}
		add(resolved)
	}
	// THEN QUOTED LITERALS, AND ONLY WHERE THE SOURCE ACTUALLY CONSUMES THE FILE.
	for _, loc := range contextualPathLiteral.FindAllStringSubmatchIndex(code, contextualContextLimit) {
		if loc[2] < 0 || loc[3] > len(code) {
			continue
		}
		if !contextualLiteralConsumed(code[:loc[0]]) {
			continue
		}
		add(code[loc[2]:loc[3]])
	}
	return out
}

// contextualNamesProducer answers whether the current consumer source still
// CONSUMES the exact producer path, for the post-turn recheck.
func contextualNamesProducer(consumerPath, body, producer string) bool {
	for _, candidate := range contextualReferencedProducers(consumerPath, body) {
		if candidate == producer {
			return true
		}
	}
	return false
}

func contextualReadFile(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("no source path")
	}
	f, err := contextualOpenRead(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !before.Mode().IsRegular() {
		return "", fmt.Errorf("source is not a regular file")
	}
	if before.Size() > contextualFileBytes {
		return "", fmt.Errorf("source exceeds contextual read bound")
	}
	body, err := io.ReadAll(io.LimitReader(f, contextualFileBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > contextualFileBytes {
		return "", fmt.Errorf("source exceeds contextual read bound")
	}
	// THE SAME FILE, BEFORE AND AFTER. os.Stat does not open, so a FIFO here is
	// answered without blocking; the identity comparison catches a swap.
	after, err := os.Stat(path)
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return "", fmt.Errorf("source changed while being read")
	}
	return string(body), nil
}

func contextualPathOwner(path string) string {
	directory := filepath.Dir(path)
	// A previously read file grants an observed link to its actual repository,
	// not to every workspace sharing one basename or a vocabulary word.
	if root, ok := repositoryRoot(directory); ok {
		directory = root
	}
	key, err := gitidentity.ProjectKey(directory)
	if err != nil || key == "" {
		return ""
	}
	return store.OwnerProject(key)
}

// contextualProducerReadAllowed asks the EXISTING consent policy whether this
// agent may read one canonical producer path, BEFORE the framework opens it. It
// reuses the pure [Agent.decide] gate (the same [approval.Policy] the read tool
// itself is judged by) rather than inventing a permission of its own: only an
// explicit allow admits the read, and an ask or a deny is a SILENT refusal -- no
// prompt is opened and no model is asked during this maintenance pass. A build
// with no policy at all is the configured-nothing case the consent engine's own
// law names allow. Because the live gate is read here, a revocation that lands
// after a worker was admitted takes effect on its next observation.
func (a *Agent) contextualProducerReadAllowed(producer string) bool {
	args, err := json.Marshal(map[string]string{"path": producer})
	if err != nil {
		return false
	}
	call := ai.ToolCall{ID: "framework-verify-read", Type: "function"}
	call.Function.Name = "read"
	call.Function.Arguments = string(args)
	decision, governed := a.decide(call)
	if !governed {
		return true
	}
	return decision.Action == approval.ActionAllow
}

// contextualProducerReadAllowedUnder judges a framework producer re-read by the
// INTERSECTION of two policies: the frozen ADMISSION CEILING a delegated origin
// was stamped with, and the LIVE policy now in force. A read runs only when
// BOTH plainly allow it, which is the whole law this seam exists to keep:
//
//   - an OLD admission that DENIED the producer never widens merely because the
//     root anchored a new repository and gained a blanket allow -- the ceiling
//     still says no;
//   - a later DENY still revokes an edge an older admission ALLOWED, because the
//     live half says no;
//   - an admission both halves allow is unchanged and permitted.
//
// A nil ceiling is the configured-nothing admission, which the consent engine's
// own law names allow, so it constrains nothing and the live policy decides. The
// ceiling is judged by the pure [approval.Policy.Check] on the same `read` call
// the live gate uses, so its flats and read-only allowance answer exactly as
// they did at admission -- no new grant, prompt or registry is introduced.
func (a *Agent) contextualProducerReadAllowedUnder(producer string, ceiling *approval.Policy) bool {
	if !a.contextualProducerReadAllowed(producer) {
		return false
	}
	if ceiling == nil {
		return true
	}
	args, err := json.Marshal(map[string]string{"path": producer})
	if err != nil {
		return false
	}
	return ceiling.Check("read", json.RawMessage(args)).Action == approval.ActionAllow
}

func (a *Agent) observeContextualDependencies(source memoryTurnEvidence) {
	if !a.remembers() {
		return
	}
	// THE ROOT'S OWN BOUNDARY READS JOIN THE ORIGIN'S. A turn handed to a quick
	// task still made its earlier reads itself, at the real executeTool
	// boundary; those receipts are merged in here, under the frozen turn both
	// sides share, before the condenser's stub can stand in for them.
	source.Receipts = a.contextualTurnReceipts(source)
	reads := contextualForwardedReads(source.Receipts)
	byPath := map[string]memoryToolReceipt{}
	for _, r := range reads {
		if _, known := byPath[r.Path]; !known {
			byPath[r.Path] = r
		}
	}
	written := 0
	for _, consumer := range reads {
		consumerOwner := contextualPathOwner(consumer.Path)
		if consumerOwner == "" {
			continue
		}
		if a.contextualDropping(consumer.Path) {
			continue
		}
		for _, producer := range contextualReferencedProducers(consumer.Path, consumer.Body) {
			if producer == consumer.Path || a.contextualDropping(producer) {
				continue
			}
			// THE NEIGHBOURHOOD DISCOVERS; THE CONSENT POLICY AUTHORIZES. A path
			// neither the LIVE policy nor the origin's FROZEN ADMISSION CEILING
			// plainly allows is refused here, before its owner is minted and
			// before any byte is read, hashed or journaled -- so a genuine-looking
			// consume of a private sibling repository the policy denies proves
			// nothing, and a later blanket allow cannot widen an old admission.
			if !a.contextualProducerReadAllowedUnder(producer, source.Ceiling) {
				continue
			}
			producerOwner := contextualPathOwner(producer)
			if producerOwner == "" || producerOwner == consumerOwner {
				continue
			}
			// THE PRODUCER SIDE IS EITHER A REAL TOOL READ OR A FRAMEWORK
			// RECHECK, AND THE RECORD SAYS WHICH. When the agent actually read
			// the producer, its own receipt (and hash) is used. When it did not
			// — the ordinary case where the consumer's source genuinely
			// CONSUMES the producer and the agent only read the consumer — the
			// framework reads the exact referenced file under the same bound a
			// tool read uses and records THAT, labelled `framework-verify:`, so a
			// reader can never mistake it for a `cat` the agent ran. No shell exit
			// code, no substring of some command's output and no quoted mention is
			// ever proof here.
			var producerHash, producerReceipt string
			if held, ok := byPath[producer]; ok {
				producerHash, producerReceipt = held.Hash, held.ID
			} else {
				body, err := contextualReadFile(producer)
				if err != nil || body == "" {
					continue
				}
				producerHash = contextualHash(body)
				producerReceipt = "framework-verify:" + producerHash
			}
			// Work can change the consumer after its first read. Recheck its
			// exact reference after the turn, so an unrelated formatting edit
			// does not leave the link attached to an obsolete whole-file
			// snapshot.
			consumerHash, consumerReceipt := consumer.Hash, consumer.ID
			current, readErr := contextualReadFile(consumer.Path)
			if readErr != nil || !contextualNamesProducer(consumer.Path, current, producer) {
				continue
			}
			if hash := contextualHash(current); hash != consumer.Hash {
				consumerHash = hash
				consumerReceipt = "contextual-recheck:" + hash
			}
			relative, err := filepath.Rel(filepath.Dir(consumer.Path), producer)
			if err != nil {
				continue
			}
			d := store.ContextualDependencyObservation{ID: "", ProducerOwner: producerOwner, ConsumerOwner: consumerOwner, EntityID: producer, ProducerPath: producer, ConsumerPath: consumer.Path, ProducerHash: producerHash, ConsumerHash: consumerHash, ReceiptIDs: []string{consumerReceipt, producerReceipt}, Assumption: "Consumer source consumes the exact producer path " + relative}
			if _, err := a.memory.store.ObserveContextualDependency([]string{producerOwner, consumerOwner}, d); err != nil {
				a.journalMemoryFailure("dependency", err)
			}
			written++
			if written >= contextualContextLimit {
				return
			}
		}
	}
}

// formatContextualImpact renders ONE dependency impact as a single physical
// record. The producer and consumer paths and the recorded assumption are
// untrusted filesystem text, so they are quoted with Go's own escaping: an
// embedded newline or a literal </contextual_impacts> becomes inert text and can
// never split the record or forge a block boundary, which is what lets
// [trimRenderedWholeRecords] treat one line as one whole record for this block
// exactly as it does for the %q-quoted outcomes block.
func formatContextualImpact(d store.ContextualDependencyObservation, notice store.ContextualImpactNotice) string {
	return fmt.Sprintf("- %q changed since %q was observed consuming it. Consumer source was re-read and its recorded assumption is unchanged: %q. Inspect that assumption before asserting a break; offer the relevant follow-up, without editing another project. notice=%q", d.ProducerPath, d.ConsumerPath, d.Assumption, notice.EvidenceHash)
}

// contextualImpactContext re-reads the consumer assumption before suggesting a
// consequence. It never schedules work or edits a consumer in another project.
func (a *Agent) contextualImpactContext(cue string) string {
	if !a.remembers() {
		return ""
	}
	a.mu.Lock()
	turn := a.turnSeq
	a.mu.Unlock()
	a.memory.mu.Lock()
	if a.memory.impactTurn == turn && a.memory.impactPrepared {
		block := a.memory.impactBlock
		a.memory.mu.Unlock()
		return block
	}
	a.memory.impactTurn = turn
	a.memory.impactPrepared = true
	a.memory.impactCue = cue
	a.memory.impactBlock = ""
	a.memory.mu.Unlock()
	// THE EVIDENCE DECIDES, NOT A KEYWORD LIST. A hardcoded vocabulary was the
	// old gate, and a person asking to "make the amount optional" — words none
	// of it contained — would not hear about a consequence their own edit just
	// produced. What runs now is the actual observed state: an edge whose
	// producer source really changed and whose consumer assumption still holds.
	// A turn that changed nothing reads no different files and says nothing, so
	// ordinary and irrelevant requests stay quiet without a word list.
	if a.config.MemoryProjectKey == "" {
		return ""
	}
	owner := store.OwnerProject(a.config.MemoryProjectKey)
	links, err := a.memory.store.DependenciesForProducer(owner, contextualContextLimit)
	if err != nil {
		return ""
	}
	var lines []string
	for _, d := range links {
		producer, err := contextualReadFile(d.ProducerPath)
		if err != nil {
			continue
		}
		hash := contextualHash(producer)
		if hash == d.ProducerHash {
			continue
		}
		consumer, err := contextualReadFile(d.ConsumerPath)
		if err != nil {
			continue
		}
		consumerHash := contextualHash(consumer)
		notices, err := a.memory.store.ContextualImpacts([]string{d.ProducerOwner, d.ConsumerOwner}, owner, d.EntityID, hash, map[string]string{d.ConsumerPath: consumerHash})
		if err != nil {
			continue
		}
		for _, notice := range notices {
			a.memory.mu.Lock()
			if a.memory.impactNotices == nil {
				a.memory.impactNotices = map[string]store.ContextualImpactNotice{}
			}
			_, said := a.memory.impactNotices[notice.EvidenceHash]
			if !said {
				// THE HELD SET IS BOUNDED AND EVICTABLE. A session that has
				// already offered its eight notices must still be able to offer
				// genuinely new material: the oldest offer is dropped rather
				// than permanently blocking the ninth. Notice identity is the
				// content hash, so the same change never repeats; a later change
				// is a different notice and can surface.
				for len(a.memory.impactOrder) >= contextualContextLimit {
					oldest := a.memory.impactOrder[0]
					a.memory.impactOrder = a.memory.impactOrder[1:]
					delete(a.memory.impactNotices, oldest)
				}
				a.memory.impactNotices[notice.EvidenceHash] = notice
				a.memory.impactOrder = append(a.memory.impactOrder, notice.EvidenceHash)
			}
			a.memory.mu.Unlock()
			if said {
				continue
			}
			lines = append(lines, formatContextualImpact(d, notice))
		}
		if len(lines) >= contextualContextLimit {
			break
		}
	}
	if len(lines) == 0 {
		return ""
	}
	block := "\n<contextual_impacts>\nMention only a useful supported consequence for the current work; batch related consequences. File change alone does not prove breakage.\n" + strings.Join(lines, "\n") + "\n</contextual_impacts>\n"
	a.memory.mu.Lock()
	a.memory.impactBlock = block
	a.memory.mu.Unlock()
	return block
}

func (a *Agent) dismissContextualNotices(user string) {
	if !a.remembers() {
		return
	}
	lower := strings.ToLower(user)
	// A NEGATED DISMISSAL IS NOT A DISMISSAL. "do not dismiss that" and its
	// friends carry the dismiss word and mean the opposite; reading them as the
	// instruction would throw away the very notice the person asked to keep.
	if !contextualDismissCue(lower) || contextualDismissNegated(lower) {
		return
	}
	a.memory.mu.Lock()
	notices := make([]store.ContextualImpactNotice, 0, len(a.memory.impactNotices))
	for _, n := range a.memory.impactNotices {
		notices = append(notices, n)
	}
	a.memory.mu.Unlock()
	if len(notices) == 0 {
		return
	}
	// DISMISS WHAT THE PERSON NAMED, OR AN UNAMBIGUOUS SINGLETON/BATCH. A bare
	// "dismiss" with several notices held names nothing, so it disappears
	// nothing rather than silencing the whole history on a substring.
	targets := make([]store.ContextualImpactNotice, 0, len(notices))
	for _, n := range notices {
		if contextualNoticeNamed(lower, n) {
			targets = append(targets, n)
		}
	}
	if len(targets) == 0 && (len(notices) == 1 || contextualDismissBatch(lower)) {
		targets = notices
	}
	if len(targets) == 0 {
		return
	}
	for _, n := range targets {
		if err := a.memory.store.DismissContextualImpact([]string{n.Dependency.ProducerOwner, n.Dependency.ConsumerOwner}, n); err != nil {
			a.journalMemoryFailure("impact-dismissal", err)
		}
	}
	a.memory.mu.Lock()
	for _, n := range targets {
		delete(a.memory.impactNotices, n.EvidenceHash)
	}
	kept := a.memory.impactOrder[:0]
	for _, hash := range a.memory.impactOrder {
		if _, held := a.memory.impactNotices[hash]; held {
			kept = append(kept, hash)
		}
	}
	a.memory.impactOrder = kept
	a.memory.mu.Unlock()
}

// contextualDismissCue answers whether the person is asking for a notice to be
// dropped at all.
func contextualDismissCue(lower string) bool {
	for _, cue := range []string{"dismiss", "don't bring that up", "do not bring that up", "don't bring it up", "do not bring it up", "stop bringing that up"} {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

// contextualDismissNegated answers whether that request is negated. It is
// checked before any notice is touched.
func contextualDismissNegated(lower string) bool {
	for _, neg := range []string{"don't dismiss", "do not dismiss", "not dismiss", "never dismiss", "don't drop", "do not drop"} {
		if strings.Contains(lower, neg) {
			return true
		}
	}
	return false
}

// contextualNoticeNamed answers whether the person named this notice's producer
// or consumer, by path or by file name.
func contextualNoticeNamed(lower string, n store.ContextualImpactNotice) bool {
	for _, path := range []string{n.Dependency.ProducerPath, n.Dependency.ConsumerPath} {
		path = strings.ToLower(strings.TrimSpace(path))
		if path == "" {
			continue
		}
		if strings.Contains(lower, path) || strings.Contains(lower, strings.ToLower(filepath.Base(path))) {
			return true
		}
	}
	return false
}

// contextualDismissBatch answers whether an explicit plural or batch reference
// makes dismissing every held notice unambiguous.
func contextualDismissBatch(lower string) bool {
	for _, word := range []string{"those", "them", "both", "all", "batch", "notices"} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// The small static resolver accepts direct paths and a literal pathlib chain.
// Dynamic module discovery is left to a later observed receipt, never guessed
// from matching names. Every accepted chain resolves to the complete producer.
var contextualPathChain = regexp.MustCompile(`(?:pathlib\.)?Path\(__file__\)\.resolve\(\)((?:\.parent)+)((?:\s*/\s*["'][^"'\n]+["'])+)`)
var contextualPathPart = regexp.MustCompile(`["']([^"']+)["']`)

func contextualResolvedReference(consumerPath, body, producerPath, relative string) bool {
	if strings.Contains(body, "\""+producerPath+"\"") || strings.Contains(body, "'"+producerPath+"'") {
		return true
	}
	if strings.HasPrefix(relative, ".."+string(filepath.Separator)) && (strings.Contains(body, "\""+relative+"\"") || strings.Contains(body, "'"+relative+"'")) {
		return true
	}
	for _, chain := range contextualPathChain.FindAllStringSubmatch(body, contextualContextLimit) {
		resolved := consumerPath
		for i := 0; i < strings.Count(chain[1], ".parent"); i++ {
			resolved = filepath.Dir(resolved)
		}
		for _, part := range contextualPathPart.FindAllStringSubmatch(chain[2], contextualContextLimit) {
			resolved = filepath.Join(resolved, part[1])
		}
		canonical, err := filepath.EvalSymlinks(resolved)
		if err == nil && canonical == producerPath {
			return true
		}
	}
	return false
}

// A successful action may change a known producer during this very turn. The
// next request receives the consequence before its reply, not a turn later.
func (a *Agent) refreshContextualImpactsAfterAction(result toolResult, tool string) {
	if !a.remembers() || result.isError || result.harness || tool == "read" {
		return
	}
	a.memory.mu.Lock()
	cue := a.memory.impactCue
	a.memory.impactPrepared = false
	a.memory.mu.Unlock()
	block := a.contextualImpactContext(cue)
	if block == "" {
		return
	}
	a.mu.Lock()
	if !strings.Contains(a.memoryText, block) {
		// The head earlier requests already carried stays whole; the fresh
		// consequence is the optional tail, trimmed by WHOLE records to what is
		// left of the one shared ceiling and omitted whole when nothing fits, so
		// no record and no instruction is ever clipped mid-sentence.
		a.memoryText = composeBeforeRequestContext(a.memoryText, block, "", "")
		a.landVolatileLocked()
	}
	a.mu.Unlock()
}

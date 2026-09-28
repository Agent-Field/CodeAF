package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/store"
)

// contractPrompt writes the working method one agent will follow.
//
// This is the dynamic half of the executor. The brief says what the job is;
// the contract says how work of this kind is done well — the discipline a
// specialised harness would have baked into its system prompt, generated per
// task instead of hand-written per domain. pi is a coding harness because its
// prompt carries a coding method; a generic loop handed a generated method for
// the leaf in front of it gets the same advantage on any kind of work, and
// the harness itself never changes.
//
// It is a structuring call: it runs with the planner's economy (reasoning
// off), one call per leaf, all leaves in parallel, so the layer costs one
// call's latency however wide the graph is.
//
// The toolbox sentence is load-bearing and used to be wrong. It named four
// tools — shell, write, edit, web — for a worker that also holds background
// jobs, recall of everything folded away, a line to its siblings, and, on ask,
// tools that read documents and generate images, music, video and speech. The
// one prompt whose entire job is to say how this kind of work is done well
// could therefore not route a method through any of them, so a job that needed
// a PDF read or a chart drawn got a method that worked around the capability
// sitting unused. It is a sentence, not a manual: the method writer needs to
// know the routes exist, and the tool descriptions themselves say how to drive
// them.
//
// THE FIRST BULLET USED TO MANUFACTURE ORIENTATION AND IS THE MEASURED REASON
// THIS COMMENT IS LONGER THAN IT WAS. It required every model-authored method to
// state "what to understand before touching anything", and the method is
// rendered FIRST in every leaf's brief (exec.Linear.brief) — so every leaf in
// every graph opened on an instruction to go and understand something, and did.
// The cost was per-leaf, unauditable, and invisible in any single trace: it does
// not read as waste, it reads as diligence. What a method is for is how the work
// is PRODUCED and how it is CHECKED, and those are the two things the bullets
// now ask for.
//
// The reading half is not deleted, it is moved to where the knowledge is. Some
// jobs genuinely cannot begin until a file has been read; the planner is the one
// party that knows which, because it holds the terrain and writes "It is
// expected to touch: …" into this very message. So the method may name a
// specific thing to read and may not ask for understanding in the abstract —
// which is the difference between a pointer and a scavenger hunt.
//
// The verify bullet binds to the brief for the same reason. Done-means has two
// authors — the instruction states the acceptance bar, the method states how to
// check — and the second was never told the first was binding. One task's brief
// asked for a flicker to stop and its contract verified the structure of the
// page instead; a sibling task with the same shape got it right, which is what a
// coin flip looks like. The clause costs a line and removes the flip: the bar is
// stated once, in the instruction, and the method exercises that one.
const contractPrompt = `You write the working method for one agent about to do one job.

` + workerPremise + `

Its toolbox is a shell that also runs work in the background, file writing and
editing, web search and page fetching, recall of work already folded away, and
one line it can pass to the other agents on this job when there are any; where
the machine is configured for them it can ask for tools that read documents and
generate images, music, video and speech.

Do not restate the job — the agent already has its instruction. Write the
method: what someone experienced in exactly this kind of work does differently
from someone merely competent.

Concretely, for this kind of work:
- What order the work is best produced in. Where this job truly cannot begin
  until something has been read, name that thing — the file, the source, the
  record it is expected to touch — and say what to take from it. Never ask for
  understanding in general: an agent told to understand the material before it
  touches anything goes and looks around, and only you know whether there is
  anything here to look at.
- What "done" means here, said as what whoever ends up using the result does
  with it and sees — including how it must connect to or be reachable from what
  already exists, if anything does. Work that functions only in isolation is
  unfinished.
- How to verify: the whole path exercised the way that user reaches it, run
  before anything may be called verified, since parts checked separately never
  add up to a working result — and, for whatever cannot be run from here, what
  to declare unverified and the one short check that would settle it. Where the
  agent's instruction already states the bar for done, the check you write
  exercises that bar itself rather than a stand-in for it. Write it as one check,
  run once, immediately before finishing: a check that came back green is a
  finished question, and a method that sends the agent back to ask it again
  spends the person's money to re-learn a fact.
- The two or three mistakes most often made in this kind of work, stated as
  things to watch for.
- Where this kind of work most often gets stuck — the source that is down, the
  tool that refuses, the case that resists — and the route an experienced hand
  takes around it: the substitute source, method, or construction that reaches
  the same end. The agent cannot ask anyone when it hits that wall; this line
  is what carries it through.

Every line must be specific to this kind of work — advice that would fit any
job ("plan first", "be thorough") is filler and wastes the agent's attention.

The method may demand evidence only in the shape the request asked for it. A
request to confirm, check, or verify something is satisfied by the fact of the
result — what was run and what came back, in the agent's own words — and the
method never escalates that into reporting the raw output, the full transcript,
or the verbatim text of anything the request did not ask to see. The method is
held against the deliverable by a reviewer later: every demand you write
becomes a requirement the person never made, so write none they did not.

Write as direct instruction to the agent. 120-200 words, plain prose or short
dashes, no headings, no preamble, no mention of "the plan" or "this task".

Answer with one bare JSON object and nothing else — {"contract": "<the method>"}
— with no code fence around it and no sentence before or after it.`

var contractSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "contract": { "type": "string" }
  },
  "required": ["contract"],
  "additionalProperties": false
}`)

// ContractPlaybook returns the earned method bullets relevant to one leaf.
// Nil and empty results preserve the fixed contract prompt byte for byte.
type ContractPlaybook func(Node) string

// Contracts writes a working method for every leaf that lacks one, all leaves
// at once. Like Briefs, it is the batch form used at run time; the wall-clock
// cost is one call however many leaves there are.
func Contracts(ctx context.Context, client Completer, graph *Graph, playbook ContractPlaybook, callbacks ...Progress) (Usage, error) {
	shared := graph.context()
	var progress Progress
	if len(callbacks) > 0 {
		progress = serialProgress(callbacks[0])
	}

	type result struct {
		id       int
		contract string
		usage    *ai.Usage
		err      error
	}
	var group sync.WaitGroup
	var mutex sync.Mutex
	var results []result
	var targets []Node

	// The deliverable owner is written for like any other leaf. It used to be
	// skipped for not being KindWork, which left the node the delivery gate
	// judges holding a two-line harness stub where every other node held a
	// method — and the gate's whole question is whether the finished whole is
	// what was asked for, judged against exactly that stub.
	sink := graph.deliverableSink()
	// Read off the graph here rather than inside the goroutines: it is one fact
	// about the goal, and the discipline in this loop is that nothing concurrent
	// touches the graph at all. The record roster is read at the same moment and
	// for the same reason.
	fileShaped := graph.FileShaped
	continues, records := graph.Continues, append([]string(nil), graph.Records...)
	for _, id := range graph.writtenLeaves() {
		node := graph.Node(id)
		if node == nil || strings.TrimSpace(node.Contract) != "" {
			continue
		}
		targets = append(targets, *node)
	}
	total := len(graph.writtenLeaves())
	base := total - len(targets)
	if progress != nil {
		emitProgress(progress, "contracts", fmt.Sprintf("%d/%d", base, total), "")
	}
	completed := 0
	for _, node := range targets {
		group.Add(1)
		go func(node Node) {
			defer group.Done()
			// The results slice is appended to, so a faulting node has to add
			// its own failed entry or it would simply vanish from the pass.
			// A contract that fails degrades to the generic loop; so does this.
			defer func() {
				if recovered := recover(); recovered != nil {
					fault := guard.Note(fmt.Sprintf("plan/contract node %d", node.ID), recovered)
					mutex.Lock()
					defer mutex.Unlock()
					results = append(results, result{id: node.ID, err: fault})
				}
			}()
			notes := ""
			if playbook != nil {
				notes = playbook(node)
			}
			contract, usage, err := writeContract(ctx, client, shared, node, notes, node.ID == sink, fileShaped, continues, records)
			mutex.Lock()
			defer mutex.Unlock()
			results = append(results, result{id: node.ID, contract: contract, usage: usage, err: err})
			completed++
			if progress != nil {
				latest := ""
				if err == nil {
					latest = nodeProgressTitle(node)
				}
				emitProgress(progress, "contracts", fmt.Sprintf("%d/%d", base+completed, total), latest)
			}
		}(node)
	}
	group.Wait()

	var usage Usage
	var failures []error
	for _, item := range results {
		usage.Add(item.usage)
		if item.err != nil {
			// A missing contract degrades to the generic loop rather than
			// failing the run: the contract is an edge, not a load-bearing wall.
			failures = append(failures, item.err)
			continue
		}
		if node := graph.Node(item.id); node != nil {
			// Dual-write, as with the brief: Contract stays the read, and the
			// spec's Method is the same string in the object that survives
			// re-targeting. The prompt above is unchanged — the method is not a
			// new thing, it is the same thing with somewhere durable to live.
			node.Contract = item.contract
			node.Spec.Method = item.contract
		}
	}
	return usage, joinErrors(failures)
}

// contractDeliverableLine tells the method writer which of the two jobs it is
// writing for. Every other leaf produces material; this one produces the thing
// itself, and its method has to be about the finished whole from the seat of the
// person who asked — what they open, what they read, what would make them say it
// is not done. Without the line the sink reads as a filing step and gets a
// filing method.
//
// The second sentence is the same lesson one level further in. A method that
// knows this job is the deliverable can still describe a way of *assembling*
// one — read the inputs, reconcile them, record the result — and a merge run to
// that method ends with an account of the merging. So the line says what the
// method must ask for in the end: the thing written out, here, in the agent's
// own last message.
const contractDeliverableLine = "This job IS the deliverable: every other result arrives here as material, and what " +
	"this agent produces is the whole of what the person who asked will read. The method must therefore end by " +
	"telling the agent to write that whole thing out in its own final message — the merged result, the figures, " +
	"the verdict, in full — and never to file it somewhere and name the place, describe how the material was " +
	"combined, or report that the assembly is finished.\n"

// contractDeliverableFileLine is the same job under the delivery law's carve-out
// (delivery.go): the ask named the file, so the method that ends by forbidding
// the file is the one that fails the run. The framing sentence is identical
// because the job is identical — what changes is only where the finished thing
// has to land, which is why that half comes from the shared constant rather than
// being said again here in slightly different words.
const contractDeliverableFileLine = "This job IS the deliverable: every other result arrives here as material, and what " +
	"this agent produces is the whole of what the person who asked will read. The method must therefore end by " +
	"holding the agent to this:\n\n" + DeliverToNamedFile + "\n"

// contractDeliverableLineFor picks the half of the law the ask calls for. False
// is what a caller that has not made the judgment passes, and it renders exactly
// the bytes this pass has always rendered.
func contractDeliverableLineFor(fileShaped bool) string {
	if fileShaped {
		return contractDeliverableFileLine
	}
	return contractDeliverableLine
}

// recordBlock tells the method writer what the agent will actually be handed of
// the work already done, and it is a statement of fact rather than an
// instruction about what to do with it.
//
// It exists because of a false statement that was shipped to a person. A leaf
// was asked to explain what had been wrong with some code; the pass that wrote
// its method had been handed nothing about the finished work but a list of file
// PATHS; with no way to say where the answer could come from, the method it
// wrote told the agent to infer the cause from the fact that the tests now
// passed, and offered an example of what such a cause might be. The agent
// shipped the example verbatim as the real root cause. Every layer behaved
// exactly as designed: the method writer had nothing, so it improvised, and the
// gate that should have caught it held file names and no content.
//
// So the mechanism is what is HANDED IN, not a rule about what to write. With a
// record, the writer knows there is somewhere for the facts to come from and can
// route the method through it. Without one, the writer knows there is not — and
// the sentence a method needs in that case is the one that permits an agent to
// say the record does not name something, which a writer who believes the agent
// can find out anyway will never think to write.
func recordBlock(continues bool, records []string) string {
	// A plan that continues nothing renders nothing at all, and the prompt is
	// what it has always been: there is no earlier work for a record to be of,
	// so both halves below would be answering a question this job never raised.
	if !continues {
		return ""
	}
	if len(records) == 0 {
		return "\nNo record of the earlier work is handed to this agent: it will have its " +
			"instruction, its inputs, and whatever it can run or read for itself, and nothing else. " +
			"Where done means stating a fact about work that has already happened, the method must " +
			"say where that fact is to come from, and must have the agent say plainly that the record " +
			"does not name it when it is not there to be found.\n"
	}
	return "\nThe agent will be handed these records of the work already done, as readable files it " +
		"can open with its shell:\n" + strings.Join(records, "\n") +
		"\nAny fact the job needs about what was done, changed, or found is in them, and the method " +
		"should say to read them for it rather than to reason it out.\n"
}

func writeContract(ctx context.Context, client Completer, shared string, node Node, playbook string, deliverable, fileShaped, continues bool, records []string) (string, *ai.Usage, error) {
	var target strings.Builder
	// A node that came out of a plan always has a title; the one-leaf job does
	// not, because there was nothing to distinguish it from. Naming the job
	// twice, or naming it as an empty handle, both spend the model's attention
	// on nothing.
	if title := strings.TrimSpace(node.Title); title != "" {
		fmt.Fprintf(&target, "The job: %s — %s\n", title, node.Summary)
	} else {
		fmt.Fprintf(&target, "The job: %s\n", node.Summary)
	}
	if len(node.Sources) > 0 {
		fmt.Fprintf(&target, "It is expected to touch: %s\n", strings.Join(node.Sources, "; "))
	}
	if brief := strings.TrimSpace(node.Brief); brief != "" {
		fmt.Fprintf(&target, "The instruction the agent will receive:\n%s\n", brief)
	}
	if deliverable {
		target.WriteString(contractDeliverableLineFor(fileShaped))
	}
	// What this agent will be able to KNOW, stated before the method is written
	// rather than discovered by an agent that has already promised an answer.
	// It goes in the per-node message and not in the shared preamble so the
	// system doctrine and the shared context stay byte-identical across the
	// fan-out and N-1 of the calls still land on a warm prefix.
	target.WriteString(recordBlock(continues, records))
	// The earned notes are per-leaf, retrieved for this node's territory, so
	// they belong here and nowhere earlier. Every leaf in a project is written
	// concurrently against the same doctrine and the same shared context; if the
	// notes rode in the system message, the first byte of the very first message
	// would differ per leaf and each of the N calls would write its own prefix
	// cold. Kept at the tail of the only per-node message, the whole 3-message
	// head — system doctrine plus shared context — is byte-identical across the
	// fan-out, and N-1 of the calls land on a warm prefix.
	if playbook = strings.TrimSpace(playbook); playbook != "" {
		target.WriteString("\nEarned method notes for this territory:\n" + playbook + "\n")
	}
	target.WriteString("\nWrite the working method for this kind of job.")

	messages := []ai.Message{
		systemMessage(contractPrompt),
		userMessage(shared),
		userMessage(target.String()),
	}
	ctx = provider.WithCall(ctx, provider.ClassPlanContract)
	// WHICH NODE THIS CALL BELONGS TO. A contract is written for one node, and
	// a fanned-out plan writes N of them at once; without the node the log has
	// N identical rows and no way to say which method belongs to which job.
	ctx = provider.WithCallNode(ctx, callNodeKey(node.ID))
	var decoded struct {
		Contract string `json:"contract"`
	}
	response, err := structured(ctx, client, messages, contractSchema, &decoded)
	if err != nil {
		return "", usageOf(response), fmt.Errorf("contract %q: %w", node.Title, err)
	}
	contract := trim(decoded.Contract)
	if contract == "" {
		provider.Report(ctx, provider.ReadingSemanticFailure)
		return "", usageOf(response), annotate(fmt.Errorf("contract %q: empty response", node.Title), response)
	}
	// The schema held and the field is not empty; whether the method it
	// describes is a good one is not checkable without running the leaf.
	provider.Report(ctx, provider.ReadingUnverifiedSuccess)
	return contract, usageOf(response), nil
}

// ComposeSkills builds an ordered list of skill names from pinned names and
// retrieval candidates, preserving input order. Pinned names come first (in
// the order given); non-pinned candidates follow in theirs. Duplicates are
// collapsed to the first occurrence, so a pinned entry always wins over a
// retrieved one of the same name.
func ComposeSkills(pinned, candidates []string) []string {
	seen := make(map[string]bool, len(pinned)+len(candidates))
	result := make([]string, 0, len(pinned)+len(candidates))
	for _, name := range pinned {
		if name != "" && !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	for _, name := range candidates {
		if name != "" && !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	return result
}

// SkillEntry is one attached skill rendered in a worker's instruction block.
// Name is the skill's shelf name; Doc is the one-line description from its
// fact; ShelfPath is the path the worker reaches the skill through — the
// skill's directory for the forge's own executable skills, and the SKILL.md
// body file itself for an agentskills folder. SkillEntryFromFact is the one
// construction path that decides which, so the convention is applied there
// and nowhere else in the render.
type SkillEntry struct {
	Name      string
	Doc       string
	ShelfPath string

	// BodyInPath says ShelfPath is the skill's readable body — the SKILL.md of
	// an agentskills folder — rather than a directory holding something to
	// run. The render says so in the line itself, because a worker handed a
	// bare path cannot tell a file it should read from a directory it should
	// run things out of, and `read` refuses a directory outright.
	BodyInPath bool
}

// SkillEntryFromFact builds the one entry the brief pass attaches for a shelf
// fact — the single construction path, so the agentskills convention is
// applied here or nowhere. A fact whose artifact directory holds a top-level
// SKILL.md is an agentskills folder: its content is that FILE, and the
// directory itself is what `read` refuses, so the entry carries the SKILL.md
// path and the render marks it as the body. Every other fact keeps the exact
// entry this pass has always built — the artifact directory itself, whatever
// the fact's trust tier says, because the convention keys on the folder and
// never on how the skill arrived.
func SkillEntryFromFact(fact store.Fact) SkillEntry {
	entry := SkillEntry{Name: fact.SkillName(), Doc: fact.Body, ShelfPath: fact.Artifact}
	if body, ok := store.SkillBodyFile(fact.Artifact); ok {
		entry.ShelfPath = body
		entry.BodyInPath = true
	}
	return entry
}

// RenderSkillsBlock renders attached skills as doc lines and shelf paths.
// Each skill produces one line: "- <doc> [<path>]" when both exist, or a
// shorter form when only one is available; an agentskills folder's line adds
// "— body in this file" inside the brackets so a worker told to read the
// path knows it is holding the skill itself. Zero entries returns zero
// bytes — no header, no placeholder, no blank line. The final line states
// that earlier-listed skills take precedence in case of conflict.
//
// This renders beside the composition above because the two are one path: the
// brief pass composes the attachment and the executor renders it into the
// instruction, and the executor cannot reach a package that itself imports the
// subharness. A render the worker prompt cannot call is a render that never
// runs.
func RenderSkillsBlock(skills []SkillEntry) string {
	if len(skills) == 0 {
		return ""
	}
	var buf strings.Builder
	for _, s := range skills {
		buf.WriteString("- ")
		if s.Doc != "" {
			buf.WriteString(s.Doc)
			if s.ShelfPath != "" {
				buf.WriteString(" [")
				buf.WriteString(s.ShelfPath)
				// Only an agentskills folder's entry carries this, and its path
				// IS the skill's body — the one line a worker reads instead of a
				// directory it runs things out of.
				if s.BodyInPath {
					buf.WriteString(" — body in this file")
				}
				buf.WriteString("]")
			}
		} else if s.ShelfPath != "" {
			buf.WriteString(s.ShelfPath)
		}
		buf.WriteString("\n")
	}
	buf.WriteString("Earlier-listed skills win when two skills conflict.")
	return buf.String()
}

// retrieveSkillCap bounds how many retrieved candidates one leaf may carry.
// Pinned skills are the person's own naming and are never capped; the fuzzy
// half is, so a runaway shelf cannot bury a leaf's instruction in recipes.
const retrieveSkillCap = 3

// attachmentStopwords are the function words that share with every instruction
// there is — "the" with all of them, "and" with almost as many. They are
// dropped from the cue side so a shelf doc saying "the" once cannot claim
// relevance to every leaf; a body word can only score against a cue the
// territory actually names.
var attachmentStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "that": true,
	"this": true, "from": true, "into": true, "your": true, "are": true,
	"was": true, "were": true, "has": true, "have": true, "will": true,
	"them": true, "they": true, "their": true, "there": true, "then": true,
	"than": true, "when": true, "what": true, "where": true, "which": true,
	"out": true, "off": true, "too": true, "also": true, "both": true,
	"about": true, "after": true, "before": true, "while": true,
	"through": true, "without": true, "within": true, "each": true,
}

// PinnedSkills returns the skills the person's own words name outright: every
// shelf skill whose name appears in the text. It is simple name-in-text
// matching — deterministic, no model call — because a person naming a skill is
// the strongest relevance signal there is, and it is read off the goal, which
// is the person's proposal in whatever words they used.
func PinnedSkills(text string, skills []store.Fact) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	// Tokenize the goal into whole words so a skill named "lint" is never
	// pinned by "splinter" or "test" by "latest".
	tokens := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	words := make(map[string]bool, len(tokens))
	for _, word := range tokens {
		if word != "" {
			words[word] = true
		}
	}
	pinned := make([]string, 0, len(skills))
	for _, fact := range skills {
		name := fact.SkillName()
		if name == "" {
			continue
		}
		lower := strings.ToLower(name)
		if words[lower] {
			pinned = append(pinned, name)
			continue
		}
		// Hyphenated skill names (e.g. "repo-audit") are broken into separate
		// tokens by the alnum splitter. Check whether the name's own alnum
		// token sequence appears as a contiguous subsequence of the goal's
		// tokens, so a literal name pins without matching its fragments
		// individually.
		if strings.ContainsAny(lower, "-_.") {
			parts := alnumParts(lower)
			if len(parts) >= 2 && containsContiguous(tokens, parts) {
				pinned = append(pinned, name)
			}
		}
	}
	return pinned
}

// alnumParts splits s into runs of alphanumeric characters.
func alnumParts(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
}

// containsContiguous reports whether sub appears as a contiguous subsequence
// of all. Both slices are from the same splitter, so elements compare by value.
func containsContiguous(all, sub []string) bool {
	if len(sub) > len(all) {
		return false
	}
	limit := len(all) - len(sub)
outer:
	for i := 0; i <= limit; i++ {
		for j, p := range sub {
			if all[i+j] != p {
				continue outer
			}
		}
		return true
	}
	return false
}

// RetrieveSkills returns the skills retrieval would attach to one leaf: those
// whose scope or doc line cues against the leaf's own territory — its rendered
// instruction, and the workspace it runs in. The shape is the chat catalog's
// window scorer (skillcatalog.go): a scope naming something in front of the
// leaf outweighs anything, a shared doc word is the weaker cue. One shared
// word is coincidence — "the" shares with every instruction there is — so only
// scores a real cue produces come back, most relevant first, capped.
func RetrieveSkills(text, workspace string, skills []store.Fact) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	cues := cueWords(text)
	for word := range cueWords(workspace) {
		cues[word] = true
	}
	type scored struct {
		name  string
		score int
	}
	found := make([]scored, 0, len(skills))
	for _, fact := range skills {
		name := fact.SkillName()
		if name == "" {
			continue
		}
		score := 0
		if scopeWords(fact.Scope, workspace, cues) {
			score += 100
		}
		for _, word := range docWords(fact.Body) {
			if cues[word] {
				score += 5
			}
		}
		if score >= 10 {
			found = append(found, scored{name: name, score: score})
		}
	}
	sort.SliceStable(found, func(first, second int) bool { return found[first].score > found[second].score })
	if len(found) > retrieveSkillCap {
		found = found[:retrieveSkillCap]
	}
	names := make([]string, 0, len(found))
	for _, hit := range found {
		names = append(names, hit.name)
	}
	return names
}

// docWords is the comparable words of one text: lowercase, split on everything
// that is not a letter or a digit, and dropping the short words that match
// everything. It is the chat catalog's own tokenizer (skillcatalog.go), held
// at this spelling because the composition here has to agree with what the
// shelf's one other reader scores with.
func docWords(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	words := fields[:0]
	for _, field := range fields {
		if len(field) >= 3 {
			words = append(words, field)
		}
	}
	return words
}

// cueWords is the cue side of the match: the comparable words of a territory
// after the function words are dropped. A body word can only score against a
// cue the territory actually names, so "the" and friends never make one.
func cueWords(text string) map[string]bool {
	cues := make(map[string]bool)
	for _, word := range docWords(text) {
		if !attachmentStopwords[word] {
			cues[word] = true
		}
	}
	return cues
}

// scopeWords asks whether a skill's scope names something the cue words hold.
// A scope is `kind:value` ("repo:/path", "tool:git", "domain:x"). For a repository
// scope, it matches if the workspace path matches or contains the repo, if the
// workspace base name matches the repo base name, or if the repo base name appears
// in the cue words. Path segments like "users", "home", or "work" do not cause a
// spurious match. Other scopes compare their tokens against cue words.
func scopeWords(scope, workspace string, cues map[string]bool) bool {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return false
	}
	if strings.HasPrefix(strings.ToLower(scope), "repo:") {
		repoPath := strings.TrimSpace(scope[len("repo:"):])
		repoClean := filepath.Clean(repoPath)
		if workspace != "" {
			wsClean := filepath.Clean(workspace)
			if wsClean == repoClean ||
				strings.HasPrefix(wsClean+string(filepath.Separator), repoClean+string(filepath.Separator)) ||
				strings.HasPrefix(repoClean+string(filepath.Separator), wsClean+string(filepath.Separator)) {
				return true
			}
			repoBase := strings.ToLower(filepath.Base(repoClean))
			if repoBase != "." && repoBase != "/" && repoBase != "\\" {
				if strings.EqualFold(filepath.Base(wsClean), repoBase) {
					return true
				}
			}
		}
		repoBase := strings.ToLower(filepath.Base(repoClean))
		if repoBase != "." && repoBase != "/" && repoBase != "\\" && len(repoBase) >= 3 && !attachmentStopwords[repoBase] {
			if cues[repoBase] {
				return true
			}
		}
		return false
	}

	for _, part := range strings.FieldsFunc(strings.ToLower(scope), func(r rune) bool {
		return r == ':' || r == '/' || r == '\\' || r == '.' || r == '-' || r == '_' || r == ' '
	}) {
		if part != "" && !attachmentStopwords[part] && cues[part] {
			return true
		}
	}
	return false
}

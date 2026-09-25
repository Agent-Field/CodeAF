package plandb

// The plandb CLI: one runner, two doors (docs/design/plandb-cli/DESIGN.md).
// cmd/plandb builds bin/plandb, and `codeaf plandb` reaches the same Main as
// the fallback road when the sibling binary is not beside the running binary.
//
// THE MODEL'S VERBS ARE COORDINATION ONLY. Everything a verb renders comes
// from the store's methods — never from a second reading of the state file —
// so the CLI and the runtime cannot disagree about what the plan says. The
// task lifecycle belongs to the runtime, which claims every task it
// dispatches under the task's own id; the lifecycle verbs are refused with
// the supervisor's own sentence, naming the verb, so a model that reaches
// for one learns the rule in one step. `go` and `done --next` are the two
// caller-owned claims (docs/design/plandb-cli/REFERENCE.md): the pulse
// re-claims a task claimed that way under its own id once it becomes a node.
//
// Output has one shape everywhere: human text by default, --json for
// structured answers, ids printed with their t- prefix and accepted with or
// without it, errors as one plain sentence on stderr with exit 1 — the cause,
// and what to do about it.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// cliVersion is what --version prints. It is this CLI's own line, and it
// moves on its own.
const cliVersion = "0.3.0"

// The runner's two streams sit behind variables so a test can drive Main
// and read exactly what a shell would have seen.
var (
	cliOut io.Writer = os.Stdout
	cliErr io.Writer = os.Stderr
)

// cliParsed is one scanned command line: the verb path (one word, or two
// for the task and what-if families), the positionals after it, and the
// flags. Flags may appear anywhere — before the verb, between positionals,
// as --flag value or --flag=value — because the doctrine's own sentences
// lean on that freedom (`plandb add "t" --description d`), which stdlib
// flag cannot parse and must not be forced to.
type cliParsed struct {
	pos   []string
	vals  map[string]string
	lists map[string][]string
	bools map[string]bool
}

// Main runs one plandb command line and answers the process exit code. It
// is the one runner both doors call, and the only place argv is interpreted.
func Main(argv []string) int {
	p, err := cliScan(argv)
	if err != nil {
		return cliFail(err)
	}
	if p.bools["version"] {
		fmt.Fprintln(cliOut, "plandb", cliVersion)
		return 0
	}
	if p.bools["help"] || len(p.pos) == 0 {
		fmt.Fprintln(cliOut, cliVerbHelp(p.verbPath()))
		return 0
	}
	if msg, refused := cliRefusal(p); refused {
		return cliFail(errors.New(msg))
	}
	if p.pos[0] == "init" {
		if err := cliInit(p); err != nil {
			return cliFail(err)
		}
		return 0
	}
	if p.pos[0] == "help" {
		if len(p.pos) >= 2 {
			fmt.Fprintln(cliOut, cliVerbHelp(p.pos[1]))
		} else {
			fmt.Fprintln(cliOut, cliUsage())
		}
		return 0
	}
	st, err := cliStore(p)
	if err != nil {
		return cliFail(err)
	}
	if err := cliDispatch(st, p); err != nil {
		return cliFail(err)
	}
	return 0
}

// cliFail says the cause over stderr and ends the run — every CLI error
// leaves through here, so the shape (`error: <plain sentence>`, exit 1) is
// one law a model can learn once.
func cliFail(err error) int {
	fmt.Fprintln(cliErr, "error:", err.Error())
	return 1
}

// cliScan is the whole argument grammar: positionals collected in order,
// --flag value, --flag=value, bare boolean flags, --dep repeatable, and --
// ending the flags. Flags are allowed after positionals, because the
// doctrine's own sentences put them there.
func cliScan(argv []string) (*cliParsed, error) {
	p := &cliParsed{vals: map[string]string{}, lists: map[string][]string{}, bools: map[string]bool{}}
	boolFlags := map[string]bool{
		"json": true, "compact": true, "full": true, "next": true,
		"keep-done": true, "cascade": true, "version": true, "archived": true,
	}
	valueFlags := map[string]bool{
		"db": true, "agent": true, "project": true, "as": true, "kind": true,
		"dep": true, "check": true, "priority": true, "description": true, "parent": true, "into": true,
		"after": true, "before": true, "title": true, "prepend": true,
		"result": true, "subtasks": true, "task": true, "limit": true,
		"status": true, "chat": true, "older-than": true, "role": true,
		"by": true, "since": true,
	}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch {
		case arg == "--":
			p.pos = append(p.pos, argv[i+1:]...)
			return p, nil
		case strings.HasPrefix(arg, "--"):
			name := arg[2:]
			value, hasValue := "", false
			if eq := strings.Index(name, "="); eq >= 0 {
				name, value, hasValue = name[:eq], name[eq+1:], true
			}
			switch {
			case name == "help":
				p.bools["help"] = true
			case boolFlags[name]:
				p.bools[name] = !hasValue || value != "false"
			case valueFlags[name]:
				if !hasValue {
					if i+1 >= len(argv) {
						return nil, fmt.Errorf("flag --%s needs a value", name)
					}
					i++
					value = argv[i]
				}
				if name == "dep" || name == "check" {
					p.lists[name] = append(p.lists[name], value)
				} else {
					p.vals[name] = value
				}
			default:
				return nil, fmt.Errorf("unknown flag --%s — run \"plandb help\" for the ported flags", name)
			}
		case len(arg) > 1 && arg[0] == '-':
			for _, r := range arg[1:] {
				switch r {
				case 'c':
					p.bools["compact"] = true
				case 'h':
					p.bools["help"] = true
				default:
					return nil, fmt.Errorf("unknown flag -%c — run \"plandb help\" for the ported flags", r)
				}
			}
		default:
			p.pos = append(p.pos, arg)
		}
	}
	return p, nil
}

// cliPrintJSON writes one value as --json prints it: indented, newline-
// ended, with HTML escaping off so a description carrying `>` or `&` survives
// as the characters the worker wrote rather than their escapes.
func cliPrintJSON(value any) error {
	encoder := json.NewEncoder(cliOut)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func (p *cliParsed) verbPath() string {
	if len(p.pos) >= 2 && (p.pos[0] == "task" || p.pos[0] == "what-if") {
		return p.pos[0] + " " + p.pos[1]
	}
	if len(p.pos) > 0 {
		return p.pos[0]
	}
	return ""
}

// tail answers the parse with the first n words dropped: the task and
// what-if families then read their object from position one, exactly the
// way the top-level verbs do, so no handler carries two spellings of where
// its arguments live.
func (p *cliParsed) tail(n int) *cliParsed {
	sub := *p
	sub.pos = p.pos[n:]
	return &sub
}

// cliRefusal answers the supervisor's own sentence for every verb that owns
// the task lifecycle or the run's scope. The verb is named in the refusal so
// a model that reaches for one learns the rule in one step, and nothing
// here touches a store — a refused verb must change nothing at all.
func cliRefusal(p *cliParsed) (string, bool) {
	verb := p.verbPath()
	switch {
	case len(p.pos) == 0:
		return "", false
	case oneOf(p.pos[0], "claim", "start", "fail", "pause", "next", "heartbeat", "progress", "approve",
		"use", "mcp", "serve", "watch", "events", "ahead", "artifact", "export", "import"):
		return verb + ": Task lifecycle and scope are managed by the supervisor", true
	case p.pos[0] == "project":
		// `project` beyond `init` is the runtime's scope: one store per run,
		// and the run's project is fixed at init.
		return verb + ": Task lifecycle and scope are managed by the supervisor", true
	case p.pos[0] == "task" && len(p.pos) >= 2 &&
		oneOf(p.pos[1], "claim", "start", "fail", "next", "heartbeat", "progress", "approve"):
		return verb + ": Task lifecycle and scope are managed by the supervisor", true
	}
	return "", false
}

// RunEnv names the run a worker belongs to, by its root task's id. The door
// that seats a run worker exports it beside PLANDB_DB, and a store found at
// that path whose root is ANOTHER run's is refused rather than written: a path
// says where a run's store was, and only the root says which run it is.
const RunEnv = "PLANDB_RUN"

// cliStore opens the run's store without being told where it is: --db, then
// PLANDB_DB, then the first ancestor holding plandb.db or
// .codeaf/plandb.db. One store per file; --project is accepted and checked
// only when given.
func cliStore(p *cliParsed) (*Store, error) {
	path := p.vals["db"]
	if path == "" {
		path = os.Getenv("PLANDB_DB")
	}
	if path == "" {
		path = cliFindStore()
	}
	if path == "" {
		return nil, errors.New("no plan store found — run \"plandb init <name>\" in the run's directory, or pass --db <path>")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("no plan store at %s — run \"plandb init <name>\" first", path)
	}
	st, err := Open(path, "", "", "", "")
	if err != nil {
		return nil, err
	}
	if want := p.vals["project"]; want != "" && st.Project() != want {
		return nil, fmt.Errorf("the plan store at %s belongs to project %q, not %q", path, st.Project(), want)
	}
	// A STORE THAT IS ANOTHER RUN'S IS REFUSED WHOLE, reads and writes alike: a
	// worker reading another run's plan would plan against work that is not its
	// own, and one writing it filed its children under the other run's root.
	if want := os.Getenv(RunEnv); want != "" && st.RootID() != want {
		root := st.RootID()
		_ = st.Close()
		return nil, fmt.Errorf("the plan store at %s is another run's (t-%s), not this worker's run (t-%s), so nothing was read or written; this worker's run is over or was set aside", path, root, want)
	}
	return st, nil
}

// cliFindStore walks up from the current directory looking for the first
// ancestor holding plandb.db or .codeaf/plandb.db — the same store the
// runtime derives for the run, found the same way from every worker's shell.
func cliFindStore() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		for _, name := range []string{"plandb.db", filepath.Join(".codeaf", "plandb.db")} {
			candidate := filepath.Join(dir, name)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// cliInit creates the run: one store, one project named NAME, and the root
// task that IS the run — claimed running by "runtime", which is what keeps
// every worker verb from completing it. A store already at the resolved path
// is a refusal, not a merge: two runs sharing one file would dispatch each
// other's children.
func cliInit(p *cliParsed) error {
	if len(p.pos) < 2 {
		return errors.New(`init needs a project name — plandb init <name> --description "the goal"`)
	}
	name := strings.TrimSpace(p.pos[1])
	if name == "" {
		return errors.New("init needs a project name")
	}
	path := p.vals["db"]
	if path == "" {
		path = os.Getenv("PLANDB_DB")
	}
	if path == "" {
		if found := cliFindStore(); found != "" {
			return fmt.Errorf("a plan store already exists at %s — this directory is already inside a run; point --db somewhere new to start another", found)
		}
		path = "plandb.db"
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("a plan store already exists at %s — point --db somewhere new to start another run", path)
	}
	st, err := Open(path, name, "root", name, p.vals["description"])
	if err != nil {
		return err
	}
	if p.bools["json"] {
		return cliPrintJSON(cliTaskObject(st, st.Task(st.RootID())))
	}
	fmt.Fprintf(cliOut, "created %s (%s)\n", cliProjectID(name), name)
	fmt.Fprintln(cliOut)
	fmt.Fprintln(cliOut, `next: plandb add "title" --description "detailed spec" [--dep t-upstream] [--check command] [--as custom-id]`)
	fmt.Fprintln(cliOut, "tip:  create tasks in dependency order. use --dep to chain them.")
	fmt.Fprintln(cliOut, `      plandb add "A" --as a && plandb add "B" --dep t-a --as b`)
	fmt.Fprintln(cliOut, "      plandb go → work → plandb done --next → repeat")
	return nil
}

// cliDispatch runs the ported verb. The switch is flat on purpose: every
// verb is one store call plus its rendering, and a registry would only hide
// that.
func cliDispatch(st *Store, p *cliParsed) error {
	switch p.pos[0] {
	case "add":
		return cliAdd(st, p)
	case "split":
		return cliSplit(st, p)
	case "go":
		return cliGo(st, p)
	case "done":
		return cliDone(st, p)
	case "wait":
		return cliWait(st, p)
	case "list":
		return cliList(st, p)
	case "archive":
		return cliArchive(st, p)
	case "status":
		return cliStatus(st, p)
	case "spend":
		return cliSpendVerb(st, p)
	case "search":
		return cliSearchVerb(st, p)
	case "context":
		return cliContext(st, p)
	case "contexts":
		return cliContexts(st, p)
	case "prune":
		return cliPrune(st, p)
	case "critical-path":
		return cliCriticalPath(st, p)
	case "bottlenecks":
		return cliBottlenecks(st, p)
	case "show":
		return cliShow(st, p)
	case "task":
		if len(p.pos) < 2 {
			return errors.New("task needs a subcommand — one of add-dep, amend, cancel, get, insert, note, notes, overview, pause, pivot, resume, set-checks")
		}
		switch p.pos[1] {
		case "add-dep":
			return cliAddDep(st, p.tail(1))
		case "amend":
			return cliAmend(st, p.tail(1))
		case "cancel":
			return cliCancel(st, p.tail(1))
		case "get":
			return cliShow(st, p.tail(1))
		case "insert":
			return cliInsert(st, p.tail(1))
		case "note":
			return cliNote(st, p.tail(1))
		case "notes":
			return cliNotes(st, p.tail(1))
		case "overview":
			return cliOverview(st, p.tail(1))
		case "pause":
			return cliTaskHold(st, p.tail(1), true)
		case "pivot":
			return cliPivot(st, p.tail(1))
		case "resume":
			return cliTaskHold(st, p.tail(1), false)
		case "set-checks":
			return cliSetChecks(st, p.tail(1))
		default:
			return fmt.Errorf("unknown task subcommand %q — run \"plandb help\" for the ported set", p.pos[1])
		}
	case "what-if":
		if len(p.pos) < 2 {
			return errors.New(`what-if previews a cancel — plandb what-if cancel <task-id>`)
		}
		if p.pos[1] != "cancel" {
			return fmt.Errorf("unknown what-if %q — cancel is the one preview", p.pos[1])
		}
		return cliWhatIf(st, p.tail(1))
	default:
		return fmt.Errorf("unknown command %q — run \"plandb help\" for the ported verbs", p.pos[0])
	}
}

// cliAdd creates one task. Absent --parent the task lands under the run's
// root — the caller's running task is the runtime's business, and the
// doctrine teaches --parent explicitly for decomposition.
func cliAdd(st *Store, p *cliParsed) error {
	if len(p.pos) < 2 {
		return errors.New(`add needs a title — plandb add "title" --description "the work order"`)
	}
	spec := TaskSpec{
		ID:          st.NextID(),
		Title:       p.pos[1],
		Description: p.vals["description"],
		Kind:        p.vals["kind"],
		Checks:      append([]string(nil), p.lists["check"]...),
	}
	if as := p.vals["as"]; as != "" {
		spec.ID = as
	}
	if role := p.vals["role"]; role != "" {
		if !validRole(role) {
			return roleRefusal()
		}
		spec.Role = role
	}
	if parent := p.vals["parent"]; parent != "" {
		up, err := cliResolve(st, parent)
		if err != nil {
			return err
		}
		spec.ParentID = up.ID
	}
	for _, dep := range p.lists["dep"] {
		word, kind, err := cliDepArg(dep)
		if err != nil {
			return err
		}
		up, err := st.Resolve(word)
		if err != nil {
			if strings.Contains(err.Error(), "no task matches") {
				return fmt.Errorf("dependency task '%s' not found. Create it first, then add the dependency.", cliID(strings.TrimPrefix(word, "t-")))
			}
			return err
		}
		spec.Dependencies = append(spec.Dependencies, Dependency{TaskID: up.ID, Kind: kind})
	}
	if pr := p.vals["priority"]; pr != "" {
		n, err := strconv.Atoi(pr)
		if err != nil {
			return fmt.Errorf("--priority needs a whole number, not %q", pr)
		}
		spec.Priority = n
	}
	created, err := st.AddMany([]TaskSpec{spec})
	if err != nil {
		return err
	}
	task := created[0]
	if p.bools["json"] {
		return cliPrintJSON(cliTaskObject(st, task))
	}
	fmt.Fprintf(cliOut, "created task %s (%s)\n", cliID(task.ID), task.Title)
	return nil
}

// cliDepArg splits one --dep value: TASK_ID, or TASK_ID:KIND where KIND is
// one of the store's three kinds.
func cliDepArg(value string) (string, DepKind, error) {
	if id, kind, found := strings.Cut(value, ":"); found {
		switch DepKind(kind) {
		case DepFeedsInto, DepBlocks, DepSuggests:
			return id, DepKind(kind), nil
		default:
			return "", "", fmt.Errorf("--dep kind %q is not one of feeds_into, blocks, suggests", kind)
		}
	}
	return value, DepFeedsInto, nil
}

// cliSplit creates the named children under one task. The whole batch is
// validated before anything is written (AddMany's law), so a bad third part
// creates nothing — which is what the split answer and the runtime's
// dispatch both rest on.
func cliSplit(st *Store, p *cliParsed) error {
	if len(p.pos) < 2 {
		return errors.New(`split needs a task and --into — plandb split <task-id> --into '[{"title":"..."}]'`)
	}
	parent, err := cliResolve(st, p.pos[1])
	if err != nil {
		return err
	}
	into := p.vals["into"]
	if into == "" {
		return errors.New(`split needs --into — a JSON array of {title, description, deps_on}, comma titles ("A, B"), or a chain ("A > B > C")`)
	}
	parts, err := cliSplitSpecs("--into", into)
	if err != nil {
		return err
	}
	_, specs, err := cliBatchSpecs(st, parent.ID, parts)
	if err != nil {
		return err
	}
	created, err := st.AddMany(specs)
	if err != nil {
		return err
	}
	if p.bools["json"] {
		return cliPrintJSON(cliSplitObject(st, parent, created))
	}
	fmt.Fprintf(cliOut, "split %s\n", cliID(parent.ID))
	return nil
}

// cliSplitSpecs reads an --into/--subtasks value. A JSON array of parts wins
// when the value starts with '[', then a "A > B > C" chain — each part
// waiting on the one before — then comma-separated titles.
func cliSplitSpecs(flag, value string) ([]cliSplitPart, error) {
	if strings.HasPrefix(value, "[") {
		var parts []cliSplitPart
		if err := json.Unmarshal([]byte(value), &parts); err != nil {
			return nil, fmt.Errorf("%s is not a JSON array of {title, description, deps_on}: %v", flag, err)
		}
		if len(parts) == 0 {
			return nil, fmt.Errorf("%s names no parts", flag)
		}
		for i, part := range parts {
			if strings.TrimSpace(part.Title) == "" {
				return nil, fmt.Errorf("%s part %d has no title", flag, i)
			}
		}
		return parts, nil
	}
	if strings.Contains(value, ">") {
		var parts []cliSplitPart
		for _, title := range strings.Split(value, ">") {
			title = strings.TrimSpace(title)
			if title == "" {
				return nil, fmt.Errorf("%s chain has an empty step", flag)
			}
			parts = append(parts, cliSplitPart{Title: title})
		}
		for i := 1; i < len(parts); i++ {
			parts[i].DepsOn = []string{parts[i-1].Title}
		}
		return parts, nil
	}
	var parts []cliSplitPart
	for _, title := range strings.Split(value, ",") {
		title = strings.TrimSpace(title)
		if title == "" {
			continue
		}
		parts = append(parts, cliSplitPart{Title: title})
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("%s names no titles", flag)
	}
	return parts, nil
}

// cliBatchSpecs mints one fresh id per part and resolves deps_on — which
// names sibling TITLES — against the batch, so the store's whole-batch
// validation sees exact ids.
func cliBatchSpecs(st *Store, parentID string, parts []cliSplitPart) ([]string, []TaskSpec, error) {
	ids := make([]string, len(parts))
	used := map[string]bool{}
	for i := range parts {
		ids[i] = st.NextID()
		for used[ids[i]] {
			ids[i] = st.NextID()
		}
		used[ids[i]] = true
	}
	index := make(map[string]string, len(parts))
	for i, part := range parts {
		index[part.Title] = ids[i]
	}
	specs := make([]TaskSpec, len(parts))
	for i, part := range parts {
		spec := TaskSpec{ID: ids[i], Title: part.Title, Description: part.Description, ParentID: parentID}
		for _, sibling := range part.DepsOn {
			if sibling == part.Title {
				return nil, nil, fmt.Errorf("part %q depends on itself", part.Title)
			}
			up, ok := index[sibling]
			if !ok {
				return nil, nil, fmt.Errorf("part %q depends on %q, which is not one of the split's own parts", part.Title, sibling)
			}
			spec.Dependencies = append(spec.Dependencies, Dependency{TaskID: up, Kind: DepFeedsInto})
		}
		specs[i] = spec
	}
	return ids, specs, nil
}

// cliGo claims the highest-priority ready task for the CALLER, not for the
// runtime.
// The runtime re-claims a task claimed this way under its own id once the
// pulse makes it a node, which is what keeps the worker's finish command
// enforceable.
func cliGo(st *Store, p *cliParsed) error {
	task, err := st.ClaimNext(cliAgent(p))
	if err != nil {
		return err
	}
	if task == nil {
		if p.bools["json"] {
			fmt.Fprintln(cliOut, "null")
			return nil
		}
		fmt.Fprintln(cliOut, `nothing ready to claim — tasks may be waiting on dependencies or blocked by running work. see "plandb status".`)
		return nil
	}
	return cliPrintClaimed(st, task, p.bools["json"])
}

// cliPrintClaimed renders the claimed task: the arrow line with the run's
// counts, the downstream work that will receive the result, and the action
// reminder.
func cliPrintClaimed(st *Store, task *Task, asJSON bool) error {
	if asJSON {
		return cliPrintJSON(cliTaskObject(st, task))
	}
	fmt.Fprintf(cliOut, "→ %s %q %s\n", cliID(task.ID), task.Title, cliBracket(st))
	if len(task.Checks) > 0 {
		fmt.Fprintln(cliOut, "checks:")
		for _, check := range task.Checks {
			fmt.Fprintf(cliOut, "  %s\n", check)
		}
	}
	if deps := cliHardDependents(st, task.ID); len(deps) > 0 {
		fmt.Fprintln(cliOut)
		for _, down := range deps {
			fmt.Fprintf(cliOut, "downstream: %s %q (receives YOUR result)\n", cliID(down.ID), down.Title)
		}
	}
	fmt.Fprintln(cliOut)
	fmt.Fprintln(cliOut, `actions: context "..." --kind discovery | search "query" | split --into "A, B" | done --next`)
	return nil
}

// cliDone completes a task the caller owns — the store enforces ownership
// against the name the runtime claimed with. With --next it then claims the
// next ready task; a crash between the two leaves that task unclaimed,
// which the next pass simply claims.
func cliDone(st *Store, p *cliParsed) error {
	if p.vals["result"] == "" {
		return errors.New(`done needs --result — the completion record the next worker reads`)
	}
	agent := cliAgent(p)
	var id string
	if len(p.pos) >= 2 {
		task, err := cliResolve(st, p.pos[1])
		if err != nil {
			return err
		}
		id = task.ID
	} else {
		task := cliRunningFor(st, agent)
		if task == nil {
			return fmt.Errorf("no running task found for agent '%s'. Specify task ID explicitly.", agent)
		}
		id = task.ID
	}
	task, err := st.Done(id, agent, p.vals["result"], nil, nil)
	if err != nil {
		return err
	}
	if p.bools["json"] {
		out := struct {
			Task *cliTaskJSON `json:"task"`
			Next *cliTaskJSON `json:"next,omitempty"`
		}{Task: cliTaskObject(st, task)}
		if p.bools["next"] {
			next, err := st.ClaimNext(agent)
			if err != nil {
				return err
			}
			if next != nil {
				out.Next = cliTaskObject(st, next)
			}
		}
		return cliPrintJSON(out)
	}
	fmt.Fprintf(cliOut, "✓ %s done %s\n", cliID(task.ID), cliBracket(st))
	c := cliCount(st)
	if c.total > 0 && c.done == c.total {
		fmt.Fprintln(cliOut)
		fmt.Fprintln(cliOut, "all tasks complete!")
		return nil
	}
	if p.bools["next"] {
		next, err := st.ClaimNext(agent)
		if err != nil {
			return err
		}
		if next != nil {
			fmt.Fprintln(cliOut)
			if err := cliPrintClaimed(st, next, false); err != nil {
				return err
			}
		}
	}
	return nil
}

// cliRunningFor answers the caller's claimed-or-running task, the implicit
// object of a bare `done`.
func cliRunningFor(st *Store, agent string) *Task {
	for _, task := range st.Tasks() {
		if task.ID != st.RootID() && task.ClaimedBy == agent &&
			(task.Status == StatusClaimed || task.Status == StatusRunning) {
			return task
		}
	}
	return nil
}

// cliWait parks a task its worker cannot go on: the store releases the claim
// and leaves the task open and not done, and the runtime launches it again when
// its wait is over — once every dependency is done and every child has finished,
// or at once if one of them failed or was cancelled. A task with nothing open to
// wait on — no dependency that is not done, no child that is not terminal — is
// refused by the store with the reason, so a worker cannot park on nothing.
func cliWait(st *Store, p *cliParsed) error {
	agent := cliAgent(p)
	var id string
	if len(p.pos) >= 2 {
		task, err := cliResolve(st, p.pos[1])
		if err != nil {
			return err
		}
		id = task.ID
	} else {
		task := cliRunningFor(st, agent)
		if task == nil {
			return fmt.Errorf("no running task found for agent '%s'. Specify task ID explicitly.", agent)
		}
		id = task.ID
	}
	task, err := st.Wait(id, agent)
	if err != nil {
		return err
	}
	if p.bools["json"] {
		return cliPrintJSON(cliTaskObject(st, task))
	}
	fmt.Fprintf(cliOut, "⏸ %s waiting %s — it is launched again once its wait is over: when nothing it waited on is open, or when one of them failed\n", cliID(task.ID), cliBracket(st))
	return nil
}

// cliAgent answers the caller's identity: --agent, then PLANDB_AGENT, then
// "default".
func cliAgent(p *cliParsed) string {
	if agent := p.vals["agent"]; agent != "" {
		return agent
	}
	if agent := os.Getenv("PLANDB_AGENT"); agent != "" {
		return agent
	}
	return "default"
}

// cliAddDep adds one edge. The graph laws are asked of the whole result — a
// hard edge between a task and its own ancestor or descendant, or one that
// closes a cycle, refuses the edge rather than bending the plan.
func cliAddDep(st *Store, p *cliParsed) error {
	if len(p.pos) < 2 {
		return errors.New(`add-dep needs the downstream task — plandb task add-dep <downstream> --after <upstream>`)
	}
	down, err := cliResolve(st, p.pos[1])
	if err != nil {
		return err
	}
	up, err := cliResolve(st, p.vals["after"])
	if err != nil {
		return err
	}
	kind := DepKind(p.vals["kind"])
	if kind == "" {
		kind = DepFeedsInto
	}
	task, err := st.AddDep(down.ID, up.ID, kind)
	if err != nil {
		return err
	}
	if p.bools["json"] {
		return cliPrintJSON(cliTaskObject(st, task))
	}
	fmt.Fprintf(cliOut, "added %s ← %s (%s)\n", cliID(down.ID), cliID(up.ID), kind)
	return nil
}

// cliSetChecks replaces a task's declared proof commands.
func cliSetChecks(st *Store, p *cliParsed) error {
	if len(p.pos) < 2 {
		return errors.New("set-checks needs a task")
	}
	task, err := cliResolve(st, p.pos[1])
	if err != nil {
		return err
	}
	checks := append([]string(nil), p.lists["check"]...)
	updated, err := st.Revise(task.ID, TaskPatch{Checks: &checks})
	if err != nil {
		return err
	}
	if p.bools["json"] {
		return cliPrintJSON(cliTaskObject(st, updated))
	}
	fmt.Fprintf(cliOut, "set checks on %s\n", cliID(updated.ID))
	return nil
}

// cliAmend prepends to a task's description — one of the two ways a plan
// learns while it runs.
func cliAmend(st *Store, p *cliParsed) error {
	if len(p.pos) < 2 {
		return errors.New(`amend needs a task — plandb task amend <task-id> --prepend "NOTE: ..."`)
	}
	if p.vals["prepend"] == "" {
		return errors.New(`amend needs --prepend — the text to put at the front of the description`)
	}
	task, err := cliResolve(st, p.pos[1])
	if err != nil {
		return err
	}
	updated, err := st.Amend(task.ID, p.vals["prepend"])
	if err != nil {
		return err
	}
	if p.bools["json"] {
		return cliPrintJSON(cliTaskObject(st, updated))
	}
	fmt.Fprintf(cliOut, "amended %s\n", cliID(updated.ID))
	return nil
}

// cliInsert adds a task between two existing ones. The new task depends on
// --after and takes --after's place in the chain; with --before, the DIRECT
// after→before edge is lifted once the new task is wired between them, so
// --before waits on the new task alone rather than on both.
func cliInsert(st *Store, p *cliParsed) error {
	after := p.vals["after"]
	if after == "" {
		return errors.New(`insert needs --after — the task the new one depends on`)
	}
	if p.vals["title"] == "" {
		return errors.New(`insert needs --title`)
	}
	up, err := cliResolve(st, after)
	if err != nil {
		return err
	}
	var before *Task
	if word := p.vals["before"]; word != "" {
		down, err := cliResolve(st, word)
		if err != nil {
			return err
		}
		before = down
	}
	created, err := st.AddMany([]TaskSpec{{
		ID: st.NextID(), Title: p.vals["title"], Description: p.vals["description"],
		ParentID: up.ParentID,
	}})
	if err != nil {
		return err
	}
	newID := created[0].ID
	// THE NEW TASK WAITS ON --after, and takes --after's place. Inserted
	// between the two, the work downstream reads must arrive through it —
	// without this edge the new task is free to start beside --after, which
	// is not "between". With --before it becomes --before's upstream AND the
	// old direct after→before edge goes: leaving it would make --before wait
	// on both the new task and the task it was inserted to follow, which is
	// not between them.
	if _, err := st.AddDep(newID, up.ID, DepFeedsInto); err != nil {
		return err
	}
	if before != nil {
		if _, err := st.AddDep(before.ID, newID, DepFeedsInto); err != nil {
			return err
		}
		if _, err := st.RemoveDep(before.ID, up.ID); err != nil {
			return err
		}
	}
	if p.bools["json"] {
		return cliPrintJSON(cliInsertObject(st, st.Task(newID)))
	}
	fmt.Fprintf(cliOut, "inserted %s\n", cliID(newID))
	return nil
}

// cliPivot replaces a parent's open subtree with the named children. The
// order is the whole trick, read bottom-up: the parts are validated and
// minted first, so a bad --subtasks value writes nothing; the doomed
// subtree is READ before the new children go in, because once added they
// are children of the parent too and the same read would sweep them with
// the old ones; then the new children are added, and only then are the old
// ones cancelled — cancelling the last open child would auto-complete the
// composite parent itself (promote's law — a parent whose children are all
// terminal is terminal, failed when they did not all finish), and an
// AddMany running after that would refuse the now-terminal parent and
// leave the plan without its replacement. The store's Cancel cascades
// descendants and hard dependents on its own.
func cliPivot(st *Store, p *cliParsed) error {
	if len(p.pos) < 2 {
		return errors.New(`pivot needs a parent — plandb task pivot <parent-id> --subtasks '[{"title":"..."}]'`)
	}
	parent, err := cliResolve(st, p.pos[1])
	if err != nil {
		return err
	}
	if parent.ID == st.RootID() {
		return errors.New("the root task is the run itself — pivot one of its children instead")
	}
	sub := p.vals["subtasks"]
	if sub == "" {
		return errors.New(`pivot needs --subtasks — a JSON array of {title, description, deps_on}`)
	}
	parts, err := cliSplitSpecs("--subtasks", sub)
	if err != nil {
		return err
	}
	_, specs, err := cliBatchSpecs(st, parent.ID, parts)
	if err != nil {
		return err
	}
	doomed := cliSubtree(st, parent.ID, p.bools["keep-done"])
	// The whole subtree, read before the new children go in, so what
	// --keep-done spared can be told apart from the replacements and reported.
	standing := cliSubtreeAll(st, parent.ID)
	created, err := st.AddMany(specs)
	if err != nil {
		return err
	}
	cancelled := make([]string, 0, len(doomed))
	for _, task := range doomed {
		if _, err := st.Cancel(task.ID, "pivot: the subtree of "+cliID(parent.ID)+" was replaced"); err != nil {
			if strings.Contains(err.Error(), "already terminal") {
				continue // an earlier cancel in this same cascade got there first
			}
			return err
		}
		cancelled = append(cancelled, cliID(task.ID))
	}
	if p.bools["json"] {
		ids := make([]string, 0, len(created))
		for _, task := range created {
			ids = append(ids, cliID(task.ID))
		}
		// kept is what --keep-done spared: the finished and in-flight tasks the
		// pivot left standing. Without the flag nothing is spared, so it is
		// empty even though terminal work survives either way.
		kept := []string{}
		if p.bools["keep-done"] {
			cancelledSet := map[string]bool{}
			for _, id := range cancelled {
				cancelledSet[id] = true
			}
			for _, task := range standing {
				if !cancelledSet[cliID(task.ID)] {
					kept = append(kept, cliID(task.ID))
				}
			}
		}
		return cliPrintJSON(struct {
			ParentTaskID string          `json:"parent_task_id"`
			Created      []string        `json:"created"`
			Cancelled    []string        `json:"cancelled"`
			Kept         []string        `json:"kept"`
			Effect       cliEffectJSON   `json:"effect"`
			ProjectState cliProjectState `json:"project_state"`
		}{cliID(parent.ID), ids, cancelled, kept, cliEffect(st, created, nil), cliProjectStateOf(st)})
	}
	fmt.Fprintf(cliOut, "pivoted %s\n", cliID(parent.ID))
	return nil
}

// cliCancel ends a task; the store cascades its descendants and its hard
// dependents. The printed rows count everything the cascade ends, which is
// what the preview promised.
func cliCancel(st *Store, p *cliParsed) error {
	if len(p.pos) < 2 {
		return errors.New(`cancel needs a task — plandb task cancel <task-id>`)
	}
	target, err := cliResolve(st, p.pos[1])
	if err != nil {
		return err
	}
	desc, deps, err := cliCancelPreview(st, target)
	if err != nil {
		return err
	}
	if _, err := st.Cancel(target.ID, "cancelled by "+cliAgent(p)); err != nil {
		return err
	}
	if p.bools["json"] {
		ids := []string{cliID(target.ID)}
		for _, d := range desc {
			ids = append(ids, cliID(d.ID))
		}
		for _, d := range deps {
			ids = append(ids, cliID(d.ID))
		}
		return cliPrintJSON(struct {
			Cancelled []string `json:"cancelled"`
		}{ids})
	}
	fmt.Fprintf(cliOut, "cancelled rows=%d\n", 1+len(desc)+len(deps))
	return nil
}

// cliWhatIf previews a cancel without applying it: the task, its
// non-terminal descendants, and the hard dependents that would be ended
// with them. A what-if previews the cascade rather than echoing the verb
// back, because "preview effects" is what its help promises.
func cliWhatIf(st *Store, p *cliParsed) error {
	if len(p.pos) < 2 {
		return errors.New(`what-if needs a task — plandb what-if cancel <task-id>`)
	}
	target, err := cliResolve(st, p.pos[1])
	if err != nil {
		return err
	}
	desc, deps, err := cliCancelPreview(st, target)
	if err != nil {
		return err
	}
	if p.bools["json"] {
		return cliPrintJSON(struct {
			Task        string   `json:"task"`
			Descendants []string `json:"descendants"`
			Dependents  []string `json:"dependents"`
			Total       int      `json:"total"`
		}{cliID(target.ID), cliIDs(desc), cliIDs(deps), 1 + len(desc) + len(deps)})
	}
	fmt.Fprintf(cliOut, "what-if cancel %s\n", cliID(target.ID))
	fmt.Fprintf(cliOut, "would cancel: %s %q [%s]\n", cliID(target.ID), target.Title, target.Status)
	if len(desc) == 0 {
		fmt.Fprintln(cliOut, "descendants: (none)")
	} else {
		fmt.Fprintln(cliOut, "descendants:")
		for _, d := range desc {
			fmt.Fprintf(cliOut, "  %s %q [%s]\n", cliID(d.ID), d.Title, d.Status)
		}
	}
	if len(deps) == 0 {
		fmt.Fprintln(cliOut, "hard dependents: (none)")
	} else {
		fmt.Fprintln(cliOut, "hard dependents:")
		for _, d := range deps {
			fmt.Fprintf(cliOut, "  %s %q [%s]\n", cliID(d.ID), d.Title, d.Status)
		}
	}
	fmt.Fprintln(cliOut, `nothing written — run "plandb task cancel `+cliID(target.ID)+`" to apply it.`)
	return nil
}

// cliCancelPreview mirrors, as a read, the cascade Cancel runs: the target's
// non-terminal descendants, then the fixpoint of non-terminal hard
// dependents of everything already doomed.
func cliCancelPreview(st *Store, target *Task) (desc, deps []*Task, err error) {
	if target.ID == st.RootID() {
		return nil, nil, errors.New("the harness owns the root task")
	}
	desc = cliSubtree(st, target.ID, false)
	willCancel := map[string]bool{target.ID: true}
	for _, d := range desc {
		willCancel[d.ID] = true
	}
	tasks := st.Tasks()
	changed := true
	for changed {
		changed = false
		for _, task := range tasks {
			if willCancel[task.ID] || terminal(task.Status) {
				continue
			}
			for _, dep := range task.Dependencies {
				if dep.Kind == DepSuggests {
					continue
				}
				up := cliTaskByID(tasks, dep.TaskID)
				if willCancel[dep.TaskID] || (up != nil && up.Status == StatusCancelled) {
					deps = append(deps, task)
					willCancel[task.ID] = true
					changed = true
					break
				}
			}
		}
	}
	return desc, deps, nil
}

// cliSubtree answers the parent's open descendants, shallowest first — the
// order a pivot cancels in. With keepDone only pending and ready tasks are
// answered; claimed and running work is left standing.
func cliSubtree(st *Store, parentID string, keepDone bool) []*Task {
	tasks := st.Tasks()
	byParent := make(map[string][]*Task, len(tasks))
	for _, task := range tasks {
		byParent[task.ParentID] = append(byParent[task.ParentID], task)
	}
	var out []*Task
	frontier := []string{parentID}
	for len(frontier) > 0 {
		var next []string
		for _, id := range frontier {
			for _, child := range byParent[id] {
				if terminal(child.Status) {
					continue
				}
				if keepDone && child.Status != StatusPending && child.Status != StatusReady {
					continue
				}
				out = append(out, child)
				next = append(next, child.ID)
			}
		}
		frontier = next
	}
	return out
}

// cliSubtreeAll answers every descendant of the parent, terminal and
// in-flight work included — the set a pivot leaves standing once --keep-done
// has protected them. It is read before the replacements go in, so the new
// children are never mistaken for kept work.
func cliSubtreeAll(st *Store, parentID string) []*Task {
	tasks := st.Tasks()
	byParent := make(map[string][]*Task, len(tasks))
	for _, task := range tasks {
		byParent[task.ParentID] = append(byParent[task.ParentID], task)
	}
	var out []*Task
	frontier := []string{parentID}
	for len(frontier) > 0 {
		var next []string
		for _, id := range frontier {
			for _, child := range byParent[id] {
				out = append(out, child)
				next = append(next, child.ID)
			}
		}
		frontier = next
	}
	return out
}

func cliTaskByID(tasks []*Task, id string) *Task {
	for _, task := range tasks {
		if task.ID == id {
			return task
		}
	}
	return nil
}

func cliIDs(tasks []*Task) []string {
	out := make([]string, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, cliID(task.ID))
	}
	return out
}

// cliHardDependents answers the tasks that wait directly on id through a
// hard edge — the work that receives the claimed task's result.
func cliHardDependents(st *Store, id string) []*Task {
	var out []*Task
	for _, task := range st.Tasks() {
		if task.ID == st.RootID() {
			continue
		}
		for _, dep := range task.Dependencies {
			if dep.Kind != DepSuggests && dep.TaskID == id {
				out = append(out, task)
				break
			}
		}
	}
	return out
}

// cliList answers the tasks in admission order, filtered the way the
// doctrine filters. The root is the run, not a row.
// cliFilter is the project/chat narrowing the reading verbs accept. An empty
// flag narrows nothing, which is the answer a caller that names neither gets.
func cliFilter(p *cliParsed) Filter {
	return Filter{Project: p.vals["project"], Chat: p.vals["chat"]}
}

func cliList(st *Store, p *cliParsed) error {
	if p.bools["archived"] {
		return cliListArchived(st, p)
	}
	var rows []*Task
	for _, task := range st.Tasks(cliFilter(p)) {
		if task.ID == st.RootID() {
			continue
		}
		if s := p.vals["status"]; s != "" && string(task.Status) != s {
			continue
		}
		if k := p.vals["kind"]; k != "" && task.Kind != k {
			continue
		}
		if a := p.vals["agent"]; a != "" && task.ClaimedBy != a {
			continue
		}
		rows = append(rows, task)
	}
	if p.bools["json"] {
		out := make([]*cliTaskJSON, 0, len(rows))
		for _, task := range rows {
			out = append(out, cliTaskObject(st, task))
		}
		return cliPrintJSON(out)
	}
	if len(rows) == 0 {
		fmt.Fprintln(cliOut, "(no rows)")
		return nil
	}
	for _, task := range rows {
		fmt.Fprintf(cliOut, "  %s %s [%s]\n", cliID(task.ID), task.Title, task.Status)
	}
	return nil
}

// cliListArchived is `list --archived`: the rows the archive holds, in the
// same shape the live list prints them.
func cliListArchived(st *Store, p *cliParsed) error {
	tasks, err := st.Archived()
	if err != nil {
		return err
	}
	if p.bools["json"] {
		out := make([]*cliTaskJSON, 0, len(tasks))
		for _, task := range tasks {
			out = append(out, cliTaskObject(st, task))
		}
		return cliPrintJSON(out)
	}
	if len(tasks) == 0 {
		fmt.Fprintln(cliOut, "(no rows)")
		return nil
	}
	for _, task := range tasks {
		fmt.Fprintf(cliOut, "  %s %s [%s]\n", cliID(task.ID), task.Title, task.Status)
	}
	return nil
}

// cliArchive moves old finished subtrees out of the live plan and into the
// archive. It is the runtime's and a person's verb, like pause: nothing a
// worker needs while it works, and the window defaults to the doctrine's
// 72h.
func cliArchive(st *Store, p *cliParsed) error {
	window := 72 * time.Hour
	if raw := p.vals["older-than"]; raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return fmt.Errorf("--older-than needs a duration like 72h, not %q", raw)
		}
		if parsed < 0 {
			return fmt.Errorf("--older-than cannot be negative, got %q", raw)
		}
		window = parsed
	}
	moved, err := st.Archive(window)
	if err != nil {
		return err
	}
	if p.bools["json"] {
		out := make([]*cliTaskJSON, 0, len(moved))
		for _, task := range moved {
			out = append(out, cliTaskObject(st, task))
		}
		return cliPrintJSON(out)
	}
	fmt.Fprintf(cliOut, "archived %d tasks\n", len(moved))
	return nil
}

// cliSpend prints the ledger's per-project and per-chat totals under the
// --full tree, and nothing at all when the run has never been charged. The
// tags are sorted so the same ledger prints the same lines twice.
func cliSpend(st *Store) {
	summary := st.Summary()
	for _, project := range sortedSpendTags(summary.ProjectSpend) {
		total := summary.ProjectSpend[project]
		fmt.Fprintf(cliOut, "spend project %s: $%.4f (%d calls)\n", project, total.USD, total.Calls)
	}
	for _, chat := range sortedSpendTags(summary.ChatSpend) {
		total := summary.ChatSpend[chat]
		fmt.Fprintf(cliOut, "spend chat %s: $%.4f (%d calls)\n", chat, total.USD, total.Calls)
	}
}

// cliSpendVerb is `plandb spend`: the ledger read back by the two groupings
// the seats care about — what each role spent and what each model spent — or,
// when --by names an axis, rolled up under that axis alone. The tags are
// sorted so the same ledger prints the same lines twice.
func cliSpendVerb(st *Store, p *cliParsed) error {
	if axis := strings.TrimSpace(p.vals["by"]); axis != "" {
		return cliSpendBy(st, p, axis)
	}
	summary := st.SpendSummary()
	if p.bools["json"] {
		return cliPrintJSON(summary)
	}
	if len(summary.ByRole) == 0 && len(summary.ByModel) == 0 {
		fmt.Fprintln(cliOut, "(no spend)")
		return nil
	}
	for _, role := range sortedSpendTags(summary.ByRole) {
		total := summary.ByRole[role]
		fmt.Fprintf(cliOut, "spend role %s: $%.4f (%d calls)\n", role, total.USD, total.Calls)
	}
	for _, model := range sortedSpendTags(summary.ByModel) {
		total := summary.ByModel[model]
		fmt.Fprintf(cliOut, "spend model %s: $%.4f (%d calls)\n", model, total.USD, total.Calls)
	}
	return nil
}

// cliSpendBy is `plandb spend --by AXIS`: the ledger rolled up under one axis,
// the heaviest key first. --since bounds the window to the charges written
// since a duration or a date. A store nobody charged prints one line saying
// what arrives there, never a zero row.
func cliSpendBy(st *Store, p *cliParsed, axis string) error {
	if !oneOf(axis, spendAxes...) {
		return fmt.Errorf("--by %s is not one of %s", axis, strings.Join(spendAxes, ", "))
	}
	since, err := cliSince(p.vals["since"])
	if err != nil {
		return err
	}
	lines := st.SpendBy(axis, since)
	if p.bools["json"] {
		if lines == nil {
			lines = []SpendLine{}
		}
		return cliPrintJSON(lines)
	}
	if len(lines) == 0 {
		fmt.Fprintf(cliOut, "no spend by %s yet — every charge the run makes lands here\n", axis)
		return nil
	}
	for _, line := range lines {
		fmt.Fprintf(cliOut, "spend %s %s: $%.4f (in %d out %d, %d calls)\n", axis, line.Key, line.USD, line.In, line.Out, line.Calls)
	}
	return nil
}

// cliSince reads the --since bound: a Go duration (24h, 90m), a whole number
// of days (7d), or a date (2026-09-01). An empty value bounds nothing, so the
// whole ledger is read.
func cliSince(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	if days, ok := strings.CutSuffix(raw, "d"); ok {
		if n, err := strconv.Atoi(days); err == nil && n >= 0 {
			return time.Now().AddDate(0, 0, -n), nil
		}
	}
	if span, err := time.ParseDuration(raw); err == nil {
		return time.Now().Add(-span), nil
	}
	if date, err := time.Parse("2006-01-02", raw); err == nil {
		return date, nil
	}
	return time.Time{}, fmt.Errorf("--since %q is not a duration like 24h, a day count like 7d, or a date like 2026-09-01", raw)
}

// sortedSpendTags answers a spend map's keys in order, so a render is stable.
func sortedSpendTags(totals map[string]SpendTotal) []string {
	tags := make([]string, 0, len(totals))
	for tag := range totals {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

// cliStatus renders the one-line summary, and with --full the containment
// tree and every dependency edge under it.
func cliStatus(st *Store, p *cliParsed) error {
	c := cliCount(st)
	if p.bools["json"] {
		return cliPrintJSON(struct {
			Project   string `json:"project"`
			ProjectID string `json:"project_id"`
			Total     int    `json:"total"`
			Pending   int    `json:"pending"`
			Ready     int    `json:"ready"`
			Running   int    `json:"running"`
			Done      int    `json:"done"`
			Failed    int    `json:"failed"`
			Cancelled int    `json:"cancelled"`
		}{st.Project(), cliProjectID(st.Project()), c.total, c.pending, c.ready, c.running, c.done, c.failed, c.cancelled})
	}
	fmt.Fprintf(cliOut, "%s %s: %d/%d done (%d%%) | ready: %s | running: %s | blocked: %d\n",
		cliProjectID(st.Project()), st.Project(), c.done, c.total, cliPercent(c.done, c.total),
		cliReadyList(st), cliRunningList(st), c.pending)
	if !p.bools["full"] {
		return nil
	}
	fmt.Fprintln(cliOut)
	cliTree(st)
	cliSpend(st)
	return nil
}

// cliTree prints the containment tree under the root — `├─`/`└─` before the
// child rows — then every dependency edge the plan carries.
func cliTree(st *Store) {
	tasks := st.Tasks()
	byParent := make(map[string][]*Task, len(tasks))
	for _, task := range tasks {
		byParent[task.ParentID] = append(byParent[task.ParentID], task)
	}
	var row func(task *Task)
	row = func(task *Task) {
		fmt.Fprintf(cliOut, "%s %s %s [%s]", cliIcon(task.Status), cliID(task.ID), task.Title, task.Status)
		if task.ClaimedBy != "" {
			fmt.Fprintf(cliOut, " %s", task.ClaimedBy)
		}
		fmt.Fprintln(cliOut)
	}
	if root := st.Task(st.RootID()); root != nil {
		row(root)
	}
	var walk func(id, prefix string)
	walk = func(id, prefix string) {
		children := byParent[id]
		for i, child := range children {
			branch, next := "├─ ", "│  "
			if i == len(children)-1 {
				branch, next = "└─ ", "   "
			}
			fmt.Fprint(cliOut, prefix+branch)
			row(child)
			walk(child.ID, prefix+next)
		}
	}
	walk(st.RootID(), "")
	fmt.Fprintln(cliOut, "dependencies:")
	hasEdges := false
	for _, task := range tasks {
		for _, dep := range task.Dependencies {
			hasEdges = true
			fmt.Fprintf(cliOut, "  %s ← %s (%s)\n", cliID(task.ID), cliID(dep.TaskID), dep.Kind)
		}
	}
	if !hasEdges {
		fmt.Fprintln(cliOut, "  (none)")
	}
}

// cliSearchVerb answers the ranked rows: kind, id, the task's own kind where
// there is one, and the line that matched.
func cliSearchVerb(st *Store, p *cliParsed) error {
	if len(p.pos) < 2 {
		return errors.New(`search needs a query — plandb search "schema"`)
	}
	limit := 0
	if n := p.vals["limit"]; n != "" {
		parsed, err := strconv.Atoi(n)
		if err != nil {
			return fmt.Errorf("--limit needs a whole number, not %q", n)
		}
		limit = parsed
	}
	results := st.Search(p.pos[1], limit, cliFilter(p))
	if p.bools["json"] {
		out := make([]map[string]any, 0, len(results))
		for _, r := range results {
			entry := map[string]any{"kind": r.Kind, "id": r.ID, "detail": r.Detail}
			if r.Kind == "task" {
				entry["id"] = cliID(r.ID)
				entry["title"] = r.Title
			}
			if r.TaskID != "" {
				entry["task_id"] = cliID(r.TaskID)
			}
			out = append(out, entry)
		}
		return cliPrintJSON(out)
	}
	if len(results) == 0 {
		fmt.Fprintln(cliOut, "(no results)")
		return nil
	}
	for _, r := range results {
		switch r.Kind {
		case "task":
			kind := ""
			if task := st.Task(r.ID); task != nil {
				kind = task.Kind
			}
			fmt.Fprintf(cliOut, "  [task] %s %s %s: %s\n", cliID(r.ID), kind, r.Title, r.Detail)
		case "note":
			fmt.Fprintf(cliOut, "  [note] %s on %s: %s\n", r.ID, cliID(r.TaskID), r.Detail)
		default:
			fmt.Fprintf(cliOut, "  [%s] %s: %s\n", r.Kind, r.ID, r.Detail)
		}
	}
	return nil
}

// cliContext records a run-wide fact; kinds are freeform, because the
// doctrine names them (`--kind decision`) and the store takes the word at
// face value.
func cliContext(st *Store, p *cliParsed) error {
	content := strings.Join(p.pos[1:], " ")
	if content == "" {
		return errors.New(`context needs the content — plandb context "what you learned" --kind decision`)
	}
	var taskID string
	if word := p.vals["task"]; word != "" {
		task, err := cliResolve(st, word)
		if err != nil {
			return err
		}
		taskID = task.ID
	}
	entry, err := st.AddContext(taskID, p.vals["kind"], content)
	if err != nil {
		return err
	}
	if p.bools["json"] {
		return cliPrintJSON(entry)
	}
	fmt.Fprintf(cliOut, "%s [%s]\n", entry.ID, entry.Kind)
	return nil
}

// cliContexts lists the run's context entries, newest first.
func cliContexts(st *Store, p *cliParsed) error {
	limit := 0
	if n := p.vals["limit"]; n != "" {
		parsed, err := strconv.Atoi(n)
		if err != nil {
			return fmt.Errorf("--limit needs a whole number, not %q", n)
		}
		limit = parsed
	}
	entries := st.Contexts("", p.vals["kind"], limit, cliFilter(p))
	if p.bools["json"] {
		return cliPrintJSON(entries)
	}
	if len(entries) == 0 {
		fmt.Fprintln(cliOut, "(no rows)")
		return nil
	}
	for _, entry := range entries {
		fmt.Fprintf(cliOut, "  %s [%s] %s\n", entry.ID, entry.Kind, entry.Content)
	}
	return nil
}

// cliPrune removes one context entry by the id AddContext answered.
func cliPrune(st *Store, p *cliParsed) error {
	if len(p.pos) < 2 {
		return errors.New(`prune needs the entry id — plandb prune c-00000001`)
	}
	if err := st.Prune(p.pos[1]); err != nil {
		return err
	}
	if p.bools["json"] {
		return cliPrintJSON(struct {
			Pruned string `json:"pruned"`
		}{p.pos[1]})
	}
	fmt.Fprintf(cliOut, "pruned %s\n", p.pos[1])
	return nil
}

// cliCriticalPath answers the longest hard-dependency chain, upstream
// first. Empty is the honest answer for a plan with no such chain; --json
// spells it as length zero and an empty path.
func cliCriticalPath(st *Store, p *cliParsed) error {
	path := st.CriticalPath()
	if p.bools["json"] {
		ids := make([]string, 0, len(path))
		for _, task := range path {
			ids = append(ids, cliID(task.ID))
		}
		return cliPrintJSON(struct {
			Length int    `json:"length"`
			Path   string `json:"path"`
		}{len(ids), strings.Join(ids, " > ")})
	}
	if len(path) == 0 {
		fmt.Fprintln(cliOut, "no critical path — no unfinished hard dependency chain.")
		return nil
	}
	fmt.Fprintf(cliOut, "Critical path (%d tasks):\n", len(path))
	for _, task := range path {
		fmt.Fprintf(cliOut, "  %s %s %s [%s]\n", cliIcon(task.Status), cliID(task.ID), task.Title, task.Status)
	}
	return nil
}

// cliBottlenecks answers the unfinished tasks holding up the most
// downstream work, most first.
func cliBottlenecks(st *Store, p *cliParsed) error {
	limit := 0
	if n := p.vals["limit"]; n != "" {
		parsed, err := strconv.Atoi(n)
		if err != nil {
			return fmt.Errorf("--limit needs a whole number, not %q", n)
		}
		limit = parsed
	}
	rows := st.Bottlenecks(limit)
	if p.bools["json"] {
		out := make([]cliBottleneckJSON, 0, len(rows))
		for _, row := range rows {
			out = append(out, cliBottleneckJSON{
				TaskID: cliID(row.Task.ID), Title: row.Task.Title,
				Status: row.Task.Status, DownstreamCount: row.Downstream,
			})
		}
		return cliPrintJSON(out)
	}
	if len(rows) == 0 {
		fmt.Fprintln(cliOut, "no unfinished tasks — nothing is blocking work.")
		return nil
	}
	fmt.Fprintln(cliOut, "Bottlenecks (tasks blocking the most downstream work):")
	for _, row := range rows {
		fmt.Fprintf(cliOut, "  %s %s — blocks %d tasks [%s]\n", cliID(row.Task.ID), row.Task.Title, row.Downstream, row.Task.Status)
	}
	return nil
}

// cliBottleneckJSON is one bottleneck row as --json prints it: the task's id,
// title and status, and the count of work that hard-depends on it directly.
type cliBottleneckJSON struct {
	TaskID          string `json:"task_id"`
	Title           string `json:"title"`
	Status          Status `json:"status"`
	DownstreamCount int    `json:"downstream_count"`
}

// cliShow renders one task's card, fuzzy id and all — `task get` is the
// same door under its longer spelling.
func cliShow(st *Store, p *cliParsed) error {
	if len(p.pos) < 2 {
		return errors.New(`show needs a task — plandb show <task-id>`)
	}
	task, err := cliResolve(st, p.pos[1])
	if err != nil {
		return err
	}
	if p.bools["json"] {
		return cliPrintJSON(cliTaskObject(st, task))
	}
	fmt.Fprintf(cliOut, "id: %s\n", cliID(task.ID))
	fmt.Fprintf(cliOut, "project: %s\n", cliProjectID(st.Project()))
	fmt.Fprintf(cliOut, "title: %s\n", task.Title)
	fmt.Fprintf(cliOut, "status: %s %s [role %s]\n", cliIcon(task.Status), task.Status, roleOf(task))
	fmt.Fprintf(cliOut, "kind: %s\n", task.Kind)
	fmt.Fprintf(cliOut, "priority: %d\n", task.Priority)
	if task.ClaimedBy != "" {
		fmt.Fprintf(cliOut, "agent: %s\n", task.ClaimedBy)
	}
	fmt.Fprintf(cliOut, "description: %s\n", task.Description)
	if task.Question != "" {
		fmt.Fprintf(cliOut, "question: %s\n", task.Question)
	}
	if len(task.Checks) > 0 {
		fmt.Fprintln(cliOut, "checks:")
		for _, check := range task.Checks {
			fmt.Fprintf(cliOut, "  %s\n", check)
		}
	}
	if task.ParentID != "" && task.ParentID != st.RootID() {
		fmt.Fprintf(cliOut, "parent: %s\n", cliID(task.ParentID))
	}
	if len(task.Dependencies) > 0 {
		parts := make([]string, 0, len(task.Dependencies))
		for _, dep := range task.Dependencies {
			parts = append(parts, fmt.Sprintf("%s (%s)", cliID(dep.TaskID), dep.Kind))
		}
		fmt.Fprintf(cliOut, "depends on: %s\n", strings.Join(parts, ", "))
	}
	if task.Result != "" {
		fmt.Fprintf(cliOut, "result: %s\n", task.Result)
	}
	if notes := st.Notes(task.ID, 0); len(notes) > 0 {
		fmt.Fprintln(cliOut, "notes:")
		for _, note := range notes {
			fmt.Fprintln(cliOut, cliNoteLine(note))
		}
	}
	return nil
}

// cliOverview renders every task in admission order — the reading set's
// widest answer. The --json shape pairs the task rows with the dependency
// edges between them, so a reader sees the plan's whole picture in one parse.
func cliOverview(st *Store, p *cliParsed) error {
	all := st.Tasks(cliFilter(p))
	if p.bools["json"] {
		tasks := make([]*cliTaskJSON, 0, len(all))
		for _, task := range all {
			if task.ID == st.RootID() {
				continue
			}
			tasks = append(tasks, cliTaskObject(st, task))
		}
		return cliPrintJSON(struct {
			Tasks        []*cliTaskJSON   `json:"tasks"`
			Dependencies []cliDepEdgeJSON `json:"dependencies"`
			Total        int              `json:"total"`
		}{tasks, cliDepEdges(st), len(tasks)})
	}
	count := 0
	for _, task := range all {
		if task.ID != st.RootID() {
			count++
		}
	}
	fmt.Fprintf(cliOut, "Project overview: %d tasks\n", count)
	for _, task := range all {
		if task.ID == st.RootID() {
			continue
		}
		fmt.Fprintf(cliOut, "  %s %s %s [%s] [role %s]", cliIcon(task.Status), cliID(task.ID), task.Title, task.Status, roleOf(task))
		if task.ClaimedBy != "" {
			fmt.Fprintf(cliOut, " %s", task.ClaimedBy)
		}
		if task.Chat != "" {
			fmt.Fprintf(cliOut, " [chat:%s]", task.Chat)
		}
		fmt.Fprintln(cliOut)
	}
	return nil
}

// cliDepEdgeJSON is one dependency in the overview's --json: the upstream
// task the work flows from, the downstream task it flows to, the edge's kind,
// and the condition every hard edge carries.
type cliDepEdgeJSON struct {
	ID        int     `json:"id"`
	FromTask  string  `json:"from_task"`
	ToTask    string  `json:"to_task"`
	Kind      DepKind `json:"kind"`
	Condition string  `json:"condition"`
	Metadata  any     `json:"metadata"`
}

// cliDepEdges lists every dependency edge in admission order, numbered from
// one, the way the overview's --json carries them.
func cliDepEdges(st *Store) []cliDepEdgeJSON {
	out := []cliDepEdgeJSON{}
	n := 0
	for _, task := range st.Tasks() {
		if task.ID == st.RootID() {
			continue
		}
		for _, dep := range task.Dependencies {
			n++
			out = append(out, cliDepEdgeJSON{
				ID: n, FromTask: cliID(dep.TaskID), ToTask: cliID(task.ID),
				Kind: dep.Kind, Condition: "All", Metadata: nil,
			})
		}
	}
	return out
}

// cliNote leaves a task-scoped message for the workers around the same
// task; the author is recorded so an owner's handoff reads differently from
// a bystander's observation.
func cliNote(st *Store, p *cliParsed) error {
	if len(p.pos) < 3 {
		return errors.New(`note needs a task and the text — plandb task note <task-id> "what the next worker needs"`)
	}
	task, err := cliResolve(st, p.pos[1])
	if err != nil {
		return err
	}
	note, err := st.AddNote(task.ID, cliAgent(p), strings.Join(p.pos[2:], " "))
	if err != nil {
		return err
	}
	if p.bools["json"] {
		return cliPrintJSON(cliNoteObject(note))
	}
	fmt.Fprintf(cliOut, "noted %s (%s)\n", cliID(task.ID), note.ID)
	return nil
}

// cliNotes reads one task's notes back, in the order they were left.
func cliNotes(st *Store, p *cliParsed) error {
	if len(p.pos) < 2 {
		return errors.New(`notes needs a task — plandb task notes <task-id>`)
	}
	task, err := cliResolve(st, p.pos[1])
	if err != nil {
		return err
	}
	notes := st.Notes(task.ID, 0)
	if p.bools["json"] {
		out := make([]cliNoteJSON, 0, len(notes))
		for _, note := range notes {
			out = append(out, cliNoteObject(note))
		}
		return cliPrintJSON(out)
	}
	if len(notes) == 0 {
		fmt.Fprintln(cliOut, "(no notes)")
		return nil
	}
	for _, note := range notes {
		fmt.Fprintln(cliOut, cliNoteLine(note))
	}
	return nil
}

// cliNoteLine renders one note the one way both `task notes` and `show`
// print it: the person's note carries its `person:` prefix, and a worker's
// note is attributed to the agent that left it.
func cliNoteLine(note Note) string {
	switch {
	case note.From == NoteFromPerson:
		return fmt.Sprintf("  %s person: %s", note.ID, note.Body)
	case note.Agent != "":
		return fmt.Sprintf("  %s [%s] %s", note.ID, note.Agent, note.Body)
	default:
		return fmt.Sprintf("  %s %s", note.ID, note.Body)
	}
}

// cliTaskHold runs the runtime's and a person's hold verbs: `task pause`
// sets the status-independent flag, `task resume` clears it. They are not
// worker verbs — the bare supervisor refusal still stands for the lifecycle —
// so the store's own root guard and the task lookup are all the argument
// check that is needed.
func cliTaskHold(st *Store, p *cliParsed, pause bool) error {
	verb := "resume"
	if pause {
		verb = "pause"
	}
	if len(p.pos) < 2 {
		return fmt.Errorf("%s needs a task — plandb task %s <task-id>", verb, verb)
	}
	task, err := cliResolve(st, p.pos[1])
	if err != nil {
		return err
	}
	var held *Task
	if pause {
		held, err = st.Pause(task.ID)
	} else {
		held, err = st.Resume(task.ID)
	}
	if err != nil {
		return err
	}
	if p.bools["json"] {
		return cliPrintJSON(cliTaskObject(st, held))
	}
	done := "resumed"
	if pause {
		done = "paused"
	}
	fmt.Fprintf(cliOut, "%s %s\n", done, cliID(held.ID))
	return nil
}

// cliNoteJSON is one note as --json prints it: the note's own id, the task it
// hangs on spelled with its t- prefix, the agent that left it, its words and
// when it was left.
type cliNoteJSON struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	AgentID   string    `json:"agent_id,omitempty"`
	From      string    `json:"from,omitempty"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

func cliNoteObject(note Note) cliNoteJSON {
	return cliNoteJSON{
		ID: note.ID, TaskID: cliID(note.TaskID), AgentID: note.Agent,
		From: note.From, Content: note.Body, CreatedAt: note.At,
	}
}

// cliResolve answers one task for a word the model may have written loosely,
// and spells a miss as one plain sentence: `not found: task t-x`.
func cliResolve(st *Store, word string) (*Task, error) {
	task, err := st.Resolve(word)
	if err != nil {
		if strings.Contains(err.Error(), "no task matches") {
			return nil, fmt.Errorf("not found: task %s", cliID(strings.TrimPrefix(word, "t-")))
		}
		return nil, err
	}
	return task, nil
}

// cliID prints the store's bare id with its t- prefix — the CLI's spelling
// of every id it shows.
func cliID(id string) string { return "t-" + id }

// cliIcon is the status vocabulary: one mark per state, the same in every row.
func cliIcon(status Status) string {
	switch status {
	case StatusDone:
		return "✓"
	case StatusClaimed, StatusRunning:
		return "◉"
	case StatusReady:
		return "○"
	case StatusFailed:
		return "✗"
	case StatusCancelled:
		return "⊘"
	default:
		return "·"
	}
}

// cliCounts are the run's numbers with the root left out — the root is the
// run itself, claimed running by "runtime", and none of the doctrine's
// counts read it as work.
type cliCounts struct {
	total, pending, ready, running, done, failed, cancelled int
}

func cliCount(st *Store) cliCounts {
	var c cliCounts
	for _, task := range st.Tasks() {
		if task.ID == st.RootID() {
			continue
		}
		c.total++
		switch task.Status {
		case StatusPending:
			c.pending++
		case StatusReady:
			c.ready++
		case StatusClaimed, StatusRunning:
			c.running++
		case StatusDone:
			c.done++
		case StatusFailed:
			c.failed++
		case StatusCancelled:
			c.cancelled++
		}
	}
	return c
}

// cliBracket is the count line a claimed or finished task prints beside its
// own row: `✓ t-x done [1/2 · 3 ready · 0 blocked]`.
func cliBracket(st *Store) string {
	c := cliCount(st)
	return fmt.Sprintf("[%d/%d · %d ready · %d blocked]", c.done, c.total, c.ready, c.pending)
}

func cliReadyList(st *Store) string {
	var ids []string
	for _, task := range st.Tasks() {
		if task.ID != st.RootID() && task.Status == StatusReady {
			ids = append(ids, cliID(task.ID))
		}
	}
	if len(ids) == 0 {
		return "-"
	}
	return strings.Join(ids, ", ")
}

func cliRunningList(st *Store) string {
	var parts []string
	for _, task := range st.Tasks() {
		if task.ID == st.RootID() || (task.Status != StatusClaimed && task.Status != StatusRunning) {
			continue
		}
		agent := task.ClaimedBy
		if agent == "" {
			agent = "-"
		}
		parts = append(parts, cliID(task.ID)+"@"+agent)
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}

func cliPercent(done, total int) int {
	if total == 0 {
		return 0
	}
	return done * 100 / total
}

// cliProjectID derives the project's printed identity from its name — one
// store per file, so the name is the identity, spelled with the p- prefix
// every printed row carries.
func cliProjectID(name string) string {
	slug := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, name)
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "plan"
	}
	return "p-" + slug
}

// cliTaskJSON is the task as --json prints it: the fields this store
// carries, ids spelled with their t- prefix, and no field the store has no
// answer for.
type cliTaskJSON struct {
	ID           string        `json:"id"`
	ProjectID    string        `json:"project_id"`
	ParentTaskID *string       `json:"parent_task_id"`
	IsComposite  bool          `json:"is_composite"`
	Paused       bool          `json:"paused,omitempty"`
	Title        string        `json:"title"`
	Description  string        `json:"description"`
	Status       Status        `json:"status"`
	Role         string        `json:"role,omitempty"`
	Kind         string        `json:"kind"`
	Priority     int           `json:"priority"`
	Parallel     string        `json:"parallel,omitempty"`
	Isolation    string        `json:"isolation,omitempty"`
	Dependencies []cliDepJSON  `json:"dependencies,omitempty"`
	Checks       []string      `json:"checks,omitempty"`
	VerdictBasis *VerdictBasis `json:"verdict_basis,omitempty"`
	ClaimedBy    string        `json:"claimed_by,omitempty"`
	Result       string        `json:"result,omitempty"`
	Error        string        `json:"error,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	CompletedAt  *time.Time    `json:"completed_at,omitempty"`
	ArchivedAt   *time.Time    `json:"archived_at,omitempty"`
}

type cliDepJSON struct {
	TaskID string  `json:"task_id"`
	Kind   DepKind `json:"kind,omitempty"`
}

func cliTaskObject(st *Store, task *Task) *cliTaskJSON {
	out := &cliTaskJSON{
		ID:          cliID(task.ID),
		ProjectID:   cliProjectID(st.Project()),
		IsComposite: task.Composite,
		Paused:      task.Paused,
		Title:       task.Title,
		Description: task.Description,
		Status:      task.Status,
		Role:        roleOf(task),
		Kind:        task.Kind,
		Priority:    task.Priority,
		Parallel:    task.Parallel,
		Isolation:   task.Isolation,
		Checks:      append([]string(nil), task.Checks...),
		ClaimedBy:   task.ClaimedBy,
		Result:      task.Result,
		Error:       task.Error,
		CreatedAt:   task.CreatedAt,
		UpdatedAt:   task.UpdatedAt,
	}
	if task.ParentID != "" {
		parent := cliID(task.ParentID)
		out.ParentTaskID = &parent
	}
	for _, dep := range task.Dependencies {
		out.Dependencies = append(out.Dependencies, cliDepJSON{TaskID: cliID(dep.TaskID), Kind: dep.Kind})
	}
	if !task.CompletedAt.IsZero() {
		completed := task.CompletedAt
		out.CompletedAt = &completed
	}
	if !task.ArchivedAt.IsZero() {
		archived := task.ArchivedAt
		out.ArchivedAt = &archived
	}
	if task.VerdictBasis.Kind != "" {
		basis := task.VerdictBasis
		out.VerdictBasis = &basis
	}
	return out
}

// cliSplitJSON is the split answer as --json prints it: the created
// ids in part order, the title map the doctrine tells the model to read, and
// the effect the plan now runs under.
type cliSplitJSON struct {
	Created      []string          `json:"created"`
	Done         []string          `json:"done"`
	Effect       cliEffectJSON     `json:"effect"`
	ParentTaskID string            `json:"parent_task_id"`
	ProjectState cliProjectState   `json:"project_state"`
	TitleToID    map[string]string `json:"title_to_id"`
}

type cliEffectJSON struct {
	Accelerated  []string `json:"accelerated"`
	BlockedNow   []string `json:"blocked_now"`
	CriticalPath []string `json:"critical_path"`
	Delayed      []string `json:"delayed"`
	Depth        int      `json:"depth"`
	ReadyNow     []string `json:"ready_now"`
}

type cliProjectState struct {
	Done    int `json:"done"`
	Pending int `json:"pending"`
	Ready   int `json:"ready"`
	Running int `json:"running"`
	Total   int `json:"total"`
}

func cliSplitObject(st *Store, parent *Task, created []*Task) cliSplitJSON {
	out := cliSplitJSON{
		Created:      make([]string, 0, len(created)),
		Done:         []string{},
		Effect:       cliEffect(st, created, nil),
		ParentTaskID: cliID(parent.ID),
		ProjectState: cliProjectStateOf(st),
		TitleToID:    make(map[string]string, len(created)),
	}
	for _, task := range created {
		id := cliID(task.ID)
		out.Created = append(out.Created, id)
		out.TitleToID[task.Title] = id
	}
	return out
}

// cliInsertJSON is the insert answer as --json prints it: the new task's id,
// title and status, and the effect the inserted step had on the plan.
type cliInsertJSON struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	Status       Status          `json:"status"`
	Effect       cliEffectJSON   `json:"effect"`
	ProjectState cliProjectState `json:"project_state"`
}

func cliInsertObject(st *Store, task *Task) cliInsertJSON {
	return cliInsertJSON{
		ID: cliID(task.ID), Title: task.Title, Status: task.Status,
		Effect: cliEffect(st, []*Task{task}, nil), ProjectState: cliProjectStateOf(st),
	}
}

// cliEffect is the effect a mutation writes into its --json answer: the tasks
// it accelerated, the created ones it left blocked or ready now, the plan's
// critical path and its depth. delayed names tasks a mutation pushed out of
// the ready set, which the additive verbs pass none of.
func cliEffect(st *Store, created []*Task, delayed []string) cliEffectJSON {
	blocked := map[string]bool{}
	for _, bt := range st.ReadySet().Blocked {
		blocked[bt.Task.ID] = true
	}
	out := cliEffectJSON{
		Accelerated: []string{}, BlockedNow: []string{}, CriticalPath: []string{},
		Delayed: []string{}, ReadyNow: []string{},
	}
	for _, task := range created {
		id := cliID(task.ID)
		out.Accelerated = append(out.Accelerated, id)
		if task.Status == StatusReady {
			if blocked[task.ID] {
				out.BlockedNow = append(out.BlockedNow, id)
			} else {
				out.ReadyNow = append(out.ReadyNow, id)
			}
		}
	}
	for _, id := range delayed {
		out.Delayed = append(out.Delayed, cliID(id))
	}
	for _, task := range st.CriticalPath() {
		out.CriticalPath = append(out.CriticalPath, cliID(task.ID))
	}
	out.Depth = len(out.CriticalPath)
	return out
}

func cliProjectStateOf(st *Store) cliProjectState {
	c := cliCount(st)
	return cliProjectState{Done: c.done, Pending: c.pending, Ready: c.ready, Running: c.running, Total: c.total}
}

// cliSplitPart is one part as --into and --subtasks carry it.
type cliSplitPart struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	DepsOn      []string `json:"deps_on"`
}

// cliVerbHelp answers one verb's usage, or the whole surface for a word it
// does not know.
func cliVerbHelp(verb string) string {
	lines := map[string]string{
		"init":           `usage: plandb init NAME [--description TEXT] — create the run's store and its root task`,
		"add":            `usage: plandb add TITLE [--description TEXT] [--parent TASK_ID] [--dep TASK_ID[:KIND]]... [--as ID] [--kind K] [--priority N] [--role plan|work|check|probe] [--check COMMAND]...`,
		"split":          `usage: plandb split TASK_ID --into SPEC   (SPEC: JSON parts, "A, B", or "A > B > C")`,
		"go":             `usage: plandb go [--agent ID] — claim the highest-priority ready task for you`,
		"done":           `usage: plandb done [TASK_ID] --result TEXT [--agent ID] [--next]`,
		"wait":           `usage: plandb wait [TASK_ID] [--agent ID] — park the task until its wait is over: it is launched again once nothing it waited on is open any more (every dependency done, every child finished), or at once if one of them failed or was cancelled`,
		"archive":        `usage: plandb archive [--older-than 72h] — move old finished subtrees into the archive`,
		"list":           `usage: plandb list [--status STATUS] [--kind K] [--agent ID] [--project P] [--chat C] [--archived]`,
		"status":         `usage: plandb status [--full] — the one-line summary, or the containment tree with it`,
		"search":         `usage: plandb search QUERY [--limit N] [--project P] [--chat C]`,
		"context":        `usage: plandb context TEXT [--kind K] [--task TASK_ID]`,
		"contexts":       `usage: plandb contexts [--kind K] [--limit N] [--project P] [--chat C]`,
		"prune":          `usage: plandb prune CONTEXT_ID`,
		"critical-path":  `usage: plandb critical-path`,
		"bottlenecks":    `usage: plandb bottlenecks [--limit N]`,
		"show":           `usage: plandb show TASK_ID`,
		"spend":          `usage: plandb spend [--by chat|project|role|model|task] [--since 7d|24h|2026-09-01] — the ledger by role and by model, or rolled up under one axis`,
		"help":           `usage: plandb help`,
		"task":           `usage: plandb task <add-dep|amend|cancel|get|insert|note|notes|overview|pause|pivot|resume>`,
		"task add-dep":   `usage: plandb task add-dep DOWNSTREAM --after UPSTREAM [--kind feeds_into|blocks|suggests]`,
		"task amend":     `usage: plandb task amend TASK_ID --prepend TEXT`,
		"task cancel":    `usage: plandb task cancel TASK_ID`,
		"task get":       `usage: plandb task get TASK_ID`,
		"task insert":    `usage: plandb task insert --after A [--before B] --title T [--description D]`,
		"task note":      `usage: plandb task note TASK_ID TEXT`,
		"task notes":     `usage: plandb task notes TASK_ID`,
		"task pause":     `usage: plandb task pause TASK_ID — hold the task and its subtree out of the ready frontier`,
		"task resume":    `usage: plandb task resume TASK_ID — release the hold Pause set`,
		"task overview":  `usage: plandb task overview [--project P] [--chat C]`,
		"task pivot":     `usage: plandb task pivot TASK_ID --subtasks JSON [--keep-done]`,
		"what-if":        `usage: plandb what-if cancel TASK_ID`,
		"what-if cancel": `usage: plandb what-if cancel TASK_ID — the task, its descendants, its hard dependents`,
	}
	if line, ok := lines[verb]; ok {
		return line
	}
	return cliUsage()
}

// cliUsage is the whole surface, in the voice the doctrine teaches it.
func cliUsage() string {
	return `plandb ` + cliVersion + ` — the plan store on the bash belt (docs/design/plandb-cli/DESIGN.md)

usage: plandb [--db PATH] [--json] [-c] [--agent ID] [--project NAME] <verb> [args]

the loop:
  init NAME                 create the run's store and its root task
  add TITLE                 create a task; --description is the work order
  split [TASK_ID] --into    JSON parts, comma titles, or a "A > B > C" chain
  go                        claim the highest-priority ready task for you
  done [TASK_ID] --result   complete a task; --next claims the next

adapting the plan:
  task add-dep DOWNSTREAM --after UPSTREAM [--kind K]
  task amend TASK_ID --prepend TEXT
  task insert --after A [--before B] --title T [--description D]
  task pivot TASK_ID --subtasks JSON [--keep-done]
  task cancel TASK_ID       |  what-if cancel TASK_ID
  task note TASK_ID TEXT    |  task notes TASK_ID
  task pause TASK_ID        |  task resume TASK_ID

reading:
  show TASK_ID | task get TASK_ID | task overview [--project P] [--chat C]
  list [--status STATUS] [--kind K] [--agent ID] [--project P] [--chat C] [--archived]
  archive [--older-than 72h]  move old finished subtrees into the archive
  status [--full] | search QUERY [--limit N] [--project P] [--chat C] | critical-path | bottlenecks [--limit N]
  context TEXT [--kind K] [--task TASK_ID] | contexts [--kind K] [--limit N] [--project P] [--chat C] | prune CONTEXT_ID
  spend                     the ledger by role and by model, or --by chat|project|role|model|task [--since WHEN]

global flags:
  --db PATH      the store file (found by walking up when not given)
  --json         structured answers
  -c, --compact  accepted; compact is already the text shape
  --agent ID     who you are (default: PLANDB_AGENT, then "default")
  --project NAME accepted; one store per file, checked when given

the lifecycle is the runtime's, not yours: claim, start, fail, pause, next,
heartbeat, progress, approve, and the scope verbs use, project (beyond init),
mcp, serve, watch, events, ahead, artifact, export and import are refused
with the supervisor's own sentence and change nothing.`
}

package main

// standing.go is the terminal's door onto ongoing work: set it up, change it,
// pause it, stop it, look at what it did and why, and run one check now.
//
// ── WHY A SECOND DOOR EXISTS ──
//
// The conversation's door is a card: the model reads an ordinary sentence,
// proposes an item, and the person answers. That door is deliberately closed to
// every session nobody is watching (`aforge chat --once` removes it by law), so
// until this file there was no way to set up, change or inspect ongoing work
// without sitting in the chat surface — not from a script, not from a shell, not
// over ssh to a machine with no terminal UI.
//
// ── NOTHING STANDS WITHOUT THE PERSON, STILL ──
//
// Here the person writes the item WHOLE — the words, the brief, what wakes it,
// where its report goes — so there is no proposal to answer and no model's
// reading of their sentence to ratify. The command itself is the yes, and the
// item's adoption receipt says so (`via: terminal`, [standing.Adoption.Via]).
// Nothing in this file calls a model except `check`, which runs the same pass
// the operating system's timer runs.
//
// ── IT NEVER TOUCHES THE BACKGROUND TIMER ──
//
// The timer is one per login and names one home and one program. Installing it
// from a command — especially one pointed at a scratch AFORGE_HOME — would take
// it from whatever the person's real install had. So this door says how items
// get checked and leaves the timer to the settings row that owns it.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

const standingSummary = `  aforge standing [add|edit|show|stop|check]  ongoing work and its runs`

const standingUsage = `  aforge standing [list] [--json]       everything standing on this machine
  aforge standing add --words "<your sentence>" --instructions "<the work>"
      (--watch <glob> | --every <cron or 20m>) [--report <path>]
      [--workspace <dir>] [--place <folder-id>] [--acceptance "<done when>"]
      [--max-per-day n] [--per-run-usd n] [--model slug]
  aforge standing add --hold --words "<a rule>" [--scope <folder-id>]
      [--descendants] [--workspace <dir>]
  aforge standing edit <id> [--words ..] [--instructions ..] [--watch ..]
      [--every ..] [--report ..] [--acceptance ..] [--version n]
  aforge standing show <id> [--json] [--runs n]
      what it is, which rules reach it, what woke each run, what each made
  aforge standing pause|resume|stop <id>
  aforge standing check
      run one check of everything standing now, the pass the timer runs
      --watch is a glob inside the workspace, checked on every pass; a run
      starts only when a matching file was added, changed or removed.
      --report is a path inside the workspace the run's final reply is
      written to, replacing the last one. This door never turns on the
      background timer.`

// standingFlags is every flag the door reads. One set for every verb, the way
// `aforge collections` is written, with the verbs refusing what is not theirs.
type standingFlags struct {
	asJSON                            *bool
	words, instructions, watch, every *string
	report, acceptance, workspace     *string
	place, scope, model, title        *string
	hold, descendants                 *bool
	maxPerDay, runs                   *int
	perRunUSD                         *float64
	version                           *uint64
}

func runStanding(args []string) error { return runStandingTo(args, os.Stdout) }

func runStandingTo(args []string, out io.Writer) error {
	flags := commandFlags("standing")
	f := standingFlags{
		asJSON:       flags.Bool("json", false, "print structured records"),
		words:        flags.String("words", "", "your own sentence; every row leads with it"),
		instructions: flags.String("instructions", "", "the work one run does, self-contained: nobody is there to ask"),
		watch:        flags.String("watch", "", "a glob inside the workspace; a run starts when a matching file changes"),
		every:        flags.String("every", "", "a rhythm: a five-field cron line or a duration of at least a minute"),
		report:       flags.String("report", "", "a path inside the workspace the run's final reply is published to"),
		acceptance:   flags.String("acceptance", "", "how anybody checks one run's work is done"),
		workspace:    flags.String("workspace", "", "the folder the work runs in (default: this directory)"),
		place:        flags.String("place", "", "a folder id this work is placed in; that folder's rules then reach it"),
		scope:        flags.String("scope", "", "for --hold: the folder id whose placed work the rule governs"),
		model:        flags.String("model", "", "the model one run works on, when not the configured one"),
		title:        flags.String("title", "", "three or four words for a narrow row"),
		hold:         flags.Bool("hold", false, "a rule that never wakes: it rides into the work it reaches"),
		descendants:  flags.Bool("descendants", false, "for --scope: include work placed in subfolders"),
		maxPerDay:    flags.Int("max-per-day", standing.DefaultMaxPerDay, "the most runs in one local day"),
		runs:         flags.Int("runs", 10, "for show: how many runs of history to print"),
		perRunUSD:    flags.Float64("per-run-usd", standing.DefaultPerRunUSD, "the most one run may spend; 0 is only the daily limit"),
		version:      flags.Uint64("version", 0, "for edit: the instructions version you read; a newer one refuses the edit"),
	}
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	rest := flags.Args()
	if len(rest) == 0 {
		rest = []string{"list"}
	}
	verb := rest[0]
	want := map[string]int{"list": 1, "add": 1, "edit": 2, "show": 2, "pause": 2, "resume": 2, "stop": 2, "check": 1}
	if n, ok := want[verb]; !ok || len(rest) != n {
		return fmt.Errorf("usage: %s", strings.TrimLeft(standingUsage, " "))
	}
	set := map[string]bool{}
	flags.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	if err := standingFlagsFor(verb, set); err != nil {
		return err
	}
	store, err := standing.Open(v3StandingRoot())
	if err != nil {
		return err
	}
	switch verb {
	case "list":
		return standingList(out, store, *f.asJSON)
	case "add":
		return standingAdd(out, store, f)
	case "check":
		return standingCheck(out, store)
	}
	id, err := standingResolve(store, rest[1])
	if err != nil {
		return err
	}
	switch verb {
	case "show":
		return standingShow(out, store, id, *f.runs, *f.asJSON)
	case "edit":
		return standingEdit(out, store, id, f, set)
	case "pause":
		return standingControl(out, store, id, standing.StatusPaused)
	case "resume":
		return standingControl(out, store, id, standing.StatusActive)
	default:
		return standingControl(out, store, id, standing.StatusRetired)
	}
}

// standingFlagsFor refuses a flag that means nothing to the verb it was given
// with, by name, so a flag never quietly does nothing.
func standingFlagsFor(verb string, set map[string]bool) error {
	allowed := map[string][]string{
		"list":   {"json"},
		"add":    {"json", "words", "instructions", "watch", "every", "report", "acceptance", "workspace", "place", "scope", "model", "title", "hold", "descendants", "max-per-day", "per-run-usd"},
		"edit":   {"json", "words", "instructions", "watch", "every", "report", "acceptance", "max-per-day", "per-run-usd", "version", "title"},
		"show":   {"json", "runs"},
		"pause":  {"json"},
		"resume": {"json"},
		"stop":   {"json"},
		"check":  {"json"},
	}
	ok := map[string]bool{}
	for _, name := range allowed[verb] {
		ok[name] = true
	}
	var refused []string
	for name := range set {
		if !ok[name] {
			refused = append(refused, "--"+name)
		}
	}
	if len(refused) > 0 {
		sort.Strings(refused)
		return fmt.Errorf("aforge standing %s does not take %s", verb, strings.Join(refused, ", "))
	}
	return nil
}

// standingResolve takes an id or a unique beginning of one, because an id is
// sixteen hex characters and a person types the first few.
func standingResolve(store *standing.Store, typed string) (string, error) {
	typed = strings.TrimSpace(typed)
	if typed == "" {
		return "", errors.New("name an item by its id")
	}
	if _, err := store.Get(typed); err == nil {
		return typed, nil
	}
	items, err := store.List()
	if err != nil {
		return "", err
	}
	var found []string
	for _, item := range items {
		if strings.HasPrefix(item.ID, typed) {
			found = append(found, item.ID)
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no standing item %q here; aforge standing list shows them", typed)
	case 1:
		return found[0], nil
	}
	return "", fmt.Errorf("%q could be %s; type more of the id", typed, strings.Join(found, " or "))
}

// ── add ─────────────────────────────────────────────────────────────────────

func standingAdd(out io.Writer, store *standing.Store, f standingFlags) error {
	words := strings.TrimSpace(*f.words)
	if words == "" {
		return errors.New(`--words is required: your own sentence, which every row leads with`)
	}
	dir := strings.TrimSpace(*f.workspace)
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		dir = wd
	}
	dir, err := expandHome(dir)
	if err != nil {
		return err
	}
	if dir, err = filepath.Abs(dir); err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("the workspace %s is not a folder here", dir)
	}
	item := standing.Item{
		Words:     words,
		Workspace: dir,
		Brief:     standing.Brief{Title: strings.TrimSpace(*f.title)},
		Adoption:  &standing.Adoption{Actor: "person", Via: "terminal", At: time.Now().UTC()},
	}
	if *f.hold {
		if *f.watch != "" || *f.every != "" || *f.instructions != "" || *f.report != "" || *f.place != "" || *f.model != "" || *f.acceptance != "" {
			return errors.New("a rule (--hold) never wakes and runs nothing: it takes --words, --scope and --descendants only")
		}
		item.When = standing.When{Kind: standing.WhenHold}
		if scope := strings.TrimSpace(*f.scope); scope != "" {
			name, err := standingFolderName(scope)
			if err != nil {
				return err
			}
			item.Scope = &standing.Scope{CollectionIDs: []string{scope}, Descendants: *f.descendants}
			item.When.Words = "applies in " + name
			if *f.descendants {
				item.When.Words += " and its subfolders"
			} else {
				item.When.Words += " (directly placed work only)"
			}
		} else if *f.descendants {
			return errors.New("--descendants widens a --scope; name the folder with --scope")
		}
	} else {
		if *f.scope != "" || *f.descendants {
			return errors.New("--scope belongs to a rule (--hold); place work in a folder with --place")
		}
		if strings.TrimSpace(*f.instructions) == "" {
			return errors.New(`--instructions is required: the work one run does, written so nobody has to be asked`)
		}
		when, err := standingWhenFlags(*f.watch, *f.every)
		if err != nil {
			return err
		}
		item.When = when
		item.Does = standing.Action{
			Kind:       standing.ActionTask,
			Brief:      strings.TrimSpace(*f.instructions),
			Acceptance: strings.TrimSpace(*f.acceptance),
			Model:      strings.TrimSpace(*f.model),
			Report:     strings.TrimSpace(*f.report),
		}
		item.Rails = standing.Rails{PerRunUSD: *f.perRunUSD, MaxPerDay: *f.maxPerDay}
	}
	// THE FOLDER IS CHECKED BEFORE ANYTHING STANDS, so a mistyped id is a
	// refusal rather than an item that stands in no folder at all.
	place := strings.TrimSpace(*f.place)
	if place != "" {
		if _, err := standingFolderName(place); err != nil {
			return err
		}
	}
	if err := item.Validate(); err != nil {
		return err
	}
	created, err := store.Create(item)
	if err != nil {
		return err
	}
	_ = store.Log(created.ID, "set up at the terminal")
	if place != "" {
		if err := standingPlace(place, created.ID); err != nil {
			return fmt.Errorf("%s stands, but could not be placed in %s: %w", created.ID, place, err)
		}
	}
	if *f.asJSON {
		return json.NewEncoder(out).Encode(created)
	}
	fmt.Fprintf(out, "set up %s: %s\n", created.ID, created.Words)
	if line := standingWakesLine(created); line != "" {
		fmt.Fprintln(out, line)
	}
	if created.Does.Report != "" {
		fmt.Fprintln(out, "report: "+filepath.Join(created.Workspace, created.Does.Report))
	}
	if place != "" {
		fmt.Fprintln(out, "placed in folder "+place+"; its rules reach this work")
	}
	if created.When.Kind != standing.WhenHold {
		fmt.Fprintln(out, "checked on every pass of an open window or the background timer; aforge standing check runs one now")
	}
	return nil
}

// standingWhenFlags turns --watch or --every into what wakes an item. Exactly
// one, because an item has one waking kind.
func standingWhenFlags(watch, every string) (standing.When, error) {
	watch, every = strings.TrimSpace(watch), strings.TrimSpace(every)
	switch {
	case watch != "" && every != "":
		return standing.When{}, errors.New("choose --watch or --every, not both: an item has one thing that wakes it")
	case watch != "":
		if filepath.IsAbs(watch) {
			return standing.When{}, errors.New("--watch is a glob inside the workspace, like inbox/*")
		}
		if _, err := filepath.Match(watch, "probe"); err != nil {
			return standing.When{}, fmt.Errorf("--watch %q is not a pattern this can read: %w", watch, err)
		}
		return standing.When{Kind: standing.WhenFile, Glob: watch, Words: "when " + watch + " changes"}, nil
	case every != "":
		if _, err := standing.ParseEvery(every); err != nil {
			return standing.When{}, err
		}
		return standing.When{Kind: standing.WhenEvery, Every: every, Words: "every " + every}, nil
	}
	return standing.When{}, errors.New("say what wakes it: --watch <glob> or --every <rhythm>")
}

// standingOrganization opens the folder database, or answers that there are no
// folders here yet — which is a sentence, not a crash, on a fresh home.
func standingOrganization() (*workspace.Store, error) {
	path := home.Join("v3", "collections.db")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("there are no folders here yet; aforge collections create <name> makes one")
	}
	return workspace.Open(path)
}

func standingFolderName(id string) (string, error) {
	if err := (workspace.Ref{Kind: workspace.CollectionKind, ID: id}).Validate(); err != nil {
		return "", err
	}
	org, err := standingOrganization()
	if err != nil {
		return "", err
	}
	defer org.Close()
	folders, err := org.Collections(context.Background())
	if err != nil {
		return "", err
	}
	for _, folder := range folders {
		if folder.ID == id {
			return folder.Name, nil
		}
	}
	return "", fmt.Errorf("no folder %s here; aforge collections lists them", id)
}

// standingPlace is the governing placement, the same relation the chat's
// place verb writes: a reference would only file it, a placement is what makes
// that folder's rules reach the work.
func standingPlace(folder, id string) error {
	org, err := standingOrganization()
	if err != nil {
		return err
	}
	defer org.Close()
	return org.AddPlacement(context.Background(), folder, workspace.Ref{Kind: workspace.StandingKind, ID: id})
}

// ── edit ────────────────────────────────────────────────────────────────────

func standingEdit(out io.Writer, store *standing.Store, id string, f standingFlags, set map[string]bool) error {
	current, err := store.Get(id)
	if err != nil {
		return err
	}
	expected := current.SpecRevision
	if set["version"] {
		expected = *f.version
	}
	revised, changed, err := store.Revise(id, expected, func(item *standing.Item) error {
		if set["words"] {
			item.Words = strings.TrimSpace(*f.words)
		}
		if set["title"] {
			item.Brief.Title = strings.TrimSpace(*f.title)
		}
		if set["instructions"] {
			if item.When.Kind == standing.WhenHold {
				item.Brief.Prompt = strings.TrimSpace(*f.instructions)
			} else {
				item.Does.Brief = strings.TrimSpace(*f.instructions)
			}
		}
		if set["acceptance"] {
			item.Does.Acceptance = strings.TrimSpace(*f.acceptance)
		}
		if set["report"] {
			item.Does.Report = strings.TrimSpace(*f.report)
		}
		if set["watch"] || set["every"] {
			if item.When.Kind == standing.WhenHold {
				return errors.New("a rule never wakes; set up work with aforge standing add instead")
			}
			when, err := standingWhenFlags(*f.watch, *f.every)
			if err != nil {
				return err
			}
			item.When = when
		}
		if set["max-per-day"] {
			item.Rails.MaxPerDay = *f.maxPerDay
		}
		if set["per-run-usd"] {
			item.Rails.PerRunUSD = *f.perRunUSD
		}
		return nil
	})
	switch {
	case errors.Is(err, standing.ErrConflict):
		return fmt.Errorf("the instructions changed since version %d (now %d); read them again with aforge standing show %s", expected, current.SpecRevision, id)
	case errors.Is(err, standing.ErrUnchanged):
		return errors.New("nothing to change: name what is different with a flag")
	case err != nil:
		return err
	}
	_ = store.Log(id, fmt.Sprintf("revised at the terminal to version %d: %s", revised.SpecRevision, strings.Join(changed, ", ")))
	if *f.asJSON {
		return json.NewEncoder(out).Encode(revised)
	}
	fmt.Fprintf(out, "revised %s to version %d: %s\n", id, revised.SpecRevision, strings.Join(changed, ", "))
	fmt.Fprintln(out, "the next run uses it; a run already under way keeps what it started with")
	return nil
}

// ── controls ────────────────────────────────────────────────────────────────

func standingControl(out io.Writer, store *standing.Store, id string, status standing.Status) error {
	reason := ""
	if status == standing.StatusRetired {
		reason = standing.StoppedWhy
	}
	item, err := store.SetStatus(id, status, reason)
	if err != nil {
		return err
	}
	word := map[standing.Status]string{standing.StatusPaused: "paused", standing.StatusActive: "resumed", standing.StatusRetired: "stopped"}[status]
	_ = store.Log(id, word+" at the terminal")
	fmt.Fprintf(out, "%s · %s\n", word, item.Words)
	switch status {
	case standing.StatusPaused:
		fmt.Fprintln(out, "not checked and not run until resumed; what it knows is kept")
	case standing.StatusRetired:
		fmt.Fprintln(out, "it will not run again; its history stays in aforge standing show "+id)
	}
	return nil
}

// ── check ───────────────────────────────────────────────────────────────────

// standingCheck runs ONE pass through the same constructor the timer and every
// window use ([v3StandingTicker]), so what runs here is what runs unattended.
func standingCheck(out io.Writer, store *standing.Store) error {
	// THE PASS'S HOUSEKEEPING GOES TO THE STANDING LOG, not onto this screen:
	// the catalog and media resolvers log through the standard logger while
	// the pass is assembled, and those lines are about this machine's model
	// rows rather than about anything the person asked this command.
	if file, err := os.OpenFile(home.Join("v3", standingLogName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		defer file.Close()
		log.SetOutput(file)
		defer log.SetOutput(os.Stderr)
	}
	ticker, err := v3StandingTicker(store)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), standing.TickWindow)
	defer cancel()
	pass, err := ticker.Tick(ctx)
	if errors.Is(err, standing.ErrHeld) {
		fmt.Fprintln(out, "an aforge window is already checking; nothing was run here")
		return nil
	}
	if err != nil {
		return err
	}
	var parts []string
	for _, part := range []struct {
		n    int
		word string
	}{{pass.Checked, "checked"}, {pass.Fired, "ran"}, {pass.NeedsYou, "need you"}, {pass.Skipped, "held back"}, {pass.Errors, "could not check"}} {
		if part.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", part.n, part.word))
		}
	}
	if len(parts) == 0 {
		fmt.Fprintln(out, "nothing was due")
	} else {
		fmt.Fprintln(out, strings.Join(parts, " · "))
	}
	for _, note := range pass.Notes {
		fmt.Fprintln(out, "  "+note)
	}
	return nil
}

// ── list and show ───────────────────────────────────────────────────────────

func standingList(out io.Writer, store *standing.Store, asJSON bool) error {
	items, err := store.List()
	if err != nil {
		return err
	}
	if asJSON {
		if items == nil {
			items = []standing.Item{}
		}
		return json.NewEncoder(out).Encode(items)
	}
	if len(items) == 0 {
		fmt.Fprintln(out, "Nothing stands here. aforge standing add sets something up.")
		return nil
	}
	for _, item := range items {
		fmt.Fprintf(out, "%s  %-7s  %s\n", item.ID, item.Status, item.Words)
		if line := standingWakesLine(item); line != "" {
			fmt.Fprintln(out, "    "+line)
		}
		if item.Runs > 0 {
			fmt.Fprintf(out, "    ran %d · last %s %s\n", item.Runs, item.LastOutcome, item.LastFired.Local().Format("Jan 2 15:04"))
		}
	}
	return nil
}

// standingWakesLine says what wakes an item, in its own words.
func standingWakesLine(item standing.Item) string {
	words := strings.TrimSpace(item.When.Words)
	if item.When.Kind == standing.WhenHold {
		if words == "" {
			return "a rule: it rides into the work it reaches"
		}
		return "a rule: " + words
	}
	if words == "" {
		return ""
	}
	return "wakes " + words
}

// standingRecord is show's structured form: the item, where it is placed, the
// rules that reach it now, and its runs with their causes and effects.
type standingRecord struct {
	Item        standing.Item                   `json:"item"`
	Placements  []workspace.GoverningCollection `json:"placements,omitempty"`
	Rules       []standingRule                  `json:"rules,omitempty"`
	Occurrences []standingRun                   `json:"occurrences,omitempty"`
	Log         []string                        `json:"log,omitempty"`
	RulesError  string                          `json:"rulesError,omitempty"`
}

type standingRule struct {
	ID    string          `json:"id"`
	Words string          `json:"words"`
	Scope *standing.Scope `json:"scope,omitempty"`
	Spec  uint64          `json:"spec,omitempty"`
}

type standingRun struct {
	standing.Occurrence
	RunDir  string `json:"runDir"`
	Journal string `json:"journal,omitempty"`
	CameTo  string `json:"cameTo,omitempty"`
}

func standingShow(out io.Writer, store *standing.Store, id string, limit int, asJSON bool) error {
	item, err := store.Get(id)
	if err != nil {
		return err
	}
	record := standingRecord{Item: item}
	collections := map[string]int{}
	if org, err := standingOrganization(); err == nil {
		places, err := org.GoverningCollections(context.Background(), workspace.Ref{Kind: workspace.StandingKind, ID: id})
		org.Close()
		if err != nil {
			record.RulesError = err.Error()
		}
		record.Placements = places
		for _, place := range places {
			collections[place.ID] = place.Depth
		}
	}
	if record.RulesError == "" {
		rules, err := store.ApplicableScope(item.Workspace, "", collections)
		if err != nil {
			record.RulesError = err.Error()
		}
		for _, rule := range rules {
			if rule.When.Kind != standing.WhenHold || rule.ID == id {
				continue
			}
			record.Rules = append(record.Rules, standingRule{ID: rule.ID, Words: rule.Prompt(), Scope: rule.Scope, Spec: rule.SpecRevision})
		}
	}
	occurrences, err := store.Occurrences(id, limit)
	if err != nil {
		return err
	}
	for _, occurrence := range occurrences {
		run := standingRun{Occurrence: occurrence, RunDir: occurrence.RunDir}
		if _, err := os.Stat(filepath.Join(occurrence.RunDir, "transcript.jsonl")); err == nil {
			run.Journal = filepath.Join(occurrence.RunDir, "transcript.jsonl")
		}
		if raw, err := os.ReadFile(filepath.Join(occurrence.RunDir, standing.CameTo)); err == nil {
			run.CameTo = strings.TrimSpace(string(raw))
		}
		record.Occurrences = append(record.Occurrences, run)
	}
	if raw, err := os.ReadFile(store.LogPath(id)); err == nil {
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		if len(lines) > 12 {
			lines = lines[len(lines)-12:]
		}
		record.Log = lines
	}
	if asJSON {
		return json.NewEncoder(out).Encode(record)
	}
	return writeStandingRecord(out, record)
}

func writeStandingRecord(out io.Writer, record standingRecord) error {
	item := record.Item
	fmt.Fprintf(out, "%s  %s  %s\n", item.ID, item.Status, item.Words)
	if item.Status == standing.StatusRetired && item.RetiredWhy != "" {
		fmt.Fprintln(out, "  retired: "+item.RetiredWhy)
	}
	if line := standingWakesLine(item); line != "" {
		fmt.Fprintln(out, "  "+line)
	}
	fmt.Fprintln(out, "  workspace: "+item.Workspace)
	if item.Does.Kind == standing.ActionTask {
		fmt.Fprintln(out, "  instructions: "+oneLineOf(item.Does.Brief))
		if item.Does.Report != "" {
			fmt.Fprintln(out, "  report: "+item.Does.Report)
		}
	}
	if item.SpecRevision > 0 {
		fmt.Fprintf(out, "  instructions: version %d\n", item.SpecRevision)
	}
	if item.Adoption != nil {
		via := "a card in a conversation"
		if item.Adoption.Via != "" {
			via = "the " + item.Adoption.Via
		}
		fmt.Fprintf(out, "  set up by: %s, through %s\n", item.Adoption.Actor, via)
	}
	for _, place := range record.Placements {
		how := "placed directly"
		if place.Depth > 0 {
			how = fmt.Sprintf("placed %d folder(s) below", place.Depth)
		}
		fmt.Fprintf(out, "  folder: %s %s (%s)\n", place.ID, place.Name, how)
	}
	for _, rule := range record.Rules {
		fmt.Fprintf(out, "  rule reaching it: %s %s\n", rule.ID, oneLineOf(rule.Words))
	}
	if record.RulesError != "" {
		fmt.Fprintln(out, "  rules could not be read: "+record.RulesError)
	}
	if item.Runs > 0 {
		line := fmt.Sprintf("  ran %d time(s); last came to %s", item.Runs, item.LastOutcome)
		if item.SpentUSD > 0 {
			line += fmt.Sprintf(" · spent $%.4f", item.SpentUSD)
		}
		fmt.Fprintln(out, line)
	}
	if len(record.Occurrences) > 0 {
		fmt.Fprintln(out, "runs, newest first:")
	}
	for _, run := range record.Occurrences {
		head := fmt.Sprintf("  %s  %s", filepath.Base(run.RunDir), run.Phase)
		if run.Outcome != "" {
			head += " · " + run.Outcome
		}
		if run.Spec > 0 {
			head += fmt.Sprintf(" · instructions v%d", run.Spec)
		}
		if run.Attempt > 1 {
			head += fmt.Sprintf(" · attempt %d", run.Attempt)
		}
		head += " · " + run.Admitted.Local().Format("Jan 2 15:04:05")
		fmt.Fprintln(out, head)
		if run.Because != "" {
			fmt.Fprintln(out, "      woken: "+run.Because)
		}
		if run.ChangesUnknown {
			fmt.Fprintln(out, "      changed: unknown (no earlier reading)")
		}
		for _, change := range run.Changes {
			fmt.Fprintf(out, "      %s %s\n", change.Kind, change.Path)
		}
		for _, superseded := range run.Supersedes {
			fmt.Fprintln(out, "      retries interrupted run "+filepath.Base(superseded))
		}
		if run.SupersededBy != "" {
			fmt.Fprintln(out, "      interrupted; retried as "+run.SupersededBy)
		}
		if run.USD > 0 {
			fmt.Fprintf(out, "      cost $%.4f\n", run.USD)
		}
		if run.Published != nil {
			fmt.Fprintf(out, "      published %s (%d bytes, sha256 %s)\n", run.Published.Path, run.Published.Bytes, shortHash(run.Published.SHA256))
		}
		if run.Error != "" {
			fmt.Fprintln(out, "      error: "+run.Error)
		}
		if run.Journal != "" {
			fmt.Fprintln(out, "      journal: "+run.Journal)
		}
	}
	if len(record.Log) > 0 {
		fmt.Fprintln(out, "log:")
		for _, line := range record.Log {
			fmt.Fprintln(out, "  "+line)
		}
	}
	return nil
}

func oneLineOf(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 160 {
		return text[:160] + "…"
	}
	return text
}

func shortHash(sum string) string {
	if len(sum) > 12 {
		return sum[:12]
	}
	return sum
}

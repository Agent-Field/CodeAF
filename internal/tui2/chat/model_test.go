package chat

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/modelui"
	"github.com/Agent-Field/aforge-v2/internal/tui2/settings"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// -- the fakes ---------------------------------------------------------------

// rolesBackend is a store that answers the roles table. It records the order of
// its writes, because the order is a law: the binding is durable and the funnel
// is not, so the storage is written first (see setRoleResult).
type rolesBackend struct {
	fakeBackend
	bound  map[store.ModelRole]string
	source map[store.ModelRole]store.RoleSource
	writes []string
	setErr error
}

func newRolesBackend() *rolesBackend {
	return &rolesBackend{
		bound:  map[store.ModelRole]string{},
		source: map[store.ModelRole]store.RoleSource{},
	}
}

func (r *rolesBackend) ResolveRole(role store.ModelRole, _ string) (store.ResolvedRole, error) {
	source, ok := r.source[role]
	if !ok {
		source = store.RoleUnbound
	}
	return store.ResolvedRole{Role: role, Model: r.bound[role], Source: source}, nil
}

func (r *rolesBackend) RoleBindingAt(role store.ModelRole, scope store.BindingScope) (store.RoleBinding, bool, error) {
	value, ok := r.bound[role]
	return store.RoleBinding{Role: role, Scope: scope, Value: value}, ok, nil
}

func (r *rolesBackend) SetRoleBinding(role store.ModelRole, _ store.BindingScope, value, origin string) (bool, error) {
	if r.setErr != nil {
		return false, r.setErr
	}
	r.writes = append(r.writes, "bind "+string(role)+"="+value+" by "+origin)
	if r.bound[role] == value {
		return false, nil
	}
	r.bound[role] = value
	r.source[role] = store.RoleFromGlobal
	return true, nil
}

func (r *rolesBackend) ClearRoleBinding(role store.ModelRole, _ store.BindingScope, origin string) (bool, error) {
	r.writes = append(r.writes, "clear "+string(role)+" by "+origin)
	if _, ok := r.bound[role]; !ok {
		return false, nil
	}
	delete(r.bound, role)
	delete(r.source, role)
	return true, nil
}

// modelCommander is an engine that can be re-pointed. It writes into the same
// ledger the backend does, so a test can assert that the storage was written
// before the funnel was asked.
type modelCommander struct {
	fakeCommander
	ledger *[]string
	models []string
	err    error
}

func (m *modelCommander) SetModel(slot, slug string) error {
	if m.err != nil {
		return m.err
	}
	if m.ledger != nil {
		*m.ledger = append(*m.ledger, "funnel "+slot+"="+slug)
	}
	return nil
}

func (m *modelCommander) Models() []string { return m.models }

// modelApp is a window with a roles table and an engine that can be re-pointed.
func modelApp(t *testing.T) (*App, *rolesBackend, *modelCommander) {
	t.Helper()
	backend := newRolesBackend()
	commander := &modelCommander{
		fakeCommander: fakeCommander{model: "anthropic/claude-k3", window: 200000},
		ledger:        &backend.writes,
		models:        []string{"anthropic/claude-k3", "openai/gpt-oss-120b"},
	}
	return newTestApp(backend, commander, nil), backend, commander
}

// run drives one command to its message, the way the runtime would.
func run(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command")
	}
	return cmd()
}

// 5.10 and 5.23's chip grammar, on the one line that used to spell a model by
// hand: role word, model word, and the effort that rides the slug. The vendor
// prefix and the release stamp are provenance and must not reach a cell.
func TestTheMetaStripSpeaksTheChipGrammar(t *testing.T) {
	strip := &metaStrip{
		style: tokens.NewStyler(tokens.NoColor, tokens.FocusNormal),
		model: "anthropic/claude-sonnet-4-20250514:high",
	}
	row := ansi.Strip(strip.render(60))
	for _, want := range []string{store.RoleOrchestrate.Word(), "claude-sonnet-4", "high"} {
		if !strings.Contains(row, want) {
			t.Fatalf("the meta strip's chip does not say %q: %q", want, row)
		}
	}
	for _, refused := range []string{"anthropic/", "20250514"} {
		if strings.Contains(row, refused) {
			t.Fatalf("provenance reached the chip (%q): %q", refused, row)
		}
	}
}

// One word, everywhere. The chip, a reply header's meta cell and the receipt a
// model switch posts all go through the same shortening, so a reader never sees
// one model under two names.
func TestOneModelWordForTheChipAndTheHeader(t *testing.T) {
	const slug = "anthropic/claude-sonnet-4-20250514:high"
	if got := modelWord(slug); got != "claude-sonnet-4" {
		t.Fatalf("modelWord(%q) = %q", slug, got)
	}
	strip := &metaStrip{model: slug}
	if got := strip.chip().Model; got != slug {
		t.Fatalf("the chip was handed %q rather than the slug it must shorten itself", got)
	}
}

// -- the five duties ----------------------------------------------------------

// Duties 1 and 2 (modelui/result.go), in the order they must happen: the roles
// table is the storage and is written first, then the command funnel is asked
// to move what is already running. A funnel asked before the durable answer
// existed could leave the work on one model and the table saying another.
func TestChoosingAModelWritesTheTableThenAsksTheFunnel(t *testing.T) {
	app, backend, _ := modelApp(t)
	msg := run(t, app.chooseModel(modelui.SetRole{
		Role: store.RoleWork, ModelSlug: "openai/gpt-oss-120b", Scope: store.ScopeGlobal,
	}))
	result, ok := msg.(modelResultMsg)
	if !ok {
		t.Fatalf("chooseModel answered %#v", msg)
	}
	if result.err != nil {
		t.Fatalf("the write failed: %v", result.err)
	}
	if !result.changed || !result.moved {
		t.Fatalf("the work slot neither bound nor moved: %#v", result)
	}
	want := []string{
		"bind work=openai/gpt-oss-120b by " + modelChipOrigin,
		"funnel work=openai/gpt-oss-120b",
	}
	if len(backend.writes) != 2 || backend.writes[0] != want[0] || backend.writes[1] != want[1] {
		t.Fatalf("writes = %q, want %q", backend.writes, want)
	}
}

// A chip is a person, so the binding it writes is a person's — never a seed,
// which exists for initializers that may not clobber a human choice.
func TestTheChipWritesAsAPersonAndNeverAsASeed(t *testing.T) {
	if strings.HasPrefix(modelChipOrigin, store.RoleSeedOriginPrefix) {
		t.Fatalf("the chip's origin %q is a seed origin", modelChipOrigin)
	}
}

// Duty 5, and 5.20 rule 5's other direction: the receipt is evidence of the
// journal. It lands only after the write returned, it lands as a durable system
// row in the room, and it says 5.10's sentence only for the slot whose change
// actually asked the funnel to move running work.
func TestTheReceiptFollowsTheJournalAndNotTheKeystroke(t *testing.T) {
	app, backend, _ := modelApp(t)
	before := len(backend.posted)

	// The work slot: work already running is asked to move.
	msg := run(t, app.chooseModel(modelui.SetRole{
		Role: store.RoleWork, ModelSlug: "openai/gpt-oss-120b", Scope: store.ScopeGlobal,
	})).(modelResultMsg)
	if len(backend.posted) != before {
		t.Fatal("a receipt was posted before the result was folded in")
	}
	run(t, app.applyModelResult(msg))
	if len(backend.posted) != before+1 {
		t.Fatalf("no receipt landed: %d posts", len(backend.posted))
	}
	receipt := backend.posted[len(backend.posted)-1]
	if receipt.Role != store.RoleSystem {
		t.Fatalf("the receipt is a %q row, not a system one", receipt.Role)
	}
	if want := "switching remaining work to gpt-oss-120b"; receipt.Body != want {
		t.Fatalf("receipt = %q, want %q", receipt.Body, want)
	}

	// The voice: read at the next call, with nothing in flight to move. The
	// stronger sentence would be describing a mutation that has not happened.
	voice := run(t, app.chooseModel(modelui.SetRole{
		Role: store.RoleOrchestrate, ModelSlug: "openai/gpt-oss-120b", Scope: store.ScopeGlobal,
	})).(modelResultMsg)
	run(t, app.applyModelResult(voice))
	receipt = backend.posted[len(backend.posted)-1]
	if strings.Contains(receipt.Body, "remaining work") {
		t.Fatalf("the voice claimed it moved running work: %q", receipt.Body)
	}
	for _, want := range []string{store.RoleOrchestrate.Word(), "gpt-oss-120b", "next call"} {
		if !strings.Contains(receipt.Body, want) {
			t.Fatalf("receipt %q does not say %q", receipt.Body, want)
		}
	}
}

// A write that changed nothing is not a mutation, so it gets no receipt: the
// role already ran on this, and a row saying otherwise would be a claim with
// nothing behind it.
func TestAnUnchangedBindingPostsNoReceipt(t *testing.T) {
	app, backend, _ := modelApp(t)
	first := run(t, app.chooseModel(modelui.SetRole{
		Role: store.RoleWork, ModelSlug: "openai/gpt-oss-120b", Scope: store.ScopeGlobal,
	})).(modelResultMsg)
	run(t, app.applyModelResult(first))
	posted := len(backend.posted)

	again := run(t, app.chooseModel(modelui.SetRole{
		Role: store.RoleWork, ModelSlug: "openai/gpt-oss-120b", Scope: store.ScopeGlobal,
	})).(modelResultMsg)
	if again.changed {
		t.Fatal("re-binding the same model reported a change")
	}
	if cmd := app.applyModelResult(again); cmd != nil {
		t.Fatal("an unchanged binding produced a receipt command")
	}
	if len(backend.posted) != posted {
		t.Fatalf("an unchanged binding posted a receipt: %d → %d", posted, len(backend.posted))
	}
}

// A failed write posts nothing and says so. The surface never claims a mutation
// it did not perform (5.20 rule 5, modelui/result.go duty 5).
func TestAFailedWritePostsNoReceipt(t *testing.T) {
	app, backend, _ := modelApp(t)
	backend.setErr = errors.New("the table is locked")
	msg := run(t, app.chooseModel(modelui.SetRole{
		Role: store.RoleWork, ModelSlug: "openai/gpt-oss-120b", Scope: store.ScopeGlobal,
	})).(modelResultMsg)
	if cmd := app.applyModelResult(msg); cmd != nil {
		t.Fatal("a failed write produced a receipt command")
	}
	if len(backend.posted) != 0 {
		t.Fatalf("a failed write posted %d rows", len(backend.posted))
	}
	if !strings.Contains(app.status.err, "locked") {
		t.Fatalf("the failure did not reach the status line: %q", app.status.err)
	}
}

// Clearing is its own shape and its own sentence: an unbound scope inherits,
// which is a different fact from a scope bound to nothing.
func TestClearingSaysTheWiderScopeAnswersAgain(t *testing.T) {
	app, backend, _ := modelApp(t)
	set := run(t, app.chooseModel(modelui.SetRole{
		Role: store.RolePlan, ModelSlug: "openai/gpt-oss-120b", Scope: store.ScopeGlobal,
	})).(modelResultMsg)
	run(t, app.applyModelResult(set))

	cleared := run(t, app.chooseModel(modelui.ClearRole{
		Role: store.RolePlan, Scope: store.ScopeGlobal,
	})).(modelResultMsg)
	if !cleared.cleared || !cleared.changed {
		t.Fatalf("the clear did not land: %#v", cleared)
	}
	run(t, app.applyModelResult(cleared))
	body := backend.posted[len(backend.posted)-1].Body
	if !strings.Contains(body, store.RolePlan.Word()) || !strings.Contains(body, "wider scope") {
		t.Fatalf("the clear receipt does not say what happened: %q", body)
	}
}

// -- 5.20 rule 3: the reasons ---------------------------------------------------

// The three altitudes, in this surface's own words. modelui refuses to invent
// any of them because only the wiring knows whether a binding can take effect.
func TestTheDisabledReasonsAreThisSurfacesWords(t *testing.T) {
	app, backend, _ := modelApp(t)
	catalog := app.modelCatalog()
	if catalog.Disabled != "" {
		t.Fatalf("a live window disabled its whole palette: %q", catalog.Disabled)
	}

	// A role with no consumer says so rather than offering a binding nothing
	// would read (5.22 rule 5).
	for _, role := range []store.ModelRole{store.RoleVerify, store.RoleScribe} {
		row := rowFor(t, catalog, role)
		if row.Disabled == "" {
			t.Fatalf("%s offers a binding no caller reads", role)
		}
	}
	for _, role := range []store.ModelRole{store.RoleOrchestrate, store.RolePlan, store.RoleWork} {
		if row := rowFor(t, catalog, role); row.Disabled != "" {
			t.Fatalf("%s is disabled on a live window: %q", role, row.Disabled)
		}
	}

	// A pin outranks a binding, so the row refuses the keystroke and says why.
	backend.source[store.RoleWork] = store.RoleFromPin
	backend.bound[store.RoleWork] = "anthropic/claude-k3"
	if row := rowFor(t, app.modelCatalog(), store.RoleWork); !strings.Contains(row.Disabled, "pin") {
		t.Fatalf("a pinned role does not say so: %q", row.Disabled)
	}

	// A visitor cannot reach the head that would have to honour the switch.
	app.residency = Residency{Visitor: true}
	if reason := app.modelCatalog().Disabled; !strings.Contains(reason, "visitor") {
		t.Fatalf("a visitor window's palette is live: %q", reason)
	}
}

// A window with no roles table says so on every row rather than accepting
// keystrokes that would write nowhere.
func TestAWindowWithNoRolesTableSaysSo(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	if reason := app.modelCatalog().Disabled; reason == "" {
		t.Fatal("a window with no roles table offered a live palette")
	}
}

// -- the doors ------------------------------------------------------------------

// The registry lists a model door; this surface used to list it and open
// nothing (5.22 rule 5, from the other side).
func TestTheRegistrysModelEntryOpensThePalette(t *testing.T) {
	app, _, _ := modelApp(t)
	app.runEntry(modelEntryID)
	if app.overlay != overlayModel {
		t.Fatalf("the model entry raised overlay %d", app.overlay)
	}
	if app.models == nil || app.models.Level() != modelui.LevelRoles {
		t.Fatal("the palette did not open at the role level")
	}
}

// The settings sheet's model rows are the one kind it does not edit itself.
// A role slot opens the palette live; a capability slot opens it saying why the
// five roles do not govern it (5.23: vision is a flag within a role, never a
// sixth role).
func TestTheSettingsSheetHandsOffTheModelRows(t *testing.T) {
	app, _, _ := modelApp(t)
	app.openModelSlot(settings.ModelMsg{Slot: "work"})
	if app.overlay != overlayModel {
		t.Fatalf("a work row raised overlay %d", app.overlay)
	}

	app.openModelSlot(settings.ModelMsg{Slot: "image"})
	reason := app.modelCatalog().Disabled
	if reason != "" {
		t.Fatalf("the window itself is disabled (%q); this test proves nothing", reason)
	}
	if app.overlay != overlayModel {
		t.Fatalf("a capability row raised overlay %d", app.overlay)
	}
}

// rowFor finds one role's row, or fails.
func rowFor(t *testing.T, catalog modelui.Catalog, role store.ModelRole) modelui.RoleRow {
	t.Helper()
	for _, row := range catalog.Roles {
		if row.Role == role {
			return row
		}
	}
	t.Fatalf("the catalog carries no %s row", role)
	return modelui.RoleRow{}
}

// runAll drives a command and everything a batch under it carries, which is
// what the runtime does and what a test asserting on a batched send has to.
func runAll(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, sub := range batch {
			runAll(t, sub)
		}
	}
}

// -- taking things out (JOURNEY 18) ---------------------------------------------

// The registry has named both copy doors since before this surface existed, and
// neither opened. What leaves is the RECORD — what was said and where the file
// is — never the rendering, which carries indents, glyphs and fold hints nobody
// wants in a paste.
func TestTheCopyDoorsTakeTheRecordAndNotTheRendering(t *testing.T) {
	backend := &fakeBackend{}
	backend.add(store.Message{SessionID: testSession, Role: store.RoleAgent,
		Body: "Here is the plan:\n\n- read navctx.rs",
		Parts: []store.MessagePart{{Kind: store.PartArtifact,
			Artifact: &store.ArtifactPart{Path: "workspace/wisp/plan.md"}}}})
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)

	if got := app.latestAnswer(); got != "Here is the plan:\n\n- read navctx.rs" {
		t.Fatalf("copy answer would paste %q", got)
	}
	if got := app.latestArtifact(); got != "workspace/wisp/plan.md" {
		t.Fatalf("copy file would paste %q", got)
	}
	if cmd := app.copyAnswer(); cmd == nil {
		t.Fatal("the copy-answer door produced no command")
	}
	if cmd := app.copyFile(); cmd == nil {
		t.Fatal("the copy-file door produced no command")
	}
	// Both are runnable from the palette, which is where 5.22's no-typed-only
	// rule is actually satisfied for a chord this footer has no room for.
	if cmd := app.runEntry("key.thread.copy-answer"); cmd == nil {
		t.Fatal("the palette cannot run the copy-answer row")
	}
}

// An empty room says why rather than putting nothing on the clipboard and
// looking like it worked (5.20 rule 3).
func TestCopyingFromAnEmptyRoomSaysWhy(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	if cmd := app.copyAnswer(); cmd != nil {
		t.Fatal("an empty room produced a clipboard write")
	}
	if app.status.err == "" {
		t.Fatal("a refused copy said nothing")
	}
	if reason := app.entryReason("key.thread.copy-file"); reason == "" {
		t.Fatal("the palette row offers a door with nothing behind it and no reason")
	}
}

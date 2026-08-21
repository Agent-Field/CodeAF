package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// The roles table is where the model economics of the whole system finally has
// one home. Before it, "which model does this" was answered in five different
// places — a flag, an environment variable, a preference file, a node column, a
// slot on a live client — and none of them could be asked a question by anybody
// else. This file is the answer to the question, journaled, and nothing more:
// bindings are storage, the chip is the only surface (5.10 / 8.2.16).
//
// THE ROLE LADDER (5.23, five roles, no more):
//
//	orchestrate  the voice     head and task-orchestrator turns      mid
//	plan         the architect compile, replan, graph revision       high
//	work         the hands     worker and executor turns             mid-high
//	verify       the skeptic   gates, judges, verify-then-escalate   cheap
//	scribe       the clerk     labels, titles, folds, briefs         ultra-cheap
//
// THE ORDERING LAW, which every consumer of this table cites and obeys:
// DETERMINISTIC FIRST, SCRIBE SECOND, FRONTIER LAST. Receipts, counts, states,
// and anything a template can say stay templates — free beats ultra-cheap, and
// a role binding is not a licence to spend a model on arithmetic. Scribe takes
// the prose-shaped one-liners. Verify runs before any escalation, because
// verify-then-escalate beat probing when it was measured. Frontier tiers are
// reserved for judgment. A sixth role needs the design doc amended before it
// needs code.
//
// Vision stays a capability flag resolved inside a role, not a role. Reasoning
// effort has no axis of its own here either: it is encoded in the model slug
// the catalog accepts (12.3.5), so a binding's value is exactly one string and
// inventing a second axis is a change to the doc before it is a change to this
// file.
//
// Nothing about the table is automatic. A binding takes effect at the next
// provider call of whatever reads it, silent downgrades are forbidden by the
// predictability law, and with no bindings at all every resolution in this file
// answers exactly what the system answered before the table existed.

// EventRoleBindingSet records one role binding decision, or — carrying Cleared
// — its removal. It is journaled against the node the scope names so an
// indexed reader can find a scope's history without scanning; a global binding
// names no node, exactly as a message names none.
const EventRoleBindingSet EventKind = "role_binding_set"

// roleBindingSchema is the projection: one row per (role, scope) that is
// currently bound. The journal is the record and this table is the answer, and
// the answer must be reachable in one indexed probe because resolution sits in
// front of dispatch. It holds no rows at all until somebody binds something.
const roleBindingSchema = `
CREATE TABLE IF NOT EXISTS role_bindings (
    role   TEXT NOT NULL,
    scope  TEXT NOT NULL,
    value  TEXT NOT NULL,
    origin TEXT NOT NULL DEFAULT '',
    seq    INTEGER NOT NULL,
    ts     TEXT NOT NULL,
    PRIMARY KEY (role, scope)
);
`

// ModelRole is one of the five named slots. It is a distinct type from the
// conversation's Role because they are different words for different things:
// one names who spoke, this one names what a call is for.
type ModelRole string

const (
	// RoleOrchestrate is the voice: head turns and task-orchestrator turns.
	RoleOrchestrate ModelRole = "orchestrate"
	// RolePlan is the architect: compiling, replanning, revising the graph.
	RolePlan ModelRole = "plan"
	// RoleWork is the hands: worker and executor turns.
	RoleWork ModelRole = "work"
	// RoleVerify is the skeptic: gates, judges, verify-then-escalate probes.
	RoleVerify ModelRole = "verify"
	// RoleScribe is the clerk: labels, titles, folds, briefs, sentinels — the
	// one-sentence-out jobs an ultra-cheap model does indistinguishably well.
	RoleScribe ModelRole = "scribe"
)

// ModelRoles lists the five in ladder order — the order the settings surface
// shows them in, and the order this doc names them. It is a fresh slice every
// call because a caller sorting the ladder must not resort the ladder.
func ModelRoles() []ModelRole {
	return []ModelRole{RoleOrchestrate, RolePlan, RoleWork, RoleVerify, RoleScribe}
}

// Word is the role's word in the product's language — what the chip says when
// it is not saying a model name. The words are the doc's (5.23) and not a
// second vocabulary invented here.
func (role ModelRole) Word() string {
	switch role {
	case RoleOrchestrate:
		return "voice"
	case RolePlan:
		return "architect"
	case RoleWork:
		return "hands"
	case RoleVerify:
		return "skeptic"
	case RoleScribe:
		return "clerk"
	}
	return ""
}

// Valid is a closed list and stays one. An unrecognized role is refused rather
// than stored, for the same reason an unrecognized issuer is: a typo that gets
// written becomes a binding nobody can find and nobody can clear.
func (role ModelRole) Valid() bool {
	switch role {
	case RoleOrchestrate, RolePlan, RoleWork, RoleVerify, RoleScribe:
		return true
	}
	return false
}

// ParseModelRole turns a typed or journaled word into a role. It trims and
// lowercases because the word arrives from a palette, a flag, and a JSON
// payload, and refuses everything else with ErrInvalid — never a panic, and
// never a silent promotion to some default role.
func ParseModelRole(text string) (ModelRole, error) {
	role := ModelRole(strings.ToLower(strings.TrimSpace(text)))
	if !role.Valid() {
		return "", fmt.Errorf("%w: %q is not one of the five roles", ErrInvalid, text)
	}
	return role, nil
}

// BindingScope names how far one binding reaches. The three shapes are the
// doc's: the machine, one task's subtree, one node.
type BindingScope string

// ScopeGlobal is the whole brain file — the binding a chip at home sets.
const ScopeGlobal BindingScope = "global"

const (
	taskScopePrefix = "task:"
	nodeScopePrefix = "node:"
)

// TaskScope reaches root and everything beneath it, the way a task ceiling
// does: the nearest governing root wins and an outer one takes over the moment
// the nearer one is cleared.
func TaskScope(root string) BindingScope {
	return BindingScope(taskScopePrefix + strings.TrimSpace(root))
}

// NodeScope reaches exactly one node. It is the finest binding and still not
// the finest word about a node's model — a pin outranks it, because a pin is
// what the node was promised and a binding is only what it inherits.
func NodeScope(id string) BindingScope {
	return BindingScope(nodeScopePrefix + strings.TrimSpace(id))
}

// Kind is "global", "task", "node", or "" for a scope nobody can interpret.
func (scope BindingScope) Kind() string {
	switch {
	case scope == ScopeGlobal:
		return "global"
	case strings.HasPrefix(string(scope), taskScopePrefix):
		if strings.TrimSpace(strings.TrimPrefix(string(scope), taskScopePrefix)) == "" {
			return ""
		}
		return "task"
	case strings.HasPrefix(string(scope), nodeScopePrefix):
		if strings.TrimSpace(strings.TrimPrefix(string(scope), nodeScopePrefix)) == "" {
			return ""
		}
		return "node"
	}
	return ""
}

// Target is the node id a task or node scope names, and empty for global. A
// scope that names nothing is not a scope; Kind reports it invalid and every
// writer refuses it.
func (scope BindingScope) Target() string {
	switch scope.Kind() {
	case "task":
		return strings.TrimSpace(strings.TrimPrefix(string(scope), taskScopePrefix))
	case "node":
		return strings.TrimSpace(strings.TrimPrefix(string(scope), nodeScopePrefix))
	}
	return ""
}

// Valid is Kind's closed list, said as a question.
func (scope BindingScope) Valid() bool { return scope.Kind() != "" }

// ParseBindingScope reads a scope word from a palette or a payload. Malformed
// input is ErrInvalid, including the two shapes that look almost right —
// "task:" and "node:" with nothing after them name no target at all.
func ParseBindingScope(text string) (BindingScope, error) {
	scope := BindingScope(strings.TrimSpace(text))
	if !scope.Valid() {
		return "", fmt.Errorf("%w: %q is not a binding scope", ErrInvalid, text)
	}
	return scope, nil
}

// RoleBinding is one journaled decision about what a role runs on inside one
// scope.
//
// Value is one model slug and carries its own effort, because effort has no
// axis of its own today (12.3.5). The store keeps a name and no opinion about
// which names exist — whether a slug reaches a reachable model is the client
// pool's question, one layer up, at dispatch, exactly as it is for a node's
// pinned model and for a worker's name.
//
// Cleared says the scope binds nothing again, which is not the same as binding
// the empty string: an unbound scope inherits from its parent scope, so the two
// must be different rows in the journal rather than the same one.
type RoleBinding struct {
	Role    ModelRole    `json:"role"`
	Scope   BindingScope `json:"scope"`
	Value   string       `json:"value,omitempty"`
	Origin  string       `json:"origin,omitempty"`
	Cleared bool         `json:"cleared,omitempty"`
	Seq     int64        `json:"-"`
}

// RoleSeedOriginPrefix marks an origin as an initializer's rather than a
// person's. It is the whole of the never-clobber rule: a seed may replace what
// a previous seed wrote, and may never replace what somebody chose.
const RoleSeedOriginPrefix = "seed:"

// RoleSource names which rung of the ladder answered a resolution. It exists so
// a receipt can say why a model was chosen — the predictability law forbids a
// switch without one — and so a chip can render "inherited" honestly.
type RoleSource string

const (
	// RoleFromPin is the node's own promised model, journaled at splice or by
	// CommandSetModel's subtree sweep. It outranks every binding.
	RoleFromPin RoleSource = "pin"
	// RoleFromNode is a binding on exactly this node.
	RoleFromNode RoleSource = "node"
	// RoleFromTask is a binding on the nearest ancestor task root, itself
	// included.
	RoleFromTask RoleSource = "task"
	// RoleFromGlobal is the machine-wide binding.
	RoleFromGlobal RoleSource = "global"
	// RoleFromDefault is the compiled-in fallback the surface installed.
	RoleFromDefault RoleSource = "default"
	// RoleUnbound is the answer on a machine that has bound nothing and
	// installed no defaults: the caller's own resolution stands, unchanged.
	RoleUnbound RoleSource = "unbound"
)

// ResolvedRole is one answer and the reason for it.
type ResolvedRole struct {
	Role   ModelRole
	Model  string
	Source RoleSource
	// Scope is the scope that answered, empty for a pin, a default, and an
	// unbound role.
	Scope BindingScope
}

// Bound reports whether anything at all answered. False is the untouched
// machine, and a caller that gets it must do exactly what it did before this
// table existed.
func (resolved ResolvedRole) Bound() bool { return resolved.Model != "" }

// RoleDefaults is the compiled-in floor of the ladder: what a role resolves to
// when nothing is bound anywhere. It is process configuration rather than
// journaled policy — it comes from flags, the environment, and the catalog,
// none of which belong in a brain file — so it is installed on the open store
// and never written to the journal.
type RoleDefaults map[ModelRole]string

// NewRoleDefaults seeds the five from the models a surface already resolved.
// Verify and scribe take the cheap model when the configuration names one and
// the work model when it does not, which is the honest fallback: the ladder is
// how spend is steered, and steering nowhere must never cost more than not
// steering. Empty arguments bind nothing rather than binding "".
func NewRoleDefaults(orchestrate, plan, work, cheap string) RoleDefaults {
	defaults := RoleDefaults{}
	defaults.set(RoleOrchestrate, orchestrate)
	defaults.set(RolePlan, plan)
	defaults.set(RoleWork, work)
	small := strings.TrimSpace(cheap)
	if small == "" {
		small = strings.TrimSpace(work)
	}
	defaults.set(RoleVerify, small)
	defaults.set(RoleScribe, small)
	return defaults
}

func (defaults RoleDefaults) set(role ModelRole, value string) {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		defaults[role] = trimmed
	}
}

func (defaults RoleDefaults) clone() RoleDefaults {
	if len(defaults) == 0 {
		return nil
	}
	copied := make(RoleDefaults, len(defaults))
	for role, value := range defaults {
		copied[role] = value
	}
	return copied
}

// roleDefaultsCell holds the installed defaults. It is a cell rather than a
// bare map because the surface installs it once at startup while dispatch reads
// it from every worker goroutine, and the zero value has to be usable — a store
// nobody configured resolves to unbound rather than to a nil-map panic.
type roleDefaultsCell struct {
	mu       sync.RWMutex
	defaults RoleDefaults
}

// InstallRoleDefaults sets the compiled-in floor for this process. It journals
// nothing: two machines sharing a brain file may be built and configured
// differently, and a default is a statement about this build, not about this
// history.
func (s *Store) InstallRoleDefaults(defaults RoleDefaults) {
	s.roleDefaults.mu.Lock()
	defer s.roleDefaults.mu.Unlock()
	s.roleDefaults.defaults = defaults.clone()
}

// RoleDefaults reports the installed floor, as a copy.
func (s *Store) RoleDefaults() RoleDefaults {
	s.roleDefaults.mu.RLock()
	defer s.roleDefaults.mu.RUnlock()
	return s.roleDefaults.defaults.clone()
}

func (s *Store) roleDefault(role ModelRole) string {
	s.roleDefaults.mu.RLock()
	defer s.roleDefaults.mu.RUnlock()
	return s.roleDefaults.defaults[role]
}

// SetRoleBinding binds one role inside one scope, and reports whether the
// journal grew.
//
// Asking twice writes once: a binding already carrying this value is left
// alone, because a journal records decisions that changed something and a
// re-set changed nothing. Origin is recorded and deliberately not compared —
// the same choice arriving from a chip and from a flag is the same choice, and
// a machine that re-declared it on every boot would fill the journal with
// events nobody made.
func (s *Store) SetRoleBinding(role ModelRole, scope BindingScope, value, origin string) (bool, error) {
	binding, err := validRoleBinding(role, scope, value, origin)
	if err != nil {
		return false, err
	}
	return s.journalRoleBinding(binding, func(current RoleBinding, bound bool) bool {
		return !bound || current.Value != binding.Value
	})
}

// SeedRoleBinding is the initializer's door: AFORGE_PLAN_MODEL and
// --plan-model set the global plan binding through it at startup.
//
// It writes in exactly two cases — nothing is bound yet, or what is bound was
// written by an initializer and the initializer now says something else. It
// never overwrites a binding a person made, because an environment variable
// that outranks the palette would make the palette a liar the next time the
// process restarted, and it never rewrites its own unchanged value, because
// every boot would then journal an event nobody caused.
func (s *Store) SeedRoleBinding(role ModelRole, scope BindingScope, value, origin string) (bool, error) {
	binding, err := validRoleBinding(role, scope, value, origin)
	if err != nil {
		return false, err
	}
	if !strings.HasPrefix(binding.Origin, RoleSeedOriginPrefix) {
		return false, fmt.Errorf("seed role binding: %w: origin must start with %q", ErrInvalid, RoleSeedOriginPrefix)
	}
	return s.journalRoleBinding(binding, func(current RoleBinding, bound bool) bool {
		if !bound {
			return true
		}
		if !strings.HasPrefix(current.Origin, RoleSeedOriginPrefix) {
			return false
		}
		return current.Value != binding.Value
	})
}

// ClearRoleBinding unbinds one role inside one scope and reports whether
// anything was there to unbind. A scope with nothing bound is not an error: the
// caller asked for a state the store is already in, and the parent scope — or
// the compiled-in default — resumes answering with no second pass.
func (s *Store) ClearRoleBinding(role ModelRole, scope BindingScope, origin string) (bool, error) {
	binding, err := validRoleBinding(role, scope, "", origin)
	if errors.Is(err, errRoleValueRequired) {
		err = nil
	}
	if err != nil {
		return false, err
	}
	binding.Cleared = true
	binding.Value = ""
	return s.journalRoleBinding(binding, func(_ RoleBinding, bound bool) bool { return bound })
}

var errRoleValueRequired = fmt.Errorf("%w: a binding needs a model", ErrInvalid)

func validRoleBinding(role ModelRole, scope BindingScope, value, origin string) (RoleBinding, error) {
	if !role.Valid() {
		return RoleBinding{}, fmt.Errorf("role binding: %w: %q is not one of the five roles", ErrInvalid, string(role))
	}
	if !scope.Valid() {
		return RoleBinding{}, fmt.Errorf("role binding: %w: %q is not a binding scope", ErrInvalid, string(scope))
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return RoleBinding{Role: role, Scope: scope, Origin: strings.TrimSpace(origin)}, errRoleValueRequired
	}
	return RoleBinding{Role: role, Scope: scope, Value: trimmed, Origin: strings.TrimSpace(origin)}, nil
}

// journalRoleBinding is the one write path. The decision to write is taken
// inside the transaction against the row as it stands there, so two surfaces
// binding the same role at the same moment cannot both decide they are the
// change.
func (s *Store) journalRoleBinding(binding RoleBinding, changed func(current RoleBinding, bound bool) bool) (bool, error) {
	tx, err := s.beginWrite()
	if err != nil {
		return false, fmt.Errorf("journal role binding: %w", err)
	}
	defer tx.Rollback()
	// A scope that names a node must name one that exists, for the reason a
	// ceiling must: a binding on a node nobody can reach is a policy nobody can
	// see and nobody can clear.
	if target := binding.Scope.Target(); target != "" {
		if err := requireNode(tx, target); err != nil {
			return false, fmt.Errorf("journal role binding: %w", err)
		}
	}
	current, bound, err := roleBindingAt(tx, binding.Role, binding.Scope)
	if err != nil {
		return false, fmt.Errorf("journal role binding: %w", err)
	}
	if !changed(current, bound) {
		return false, nil
	}
	seq, at, err := appendEvent(tx, binding.Scope.Target(), EventRoleBindingSet, binding)
	if err != nil {
		return false, fmt.Errorf("journal role binding: %w", err)
	}
	if err := applyRoleBindingView(tx, binding, seq, at); err != nil {
		return false, fmt.Errorf("journal role binding: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("journal role binding: %w", err)
	}
	return true, nil
}

func applyRoleBindingView(tx *sql.Tx, binding RoleBinding, seq int64, at time.Time) error {
	if binding.Cleared {
		_, err := tx.Exec(`DELETE FROM role_bindings WHERE role = ? AND scope = ?`, binding.Role, binding.Scope)
		return err
	}
	_, err := tx.Exec(`
		INSERT INTO role_bindings (role, scope, value, origin, seq, ts) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(role, scope) DO UPDATE SET
		    value = excluded.value, origin = excluded.origin,
		    seq = excluded.seq, ts = excluded.ts`,
		binding.Role, binding.Scope, binding.Value, binding.Origin, seq, formatTime(at))
	return err
}

// RoleBindingAt reads exactly what is bound at one scope, which is not the same
// question as what governs a node there. ResolveRole answers that one.
func (s *Store) RoleBindingAt(role ModelRole, scope BindingScope) (RoleBinding, bool, error) {
	if !role.Valid() {
		return RoleBinding{}, false, fmt.Errorf("read role binding: %w: %q is not one of the five roles", ErrInvalid, string(role))
	}
	if !scope.Valid() {
		return RoleBinding{}, false, fmt.Errorf("read role binding: %w: %q is not a binding scope", ErrInvalid, string(scope))
	}
	return roleBindingAt(s.db, role, scope)
}

func roleBindingAt(query rowQuerier, role ModelRole, scope BindingScope) (RoleBinding, bool, error) {
	binding := RoleBinding{Role: role, Scope: scope}
	err := query.QueryRow(`SELECT value, origin, seq FROM role_bindings WHERE role = ? AND scope = ?`,
		role, scope).Scan(&binding.Value, &binding.Origin, &binding.Seq)
	if errors.Is(err, sql.ErrNoRows) {
		return RoleBinding{}, false, nil
	}
	if err != nil {
		return RoleBinding{}, false, fmt.Errorf("read role binding %s@%s: %w", role, scope, err)
	}
	return binding, true, nil
}

// RoleBindings lists every binding in force, in ladder order and then by scope,
// which is the order the settings view shows five rows in.
func (s *Store) RoleBindings() ([]RoleBinding, error) {
	rows, err := s.db.Query(`SELECT role, scope, value, origin, seq FROM role_bindings`)
	if err != nil {
		return nil, fmt.Errorf("read role bindings: %w", err)
	}
	defer rows.Close()
	byRole := map[ModelRole][]RoleBinding{}
	for rows.Next() {
		var binding RoleBinding
		if err := rows.Scan(&binding.Role, &binding.Scope, &binding.Value, &binding.Origin, &binding.Seq); err != nil {
			return nil, fmt.Errorf("read role bindings: %w", err)
		}
		byRole[binding.Role] = append(byRole[binding.Role], binding)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read role bindings: %w", err)
	}
	bindings := make([]RoleBinding, 0, len(byRole))
	for _, role := range ModelRoles() {
		group := byRole[role]
		sort.SliceStable(group, func(first, second int) bool {
			return group[first].Scope < group[second].Scope
		})
		bindings = append(bindings, group...)
	}
	return bindings, nil
}

// ResolveRole answers what one role runs on at one node, and says which rung
// answered.
//
// The precedence is one sentence in five parts, and the order is the whole
// contract:
//
//  1. the node's PIN — nodes.work_model for the work role, nodes.plan_model for
//     the plan role. A pin is what the node was promised, by its splice or by
//     CommandSetModel's subtree sweep, and a promise outranks an inheritance.
//  2. a binding at node:<id> — this node and no other.
//  3. a binding at task:<root> — the nearest governing ancestor, itself
//     included, exactly as a task ceiling resolves.
//  4. a binding at global.
//  5. the compiled-in default this process installed.
//
// With nothing bound and no defaults installed the answer is RoleUnbound with
// an empty model, and a caller must then do precisely what it did before this
// table existed. That is the guarantee the empty table buys, and it is why the
// first thing this does after reading a pin is ask whether the table holds a
// single row for this role — on an untouched machine the walk never runs.
//
// An empty node id is the global question, which is what a chip at home asks:
// pins and scopes are skipped and resolution starts at global.
func (s *Store) ResolveRole(role ModelRole, nodeID string) (ResolvedRole, error) {
	if !role.Valid() {
		return ResolvedRole{}, fmt.Errorf("resolve role: %w: %q is not one of the five roles", ErrInvalid, string(role))
	}
	id := strings.TrimSpace(nodeID)
	pin := ""
	if id != "" && rolePins(role) {
		var err error
		if pin, err = rolePin(s.db, role, id); err != nil {
			return ResolvedRole{}, err
		}
	}
	return s.resolveRoleWithPin(role, id, pin)
}

// ResolveRoleForNode is the dispatch-path shape: the caller already holds the
// row, so the pin costs no query at all and an unbound machine answers out of
// one covering probe of an empty table.
func (s *Store) ResolveRoleForNode(role ModelRole, node Node) (ResolvedRole, error) {
	if !role.Valid() {
		return ResolvedRole{}, fmt.Errorf("resolve role: %w: %q is not one of the five roles", ErrInvalid, string(role))
	}
	pin := ""
	switch role {
	case RoleWork:
		pin = strings.TrimSpace(node.Provenance.WorkModel)
	case RolePlan:
		pin = strings.TrimSpace(node.Provenance.PlanModel)
	}
	return s.resolveRoleWithPin(role, strings.TrimSpace(node.ID), pin)
}

func (s *Store) resolveRoleWithPin(role ModelRole, nodeID, pin string) (ResolvedRole, error) {
	if pin != "" {
		return ResolvedRole{Role: role, Model: pin, Source: RoleFromPin}, nil
	}
	bound, err := roleBindingsExist(s.db, role)
	if err != nil {
		return ResolvedRole{}, err
	}
	if !bound {
		return s.roleFallback(role), nil
	}
	if nodeID != "" {
		binding, found, err := roleBindingAt(s.db, role, NodeScope(nodeID))
		if err != nil {
			return ResolvedRole{}, err
		}
		if found {
			return ResolvedRole{Role: role, Model: binding.Value, Source: RoleFromNode, Scope: binding.Scope}, nil
		}
		scope, value, found, err := nearestTaskRoleBinding(s.db, role, nodeID)
		if err != nil {
			return ResolvedRole{}, err
		}
		if found {
			return ResolvedRole{Role: role, Model: value, Source: RoleFromTask, Scope: scope}, nil
		}
	}
	binding, found, err := roleBindingAt(s.db, role, ScopeGlobal)
	if err != nil {
		return ResolvedRole{}, err
	}
	if found {
		return ResolvedRole{Role: role, Model: binding.Value, Source: RoleFromGlobal, Scope: ScopeGlobal}, nil
	}
	return s.roleFallback(role), nil
}

func (s *Store) roleFallback(role ModelRole) ResolvedRole {
	if value := s.roleDefault(role); value != "" {
		return ResolvedRole{Role: role, Model: value, Source: RoleFromDefault}
	}
	return ResolvedRole{Role: role, Source: RoleUnbound}
}

// rolePins reports whether a role has a pin column at all. Only work and plan
// do: they are the two the graph has always recorded per node, and a role with
// no pin skips the read rather than inventing one.
func rolePins(role ModelRole) bool { return role == RoleWork || role == RolePlan }

func rolePin(query rowQuerier, role ModelRole, nodeID string) (string, error) {
	column := "work_model"
	if role == RolePlan {
		column = "plan_model"
	}
	var pin string
	// One primary-key lookup. A node that is not there is not an error here:
	// resolution degrades to the scopes above it rather than refusing to answer
	// a question about a node that has been folded away underneath the caller.
	err := query.QueryRow(`SELECT `+column+` FROM nodes WHERE id = ?`, nodeID).Scan(&pin)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s pin for %q: %w", role, nodeID, err)
	}
	return strings.TrimSpace(pin), nil
}

// roleBindingsExistQuery is the short-circuit that keeps the unbound path free.
// role_bindings has no rows at all until somebody binds something, and the
// primary key's leading column is the role, so this is one covering probe of an
// index rather than a walk of anything.
const roleBindingsExistQuery = `SELECT EXISTS(SELECT 1 FROM role_bindings WHERE role = ?)`

func roleBindingsExist(query rowQuerier, role ModelRole) (bool, error) {
	var any int
	if err := query.QueryRow(roleBindingsExistQuery, role).Scan(&any); err != nil {
		return false, fmt.Errorf("read role bindings: %w", err)
	}
	return any != 0, nil
}

// nearestTaskRoleBindingQuery walks up from the node and stops at the first
// ancestor that binds this role. It is one recursive CTE resolved by primary
// key at each hop — the same ancestry idiom the task rail and the issuer check
// use, and for the same reason: walking up is bounded by the graph's depth,
// while walking down is bounded by a task's width, and a task may be wide.
//
// CROSS JOIN fixes the join order, ancestry outside and role_bindings inside,
// so the cost is one indexed lookup per hop instead of a read of the whole
// binding table per ancestor.
const nearestTaskRoleBindingQuery = `
	WITH RECURSIVE ancestry(id, parent_id, depth) AS (
	    SELECT id, parent_id, 0 FROM nodes WHERE id = ?
	    UNION ALL
	    SELECT parent.id, parent.parent_id, ancestry.depth + 1
	    FROM nodes AS parent
	    JOIN ancestry ON parent.id = ancestry.parent_id
	    WHERE ancestry.depth < ?
	)
	SELECT role_bindings.scope, role_bindings.value
	FROM ancestry CROSS JOIN role_bindings
	    ON role_bindings.role = ? AND role_bindings.scope = 'task:' || ancestry.id
	ORDER BY ancestry.depth
	LIMIT 1`

func nearestTaskRoleBinding(query rowQuerier, role ModelRole, nodeID string) (BindingScope, string, bool, error) {
	var (
		scope BindingScope
		value string
	)
	err := query.QueryRow(nearestTaskRoleBindingQuery, nodeID, maxAncestryDepth, role).Scan(&scope, &value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("resolve %s binding for %q: %w", role, nodeID, err)
	}
	return scope, value, true, nil
}

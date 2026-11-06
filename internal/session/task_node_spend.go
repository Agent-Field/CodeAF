package session

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// nodeTreeSpend is one older-road assignment's money and cancellation scope.
// The graph lock protects stopped; SpendGuard protects the shared tally.
type nodeTreeSpend struct {
	graph   *TaskGraph
	root    uint64
	guard   *SpendGuard
	ctx     context.Context
	halt    context.CancelFunc
	stopped bool
}

type nodeTreeSpendRecord struct {
	Spent float64 `json:"spent,omitempty"`
}

type nodeTreeKey struct{}

// Supervising helpers run on the owner, so their context carries the tree
// without giving the conversation the worker's persistent guard.
func withNodeTree(ctx context.Context, node *TaskNode) context.Context {
	if tree := node.graph.spendTree(node); tree != nil {
		return context.WithValue(ctx, nodeTreeKey{}, tree)
	}
	return ctx
}

func (a *Agent) nodeTreeForCall(ctx context.Context) *nodeTreeSpend {
	if tree, ok := ctx.Value(nodeTreeKey{}).(*nodeTreeSpend); ok {
		return tree
	}
	return a.config.nodeTree
}

func (g *TaskGraph) nodeSpendRecordLocked(node *TaskNode) *nodeTreeSpendRecord {
	if tree := g.nodeTrees[node.id]; tree != nil {
		return &nodeTreeSpendRecord{Spent: tree.guard.tally().Total()}
	}
	return node.savedTreeSpend
}

func (g *TaskGraph) prepareNodeTrees() {
	if bashBeltAsked() {
		return
	}
	g.mu.Lock()
	var roots []*TaskNode
	for _, id := range g.order {
		if node := g.nodes[id]; node != nil && node.parent == 0 && g.nodeTrees[id] == nil {
			roots = append(roots, node)
		}
	}
	g.mu.Unlock()
	for _, node := range roots {
		g.spendTree(node)
	}
}

func (g *TaskGraph) savedNodeTreeSpendLocked(root uint64) float64 {
	if node := g.nodes[root]; node != nil && node.savedTreeSpend != nil {
		return node.savedTreeSpend.Spent
	}
	// Older checkpoints have only node bills, which may include folded
	// descendants. Counting those conservatively cannot grant fresh money.
	spent := 0.0
	for _, node := range g.nodes {
		if node != nil && g.nodeRootLocked(node.id) == root {
			spent += node.cost
		}
	}
	return spent
}

func (g *TaskGraph) spendTree(node *TaskNode) *nodeTreeSpend {
	if bashBeltAsked() || node == nil {
		return nil
	}
	g.mu.Lock()
	root, home := g.nodeRootLocked(node.id), g.home
	tree := g.nodeTrees[root]
	if tree != nil || home == nil {
		g.mu.Unlock()
		return tree
	}
	spent := g.savedNodeTreeSpendLocked(root)
	g.mu.Unlock()
	// Read settings and the conversation's books outside the graph lock.
	taskCap, action := config.CrewTaskSpendCap(home.config.ProfileDir)
	conversationCap := home.railCap(0)
	allowance, conversation := taskTreeCost(taskCap, spent, runCostLeft(conversationCap, home.Usage().CostUSD))
	// The run engine's positive sentinel means no allowance, including for
	// an unpriced model whose first call cannot be estimated by SpendGuard.
	if allowance == math.SmallestNonzeroFloat64 {
		allowance = 0
	}
	if conversation {
		action = fmt.Sprintf("the conversation reached its %s limit · raise the per conversation limit in /settings", config.CrewTaskMoney(conversationCap))
	}
	ctx, halt := context.WithCancel(context.Background())
	candidate := &nodeTreeSpend{graph: g, root: root, ctx: ctx, halt: halt,
		guard: &SpendGuard{Price: config.CrewCallPriceAt(home.config.ProfileDir), TaskCap: spent + allowance, TaskAction: action, Task: &SpendTask{spent: spent}}}
	g.mu.Lock()
	if g.nodeTrees == nil {
		g.nodeTrees = make(map[uint64]*nodeTreeSpend)
	}
	if tree = g.nodeTrees[root]; tree == nil {
		tree = candidate
		g.nodeTrees[root] = tree
	}
	g.mu.Unlock()
	if tree != candidate {
		halt()
	}
	if tree.guard.tally().Total() >= tree.guard.TaskCap {
		tree.stop()
	}
	return tree
}

// nodeTreeStopLocked keeps the frontier and the landing on the same ending.
func (g *TaskGraph) nodeTreeStopLocked(node *TaskNode) string {
	tree := g.nodeTrees[g.nodeRootLocked(node.id)]
	if tree == nil || !tree.stopped {
		return ""
	}
	if node.ending == "" {
		node.ending = TaskEndingCostLimit
	}
	return tree.guard.TaskAction
}

func (g *TaskGraph) nodeReadinessLocked(node *TaskNode) (bool, string) {
	if limit := g.nodeTreeStopLocked(node); limit != "" {
		return false, limit
	}
	return g.readinessLocked(node)
}

func (g *TaskGraph) nodeTreeLandingLocked(node *TaskNode, state TaskState) TaskState {
	if limit := g.nodeTreeStopLocked(node); limit != "" {
		if !strings.Contains(node.report, limit) {
			node.landLocked(withReport(node.landed, limit))
		}
		return TaskFailed
	}
	return state
}

func (tree *nodeTreeSpend) stop() {
	g := tree.graph
	g.mu.Lock()
	if tree.stopped {
		g.mu.Unlock()
		return
	}
	tree.stopped = true
	var ids []uint64
	for _, id := range g.order {
		node := g.nodes[id]
		if node == nil || node.state.settled() || g.nodeRootLocked(id) != tree.root {
			continue
		}
		if node.ending == "" {
			node.ending = TaskEndingCostLimit
		}
		ids = append(ids, id)
	}
	g.mu.Unlock()
	// Mark the whole scope before any stop can turn the frontier again.
	for _, id := range ids {
		_, _ = g.stopFor(id, tree.guard.TaskAction)
	}
	tree.halt()
}

func (tree *nodeTreeSpend) book(ctx context.Context, model string, response *ai.Response, held float64) {
	tree.guard.after(ctx, model, response, held)
	tree.graph.checkpoint()
	if tree.guard.TaskCap > 0 && tree.guard.tally().Total() >= tree.guard.TaskCap {
		tree.stop()
	}
}

// nodeTreeCompleter covers turns and helpers alike, including discarded paid
// attempts. The existing crew and seat guards keep their independent bounds.
type nodeTreeCompleter struct {
	tree  *nodeTreeSpend
	model string
	next  Completer
}

func (c nodeTreeCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	c.tree.graph.mu.Lock()
	stopped := c.tree.stopped
	c.tree.graph.mu.Unlock()
	if stopped {
		return nil, ErrSpendStopped{Action: c.tree.guard.TaskAction, TaskLimit: true}
	}
	held, err := c.tree.guard.before(ctx, c.model, messages, options)
	if err != nil {
		var stopped ErrSpendStopped
		if errors.As(err, &stopped) && stopped.TaskLimit {
			c.tree.stop()
		}
		return nil, err
	}
	callCtx, release := context.WithCancel(ctx)
	defer release()
	unhook := context.AfterFunc(c.tree.ctx, release)
	defer unhook()
	callCtx = provider.WithDiscardedUsage(callCtx, func(_ string, _ string, response *ai.Response) {
		c.tree.book(ctx, c.model, response, 0)
	})
	response, err := c.next.CompleteWithMessages(callCtx, messages, options...)
	c.tree.book(ctx, c.model, response, held)
	return response, err
}

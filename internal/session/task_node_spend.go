package session

import (
	"fmt"
	"math"

	"github.com/Agent-Field/codeaf/internal/config"
)

// nodeTreeLimit is fixed at root admission and survives changes to the
// conversation's books or settings while an interrupted tree is restored.
type nodeTreeLimit struct {
	CeilingUSD         float64 `json:"ceiling_usd"`
	Conversation       bool    `json:"conversation,omitempty"`
	ConversationCapUSD float64 `json:"conversation_cap_usd,omitempty"`
}

// nodeBill is one node's live bill and the part of it that is other nodes'
// bills folded in ([TaskNode.folded]), read together for a checkpoint.
type nodeBill struct {
	spend, folded float64
}

// nodeTreeBills snapshots live bills before a checkpoint takes the graph lock.
// Atomic direction and app writes need the same unfolded costs as ordinary saves.
func (g *TaskGraph) nodeTreeBills() map[uint64]nodeBill {
	if bashBeltAsked() {
		return nil
	}
	g.mu.Lock()
	nodes := make([]*TaskNode, 0, len(g.nodes))
	for _, node := range g.nodes {
		if node != nil {
			nodes = append(nodes, node)
		}
	}
	g.mu.Unlock()
	bills := make(map[uint64]nodeBill, len(nodes))
	for _, node := range nodes {
		bills[node.id] = node.liveBill()
	}
	return bills
}

// liveBill is [TaskNode.spend] with the folded part beside it, both read off
// the same frozen figures and the same live worker.
func (n *TaskNode) liveBill() nodeBill {
	n.graph.mu.Lock()
	room, bill := n.room, nodeBill{spend: n.cost, folded: n.folded}
	n.graph.mu.Unlock()
	child := room.speaker()
	if child == nil {
		// The bill names the worker until its fold, as [TaskNode.spend] reads it.
		child = room.billed()
	}
	if child != nil {
		child.mu.Lock()
		bill.spend += child.usage.CostUSD
		bill.folded += child.foldedTaskUSD
		child.mu.Unlock()
	}
	return bill
}

// treeOwnSpend is what this node itself spent toward its tree's total: its bill
// without the parts that folded into it, which their own nodes count.
//
// A CHILD'S BILL IS IN ITS PARENT TOO. A part runs on the worker that proposed
// it and folds into that worker when it lands, so a sum of every node's
// [TaskNode.spend] counted a child twice and a grandchild three times, and a
// tree stopped at a fraction of its limit.
func (n *TaskNode) treeOwnSpend() float64 {
	bill := n.liveBill()
	return bill.spend - bill.folded
}

func (g *TaskGraph) prepareNodeTreeLimits() {
	if bashBeltAsked() {
		return
	}
	g.mu.Lock()
	var roots []*TaskNode
	for _, id := range g.order {
		if node := g.nodes[id]; node != nil && node.parent == 0 && node.treeLimit == nil {
			roots = append(roots, node)
		}
	}
	g.mu.Unlock()
	for _, node := range roots {
		g.setNodeTreeLimit(node)
	}
}

func (g *TaskGraph) setNodeTreeLimit(node *TaskNode) {
	if bashBeltAsked() || node == nil || node.parent != 0 {
		return
	}
	g.mu.Lock()
	home, saved, spent := g.home, node.treeLimit, 0.0
	if saved == nil {
		for _, member := range g.nodes {
			if member != nil && g.nodeRootLocked(member.id) == node.id {
				spent += member.cost - member.folded
			}
		}
	}
	g.mu.Unlock()
	if saved != nil || home == nil {
		return
	}
	// Old checkpoints have no ceiling fact. Their existing bills consume the
	// task cap, and only the conversation's current remainder is still offered.
	taskCap, _ := config.CrewTaskSpendCap(home.config.ProfileDir)
	conversationCap := home.railCap(0)
	allowance, conversation := taskTreeCost(taskCap, spent, runCostLeft(conversationCap, home.Usage().CostUSD))
	if allowance == math.SmallestNonzeroFloat64 {
		allowance = 0
	}
	limit := &nodeTreeLimit{CeilingUSD: taskCap, Conversation: conversation}
	if conversation {
		limit.CeilingUSD = spent + allowance
		limit.ConversationCapUSD = conversationCap
	}
	g.mu.Lock()
	if node.treeLimit == nil {
		node.treeLimit = limit
	}
	g.mu.Unlock()
}

// treeSpendLimit reads the same bills as the per-node boundary, each dollar
// once ([TaskNode.treeOwnSpend]). Collect under the graph lock, then release it
// before each node's bill takes that lock.
func (n *TaskNode) treeSpendLimit() string {
	if bashBeltAsked() {
		return ""
	}
	g := n.graph
	g.mu.Lock()
	root := g.nodeRootLocked(n.id)
	var limit *nodeTreeLimit
	if node := g.nodes[root]; node != nil {
		limit = node.treeLimit
	}
	var nodes []*TaskNode
	if limit != nil {
		for _, node := range g.nodes {
			if node != nil && g.nodeRootLocked(node.id) == root {
				nodes = append(nodes, node)
			}
		}
	}
	g.mu.Unlock()
	if limit == nil {
		return ""
	}
	spent := 0.0
	for _, node := range nodes {
		spent += node.treeOwnSpend()
	}
	if spent < limit.CeilingUSD {
		return ""
	}
	if limit.Conversation {
		return fmt.Sprintf("the conversation reached its %s limit · raise the per conversation limit in /settings", config.CrewTaskMoney(limit.ConversationCapUSD))
	}
	return config.CrewTaskCapAction(limit.CeilingUSD)
}

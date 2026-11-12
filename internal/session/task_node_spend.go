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

// nodeTreeBills snapshots live bills before a checkpoint takes the graph lock.
// Atomic direction and app writes need the same unfolded costs as ordinary saves.
func (g *TaskGraph) nodeTreeBills() map[uint64]float64 {
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
	bills := make(map[uint64]float64, len(nodes))
	for _, node := range nodes {
		bills[node.id] = node.spend()
	}
	return bills
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
				spent += member.cost
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

// treeSpendLimit reads the same bills as the per-node boundary. Collect under
// the graph lock, then release it before spend takes that lock for each node.
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
		spent += node.spend()
	}
	if spent < limit.CeilingUSD {
		return ""
	}
	if limit.Conversation {
		return fmt.Sprintf("the conversation reached its %s limit · raise the per conversation limit in /settings", config.CrewTaskMoney(limit.ConversationCapUSD))
	}
	return config.CrewTaskCapAction(limit.CeilingUSD)
}

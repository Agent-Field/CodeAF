package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

// render prints the graph. The schedule section deliberately shows the naive
// stage-barrier schedule beside the real one, because "stages do not gate
// execution" is the central claim of the design and it should be visible rather
// than asserted.
func render(graph *plan.Graph) {
	renderGround(graph)
	renderSpine(graph)
	renderNodes(graph)
	renderSchedule(graph)
	renderBriefs(graph)
	renderUsage(graph)
}

// renderGround shows the commitments the plan is built on. They are choices the
// goal did not make, so they are the first thing a person should be able to
// disagree with — and the cheapest thing to correct, since everything below
// inherits them.
func renderGround(graph *plan.Graph) {
	if len(graph.Settled) == 0 && len(graph.Open) == 0 {
		return
	}
	fmt.Printf("\n── settled ─────────────────────────────────────────────────────────\n")
	for _, item := range graph.Settled {
		fmt.Printf("  · %s\n", item)
	}
	for _, item := range graph.Open {
		fmt.Printf("  ? %s  (decided by the work)\n", item)
	}
}

func renderSpine(graph *plan.Graph) {
	if len(graph.Stages) == 0 {
		return
	}
	fmt.Printf("\n── spine ───────────────────────────────────────────────────────────\n")
	for index, stage := range graph.Stages {
		fmt.Printf("  S%d  %-14s %s\n", index+1, clip(stage.Title, 14), stage.Summary)
		if index < len(graph.Stages)-1 {
			fmt.Printf("      │\n")
		}
	}
}

func renderNodes(graph *plan.Graph) {
	fmt.Printf("\n── nodes ───────────────────────────────────────────────────────────\n")
	fmt.Printf("   #  %-24s %-11s %-12s %s\n", "node", "size", "waits for", "context received")
	for _, node := range graph.Nodes {
		waits, context := "—", "goal only"
		if len(node.Needs) > 0 {
			waits = joinInts(node.Needs)
			context = fmt.Sprintf("goal + %s", plural(len(node.Needs), "output"))
		}
		marker := " "
		size := string(node.Size)
		if node.Kind == plan.KindSynthesis {
			marker, size = "*", "synthesis"
		}
		// Indentation carries the hierarchy, so a spliced subtree reads as
		// belonging to the node it replaced rather than as more siblings.
		title := strings.Repeat("  ", node.Depth) + clip(node.Title, 24-2*node.Depth)
		fmt.Printf(" %s%2d  %-24s %-11s %-12s %s\n", marker, node.ID, title, size, waits, context)
	}
}

func renderBriefs(graph *plan.Graph) {
	printed := false
	for _, id := range graph.Leaves() {
		node := graph.Node(id)
		if node == nil || node.Brief == "" {
			continue
		}
		if !printed {
			fmt.Printf("\n── briefs ──────────────────────────────────────────────────────────\n")
			printed = true
		}
		fmt.Printf("\n%2d  %s\n", node.ID, node.Title)
		for _, line := range wrap(node.Brief, 68) {
			fmt.Printf("    %s\n", line)
		}
	}
}

// wrap breaks a brief to a readable width without needing a dependency.
func wrap(text string, width int) []string {
	var lines []string
	var line strings.Builder
	for _, word := range strings.Fields(text) {
		if line.Len() > 0 && line.Len()+1+len(word) > width {
			lines = append(lines, line.String())
			line.Reset()
		}
		if line.Len() > 0 {
			line.WriteString(" ")
		}
		line.WriteString(word)
	}
	if line.Len() > 0 {
		lines = append(lines, line.String())
	}
	return lines
}

func renderSchedule(graph *plan.Graph) {
	waves := graph.Waves()
	stageWaves := graph.StageWaves()
	if len(waves) == 0 {
		return
	}

	fmt.Printf("\n── schedule ────────────────────────────────────────────────────────\n")
	fmt.Printf("  by data flow            %s\n", plural(len(waves), "wave"))
	for index, wave := range waves {
		fmt.Printf("    %d  %s\n", index, describe(graph, wave))
	}
	if len(stageWaves) > 1 {
		fmt.Printf("\n  if stages were barriers %s\n", plural(len(stageWaves), "wave"))
		for index, wave := range stageWaves {
			fmt.Printf("    %d  %s\n", index, describe(graph, wave))
		}
	}

	freed := 0
	if len(stageWaves) > 0 {
		freed = len(waves[0]) - len(stageWaves[0])
	}
	fmt.Printf("\n  %d of %d nodes start immediately", graph.Roots(), len(graph.Nodes))
	if freed > 0 {
		fmt.Printf(" — %d freed from the stage barrier", freed)
	}
	fmt.Printf("\n  %s across %s, critical path %d (%d doing real work)\n",
		plural(graph.Edges(), "edge"), plural(len(graph.Nodes), "node"), len(waves), graph.WorkDepth())
	fmt.Printf("  %s to execute, decomposed %s deep",
		plural(len(graph.Leaves()), "leaf"), plural(graph.Depth()+1, "level"))
	if unresolved := graph.Unresolved(); unresolved > 0 {
		fmt.Printf(" — %d still oversized", unresolved)
	}
	fmt.Println()
}

func renderOperations(operations []plan.Operation) {
	if len(operations) == 0 {
		fmt.Printf("  no change — nothing in the plan was contradicted\n")
		return
	}
	fmt.Printf("── revisions ───────────────────────────────────────────────────────\n")
	for _, operation := range operations {
		status := "applied"
		if !operation.Applied {
			status = "refused: " + operation.Refused
		}
		label := operation.Title
		if label == "" {
			label = fmt.Sprintf("node %d", operation.Node)
		}
		fmt.Printf("  %-8s %-22s %s\n", operation.Op, clip(label, 22), status)
		if operation.Reason != "" {
			fmt.Printf("           %s\n", operation.Reason)
		}
	}
}

func renderUsage(graph *plan.Graph) {
	usage := graph.Usage
	fmt.Printf("\n%s  |  %d in (%d cached) / %d out tokens  |  $%.6f\n",
		plural(usage.Calls, "call"), usage.PromptTokens, usage.CachedTokens, usage.CompletionTokens, usage.Cost)
}

func describe(graph *plan.Graph, ids []int) string {
	sort.Ints(ids)
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if node := graph.Node(id); node != nil {
			names = append(names, fmt.Sprintf("%d·%s", id, node.Title))
		}
	}
	return strings.Join(names, "  ")
}

func joinInts(values []int) string {
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = fmt.Sprint(value)
	}
	return strings.Join(parts, ",")
}

func clip(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	return string(runes[:width-1]) + "…"
}

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// The text inspection joins captured documents for understanding, not for run
// admission. JSON and selected-Input inspection retain their existing shapes.
func writeMissionRead(stdout, stderr io.Writer, board *formations.BoardDocument, notes *formations.BoardNotesDocument, report formations.BoardValidationReport) int {
	if notes.BoardID != board.ID {
		return fail(stderr, fmt.Errorf("mission notes belong to %q, not displayed mission %q; retry inspection", notes.BoardID, board.ID))
	}
	nodes, err := missionReadNodes(board)
	if err != nil {
		return fail(stderr, err)
	}
	var text strings.Builder
	fmt.Fprintf(&text, "%s [%s] · %s · mission rev%d/notes rev%d\n", board.Title, board.ID, board.Slug, board.Rev, notes.Rev)
	text.WriteString("Graph outline.\n\nMISSION NOTES\n")
	if len(notes.Board) == 0 {
		text.WriteString("(no mission notes)\n")
	} else {
		writeMissionReadNotes(&text, notes.Board)
	}
	threads := map[string][]formations.NoteEntry{}
	for _, thread := range notes.Elements {
		threads[thread.NodeID] = append(threads[thread.NodeID], thread.Entries...)
	}
	known := map[string]bool{}
	routesRead := make([]bool, len(board.Connections))
	for _, node := range orderMissionReadNodes(nodes, board.Connections) {
		known[node.id] = true
		fmt.Fprintf(&text, "\n%s: %s [%s]\n", node.kind, node.title, node.id)
		for _, field := range sortedMissionReadFields(node.fields) {
			if field == "id" || field == "title" {
				continue
			}
			if field == "brief" {
				var brief map[string]json.RawMessage
				if err := json.Unmarshal(node.fields[field], &brief); err != nil {
					return fail(stderr, err)
				}
				for _, part := range sortedMissionReadFields(brief) {
					writeMissionReadField(&text, "brief "+part, brief[part])
				}
			} else {
				writeMissionReadField(&text, field, node.fields[field])
			}
		}
		if node.kind == "Input card" && node.fields["inputs"] == nil {
			text.WriteString("run input: brief · text · required (implicit)\n")
		}
		writeMissionReadNotes(&text, threads[node.id])
		for i, edge := range board.Connections {
			if !routesRead[i] && missionReadEndpointNode(edge.From) == node.id {
				fmt.Fprintf(&text, "%s -> %s [%s]\n", edge.From, edge.To, edge.ID)
				routesRead[i] = true
			}
		}
	}
	for i, edge := range board.Connections {
		if !routesRead[i] {
			fmt.Fprintf(&text, "\nUnattached route: %s -> %s [%s]\n", edge.From, edge.To, edge.ID)
		}
	}
	// Preserve thread order, including notes whose object was deleted. Do not
	// interpret QUESTION prefixes, replies, partial answers or contradictions.
	for _, thread := range notes.Elements {
		if !known[thread.NodeID] {
			fmt.Fprintf(&text, "\nOrphan notes [%s]\n", thread.NodeID)
			writeMissionReadNotes(&text, thread.Entries)
		}
	}
	fmt.Fprintf(&text, "\nVALIDATION: %d errors · %d warnings\n", len(report.Errors), len(report.Warnings))
	writeFindingsText(&text, "ERROR", report.Errors, board)
	writeFindingsText(&text, "WARN", report.Warnings, board)
	if _, err := io.WriteString(stdout, text.String()); err != nil {
		return fail(stderr, err)
	}
	return 0
}

func writeMissionReadNotes(text *strings.Builder, entries []formations.NoteEntry) {
	for _, entry := range entries {
		edited := ""
		if entry.EditedAt != nil {
			edited = " · edited"
		}
		fmt.Fprintf(text, "%s\t%s%s\n", entry.Author, entry.ID, edited)
		text.WriteString(entry.Text)
		text.WriteString("\n\n")
	}
}

func writeMissionReadField(text *strings.Builder, key string, value json.RawMessage) {
	var words string
	if json.Unmarshal(value, &words) == nil && string(value) != "null" {
		fmt.Fprintf(text, "%s: %s\n", key, words)
	} else {
		fmt.Fprintf(text, "%s: %s\n", key, value)
	}
}

func sortedMissionReadFields(fields map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type missionReadNode struct {
	id, title, kind string
	fields          map[string]json.RawMessage
}

// Render the model's complete configuration, so an added field does not vanish
// from the hand-over. Strings and brief goals remain directly readable.
func missionReadNodes(board *formations.BoardDocument) ([]missionReadNode, error) {
	var nodes []missionReadNode
	add := func(kind string, value any) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		var node missionReadNode
		node.kind = kind
		if err := json.Unmarshal(raw, &node.fields); err != nil {
			return err
		}
		_ = json.Unmarshal(node.fields["id"], &node.id)
		_ = json.Unmarshal(node.fields["title"], &node.title)
		nodes = append(nodes, node)
		return nil
	}
	for _, node := range board.Missions {
		if err := add("Input card", node); err != nil {
			return nil, err
		}
	}
	for _, node := range board.Formations {
		if err := add("Formation", node); err != nil {
			return nil, err
		}
	}
	for _, node := range board.Gates {
		if err := add("Gate", node); err != nil {
			return nil, err
		}
	}
	for _, node := range board.Tools {
		if err := add("Tool", node); err != nil {
			return nil, err
		}
	}
	for _, node := range board.Ends {
		if err := add("End node", node); err != nil {
			return nil, err
		}
	}
	for _, node := range board.Limits {
		if err := add("Limit card", node); err != nil {
			return nil, err
		}
	}
	return nodes, nil
}

// This ordering tolerates dangling edges, cycles and duplicate node IDs in
// readable drafts. It is an outline of the graph, not execution chronology.
func orderMissionReadNodes(nodes []missionReadNode, edges []formations.BoardConnection) []missionReadNode {
	byID := map[string][]int{}
	for i, node := range nodes {
		byID[node.id] = append(byID[node.id], i)
	}
	seen := make([]bool, len(nodes))
	ordered := make([]missionReadNode, 0, len(nodes))
	var visit func(int)
	visit = func(i int) {
		if seen[i] {
			return
		}
		seen[i] = true
		ordered = append(ordered, nodes[i])
		// Normal pass routes precede judging and rework branches. Stable input
		// order breaks ties and every actual node appears only once.
		outgoing := []formations.BoardConnection{}
		for _, edge := range edges {
			if missionReadEndpointNode(edge.From) == nodes[i].id {
				outgoing = append(outgoing, edge)
			}
		}
		priority := func(endpoint string) int {
			_, port, _ := strings.Cut(endpoint, ":")
			switch port {
			case "pass":
				return 0
			case "judge":
				return 1
			case "fail":
				return 2
			}
			return 0
		}
		sort.SliceStable(outgoing, func(a, b int) bool { return priority(outgoing[a].From) < priority(outgoing[b].From) })
		for _, edge := range outgoing {
			for _, j := range byID[missionReadEndpointNode(edge.To)] {
				visit(j)
			}
		}
	}
	for i, node := range nodes {
		if node.kind == "Input card" {
			visit(i)
		}
	}
	for i := range nodes {
		visit(i)
	}
	return ordered
}

func missionReadEndpointNode(endpoint string) string {
	node, _, _ := strings.Cut(endpoint, ":")
	return node
}

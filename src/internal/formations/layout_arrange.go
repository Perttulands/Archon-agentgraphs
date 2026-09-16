package formations

import (
	"errors"
	"sort"
	"strings"
)

const formationLayoutGrid = 28

type arrangementItem struct {
	id    string
	kind  string
	slots int
	// files is set when the card shows a row of referenced file chips.
	files bool
}

// ArrangeLayout is the one explicit whole-board layout operation used by both
// the API/UI and Archon. Loading, rendering, running, and reconnecting never
// call it.
func (s *Store) ArrangeLayout(slug string, opts WriteOptions) (*LayoutDocument, error) {
	if err := validateSlug(slug); err != nil {
		return nil, err
	}
	if opts.ExpectedETag == "" {
		return nil, ErrPreconditionRequired
	}
	var arranged *LayoutDocument
	err := s.withBoardDefinitionLock(slug, func(definition *definitionFile) error {
		raw, err := definition.readBytes()
		if err != nil {
			return err
		}
		board, err := parseBoardForWrite(raw)
		if err != nil {
			return err
		}
		layout, err := s.ReadLayout(slug)
		if err != nil {
			if !errors.Is(err, ErrNotFound) || opts.ExpectedETag != "*" {
				return err
			}
		} else if layout.BoardID != board.ID {
			return ErrConflict
		}
		arranged, err = s.updateLayoutNodes(slug, arrangedLayoutNodes(board), board, opts)
		return err
	})
	if err != nil {
		return nil, err
	}
	return arranged, nil
}

// arrangedLayoutNodes lays a board out along its main path. Columns follow the
// run: mission out, formation and Tool outputs, and gate pass. A gate's fail
// edges back to earlier steps and its judge wiring do not move columns; judge
// formations sit below the gate they judge. Nodes reached only through a fail
// edge follow the gate that sends them, and nodes no mission reaches follow the
// main path. The result depends only on the board.
func arrangedLayoutNodes(board *BoardDocument) []LayoutNode {
	items := make(map[string]arrangementItem, len(board.Missions)+len(board.Formations)+len(board.Gates)+len(board.Tools))
	missions := []string{}
	for _, mission := range board.Missions {
		items[mission.ID] = arrangementItem{id: mission.ID, kind: "mission", files: len(mission.Files) > 0}
		missions = append(missions, mission.ID)
	}
	for _, formation := range board.Formations {
		items[formation.ID] = arrangementItem{id: formation.ID, kind: formation.Type, slots: len(formation.Slots), files: formation.Brief != nil && len(formation.Brief.Files) > 0}
	}
	gates := []string{}
	for _, gate := range board.Gates {
		// A gate's card also shows the brief files of the formations judging it.
		files := len(gate.Files) > 0
		for _, judge := range judgeChainForGate(board, gate.ID) {
			files = files || (judge.Brief != nil && len(judge.Brief.Files) > 0)
		}
		items[gate.ID] = arrangementItem{id: gate.ID, kind: "gate", files: files}
		gates = append(gates, gate.ID)
	}
	for _, tool := range board.Tools {
		items[tool.ID] = arrangementItem{id: tool.ID, kind: "tool"}
	}
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	sort.Strings(missions)
	sort.Strings(gates)

	// Judge formations are placed with their gate, not on the main path.
	judgeOf := map[string]string{}
	judgesBelow := map[string][]string{}
	for _, gateID := range gates {
		for _, judge := range judgeChainForGate(board, gateID) {
			if _, taken := judgeOf[judge.ID]; taken {
				continue
			}
			judgeOf[judge.ID] = gateID
			judgesBelow[gateID] = append(judgesBelow[gateID], judge.ID)
		}
	}

	forward := map[string][]string{}
	fail := map[string][]string{}
	for _, connection := range board.Connections {
		from, fromPort, _ := strings.Cut(connection.From, ":")
		to, toPort, _ := strings.Cut(connection.To, ":")
		if from == to || items[from].id == "" || items[to].id == "" {
			continue
		}
		fromGate, toGate := items[from].kind == "gate", items[to].kind == "gate"
		switch {
		case fromGate && fromPort == "judge", toGate && toPort == "judge":
			continue
		case judgeOf[from] != "" || judgeOf[to] != "":
			continue
		case fromGate && fromPort == "fail":
			fail[from] = append(fail[from], to)
		default:
			forward[from] = append(forward[from], to)
		}
	}
	for _, targets := range []map[string][]string{forward, fail} {
		for id := range targets {
			sort.Strings(targets[id])
		}
	}

	depths := map[string]int{}
	order := []string{}
	place := func(roots []string, base func(string) int) {
		for id, depth := range arrangementLayers(roots, forward, depths, judgeOf) {
			depths[id] = depth.depth + base(depth.root)
		}
		order = append(order, arrangementDiscoveryOrder(roots, forward, depths, order)...)
	}
	place(missions, func(string) int { return 0 })

	// Steps reached only through a gate's fail edge follow that gate.
	for {
		failRoots := map[string]int{}
		for _, gateID := range gates {
			gateDepth, placed := depths[gateID]
			if !placed {
				continue
			}
			for _, target := range fail[gateID] {
				if _, done := depths[target]; done || judgeOf[target] != "" {
					continue
				}
				if current, seen := failRoots[target]; !seen || gateDepth+1 > current {
					failRoots[target] = gateDepth + 1
				}
			}
		}
		if len(failRoots) == 0 {
			break
		}
		roots := make([]string, 0, len(failRoots))
		for id := range failRoots {
			roots = append(roots, id)
		}
		sort.Strings(roots)
		place(roots, func(root string) int { return failRoots[root] })
	}

	// Nodes no mission reaches follow the main path, in their own order.
	after := 0
	for _, depth := range depths {
		if depth+1 > after {
			after = depth + 1
		}
	}
	for {
		reachedByUnplaced := map[string]bool{}
		for _, id := range ids {
			if _, done := depths[id]; done || judgeOf[id] != "" {
				continue
			}
			for _, target := range forward[id] {
				reachedByUnplaced[target] = true
			}
		}
		roots := []string{}
		for _, id := range ids {
			if _, done := depths[id]; !done && judgeOf[id] == "" && !reachedByUnplaced[id] {
				roots = append(roots, id)
			}
		}
		if len(roots) == 0 {
			// Only cycles remain: start from the first of them.
			for _, id := range ids {
				if _, done := depths[id]; !done && judgeOf[id] == "" {
					roots = []string{id}
					break
				}
			}
		}
		if len(roots) == 0 {
			break
		}
		place(roots, func(string) int { return after })
	}

	columns := map[int][]string{}
	columnOrder := []int{}
	for _, id := range order {
		depth := depths[id]
		if _, ok := columns[depth]; !ok {
			columnOrder = append(columnOrder, depth)
		}
		columns[depth] = append(columns[depth], id)
	}
	sort.Ints(columnOrder)

	nodes := make([]LayoutNode, 0, len(items))
	x := formationLayoutGrid * 4
	for _, depth := range columnOrder {
		y := formationLayoutGrid * 4
		width := 0
		for _, id := range columns[depth] {
			for _, placed := range append([]string{id}, judgesBelow[id]...) {
				item := items[placed]
				itemWidth, itemHeight := arrangementItemSize(item)
				nodes = append(nodes, LayoutNode{ID: placed, X: x, Y: y})
				y = snapLayoutUp(y + itemHeight + arrangementRowGap)
				if itemWidth > width {
					width = itemWidth
				}
			}
		}
		x = snapLayoutUp(x + width + 84)
	}
	return nodes
}

// arrangementRowGap leaves room under each card for its note preview.
const arrangementRowGap = 112

type arrangementLayer struct {
	root  string
	depth int
}

// arrangementLayers gives each node reachable from roots over forward edges its
// longest distance from a root, ignoring edges that close a cycle. Nodes that
// already have a depth, and judge formations, are not entered.
func arrangementLayers(roots []string, forward map[string][]string, placed map[string]int, judgeOf map[string]string) map[string]arrangementLayer {
	onStack := map[string]bool{}
	visited := map[string]bool{}
	rootOf := map[string]string{}
	predecessors := map[string][]string{}
	var visit func(id, root string)
	visit = func(id, root string) {
		visited[id] = true
		rootOf[id] = root
		onStack[id] = true
		for _, next := range forward[id] {
			if _, done := placed[next]; done || judgeOf[next] != "" || onStack[next] {
				continue
			}
			predecessors[next] = append(predecessors[next], id)
			if !visited[next] {
				visit(next, root)
			}
		}
		delete(onStack, id)
	}
	for _, root := range roots {
		if _, done := placed[root]; done || visited[root] {
			continue
		}
		visit(root, root)
	}
	layers := map[string]arrangementLayer{}
	var depthOf func(string) int
	depthOf = func(id string) int {
		if layer, ok := layers[id]; ok {
			return layer.depth
		}
		depth := 0
		for _, predecessor := range predecessors[id] {
			if candidate := depthOf(predecessor) + 1; candidate > depth {
				depth = candidate
			}
		}
		layers[id] = arrangementLayer{root: rootOf[id], depth: depth}
		return depth
	}
	for id := range visited {
		depthOf(id)
	}
	return layers
}

// arrangementDiscoveryOrder lists newly placed nodes in the order a walk from
// the roots meets them, so rows within a column follow the path.
func arrangementDiscoveryOrder(roots []string, forward map[string][]string, depths map[string]int, already []string) []string {
	seen := make(map[string]bool, len(already))
	for _, id := range already {
		seen[id] = true
	}
	order := []string{}
	queue := append([]string(nil), roots...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		if _, placed := depths[id]; !placed {
			continue
		}
		seen[id] = true
		order = append(order, id)
		queue = append(queue, forward[id]...)
	}
	return order
}

// arrangementItemSize is the room a card takes on the canvas. Heights are the
// cockpit's rendered heights (form-ged.10 card text, measured on Wayfinding and
// Delivery) plus a run-tools row and, when the card has referenced files, their
// chip row, so stacked cards never overlap.
func arrangementItemSize(item arrangementItem) (int, int) {
	width, height := 300, 310
	switch item.kind {
	case "mission":
		width, height = 236, 144
	case "gate":
		width, height = 300, 124
	case FormationTypePeer:
		width, height = 330, 340
	case FormationTypeOrchestrated:
		width, height = 320, 440
	}
	if item.files {
		height += arrangementFileRow
	}
	return width, height
}

// arrangementFileRow is the height of a card's referenced file chips.
const arrangementFileRow = 30

func snapLayoutUp(value int) int {
	return ((value + formationLayoutGrid - 1) / formationLayoutGrid) * formationLayoutGrid
}

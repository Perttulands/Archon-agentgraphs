import type { BoardDocument, LayoutNode } from './formationsTypes'
import { judgeChain } from '../flow/flowModel'
import { nodeFileRefs } from '../files/referencedFiles'

/** Display counterpart of formations/arrangedLayoutNodes. Keep graph traversal,
 * card sizes and spacing in sync with layout_arrange.go (Arrange's persisted layout).
 * No authored geometry or mission state is changed here. */
export function arrangedLayoutNodes(board: BoardDocument): LayoutNode[] {
  const items = new Map([
    ...(board.inputCards || []).map(node => [node.id, 'inputCard'] as const),
    ...board.formations.map(node => [node.id, node.type] as const),
    ...(board.gates || []).map(node => [node.id, 'gate'] as const),
    ...(board.tools || []).map(node => [node.id, 'tool'] as const),
    ...(board.ends || []).map(node => [node.id, 'end'] as const),
    ...(board.limits || []).map(node => [node.id, 'limit'] as const),
  ])
  const ids = [...items.keys()].sort()
  const gates = (board.gates || []).map(node => node.id).sort()
  const attached = new Set<string>()
  const below = new Map<string, string[]>()
  const append = (map: Map<string, string[]>, key: string, value: string) => map.set(key, [...(map.get(key) || []), value])
  for (const gate of gates) {
    for (const judge of judgeChain(board, gate)) {
      if (attached.has(judge)) continue
      attached.add(judge)
      append(below, gate, judge)
    }
  }
  const targets = new Set([...(board.inputCards || []), ...board.formations].map(node => node.id))
  for (const limit of board.limits || []) {
    if (!targets.has(limit.target)) continue
    attached.add(limit.id)
    append(below, limit.target, limit.id)
  }
  const forward = new Map<string, string[]>()
  const fail = new Map<string, string[]>()
  for (const edge of board.connections || []) {
    const [from, fromPort] = edge.from.split(':')
    const [to, toPort] = edge.to.split(':')
    if (from === to || !items.has(from) || !items.has(to)) continue
    if ((items.get(from) === 'gate' && fromPort === 'judge') || (items.get(to) === 'gate' && toPort === 'judge')) continue
    if (attached.has(from) || attached.has(to)) continue
    append(items.get(from) === 'gate' && fromPort === 'fail' ? fail : forward, from, to)
  }
  for (const map of [forward, fail]) for (const values of map.values()) values.sort()

  const depths = new Map<string, number>()
  const order: string[] = []
  const place = (roots: string[], base: (root: string) => number) => {
    // Longest forward path, dropping DFS back edges so draft cycles terminate.
    const visiting = new Set<string>()
    const rootOf = new Map<string, string>()
    const predecessors = new Map<string, string[]>()
    const visit = (id: string, root: string) => {
      rootOf.set(id, root)
      visiting.add(id)
      for (const next of forward.get(id) || []) {
        if (depths.has(next) || attached.has(next) || visiting.has(next)) continue
        append(predecessors, next, id)
        if (!rootOf.has(next)) visit(next, root)
      }
      visiting.delete(id)
    }
    for (const root of roots) if (!depths.has(root) && !rootOf.has(root)) visit(root, root)
    const layers = new Map<string, number>()
    const depthOf = (id: string): number => {
      if (layers.has(id)) return layers.get(id)!
      const depth = Math.max(0, ...(predecessors.get(id) || []).map(parent => depthOf(parent) + 1))
      layers.set(id, depth)
      return depth
    }
    for (const [id, root] of rootOf) depths.set(id, depthOf(id) + base(root))
    const seen = new Set(order)
    const queue = [...roots]
    for (let i = 0; i < queue.length; i++) {
      const id = queue[i]
      if (seen.has(id) || !depths.has(id)) continue
      seen.add(id)
      order.push(id)
      queue.push(...(forward.get(id) || []))
    }
  }
  place((board.inputCards || []).map(node => node.id).sort(), () => 0)
  while (true) {
    const roots = new Map<string, number>()
    for (const gate of gates) {
      if (!depths.has(gate)) continue
      for (const target of fail.get(gate) || []) {
        if (depths.has(target) || attached.has(target)) continue
        roots.set(target, Math.max(roots.get(target) || 0, depths.get(gate)! + 1))
      }
    }
    if (!roots.size) break
    place([...roots.keys()].sort(), root => roots.get(root)!)
  }
  const after = Math.max(0, ...[...depths.values()].map(depth => depth + 1))
  while (true) {
    const unplaced = ids.filter(id => !depths.has(id) && !attached.has(id))
    if (!unplaced.length) break
    const reached = new Set(unplaced.flatMap(id => forward.get(id) || []))
    const roots = unplaced.filter(id => !reached.has(id))
    place(roots.length ? roots : [unplaced[0]], () => after)
  }
  const columns = new Map<number, string[]>()
  for (const id of order) {
    const depth = depths.get(id)!
    columns.set(depth, [...(columns.get(depth) || []), id])
  }
  const stack = (id: string): string[] => [id, ...(below.get(id) || []).flatMap(stack)]
  const snapUp = (value: number) => Math.ceil(value / 28) * 28
  const nodes: LayoutNode[] = []
  // Match arrangementColumnsPerRow in Go: continue left to right below the
  // tallest branch/judge stack after four columns, rather than widening forever.
  let x = 112, rowY = 112, rowBottom = 112
  const columnOrder = [...columns.keys()].sort((a, b) => a - b)
  for (const [column, depth] of columnOrder.entries()) {
    if (column > 0 && column % 4 === 0) {
      x = 112
      rowY = rowBottom
    }
    let y = rowY
    let width = 0
    for (const id of columns.get(depth)!.flatMap(stack)) {
      const [w, h] = ({ inputCard: [260, 168], gate: [332, 132], end: [200, 64], limit: [220, 72], peer: [336, 360], orchestrated: [336, 460] } as Record<string, number[]>)[items.get(id)!] || [320, 320]
      nodes.push({ id, x, y })
      y = snapUp(y + h + 112 + (nodeFileRefs(board, id).length ? 30 : 0) + 112)
      width = Math.max(width, w)
    }
    rowBottom = Math.max(rowBottom, y)
    x = snapUp(x + width + 84)
  }
  return nodes
}

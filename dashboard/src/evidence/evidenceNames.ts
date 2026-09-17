import type { BoardDocument } from '../components/formationsTypes'

// Evidence speaks in the ledger's IDs. The board names them for the operator;
// a node the board no longer has keeps its ID.

export interface EvidenceNames {
  node: (nodeId: string) => string
  port: (nodeId: string, portId: string) => string
  slot: (nodeId: string, slotId: string) => string
  /** The seat that relayed a decision, by its slot ID alone: the slot's agent, else its label. */
  relayer: (slotId: string) => string
  /** Where a routed input came from: the step, and its port when it has several. */
  source: (nodeId?: string, portId?: string) => string
}

export function evidenceNamesForBoard(board: BoardDocument | null | undefined): EvidenceNames {
  const titles = new Map<string, string>()
  const ports = new Map<string, Map<string, string>>()
  const outputCounts = new Map<string, number>()
  const slots = new Map<string, Map<string, string>>()
  const relayers = new Map<string, string>()
  for (const mission of board?.missions || []) titles.set(mission.id, mission.title)
  for (const gate of board?.gates || []) titles.set(gate.id, gate.title)
  for (const node of [...(board?.formations || []), ...(board?.tools || [])]) {
    titles.set(node.id, node.title)
    ports.set(node.id, new Map([...node.inputs, ...node.outputs].map(port => [port.id, port.label])))
    outputCounts.set(node.id, node.outputs.length)
  }
  for (const formation of board?.formations || []) {
    slots.set(formation.id, new Map(formation.slots.map(slot => [slot.id, slot.label])))
    for (const slot of formation.slots) if (!relayers.has(slot.id)) relayers.set(slot.id, slot.agentId || `${slot.label} in ${formation.title}`)
  }
  const node = (nodeId: string) => titles.get(nodeId) || nodeId
  const port = (nodeId: string, portId: string) => ports.get(nodeId)?.get(portId) || portId
  return {
    node,
    port,
    slot: (nodeId, slotId) => slots.get(nodeId)?.get(slotId) || slotId,
    relayer: slotId => relayers.get(slotId) || slotId,
    source: (nodeId, portId) => {
      if (!nodeId) return 'the run brief'
      if (!titles.has(nodeId)) return [nodeId, portId].filter(Boolean).join(':')
      // A gate routes by verdict; a step with one output needs no port name.
      const gate = board?.gates?.some(item => item.id === nodeId)
      const named = portId && (gate || (outputCounts.get(nodeId) || 0) > 1)
      return named ? `${node(nodeId)} · ${gate ? portId : port(nodeId, portId)}` : node(nodeId)
    },
  }
}

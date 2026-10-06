import type { BoardDocument, LayoutDocument } from './formationsTypes'
import { boardStep, nodeDeleteUndo, portRemoveUndo, type UndoStep } from './formationsUndo'
import { judgeChain } from '../flow/flowModel'

/** Capture the inverse of the exact fields an undo will write, before writing
 * them. Redo uses current IDs, not a replay of allocating create commands. */
export function inverseUndoStep(board: BoardDocument, layout: LayoutDocument, step: UndoStep): UndoStep[] {
  if ('layout' in step) return [{ layout: {
    ...(step.layout.nodes ? { nodes: step.layout.nodes.map(node => layout.nodes.find(saved => saved.id === node.id) || node) } : {}),
    ...(step.layout.edges ? { edges: step.layout.edges.map(edge => layout.edges?.find(saved => saved.id === edge.id) || { id: edge.id, lane: 'auto' }) } : {}),
  } }]
  const [op, value] = Object.entries(step.board)[0]
  if (op === 'title') return [boardStep({title:board.title})]
  const patch = value as Record<string, unknown>
  const id = String(patch.id || '')
  if (op.startsWith('delete')) return nodeDeleteUndo(board, id, layout.nodes.find(node => node.id === id) || { x: 200, y: 200 })?.steps || []
  if (op === 'restoreNode') {
    const kind = ['formation', 'gate', 'inputCard', 'end', 'limit'].find(kind => patch[kind])
    if (!kind) throw new Error('The restored node has no kind.')
    return [boardStep({ [`delete${kind[0].toUpperCase()}${kind.slice(1)}`]: { id: (patch[kind] as { id: string }).id } })]
  }
  if (op.startsWith('update')) {
    const nodes = [...board.formations, ...(board.inputCards || []), ...(board.gates || []), ...(board.ends || []), ...(board.limits || [])]
    const previous = nodes.find(node => node.id === id) as unknown as Record<string, unknown> | undefined
    if (!previous) throw new Error('The edited card no longer exists.')
    const fields: Record<string, unknown> = { id }
    for (const key of Object.keys(patch).filter(key => key !== 'id')) fields[key] = previous[key] ?? (['files','inputs','kinds'].includes(key) ? [] : ['rounds','seconds','warnSeconds','tokens'].includes(key) ? 0 : '')
    return [boardStep({ [op]: fields })]
  }
  if (op === 'setBrief' || op === 'clearBrief') {
    const formationId = String(patch.formationId)
    const brief = board.formations.find(node => node.id === formationId)?.brief
    return [boardStep(brief ? { setBrief: { formationId, goal: brief.goal || '', beadId: brief.beadId || '', files: brief.files || [], links: brief.links || [] } } : { clearBrief: { formationId } })]
  }
  if (op === 'assignSlot') {
    const slot = board.formations.find(node => node.id === patch.formationId)?.slots.find(slot => slot.id === patch.slotId)
    if (!slot) throw new Error('The staffed slot no longer exists.')
    return [boardStep({ assignSlot: { formationId: patch.formationId, slotId: patch.slotId, agentId: slot.agentId || '', harness: slot.harness || '', model: slot.model || '', effort: slot.effort || '' } })]
  }
  if (op === 'setFormationType') {
    const formation = board.formations.find(node => node.id === id)
    if (!formation) throw new Error('The formation no longer exists.')
    return [boardStep({ setFormationType: { id, type: formation.type, slots: formation.slots,
      ...(formation.type === 'solo' && formation.slots.length === 1 ? { keepSlotId: formation.slots[0].id } : {}),
    } })]
  }
  if (op === 'wireConnection') return [boardStep({ unwireConnection: { from: patch.from, to: patch.to } })]
  if (op === 'unwireConnection') {
    const wire = board.connections.find(wire => wire.from === patch.from && wire.to === patch.to)
    if (!wire) throw new Error('The wire no longer exists.')
    return [boardStep({ wireConnection: { id: wire.id, from: wire.from, to: wire.to } })]
  }
  if (op === 'rewireConnection') {
    const steps = [boardStep({ rewireConnection: { from: patch.from, previousTo: patch.to, to: patch.previousTo } })]
    if (patch.removePreviousInput) {
      const [formationId, portId] = String(patch.previousTo).split(':')
      const formation = board.formations.find(node => node.id === formationId)
      const index = formation?.inputs.findIndex(port => port.id === portId) ?? -1
      if (formation && index >= 0) steps.unshift(boardStep({ restorePort: { formationId, direction: 'input', port: formation.inputs[index], index, connections: [] } }))
    }
    return steps
  }
  if (op === 'removePort') return portRemoveUndo(board, String(patch.formationId), String(patch.portId))?.steps || []
  if (op === 'restorePort') return [boardStep({ removePort: { formationId: patch.formationId, portId: (patch.port as { id: string }).id } })]
  if (op === 'setGateJudge' || op === 'detachGateJudge') {
    const gateId = String(patch.gateId)
    const chain = judgeChain(board, gateId)
    return [boardStep(chain.length ? { setGateJudge: { gateId, chain } } : { detachGateJudge: { gateId } })]
  }
  throw new Error(`Cannot reverse ${op}.`)
}

import type { BoardDocument } from '../components/formationsTypes'
import { judgeChain } from '../nodeWindow/boardRoutes'

// The files a board node references: a mission's or a gate's files and a
// formation's brief files. A gate also shows the brief files of the formations
// that judge it, so its rubric is on the gate whichever of them names it.

export interface ReferencedFile {
  ref: string
  /** The title of the node whose field names the file. */
  owner: string
  /** Named by the brief of a formation judging this gate. */
  judge?: boolean
}

export function nodeFileRefs(board: BoardDocument | null, nodeId: string): ReferencedFile[] {
  if (!board) return []
  const formationFiles = (id: string, judge?: boolean): ReferencedFile[] => {
    const formation = board.formations.find(node => node.id === id)
    return (formation?.brief?.files || []).map(ref => ({ ref, owner: formation?.title || id, ...(judge ? { judge } : {}) }))
  }
  const mission = board.missions?.find(node => node.id === nodeId)
  if (mission) return (mission.files || []).map(ref => ({ ref, owner: mission.title }))
  const gate = board.gates?.find(node => node.id === nodeId)
  if (!gate) return formationFiles(nodeId)
  const files: ReferencedFile[] = (gate.files || []).map(ref => ({ ref, owner: gate.title }))
  for (const judgeId of judgeChain(board, gate.id)) {
    for (const file of formationFiles(judgeId, true)) {
      if (!files.some(shown => shown.ref === file.ref)) files.push(file)
    }
  }
  return files
}

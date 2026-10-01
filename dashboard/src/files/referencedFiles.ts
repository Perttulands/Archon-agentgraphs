import { createContext, useContext } from 'react'
import type { BoardDocument, BoardFinding } from '../components/formationsTypes'
import { judgeChain } from '../nodeWindow/boardRoutes'
import { splitList } from '../components/formationsCockpitDom'

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
  const mission = board.inputCards?.find(node => node.id === nodeId)
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

// Why a referenced file cannot be opened, by its path as authored, from the
// mission's validation: a file that does not exist or a relative path. Its
// chip and its line in the node window say so (archon-n7u.26).
export type FileProblems = ReadonlyMap<string, string>

const FILE_PROBLEMS: Record<string, string> = {
  missing_file: 'does not exist',
  relative_file: 'is relative: use an absolute path',
}

export function fileProblems(findings: readonly BoardFinding[]): Map<string, string> {
  const problems = new Map<string, string>()
  for (const finding of findings) {
    const problem = FILE_PROBLEMS[finding.code]
    if (problem && finding.path) problems.set(finding.path, problem)
  }
  return problems
}

export const FileProblemsContext = createContext<FileProblems>(new Map())

export function useFileProblems(): FileProblems {
  return useContext(FileProblemsContext)
}

/** A relative file has no base, so the daemon refuses it (RELATIVE_FILE_REFERENCE);
 *  the node window says so where it is typed (archon-ka59). */
export function relativeFileProblem(value: string): string {
  const relative = splitList(value).find(file => !file.startsWith('/'))
  return relative ? `file "${relative}" is relative: use an absolute path` : ''
}

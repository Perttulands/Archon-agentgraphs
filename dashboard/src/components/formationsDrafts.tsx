/* Draft markers — authoring saves incomplete sketches, so the canvas shows
 * which nodes a run could not use yet. Board validation supplies the quiet
 * "draft" tags; a rejected run start marks the nodes it named. */
import type { BoardDocument, BoardFinding } from './formationsTypes'
import '../styles/formations-drafts.css'

export type NodeFindings = Map<string, BoardFinding[]>

/** Groups findings by canvas node. A connection finding marks both endpoint
 *  nodes; board-level findings (no nodeId) mark none. */
export function findingsByNode(board: BoardDocument | null, findings: BoardFinding[]): NodeFindings {
  const byNode: NodeFindings = new Map()
  const add = (nodeId: string, finding: BoardFinding) => {
    if (!nodeId) return
    const list = byNode.get(nodeId) ?? []
    if (!list.includes(finding)) list.push(finding)
    byNode.set(nodeId, list)
  }
  for (const finding of findings) {
    const connection = board?.connections.find(candidate => candidate.id === finding.nodeId)
    if (connection) {
      add(connection.from.split(':')[0], finding)
      add(connection.to.split(':')[0], finding)
    } else {
      add(finding.nodeId, finding)
    }
  }
  return byNode
}

/** Finding text for a node that is already named on screen: drops the leading
 *  `gate "gate_x" ` style reference the API adds for CLI readers. */
export function findingText(finding: BoardFinding): string {
  const prefix = `"${finding.nodeId}" `
  const at = finding.nodeId ? finding.message.indexOf(prefix) : -1
  if (at < 0 || !/^[A-Za-z ]*$/.test(finding.message.slice(0, at))) return finding.message
  return finding.message.slice(at + prefix.length)
}

/** Keeps the run findings that the latest board validation still reports. */
export function unresolvedFindings(runFindings: BoardFinding[], current: BoardFinding[]): BoardFinding[] {
  return runFindings.filter(finding => current.some(candidate => candidate.code === finding.code && candidate.nodeId === finding.nodeId))
}

export function DraftMarker({ nodeId, findings, blocked }: { nodeId: string; findings?: BoardFinding[]; blocked: boolean }) {
  if (!findings?.length) return null
  // A connection finding shown on its endpoint keeps its own reference.
  const summary = findings.map(finding => finding.nodeId === nodeId ? findingText(finding) : finding.message).join('\n')
  return (
    <span
      className={`draft-marker${blocked ? ' blocked' : ''}`}
      data-testid={`draft-marker-${nodeId}`}
      role="note"
      aria-label={blocked ? `Run needs: ${summary}` : `Draft: ${summary}`}
      title={summary}
    >
      {blocked ? 'needs fix' : 'draft'}
    </span>
  )
}

export function AdmissionFindingsPanel({ findings, titleOf, onDismiss }: {
  findings: BoardFinding[]
  titleOf: (nodeId: string) => string
  onDismiss: () => void
}) {
  if (!findings.length) return null
  return (
    <div className="admission-findings" role="alert" data-testid="admission-findings">
      <div className="admission-findings-hd">
        <span>Run needs {findings.length === 1 ? '1 fix' : `${findings.length} fixes`}</span>
        <button type="button" aria-label="Dismiss run findings" onClick={onDismiss}>x</button>
      </div>
      <ul>
        {findings.map(finding => {
          const title = finding.nodeId ? titleOf(finding.nodeId) : ''
          const named = title !== '' && title !== finding.nodeId
          return (
            <li key={`${finding.code}-${finding.nodeId}-${finding.message}`} data-node={finding.nodeId}>
              {named ? <span className="admission-findings-where">{title}</span> : null}
              <span>{named ? findingText(finding) : finding.message}</span>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

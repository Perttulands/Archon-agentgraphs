import { fetchApi } from '../components/formationsApi'

// Read models for the run evidence routes (ADR-0017). They mirror the Go
// response types in internal/formations/run_evidence*.go.

export interface EvidenceText {
  text: string
  bytes: number
  truncated?: boolean
}

export interface EvidenceRef {
  artifact?: string
  external?: string
}

export interface EvidenceInput {
  edgeId?: string
  fromNodeId?: string
  fromPortId?: string
  toPortId?: string
  text: EvidenceText
  ref?: EvidenceRef
}

export interface EvidencePort {
  portId: string
  text: EvidenceText
  ref?: EvidenceRef
}

export interface EvidenceOutput {
  seq: number
  status?: string
  reason?: EvidenceText
  text: EvidenceText
  ports: EvidencePort[]
}

export interface EvidenceDispatch {
  seq: number
  slotId?: string
  agentId?: string
  harness?: string
  phase?: string
  brief: boolean
  resultSeq?: number
  status?: string
}

export interface EvidenceAttempt {
  attempt: number
  startedSeq?: number
  reason?: string
  inputs: EvidenceInput[]
  dispatches: EvidenceDispatch[]
  seatCleanups?: Array<{ seq: number; slotId?: string; outcome: string }>
  output?: EvidenceOutput
}

export interface EvidenceItem {
  kind?: string
  text: EvidenceText
}

export interface EvidenceKindResult {
  seq: number
  kind: string
  verdict: string
  reason: EvidenceText
  evidence: EvidenceItem[]
  evidenceOmitted?: number
}

export interface EvidenceJudgeFailure {
  seq: number
  code?: string
  reason: EvidenceText
}

export interface EvidenceHumanRequest {
  seq: number
  pending: boolean
  /** relayedBy is the slot ID of the seat that recorded the operator's confirmed decision (ADR-0019). */
  decision?: { seq: number; verdict: string; response: EvidenceText; decidedBy?: string; relayedBy?: string }
}

export interface EvidenceGateVerdict {
  seq: number
  verdict: string
  reason: EvidenceText
  perKind?: Record<string, string>
  routePort?: string
  evidence: EvidenceItem[]
  evidenceOmitted?: number
}

export interface EvidenceEvaluation {
  seq: number
  attempt?: number
  kinds: string[]
  criterion: EvidenceText
  judgeChain?: string[]
  input?: EvidenceInput
  kindResults: EvidenceKindResult[]
  judgeFailures?: EvidenceJudgeFailure[]
  humanRequests?: EvidenceHumanRequest[]
  verdict?: EvidenceGateVerdict
}

export interface EvidenceProblem {
  seq: number
  type: string
  code?: string
  reason: EvidenceText
  resumeAllowed?: boolean
}

/** A block or error from anywhere in a run, with the nodes it names (none for, say, an exceeded wall clock). */
export interface RunProblem extends EvidenceProblem {
  nodeIds: string[]
}

export interface NodeEvidence {
  runId: string
  nodeId: string
  kind: 'mission' | 'formation' | 'gate' | 'tool'
  /** Display identity and topology from the board frozen at admission. */
  definition?: {
    title: string
    outputs: Array<{ id: string; label: string }>
    outgoing: Array<{ id: string; from: string; to: string }>
  }
  attempts?: EvidenceAttempt[]
  evaluations?: EvidenceEvaluation[]
  problems?: EvidenceProblem[]
}

export interface RunBrief {
  dispatchSeq: number
  nodeId: string
  slotId?: string
  attempt?: number
  text: EvidenceText
}

export interface RunArtifactEntry {
  name: string
  size: number
  modifiedAt: string
}

export type ArtifactKind = 'markdown' | 'json' | 'text' | 'image' | 'pdf' | 'binary'

export interface RunArtifactPreview extends RunArtifactEntry {
  kind: ArtifactKind
  text?: EvidenceText
}

const runPath = (runId: string) => `/api/formations/runs/${encodeURIComponent(runId)}`
const artifactPath = (name: string) => name.split('/').map(encodeURIComponent).join('/')

/**
 * Whether a problem is the pause the engine records after a human verdict until
 * the run resumes. Ledgers before its code carry only the fixed reason.
 */
export function isHumanVerdictPause(problem: EvidenceProblem): boolean {
  return problem.type === 'run_blocked'
    && (problem.code === 'resume_after_verdict' || problem.reason.text === 'human gate verdict recorded; resume required')
}

export async function fetchNodeEvidence(runId: string, nodeId: string): Promise<NodeEvidence> {
  const { data } = await fetchApi<{ evidence?: NodeEvidence }>(`${runPath(runId)}/evidence/nodes/${encodeURIComponent(nodeId)}`)
  if (!data.evidence) throw new Error('the daemon returned no node evidence')
  return data.evidence
}

/** Every block and error the run recorded, oldest first, including those that name no node. */
export async function fetchRunProblems(runId: string): Promise<RunProblem[]> {
  const { data } = await fetchApi<{ problems?: RunProblem[] }>(`${runPath(runId)}/evidence/problems`)
  return data.problems || []
}

export async function fetchRunBrief(runId: string, dispatchSeq: number): Promise<RunBrief> {
  const { data } = await fetchApi<{ brief?: RunBrief }>(`${runPath(runId)}/evidence/briefs/${dispatchSeq}`)
  if (!data.brief) throw new Error('the daemon returned no brief')
  return data.brief
}

export async function fetchRunArtifacts(runId: string): Promise<{ artifacts: RunArtifactEntry[]; truncated: boolean }> {
  const { data } = await fetchApi<{ artifacts?: RunArtifactEntry[]; truncated?: boolean }>(`${runPath(runId)}/evidence/artifacts`)
  return { artifacts: data.artifacts || [], truncated: Boolean(data.truncated) }
}

export async function fetchArtifactPreview(runId: string, name: string): Promise<RunArtifactPreview> {
  const { data } = await fetchApi<{ artifact?: RunArtifactPreview }>(`${runPath(runId)}/evidence/artifacts/${artifactPath(name)}`)
  if (!data.artifact) throw new Error('the daemon returned no artifact')
  return data.artifact
}

/** The raw artifact URL, for a new tab or an image source. */
export function artifactRawUrl(runId: string, name: string): string {
  return `${runPath(runId)}/artifacts/${artifactPath(name)}`
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(bytes < 10 * 1024 ? 1 : 0)} KiB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`
}

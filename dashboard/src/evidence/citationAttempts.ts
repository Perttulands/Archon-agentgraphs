import { evidencePaths, attemptCitationScope, type CitationScope } from './CitationFiles'
import { fetchRunAttempts, type EvidenceEvaluation, type NodeEvidence, type StepAttempt, type StepAttempts } from './runEvidenceApi'

/** Read only the steps named by this evidence, including reports written by judges. */
export async function fetchCitationAttempts(runId: string, evidence: NodeEvidence): Promise<StepAttempts[]> {
  const nodes = new Set<string>()
  if (evidence.kind === 'formation') nodes.add(evidence.nodeId)
  for (const evaluation of evidence.evaluations || []) {
    if (evaluation.input?.fromNodeId) nodes.add(evaluation.input.fromNodeId)
    for (const judge of evaluation.judgeChain || []) nodes.add(judge)
  }
  for (const node of citedAttemptNodes(runId, evidence)) nodes.add(node)
  const results = await Promise.allSettled([...nodes].map(node => fetchRunAttempts(runId, node)))
  const steps = results.flatMap(result => result.status === 'fulfilled' ? result.value.steps : [])
  const judges = citedAttemptNodes(runId, steps).filter(node => !nodes.has(node))
  const extra = await Promise.allSettled(judges.map(node => fetchRunAttempts(runId, node)))
  return [...steps, ...extra.flatMap(result => result.status === 'fulfilled' ? result.value.steps : [])]
}

export function citedAttemptNodes(runId: string, evidence: unknown): string[] {
  const marker = `/artifacts/${runId}/`
  return [...new Set(evidencePaths(evidence).flatMap(path => {
    const suffix = path.split(marker)[1]
    return suffix && /^[^/]+\/\d+\//.test(suffix) ? [suffix.split('/')[0]] : []
  }))]
}

/** Explicit report paths identify the judge attempt; never use another round's files. */
export function withJudgingAttempts(attempt: StepAttempt, steps: StepAttempts[]): CitationScope {
  const reports = evidencePaths(attempt.verdicts)
  const judges = steps.flatMap(step => step.attempts.filter(candidate => candidate !== attempt && candidate.directory && reports.some(path => path.startsWith(`${candidate.directory!.path}/`))))
  return mergeScopes([attemptCitationScope(attempt), ...judges.map(attemptCitationScope)], reports)
}

export function evaluationCitationScope(evaluation: EvidenceEvaluation, steps: StepAttempts[]): CitationScope {
  const reports = evidencePaths(evaluation)
  const matched = steps.flatMap(step => step.attempts.filter(attempt => attempt.verdicts.some(verdict => verdict.evaluationSeq === evaluation.seq) || (attempt.directory && reports.some(path => path.startsWith(`${attempt.directory!.path}/`)))))
  return mergeScopes(matched.map(attempt => withJudgingAttempts(attempt, steps)), reports)
}

function mergeScopes(scopes: CitationScope[], paths: string[]): CitationScope {
  return { artifacts: scopes.flatMap(scope => scope.artifacts), directories: scopes.flatMap(scope => scope.directories), paths: [...paths, ...scopes.flatMap(scope => scope.paths)] }
}
